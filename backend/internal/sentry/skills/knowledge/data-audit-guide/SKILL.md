---
name: data-audit-guide
description: >
  The data-security audit checklist (A01–A27) as an interactive, self-contained
  playbook. Every A-code carries a ready exploration query over
  agentguard_cim_event, plus the fields it uses (and the gaps) and the evidence
  to record. Load this before auditing conversation / tool-call data.
metadata:
  author: timeplus-io
  scope: AgentGuard Sentry (read-only exploration of agentguard_cim_event)
---

# Data-Security Audit Guide (Checklist A01–A27)

You are auditing agent conversation, gateway, and tool-call data against a
27-point data-security checklist (A01–A27). This guide is self-contained: every
A-code has a ready **exploration query** you run directly over the
`agentguard_cim_event` stream with `run_sql`. No other stream is needed —
`agentguard_cim_event` is the single source for this audit.

## Audit boundary

Scope is **runtime data only** — user input, system prompts, context,
knowledge-base results, model output, gateway request/response, and tool calls
(name, params, return, status, call chain). Infrastructure, database, backup,
endpoint, and network-device configuration are **out of scope**: the data to
judge them does not exist here. A query returning no rows is a technical signal,
not proof that contracts, approvals, or policy are satisfied.

## The A01–A27 catalog

Every A-code has an exploration query in the reference file named below. Some
are precise; others are best-effort **proxies** because the field they truly
need is absent from the event model (see Field reality) — the reference query
says so where that applies.

| A | 中文 | English | Reference |
|---|------|---------|-----------|
| A01 | 用户输入敏感数据检测 | Sensitive PII in user input | SENSITIVE_DATA.md |
| A02 | 输入模型前未脱敏 | Not masked before model (proxy) | SENSITIVE_DATA.md |
| A03 | 模型输出敏感信息 | Sensitive info in model output | SENSITIVE_DATA.md |
| A04 | 工具参数携带非必要敏感数据 | Sensitive data in tool params | SENSITIVE_DATA.md |
| A05 | 工具返回结果未脱敏 | Tool result not masked | SENSITIVE_DATA.md |
| A06 | 身份鉴别信息进入对话链路 | Credentials in the chain | SENSITIVE_DATA.md |
| A07 | 数据汇聚后风险升级 | Aggregation / re-identification | SENSITIVE_DATA.md |
| A08 | 跨用户或跨租户数据泄露 | Cross-user / tenant leak (proxy) | PERMISSIONS.md |
| A09 | 未授权工具调用 | Unauthorized tool call (proxy) | PERMISSIONS.md |
| A10 | 越权参数调用 | Privilege-parameter call | PERMISSIONS.md |
| A11 | 高风险工具缺少身份信息 | High-risk call, no identity | PERMISSIONS.md |
| A12 | 策略拒绝后绕过调用 | Bypass after denial (proxy) | PERMISSIONS.md |
| A13 | 未登记工具或外部地址 | Unregistered tool / address (proxy) | EXFIL.md |
| A14 | 敏感数据传向外部工具 | Sensitive data to external tool | EXFIL.md |
| A15 | 外传保护等级下降 | Egress protection downgrade (proxy) | EXFIL.md |
| A16 | 外部传输通道不安全 | Insecure channel (proxy) | EXFIL.md |
| A17 | 批量提取或枚举数据 | Bulk extraction / enumeration | EXFIL.md |
| A18 | 隐蔽外传行为 | Covert exfiltration | EXFIL.md |
| A19 | 提示词注入导致数据访问 | Prompt injection → data access | ATTACK.md |
| A20 | 系统提示词或隐藏上下文泄露 | System-prompt / hidden-context leak | ATTACK.md |
| A21 | 智能体行为与声明能力不一致 | Behavior vs. declared (proxy) | ATTACK.md |
| A22 | 异常调用频率和调用链 | Anomalous frequency / chain | ATTACK.md |
| A23 | 安全策略命中但未阻断 | Policy hit but not blocked | ATTACK.md |
| A24 | 审计字段缺失 | Missing audit fields | AUDIT_TRAIL.md |
| A25 | 调用链断裂 | Broken call chain | AUDIT_TRAIL.md |
| A26 | 模型和策略版本不可追溯 | Untraceable versions (proxy) | AUDIT_TRAIL.md |
| A27 | 日志内容过度留存 | Excessive log retention (proxy) | AUDIT_TRAIL.md |

## Reference files

| A-codes | Reference |
|---------|-----------|
| A01–A07 sensitive data | `references/SENSITIVE_DATA.md` |
| A08–A12 permissions & isolation | `references/PERMISSIONS.md` |
| A13–A18 external transport | `references/EXFIL.md` |
| A19–A23 attack & anomaly | `references/ATTACK.md` |
| A24–A27 audit trail | `references/AUDIT_TRAIL.md` |

## Field reality

`agentguard_cim_event` **has**: `event_time`, `event_type`, `agent_id`,
`agent_type`, `deployment_id`, `session_id`, `run_id`, `provider`, `model`,
`user_message`, `assistant_message`, `tool_name`, `tool_input`, `tool_result`,
`tool_success`, `tool_duration_ms`, `hook_decision`, `block_reason`,
`raw_hook_name`, `raw_event_data`. There is **no** `user_id` column — user
attribution comes from `agent_id`/`session_id`. The model/assistant text column
is `assistant_message` (`llm_response` is an `event_type` value, not a column).

It does **not** have: `tenant_id`, `role_id`, `content_direction`,
`sensitive_types`, `policy_decision`, `policy_rule_id`, `prompt_version`,
`model_version`, `tool_version`, `protocol`, `tls_version`. Never claim to audit
a field that is not here — say it is "not captured". A15, A16, A26, and A27
depend on absent fields, so their exploration queries are proxies over the
closest observable signal.

## How to investigate

1. Open the reference file for the A-code and run its **exploration query** with
   `run_sql`. It is a single read-only `SELECT` over `table(agentguard_cim_event)`
   (always `table(...)` — a bare `FROM agentguard_cim_event` is an unbounded
   subscription; keep the `LIMIT`).
2. For each hit, drill into the surrounding session: filter the same query's
   `WHERE` down to that `session_id` (and widen the time window) to see the full
   call chain before and after the flagged event.
3. Tighten or loosen the keyword / pattern lists in the query to fit the
   deployment — they are starting points, not fixed thresholds.
4. When an A-code is a proxy (its reference says so), report the proxy's limits
   alongside the finding — say which field is "not captured".

## Evidence to record

For every finding, capture what the event supports and mark the rest "not
captured": the A-code, risk level, session & call chain (`session_id`, `run_id`),
masked hit fragment, sensitive-data type, source → destination, `event_time`,
event payload hash (`raw_event_data`), and the reason for the judgment. Tenant,
role, policy decision, and version fields are not captured — note them as such.
