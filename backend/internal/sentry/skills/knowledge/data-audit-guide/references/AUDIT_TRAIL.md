# Audit Trail & Traceability (A24–A27)

Each rule states its **goal**, a **sample case**, and an **example query**. These
are aggregations / payload scans over `agentguard_cim_event`; several target
fields the event model does not carry, so the example measures the closest
observable proxy — say which field is "not captured".

## A24 审计字段缺失 (aggregation)
**Detects:** events **missing key fields** — user / tenant / time / session id /
agent / model / tool / operation type / input-output summary. (`tenant_id` /
`role_id` are absent from the model — report those as "not captured".)
**Sample case:** 12% of the day's events have empty `session_id`; there is no `tenant_id`/`role_id` at all.
**Example:**
```sql
SELECT
  count_if(session_id = '') AS no_session,
  count_if(agent_id = '')   AS no_agent,
  count_if(run_id = '')     AS no_run,
  count()                   AS total
FROM table(agentguard_cim_event)
WHERE event_time >= now() - INTERVAL 1 DAY
```
- Timeplus uses `count_if` (snake_case), not `countIf`.
**Evidence:** which field is empty and on how many events over the window.

## A25 调用链断裂
**Detects:** a **tool call that cannot be linked** to the conversation / model
response / user that triggered it, or a tool result that cannot be linked to the
final reply — an orphan in the chain (empty `run_id` / `session_id`).
**Sample case:** a `tool_complete` with empty `run_id` and `session_id`.
**Example:**
```sql
SELECT event_time, event_type, tool_name, session_id, run_id
FROM table(agentguard_cim_event)
WHERE event_type IN ('tool_invoke','tool_complete') AND (run_id = '' OR session_id = '')
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** the orphaned tool event, which linkage key is empty.

## A26 模型和策略版本不可追溯 (aggregation)
**Detects:** events **missing version identity** — model version / agent version /
system-prompt version / tool version / security-policy version. Only `model` is
present; the version fields are absent — report them "not captured".
**Sample case:** `llm_response` rows with `model` blank; no `model_version` / `prompt_version` captured anywhere.
**Example:**
```sql
SELECT
  count_if(model = '') AS missing_model,
  count()              AS total_llm_events
FROM table(agentguard_cim_event)
WHERE event_type IN ('llm_request', 'llm_response')
```
**Evidence:** count of LLM events missing `model`; note the absent version fields.

## A27 日志内容过度留存 (payload scan)
**Detects:** the audit log keeps the **full conversation or full tool result**,
when the audit only needs a summary / hash / data-label / masked hit fragment.
Surface the largest raw payloads as candidates for over-retention review.
**Sample case:** `raw_event_data` rows of 200 KB storing entire tool outputs verbatim.
**Example:**
```sql
SELECT event_time, event_type, length(raw_event_data) AS bytes, left(raw_event_data, 120) AS head
FROM table(agentguard_cim_event)
ORDER BY bytes DESC
LIMIT 20
```
**Evidence:** payload size, event type; this is a retention-policy judgment.
