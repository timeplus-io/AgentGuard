# Sensitive Data Handling (A01–A07)

This file first defines the **sensitive-data pattern library** — what a piece
of sensitive data actually looks like — then each rule (A01–A07) applies the
relevant patterns to a column of `agentguard_cim_event` with `run_sql`. Match
the **value**, never the mere word: "please enter your password" is not a leak;
`password=Hunter2xyz` is.

## Sensitive-data pattern library

RE2 regexes for Timeplus `match(column, 'REGEX')`. They are **backslash-free on
purpose**: inside a Proton string literal `\b \t \n` become control characters,
so use POSIX classes (`[[:space:]]`, `[0-9]`), explicit classes, `[.]` for a
literal dot, and distinctive prefixes instead. When a user asks about a data
type not listed here (e.g. an internal `EMP-#####` staff ID or a policy number),
**compose a new pattern in this same backslash-free style** and OR it into the
query.

### Credential / secret value patterns

| Name | Matches | Regex (use inside `match(col, '…')`) |
|------|---------|--------------------------------------|
| Anthropic API key | `sk-ant-…` | `sk-ant-[A-Za-z0-9_-]{16,}` |
| OpenAI API key | `sk-…` (long) | `sk-[A-Za-z0-9]{32,}` |
| AWS access key id | `AKIA…` | `AKIA[0-9A-Z]{16}` |
| Google API key | `AIza…` | `AIza[0-9A-Za-z_-]{35}` |
| GitHub token | `ghp_/gho_/ghu_/ghr_/ghs_…` | `gh[pousr]_[A-Za-z0-9]{30,}` |
| Slack token | `xoxb-/xoxp-…` | `xox[baprs]-[A-Za-z0-9-]{10,}` |
| JWT | `eyJ….….…` | `eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}` |
| Private key (PEM) | `-----BEGIN … PRIVATE KEY-----` | `-----BEGIN [A-Z ]*PRIVATE KEY-----` |
| Key assignment | `api_key = <value>` etc. (on `lower(col)`) | `(api\|secret\|access\|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}` |
| Password / token assignment | `password: <value>` (on `lower(col)`) | `(password\|passwd\|pwd\|token\|bearer)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}` |
| 中文凭据赋值 | `密码：值` / `密钥＝值` | `(密码\|口令\|密钥\|令牌)[：:＝=][^[:space:]]{6,}` |

The two assignment patterns require a real `=`/`:` operator between the keyword
and the value, so plain prose ("reset your password", "the api token lifecycle")
does **not** match — only an actual `keyword = value` does.

### PII value patterns

| Name | Matches | Regex |
|------|---------|-------|
| 身份证 (PRC national ID, 18-digit) | `11010519900307231X` | `[1-9][0-9]{5}(19\|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]` |
| 银行卡 / payment card (13–19, spaced/dashed) | `4111 1111 1111 1111` | `[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}` |
| SSN (US) | `123-45-6789` | `[0-9]{3}-[0-9]{2}-[0-9]{4}` |
| 手机号 / CN mobile | `13912345678` | `(^\|[^0-9])1[3-9][0-9]{9}([^0-9]\|$)` |
| Email (broad) | `a.b@corp.com` | `[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+[.][A-Za-z]{2,}` |

**身份证 / 银行卡 / SSN / 手机号** are the "strong" identifiers used for the
standalone checks (A01/A03/A04/A05/A14/A15). **Email** is deliberately common
and often benign, so by default it is applied only in **A07** (co-occurring with
account/transaction context); add it to another rule only when a specific case
warrants it.

> Tables escape the RE2 alternation `|` as `\|` for Markdown only — drop the
> backslash when you paste the regex into SQL. The query fences below already
> contain the correct, ready-to-run SQL.

## How each rule uses the library

Each A-code applies the relevant pattern group (credential, strong-PII, or both)
to one column and event type. The fenced query is the ready-to-run form; to
extend it, add a composed pattern as another `OR match(col, '…')` branch.

## A01 用户输入敏感数据检测 — credential + strong-PII on `user_message`
Sensitive secret / PII **values** in user input.
- Column: `user_message` on `event_type = 'user_input'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, left(user_message, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'user_input'
    AND (
           match(user_message, 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(user_message, 'sk-[A-Za-z0-9]{32,}') OR match(user_message, 'AKIA[0-9A-Z]{16}') OR match(user_message, 'AIza[0-9A-Za-z_-]{35}') OR match(user_message, 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(user_message, 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(user_message, 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(user_message, '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(user_message), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(user_message), '(password|passwd|pwd|token|bearer)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}') OR match(lower(user_message), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
        OR match(user_message, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(user_message, '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(user_message, '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(user_message, '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
      )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: risk level, `session_id`/`run_id`, masked hit, which pattern matched.

## A02 输入模型前未脱敏 — proxy on `user_message`
Sensitive fields still in plaintext when forwarded toward the model. True
masking comparison needs `content_direction` (not captured), so this is a proxy
that flags sensitive keywords still present on a pre-model event.
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

## A03 模型输出敏感信息 — credential + strong-PII on `assistant_message`
PII / account / auth **values** in the model reply.
- Column: `assistant_message` on `event_type = 'llm_response'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, left(assistant_message, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'llm_response'
    AND (
           match(assistant_message, 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(assistant_message, 'sk-[A-Za-z0-9]{32,}') OR match(assistant_message, 'AKIA[0-9A-Z]{16}') OR match(assistant_message, 'AIza[0-9A-Za-z_-]{35}') OR match(assistant_message, 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(assistant_message, 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(assistant_message, 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(assistant_message, '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(assistant_message), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(assistant_message), '(password|passwd|pwd|token|bearer)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}') OR match(lower(assistant_message), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
        OR match(assistant_message, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(assistant_message, '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(assistant_message, '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(assistant_message, '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
      )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: masked hit, matched pattern, `session_id`. Whether the data belongs
  to the current user is not captured — note it.

## A04 工具参数携带非必要敏感数据 — credential + strong-PII on `tool_input`
Sensitive **values** in tool parameters not needed for the operation.
- Column: `tool_input` on `event_type = 'tool_invoke'` (authentication tools
  are excluded — a login tool legitimately carries credentials).
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_invoke'
    AND NOT multi_search_any(lower(tool_name), ['login', 'auth', 'oauth', 'authenticate', 'credential'])
    AND (
           match(tool_input, 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(tool_input, 'sk-[A-Za-z0-9]{32,}') OR match(tool_input, 'AKIA[0-9A-Z]{16}') OR match(tool_input, 'AIza[0-9A-Za-z_-]{35}') OR match(tool_input, 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(tool_input, 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(tool_input, 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(tool_input, '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(tool_input), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(tool_input), '(password|passwd|pwd|token|bearer)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}') OR match(lower(tool_input), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
        OR match(tool_input, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(tool_input, '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(tool_input, '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(tool_input, '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
      )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: tool, masked hit, matched pattern, `session_id`.

## A05 工具返回结果未脱敏 — credential + strong-PII on `tool_result`
Full customer data returned with no masking (the `[*]{3,}` exclusion drops
already-masked results).
- Column: `tool_result` on `event_type = 'tool_complete'`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, tool_name, left(tool_result, 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type = 'tool_complete'
    AND (
           match(tool_result, 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(tool_result, 'sk-[A-Za-z0-9]{32,}') OR match(tool_result, 'AKIA[0-9A-Z]{16}') OR match(tool_result, 'AIza[0-9A-Za-z_-]{35}') OR match(tool_result, 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(tool_result, 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(tool_result, 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(tool_result, '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(tool_result), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(tool_result), '(password|passwd|pwd|token|bearer)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}') OR match(lower(tool_result), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
        OR match(tool_result, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(tool_result, '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(tool_result, '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(tool_result, '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
      )
    AND NOT match(tool_result, '[*]{3,}')
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: tool, masked hit, matched pattern, `session_id`.

## A06 身份鉴别信息进入对话链路 — credential patterns only, all content
Authentication **credentials** anywhere in the chain — a direct high-risk
finding. Review first. Uses only the credential/secret patterns (no PII noise).
- Columns: `user_message`, `assistant_message`, `tool_input`, `tool_result`,
  all event types.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, raw_hook_name, left(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('user_input', 'llm_request', 'tool_invoke', 'tool_complete', 'llm_response')
    AND (
         match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'sk-ant-[A-Za-z0-9_-]{16,}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'sk-[A-Za-z0-9]{32,}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'AKIA[0-9A-Z]{16}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'AIza[0-9A-Za-z_-]{35}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'gh[pousr]_[A-Za-z0-9]{30,}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'xox[baprs]-[A-Za-z0-9-]{10,}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), 'eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}') OR match(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result), '-----BEGIN [A-Z ]*PRIVATE KEY-----') OR match(lower(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result)), '(api|secret|access|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}') OR match(lower(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result)), '(password|passwd|pwd|token|bearer)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}') OR match(lower(concat(user_message, ' ', assistant_message, ' ', tool_input, ' ', tool_result)), '(密码|口令|密钥|令牌)[：:＝=][^[:space:]]{6,}')
    )
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: credential type (masked), where it appeared (`raw_hook_name`),
  `session_id`, `event_time`.

## A07 数据汇聚后风险升级 — PII value (incl. email) + account/txn context
A strong identifier or email **value** co-occurring with account / transaction
context — the re-identification signal. This is the one rule where the broad
email pattern is in play, because the account/txn keyword gates it.
- Columns: `assistant_message`, `tool_result`.
- Exploration query:
  ```sql
  SELECT event_time, agent_id, session_id, left(concat(assistant_message, ' ', tool_result), 240) AS evidence
  FROM table(agentguard_cim_event)
  WHERE event_type IN ('llm_response', 'tool_complete')
    AND (
           match(concat(assistant_message, ' ', tool_result), '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]') OR match(concat(assistant_message, ' ', tool_result), '[0-9]{3}-[0-9]{2}-[0-9]{4}') OR match(concat(assistant_message, ' ', tool_result), '[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}') OR match(concat(assistant_message, ' ', tool_result), '(^|[^0-9])1[3-9][0-9]{9}([^0-9]|$)')
        OR match(concat(assistant_message, ' ', tool_result), '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+[.][A-Za-z]{2,}')
    )
    AND multi_search_any(lower(concat(assistant_message, ' ', tool_result)), ['姓名', 'name', '账户', 'account', '余额', 'balance', '交易', 'transaction', '手机', 'phone', '地址', 'address'])
  ORDER BY event_time DESC
  LIMIT 50
  ```
- Evidence: which identifier + which context co-occur, `session_id`. For
  multi-turn aggregation, widen to scan the whole session's rows.
