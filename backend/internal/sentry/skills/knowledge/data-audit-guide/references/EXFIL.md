# External Transport & Tool Calls (A13–A18)

Each entry states the **goal** and an **example** to adapt to the user's request
and compose into the skeleton in `SKILL.md`. A14/A15 reuse the credential +
strong-PII **pattern library** in `SENSITIVE_DATA.md` — value formats, not
keywords.

## Per-rule spec

### A13 未登记工具或外部地址 — proxy
No tool/endpoint registry, so list external targets to compare out of band.
event_type = `tool_invoke`.
- Example: `multi_search_any(lower(tool_input), ['http://','https://','ftp://','ssh://'])`
- Evidence: the external target, tool, `session_id`.

### A14 敏感数据传向外部工具
A sensitive **value** (credential / strong-PII pattern from the library) in the
parameters of an external / network tool. event_type = `tool_invoke`.
- Gate (network tool): `multi_search_any(lower(tool_name), ['http','fetch','curl','webhook','upload','post','send','email','slack','s3','url','request'])`
- AND the credential + strong-PII pattern OR-chain (SENSITIVE_DATA library) over `tool_input`.
- Evidence: destination tool, masked hit, matched pattern, `session_id`.

### A15 外传保护等级下降 — proxy
No `sensitive_types`/data-label column, so flag a sensitive **value** entering an
outbound tool (the observable symptom of a lost protection level). Same as A14
with the outbound-tool subset `['http','fetch','curl','upload','webhook','post','send','s3','url']`.
- Evidence: outbound tool + matched pattern; mark "labels not captured".

### A16 外部传输通道不安全 — proxy
No `protocol`/`tls_version` column, so flag a plaintext `http://` target with no
`https://` present. event_type = `tool_invoke`.
- Example: `position(lower(tool_input), 'http://') > 0 AND position(lower(tool_input), 'https://') = 0`
- Evidence: the `http://` target, tool; mark "transport metadata not captured".

### A17 批量提取或枚举数据
Two dimensions: a single call asking for a huge page, and short-time enumeration
across many calls.

Single-call (large page / fetch-all), event_type = `tool_invoke`:
- Example: `match(lower(tool_input), '(limit|page_?size|per_?page|count|top|rows)[^0-9]{0,6}[1-9][0-9]{3,}') OR multi_search_any(lower(tool_input), ['export_all','dump_all','fetch_all','select * from','"all": true','"all":true','download all','bulk export'])`

Frequency (behavioral — many extraction calls per session in the window):

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
- Evidence: the size parameter / flag or the call count + window, tool, `session_id`.

### A18 隐蔽外传行为
Base64 / hex / percent-encoded covert egress. Review first. event_type = `tool_invoke`.
- Example: `match(tool_input, '[A-Za-z0-9+/]{120,}') OR match(tool_input, '[0-9a-fA-F]{80,}') OR match(lower(tool_input), 'data:[a-z/]+;base64,') OR match(tool_input, '(%[0-9A-Fa-f]{2}){20,}')`
- Evidence: the encoded blob (masked / truncated), tool, `session_id`.
