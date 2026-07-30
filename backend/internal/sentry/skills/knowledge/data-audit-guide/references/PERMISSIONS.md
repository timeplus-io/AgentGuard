# Permissions & Data Isolation (A08–A12)

Compose each check from the **query skeleton** in `SKILL.md` — drop the
predicate below into `WHERE event_type … AND <predicate>`. Several are
best-effort proxies because identity/policy fields are absent from the event
model; the note says so. A12 is **behavioral** (a cross-event sequence), so it
carries a ready-to-run sample instead of a single-event predicate.

## Per-rule spec

### A08 跨用户或跨租户数据泄露 — proxy
No `tenant_id` column, so flag tenant/org/customer identifier markers in tool
traffic. event_type IN (`tool_invoke`, `tool_complete`, `llm_response`).
- Predicate: `multi_search_any(lower(concat(tool_input, ' ', tool_result)), ['tenant_id','tenantid','org_id','orgid','customer_id','account_id','cross-tenant','other tenant','其他租户','跨租户'])`
- Evidence: mark "tenant identity not captured — heuristic"; `session_id`.

### A09 未授权工具调用 — proxy
No `role_id`/allow-list, so flag high-privilege / admin tool-name patterns.
event_type = `tool_invoke`.
- Predicate: `multi_search_any(lower(tool_name), ['admin','sudo','root','grant','iam','privilege','delete_user','drop','truncate','exec','shell','system','chmod','setuid'])`
- Evidence: tool, `agent_id`/`session_id`; confirm intent from the session.

### A10 越权参数调用
ID / path manipulation to reach out-of-scope data. event_type = `tool_invoke`.
- Predicate: `position(tool_input, '../') > 0 OR multi_search_any(lower(tool_input), ['/etc/passwd','/root/','%2e%2e','..%2f','file://','user_id=','tenant_id=','resource_id=','org_id='])`
- Evidence: the manipulated parameter / path, tool, `session_id`.

### A11 高风险工具缺少身份信息
High-risk verb with no attributable identity. No `user_id` column — attribution
is via `agent_id`/`session_id`. event_type IN (`tool_invoke`, `tool_complete`).
- Predicate: `(agent_id = '' OR session_id = '') AND multi_search_any(lower(tool_name), ['export','download','send','delete','remove','modify','update','query','search','dump','backup','transfer'])`
- Evidence: tool, which identity signal is missing, `event_time`.

### A12 策略拒绝后绕过调用 (behavioral / cross-event)
The real signal is a **denied call followed by continued tool use in the same
session** — not the word "retry" in `tool_input`. Use the policy-decision
columns (`hook_decision` / `block_reason`): find each session's first denial,
then any tool call after it.

```sql
WITH denials AS (
  SELECT session_id, min(event_time) AS first_denial
  FROM table(agentguard_cim_event)
  WHERE lower(hook_decision) IN ('block', 'deny', 'denied', 'reject', 'rejected', 'ask')
     OR block_reason != ''
  GROUP BY session_id
)
SELECT e.event_time, e.agent_id, e.session_id, e.tool_name,
       left(e.tool_input, 200) AS retry_evidence, d.first_denial
FROM table(agentguard_cim_event) AS e
JOIN denials AS d ON e.session_id = d.session_id
WHERE e.event_type = 'tool_invoke' AND e.event_time > d.first_denial
ORDER BY e.event_time DESC
LIMIT 50
```
- If `hook_decision` is `observe` everywhere (monitoring-only deployment) there
  are no denials to bypass — report "no blocking policy in effect". To focus on
  same-goal retries via a different tool, add `AND e.tool_name != <denied tool>`.
- Evidence: the denied event, the subsequent tool + params, `session_id`.
