package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// metricNameRe validates model names in query params.
// Allows colons for names like "kimi-k2.5:cloud".
var metricNameRe = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)

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
FROM table(agentguard_cim_metrics)
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

		// ── Q2: stop reason breakdown + successCalls ──────────────────────────
		q2Where := append([]string{"metric_name = 'llm.call'"}, base...)
		q2 := fmt.Sprintf(`
SELECT
    tags['stop_reason']         AS stop_reason,
    to_int64(sum(metric_value)) AS cnt
FROM table(agentguard_cim_metrics)
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
FROM table(agentguard_cim_metrics)
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
FROM table(agentguard_cim_metrics)
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
FROM table(agentguard_cim_metrics)
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
			failed := parseInt(r["failed_calls"])
			if failed < 0 {
				failed = 0
			}
			countByTool[tn] = countRow{
				total:   parseInt(r["total_calls"]),
				success: parseInt(r["success_calls"]),
				failed:  failed,
			}
			toolOrder = append(toolOrder, tn)
		}

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
FROM table(agentguard_cim_metrics)
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
