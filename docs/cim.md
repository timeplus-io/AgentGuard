# AgentGuard — Common Information Model (CIM)

AgentGuard ingests telemetry from heterogeneous AI coding agents — currently OpenClaw, Claude Code, and Hermes Agent — each with its own hook vocabulary, event payloads, and observability integrations. The **Common Information Model (CIM)** is the normalization layer that abstracts these differences away so that security rules, metrics, and dashboards can be written once and work across all agent types without modification.

The CIM consists of two layers:

| Layer | Stream | Purpose |
|-------|--------|---------|
| **CIM Events** | `agentguard_cim_event` | Normalized agent lifecycle events (session, LLM, tool) |
| **CIM Metrics** | `agentguard_cim_metrics` | Pre-computed, named scalar metrics per call |

---

## Table of Contents

1. [Why CIM](#1-why-cim)
2. [CIM Event Layer](#2-cim-event-layer)
   - 2.1 [Event Schema](#21-event-schema)
   - 2.2 [Canonical Event Types](#22-canonical-event-types)
   - 2.3 [Normalization Pipeline](#23-normalization-pipeline)
   - 2.4 [Field Mapping by Agent](#24-field-mapping-by-agent)
3. [CIM Metrics Layer](#3-cim-metrics-layer)
   - 3.1 [Metrics Schema](#31-metrics-schema)
   - 3.2 [Metric Catalog](#32-metric-catalog)
   - 3.3 [Metrics Normalization Pipeline](#33-metrics-normalization-pipeline)
   - 3.4 [Lookup Streams](#34-lookup-streams)
   - 3.5 [Metrics Materialized Views](#35-metrics-materialized-views)
4. [End-to-End Data Flow](#4-end-to-end-data-flow)
5. [Querying CIM Data](#5-querying-cim-data)

---

## 1. Why CIM

Without CIM, every consumer (security rule, metric query, dashboard widget) would need to handle agent-specific differences:

| Concern | OpenClaw | Claude Code | Hermes |
|---------|----------|-------------|--------|
| LLM call event name | `llm_output` hook | OTel `api_request` log | `post_llm_call` hook |
| Token field path | `event_data.lastAssistant.usage.input` | `log_attributes['input_tokens']` | `event_data.usage.input_tokens` |
| LLM call latency source | `agent_end` hook (`durationMs`) | OTel `log_attributes['duration_ms']` | `post_api_request` hook (`api_duration` in seconds × 1000) |
| Agent identity in LLM data | `agent_id` column (always hostname) | OTel `user.id` (SHA hash, not hostname) | `agent_id` column (configured hostname) |
| Session identity | `session_id` column | `log_attributes['session.id']` | `session_id` column |
| Tool call duration | `after_tool_call` → `durationMs` | Not available | Not available |
| Prompt text | `llm_input` hook → `event_data.prompt` | Virtual `prompt_context` event → `messages[]` | `pre_llm_call` hook → `event_data.messages[]` |
| Session end event | `session_end` | `session_end` | `on_session_finalize` (sent as `on_session_end`) |
| Per-turn end event | — | — | `on_session_end` (sent as `conversation_end`) |

CIM resolves all of this upstream, in materialized views, once. Downstream consumers always see the same schema regardless of which agent produced the data.

---

## 2. CIM Event Layer

### 2.1 Event Schema

`agentguard_cim_event` is a regular append-only stream. Each row represents one normalized agent lifecycle event.

```sql
CREATE STREAM agentguard_cim_event
(
  event_time        datetime64(3),              -- when the event occurred
  event_type        low_cardinality(string),    -- canonical type (see §2.2)
  agent_type        low_cardinality(string),    -- 'claudecode' | 'openclaw'
  agent_id          string,                     -- configured agent hostname/identifier
  deployment_id     low_cardinality(string),    -- deployment environment id
  session_id        string,                     -- conversation session UUID
  run_id            string,                     -- single LLM turn identifier
  provider          low_cardinality(string),    -- 'anthropic', 'openai', etc.
  model             low_cardinality(string),    -- e.g. 'claude-sonnet-4-6'
  tool_name         string,                     -- tool being called/completed
  tool_input        string,                     -- JSON: tool parameters
  tool_result       string,                     -- JSON: tool output
  tool_success      bool,                       -- false if tool errored or was blocked
  tool_duration_ms  int64,                      -- tool wall-clock time (OpenClaw only)
  user_message      string,                     -- user text content
  assistant_message string,                     -- first assistant response text
  hook_decision     low_cardinality(string),    -- 'observe' | 'block'
  block_reason      string,                     -- reason if hook_decision = 'block'
  raw_hook_name     low_cardinality(string),    -- original hook name from the agent
  raw_event_data    string                      -- original JSON payload (for rules/context)
)
```

**Field notes:**

| Field | Notes |
|-------|-------|
| `run_id` | Claude Code has no "run" concept — `run_id` is always set to `session_id` for all Claude Code events |
| `provider` | Empty string for Claude Code CIM events (provider is carried in OTel logs, not hook events) |
| `tool_duration_ms` | Always 0 for Claude Code (the PostToolUse hook payload does not include duration) |
| `raw_event_data` | For `prompt_context` / `llm_context` events, contains `messages[]` — the full conversation transcript used by prompt-injection rules |

### 2.2 Canonical Event Types

The `event_type` field collapses each agent's hook vocabulary into a shared set of lifecycle concepts:

| `event_type` | Meaning | Claude Code source hooks | OpenClaw source hooks | Hermes source hooks |
|--------------|---------|-------------------------|----------------------|---------------------|
| `session_start` | Session or sub-agent started | `session_start`, `subagent_spawning` | `session_start` | `on_session_start`, `on_session_reset` |
| `session_end` | Session ended | — (no event emitted) | `session_end` | `on_session_end`, `on_session_finalize` |
| `user_input` | User message submitted | `user_prompt_submit` | `message_received` | `pre_llm_call` |
| `llm_request` | Assembled prompt sent to LLM | — (not available) | `llm_input` | `pre_api_request` |
| `llm_response` | LLM response received | `llm_output` | `llm_output` | `post_llm_call` |
| `tool_invoke` | Tool call initiated | `before_tool_call` | `before_tool_call` | `pre_tool_call` |
| `tool_complete` | Tool call completed | `after_tool_call` | `after_tool_call` | `post_tool_call` |
| `prompt_context` | Full conversation context before LLM turn | `prompt_context` (virtual) | — | — |
| `llm_context` | Full conversation + tool results after turn | `llm_context` (virtual) | — | — |
| `other` | Any hook not matched above | — | — | — |

> **Virtual events (`prompt_context` / `llm_context`):** The Claude Code plugin synthesizes these by reading the `transcript_path` JSONL file embedded in every hook payload. They are not fired by Claude Code itself. They carry the full conversation in `raw_event_data.messages[]` and are the primary source for full-prompt security analysis (e.g., prompt injection rule `rp-001`).

> **`llm_request` gap:** OpenClaw fires an `llm_input` hook with the full assembled prompt before sending to the LLM. Claude Code has no equivalent — the closest substitute is the virtual `prompt_context` event. Security rules that need pre-send prompt content should check `event_type IN ('llm_request', 'prompt_context')`.

### 2.3 Normalization Pipeline

```
agentguard_hook_events
        │
        ├─── [WHERE agent_type = 'claudecode'] ──► mv_cim_claudecode
        │                                                │
        ├─── [WHERE agent_type = 'openclaw']  ──► mv_cim_openclaw
        │                                                │
        └─── [WHERE agent_type = 'hermes']   ──► mv_cim_hermes
                                                         │
                                               ──────────┘
                                               ▼
                                    agentguard_cim_event
                                    (unified, agent-agnostic)
                                               │
                              ┌────────────────┤
                              ▼                ▼
                     Security rules      CIM Metrics MVs
                    (mv_rule_rp001 …)    (mv_cim_metrics_*)
```

`mv_cim_claudecode`, `mv_cim_openclaw`, and `mv_cim_hermes` are created on startup via `execIfNotExists` — they are **only created if they do not already exist**. To apply DDL changes (field extraction fixes, new hook mappings), drop the affected view in Timeplus and restart the server so it is recreated with the updated DDL.

### 2.4 Field Mapping by Agent

The tables below show how each CIM field is populated for each agent type.

#### Claude Code (`mv_cim_claudecode`)

Source: `agentguard_hook_events WHERE agent_type = 'claudecode'`

Included hooks: `session_start`, `subagent_spawning`, `user_prompt_submit`, `before_tool_call`, `after_tool_call`, `llm_output`, `prompt_context`, `llm_context`

| CIM field | Mapping |
|-----------|---------|
| `event_type` | `multi_if(hook_name = 'user_prompt_submit', 'user_input', hook_name = 'llm_output', 'llm_response', hook_name = 'before_tool_call', 'tool_invoke', hook_name = 'after_tool_call', 'tool_complete', hook_name IN ('session_start','subagent_spawning'), 'session_start', hook_name, 'other')` |
| `agent_type` | `'claudecode'` (passthrough) |
| `agent_id` | `agent_id` column (configured hostname, e.g. `Gangs-MacBook-Pro.local`) |
| `session_id` | `session_id` column |
| `run_id` | `run_id` column (equals `session_id` for Claude Code) |
| `provider` | `''` (empty — not present in hook events) |
| `model` | `if(model != '', model, json_value(event_data, '$.model'))` |
| `tool_name` | `if(tool_name != '', tool_name, json_value(event_data, '$.tool_name'))` |
| `tool_input` | `json_query(event_data, '$.tool_input')` |
| `tool_result` | `json_query(event_data, '$.tool_response')` |
| `tool_success` | `json_value(event_data, '$.tool_response.interrupted') != 'true'` |
| `tool_duration_ms` | `0` (not available) |
| `user_message` | `json_value(event_data, '$.prompt')` |
| `assistant_message` | `json_value(event_data, '$.last_assistant_message')` |
| `raw_event_data` | `event_data` (full original JSON) |

#### OpenClaw (`mv_cim_openclaw`)

Source: `agentguard_hook_events WHERE agent_type = 'openclaw'`

Included hooks: `session_start`, `session_end`, `message_received`, `llm_input`, `llm_output`, `before_tool_call`, `after_tool_call`

| CIM field | Mapping |
|-----------|---------|
| `event_type` | `multi_if(hook_name = 'message_received', 'user_input', hook_name = 'llm_input', 'llm_request', hook_name = 'llm_output', 'llm_response', hook_name = 'before_tool_call', 'tool_invoke', hook_name = 'after_tool_call', 'tool_complete', hook_name = 'session_start', 'session_start', hook_name = 'session_end', 'session_end', 'other')` |
| `agent_type` | `'openclaw'` (passthrough) |
| `agent_id` | `agent_id` column (passthrough) |
| `session_id` | `session_id` column |
| `run_id` | `run_id` column (OpenClaw assigns a unique ID per LLM turn) |
| `provider` | `if(provider != '', provider, json_value(event_data, '$.provider'))` |
| `model` | `if(model != '', model, json_value(event_data, '$.model'))` |
| `tool_name` | `if(tool_name != '', tool_name, json_value(event_data, '$.toolName'))` |
| `tool_input` | `json_query(event_data, '$.params')` |
| `tool_result` | `json_query(event_data, '$.result')` |
| `tool_success` | `json_value(event_data, '$.error') = ''` |
| `tool_duration_ms` | `json_extract_int(event_data, 'durationMs')` |
| `user_message` | `if(hook_name = 'llm_input', json_value(event_data, '$.prompt'), json_value(event_data, '$.content'))` |
| `assistant_message` | `json_value(event_data, '$.assistantTexts[0]')` |
| `raw_event_data` | `event_data` (full original JSON) |

#### Hermes (`mv_cim_hermes`)

Source: `agentguard_hook_events WHERE agent_type = 'hermes'`

Included hooks: `on_session_start`, `on_session_end`, `on_session_finalize`, `on_session_reset`, `pre_llm_call`, `post_llm_call`, `pre_api_request`, `pre_tool_call`, `post_tool_call`

> **Hook name remapping:** The Hermes plugin remaps hook names before ingestion. Hermes `on_session_end` (fires after each chat turn) is sent to AgentGuard as `conversation_end`. Hermes `on_session_finalize` (true session teardown) is sent as `on_session_end`. This ensures session lifetime is tracked correctly.

> **`run_id` for Hermes:** Hermes has no explicit run concept. `run_id` is always set to `session_id` for all Hermes events.

> **`post_api_request` not in CIM events:** `post_api_request` carries token counts and latency but is excluded from `mv_cim_hermes` to avoid a duplicate `llm_response` event per LLM call. It is consumed directly by `mv_cim_metrics_hermes_llm` from `agentguard_hook_events`.

> **`provider` gap:** `post_llm_call` (the `llm_response` source) does not carry `provider` in its Hermes kwargs. Provider is only populated for `llm_request` events (from `pre_api_request`). `llm_response` CIM events for Hermes will have an empty `provider` field.

| CIM field | Mapping |
|-----------|---------|
| `event_type` | `pre_llm_call` → `user_input`; `post_llm_call` → `llm_response`; `pre_api_request` → `llm_request`; `pre_tool_call` → `tool_invoke`; `post_tool_call` → `tool_complete`; `on_session_start`/`on_session_reset` → `session_start`; `on_session_end`/`on_session_finalize` → `session_end` |
| `agent_type` | `'hermes'` (passthrough) |
| `agent_id` | `agent_id` column (configured hostname) |
| `session_id` | `session_id` column |
| `run_id` | `run_id` column (equals `session_id` for Hermes) |
| `provider` | `if(provider != '', provider, json_value(event_data, '$.provider'))` — populated only for `llm_request` events |
| `model` | `model` column |
| `tool_name` | `if(tool_name != '', tool_name, json_value(event_data, '$.tool_name'))` |
| `tool_input` | `json_query(event_data, '$.args')` (Hermes uses `args` as the kwarg name) |
| `tool_result` | `json_query(event_data, '$.result')` |
| `tool_success` | `true` (hardcoded — Hermes `post_tool_call` does not emit an explicit success/error boolean) |
| `tool_duration_ms` | `0` (not available) |
| `user_message` | `json_value(event_data, '$.user_message')` — populated on both `pre_llm_call` (`user_input`) and `post_llm_call` (`llm_response`) events |
| `assistant_message` | `json_value(event_data, '$.assistant_response')` — populated on `post_llm_call` (`llm_response`) events |
| `raw_event_data` | `event_data` (full original JSON) |

---

## 3. CIM Metrics Layer

### 3.1 Metrics Schema

`agentguard_cim_metrics` is an append-only stream. Each row is one named scalar metric observation tied to a single agent event.

```sql
CREATE STREAM agentguard_cim_metrics
(
  event_time    datetime64(3),                        -- timestamp of the source event
  metric_name   low_cardinality(string),              -- metric identifier (see §3.2)
  metric_value  float64,                              -- numeric value
  agent_type    low_cardinality(string),              -- 'claudecode' | 'openclaw'
  agent_id      string,                               -- resolved agent hostname
  deployment_id low_cardinality(string),
  session_id    string,
  tags          map(low_cardinality(string), string)  -- metric-specific dimensions
)
```

The **fan-out pattern** is key: a single LLM call event (one row in `agentguard_cim_event`) produces **7 rows** in `agentguard_cim_metrics` (one per metric name) via `ARRAY JOIN`. A tool call produces **2–3 rows**. This trades storage for query simplicity — aggregations become plain `sum_if` pivots with no JSON parsing.

### 3.2 Metric Catalog

#### LLM Metrics

Tags for all LLM metrics: `model`, `stop_reason`, `run_id`

| `metric_name` | Value | OpenClaw source | Claude Code source | Hermes source |
|---------------|-------|-----------------|-------------------|----|
| `llm.call` | `1.0` per call | `agentguard_cim_event` WHERE `event_type = 'llm_response'` | `otel_logs` WHERE `log_attributes['event.name'] = 'api_request'` | `agentguard_hook_events` WHERE `hook_name = 'post_api_request'` |
| `llm.input_tokens` | prompt token count | `json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'input')` | `log_attributes['input_tokens']` | `json_extract_string(event_data, 'usage', 'input_tokens')` |
| `llm.output_tokens` | completion token count | `json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'output')` | `log_attributes['output_tokens']` | `json_extract_string(event_data, 'usage', 'output_tokens')` |
| `llm.total_tokens` | input + output | `json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'totalTokens')` | `input_tokens + output_tokens` (computed) | `input_tokens + output_tokens` (computed) |
| `llm.cache_read_tokens` | prompt-cache hit tokens | `json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'cacheRead')` | `log_attributes['cache_read_tokens']` | `json_extract_string(event_data, 'usage', 'cache_read_input_tokens')` |
| `llm.cache_write_tokens` | prompt-cache write tokens | `json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'cacheWrite')` | `log_attributes['cache_creation_tokens']` | `json_extract_string(event_data, 'usage', 'cache_creation_input_tokens')` |
| `llm.latency_ms` | LLM wall-clock time (ms) | `agentguard_run_latency.latency_ms` (from `agent_end` hook, same `run_id`) | `log_attributes['duration_ms']` | `json_extract_string(event_data, 'api_duration') * 1000` (seconds → ms, from `post_api_request`) |

> **`stop_reason` tag:** Only populated for OpenClaw (extracted from `raw_event_data.lastAssistant.stopReason`). Always empty for Claude Code and Hermes — OTel logs and Hermes hooks do not carry a stop reason field.

#### Tool Metrics

Tags for all tool metrics: `tool_name`, `run_id`

| `metric_name` | Value | OpenClaw source | Claude Code source | Hermes source |
|---------------|-------|-----------------|-------------------|----|
| `tool.call` | `1.0` per call | `agentguard_cim_event` WHERE `event_type = 'tool_complete'` | same | same |
| `tool.success` | `1.0` if success, `0.0` if failed | `tool_success` field | `tool_success` field | `tool_success` field |
| `tool.duration_ms` | tool wall-clock time (ms) | `tool_duration_ms` field (from `durationMs` in `after_tool_call` payload) | **not emitted** | **not emitted** |

### 3.3 Metrics Normalization Pipeline

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│  Raw Sources                                                                            │
│                                                                                         │
│  agentguard_hook_events                          otel_logs                             │
│  (all agents, all hooks)                          (Claude Code OTel plugin)            │
└────────────────┬─────────────────────────────────────────┬──────────────────────────────┘
                 │                                         │
       ┌─────────┴──────────┐                              │
       │                    │                              │
       ▼                    ▼                              │
 mv_session_agents    mv_run_latency                       │
       │                    │                              │
       ▼                    ▼                              │
 agentguard_        agentguard_                           │
 session_agents      run_latency                           │
 (mutable lookup:    (mutable lookup:                      │
  session→agent_id)   run→latency_ms)                      │
       │                    │                              │
       └──────────┬─────────┘                              │
                  │                                        │
       ┌──────────┴──────────────────────┐                 │
       │ agentguard_cim_event           │                 │
       │ (from mv_cim_claudecode +       │                 │
       │        mv_cim_openclaw  +       │                 │
       │        mv_cim_hermes)           │                 │
       └──────────┬──────────────────────┘                 │
                  │                                        │
    ┌─────────────┼──────────────────────────┐             │
    │             │                          │             │
    ▼             ▼                          ▼             ▼
mv_cim_       mv_cim_              mv_cim_metrics_  mv_cim_metrics_
metrics_      metrics_             claudecode_otel  hermes_tools
openclaw_llm  openclaw_tools       (JOINs           (from cim_event)
(JOINs        (no JOIN)            session_agents)
run_latency)
    │             │                          │
    │             ▼                          │
    │    mv_cim_metrics_                     │    mv_cim_metrics_
    │    claudecode_tools                    │    hermes_llm
    │    (no JOIN)                           │    (reads hook_events
    │             │                          │     WHERE post_api_request
    │             │                          │     directly — not cim_event)
    └─────────────┴──────────────────────────┴──────────┐
                                                         │
                                                         ▼
                                              agentguard_cim_metrics
                                              (unified, agent-agnostic metrics stream)
```

### 3.4 Lookup Streams

The metrics MVs require two pieces of information that are not available in their primary source:

1. **`agentguard_session_agents`** — Claude Code's OTel logs carry an internal SHA hash as `user.id`, not the configured agent hostname. This mutable stream maps `session_id → agent_id` so the OTel MV can resolve the correct identifier.

2. **`agentguard_run_latency`** — OpenClaw's `llm_output` hook does not carry latency. The latency is in the `agent_end` hook (which fires ~2 ms earlier for the same `run_id`). This mutable stream bridges the two events.

Both are **mutable streams** (primary-key deduplication), not regular streams, because:
- The same `session_id` appears in thousands of hook events — only the latest row per key is kept.
- The streaming MV JOIN needs a **live lookup**, not a static snapshot. Using `table(stream)` in a streaming MV takes a snapshot at creation time (empty on startup) and would never see events that arrive later.

```sql
-- Session → Agent ID lookup
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_session_agents
(
  session_id    string,
  agent_id      string,       -- configured hostname (e.g. 'Gangs-MacBook-Pro.local')
  deployment_id string,
  first_seen    datetime64(3)
) PRIMARY KEY (session_id)

-- Run → Latency lookup (OpenClaw only)
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_run_latency
(
  run_id     string,
  latency_ms float64          -- from agent_end hook's durationMs field
) PRIMARY KEY (run_id)
```

Feeder MVs that populate the lookups:

```sql
-- Feeds from ALL hook events (both agents)
CREATE MATERIALIZED VIEW mv_session_agents
INTO agentguard_session_agents AS
SELECT session_id, agent_id, deployment_id, event_time AS first_seen
FROM agentguard_hook_events

-- Feeds from agent_end hook only (OpenClaw)
CREATE MATERIALIZED VIEW mv_run_latency
INTO agentguard_run_latency AS
SELECT
  run_id,
  to_float64_or_zero(json_extract_string(event_data, 'durationMs')) AS latency_ms
FROM agentguard_hook_events
WHERE hook_name = 'agent_end'
```

### 3.5 Metrics Materialized Views

#### OpenClaw LLM (`mv_cim_metrics_openclaw_llm`)

Source: `agentguard_cim_event WHERE agent_type = 'openclaw' AND event_type = 'llm_response'`

JOIN: `LEFT JOIN agentguard_run_latency AS r ON r.run_id = e.run_id`

Produces 7 rows per LLM call via `ARRAY JOIN`:

```
llm_response CIM event
      │
      ├─ llm.call          = 1.0
      ├─ llm.input_tokens  = raw_event_data.lastAssistant.usage.input
      ├─ llm.output_tokens = raw_event_data.lastAssistant.usage.output
      ├─ llm.total_tokens  = raw_event_data.lastAssistant.usage.totalTokens
      ├─ llm.cache_read_tokens  = raw_event_data.lastAssistant.usage.cacheRead
      ├─ llm.cache_write_tokens = raw_event_data.lastAssistant.usage.cacheWrite
      └─ llm.latency_ms    = agentguard_run_latency.latency_ms  ← via JOIN
```

#### OpenClaw Tools (`mv_cim_metrics_openclaw_tools`)

Source: `agentguard_cim_event WHERE agent_type = 'openclaw' AND event_type = 'tool_complete'`

Produces 3 rows per tool call:

```
tool_complete CIM event
      │
      ├─ tool.call        = 1.0
      ├─ tool.success     = 1.0 if tool_success else 0.0
      └─ tool.duration_ms = tool_duration_ms column
```

#### Claude Code LLM (`mv_cim_metrics_claudecode_otel`)

Source: `otel_logs WHERE log_attributes['event.name'] = 'api_request'`

JOIN: `LEFT JOIN agentguard_session_agents AS s ON s.session_id = o.log_attributes['session.id']`

This MV reads directly from `otel_logs`, **not** from `agentguard_cim_event`. Claude Code's OTel plugin emits one log record per LLM API call with accurate token counts and latency — these are not available in the hook events.

```
otel_logs record (event.name = 'api_request')
      │
      ├─ agent_id         = session_agents.agent_id    ← via JOIN (resolves SHA hash)
      │                     fallback: log_attributes['user.id']
      ├─ session_id       = log_attributes['session.id']
      │
      ├─ llm.call          = 1.0
      ├─ llm.input_tokens  = log_attributes['input_tokens']
      ├─ llm.output_tokens = log_attributes['output_tokens']
      ├─ llm.total_tokens  = input_tokens + output_tokens  (computed)
      ├─ llm.cache_read_tokens  = log_attributes['cache_read_tokens']
      ├─ llm.cache_write_tokens = log_attributes['cache_creation_tokens']
      └─ llm.latency_ms    = log_attributes['duration_ms']
```

Key OTel attribute paths:

| Attribute | Location | Notes |
|-----------|----------|-------|
| `event.name` | `log_attributes` | Value is `'api_request'` (not `'claude_code.api_request'`) |
| `session.id` | `log_attributes` | The session UUID; **not** in `resource_attributes` |
| `user.id` | `log_attributes` | Internal SHA hash; resolved to hostname via session_agents JOIN |
| `model` | `log_attributes` | Model identifier |
| `input_tokens` | `log_attributes` | Prompt tokens |
| `output_tokens` | `log_attributes` | Completion tokens |
| `cache_read_tokens` | `log_attributes` | Tokens served from prompt cache |
| `cache_creation_tokens` | `log_attributes` | Tokens written to prompt cache (note: `creation`, not `write`) |
| `duration_ms` | `log_attributes` | API call wall-clock time in ms |

#### Claude Code Tools (`mv_cim_metrics_claudecode_tools`)

Source: `agentguard_cim_event WHERE agent_type = 'claudecode' AND event_type = 'tool_complete'`

Produces 2 rows per tool call (no duration — not available from Claude Code hooks):

```
tool_complete CIM event
      │
      ├─ tool.call    = 1.0
      └─ tool.success = 1.0 if tool_success else 0.0
```

#### Hermes LLM (`mv_cim_metrics_hermes_llm`)

Source: `agentguard_hook_events WHERE agent_type = 'hermes' AND hook_name = 'post_api_request'`

Reads **directly from `agentguard_hook_events`**, not from `agentguard_cim_event`. `post_api_request` is excluded from `mv_cim_hermes` to avoid a duplicate `llm_response` CIM event per LLM call; the metrics MV bypasses CIM and reads the raw hook directly.

```
post_api_request hook event (agentguard_hook_events)
      │
      ├─ llm.call              = 1.0
      ├─ llm.input_tokens      = event_data.usage.input_tokens
      ├─ llm.output_tokens     = event_data.usage.output_tokens
      ├─ llm.total_tokens      = input_tokens + output_tokens (computed)
      ├─ llm.cache_read_tokens = event_data.usage.cache_read_input_tokens
      ├─ llm.cache_write_tokens= event_data.usage.cache_creation_input_tokens
      └─ llm.latency_ms        = event_data.api_duration × 1000  (seconds → ms)
```

#### Hermes Tools (`mv_cim_metrics_hermes_tools`)

Source: `agentguard_cim_event WHERE agent_type = 'hermes' AND event_type = 'tool_complete'`

Produces 2 rows per tool call (no duration — not available from Hermes hooks):

```
tool_complete CIM event
      │
      ├─ tool.call    = 1.0
      └─ tool.success = 1.0 if tool_success else 0.0
```

> **Note:** `tool_success` is hardcoded `true` for Hermes — `post_tool_call` does not emit an explicit success/error boolean. Rules that filter `tool_success = false` will not fire for Hermes tool failures.

---

## 4. End-to-End Data Flow

The full pipeline from agent action to queryable metric:

```
Agent Action
     │
     ├── Claude Code ──────────────────────────────────────────────────────┐
     │   │                                                                  │
     │   ├── Hook plugin → POST :3218 → agentguard_hook_events            │
     │   │       │                                                          │
     │   │       ├── mv_cim_claudecode ──────────► agentguard_cim_event   │
     │   │       │       (hook_name → event_type mapping)                  │
     │   │       │                                                          │
     │   │       └── mv_session_agents ──────────► agentguard_session_agents
     │   │                                         (session_id → agent_id) │
     │   │                                                                  │
     │   └── OTel plugin → OTLP/HTTP :4318 → otel_logs                     │
     │           │                                                          │
     │           └── mv_cim_metrics_claudecode_otel                        │
     │                   (JOIN session_agents for agent_id) ───────────────┤
     │                                                                      │
     ├── OpenClaw ───────────────────────────────────────────────────────  │
     │   │                                                                  │
     │   ├── Hook plugin → POST :3218 → agentguard_hook_events            │
     │   │       │                                                          │
     │   │       ├── mv_cim_openclaw ────────────► agentguard_cim_event   │
     │   │       │       (hook_name → event_type mapping)                  │
     │   │       │                                                          │
     │   │       ├── mv_run_latency ─────────────► agentguard_run_latency │
     │   │       │       (agent_end hook → durationMs)                     │
     │   │       │                                                          │
     │   │       ├── mv_cim_metrics_openclaw_llm                           │
     │   │       │       (JOIN run_latency for latency_ms) ────────────────┤
     │   │       │                                                          │
     │   │       └── mv_cim_metrics_openclaw_tools ──────────────────────  │
     │   │                                                                  │
     │   └── OTel plugin → OTLP/HTTP :4318 → otel_traces / otel_metrics_* │
     │           (used for tracing, not CIM metrics)                       │
     │                                                                      │
     └── Hermes ──────────────────────────────────────────────────────────  │
         │                                                                  │
         └── Hook plugin → POST :3218 → agentguard_hook_events            │
                 │                                                          │
                 ├── mv_cim_hermes ───────────────► agentguard_cim_event   │
                 │       (pre_llm_call/post_llm_call/pre_api_request/       │
                 │        pre_tool_call/post_tool_call → event_type)       │
                 │                                                          │
                 ├── mv_cim_metrics_hermes_llm (reads hook_events directly)│
                 │       (post_api_request → token counts + latency) ───────┤
                 │                                                          │
                 └── mv_cim_metrics_hermes_tools ─────────────────────────  │
                                                                           │
                    ┌──────────────────────────────────────────────────────┘
                    │
                    ▼                              ▼
         agentguard_cim_event          agentguard_cim_metrics
         (all event types,              (llm.* and tool.* metrics,
          all agents)                   all agents)
                    │                              │
         ┌──────────┤               ┌──────────────┤
         ▼          ▼               ▼              ▼
   Security     Agent           /api/metrics   /api/metrics
   rule MVs     session/event   /llm           /tools
   (mv_rule_*)  queries
         │
         ▼
   agentguard_security_events
   → agentguard_threats
   → notifications / SSE
```

---

## 5. Querying CIM Data

### CIM Events — example queries

**All LLM responses in the last hour across all agents:**
```sql
SELECT event_time, agent_type, agent_id, model, session_id, run_id,
       raw_event_data
FROM agentguard_cim_event
WHERE event_type = 'llm_response'
  AND event_time >= now() - interval 1 hour
ORDER BY event_time DESC
```

**Tool calls blocked by a hook policy:**
```sql
SELECT event_time, agent_type, agent_id, tool_name, hook_decision, block_reason
FROM agentguard_cim_event
WHERE event_type = 'tool_invoke'
  AND hook_decision = 'block'
ORDER BY event_time DESC
```

**Session timeline for a given session (ordered waterfall):**
```sql
SELECT event_time, event_type, tool_name, user_message, assistant_message
FROM agentguard_cim_event
WHERE session_id = '<uuid>'
ORDER BY event_time ASC
```

### CIM Metrics — example queries

**Token totals by model across all agents:**
```sql
SELECT
    tags['model']                                                            AS model,
    agent_type,
    to_int64(sum_if(metric_value, metric_name = 'llm.call'))                AS total_calls,
    to_int64(sum_if(metric_value, metric_name = 'llm.input_tokens'))        AS input_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.output_tokens'))       AS output_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.total_tokens'))        AS total_tokens
FROM table(agentguard_cim_metrics)
WHERE metric_name IN ('llm.call', 'llm.input_tokens', 'llm.output_tokens', 'llm.total_tokens')
GROUP BY model, agent_type
ORDER BY total_tokens DESC
```

**P95 LLM latency by agent:**
```sql
SELECT agent_id, agent_type,
       quantile(0.95)(metric_value) AS p95_ms,
       count()                      AS samples
FROM table(agentguard_cim_metrics)
WHERE metric_name = 'llm.latency_ms' AND metric_value > 0
GROUP BY agent_id, agent_type
ORDER BY p95_ms DESC
```

**Tool success rate by tool name:**
```sql
SELECT
    tags['tool_name']                                                           AS tool_name,
    to_int64(sum_if(metric_value, metric_name = 'tool.call'))                  AS total,
    to_int64(sum_if(metric_value, metric_name = 'tool.success'))               AS success,
    round(sum_if(metric_value, metric_name = 'tool.success')
        / sum_if(metric_value, metric_name = 'tool.call') * 100, 1)           AS success_pct
FROM table(agentguard_cim_metrics)
WHERE metric_name IN ('tool.call', 'tool.success')
GROUP BY tool_name
ORDER BY total DESC
```

**Hourly token throughput (last 24 h):**
```sql
SELECT
    to_start_of_hour(event_time)                                              AS hour,
    to_int64(sum_if(metric_value, metric_name = 'llm.input_tokens'))          AS input_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.output_tokens'))         AS output_tokens
FROM table(agentguard_cim_metrics)
WHERE metric_name IN ('llm.input_tokens', 'llm.output_tokens')
  AND event_time >= now() - interval 24 hour
GROUP BY hour
ORDER BY hour ASC
```
