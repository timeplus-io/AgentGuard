# Permissions & Data Isolation (A08–A12)

Compose each check from the **query skeleton** in `SKILL.md` — drop the
predicate below into `WHERE event_type … AND <predicate>`. Several are
best-effort proxies because identity/policy fields are absent from the event
model; the note says so.

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

### A12 策略拒绝后绕过调用 — proxy
Single-event retry/encoding markers; confirm a true bypass by reading the
surrounding call chain in the session. event_type IN (`tool_invoke`, `tool_complete`).
- Predicate: `multi_search_any(lower(tool_input), ['retry','bypass','workaround','alternate','another way','base64','fromcharcode','atob','decode','circumvent','绕过','重试'])`
- Evidence: the retry/encoding marker, tool, `session_id`.
