# Permissions & Data Isolation (A08–A12)

Each rule states its **goal**, a **sample case**, and an **example query** to
adapt. A08/A09/A12 are **behavioral** — the real signal is a pattern of events
(distinct-entity breadth, tool-use deviation, denial-then-continue), not a
keyword in one field — so their examples are full queries. Compose single-event
ones from the skeleton in `SKILL.md`.

## A08 跨用户或跨租户数据泄露 (behavioral)
**Detects:** the data subject / tenant identifier in the model output, KB result,
or tool result is **different from the calling user** — cross-user / cross-tenant
leakage; the checklist treats this as highest risk. There is no `tenant_id`
column, so the behavioral proxy is a session/agent whose tool traffic references
**many distinct** customer/tenant identifiers — reaching across data subjects.
**Sample case:** in one session an agent's tool calls return records for
`customer_id=1001, 1002, 1003, 1004…` — one session enumerating many customers.
**Example:**
```sql
WITH entities AS (
  SELECT session_id, agent_id,
         extract(concat(tool_input, ' ', tool_result), '(?:customer_id|tenant_id|account_id|org_id|user_id)[^A-Za-z0-9]{1,4}([A-Za-z0-9_-]{3,})') AS entity_id
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('tool_invoke', 'tool_complete')
)
SELECT session_id, agent_id,
       count_distinct(entity_id)        AS distinct_entities,
       group_uniq_array(8)(entity_id)   AS sample_ids
FROM entities
WHERE entity_id != ''
GROUP BY session_id, agent_id
HAVING distinct_entities > 3
ORDER BY distinct_entities DESC
LIMIT 50
```
**Evidence:** the distinct-entity count + `sample_ids`; mark "tenant identity not captured — heuristic". Confirm by reading whether those IDs belong to the calling user.

## A09 未授权工具调用 (behavioral)
**Detects:** the agent calls a tool **not on its allow-list**, or the caller's
role lacks permission. There is no role / allow-list field, so the behavioral
proxy is a tool the agent **rarely or never** uses — a deviation from its
established tool set (optionally intersect with high-privilege tool names:
`admin`, `sudo`, `grant`, `delete_user`, `drop`, `exec`, …).
**Sample case:** a read-only reporting agent that normally calls `query`/`read`
suddenly invokes `delete_user` once.
**Example:**
```sql
SELECT agent_id, tool_name, count() AS calls
FROM table(agentguard_cim_event)
WHERE event_type = 'tool_invoke' AND tool_name != ''
GROUP BY agent_id, tool_name
HAVING calls <= 2
ORDER BY agent_id, calls
LIMIT 100
```
**Evidence:** agent, the rare/unexpected tool, its call count vs the agent's usual set; confirm intent from the session.

## A10 越权参数调用
**Detects:** accessing out-of-scope data by manipulating a user id / org id /
tenant id / resource id / **file path** — path traversal or id-swapping.
**Sample case:** `tool_input = {"path":"../../etc/passwd"}` or `{"user_id":"9999","resource":"/other-tenant/data"}`.
**Example:**
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
**Evidence:** the manipulated parameter / path, tool, `session_id`.

## A11 高风险工具缺少身份信息
**Detects:** a high-risk operation — query-sensitive / export / download / send /
modify / delete — that **cannot be tied to a specific user / agent / session**.
No `user_id` column, so attribution is via `agent_id`/`session_id`.
**Sample case:** an `export_all_customers` `tool_invoke` with empty `agent_id`
and `session_id` — no accountable actor.
**Example:**
```sql
SELECT event_time, agent_id, session_id, tool_name
FROM table(agentguard_cim_event)
WHERE event_type IN ('tool_invoke', 'tool_complete')
  AND (agent_id = '' OR session_id = '')
  AND multi_search_any(lower(tool_name), ['export', 'download', 'send', 'delete', 'remove', 'modify', 'update', 'query', 'search', 'dump', 'backup', 'transfer'])
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** tool, which identity signal is missing, `event_time`.

## A12 策略拒绝后绕过调用 (behavioral / cross-event)
**Detects:** after a call is **denied**, the agent achieves the same goal another
way — a different tool, encoded parameters, or splitting into several calls. The
real signal is the **denial followed by continued tool use in the same session**,
via `hook_decision` / `block_reason` — not the word "retry" in `tool_input`.
**Sample case:** `delete_user` is blocked (`hook_decision=deny`), then the same
session calls `admin_api` with `{"op":"remove"}`.
**Example:**
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
WHERE e.event_type = 'tool_invoke'
  AND e.event_time > d.first_denial
ORDER BY e.event_time DESC
LIMIT 50
```
**Evidence:** the denied event, the subsequent tool + params, `session_id`. If `hook_decision` is `observe` everywhere (monitoring-only deployment) there are no denials to bypass — report "no blocking policy in effect".
