package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/mock"
	"github.com/timeplus/agentshield/internal/models"
	tp "github.com/timeplus/agentshield/internal/timeplus"
)

// pricingKeyRe validates provider and model values used in SQL path params.
var pricingKeyRe = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)

// GetCost handles GET /api/cost.
// Runs three Proton queries to compute cost KPIs, daily trend, and token breakdown.
// Budget policies and anomalies remain as mock data (out of scope).
func GetCost(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		now := time.Now().UTC()
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).
			Format("2006-01-02 15:04:05")

		// All-time per-agent costs (for cost cards and AvgTokenCost KPI).
		allCosts, err := queryCostByAgent(&cfg.Timeplus, "")
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Month-to-date per-agent costs (for MonthToDate + Forecasted KPIs).
		mtdCosts, err := queryCostByAgent(&cfg.Timeplus, monthStart)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Daily spend trend, last 30 days.
		daily, err := queryCostDailyTrend(&cfg.Timeplus)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Token-type cost breakdown (input / output / cache-read).
		breakdown, err := queryCostTokenBreakdown(&cfg.Timeplus)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Derive KPIs from query results.
		var mtd float64
		for _, a := range mtdCosts {
			mtd += a.Cost
		}

		elapsedDays := float64(now.Day())
		if elapsedDays < 1 {
			elapsedDays = 1
		}
		forecasted := mtd / elapsedDays * 30

		var totalCost float64
		var totalTokens int64
		for _, a := range allCosts {
			totalCost += a.Cost
			totalTokens += a.InputTokens + a.OutputTokens + a.CacheTokens
		}
		avgTokenCost := 0.0
		if totalTokens > 0 {
			avgTokenCost = totalCost / float64(totalTokens) * 1_000_000
		}

		// Cache savings estimate: cache-read tokens cost ~10% of input price on average.
		// Savings = what would have been paid at input price minus what was actually paid.
		cacheSavings := 0.0
		for _, b := range breakdown {
			if b.Label == "Cache Tokens" {
				// b.Amount is what was paid. The "full" input price would be ~10x higher.
				cacheSavings = b.Amount * 9
			}
		}

		c.JSON(http.StatusOK, models.CostResponse{
			MonthToDate:     mtd,
			Forecasted:      forecasted,
			AvgTokenCost:    avgTokenCost,
			PolicyBreaches:  2, // mock — out of scope
			DailySpend:      daily,
			TokenBreakdown:  breakdown,
			CacheSavings:    cacheSavings,
			WastedSpend:     0,
			AgentModelCosts: allCosts,
			BudgetPolicies:  mock.BudgetPolicies,
			Anomalies:       mock.CostAnomalies,
		})
	}
}

// ListPricing handles GET /api/cost/pricing.
func ListPricing(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rows, err := tp.Query(&cfg.Timeplus, `
			SELECT provider, model, input_cost_per_million, output_cost_per_million,
			       cache_read_cost_per_million, updated_at
			FROM table(agentshield_pricing)
			ORDER BY provider ASC, model ASC`)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		entries := make([]models.PricingEntry, 0, len(rows))
		for _, r := range rows {
			entries = append(entries, models.PricingEntry{
				Provider:                parseStr(r["provider"]),
				Model:                   parseStr(r["model"]),
				InputCostPerMillion:     parseFloat64(r["input_cost_per_million"]),
				OutputCostPerMillion:    parseFloat64(r["output_cost_per_million"]),
				CacheReadCostPerMillion: parseFloat64(r["cache_read_cost_per_million"]),
				UpdatedAt:               parseStr(r["updated_at"]),
			})
		}
		c.JSON(http.StatusOK, models.PricingListResponse{Pricing: entries})
	}
}

// UpsertPricing handles POST /api/cost/pricing.
// Inserts a row into the mutable stream; Proton upserts by PRIMARY KEY (provider, model).
func UpsertPricing(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body models.PricingEntry
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		if body.Provider == "" || body.Model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "provider and model are required"})
			return
		}

		sql := fmt.Sprintf(
			`INSERT INTO agentshield_pricing
			 (provider, model, input_cost_per_million, output_cost_per_million, cache_read_cost_per_million, updated_at)
			 VALUES ('%s', '%s', %f, %f, %f, now64(3))`,
			escapeSQL(body.Provider),
			escapeSQL(body.Model),
			body.InputCostPerMillion,
			body.OutputCostPerMillion,
			body.CacheReadCostPerMillion,
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upsert failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// DeletePricing handles DELETE /api/cost/pricing/:provider/:model.
func DeletePricing(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		provider := c.Param("provider")
		model := c.Param("model")
		if provider == "" || model == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "provider and model are required"})
			return
		}
		if !pricingKeyRe.MatchString(provider) || !pricingKeyRe.MatchString(model) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider or model"})
			return
		}

		sql := fmt.Sprintf(
			`DELETE FROM agentshield_pricing WHERE provider = '%s' AND model = '%s'`,
			escapeSQL(provider),
			escapeSQL(model),
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": true})
	}
}

// ── Query helpers ─────────────────────────────────────────────────────────────

// queryCostByAgent returns cost aggregated by (agent_id, deployment_id, provider, model).
// Pass since as "2006-01-02 15:04:05" UTC to filter by event_time; empty string = all time.
func queryCostByAgent(cfg *config.TimeplusConfig, since string) ([]models.AgentModelCost, error) {
	where := []string{"e.hook_name = 'llm_output'"}
	if since != "" {
		where = append(where, fmt.Sprintf("e.event_time >= '%s'", since))
	}

	q := fmt.Sprintf(`
SELECT
  e.agent_id,
  e.deployment_id,
  e.provider,
  e.model,
  count()                                                                                     AS request_count,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'input')))                         AS input_tokens,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'output')))                        AS output_tokens,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))    AS cache_tokens,
  (
    sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'input')))
      * any(p.input_cost_per_million)
    + sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'output')))
      * any(p.output_cost_per_million)
    + sum(to_int64_or_zero(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))
      * any(p.cache_read_cost_per_million)
  ) / 1000000.0                                                                               AS total_cost
FROM table(agentshield_hook_events) AS e
LEFT JOIN table(agentshield_pricing) AS p
  ON e.provider = p.provider AND e.model = p.model
WHERE %s
GROUP BY e.agent_id, e.deployment_id, e.provider, e.model
ORDER BY total_cost DESC`, strings.Join(where, " AND "))

	rows, err := tp.Query(cfg, q)
	if err != nil {
		return nil, err
	}

	costs := make([]models.AgentModelCost, 0, len(rows))
	for _, r := range rows {
		agentID := parseStr(r["agent_id"])
		costs = append(costs, models.AgentModelCost{
			AgentID:      agentID,
			AgentName:    agentID,
			Provider:     parseStr(r["provider"]),
			Model:        parseStr(r["model"]),
			Cost:         parseFloat64(r["total_cost"]),
			Requests:     formatCostCount(parseInt64(r["request_count"])),
			InputTokens:  parseInt64(r["input_tokens"]),
			OutputTokens: parseInt64(r["output_tokens"]),
			CacheTokens:  parseInt64(r["cache_tokens"]),
		})
	}
	return costs, nil
}

// queryCostDailyTrend returns per-day total cost for the last 30 days.
// Groups by (day, provider, model) to keep any(price) deterministic, then
// aggregates per-day in Go.
func queryCostDailyTrend(cfg *config.TimeplusConfig) ([]models.DailySpend, error) {
	q := `
SELECT
  to_start_of_day(e.event_time)                                                              AS day,
  e.provider,
  e.model,
  (
    sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'input')))
      * any(p.input_cost_per_million)
    + sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'output')))
      * any(p.output_cost_per_million)
    + sum(to_int64_or_zero(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))
      * any(p.cache_read_cost_per_million)
  ) / 1000000.0                                                                              AS cost_per_model
FROM table(agentshield_hook_events) AS e
LEFT JOIN table(agentshield_pricing) AS p
  ON e.provider = p.provider AND e.model = p.model
WHERE e.hook_name = 'llm_output'
  AND e.event_time >= now() - interval 30 day
GROUP BY day, e.provider, e.model
ORDER BY day ASC`

	rows, err := tp.Query(cfg, q)
	if err != nil {
		return nil, err
	}

	// Aggregate per-day across all models.
	dailyMap := make(map[string]float64)
	var dayOrder []string
	for _, r := range rows {
		label := parseTime(r["day"]).Format("Jan 02")
		if _, exists := dailyMap[label]; !exists {
			dayOrder = append(dayOrder, label)
		}
		dailyMap[label] += parseFloat64(r["cost_per_model"])
	}

	daily := make([]models.DailySpend, 0, len(dayOrder))
	for _, d := range dayOrder {
		daily = append(daily, models.DailySpend{Day: d, Amount: dailyMap[d]})
	}
	return daily, nil
}

// queryCostTokenBreakdown returns the input/output/cache-read cost split across all agents.
// Groups by (provider, model) to keep any(price) deterministic, then sums in Go.
func queryCostTokenBreakdown(cfg *config.TimeplusConfig) ([]models.TokenBreakdown, error) {
	q := `
SELECT
  e.provider,
  e.model,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'input')))                        AS input_tokens,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'output')))                       AS output_tokens,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))   AS cache_tokens,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'input')))
    * any(p.input_cost_per_million) / 1000000.0                                              AS input_cost,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'output')))
    * any(p.output_cost_per_million) / 1000000.0                                             AS output_cost,
  sum(to_int64_or_zero(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))
    * any(p.cache_read_cost_per_million) / 1000000.0                                         AS cache_cost
FROM table(agentshield_hook_events) AS e
LEFT JOIN table(agentshield_pricing) AS p
  ON e.provider = p.provider AND e.model = p.model
WHERE e.hook_name = 'llm_output'
GROUP BY e.provider, e.model`

	rows, err := tp.Query(cfg, q)
	if err != nil {
		return nil, err
	}

	var inputTokens, outputTokens, cacheTokens int64
	var inputCost, outputCost, cacheCost float64
	for _, r := range rows {
		inputTokens += parseInt64(r["input_tokens"])
		outputTokens += parseInt64(r["output_tokens"])
		cacheTokens += parseInt64(r["cache_tokens"])
		inputCost += parseFloat64(r["input_cost"])
		outputCost += parseFloat64(r["output_cost"])
		cacheCost += parseFloat64(r["cache_cost"])
	}

	// Percentages are based on token count, not cost — so they're meaningful even for free models.
	totalTokens := inputTokens + outputTokens + cacheTokens
	inputPct, outputPct, cachePct := 0, 0, 0
	if totalTokens > 0 {
		inputPct = int(float64(inputTokens) / float64(totalTokens) * 100)
		outputPct = int(float64(outputTokens) / float64(totalTokens) * 100)
		cachePct = 100 - inputPct - outputPct // ensure sum is exactly 100
	}

	return []models.TokenBreakdown{
		{Label: "Input Tokens", Percentage: inputPct, Tokens: inputTokens, Amount: inputCost},
		{Label: "Output Tokens", Percentage: outputPct, Tokens: outputTokens, Amount: outputCost},
		{Label: "Cache Tokens", Percentage: cachePct, Tokens: cacheTokens, Amount: cacheCost},
	}, nil
}

// ── Utilities ─────────────────────────────────────────────────────────────────

// escapeSQL replaces single quotes to prevent SQL injection in string literals.
func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// formatCostCount formats a raw count into "14.2K" or "1.4M" for display.
func formatCostCount(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
