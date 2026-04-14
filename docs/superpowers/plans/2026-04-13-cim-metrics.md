# CIM Metrics Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unify agent metrics from OpenClaw and Claude Code into a single `agentshield_cim_metrics` stream so all metric API queries are agent-agnostic.

**Architecture:** Four materialized views feed `agentshield_cim_metrics`. Each uses ARRAY JOIN to emit one metric row per measurement per source event. OpenClaw gets two MVs (LLM + tool, sourced from `agentshield_cim_event`). Claude Code gets two MVs (LLM from `otel_logs`, tool from `agentshield_cim_event`). Both metric handlers are rewritten to query the unified stream with `sum_if(metric_value, metric_name = '...')` patterns.

**Tech Stack:** Go/Gin, Timeplus Proton (streaming SQL, ARRAY JOIN in materialized views).

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/timeplus/init.go` | Add 5 DDL constants (1 stream + 4 MVs); rewrite `createCIMMetricsMVs`; wire both functions into `Init()` |
| `backend/internal/handlers/metrics.go` | Rewrite all 6 queries in `GetLLMMetrics` and `GetToolMetrics`; update `model` and `tool_name` filter expressions |

**Note on existing stubs:** `init.go` already has `createCIMMetricsStream()` (line 186) and `createCIMMetricsMVs()` (lines 190–195), but they reference DDL constants that don't exist yet and `createCIMMetricsMVs` creates 2 wrong-named MVs. Task 1 adds the missing constants and fixes both functions.

---

### Task 1: Add `agentshield_cim_metrics` stream + 4 feeder MVs

**Files:**
- Modify: `backend/internal/timeplus/init.go`

- [ ] **Step 1: Add the stream DDL constant**

Append after `ddlCIMOpenClawMV` (after line 702):

```go
const ddlCIMMetrics = `
CREATE STREAM agentshield_cim_metrics
(
  event_time    datetime64(3),
  metric_name   low_cardinality(string),
  metric_value  float64,
  agent_type    low_cardinality(string),
  agent_id      string,
  deployment_id low_cardinality(string),
  session_id    string,
  tags          map(low_cardinality(string), string)
)`
```

- [ ] **Step 2: Add OpenClaw LLM MV DDL constant**

Tokens come from `raw_event_data.lastAssistant.usage.*`. LLM latency for OpenClaw requires a streaming self-join not used here; those rows are absent from the stream (empty latency result is better than wrong data).

```go
const ddlCIMMetricsOpenClawLlmMV = `
CREATE MATERIALIZED VIEW mv_cim_metrics_openclaw_llm
INTO agentshield_cim_metrics AS
SELECT
  event_time,
  metric_name,
  metric_value,
  'openclaw'  AS agent_type,
  agent_id,
  deployment_id,
  session_id,
  map(
    'model',       model,
    'provider',    provider,
    'stop_reason', json_value(raw_event_data, '$.lastAssistant.stopReason'),
    'run_id',      run_id
  ) AS tags
FROM agentshield_cim_event
ARRAY JOIN
  ['llm.call', 'llm.input_tokens', 'llm.output_tokens', 'llm.total_tokens',
   'llm.cache_read_tokens', 'llm.cache_write_tokens']                           AS metric_name,
  [
    to_float64(1),
    to_float64_or_zero(json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'input')),
    to_float64_or_zero(json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'output')),
    to_float64_or_zero(json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'totalTokens')),
    to_float64_or_zero(json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'cacheRead')),
    to_float64_or_zero(json_extract_string(raw_event_data, 'lastAssistant', 'usage', 'cacheWrite'))
  ]                                                                              AS metric_value
WHERE agent_type = 'openclaw'
  AND event_type = 'llm_response'`
```

- [ ] **Step 3: Add OpenClaw tools MV DDL constant**

`tool_duration_ms` is already populated in `agentshield_cim_event` by `mv_cim_openclaw` (from `event_data.durationMs`), so no join is needed.

```go
const ddlCIMMetricsOpenClawToolsMV = `
CREATE MATERIALIZED VIEW mv_cim_metrics_openclaw_tools
INTO agentshield_cim_metrics AS
SELECT
  event_time,
  metric_name,
  metric_value,
  'openclaw' AS agent_type,
  agent_id,
  deployment_id,
  session_id,
  map(
    'tool_name', tool_name,
    'run_id',    run_id
  ) AS tags
FROM agentshield_cim_event
ARRAY JOIN
  ['tool.call', 'tool.success', 'tool.duration_ms']  AS metric_name,
  [
    to_float64(1),
    if(tool_success, to_float64(1), to_float64(0)),
    to_float64(tool_duration_ms)
  ]                                                   AS metric_value
WHERE agent_type = 'openclaw'
  AND event_type = 'tool_complete'`
```

- [ ] **Step 4: Add Claude Code OTel LLM MV DDL constant**

Source is `otel_logs` filtered to `claude_code.api_request` events. Latency comes from `log_attributes['duration_ms']`. Cache tokens are not present in this event type and are not emitted.

```go
const ddlCIMMetricsClaudeCodeOtelMV = `
CREATE MATERIALIZED VIEW mv_cim_metrics_claudecode_otel
INTO agentshield_cim_metrics AS
SELECT
  _tp_time                             AS event_time,
  metric_name,
  metric_value,
  'claudecode'                         AS agent_type,
  resource_attributes['agent.id']      AS agent_id,
  resource_attributes['deployment.id'] AS deployment_id,
  resource_attributes['session.id']    AS session_id,
  map(
    'model',  log_attributes['model'],
    'run_id', resource_attributes['session.id']
  )                                    AS tags
FROM otel_logs
ARRAY JOIN
  ['llm.call', 'llm.input_tokens', 'llm.output_tokens',
   'llm.total_tokens', 'llm.latency_ms']                              AS metric_name,
  [
    to_float64(1),
    to_float64_or_zero(log_attributes['input_tokens']),
    to_float64_or_zero(log_attributes['output_tokens']),
    to_float64_or_zero(log_attributes['input_tokens'])
      + to_float64_or_zero(log_attributes['output_tokens']),
    to_float64_or_zero(log_attributes['duration_ms'])
  ]                                                                   AS metric_value
WHERE log_attributes['event.name'] = 'claude_code.api_request'`
```

- [ ] **Step 5: Add Claude Code hooks tools MV DDL constant**

`tool_duration_ms` is 0 for Claude Code events (set by `mv_cim_claudecode`). Computing duration requires a streaming self-join on `tool_use_id` which is not implemented here. `tool.duration_ms` rows are absent for Claude Code; the latency handler returns empty rather than wrong data.

```go
const ddlCIMMetricsClaudeCodeToolsMV = `
CREATE MATERIALIZED VIEW mv_cim_metrics_claudecode_tools
INTO agentshield_cim_metrics AS
SELECT
  event_time,
  metric_name,
  metric_value,
  'claudecode' AS agent_type,
  agent_id,
  deployment_id,
  session_id,
  map(
    'tool_name', tool_name,
    'run_id',    run_id
  ) AS tags
FROM agentshield_cim_event
ARRAY JOIN
  ['tool.call', 'tool.success']             AS metric_name,
  [
    to_float64(1),
    if(tool_success, to_float64(1), to_float64(0))
  ]                                         AS metric_value
WHERE agent_type = 'claudecode'
  AND event_type = 'tool_complete'`
```

- [ ] **Step 6: Rewrite `createCIMMetricsMVs` to create all 4 MVs**

Replace lines 190–195 (the current stub that creates 2 wrong-named MVs):

```go
func createCIMMetricsMVs(ctx context.Context, conn proton.Conn) error {
	mvs := []struct {
		name string
		ddl  string
	}{
		{"mv_cim_metrics_openclaw_llm",     ddlCIMMetricsOpenClawLlmMV},
		{"mv_cim_metrics_openclaw_tools",   ddlCIMMetricsOpenClawToolsMV},
		{"mv_cim_metrics_claudecode_otel",  ddlCIMMetricsClaudeCodeOtelMV},
		{"mv_cim_metrics_claudecode_tools", ddlCIMMetricsClaudeCodeToolsMV},
	}
	for _, mv := range mvs {
		if err := execIfNotExists(ctx, conn, "view", mv.name, mv.ddl); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 7: Wire into `Init()`**

After `createCIMMVs` call (line 38), add:

```go
	if err := createCIMMetricsStream(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating CIM metrics stream: %w", err)
	}
	if err := createCIMMetricsMVs(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating CIM metrics MVs: %w", err)
	}
```

- [ ] **Step 8: Compile check**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output (clean build).

- [ ] **Step 9: Commit**

```bash
git add internal/timeplus/init.go
git commit -m "feat(metrics): add agentshield_cim_metrics stream and 4 feeder MVs"
```

---

### Task 2: Rewrite `GetLLMMetrics` to query `agentshield_cim_metrics`

**Files:**
- Modify: `backend/internal/handlers/metrics.go`

The model filter changes from `model = '...'` to `tags['model'] = '...'`. `successCalls` moves from Q1 to Q2 (derived from stop-reason breakdown). The join in Q3 is replaced by a simple filter on `metric_name = 'llm.latency_ms'`.

- [ ] **Step 1: Replace the shared filter setup and Q1**

Find and replace the block starting at `// ── Shared dimension filters ──` through the `successRate` assignment (through line ~165). The old block sets `combinedHooks`, `llmOutWhere`, runs Q1, and computes `failedCalls`/`successRate`. Replace the entire block with:

```go
		// ── Shared dimension filters ──────────────────────────────────────────
		base := addAgentFilter(nil, agentID, "")
		if model != "" {
			base = append(base, fmt.Sprintf("tags['model'] = '%s'", model))
		}
		base = addTimeFilters(base, since, until, "")

		// ── Q1: token counts and total call count ─────────────────────────────
		llmNames := "'llm.call','llm.input_tokens','llm.output_tokens','llm.total_tokens','llm.cache_read_tokens','llm.cache_write_tokens'"
		q1Where := append([]string{fmt.Sprintf("metric_name IN (%s)", llmNames)}, base...)
		q1 := fmt.Sprintf(`
SELECT
    to_int64(sum_if(metric_value, metric_name = 'llm.call'))               AS total_calls,
    to_int64(sum_if(metric_value, metric_name = 'llm.input_tokens'))       AS input_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.output_tokens'))      AS output_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.total_tokens'))       AS total_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.cache_read_tokens'))  AS cache_read_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.cache_write_tokens')) AS cache_write_tokens
FROM table(agentshield_cim_metrics)
WHERE %s`, strings.Join(q1Where, "\n  AND "))

		rows1, err := timeplus.Query(&cfg.Timeplus, q1)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		var totalCalls int
		var inputTokens, outputTokens, totalTokens, cacheRead, cacheWrite int64
		if len(rows1) > 0 {
			r := rows1[0]
			totalCalls = parseInt(r["total_calls"])
			inputTokens = parseInt64(r["input_tokens"])
			outputTokens = parseInt64(r["output_tokens"])
			totalTokens = parseInt64(r["total_tokens"])
			cacheRead = parseInt64(r["cache_read_tokens"])
			cacheWrite = parseInt64(r["cache_write_tokens"])
		}
```

- [ ] **Step 2: Replace Q2 (stop reason breakdown) and compute `successCalls`**

Replace the existing Q2 block (from `// ── Q2:` through the closing `}` of the `stopReasons` loop):

```go
		// ── Q2: stop reason breakdown + successCalls ──────────────────────────
		q2Where := append([]string{"metric_name = 'llm.call'"}, base...)
		q2 := fmt.Sprintf(`
SELECT
    tags['stop_reason']         AS stop_reason,
    to_int64(sum(metric_value)) AS cnt
FROM table(agentshield_cim_metrics)
WHERE %s
GROUP BY stop_reason`, strings.Join(q2Where, "\n  AND "))

		rows2, err := timeplus.Query(&cfg.Timeplus, q2)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		stopReasons := make(map[string]int, len(rows2))
		var successCalls int
		for _, r := range rows2 {
			sr := parseStr(r["stop_reason"])
			if sr == "" {
				sr = "unknown"
			}
			cnt := parseInt(r["cnt"])
			stopReasons[sr] = cnt
			if sr != "error" {
				successCalls += cnt
			}
		}
		failedCalls := totalCalls - successCalls
		if failedCalls < 0 {
			failedCalls = 0
		}
		var successRate *float64
		if totalCalls > 0 {
			r := float64(successCalls) / float64(totalCalls)
			successRate = &r
		}
```

- [ ] **Step 3: Replace Q3 (LLM latency — no join needed)**

Replace the Q3 block (from `// ── Q3:` through the `latency` assignment). The inner join is gone; the pre-computed `llm.latency_ms` rows are queried directly:

```go
		// ── Q3: LLM latency ───────────────────────────────────────────────────
		q3Where := append([]string{"metric_name = 'llm.latency_ms'", "metric_value > 0"}, base...)
		q3 := fmt.Sprintf(`
SELECT
    quantile(0.5)(metric_value)  AS p50_ms,
    quantile(0.95)(metric_value) AS p95_ms,
    quantile(0.99)(metric_value) AS p99_ms,
    avg(metric_value)            AS avg_ms,
    min(metric_value)            AS min_ms,
    max(metric_value)            AS max_ms,
    count()                      AS sample_count
FROM table(agentshield_cim_metrics)
WHERE %s`, strings.Join(q3Where, "\n  AND "))

		rows3, err := timeplus.Query(&cfg.Timeplus, q3)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		var latency models.LatencyStats
		if len(rows3) > 0 {
			r := rows3[0]
			latency = models.LatencyStats{
				P50:     parseFloat64(r["p50_ms"]),
				P95:     parseFloat64(r["p95_ms"]),
				P99:     parseFloat64(r["p99_ms"]),
				Avg:     parseFloat64(r["avg_ms"]),
				Min:     parseFloat64(r["min_ms"]),
				Max:     parseFloat64(r["max_ms"]),
				Samples: parseInt(r["sample_count"]),
			}
		}
```

- [ ] **Step 4: Replace Q4 (throughput time series)**

Replace the Q4 block (from `// ── Q4:` through the closing `}` of the `throughput` loop):

```go
		// ── Q4: throughput time series ────────────────────────────────────────
		bfn := bucketFn(granularity)
		tsNames := "'llm.call','llm.input_tokens','llm.output_tokens','llm.total_tokens'"
		q4Where := append([]string{fmt.Sprintf("metric_name IN (%s)", tsNames)}, base...)
		q4 := fmt.Sprintf(`
SELECT
    %s(event_time)                                                             AS bucket,
    to_int64(sum_if(metric_value, metric_name = 'llm.call'))                  AS llm_calls,
    to_int64(sum_if(metric_value, metric_name = 'llm.input_tokens'))          AS input_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.output_tokens'))         AS output_tokens,
    to_int64(sum_if(metric_value, metric_name = 'llm.total_tokens'))          AS total_tokens
FROM table(agentshield_cim_metrics)
WHERE %s
GROUP BY bucket
ORDER BY bucket ASC`, bfn, strings.Join(q4Where, "\n  AND "))

		rows4, err := timeplus.Query(&cfg.Timeplus, q4)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		throughput := make([]models.LLMThroughputBucket, 0, len(rows4))
		for _, r := range rows4 {
			throughput = append(throughput, models.LLMThroughputBucket{
				Bucket:       parseTime(r["bucket"]),
				LLMCalls:     parseInt(r["llm_calls"]),
				InputTokens:  parseInt64(r["input_tokens"]),
				OutputTokens: parseInt64(r["output_tokens"]),
				TotalTokens:  parseInt64(r["total_tokens"]),
			})
		}
```

The `// ── Assemble response ──` block that follows is unchanged.

- [ ] **Step 5: Compile check**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output.

- [ ] **Step 6: Smoke test (requires running server + Timeplus)**

```bash
curl -s "http://localhost:8080/api/metrics/llm" | jq '{total_calls: .calls.total, input_tokens: .tokens.input, stop_reasons: .stop_reasons}'
```

Expected: JSON with those keys present; values may be 0 if no events yet.

- [ ] **Step 7: Commit**

```bash
git add internal/handlers/metrics.go
git commit -m "feat(metrics): rewrite GetLLMMetrics to query agentshield_cim_metrics"
```

---

### Task 3: Rewrite `GetToolMetrics` to query `agentshield_cim_metrics`

**Files:**
- Modify: `backend/internal/handlers/metrics.go`

The `tool_name` filter changes from `tool_name = '...'` to `tags['tool_name'] = '...'`. Queries change from hook-event-specific joins to `sum_if` on the unified stream.

- [ ] **Step 1: Replace the WHERE setup and Q1**

Replace the block starting `// ── Build WHERE ───` through the closing `}` of the Q1 `for _, r := range rows1` loop (around line 344):

```go
		// ── Build WHERE ───────────────────────────────────────────────────────
		base := addAgentFilter(nil, agentID, "")
		if toolName != "" {
			base = append(base, fmt.Sprintf("tags['tool_name'] = '%s'", toolName))
		}
		base = addTimeFilters(base, since, until, "")

		// ── Q1: count + success/fail per tool_name ────────────────────────────
		q1Where := append([]string{"metric_name IN ('tool.call', 'tool.success')"}, base...)
		q1 := fmt.Sprintf(`
SELECT
    tags['tool_name']                                                           AS tool_name,
    to_int64(sum_if(metric_value, metric_name = 'tool.call'))                  AS total_calls,
    to_int64(sum_if(metric_value, metric_name = 'tool.success'))               AS success_calls,
    (to_int64(sum_if(metric_value, metric_name = 'tool.call'))
      - to_int64(sum_if(metric_value, metric_name = 'tool.success')))          AS failed_calls
FROM table(agentshield_cim_metrics)
WHERE %s
GROUP BY tool_name
ORDER BY total_calls DESC`, strings.Join(q1Where, "\n  AND "))

		rows1, err := timeplus.Query(&cfg.Timeplus, q1)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		type countRow struct{ total, success, failed int }
		countByTool := make(map[string]countRow, len(rows1))
		toolOrder := make([]string, 0, len(rows1))
		for _, r := range rows1 {
			tn := parseStr(r["tool_name"])
			countByTool[tn] = countRow{
				total:   parseInt(r["total_calls"]),
				success: parseInt(r["success_calls"]),
				failed:  parseInt(r["failed_calls"]),
			}
			toolOrder = append(toolOrder, tn)
		}
```

- [ ] **Step 2: Replace Q2 (latency per tool)**

Replace the Q2 block (from `// ── Q2:` through the closing `}` of the `latByTool` loop):

```go
		// ── Q2: latency per tool_name ─────────────────────────────────────────
		q2Where := append([]string{"metric_name = 'tool.duration_ms'", "metric_value > 0"}, base...)
		q2 := fmt.Sprintf(`
SELECT
    tags['tool_name']            AS tool_name,
    quantile(0.5)(metric_value)  AS p50_ms,
    quantile(0.95)(metric_value) AS p95_ms,
    quantile(0.99)(metric_value) AS p99_ms,
    avg(metric_value)            AS avg_ms,
    min(metric_value)            AS min_ms,
    max(metric_value)            AS max_ms,
    count()                      AS sample_count
FROM table(agentshield_cim_metrics)
WHERE %s
GROUP BY tool_name`, strings.Join(q2Where, "\n  AND "))

		rows2, err := timeplus.Query(&cfg.Timeplus, q2)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		latByTool := make(map[string]models.LatencyStats, len(rows2))
		for _, r := range rows2 {
			tn := parseStr(r["tool_name"])
			latByTool[tn] = models.LatencyStats{
				P50:     parseFloat64(r["p50_ms"]),
				P95:     parseFloat64(r["p95_ms"]),
				P99:     parseFloat64(r["p99_ms"]),
				Avg:     parseFloat64(r["avg_ms"]),
				Min:     parseFloat64(r["min_ms"]),
				Max:     parseFloat64(r["max_ms"]),
				Samples: parseInt(r["sample_count"]),
			}
		}
```

The `// ── Merge and build response ──` block is unchanged.

- [ ] **Step 3: Compile check**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output.

- [ ] **Step 4: Smoke test (requires running server + Timeplus)**

```bash
curl -s "http://localhost:8080/api/metrics/tools" | jq '.summary'
```

Expected: JSON with `total_calls`, `success_calls`, `failed_calls` keys.

- [ ] **Step 5: Commit**

```bash
git add internal/handlers/metrics.go
git commit -m "feat(metrics): rewrite GetToolMetrics to query agentshield_cim_metrics"
```

---

## Known Limitations

| Limitation | Impact |
|------------|--------|
| OpenClaw LLM latency not in MV (streaming self-join not implemented) | `llm.latency_ms` absent for OpenClaw; latency handler returns empty for OpenClaw-only deployments |
| Claude Code tool duration not in MV (streaming join on `tool_use_id` not implemented) | `tool.duration_ms` absent for Claude Code; tool latency handler returns empty for Claude Code tools |
| Claude Code cache tokens not in OTel `api_request` event | `llm.cache_read_tokens` / `llm.cache_write_tokens` always 0 for Claude Code |
| MVs are forward-only | Historical data in `agentshield_hook_events` / `otel_logs` does not appear in `agentshield_cim_metrics` |
| Claude Code LLM calls always counted as successful | `claude_code.api_request` OTel log event does not carry a `stop_reason` field; all Claude Code `llm.call` rows get an empty `stop_reason` tag, which the handler maps to `"unknown"` and counts as success. `calls.failed` will always be 0 for Claude Code. |
