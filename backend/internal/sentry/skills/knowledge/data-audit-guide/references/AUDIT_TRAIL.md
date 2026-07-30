# Audit Trail & Traceability (A24–A27)

Each rule has an **exploration query** over `agentguard_cim_event`. Several
target fields the event model does not carry, so the query measures the closest
observable proxy.

## A24 审计字段缺失
Events missing key attribution fields.
- Fields: `session_id`, `agent_id`, `run_id`. (`tenant_id` / `role_id` are not
  in the event model — report those as "not captured".)
- Exploration query:
  ```sql
  SELECT
    count_if(session_id = '') AS no_session,
    count_if(agent_id = '')   AS no_agent,
    count_if(run_id = '')     AS no_run,
    count()                   AS total
  FROM table(agentguard_cim_event)
  WHERE event_time >= now() - INTERVAL 1 DAY
  ```
- Evidence: which field is empty and on how many events over the window.

## A25 调用链断裂
A tool event not associable to its conversation / run.
- Fields: `session_id`, `run_id` on tool events.
- Exploration query:
  ```sql
  SELECT event_time, event_type, tool_name, session_id, run_id
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('tool_invoke', 'tool_complete') AND (run_id = '' OR session_id = '')
  ORDER BY event_time DESC
  LIMIT 100
  ```
- Evidence: the orphaned tool event, which linkage key is empty.

## A26 模型和策略版本不可追溯
Missing model / version identity on LLM events.
- Fields: `model`. (`model_version` / `prompt_version` / `policy_version` are
  not in the event model — report those as "not captured".)
- Exploration query:
  ```sql
  SELECT
    count_if(model = '') AS missing_model,
    count()              AS total_llm_events
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('llm_request', 'llm_response')
  ```
- Evidence: count of LLM events missing `model`; note absent version fields.

## A27 日志内容过度留存
Full conversation / tool result retained when only a summary is needed.
- Fields: `raw_event_data`. Proxy — surface the largest raw payloads as
  candidates for over-retention review.
- Exploration query:
  ```sql
  SELECT event_time, event_type, length(raw_event_data) AS bytes, left(raw_event_data, 120) AS head
  FROM table(agentguard_cim_event)
  ORDER BY bytes DESC
  LIMIT 20
  ```
- Evidence: payload size, event type; this is a retention-policy judgment.
