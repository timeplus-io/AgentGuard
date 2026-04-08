package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	"github.com/timeplus/agentshield/internal/timeplus"
)

// metricNameRe validates model names in query params.
// Allows colons for names like "kimi-k2.5:cloud".
var metricNameRe = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)

func parseFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case interface{ Float64() (float64, error) }:
		if f, err := n.Float64(); err == nil {
			return f
		}
	}
	return 0
}

func parseInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case interface{ Int64() (int64, error) }:
		if i, err := n.Int64(); err == nil {
			return i
		}
	}
	return 0
}

func bucketFn(granularity string) string {
	switch granularity {
	case "minute":
		return "to_start_of_minute"
	case "day":
		return "to_start_of_day"
	default:
		return "to_start_of_hour"
	}
}

// fmtTime formats a time.Time for use in Proton SQL WHERE clauses.
func fmtTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.999")
}

// addAgentFilter appends deployment_id / agent_id conditions to where.
// prefix is prepended to column names (e.g., "i." for join aliases).
func addAgentFilter(where []string, agentID, prefix string) []string {
	if agentID == "" {
		return where
	}
	depID, agID, _ := splitAgentID(agentID) // already validated by caller
	return append(where,
		fmt.Sprintf("%sdeployment_id = '%s'", prefix, depID),
		fmt.Sprintf("%sagent_id = '%s'", prefix, agID),
	)
}

// addTimeFilters appends since/until conditions to where.
func addTimeFilters(where []string, since, until, prefix string) []string {
	if since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			where = append(where, fmt.Sprintf("%sevent_time >= '%s'", prefix, fmtTime(t)))
		}
	}
	if until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			where = append(where, fmt.Sprintf("%sevent_time <= '%s'", prefix, fmtTime(t)))
		}
	}
	return where
}

// parseCommonParams validates shared query params (agent_id, model, since, until).
// Returns the raw validated strings or an error message to return as 400.
func parseCommonParams(c *gin.Context) (agentID, model, since, until, errMsg string) {
	agentID = c.Query("agent_id")
	model = c.Query("model")
	since = c.Query("since")
	until = c.Query("until")

	if agentID != "" {
		if _, _, ok := splitAgentID(agentID); !ok {
			errMsg = "invalid agent_id: expected deploymentId__agentId"
			return
		}
	}
	if model != "" && !metricNameRe.MatchString(model) {
		errMsg = "invalid model"
		return
	}
	if since != "" {
		if _, err := time.Parse(time.RFC3339, since); err != nil {
			errMsg = "invalid since: use RFC3339"
			return
		}
	}
	if until != "" {
		if _, err := time.Parse(time.RFC3339, until); err != nil {
			errMsg = "invalid until: use RFC3339"
			return
		}
	}
	return
}

// GetLLMMetrics handles GET /api/metrics/llm
//
// Query params: agent_id, model, since, until (RFC3339), granularity (minute|hour|day)
func GetLLMMetrics(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID, model, since, until, errMsg := parseCommonParams(c)
		if errMsg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
			return
		}
		granularity := c.DefaultQuery("granularity", "hour")

		// ── Shared dimension filters ──────────────────────────────────────────
		base := addAgentFilter(nil, agentID, "")
		if model != "" {
			base = append(base, fmt.Sprintf("model = '%s'", model))
		}
		base = addTimeFilters(base, since, until, "")

		combinedHooks := "hook_name IN ('llm_input', 'llm_output')"
		if len(base) > 0 {
			combinedHooks += "\n  AND " + strings.Join(base, "\n  AND ")
		}

		llmOutWhere := append([]string{"hook_name = 'llm_output'"}, base...)

		// ── Q1: tokens + success/fail in one pass ─────────────────────────────
		// sum(if(..., expr, 0)) is safe: json_extract on llm_input rows returns
		// "" → to_int64("") = 0 anyway, but explicit 0 guard is clearer.
		q1 := fmt.Sprintf(`
SELECT
    count_if(hook_name = 'llm_input')                                                                           AS total_calls,
    count_if(hook_name = 'llm_output')                                                                          AS success_calls,
    sum(if(hook_name = 'llm_output', to_int64(json_extract_string(event_data, 'usage', 'input')),  0))          AS input_tokens,
    sum(if(hook_name = 'llm_output', to_int64(json_extract_string(event_data, 'usage', 'output')), 0))          AS output_tokens,
    sum(if(hook_name = 'llm_output', to_int64(json_extract_string(event_data, 'usage', 'total')),  0))          AS total_tokens,
    sum(if(hook_name = 'llm_output', to_int64(json_extract_string(event_data, 'lastAssistant', 'usage', 'cacheRead')),  0)) AS cache_read_tokens,
    sum(if(hook_name = 'llm_output', to_int64(json_extract_string(event_data, 'lastAssistant', 'usage', 'cacheWrite')), 0)) AS cache_write_tokens
FROM table(agentshield_hook_events)
WHERE %s`, combinedHooks)

		rows1, err := timeplus.Query(&cfg.Timeplus, q1)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		var totalCalls, successCalls int
		var inputTokens, outputTokens, totalTokens, cacheRead, cacheWrite int64
		if len(rows1) > 0 {
			r := rows1[0]
			totalCalls = parseInt(r["total_calls"])
			successCalls = parseInt(r["success_calls"])
			inputTokens = parseInt64(r["input_tokens"])
			outputTokens = parseInt64(r["output_tokens"])
			totalTokens = parseInt64(r["total_tokens"])
			cacheRead = parseInt64(r["cache_read_tokens"])
			cacheWrite = parseInt64(r["cache_write_tokens"])
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

		// ── Q2: stop reason breakdown ─────────────────────────────────────────
		q2 := fmt.Sprintf(`
SELECT
    json_extract_string(event_data, 'lastAssistant', 'stopReason') AS stop_reason,
    count()                                                         AS cnt
FROM table(agentshield_hook_events)
WHERE %s
GROUP BY stop_reason`, strings.Join(llmOutWhere, "\n  AND "))

		rows2, err := timeplus.Query(&cfg.Timeplus, q2)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		stopReasons := make(map[string]int, len(rows2))
		for _, r := range rows2 {
			sr := parseStr(r["stop_reason"])
			if sr == "" {
				sr = "unknown"
			}
			stopReasons[sr] = parseInt(r["cnt"])
		}

		// ── Q3: LLM latency via self-join on run_id ───────────────────────────
		innerWhere := []string{"i.hook_name = 'llm_input'", "o.hook_name = 'llm_output'"}
		innerWhere = addAgentFilter(innerWhere, agentID, "i.")
		if model != "" {
			innerWhere = append(innerWhere, fmt.Sprintf("i.model = '%s'", model))
		}
		innerWhere = addTimeFilters(innerWhere, since, until, "i.")

		q3 := fmt.Sprintf(`
SELECT
    quantile(0.5)(llm_latency_ms)  AS p50_ms,
    quantile(0.95)(llm_latency_ms) AS p95_ms,
    quantile(0.99)(llm_latency_ms) AS p99_ms,
    avg(llm_latency_ms)            AS avg_ms,
    min(llm_latency_ms)            AS min_ms,
    max(llm_latency_ms)            AS max_ms,
    count()                        AS sample_count
FROM (
    SELECT date_diff('millisecond', i.event_time, o.event_time) AS llm_latency_ms
    FROM table(agentshield_hook_events) AS i
    JOIN table(agentshield_hook_events) AS o ON i.run_id = o.run_id
    WHERE %s
)`, strings.Join(innerWhere, "\n      AND "))

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

		// ── Q4: throughput time series ────────────────────────────────────────
		bfn := bucketFn(granularity)
		q4 := fmt.Sprintf(`
SELECT
    %s(event_time)                                                                    AS bucket,
    count()                                                                           AS llm_calls,
    sum(to_int64(json_extract_string(event_data, 'usage', 'input')))                  AS input_tokens,
    sum(to_int64(json_extract_string(event_data, 'usage', 'output')))                 AS output_tokens,
    sum(to_int64(json_extract_string(event_data, 'usage', 'total')))                  AS total_tokens
FROM table(agentshield_hook_events)
WHERE %s
GROUP BY bucket
ORDER BY bucket ASC`, bfn, strings.Join(llmOutWhere, "\n  AND "))

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

		// ── Assemble response ─────────────────────────────────────────────────
		filters := models.MetricsFilters{AgentID: agentID, Model: model}
		c.JSON(http.StatusOK, models.LLMMetrics{
			Period:  models.MetricsPeriod{Since: since, Until: until},
			Filters: filters,
			Tokens: models.TokenStats{
				Input:      inputTokens,
				Output:     outputTokens,
				Total:      totalTokens,
				CacheRead:  cacheRead,
				CacheWrite: cacheWrite,
			},
			Calls: models.LLMCallStats{
				Total:       totalCalls,
				Success:     successCalls,
				Failed:      failedCalls,
				SuccessRate: successRate, // nil when totalCalls == 0
			},
			StopReasons: stopReasons,
			LatencyMs:   latency,
			Throughput:  throughput,
		})
	}
}

// GetToolMetrics handles GET /api/metrics/tools
//
// Query params: agent_id, tool_name, since, until (RFC3339)
// Note: model is not a dimension for tool metrics.
func GetToolMetrics(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		agentID, _, since, until, errMsg := parseCommonParams(c)
		if errMsg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
			return
		}
		toolName := c.Query("tool_name")
		if toolName != "" && !idPartRe.MatchString(toolName) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tool_name"})
			return
		}

		// ── Build WHERE ───────────────────────────────────────────────────────
		base := addAgentFilter(nil, agentID, "")
		if toolName != "" {
			base = append(base, fmt.Sprintf("tool_name = '%s'", toolName))
		}
		base = addTimeFilters(base, since, until, "")

		countWhere := append([]string{"hook_name = 'tool_result_persist'"}, base...)
		latWhere := append([]string{"hook_name = 'after_tool_call'"}, base...)

		// ── Q1: count + success/fail per tool_name ────────────────────────────
		q1 := fmt.Sprintf(`
SELECT
    tool_name,
    count()                                                                    AS total_calls,
    count_if(json_extract_string(event_data, 'message', 'isError') = 'false') AS success_calls,
    count_if(json_extract_string(event_data, 'message', 'isError') = 'true')  AS failed_calls
FROM table(agentshield_hook_events)
WHERE %s
GROUP BY tool_name
ORDER BY total_calls DESC`, strings.Join(countWhere, "\n  AND "))

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

		// ── Q2: latency per tool_name ─────────────────────────────────────────
		q2 := fmt.Sprintf(`
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
WHERE %s
GROUP BY tool_name`, strings.Join(latWhere, "\n  AND "))

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

		// ── Merge and build response ──────────────────────────────────────────
		byTool := make([]models.ToolStat, 0, len(toolOrder))
		var sumTotal, sumSuccess, sumFailed int
		for _, tn := range toolOrder {
			cr := countByTool[tn]
			var sr *float64
			if cr.total > 0 {
				r := float64(cr.success) / float64(cr.total)
				sr = &r
			}
			sumTotal += cr.total
			sumSuccess += cr.success
			sumFailed += cr.failed
			byTool = append(byTool, models.ToolStat{
				ToolName:     tn,
				TotalCalls:   cr.total,
				SuccessCalls: cr.success,
				FailedCalls:  cr.failed,
				SuccessRate:  sr,
				LatencyMs:    latByTool[tn],
			})
		}

		var summaryRate *float64
		if sumTotal > 0 {
			r := float64(sumSuccess) / float64(sumTotal)
			summaryRate = &r
		}

		filters := models.MetricsFilters{AgentID: agentID, ToolName: toolName}
		c.JSON(http.StatusOK, models.ToolMetrics{
			Period:  models.MetricsPeriod{Since: since, Until: until},
			Filters: filters,
			Summary: models.ToolCallStats{
				TotalCalls:   sumTotal,
				SuccessCalls: sumSuccess,
				FailedCalls:  sumFailed,
				SuccessRate:  summaryRate,
			},
			ByTool: byTool,
		})
	}
}
