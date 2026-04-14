package timeplus

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	proton "github.com/timeplus-io/proton-go-driver/v2"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
)

// bt replaces « » placeholders with SQL backticks so raw-string DDL literals
// can quote identifiers that contain dots (e.g. `events.timestamp`,
// `exemplars.value`). Go raw strings can't contain backticks directly.
var btReplacer = strings.NewReplacer("«", "`", "»", "`")

func bt(s string) string { return btReplacer.Replace(s) }

// Init waits until Timeplus is reachable, then ensures all streams, MVs, and
// seed data are in place. catalogEntries are used to seed rules on first startup.
func Init(cfg *config.TimeplusConfig, catalogEntries []catalog.Entry) error {
	conn, err := waitForTimeplus(cfg)
	if err != nil {
		return fmt.Errorf("timeplus init: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()

	if err := createOTelStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating otel streams: %w", err)
	}
	if err := createOTelInput(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating otel input: %w", err)
	}
	if err := createAgentGuardStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating agentguard streams: %w", err)
	}
	if err := createCIMStream(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating CIM stream: %w", err)
	}
	if err := createCIMMVs(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating CIM MVs: %w", err)
	}
	if err := createCIMMetricsStream(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating CIM metrics stream: %v", err)
	}
	if err := createCIMMetricsMVs(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating CIM metrics MVs: %v", err)
	}
	if err := createSecurityStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating security streams: %w", err)
	}
	if err := createThreatsStream(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating threats stream: %v", err)
	}
	if err := createThreatsMV(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating threats MV: %v", err)
	}
	if err := createPricingStream(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating pricing stream: %v", err)
	}
	if err := seedPricingData(ctx, conn); err != nil {
		log.Printf("timeplus init: warning seeding pricing: %v", err)
	}
	if err := seedCatalogRules(ctx, conn, catalogEntries); err != nil {
		log.Printf("timeplus init: warning seeding rules: %v", err)
	}
	if err := dropLegacyMVs(ctx, conn); err != nil {
		log.Printf("timeplus init: warning dropping legacy MVs: %v", err)
	}
	if err := createNotifyUDF(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating UDF: %v", err)
	}
	if err := createNotifyFilterMV(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating notify filter MV: %v", err)
	}
	if err := createNotifierAlert(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating alert: %v", err)
	}
	if err := startupSyncMVs(cfg, ctx, conn); err != nil {
		log.Printf("timeplus init: warning syncing MVs: %v", err)
	}
	return nil
}

// waitForTimeplus pings Timeplus every 5 seconds until it responds, then
// returns the open connection. It never gives up — the caller (main) treats
// a failed init as non-fatal, so there is no point returning early.
func waitForTimeplus(cfg *config.TimeplusConfig) (proton.Conn, error) {
	opts := &proton.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.NativePort)},
		Auth: proton.Auth{
			Username: cfg.User,
			Password: cfg.Password,
		},
	}

	log.Printf("timeplus: waiting for %s:%d to become ready…", cfg.Host, cfg.NativePort)
	for attempt := 1; ; attempt++ {
		conn, err := proton.Open(opts)
		if err == nil {
			if pingErr := conn.Ping(context.Background()); pingErr == nil {
				log.Printf("timeplus: connected after %d attempt(s)", attempt)
				return conn, nil
			} else {
				_ = conn.Close()
				err = pingErr
			}
		}
		log.Printf("timeplus: not ready (attempt %d): %v — retrying in 5s", attempt, err)
		time.Sleep(5 * time.Second)
	}
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

func execIfNotExists(ctx context.Context, conn proton.Conn, kind, name, ddl string) error {
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
		log.Printf("timeplus: %s %q already exists, skipping", kind, name)
		return nil
	}
	if err := conn.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("creating %s %q: %w", kind, name, err)
	}
	log.Printf("timeplus: %s %q created", kind, name)
	return nil
}

func createOTelStreams(ctx context.Context, conn proton.Conn) error {
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
		if err := execIfNotExists(ctx, conn, "stream", s.name, s.ddl); err != nil {
			return err
		}
	}
	return nil
}

func createOTelInput(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "input", "openclaw_otel_input", ddlOTelInput)
}

func createAgentGuardStreams(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_hook_events", ddlAgentGuardHookEvents)
}

func createCIMStream(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_cim_event", ddlCIMEvent)
}

func createCIMMVs(ctx context.Context, conn proton.Conn) error {
	// Drop and recreate CIM MVs on every startup so DDL changes (e.g. field
	// extraction fixes) take effect without manual intervention. The gap between
	// DROP and CREATE during startup is harmless — no agent traffic is flowing.
	for _, name := range []string{"mv_cim_claudecode", "mv_cim_openclaw"} {
		if err := conn.Exec(ctx, fmt.Sprintf("DROP VIEW IF EXISTS %s", name)); err != nil {
			log.Printf("timeplus: warning dropping %s for recreation: %v", name, err)
		}
	}
	if err := conn.Exec(ctx, ddlCIMClaudeCodeMV); err != nil {
		return fmt.Errorf("creating view %q: %w", "mv_cim_claudecode", err)
	}
	if err := conn.Exec(ctx, ddlCIMOpenClawMV); err != nil {
		return fmt.Errorf("creating view %q: %w", "mv_cim_openclaw", err)
	}
	return nil
}

func createCIMMetricsStream(ctx context.Context, conn proton.Conn) error {
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_session_agents", ddlSessionAgentsStream); err != nil {
		return err
	}
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_run_latency", ddlRunLatencyStream); err != nil {
		return err
	}
	return execIfNotExists(ctx, conn, "stream", "agentguard_cim_metrics", ddlCIMMetrics)
}

func createCIMMetricsMVs(ctx context.Context, conn proton.Conn) error {
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
	}
	// Drop and recreate all CIM metrics MVs on every startup so DDL fixes take
	// effect without manual intervention. The startup gap is harmless.
	for _, mv := range mvs {
		if err := conn.Exec(ctx, fmt.Sprintf("DROP VIEW IF EXISTS %s", mv.name)); err != nil {
			log.Printf("timeplus: warning dropping %s for recreation: %v", mv.name, err)
		}
	}
	for _, mv := range mvs {
		if err := conn.Exec(ctx, mv.ddl); err != nil {
			return fmt.Errorf("creating view %q: %w", mv.name, err)
		}
		log.Printf("timeplus: view %q created", mv.name)
	}
	return nil
}

func createSecurityStreams(ctx context.Context, conn proton.Conn) error {
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_security_events", ddlSecurityEvents); err != nil {
		return err
	}
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_notify_events", ddlNotifyEventsStream); err != nil {
		return err
	}
	return execIfNotExists(ctx, conn, "stream", "agentguard_rules", ddlRulesStream)
}

func createNotifyFilterMV(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "stream", "mv_notify_filter", ddlNotifyFilterMV)
}

// ── Catalog rule seeding ──────────────────────────────────────────────────────

// seedCatalogRules seeds the agentguard_rules stream with entries from the
// catalog on first startup (when the stream is empty). Subsequent starts skip
// seeding so that user toggle/install state is preserved.
func seedCatalogRules(ctx context.Context, conn proton.Conn, entries []catalog.Entry) error {
	if len(entries) == 0 {
		return nil
	}
	row := conn.QueryRow(ctx, "SELECT count() FROM table(agentguard_rules)")
	var count uint64
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("seed check: %w", err)
	}
	if count > 0 {
		log.Printf("timeplus: rules already seeded (%d records), skipping", count)
		return nil
	}

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at)")
	if err != nil {
		return fmt.Errorf("seed prepare: %w", err)
	}

	now := time.Now()
	for _, e := range entries {
		if err := batch.Append(
			e.ID, e.Name, e.Version, e.Description, e.QuerySQL,
			false, false, e.Severity, e.Category, e.RequiredTelemetry,
			e.FPRate, e.HookSources, now,
		); err != nil {
			return fmt.Errorf("seed append %s: %w", e.ID, err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("seed send: %w", err)
	}
	log.Printf("timeplus: seeded %d catalog rules", len(entries))
	return nil
}

func dropLegacyMVs(ctx context.Context, conn proton.Conn) error {
	legacy := []string{
		"mv_security_prompt_injection",
		"mv_security_dlp",
		"mv_security_privilege_guard",
		"mv_security_supply_chain",
	}
	for _, name := range legacy {
		exists, err := streamExists(ctx, conn, name)
		if err != nil || !exists {
			continue
		}
		if err := conn.Exec(ctx, fmt.Sprintf("DROP VIEW IF EXISTS %s", name)); err != nil {
			log.Printf("timeplus: warning dropping legacy MV %s: %v", name, err)
		} else {
			log.Printf("timeplus: dropped legacy MV %s", name)
		}
	}
	return nil
}

func createNotifyUDF(ctx context.Context, conn proton.Conn) error {
	if err := conn.Exec(ctx, ddlNotifyUDF); err != nil {
		return fmt.Errorf("create UDF: %w", err)
	}
	log.Printf("timeplus: UDF notify_agentguard_udf created/updated")
	return nil
}

func createNotifierAlert(ctx context.Context, conn proton.Conn) error {
	// Drop first so the DDL always reflects the current definition on restart.
	// Ignore the error — it just means the alert didn't exist yet.
	_ = conn.Exec(ctx, `DROP ALERT IF EXISTS agentguard_notifier`)
	if err := conn.Exec(ctx, ddlNotifierAlert); err != nil {
		return fmt.Errorf("create alert: %w", err)
	}
	log.Printf("timeplus: alert agentguard_notifier created")
	return nil
}

const sqlStartupRules = `
SELECT
  rule_id,
  arg_max(query_sql, updated_at) AS query_sql,
  arg_max(enabled,   updated_at) AS enabled
FROM table(agentguard_rules)
GROUP BY rule_id
HAVING arg_max(deleted, updated_at) = false`

func startupSyncMVs(cfg *config.TimeplusConfig, ctx context.Context, conn proton.Conn) error {
	rows, err := conn.Query(ctx, sqlStartupRules)
	if err != nil {
		return fmt.Errorf("startup sync query: %w", err)
	}
	defer rows.Close()

	type ruleRow struct {
		ruleID   string
		querySQL string
		enabled  bool
	}
	var rules []ruleRow
	for rows.Next() {
		var r ruleRow
		if err := rows.Scan(&r.ruleID, &r.querySQL, &r.enabled); err != nil {
			log.Printf("timeplus: startup sync scan error: %v", err)
			continue
		}
		rules = append(rules, r)
	}

	for _, r := range rules {
		mvName := MVName(r.ruleID)
		exists, _ := streamExists(ctx, conn, mvName)
		if r.enabled {
			if !exists {
				ddl := fmt.Sprintf("CREATE MATERIALIZED VIEW IF NOT EXISTS %s INTO agentshield_security_events AS %s", mvName, r.querySQL)
				if err := conn.Exec(ctx, ddl); err != nil {
					log.Printf("timeplus: startup create MV %s: %v", mvName, err)
				} else {
					log.Printf("timeplus: startup created MV %s", mvName)
				}
			} else {
				if err := conn.Exec(ctx, fmt.Sprintf("SYSTEM RESUME MATERIALIZED VIEW %s PERMANENT", mvName)); err != nil {
					log.Printf("timeplus: startup resume MV %s: %v", mvName, err)
				}
			}
		} else if exists {
			if err := conn.Exec(ctx, fmt.Sprintf("SYSTEM PAUSE MATERIALIZED VIEW %s PERMANENT", mvName)); err != nil {
				log.Printf("timeplus: startup pause MV %s: %v", mvName, err)
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
  event_time       datetime64(3),
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

const ddlRulesStream = `
CREATE STREAM agentguard_rules
(
  rule_id            low_cardinality(string),
  name               string,
  version            string,
  description        string,
  query_sql          string,
  enabled            bool,
  deleted            bool,
  severity           low_cardinality(string),
  category           low_cardinality(string),
  required_telemetry string,
  fp_rate            low_cardinality(string),
  hook_sources       string,
  updated_at         datetime64(3)
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
  ''                                                               AS provider,
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
    'session_start', 'subagent_spawning',
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
  e.event_time,
  metric_name,
  metric_value,
  'openclaw'                                                              AS agent_type,
  e.agent_id,
  e.deployment_id,
  e.session_id,
  map_cast(
    ['model', 'stop_reason', 'run_id'],
    [e.model, json_value(e.raw_event_data, '$.lastAssistant.stopReason'), e.run_id]
  )                                                                       AS tags
FROM agentguard_cim_event AS e
LEFT JOIN agentguard_run_latency AS r ON r.run_id = e.run_id
ARRAY JOIN
  ['llm.call', 'llm.input_tokens', 'llm.output_tokens', 'llm.total_tokens',
   'llm.cache_read_tokens', 'llm.cache_write_tokens', 'llm.latency_ms']  AS metric_name,
  [
    to_float64(1),
    to_float64_or_zero(json_extract_string(e.raw_event_data, 'lastAssistant', 'usage', 'input')),
    to_float64_or_zero(json_extract_string(e.raw_event_data, 'lastAssistant', 'usage', 'output')),
    to_float64_or_zero(json_extract_string(e.raw_event_data, 'lastAssistant', 'usage', 'totalTokens')),
    to_float64_or_zero(json_extract_string(e.raw_event_data, 'lastAssistant', 'usage', 'cacheRead')),
    to_float64_or_zero(json_extract_string(e.raw_event_data, 'lastAssistant', 'usage', 'cacheWrite')),
    r.latency_ms
  ]                                                                       AS metric_value
WHERE e.agent_type = 'openclaw'
  AND e.event_type = 'llm_response'`

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
  map_cast(['tool_name', 'run_id'], [tool_name, run_id])      AS tags
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
    ['model', 'run_id'],
    [o.log_attributes['model'], o.log_attributes['session.id']]
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
  map_cast(['tool_name', 'run_id'], [tool_name, run_id]) AS tags
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

func createPricingStream(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_pricing", ddlPricingStream)
}

func createThreatsStream(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_threats", ddlThreatsStream)
}

func createThreatsMV(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "view", "mv_threats", ddlThreatsMV)
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

const ddlNotifyUDF = `CREATE OR REPLACE FUNCTION notify_agentguard_udf(
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
            requests.post("http://agentguard:8080/api/alerts/webhook", json={
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
CREATE INPUT openclaw_otel_input
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
