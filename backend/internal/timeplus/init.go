package timeplus

import (
	"context"
	"fmt"
	"log"
	"time"

	proton "github.com/timeplus-io/proton-go-driver/v2"
	"github.com/timeplus/agentshield/internal/config"
)

// Init waits until Timeplus is reachable, then ensures all OTel streams and
// the OTel input exist. It blocks indefinitely until the connection succeeds.
func Init(cfg *config.TimeplusConfig) error {
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
	if err := createAgentShieldStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating agentshield streams: %w", err)
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
	if err := seedBuiltinRules(ctx, conn); err != nil {
		log.Printf("timeplus init: warning seeding rules: %v", err)
	}
	if err := dropLegacyMVs(ctx, conn); err != nil {
		log.Printf("timeplus init: warning dropping legacy MVs: %v", err)
	}
	if err := createNotifyUDF(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating UDF: %v", err)
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
				conn.Close()
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

func createAgentShieldStreams(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "stream", "agentshield_hook_events", ddlAgentShieldHookEvents)
}

func createSecurityStreams(ctx context.Context, conn proton.Conn) error {
	if err := execIfNotExists(ctx, conn, "stream", "agentshield_security_events", ddlSecurityEvents); err != nil {
		return err
	}
	return execIfNotExists(ctx, conn, "stream", "agentshield_rules", ddlRulesStream)
}

// ── Builtin rules ─────────────────────────────────────────────────────────────

type builtinRule struct {
	ruleID            string
	name              string
	version           string
	description       string
	querySQL          string
	severity          string
	category          string
	requiredTelemetry string
	fpRate            string
	hookSources       string
}

var builtinRuleData = []builtinRule{
	{
		ruleID:      "rp-001",
		name:        "Prompt Injection Shield",
		version:     "1.0.0",
		description: "Detects direct and indirect prompt injection attacks, jailbreak attempts, and goal hijacking across all message surfaces.",
		severity:    "critical",
		category:    "prompt_injection",
		fpRate:      "medium",
		hookSources: "llm_input",
		querySQL: `SELECT
  event_time                AS detected_at,
  'rp-001'                  AS rule_id,
  'Prompt Injection Shield' AS rule_name,
  'critical'                AS severity,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  hook_name,
  ''                        AS tool_name,
  concat('Potential prompt injection in ', hook_name, ' from agent ', agent_id) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name = 'llm_input'
  AND (
       lower(event_data) LIKE '%ignore previous instructions%'
    OR lower(event_data) LIKE '%ignore all previous%'
    OR lower(event_data) LIKE '%jailbreak%'
    OR lower(event_data) LIKE '%disregard your%'
    OR lower(event_data) LIKE '%you are now a%'
    OR lower(event_data) LIKE '%forget everything%'
    OR lower(event_data) LIKE '%new instructions:%'
  )`,
	},
	{
		ruleID:            "rp-002",
		name:              "DLP Sentinel",
		version:           "1.0.0",
		description:       "Scans outbound agent communication for PII, API keys, and sensitive internal documentation leaks.",
		severity:          "critical",
		category:          "dlp",
		requiredTelemetry: "content_capture",
		fpRate:            "low",
		hookSources:       "tool_result_persist,after_tool_call,llm_output",
		querySQL: `SELECT
  event_time     AS detected_at,
  'rp-002'       AS rule_id,
  'DLP Sentinel' AS rule_name,
  'critical'     AS severity,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  hook_name,
  tool_name,
  concat('Sensitive data in ', hook_name, if(tool_name != '', concat(' (', tool_name, ')'), ''), ' from agent ', agent_id) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name IN ('tool_result_persist', 'after_tool_call', 'llm_output')
  AND (
       match(event_data, '[0-9]{3}-[0-9]{2}-[0-9]{4}')
    OR lower(event_data) LIKE '%api_key%'
    OR lower(event_data) LIKE '%secret_key%'
    OR lower(event_data) LIKE '%bearer %'
    OR lower(event_data) LIKE '%sk-ant-%'
    OR lower(event_data) LIKE '%ghp_%'
    OR lower(event_data) LIKE '%private_key%'
    OR lower(event_data) LIKE '%aws_secret%'
  )`,
	},
	{
		ruleID:      "rp-003",
		name:        "Privilege Guard",
		version:     "1.0.0",
		description: "Monitors privilege escalation attempts through tool abuse, shell escapes, and dangerous system commands.",
		severity:    "warning",
		category:    "privilege",
		fpRate:      "low",
		hookSources: "before_tool_call",
		querySQL: `SELECT
  event_time        AS detected_at,
  'rp-003'          AS rule_id,
  'Privilege Guard' AS rule_name,
  'warning'         AS severity,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  hook_name,
  tool_name,
  concat('Dangerous tool call: ', tool_name, ' by agent ', agent_id) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name = 'before_tool_call'
  AND (
       lower(tool_name) IN ('bash', 'shell', 'exec', 'run_terminal_cmd', 'computer')
    OR lower(event_data) LIKE '%sudo %'
    OR lower(event_data) LIKE '%rm -rf%'
    OR lower(event_data) LIKE '%chmod %'
    OR lower(event_data) LIKE '%/etc/passwd%'
    OR lower(event_data) LIKE '%/etc/shadow%'
  )`,
	},
	{
		ruleID:      "rp-004",
		name:        "Supply Chain Watch",
		version:     "1.0.0",
		description: "Monitors external model dependencies and third-party library calls for anomalous behavior.",
		severity:    "warning",
		category:    "supply_chain",
		fpRate:      "high",
		hookSources: "before_install,before_tool_call",
		querySQL: `SELECT
  event_time           AS detected_at,
  'rp-004'             AS rule_id,
  'Supply Chain Watch' AS rule_name,
  'warning'            AS severity,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  hook_name,
  tool_name,
  concat('Package install by agent ', agent_id, ' via ', if(tool_name != '', tool_name, hook_name)) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name = 'before_install'
   OR (
       hook_name = 'before_tool_call'
    AND (
           lower(event_data) LIKE '%npm install%'
        OR lower(event_data) LIKE '%pip install%'
        OR lower(event_data) LIKE '%yarn add%'
        OR lower(event_data) LIKE '%cargo install%'
        OR lower(event_data) LIKE '%apt-get install%'
        OR lower(event_data) LIKE '%brew install%'
    )
  )`,
	},
}

func seedBuiltinRules(ctx context.Context, conn proton.Conn) error {
	row := conn.QueryRow(ctx, "SELECT count() FROM table(agentshield_rules)")
	var count uint64
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("seed check: %w", err)
	}
	if count > 0 {
		log.Printf("timeplus: rules already seeded (%d records), skipping", count)
		return nil
	}

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO agentshield_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at)")
	if err != nil {
		return fmt.Errorf("seed prepare: %w", err)
	}

	now := time.Now()
	for _, r := range builtinRuleData {
		if err := batch.Append(
			r.ruleID, r.name, r.version, r.description, r.querySQL,
			false, false, r.severity, r.category, r.requiredTelemetry,
			r.fpRate, r.hookSources, now,
		); err != nil {
			return fmt.Errorf("seed append %s: %w", r.ruleID, err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("seed send: %w", err)
	}
	log.Printf("timeplus: seeded %d built-in rules", len(builtinRuleData))
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
	log.Printf("timeplus: UDF notify_agentshield_udf created/updated")
	return nil
}

func createNotifierAlert(ctx context.Context, conn proton.Conn) error {
	if err := conn.Exec(ctx, ddlNotifierAlert); err != nil {
		return fmt.Errorf("create alert: %w", err)
	}
	log.Printf("timeplus: alert agentshield_notifier created")
	return nil
}

const sqlStartupRules = `
SELECT
  rule_id,
  arg_max(query_sql, updated_at) AS query_sql,
  arg_max(enabled,   updated_at) AS enabled
FROM table(agentshield_rules)
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
				ddl := fmt.Sprintf("CREATE MATERIALIZED VIEW %s INTO agentshield_security_events AS %s", mvName, r.querySQL)
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

const ddlOTelTraces = `
CREATE STREAM otel_traces
(
  trace_id            string                                      CODEC(ZSTD(1)),
  span_id             string                                      CODEC(ZSTD(1)),
  parent_span_id      string                                      CODEC(ZSTD(1)),
  trace_state         string                                      CODEC(ZSTD(1)),
  span_name           low_cardinality(string)                     CODEC(ZSTD(1)),
  span_kind           low_cardinality(string)                     CODEC(ZSTD(1)),
  service_name        low_cardinality(string)                     CODEC(ZSTD(1)),
  resource_attributes map(low_cardinality(string), string)        CODEC(ZSTD(1)),
  scope_name          string                                      CODEC(ZSTD(1)),
  scope_version       string                                      CODEC(ZSTD(1)),
  span_attributes     map(low_cardinality(string), string)        CODEC(ZSTD(1)),
  duration            uint64                                      CODEC(ZSTD(1)),
  start_time_ns       uint64                                      CODEC(ZSTD(1)),
  end_time_ns         uint64                                      CODEC(ZSTD(1)),
  status_code         low_cardinality(string)                     CODEC(ZSTD(1)),
  status_message      string                                      CODEC(ZSTD(1)),
  events_timestamp    array(datetime64(9))                        CODEC(ZSTD(1)),
  events_name         array(low_cardinality(string))              CODEC(ZSTD(1)),
  events_attributes   array(map(low_cardinality(string), string)) CODEC(ZSTD(1)),
  links_trace_id      array(string)                               CODEC(ZSTD(1)),
  links_span_id       array(string)                               CODEC(ZSTD(1)),
  links_trace_state   array(string)                               CODEC(ZSTD(1)),
  links_attributes    array(map(low_cardinality(string), string)) CODEC(ZSTD(1)),
  INDEX idx_trace_id       trace_id                       TYPE bloom_filter(0.001) GRANULARITY 1,
  INDEX idx_res_attr_key   map_keys(resource_attributes)  TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_res_attr_val   map_values(resource_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX idx_span_attr_key  map_keys(span_attributes)      TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_span_attr_val  map_values(span_attributes)    TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_duration       duration                       TYPE minmax              GRANULARITY 1
)`

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

const ddlOTelMetricsGauge = `
CREATE STREAM otel_metrics_gauge
(
  resource_attributes          map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  resource_schema_url          string                                        CODEC(ZSTD(1)),
  scope_name                   string                                        CODEC(ZSTD(1)),
  scope_version                string                                        CODEC(ZSTD(1)),
  scope_attributes             map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  scope_dropped_attr_count     uint32                                        CODEC(ZSTD(1)),
  scope_schema_url             string                                        CODEC(ZSTD(1)),
  service_name                 low_cardinality(string)                       CODEC(ZSTD(1)),
  metric_name                  string                                        CODEC(ZSTD(1)),
  metric_description           string                                        CODEC(ZSTD(1)),
  metric_unit                  string                                        CODEC(ZSTD(1)),
  attributes                   map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  start_time_unix              datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  time_unix                    datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  value                        float64                                       CODEC(ZSTD(1)),
  flags                        uint32                                        CODEC(ZSTD(1)),
  exemplars_filtered_attributes array(map(low_cardinality(string), string))  CODEC(ZSTD(1)),
  exemplars_time_unix          array(datetime64(9))                          CODEC(ZSTD(1)),
  exemplars_value              array(float64)                                CODEC(ZSTD(1)),
  exemplars_span_id            array(string)                                 CODEC(ZSTD(1)),
  exemplars_trace_id           array(string)                                 CODEC(ZSTD(1))
)`

const ddlOTelMetricsSum = `
CREATE STREAM otel_metrics_sum
(
  resource_attributes          map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  resource_schema_url          string                                        CODEC(ZSTD(1)),
  scope_name                   string                                        CODEC(ZSTD(1)),
  scope_version                string                                        CODEC(ZSTD(1)),
  scope_attributes             map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  scope_dropped_attr_count     uint32                                        CODEC(ZSTD(1)),
  scope_schema_url             string                                        CODEC(ZSTD(1)),
  service_name                 low_cardinality(string)                       CODEC(ZSTD(1)),
  metric_name                  string                                        CODEC(ZSTD(1)),
  metric_description           string                                        CODEC(ZSTD(1)),
  metric_unit                  string                                        CODEC(ZSTD(1)),
  attributes                   map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  start_time_unix              datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  time_unix                    datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  value                        float64                                       CODEC(ZSTD(1)),
  flags                        uint32                                        CODEC(ZSTD(1)),
  exemplars_filtered_attributes array(map(low_cardinality(string), string))  CODEC(ZSTD(1)),
  exemplars_time_unix          array(datetime64(9))                          CODEC(ZSTD(1)),
  exemplars_value              array(float64)                                CODEC(ZSTD(1)),
  exemplars_span_id            array(string)                                 CODEC(ZSTD(1)),
  exemplars_trace_id           array(string)                                 CODEC(ZSTD(1)),
  aggregation_temporality      int32                                         CODEC(ZSTD(1)),
  is_monotonic                 bool                                          CODEC(Delta(1), ZSTD(1))
)`

const ddlOTelMetricsHistogram = `
CREATE STREAM otel_metrics_histogram
(
  resource_attributes          map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  resource_schema_url          string                                        CODEC(ZSTD(1)),
  scope_name                   string                                        CODEC(ZSTD(1)),
  scope_version                string                                        CODEC(ZSTD(1)),
  scope_attributes             map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  scope_dropped_attr_count     uint32                                        CODEC(ZSTD(1)),
  scope_schema_url             string                                        CODEC(ZSTD(1)),
  service_name                 low_cardinality(string)                       CODEC(ZSTD(1)),
  metric_name                  string                                        CODEC(ZSTD(1)),
  metric_description           string                                        CODEC(ZSTD(1)),
  metric_unit                  string                                        CODEC(ZSTD(1)),
  attributes                   map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  start_time_unix              datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  time_unix                    datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  count                        uint64                                        CODEC(Delta(8), ZSTD(1)),
  sum                          float64                                       CODEC(ZSTD(1)),
  bucket_counts                array(uint64)                                 CODEC(ZSTD(1)),
  explicit_bounds              array(float64)                                CODEC(ZSTD(1)),
  min                          float64                                       CODEC(ZSTD(1)),
  max                          float64                                       CODEC(ZSTD(1)),
  flags                        uint32                                        CODEC(ZSTD(1)),
  exemplars_filtered_attributes array(map(low_cardinality(string), string))  CODEC(ZSTD(1)),
  exemplars_time_unix          array(datetime64(9))                          CODEC(ZSTD(1)),
  exemplars_value              array(float64)                                CODEC(ZSTD(1)),
  exemplars_span_id            array(string)                                 CODEC(ZSTD(1)),
  exemplars_trace_id           array(string)                                 CODEC(ZSTD(1)),
  aggregation_temporality      int32                                         CODEC(ZSTD(1))
)`

const ddlAgentShieldHookEvents = `
CREATE STREAM agentshield_hook_events
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
  event_data       string
)`

const ddlSecurityEvents = `
CREATE STREAM agentshield_security_events
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

const ddlRulesStream = `
CREATE STREAM agentshield_rules
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

const ddlPricingStream = `
CREATE MUTABLE STREAM IF NOT EXISTS agentshield_pricing
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
	return execIfNotExists(ctx, conn, "stream", "agentshield_pricing", ddlPricingStream)
}

func createThreatsStream(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "stream", "agentshield_threats", ddlThreatsStream)
}

func createThreatsMV(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "view", "mv_threats", ddlThreatsMV)
}

func seedPricingData(ctx context.Context, conn proton.Conn) error {
	row := conn.QueryRow(ctx, "SELECT count() FROM table(agentshield_pricing)")
	var count uint64
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("pricing seed check: %w", err)
	}
	if count > 0 {
		log.Printf("timeplus: pricing already seeded (%d records), skipping", count)
		return nil
	}

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO agentshield_pricing (provider, model, input_cost_per_million, output_cost_per_million, cache_read_cost_per_million, updated_at)")
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

const ddlNotifyUDF = `CREATE OR REPLACE FUNCTION notify_agentshield_udf(
  rule_id string, rule_name string, severity string,
  agent_id string, deployment_id string, session_id string,
  hook_name string, tool_name string, message string, detected_at string
) RETURNS string LANGUAGE PYTHON AS $$
import requests
def notify_agentshield_udf(rule_ids, rule_names, severities,
                            agent_ids, deployment_ids, session_ids,
                            hook_names, tool_names, messages, detected_ats):
    for i in range(len(rule_ids)):
        try:
            requests.post("http://agentshield:8080/api/alerts/webhook", json={
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

const ddlNotifierAlert = `CREATE ALERT IF NOT EXISTS agentshield_notifier
  BATCH 1 EVENTS WITH TIMEOUT 5s
  LIMIT 10 ALERTS PER 1m
  CALL notify_agentshield_udf
AS SELECT
  rule_id, rule_name, severity, agent_id, deployment_id,
  session_id, hook_name, tool_name, message, detected_at
FROM agentshield_security_events`

const ddlOTelInput = `
CREATE INPUT openclaw_otel_input
SETTINGS
  type                            = 'otel',
  protocol                        = 'http',
  tcp_port                        = 4318,
  listen_host                     = '0.0.0.0',
  traces_target_stream            = 'otel_traces',
  logs_target_stream              = 'otel_logs',
  metrics_gauge_target_stream     = 'otel_metrics_gauge',
  metrics_sum_target_stream       = 'otel_metrics_sum',
  metrics_histogram_target_stream = 'otel_metrics_histogram'
COMMENT 'OpenClaw agent telemetry via OTLP/HTTP'`

const ddlThreatsStream = `
CREATE MUTABLE STREAM IF NOT EXISTS agentshield_threats
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
INTO agentshield_threats
AS SELECT
    agent_id,
    deployment_id,
    session_id,
    rule_id,
    rule_name,
    severity,
    detected_at AS last_seen
FROM agentshield_security_events
SETTINGS seek_to = 'earliest'`
