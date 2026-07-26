# Cost, Pricing, Budgets

Cost is **computed, not stored**: token metrics joined to per-model prices.

- **`agentguard_cim_metrics`** — token counts (see `METRICS.md`).
- **`agentguard_pricing`** (mutable, PK `(provider, model)`): `provider`,
  `model`, `input_cost_per_million`, `output_cost_per_million`,
  `cache_read_cost_per_million`, `updated_at`.
- **`agentguard_cost_budgets`** (append log): `deployment_id`,
  `monthly_budget`, `currency`, `updated_by`, `deleted`, `updated_at`.

## The cost formula

`cost = tokens × price_per_million / 1,000,000`, joined on
`(tags['provider'], tags['model'])` = `(provider, model)`. Always GROUP BY
provider+model (so `any(price)` is deterministic) and roll up afterwards.
A model missing from `agentguard_pricing` contributes **zero** cost (LEFT
JOIN + null prices) — flag that when totals look suspiciously low.

## Cost by agent and model (the canonical query)

```sql
SELECT m.agent_id, m.deployment_id,
       m.tags['provider'] AS provider, m.tags['model'] AS model,
       to_int64(sum_if(m.metric_value, m.metric_name = 'llm.call'))              AS request_count,
       to_int64(sum_if(m.metric_value, m.metric_name = 'llm.input_tokens'))      AS input_tokens,
       to_int64(sum_if(m.metric_value, m.metric_name = 'llm.output_tokens'))     AS output_tokens,
       to_int64(sum_if(m.metric_value, m.metric_name = 'llm.cache_read_tokens')) AS cache_tokens,
       ( sum_if(m.metric_value, m.metric_name = 'llm.input_tokens')      * any(p.input_cost_per_million)
       + sum_if(m.metric_value, m.metric_name = 'llm.output_tokens')     * any(p.output_cost_per_million)
       + sum_if(m.metric_value, m.metric_name = 'llm.cache_read_tokens') * any(p.cache_read_cost_per_million)
       ) / 1000000.0 AS total_cost
FROM table(agentguard_cim_metrics) AS m
LEFT JOIN table(agentguard_pricing) AS p
  ON m.tags['provider'] = p.provider AND m.tags['model'] = p.model
WHERE m.metric_name IN ('llm.call', 'llm.input_tokens', 'llm.output_tokens', 'llm.cache_read_tokens')
  AND m.event_time >= '<since>' AND m.event_time < '<until>'
GROUP BY m.agent_id, m.deployment_id, provider, model
ORDER BY total_cost DESC
```

## Month windows (match the Cost page's numbers)

Windows are **UTC calendar months**, half-open `[since, until)`, timestamps
formatted `YYYY-MM-DD HH:MM:SS`:

- Month-to-date: `[first-of-month 00:00 UTC, now)`.
- Prior-period comparison: the **same elapsed window** in the previous month
  `[first-of-prev-month, first-of-prev-month + elapsed)`, clipped to the
  start of the current month when the previous month was shorter.
- Forecast = `MTD_spend / elapsed_days × 30`.

## Daily trend (last 30 days)

```sql
SELECT to_start_of_day(m.event_time) AS day,
       m.tags['provider'] AS provider, m.tags['model'] AS model,
       ( sum_if(m.metric_value, m.metric_name = 'llm.input_tokens')      * any(p.input_cost_per_million)
       + sum_if(m.metric_value, m.metric_name = 'llm.output_tokens')     * any(p.output_cost_per_million)
       + sum_if(m.metric_value, m.metric_name = 'llm.cache_read_tokens') * any(p.cache_read_cost_per_million)
       ) / 1000000.0 AS cost
FROM table(agentguard_cim_metrics) AS m
LEFT JOIN table(agentguard_pricing) AS p
  ON m.tags['provider'] = p.provider AND m.tags['model'] = p.model
WHERE m.metric_name IN ('llm.input_tokens', 'llm.output_tokens', 'llm.cache_read_tokens')
  AND m.event_time >= now() - INTERVAL 30 DAY
GROUP BY day, provider, model ORDER BY day ASC
```

(Sum across models per day after the query, or wrap in an outer
`SELECT day, sum(cost) GROUP BY day`.)

## Pricing table

```sql
SELECT provider, model, input_cost_per_million, output_cost_per_million,
       cache_read_cost_per_million, updated_at
FROM table(agentguard_pricing) ORDER BY provider, model
```

## Current budgets (append-log reconstruction)

```sql
SELECT deployment_id,
       arg_max(monthly_budget, updated_at) AS monthly_budget,
       arg_max(currency, updated_at)       AS currency,
       arg_max(updated_by, updated_at)     AS updated_by,
       arg_max(deleted, updated_at)        AS deleted,
       max(updated_at)                     AS last_updated
FROM table(agentguard_cost_budgets)
GROUP BY deployment_id HAVING NOT deleted
ORDER BY deployment_id
```

Budget status vs forecast: within ±5% of the summed budgets → `on`, above →
`over`, below → `under`.
