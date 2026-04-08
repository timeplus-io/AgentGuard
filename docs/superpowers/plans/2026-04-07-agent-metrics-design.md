# Agent Metrics Design

> **Status:** Design / Research — written from live Timeplus data exploration (2026-04-07)

## Data Sources

AgentShield has two independent data pipelines from OpenClaw:

| Stream | Transport | Granularity | Agent Identity |
|---|---|---|---|
| `agentshield_hook_events` | HTTP (plugin) | per-event | `agent_id`, `deployment_id`, `session_id`, `run_id` |
| `otel_metrics_*` / `otel_traces` | OTLP | periodic/per-call | `openclaw.sessionKey` in attributes (gauge only) |
| `otel_logs` | OTLP | per log line | `service_name` only |

### `agentshield_hook_events` — Primary Source

```
hook_name           low_cardinality(string)   -- event type
event_time          datetime64(3)
session_id          string                    -- UUID per conversation episode
run_id              string                    -- per LLM turn (chatcmpl_*, UUID, announce:v1:*)
agent_id            string
deployment_id       low_cardinality(string)
deployment_name     low_cardinality(string)
session_key         string                    -- routing key: agent:<id>:<type>:<user>
tool_name           string
provider            string
model               string
hook_decision       low_cardinality(string)   -- "observe" | "block"
block_reason        string
event_data          string (JSON)             -- hook-specific payload
```

**Hook types seen in production data (by frequency):**

| Hook | Count | Trigger |
|---|---|---|
| `before_message_write` | 213 | User message arrives |
| `before_agent_start` | 136 | Agent begins processing a run |
| `before_model_resolve` | 73 | Model selection |
| `llm_output` | 63 | LLM response received |
| `agent_end` | 63 | Agent turn complete |
| `before_prompt_build` | 63 | Prompt assembled |
| `llm_input` | 63 | LLM API call dispatched |
| `after_tool_call` | 55 | Tool execution complete |
| `tool_result_persist` | 55 | Tool result saved |
| `before_tool_call` | 55 | Tool execution begins |
| `before_agent_reply` | 46 | Reply being dispatched |
| `before_dispatch` | 38 | Message dispatched |
| `reply_dispatch` | 38 | Reply sent to user |
| `message_received` | 38 | Message received from channel |
| `session_end` | 28 | Session closed |
| `subagent_ended` | 19 | Sub-agent completed/deleted |
| `before_reset` | 10 | `/new` or `/reset` triggered |
| `session_start` | 10 | Session opened |
| `gateway_start` | 7 | Gateway process started |
| `gateway_stop` | 3 | Gateway process stopped |
| `subagent_spawned` | 1 | Sub-agent launched |

**`event_data` JSON shapes (extracted from live data):**

`llm_input`:
```json
{
  "runId": "uuid",
  "sessionId": "uuid",
  "provider": "ollama",
  "model": "kimi-k2.5:cloud",
  "systemPrompt": "...",
  "messages": [...]
}
```

`llm_output`:
```json
{
  "runId": "uuid",
  "sessionId": "uuid",
  "provider": "ollama",
  "model": "kimi-k2.5:cloud",
  "assistantTexts": ["..."],
  "lastAssistant": {
    "stopReason": "stop",
    "usage": {
      "input": 8996,
      "output": 5,
      "cacheRead": 0,
      "cacheWrite": 0,
      "totalTokens": 9001,
      "cost": { "input": 0, "output": 0, "total": 0 }
    }
  },
  "usage": { "input": 8996, "output": 5, "total": 9001 },
  "success": true,
  "durationMs": 2985
}
```

`before_tool_call`:
```json
{
  "toolName": "read",
  "params": { "path": "/home/node/.openclaw/workspace/HEARTBEAT.md" },
  "runId": "uuid",
  "toolCallId": "ollama_call_..."
}
```

`agent_end`:
```json
{
  "messages": [...],
  "success": true,
  "durationMs": 2985
}
```

`subagent_ended`:
```json
{
  "targetSessionKey": "agent:main:openai-user:test-s6",
  "targetKind": "acp",
  "reason": "session-delete",
  "outcome": "deleted",
  "_ctx": {
    "childSessionKey": "agent:main:openai-user:test-s6",
    "requesterSessionKey": "agent:main:main"
  }
}
```

### OTEL Metrics — Secondary Source

**Critical limitation:** Most OTEL counter/histogram metrics have `attributes = {}` (empty) — no per-agent or per-session breakdown. Only `openclaw.model.usage` (gauge) and `openclaw.message.processed` (gauge) carry rich per-session attributes.

#### `otel_metrics_gauge` — Per-call rich attributes

| Metric | Attributes available |
|---|---|
| `openclaw.model.usage` | `openclaw.channel`, `openclaw.provider`, `openclaw.model`, `openclaw.sessionKey`, `openclaw.sessionId`, `openclaw.tokens.input`, `openclaw.tokens.output`, `openclaw.tokens.cache_read`, `openclaw.tokens.cache_write`, `openclaw.tokens.total` |
| `openclaw.message.processed` | `openclaw.channel`, `openclaw.outcome`, `openclaw.sessionKey`, `openclaw.messageId` |

Example `openclaw.model.usage` attributes:
```
openclaw.channel      = "webchat"
openclaw.provider     = "ollama"
openclaw.model        = "kimi-k2.5:cloud"
openclaw.sessionKey   = "agent:main:main"
openclaw.sessionId    = "af06d234-..."
openclaw.tokens.input = "10452"
openclaw.tokens.total = "10458"
```

#### `otel_metrics_sum` — Counters (no per-agent breakdown)

| Metric | Description |
|---|---|
| `openclaw.tokens` | Cumulative token count (deployment-level) |
| `openclaw.queue.lane.enqueue` | Queue enqueue events |
| `openclaw.queue.lane.dequeue` | Queue dequeue events |
| `openclaw.session.state` | Session state transitions |
| `openclaw.message.queued` | Messages queued |
| `openclaw.message.processed` | Messages processed |

#### `otel_metrics_histogram` — Distributions (no per-agent breakdown)

| Metric | Unit | Description |
|---|---|---|
| `openclaw.queue.depth` | count | Queue depth snapshot |
| `openclaw.queue.wait_ms` | ms | Time message spent in queue |
| `openclaw.context.tokens` | tokens | Context window size used (max observed: 262,144) |
| `openclaw.run.duration_ms` | ms | LLM call duration (observed: 1816–4361ms) |
| `openclaw.message.duration_ms` | ms | End-to-end message processing time |

#### `otel_traces` — Spans

| Span name | Carries |
|---|---|
| `openclaw.model.usage` | Same attributes as gauge |
| `openclaw.message.processed` | Same attributes as gauge |

---

## Metric Catalogue

Metrics are grouped by domain. Each entry notes the primary source and derivation SQL.

### 1. Activity & Volume

| Metric | Source | SQL sketch |
|---|---|---|
| **Total events** | hook_events | `count()` |
| **Events by type** | hook_events | `count() GROUP BY hook_name` |
| **Active agents (window)** | hook_events | `count_distinct(agent_id) WHERE event_time > now() - interval X` |
| **Events per hour** | hook_events | `count() GROUP BY to_start_of_hour(event_time)` |

### 2. Session Metrics

| Metric | Source | SQL sketch |
|---|---|---|
| **Session count (filtered)** | hook_events | `count_distinct(session_id)` with HAVING filters |
| **Sessions by type** | hook_events | `split_by_char(':', session_key)[3]` → Web UI / HTTP API / Cron / Sub-agent |
| **Session duration** | hook_events | `date_diff('second', min(event_time), max(event_time)) GROUP BY session_id` |
| **Messages per session** | hook_events | `count_if(hook_name = 'before_message_write') GROUP BY session_id` |
| **Runs per session** | hook_events | `count_distinct(if(run_id != '', run_id, null)) GROUP BY session_id` |
| **Sessions started per day** | hook_events | `count_if(hook_name = 'session_start') GROUP BY to_date(event_time)` |
| **Active session count (live)** | otel_metrics_sum | `SUM(value) WHERE metric_name = 'openclaw.session.state'` |

### 3. LLM Usage & Token Metrics

These are the richest metrics. Token data is available from two places:

**Source A — `llm_output.event_data`** (best for per-agent/session breakdown):
```sql
SELECT
    agent_id,
    session_id,
    run_id,
    json_extract_string(event_data, 'usage', 'input')  AS input_tokens,
    json_extract_string(event_data, 'usage', 'output') AS output_tokens,
    json_extract_string(event_data, 'usage', 'total')  AS total_tokens,
    json_extract_string(event_data, 'lastAssistant', 'usage', 'cacheRead')  AS cache_read,
    json_extract_string(event_data, 'lastAssistant', 'usage', 'cacheWrite') AS cache_write,
    json_extract_string(event_data, 'lastAssistant', 'usage', 'cost', 'total') AS cost
FROM table(agentshield_hook_events)
WHERE hook_name = 'llm_output'
```

**Source B — `otel_metrics_gauge.openclaw.model.usage`** (per session, pre-aggregated):
```sql
SELECT
    attributes['openclaw.sessionKey']      AS session_key,
    attributes['openclaw.model']           AS model,
    attributes['openclaw.tokens.input']    AS input_tokens,
    attributes['openclaw.tokens.total']    AS total_tokens
FROM table(otel_metrics_gauge)
WHERE metric_name = 'openclaw.model.usage'
```

| Metric | Source | Notes |
|---|---|---|
| **Input tokens per run** | llm_output.event_data | `usage.input` |
| **Output tokens per run** | llm_output.event_data | `usage.output` |
| **Total tokens per run** | llm_output.event_data | `usage.total` |
| **Cache hit tokens per run** | llm_output.event_data | `lastAssistant.usage.cacheRead` |
| **Cache write tokens per run** | llm_output.event_data | `lastAssistant.usage.cacheWrite` |
| **Cache efficiency ratio** | llm_output.event_data | `cacheRead / totalTokens` |
| **Cost per run** | llm_output.event_data | `lastAssistant.usage.cost.total` (0 for self-hosted) |
| **Token usage per session** | llm_output.event_data | SUM over session_id |
| **Token usage per agent** | llm_output.event_data | SUM over agent_id |
| **Token usage per model** | llm_output.event_data | SUM GROUP BY model |
| **LLM calls per run** | hook_events | `count_if(hook_name = 'llm_output') GROUP BY run_id` |
| **Context window utilization** | otel_metrics_histogram | `openclaw.context.tokens` sum/count/max — deployment-level only |
| **Model distribution** | hook_events | `count() GROUP BY model WHERE hook_name = 'llm_output'` |
| **Provider distribution** | hook_events | `count() GROUP BY provider WHERE hook_name = 'llm_output'` |

### 4. Tool Usage Metrics

| Metric | Source | SQL sketch |
|---|---|---|
| **Tool calls per run** | hook_events | `count_if(hook_name = 'before_tool_call') GROUP BY run_id` |
| **Tool call distribution** | hook_events | `count() GROUP BY tool_name WHERE hook_name = 'before_tool_call'` |
| **Tool calls per agent** | hook_events | `count_if(hook_name = 'before_tool_call') GROUP BY agent_id` |
| **Top tools** | hook_events | `count() GROUP BY tool_name ORDER BY count() DESC` |
| **Tool call rate (time series)** | hook_events | `count_if(hook_name = 'before_tool_call') GROUP BY to_start_of_minute(event_time)` |
| **Tool call params** | hook_events.event_data | `json_extract_string(event_data, 'params')` on `before_tool_call` |

**Tools seen in production:** `read` (120), `write` (21), `exec` (12), `edit` (6), `sessions_yield` (3), `sessions_spawn` (3)

### 5. Performance & Latency Metrics

| Metric | Source | Notes |
|---|---|---|
| **Run duration** | hook_events.event_data | `json_extract_string(event_data, 'durationMs')` on `agent_end` |
| **Run duration (alternative)** | hook_events | `date_diff('millisecond', min(event_time), max(event_time)) GROUP BY run_id` |
| **LLM latency per run** | otel_metrics_histogram | `openclaw.run.duration_ms` (deployment-level, observed: 1816–4361ms) |
| **LLM latency per run (rich)** | otel_metrics_histogram | `openclaw.run.duration_ms` — no per-agent breakdown currently |
| **Message end-to-end latency** | otel_metrics_histogram | `openclaw.message.duration_ms` — deployment-level |
| **Queue wait time** | otel_metrics_histogram | `openclaw.queue.wait_ms` — deployment-level |
| **Queue depth** | otel_metrics_histogram | `openclaw.queue.depth` — deployment-level |
| **Queue throughput** | otel_metrics_sum | `openclaw.queue.lane.enqueue/dequeue` — deployment-level |

**Note:** OTEL histogram metrics (`openclaw.run.duration_ms`, `openclaw.message.duration_ms`, `openclaw.queue.wait_ms`, `openclaw.queue.depth`) currently have empty `attributes = {}`. They are deployment-level aggregates only and cannot be sliced by agent, session, or model without upstream changes in OpenClaw's OTEL instrumentation.

### 6. Security & Governance Metrics

| Metric | Source | SQL sketch |
|---|---|---|
| **Block events total** | hook_events | `count_if(hook_decision = 'block')` |
| **Block rate** | hook_events | `count_if(hook_decision = 'block') / count_if(hook_name = 'llm_output')` |
| **Blocks per agent** | hook_events | `count_if(hook_decision = 'block') GROUP BY agent_id` |
| **Block reasons distribution** | hook_events | `count() GROUP BY block_reason WHERE hook_decision = 'block'` |
| **Blocked tools** | hook_events | `count() GROUP BY tool_name WHERE hook_decision = 'block'` |
| **Blocks over time** | hook_events | `count_if(hook_decision = 'block') GROUP BY to_start_of_hour(event_time)` |
| **Sessions with blocks** | hook_events | `count_distinct(session_id) WHERE hook_decision = 'block'` |

### 7. Sub-agent Metrics

| Metric | Source | SQL sketch |
|---|---|---|
| **Sub-agent spawns per session** | hook_events | `count_if(hook_name = 'subagent_spawned') GROUP BY session_id` |
| **Sub-agent completions** | hook_events | `count_if(hook_name = 'subagent_ended') GROUP BY session_id` |
| **Sub-agent outcomes** | hook_events.event_data | `json_extract_string(event_data, 'outcome')` on `subagent_ended` |
| **Sub-agent parent-child graph** | hook_events.event_data | `_ctx.childSessionKey` + `_ctx.requesterSessionKey` on subagent hooks |
| **Sub-agent chain depth** | hook_events.event_data | derived from parent-child key relationships |

### 8. Deployment & Infrastructure Metrics

| Metric | Source | SQL sketch |
|---|---|---|
| **Gateway uptime** | hook_events | `count_if(hook_name = 'gateway_start')`, `count_if(hook_name = 'gateway_stop')` |
| **Deployments seen** | hook_events | `count_distinct(deployment_id)` |
| **Agents per deployment** | hook_events | `count_distinct(agent_id) GROUP BY deployment_id` |
| **Message queue throughput** | otel_metrics_sum | `openclaw.message.queued`, `openclaw.message.processed` |
| **Active sessions (live)** | otel_metrics_sum | `openclaw.session.state` |

---

## Availability Summary

| Category | Data available? | Per-agent? | Per-session? | Requires JSON extract? |
|---|---|---|---|---|
| Activity & Volume | ✅ Full | ✅ | ✅ | ❌ |
| Session Metrics | ✅ Full | ✅ | ✅ | ❌ |
| Token / LLM Usage | ✅ Full | ✅ | ✅ | ✅ (from event_data) |
| Cost | ✅ (0 for self-hosted) | ✅ | ✅ | ✅ (from event_data) |
| Tool Usage | ✅ Full | ✅ | ✅ | ❌ (tool_name in top-level column) |
| Run Duration | ✅ Full | ✅ | ✅ | ✅ (from event_data) or date_diff |
| LLM Latency | ⚠️ Partial | ❌ (deployment only) | ❌ | ❌ (OTEL histogram) |
| Queue Metrics | ⚠️ Partial | ❌ (deployment only) | ❌ | ❌ (OTEL histogram) |
| Security / Blocks | ✅ Full | ✅ | ✅ | ❌ |
| Sub-agent | ✅ Full | ✅ | ✅ | ✅ (for relationship graph) |
| Infrastructure | ⚠️ Basic | ❌ | ❌ | ❌ |

---

## Implementation Priorities

### Tier 1 — Already implemented in API
- `event_count`, `session_count`, `run_count`, `tool_call_count` per agent (in `GET /api/agents/:id`)
- Session list with `run_count`, `event_count` per session
- Run list with `tool_count`, `has_block` per run
- Event timeline per session/run

### Tier 2 — High value, implementable now

These require SQL-level JSON extraction from `event_data` but use existing data:

1. **Token usage per agent/session/run** — extract from `llm_output.event_data.usage`
2. **Run duration per run** — extract `durationMs` from `agent_end.event_data` or `date_diff`
3. **Model & provider breakdown** — already in top-level `model`/`provider` columns
4. **Block events per agent/session** — already in top-level `hook_decision`/`block_reason` columns
5. **Session type breakdown** — parse `session_key` third segment
6. **Tool call breakdown by tool name** — already in top-level `tool_name` column

### Tier 3 — Dashboard / aggregated views

These are derived from Tier 1+2 and need new API endpoints + UI components:

- **Agent overview dashboard** — tokens/costs/blocks/runs over time (time-series)
- **Tool usage heatmap** — which tools each agent uses most
- **Block analysis** — reasons, patterns, affected sessions
- **Session type pie** — Web UI vs HTTP API vs Cron vs Sub-agent
- **Model usage breakdown** — by model and provider

### Tier 4 — Requires OTEL instrumentation changes

Per-agent/session slicing of OTEL histogram metrics requires OpenClaw to add `openclaw.agentId` and `openclaw.sessionId` to the `attributes` map of `openclaw.run.duration_ms`, `openclaw.queue.wait_ms`, etc. Currently these have `attributes = {}`.

---

## Key SQL Patterns

### Extract token usage per session
```sql
SELECT
    agent_id,
    session_id,
    sum(to_int32(json_extract_string(event_data, 'usage', 'input')))  AS total_input_tokens,
    sum(to_int32(json_extract_string(event_data, 'usage', 'output'))) AS total_output_tokens,
    sum(to_int32(json_extract_string(event_data, 'usage', 'total')))  AS total_tokens
FROM table(agentshield_hook_events)
WHERE hook_name = 'llm_output'
GROUP BY agent_id, session_id
```

### Run duration from agent_end
```sql
SELECT
    agent_id, session_id, run_id,
    to_int32(json_extract_string(event_data, 'durationMs')) AS duration_ms
FROM table(agentshield_hook_events)
WHERE hook_name = 'agent_end'
  AND run_id != ''
```

### Block rate per agent
```sql
SELECT
    agent_id,
    count_if(hook_decision = 'block')                   AS blocks,
    count_if(hook_name = 'llm_output')                  AS llm_calls,
    if(llm_calls > 0, blocks / llm_calls, 0)            AS block_rate
FROM table(agentshield_hook_events)
GROUP BY agent_id
```

### Session type breakdown
```sql
SELECT
    agent_id,
    split_by_char(':', any_if(session_key, session_key != ''))[3] AS session_type,
    count() AS session_count
FROM table(agentshield_hook_events)
WHERE session_id != ''
GROUP BY agent_id, session_id
HAVING any_if(session_key, session_key != '') NOT LIKE 'temp:%'
   AND session_id NOT LIKE 'slug-generator-%'
   AND count_if(run_id != '') > 0
GROUP BY agent_id, session_type
```

### OTEL model usage per session (gauge stream)
```sql
SELECT
    attributes['openclaw.sessionKey'] AS session_key,
    attributes['openclaw.model']      AS model,
    sum(to_int64(attributes['openclaw.tokens.input']))  AS input_tokens,
    sum(to_int64(attributes['openclaw.tokens.output'])) AS output_tokens,
    sum(to_int64(attributes['openclaw.tokens.total']))  AS total_tokens
FROM table(otel_metrics_gauge)
WHERE metric_name = 'openclaw.model.usage'
GROUP BY session_key, model
```
