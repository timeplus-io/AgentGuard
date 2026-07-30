---
name: data-audit-guide
description: >
  The customer data-security audit checklist (A01–A27) as an interactive
  playbook: which A-codes have a live detection rule, which require manual
  investigation, the CIM fields each needs (and the gaps), and the evidence to
  record. Load this before auditing conversation / tool-call data against the
  customer checklist.
metadata:
  author: timeplus-io
  scope: AgentGuard Sentry (read-only investigation of the customer audit rules)
---

# Data-Security Audit Guide (Customer Checklist A01–A27)

You are auditing AgentGuard conversation, gateway, and tool-call data against a
customer-authored checklist of 27 data-security rules (A01–A27). This guide is
the map that keeps your answers accurate: which A-code has a live rule, which
you must investigate by hand, what fields exist, and what evidence to record.

## Audit boundary

Scope is **runtime data only** — user input, system prompts, context,
knowledge-base results, model output, gateway request/response, and tool calls
(name, params, return, status, call chain). Infrastructure, database, backup,
endpoint, and network-device configuration are **out of scope**: the data to
judge them does not exist here. A rule "passing" is a technical signal, not
proof that contracts, approvals, or policy are satisfied.

## The A01–A27 catalog

Legend: ✅ = a live `dsa-aNN` rule writes detections to
`agentguard_security_events`; 🔍 = investigate-only (no single rule — check by
hand using the recipe in the reference file).

| A | 中文 | Status | Rule / where |
|---|------|--------|--------------|
| A01 | 用户输入敏感数据检测 | ✅ | `dsa-a01` |
| A02 | 输入模型前未脱敏 | 🔍 | SENSITIVE_DATA.md |
| A03 | 模型输出敏感信息 | ✅ | `dsa-a03` |
| A04 | 工具参数携带非必要敏感数据 | ✅ | `dsa-a04` |
| A05 | 工具返回结果未脱敏 | ✅ | `dsa-a05` |
| A06 | 身份鉴别信息进入对话链路 | ✅ | `dsa-a06` |
| A07 | 数据汇聚后风险升级 | ✅ | `dsa-a07` |
| A08 | 跨用户或跨租户数据泄露 | ✅ | `dsa-a08` |
| A09 | 未授权工具调用 | ✅ | `dsa-a09` |
| A10 | 越权参数调用 | ✅ | `dsa-a10` |
| A11 | 高风险工具缺少身份信息 | ✅ | `dsa-a11` |
| A12 | 策略拒绝后绕过调用 | ✅ | `dsa-a12` |
| A13 | 未登记工具或外部地址 | 🔍 | EXFIL.md |
| A14 | 敏感数据传向外部工具 | ✅ | `dsa-a14` |
| A15 | 外传保护等级下降 | 🔍 | EXFIL.md |
| A16 | 外部传输通道不安全 | 🔍 | EXFIL.md |
| A17 | 批量提取或枚举数据 | ✅ | `dsa-a17` |
| A18 | 隐蔽外传行为 | ✅ | `dsa-a18` |
| A19 | 提示词注入导致数据访问 | ✅ | `dsa-a19` |
| A20 | 系统提示词或隐藏上下文泄露 | ✅ | `dsa-a20` |
| A21 | 智能体行为与声明能力不一致 | 🔍 | ATTACK.md |
| A22 | 异常调用频率和调用链 | ✅ | `dsa-a22` |
| A23 | 安全策略命中但未阻断 | 🔍 | ATTACK.md |
| A24 | 审计字段缺失 | 🔍 | AUDIT_TRAIL.md |
| A25 | 调用链断裂 | 🔍 | AUDIT_TRAIL.md |
| A26 | 模型和策略版本不可追溯 | 🔍 | AUDIT_TRAIL.md |
| A27 | 日志内容过度留存 | 🔍 | AUDIT_TRAIL.md |

## Reference files

| A-codes | Reference |
|---------|-----------|
| A01–A07 sensitive data | `references/SENSITIVE_DATA.md` |
| A08–A12 permissions & isolation | `references/PERMISSIONS.md` |
| A13–A18 external transport | `references/EXFIL.md` |
| A19–A23 attack & anomaly | `references/ATTACK.md` |
| A24–A27 audit trail | `references/AUDIT_TRAIL.md` |

## Field reality (checklist §8 vs. CIM)

`agentguard_cim_event` **has**: `event_time`, `event_type`, `agent_id`,
`agent_type`, `deployment_id`, `session_id`, `run_id`, `model`, `user_message`,
`llm_response`, `tool_name`, `tool_input`, `tool_result`, `raw_hook_name`,
`raw_event_data`, and (sparsely) `user_id`.

It does **not** have: `tenant_id`, `role_id`, `content_direction`,
`sensitive_types`, `policy_decision`, `policy_rule_id`, `prompt_version`,
`model_version`, `tool_version`, `protocol`, `tls_version`. Never claim to
audit a field that is not here — say it is "not captured" and point at
**issue #261** (the CIM audit-metadata extension that will add these). A16, A26,
A27 depend entirely on absent fields and are always investigate-only /
inconclusive today.

## How to investigate

1. For a ✅ A-code, read its detections first — they are authoritative:
   ```sql
   SELECT detected_at, agent_id, session_id, message
   FROM table(agentguard_security_events)
   WHERE rule_id = 'dsa-a06' AND detected_at >= now() - INTERVAL 1 DAY
   ORDER BY detected_at DESC
   ```
2. For a 🔍 A-code, follow the recipe in the reference file — usually a
   `run_sql` predicate over `table(agentguard_cim_event)` or a `dlp_search`
   for meaning-based content.
3. Prefer `dlp_search` over hand-written SQL for "does any message contain X
   kind of sensitive content" questions.
4. For rule state (installed / enabled / MV health) use `get_rule_status` and
   `get_rule_catalog`.

## Evidence to record (checklist §9)

For every finding, capture what CIM supports and mark the rest "not captured":
rule code (A-code + `dsa-*`), risk level, user & tenant (tenant = not
captured), session & call chain (`session_id`, `run_id`; `trace_id` linkage =
best-effort), masked hit fragment, sensitive-data type, source → destination,
tool & model version (versions = not captured), policy decision (not captured),
event time, event hash (`raw_event_data`), and the reason for the judgment.
