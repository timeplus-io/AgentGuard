# Sensitive Data Handling (A01–A07)

Each rule below has an **exploration query** you run directly over
`agentguard_cim_event` via `run_sql`. Never echo a full secret — show a short
masked slice in findings.

## A01 用户输入敏感数据检测
Sensitive PII (ID / bank-card / password / key / customer data) in user input.
- Fields: `user_message` on `event_type = 'user_input'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, left(user_message, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'user_input'
    AND (
         match(user_message, '[0-9]{17}[0-9Xx]')
      OR match(user_message, '[0-9]{13,19}')
      OR multi_search_any(lower(user_message), ['密码', '验证码', '密钥', '身份证', '银行卡', 'password', 'passwd', 'verification code', 'secret key', 'api key', 'access key'])
    )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: risk level, `session_id`/`run_id`, masked hit, PII type, `event_time`.

## A02 输入模型前未脱敏
Sensitive fields still in plaintext when forwarded toward the model.
- Fields: `user_message` on `user_input` / `llm_request`. True masking-state
  comparison is not available (no `content_direction`), so this is a proxy:
  the same sensitive markers still present on a pre-model event.
- Exploration query:
  ```sql
  SELECT event_time, event_type, session_id, left(user_message, 200) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('user_input', 'llm_request')
    AND multi_search_any(lower(user_message), ['password', '密码', '身份证', 'api key', 'secret', '银行卡'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: mark "masking state not captured — proxy over available content".

## A03 模型输出敏感信息
PII / account / auth data in the model reply.
- Fields: `assistant_message` on `event_type = 'llm_response'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, left(assistant_message, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'llm_response'
    AND (
         match(assistant_message, '[0-9]{17}[0-9Xx]')
      OR match(assistant_message, '[0-9]{13,19}')
      OR multi_search_any(lower(assistant_message), ['身份证', '银行卡', '信用卡', 'social security', 'ssn', 'account number', 'api key', 'access key', 'password', 'token', '密码'])
    )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: risk, `session_id`, masked hit, data type. Whether the data belongs
  to the current user is not captured — note it.

## A04 工具参数携带非必要敏感数据
Sensitive fields in tool parameters not needed for the operation.
- Fields: `tool_input`, `tool_name` on `event_type = 'tool_invoke'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND NOT multi_search_any(lower(tool_name), ['login', 'auth', 'oauth', 'authenticate', 'credential'])
    AND multi_search_any(lower(tool_input), ['password', 'passwd', 'secret', 'api key', 'api_key', 'apikey', 'access key', 'access_key', 'private key', 'private_key', 'ssn', 'credit card', '身份证', '银行卡', '密码', '验证码'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: tool, masked hit, which sensitive field, `session_id`.

## A05 工具返回结果未脱敏
Full customer data returned and written back without masking.
- Fields: `tool_result` on `event_type = 'tool_complete'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_result, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_complete'
    AND (
         match(tool_result, '[0-9]{17}[0-9Xx]')
      OR match(tool_result, '[0-9]{13,19}')
      OR multi_search_any(lower(tool_result), ['身份证', '银行卡', 'social security', 'ssn', 'credit card'])
    )
    AND NOT match(tool_result, '[*]{3,}')
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: tool, masked hit, data type, `session_id`.

## A06 身份鉴别信息进入对话链路
Credentials anywhere in the chain — a direct high-risk finding. Review first.
- Fields: `user_message`, `assistant_message`, `tool_input`, `tool_result`
  across all event types.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, raw_hook_name, left(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('user_input', 'llm_request', 'tool_invoke', 'tool_complete', 'llm_response')
    AND (
         match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'AKIA[0-9A-Z]{16}')
      OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'eyJ[A-Za-z0-9_-]{10,}')
      OR (position(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'BEGIN') > 0
          AND position(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'PRIVATE KEY') > 0)
      OR multi_search_any(lower(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result)), ['bearer ', 'authorization: bearer', 'password=', 'passwd=', 'set-cookie:', 'session_token', 'access_token', 'accesskey', '私钥', '访问密钥'])
    )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: credential type (masked), where it appeared (`raw_hook_name`),
  `session_id`, `event_time`.

## A07 数据汇聚后风险升级
Identity-linkable data co-occurring (aggregation / re-identification).
- Fields: `assistant_message`, `tool_result`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, left(concat(assistant_message, ' ', tool_result), 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('llm_response', 'tool_complete')
    AND (
      match(concat(assistant_message, ' ', tool_result), '[0-9]{17}[0-9Xx]')
      OR match(concat(assistant_message, ' ', tool_result), '[0-9]{13,19}')
    )
    AND multi_search_any(lower(concat(assistant_message, ' ', tool_result)), ['姓名', 'name', '账户', 'account', '余额', 'balance', '交易', 'transaction', '手机号', 'phone'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: which identifiers co-occur, `session_id`. Multi-turn aggregation
  across separate events: widen by scanning the whole session's rows.
