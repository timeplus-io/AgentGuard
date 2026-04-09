# Cost Governance Feature — Design Spec

**Date:** 2026-04-08
**Branch:** feature/22-cost-page
**Status:** Approved

## Overview

Replace the mock-backed `GET /api/cost` endpoint with real data derived from a JOIN between the existing `agentshield_hook_events` stream (which already contains per-event token usage) and a new `agentshield_pricing` mutable stream (provider/model → token prices). Add a pricing management UI panel so users can view and edit model prices without touching Proton directly.

Budget policies and anomalies remain as mock data — out of scope for this feature.

---

## 1. Data Layer

### 1.1 Mutable Stream: `agentshield_pricing`

```sql
CREATE MUTABLE STREAM agentshield_pricing (
  provider                    low_cardinality(string),
  model                       low_cardinality(string),
  input_cost_per_million      float64,
  output_cost_per_million     float64,
  cache_read_cost_per_million float64,
  updated_at                  datetime64(3)
) PRIMARY KEY (provider, model)
```

Mutable streams support upserts by primary key. `table(agentshield_pricing)` always returns the latest row per `(provider, model)` — correct semantics for a pricing reference table where values change over time without needing history.

### 1.2 Seed Data

Inserted at startup via `seedPricingData()` in `internal/timeplus/init.go`, using the same guard pattern as `seedBuiltinRules`: check `SELECT count() FROM table(agentshield_pricing)` and skip if non-zero.

| Provider  | Model                    | Input $/1M | Output $/1M | Cache-Read $/1M |
|-----------|--------------------------|-----------|------------|----------------|
| anthropic | claude-sonnet-4-6        | 3.00      | 15.00      | 0.30           |
| anthropic | claude-opus-4-6          | 15.00     | 75.00      | 1.50           |
| anthropic | claude-haiku-4-5         | 0.80      | 4.00       | 0.08           |
| openai    | gpt-4o                   | 2.50      | 10.00      | 1.25           |
| openai    | gpt-4o-mini              | 0.15      | 0.60       | 0.075          |
| openai    | o3                       | 10.00     | 40.00      | 2.50           |

### 1.3 Core Cost Query

All cost aggregations derive from this JOIN pattern:

```sql
SELECT
  e.agent_id,
  e.deployment_id,
  e.provider,
  e.model,
  sum(to_int64(json_extract_string(e.event_data, 'usage', 'input')))                        AS input_tokens,
  sum(to_int64(json_extract_string(e.event_data, 'usage', 'output')))                       AS output_tokens,
  sum(to_int64(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))   AS cache_tokens,
  any(p.input_cost_per_million)                                                              AS input_cpm,
  any(p.output_cost_per_million)                                                             AS output_cpm,
  any(p.cache_read_cost_per_million)                                                         AS cache_cpm,
  (
    sum(to_int64(json_extract_string(e.event_data, 'usage', 'input')))
      * any(p.input_cost_per_million)
    + sum(to_int64(json_extract_string(e.event_data, 'usage', 'output')))
      * any(p.output_cost_per_million)
    + sum(to_int64(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))
      * any(p.cache_read_cost_per_million)
  ) / 1000000.0                                                                              AS total_cost
FROM table(agentshield_hook_events) AS e
LEFT JOIN table(agentshield_pricing) AS p
  ON e.provider = p.provider AND e.model = p.model
WHERE e.hook_name = 'llm_output'
GROUP BY e.agent_id, e.deployment_id, e.provider, e.model
ORDER BY total_cost DESC
```

Agents using models not in the pricing table get `NULL` costs (LEFT JOIN). The handler treats `NULL` as `0.0` and the frontend shows `$0.00`.

---

## 2. Backend API

### 2.1 File Structure

`internal/handlers/cost.go` — replaces the current mock-based implementation. Organized as:
- `GetCost(cfg)` — main handler, orchestrates three sub-queries
- `ListPricing(cfg)` — list all pricing entries
- `UpsertPricing(cfg)` — insert/update a pricing entry
- `DeletePricing(cfg)` — delete a pricing entry
- Private helpers: `queryCostByAgent`, `queryCostDailyTrend`, `queryCostTokenBreakdown`

### 2.2 Endpoints

| Method   | Path                              | Description |
|----------|-----------------------------------|-------------|
| `GET`    | `/api/cost`                       | All cost aggregates (replaces mock) |
| `GET`    | `/api/cost/pricing`               | List all pricing entries |
| `POST`   | `/api/cost/pricing`               | Upsert a pricing entry |
| `DELETE` | `/api/cost/pricing/:provider/:model` | Delete a pricing entry |

Routes registered in `main.go` before the existing `/api/cost` line (which changes from a plain handler to a closure).

### 2.3 `GET /api/cost` Handler

Runs three Proton queries in sequence:

1. **`queryCostByAgent`** — the core join (Section 1.3). Returns `[]AgentModelCost` for the cost-per-agent cards and is used to derive MTD total and average token cost.
2. **`queryCostDailyTrend`** — same join grouped by `to_start_of_day(e.event_time)`, last 30 days. Returns `[]DailySpend` for the bar chart.
3. **`queryCostTokenBreakdown`** — aggregates total input/output/cache tokens and their dollar amounts across all agents. Returns `[]TokenBreakdown` for the breakdown panel.

KPIs derived from query results:
- `MonthToDate` — `queryCostByAgent` is called with `since = start of current month` to scope the sum to MTD only.
- `Forecasted` — MTD spend extrapolated to end of month: `mtd / elapsed_days * 30`
- `AvgTokenCost` — total cost / total tokens * 1,000,000 (cost per 1M tokens), computed from the unbounded `queryCostByAgent` result.
- `PolicyBreaches` — remains hardcoded `2` (mock, out of scope)

The agent/model cost cards use a separate unbounded call to `queryCostByAgent` (no time filter) so they show all-time cost.

If Proton is unavailable (query error), the handler returns `503` with `{"error": "data store unavailable"}` — consistent with `GetLLMMetrics`.

### 2.4 New Models (`internal/models/models.go`)

```go
type PricingEntry struct {
    Provider               string  `json:"provider"`
    Model                  string  `json:"model"`
    InputCostPerMillion    float64 `json:"input_cost_per_million"`
    OutputCostPerMillion   float64 `json:"output_cost_per_million"`
    CacheReadCostPerMillion float64 `json:"cache_read_cost_per_million"`
    UpdatedAt              string  `json:"updated_at"`
}

type PricingListResponse struct {
    Pricing []PricingEntry `json:"pricing"`
}
```

`AgentModelCost` (already exists) gains two fields:
```go
type AgentModelCost struct {
    // existing fields
    Model      string  `json:"model"`
    AgentName  string  `json:"agent_name"`   // maps to agent_id
    Cost       float64 `json:"cost"`
    Requests   string  `json:"requests"`
    // new fields
    Provider   string  `json:"provider"`
    AgentID    string  `json:"agent_id"`
    InputTokens  int64 `json:"input_tokens"`
    OutputTokens int64 `json:"output_tokens"`
    CacheTokens  int64 `json:"cache_tokens"`
}
```

`DailySpend` and `TokenBreakdown` already exist in `models.go` — no changes needed.

---

## 3. Frontend

### 3.1 `api/client.js`

Replace:
```js
getCost: () => get('/cost'),
```
With:
```js
getCost: (params = {}) => {
  const q = new URLSearchParams(params).toString()
  return get(`/cost${q ? '?' + q : ''}`)
},
listPricing: () => get('/cost/pricing'),
upsertPricing: (entry) => post('/cost/pricing', entry),
deletePricing: (provider, model) =>
  fetch(`${BASE}/cost/pricing/${encodeURIComponent(provider)}/${encodeURIComponent(model)}`, { method: 'DELETE' })
    .then(res => { if (!res.ok) throw new Error(`HTTP ${res.status}`); return res.json() }),
```

### 3.2 `CostGovernance.jsx`

The existing layout and all visual patterns are preserved. Changes are data-wiring only:

- Remove `staticSpendData`, `agentModels`, `budgetPolicies`, `anomalies` static arrays from the file.
- Add `pricing` state alongside `data` and `loading`.
- Add second `useEffect` to call `api.listPricing()`.
- Wire `data.daily_spend` → bar chart (day label formatted as `"Apr 06"`).
- Wire `data.token_breakdown` → percentage bars (computed amounts and ratios).
- Wire `data.agent_model_costs` → cost-per-agent cards (real `agent_id`, `model`, `provider`, formatted cost).
- KPI values wired directly: `data.month_to_date`, `data.forecasted`, `data.avg_token_cost`.
- `budget_policies` and `anomalies` still come from `data.*` (backend passes mock data through).

**New panel: Model Pricing** — inserted between the "Cost per Agent/Model" card and the existing "Budget Policy / Anomaly" row. Uses the same `bg-[#2c2834] p-6 rounded` card container and the same `<table>` + `divide-y` pattern as the Budget Policy Table already in the page.

The panel has three states:
1. **List view** — table of current entries, each row has a delete (✕) button.
2. **Add form** — clicking "+ Add Model" reveals an inline form row at the bottom of the table (provider input, model input, three price inputs, Save + Cancel).
3. **Saving/deleting** — button disabled with loading indicator, re-fetches pricing list on success.

No new color tokens or component patterns are introduced.

---

## 4. Init Changes (`internal/timeplus/init.go`)

Two additions to the `Init()` call sequence:

```go
if err := createPricingStream(ctx, conn); err != nil {
    return fmt.Errorf("timeplus init: creating pricing stream: %w", err)
}
if err := seedPricingData(ctx, conn); err != nil {
    log.Printf("timeplus init: warning seeding pricing: %v", err)
}
```

`createPricingStream` uses the existing `execIfNotExists` helper with `kind="stream"`. Mutable streams appear in `system.tables` just like regular streams, so the helper works unchanged.
`seedPricingData` checks count, then uses `conn.PrepareBatch` to insert all seed rows (same pattern as `seedBuiltinRules`).

---

## 5. Implementation Order

1. Add `agentshield_pricing` DDL constant and `createPricingStream` + `seedPricingData` to `init.go`
2. Add `PricingEntry` and `PricingListResponse` models, update `AgentModelCost` in `models.go`
3. Rewrite `handlers/cost.go` with real Proton queries and pricing CRUD handlers
4. Register new routes in `main.go`
5. Update `api/client.js` with new functions
6. Update `CostGovernance.jsx` — replace static data, add pricing panel
