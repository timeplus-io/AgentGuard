# AgentShield — Technical Design

AgentShield is a real-time security monitoring and governance platform for AI coding agents. It ingests telemetry from agents via OpenTelemetry, applies streaming SQL rules to detect threats, and exposes results through a live security dashboard.

---

## Table of Contents

1. [Overall Architecture](#1-overall-architecture)
2. [Data Collection](#2-data-collection)
3. [Agents](#3-agents)
4. [Threats and Rules](#4-threats-and-rules)
5. [Rule Catalog](#5-rule-catalog)
6. [Cost Governance](#6-cost-governance)

---

## 1. Overall Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Agent (OpenClaw / Claude Code)                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Hook events → agentshield plugin → OTLP/HTTP :4318      │   │
│  └──────────────────────────────────────────────────────────┘   │
└──────────────────────────────┬──────────────────────────────────┘
                               │ OpenTelemetry / custom HTTP
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│  Timeplus (streaming SQL engine)                                  │
│                                                                   │
│  Inputs         Streams                     MVs / Alerts          │
│  ──────────     ────────────────────────    ──────────────────── │
│  openclaw_      agentshield_hook_events  →  mv_rule_rp001        │
│  otel_input     agentshield_security_    →  mv_rule_rp002   ┐    │
│                 events                      mv_rule_rp003   │    │
│  otel_logs      agentshield_threats      ←  mv_threats      │    │
│  otel_traces    agentshield_notify_      ←  mv_notify_filter│    │
│  otel_metrics_* events                                       │    │
│                 agentshield_rules           agentshield_     │    │
│                 agentshield_pricing         notifier (alert) ┘    │
└──────────────────────────────┬──────────────────────────────────┘
                               │ HTTP / native TCP
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│  AgentShield Backend  (Go / Gin, :8080)                          │
│                                                                   │
│  /api/agents        /api/threats     /api/rules                  │
│  /api/metrics       /api/cost        /api/notifications/stream   │
│  /api/proton/query  /api/investigate /api/onboarding             │
└──────────────────────────────┬──────────────────────────────────┘
                               │ REST + SSE
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│  React SPA  (:5173 dev / embedded in binary for prod)            │
└─────────────────────────────────────────────────────────────────┘
```

### Technology Stack

| Layer | Technology |
|-------|-----------|
| Streaming DB | Timeplus (streaming SQL, materialized views, mutable streams, alerts) |
| Backend | Go 1.23, Gin, Cobra, Viper |
| Frontend | React 18, Vite, React Router v6, Tailwind CSS |
| Agent telemetry | OpenTelemetry OTLP/HTTP (logs, traces, metrics) |
| Hook events | Custom HTTP POST to `/api/alerts/webhook` + SSE fan-out |

### Two Timeplus Clients

The backend uses two separate connections to Timeplus:

| Client | Port | Protocol | Used for |
|--------|------|----------|---------|
| `proton-go-driver` | 8463 | Native TCP | Startup `Init()` only — stream/MV/UDF creation |
| HTTP client | 3218 | HTTP | All runtime queries (`Query()`) and DDL (`ExecHTTP()`) |

### Startup Initialization Sequence

`timeplus.Init()` runs at server start (non-fatal — server starts even if Timeplus is unavailable):

1. Wait for Timeplus native port 8463 to respond (polls every 5 s indefinitely)
2. Create 5 OTel streams (`otel_traces`, `otel_logs`, `otel_metrics_gauge/sum/histogram`)
3. Create `openclaw_otel_input` (OTLP/HTTP input on port 4318)
4. Create `agentshield_hook_events`
5. Create `agentshield_security_events`, `agentshield_notify_events`, `agentshield_rules`
6. Create `agentshield_threats` (mutable stream)
7. Create `mv_threats`
8. Create `agentshield_pricing` (mutable stream)
9. Seed 6 default pricing rows (skipped if any rows exist)
10. Seed catalog rules as disabled (skipped if any rows exist)
11. Drop 4 legacy named security MVs
12. `CREATE OR REPLACE` the `notify_agentshield_udf` Python UDF
13. Create `mv_notify_filter`
14. Drop and recreate `agentshield_notifier` alert
15. Sync per-rule MVs to match current `agentshield_rules` state

---

## 2. Data Collection

AgentShield receives two types of telemetry from agents:

### 2.1 OTel Telemetry (via OTLP/HTTP Input)

A Timeplus **Input** listens on port 4318 and routes incoming OTLP data into five streams.

**Input definition:**
```sql
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
COMMENT 'OpenClaw agent telemetry via OTLP/HTTP'
```

#### `otel_traces`
```sql
CREATE STREAM otel_traces (
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
  links_attributes    array(map(low_cardinality(string), string)) CODEC(ZSTD(1))
)
```

#### `otel_logs`
```sql
CREATE STREAM otel_logs (
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
  log_attributes      map(low_cardinality(string), string) CODEC(ZSTD(1))
)
```

#### `otel_metrics_gauge` and `otel_metrics_sum`

Gauge and sum share the same base columns. Sum adds `aggregation_temporality int32` and `is_monotonic bool`.

```sql
CREATE STREAM otel_metrics_gauge (  -- otel_metrics_sum adds 2 extra columns
  resource_attributes          map(low_cardinality(string), string) CODEC(ZSTD(1)),
  resource_schema_url          string                               CODEC(ZSTD(1)),
  scope_name                   string                               CODEC(ZSTD(1)),
  scope_version                string                               CODEC(ZSTD(1)),
  scope_attributes             map(low_cardinality(string), string) CODEC(ZSTD(1)),
  scope_dropped_attr_count     uint32                               CODEC(ZSTD(1)),
  scope_schema_url             string                               CODEC(ZSTD(1)),
  service_name                 low_cardinality(string)              CODEC(ZSTD(1)),
  metric_name                  low_cardinality(string)              CODEC(ZSTD(1)),
  metric_description           string                               CODEC(ZSTD(1)),
  metric_unit                  string                               CODEC(ZSTD(1)),
  attributes                   map(low_cardinality(string), string) CODEC(ZSTD(1)),
  start_time_unix              datetime64(9)                        CODEC(ZSTD(1)),
  time_unix                    datetime64(9)                        CODEC(ZSTD(1)),
  value                        float64                              CODEC(ZSTD(1)),
  flags                        uint32                               CODEC(ZSTD(1)),
  exemplar_filter_type         array(string)                        CODEC(ZSTD(1)),
  exemplar_attributes          array(map(low_cardinality(string), string)) CODEC(ZSTD(1)),
  exemplar_time_unix           array(datetime64(9))                 CODEC(ZSTD(1)),
  exemplar_value               array(float64)                       CODEC(ZSTD(1)),
  exemplar_span_id             array(string)                        CODEC(ZSTD(1)),
  exemplar_trace_id            array(string)                        CODEC(ZSTD(1))
)
```

#### `otel_metrics_histogram`

Extends the sum schema (minus `is_monotonic`) with histogram-specific columns: `count uint64`, `sum float64`, `bucket_counts array(uint64)`, `explicit_bounds array(float64)`, `min float64`, `max float64`.

### 2.2 Hook Events (Primary Security Stream)

Agent hook events are written directly by the AgentShield plugin into `agentshield_hook_events`. This is the primary stream consumed by all security rules.

```sql
CREATE STREAM agentshield_hook_events (
  hook_name        low_cardinality(string),  -- e.g. 'llm_input', 'before_tool_call'
  event_time       datetime64(3),
  session_id       string,
  run_id           string,
  agent_id         string,
  deployment_id    low_cardinality(string),
  deployment_name  low_cardinality(string),
  session_key      string,
  tool_name        string,
  provider         string,                   -- e.g. 'anthropic', 'openai'
  model            string,                   -- e.g. 'claude-sonnet-4-6'
  hook_decision    low_cardinality(string),  -- 'allow' | 'block'
  block_reason     string,
  event_data       string                    -- JSON blob; schema varies by hook
)
```

**Key hook names and their `event_data` payloads:**

| `hook_name` | Payload fields of interest |
|-------------|---------------------------|
| `llm_input` | `prompt`, `system` |
| `llm_output` | `content`, `usage.input`, `usage.output`, `stop_reason` |
| `before_tool_call` | `tool_name`, `tool_input` |
| `after_tool_call` | `tool_name`, `tool_output`, `duration_ms` |
| `tool_result_persist` | `tool_name`, `content` |
| `before_install` | `package_name`, `registry` |
| `session_start` | `session_key` |
| `session_end` | (no additional fields) |

### 2.3 API — Data Collection

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/alerts/webhook` | Receives `AlertWebhookPayload` from the Timeplus alert UDF and fans it out to all SSE clients |
| GET | `/api/notifications/stream` | SSE long-poll stream; sends `event: alert\ndata: {...}` on each detection; 20 s keepalive |
| POST | `/api/onboarding/generate` | Returns plugin config snippet for `openclaw` or `claude_code` agent types |
| POST | `/api/proton/query` | Streams arbitrary SQL directly to Timeplus; returns NDJSON rows line-by-line |

---

## 3. Agents

### 3.1 Data Model

Agent identity is derived from `(deployment_id, agent_id)` pairs in `agentshield_hook_events`. There is no separate agents table — all agent data is computed on-the-fly from the hook event stream.

**Key computed fields per agent:**

| Field | Source query |
|-------|-------------|
| `last_seen` | `max(event_time)` |
| `event_count` | `count()` |
| `session_count` | `count(distinct session_id)` |
| `run_count` | `count(distinct run_id)` |
| `tool_call_count` | `count_if(hook_name = 'after_tool_call')` |
| `total_tokens` | Sum of `json_extract_string(event_data, 'usage', 'input') + output` |
| `llm_call_count` | `count_if(hook_name = 'llm_output')` |

### 3.2 Queries

**List all agents:**
```sql
SELECT
  agent_id,
  deployment_id,
  arg_max(deployment_name, event_time) AS deployment_name,
  max(event_time)                      AS last_seen,
  count()                              AS event_count,
  count(distinct session_id)           AS session_count,
  count(distinct run_id)               AS run_count,
  count_if(hook_name = 'after_tool_call') AS tool_call_count,
  sum(to_int64_or_zero(json_extract_string(event_data, 'usage', 'input'))
    + to_int64_or_zero(json_extract_string(event_data, 'usage', 'output'))) AS total_tokens,
  count_if(hook_name = 'llm_output')   AS llm_call_count
FROM table(agentshield_hook_events)
GROUP BY agent_id, deployment_id
ORDER BY last_seen DESC
```

**List sessions for an agent** (excludes temporary and slug-generator sessions):
```sql
SELECT
  session_id,
  arg_max(session_key, event_time)    AS session_key,
  min(event_time)                     AS first_seen,
  max(event_time)                     AS last_seen,
  count()                             AS event_count,
  count(distinct run_id)              AS run_count
FROM table(agentshield_hook_events)
WHERE agent_id = '<id>'
  AND deployment_id = '<id>'
  AND session_id NOT LIKE 'temp:%'
  AND session_id NOT LIKE 'slug-generator-%'
GROUP BY session_id
HAVING count_if(run_id != '') > 0
ORDER BY last_seen DESC
```

**Run waterfall trace** (builds a pseudo-trace from hook events):
```sql
SELECT hook_name, event_time, run_id, session_id,
       tool_name, provider, model, hook_decision, block_reason, event_data
FROM table(agentshield_hook_events)
WHERE session_id = '<id>' AND run_id = '<id>'
ORDER BY event_time ASC
```
Each row becomes a span; duration is computed as the gap to the next event. Spans are typed (`run`, `llm`, `tool`, `lifecycle`) based on `hook_name`.

**LLM metrics — tokens and calls:**
```sql
SELECT
  count()                                          AS total_calls,
  count_if(json_extract_string(event_data,'stop_reason') != 'error') AS success,
  sum(to_int64_or_zero(json_extract_string(event_data,'usage','input')))  AS input_tokens,
  sum(to_int64_or_zero(json_extract_string(event_data,'usage','output'))) AS output_tokens,
  sum(to_int64_or_zero(json_extract_string(event_data,'usage','input'))
    + to_int64_or_zero(json_extract_string(event_data,'usage','output'))) AS total_tokens
FROM table(agentshield_hook_events)
WHERE hook_name = 'llm_output'
```

**LLM latency** (self-join `llm_input` ↔ `llm_output` on `run_id`):
```sql
SELECT
  quantile(0.50)(date_diff('millisecond', i.event_time, o.event_time)) AS p50,
  quantile(0.95)(date_diff('millisecond', i.event_time, o.event_time)) AS p95,
  quantile(0.99)(date_diff('millisecond', i.event_time, o.event_time)) AS p99,
  avg(date_diff('millisecond', i.event_time, o.event_time))            AS avg,
  min(date_diff('millisecond', i.event_time, o.event_time))            AS min,
  max(date_diff('millisecond', i.event_time, o.event_time))            AS max,
  count()                                                               AS samples
FROM table(agentshield_hook_events) AS i
JOIN table(agentshield_hook_events) AS o ON i.run_id = o.run_id
WHERE i.hook_name = 'llm_input' AND o.hook_name = 'llm_output'
```

**Tool metrics — calls and latency:**
```sql
-- Call counts by tool
SELECT
  tool_name,
  count()                                        AS total_calls,
  count_if(hook_decision != 'block')             AS success_calls,
  count_if(hook_decision  = 'block')             AS failed_calls
FROM table(agentshield_hook_events)
WHERE hook_name = 'tool_result_persist'
GROUP BY tool_name

-- Latency by tool (from after_tool_call event_data)
SELECT
  tool_name,
  quantile(0.50)(to_float64(json_extract_string(event_data, 'duration_ms'))) AS p50,
  quantile(0.95)(to_float64(json_extract_string(event_data, 'duration_ms'))) AS p95
FROM table(agentshield_hook_events)
WHERE hook_name = 'after_tool_call'
GROUP BY tool_name
```

### 3.3 API — Agents

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/agents` | List all agents with metrics; `?page=&limit=` |
| GET | `/api/agents/:id` | Single agent detail; `:id` = `deploymentId__agentId`; `?since=&until=` RFC3339 filters |
| GET | `/api/agents/:id/sessions` | Sessions for an agent (excludes ephemeral sessions) |
| GET | `/api/agents/:id/sessions/:session_id` | Session detail with ordered run list |
| GET | `/api/agents/:id/sessions/:session_id/runs/:run_id/trace` | Hook event waterfall as trace spans |
| GET | `/api/agents/:id/events` | Raw hook events; `?page=&limit=&session_id=&run_id=` |
| GET | `/api/metrics/llm` | LLM token/call/latency/throughput metrics; `?agent_id=&model=&since=&until=` |
| GET | `/api/metrics/tools` | Tool call/latency breakdown by tool name |

---

## 4. Threats and Rules

### 4.1 Streams

#### `agentshield_security_events` — Detection sink

Written to by each per-rule materialized view. Every row is one rule match.

```sql
CREATE STREAM agentshield_security_events (
  detected_at   datetime64(3),
  rule_id       low_cardinality(string),
  rule_name     low_cardinality(string),
  severity      low_cardinality(string),  -- 'critical' | 'warning' | 'info'
  agent_id      string,
  deployment_id low_cardinality(string),
  session_id    string,
  run_id        string,
  hook_name     low_cardinality(string),
  tool_name     string,
  message       string,
  event_data    string
)
```

#### `agentshield_rules` — Append-only rule log

Every rule mutation (install, toggle, upgrade, delete) appends a new row. Current state is resolved via `arg_max`.

```sql
CREATE STREAM agentshield_rules (
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
)
```

**Current rule state query:**
```sql
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  arg_max(enabled,            updated_at) AS enabled,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources
FROM table(agentshield_rules)
GROUP BY rule_id
HAVING arg_max(deleted, updated_at) = false
```

#### `agentshield_threats` — Mutable threat state

A mutable stream deduplicated by `(agent_id, deployment_id, session_id, rule_id)`. Uses column families to separate detection data from status data, allowing independent upserts.

```sql
CREATE MUTABLE STREAM agentshield_threats (
  agent_id          string,
  deployment_id     string,
  session_id        string,
  rule_id           string,
  rule_name         string,
  severity          string,
  last_seen         datetime64(3),
  status            string,            -- 'open' | 'acknowledged' | 'cleared'
  status_updated_at datetime64(3),
  FAMILY cf_events(rule_name, severity, last_seen),
  FAMILY cf_status(status, status_updated_at)
) PRIMARY KEY (agent_id, deployment_id, session_id, rule_id)
SETTINGS coalesced = true
```

#### `agentshield_notify_events` — Alert filter stream

Receives only threats that are new or have re-opened after being cleared, avoiding alert storms for already-known threats.

```sql
CREATE STREAM agentshield_notify_events (
  -- same schema as agentshield_security_events
)
```

### 4.2 Materialized Views

#### `mv_threats` — Threat deduplication

Continuously upserts into `agentshield_threats` as security events arrive. Sets `status = ''` (empty) so `mv_notify_filter` can distinguish new from already-triaged threats.

```sql
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
SETTINGS seek_to = 'earliest'
```

#### `mv_notify_filter` — Noise suppression

Passes only detections where the threat has no status or was previously cleared (i.e., newly opened or re-opened threats).

```sql
CREATE MATERIALIZED VIEW mv_notify_filter
INTO agentshield_notify_events
AS SELECT
  se.detected_at, se.rule_id, se.rule_name, se.severity,
  se.agent_id, se.deployment_id, se.session_id, se.run_id,
  se.hook_name, se.tool_name, se.message, se.event_data
FROM agentshield_security_events AS se
LEFT JOIN agentshield_threats AS t
  ON  se.agent_id      = t.agent_id
  AND se.deployment_id = t.deployment_id
  AND se.session_id    = t.session_id
  AND se.rule_id       = t.rule_id
WHERE t.status = '' OR t.status = 'cleared'
```

#### Per-rule MVs — Detection pipeline

Each enabled rule has a MV named `mv_rule_<ruleID_no_hyphens>` (e.g., `rp-001` → `mv_rule_rp001`). The MV query reads `agentshield_hook_events` with user-defined logic and writes to `agentshield_security_events`.

**Create:**
```sql
CREATE MATERIALIZED VIEW mv_rule_rp001
INTO agentshield_security_events
AS <rule query_sql>
```

**Pause** (survives Timeplus restarts):
```sql
SYSTEM PAUSE MATERIALIZED VIEW mv_rule_rp001 PERMANENT
```

**Resume:**
```sql
SYSTEM RESUME MATERIALIZED VIEW mv_rule_rp001 PERMANENT
```

**Drop:**
```sql
DROP VIEW IF EXISTS mv_rule_rp001
```

**MV health check:**
```sql
SELECT name, engine
FROM system.tables
WHERE name LIKE 'mv_rule_%'
```

### 4.3 Alert Pipeline

#### UDF — `notify_agentshield_udf`

A Python UDF that POSTs each detection to the AgentShield backend webhook endpoint. The backend then fans the event to all SSE-connected browser clients.

```sql
CREATE OR REPLACE FUNCTION notify_agentshield_udf(
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
                "rule_id":     rule_ids[i],
                "rule_name":   rule_names[i],
                "severity":    severities[i],
                "agent_id":    agent_ids[i],
                "message":     messages[i],
                "detected_at": detected_ats[i]
            }, timeout=3)
        except Exception:
            pass
    return rule_ids
$$
```

#### Alert — `agentshield_notifier`

Batches up to 1 event at a time with a 5 s timeout, rate-limited to 10 alerts per minute.

```sql
CREATE ALERT agentshield_notifier
  BATCH 1 EVENTS WITH TIMEOUT 5s
  LIMIT 10 ALERTS PER 1m
  CALL notify_agentshield_udf
AS SELECT
  rule_id, rule_name, severity, agent_id, deployment_id,
  session_id, hook_name, tool_name, message, detected_at
FROM agentshield_notify_events
```

### 4.4 Threat Identity and Status

**ThreatID** is computed in Go as an MD5 hash of `agentID + deploymentID + sessionID + ruleID` using length-prefix encoding. It is never stored in Timeplus — it is a stable key for the API.

**Status lifecycle:**
- `""` (empty) — detected by MV, not yet triaged
- `"open"` — actively firing or re-opened
- `"acknowledged"` — operator has seen it
- `"cleared"` — operator has resolved it

**Re-open logic:** If a threat's `last_seen > status_updated_at` and its status is `"cleared"`, the API re-opens it to `"open"` and fires an async upsert to `agentshield_threats`.

**Status upsert:**
```sql
-- Acknowledge:
INSERT INTO agentshield_threats
  (agent_id, deployment_id, session_id, rule_id, status, status_updated_at)
VALUES ('<id>', '<id>', '<id>', '<id>', 'acknowledged', now64(3))

-- Clear:
INSERT INTO agentshield_threats
  (agent_id, deployment_id, session_id, rule_id, status, status_updated_at)
VALUES ('<id>', '<id>', '<id>', '<id>', 'cleared', now64(3))
```

### 4.5 API — Threats and Rules

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/threats` | List all threats; `?severity=&category=`; merges threats table + stats + events |
| GET | `/api/threats/:id` | Single threat by ThreatID (MD5 of 4 key fields) |
| POST | `/api/threats/:id/acknowledge` | Upsert `status='acknowledged'` into `agentshield_threats` |
| POST | `/api/threats/:id/clear` | Upsert `status='cleared'` into `agentshield_threats` |
| GET | `/api/rules` | List installed rules with 24 h detection counts |
| GET | `/api/rules/:id` | Rule detail with 50 most recent detections |
| POST | `/api/rules/:id/install` | Install from catalog: insert to `agentshield_rules`, create MV |
| POST | `/api/rules/:id/toggle` | Flip `enabled`; pause or resume MV accordingly |
| POST | `/api/rules/:id/upgrade` | Insert new version row; drop and recreate MV with updated query |
| DELETE | `/api/rules/:id` | Soft-delete: insert `deleted=true` row, drop MV |
| POST | `/api/rules` | Create a custom rule: validate, substitute template placeholders, save to catalog, insert, create MV |
| GET | `/api/rules/mv-status` | MV health: cross-reference `system.tables` with installed rules |
| POST | `/api/rules/sync` | Reconcile MV state without server restart |

---

## 5. Rule Catalog

The catalog is a two-layer, version-controlled store of rule definitions.

### 5.1 Rule Entry Schema

```go
type Entry struct {
    ID                string   `yaml:"id"`                 // e.g. "rp-001"
    Name              string   `yaml:"name"`
    Version           string   `yaml:"version"`            // semver "X.Y.Z"
    Description       string   `yaml:"description"`
    Severity          string   `yaml:"severity"`           // "critical" | "warning" | "info"
    Category          string   `yaml:"category"`           // e.g. "prompt_injection"
    FPRate            string   `yaml:"fp_rate"`            // "low" | "medium" | "high"
    HookSources       string   `yaml:"hook_sources"`       // comma-separated hook names
    RequiredTelemetry string   `yaml:"required_telemetry"`
    Author            string   `yaml:"author"`
    Tags              []string `yaml:"tags"`
    QuerySQL          string   `yaml:"query_sql"`          // SELECT into agentshield_security_events
}
```

### 5.2 Catalog Layers

The `Manager` merges two layers (local overrides seed by ID):

| Layer | Source | Mutability |
|-------|--------|-----------|
| **Seed** | Go-embedded YAML files (`backend/internal/catalog/seed/*.yaml`) | Read-only |
| **Local** | Disk files in `--catalog-dir` (default `./catalog`) | Read-write via `Save()` |

### 5.3 Built-in Rules

| ID | Name | Severity | Category | Hook Sources |
|----|------|----------|----------|-------------|
| `rp-001` | Prompt Injection Shield | critical | prompt_injection | `llm_input` |
| `rp-002` | DLP Sentinel | critical | dlp | `tool_result_persist`, `after_tool_call`, `llm_output` |
| `rp-003` | Privilege Guard | warning | privilege | `before_tool_call` |
| `rp-004` | Supply Chain Watch | warning | supply_chain | `before_install`, `before_tool_call` |

**`rp-001` — Prompt Injection Shield:**
```sql
SELECT
  now64(3) AS detected_at,
  'rp-001' AS rule_id, 'Prompt Injection Shield' AS rule_name, 'critical' AS severity,
  agent_id, deployment_id, session_id, run_id, hook_name, tool_name,
  concat('Prompt injection pattern detected in input from ', agent_id) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name = 'llm_input'
  AND (lower(event_data) LIKE '%ignore previous instructions%'
    OR lower(event_data) LIKE '%ignore all previous%'
    OR lower(event_data) LIKE '%jailbreak%'
    OR lower(event_data) LIKE '%disregard your%'
    OR lower(event_data) LIKE '%you are now a%'
    OR lower(event_data) LIKE '%forget everything%'
    OR lower(event_data) LIKE '%new instructions:%')
```

**`rp-002` — DLP Sentinel:**
```sql
SELECT
  now64(3) AS detected_at,
  'rp-002' AS rule_id, 'DLP Sentinel' AS rule_name, 'critical' AS severity,
  agent_id, deployment_id, session_id, run_id, hook_name, tool_name,
  concat('Sensitive data pattern detected in output from ', agent_id) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name IN ('tool_result_persist', 'after_tool_call', 'llm_output')
  AND (match(event_data, '[0-9]{3}-[0-9]{2}-[0-9]{4}')  -- SSN
    OR lower(event_data) LIKE '%api_key%'
    OR lower(event_data) LIKE '%secret_key%'
    OR lower(event_data) LIKE '%bearer %'
    OR lower(event_data) LIKE '%sk-ant-%'
    OR lower(event_data) LIKE '%ghp_%'
    OR lower(event_data) LIKE '%private_key%'
    OR lower(event_data) LIKE '%aws_secret%')
```

**`rp-003` — Privilege Guard:**
```sql
SELECT
  now64(3) AS detected_at,
  'rp-003' AS rule_id, 'Privilege Guard' AS rule_name, 'warning' AS severity,
  agent_id, deployment_id, session_id, run_id, hook_name, tool_name,
  concat('Privileged tool or command usage by ', agent_id) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name = 'before_tool_call'
  AND (tool_name IN ('bash', 'shell', 'exec', 'run_terminal_cmd', 'computer')
    OR lower(event_data) LIKE '%sudo %'
    OR lower(event_data) LIKE '%rm -rf%'
    OR lower(event_data) LIKE '%chmod %'
    OR lower(event_data) LIKE '%/etc/passwd%'
    OR lower(event_data) LIKE '%/etc/shadow%')
```

**`rp-004` — Supply Chain Watch:**
```sql
SELECT
  now64(3) AS detected_at,
  'rp-004' AS rule_id, 'Supply Chain Watch' AS rule_name, 'warning' AS severity,
  agent_id, deployment_id, session_id, run_id, hook_name, tool_name,
  concat('Package installation attempt detected from ', agent_id) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name = 'before_install'
  OR (hook_name = 'before_tool_call'
    AND (lower(event_data) LIKE '%npm install%'
      OR lower(event_data) LIKE '%pip install%'
      OR lower(event_data) LIKE '%yarn add%'
      OR lower(event_data) LIKE '%cargo install%'
      OR lower(event_data) LIKE '%apt-get install%'
      OR lower(event_data) LIKE '%brew install%'))
```

### 5.4 Version Comparison

Import and upgrade operations compare semver strings part-by-part:

```go
// versionGreater("1.2.0", "1.1.9") == true
func versionGreater(a, b string) bool {
    // splits X.Y.Z, compares integer parts left-to-right
}
```

### 5.5 Import / Export

- **Export:** `GET /api/rules/catalog/export` streams a ZIP of all merged entries as `<id>.yaml` files
- **Import:** `POST /api/rules/catalog/import` accepts a multipart form ZIP; merges rules by version comparison; `?force=true` overwrites regardless of version

### 5.6 API — Catalog

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/rules/catalog` | All catalog entries with installed/enabled/upgradeable status |
| GET | `/api/rules/catalog/export` | Download ZIP of all catalog entries |
| POST | `/api/rules/catalog/import` | Upload ZIP to merge rules into local catalog; `?force=true` to overwrite |

---

## 6. Cost Governance

### 6.1 Streams

#### `agentshield_pricing` — Provider pricing table

A mutable stream used as a key-value store. Upserts by `(provider, model)` primary key.

```sql
CREATE MUTABLE STREAM agentshield_pricing (
  provider                    low_cardinality(string),
  model                       low_cardinality(string),
  input_cost_per_million      float64,
  output_cost_per_million     float64,
  cache_read_cost_per_million float64,
  updated_at                  datetime64(3)
) PRIMARY KEY (provider, model)
```

**Default pricing (seeded at startup):**

| Provider | Model | Input $/M | Output $/M | Cache Read $/M |
|----------|-------|-----------|------------|----------------|
| anthropic | claude-sonnet-4-6 | 3.00 | 15.00 | 0.30 |
| anthropic | claude-opus-4-6 | 15.00 | 75.00 | 1.50 |
| anthropic | claude-haiku-4-5 | 0.80 | 4.00 | 0.08 |
| openai | gpt-4o | 2.50 | 10.00 | 1.25 |
| openai | gpt-4o-mini | 0.15 | 0.60 | 0.075 |
| openai | o3 | 10.00 | 40.00 | 2.50 |

### 6.2 Token Extraction from `event_data`

All cost queries extract token counts from the JSON `event_data` field of `llm_output` hook events:

```sql
to_int64_or_zero(json_extract_string(event_data, 'usage', 'input'))            AS input_tokens
to_int64_or_zero(json_extract_string(event_data, 'usage', 'output'))           AS output_tokens
to_int64_or_zero(json_extract_string(event_data, 'lastAssistant', 'usage', 'cacheRead')) AS cache_tokens
```

### 6.3 Cost Formula

```sql
(  input_tokens  * input_cost_per_million
 + output_tokens * output_cost_per_million
 + cache_tokens  * cache_read_cost_per_million
) / 1000000.0  AS cost_usd
```

### 6.4 Queries

**Cost by agent and model** (all-time or since a given time):
```sql
SELECT
  e.agent_id,
  e.deployment_id,
  e.provider,
  e.model,
  count()  AS requests,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'input')))  AS input_tokens,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'output'))) AS output_tokens,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead'))) AS cache_tokens,
  sum((  to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'input'))  * p.input_cost_per_million
       + to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'output')) * p.output_cost_per_million
       + to_int64_or_zero(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead'))
         * p.cache_read_cost_per_million
      ) / 1000000.0)  AS cost_usd
FROM table(agentshield_hook_events) AS e
LEFT JOIN table(agentshield_pricing) AS p
  ON e.provider = p.provider AND e.model = p.model
WHERE e.hook_name = 'llm_output'
GROUP BY e.agent_id, e.deployment_id, e.provider, e.model
ORDER BY cost_usd DESC
```

**Daily spend trend (last 30 days):**
```sql
SELECT
  to_start_of_day(e.event_time) AS day,
  e.provider,
  e.model,
  sum((  to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'input'))  * p.input_cost_per_million
       + to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'output')) * p.output_cost_per_million
       + to_int64_or_zero(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead'))
         * p.cache_read_cost_per_million
      ) / 1000000.0)  AS cost_usd
FROM table(agentshield_hook_events) AS e
LEFT JOIN table(agentshield_pricing) AS p
  ON e.provider = p.provider AND e.model = p.model
WHERE e.hook_name = 'llm_output'
  AND e.event_time >= now() - interval 30 day
GROUP BY day, e.provider, e.model
ORDER BY day ASC
```

**Dashboard 24 h cost:**
```sql
SELECT
  sum((  to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'input'))  * p.input_cost_per_million
       + to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'output')) * p.output_cost_per_million
       + to_int64_or_zero(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead'))
         * p.cache_read_cost_per_million
      ) / 1000000.0)  AS total_cost
FROM table(agentshield_hook_events) AS e
LEFT JOIN table(agentshield_pricing) AS p
  ON e.provider = p.provider AND e.model = p.model
WHERE e.hook_name = 'llm_output'
  AND e.event_time >= now() - interval 1 day
```

**MTD forecasting** (computed in Go, not SQL):
```
forecasted = MTD / elapsed_days * 30
```

**Cache savings estimate** (computed in Go):
```
cache_savings = cache_amount * 9
-- Cache tokens cost ~10% of full input price, so 9× is the saving
```

### 6.5 Pricing Management Queries

**Upsert pricing:**
```sql
INSERT INTO agentshield_pricing
  (provider, model, input_cost_per_million, output_cost_per_million, cache_read_cost_per_million, updated_at)
VALUES ('<provider>', '<model>', <input>, <output>, <cache>, now64(3))
-- Mutable stream semantics: upserts by PRIMARY KEY (provider, model)
```

**Delete pricing:**
```sql
DELETE FROM agentshield_pricing
WHERE provider = '<provider>' AND model = '<model>'
```

### 6.6 API — Cost

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/cost` | Full cost report: MTD, forecast, daily trend (30 d), token breakdown, per-agent/model costs, cache savings |
| GET | `/api/cost/pricing` | List all pricing entries from `agentshield_pricing` |
| POST | `/api/cost/pricing` | Upsert a pricing entry (insert into mutable stream) |
| DELETE | `/api/cost/pricing/:provider/:model` | Delete a pricing entry by primary key |

---

## Appendix — Investigate and Dashboard APIs

### Investigate

The Investigate page exposes raw Timeplus query access.

**Priority streams** always listed first:

| Stream | Description |
|--------|-------------|
| `agentshield_hook_events` | Raw agent hook events |
| `agentshield_security_events` | Security rule detections |
| `agentshield_threats` | Deduplicated threat state |

OTel streams (`otel_*`) are discovered dynamically from `system.tables`.

**Column schema query:**
```sql
SELECT name, type FROM system.columns
WHERE table = '<stream_name>' AND database = current_database()
```

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/investigate/tables` | Priority streams + all `otel_*` streams with column schemas |
| POST | `/api/proton/query` | Proxy arbitrary SQL to Timeplus; streams NDJSON rows |

### Dashboard

The dashboard aggregates data from multiple streams in parallel:

| Metric | Query |
|--------|-------|
| Total agents | `count(distinct concat(deployment_id, agent_id))` from `agentshield_hook_events` |
| Active threats (24h) | `count()` from `agentshield_threats` where `last_seen >= now() - 1d` |
| Critical escalations | Count where `severity = 'critical'` from above |
| Total cost (24h) | JOIN query on `hook_events` + `pricing` last 24 h |
| Active rules | `count()` from `agentshield_rules` current state (enabled, not deleted) |
| Threat categories | Group active threats by rule `category`, compute percentages |

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/dashboard` | All KPIs, threat categories in one response |
