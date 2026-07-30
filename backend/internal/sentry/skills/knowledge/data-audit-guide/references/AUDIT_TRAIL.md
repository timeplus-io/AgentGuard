# Audit Trail & Traceability (A24–A27)

Each entry states the **goal** and an **example** query to adapt to the user's
request. These are aggregations / payload scans over `agentguard_cim_event` (not
the single-event skeleton); several target fields the event model does not carry,
so the example measures the closest observable proxy — say which field is "not
captured".

### A24 审计字段缺失 (aggregation)
Count events missing key attribution fields. (`tenant_id`/`role_id` are not in the
model — report those as "not captured".)

```sql
SELECT
  count_if(session_id = '') AS no_session,
  count_if(agent_id = '')   AS no_agent,
  count_if(run_id = '')     AS no_run,
  count()                   AS total
FROM table(agentguard_cim_event)
WHERE event_time >= now() - INTERVAL 1 DAY
```
- Note: Timeplus uses `count_if` (snake_case), not `countIf`.
- Evidence: which field is empty and on how many events over the window.

### A25 调用链断裂
Tool events not associable to a conversation / run. event_type IN
(`tool_invoke`, `tool_complete`).
- Example: `run_id = '' OR session_id = ''` (drop into the skeleton, selecting
  `event_time, event_type, tool_name, session_id, run_id`).
- Evidence: the orphaned tool event, which linkage key is empty.

### A26 模型和策略版本不可追溯 (aggregation)
Count LLM events missing model identity. (`model_version`/`prompt_version`/
`policy_version` are not in the model — report those as "not captured".)

```sql
SELECT
  count_if(model = '') AS missing_model,
  count()              AS total_llm_events
FROM table(agentguard_cim_event)
WHERE event_type IN ('llm_request', 'llm_response')
```
- Evidence: count of LLM events missing `model`; note absent version fields.

### A27 日志内容过度留存 (payload scan)
Surface the largest raw payloads as candidates for over-retention review.

```sql
SELECT event_time, event_type, length(raw_event_data) AS bytes, left(raw_event_data, 120) AS head
FROM table(agentguard_cim_event)
ORDER BY bytes DESC
LIMIT 20
```
- Evidence: payload size, event type; this is a retention-policy judgment.
