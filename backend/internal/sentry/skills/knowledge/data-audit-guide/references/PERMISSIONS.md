# Permissions & Data Isolation (A08–A12)

## A08 跨用户或跨租户数据泄露 — ✅ `dsa-a08` (best-effort)
Data subject / tenant ≠ calling user.
- Gap: no `tenant_id` in CIM (#261). Rule detects tenant/org/customer-id
  markers in `tool_input`/`tool_result` only. Confirm by hand:
  ```sql
  SELECT session_id, tool_name, left(tool_input, 200) FROM table(agentguard_cim_event)
  WHERE event_type='tool_invoke'
    AND multi_search_any(lower(tool_input), ['tenant_id','customer_id','org_id'])
  ORDER BY event_time DESC LIMIT 50
  ```
- Evidence: mark "tenant identity not captured — heuristic".

## A09 未授权工具调用 — ✅ `dsa-a09` (best-effort)
Tool outside allow-list, or caller role lacks permission.
- Gap: no `role_id`, no per-deployment allow-list in CIM. Rule flags
  high-privilege tool-name patterns. Confirm intent by inspecting the session.

## A10 越权参数调用 — ✅ `dsa-a10`
ID / path manipulation to reach out-of-scope data.
- Fields: `tool_input` on `tool_invoke`. Detections: `rule_id='dsa-a10'`.

## A11 高风险工具缺少身份信息 — ✅ `dsa-a11`
High-risk verb with no attributable agent/session identity. There is no
`user_id` column in CIM — attribution is via `agent_id`/`session_id`.
- Cross-check which high-risk calls lack an `agent_id` or `session_id`:
  ```sql
  SELECT tool_name, count() FROM table(agentguard_cim_event)
  WHERE event_type='tool_invoke' AND (agent_id='' OR session_id='')
    AND multi_search_any(lower(tool_name), ['export','delete','send'])
  GROUP BY tool_name ORDER BY count() DESC
  ```

## A12 策略拒绝后绕过调用 — ✅ `dsa-a12` (best-effort)
Re-attempt after a denial via another tool / encoding / split.
- Gap: cross-event denial→retry correlation needs `policy_decision` (#261).
  Rule flags single-event retry/encoding markers. Investigate the surrounding
  call chain in the session timeline to confirm a true bypass.
