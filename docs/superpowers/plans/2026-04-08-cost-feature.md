# Cost Governance Feature Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the mock-backed `/api/cost` endpoint with real data computed by joining token-usage events with a new mutable pricing stream, and add a pricing management UI panel to the Cost Governance page.

**Architecture:** A new `agentshield_pricing` mutable stream (PRIMARY KEY provider+model) is provisioned at startup and seeded with common model prices. Three focused Proton queries in `handlers/cost.go` JOIN this pricing table against `agentshield_hook_events` to produce KPIs, a daily spend trend, and a token-type cost breakdown. Four new pricing CRUD endpoints manage the mutable stream. The frontend wires real data and adds an inline pricing panel reusing existing table/card patterns.

**Tech Stack:** Go/Gin backend, Timeplus Proton (mutable streams, LEFT JOIN, `table()` historical scan), React + Tailwind frontend.

---

## File Map

| File | Action | Responsibility |
|------|--------|---------------|
| `backend/internal/timeplus/init.go` | Modify | Add `ddlPricingStream`, `createPricingStream`, `seedPricingData`, call them in `Init()` |
| `backend/internal/models/models.go` | Modify | Add `PricingEntry`, `PricingListResponse`; extend `AgentModelCost` |
| `backend/internal/handlers/cost.go` | Rewrite | Real Proton queries for cost + pricing CRUD |
| `backend/cmd/server/main.go` | Modify | Register new routes, change `GetCost` to closure |
| `frontend/src/api/client.js` | Modify | Add `listPricing`, `upsertPricing`, `deletePricing`; update `getCost` |
| `frontend/src/pages/CostGovernance.jsx` | Modify | Wire real data; add pricing management panel |

---

## Task 1: Add pricing mutable stream to Timeplus init

**Files:**
- Modify: `backend/internal/timeplus/init.go`

- [ ] **Step 1: Add the DDL constant and helper functions**

Add the following to `backend/internal/timeplus/init.go`, after the `ddlRulesStream` constant and before `ddlNotifyUDF`:

```go
const ddlPricingStream = `
CREATE MUTABLE STREAM agentshield_pricing
(
  provider                    low_cardinality(string),
  model                       low_cardinality(string),
  input_cost_per_million      float64,
  output_cost_per_million     float64,
  cache_read_cost_per_million float64,
  updated_at                  datetime64(3)
) PRIMARY KEY (provider, model)`

type pricingSeed struct {
	provider  string
	model     string
	inputCPM  float64
	outputCPM float64
	cacheCPM  float64
}

var pricingSeedData = []pricingSeed{
	{"anthropic", "claude-sonnet-4-6", 3.00, 15.00, 0.30},
	{"anthropic", "claude-opus-4-6", 15.00, 75.00, 1.50},
	{"anthropic", "claude-haiku-4-5", 0.80, 4.00, 0.08},
	{"openai", "gpt-4o", 2.50, 10.00, 1.25},
	{"openai", "gpt-4o-mini", 0.15, 0.60, 0.075},
	{"openai", "o3", 10.00, 40.00, 2.50},
}

func createPricingStream(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "stream", "agentshield_pricing", ddlPricingStream)
}

func seedPricingData(ctx context.Context, conn proton.Conn) error {
	row := conn.QueryRow(ctx, "SELECT count() FROM table(agentshield_pricing)")
	var count uint64
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("pricing seed check: %w", err)
	}
	if count > 0 {
		log.Printf("timeplus: pricing already seeded (%d records), skipping", count)
		return nil
	}

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO agentshield_pricing (provider, model, input_cost_per_million, output_cost_per_million, cache_read_cost_per_million, updated_at)")
	if err != nil {
		return fmt.Errorf("pricing seed prepare: %w", err)
	}

	now := time.Now()
	for _, p := range pricingSeedData {
		if err := batch.Append(p.provider, p.model, p.inputCPM, p.outputCPM, p.cacheCPM, now); err != nil {
			return fmt.Errorf("pricing seed append %s/%s: %w", p.provider, p.model, err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("pricing seed send: %w", err)
	}
	log.Printf("timeplus: seeded %d pricing entries", len(pricingSeedData))
	return nil
}
```

- [ ] **Step 2: Call the new functions from `Init()`**

In `backend/internal/timeplus/init.go`, find the `Init()` function. Add two calls after `createSecurityStreams` and before `seedBuiltinRules`:

```go
	if err := createPricingStream(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating pricing stream: %w", err)
	}
	if err := seedPricingData(ctx, conn); err != nil {
		log.Printf("timeplus init: warning seeding pricing: %v", err)
	}
```

The updated `Init()` body should read:
```go
	if err := createOTelStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating otel streams: %w", err)
	}
	if err := createOTelInput(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating otel input: %w", err)
	}
	if err := createAgentShieldStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating agentshield streams: %w", err)
	}
	if err := createSecurityStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating security streams: %w", err)
	}
	if err := createPricingStream(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating pricing stream: %w", err)
	}
	if err := seedPricingData(ctx, conn); err != nil {
		log.Printf("timeplus init: warning seeding pricing: %v", err)
	}
	if err := seedBuiltinRules(ctx, conn); err != nil {
		log.Printf("timeplus init: warning seeding rules: %v", err)
	}
	// ... rest unchanged
```

- [ ] **Step 3: Verify it compiles**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output (clean compile).

- [ ] **Step 4: Commit**

```bash
cd /Users/gangtao/Code/timeplus/agentshield
git add backend/internal/timeplus/init.go
git commit -m "feat(cost): add agentshield_pricing mutable stream and seed data"
```

---

## Task 2: Add pricing models and extend AgentModelCost

**Files:**
- Modify: `backend/internal/models/models.go`

- [ ] **Step 1: Add `PricingEntry` and `PricingListResponse`**

In `backend/internal/models/models.go`, find the `CostResponse` struct and add the following two types directly after it (before `RulePack`):

```go
// PricingEntry is one row in the agentshield_pricing mutable stream.
type PricingEntry struct {
	Provider               string  `json:"provider"`
	Model                  string  `json:"model"`
	InputCostPerMillion    float64 `json:"input_cost_per_million"`
	OutputCostPerMillion   float64 `json:"output_cost_per_million"`
	CacheReadCostPerMillion float64 `json:"cache_read_cost_per_million"`
	UpdatedAt              string  `json:"updated_at"`
}

// PricingListResponse is returned by GET /api/cost/pricing.
type PricingListResponse struct {
	Pricing []PricingEntry `json:"pricing"`
}
```

- [ ] **Step 2: Extend `AgentModelCost`**

Find the existing `AgentModelCost` struct:
```go
type AgentModelCost struct {
	Model     string  `json:"model"`
	AgentName string  `json:"agent_name"`
	Cost      float64 `json:"cost"`
	Requests  string  `json:"requests"`
}
```

Replace it with:
```go
type AgentModelCost struct {
	Model        string  `json:"model"`
	AgentName    string  `json:"agent_name"`
	Cost         float64 `json:"cost"`
	Requests     string  `json:"requests"`
	Provider     string  `json:"provider"`
	AgentID      string  `json:"agent_id"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheTokens  int64   `json:"cache_tokens"`
}
```

- [ ] **Step 3: Verify it compiles**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output (clean compile).

- [ ] **Step 4: Commit**

```bash
cd /Users/gangtao/Code/timeplus/agentshield
git add backend/internal/models/models.go
git commit -m "feat(cost): add PricingEntry model and extend AgentModelCost"
```

---

## Task 3: Rewrite the cost handler with real Proton queries

**Files:**
- Rewrite: `backend/internal/handlers/cost.go`

Context on helpers available in the `handlers` package (defined in other files, usable here):
- `parseFloat64(v any) float64` — in `metrics.go`
- `parseInt64(v any) int64` — in `metrics.go`
- `parseInt(v any) int` — in `agent_sessions.go`
- `parseStr(v any) string` — in `agent_sessions.go`
- `parseTime(v any) time.Time` — in `agent_sessions.go`

- [ ] **Step 1: Replace `backend/internal/handlers/cost.go` entirely**

```go
package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/mock"
	"github.com/timeplus/agentshield/internal/models"
	tp "github.com/timeplus/agentshield/internal/timeplus"
)

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
				Provider:               parseStr(r["provider"]),
				Model:                  parseStr(r["model"]),
				InputCostPerMillion:    parseFloat64(r["input_cost_per_million"]),
				OutputCostPerMillion:   parseFloat64(r["output_cost_per_million"]),
				CacheReadCostPerMillion: parseFloat64(r["cache_read_cost_per_million"]),
				UpdatedAt:              parseStr(r["updated_at"]),
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
  sum(to_int64(json_extract_string(e.event_data, 'usage', 'input')))                         AS input_tokens,
  sum(to_int64(json_extract_string(e.event_data, 'usage', 'output')))                        AS output_tokens,
  sum(to_int64(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))    AS cache_tokens,
  (
    sum(to_int64(json_extract_string(e.event_data, 'usage', 'input')))
      * any(p.input_cost_per_million)
    + sum(to_int64(json_extract_string(e.event_data, 'usage', 'output')))
      * any(p.output_cost_per_million)
    + sum(to_int64(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))
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
    sum(to_int64(json_extract_string(e.event_data, 'usage', 'input')))
      * any(p.input_cost_per_million)
    + sum(to_int64(json_extract_string(e.event_data, 'usage', 'output')))
      * any(p.output_cost_per_million)
    + sum(to_int64(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))
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
  sum(to_int64(json_extract_string(e.event_data, 'usage', 'input')))
    * any(p.input_cost_per_million) / 1000000.0                                              AS input_cost,
  sum(to_int64(json_extract_string(e.event_data, 'usage', 'output')))
    * any(p.output_cost_per_million) / 1000000.0                                             AS output_cost,
  sum(to_int64(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))
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

	var inputCost, outputCost, cacheCost float64
	for _, r := range rows {
		inputCost += parseFloat64(r["input_cost"])
		outputCost += parseFloat64(r["output_cost"])
		cacheCost += parseFloat64(r["cache_cost"])
	}

	totalCost := inputCost + outputCost + cacheCost
	pct := func(part float64) int {
		if totalCost == 0 {
			return 0
		}
		return int(part / totalCost * 100)
	}

	return []models.TokenBreakdown{
		{Label: "Input Tokens", Percentage: pct(inputCost), Amount: inputCost},
		{Label: "Output Tokens", Percentage: pct(outputCost), Amount: outputCost},
		{Label: "Cache Tokens", Percentage: pct(cacheCost), Amount: cacheCost},
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
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output (clean compile).

- [ ] **Step 3: Commit**

```bash
cd /Users/gangtao/Code/timeplus/agentshield
git add backend/internal/handlers/cost.go
git commit -m "feat(cost): rewrite cost handler with real Proton queries and pricing CRUD"
```

---

## Task 4: Register new routes in main.go

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Update the cost and pricing routes**

In `backend/cmd/server/main.go`, find this line:
```go
		api.GET("/cost", handlers.GetCost)
```

Replace it with:
```go
		api.GET("/cost", handlers.GetCost(cfg))
		api.GET("/cost/pricing", handlers.ListPricing(cfg))
		api.POST("/cost/pricing", handlers.UpsertPricing(cfg))
		api.DELETE("/cost/pricing/:provider/:model", handlers.DeletePricing(cfg))
```

Note: the new routes must be registered **before** any wildcard routes and in this specific order — Gin matches `/cost/pricing` literally before `:provider/:model` only if listed first.

- [ ] **Step 2: Verify it compiles and binary builds**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend
CGO_ENABLED=0 go build -o server cmd/server/main.go
```

Expected: `server` binary produced with no errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/gangtao/Code/timeplus/agentshield
git add backend/cmd/server/main.go
git commit -m "feat(cost): register pricing CRUD routes"
```

---

## Task 5: Update the frontend API client

**Files:**
- Modify: `frontend/src/api/client.js`

- [ ] **Step 1: Replace `getCost` and add pricing functions**

In `frontend/src/api/client.js`, find:
```js
  getCost: () => get('/cost'),
```

Replace it with:
```js
  getCost: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/cost${q ? '?' + q : ''}`)
  },
  listPricing: () => get('/cost/pricing'),
  upsertPricing: (entry) => post('/cost/pricing', entry),
  deletePricing: (provider, model) =>
    fetch(`${BASE}/cost/pricing/${encodeURIComponent(provider)}/${encodeURIComponent(model)}`, {
      method: 'DELETE',
    }).then(res => {
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      return res.json()
    }),
```

- [ ] **Step 2: Verify the frontend still builds**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/frontend
npm run build 2>&1 | tail -5
```

Expected: `✓ built in ...` with no errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/gangtao/Code/timeplus/agentshield
git add frontend/src/api/client.js
git commit -m "feat(cost): add pricing API functions to client"
```

---

## Task 6: Update CostGovernance.jsx — wire real data and add pricing panel

**Files:**
- Modify: `frontend/src/pages/CostGovernance.jsx`

- [ ] **Step 1: Replace `CostGovernance.jsx` entirely**

The existing static arrays (`staticSpendData`, `agentModels`, `budgetPolicies`, `anomalies`) are removed. All data now comes from the API. The pricing panel is added between the agent/model cards and the budget/anomaly row, using the same card + table visual pattern already present.

```jsx
import { useState, useEffect } from 'react'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Cell } from 'recharts'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'

const CustomTooltip = ({ active, payload, label }) => {
  if (active && payload && payload.length) {
    return (
      <div className="bg-[#2c2834] border border-[#574149]/20 px-3 py-2 rounded-sm text-xs">
        <p className="text-zinc-400 font-semibold">{label}</p>
        <p className="text-[#ffb0cf] font-bold">${payload[0].value.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}</p>
      </div>
    )
  }
  return null
}

const fmtUSD = (n) =>
  typeof n === 'number'
    ? '$' + n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
    : '$0.00'

const emptyForm = { provider: '', model: '', input_cost_per_million: '', output_cost_per_million: '', cache_read_cost_per_million: '' }

export default function CostGovernance() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [pricing, setPricing] = useState([])
  const [showAddForm, setShowAddForm] = useState(false)
  const [formValues, setFormValues] = useState(emptyForm)
  const [saving, setSaving] = useState(false)
  const [deletingKey, setDeletingKey] = useState(null) // "provider/model" being deleted

  useEffect(() => {
    api.getCost()
      .then(setData)
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    api.listPricing()
      .then(r => setPricing(r.pricing ?? []))
      .catch(() => {})
  }, [])

  const refreshPricing = () =>
    api.listPricing()
      .then(r => setPricing(r.pricing ?? []))
      .catch(() => {})

  const handleUpsert = async () => {
    setSaving(true)
    try {
      await api.upsertPricing({
        provider: formValues.provider,
        model: formValues.model,
        input_cost_per_million: parseFloat(formValues.input_cost_per_million) || 0,
        output_cost_per_million: parseFloat(formValues.output_cost_per_million) || 0,
        cache_read_cost_per_million: parseFloat(formValues.cache_read_cost_per_million) || 0,
      })
      setShowAddForm(false)
      setFormValues(emptyForm)
      await refreshPricing()
    } catch (_) {
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async (provider, model) => {
    const key = `${provider}/${model}`
    setDeletingKey(key)
    try {
      await api.deletePricing(provider, model)
      await refreshPricing()
    } catch (_) {
    } finally {
      setDeletingKey(null)
    }
  }

  const spendData = data?.daily_spend ?? []
  const maxSpend = spendData.length ? Math.max(...spendData.map(d => d.amount)) : 0

  const kpis = [
    {
      label: 'Month-to-Date Spend',
      value: loading ? '...' : fmtUSD(data?.month_to_date),
      delta: '4.2% from last month', deltaColor: 'text-emerald-400', deltaIcon: 'trending_down',
    },
    {
      label: 'Forecasted Total',
      value: loading ? '...' : fmtUSD(data?.forecasted),
      delta: 'Within 15% of budget', deltaColor: 'text-zinc-500', deltaIcon: null,
    },
    {
      label: 'Avg. Token Cost / 1M',
      value: loading ? '...' : fmtUSD(data?.avg_token_cost),
      delta: 'Optimized by 12%', deltaColor: 'text-[#ffb0cf]', deltaIcon: null,
    },
    {
      label: 'Active Policy Breaches',
      value: loading ? '...' : (data?.policy_breaches ?? 0).toString().padStart(2, '0'),
      delta: 'Requires Attention', deltaColor: 'text-[#ffb4ab]', deltaIcon: null,
      valueColor: 'text-[#ffb4ab]',
    },
  ]

  const tokenBreakdown = data?.token_breakdown ?? [
    { label: 'Input Tokens', percentage: 42, amount: 0 },
    { label: 'Output Tokens', percentage: 48, amount: 0 },
    { label: 'Cache Tokens', percentage: 10, amount: 0 },
  ]

  const tokenColors = ['bg-[#D53F8C]', 'bg-[#ffb0cf]', 'bg-[#ffb2b8]']

  const agentModelCosts = data?.agent_model_costs ?? []

  const budgetPolicies = data?.budget_policies ?? []
  const anomalies = data?.anomalies ?? []

  const enfClass = (name) => {
    if (name === 'KILL_AGENT') return 'bg-[#93000a] text-[#ffdad6]'
    return 'bg-[#4b4754] text-[#bcb6c5]'
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">
          <PageHeader
            title="Cost Governance"
            subtitle="Monitor token spend, enforce budget policies, and detect cost anomalies across all agents."
            actions={<>
              <button className="px-4 py-2 bg-[#2c2834] text-[#e7dff0] text-xs font-medium rounded-sm border border-[#574149]/10 hover:bg-[#37333f] transition-colors flex items-center gap-2">
                <span className="material-symbols-outlined text-[16px]">download</span>
                Export Report
              </button>
              <button className="px-4 py-2 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] text-xs font-semibold rounded-sm flex items-center gap-2">
                <span className="material-symbols-outlined text-[16px]">add</span>
                New Policy
              </button>
            </>}
          />

          {/* KPI Grid */}
          <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
            {kpis.map((kpi, i) => (
              <div key={i} className="bg-[#1d1a25] p-6 rounded relative overflow-hidden group">
                <div className="absolute top-0 right-0 w-16 h-16 bg-[#ffb0cf]/5 rounded-full -mr-8 -mt-8 group-hover:scale-150 transition-transform duration-500"></div>
                <p className="text-zinc-500 text-xs font-medium uppercase tracking-wider">{kpi.label}</p>
                <h3 className={`text-2xl font-semibold mt-2 ${kpi.valueColor || 'text-[#e7dff0]'}`}>{kpi.value}</h3>
                <div className={`flex items-center gap-1 mt-4 text-xs font-semibold ${kpi.deltaColor}`}>
                  {kpi.deltaIcon && <span className="material-symbols-outlined text-xs">{kpi.deltaIcon}</span>}
                  {kpi.delta}
                </div>
              </div>
            ))}
          </div>

          {/* Charts Row */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
            {/* Daily Spend Trend */}
            <div className="lg:col-span-2 bg-[#2c2834] p-6 rounded">
              <div className="flex justify-between items-center mb-8">
                <h4 className="text-base font-semibold text-[#e7dff0]">Daily Spend Trend</h4>
                <span className="px-2 py-1 bg-[#37333f] text-[10px] rounded text-[#ffb0cf] border border-[#ffb0cf]/20">30 DAYS</span>
              </div>
              {spendData.length === 0 ? (
                <div className="h-64 flex items-center justify-center text-zinc-500 text-sm">No spend data yet</div>
              ) : (
                <ResponsiveContainer width="100%" height={256}>
                  <BarChart data={spendData} margin={{ top: 0, right: 0, bottom: 0, left: 0 }}>
                    <XAxis dataKey="day" axisLine={false} tickLine={false} tick={{ fill: '#52525b', fontSize: 10, fontWeight: 700 }} />
                    <YAxis hide />
                    <Tooltip content={<CustomTooltip />} cursor={{ fill: 'rgba(255,176,207,0.05)' }} />
                    <Bar dataKey="amount" radius={[2, 2, 0, 0]}>
                      {spendData.map((entry, index) => (
                        <Cell key={index} fill={entry.amount === maxSpend ? '#ffb0cf' : '#1d1a25'} />
                      ))}
                    </Bar>
                  </BarChart>
                </ResponsiveContainer>
              )}
            </div>

            {/* Token Breakdown */}
            <div className="bg-[#2c2834] p-6 rounded">
              <h4 className="text-base font-semibold text-[#e7dff0] mb-8">Token Breakdown</h4>
              <div className="space-y-6">
                {tokenBreakdown.map((t, i) => (
                  <div key={t.label}>
                    <div className="flex justify-between text-xs mb-2">
                      <span className="text-zinc-400">{t.label}</span>
                      <span className="text-[#e7dff0] font-semibold">{t.percentage}% ({fmtUSD(t.amount)})</span>
                    </div>
                    <div className="h-1.5 w-full bg-[#0f0c17] rounded-full overflow-hidden">
                      <div className={`h-full ${tokenColors[i]}`} style={{ width: `${t.percentage}%` }}></div>
                    </div>
                  </div>
                ))}
              </div>
              <div className="mt-8 pt-8 border-t border-[#574149]/10">
                <div className="flex items-center gap-4">
                  <div className="flex-1 bg-[#0f0c17] p-3 rounded text-center">
                    <p className="text-[10px] text-zinc-500 uppercase font-semibold">Cache Savings</p>
                    <p className="text-base font-semibold text-emerald-400">{fmtUSD(data?.cache_savings)}</p>
                  </div>
                  <div className="flex-1 bg-[#0f0c17] p-3 rounded text-center">
                    <p className="text-[10px] text-zinc-500 uppercase font-semibold">Wasted Spend</p>
                    <p className="text-base font-semibold text-[#ffb4ab]">{fmtUSD(data?.wasted_spend)}</p>
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* Cost per Agent / Model */}
          <div className="bg-[#2c2834] p-6 rounded">
            <h4 className="text-base font-semibold text-[#e7dff0] mb-6">Cost per Agent / Model</h4>
            {agentModelCosts.length === 0 ? (
              <p className="text-zinc-500 text-sm">No cost data yet. Ensure the pricing table has entries for your active models.</p>
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                {agentModelCosts.slice(0, 8).map((am, i) => (
                  <div key={i} className="bg-[#211e29] p-4 rounded flex items-center justify-between group hover:bg-[#37333f] transition-colors">
                    <div>
                      <p className="text-[10px] text-zinc-500 uppercase font-semibold mb-1">{am.model}</p>
                      <h5 className="font-semibold text-[#e7dff0] truncate max-w-[120px]" title={am.agent_id}>{am.agent_name}</h5>
                    </div>
                    <div className="text-right">
                      <p className="font-semibold text-[#ffb0cf]">{fmtUSD(am.cost)}</p>
                      <p className="text-[10px] text-zinc-500">{am.requests}</p>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* Model Pricing */}
          <div className="bg-[#2c2834] p-6 rounded">
            <div className="flex justify-between items-center mb-6">
              <h4 className="text-base font-semibold text-[#e7dff0]">Model Pricing</h4>
              {!showAddForm && (
                <button
                  onClick={() => setShowAddForm(true)}
                  className="text-xs text-[#ffb0cf] font-semibold flex items-center gap-1 hover:underline"
                >
                  <span className="material-symbols-outlined text-xs">add_circle</span>
                  Add Model
                </button>
              )}
            </div>
            <table className="w-full text-left text-xs">
              <thead>
                <tr className="text-zinc-500 border-b border-[#574149]/10">
                  <th className="pb-3 font-medium">Provider</th>
                  <th className="pb-3 font-medium">Model</th>
                  <th className="pb-3 font-medium text-right">Input $/1M</th>
                  <th className="pb-3 font-medium text-right">Output $/1M</th>
                  <th className="pb-3 font-medium text-right">Cache-Read $/1M</th>
                  <th className="pb-3 font-medium w-8"></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-[#574149]/5">
                {pricing.map(p => {
                  const key = `${p.provider}/${p.model}`
                  return (
                    <tr key={key}>
                      <td className="py-3 text-zinc-400">{p.provider}</td>
                      <td className="py-3 font-medium text-[#e7dff0]">{p.model}</td>
                      <td className="py-3 text-right text-zinc-400">${p.input_cost_per_million.toFixed(3)}</td>
                      <td className="py-3 text-right text-zinc-400">${p.output_cost_per_million.toFixed(3)}</td>
                      <td className="py-3 text-right text-zinc-400">${p.cache_read_cost_per_million.toFixed(3)}</td>
                      <td className="py-3 text-right">
                        <button
                          onClick={() => handleDelete(p.provider, p.model)}
                          disabled={deletingKey === key}
                          className="text-zinc-600 hover:text-[#ffb4ab] transition-colors disabled:opacity-40"
                          title="Remove pricing entry"
                        >
                          <span className="material-symbols-outlined text-base">close</span>
                        </button>
                      </td>
                    </tr>
                  )
                })}
                {showAddForm && (
                  <tr>
                    <td className="py-2 pr-2">
                      <input
                        value={formValues.provider}
                        onChange={e => setFormValues(v => ({ ...v, provider: e.target.value }))}
                        placeholder="anthropic"
                        className="w-full bg-[#0f0c17] border border-[#574149]/20 rounded-sm px-2 py-1 text-[#e7dff0] text-xs placeholder:text-zinc-600 focus:outline-none focus:border-[#ffb0cf]/40"
                      />
                    </td>
                    <td className="py-2 pr-2">
                      <input
                        value={formValues.model}
                        onChange={e => setFormValues(v => ({ ...v, model: e.target.value }))}
                        placeholder="model-name"
                        className="w-full bg-[#0f0c17] border border-[#574149]/20 rounded-sm px-2 py-1 text-[#e7dff0] text-xs placeholder:text-zinc-600 focus:outline-none focus:border-[#ffb0cf]/40"
                      />
                    </td>
                    <td className="py-2 pr-2">
                      <input
                        type="number"
                        step="0.001"
                        value={formValues.input_cost_per_million}
                        onChange={e => setFormValues(v => ({ ...v, input_cost_per_million: e.target.value }))}
                        placeholder="0.000"
                        className="w-full bg-[#0f0c17] border border-[#574149]/20 rounded-sm px-2 py-1 text-[#e7dff0] text-xs text-right placeholder:text-zinc-600 focus:outline-none focus:border-[#ffb0cf]/40"
                      />
                    </td>
                    <td className="py-2 pr-2">
                      <input
                        type="number"
                        step="0.001"
                        value={formValues.output_cost_per_million}
                        onChange={e => setFormValues(v => ({ ...v, output_cost_per_million: e.target.value }))}
                        placeholder="0.000"
                        className="w-full bg-[#0f0c17] border border-[#574149]/20 rounded-sm px-2 py-1 text-[#e7dff0] text-xs text-right placeholder:text-zinc-600 focus:outline-none focus:border-[#ffb0cf]/40"
                      />
                    </td>
                    <td className="py-2 pr-2">
                      <input
                        type="number"
                        step="0.001"
                        value={formValues.cache_read_cost_per_million}
                        onChange={e => setFormValues(v => ({ ...v, cache_read_cost_per_million: e.target.value }))}
                        placeholder="0.000"
                        className="w-full bg-[#0f0c17] border border-[#574149]/20 rounded-sm px-2 py-1 text-[#e7dff0] text-xs text-right placeholder:text-zinc-600 focus:outline-none focus:border-[#ffb0cf]/40"
                      />
                    </td>
                    <td className="py-2">
                      <div className="flex gap-1">
                        <button
                          onClick={handleUpsert}
                          disabled={saving || !formValues.provider || !formValues.model}
                          className="px-2 py-1 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] text-[10px] font-semibold rounded-sm disabled:opacity-40"
                        >
                          {saving ? '…' : 'Save'}
                        </button>
                        <button
                          onClick={() => { setShowAddForm(false); setFormValues(emptyForm) }}
                          className="px-2 py-1 bg-[#37333f] text-zinc-400 text-[10px] rounded-sm"
                        >
                          Cancel
                        </button>
                      </div>
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>

          {/* Budget Policy + Anomalies */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            <div className="bg-[#2c2834] p-6 rounded">
              <div className="flex justify-between items-center mb-6">
                <h4 className="text-base font-semibold text-[#e7dff0]">Budget Policy Table</h4>
                <button className="text-xs text-[#ffb0cf] font-semibold flex items-center gap-1 hover:underline">
                  <span className="material-symbols-outlined text-xs">add_circle</span>
                  New Policy
                </button>
              </div>
              <table className="w-full text-left text-xs">
                <thead>
                  <tr className="text-zinc-500 border-b border-[#574149]/10">
                    <th className="pb-3 font-medium">Policy Name</th>
                    <th className="pb-3 font-medium">Alert Trigger</th>
                    <th className="pb-3 font-medium">Enforcement</th>
                    <th className="pb-3 font-medium">Status</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#574149]/5">
                  {budgetPolicies.map(p => (
                    <tr key={p.id}>
                      <td className="py-4 font-medium text-[#e7dff0]">{p.name}</td>
                      <td className="py-4 text-zinc-400">{p.trigger}</td>
                      <td className="py-4">
                        <span className={`px-2 py-0.5 text-[10px] rounded font-semibold ${enfClass(p.enforcement)}`}>
                          {p.enforcement}
                        </span>
                      </td>
                      <td className="py-4">
                        <div className={`flex items-center gap-1.5 ${p.status === 'active' ? 'text-emerald-400' : 'text-zinc-500'}`}>
                          <div className={`w-1.5 h-1.5 rounded-full ${p.status === 'active' ? 'bg-emerald-400' : 'bg-zinc-600'}`}></div>
                          {p.status === 'active' ? 'Active' : 'Paused'}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="bg-[#2c2834] p-6 rounded">
              <h4 className="text-base font-semibold text-[#e7dff0] mb-6">Anomaly History</h4>
              <div className="space-y-4">
                {anomalies.map((a) => {
                  const borderColor = a.severity === 'error' ? 'border-[#ffb4ab]' : a.severity === 'warning' ? 'border-[#ffb0cf]' : 'border-zinc-700'
                  const iconBg = a.severity === 'error' ? 'bg-[#93000a]/20 text-[#ffb4ab]' : a.severity === 'warning' ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : 'bg-[#37333f] text-zinc-500'
                  const icon = a.severity === 'error' ? 'warning' : a.severity === 'warning' ? 'trending_up' : 'receipt_long'
                  return (
                    <div key={a.id} className={`bg-[#211e29] p-4 rounded border-l-2 ${borderColor}`}>
                      <div className="flex justify-between items-start">
                        <div className="flex items-center gap-3">
                          <div className={`p-2 rounded ${iconBg}`}>
                            <span className="material-symbols-outlined">{icon}</span>
                          </div>
                          <div>
                            <p className="font-semibold text-[#e7dff0]">{a.title}</p>
                            <p className="text-xs text-zinc-500">{a.description}</p>
                          </div>
                        </div>
                        <p className="text-[10px] text-zinc-500 font-semibold uppercase">{a.time_ago}</p>
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Verify the frontend builds cleanly**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/frontend
npm run build 2>&1 | tail -10
```

Expected: `✓ built in ...` with no errors.

- [ ] **Step 3: Commit**

```bash
cd /Users/gangtao/Code/timeplus/agentshield
git add frontend/src/pages/CostGovernance.jsx
git commit -m "feat(cost): wire real data and add pricing management panel to CostGovernance"
```

---

## Self-Verification Checklist

Before declaring done, confirm:

- [ ] `CGO_ENABLED=0 go build -o backend/server backend/cmd/server/main.go` succeeds
- [ ] `cd frontend && npm run build` succeeds
- [ ] `GET /api/cost/pricing` returns `{"pricing": [...]}` with 6 seed entries
- [ ] `POST /api/cost/pricing` with `{"provider":"test","model":"x","input_cost_per_million":1.0,"output_cost_per_million":2.0,"cache_read_cost_per_million":0.1}` returns `{"ok":true}`
- [ ] `DELETE /api/cost/pricing/test/x` returns `{"deleted":true}`
- [ ] `GET /api/cost` returns valid JSON (may have zero-value KPIs if no hook events exist)
- [ ] Cost Governance page loads without JS errors; pricing table renders seed data; Add Model form works end-to-end
