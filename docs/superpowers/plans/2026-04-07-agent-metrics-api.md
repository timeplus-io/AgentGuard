# Agent Metrics API Design

> **Status:** Design — 2026-04-07  
> **Data validated against:** live `agentshield_hook_events` in Timeplus (Proton 3.2.1)

## Scope

This document defines five operational metric groups for AI agent observability:

1. **LLM Token Counts** — input, output, total tokens per call
2. **LLM Throughput** — tokens and calls over time
3. **LLM Latency** — time from LLM request to response
4. **LLM Call Success/Fail** — outcome classification
5. **Tool Call Count, Latency, Success/Fail** — per tool name

All metrics support four dimension scopes: **global**, **per agent**, **per model**, **per tool name**.

---

## Source Data

All metrics derive from `agentshield_hook_events`. The relevant hooks and their data:

| Hook | When it fires | Key fields |
|---|---|---|
| `llm_input` | LLM API call dispatched | `run_id`, `agent_id`, `model`, `provider`, `session_id` |
| `llm_output` | LLM API response received | `run_id`, `agent_id`, `model`, `provider` + `event_data.usage.{input,output,total}`, `event_data.durationMs` |
| `before_tool_call` | Tool execution starts | `run_id`, `agent_id`, `tool_name` + `event_data.toolCallId` |
| `after_tool_call` | Tool execution ends | `tool_name`, `agent_id` + `event_data.{toolCallId,durationMs,result.isError}` |
| `tool_result_persist` | Tool result saved to context | `tool_name`, `agent_id` + `event_data.{toolCallId,message.isError}` |

**Important columns available at the top level** (no JSON extraction needed for filtering/grouping):

| Column | Type | Used for |
|---|---|---|
| `agent_id` | string | per-agent dimension |
| `deployment_id` | low_cardinality | per-deployment filter |
| `model` | string | per-model dimension |
| `provider` | string | per-provider filter |
| `tool_name` | string | per-tool dimension |
| `run_id` | string | pairing llm_input ↔ llm_output for latency |
| `hook_decision` | low_cardinality | block detection |

---

## Metric Definitions

### M1 — LLM Token Counts

**What it measures:** Tokens consumed per LLM call, aggregated over any dimension.

**Source:** `hook_name = 'llm_output'`, `event_data.usage.{input, output, total, cacheRead, cacheWrite}`

**Validated fields from live data:**
```json
{
  "usage": {
    "input": 8996,
    "output": 5,
    "total": 9001
  },
  "lastAssistant": {
    "usage": {
      "cacheRead": 0,
      "cacheWrite": 0,
      "cost": { "total": 0 }
    }
  }
}
```

**Base SQL (global):**
```sql
SELECT
    count()                                                                             AS llm_calls,
    sum(to_int64(json_extract_string(event_data, 'usage', 'input')))                   AS input_tokens,
    sum(to_int64(json_extract_string(event_data, 'usage', 'output')))                  AS output_tokens,
    sum(to_int64(json_extract_string(event_data, 'usage', 'total')))                   AS total_tokens,
    sum(to_int64(json_extract_string(event_data, 'lastAssistant', 'usage', 'cacheRead'))) AS cache_read_tokens,
    sum(to_int64(json_extract_string(event_data, 'lastAssistant', 'usage', 'cacheWrite'))) AS cache_write_tokens
FROM table(agentshield_hook_events)
WHERE hook_name = 'llm_output'
  -- append time range and dimension filters from table below
```

**Dimension filters appended to WHERE:**

| Scope | Append |
|---|---|
| Global | *(none)* |
| Per agent | `AND agent_id = '{agent_id}'` |
| Per model | `AND model = '{model}'` |
| Per agent + model | `AND agent_id = '{agent_id}' AND model = '{model}'` |

**GROUP BY for breakdown tables:**

| Breakdown | Append |
|---|---|
| By agent | `GROUP BY agent_id ORDER BY total_tokens DESC` |
| By model | `GROUP BY model ORDER BY total_tokens DESC` |
| By agent + model | `GROUP BY agent_id, model ORDER BY total_tokens DESC` |

---

### M2 — LLM Throughput (time series)

**What it measures:** LLM calls and tokens over time, at configurable granularity (minute / hour / day).

**Source:** `hook_name = 'llm_output'`

**Base SQL:**
```sql
SELECT
    to_start_of_hour(event_time)                                                       AS bucket,
    count()                                                                             AS llm_calls,
    sum(to_int64(json_extract_string(event_data, 'usage', 'input')))                   AS input_tokens,
    sum(to_int64(json_extract_string(event_data, 'usage', 'output')))                  AS output_tokens,
    sum(to_int64(json_extract_string(event_data, 'usage', 'total')))                   AS total_tokens
FROM table(agentshield_hook_events)
WHERE hook_name = 'llm_output'
  -- dimension filters
GROUP BY bucket
ORDER BY bucket ASC
```

Replace `to_start_of_hour` with `to_start_of_minute` or `to_start_of_day` based on the `granularity` query param.

**Observed ranges from live data:**
- Calls per minute: 1–5
- Total tokens per call: 9,001–18,106 (heartbeat = ~9K, code task = ~18K)

---

### M3 — LLM Latency

**What it measures:** Wall-clock time from when the LLM request was dispatched (`llm_input`) to when the response arrived (`llm_output`), joined on `run_id`.

**Source:** Self-join on `run_id` between `llm_input` and `llm_output`.

**Why run_id works:** Both hooks fire in the same run. `llm_input` fires first, `llm_output` fires after the API responds. The `run_id` is identical for both events.

**Validated latency values from live data:**
- Min: 752ms (heartbeat / short responses)  
- Max: 2,985ms (multi-tool runs)
- Range observed: 752ms–2,985ms (ollama/kimi-k2.5)

**Base SQL:**
```sql
SELECT
    quantile(0.5)(llm_latency_ms)  AS p50_ms,
    quantile(0.95)(llm_latency_ms) AS p95_ms,
    quantile(0.99)(llm_latency_ms) AS p99_ms,
    avg(llm_latency_ms)            AS avg_ms,
    min(llm_latency_ms)            AS min_ms,
    max(llm_latency_ms)            AS max_ms,
    count()                        AS sample_count
FROM (
    SELECT
        i.run_id,
        i.agent_id,
        i.model,
        date_diff('millisecond', i.event_time, o.event_time) AS llm_latency_ms
    FROM table(agentshield_hook_events) AS i
    JOIN table(agentshield_hook_events) AS o ON i.run_id = o.run_id
    WHERE i.hook_name = 'llm_input'
      AND o.hook_name = 'llm_output'
      -- dimension filters applied to inner query (use i.agent_id, i.model)
)
```

**Dimension filters in inner WHERE:**

| Scope | Append (inner) |
|---|---|
| Per agent | `AND i.agent_id = '{agent_id}'` |
| Per model | `AND i.model = '{model}'` |

**GROUP BY for breakdown (outer query):**

| Breakdown | Outer GROUP BY |
|---|---|
| By agent | `GROUP BY agent_id` |
| By model | `GROUP BY model` |

---

### M4 — LLM Call Success / Fail

**What it measures:** Whether each LLM call completed successfully or failed.

**Success definition:** `llm_output` event fires for the run → LLM responded.  
**Failure definition:** `llm_input` event fires but no `llm_output` for the same `run_id` within the time window → request timed out, network error, or rate limit.

**Note on live data:** All 63 observed `llm_input` events have a matching `llm_output` (0 failures). The SQL below handles the general case correctly when failures do occur.

**Base SQL:**
```sql
SELECT
    count_if(hook_name = 'llm_input')                                      AS total_calls,
    count_if(hook_name = 'llm_output')                                     AS success_calls,
    count_if(hook_name = 'llm_input') - count_if(hook_name = 'llm_output') AS failed_calls,
    if(count_if(hook_name = 'llm_input') > 0,
       count_if(hook_name = 'llm_output') / count_if(hook_name = 'llm_input'),
       1.0)                                                                 AS success_rate
FROM table(agentshield_hook_events)
WHERE hook_name IN ('llm_input', 'llm_output')
  -- dimension filters
```

**Additional signal: stop reason** (from `llm_output.event_data.lastAssistant.stopReason`):

| stopReason | Meaning |
|---|---|
| `stop` | Normal completion |
| `toolUse` | LLM requested tool call (normal, more turns follow) |
| `length` | Hit context window limit |
| `error` | LLM API error embedded in response |

```sql
SELECT
    json_extract_string(event_data, 'lastAssistant', 'stopReason') AS stop_reason,
    count() AS cnt
FROM table(agentshield_hook_events)
WHERE hook_name = 'llm_output'
GROUP BY stop_reason
```

**Dimension filters:** Same pattern as M1 — append `AND agent_id`, `AND model` to WHERE.

---

### M5 — Tool Call Count, Latency, Success / Fail

**What it measures:** Tool execution count, latency, and outcome per tool name.

**Sources:**
- `after_tool_call`: carries `event_data.durationMs` (reported by OpenClaw) and `event_data.toolCallId`
- `tool_result_persist`: carries `event_data.message.isError` (true/false) — the authoritative success/fail flag

**Validated from live data:**
- `after_tool_call.event_data.durationMs`: 18ms (read), 26ms (write), 57ms (edit)
- `tool_result_persist.event_data.message.isError`: `false` for all 55 observed tool calls
- Tool types observed: `read` (40), `write` (7), `exec` (4), `edit` (2), `sessions_yield` (1), `sessions_spawn` (1)

#### Tool Call Count + Success/Fail

**Source:** `tool_result_persist` (has `isError` per call)

```sql
SELECT
    tool_name,
    count()                                                                     AS total_calls,
    count_if(json_extract_string(event_data, 'message', 'isError') = 'false')  AS success_calls,
    count_if(json_extract_string(event_data, 'message', 'isError') = 'true')   AS failed_calls,
    if(count() > 0,
       count_if(json_extract_string(event_data, 'message', 'isError') = 'false') / count(),
       1.0)                                                                     AS success_rate
FROM table(agentshield_hook_events)
WHERE hook_name = 'tool_result_persist'
  -- dimension filters
GROUP BY tool_name
ORDER BY total_calls DESC
```

**For global count (no tool_name grouping), remove GROUP BY and tool_name in SELECT.**

#### Tool Call Latency

**Source:** `after_tool_call` — `event_data.durationMs` (ms, reported by OpenClaw natively)

```sql
SELECT
    tool_name,
    quantile(0.5)(to_int64(json_extract_string(event_data, 'durationMs')))  AS p50_ms,
    quantile(0.95)(to_int64(json_extract_string(event_data, 'durationMs'))) AS p95_ms,
    quantile(0.99)(to_int64(json_extract_string(event_data, 'durationMs'))) AS p99_ms,
    avg(to_int64(json_extract_string(event_data, 'durationMs')))            AS avg_ms,
    min(to_int64(json_extract_string(event_data, 'durationMs')))            AS min_ms,
    max(to_int64(json_extract_string(event_data, 'durationMs')))            AS max_ms,
    count()                                                                  AS sample_count
FROM table(agentshield_hook_events)
WHERE hook_name = 'after_tool_call'
  -- dimension filters
GROUP BY tool_name
ORDER BY avg_ms DESC
```

**Dimension filters for tool metrics:**

| Scope | Append |
|---|---|
| Global | *(no GROUP BY changes; keep GROUP BY tool_name for breakdown)* |
| Per agent | `AND agent_id = '{agent_id}'` |
| Per tool | `AND tool_name = '{tool_name}'` (remove GROUP BY tool_name) |
| Per agent + tool | Both above |

**Note:** Tool metrics do not have a "per model" dimension because tools are invoked by the agent, not by a specific model call. If needed, the `run_id` can be used to join back to `llm_output` to associate a model.

---

## API Design

### Endpoints

```
GET /api/metrics/llm
GET /api/metrics/tools
```

### Query Parameters

| Param | Type | Description |
|---|---|---|
| `agent_id` | string | Filter to one agent (`deploymentId__agentId` format) |
| `model` | string | Filter to one model name (LLM metrics only) |
| `tool_name` | string | Filter to one tool name (tool metrics only) |
| `since` | RFC3339 | Start of time range |
| `until` | RFC3339 | End of time range |
| `granularity` | `minute`\|`hour`\|`day` | Bucket size for throughput time series (default: `hour`) |

### `GET /api/metrics/llm` Response

```json
{
  "period": {
    "since": "2026-04-07T00:00:00Z",
    "until": "2026-04-08T00:00:00Z"
  },
  "filters": {
    "agent_id": "local__main",
    "model": "kimi-k2.5:cloud"
  },
  "tokens": {
    "input":       123456,
    "output":      789,
    "total":       124245,
    "cache_read":  0,
    "cache_write": 0
  },
  "calls": {
    "total":        63,
    "success":      63,
    "failed":       0,
    "success_rate": 1.0
  },
  "stop_reasons": {
    "stop":    62,
    "toolUse":  1
  },
  "latency_ms": {
    "p50": 1200,
    "p95": 2985,
    "p99": 2985,
    "avg": 1580,
    "min": 752,
    "max": 2985,
    "samples": 63
  },
  "throughput": [
    {
      "bucket":        "2026-04-08T04:00:00Z",
      "llm_calls":     2,
      "input_tokens":  17992,
      "output_tokens": 10,
      "total_tokens":  18002
    }
  ]
}
```

### `GET /api/metrics/tools` Response

```json
{
  "period": {
    "since": "2026-04-07T00:00:00Z",
    "until": "2026-04-08T00:00:00Z"
  },
  "filters": {
    "agent_id": "local__main"
  },
  "summary": {
    "total_calls":   55,
    "success_calls": 55,
    "failed_calls":  0,
    "success_rate":  1.0
  },
  "by_tool": [
    {
      "tool_name":     "read",
      "total_calls":   40,
      "success_calls": 40,
      "failed_calls":  0,
      "success_rate":  1.0,
      "latency_ms": {
        "p50": 30,
        "p95": 580,
        "p99": 580,
        "avg": 63,
        "min": 18,
        "max": 580,
        "samples": 40
      }
    },
    {
      "tool_name":     "write",
      "total_calls":   7,
      "success_calls": 7,
      "failed_calls":  0,
      "success_rate":  1.0,
      "latency_ms": { "p50": 26, "p95": 26, "avg": 26, "min": 26, "max": 26, "samples": 7 }
    }
  ]
}
```

---

## Backend Implementation Notes

### File changes required

| File | Change |
|---|---|
| `backend/internal/handlers/metrics.go` | New file — `GetLLMMetrics`, `GetToolMetrics` handlers |
| `backend/internal/models/models.go` | Add `LLMMetrics`, `ToolMetrics`, `ToolStat` structs |
| `backend/cmd/server/main.go` | Register `GET /api/metrics/llm` and `GET /api/metrics/tools` |

### Handler structure

Each handler runs two to three queries in sequence:

**`GetLLMMetrics`:**
1. Token totals query (M1)
2. Success/fail + stop reason query (M4)
3. Latency query (M3) — self-join; can be skipped if `skip_latency=true`
4. Throughput time-series query (M2)

**`GetToolMetrics`:**
1. Count + success/fail per tool (from `tool_result_persist`) (M5a)
2. Latency per tool (from `after_tool_call`) (M5b)
3. Merge by `tool_name`

### Proton-specific notes

- `quantile(p)(expr)` is supported — use for p50/p95/p99
- Self-join: `FROM table(T) AS a JOIN table(T) AS b ON a.col = b.col` — validated working
- `json_extract_string(col, 'key1', 'key2')` for nested JSON path — validated working
- `to_int64()` needed when summing JSON-extracted string numbers
- Time bucketing: `to_start_of_minute()`, `to_start_of_hour()`, `to_start_of_day()`
- All time filters use format: `event_time >= '2006-01-02 15:04:05.999'` (UTC, no timezone)

### SQL injection safety

Apply same validation as existing handlers:
- `agent_id` → split with `idPartRe = ^[a-zA-Z0-9._-]+$` (deployment + agent parts)
- `model` → validate with `idPartRe` (model names like `kimi-k2.5:cloud` need colon — use `modelRe = ^[a-zA-Z0-9._:/-]+$`)
- `tool_name` → validate with `idPartRe`
- `since`/`until` → parse with `time.Parse(time.RFC3339, ...)` then format safely

---

## Dimension Compatibility Matrix

| Metric | Global | Per Agent | Per Model | Per Tool |
|---|---|---|---|---|
| Token counts | ✅ | ✅ | ✅ | ❌ |
| Throughput | ✅ | ✅ | ✅ | ❌ |
| LLM latency | ✅ | ✅ | ✅ | ❌ |
| LLM success/fail | ✅ | ✅ | ✅ | ❌ |
| Tool call count | ✅ | ✅ | ❌ | ✅ |
| Tool call latency | ✅ | ✅ | ❌ | ✅ |
| Tool success/fail | ✅ | ✅ | ❌ | ✅ |

**Model is not a natural dimension for tool metrics** — tools are agent actions, not model actions. Joining tool events back to their triggering model (via `run_id` → `llm_output.model`) is possible but adds latency and complexity; defer to a future iteration if needed.
