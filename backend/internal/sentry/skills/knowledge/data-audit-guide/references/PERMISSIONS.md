# Permissions & Data Isolation (A08–A12)

Each rule has an **exploration query** over `agentguard_cim_event`. Several are
best-effort proxies because identity/policy fields are absent from the event
model — the query notes call it out.

## A08 跨用户或跨租户数据泄露
Data subject / tenant differs from the calling user.
- Fields: `tool_input`, `tool_result`. Proxy — there is no `tenant_id` column,
  so this flags tenant/org/customer identifier markers in tool traffic.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(concat(tool_input, ' ', tool_result), 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('tool_invoke', 'tool_complete', 'llm_response')
    AND multi_search_any(lower(concat(tool_input, ' ', tool_result)), ['tenant_id', 'tenantid', 'org_id', 'orgid', 'customer_id', 'account_id', 'cross-tenant', 'other tenant', '其他租户', '跨租户'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: mark "tenant identity not captured — heuristic"; `session_id`.

## A09 未授权工具调用
Tool outside the allow-list, or caller role lacks permission.
- Fields: `tool_name`. Proxy — there is no `role_id` / allow-list in the event
  model, so this flags high-privilege / admin tool-name patterns.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND multi_search_any(lower(tool_name), ['admin', 'sudo', 'root', 'grant', 'iam', 'privilege', 'delete_user', 'drop', 'truncate', 'exec', 'shell', 'system', 'chmod', 'setuid'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: tool, `agent_id`/`session_id`; confirm intent from the session.

## A10 越权参数调用
ID / path manipulation to reach out-of-scope data.
- Fields: `tool_input` on `event_type = 'tool_invoke'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND (
         position(tool_input, '../') > 0
      OR multi_search_any(lower(tool_input), ['/etc/passwd', '/root/', '%2e%2e', '..%2f', 'file://', 'user_id=', 'tenant_id=', 'resource_id=', 'org_id='])
    )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: the manipulated parameter / path, tool, `session_id`.

## A11 高风险工具缺少身份信息
High-risk verb with no attributable identity. There is no `user_id` column —
attribution is via `agent_id` / `session_id`.
- Fields: `tool_name`, `agent_id`, `session_id`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('tool_invoke', 'tool_complete')
    AND (agent_id = '' OR session_id = '')
    AND multi_search_any(lower(tool_name), ['export', 'download', 'send', 'delete', 'remove', 'modify', 'update', 'query', 'search', 'dump', 'backup', 'transfer'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: tool, which identity signal is missing, `event_time`.

## A12 策略拒绝后绕过调用
Re-attempt after a denial via another tool / encoding / split.
- Fields: `tool_input`. Proxy — single-event retry/encoding markers; confirm a
  true bypass by reading the surrounding call chain in the session.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('tool_invoke', 'tool_complete')
    AND multi_search_any(lower(tool_input), ['retry', 'bypass', 'workaround', 'alternate', 'another way', 'base64', 'fromcharcode', 'atob', 'decode', 'circumvent', '绕过', '重试'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: the retry/encoding marker, tool, `session_id`.
