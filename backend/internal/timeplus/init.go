package timeplus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	proton "github.com/timeplus-io/proton-go-driver/v2"
	"github.com/timeplus/agentguard/internal/config"
)

// connectOnce makes a single connection attempt to Timeplus. It does not retry.
// The caller is responsible for retrying or reporting the error.
func connectOnce(cfg *config.TimeplusConfig) (proton.Conn, error) {
	opts := &proton.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.NativePort)},
		Auth: proton.Auth{
			Username: cfg.User,
			Password: cfg.Password,
		},
	}
	conn, err := proton.Open(opts)
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(context.Background()); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// bt replaces « » placeholders with SQL backticks so raw-string DDL literals
// can quote identifiers that contain dots (e.g. `events.timestamp`,
// `exemplars.value`). Go raw strings can't contain backticks directly.
var btReplacer = strings.NewReplacer("«", "`", "»", "`")

func bt(s string) string { return btReplacer.Replace(s) }

// Init ensures all Timeplus streams, materialized views, and seed data are in
// place. It is called from the setup wizard (not at server startup), so it
// receives explicit credentials rather than reading them from the config file.
// emit is a progress-reporting callback; if nil, progress is logged instead.
//
// Provisioning creates an EMPTY agentguard_rules stream: catalog definitions
// do not become rule instances until they are explicitly installed. If an
// agentguard_rules stream from an earlier release already exists with an
// incompatible schema, Init fails and asks the operator to reprovision.
func Init(timeplusCfg config.TimeplusConfig, agentGuardURL string, dlpCfg config.DLPConfig, emit func(status, msg string)) error {
	if emit == nil {
		emit = func(status, msg string) { log.Printf("[provision:%s] %s", status, msg) }
	}

	emit("running", "$ Connecting to Timeplus at "+timeplusCfg.Host+"…")
	conn, err := connectOnce(&timeplusCfg)
	if err != nil {
		emit("error", "  ✗ "+err.Error())
		return fmt.Errorf("timeplus init: connect: %w", err)
	}
	emit("ok", "  ✓ Connected")
	defer conn.Close()

	ctx := context.Background()

	emit("running", "$ Creating OTel streams…")
	if err := createOTelStreams(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating otel streams: %w", err)
	}
	emit("running", "$ Creating OTel input…")
	if err := createOTelInput(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating otel input: %w", err)
	}
	emit("running", "$ Creating AgentGuard streams…")
	if err := createAgentGuardStreams(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating agentguard streams: %w", err)
	}
	emit("running", "$ Creating CIM stream…")
	if err := createCIMStream(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating CIM stream: %w", err)
	}
	emit("running", "$ Creating CIM materialized views…")
	if err := createCIMMVs(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating CIM MVs: %w", err)
	}
	emit("running", "$ Creating CIM metrics streams…")
	if err := createCIMMetricsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating CIM metrics stream: %v", err)
	}
	emit("running", "$ Creating CIM metrics materialized views…")
	if err := createCIMMetricsMVs(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating CIM metrics MVs: %v", err)
	}
	emit("running", "$ Creating security streams…")
	if err := createSecurityStreams(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating security streams: %w", err)
	}
	if err := createThreatsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating threats stream: %v", err)
	}
	if err := createThreatsMV(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating threats MV: %v", err)
	}
	if err := createPricingStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating pricing stream: %v", err)
	}
	if err := seedPricingData(ctx, conn); err != nil {
		log.Printf("timeplus init: warning seeding pricing: %v", err)
	}
	if err := createCostBudgetsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating cost budgets stream: %v", err)
	}
	if err := createSkillsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating skills stream: %v", err)
	}
	if err := createSkillsMVs(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating skills MVs: %v", err)
	}
	if err := createMemoryOpsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating memory ops stream: %v", err)
	}
	if err := createMemoryMVs(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating memory MVs: %v", err)
	}
	emit("running", "$ Creating notification UDF and alert…")
	if err := createNotifyUDF(ctx, conn, agentGuardURL); err != nil {
		log.Printf("timeplus init: warning creating UDF: %v", err)
	}
	if err := createNotifyFilterMV(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating notify filter MV: %v", err)
	}
	if err := createNotifierAlert(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating alert: %v", err)
	}
	emit("running", "$ Syncing rule materialized views…")
	if err := startupSyncMVs(ctx, conn, timeplusCfg); err != nil {
		log.Printf("timeplus init: warning syncing MVs: %v", err)
	}

	// Setup holds storage (stream + MV).
	if err := SetupHoldsStorage(ctx, conn, emit); err != nil {
		log.Printf("timeplus: holds storage setup failed: %v (continuing — /api/holds will return 503)", err)
	}

	emit("running", "$ Provisioning Semantic DLP…")
	if err := InitDLP(ctx, conn, dlpCfg, emit); err != nil {
		log.Printf("timeplus init: warning provisioning DLP: %v (continuing)", err)
		emit("ok", "  - Semantic DLP provisioning failed (non-fatal): "+err.Error())
	}

	emit("ok", "  ✓ All resources provisioned")
	return nil
}

func streamExists(ctx context.Context, conn proton.Conn, name string) (bool, error) {
	q := fmt.Sprintf("SELECT count() FROM system.tables WHERE name = '%s' AND database = current_database()", name)
	row := conn.QueryRow(ctx, q)
	var count uint64
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func inputExists(ctx context.Context, conn proton.Conn, name string) (bool, error) {
	q := fmt.Sprintf("SELECT count() FROM system.inputs WHERE name = '%s'", name)
	row := conn.QueryRow(ctx, q)
	var count uint64
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func execIfNotExists(ctx context.Context, conn proton.Conn, kind, name, ddl string, emit func(string, string)) error {
	var exists bool
	var err error
	switch kind {
	case "stream", "view":
		exists, err = streamExists(ctx, conn, name)
	default:
		exists, err = inputExists(ctx, conn, name)
	}
	if err != nil {
		return fmt.Errorf("checking %s %q: %w", kind, name, err)
	}
	if exists {
		emit("ok", "  ✓ "+name+" (already exists)")
		return nil
	}
	if err := conn.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("creating %s %q: %w", kind, name, err)
	}
	emit("ok", "  ✓ "+name)
	return nil
}

func createOTelStreams(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	streams := []struct {
		name string
		ddl  string
	}{
		{"otel_traces", ddlOTelTraces},
		{"otel_logs", ddlOTelLogs},
		{"otel_metrics_gauge", ddlOTelMetricsGauge},
		{"otel_metrics_sum", ddlOTelMetricsSum},
		{"otel_metrics_histogram", ddlOTelMetricsHistogram},
		{"otel_metrics_exponential_histogram", ddlOTelMetricsExponentialHistogram},
		{"otel_metrics_summary", ddlOTelMetricsSummary},
	}
	for _, s := range streams {
		if err := execIfNotExists(ctx, conn, "stream", s.name, s.ddl, emit); err != nil {
			return err
		}
	}
	return nil
}

func createOTelInput(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "input", "agentguard_otel_input", ddlOTelInput, emit)
}

func createAgentGuardStreams(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_hook_events", ddlAgentGuardHookEvents, emit)
}

func createCIMStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_cim_event", ddlCIMEvent, emit)
}

func createCIMMVs(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	mvs := []struct {
		name string
		ddl  string
	}{
		{"mv_cim_claudecode", ddlCIMClaudeCodeMV},
		{"mv_cim_openclaw", ddlCIMOpenClawMV},
		{"mv_cim_hermes", ddlCIMHermesMV},
		{"mv_cim_openinference", ddlCIMOpenInferenceMV},
		{"mv_hook_bridge_openinference", ddlHookBridgeOpenInferenceMV},
	}
	for _, mv := range mvs {
		if err := execIfNotExists(ctx, conn, "view", mv.name, mv.ddl, emit); err != nil {
			return err
		}
	}
	return nil
}

func createCIMMetricsStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_session_agents", ddlSessionAgentsStream, emit); err != nil {
		return err
	}
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_run_latency", ddlRunLatencyStream, emit); err != nil {
		return err
	}
	return execIfNotExists(ctx, conn, "stream", "agentguard_cim_metrics", ddlCIMMetrics, emit)
}

func createCIMMetricsMVs(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	mvs := []struct {
		name string
		ddl  string
	}{
		// Lookup feeders must come first — the metrics MVs JOIN against these
		// mutable streams, so the streams must be populated before the joins run.
		{"mv_session_agents", ddlSessionAgentsMV},
		{"mv_run_latency", ddlRunLatencyMV},
		{"mv_cim_metrics_openclaw_llm", ddlCIMMetricsOpenClawLlmMV},
		{"mv_cim_metrics_openclaw_tools", ddlCIMMetricsOpenClawToolsMV},
		{"mv_cim_metrics_claudecode_otel", ddlCIMMetricsClaudeCodeOtelMV},
		{"mv_cim_metrics_claudecode_tools", ddlCIMMetricsClaudeCodeToolsMV},
		{"mv_cim_metrics_hermes_llm", ddlCIMMetricsHermesLlmMV},
		{"mv_cim_metrics_hermes_tools", ddlCIMMetricsHermesToolsMV},
		{"mv_cim_metrics_openinference_llm", ddlCIMMetricsOpenInferenceLlmMV},
	}
	for _, mv := range mvs {
		if err := execIfNotExists(ctx, conn, "view", mv.name, mv.ddl, emit); err != nil {
			return err
		}
	}
	return nil
}

func createSecurityStreams(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_security_events", ddlSecurityEvents, emit); err != nil {
		return err
	}
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_notify_events", ddlNotifyEventsStream, emit); err != nil {
		return err
	}
	return ensureRulesStream(ctx, conn, emit)
}

// ── Rules stream schema ───────────────────────────────────────────────────────

// expectedRulesSchema is the complete column set of the snapshot-model
// agentguard_rules stream, mapping column name → Proton type. It must stay in
// sync with ddlRulesStream (guarded by a unit test).
var expectedRulesSchema = map[string]string{
	"rule_id":                  "low_cardinality(string)",
	"snapshot_id":              "string",
	"revision":                 "uint64",
	"name":                     "string",
	"definition_version":       "string",
	"definition_hash":          "string",
	"description":              "string",
	"rendered_sql":             "string",
	"deployed_udfs_json":       "string",
	"parameter_overrides_json": "string",
	"enabled":                  "bool",
	"deleted":                  "bool",
	"severity":                 "low_cardinality(string)",
	"category":                 "low_cardinality(string)",
	"required_telemetry":       "string",
	"fp_rate":                  "low_cardinality(string)",
	"hook_sources":             "string",
	"block_policy":             "low_cardinality(string)",
	"update_reason":            "low_cardinality(string)",
	"updated_by":               "string",
	"updated_at":               "datetime64(9)",
}

// rulesKeyColumnTypes are the columns whose exact type is load-bearing for the
// snapshot model (ordering, SQL replay, JSON payloads). Presence alone is not
// enough for these — the type must match exactly.
var rulesKeyColumnTypes = map[string]string{
	"snapshot_id":              "string",
	"revision":                 "uint64",
	"rendered_sql":             "string",
	"deployed_udfs_json":       "string",
	"parameter_overrides_json": "string",
	"updated_at":               "datetime64(9)",
}

// validateRulesStreamSchema compares an existing agentguard_rules column set
// (name → type, as reported by system.columns) against the complete expected
// schema. Internal _tp_* columns are ignored. Any missing column, unexpected
// extra column, or key-column type mismatch makes the stream incompatible: the
// returned error tells the operator to drop the rule resources and reprovision.
// It is a pure function so it can be unit-tested without a live Proton.
func validateRulesStreamSchema(cols map[string]string) error {
	var problems []string
	actual := make(map[string]string, len(cols))
	for name, typ := range cols {
		if strings.HasPrefix(name, "_tp_") {
			continue // Proton-internal bookkeeping columns
		}
		actual[name] = typ
	}
	for name := range expectedRulesSchema {
		if _, ok := actual[name]; !ok {
			problems = append(problems, fmt.Sprintf("missing column %q", name))
		}
	}
	for name := range actual {
		if _, ok := expectedRulesSchema[name]; !ok {
			problems = append(problems, fmt.Sprintf("unexpected column %q", name))
		}
	}
	for name, want := range rulesKeyColumnTypes {
		if got, ok := actual[name]; ok && normalizeColumnType(got) != normalizeColumnType(want) {
			problems = append(problems, fmt.Sprintf("column %q has type %s, want %s", name, got, want))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf(
		"agentguard_rules has an incompatible schema (%s); this AgentGuard release requires a clean reprovision of rule resources: drop the agentguard_rules stream and all mv_rule_* materialized views, then re-run setup",
		strings.Join(problems, "; "),
	)
}

// normalizeColumnType canonicalizes a Proton column type string before
// comparison. system.columns may report a datetime type with a session
// timezone argument (e.g. "datetime64(9, 'UTC')") even when the column was
// declared without one, and may vary in casing/whitespace. The timezone is not
// load-bearing for the snapshot model, so it is stripped; precision and kind
// are preserved, so datetime64(9) vs datetime64(3) and uint64 vs uint32 still
// compare as different. This keeps validateRulesStreamSchema from rejecting a
// stream this code just created merely because of a cosmetic type-string
// difference, while still catching genuine incompatibilities.
func normalizeColumnType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	if strings.HasPrefix(t, "datetime") {
		if open := strings.Index(t, "("); open != -1 && strings.HasSuffix(t, ")") {
			inner := t[open+1 : len(t)-1]
			if comma := strings.Index(inner, ","); comma != -1 {
				inner = inner[:comma] // drop the timezone argument
			}
			inner = strings.TrimSpace(inner)
			if inner == "" || strings.HasPrefix(inner, "'") {
				return t[:open] // no precision, only a timezone: bare datetime[64]
			}
			return t[:open+1] + inner + ")"
		}
	}
	return t
}

// ensureRulesStream creates the agentguard_rules stream if it does not exist.
// If it already exists, its schema must match expectedRulesSchema exactly —
// there is no migration path from the pre-snapshot schema, so a mismatch fails
// provisioning instead of continuing partially.
func ensureRulesStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	exists, err := streamExists(ctx, conn, "agentguard_rules")
	if err != nil {
		return fmt.Errorf("checking stream %q: %w", "agentguard_rules", err)
	}
	if !exists {
		if err := conn.Exec(ctx, ddlRulesStream); err != nil {
			return fmt.Errorf("creating stream %q: %w", "agentguard_rules", err)
		}
		emit("ok", "  ✓ agentguard_rules")
		return nil
	}
	cols, err := fetchStreamColumns(ctx, conn, "agentguard_rules")
	if err != nil {
		return fmt.Errorf("reading agentguard_rules schema: %w", err)
	}
	if err := validateRulesStreamSchema(cols); err != nil {
		emit("error", "  ✗ "+err.Error())
		return err
	}
	emit("ok", "  ✓ agentguard_rules (already exists, schema compatible)")
	return nil
}

// fetchStreamColumns returns the column name → type map for a stream in the
// current database, as reported by system.columns.
func fetchStreamColumns(ctx context.Context, conn proton.Conn, stream string) (map[string]string, error) {
	q := fmt.Sprintf(
		"SELECT name, type FROM system.columns WHERE database = current_database() AND table = '%s'",
		stream,
	)
	rows, err := conn.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := make(map[string]string)
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			return nil, err
		}
		cols[name] = typ
	}
	return cols, rows.Err()
}

func createNotifyFilterMV(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "view", "mv_notify_filter", ddlNotifyFilterMV, emit)
}

func createNotifyUDF(ctx context.Context, conn proton.Conn, agentGuardURL string) error {
	ddl := fmt.Sprintf(ddlNotifyUDFTemplate, agentGuardURL+"/api/alerts/webhook")
	if err := conn.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("create UDF: %w", err)
	}
	log.Printf("timeplus: UDF notify_agentguard_udf created/updated (webhook: %s)", agentGuardURL)
	return nil
}

func createNotifierAlert(ctx context.Context, conn proton.Conn) error {
	err := conn.Exec(ctx, ddlNotifierAlert)
	if err == nil {
		log.Printf("timeplus: alert agentguard_notifier created")
		return nil
	}
	if strings.Contains(err.Error(), "already exists") {
		log.Printf("timeplus: alert agentguard_notifier already exists, skipping")
		return nil
	}
	return fmt.Errorf("create alert: %w", err)
}

// sqlStartupRules reconstructs the current rule instances from the snapshot
// log. Ordering is by revision — a monotonically increasing counter per rule —
// not by wall-clock updated_at, so replays and clock skew cannot reorder
// snapshots.
const sqlStartupRules = `
SELECT
  rule_id,
  arg_max(rendered_sql,       revision) AS rendered_sql,
  arg_max(deployed_udfs_json, revision) AS deployed_udfs_json,
  arg_max(enabled,            revision) AS enabled
FROM table(agentguard_rules)
GROUP BY rule_id
HAVING arg_max(deleted, revision) = false`

// decodeDeployedUDFs parses the deployed_udfs_json snapshot column: a JSON
// array of UDF DDL statements. Empty or blank input means no UDFs.
func decodeDeployedUDFs(s string) ([]string, error) {
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

// startupSyncMVs reconciles mv_rule_* materialized views with the current rule
// instances in agentguard_rules. It reproduces exactly what was deployed —
// rendered_sql and deployed_udfs_json from the latest snapshot — and never
// consults catalog artifacts, which may have changed since deployment.
func startupSyncMVs(ctx context.Context, conn proton.Conn, timeplusCfg config.TimeplusConfig) error {
	rows, err := conn.Query(ctx, sqlStartupRules)
	if err != nil {
		return fmt.Errorf("startup sync query: %w", err)
	}
	defer rows.Close()

	type ruleRow struct {
		ruleID      string
		renderedSQL string
		udfsJSON    string
		enabled     bool
	}
	var rules []ruleRow
	for rows.Next() {
		var r ruleRow
		if err := rows.Scan(&r.ruleID, &r.renderedSQL, &r.udfsJSON, &r.enabled); err != nil {
			log.Printf("timeplus: startup sync scan error: %v", err)
			continue
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("startup sync rows: %w", err)
	}

	for _, r := range rules {
		mvName := MVName(r.ruleID)
		// Drop/create, not pause/resume + IF NOT EXISTS: the lifecycle service
		// (internal/rules) uses drop/create so it never trusts an existing MV,
		// and startup must converge the same way. An MV that already exists but
		// runs drifted SQL (e.g. left by a CriticalError) would survive an
		// IF-NOT-EXISTS create or a RESUME; recreating from the snapshot's
		// rendered_sql guarantees the live MV matches what was persisted.
		if r.enabled {
			udfs, err := decodeDeployedUDFs(r.udfsJSON)
			if err != nil {
				log.Printf("timeplus: startup sync %s: %v — skipping MV creation", r.ruleID, err)
				continue
			}
			// Apply ALL the snapshot's UDFs or do not touch the MV (fail-fast,
			// like the lifecycle service's deployMV, via the shared ApplyUDFs).
			// UDFs are global: a partial apply could leave the recreated MV bound
			// to a stale function body, or mutate global UDF state while leaving
			// the old MV in place. On error, skip — the existing MV keeps running
			// on its current (unchanged) UDFs.
			if err := ApplyUDFs(&timeplusCfg, udfs); err != nil {
				log.Printf("timeplus: startup apply UDFs for %s failed, leaving existing MV intact: %v", r.ruleID, err)
				continue
			}
			// Validate-first: prove the snapshot SQL builds against a disposable
			// sink BEFORE the destructive drop, so a failed recreate can never
			// turn a healthy MV into no MV. On failure, leave the existing MV
			// untouched and move on.
			if err := PreflightCandidate(&timeplusCfg, r.ruleID, r.renderedSQL); err != nil {
				log.Printf("timeplus: startup preflight %s failed, leaving existing MV intact: %v", r.ruleID, err)
				continue
			}
			if err := conn.Exec(ctx, fmt.Sprintf("DROP VIEW IF EXISTS %s NO DELAY", mvName)); err != nil {
				log.Printf("timeplus: startup drop stale MV %s: %v", mvName, err)
			}
			ddl := fmt.Sprintf("CREATE MATERIALIZED VIEW %s INTO agentguard_security_events AS %s", mvName, r.renderedSQL)
			if err := conn.Exec(ctx, ddl); err != nil {
				log.Printf("timeplus: startup create MV %s: %v", mvName, err)
			} else {
				log.Printf("timeplus: startup created MV %s", mvName)
			}
		} else {
			// Disabled rules deploy no MV; drop any leftover unconditionally.
			if err := conn.Exec(ctx, fmt.Sprintf("DROP VIEW IF EXISTS %s NO DELAY", mvName)); err != nil {
				log.Printf("timeplus: startup drop disabled MV %s: %v", mvName, err)
			}
		}
	}
	log.Printf("timeplus: startup sync complete for %d rules", len(rules))
	return nil
}

// ── DDL statements ────────────────────────────────────────────────────────────

var ddlOTelTraces = bt(`
CREATE STREAM otel_traces
(
  trace_id             string                                      CODEC(ZSTD(1)),
  span_id              string                                      CODEC(ZSTD(1)),
  parent_span_id       string                                      CODEC(ZSTD(1)),
  trace_state          string                                      CODEC(ZSTD(1)),
  span_name            low_cardinality(string)                     CODEC(ZSTD(1)),
  span_kind            low_cardinality(string)                     CODEC(ZSTD(1)),
  service_name         low_cardinality(string)                     CODEC(ZSTD(1)),
  resource_attributes  map(low_cardinality(string), string)        CODEC(ZSTD(1)),
  scope_name           string                                      CODEC(ZSTD(1)),
  scope_version        string                                      CODEC(ZSTD(1)),
  span_attributes      map(low_cardinality(string), string)        CODEC(ZSTD(1)),
  duration             uint64                                      CODEC(ZSTD(1)),
  start_time_ns        uint64                                      CODEC(ZSTD(1)),
  end_time_ns          uint64                                      CODEC(ZSTD(1)),
  status_code          low_cardinality(string)                     CODEC(ZSTD(1)),
  status_message       string                                      CODEC(ZSTD(1)),
  «events.timestamp»   array(datetime64(9))                        CODEC(ZSTD(1)),
  «events.name»        array(low_cardinality(string))              CODEC(ZSTD(1)),
  «events.attributes»  array(map(low_cardinality(string), string)) CODEC(ZSTD(1)),
  «links.trace_id»     array(string)                               CODEC(ZSTD(1)),
  «links.span_id»      array(string)                               CODEC(ZSTD(1)),
  «links.trace_state»  array(string)                               CODEC(ZSTD(1)),
  «links.attributes»   array(map(low_cardinality(string), string)) CODEC(ZSTD(1)),
  INDEX idx_trace_id       trace_id                        TYPE bloom_filter(0.001) GRANULARITY 1,
  INDEX idx_res_attr_key   map_keys(resource_attributes)   TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_res_attr_val   map_values(resource_attributes) TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_span_attr_key  map_keys(span_attributes)       TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_span_attr_val  map_values(span_attributes)     TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_duration       duration                        TYPE minmax              GRANULARITY 1
)`)

const ddlOTelLogs = `
CREATE STREAM otel_logs
(
  trace_id            string                               CODEC(ZSTD(1)),
  span_id             string                               CODEC(ZSTD(1)),
  trace_flags         uint32                               CODEC(ZSTD(1)),
  severity_text       low_cardinality(string)              CODEC(ZSTD(1)),
  severity_number     int32                                CODEC(ZSTD(1)),
  service_name        low_cardinality(string)              CODEC(ZSTD(1)),
  body                string                               CODEC(ZSTD(1)),
  resource_schema_url string                               CODEC(ZSTD(1)),
  resource_attributes map(low_cardinality(string), string) CODEC(ZSTD(1)),
  scope_schema_url    string                               CODEC(ZSTD(1)),
  scope_name          string                               CODEC(ZSTD(1)),
  scope_version       string                               CODEC(ZSTD(1)),
  scope_attributes    map(low_cardinality(string), string) CODEC(ZSTD(1)),
  log_attributes      map(low_cardinality(string), string) CODEC(ZSTD(1)),
  INDEX idx_trace_id      trace_id                      TYPE bloom_filter(0.001) GRANULARITY 1,
  INDEX idx_res_attr_key  map_keys(resource_attributes) TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_res_attr_val  map_values(resource_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX idx_log_attr_key  map_keys(log_attributes)      TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_log_attr_val  map_values(log_attributes)    TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_body          body                          TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
)`

var ddlOTelMetricsGauge = bt(`
CREATE STREAM otel_metrics_gauge
(
  resource_attributes             map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  resource_schema_url             string                                       CODEC(ZSTD(1)),
  scope_name                      string                                       CODEC(ZSTD(1)),
  scope_version                   string                                       CODEC(ZSTD(1)),
  scope_attributes                map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  scope_dropped_attr_count        uint32                                       CODEC(ZSTD(1)),
  scope_schema_url                string                                       CODEC(ZSTD(1)),
  service_name                    low_cardinality(string)                      CODEC(ZSTD(1)),
  metric_name                     string                                       CODEC(ZSTD(1)),
  metric_description              string                                       CODEC(ZSTD(1)),
  metric_unit                     string                                       CODEC(ZSTD(1)),
  attributes                      map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  start_time_unix                 datetime64(9)                                CODEC(Delta(8), ZSTD(1)),
  time_unix                       datetime64(9)                                CODEC(Delta(8), ZSTD(1)),
  value                           float64                                      CODEC(ZSTD(1)),
  flags                           uint32                                       CODEC(ZSTD(1)),
  «exemplars.filtered_attributes» array(map(low_cardinality(string), string))  CODEC(ZSTD(1)),
  «exemplars.time_unix»           array(datetime64(9))                         CODEC(ZSTD(1)),
  «exemplars.value»               array(float64)                               CODEC(ZSTD(1)),
  «exemplars.span_id»             array(string)                                CODEC(ZSTD(1)),
  «exemplars.trace_id»            array(string)                                CODEC(ZSTD(1))
)`)

var ddlOTelMetricsSum = bt(`
CREATE STREAM otel_metrics_sum
(
  resource_attributes             map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  resource_schema_url             string                                       CODEC(ZSTD(1)),
  scope_name                      string                                       CODEC(ZSTD(1)),
  scope_version                   string                                       CODEC(ZSTD(1)),
  scope_attributes                map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  scope_dropped_attr_count        uint32                                       CODEC(ZSTD(1)),
  scope_schema_url                string                                       CODEC(ZSTD(1)),
  service_name                    low_cardinality(string)                      CODEC(ZSTD(1)),
  metric_name                     string                                       CODEC(ZSTD(1)),
  metric_description              string                                       CODEC(ZSTD(1)),
  metric_unit                     string                                       CODEC(ZSTD(1)),
  attributes                      map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  start_time_unix                 datetime64(9)                                CODEC(Delta(8), ZSTD(1)),
  time_unix                       datetime64(9)                                CODEC(Delta(8), ZSTD(1)),
  value                           float64                                      CODEC(ZSTD(1)),
  flags                           uint32                                       CODEC(ZSTD(1)),
  «exemplars.filtered_attributes» array(map(low_cardinality(string), string))  CODEC(ZSTD(1)),
  «exemplars.time_unix»           array(datetime64(9))                         CODEC(ZSTD(1)),
  «exemplars.value»               array(float64)                               CODEC(ZSTD(1)),
  «exemplars.span_id»             array(string)                                CODEC(ZSTD(1)),
  «exemplars.trace_id»            array(string)                                CODEC(ZSTD(1)),
  aggregation_temporality         int32                                        CODEC(ZSTD(1)),
  is_monotonic                    bool                                         CODEC(Delta(1), ZSTD(1))
)`)

var ddlOTelMetricsHistogram = bt(`
CREATE STREAM otel_metrics_histogram
(
  resource_attributes             map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  resource_schema_url             string                                       CODEC(ZSTD(1)),
  scope_name                      string                                       CODEC(ZSTD(1)),
  scope_version                   string                                       CODEC(ZSTD(1)),
  scope_attributes                map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  scope_dropped_attr_count        uint32                                       CODEC(ZSTD(1)),
  scope_schema_url                string                                       CODEC(ZSTD(1)),
  service_name                    low_cardinality(string)                      CODEC(ZSTD(1)),
  metric_name                     string                                       CODEC(ZSTD(1)),
  metric_description              string                                       CODEC(ZSTD(1)),
  metric_unit                     string                                       CODEC(ZSTD(1)),
  attributes                      map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  start_time_unix                 datetime64(9)                                CODEC(Delta(8), ZSTD(1)),
  time_unix                       datetime64(9)                                CODEC(Delta(8), ZSTD(1)),
  count                           uint64                                       CODEC(Delta(8), ZSTD(1)),
  sum                             float64                                      CODEC(ZSTD(1)),
  bucket_counts                   array(uint64)                                CODEC(ZSTD(1)),
  explicit_bounds                 array(float64)                               CODEC(ZSTD(1)),
  min                             float64                                      CODEC(ZSTD(1)),
  max                             float64                                      CODEC(ZSTD(1)),
  flags                           uint32                                       CODEC(ZSTD(1)),
  «exemplars.filtered_attributes» array(map(low_cardinality(string), string))  CODEC(ZSTD(1)),
  «exemplars.time_unix»           array(datetime64(9))                         CODEC(ZSTD(1)),
  «exemplars.value»               array(float64)                               CODEC(ZSTD(1)),
  «exemplars.span_id»             array(string)                                CODEC(ZSTD(1)),
  «exemplars.trace_id»            array(string)                                CODEC(ZSTD(1)),
  aggregation_temporality         int32                                        CODEC(ZSTD(1))
)`)

var ddlOTelMetricsExponentialHistogram = bt(`
CREATE STREAM otel_metrics_exponential_histogram
(
  resource_attributes             map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  resource_schema_url             string                                       CODEC(ZSTD(1)),
  scope_name                      string                                       CODEC(ZSTD(1)),
  scope_version                   string                                       CODEC(ZSTD(1)),
  scope_attributes                map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  scope_dropped_attr_count        uint32                                       CODEC(ZSTD(1)),
  scope_schema_url                string                                       CODEC(ZSTD(1)),
  service_name                    low_cardinality(string)                      CODEC(ZSTD(1)),
  metric_name                     string                                       CODEC(ZSTD(1)),
  metric_description              string                                       CODEC(ZSTD(1)),
  metric_unit                     string                                       CODEC(ZSTD(1)),
  attributes                      map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  start_time_unix                 datetime64(9)                                CODEC(Delta(8), ZSTD(1)),
  time_unix                       datetime64(9)                                CODEC(Delta(8), ZSTD(1)),
  count                           uint64                                       CODEC(Delta(8), ZSTD(1)),
  sum                             float64                                      CODEC(ZSTD(1)),
  scale                           int32                                        CODEC(ZSTD(1)),
  zero_count                      uint64                                       CODEC(ZSTD(1)),
  positive_offset                 int32                                        CODEC(ZSTD(1)),
  positive_bucket_counts          array(uint64)                                CODEC(ZSTD(1)),
  negative_offset                 int32                                        CODEC(ZSTD(1)),
  negative_bucket_counts          array(uint64)                                CODEC(ZSTD(1)),
  min                             float64                                      CODEC(ZSTD(1)),
  max                             float64                                      CODEC(ZSTD(1)),
  flags                           uint32                                       CODEC(ZSTD(1)),
  «exemplars.filtered_attributes» array(map(low_cardinality(string), string))  CODEC(ZSTD(1)),
  «exemplars.time_unix»           array(datetime64(9))                         CODEC(ZSTD(1)),
  «exemplars.value»               array(float64)                               CODEC(ZSTD(1)),
  «exemplars.span_id»             array(string)                                CODEC(ZSTD(1)),
  «exemplars.trace_id»            array(string)                                CODEC(ZSTD(1)),
  aggregation_temporality         int32                                        CODEC(ZSTD(1))
)`)

var ddlOTelMetricsSummary = bt(`
CREATE STREAM otel_metrics_summary
(
  resource_attributes          map(low_cardinality(string), string)  CODEC(ZSTD(1)),
  resource_schema_url          string                                CODEC(ZSTD(1)),
  scope_name                   string                                CODEC(ZSTD(1)),
  scope_version                string                                CODEC(ZSTD(1)),
  scope_attributes             map(low_cardinality(string), string)  CODEC(ZSTD(1)),
  scope_dropped_attr_count     uint32                                CODEC(ZSTD(1)),
  scope_schema_url             string                                CODEC(ZSTD(1)),
  service_name                 low_cardinality(string)               CODEC(ZSTD(1)),
  metric_name                  string                                CODEC(ZSTD(1)),
  metric_description           string                                CODEC(ZSTD(1)),
  metric_unit                  string                                CODEC(ZSTD(1)),
  attributes                   map(low_cardinality(string), string)  CODEC(ZSTD(1)),
  start_time_unix              datetime64(9)                         CODEC(Delta(8), ZSTD(1)),
  time_unix                    datetime64(9)                         CODEC(Delta(8), ZSTD(1)),
  count                        uint64                                CODEC(Delta(8), ZSTD(1)),
  sum                          float64                               CODEC(ZSTD(1)),
  «value_at_quantiles.quantile» array(float64)                       CODEC(ZSTD(1)),
  «value_at_quantiles.value»    array(float64)                       CODEC(ZSTD(1)),
  flags                        uint32                                CODEC(ZSTD(1))
)`)

const ddlAgentGuardHookEvents = `
CREATE STREAM agentguard_hook_events
(
  hook_name        low_cardinality(string),
  event_time       datetime64(3, 'UTC'),
  session_id       string,
  run_id           string,
  agent_id         string,
  deployment_id    low_cardinality(string),
  deployment_name  low_cardinality(string),
  session_key      string,
  tool_name        string,
  provider         string,
  model            string,
  hook_decision    low_cardinality(string),
  block_reason     string,
  event_data       string,
  agent_type       low_cardinality(string)
)`

const ddlSecurityEvents = `
CREATE STREAM agentguard_security_events
(
  detected_at   datetime64(3),
  rule_id       low_cardinality(string),
  rule_name     low_cardinality(string),
  severity      low_cardinality(string),
  agent_id      string,
  deployment_id low_cardinality(string),
  session_id    string,
  run_id        string,
  hook_name     low_cardinality(string),
  tool_name     string,
  message       string,
  event_data    string
)`

// ddlNotifyEventsStream holds filtered security events that the notifier alert
// should act on (i.e. events where the matching threat is new or was cleared).
const ddlNotifyEventsStream = `
CREATE STREAM agentguard_notify_events
(
  detected_at   datetime64(3),
  rule_id       low_cardinality(string),
  rule_name     low_cardinality(string),
  severity      low_cardinality(string),
  agent_id      string,
  deployment_id low_cardinality(string),
  session_id    string,
  run_id        string,
  hook_name     low_cardinality(string),
  tool_name     string,
  message       string,
  event_data    string
)`

// ddlNotifyFilterMV feeds agentguard_notify_events by joining security events
// with the current threat state. Only events where the threat is new (no row in
// agentguard_threats) or was previously cleared pass through.
// Alerts do not support JOINs, so the join lives here instead.
const ddlNotifyFilterMV = `
CREATE MATERIALIZED VIEW mv_notify_filter
INTO agentguard_notify_events
AS SELECT
  se.detected_at, se.rule_id, se.rule_name, se.severity,
  se.agent_id, se.deployment_id, se.session_id, se.run_id,
  se.hook_name, se.tool_name, se.message, se.event_data
FROM agentguard_security_events AS se
LEFT JOIN agentguard_threats AS t
  ON  se.agent_id      = t.agent_id
  AND se.deployment_id = t.deployment_id
  AND se.session_id    = t.session_id
  AND se.rule_id       = t.rule_id
WHERE t.status = '' OR t.status = 'cleared'`

// ddlRulesStream is the snapshot-model agentguard_rules stream. Each row is one
// immutable snapshot of a rule; current state is reconstructed with
// arg_max(..., revision). It must stay in sync with expectedRulesSchema
// (guarded by TestRulesStreamDDLMatchesExpectedSchema). No column carries a
// DEFAULT: the snapshot store always writes every column explicitly.
const ddlRulesStream = `
CREATE STREAM agentguard_rules
(
  rule_id                  low_cardinality(string),
  snapshot_id              string,
  revision                 uint64,
  name                     string,
  definition_version       string,
  definition_hash          string,
  description              string,
  rendered_sql             string,
  deployed_udfs_json       string,
  parameter_overrides_json string,
  enabled                  bool,
  deleted                  bool,
  severity                 low_cardinality(string),
  category                 low_cardinality(string),
  required_telemetry       string,
  fp_rate                  low_cardinality(string),
  hook_sources             string,
  block_policy             low_cardinality(string),
  update_reason            low_cardinality(string),
  updated_by               string,
  updated_at               datetime64(9)
)`

const ddlCIMEvent = `
CREATE STREAM agentguard_cim_event
(
  event_time        datetime64(3),
  event_type        low_cardinality(string),
  agent_type        low_cardinality(string),
  agent_id          string,
  deployment_id     low_cardinality(string),
  session_id        string,
  run_id            string,
  provider          low_cardinality(string),
  model             low_cardinality(string),
  tool_name         string,
  tool_input        string,
  tool_result       string,
  tool_success      bool,
  tool_duration_ms  int64,
  user_message      string,
  assistant_message string,
  hook_decision     low_cardinality(string),
  block_reason      string,
  raw_hook_name     low_cardinality(string),
  raw_event_data    string
)`

const ddlCIMClaudeCodeMV = `
CREATE MATERIALIZED VIEW mv_cim_claudecode
INTO agentguard_cim_event AS
SELECT
  event_time,
  multi_if(
    hook_name = 'session_start',      'session_start',
    hook_name = 'subagent_spawning',  'session_start',
    hook_name = 'session_end',        'session_end',
    hook_name = 'user_prompt_submit', 'user_input',
    hook_name = 'llm_output',         'llm_response',
    hook_name = 'before_tool_call',   'tool_invoke',
    hook_name = 'after_tool_call',    'tool_complete',
    hook_name = 'prompt_context',     'prompt_context',
    hook_name = 'llm_context',        'llm_context',
    'other'
  )                                                                AS event_type,
  agent_type,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  if(provider != '', provider,
     multi_if(
       position(if(model != '', model, json_value(event_data, '$.model')), '@') > 0, 'vertex',
       match(if(model != '', model, json_value(event_data, '$.model')), '^(us\\.|eu\\.|ap\\.)?anthropic\\.'), 'bedrock',
       if(model != '', model, json_value(event_data, '$.model')) != '', 'anthropic',
       ''
     ))                                                            AS provider,
  if(model != '', model, json_value(event_data, '$.model'))       AS model,
  if(tool_name != '', tool_name,
     json_value(event_data, '$.tool_name'))                       AS tool_name,
  json_query(event_data, '$.tool_input')                          AS tool_input,
  json_query(event_data, '$.tool_response')                       AS tool_result,
  json_value(event_data, '$.tool_response.interrupted') != 'true' AS tool_success,
  0                                                               AS tool_duration_ms,
  json_value(event_data, '$.prompt')                              AS user_message,
  json_value(event_data, '$.last_assistant_message')              AS assistant_message,
  hook_decision,
  block_reason,
  hook_name                                                       AS raw_hook_name,
  event_data                                                      AS raw_event_data
FROM agentguard_hook_events
WHERE agent_type = 'claudecode'
  AND hook_name IN (
    'session_start', 'subagent_spawning', 'session_end',
    'user_prompt_submit',
    'before_tool_call', 'after_tool_call',
    'llm_output',
    'prompt_context', 'llm_context'
  )`

const ddlCIMOpenClawMV = `
CREATE MATERIALIZED VIEW mv_cim_openclaw
INTO agentguard_cim_event AS
SELECT
  event_time,
  multi_if(
    hook_name = 'session_start',    'session_start',
    hook_name = 'session_end',      'session_end',
    hook_name = 'message_received', 'user_input',
    hook_name = 'llm_input',        'llm_request',
    hook_name = 'llm_output',       'llm_response',
    hook_name = 'before_tool_call', 'tool_invoke',
    hook_name = 'after_tool_call',  'tool_complete',
    'other'
  )                                                                        AS event_type,
  agent_type,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  if(provider != '', provider,
     json_value(event_data, '$.provider'))                                 AS provider,
  if(model != '', model,
     json_value(event_data, '$.model'))                                    AS model,
  if(tool_name != '', tool_name,
     json_value(event_data, '$.toolName'))                                 AS tool_name,
  json_query(event_data, '$.params')                                       AS tool_input,
  json_query(event_data, '$.result')                                       AS tool_result,
  json_value(event_data, '$.error') = ''                                   AS tool_success,
  json_extract_int(event_data, 'durationMs')                               AS tool_duration_ms,
  if(hook_name = 'llm_input',
     json_value(event_data, '$.prompt'),
     json_value(event_data, '$.content'))                                  AS user_message,
  json_value(event_data, '$.assistantTexts[0]')                            AS assistant_message,
  hook_decision,
  block_reason,
  hook_name                                                                AS raw_hook_name,
  event_data                                                               AS raw_event_data
FROM agentguard_hook_events
WHERE agent_type = 'openclaw'
  AND hook_name IN (
    'session_start', 'session_end',
    'message_received',
    'llm_input', 'llm_output',
    'before_tool_call', 'after_tool_call'
  )`

const ddlCIMHermesMV = `
CREATE MATERIALIZED VIEW mv_cim_hermes
INTO agentguard_cim_event AS
SELECT
  event_time,
  multi_if(
    hook_name = 'on_session_start',    'session_start',
    hook_name = 'on_session_end',      'session_end',
    hook_name = 'on_session_finalize', 'session_end',
    hook_name = 'on_session_reset',    'session_start',
    hook_name = 'pre_llm_call',        'user_input',
    hook_name = 'post_llm_call',       'llm_response',
    hook_name = 'pre_api_request',     'llm_request',
    hook_name = 'pre_tool_call',       'tool_invoke',
    hook_name = 'post_tool_call',      'tool_complete',
    'other'
  )                                                                    AS event_type,
  agent_type,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  if(provider != '', provider,
     json_value(event_data, '$.provider'))                             AS provider,
  model,
  if(tool_name != '', tool_name,
     json_value(event_data, '$.tool_name'))                            AS tool_name,
  json_query(event_data, '$.args')                                     AS tool_input,
  json_query(event_data, '$.result')                                   AS tool_result,
  true                                                                 AS tool_success,
  0                                                                    AS tool_duration_ms,
  json_value(event_data, '$.user_message')                             AS user_message,
  json_value(event_data, '$.assistant_response')                       AS assistant_message,
  hook_decision,
  block_reason,
  hook_name                                                            AS raw_hook_name,
  event_data                                                           AS raw_event_data
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND agent_type = 'hermes'
  AND hook_name IN (
    'on_session_start', 'on_session_end', 'on_session_finalize', 'on_session_reset',
    'pre_llm_call', 'post_llm_call',
    'pre_api_request',
    'pre_tool_call', 'post_tool_call'
  )`

const ddlCIMOpenInferenceMV = `
CREATE MATERIALIZED VIEW mv_cim_openinference
INTO agentguard_cim_event AS
SELECT
  multi_if(
    idx = 1,
    to_datetime64(start_time_ns / 1000000000, 3, 'UTC'),
    to_datetime64(end_time_ns / 1000000000, 3, 'UTC')
  )                                                                 AS event_time,
  multi_if(
    idx = 1,
    multi_if(
      span_attributes['openinference.span.kind'] == 'LLM', 'llm_request',
      span_attributes['openinference.span.kind'] == 'TOOL', 'tool_invoke',
      span_attributes['agentguard.span.kind'] == 'SESSION', 'session_start',
      'other'
    ),
    multi_if(
	  span_attributes['openinference.span.kind'] == 'LLM', 'llm_response',
      span_attributes['openinference.span.kind'] == 'TOOL', 'tool_complete',
      span_attributes['agentguard.span.kind'] == 'SESSION', 'session_end',
      'other'
    )
  )                                                                 AS event_type,
  service_name                                                      AS agent_type,
  resource_attributes['agent.id']                                   AS agent_id,
  resource_attributes['deployment.id']                              AS deployment_id,
  span_attributes['session.id']                                     AS session_id,
  ''                                                                AS run_id,
  if (map_contains(span_attributes, 'llm.provider'),
    span_attributes['llm.provider'],
    split_by_string('/', span_attributes['llm.model_name'])[1])     AS provider,
  split_by_string('/', span_attributes['llm.model_name'])[-1]       AS model,
  span_attributes['tool.name']                                      AS tool_name,
  span_attributes['tool.parameters']                                AS tool_input,
  if (span_attributes['openinference.span.kind'] == 'TOOL',
    span_attributes['output.value'], '')                            AS tool_result,
  true                                                              AS tool_success,
  if(idx = 2, int_div(end_time_ns - start_time_ns, 1000000), 0)     AS tool_duration_ms,
  json_value(array_filter(
      m -> json_value(m, '$.role') = 'user',
      json_extract_array(span_attributes['input.value'], 'messages')
    )[-1], '$.content')                                             AS user_message,
  span_attributes['llm.output_messages.0.message.content']          AS assistant_message,
  ''                                                                AS hook_decision,
  ''                                                                AS block_reason,
  span_name                                                         AS raw_hook_name,
  to_json_string(span_attributes)                                   AS raw_event_data
FROM otel_traces
ARRAY JOIN array_cast(1, 2) AS idx
WHERE _tp_time > earliest_ts()
  AND event_type != 'other'
`

const ddlHookBridgeOpenInferenceMV = `
CREATE MATERIALIZED VIEW mv_hook_bridge_openinference
INTO agentguard_hook_events AS
SELECT
  multi_if(
    idx = 1,
    multi_if(
      span_attributes['openinference.span.kind'] = 'LLM', 'llm_input',
      span_attributes['openinference.span.kind'] = 'TOOL', 'before_tool_call',
      span_attributes['agentguard.span.kind'] == 'SESSION', 'session_start',
      'span_start'
    ),
    multi_if(
      span_attributes['openinference.span.kind'] = 'LLM', 'llm_output',
      span_attributes['openinference.span.kind'] = 'TOOL', 'after_tool_call',
      span_attributes['agentguard.span.kind'] == 'SESSION', 'session_end',
      'span_end'
    )
  )                                                                       AS hook_name,
  multi_if(
    idx = 1,
    to_datetime64(start_time_ns / 1000000000, 3, 'UTC'),
    to_datetime64(end_time_ns / 1000000000, 3, 'UTC')
  )                                                                       AS event_time,
  coalesce(span_attributes['session.id'], trace_id)                       AS session_id,
  coalesce(span_attributes['session.id'], trace_id)                       AS run_id,
  resource_attributes['agent.id']                                         AS agent_id,
  resource_attributes['deployment.id']                                    AS deployment_id,
  resource_attributes['deployment.name']                                  AS deployment_name,
  ''                                                                      AS session_key,
  span_attributes['tool.name']                                            AS tool_name,
  if (map_contains(span_attributes, 'llm.provider'),
    span_attributes['llm.provider'],
    split_by_string('/', span_attributes['llm.model_name'])[1])           AS provider,
  split_by_string('/', span_attributes['llm.model_name'])[-1]             AS model,
  'observe'                                                               AS hook_decision,
  ''                                                                      AS block_reason,
  to_json_string(span_attributes)                                         AS event_data,
  service_name                                                            AS agent_type
FROM otel_traces
ARRAY JOIN array_cast(1, 2) AS idx
WHERE _tp_time > earliest_ts()
`

// ddlCIMMetricsHermesLlmMV extracts LLM token and latency metrics from
// post_api_request events, which carry the usage dict with token counts and
// api_duration in seconds. Reads directly from agentguard_hook_events so that
// post_api_request does not need to appear in agentguard_cim_event.
const ddlCIMMetricsHermesLlmMV = `
CREATE MATERIALIZED VIEW mv_cim_metrics_hermes_llm
INTO agentguard_cim_metrics AS
SELECT
  event_time,
  metric_name,
  metric_value,
  'hermes'                                                              AS agent_type,
  agent_id,
  deployment_id,
  session_id,
  map_cast(
    ['model', 'provider', 'run_id'],
    [model, provider, run_id]
  )                                                                     AS tags
FROM agentguard_hook_events
ARRAY JOIN
  ['llm.call', 'llm.input_tokens', 'llm.output_tokens', 'llm.total_tokens',
   'llm.cache_read_tokens', 'llm.cache_write_tokens', 'llm.latency_ms']  AS metric_name,
  [
    to_float64(1),
    to_float64_or_zero(json_extract_string(event_data, 'usage', 'input_tokens')),
    to_float64_or_zero(json_extract_string(event_data, 'usage', 'output_tokens')),
    to_float64_or_zero(json_extract_string(event_data, 'usage', 'input_tokens'))
      + to_float64_or_zero(json_extract_string(event_data, 'usage', 'output_tokens')),
    to_float64_or_zero(json_extract_string(event_data, 'usage', 'cache_read_input_tokens')),
    to_float64_or_zero(json_extract_string(event_data, 'usage', 'cache_creation_input_tokens')),
    to_float64_or_zero(json_extract_string(event_data, 'api_duration')) * 1000
  ]                                                                     AS metric_value
WHERE _tp_time > earliest_ts()
  AND agent_type = 'hermes'
  AND hook_name = 'post_api_request'`

const ddlCIMMetricsHermesToolsMV = `
CREATE MATERIALIZED VIEW mv_cim_metrics_hermes_tools
INTO agentguard_cim_metrics AS
SELECT
  event_time,
  metric_name,
  metric_value,
  'hermes'                                                              AS agent_type,
  agent_id,
  deployment_id,
  session_id,
  map_cast(['tool_name', 'model', 'provider', 'run_id'], [tool_name, model, provider, run_id]) AS tags
FROM agentguard_cim_event
ARRAY JOIN
  ['tool.call', 'tool.success']                                        AS metric_name,
  [
    to_float64(1),
    if(tool_success, to_float64(1), to_float64(0))
  ]                                                                     AS metric_value
WHERE _tp_time > earliest_ts()
  AND agent_type = 'hermes'
  AND event_type = 'tool_complete'`

// ddlCIMMetricsOpenInferenceLlmMV extracts LLM token metrics from
// OpenInference OTel traces (openinference.span.kind = 'LLM').
const ddlCIMMetricsOpenInferenceLlmMV = `
CREATE MATERIALIZED VIEW mv_cim_metrics_openinference_llm
INTO agentguard_cim_metrics AS
SELECT
  to_datetime64(end_time_ns / 1000000000, 3, 'UTC')                          AS event_time,
  metric_name,
  metric_value,
  service_name                                                               AS agent_type,
  resource_attributes['agent.id']                                            AS agent_id,
  resource_attributes['deployment.id']                                       AS deployment_id,
  span_attributes['session.id']                                              AS session_id,
  map_cast(
    ['model', 'provider', 'trace_id'],
    [
      split_by_string('/', span_attributes['llm.model_name'])[-1],
      if (map_contains(span_attributes, 'llm.provider'),
        span_attributes['llm.provider'],
        split_by_string('/', span_attributes['llm.model_name'])[1]),
      trace_id
    ]
  )                                                                          AS tags
FROM otel_traces
ARRAY JOIN
  ['llm.call', 'llm.total_tokens', 'llm.input_tokens', 'llm.output_tokens', 'llm.cache_read_tokens', 'llm.latency_ms'] AS metric_name,
  [
    to_float64(1),
    to_float64_or_zero(span_attributes['llm.token_count.total']),
    to_float64_or_zero(span_attributes['llm.token_count.prompt']),
    to_float64_or_zero(span_attributes['llm.token_count.completion']),
    to_float64_or_zero(span_attributes['llm.token_count.prompt_details.cache_read']),
    (end_time_ns - start_time_ns) / 1000000
  ]                                                                          AS metric_value
WHERE _tp_time > earliest_ts()
  AND span_attributes['openinference.span.kind'] = 'LLM'
`

// ddlSessionAgentsStream is a mutable stream keyed by session_id that maps each
// Claude Code session to its agent_id and deployment_id (set by the plugin via
// environment variables). It is used as a live lookup in the OTel metrics MV so
// that LLM metrics get the same agent_id as all other hook-based events.
const ddlSessionAgentsStream = `
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_session_agents
(
  session_id    string,
  agent_id      string,
  deployment_id string,
  first_seen    datetime64(3)
) PRIMARY KEY (session_id)`

// ddlSessionAgentsMV feeds agentguard_session_agents from hook events for all
// agent types. Because agentguard_session_agents is a mutable stream
// (primary-key dedup on session_id), duplicate inserts are harmless — the stream
// always holds the latest agent_id/deployment_id seen for each session.
const ddlSessionAgentsMV = `
CREATE MATERIALIZED VIEW mv_session_agents
INTO agentguard_session_agents AS
SELECT
  session_id,
  agent_id,
  deployment_id,
  event_time AS first_seen
FROM agentguard_hook_events`

// ddlRunLatencyStream is a mutable stream keyed by run_id that captures LLM
// call latency from OpenClaw's agent_end hook events (which carry durationMs).
// It is joined in mv_cim_metrics_openclaw_llm to populate llm.latency_ms.
const ddlRunLatencyStream = `
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_run_latency
(
  run_id     string,
  latency_ms float64
) PRIMARY KEY (run_id)`

// ddlRunLatencyMV feeds agentguard_run_latency from agent_end hook events.
// agent_end fires ~2ms before llm_output for the same run_id, so the entry is
// available in the mutable stream by the time the metrics MV processes the
// corresponding llm_response CIM event.
const ddlRunLatencyMV = `
CREATE MATERIALIZED VIEW mv_run_latency
INTO agentguard_run_latency AS
SELECT
  run_id,
  to_float64_or_zero(json_extract_string(event_data, 'durationMs')) AS latency_ms
FROM agentguard_hook_events
WHERE hook_name = 'agent_end'`

const ddlCIMMetrics = `
CREATE STREAM agentguard_cim_metrics
(
  event_time    datetime64(3),
  metric_name   low_cardinality(string),
  metric_value  float64,
  agent_type    low_cardinality(string),
  agent_id      string,
  deployment_id low_cardinality(string),
  session_id    string,
  tags          map(low_cardinality(string), string)
)`

const ddlCIMMetricsOpenClawLlmMV = `
CREATE MATERIALIZED VIEW mv_cim_metrics_openclaw_llm
INTO agentguard_cim_metrics AS
SELECT
  event_time,
  metric_name,
  metric_value,
  'openclaw'                                                              AS agent_type,
  agent_id,
  deployment_id,
  session_id,
  map_cast(
    ['model', 'provider', 'stop_reason', 'run_id'],
    [model, provider, json_value(raw_event_data, '$.lastAssistant.stopReason'), run_id]
  )                                                                       AS tags
FROM (
  SELECT
    e.event_time,
    e.agent_id,
    e.deployment_id,
    e.session_id,
    e.run_id,
    e.model,
    e.provider,
    e.raw_event_data,
    if(r.latency_ms > 0, r.latency_ms, to_float64(0))                    AS latency_ms
  FROM agentguard_cim_event AS e
  LEFT JOIN agentguard_run_latency AS r ON r.run_id = e.run_id
  WHERE e.agent_type = 'openclaw'
    AND e.event_type = 'llm_response'
)
ARRAY JOIN
  ['llm.call', 'llm.input_tokens', 'llm.output_tokens', 'llm.total_tokens',
   'llm.cache_read_tokens', 'llm.cache_write_tokens', 'llm.latency_ms']  AS metric_name,
  [
    to_float64(1),
    to_float64_or_zero(json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'input')),
    to_float64_or_zero(json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'output')),
    to_float64_or_zero(json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'totalTokens')),
    to_float64_or_zero(json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'cacheRead')),
    to_float64_or_zero(json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'cacheWrite')),
    latency_ms
  ]                                                                       AS metric_value`

const ddlCIMMetricsOpenClawToolsMV = `
CREATE MATERIALIZED VIEW mv_cim_metrics_openclaw_tools
INTO agentguard_cim_metrics AS
SELECT
  event_time,
  metric_name,
  metric_value,
  'openclaw'                                                   AS agent_type,
  agent_id,
  deployment_id,
  session_id,
  map_cast(['tool_name', 'model', 'provider', 'run_id'], [tool_name, model, provider, run_id]) AS tags
FROM agentguard_cim_event
ARRAY JOIN
  ['tool.call', 'tool.success', 'tool.duration_ms']           AS metric_name,
  [
    to_float64(1),
    if(tool_success, to_float64(1), to_float64(0)),
    to_float64(tool_duration_ms)
  ]                                                            AS metric_value
WHERE agent_type = 'openclaw'
  AND event_type = 'tool_complete'`

const ddlCIMMetricsClaudeCodeOtelMV = `
CREATE MATERIALIZED VIEW mv_cim_metrics_claudecode_otel
INTO agentguard_cim_metrics AS
SELECT
  o._tp_time                                                              AS event_time,
  metric_name,
  metric_value,
  'claudecode'                                                            AS agent_type,
  if(s.agent_id != '', s.agent_id, o.log_attributes['user.id'])          AS agent_id,
  s.deployment_id                                                         AS deployment_id,
  o.log_attributes['session.id']                                         AS session_id,
  map_cast(
    ['model', 'provider', 'run_id'],
    [
      o.log_attributes['model'],
      multi_if(
        position(o.log_attributes['model'], '@') > 0, 'vertex',
        match(o.log_attributes['model'], '^(us\\.|eu\\.|ap\\.)?anthropic\\.'), 'bedrock',
        'anthropic'
      ),
      o.log_attributes['session.id']
    ]
  )                                                                       AS tags
FROM otel_logs AS o
LEFT JOIN agentguard_session_agents AS s
  ON s.session_id = o.log_attributes['session.id']
ARRAY JOIN
  ['llm.call', 'llm.input_tokens', 'llm.output_tokens', 'llm.total_tokens',
   'llm.cache_read_tokens', 'llm.cache_write_tokens', 'llm.latency_ms']  AS metric_name,
  [
    to_float64(1),
    to_float64_or_zero(o.log_attributes['input_tokens']),
    to_float64_or_zero(o.log_attributes['output_tokens']),
    to_float64_or_zero(o.log_attributes['input_tokens'])
      + to_float64_or_zero(o.log_attributes['output_tokens']),
    to_float64_or_zero(o.log_attributes['cache_read_tokens']),
    to_float64_or_zero(o.log_attributes['cache_creation_tokens']),
    to_float64_or_zero(o.log_attributes['duration_ms'])
  ]                                                                       AS metric_value
WHERE o.log_attributes['event.name'] = 'api_request'`

const ddlCIMMetricsClaudeCodeToolsMV = `
CREATE MATERIALIZED VIEW mv_cim_metrics_claudecode_tools
INTO agentguard_cim_metrics AS
SELECT
  event_time,
  metric_name,
  metric_value,
  'claudecode' AS agent_type,
  agent_id,
  deployment_id,
  session_id,
  map_cast(['tool_name', 'model', 'provider', 'run_id'], [tool_name, model, provider, run_id]) AS tags
FROM agentguard_cim_event
ARRAY JOIN
  ['tool.call', 'tool.success']             AS metric_name,
  [
    to_float64(1),
    if(tool_success, to_float64(1), to_float64(0))
  ]                                         AS metric_value
WHERE agent_type = 'claudecode'
  AND event_type = 'tool_complete'`

const ddlPricingStream = `
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_pricing
(
  provider                    low_cardinality(string),
  model                       low_cardinality(string),
  input_cost_per_million      float64,
  output_cost_per_million     float64,
  cache_read_cost_per_million float64,
  updated_at                  datetime64(3)
) PRIMARY KEY (provider, model)`

// ddlCostBudgetsStream is the append-only stream backing per-deployment monthly
// cost budgets. Current state is reconstructed via
//
//	arg_max(monthly_budget, updated_at), arg_max(deleted, updated_at)
//	GROUP BY deployment_id
//	HAVING NOT deleted
//
// — same pattern as agentguard_rules. Operators edit budgets via
// POST /api/cost/budget; each call appends a new row rather than mutating the
// previous one, giving us a free audit trail of who changed what when.
const ddlCostBudgetsStream = `
CREATE STREAM IF NOT EXISTS agentguard_cost_budgets
(
  deployment_id   low_cardinality(string),
  monthly_budget  float64,
  currency        low_cardinality(string),
  updated_by      string,
  deleted         bool,
  updated_at      datetime64(3)
)`

const ddlSkillsStream = `
CREATE STREAM IF NOT EXISTS agentguard_skills
(
  event_time        datetime64(3),
  agent_id          string,
  session_id        string,
  deployment_id     string,
  agent_type        low_cardinality(string),
  skill_name        string,
  skill_action      low_cardinality(string),
  skill_namespace   low_cardinality(string),
  skill_source_type low_cardinality(string),
  skill_location    string,
  skill_description string,
  skill_args        string
)`

const ddlMVSkillsInvokedClaudeCode = `
CREATE MATERIALIZED VIEW mv_skills_invoked_claudecode
INTO agentguard_skills AS
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  json_value(event_data, '$.tool_input.skill') AS skill_name,
  'invoked' AS skill_action,
  if(position(json_value(event_data, '$.tool_input.skill'), ':') > 0,
     split_by_string(':', json_value(event_data, '$.tool_input.skill'))[1],
     '') AS skill_namespace,
  if(position(json_value(event_data, '$.tool_input.skill'), ':') > 0,
     'plugin', 'user') AS skill_source_type,
  ''  AS skill_location,
  ''  AS skill_description,
  json_value(event_data, '$.tool_input.args') AS skill_args
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND tool_name = 'Skill'
  AND agent_type = 'claudecode'
  AND json_value(event_data, '$.tool_input.skill') != ''`

const ddlMVSkillsInvokedOpenClaw = `
CREATE MATERIALIZED VIEW mv_skills_invoked_openclaw
INTO agentguard_skills AS
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  extract(json_value(event_data, '$.params.path'),
          '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') AS skill_name,
  'invoked' AS skill_action,
  ''         AS skill_namespace,
  CASE
    WHEN position(json_value(event_data, '$.params.path'), '/.openclaw/')        > 0 THEN 'managed'
    WHEN position(json_value(event_data, '$.params.path'), '/.config/openclaw/') > 0 THEN 'managed'
    WHEN position(json_value(event_data, '$.params.path'), '/.agents/')          > 0 THEN 'personal'
    WHEN position(json_value(event_data, '$.params.path'), 'node_modules')       > 0 THEN 'bundled'
    WHEN length(json_value(event_data, '$.params.path'))                         > 0 THEN 'workspace'
    ELSE 'unknown'
  END        AS skill_source_type,
  json_value(event_data, '$.params.path') AS skill_location,
  ''         AS skill_description,
  ''         AS skill_args
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND tool_name = 'read'
  AND agent_type = 'openclaw'
  AND position(event_data, 'SKILL.md') > 0
  AND extract(json_value(event_data, '$.params.path'),
              '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') != ''`

const ddlMVSkillsInvokedHermes = `
CREATE MATERIALIZED VIEW mv_skills_invoked_hermes
INTO agentguard_skills AS
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  json_value(event_data, '$.args.name') AS skill_name,
  'invoked' AS skill_action,
  if(position(json_value(event_data, '$.args.name'), ':') > 0,
    split_by_string(':', json_value(event_data, '$.args.name'))[1],
    '') AS skill_namespace,
  if(position(json_value(event_data, '$.args.name'), ':') > 0,
    'plugin', 'user') AS skill_source_type,
  json_value(event_data, '$.args.path') AS skill_location,
  ''  AS skill_description,
  json_value(event_data, '$.args.args') AS skill_args
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'pre_tool_call'
  AND tool_name = 'skill_view'
  AND agent_type = 'hermes'
  AND skill_name != ''`

const ddlMVSkillsLoadedClaudeCode = `
CREATE MATERIALIZED VIEW mv_skills_loaded_claudecode
INTO agentguard_skills AS
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  trim(sn) AS skill_name,
  'loaded'  AS skill_action,
  if(position(trim(sn), ':') > 0,
     split_by_string(':', trim(sn))[1], '') AS skill_namespace,
  if(position(trim(sn), ':') > 0, 'plugin', 'user') AS skill_source_type,
  '' AS skill_location,
  '' AS skill_description,
  '' AS skill_args
FROM agentguard_hook_events
ARRAY JOIN extract_all(event_data, '---..name: ([a-zA-Z][a-zA-Z0-9:-]*)') AS sn
WHERE _tp_time > earliest_ts()
  AND hook_name = 'prompt_context'
  AND agent_type = 'claudecode'
  AND length(trim(sn)) > 0`

const ddlMVSkillsLoadedOpenClaw = `
CREATE MATERIALIZED VIEW mv_skills_loaded_openclaw
INTO agentguard_skills AS
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  trim(skill_tuple.1)    AS skill_name,
  'loaded'               AS skill_action,
  if(position(trim(skill_tuple.1), ':') > 0,
     split_by_string(':', trim(skill_tuple.1))[1], '') AS skill_namespace,
  CASE
    WHEN position(skill_tuple.3, '/.openclaw/')        > 0 THEN 'managed'
    WHEN position(skill_tuple.3, '/.config/openclaw/') > 0 THEN 'managed'
    WHEN position(skill_tuple.3, '/.agents/')          > 0 THEN 'personal'
    WHEN position(skill_tuple.3, 'node_modules')       > 0 THEN 'bundled'
    WHEN length(skill_tuple.3)                         > 0 THEN 'workspace'
    ELSE 'unknown'
  END                    AS skill_source_type,
  trim(skill_tuple.3)    AS skill_location,
  trim(skill_tuple.2)    AS skill_description,
  ''                     AS skill_args
FROM agentguard_hook_events
ARRAY JOIN array_zip(
  extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>'),
  array_resize(
    extract_all(event_data, '<description>([^<]+)</description>'),
    length(extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>')),
    ''
  ),
  array_resize(
    extract_all(event_data, '<location>([^<]+)</location>'),
    length(extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>')),
    ''
  )
) AS skill_tuple
WHERE _tp_time > earliest_ts()
  AND hook_name = 'llm_input'
  AND agent_type = 'openclaw'
  AND length(trim(skill_tuple.1)) > 0`

const ddlMemoryOpsStream = `
CREATE STREAM IF NOT EXISTS agentguard_memory_ops
(
  event_time      datetime64(3),
  session_id      string,
  agent_id        string,
  agent_type      low_cardinality(string),
  deployment_id   low_cardinality(string),
  memory_source   low_cardinality(string),
  memory_op       low_cardinality(string),
  tool_name       string,
  server_name     low_cardinality(string),
  memory_path     string,
  content_payload string
)`

const ddlMVMemoryClaudeCodeAutoMem = `
CREATE MATERIALIZED VIEW mv_memory_claudecode_automem
INTO agentguard_memory_ops AS
SELECT
  event_time,
  session_id,
  agent_id,
  agent_type,
  deployment_id,
  'auto_memory' AS memory_source,
  CASE
    WHEN tool_name = 'Write' THEN 'save'
    WHEN tool_name = 'Edit'  THEN 'update'
    WHEN tool_name = 'Read'  THEN 'read'
    ELSE 'other'
  END AS memory_op,
  tool_name,
  '' AS server_name,
  json_value(event_data, '$.tool_input.file_path') AS memory_path,
  json_query(event_data, '$.tool_input') AS content_payload
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND agent_type = 'claudecode'
  AND tool_name IN ('Write', 'Edit', 'Read')
  AND position(json_value(event_data, '$.tool_input.file_path'), '/.claude/projects/') > 0
  AND position(json_value(event_data, '$.tool_input.file_path'), '/memory/') > 0`

const ddlMVMemoryClaudeCodeMCP = `
CREATE MATERIALIZED VIEW mv_memory_claudecode_mcp
INTO agentguard_memory_ops AS
SELECT
  event_time,
  session_id,
  agent_id,
  agent_type,
  deployment_id,
  'mcp' AS memory_source,
  CASE
    WHEN match(lower(tool_name), 'save|store|remember|upsert|embed|write') THEN 'save'
    WHEN match(lower(tool_name), 'search|query|recall|retrieve|find')      THEN 'search'
    WHEN match(lower(tool_name), 'read|load|get|fetch|observation')        THEN 'read'
    WHEN match(lower(tool_name), 'delete|forget|remove')                   THEN 'delete'
    WHEN match(lower(tool_name), 'update|edit|patch')                      THEN 'update'
    ELSE 'other'
  END AS memory_op,
  tool_name,
  split_by_string('__', tool_name)[2] AS server_name,
  '' AS memory_path,
  json_query(event_data, '$.tool_input') AS content_payload
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND agent_type = 'claudecode'
  AND match(tool_name, '^mcp__')
  AND match(lower(tool_name), 'memory|mem|recall|remember|store|knowledge|forget|embedding')`

const ddlMVMemoryOpenClawFile = `
CREATE MATERIALIZED VIEW mv_memory_openclaw_file
INTO agentguard_memory_ops AS
SELECT
  event_time,
  session_id,
  agent_id,
  agent_type,
  deployment_id,
  'openclaw_file' AS memory_source,
  CASE
    WHEN tool_name = 'write' THEN 'save'
    WHEN tool_name = 'read'  THEN 'read'
    ELSE 'other'
  END AS memory_op,
  tool_name,
  '' AS server_name,
  json_value(event_data, '$.params.path') AS memory_path,
  json_query(event_data, '$.params') AS content_payload
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND agent_type = 'openclaw'
  AND tool_name IN ('write', 'read')
  AND match(json_value(event_data, '$.params.path'), '\.openclaw/workspace/memory/')`

const ddlMVMemoryHermes = `
CREATE MATERIALIZED VIEW mv_memory_hermes
INTO agentguard_memory_ops AS
SELECT
  event_time,
  session_id,
  agent_id,
  agent_type,
  deployment_id,
  'hermes_memory' AS memory_source,
  CASE
    WHEN json_value(event_data, '$.args.action') = 'add' THEN 'save'
    WHEN json_value(event_data, '$.args.action') = 'replace' THEN 'update'
    WHEN json_value(event_data, '$.args.action') = 'remove' THEN 'delete'
    ELSE 'other'
  END AS memory_op,
  tool_name,
  '' AS server_name,
  json_value(event_data, '$.args.target') AS memory_path,
  json_query(event_data, '$.args') AS content_payload
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'pre_tool_call'
  AND agent_type = 'hermes'
  AND tool_name = 'memory'`

type pricingSeed struct {
	provider  string
	model     string
	inputCPM  float64
	outputCPM float64
	cacheCPM  float64
}

var pricingSeedData = []pricingSeed{
	{"anthropic", "claude-sonnet-4-6", 3.00, 15.00, 0.30},
	{"anthropic", "claude-opus-4-6", 15.00, 75.00, 1.50},
	{"anthropic", "claude-haiku-4-5", 0.80, 4.00, 0.08},
	{"openai", "gpt-4o", 2.50, 10.00, 1.25},
	{"openai", "gpt-4o-mini", 0.15, 0.60, 0.075},
	{"openai", "o3", 10.00, 40.00, 2.50},
}

func createPricingStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_pricing", ddlPricingStream, emit)
}

// createCostBudgetsStream provisions the per-deployment monthly budget stream
// during initial setup. Existing deployments that completed setup before this
// stream existed should call EnsureCostBudgetsStream from the budget handlers
// instead — that path is idempotent and HTTP-based, so it works against a live
// server without re-running the setup wizard.
func createCostBudgetsStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_cost_budgets", ddlCostBudgetsStream, emit)
}

// EnsureCostBudgetsStream issues the budget stream DDL via the HTTP transport.
// Proton's CREATE STREAM IF NOT EXISTS makes it idempotent, so handlers can call
// this on first use without doing their own existence check. This is the
// migration path for deployments that finished setup before budgets shipped.
func EnsureCostBudgetsStream(cfg *config.TimeplusConfig) error {
	return ExecHTTP(cfg, ddlCostBudgetsStream)
}

func createThreatsStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_threats", ddlThreatsStream, emit)
}

func createThreatsMV(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "view", "mv_threats", ddlThreatsMV, emit)
}

func createSkillsStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_skills", ddlSkillsStream, emit)
}

func createSkillsMVs(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	mvs := []struct {
		name string
		ddl  string
	}{
		{"mv_skills_invoked_claudecode", ddlMVSkillsInvokedClaudeCode},
		{"mv_skills_invoked_openclaw", ddlMVSkillsInvokedOpenClaw},
		{"mv_skills_invoked_hermes", ddlMVSkillsInvokedHermes},
		{"mv_skills_loaded_claudecode", ddlMVSkillsLoadedClaudeCode},
		{"mv_skills_loaded_openclaw", ddlMVSkillsLoadedOpenClaw},
	}
	for _, mv := range mvs {
		if err := execIfNotExists(ctx, conn, "view", mv.name, mv.ddl, emit); err != nil {
			return err
		}
	}
	return nil
}

func createMemoryOpsStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_memory_ops", ddlMemoryOpsStream, emit)
}

func createMemoryMVs(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	mvs := []struct {
		name string
		ddl  string
	}{
		{"mv_memory_claudecode_automem", ddlMVMemoryClaudeCodeAutoMem},
		{"mv_memory_claudecode_mcp", ddlMVMemoryClaudeCodeMCP},
		{"mv_memory_openclaw_file", ddlMVMemoryOpenClawFile},
		{"mv_memory_hermes", ddlMVMemoryHermes},
	}
	for _, mv := range mvs {
		if err := execIfNotExists(ctx, conn, "view", mv.name, mv.ddl, emit); err != nil {
			return err
		}
	}
	return nil
}

func seedPricingData(ctx context.Context, conn proton.Conn) error {
	row := conn.QueryRow(ctx, "SELECT count() FROM table(agentguard_pricing)")
	var count uint64
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("pricing seed check: %w", err)
	}
	if count > 0 {
		log.Printf("timeplus: pricing already seeded (%d records), skipping", count)
		return nil
	}

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO agentguard_pricing (provider, model, input_cost_per_million, output_cost_per_million, cache_read_cost_per_million, updated_at)")
	if err != nil {
		return fmt.Errorf("pricing seed prepare: %w", err)
	}
	defer func() { _ = batch.Abort() }()

	now := time.Now()
	for _, p := range pricingSeedData {
		if err := batch.Append(p.provider, p.model, p.inputCPM, p.outputCPM, p.cacheCPM, now); err != nil {
			return fmt.Errorf("pricing seed append %s/%s: %w", p.provider, p.model, err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("pricing seed send: %w", err)
	}
	log.Printf("timeplus: seeded %d pricing entries", len(pricingSeedData))
	return nil
}

// ddlNotifyUDFTemplate is a format string; %s is replaced with the full
// webhook URL (e.g. "http://agentguard:8080/api/alerts/webhook").
const ddlNotifyUDFTemplate = `CREATE OR REPLACE FUNCTION notify_agentguard_udf(
  rule_id string, rule_name string, severity string,
  agent_id string, deployment_id string, session_id string,
  hook_name string, tool_name string, message string, detected_at string
) RETURNS string LANGUAGE PYTHON AS $$
import requests
def notify_agentguard_udf(rule_ids, rule_names, severities,
                            agent_ids, deployment_ids, session_ids,
                            hook_names, tool_names, messages, detected_ats):
    for i in range(len(rule_ids)):
        try:
            requests.post("%s", json={
                "rule_id": rule_ids[i],
                "rule_name": rule_names[i],
                "severity": severities[i],
                "agent_id": agent_ids[i],
                "message": messages[i],
                "detected_at": detected_ats[i]
            }, timeout=3)
        except Exception:
            pass
    return rule_ids
$$`

// ddlNotifierAlert fires the notify UDF for events in agentguard_notify_events.
// The threat-status filter lives in mv_notify_filter (alerts do not support JOINs).
const ddlNotifierAlert = `CREATE ALERT agentguard_notifier
  BATCH 1 EVENTS WITH TIMEOUT 5s
  LIMIT 10 ALERTS PER 1m
  CALL notify_agentguard_udf
AS SELECT
  rule_id, rule_name, severity, agent_id, deployment_id,
  session_id, hook_name, tool_name, message, detected_at
FROM agentguard_notify_events`

const ddlOTelInput = `
CREATE INPUT agentguard_otel_input
SETTINGS
  type                            = 'otel',
  protocol                        = 'http',
  tcp_port                        = 4318,
  listen_host                     = '0.0.0.0',
  traces_target_stream                        = 'otel_traces',
  logs_target_stream                          = 'otel_logs',
  metrics_gauge_target_stream                 = 'otel_metrics_gauge',
  metrics_sum_target_stream                   = 'otel_metrics_sum',
  metrics_histogram_target_stream             = 'otel_metrics_histogram',
  metrics_exponential_histogram_target_stream = 'otel_metrics_exponential_histogram',
  metrics_summary_target_stream               = 'otel_metrics_summary'
COMMENT 'OpenClaw agent telemetry via OTLP/HTTP'`

const ddlThreatsStream = `
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_threats
(
    agent_id          string,
    deployment_id     string,
    session_id        string,
    rule_id           string,
    rule_name         string,
    severity          string,
    last_seen         datetime64(3),
    status            string,
    status_updated_at datetime64(3),
    FAMILY cf_events(rule_name, severity, last_seen),
    FAMILY cf_status(status, status_updated_at)
) PRIMARY KEY (agent_id, deployment_id, session_id, rule_id)
SETTINGS coalesced = true`

const ddlThreatsMV = `
CREATE MATERIALIZED VIEW mv_threats
INTO agentguard_threats
AS SELECT
    agent_id,
    deployment_id,
    session_id,
    rule_id,
    rule_name,
    severity,
    detected_at AS last_seen
FROM agentguard_security_events
SETTINGS seek_to = 'earliest'`
