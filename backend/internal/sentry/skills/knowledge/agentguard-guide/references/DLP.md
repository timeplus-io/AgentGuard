# DLP Enriched Messages & Risk Profiles

`agentguard_dlp_enriched` holds one enriched row per conversation message
(user inputs, LLM responses, tool completions), written live by the DLP
ingest MV.

Columns: `event_time`, `message_id`, `session_id`, `run_id`, `role`,
`agent_type`, `agent_id`, `deployment_id`, `model`, `tool_name`, `content`,
`entities` (map: entity type → array of masked values), `risk_tags`
(array), `risk_score` (float, 0 = benign), `embedding` (array(float32)).

Useful predicates:

| Question | Predicate |
|----------|-----------|
| Has risk tag X | `has(risk_tags, '<tag>')` |
| Contains entity type X | `not empty(entities['<type>'])` |
| Risky at all | `risk_score > 0` |
| Keyword in content | `position(lower(content), '<kw>') > 0` |

Project `length(embedding) AS embedding_dim` instead of the raw vector —
embeddings are large and useless to read.

## Browse flagged messages

```sql
SELECT event_time, message_id, session_id, role, agent_id, model, tool_name,
       content, entities, risk_tags, risk_score
FROM table(agentguard_dlp_enriched)
WHERE risk_score > 0 AND event_time >= now() - INTERVAL 7 DAY
ORDER BY risk_score DESC, event_time DESC LIMIT 50
```

Add `has(risk_tags, 'credential_exposure')`, `agent_id = '...'`,
`role = 'user'` etc. as needed. One message: `WHERE message_id = '<id>'`.

## Risk profiles (top offenders)

Same shape, three key expressions — `agent_id`, `session_id`, or
`array_join(risk_tags)` (explodes the array so each tag ranks independently):

```sql
SELECT agent_id AS key, count() AS flagged_count,
       max(risk_score) AS max_risk_score, avg(risk_score) AS avg_risk_score,
       max(event_time) AS last_seen
FROM table(agentguard_dlp_enriched)
WHERE risk_score > 0 AND event_time >= now() - INTERVAL 30 DAY
GROUP BY key ORDER BY flagged_count DESC, max_risk_score DESC LIMIT 10
```

## Enrichment health

```sql
SELECT count() AS total,
       count_if(length(embedding) > 0) AS embedded,
       count_if(risk_score > 0) AS flagged
FROM table(agentguard_dlp_enriched)
```

Low `embedded/total` coverage means the embedding endpoint was down during
ingest (DLP fails open) — semantic search will silently miss those rows.

## Semantic search: use the dlp_search tool

For meaning-based questions ("did anyone paste credentials?", "find messages
about the payment service") use the **`dlp_search` tool**, not hand-written
SQL — it runs the full understand → recall (structured + vector) → rerank
funnel with the configured models. Hand-written SQL is right for exact
filters (tags, entities, agents, time ranges, scores) like the queries above.
