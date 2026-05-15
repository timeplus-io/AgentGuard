package handlers

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// pricingKeyRe validates provider and model values used in SQL path params.
var pricingKeyRe = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)

// GetCost handles GET /api/cost.
// Runs three Proton queries to compute cost KPIs, daily trend, and token breakdown.
// Budget policies and anomalies remain as mock data (out of scope).
func GetCost(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		now := time.Now().UTC()
		curMTD := currentMonthToDate(now)
		priMTD := priorMonthSameElapsed(now)

		// All-time per-agent costs (powers the per-agent breakdown cards).
		allCosts, err := queryCostByAgent(&cfg.Timeplus, "", "")
		if err != nil {
			log.Printf("[cost] GetCost all-time cost query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Current month-to-date (powers MTD spend, forecast, and avg-token KPIs).
		mtdCosts, err := queryCostByAgent(&cfg.Timeplus, formatPeriodBound(curMTD.Start), "")
		if err != nil {
			log.Printf("[cost] GetCost current MTD query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Prior month, same elapsed window (powers the deltas).
		priMTDCosts, err := queryCostByAgent(&cfg.Timeplus,
			formatPeriodBound(priMTD.Start), formatPeriodBound(priMTD.End))
		if err != nil {
			log.Printf("[cost] GetCost prior MTD query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Daily spend trend, last 30 days.
		daily, err := queryCostDailyTrend(&cfg.Timeplus)
		if err != nil {
			log.Printf("[cost] GetCost daily trend query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Token-type cost breakdown (input / output / cache-read).
		breakdown, err := queryCostTokenBreakdown(&cfg.Timeplus)
		if err != nil {
			log.Printf("[cost] GetCost token breakdown query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Current MTD aggregates.
		var mtd, mtdCost float64
		var mtdTokens int64
		for _, a := range mtdCosts {
			mtd += a.Cost
			mtdCost += a.Cost
			mtdTokens += a.InputTokens + a.OutputTokens + a.CacheTokens
		}

		elapsedDays := float64(now.Day())
		if elapsedDays < 1 {
			elapsedDays = 1
		}
		forecasted := mtd / elapsedDays * 30

		// Avg cost per million tokens — current MTD only.
		// Was previously computed against all-time data; switched to MTD so the
		// prior-period delta below compares like-for-like windows. The KPI label
		// on /cost no longer shifts meaning across the month boundary.
		avgTokenCost := 0.0
		if mtdTokens > 0 {
			avgTokenCost = mtdCost / float64(mtdTokens) * 1_000_000
		}

		// Prior MTD aggregates.
		var mtdPrev, priCost float64
		var priTokens int64
		for _, a := range priMTDCosts {
			mtdPrev += a.Cost
			priCost += a.Cost
			priTokens += a.InputTokens + a.OutputTokens + a.CacheTokens
		}
		avgTokenCostPrev := 0.0
		if priTokens > 0 {
			avgTokenCostPrev = priCost / float64(priTokens) * 1_000_000
		}

		// Delta percents are nil when the prior denominator is zero — per #123,
		// the frontend treats nil as "no delta to display" and renders the KPI
		// tile without a delta row.
		mtdDeltaPct := deltaPctOrNil(mtd, mtdPrev)
		avgTokenCostDeltaPct := deltaPctOrNil(avgTokenCost, avgTokenCostPrev)

		// Cache savings estimate: cache-read tokens cost ~10% of input price on average.
		// Savings = what would have been paid at input price minus what was actually paid.
		cacheSavings := 0.0
		for _, b := range breakdown {
			if b.Label == "Cache Tokens" {
				cacheSavings = b.Amount * 9
			}
		}

		// Forecast-vs-budget: only populated when at least one budget is configured.
		budgetAmount, forecastBudgetDeltaPct, budgetStatus, err := queryForecastVsBudget(&cfg.Timeplus, forecasted)
		if err != nil {
			log.Printf("[cost] GetCost budget query failed: %v", err)
			// Non-fatal: budgets are optional. Fall through with nil delta fields.
		}

		c.JSON(http.StatusOK, models.CostResponse{
			MonthToDate:            mtd,
			MonthToDatePrev:        mtdPrev,
			MonthToDateDeltaPct:    mtdDeltaPct,
			Forecasted:             forecasted,
			ForecastBudgetDeltaPct: forecastBudgetDeltaPct,
			BudgetAmount:           budgetAmount,
			BudgetStatus:           budgetStatus,
			AvgTokenCost:           avgTokenCost,
			AvgTokenCostPrev:       avgTokenCostPrev,
			AvgTokenCostDeltaPct:   avgTokenCostDeltaPct,
			DailySpend:             daily,
			TokenBreakdown:         breakdown,
			CacheSavings:           cacheSavings,
			WastedSpend:            0,
			AgentModelCosts:        allCosts,
		})
	}
}

// ListPricing handles GET /api/cost/pricing.
func ListPricing(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		rows, err := tp.Query(&cfg.Timeplus, `
			SELECT provider, model, input_cost_per_million, output_cost_per_million,
			       cache_read_cost_per_million, updated_at
			FROM table(agentguard_pricing)
			ORDER BY provider ASC, model ASC`)
		if err != nil {
			log.Printf("[cost] ListPricing query failed: %v", err)
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
		cfg := auth.RequestCfg(c, cfg)
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
			`INSERT INTO agentguard_pricing
			 (provider, model, input_cost_per_million, output_cost_per_million, cache_read_cost_per_million, updated_at)
			 VALUES ('%s', '%s', %f, %f, %f, now64(3))`,
			escapeSQL(body.Provider),
			escapeSQL(body.Model),
			body.InputCostPerMillion,
			body.OutputCostPerMillion,
			body.CacheReadCostPerMillion,
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			log.Printf("[cost] UpsertPricing failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upsert failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// DeletePricing handles DELETE /api/cost/pricing/:provider/:model.
func DeletePricing(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
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
			`DELETE FROM agentguard_pricing WHERE provider = '%s' AND model = '%s'`,
			escapeSQL(provider),
			escapeSQL(model),
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			log.Printf("[cost] DeletePricing failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": true})
	}
}

// ── Period helpers ────────────────────────────────────────────────────────────

// costPeriod is a half-open UTC window: [Start, End).
type costPeriod struct {
	Start time.Time
	End   time.Time
}

// formatPeriodBound converts a UTC time.Time to the Proton-friendly string used
// by queryCostByAgent. A zero time renders as empty, which the query helpers
// treat as "unbounded on that side".
func formatPeriodBound(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

// currentMonthToDate returns [monthStart, now) in UTC for the calendar month
// containing now.
func currentMonthToDate(now time.Time) costPeriod {
	now = now.UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return costPeriod{Start: start, End: now}
}

// priorMonthSameElapsed returns the previous month's window of the same elapsed
// duration as the current month-to-date in UTC. If the previous month is
// shorter than the elapsed duration (e.g. March 31 → February has 28 days),
// End is clipped to the start of the current month so the comparison covers
// the full previous month rather than overflowing into the current one.
// Go normalizes month-1 across year boundaries: January (1) - 1 → December
// of the previous year.
func priorMonthSameElapsed(now time.Time) costPeriod {
	now = now.UTC()
	curStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	elapsed := now.Sub(curStart)

	prevStart := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	prevEnd := prevStart.Add(elapsed)
	if prevEnd.After(curStart) {
		prevEnd = curStart
	}
	return costPeriod{Start: prevStart, End: prevEnd}
}

// deltaPctOrNil computes (current - prev) / prev * 100. Returns nil when prev
// is zero or negative — that's the signal to the frontend that the comparison
// has no meaningful denominator and the delta row should not render.
func deltaPctOrNil(current, prev float64) *float64 {
	if prev <= 0 {
		return nil
	}
	pct := (current - prev) / prev * 100.0
	return &pct
}

// ── Query helpers ─────────────────────────────────────────────────────────────

// queryCostByAgent returns cost aggregated by (agent_id, deployment_id, provider, model)
// over the half-open UTC window [since, until). Either bound may be empty, meaning
// "unbounded on that side". Bound strings must be Proton-formatted "2006-01-02 15:04:05".
func queryCostByAgent(cfg *config.TimeplusConfig, since, until string) ([]models.AgentModelCost, error) {
	bounds := ""
	if since != "" {
		bounds += fmt.Sprintf("\n  AND m.event_time >= '%s'", since)
	}
	if until != "" {
		bounds += fmt.Sprintf("\n  AND m.event_time <  '%s'", until)
	}

	q := fmt.Sprintf(`
SELECT
  m.agent_id,
  m.deployment_id,
  m.tags['provider']                                                    AS provider,
  m.tags['model']                                                       AS model,
  to_int64(sum_if(m.metric_value, m.metric_name = 'llm.call'))         AS request_count,
  to_int64(sum_if(m.metric_value, m.metric_name = 'llm.input_tokens')) AS input_tokens,
  to_int64(sum_if(m.metric_value, m.metric_name = 'llm.output_tokens')) AS output_tokens,
  to_int64(sum_if(m.metric_value, m.metric_name = 'llm.cache_read_tokens')) AS cache_tokens,
  (
    sum_if(m.metric_value, m.metric_name = 'llm.input_tokens')      * any(p.input_cost_per_million)
    + sum_if(m.metric_value, m.metric_name = 'llm.output_tokens')   * any(p.output_cost_per_million)
    + sum_if(m.metric_value, m.metric_name = 'llm.cache_read_tokens') * any(p.cache_read_cost_per_million)
  ) / 1000000.0                                                         AS total_cost
FROM table(agentguard_cim_metrics) AS m
LEFT JOIN table(agentguard_pricing) AS p
  ON m.tags['provider'] = p.provider AND m.tags['model'] = p.model
WHERE m.metric_name IN ('llm.call', 'llm.input_tokens', 'llm.output_tokens', 'llm.cache_read_tokens')%s
GROUP BY m.agent_id, m.deployment_id, provider, model
ORDER BY total_cost DESC`, bounds)

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
  to_start_of_day(m.event_time)                                        AS day,
  m.tags['provider']                                                    AS provider,
  m.tags['model']                                                       AS model,
  (
    sum_if(m.metric_value, m.metric_name = 'llm.input_tokens')      * any(p.input_cost_per_million)
    + sum_if(m.metric_value, m.metric_name = 'llm.output_tokens')   * any(p.output_cost_per_million)
    + sum_if(m.metric_value, m.metric_name = 'llm.cache_read_tokens') * any(p.cache_read_cost_per_million)
  ) / 1000000.0                                                         AS cost_per_model
FROM table(agentguard_cim_metrics) AS m
LEFT JOIN table(agentguard_pricing) AS p
  ON m.tags['provider'] = p.provider AND m.tags['model'] = p.model
WHERE m.metric_name IN ('llm.input_tokens', 'llm.output_tokens', 'llm.cache_read_tokens')
  AND m.event_time >= now() - interval 30 day
GROUP BY day, provider, model
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
  m.tags['provider']                                                         AS provider,
  m.tags['model']                                                            AS model,
  to_int64(sum_if(m.metric_value, m.metric_name = 'llm.input_tokens'))      AS input_tokens,
  to_int64(sum_if(m.metric_value, m.metric_name = 'llm.output_tokens'))     AS output_tokens,
  to_int64(sum_if(m.metric_value, m.metric_name = 'llm.cache_read_tokens')) AS cache_tokens,
  sum_if(m.metric_value, m.metric_name = 'llm.input_tokens')
    * any(p.input_cost_per_million) / 1000000.0                              AS input_cost,
  sum_if(m.metric_value, m.metric_name = 'llm.output_tokens')
    * any(p.output_cost_per_million) / 1000000.0                             AS output_cost,
  sum_if(m.metric_value, m.metric_name = 'llm.cache_read_tokens')
    * any(p.cache_read_cost_per_million) / 1000000.0                         AS cache_cost
FROM table(agentguard_cim_metrics) AS m
LEFT JOIN table(agentguard_pricing) AS p
  ON m.tags['provider'] = p.provider AND m.tags['model'] = p.model
WHERE m.metric_name IN ('llm.input_tokens', 'llm.output_tokens', 'llm.cache_read_tokens')
GROUP BY provider, model`

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
