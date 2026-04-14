# Sending Claude Code Telemetry to Timeplus

This guide walks through enabling Claude Code's built-in OpenTelemetry support and routing its metrics, events, and traces into Timeplus using the native OpenTelemetry input.

## Architecture

```
Claude Code
  └── built-in OTLP exporter (env vars)
        └── OTLP/HTTP (Protobuf) ──► Timeplus OTel Input (port 4318)
                                        ├── otel_traces
                                        ├── otel_logs
                                        ├── otel_metrics_gauge
                                        ├── otel_metrics_sum
                                        └── otel_metrics_histogram
```

Claude Code shares the same Timeplus OTEL input on port **4318** used by OpenClaw. Data from both agents lands in the same streams, distinguished by the `service_name` resource attribute (`claude-code` vs `openclaw`).

## Prerequisites

- Timeplus running and accessible (default: `localhost:3218` for SQL, `localhost:8463` for native)
- Ports `4317` and `4318` exposed on the Timeplus container (already configured in `docker-compose.yaml`)

---

## Step 1: Create Target Streams in Timeplus

Skip this step if the streams already exist (e.g. from the OpenClaw OTEL setup). Connect to your Timeplus instance and run:

### Traces

```sql
CREATE STREAM otel_traces
(
  `trace_id`           string                                        CODEC(ZSTD(1)),
  `span_id`            string                                        CODEC(ZSTD(1)),
  `parent_span_id`     string                                        CODEC(ZSTD(1)),
  `trace_state`        string                                        CODEC(ZSTD(1)),
  `span_name`          low_cardinality(string)                       CODEC(ZSTD(1)),
  `span_kind`          low_cardinality(string)                       CODEC(ZSTD(1)),
  `service_name`       low_cardinality(string)                       CODEC(ZSTD(1)),
  `resource_attributes` map(low_cardinality(string), string)         CODEC(ZSTD(1)),
  `scope_name`         string                                        CODEC(ZSTD(1)),
  `scope_version`      string                                        CODEC(ZSTD(1)),
  `span_attributes`    map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  `duration`           uint64                                        CODEC(ZSTD(1)),
  `start_time_ns`      uint64                                        CODEC(ZSTD(1)),
  `end_time_ns`        uint64                                        CODEC(ZSTD(1)),
  `status_code`        low_cardinality(string)                       CODEC(ZSTD(1)),
  `status_message`     string                                        CODEC(ZSTD(1)),
  `events.timestamp`   array(datetime64(9))                          CODEC(ZSTD(1)),
  `events.name`        array(low_cardinality(string))                CODEC(ZSTD(1)),
  `events.attributes`  array(map(low_cardinality(string), string))   CODEC(ZSTD(1)),
  `links.trace_id`     array(string)                                 CODEC(ZSTD(1)),
  `links.span_id`      array(string)                                 CODEC(ZSTD(1)),
  `links.trace_state`  array(string)                                 CODEC(ZSTD(1)),
  `links.attributes`   array(map(low_cardinality(string), string))   CODEC(ZSTD(1)),
  INDEX idx_trace_id      trace_id                      TYPE bloom_filter(0.001) GRANULARITY 1,
  INDEX idx_res_attr_key  map_keys(resource_attributes) TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_res_attr_val  map_values(resource_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX idx_span_attr_key map_keys(span_attributes)     TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_span_attr_val map_values(span_attributes)   TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_duration      duration                      TYPE minmax              GRANULARITY 1
);
```

### Logs

```sql
CREATE STREAM otel_logs
(
  `trace_id`             string                                      CODEC(ZSTD(1)),
  `span_id`              string                                      CODEC(ZSTD(1)),
  `trace_flags`          uint32                                      CODEC(ZSTD(1)),
  `severity_text`        low_cardinality(string)                     CODEC(ZSTD(1)),
  `severity_number`      int32                                       CODEC(ZSTD(1)),
  `service_name`         low_cardinality(string)                     CODEC(ZSTD(1)),
  `body`                 string                                      CODEC(ZSTD(1)),
  `resource_schema_url`  string                                      CODEC(ZSTD(1)),
  `resource_attributes`  map(low_cardinality(string), string)        CODEC(ZSTD(1)),
  `scope_schema_url`     string                                      CODEC(ZSTD(1)),
  `scope_name`           string                                      CODEC(ZSTD(1)),
  `scope_version`        string                                      CODEC(ZSTD(1)),
  `scope_attributes`     map(low_cardinality(string), string)        CODEC(ZSTD(1)),
  `log_attributes`       map(low_cardinality(string), string)        CODEC(ZSTD(1)),
  INDEX idx_trace_id       trace_id                      TYPE bloom_filter(0.001) GRANULARITY 1,
  INDEX idx_res_attr_key   map_keys(resource_attributes) TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_res_attr_val   map_values(resource_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
  INDEX idx_log_attr_key   map_keys(log_attributes)      TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_log_attr_val   map_values(log_attributes)    TYPE bloom_filter(0.01)  GRANULARITY 1,
  INDEX idx_body           body                          TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
);
```

### Metrics (Gauge)

```sql
CREATE STREAM otel_metrics_gauge
(
  `resource_attributes`         map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  `resource_schema_url`         string                                        CODEC(ZSTD(1)),
  `scope_name`                  string                                        CODEC(ZSTD(1)),
  `scope_version`               string                                        CODEC(ZSTD(1)),
  `scope_attributes`            map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  `scope_dropped_attr_count`    uint32                                        CODEC(ZSTD(1)),
  `scope_schema_url`            string                                        CODEC(ZSTD(1)),
  `service_name`                low_cardinality(string)                       CODEC(ZSTD(1)),
  `metric_name`                 string                                        CODEC(ZSTD(1)),
  `metric_description`          string                                        CODEC(ZSTD(1)),
  `metric_unit`                 string                                        CODEC(ZSTD(1)),
  `attributes`                  map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  `start_time_unix`             datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  `time_unix`                   datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  `value`                       float64                                       CODEC(ZSTD(1)),
  `flags`                       uint32                                        CODEC(ZSTD(1)),
  `exemplars.filtered_attributes` array(map(low_cardinality(string), string)) CODEC(ZSTD(1)),
  `exemplars.time_unix`         array(datetime64(9))                          CODEC(ZSTD(1)),
  `exemplars.value`             array(float64)                                CODEC(ZSTD(1)),
  `exemplars.span_id`           array(string)                                 CODEC(ZSTD(1)),
  `exemplars.trace_id`          array(string)                                 CODEC(ZSTD(1))
);
```

### Metrics (Sum)

```sql
CREATE STREAM otel_metrics_sum
(
  `resource_attributes`         map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  `resource_schema_url`         string                                        CODEC(ZSTD(1)),
  `scope_name`                  string                                        CODEC(ZSTD(1)),
  `scope_version`               string                                        CODEC(ZSTD(1)),
  `scope_attributes`            map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  `scope_dropped_attr_count`    uint32                                        CODEC(ZSTD(1)),
  `scope_schema_url`            string                                        CODEC(ZSTD(1)),
  `service_name`                low_cardinality(string)                       CODEC(ZSTD(1)),
  `metric_name`                 string                                        CODEC(ZSTD(1)),
  `metric_description`          string                                        CODEC(ZSTD(1)),
  `metric_unit`                 string                                        CODEC(ZSTD(1)),
  `attributes`                  map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  `start_time_unix`             datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  `time_unix`                   datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  `value`                       float64                                       CODEC(ZSTD(1)),
  `flags`                       uint32                                        CODEC(ZSTD(1)),
  `exemplars.filtered_attributes` array(map(low_cardinality(string), string)) CODEC(ZSTD(1)),
  `exemplars.time_unix`         array(datetime64(9))                          CODEC(ZSTD(1)),
  `exemplars.value`             array(float64)                                CODEC(ZSTD(1)),
  `exemplars.span_id`           array(string)                                 CODEC(ZSTD(1)),
  `exemplars.trace_id`          array(string)                                 CODEC(ZSTD(1)),
  `aggregation_temporality`     int32                                         CODEC(ZSTD(1)),
  `is_monotonic`                bool                                          CODEC(Delta(1), ZSTD(1))
);
```

### Metrics (Histogram)

```sql
CREATE STREAM otel_metrics_histogram
(
  `resource_attributes`         map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  `resource_schema_url`         string                                        CODEC(ZSTD(1)),
  `scope_name`                  string                                        CODEC(ZSTD(1)),
  `scope_version`               string                                        CODEC(ZSTD(1)),
  `scope_attributes`            map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  `scope_dropped_attr_count`    uint32                                        CODEC(ZSTD(1)),
  `scope_schema_url`            string                                        CODEC(ZSTD(1)),
  `service_name`                low_cardinality(string)                       CODEC(ZSTD(1)),
  `metric_name`                 string                                        CODEC(ZSTD(1)),
  `metric_description`          string                                        CODEC(ZSTD(1)),
  `metric_unit`                 string                                        CODEC(ZSTD(1)),
  `attributes`                  map(low_cardinality(string), string)          CODEC(ZSTD(1)),
  `start_time_unix`             datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  `time_unix`                   datetime64(9)                                 CODEC(Delta(8), ZSTD(1)),
  `count`                       uint64                                        CODEC(Delta(8), ZSTD(1)),
  `sum`                         float64                                       CODEC(ZSTD(1)),
  `bucket_counts`               array(uint64)                                 CODEC(ZSTD(1)),
  `explicit_bounds`             array(float64)                                CODEC(ZSTD(1)),
  `min`                         float64                                       CODEC(ZSTD(1)),
  `max`                         float64                                       CODEC(ZSTD(1)),
  `flags`                       uint32                                        CODEC(ZSTD(1)),
  `exemplars.filtered_attributes` array(map(low_cardinality(string), string)) CODEC(ZSTD(1)),
  `exemplars.time_unix`         array(datetime64(9))                          CODEC(ZSTD(1)),
  `exemplars.value`             array(float64)                                CODEC(ZSTD(1)),
  `exemplars.span_id`           array(string)                                 CODEC(ZSTD(1)),
  `exemplars.trace_id`          array(string)                                 CODEC(ZSTD(1)),
  `aggregation_temporality`     int32                                         CODEC(ZSTD(1))
);
```

---

## Step 2: Create the OTel Input in Timeplus

Skip this step if the input already exists (e.g. from the OpenClaw OTEL setup — Claude Code sends to the same port and the data is distinguished by `service_name`).

Claude Code uses **OTLP/HTTP (Protobuf)** on port **4318**.

```sql
CREATE INPUT agentguard_otel_input
SETTINGS
  type                                          = 'otel',
  protocol                                      = 'http',
  tcp_port                                      = 4318,
  listen_host                                   = '0.0.0.0',
  traces_target_stream                          = 'otel_traces',
  logs_target_stream                            = 'otel_logs',
  metrics_gauge_target_stream                   = 'otel_metrics_gauge',
  metrics_sum_target_stream                     = 'otel_metrics_sum',
  metrics_histogram_target_stream               = 'otel_metrics_histogram'
COMMENT 'AgentGuard agent telemetry via OTLP/HTTP (Claude Code + OpenClaw)';
```

> If you previously created `openclaw_otel_input` on port 4318, it already accepts Claude Code data — no new input is needed.

---

## Step 3: Configure Claude Code

OTEL settings are injected into `~/.claude/settings.json` under the `env` key. Use the Makefile targets:

```bash
# Configure OTEL (adds env vars to ~/.claude/settings.json)
make configure-otel

# Remove OTEL configuration
make unconfigure-otel

# Configure hooks + OTEL together, then launch claude
make start
```

Or configure manually by adding to `~/.claude/settings.json`:

```json
{
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
    "OTEL_METRICS_EXPORTER": "otlp",
    "OTEL_LOGS_EXPORTER": "otlp",
    "OTEL_TRACES_EXPORTER": "otlp",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318",
    "OTEL_METRIC_EXPORT_INTERVAL": "10000",
    "OTEL_LOGS_EXPORT_INTERVAL": "5000",
    "OTEL_LOG_TOOL_DETAILS": "1"
  }
}
```

**Key settings:**

| Variable | Value | Notes |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` | Timeplus OTEL HTTP input port |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` | Claude Code supports gRPC and HTTP; HTTP shares port 4318 with OpenClaw |
| `OTEL_METRICS_EXPORTER` | `otlp` | Emits `claude_code.*` counters → `otel_metrics_sum` |
| `OTEL_LOGS_EXPORTER` | `otlp` | Emits structured events → `otel_logs` |
| `OTEL_TRACES_EXPORTER` | `otlp` | Beta distributed traces → `otel_traces` |
| `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA` | `1` | Required to enable trace export |
| `OTEL_LOG_TOOL_DETAILS` | `1` | Includes bash commands, file paths, MCP tool names in log events |
| `OTEL_METRIC_EXPORT_INTERVAL` | `10000` | 10s during setup; set to `60000` for production |
| `OTEL_LOGS_EXPORT_INTERVAL` | `5000` | 5s (default) |

---

## Step 4: Verify Data is Flowing

After running `claude` and sending a prompt, query the streams in Timeplus.

> Use `table(stream)` for historical point-in-time queries. Use the bare stream name for live streaming queries.

```sql
-- Recent Claude Code log events (historical)
SELECT
  _tp_time,
  service_name,
  log_attributes['event.name']   AS event_name,
  log_attributes['model']        AS model,
  log_attributes['cost_usd']     AS cost_usd,
  log_attributes['input_tokens'] AS input_tokens,
  log_attributes['tool_name']    AS tool_name,
  log_attributes['duration_ms']  AS duration_ms
FROM table(otel_logs)
WHERE service_name = 'claude-code'
ORDER BY _tp_time DESC
LIMIT 20;
```

```sql
-- Token usage per model (last 1 hour, historical)
SELECT
  attributes['model']  AS model,
  attributes['type']   AS token_type,
  sum(value)           AS total_tokens
FROM table(otel_metrics_sum)
WHERE service_name = 'claude-code'
  AND metric_name = 'claude_code.token.usage'
  AND _tp_time > now() - INTERVAL 1 HOUR
GROUP BY model, token_type
ORDER BY total_tokens DESC;
```

```sql
-- Cost per model (last 1 hour, historical)
SELECT
  attributes['model']  AS model,
  sum(value)           AS total_cost_usd
FROM table(otel_metrics_sum)
WHERE service_name = 'claude-code'
  AND metric_name = 'claude_code.cost.usage'
  AND _tp_time > now() - INTERVAL 1 HOUR
GROUP BY model
ORDER BY total_cost_usd DESC;
```

```sql
-- Tool execution events with details (historical)
SELECT
  _tp_time,
  log_attributes['tool_name']       AS tool,
  log_attributes['success']         AS success,
  log_attributes['duration_ms']     AS duration_ms,
  log_attributes['tool_parameters'] AS parameters
FROM table(otel_logs)
WHERE service_name = 'claude-code'
  AND log_attributes['event.name'] = 'tool_result'
ORDER BY _tp_time DESC
LIMIT 20;
```

```sql
-- Live streaming tail (continuous query)
SELECT
  _tp_time,
  service_name,
  log_attributes['event.name'] AS event_name,
  log_attributes['model']      AS model
FROM otel_logs
WHERE service_name = 'claude-code';
```

```sql
-- Both agents side by side: token usage comparison
SELECT
  service_name,
  sum(value) AS total_tokens
FROM table(otel_metrics_sum)
WHERE metric_name IN ('claude_code.token.usage', 'gen_ai.token.usage')
  AND _tp_time > now() - INTERVAL 1 HOUR
GROUP BY service_name
ORDER BY total_tokens DESC;
```

---

## What Claude Code Sends

| Signal | Timeplus Stream | Examples |
|--------|----------------|---------|
| Metrics (counters) | `otel_metrics_sum` | `claude_code.session.count`, `claude_code.token.usage`, `claude_code.cost.usage`, `claude_code.lines_of_code.count`, `claude_code.commit.count` |
| Events | `otel_logs` | `claude_code.user_prompt`, `claude_code.api_request`, `claude_code.tool_result`, `claude_code.api_error`, `claude_code.tool_decision` |
| Traces (beta) | `otel_traces` | One span per user prompt, linking to API calls and tool executions |

Claude Code does not produce gauge or histogram metrics. All metrics land in `otel_metrics_sum`.

---

## Troubleshooting

**No data in streams after starting claude**

- Confirm Timeplus is listening on port 4318: `lsof -i :4318`
- Check the OTEL input exists in Timeplus: `SHOW INPUTS`
- Verify env vars are set: run `env | grep OTEL` inside a Claude Code session via a Bash tool call, or check `~/.claude/settings.json`
- Reduce export intervals for debugging: `OTEL_METRIC_EXPORT_INTERVAL=5000`

**Events appear but metrics are empty**

Metrics export on a 10-second interval (default 60s). Wait for the first flush or reduce `OTEL_METRIC_EXPORT_INTERVAL`.

**`service_name` is missing or wrong**

Claude Code automatically sets `service.name=claude-code`. If you see a different value, check that no `OTEL_RESOURCE_ATTRIBUTES` override is setting `service.name`.

**Traces not appearing in `otel_traces`**

Traces require both `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` and `OTEL_TRACES_EXPORTER=otlp`. Both must be set.

---

## References

- [Claude Code Monitoring docs](https://code.claude.com/docs/en/monitoring-usage)
- [Timeplus OTel Input docs](https://docs.timeplus.com/otel-input)
- [OpenTelemetry specification](https://opentelemetry.io/docs/)
