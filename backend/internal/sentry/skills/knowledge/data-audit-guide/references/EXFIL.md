# External Transport & Tool Calls (A13–A18)

## A13 未登记工具或外部地址 — 🔍 investigate
Tool/domain/IP not in the registry, or bypassing the gateway.
- Gap: no tool/endpoint registry in CIM. Best-effort — list external targets:
  ```sql
  SELECT tool_name, count() FROM table(agentguard_cim_event)
  WHERE event_type='tool_invoke'
    AND multi_search_any(lower(tool_input), ['http://','https://','ftp://'])
  GROUP BY tool_name ORDER BY count() DESC LIMIT 50
  ```
- Compare against the operator's known-tool list out of band.

## A14 敏感数据传向外部工具 — ✅ `dsa-a14`
Sensitive data in external/network tool params. Detections: `rule_id='dsa-a14'`.
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND multi_search_any(lower(tool_name), ['http', 'fetch', 'curl', 'webhook', 'upload', 'post', 'send', 'email', 'slack', 's3', 'url', 'request'])
    AND multi_search_any(lower(tool_input), ['password', 'secret', 'api key', 'api_key', 'token', 'private key', 'private_key', 'ssn', 'credit card', '身份证', '银行卡', '密码'])
  ORDER BY event_time DESC
  LIMIT 50
  ```

## A15 外传保护等级下降 — 🔍 investigate
Downstream call didn't inherit the upstream sensitive label / masking policy.
- Gap: no `sensitive_types` / `data_labels` in CIM (#261). Not decidable today
  — report inconclusive.

## A16 外部传输通道不安全 — 🔍 investigate (conditional)
Plaintext HTTP / invalid cert / TLS < 1.2.
- Gap: no `protocol` / `tls_version` in CIM (#261). Only a coarse proxy:
  `http://` (not `https://`) literals in `tool_input`. Report as proxy only.

## A17 批量提取或枚举数据 — ✅ `dsa-a17`
Large page/limit or fetch-all in one call. Detections: `rule_id='dsa-a17'`.
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
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

The frequency dimension (many calls in a short window) is investigate-only:
```sql
SELECT session_id, count() AS calls FROM table(agentguard_cim_event)
WHERE event_type='tool_invoke' AND multi_search_any(lower(tool_name), ['query','search','export'])
  AND event_time >= now() - INTERVAL 1 HOUR
GROUP BY session_id HAVING calls > 50 ORDER BY calls DESC
```

## A18 隐蔽外传行为 — ✅ `dsa-a18`
Base64/hex/percent-encoded covert egress. Detections: `rule_id='dsa-a18'`.
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
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

Priority rule — review first.
