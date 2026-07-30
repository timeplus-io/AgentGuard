# Sensitive Data Handling (A01–A07)

Each rule: **definition** → **status/where** → **fields & gaps** → **recipe** →
**evidence**. Masked-fragment discipline: never echo a full secret; show a
short masked slice.

## A01 用户输入敏感数据检测 — ✅ `dsa-a01`
Sensitive PII (ID/bank-card/password/key/customer data) in user input.
- Fields: `user_message` on `event_type='user_input'`.
- Detections:
  ```sql
  SELECT detected_at, agent_id, session_id, message
  FROM table(agentguard_security_events)
  WHERE rule_id = 'dsa-a01' AND detected_at >= now() - INTERVAL 7 DAY
  ORDER BY detected_at DESC LIMIT 100
  ```
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
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
- Or semantic: `dlp_search` for "customer identity or account numbers in user input".
- Evidence: A01, risk, session_id/run_id, masked hit, PII type, event_time.

## A02 输入模型前未脱敏 — 🔍 investigate
Sensitive fields still plaintext when sent to the model.
- Gap: needs raw-input vs. gateway-request comparison and `content_direction`
  — not in CIM (#261). Best-effort proxy: same sensitive markers present on
  both a `user_input` and the following `llm_request` in one session.
  ```sql
  SELECT session_id, count() FROM table(agentguard_cim_event)
  WHERE event_type IN ('user_input','llm_request')
    AND multi_search_any(lower(user_message), ['password','身份证','api key'])
  GROUP BY session_id
  ```
- Evidence: mark "masking state not captured — proxy only".

## A03 模型输出敏感信息 — ✅ `dsa-a03`
PII/account/auth data in model replies; highest risk if not from current
session.
- Fields: `assistant_message` on `event_type='llm_response'` (the model-text column is `assistant_message`; `llm_response` is an event_type value, not a column).
- Detections: `rule_id='dsa-a03'`. Cross-session origin = not captured; note it.
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
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

## A04 工具参数携带非必要敏感数据 — ✅ `dsa-a04`
Sensitive fields in tool params not needed for the operation.
- Fields: `tool_input`, `tool_name` on `tool_invoke`.
- Detections: `rule_id='dsa-a04'`.
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND NOT multi_search_any(lower(tool_name), ['login', 'auth', 'oauth', 'authenticate', 'credential'])
    AND multi_search_any(lower(tool_input), ['password', 'passwd', 'secret', 'api key', 'api_key', 'apikey', 'access key', 'access_key', 'private key', 'private_key', 'ssn', 'credit card', '身份证', '银行卡', '密码', '验证码'])
  ORDER BY event_time DESC
  LIMIT 50
  ```

## A05 工具返回结果未脱敏 — ✅ `dsa-a05`
Full customer data returned and written back without masking.
- Fields: `tool_result` on `tool_complete`.
- Detections: `rule_id='dsa-a05'`.
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
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

## A06 身份鉴别信息进入对话链路 — ✅ `dsa-a06`
Credentials anywhere in the chain — direct high risk.
- Fields: all content columns, all event types.
- Detections: `rule_id='dsa-a06'`. This is a priority rule — review first.
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
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

## A07 数据汇聚后风险升级 — ✅ `dsa-a07`
Identity-linkable data co-occurring (aggregation).
- Fields: `assistant_message`+`tool_result`.
- Detections: `rule_id='dsa-a07'`. Multi-turn aggregation across events is
  investigate-only — use `dlp_search` over a session to confirm linkage.
- Direct audit query (self-contained — works whether or not the `data-security-audit` package is installed):
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
