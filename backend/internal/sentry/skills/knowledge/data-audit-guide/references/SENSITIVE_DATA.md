# Sensitive Data Handling (A01–A07)

This file defines the **sensitive-data pattern library**, then each rule
(A01–A07) names the patterns, column, and event type to use. Compose the SQL
yourself from the **query skeleton** in `SKILL.md` — there is no full query per
rule. Match the **value**, never the mere word: "please enter your password" is
not a leak; `password=Hunter2xyz` is.

## Sensitive-data pattern library

RE2 regexes for Timeplus `match(column, 'REGEX')`. They are **backslash-free on
purpose**: inside a Proton string literal `\b \t \n` become control characters,
so use POSIX classes (`[[:space:]]`, `[0-9]`), explicit classes, `[.]` for a
literal dot, and distinctive prefixes instead. When a user asks about a data
type not listed here (an internal `EMP-#####` staff ID, a policy number, an
IBAN…), **compose a new pattern in this same backslash-free style** and OR it in.

### Credential / secret value patterns

| Name | Matches | Regex (inside `match(col, '…')`) |
|------|---------|----------------------------------|
| Anthropic API key | `sk-ant-…` | `sk-ant-[A-Za-z0-9_-]{16,}` |
| OpenAI API key | `sk-…` (long) | `sk-[A-Za-z0-9]{32,}` |
| AWS access key id | `AKIA…` | `AKIA[0-9A-Z]{16}` |
| Google API key | `AIza…` | `AIza[0-9A-Za-z_-]{35}` |
| GitHub token | `ghp_/gho_/ghu_/ghr_/ghs_…` | `gh[pousr]_[A-Za-z0-9]{30,}` |
| Slack token | `xoxb-/xoxp-…` | `xox[baprs]-[A-Za-z0-9-]{10,}` |
| JWT | `eyJ….….…` | `eyJ[A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{10,}[.][A-Za-z0-9_-]{5,}` |
| Private key (PEM) | `-----BEGIN … PRIVATE KEY-----` | `-----BEGIN [A-Z ]*PRIVATE KEY-----` |
| Key assignment | `api_key = <value>` (on `lower(col)`) | `(api\|secret\|access\|private)[_-]?key[[:space:]]*[=:][[:space:]]*[a-z0-9_+/.-]{12,}` |
| Password / token assignment | `password: <value>` (on `lower(col)`) | `(password\|passwd\|pwd\|token\|bearer)[[:space:]]*[=:][[:space:]]*[^[:space:]]{8,}` |
| 中文凭据赋值 | `密码：值` / `密钥＝值` | `(密码\|口令\|密钥\|令牌)[：:＝=][^[:space:]]{6,}` |

The two assignment patterns require a real `=`/`:` operator between keyword and
value, so plain prose ("reset your password") does **not** match — only an
actual `keyword = value` does.

### PII value patterns

| Name | Matches | Regex |
|------|---------|-------|
| 身份证 (PRC national ID, 18-digit) | `11010519900307231X` | `[1-9][0-9]{5}(19\|20)[0-9]{2}[01][0-9][0-3][0-9][0-9]{3}[0-9Xx]` |
| 银行卡 / payment card (13–19, spaced) | `4111 1111 1111 1111` | `[0-9]{4}[ -][0-9]{4}[ -][0-9]{4}[ -][0-9]{1,7}` |
| SSN (US) | `123-45-6789` | `[0-9]{3}-[0-9]{2}-[0-9]{4}` |
| 手机号 / CN mobile | `13912345678` | `(^\|[^0-9])1[3-9][0-9]{9}([^0-9]\|$)` |
| Email (broad) | `a.b@corp.com` | `[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+[.][A-Za-z]{2,}` |

**身份证 / 银行卡 / SSN / 手机号** are the "strong" identifiers used for the
standalone checks. **Email** is common and often benign, so use it only where an
account/transaction context gates it (A07) unless a specific case warrants more.

> In these tables the RE2 alternation `|` is escaped as `\|` for Markdown — drop
> the backslash when you paste a regex into SQL.

## Worked example (A01) — compose the rest from the library

Apply the named pattern groups to the column as OR branches inside the skeleton:

```sql
SELECT event_time, agent_id, session_id, left(user_message, 240) AS evidence
FROM table(agentguard_cim_event)
WHERE event_type = 'user_input'
  AND (
       match(user_message, 'sk-ant-[A-Za-z0-9_-]{16,}')          -- a credential pattern
    OR match(user_message, '[0-9]{3}-[0-9]{2}-[0-9]{4}')          -- a strong-PII pattern
    -- …OR the remaining credential + strong-PII patterns from the library
  )
ORDER BY event_time DESC
LIMIT 50
```

For the credential-assignment / Chinese patterns, wrap the column in `lower(...)`
as the library notes. Every rule below follows the same shape — only the pattern
group, column, and event type change.

## Per-rule spec

| A | 中文 | Patterns | Column | event_type |
|---|------|----------|--------|------------|
| A01 | 用户输入敏感数据检测 | credential + strong-PII | `user_message` | `user_input` |
| A02 | 输入模型前未脱敏 (proxy) | credential + strong-PII | `user_message` | `user_input`, `llm_request` |
| A03 | 模型输出敏感信息 | credential + strong-PII | `assistant_message` | `llm_response` |
| A04 | 工具参数携带非必要敏感数据 | credential + strong-PII | `tool_input` | `tool_invoke` (exclude auth tools) |
| A05 | 工具返回结果未脱敏 | credential + strong-PII | `tool_result` | `tool_complete` (skip if `match(tool_result,'[*]{3,}')`) |
| A06 | 身份鉴别信息进入对话链路 | credential only | all four content columns | all event types |
| A07 | 数据汇聚后风险升级 | strong-PII **or email**, AND account/txn keyword | `assistant_message`+`tool_result` | `llm_response`, `tool_complete` |

Notes:
- **A02** is a proxy — there is no `content_direction`, so "still unmasked pre-model"
  is only inferred from the sensitive value still being present on a pre-model event.
- **A04** exclude authentication tools: `NOT multi_search_any(lower(tool_name), ['login','auth','oauth','authenticate','credential'])` — a login tool legitimately carries credentials.
- **A06** is credentials only (no PII), over `concat(user_message,' ',assistant_message,' ',tool_input,' ',tool_result)`. Review first — a credential in the chain is a direct high-risk finding.
- **A07** account/transaction context keywords: `multi_search_any(lower(...), ['姓名','name','账户','account','余额','balance','交易','transaction','手机','phone','地址','address'])`. This is the one rule where the broad email pattern is in play, because the keyword gates it.

Evidence for every finding: risk level, `session_id`/`run_id`, masked hit
fragment, which pattern matched, `event_time`. Never echo a full secret.
