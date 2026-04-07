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
		return fmt.Errorf("timeplus init: creating streams: %w", err)
	}
	if err := createOTelInput(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating input: %w", err)
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
	if kind == "stream" {
		exists, err = streamExists(ctx, conn, name)
	} else {
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
