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

## A15 外传保护等级下降 — 🔍 investigate
Downstream call didn't inherit the upstream sensitive label / masking policy.
- Gap: no `sensitive_types` / `data_labels` in CIM (#261). Not decidable today
  — report inconclusive.

## A16 外部传输通道不安全 — 🔍 investigate (conditional)
Plaintext HTTP / invalid cert / TLS < 1.2.
- Gap: no `protocol` / `tls_version` in CIM (#261). Only a coarse proxy:
  `http://` (not `https://`) literals in `tool_input`. Report as proxy only.

## A17 批量提取或枚举数据 — ✅ `dsa-a17`
Large page/limit or fetch-all in one call. Detections: `rule_id='dsa-a17'`. The
frequency dimension (many calls in a short window) is investigate-only:
```sql
SELECT session_id, count() AS calls FROM table(agentguard_cim_event)
WHERE event_type='tool_invoke' AND multi_search_any(lower(tool_name), ['query','search','export'])
  AND event_time >= now() - INTERVAL 1 HOUR
GROUP BY session_id HAVING calls > 50 ORDER BY calls DESC
```

## A18 隐蔽外传行为 — ✅ `dsa-a18`
Base64/hex/percent-encoded covert egress. Detections: `rule_id='dsa-a18'`.
Priority rule — review first.
