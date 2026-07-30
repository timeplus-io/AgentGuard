# Sensitive Data Handling (A01–A07)

Each rule below states its **goal** (grounded in the audit checklist), a
**sample case** of the data that should trigger it, and an **example query** you
adapt to the user's request. Match the **value**, never the mere word: "please
enter your password" is not a leak; `password=Hunter2xyz` is. Compose the SQL
from the skeleton in `SKILL.md`, drawing regexes from the pattern library below —
and add new patterns when the user asks about a data type it doesn't list.

## Sensitive-data pattern library

RE2 regexes for Timeplus `match(column, 'REGEX')`, **backslash-free** (inside a
Proton string literal `\b \t \n` become control characters — use POSIX classes
`[[:space:]]`/`[0-9]`, explicit classes, `[.]` for a literal dot, distinctive
prefixes).

### Credential / secret value patterns
| Name | Regex |
|------|-------|
| Anthropic / OpenAI API key | `sk-ant-[A-Za-z0-9_-]{16,}` · `sk-[A-Za-z0-9]{32,}` |
| AWS access key id | `AKIA[0-9A-Z]{16}` |
| Google API key | `AIza[0-9A-Za-z_-]{35}` |
| GitHub / Slack token | `gh[pousr]_[A-Za-z0-9]{30,}` · `xox[baprs]-[A-Za-z0-9-]{10,}` |
| JWT | `eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}` |
| Private key (PEM) | `-----BEGIN [A-Z ]*PRIVATE KEY-----` |
| Key assignment (on `lower(col)`) | `(api\|secret\|access\|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}` |
| Password / token assignment (on `lower(col)`) | `(password\|passwd\|pwd\|token\|bearer)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}` |
| 中文凭据赋值 | `(密码\|口令\|密钥\|令牌)[：:＝=][^[:space:]]{6,}` |

### PII value patterns
| Name | Regex |
|------|-------|
| 身份证 (PRC ID, 18-digit) | `[1-9][0-9]{5}(19\|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]` |
| 银行卡 / card (spaced) | `[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}` |
| SSN | `[0-9]{3}-[0-9]{2}-[0-9]{4}` |
| 手机号 / CN mobile | `(^\|[^0-9])1[3-9][0-9]{9}([^0-9]\|$)` |
| Email (broad — gate with context) | `[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+[.][A-Za-z]{2,}` |

The assignment patterns require a real `=`/`:` operator, so prose ("reset your
password") does not match. 身份证/银行卡/SSN/手机号 are the "strong" identifiers;
email is common so gate it with account/transaction context (A07). In the tables
the RE2 `|` is markdown-escaped as `\|` — drop the backslash in SQL. The example
queries below already contain ready-to-run SQL (a representative subset of the
patterns; OR in the rest for a full sweep).

## A01 用户输入敏感数据检测
**Detects:** the user types/pastes real sensitive **values** — 身份证号、银行卡号、
账户信息、密码、验证码、密钥、客户资料 — into the prompt. A finding when the data is
not required by the task (非业务必需), exceeds the agent's purpose, or would flow
into an unauthorized model. Column `user_message` on `event_type='user_input'`.
**Sample case:** `user_message = "帮我查客户 身份证 11010519900307231X 银行卡 6222 0212 3456 7890 的额度"` — real ID + card values, not the words "身份证"/"银行卡".
**Example:**
```sql
SELECT event_time, agent_id, session_id, left(user_message, 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type = 'user_input'
  AND (
       match(user_message, 'AKIA[0-9A-Z]{16}')                                                    -- AWS key
    OR match(user_message, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]')       -- 身份证
    OR match(lower(user_message), '(password|passwd|pwd|token)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}')  -- password=value
    -- …OR the remaining credential + strong-PII patterns from the SENSITIVE_DATA library
  )
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** masked hit, which pattern (key/ID/card/…), `session_id`, `event_time`.

## A02 输入模型前未脱敏
**Detects:** the checklist compares raw input to the gateway request and flags
sensitive fields still in full plaintext when sent to the model (未脱敏). There is
no gateway/`content_direction` field here, so this is a **proxy**: the sensitive
value is still present on a pre-model event (`user_input`/`llm_request`).
**Sample case:** `user_input` carries `身份证 11010519900307231X` and the turn's
`llm_request` still contains it verbatim — no masking to `1101**********231X`.
**Example:**
```sql
SELECT event_time, event_type, session_id, left(user_message, 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type IN ('user_input','llm_request')
  AND (
       match(user_message, 'AKIA[0-9A-Z]{16}')
    OR match(user_message, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]')
    OR match(lower(user_message), '(password|passwd|pwd|token)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}')
  )
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** mark "masking state not captured — proxy over available content".

## A03 模型输出敏感信息
**Detects:** the model's reply contains 个人信息/客户信息/账户数据/认证信息/业务敏感
数据. Highest risk when the data is not from the current user's own session (i.e.
someone else's data surfacing). Column `assistant_message` on `llm_response`.
**Sample case:** `assistant_message = "该客户银行卡号 6222 0212 3456 7890，余额 128,000 元"`.
**Example:**
```sql
SELECT event_time, agent_id, session_id, left(assistant_message, 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type = 'llm_response'
  AND (
       match(assistant_message, 'AKIA[0-9A-Z]{16}')                                                    -- AWS key
    OR match(assistant_message, '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]')       -- 身份证
    OR match(lower(assistant_message), '(password|passwd|pwd|token)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}')  -- password=value
    -- …OR the remaining credential + strong-PII patterns from the SENSITIVE_DATA library
  )
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** masked hit, matched pattern, `session_id`; note that cross-session origin is not captured.

## A04 工具参数携带非必要敏感数据
**Detects:** a tool call carries sensitive fields it does not need. The checklist
builds a field whitelist from the tool's parameter template and flags sensitive
fields unnecessary to the operation. Authentication tools are excluded.
**Sample case:** a `weather_lookup` invoked with `{"city":"Beijing","ssn":"123-45-6789"}` — SSN has no place in a weather query.
**Example:**
```sql
SELECT event_time, agent_id, session_id, tool_name, left(tool_input, 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type = 'tool_invoke'
  AND NOT multi_search_any(lower(tool_name), ['login','auth','oauth','authenticate','credential'])
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
**Evidence:** tool, masked hit, which sensitive field, `session_id`.

## A05 工具返回结果未脱敏
**Detects:** a tool returns full customer data and the agent writes it straight
into context / the reply without masking, truncation, or filtering. The
`[*]{3,}` exclusion drops already-masked results.
**Sample case:** `tool_result = {"name":"张三","id_card":"11010519900307231X","phone":"13912345678"}` returned in full.
**Example:**
```sql
SELECT event_time, agent_id, session_id, tool_name, left(tool_result, 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type = 'tool_complete'
  AND NOT match(tool_result, '[*]{3,}')
  AND (
       match(tool_result, 'AKIA[0-9A-Z]{16}')                                                    -- AWS key
    OR match(tool_result, 'sk-ant-[A-Za-z0-9_-]{16,}')                                          -- Anthropic key
    OR match(tool_result, '[0-9]{3}-[0-9]{2}-[0-9]{4}')                                       -- SSN
    OR match(lower(tool_result), '(password|passwd|pwd|token)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}')  -- secret=value
    -- …OR the remaining credential + strong-PII patterns from the SENSITIVE_DATA library
  )
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** tool, masked hit, matched pattern, `session_id`.

## A06 身份鉴别信息进入对话链路
**Detects:** authentication material — 密码/验证码/Token/Cookie/私钥/AccessKey —
appearing anywhere in the chain (prompt, tool params, tool results, model
output). The checklist calls this a **direct high-risk** determination. Review
first. Credential patterns only (no PII).
**Sample case:** `tool_result` contains `aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`, or a `-----BEGIN RSA PRIVATE KEY-----` block.
**Example:**
```sql
SELECT event_time, agent_id, session_id, raw_hook_name,
       left(concat(user_message,' ',assistant_message,' ',tool_input,' ',tool_result), 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type IN ('user_input','llm_request','tool_invoke','tool_complete','llm_response')
  AND (
       match(concat(user_message,' ',assistant_message,' ',tool_input,' ',tool_result), 'AKIA[0-9A-Z]{16}')     -- AWS key
    OR match(concat(user_message,' ',assistant_message,' ',tool_input,' ',tool_result), 'sk-ant-[A-Za-z0-9_-]{16,}')  -- Anthropic key
    OR (position(concat(user_message,' ',assistant_message,' ',tool_input,' ',tool_result), 'BEGIN')>0
        AND position(concat(user_message,' ',assistant_message,' ',tool_input,' ',tool_result), 'PRIVATE KEY')>0)  -- PEM
    -- …OR the remaining credential patterns from the SENSITIVE_DATA library
  )
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** credential type (masked), where it appeared (`raw_hook_name`), `session_id`.

## A07 数据汇聚后风险升级
**Detects:** across multiple turns or multiple tool results, data combines to
identify a customer's 身份/账户/交易 relationship — so the aggregate is more
sensitive than any single field. The signal here is a strong identifier value
co-occurring with account/transaction context.
**Sample case:** one tool_result gives `name=张三`, another `card=6222…`, another `balance` — together they re-identify the customer.
**Example:**
```sql
SELECT event_time, agent_id, session_id, left(concat(assistant_message,' ',tool_result), 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type IN ('llm_response','tool_complete')
  AND (
       match(concat(assistant_message,' ',tool_result), '[1-9][0-9]{5}(19|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]')  -- 身份证
    OR match(concat(assistant_message,' ',tool_result), '[0-9]{3}-[0-9]{2}-[0-9]{4}')                                     -- SSN
  )
  AND multi_search_any(lower(concat(assistant_message,' ',tool_result)), ['账户','account','余额','balance','交易','transaction','姓名','name'])
ORDER BY event_time DESC
LIMIT 50
```
**Evidence:** which identifier + which context co-occur, `session_id`. For true multi-turn aggregation, widen to scan the whole session's rows.
