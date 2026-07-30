# Sensitive Data Handling (A01–A07)

Each rule has an **exploration query** you run directly over
`agentguard_cim_event` via `run_sql`. The queries match sensitive-data **value
patterns** — API-key / token / private-key formats (e.g. `sk-ant-…`, `AKIA…`,
`ghp_…`, JWT, PEM blocks), `key = value` / `密码：值` assignments, and PII value
shapes (national-ID, card, SSN, email, mobile) — not the mere presence of a
word like "password". Never echo a full secret; show a short masked slice.

## A01 用户输入敏感数据检测
Sensitive PII / secret **values** in user input (not the words, the values).
- Fields: `user_message` on `event_type = 'user_input'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, left(user_message, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'user_input'
    AND (
           match(user_message, 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(user_message, 'sk-[A-Za-z0-9]{32,}') OR match(user_message, 'AKIA[0-9A-Z]{16}') OR match(user_message, 'AIza[0-9A-Za-z_-]{35}') OR match(user_message, 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(user_message, 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(user_message, 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(user_message, '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(user_message), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(user_message), '(password|passwd|pwd|token|bearer)[[:space:]]*[=: ][[:space:]]*[^[:space:]]{8,}') OR match(lower(user_message), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
        OR match(user_message, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(user_message, '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(user_message, '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(user_message, '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+[.][A-Za-z]{2,}') OR match(user_message, '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
      )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: risk level, `session_id`/`run_id`, masked hit, which pattern
  matched (key / ID / card / …), `event_time`.

## A02 输入模型前未脱敏
Sensitive fields still in plaintext when forwarded toward the model.
- Fields: `user_message` on `user_input` / `llm_request`. True masking-state
  comparison is not available (no `content_direction`), so this is a proxy:
  sensitive markers still present on a pre-model event.
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
PII / account / auth **values** in the model reply.
- Fields: `assistant_message` on `event_type = 'llm_response'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, left(assistant_message, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'llm_response'
    AND (
           match(assistant_message, 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(assistant_message, 'sk-[A-Za-z0-9]{32,}') OR match(assistant_message, 'AKIA[0-9A-Z]{16}') OR match(assistant_message, 'AIza[0-9A-Za-z_-]{35}') OR match(assistant_message, 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(assistant_message, 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(assistant_message, 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(assistant_message, '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(assistant_message), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(assistant_message), '(password|passwd|pwd|token|bearer)[[:space:]]*[=: ][[:space:]]*[^[:space:]]{8,}') OR match(lower(assistant_message), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
        OR match(assistant_message, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(assistant_message, '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(assistant_message, '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(assistant_message, '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+[.][A-Za-z]{2,}') OR match(assistant_message, '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
      )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: risk, `session_id`, masked hit, matched pattern. Whether the data
  belongs to the current user is not captured — note it.

## A04 工具参数携带非必要敏感数据
Sensitive **values** in tool parameters not needed for the operation.
- Fields: `tool_input`, `tool_name` on `event_type = 'tool_invoke'`
  (authentication tools are excluded — a login tool legitimately carries them).
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND NOT multi_search_any(lower(tool_name), ['login', 'auth', 'oauth', 'authenticate', 'credential'])
    AND (
           match(tool_input, 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(tool_input, 'sk-[A-Za-z0-9]{32,}') OR match(tool_input, 'AKIA[0-9A-Z]{16}') OR match(tool_input, 'AIza[0-9A-Za-z_-]{35}') OR match(tool_input, 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(tool_input, 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(tool_input, 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(tool_input, '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(tool_input), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(tool_input), '(password|passwd|pwd|token|bearer)[[:space:]]*[=: ][[:space:]]*[^[:space:]]{8,}') OR match(lower(tool_input), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
        OR match(tool_input, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(tool_input, '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(tool_input, '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(tool_input, '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+[.][A-Za-z]{2,}') OR match(tool_input, '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
      )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: tool, masked hit, which pattern, `session_id`.

## A05 工具返回结果未脱敏
Full customer data returned and written back with no masking.
- Fields: `tool_result` on `event_type = 'tool_complete'` (the `[*]{3,}`
  exclusion drops already-masked results).
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_result, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_complete'
    AND (
           match(tool_result, 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(tool_result, 'sk-[A-Za-z0-9]{32,}') OR match(tool_result, 'AKIA[0-9A-Z]{16}') OR match(tool_result, 'AIza[0-9A-Za-z_-]{35}') OR match(tool_result, 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(tool_result, 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(tool_result, 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(tool_result, '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(tool_result), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(tool_result), '(password|passwd|pwd|token|bearer)[[:space:]]*[=: ][[:space:]]*[^[:space:]]{8,}') OR match(lower(tool_result), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
        OR match(tool_result, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(tool_result, '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(tool_result, '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(tool_result, '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+[.][A-Za-z]{2,}') OR match(tool_result, '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
      )
    AND NOT match(tool_result, '[*]{3,}')
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: tool, masked hit, matched pattern, `session_id`.

## A06 身份鉴别信息进入对话链路
Authentication **credentials** anywhere in the chain — a direct high-risk
finding. Review first. Matches key/token/PEM/bearer value formats, not the word.
- Fields: `user_message`, `assistant_message`, `tool_input`, `tool_result`
  across all event types.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, raw_hook_name, left(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('user_input', 'llm_request', 'tool_invoke', 'tool_complete', 'llm_response')
    AND (
         match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'sk-[A-Za-z0-9]{32,}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'AKIA[0-9A-Z]{16}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'AIza[0-9A-Za-z_-]{35}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result)), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result)), '(password|passwd|pwd|token|bearer)[[:space:]]*[=: ][[:space:]]*[^[:space:]]{8,}') OR match(lower(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result)), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
    )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: credential type (masked), where it appeared (`raw_hook_name`),
  `session_id`, `event_time`.

## A07 数据汇聚后风险升级
A strong identifier **value** (ID / card / SSN / email / mobile) co-occurring
with account / transaction context — the re-identification signal.
- Fields: `assistant_message`, `tool_result`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, left(concat(assistant_message, ' ', tool_result), 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('llm_response', 'tool_complete')
    AND (
         match(concat(assistant_message, ' ', tool_result), '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(concat(assistant_message, ' ', tool_result), '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(concat(assistant_message, ' ', tool_result), '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(concat(assistant_message, ' ', tool_result), '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+[.][A-Za-z]{2,}') OR match(concat(assistant_message, ' ', tool_result), '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
    )
    AND multi_search_any(lower(concat(assistant_message, ' ', tool_result)), ['姓名', 'name', '账户', 'account', '余额', 'balance', '交易', 'transaction', '手机', 'phone', '地址', 'address'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: which identifier + which context co-occur, `session_id`. For
  multi-turn aggregation, widen to scan the whole session's rows.
