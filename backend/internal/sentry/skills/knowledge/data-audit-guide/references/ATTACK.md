# Agent Attack & Anomaly (A19–A23)

## A19 提示词注入导致数据访问 — ✅ `dsa-a19`
Injection markers then sensitive retrieval / tool call. Detections:
`rule_id='dsa-a19'`.
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
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

Priority rule.

## A20 系统提示词或隐藏上下文泄露 — ✅ `dsa-a20`
System prompt / internal policy / tool defs / KB keys in a reply. Detections:
`rule_id='dsa-a20'`.
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
  ```sql
  SELECT event_time, agent_id, session_id, left(assistant_message, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'llm_response'
    AND multi_search_any(lower(assistant_message), ['system prompt', 'my instructions are', 'internal policy', 'tool definition', '以下是我的系统提示', '我的系统指令', 'my system message', 'hidden context', 'knowledge base key', 'begin system', '<system>'])
  ORDER BY event_time DESC
  LIMIT 50
  ```

Priority rule.

## A21 智能体行为与声明能力不一致 — 🔍 investigate
Read-only agent doing writes/deletes; service agent calling dev/ops/DB tools;
simple Q&A triggering bulk export.
- Gap: no declared-capability / `role_id` context in CIM. Best-effort — list
  the tool verbs a given agent actually used and eyeball against its stated
  role:
  ```sql
  SELECT agent_id, tool_name, count() FROM table(agentguard_cim_event)
  WHERE event_type='tool_invoke' GROUP BY agent_id, tool_name ORDER BY agent_id
  ```

## A22 异常调用频率和调用链 — ✅ `dsa-a22`
Single-event anomaly signals (recursion/retry/rate-limit/huge output).
Detections: `rule_id='dsa-a22'`.
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
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

Frequency/spike analysis is investigate-only —
count calls per session over a window as in A17.

## A23 安全策略命中但未阻断 — 🔍 investigate
Gateway flagged sensitive/authz/injection but the request still ran.
- Gap: needs `policy_decision` + whether execution proceeded — cross-stream and
  not in CIM (#261). Proxy: a `dsa-*` security event whose session also has a
  later successful `tool_complete`:
  ```sql
  SELECT s.session_id, s.rule_id, s.detected_at
  FROM table(agentguard_security_events) AS s
  WHERE s.detected_at >= now() - INTERVAL 1 DAY
  ORDER BY s.detected_at DESC LIMIT 100
  ```
  then check the session timeline for continued execution. Report as proxy.
