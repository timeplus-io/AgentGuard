# CIM Metrics Layer — Design Spec

**Date:** 2026-04-13  
**Status:** Approved  
**Scope:** Unified agent metrics stream for OpenClaw and Claude Code

---

## 1. Problem

All metric queries in `backend/internal/handlers/metrics.go` query `agentshield_hook_events` directly using OpenClaw-specific JSON paths (`lastAssistant.usage.input`, `durationMs`, `message.isError`). Claude Code stores equivalent data at different paths (`usage.input_tokens`, `usage.output_tokens`) and lacks some hooks entirely (`llm_input`, `tool_result_persist`). As a result:

- Token counts return 0 for all Claude Code agents
- LLM latency is unavailable for Claude Code (no `llm_input` hook)
- Tool success/fail metrics are unavailable for Claude Code (no `tool_result_persist` hook)
- Tool duration is unavailable from Claude Code hook payloads

---

## 2. Design

### 2.1 Stream: `agentshield_cim_metrics`

A narrow time-series metrics stream — one row per metric measurement — modeled after the OTel metrics format. Common identity dimensions are first-class columns for efficient filtering; metric-specific dimensions live in a `tags` map.

```sql
CREATE STREAM agentshield_cim_metrics (
  event_time    datetime64(3),
  metric_name   low_cardinality(string),             -- see §2.2
  metric_value  float64,
  agent_type    low_cardinality(string),             -- 'claudecode' | 'openclaw'
  agent_id      string,
  deployment_id low_cardinality(string),
  session_id    string,
  tags          map(low_cardinality(string), string) -- see §2.3
)
```

**Design rationale:**
- Narrow format (one row per metric) allows new metrics to be added without DDL changes
- Aligns with `otel_metrics_sum` schema — bridging OTel data into CIM metrics is trivial
- Common dimensions as columns: fast filtering on the most frequent query axes
- `metric_value float64` covers all numeric metrics (counts, tokens, durations, booleans as 0/1)

### 2.2 Metric Names

| `metric_name` | Unit | Emitted by | Description |
|--------------|------|------------|-------------|
| `llm.call` | count (1) | `mv_cim_metrics_openclaw_hooks`, `mv_cim_metrics_claudecode_otel` | One per LLM API call completion |
| `llm.input_tokens` | tokens | same | Input tokens for this call |
| `llm.output_tokens` | tokens | same | Output tokens for this call |
| `llm.total_tokens` | tokens | same | Total tokens (input + output) |
| `llm.cache_read_tokens` | tokens | same | Cache read tokens |
| `llm.cache_write_tokens` | tokens | same | Cache write (creation) tokens |
| `llm.latency_ms` | milliseconds | same | LLM API call duration |
| `tool.call` | count (1) | `mv_cim_metrics_openclaw_hooks`, `mv_cim_metrics_claudecode_hooks` | One per tool execution |
| `tool.success` | bool (1/0) | same | 1 = succeeded, 0 = failed |
| `tool.duration_ms` | milliseconds | same | Tool execution duration |

### 2.3 Standard Tags

| Tag key | Populated for | Example value |
|---------|--------------|---------------|
| `model` | all `llm.*` metrics | `claude-sonnet-4-6` |
| `provider` | all `llm.*` metrics | `anthropic` |
| `stop_reason` | `llm.call` | `end_turn`, `tool_calls`, `error` |
| `run_id` | all metrics | same as `session_id` for Claude Code; distinct run UUID for OpenClaw |
| `tool_name` | all `tool.*` metrics | `Bash`, `Write`, `bash` |

### 2.4 Materialized Views

Three MVs feed `agentshield_cim_metrics`. Each has exactly one source stream and one concern.

#### `mv_cim_metrics_openclaw_hooks`

**Source:** `agentshield_cim_event` WHERE `agent_type = 'openclaw'`  
**Covers:** OpenClaw LLM calls and tool calls

For **LLM calls** (triggered by `event_type = 'llm_response'`): emits 7 rows (call, input_tokens, output_tokens, total_tokens, cache_read_tokens, cache_write_tokens, latency_ms).

LLM latency is computed via a streaming join between `llm_request` and `llm_response` events on `run_id` within a 10-minute window. This preserves the existing latency computation that worked correctly for OpenClaw.

Token fields extracted from `raw_event_data`:
- `json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'input')` → `llm.input_tokens`
- `json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'output')` → `llm.output_tokens`
- `json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'totalTokens')` → `llm.total_tokens`
- `json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'cacheRead')` → `llm.cache_read_tokens`
- `json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'cacheWrite')` → `llm.cache_write_tokens`
- `json_value(raw_event_data, '$.lastAssistant.stopReason')` → `tags['stop_reason']`

For **tool calls** (triggered by `event_type = 'tool_complete'`): emits 3 rows (call, success, duration_ms). Duration comes from `tool_duration_ms` column already present in `agentshield_cim_event` (populated from `event_data.durationMs` by `mv_cim_openclaw`).

#### `mv_cim_metrics_claudecode_otel`

**Source:** `otel_logs`  
**Covers:** Claude Code LLM calls  
**Trigger:** rows where `log_attributes['event.name'] = 'claude_code.api_request'`

Each `api_request` log event represents one LLM API call — finer granularity than the `Stop` hook which fires once per agent turn (a turn may involve multiple API calls). Emits 7 metric rows per event.

Field mappings:
- `log_attributes['input_tokens']` → `llm.input_tokens`
- `log_attributes['output_tokens']` → `llm.output_tokens`
- `log_attributes['input_tokens'] + log_attributes['output_tokens']` → `llm.total_tokens`
- `log_attributes['duration_ms']` → `llm.latency_ms`
- `log_attributes['model']` → `tags['model']`
- `resource_attributes['agent.id']` (or `service.instance.id`) → `agent_id`

Cache tokens are not available in OTel logs for this event type — `llm.cache_read_tokens` and `llm.cache_write_tokens` rows are omitted (not emitted as 0 to avoid skewing sums).

#### `mv_cim_metrics_claudecode_hooks`

**Source:** `agentshield_cim_event` WHERE `agent_type = 'claudecode'`  
**Covers:** Claude Code tool calls  
**Trigger:** `event_type = 'tool_complete'`

Tool duration is computed via a streaming join between `tool_invoke` and `tool_complete` events on `tool_use_id` (extracted from `raw_event_data`) within a 10-minute window. `tool_success` comes from the `tool_success` boolean column already in `agentshield_cim_event`. Emits 3 metric rows per tool call (call, success, duration_ms).

---

## 3. API Handler Changes

### 3.1 `GetLLMMetrics` (`GET /api/metrics/llm`)

All 4 queries are rewritten to use `agentshield_cim_metrics`. No agent-specific branching.

**Q1 — Tokens + call counts** (replaces the `lastAssistant.usage.*` JSON extraction):

```sql
SELECT
  sum_if(metric_value, metric_name = 'llm.call')               AS total_calls,
  sum_if(metric_value, metric_name = 'llm.input_tokens')       AS input_tokens,
  sum_if(metric_value, metric_name = 'llm.output_tokens')      AS output_tokens,
  sum_if(metric_value, metric_name = 'llm.total_tokens')       AS total_tokens,
  sum_if(metric_value, metric_name = 'llm.cache_read_tokens')  AS cache_read_tokens,
  sum_if(metric_value, metric_name = 'llm.cache_write_tokens') AS cache_write_tokens
FROM table(agentshield_cim_metrics)
WHERE metric_name IN ('llm.call', 'llm.input_tokens', 'llm.output_tokens',
                      'llm.total_tokens', 'llm.cache_read_tokens', 'llm.cache_write_tokens')
  [AND agent_id = '...' AND deployment_id = '...' AND tags['model'] = '...' ...]
```

`success_calls` is derived from Q2 (stop reason breakdown): sum of `llm.call` rows where `tags['stop_reason'] != 'error'`. `failed_calls = total - success` computed in Go as before.

**Q2 — Stop reason breakdown**:

```sql
SELECT tags['stop_reason'] AS stop_reason, sum(metric_value) AS cnt
FROM table(agentshield_cim_metrics)
WHERE metric_name = 'llm.call'
  [AND filters]
GROUP BY stop_reason
```

**Q3 — LLM latency** (replaces the self-join on `agentshield_hook_events`):

```sql
SELECT
  quantile(0.5)(metric_value)  AS p50_ms,
  quantile(0.95)(metric_value) AS p95_ms,
  quantile(0.99)(metric_value) AS p99_ms,
  avg(metric_value)            AS avg_ms,
  min(metric_value)            AS min_ms,
  max(metric_value)            AS max_ms,
  count()                      AS sample_count
FROM table(agentshield_cim_metrics)
WHERE metric_name = 'llm.latency_ms'
  AND metric_value > 0
  [AND filters]
```

No join needed at query time — latency is pre-computed in the MV.

**Q4 — Throughput time-series**:

```sql
SELECT
  {bucket_fn}(event_time)                                              AS bucket,
  sum_if(metric_value, metric_name = 'llm.call')                      AS llm_calls,
  sum_if(metric_value, metric_name = 'llm.input_tokens')              AS input_tokens,
  sum_if(metric_value, metric_name = 'llm.output_tokens')             AS output_tokens,
  sum_if(metric_value, metric_name = 'llm.total_tokens')              AS total_tokens
FROM table(agentshield_cim_metrics)
WHERE metric_name IN ('llm.call', 'llm.input_tokens', 'llm.output_tokens', 'llm.total_tokens')
  [AND filters]
GROUP BY bucket ORDER BY bucket ASC
```

### 3.2 `GetToolMetrics` (`GET /api/metrics/tools`)

**Q1 — Counts + success/fail per tool** (replaces `tool_result_persist` + `message.isError`):

```sql
SELECT
  tags['tool_name']                                                    AS tool_name,
  to_int64(sum_if(metric_value, metric_name = 'tool.call'))           AS total_calls,
  to_int64(sum_if(metric_value, metric_name = 'tool.success'))        AS success_calls,
  to_int64(sum_if(metric_value, metric_name = 'tool.call'))
    - to_int64(sum_if(metric_value, metric_name = 'tool.success'))    AS failed_calls
FROM table(agentshield_cim_metrics)
WHERE metric_name IN ('tool.call', 'tool.success')
  [AND filters]
GROUP BY tool_name
ORDER BY total_calls DESC
```

**Q2 — Latency per tool** (replaces `after_tool_call` + `event_data.durationMs`):

```sql
SELECT
  tags['tool_name']           AS tool_name,
  quantile(0.5)(metric_value) AS p50_ms,
  quantile(0.95)(metric_value) AS p95_ms,
  quantile(0.99)(metric_value) AS p99_ms,
  avg(metric_value)           AS avg_ms,
  min(metric_value)           AS min_ms,
  max(metric_value)           AS max_ms,
  count()                     AS sample_count
FROM table(agentshield_cim_metrics)
WHERE metric_name = 'tool.duration_ms'
  AND metric_value > 0
  [AND filters]
GROUP BY tool_name
```

`metric_value > 0` excludes tool calls where duration was unavailable (tool calls that exceeded the 10-minute join window).

### 3.3 Filter helper changes

`addAgentFilter` works unchanged — `agent_id` and `deployment_id` are first-class columns. The `model` query param filter changes from `model = '...'` to `tags['model'] = '...'`. The `tool_name` filter changes from `tool_name = '...'` to `tags['tool_name'] = '...'`.

---

## 4. Startup Initialization

Added to `timeplus.Init()` after the existing CIM event stream steps:

```
(existing)  Create agentshield_cim_event
(existing)  Create mv_cim_claudecode, mv_cim_openclaw
(new) 18.   Create agentshield_cim_metrics
(new) 19.   Create mv_cim_metrics_openclaw_hooks
(new) 20.   Create mv_cim_metrics_claudecode_otel
(new) 21.   Create mv_cim_metrics_claudecode_hooks
```

Same `execIfNotExists` pattern — non-fatal if Timeplus unavailable at startup. New functions: `createCIMMetricsStream()` and `createCIMMetricsMVs()`.

---

## 5. Limitations

| Limitation | Cause | Impact |
|------------|-------|--------|
| Claude Code LLM metrics require OTel to be configured | Hook `Stop` event gives cumulative session tokens, not per-call; OTel logs give per-call data | If OTel not configured, Claude Code LLM metric rows are absent; handler returns empty rather than wrong data |
| Claude Code cache tokens not available | `claude_code.api_request` OTel log event does not include cache token fields | `llm.cache_read_tokens` and `llm.cache_write_tokens` will always be 0 for Claude Code |
| Tool calls > 10-min window get 0 duration | Streaming join window limit | Rare in practice; `metric_value > 0` filter excludes these from latency quantiles |
| MV forward-only | Timeplus materialized views do not backfill | Historical data already in `agentshield_hook_events` and `otel_logs` will not appear in `agentshield_cim_metrics` |
| `tags` map filtering is slower than column filtering | Map key lookup vs column index | Acceptable: `metric_name` + common columns do the heavy selectivity; tag filtering is secondary |

---

## 6. Files Changed

| File | Change |
|------|--------|
| `backend/internal/timeplus/init.go` | Add `ddlCIMMetrics`, `ddlCIMMetricsOpenClawMV`, `ddlCIMMetricsClaudeCodeOtelMV`, `ddlCIMMetricsClaudeCodeHooksMV`; add `createCIMMetricsStream()`, `createCIMMetricsMVs()`; wire into `Init()` |
| `backend/internal/handlers/metrics.go` | Rewrite all SQL in `GetLLMMetrics` and `GetToolMetrics` to query `agentshield_cim_metrics`; update `model` and `tool_name` filter helpers to use `tags[...]` syntax |
| `docs/design.md` | Add `agentshield_cim_metrics` to architecture diagram; add §2.5 CIM Metrics Layer section |
