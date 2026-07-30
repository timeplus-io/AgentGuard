# External Transport & Tool Calls (A13–A18)

Each rule has an **exploration query** over `agentguard_cim_event`. The
sensitive-data checks (A14, A15) apply the credential + strong-PII patterns
defined in `SENSITIVE_DATA.md` — value formats, not keywords.

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
Sensitive **value** (credential / strong-PII pattern) in the parameters of an
external / network tool.
- Fields: `tool_name`, `tool_input` on `event_type = 'tool_invoke'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND multi_search_any(lower(tool_name), ['http', 'fetch', 'curl', 'webhook', 'upload', 'post', 'send', 'email', 'slack', 's3', 'url', 'request'])
    AND (
           match(tool_input, 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(tool_input, 'sk-[A-Za-z0-9]{32,}') OR match(tool_input, 'AKIA[0-9A-Z]{16}') OR match(tool_input, 'AIza[0-9A-Za-z_-]{35}') OR match(tool_input, 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(tool_input, 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(tool_input, 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(tool_input, '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(tool_input), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(tool_input), '(password|passwd|pwd|token|bearer)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}') OR match(lower(tool_input), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
        OR match(tool_input, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(tool_input, '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(tool_input, '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(tool_input, '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
      )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: destination tool, masked hit, matched pattern, `session_id`.

## A15 外传保护等级下降
Downstream call did not inherit the upstream sensitive label / masking.
- Fields: `tool_name`, `tool_input`. Proxy — there is no `sensitive_types` /
  data-label column, so this flags a sensitive **value** entering an outbound
  tool (the observable symptom of a lost protection level).
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND multi_search_any(lower(tool_name), ['http', 'fetch', 'curl', 'upload', 'webhook', 'post', 'send', 's3', 'url'])
    AND (
           match(tool_input, 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(tool_input, 'sk-[A-Za-z0-9]{32,}') OR match(tool_input, 'AKIA[0-9A-Z]{16}') OR match(tool_input, 'AIza[0-9A-Za-z_-]{35}') OR match(tool_input, 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(tool_input, 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(tool_input, 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(tool_input, '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(tool_input), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(tool_input), '(password|passwd|pwd|token|bearer)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}') OR match(lower(tool_input), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
        OR match(tool_input, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(tool_input, '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(tool_input, '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(tool_input, '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
      )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: outbound tool + matched pattern; mark "labels not captured".

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

