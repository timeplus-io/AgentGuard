# External Transport & Tool Calls (A13–A18)

Each rule states its **goal**, a **sample case**, and an **example query** to
adapt. A14/A15 reuse the credential + strong-PII **pattern library** in
`SENSITIVE_DATA.md` (value formats, not keywords). A17 has a behavioral
frequency dimension.

## A13 未登记工具或外部地址
**Detects:** a tool name / target domain / IP / interface **not on the registered
list**, or a call that bypasses the unified AI/API gateway. There is no registry
in the event model, so this **lists external targets** to compare out of band.
**Sample case:** `tool_input` posts to `http://45.77.12.34/collect` — a raw IP not in the org's endpoint registry.
**Example:**
```sql
SELECT event_time, tool_name, session_id, left(tool_input, 200) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type = 'tool_invoke'
  AND multi_search_any(lower(tool_input), ['http://', 'https://', 'ftp://', 'ssh://'])
ORDER BY event_time DESC
LIMIT 100
```
**Evidence:** the external target, tool, `session_id`; compare against the known-endpoint list.

## A14 敏感数据传向外部工具
**Detects:** a third-party / internet tool's parameters contain a sensitive
**value** with no recorded allowed data-type / field scope. Gate on a network
tool name AND a sensitive value pattern.
**Sample case:** `http_post` to `https://webhook.site/abc` with body `{"id_card":"11010519900307231X"}`.
**Example:**
```sql
SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type = 'tool_invoke'
  AND multi_search_any(lower(tool_name), ['http','fetch','curl','webhook','upload','post','send','s3','url'])
  AND (
       match(tool_input, 'AKIA[0-9A-Z]{16}')                                                    -- AWS key
    OR match(tool_input, 'sk-ant-[A-Za-z0-9_-]{16,}')                                          -- Anthropic key
    OR match(tool_input, '[0-9]{3}-[0-9]{2}-[0-9]{4}')                                       -- SSN
    OR match(lower(tool_input), '(password|passwd|pwd|token)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}')  -- secret=value
    -- …OR the remaining credential + strong-PII patterns from the SENSITIVE_DATA library
  )
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** destination tool, masked hit, matched pattern, `session_id`.

## A15 外传保护等级下降
**Detects:** upstream data was marked sensitive, but the downstream tool call /
model request **did not inherit** the sensitive label / masking / audit policy.
There is no data-label field, so the proxy is a sensitive **value** entering an
outbound tool (the observable symptom of the lost protection level).
**Sample case:** a customer record retrieved earlier (sensitive) is passed
verbatim into an external `translate_api` call.
**Example:**
```sql
SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type = 'tool_invoke'
  AND multi_search_any(lower(tool_name), ['http','fetch','curl','webhook','upload','post','send','s3','url'])
  AND (
       match(tool_input, 'AKIA[0-9A-Z]{16}')                                                    -- AWS key
    OR match(tool_input, 'sk-ant-[A-Za-z0-9_-]{16,}')                                          -- Anthropic key
    OR match(tool_input, '[0-9]{3}-[0-9]{2}-[0-9]{4}')                                       -- SSN
    OR match(lower(tool_input), '(password|passwd|pwd|token)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}')  -- secret=value
    -- …OR the remaining credential + strong-PII patterns from the SENSITIVE_DATA library
  )
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** outbound tool + matched pattern; mark "labels not captured".

## A16 外部传输通道不安全 (conditional)
**Detects:** plaintext HTTP, an invalid certificate, or below TLS 1.2 for
sensitive-data transport. There is no `protocol` / `tls_version` field, so the
proxy is a plaintext `http://` target with no `https://` present.
**Sample case:** `tool_input` targets `http://api.partner.com/upload` (not https).
**Example:**
```sql
SELECT event_time, tool_name, session_id, left(tool_input, 200) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type = 'tool_invoke'
  AND position(lower(tool_input), 'http://') > 0
  AND position(lower(tool_input), 'https://') = 0
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** the `http://` target, tool; mark "transport metadata not captured".

## A17 批量提取或枚举数据
**Detects:** short-time consecutive query / search / paging / export that
accumulates a **large volume** of sensitive data beyond the user's historical
baseline. Two dimensions: one call asking for a huge page, and many calls in a
short window.
**Sample case:** 300 `search_customer` calls in two minutes in one session, or a single call with `limit=100000`.
**Example (single call — large page / fetch-all):**
```sql
SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type = 'tool_invoke'
  AND (
       match(lower(tool_input), '(limit|page_?size|per_?page|count|top|rows)[^0-9]{0,6}[1-9][0-9]{3,}')
    OR multi_search_any(lower(tool_input), ['export_all', 'dump_all', 'fetch_all', 'select * from', '"all": true', '"all":true', 'download all', 'bulk export'])
  )
ORDER BY event_time DESC
LIMIT 50
```
**Example (frequency — many extraction calls per session):**
```sql
SELECT session_id, agent_id, count() AS extraction_calls,
       min(event_time) AS first_call, max(event_time) AS last_call
FROM table(agentguard_cim_event)
WHERE event_type = 'tool_invoke'
  AND multi_search_any(lower(tool_name), ['query', 'search', 'export', 'list', 'fetch', 'get', 'read', 'download', 'select', 'scan'])
GROUP BY session_id, agent_id
HAVING extraction_calls > 50
ORDER BY extraction_calls DESC
LIMIT 50
```
**Evidence:** the size parameter / flag, or the call count + window, tool, `session_id`.

## A18 隐蔽外传行为
**Detects:** encoding sensitive data as Base64 / hex / compressed fragments / URL
parameters, or splitting it across several calls, to smuggle it out. Review first.
**Sample case:** `tool_input` contains a 400-char base64 blob, or an outbound URL with `?d=<long hex>`.
**Example:**
```sql
SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type = 'tool_invoke'
  AND (
       match(tool_input, '[A-Za-z0-9+/]{120,}')
    OR match(tool_input, '[0-9a-fA-F]{80,}')
    OR match(lower(tool_input), 'data:[a-z/]+;base64,')
    OR match(tool_input, '(%[0-9A-Fa-f]{2}){20,}')
  )
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** the encoded blob (masked / truncated), tool, `session_id`.
