# LLM & Tool Metrics

All metrics live in `agentguard_cim_metrics`: one row per scalar sample.

Columns: `event_time`, `metric_name`, `metric_value` (float64),
`agent_type`, `agent_id`, `deployment_id`, `session_id`,
`tags` (map: `tags['provider']`, `tags['model']`, `tags['stop_reason']`,
`tags['tool_name']`, `tags['run_id']`).

`metric_name` vocabulary:

| Name | Meaning |
|------|---------|
| `llm.call` | One LLM call (value 1; `tags['stop_reason']` set) |
| `llm.input_tokens` / `llm.output_tokens` / `llm.total_tokens` | Token counts per call |
| `llm.cache_read_tokens` / `llm.cache_write_tokens` | Prompt-cache tokens |
| `llm.latency_ms` | Call latency |
| `tool.call` / `tool.success` | Tool invocations / successes |
| `tool.duration_ms` | Tool latency |

Common filters: `agent_id = '...' AND deployment_id = '...'`,
`tags['model'] = '...'`, `event_time >= ...`.

## Token totals

```sql
SELECT to_int64(sum_if(metric_value, metric_name = 'llm.call'))               AS total_calls,
       to_int64(sum_if(metric_value, metric_name = 'llm.input_tokens'))       AS input_tokens,
       to_int64(sum_if(metric_value, metric_name = 'llm.output_tokens'))      AS output_tokens,
       to_int64(sum_if(metric_value, metric_name = 'llm.total_tokens'))       AS total_tokens,
       to_int64(sum_if(metric_value, metric_name = 'llm.cache_read_tokens'))  AS cache_read_tokens,
       to_int64(sum_if(metric_value, metric_name = 'llm.cache_write_tokens')) AS cache_write_tokens
FROM table(agentguard_cim_metrics)
WHERE metric_name IN ('llm.call', 'llm.input_tokens', 'llm.output_tokens',
                      'llm.total_tokens', 'llm.cache_read_tokens', 'llm.cache_write_tokens')
  AND event_time >= now() - INTERVAL 1 DAY
```

## Success vs failure (stop reasons)

```sql
SELECT tags['stop_reason'] AS stop_reason, to_int64(sum(metric_value)) AS cnt
FROM table(agentguard_cim_metrics)
WHERE metric_name = 'llm.call' AND event_time >= now() - INTERVAL 1 DAY
GROUP BY stop_reason
```

Semantics: successful calls = every `stop_reason` except `'error'`;
failed = total − successful.

## Latency percentiles

```sql
SELECT quantile(0.5)(metric_value) AS p50_ms, quantile(0.95)(metric_value) AS p95_ms,
       quantile(0.99)(metric_value) AS p99_ms, avg(metric_value) AS avg_ms,
       min(metric_value) AS min_ms, max(metric_value) AS max_ms, count() AS sample_count
FROM table(agentguard_cim_metrics)
WHERE metric_name = 'llm.latency_ms' AND metric_value > 0
  AND event_time >= now() - INTERVAL 1 DAY
```

Always add `metric_value > 0` for latency (zero = missing sample).

## Throughput time series

```sql
SELECT to_start_of_hour(event_time) AS bucket,
       to_int64(sum_if(metric_value, metric_name = 'llm.call'))          AS llm_calls,
       to_int64(sum_if(metric_value, metric_name = 'llm.input_tokens'))  AS input_tokens,
       to_int64(sum_if(metric_value, metric_name = 'llm.output_tokens')) AS output_tokens
FROM table(agentguard_cim_metrics)
WHERE metric_name IN ('llm.call', 'llm.input_tokens', 'llm.output_tokens')
  AND event_time >= now() - INTERVAL 1 DAY
GROUP BY bucket ORDER BY bucket ASC
```

Granularity: `to_start_of_minute` / `to_start_of_hour` / `to_start_of_day`.

## Per-tool metrics

```sql
SELECT tags['tool_name'] AS tool_name,
       to_int64(sum_if(metric_value, metric_name = 'tool.call'))    AS total_calls,
       to_int64(sum_if(metric_value, metric_name = 'tool.success')) AS success_calls,
       to_int64(sum_if(metric_value, metric_name = 'tool.call'))
     - to_int64(sum_if(metric_value, metric_name = 'tool.success')) AS failed_calls
FROM table(agentguard_cim_metrics)
WHERE metric_name IN ('tool.call', 'tool.success')
GROUP BY tool_name ORDER BY total_calls DESC
```

Tool latency: same percentile shape as above with
`metric_name = 'tool.duration_ms' AND metric_value > 0`, grouped by
`tags['tool_name']`.
