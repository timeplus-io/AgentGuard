# Sending OpenClaw Telemetry to Timeplus

This guide walks through enabling the OpenClaw `diagnostics-otel` plugin and routing its traces, metrics, and logs into Timeplus using the native OpenTelemetry input.

## Architecture

```
OpenClaw Gateway
  └── diagnostics-otel plugin
        └── OTLP/gRPC or HTTP ──► Timeplus OTel Input (port 4317/4318)
                                        ├── otel_traces
                                        ├── otel_logs
                                        ├── otel_metrics_gauge
                                        ├── otel_metrics_sum
                                        └── otel_metrics_histogram
```

## Prerequisites

- Timeplus Proton running and accessible (default: `localhost:3218` for SQL, `localhost:8463` for native)
- OpenClaw gateway running via Docker (`make start`)

---

## Step 1: Create Target Streams in Timeplus

Connect to your Timeplus instance and run the following SQL statements to create the required streams.

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

The `diagnostics-otel` plugin uses **OTLP/HTTP (Protobuf)** only — gRPC is not supported. Use port **4318**.

```sql
CREATE INPUT openclaw_otel_input
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
COMMENT 'OpenClaw agent telemetry via OTLP/HTTP';
```

> The plugin sends OTLP/HTTP Protobuf to `endpoint/v1/traces` and `endpoint/v1/metrics`. gRPC is not supported by the plugin.

---

## Step 3: Configure OpenClaw

Edit `.openclaw/openclaw.json` and add the `diagnostics` block and the `diagnostics-otel` plugin entry:

```json
{
  "diagnostics": {
    "enabled": true,
    "otel": {
      "enabled": true,
      "endpoint": "http://host.docker.internal:4318",
      "traces": true,
      "metrics": true,
      "logs": false,
      "serviceName": "openclaw",
      "sampleRate": 1
    }
  },
  "plugins": {
    "entries": {
      "diagnostics-otel": {
        "enabled": true
      },
      "ollama": {
        "enabled": true
      }
    }
  }
}
```

**Key settings:**

| Field | Value | Notes |
|---|---|---|
| `endpoint` | `http://host.docker.internal:4317` | `host.docker.internal` resolves to the Mac host from inside Docker. Use your machine's LAN IP if connecting across machines. |
| `traces` | `true` | LLM spans, tool calls, agent runs |
| `metrics` | `true` | Token counts, latency, cost |
| `logs` | `false` | Enable if you also created `otel_logs` stream |
| `sampleRate` | `1` | `1.0` = 100% sampling. Lower for high-volume production use. |

---

## Step 4: Restart the Gateway

```bash
make start
```

The gateway picks up config changes on restart. You can confirm the plugin loaded by checking the container logs:

```bash
docker logs openclaw 2>&1 | grep -i otel
```

---

## Step 5: Verify Data is Flowing

After sending a message to your OpenClaw agent, query the streams in Timeplus.

> Use `table(stream)` for historical point-in-time queries. Use the bare stream name for live streaming queries.

```sql
-- Recent traces (historical)
SELECT
  trace_id,
  span_name,
  service_name,
  status_code,
  duration / 1e6 AS duration_ms,
  span_attributes['gen_ai.system'] AS model_provider,
  span_attributes['gen_ai.request.model'] AS model,
  span_attributes['gen_ai.usage.input_tokens'] AS input_tokens,
  span_attributes['gen_ai.usage.output_tokens'] AS output_tokens
FROM table(otel_traces)
ORDER BY start_time_ns DESC
LIMIT 20;
```

```sql
-- Token usage per agent session (last 1 hour, historical)
SELECT
  service_name,
  span_attributes['gen_ai.request.model'] AS model,
  count()                                  AS llm_calls,
  sum(to_int64(span_attributes['gen_ai.usage.input_tokens']))  AS total_input_tokens,
  sum(to_int64(span_attributes['gen_ai.usage.output_tokens'])) AS total_output_tokens,
  avg(duration / 1e6)                      AS avg_latency_ms
FROM table(otel_traces)
WHERE span_name LIKE '%chat%'
  AND _tp_time > now() - INTERVAL 1 HOUR
GROUP BY service_name, model
ORDER BY total_input_tokens DESC;
```

```sql
-- Tool call success/failure breakdown (historical)
SELECT
  span_attributes['gen_ai.tool.name'] AS tool,
  status_code,
  count() AS calls,
  avg(duration / 1e6) AS avg_ms
FROM table(otel_traces)
WHERE span_name = 'tool.execution'
GROUP BY tool, status_code
ORDER BY calls DESC;
```

```sql
-- Live streaming tail (no table() — continuous query)
SELECT trace_id, span_name, service_name, status_code, duration / 1e6 AS duration_ms
FROM otel_traces;
```

---

## Troubleshooting

**No data in streams after restarting gateway**

- Confirm Timeplus is listening on port 4317: `lsof -i :4317`
- Check container can reach the host: `docker exec openclaw curl -s http://host.docker.internal:4317` (expect a gRPC response, not a connection refused)
- Verify the plugin loaded: `docker logs openclaw 2>&1 | grep -i diagnostics`

**`host.docker.internal` not resolving**

On Linux, add `--add-host=host.docker.internal:host-gateway` to the `docker run` command in the `Makefile`'s `start` target. On macOS and Windows Docker Desktop, it resolves automatically.

**Missing fields in traces**

The `diagnostics-otel` plugin follows the [OpenTelemetry GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/). Fields like `gen_ai.usage.input_tokens` only appear on LLM spans, not tool spans. Filter by `span_name` to target the right span type.

---

## References

- [Timeplus OTel Input docs](https://docs.timeplus.com/otel-input)
- [LangWatch: Instrumenting OpenClaw with OpenTelemetry](https://langwatch.ai/blog/instrumenting-your-openclaw-agent-with-opentelemetry)
- [OpenTelemetry GenAI Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/)
