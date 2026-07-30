# Agent Attack & Anomaly (A19–A23)

Each rule has an **exploration query** over `agentguard_cim_event`.

## A19 提示词注入导致数据访问
Injection markers followed by sensitive retrieval / tool use. Review first.
- Fields: `user_message`, `tool_result`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, raw_hook_name, left(concat(user_message, ' ', tool_result), 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('user_input', 'tool_complete')
    AND (
         match(lower(concat(user_message, ' ', tool_result)), 'ignore [a-z ]{0,20}(previous|prior|all) instructions')
      OR multi_search_any(lower(concat(user_message, ' ', tool_result)), ['reveal the system prompt', 'show system prompt', 'print system prompt', '忽略以上指令', '忽略之前的指令', '忽略所有规则', 'read other user', 'access other user', '其他用户数据', 'call internal tool', 'invoke internal tool', 'use internal tool'])
    )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: the injection phrase, what followed it, `session_id`.

## A20 系统提示词或隐藏上下文泄露
System prompt / internal policy / tool defs / KB keys in a reply. Review first.
- Fields: `assistant_message` on `event_type = 'llm_response'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, left(assistant_message, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'llm_response'
    AND multi_search_any(lower(assistant_message), ['system prompt', 'my instructions are', 'internal policy', 'tool definition', '以下是我的系统提示', '我的系统指令', 'my system message', 'hidden context', 'knowledge base key', 'begin system', '<system>'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: the leaked fragment (masked), `session_id`, `event_time`.

## A21 智能体行为与声明能力不一致
Read-only agent doing writes / deletes; service agent calling dev/ops tools.
- Fields: `agent_id`, `tool_name`. Proxy — there is no declared-capability
  field, so this lists the tool verbs each agent actually used, to eyeball
  against its stated role.
- Exploration query:
  ```sql
  SELECT agent_id, tool_name, count() AS calls
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke' AND tool_name != ''
  GROUP BY agent_id, tool_name
  ORDER BY agent_id, calls DESC
  LIMIT 200
  ```
- Evidence: agent, the out-of-role tools it used, call counts.

## A22 异常调用频率和调用链
Anomaly signals — recursion / retry / rate-limit markers or a very long reply.
- Fields: `tool_result`, `assistant_message`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, raw_hook_name, left(assistant_message, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('tool_complete', 'llm_response')
    AND (
         multi_search_any(lower(tool_result), ['recursion', 'stack overflow', 'too many requests', 'rate limit', 'retry attempt', 'repeated failure', 'timeout'])
      OR length(assistant_message) > 50000
    )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: the anomaly signal, `session_id`. For call-frequency spikes, group
  by `session_id` over a time window and look for outliers.

## A23 安全策略命中但未阻断
A policy was flagged but the request still ran.
- Fields: `hook_decision`, `block_reason` on the event. This lists every event
  carrying a policy decision so you can see which were flagged and whether
  execution continued in the same session.
- Exploration query:
  ```sql
  SELECT event_time, event_type, session_id, tool_name, hook_decision, left(block_reason, 160) AS reason
  FROM table(agentguard_cim_event)
  WHERE hook_decision != '' OR block_reason != ''
  ORDER BY event_time DESC
  LIMIT 100
  ```
- Evidence: `hook_decision`, `block_reason`, whether a later `tool_complete`
  ran in the same `session_id`, `event_time`.
