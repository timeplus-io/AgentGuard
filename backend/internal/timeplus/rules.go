package timeplus

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/timeplus/agentguard/internal/config"
)

// MVName returns the materialized view name for a rule ID.
// e.g. "rp-001" → "mv_rule_rp001"
func MVName(ruleID string) string {
	return "mv_rule_" + strings.ReplaceAll(ruleID, "-", "")
}

// CreateRuleMV creates a new materialized view for the rule.
// querySQLBody is the full SELECT ... FROM ... body.
// udfs is an optional list of CREATE OR REPLACE FUNCTION statements executed
// before the MV is created. They are idempotent and never dropped on uninstall.
func CreateRuleMV(cfg *config.TimeplusConfig, ruleID, querySQLBody string, udfs []string) error {
	mvName := MVName(ruleID)
	for i, udf := range udfs {
		if err := ExecHTTP(cfg, udf); err != nil {
			return fmt.Errorf("create UDF[%d] for rule %s: %w", i, ruleID, err)
		}
		log.Printf("rules: created/updated UDF[%d] for %s", i, ruleID)
	}
	ddl := fmt.Sprintf(
		"CREATE MATERIALIZED VIEW IF NOT EXISTS %s INTO agentguard_security_events AS %s",
		mvName, querySQLBody,
	)
	if err := ExecHTTP(cfg, ddl); err != nil {
		return fmt.Errorf("create MV %s: %w", mvName, err)
	}
	log.Printf("rules: created MV %s", mvName)
	return nil
}

// PauseRuleMV permanently pauses the MV (survives Proton restarts).
func PauseRuleMV(cfg *config.TimeplusConfig, ruleID string) error {
	mvName := MVName(ruleID)
	sql := fmt.Sprintf("SYSTEM PAUSE MATERIALIZED VIEW %s PERMANENT", mvName)
	if err := ExecHTTP(cfg, sql); err != nil {
		return fmt.Errorf("pause MV %s: %w", mvName, err)
	}
	log.Printf("rules: paused MV %s", mvName)
	return nil
}

// ResumeRuleMV permanently resumes the MV (survives Proton restarts).
func ResumeRuleMV(cfg *config.TimeplusConfig, ruleID string) error {
	mvName := MVName(ruleID)
	sql := fmt.Sprintf("SYSTEM RESUME MATERIALIZED VIEW %s PERMANENT", mvName)
	if err := ExecHTTP(cfg, sql); err != nil {
		return fmt.Errorf("resume MV %s: %w", mvName, err)
	}
	log.Printf("rules: resumed MV %s", mvName)
	return nil
}

// DropRuleMV drops the MV entirely (used on upgrade and uninstall).
func DropRuleMV(cfg *config.TimeplusConfig, ruleID string) error {
	mvName := MVName(ruleID)
	sql := fmt.Sprintf("DROP VIEW IF EXISTS %s", mvName)
	if err := ExecHTTP(cfg, sql); err != nil {
		return fmt.Errorf("drop MV %s: %w", mvName, err)
	}
	log.Printf("rules: dropped MV %s", mvName)
	return nil
}

// ── Candidate validation & rollback-capable replacement ───────────────────────
//
// These primitives back the lifecycle service's validate-first, replace-second,
// persist-last mutation ordering (see internal/rules/service.go). The service
// owns ordering and rollback; this file owns the individual Timeplus DDL steps.
//
// renderedSQL arguments are assumed to be already rendered and escaped by the
// rules compiler (internal/rules/render.go, the SQL-escaping boundary). This
// layer interpolates them verbatim and performs no further escaping.

// ruleExecutor is the minimal Timeplus surface the candidate-validation and
// replacement primitives need. The seam exists so their SQL construction and
// failure-cleanup ordering can be unit-tested with a fake, without a live Proton.
type ruleExecutor interface {
	Exec(sql string) error
	Query(sql string) ([]Row, error)
}

// protonExecutor binds the package-level ExecHTTP/Query to one config so it
// satisfies ruleExecutor.
type protonExecutor struct{ cfg *config.TimeplusConfig }

func (p protonExecutor) Exec(sql string) error           { return ExecHTTP(p.cfg, sql) }
func (p protonExecutor) Query(sql string) ([]Row, error) { return Query(p.cfg, sql) }

// candidateToken returns a short random hex token used to uniquely name the
// disposable resources of one preflight, so concurrent preflights (even across
// replicas) cannot collide on a sink/MV name. It surfaces a crypto/rand failure
// rather than degrading to a fixed token that could collide.
func candidateToken() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate preflight token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// candidateNames returns the disposable sink and MV names for a preflight,
// derived from the rule ID (dashes stripped, matching MVName) and a token.
func candidateNames(ruleID, token string) (sink, mv string) {
	base := strings.ReplaceAll(ruleID, "-", "")
	sink = "ag_preflight_sink_" + base + "_" + token
	mv = "ag_preflight_mv_" + base + "_" + token
	return sink, mv
}

// liveSinkDDL derives the disposable validation sink's DDL from the LIVE
// agentguard_security_events stream (SHOW CREATE), substituting the throwaway
// name.
//
// It must read the live stream rather than reuse this binary's compiled-in
// ddlSecurityEvents constant. The constant only describes the stream this
// release WOULD create; the deployed stream is created once with IF NOT EXISTS
// and never schema-checked, so an older release's stream survives an upgrade. If
// the two drift, a rule whose SELECT matches the constant passes preflight, and
// the replacement path then runs DROP VIEW ... NO DELAY followed by a CREATE that
// fails deterministically against the real stream — turning a healthy MV into no
// MV, and repeating on every /api/rules/sync (the startup/sync path has no
// rollback). Validating against the real target moves that failure to preflight,
// before anything is dropped. ddlSecurityEvents remains the creation template
// used by provisioning.
func liveSinkDDL(ex ruleExecutor, sink string) (string, error) {
	rows, err := ex.Query("SHOW CREATE STREAM agentguard_security_events")
	if err != nil {
		return "", fmt.Errorf("preflight read live agentguard_security_events DDL: %w", err)
	}
	var stmt string
	if len(rows) > 0 {
		stmt, _ = rows[0]["statement"].(string)
		if stmt == "" {
			// The result column name has varied across engine versions; accept any
			// string value that is a CREATE statement.
			for _, v := range rows[0] {
				if s, ok := v.(string); ok && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s)), "CREATE") {
					stmt = s
					break
				}
			}
		}
	}
	if stmt == "" {
		return "", fmt.Errorf("preflight: live agentguard_security_events DDL unavailable (stream missing? run provisioning via POST /api/setup/provision)")
	}
	return strings.Replace(stmt, "agentguard_security_events", sink, 1), nil
}

// candidateMVDDL builds the candidate MV that targets the disposable sink. No
// IF NOT EXISTS: a name collision must fail loudly rather than reuse a stale MV.
func candidateMVDDL(mv, sink, renderedSQL string) string {
	return fmt.Sprintf("CREATE MATERIALIZED VIEW %s INTO %s AS %s", mv, sink, renderedSQL)
}

// replaceMVDDL builds the real rule MV used by the replacement path. No
// IF NOT EXISTS: the caller drops any previous MV (NO DELAY) first, so this must
// create fresh and can never silently reuse a stale view.
func replaceMVDDL(ruleID, renderedSQL string) string {
	return fmt.Sprintf("CREATE MATERIALIZED VIEW %s INTO agentguard_security_events AS %s", MVName(ruleID), renderedSQL)
}

// dropViewNoDelaySQL / dropStreamNoDelaySQL build synchronous drops so a
// same-named resource can be safely recreated immediately afterwards.
func dropViewNoDelaySQL(name string) string   { return "DROP VIEW IF EXISTS " + name + " NO DELAY" }
func dropStreamNoDelaySQL(name string) string { return "DROP STREAM IF EXISTS " + name + " NO DELAY" }

// PreflightCandidate validates rendered rule SQL without touching live data: it
// creates a disposable sink with the security-events schema and a candidate MV
// that targets the sink, then always tears both down with NO DELAY. Success
// proves the SQL parses, its sources resolve, referenced UDFs are available, and
// its output columns are compatible with agentguard_security_events — while
// never writing candidate detections into the real stream.
//
// Any UDFs the SQL references must already be applied by the caller. Timeplus
// UDFs are global (CREATE OR REPLACE), so the service applies the candidate's
// UDFs before preflight and restores the previously deployed set afterward.
func PreflightCandidate(cfg *config.TimeplusConfig, ruleID, renderedSQL string) error {
	token, err := candidateToken()
	if err != nil {
		return err
	}
	return preflightCandidate(protonExecutor{cfg}, ruleID, renderedSQL, token)
}

func preflightCandidate(ex ruleExecutor, ruleID, renderedSQL, token string) (err error) {
	sink, mv := candidateNames(ruleID, token)
	// Read the live sink schema BEFORE installing the cleanup defer: nothing has
	// been created yet, so an unreadable schema must fail without any DDL.
	sinkDDL, err := liveSinkDDL(ex, sink)
	if err != nil {
		return err
	}
	// Always tear both down, NO DELAY, regardless of outcome. A cleanup failure
	// is logged and only surfaced when validation itself succeeded — it must not
	// mask the validation error that is the caller's real signal.
	defer func() {
		if dropErr := ex.Exec(dropViewNoDelaySQL(mv)); dropErr != nil {
			log.Printf("rules: preflight cleanup drop MV %s: %v", mv, dropErr)
			if err == nil {
				err = fmt.Errorf("preflight cleanup drop MV %s: %w", mv, dropErr)
			}
		}
		if dropErr := ex.Exec(dropStreamNoDelaySQL(sink)); dropErr != nil {
			log.Printf("rules: preflight cleanup drop sink %s: %v", sink, dropErr)
			if err == nil {
				err = fmt.Errorf("preflight cleanup drop sink %s: %w", sink, dropErr)
			}
		}
	}()

	if cErr := ex.Exec(sinkDDL); cErr != nil {
		return fmt.Errorf("preflight create sink %s: %w", sink, cErr)
	}
	if cErr := ex.Exec(candidateMVDDL(mv, sink, renderedSQL)); cErr != nil {
		return fmt.Errorf("preflight validate rule SQL: %w", cErr)
	}
	return nil
}

// ApplyUDFs executes a list of CREATE OR REPLACE FUNCTION statements in order.
// UDFs are global in Timeplus, so the caller owns their lifecycle: apply a
// candidate's UDFs before replacing its MV, and restore the previously deployed
// set on rollback.
func ApplyUDFs(cfg *config.TimeplusConfig, udfs []string) error {
	return applyUDFs(protonExecutor{cfg}, udfs)
}

func applyUDFs(ex ruleExecutor, udfs []string) error {
	for i, udf := range udfs {
		if err := ex.Exec(udf); err != nil {
			return fmt.Errorf("apply UDF[%d]: %w", i, err)
		}
	}
	return nil
}

// DropMVNoDelay drops a rule's MV synchronously so a same-named MV can be
// recreated immediately. The replacement path drops before it recreates.
func DropMVNoDelay(cfg *config.TimeplusConfig, ruleID string) error {
	mv := MVName(ruleID)
	if err := ExecHTTP(cfg, dropViewNoDelaySQL(mv)); err != nil {
		return fmt.Errorf("drop MV %s: %w", mv, err)
	}
	log.Printf("rules: dropped MV %s (no delay)", mv)
	return nil
}

// CreateRuleMVStrict creates a rule's MV WITHOUT IF NOT EXISTS, for the
// replacement path. The caller must have dropped any previous MV (NO DELAY)
// first so a stale MV can never be silently reused, and must have applied any
// required UDFs (this does not apply them).
func CreateRuleMVStrict(cfg *config.TimeplusConfig, ruleID, renderedSQL string) error {
	mv := MVName(ruleID)
	if err := ExecHTTP(cfg, replaceMVDDL(ruleID, renderedSQL)); err != nil {
		return fmt.Errorf("create MV %s: %w", mv, err)
	}
	log.Printf("rules: created MV %s (strict)", mv)
	return nil
}

// MVExists reports whether a rule's materialized view currently exists in the
// active database. The lifecycle service's sync path uses it to reconcile a
// snapshot's enabled state against the deployed MVs. The MV name is derived via
// MVName from a rule ID that catalog.Definition.Validate constrains to
// [A-Za-z0-9_-], so the interpolated literal carries no SQL-breaking characters.
func MVExists(cfg *config.TimeplusConfig, ruleID string) (bool, error) {
	return mvExists(protonExecutor{cfg}, ruleID)
}

func mvExists(ex ruleExecutor, ruleID string) (bool, error) {
	name := MVName(ruleID)
	rows, err := ex.Query(fmt.Sprintf(
		"SELECT count() AS cnt FROM system.tables WHERE name = '%s' AND database = current_database()", name))
	if err != nil {
		return false, fmt.Errorf("check MV %s exists: %w", name, err)
	}
	if len(rows) == 0 {
		return false, nil
	}
	// count() is uint64; Proton FORMAT JSON renders it as a quoted string, and
	// Query (UseNumber) may surface it as json.Number — accept both.
	switch v := rows[0]["cnt"].(type) {
	case string:
		return v != "" && v != "0", nil
	case json.Number:
		return v.String() != "" && v.String() != "0", nil
	default:
		return false, nil
	}
}

// FailedMV describes a materialized view that system.v_failed_mat_views reports
// as not having run in the recent introspection window.
type FailedMV struct {
	Name   string    // materialized view name (e.g. mv_rule_rp001)
	State  string    // last reported non-Executing status from introspection
	SeenAt time.Time // _tp_time of that introspection sample
}

// sqlFailedMVs reads the rule materialized views that system.v_failed_mat_views
// reports as not having run recently. That view exposes (database, name, state,
// _tp_time); we project name/state/_tp_time and scope to rule MVs via
// starts_with (the same function the view itself uses) so failures from other
// apps sharing the Proton instance are excluded and the LIKE underscore-wildcard
// pitfall is avoided.
const sqlFailedMVs = `SELECT name, state, _tp_time FROM system.v_failed_mat_views WHERE starts_with(name, 'mv_rule_')`

// FailedMVs returns the failed materialized views keyed by view name. It is a
// health signal for operator visibility and post-deploy verification, NOT the
// sole source of truth for MV existence: a permanently-paused MV (a disabled
// rule) can also surface here, so callers must cross-reference enabled state
// before treating an entry as a real failure.
func FailedMVs(cfg *config.TimeplusConfig) (map[string]FailedMV, error) {
	return failedMVs(protonExecutor{cfg})
}

func failedMVs(ex ruleExecutor) (map[string]FailedMV, error) {
	rows, err := ex.Query(sqlFailedMVs)
	if err != nil {
		return nil, fmt.Errorf("query failed MVs: %w", err)
	}
	return scanFailedMVs(rows), nil
}

func scanFailedMVs(rows []Row) map[string]FailedMV {
	out := make(map[string]FailedMV, len(rows))
	for _, r := range rows {
		name, _ := r["name"].(string)
		if name == "" {
			continue
		}
		fm := FailedMV{Name: name}
		fm.State, _ = r["state"].(string)
		if s, ok := r["_tp_time"].(string); ok && s != "" {
			if t, perr := time.ParseInLocation("2006-01-02 15:04:05.999999999", s, time.UTC); perr == nil {
				fm.SeenAt = t
			}
		}
		out[name] = fm
	}
	return out
}
