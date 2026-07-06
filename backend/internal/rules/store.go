package rules

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// Store is the single reader/writer of rule-instance snapshots in the
// append-only agentguard_rules stream. Current state is reconstructed with
// arg_max(..., revision); Append writes one complete 21-column snapshot row.
// It is the persistence seam the lifecycle service and HTTP handlers consume —
// no other code should hand-write INSERT INTO agentguard_rules or read the
// stream directly.
type Store struct {
	q execQuerier
}

// execQuerier is the minimal Timeplus surface the Store needs: a ctx-less
// reader and a ctx-less writer, mirroring timeplus.Query / timeplus.ExecHTTP.
// The seam exists so the store's SQL construction and row parsing can be
// unit-tested with a fake, without a live Proton.
type execQuerier interface {
	Query(sql string) ([]timeplus.Row, error)
	Exec(sql string) error
}

// protonQuerier binds the package-level timeplus.Query / timeplus.ExecHTTP
// functions to a single held config so they satisfy execQuerier.
type protonQuerier struct {
	cfg *config.TimeplusConfig
}

func (p protonQuerier) Query(sql string) ([]timeplus.Row, error) {
	return timeplus.Query(p.cfg, sql)
}

func (p protonQuerier) Exec(sql string) error {
	return timeplus.ExecHTTP(p.cfg, sql)
}

// NewStore returns a Store that talks to Timeplus over the HTTP interface using
// the held config.
func NewStore(cfg *config.TimeplusConfig) *Store {
	return &Store{q: protonQuerier{cfg: cfg}}
}

// rulesCurrentProjection reconstructs one current-state row per rule from the
// snapshot log. Every value column is reduced with arg_max(..., revision) and
// the revision itself with max(revision): ordering is ALWAYS by revision, never
// by updated_at — replays and clock skew can reorder wall-clock timestamps, and
// arg_max tie-breaks a tied max to the first-seen value. table(agentguard_rules)
// is a bounded historical read. Callers append GROUP BY plus any WHERE/HAVING.
// NOTE: the max-revision column is aliased max_revision, NOT revision. Proton
// (verified on 3.2.1) resolves a name used inside an aggregate to a same-named
// SELECT alias first, so `max(revision) AS revision` would make every sibling
// arg_max(col, revision) read as arg_max(col, max(revision)) — an aggregate
// inside an aggregate (ILLEGAL_AGGREGATION). Keeping the alias distinct from the
// `revision` base column avoids the shadow; scanInstance reads max_revision.
const rulesCurrentProjection = `SELECT
  rule_id,
  max(revision) AS max_revision,
  arg_max(snapshot_id, revision) AS snapshot_id,
  arg_max(name, revision) AS name,
  arg_max(definition_version, revision) AS definition_version,
  arg_max(definition_hash, revision) AS definition_hash,
  arg_max(description, revision) AS description,
  arg_max(rendered_sql, revision) AS rendered_sql,
  arg_max(deployed_udfs_json, revision) AS deployed_udfs_json,
  arg_max(parameter_overrides_json, revision) AS parameter_overrides_json,
  arg_max(enabled, revision) AS enabled,
  arg_max(deleted, revision) AS deleted,
  arg_max(severity, revision) AS severity,
  arg_max(category, revision) AS category,
  arg_max(required_telemetry, revision) AS required_telemetry,
  arg_max(fp_rate, revision) AS fp_rate,
  arg_max(hook_sources, revision) AS hook_sources,
  arg_max(block_policy, revision) AS block_policy,
  arg_max(update_reason, revision) AS update_reason,
  arg_max(updated_by, revision) AS updated_by,
  arg_max(updated_at, revision) AS updated_at
FROM table(agentguard_rules)`

// ListCurrent returns the latest snapshot of every rule whose latest snapshot
// is NOT deleted (i.e. the currently-installed rules), reconstructed via
// arg_max(..., revision). Soft-deleted rules are excluded in an OUTER scope.
//
// The deleted filter MUST NOT live in HAVING: the projection aliases
// arg_max(deleted, revision) AS deleted, and in HAVING the `deleted` argument of
// a repeated arg_max(deleted, revision) resolves to that same-named SELECT alias
// (= the arg_max itself), nesting aggregates → Code 184 ILLEGAL_AGGREGATION.
// timeplusd 3.2.1 tolerated it but 3.2.11 (CI) rejects it. Wrapping the grouped
// projection in a subquery and filtering its plain `deleted` output column is the
// version-stable, alias-shadow-free pattern (the same one holds.go uses).
func (s *Store) ListCurrent() ([]RuleInstance, error) {
	sql := "SELECT * FROM (\n" + rulesCurrentProjection +
		"\nGROUP BY rule_id\n) WHERE deleted = false"
	rows, err := s.q.Query(sql)
	if err != nil {
		return nil, fmt.Errorf("rules store: list current: %w", err)
	}
	out := make([]RuleInstance, 0, len(rows))
	for _, row := range rows {
		inst, err := scanInstance(row)
		if err != nil {
			return nil, fmt.Errorf("rules store: list current: %w", err)
		}
		out = append(out, inst)
	}
	return out, nil
}

// LatestSnapshot returns the highest-revision snapshot for one rule REGARDLESS
// of its deleted flag; found is false only when the rule has never had any
// snapshot. Callers inspect .Deleted for current state and use .Revision to
// compute the next revision (current.Revision + 1).
//
// It deliberately does NOT filter on deleted. That is what guarantees revision
// monotonicity across an uninstall→reinstall cycle: if a rule was installed
// (rev 1..3) then uninstalled (rev 4, deleted=true), a later reinstall computes
// rev 5, not rev 1. Filtering deleted rows out here would hide rev 4, the
// reinstall would reuse a low revision, and arg_max would then shadow it under
// the old deleted snapshot — a silent correctness bug.
func (s *Store) LatestSnapshot(ruleID string) (inst RuleInstance, found bool, err error) {
	sql := rulesCurrentProjection +
		"\nWHERE rule_id = '" + EscapeSQLString(ruleID) + "'" +
		"\nGROUP BY rule_id"
	rows, err := s.q.Query(sql)
	if err != nil {
		return RuleInstance{}, false, fmt.Errorf("rules store: latest snapshot %q: %w", ruleID, err)
	}
	if len(rows) == 0 {
		return RuleInstance{}, false, nil
	}
	inst, err = scanInstance(rows[0])
	if err != nil {
		return RuleInstance{}, false, fmt.Errorf("rules store: latest snapshot %q: %w", ruleID, err)
	}
	return inst, true, nil
}

// Append writes ONE complete snapshot row — all 21 columns, never a partial
// INSERT. It stamps updated_at server-side with now64(9) and persists
// snapshot_id and revision exactly as given; computing those (under a per-rule
// lock) is the caller's responsibility, not this layer's. Empty/nil overrides
// and UDFs are persisted as valid JSON ("{}" / "[]"), never as blank strings.
func (s *Store) Append(inst RuleInstance) error {
	overridesJSON, err := marshalOverridesJSON(inst.Overrides)
	if err != nil {
		return fmt.Errorf("rules store: append %q: %w", inst.RuleID, err)
	}
	udfsJSON, err := marshalUDFsJSON(inst.DeployedUDFs)
	if err != nil {
		return fmt.Errorf("rules store: append %q: %w", inst.RuleID, err)
	}

	sql := fmt.Sprintf(
		`INSERT INTO agentguard_rules (`+
			`rule_id, snapshot_id, revision, name, definition_version, definition_hash, `+
			`description, rendered_sql, deployed_udfs_json, parameter_overrides_json, `+
			`enabled, deleted, severity, category, required_telemetry, fp_rate, `+
			`hook_sources, block_policy, update_reason, updated_by, updated_at`+
			`) VALUES (`+
			`'%s','%s',%s,'%s','%s','%s',`+
			`'%s','%s','%s','%s',`+
			`%t,%t,'%s','%s','%s','%s',`+
			`'%s','%s','%s','%s',now64(9))`,
		EscapeSQLString(inst.RuleID),
		EscapeSQLString(inst.SnapshotID),
		strconv.FormatUint(inst.Revision, 10),
		EscapeSQLString(inst.Name),
		EscapeSQLString(inst.DefinitionVersion),
		EscapeSQLString(inst.DefinitionHash),
		EscapeSQLString(inst.Description),
		EscapeSQLString(inst.RenderedSQL),
		EscapeSQLString(udfsJSON),
		EscapeSQLString(overridesJSON),
		inst.Enabled,
		inst.Deleted,
		EscapeSQLString(inst.Severity),
		EscapeSQLString(inst.Category),
		EscapeSQLString(inst.RequiredTelemetry),
		EscapeSQLString(inst.FPRate),
		EscapeSQLString(inst.HookSources),
		EscapeSQLString(inst.BlockPolicy),
		EscapeSQLString(inst.UpdateReason),
		EscapeSQLString(inst.UpdatedBy),
	)
	if err := s.q.Exec(sql); err != nil {
		return fmt.Errorf("rules store: append %q: %w", inst.RuleID, err)
	}
	return nil
}

// ── Row scanning ───────────────────────────────────────────────────────────────

// scanInstance maps one query row (as decoded by timeplus.Query, which uses
// json.Decoder.UseNumber()) into a RuleInstance, decoding the two JSON columns.
func scanInstance(row timeplus.Row) (RuleInstance, error) {
	ruleID := rowStr(row["rule_id"])
	// revision is the monotonicity key, so a missing/unparseable value is an
	// error — never a silent 0, which would let a reinstall reuse a low revision
	// that arg_max then shadows under an older snapshot. The projection exposes it
	// as max_revision (the alias is kept distinct from the revision column to avoid
	// Proton's aggregate-alias shadowing; see rulesCurrentProjection).
	revision, err := rowUint64(row["max_revision"])
	if err != nil {
		return RuleInstance{}, fmt.Errorf("rule %q: revision: %w", ruleID, err)
	}
	inst := RuleInstance{
		RuleID:            ruleID,
		SnapshotID:        rowStr(row["snapshot_id"]),
		Revision:          revision,
		Name:              rowStr(row["name"]),
		DefinitionVersion: rowStr(row["definition_version"]),
		DefinitionHash:    rowStr(row["definition_hash"]),
		Description:       rowStr(row["description"]),
		RenderedSQL:       rowStr(row["rendered_sql"]),
		Enabled:           rowBool(row["enabled"]),
		Deleted:           rowBool(row["deleted"]),
		Severity:          rowStr(row["severity"]),
		Category:          rowStr(row["category"]),
		RequiredTelemetry: rowStr(row["required_telemetry"]),
		FPRate:            rowStr(row["fp_rate"]),
		HookSources:       rowStr(row["hook_sources"]),
		BlockPolicy:       rowStr(row["block_policy"]),
		UpdateReason:      rowStr(row["update_reason"]),
		UpdatedBy:         rowStr(row["updated_by"]),
		UpdatedAt:         rowTime(row["updated_at"]),
	}
	udfs, err := decodeUDFsJSON(rowStr(row["deployed_udfs_json"]))
	if err != nil {
		return RuleInstance{}, fmt.Errorf("rule %q: %w", inst.RuleID, err)
	}
	inst.DeployedUDFs = udfs
	overrides, err := decodeOverridesJSON(rowStr(row["parameter_overrides_json"]))
	if err != nil {
		return RuleInstance{}, fmt.Errorf("rule %q: %w", inst.RuleID, err)
	}
	inst.Overrides = overrides
	return inst, nil
}

// ── JSON column codecs ──────────────────────────────────────────────────────────

// marshalOverridesJSON serializes the canonical override map for the
// parameter_overrides_json column. nil/empty always becomes "{}" so the column
// is never blank. encoding/json sorts map keys, so the output is deterministic.
func marshalOverridesJSON(m map[string]any) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal parameter_overrides_json: %w", err)
	}
	return string(b), nil
}

// marshalUDFsJSON serializes the deployed-UDF DDL list for the
// deployed_udfs_json column. nil/empty always becomes "[]".
func marshalUDFsJSON(udfs []string) (string, error) {
	if len(udfs) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(udfs)
	if err != nil {
		return "", fmt.Errorf("marshal deployed_udfs_json: %w", err)
	}
	return string(b), nil
}

// decodeOverridesJSON parses the parameter_overrides_json column into the
// canonical override map. Empty/blank input means no overrides. Decoding uses
// UseNumber() so integer override values survive without float64 rounding, per
// the package's JSON-consumer contract.
func decodeOverridesJSON(s string) (map[string]any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode parameter_overrides_json: %w", err)
	}
	return m, nil
}

// decodeUDFsJSON parses the deployed_udfs_json column: a JSON array of UDF DDL
// statements. Empty/blank input means no UDFs.
func decodeUDFsJSON(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var udfs []string
	if err := json.Unmarshal([]byte(s), &udfs); err != nil {
		return nil, fmt.Errorf("decode deployed_udfs_json: %w", err)
	}
	return udfs, nil
}

// ── Row-value converters ─────────────────────────────────────────────────────────
// Equivalents of the handlers' parse* helpers (different package, can't be
// imported). They tolerate the shapes Proton's FORMAT JSON emits: uint64 as a
// quoted string, bool columns as JSON booleans, datetime64 as a string with no
// timezone.

// rowStr safely casts a row value to string.
func rowStr(v any) string {
	s, _ := v.(string)
	return s
}

// rowUint64 parses a uint64 column. Proton's FORMAT JSON renders uint64 as a
// quoted JSON string (e.g. "7"); json.Number and an integral non-negative
// float64 are also accepted for robustness against other decode paths. A
// missing or unparseable value is an error, not a silent 0 — this parses the
// revision ordering key, where a spurious 0 would silently corrupt monotonicity.
func rowUint64(v any) (uint64, error) {
	switch n := v.(type) {
	case string:
		u, err := strconv.ParseUint(strings.TrimSpace(n), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse uint64 from %q: %w", n, err)
		}
		return u, nil
	case json.Number:
		u, err := strconv.ParseUint(n.String(), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse uint64 from %q: %w", n.String(), err)
		}
		return u, nil
	case float64:
		if n < 0 {
			return 0, fmt.Errorf("parse uint64 from negative float %v", n)
		}
		if u := uint64(n); float64(u) == n {
			return u, nil
		}
		return 0, fmt.Errorf("parse uint64 from non-integral float %v", n)
	case nil:
		return 0, fmt.Errorf("missing uint64 value")
	default:
		return 0, fmt.Errorf("unexpected type %T for uint64 value", v)
	}
}

// rowBool parses a bool column. Proton's FORMAT JSON renders bool as a JSON
// boolean; the numeric/string fallbacks guard other decode paths.
func rowBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case json.Number:
		return b.String() != "" && b.String() != "0"
	case float64:
		return b != 0
	case string:
		return b == "true" || b == "1"
	}
	return false
}

// rowTime parses a datetime64 column. Proton's FORMAT JSON renders it without a
// timezone (e.g. "2026-06-15 12:00:00.123456789"); it is interpreted as UTC. A
// parse failure yields the zero time rather than an error: updated_at is
// non-load-bearing audit metadata (revision is the ordering key), so silent
// coercion is acceptable here, unlike for revision. (If a Proton session ever
// renders datetime64 in a non-UTC session timezone, the audit timestamp would
// be offset; the ordering invariant is unaffected.)
func rowTime(v any) time.Time {
	s, ok := v.(string)
	if !ok || s == "" {
		return time.Time{}
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05.999999999", s, time.UTC); err == nil {
		return t
	}
	return time.Time{}
}
