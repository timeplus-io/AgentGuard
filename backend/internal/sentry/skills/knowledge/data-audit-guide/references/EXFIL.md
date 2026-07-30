# External Transport & Tool Calls (A13–A18)

Each rule has an **exploration query** over `agentguard_cim_event`.

## A13 未登记工具或外部地址
Tool / domain / IP not in the registry, or bypassing the gateway.
- Fields: `tool_input`, `tool_name`. Proxy — there is no tool/endpoint registry
  in the event model, so this lists external targets to compare out of band.
- Exploration query:
  ```sql
  SELECT event_time, tool_name, session_id, left(tool_input, 200) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND multi_search_any(lower(tool_input), ['http://', 'https://', 'ftp://', 'ssh://'])
  ORDER BY event_time DESC
  LIMIT 100
  ```
- Evidence: the external target, tool, `session_id`.

## A14 敏感数据传向外部工具
Sensitive data in external / network tool parameters.
- Fields: `tool_name`, `tool_input` on `event_type = 'tool_invoke'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND multi_search_any(lower(tool_name), ['http', 'fetch', 'curl', 'webhook', 'upload', 'post', 'send', 'email', 'slack', 's3', 'url', 'request'])
    AND multi_search_any(lower(tool_input), ['password', 'secret', 'api key', 'api_key', 'token', 'private key', 'private_key', 'ssn', 'credit card', '身份证', '银行卡', '密码'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: destination tool, masked hit, data type, `session_id`.

## A15 外传保护等级下降
Downstream call did not inherit the upstream sensitive label / masking.
- Fields: `tool_name`, `tool_input`. Proxy — there is no `sensitive_types` /
  data-label column, so this flags sensitive data entering an outbound tool
  (the observable symptom of a lost protection level).
- Exploration query:
  ```sql
  SELECT event_time, tool_name, session_id, left(tool_input, 200) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND multi_search_any(lower(tool_name), ['http', 'fetch', 'curl', 'upload', 'webhook', 'post', 'send', 's3', 'url'])
    AND multi_search_any(lower(tool_input), ['password', 'secret', 'api key', 'token', '身份证', '银行卡', 'ssn'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: outbound tool + sensitive marker; mark "labels not captured".

## A16 外部传输通道不安全
Plaintext HTTP / invalid cert / weak TLS.
- Fields: `tool_input`. Proxy — there is no `protocol` / `tls_version` column,
  so this flags a plaintext `http://` target with no `https://` present.
- Exploration query:
  ```sql
  SELECT event_time, tool_name, session_id, left(tool_input, 200) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND position(lower(tool_input), 'http://') > 0
    AND position(lower(tool_input), 'https://') = 0
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: the `http://` target, tool; mark "transport metadata not captured".

## A17 批量提取或枚举数据
Bulk data pull — large page/limit or fetch-all in one call.
- Fields: `tool_input` on `event_type = 'tool_invoke'`.
- Exploration query:
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
- Evidence: the size parameter / flag, tool, `session_id`.

## A18 隐蔽外传行为
Base64 / hex / percent-encoded covert egress. Review first.
- Fields: `tool_input` on `event_type = 'tool_invoke'`.
- Exploration query:
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
- Evidence: the encoded blob (masked / truncated), tool, `session_id`.
