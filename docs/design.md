# AgentGuard — Technical Design

AgentGuard is a real-time security monitoring and governance platform for AI coding agents. It ingests telemetry from agents via two paths — hook events posted directly to Timeplus on port 3218, and OTel telemetry (traces, logs, metrics) via OTLP/HTTP on port 4318 — applies streaming SQL rules to detect threats, and exposes results through a live security dashboard.

---

## Table of Contents

1. [Overall Architecture](#1-overall-architecture)
2. [Data Collection](#2-data-collection)
3. [Agents](#3-agents)
4. [Threats and Rules](#4-threats-and-rules)
5. [Rule Catalog](#5-rule-catalog)
6. [Cost Governance](#6-cost-governance)
7. [CIM Metrics](#7-cim-metrics)

---

## 1. Overall Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│  Agent (OpenClaw / Claude Code)                                       │
│                                                                       │
│  ┌─────────────────────────┐   ┌──────────────────────────────────┐  │
│  │  AgentGuard hook plugin│   │  OTel plugin                     │  │
│  │  (captures hook events) │   │  (traces, logs, metrics)         │  │
│  └───────────┬─────────────┘   └──────────────┬───────────────────┘  │
└──────────────┼──────────────────────────────── ┼────────────────────-─┘
               │ HTTP ingest                      │ OTLP/HTTP
               │ POST /ingest  :3218              │ :4318
               ▼                                  ▼
┌──────────────────────────────────────────────────────────────────────┐
│  Timeplus (streaming SQL engine)                                      │
│                                                                       │
│  :3218 ingest   Streams                        MVs / Alerts           │
│  ────────────   ──────────────────────────     ───────────────────    │
│  hook events →  agentguard_hook_events                               │
│                   ├─ mv_cim_claudecode ─┐                             │
│                   └─ mv_cim_openclaw ───┴► agentguard_cim_event      │
│                                              ├─ mv_rule_rp001    ┐    │
│                 agentguard_security_events ◄─ mv_rule_rp002    │    │
│                 agentguard_notify_events   ◄─ mv_rule_rp003    │    │
│  :4318 input    agentguard_threats         ←  mv_threats       │    │
│  ────────────   agentguard_rules           ←  mv_notify_filter │    │
│  OTLP/HTTP  →   agentguard_pricing             agentguard_    │    │
│  otel_traces                                    notifier (alert)┘    │
│  otel_logs  ─── mv_cim_metrics_claudecode_otel ──────────────────┐   │
│  otel_metrics_*                                                   │   │
│                 agentguard_hook_events                           │   │
│                   ├─ mv_session_agents ──► agentguard_session_agents │
│                   ├─ mv_run_latency ─────► agentguard_run_latency    │
│                   ├─ mv_cim_metrics_openclaw_llm  ───────────────┐    │
│                   ├─ mv_cim_metrics_openclaw_tools ──────────────┤    │
│                   └─ mv_cim_metrics_claudecode_tools ────────────┤    │
│                 (+ otel_logs above) ─────────────────────────────┘    │
│                                              agentguard_cim_metrics  │
└──────────────────────────┬───────────────────────────────────────────┘
                           │ SQL queries :3218 (HTTP) / :8463 (native TCP)
                           │ Alert UDF callback → POST /api/alerts/webhook
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  AgentGuard Backend  (Go / Gin, :8080)                               │
│                                                                       │
│  /api/agents        /api/threats     /api/rules                       │
│  /api/metrics       /api/cost        /api/notifications/stream (SSE)  │
│  /api/proton/query  /api/investigate /api/onboarding                  │
└──────────────────────────┬───────────────────────────────────────────┘
                           │ REST + SSE
                           ▼
┌──────────────────────────────────────────────────────────────────────┐
│  React SPA  (:5173 dev / embedded in binary for prod)                 │
└──────────────────────────────────────────────────────────────────────┘
```
All resource overview 
<img width="1168" height="403" alt="image" src="https://github.com/user-attachments/assets/10633d9b-a6b4-4fe4-8550-34b4d08f4569" />


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
4. Create `agentguard_hook_events`
5. Create `agentguard_cim_event` (CIM unified event stream)
6. Drop and recreate `mv_cim_claudecode` and `mv_cim_openclaw` (CIM normalization MVs — always recreated so DDL fixes apply without manual drops)
7. Create `agentguard_session_agents` (mutable, session→agent_id lookup), `agentguard_run_latency` (mutable, run→latency lookup), and `agentguard_cim_metrics` (CIM metrics sink stream)
8. Drop and recreate 6 CIM metrics MVs in dependency order: `mv_session_agents`, `mv_run_latency`, `mv_cim_metrics_openclaw_llm`, `mv_cim_metrics_openclaw_tools`, `mv_cim_metrics_claudecode_otel`, `mv_cim_metrics_claudecode_tools`
9. Create `agentguard_security_events`, `agentguard_notify_events`, `agentguard_rules`
10. Create `agentguard_threats` (mutable stream) — warning only
11. Create `mv_threats` — warning only
12. Create `agentguard_pricing` (mutable stream) — warning only
13. Seed 6 default pricing rows (skipped if any rows exist) — warning only
14. Seed catalog rules as disabled (skipped if any rows exist) — warning only
15. Drop 4 legacy named security MVs — warning only
16. `CREATE OR REPLACE` the `notify_agentguard_udf` Python UDF — warning only
17. Create `mv_notify_filter` — warning only
18. Drop and recreate `agentguard_notifier` alert — warning only
19. Sync per-rule MVs to match current `agentguard_rules` state — warning only

> Steps 10–19 are non-fatal: logged as warnings but the server starts regardless.

---

## 2. Data Collection

AgentGuard receives two types of telemetry from agents:

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

### 2.2 Hook Events (Raw Ingestion Stream)

The AgentGuard hook plugin posts events directly to the Timeplus HTTP ingest API on port 3218, writing rows into `agentguard_hook_events`. This path bypasses the AgentGuard backend entirely. Security rules do **not** read from this stream directly — they read from `agentguard_cim_event` (see §2.4). The raw stream is still available for low-level investigation.

```sql
CREATE STREAM agentguard_hook_events (
  hook_name        low_cardinality(string),  -- agent-specific hook name
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
  hook_decision    low_cardinality(string),  -- 'observe' | 'block'
  block_reason     string,
  event_data       string,                   -- JSON blob; schema varies by hook
  agent_type       low_cardinality(string)   -- 'claudecode' | 'openclaw'
)
```

**OpenClaw hook names and their `event_data` payloads:**

| `hook_name` | Payload fields of interest |
|-------------|---------------------------|
| `session_start` | `session_key` (format: `agent:main:subagent:uuid`) |
| `session_end` | (none) |
| `message_received` | `content` (user message text) |
| `llm_input` | `prompt`, `system`, `provider`, `model` |
| `llm_output` | `assistantTexts[]`, `usage.input`, `usage.output` |
| `before_tool_call` | `toolName`, `params` |
| `after_tool_call` | `toolName`, `result`, `error`, `durationMs` |
| `before_install` | `package_name`, `registry` |

**Claude Code hook names and their `event_data` payloads:**

| `hook_name` | Source | Payload fields of interest |
|-------------|--------|---------------------------|
| `session_start` | `SessionStart` hook | `model` |
| `session_end` | `SessionEnd` hook | (none) |
| `user_prompt_submit` | `UserPromptSubmit` hook | `prompt` |
| `before_tool_call` | `PreToolUse` hook | `tool_name`, `tool_input` |
| `after_tool_call` | `PostToolUse` hook | `tool_name`, `tool_response` |
| `tool_call_failure` | `PostToolUseFailure` hook | `tool_name` |
| `llm_output` | `Stop` hook | (stop event, no LLM content in payload) |
| `subagent_spawning` | `SubagentStart` hook | (none) |
| `subagent_ended` | `SubagentStop` hook | (none) |
| `permission_denied` | `PermissionDenied` hook | `tool_name` |
| `prompt_context` | virtual (UserPromptSubmit) | `message_count`, `messages[]` — full transcript |
| `llm_context` | virtual (Stop) | `message_count`, `messages[]` — full transcript |

> **`run_id` for Claude Code:** Claude Code has no "run" concept. `run_id` is always set to `session_id` for all Claude Code hook events.
>
> **Virtual events (`prompt_context` / `llm_context`):** Not fired by Claude Code itself. The plugin reads the `transcript_path` JSONL (provided in every hook payload) and synthesizes two additional events per LLM turn. `prompt_context` carries the conversation state before the turn; `llm_context` carries the complete turn including all tool call/result pairs. Both are sent to Timeplus alongside the triggering real event. These are the primary source for full-prompt security analysis.
>
> **`session_key` for Claude Code:** Always an empty string. Use `session_id` for session identity. OpenClaw uses `session_key` in `agent:main:subagent:uuid` format to express session hierarchy.

### 2.3 API — Data Collection

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/alerts/webhook` | Receives `AlertWebhookPayload` from the Timeplus alert UDF and fans it out to all SSE clients |
| GET | `/api/notifications/stream` | SSE long-poll stream; sends `event: alert\ndata: {...}` on each detection; 20 s keepalive |
| POST | `/api/onboarding/generate` | Returns plugin config snippet for `openclaw` or `claude_code` agent types |
| POST | `/api/proton/query` | Streams arbitrary SQL directly to Timeplus; returns NDJSON rows line-by-line |

### 2.4 CIM Layer (Common Information Model)

The CIM layer normalizes heterogeneous hook events from multiple agent types into a single unified schema. Security rules query `agentguard_cim_event` rather than raw `agentguard_hook_events`, so rule SQL is agent-agnostic.

#### `agentguard_cim_event` — Unified event stream

```sql
CREATE STREAM agentguard_cim_event (
  event_time        datetime64(3),
  event_type        low_cardinality(string),  -- canonical event type (see table below)
  agent_type        low_cardinality(string),  -- 'claudecode' | 'openclaw'
  agent_id          string,
  deployment_id     low_cardinality(string),
  session_id        string,
  run_id            string,
  provider          low_cardinality(string),  -- 'anthropic', 'openai', etc.
  model             low_cardinality(string),  -- e.g. 'claude-sonnet-4-6'
  tool_name         string,
  tool_input        string,                   -- JSON: tool parameters
  tool_result       string,                   -- JSON: tool output
  tool_success      bool,
  tool_duration_ms  int64,
  user_message      string,                   -- user text content
  assistant_message string,                   -- first assistant response text
  hook_decision     low_cardinality(string),  -- 'observe' | 'block'
  block_reason      string,
  raw_hook_name     low_cardinality(string),  -- original hook_name from source agent
  raw_event_data    string                    -- original event_data JSON blob
)
```

**CIM `event_type` values:**

| `event_type` | Description | Claude Code source | OpenClaw source |
|--------------|-------------|-------------------|-----------------|
| `session_start` | Session or subagent started | `session_start`, `subagent_spawning` | `session_start` |
| `session_end` | Session ended | — | `session_end` |
| `user_input` | User message submitted | `user_prompt_submit` | `message_received` |
| `llm_request` | Full prompt assembled and sent to LLM | — | `llm_input` |
| `llm_response` | LLM response received | `llm_output` | `llm_output` |
| `tool_invoke` | Tool call initiated | `before_tool_call` | `before_tool_call` |
| `tool_complete` | Tool call completed | `after_tool_call` | `after_tool_call` |
| `prompt_context` | Full conversation context before LLM turn | `prompt_context` (virtual) | — |
| `llm_context` | Full conversation context after LLM turn | `llm_context` (virtual) | — |
| `other` | Any hook not matched above | — | — |

> **Note:** `llm_request` only exists for OpenClaw (which fires an `llm_input` hook with the complete assembled prompt). For Claude Code, the full prompt context is captured via the virtual `prompt_context` / `llm_context` events in `raw_event_data.messages[]`.

#### `mv_cim_claudecode` — Claude Code normalizer

Reads from `agentguard_hook_events` where `agent_type = 'claudecode'` and maps Claude Code-specific hook names to CIM `event_type` values. Key field mappings:

| CIM field | Source |
|-----------|--------|
| `event_type` | `multi_if(hook_name = 'user_prompt_submit', 'user_input', hook_name = 'llm_output', 'llm_response', ...)` |
| `tool_input` | `json_query(event_data, '$.tool_input')` |
| `tool_result` | `json_query(event_data, '$.tool_response')` |
| `user_message` | `json_value(event_data, '$.prompt')` |
| `assistant_message` | `json_value(event_data, '$.last_assistant_message')` |
| `raw_event_data` | `event_data` (full original JSON; for `prompt_context`/`llm_context`, contains `messages[]` array) |

Filters: `hook_name IN ('session_start', 'subagent_spawning', 'user_prompt_submit', 'before_tool_call', 'after_tool_call', 'llm_output', 'prompt_context', 'llm_context')`

#### `mv_cim_openclaw` — OpenClaw normalizer

Reads from `agentguard_hook_events` where `agent_type = 'openclaw'` and maps OpenClaw-specific hook names to CIM `event_type` values. Key field mappings:

| CIM field | Source |
|-----------|--------|
| `event_type` | `multi_if(hook_name = 'message_received', 'user_input', hook_name = 'llm_input', 'llm_request', ...)` |
| `tool_input` | `json_query(event_data, '$.params')` |
| `tool_result` | `json_query(event_data, '$.result')` |
| `tool_duration_ms` | `json_extract_int(event_data, 'durationMs')` |
| `user_message` | `json_value(event_data, '$.content')` |
| `assistant_message` | `json_value(event_data, '$.assistantTexts[0]')` |

Filters: `hook_name IN ('session_start', 'session_end', 'message_received', 'llm_input', 'llm_output', 'before_tool_call', 'after_tool_call')`

---

## 3. Agents

### 3.1 Data Model

Agent identity is derived from `(deployment_id, agent_id)` pairs in `agentguard_hook_events`. There is no separate agents table — all agent data is computed on-the-fly from the hook event stream.

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
FROM table(agentguard_hook_events)
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
FROM table(agentguard_hook_events)
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
FROM table(agentguard_hook_events)
WHERE session_id = '<id>' AND run_id = '<id>'
ORDER BY event_time ASC
```
Each row becomes a span; duration is computed as the gap to the next event. Spans are typed (`run`, `llm`, `tool`, `lifecycle`) based on `hook_name`.

**LLM metrics — tokens, calls, and latency** (from `agentguard_cim_metrics`; see §7 for how this stream is populated):
```sql
-- Token and call counts (Q1)
SELECT
    to_int64(sum_if(metric_value, metric_name = 'llm.call'))               AS total_calls,
    to_int64(sum_if(metric_value, metric_name = 'llm.input_tokens'))       AS input_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.output_tokens'))      AS output_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.total_tokens'))       AS total_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.cache_read_tokens'))  AS cache_read_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.cache_write_tokens')) AS cache_write_tokens
FROM table(agentguard_cim_metrics)
WHERE metric_name IN ('llm.call','llm.input_tokens','llm.output_tokens',
                      'llm.total_tokens','llm.cache_read_tokens','llm.cache_write_tokens')

-- Stop reason breakdown (Q2)
SELECT tags['stop_reason'] AS stop_reason, to_int64(sum(metric_value)) AS cnt
FROM table(agentguard_cim_metrics)
WHERE metric_name = 'llm.call'
GROUP BY stop_reason

-- Latency percentiles (Q3)
SELECT
    quantile(0.5)(metric_value)  AS p50_ms,
    quantile(0.95)(metric_value) AS p95_ms,
    quantile(0.99)(metric_value) AS p99_ms,
    avg(metric_value)            AS avg_ms,
    min(metric_value)            AS min_ms,
    max(metric_value)            AS max_ms,
    count()                      AS sample_count
FROM table(agentguard_cim_metrics)
WHERE metric_name = 'llm.latency_ms' AND metric_value > 0

-- Throughput time series (Q4; granularity = to_start_of_hour/minute/day)
SELECT
    to_start_of_hour(event_time)                                                 AS bucket,
    to_int64(sum_if(metric_value, metric_name = 'llm.call'))                    AS llm_calls,
    to_int64(sum_if(metric_value, metric_name = 'llm.input_tokens'))            AS input_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.output_tokens'))           AS output_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.total_tokens'))            AS total_tokens
FROM table(agentguard_cim_metrics)
WHERE metric_name IN ('llm.call','llm.input_tokens','llm.output_tokens','llm.total_tokens')
GROUP BY bucket ORDER BY bucket ASC
```

All four queries accept optional `WHERE` filters on `agent_id`, `deployment_id`, `tags['model']`, and `event_time`.

**Tool metrics — calls and latency** (from `agentguard_cim_metrics`):
```sql
-- Call counts and success/fail per tool (Q1)
SELECT
    tags['tool_name']                                                          AS tool_name,
    to_int64(sum_if(metric_value, metric_name = 'tool.call'))                 AS total_calls,
    to_int64(sum_if(metric_value, metric_name = 'tool.success'))              AS success_calls,
    to_int64(sum_if(metric_value, metric_name = 'tool.call'))
      - to_int64(sum_if(metric_value, metric_name = 'tool.success'))          AS failed_calls
FROM table(agentguard_cim_metrics)
WHERE metric_name IN ('tool.call', 'tool.success')
GROUP BY tool_name ORDER BY total_calls DESC

-- Latency per tool (Q2; OpenClaw only — Claude Code does not emit tool.duration_ms)
SELECT
    tags['tool_name']            AS tool_name,
    quantile(0.5)(metric_value)  AS p50_ms,
    quantile(0.95)(metric_value) AS p95_ms,
    avg(metric_value)            AS avg_ms,
    count()                      AS sample_count
FROM table(agentguard_cim_metrics)
WHERE metric_name = 'tool.duration_ms' AND metric_value > 0
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

#### `agentguard_security_events` — Detection sink

Written to by each per-rule materialized view. Every row is one rule match.

```sql
CREATE STREAM agentguard_security_events (
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

#### `agentguard_rules` — Append-only rule log

Every rule mutation (install, toggle, upgrade, delete) appends a new row. Current state is resolved via `arg_max`.

```sql
CREATE STREAM agentguard_rules (
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
FROM table(agentguard_rules)
GROUP BY rule_id
HAVING arg_max(deleted, updated_at) = false
```

#### `agentguard_threats` — Mutable threat state

A mutable stream deduplicated by `(agent_id, deployment_id, session_id, rule_id)`. Uses column families to separate detection data from status data, allowing independent upserts.

```sql
CREATE MUTABLE STREAM agentguard_threats (
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

#### `agentguard_notify_events` — Alert filter stream

Receives only threats that are new or have re-opened after being cleared, avoiding alert storms for already-known threats.

```sql
CREATE STREAM agentguard_notify_events (
  -- same schema as agentguard_security_events
)
```

### 4.2 Materialized Views

#### `mv_threats` — Threat deduplication

Continuously upserts into `agentguard_threats` as security events arrive. Sets `status = ''` (empty) so `mv_notify_filter` can distinguish new from already-triaged threats.

```sql
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
SETTINGS seek_to = 'earliest'
```

#### `mv_notify_filter` — Noise suppression

Passes only detections where the threat has no status or was previously cleared (i.e., newly opened or re-opened threats).

```sql
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
WHERE t.status = '' OR t.status = 'cleared'
```

#### Per-rule MVs — Detection pipeline

Each enabled rule has a MV named `mv_rule_<ruleID_no_hyphens>` (e.g., `rp-001` → `mv_rule_rp001`). The MV query reads **`agentguard_cim_event`** (not the raw hook events stream) and writes matches to `agentguard_security_events`. Using the CIM stream means rules are agent-agnostic: the same SQL detects threats from both Claude Code and OpenClaw agents without any agent-specific conditionals.

**Create:**
```sql
CREATE MATERIALIZED VIEW mv_rule_rp001
INTO agentguard_security_events
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

#### UDF — `notify_agentguard_udf`

A Python UDF that POSTs each detection to the AgentGuard backend webhook endpoint. The backend then fans the event to all SSE-connected browser clients.

```sql
CREATE OR REPLACE FUNCTION notify_agentguard_udf(
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

#### Alert — `agentguard_notifier`

Batches up to 1 event at a time with a 5 s timeout, rate-limited to 10 alerts per minute.

```sql
CREATE ALERT agentguard_notifier
  BATCH 1 EVENTS WITH TIMEOUT 5s
  LIMIT 10 ALERTS PER 1m
  CALL notify_agentguard_udf
AS SELECT
  rule_id, rule_name, severity, agent_id, deployment_id,
  session_id, hook_name, tool_name, message, detected_at
FROM agentguard_notify_events
```

### 4.4 Threat Identity and Status

**ThreatID** is computed in Go as an MD5 hash of `agentID + deploymentID + sessionID + ruleID` using length-prefix encoding. It is never stored in Timeplus — it is a stable key for the API.

**Status lifecycle:**
- `""` (empty) — detected by MV, not yet triaged
- `"open"` — actively firing or re-opened
- `"acknowledged"` — operator has seen it
- `"cleared"` — operator has resolved it

**Re-open logic:** If a threat's `last_seen > status_updated_at` and its status is `"cleared"`, the API re-opens it to `"open"` and fires an async upsert to `agentguard_threats`.

**Status upsert:**
```sql
-- Acknowledge:
INSERT INTO agentguard_threats
  (agent_id, deployment_id, session_id, rule_id, status, status_updated_at)
VALUES ('<id>', '<id>', '<id>', '<id>', 'acknowledged', now64(3))

-- Clear:
INSERT INTO agentguard_threats
  (agent_id, deployment_id, session_id, rule_id, status, status_updated_at)
VALUES ('<id>', '<id>', '<id>', '<id>', 'cleared', now64(3))
```

### 4.5 API — Threats and Rules

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/threats` | List all threats; `?severity=&category=`; merges threats table + stats + events |
| GET | `/api/threats/:id` | Single threat by ThreatID (MD5 of 4 key fields) |
| POST | `/api/threats/:id/acknowledge` | Upsert `status='acknowledged'` into `agentguard_threats` |
| POST | `/api/threats/:id/clear` | Upsert `status='cleared'` into `agentguard_threats` |
| GET | `/api/rules` | List installed rules with 24 h detection counts |
| GET | `/api/rules/:id` | Rule detail with 50 most recent detections |
| POST | `/api/rules/:id/install` | Install from catalog: insert to `agentguard_rules`, create MV |
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
    QuerySQL          string   `yaml:"query_sql"`          // SELECT into agentguard_security_events
}
```

### 5.2 Catalog Layers

The `Manager` merges two layers (local overrides seed by ID):

| Layer | Source | Mutability |
|-------|--------|-----------|
| **Seed** | Go-embedded YAML files (`backend/internal/catalog/seed/*.yaml`) | Read-only |
| **Local** | Disk files in `--catalog-dir` (default `./catalog`) | Read-write via `Save()` |

### 5.3 Built-in Rules

All built-in rules query `agentguard_cim_event` using normalized `event_type` values, making them agent-agnostic (they detect threats from both Claude Code and OpenClaw without modification). The `hook_sources` field in the catalog entry lists the CIM `event_type` values the rule inspects.

| ID | Name | Severity | Category | CIM Event Types |
|----|------|----------|----------|----------------|
| `rp-001` | Prompt Injection Shield | critical | prompt_injection | `user_input`, `llm_request` |
| `rp-002` | DLP Sentinel | critical | dlp | `tool_complete`, `llm_response` |
| `rp-003` | Privilege Guard | warning | privilege | `tool_invoke` |
| `rp-004` | Supply Chain Watch | warning | supply_chain | `tool_invoke` |

**`rp-001` — Prompt Injection Shield:**
```sql
SELECT
  event_time                AS detected_at,
  'rp-001'                  AS rule_id,
  'Prompt Injection Shield' AS rule_name,
  'critical'                AS severity,
  agent_id, deployment_id, session_id, run_id,
  raw_hook_name             AS hook_name,
  tool_name,
  concat('Potential prompt injection in ', raw_hook_name, ' from agent ', agent_id) AS message,
  raw_event_data            AS event_data
FROM agentguard_cim_event
WHERE event_type IN ('user_input', 'llm_request')
  AND (
       lower(user_message) LIKE '%ignore previous instructions%'
    OR lower(user_message) LIKE '%ignore all previous%'
    OR lower(user_message) LIKE '%jailbreak%'
    OR lower(user_message) LIKE '%disregard your%'
    OR lower(user_message) LIKE '%you are now a%'
    OR lower(user_message) LIKE '%forget everything%'
    OR lower(user_message) LIKE '%new instructions:%'
  )
```

**`rp-002` — DLP Sentinel:**
```sql
SELECT
  event_time     AS detected_at,
  'rp-002'       AS rule_id,
  'DLP Sentinel' AS rule_name,
  'critical'     AS severity,
  agent_id, deployment_id, session_id, run_id,
  raw_hook_name  AS hook_name,
  tool_name,
  concat('Sensitive data in ', raw_hook_name,
    if(tool_name != '', concat(' (', tool_name, ')'), ''),
    ' from agent ', agent_id) AS message,
  raw_event_data AS event_data
FROM agentguard_cim_event
WHERE event_type IN ('tool_complete', 'llm_response')
  AND session_id != ''
  AND (
       match(concat(tool_result, ' ', assistant_message), '[0-9]{3}-[0-9]{2}-[0-9]{4}')
    OR lower(concat(tool_result, ' ', assistant_message)) LIKE '%api_key%'
    OR lower(concat(tool_result, ' ', assistant_message)) LIKE '%secret_key%'
    OR lower(concat(tool_result, ' ', assistant_message)) LIKE '%bearer %'
    OR lower(concat(tool_result, ' ', assistant_message)) LIKE '%sk-ant-%'
    OR lower(concat(tool_result, ' ', assistant_message)) LIKE '%ghp_%'
    OR lower(concat(tool_result, ' ', assistant_message)) LIKE '%private_key%'
    OR lower(concat(tool_result, ' ', assistant_message)) LIKE '%aws_secret%'
  )
```

**`rp-003` — Privilege Guard:**
```sql
SELECT
  event_time        AS detected_at,
  'rp-003'          AS rule_id,
  'Privilege Guard' AS rule_name,
  'warning'         AS severity,
  agent_id, deployment_id, session_id, run_id,
  raw_hook_name     AS hook_name,
  tool_name,
  concat('Dangerous tool call: ', tool_name, ' by agent ', agent_id) AS message,
  raw_event_data    AS event_data
FROM agentguard_cim_event
WHERE event_type = 'tool_invoke'
  AND (
       lower(tool_name) IN ('bash', 'shell', 'exec', 'run_terminal_cmd', 'computer')
    OR lower(tool_input) LIKE '%sudo %'
    OR lower(tool_input) LIKE '%rm -rf%'
    OR lower(tool_input) LIKE '%chmod%'
    OR lower(tool_input) LIKE '%/etc/passwd%'
    OR lower(tool_input) LIKE '%/etc/shadow%'
  )
```

**`rp-004` — Supply Chain Watch:**
```sql
SELECT
  event_time           AS detected_at,
  'rp-004'             AS rule_id,
  'Supply Chain Watch' AS rule_name,
  'warning'            AS severity,
  agent_id, deployment_id, session_id, run_id,
  raw_hook_name        AS hook_name,
  tool_name,
  concat('Package install by agent ', agent_id,
    ' via ', if(tool_name != '', tool_name, raw_hook_name)) AS message,
  raw_event_data       AS event_data
FROM agentguard_cim_event
WHERE event_type = 'tool_invoke'
  AND (
       lower(tool_input) LIKE '%npm install%'
    OR lower(tool_input) LIKE '%pip install%'
    OR lower(tool_input) LIKE '%yarn add%'
    OR lower(tool_input) LIKE '%cargo install%'
    OR lower(tool_input) LIKE '%apt-get install%'
    OR lower(tool_input) LIKE '%brew install%'
  )
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

#### `agentguard_pricing` — Provider pricing table

A mutable stream used as a key-value store. Upserts by `(provider, model)` primary key.

```sql
CREATE MUTABLE STREAM agentguard_pricing (
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
FROM table(agentguard_hook_events) AS e
LEFT JOIN table(agentguard_pricing) AS p
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
FROM table(agentguard_hook_events) AS e
LEFT JOIN table(agentguard_pricing) AS p
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
FROM table(agentguard_hook_events) AS e
LEFT JOIN table(agentguard_pricing) AS p
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
INSERT INTO agentguard_pricing
  (provider, model, input_cost_per_million, output_cost_per_million, cache_read_cost_per_million, updated_at)
VALUES ('<provider>', '<model>', <input>, <output>, <cache>, now64(3))
-- Mutable stream semantics: upserts by PRIMARY KEY (provider, model)
```

**Delete pricing:**
```sql
DELETE FROM agentguard_pricing
WHERE provider = '<provider>' AND model = '<model>'
```

### 6.6 API — Cost

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/cost` | Full cost report: MTD, forecast, daily trend (30 d), token breakdown, per-agent/model costs, cache savings |
| GET | `/api/cost/pricing` | List all pricing entries from `agentguard_pricing` |
| POST | `/api/cost/pricing` | Upsert a pricing entry (insert into mutable stream) |
| DELETE | `/api/cost/pricing/:provider/:model` | Delete a pricing entry by primary key |

---

## 7. CIM Metrics

The CIM Metrics layer is a pre-aggregated metrics pipeline that normalizes per-call LLM and tool telemetry from all agent types into a single `agentguard_cim_metrics` stream. The `/api/metrics/llm` and `/api/metrics/tools` handlers query this stream exclusively.

### 7.1 Design Rationale

**Why a separate metrics stream instead of querying raw events?**

- **Multi-source fusion**: Claude Code LLM metrics come from OTel logs (`otel_logs`), not hook events. OpenClaw LLM metrics come from `agentguard_cim_event`. A single metrics stream hides this difference from the API layer.
- **Agent-ID reconciliation**: OTel logs carry Claude Code's internal SHA hash as `user.id`. The metrics MVs resolve this to the configured hostname by joining `agentguard_session_agents` (populated from hook events where the correct `agent_id` is always present).
- **Latency sourcing**: OpenClaw emits `durationMs` in its `agent_end` hook, not in `llm_output`. A `agentguard_run_latency` mutable stream bridges the gap, allowing `mv_cim_metrics_openclaw_llm` to join latency into the same row as the token counts.
- **Pre-computed fan-out**: Each LLM call emits 7 metric rows in a single `ARRAY JOIN`; each tool call emits 2–3. This trades storage for query simplicity — aggregation queries are simple `sum_if` pivots.

### 7.2 Lookup Streams

Two mutable streams act as live key-value lookup tables joined by the metrics MVs.

#### `agentguard_session_agents` — Session → Agent mapping

```sql
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_session_agents
(
  session_id    string,
  agent_id      string,
  deployment_id string,
  first_seen    datetime64(3)
) PRIMARY KEY (session_id)
```

Fed by `mv_session_agents` which reads **all** hook events (both agent types):

```sql
CREATE MATERIALIZED VIEW mv_session_agents
INTO agentguard_session_agents AS
SELECT session_id, agent_id, deployment_id, event_time AS first_seen
FROM agentguard_hook_events
```

Because this is a mutable stream, duplicate inserts (many events per session) are harmless — only the latest row per `session_id` is retained. Used by `mv_cim_metrics_claudecode_otel` to resolve the OTel `user.id` hash to the human-readable `agent_id` set in the hook plugin config.

#### `agentguard_run_latency` — Run → LLM latency mapping

```sql
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_run_latency
(
  run_id     string,
  latency_ms float64
) PRIMARY KEY (run_id)
```

Fed by `mv_run_latency` which reads `agent_end` hook events (OpenClaw only):

```sql
CREATE MATERIALIZED VIEW mv_run_latency
INTO agentguard_run_latency AS
SELECT
  run_id,
  to_float64_or_zero(json_extract_string(event_data, 'durationMs')) AS latency_ms
FROM agentguard_hook_events
WHERE hook_name = 'agent_end'
```

OpenClaw fires `agent_end` ~2 ms before `llm_output` for the same `run_id`, so the latency entry is available in the mutable stream by the time `mv_cim_metrics_openclaw_llm` processes the corresponding `llm_response` CIM event.

### 7.3 `agentguard_cim_metrics` — Metrics Sink

```sql
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
)
```

**Metric names and their sources:**

| `metric_name` | Unit | OpenClaw source | Claude Code source |
|---------------|------|-----------------|-------------------|
| `llm.call` | count (1 per call) | `llm_response` CIM event | `api_request` OTel log |
| `llm.input_tokens` | tokens | `raw_event_data.lastAssistant.usage.input` | `log_attributes['input_tokens']` |
| `llm.output_tokens` | tokens | `raw_event_data.lastAssistant.usage.output` | `log_attributes['output_tokens']` |
| `llm.total_tokens` | tokens | `totalTokens` field | `input_tokens + output_tokens` |
| `llm.cache_read_tokens` | tokens | `usage.cacheRead` | `log_attributes['cache_read_tokens']` |
| `llm.cache_write_tokens` | tokens | `usage.cacheWrite` | `log_attributes['cache_creation_tokens']` |
| `llm.latency_ms` | milliseconds | `agentguard_run_latency.latency_ms` (from `agent_end`) | `log_attributes['duration_ms']` |
| `tool.call` | count (1 per call) | `tool_complete` CIM event | `tool_complete` CIM event |
| `tool.success` | 0 or 1 | `tool_success` field | `tool_success` field |
| `tool.duration_ms` | milliseconds | `tool_duration_ms` field | — (not emitted) |

**Tags per metric type:**

| Metric family | Tags |
|---------------|------|
| `llm.*` | `model`, `stop_reason`, `run_id` |
| `tool.*` | `tool_name`, `run_id` |

### 7.4 Metrics Materialized Views

Four MVs feed `agentguard_cim_metrics`. They are **dropped and recreated on every startup** so DDL fixes apply without manual intervention.

**Creation order** (dependency-safe):
1. `mv_session_agents` — populates lookup before any MV joins it
2. `mv_run_latency` — populates lookup before OpenClaw LLM MV joins it
3. `mv_cim_metrics_openclaw_llm` — JOINs `agentguard_run_latency`
4. `mv_cim_metrics_openclaw_tools`
5. `mv_cim_metrics_claudecode_otel` — JOINs `agentguard_session_agents`
6. `mv_cim_metrics_claudecode_tools`

#### `mv_cim_metrics_openclaw_llm`

```sql
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
  AND e.event_type = 'llm_response'
```

#### `mv_cim_metrics_openclaw_tools`

```sql
CREATE MATERIALIZED VIEW mv_cim_metrics_openclaw_tools
INTO agentguard_cim_metrics AS
SELECT
  event_time,
  metric_name,
  metric_value,
  'openclaw'                                                   AS agent_type,
  agent_id, deployment_id, session_id,
  map_cast(['tool_name', 'run_id'], [tool_name, run_id])      AS tags
FROM agentguard_cim_event
ARRAY JOIN
  ['tool.call', 'tool.success', 'tool.duration_ms']           AS metric_name,
  [
    to_float64(1),
    if(tool_success, to_float64(1), to_float64(0)),
    to_float64(tool_duration_ms)
  ]                                                            AS metric_value
WHERE agent_type = 'openclaw' AND event_type = 'tool_complete'
```

#### `mv_cim_metrics_claudecode_otel`

Reads directly from `otel_logs` (not from `agentguard_cim_event`) because Claude Code's OTel plugin emits accurate per-call token and latency data that is not available in hook events.

```sql
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
WHERE o.log_attributes['event.name'] = 'api_request'
```

> **OTel log attribute key facts for Claude Code:**
> - `log_attributes['event.name']` = `'api_request'` (not `'claude_code.api_request'`)
> - `log_attributes['session.id']` = the session UUID (NOT `resource_attributes`)
> - `log_attributes['user.id']` = internal SHA hash — resolved to the configured hostname via the `agentguard_session_agents` JOIN
> - `log_attributes['cache_creation_tokens']` = prompt-cache write tokens (not `cache_write_tokens`)

#### `mv_cim_metrics_claudecode_tools`

```sql
CREATE MATERIALIZED VIEW mv_cim_metrics_claudecode_tools
INTO agentguard_cim_metrics AS
SELECT
  event_time,
  metric_name,
  metric_value,
  'claudecode' AS agent_type,
  agent_id, deployment_id, session_id,
  map_cast(['tool_name', 'run_id'], [tool_name, run_id]) AS tags
FROM agentguard_cim_event
ARRAY JOIN
  ['tool.call', 'tool.success']             AS metric_name,
  [
    to_float64(1),
    if(tool_success, to_float64(1), to_float64(0))
  ]                                         AS metric_value
WHERE agent_type = 'claudecode' AND event_type = 'tool_complete'
```

> Claude Code does not populate `tool_duration_ms` in its CIM events (the `PostToolUse` hook payload does not include duration), so `tool.duration_ms` is not emitted for Claude Code agents.

### 7.5 Mutable Stream Constraints

- `agentguard_session_agents` and `agentguard_run_latency` are **simple mutable streams** (single column family, no `SETTINGS coalesced = true`). The `coalesced` setting requires multiple `FAMILY` declarations and causes a `code: 997` error on single-family streams.
- The JOIN in `mv_cim_metrics_claudecode_otel` uses `agentguard_session_agents` as a **live mutable stream** (not `table()`). Using `table()` in a streaming MV takes a static snapshot at creation time (empty on fresh start) and would never see hook events that arrive after startup.

### 7.6 Known Limitations

| Limitation | Detail |
|------------|--------|
| Claude Code `stop_reason` in LLM metrics | OTel logs do not carry a stop reason field. The `stop_reason` tag is always empty for Claude Code metrics; `tags['stop_reason']` filtering only works for OpenClaw. |
| Claude Code `tool.duration_ms` | Not emitted — the `PostToolUse` hook payload does not include a duration field. |
| `run_id` for Claude Code | Claude Code has no "run" concept; `run_id` in CIM events is always equal to `session_id`. The `run_id` tag in LLM metrics is also set to `session_id`. |

### 7.7 API — Metrics

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/metrics/llm` | LLM metrics: token counts, call stats, stop-reason breakdown, latency percentiles, throughput time series. Params: `agent_id`, `model`, `since`, `until` (RFC3339), `granularity` (minute\|hour\|day) |
| GET | `/api/metrics/tools` | Tool metrics: call counts and latency per tool. Params: `agent_id`, `tool_name`, `since`, `until` |

---

## Appendix — Investigate and Dashboard APIs

### Investigate

The Investigate page exposes raw Timeplus query access.

**Priority streams** always listed first:

| Stream | Description |
|--------|-------------|
| `agentguard_hook_events` | Raw agent hook events |
| `agentguard_security_events` | Security rule detections |
| `agentguard_threats` | Deduplicated threat state |

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
| Total agents | `count(distinct concat(deployment_id, agent_id))` from `agentguard_hook_events` |
| Active threats (24h) | `count()` from `agentguard_threats` where `last_seen >= now() - 1d` |
| Critical escalations | Count where `severity = 'critical'` from above |
| Total cost (24h) | JOIN query on `hook_events` + `pricing` last 24 h |
| Active rules | `count()` from `agentguard_rules` current state (enabled, not deleted) |
| Threat categories | Group active threats by rule `category`, compute percentages |

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/dashboard` | All KPIs, threat categories in one response |
