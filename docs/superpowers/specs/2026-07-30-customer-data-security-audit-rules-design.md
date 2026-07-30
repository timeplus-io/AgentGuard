# Customer Data-Security Audit Rules — Design (Issue #250)

- **Issue:** [#250](https://github.com/timeplus-io/AgentGuard/issues/250) — "make security rules from customer part of built-in skills of agentguard"
- **Follow-up:** [#261](https://github.com/timeplus-io/AgentGuard/issues/261) — CIM audit-metadata schema extension to unblock the investigate-only rules.
- **Source:** `docs/research/智能体对话与工具调用数据安全审计规则清单.pdf` — a customer-authored checklist of 27 data-security audit rules (A01–A27) for agent conversations, AI gateways, and tool calls.
- **Date:** 2026-07-30
- **Status:** Design — awaiting review

## 1. Goal

Bring the customer's 27-rule audit checklist into AgentGuard as two coordinated deliverables:

1. **A self-contained rule package** (`data-security-audit`) whose members are **newly authored** rules — one per machine-detectable A-code — each carrying the PDF's own Chinese name/description verbatim. No existing rule is reused or modified.
2. **A Sentry investigation skill** (`data-audit-guide`) so an operator can ask Sentry to interactively audit against *all 27 rules* — including the ones that cannot be reduced to a single materialized view — knowing exactly which CIM fields, queries, and evidence each rule requires.

The two deliverables are complementary: the package is the always-on detection layer; the skill is the on-demand analyst layer that also covers the rules the package structurally cannot.

### 1.1 Why "recreate, not reuse"

An earlier draft mapped some A-codes onto existing `rp-*` seed rules. That created a **many-to-many tangle** (one A-code → several `rp-*`; one `rp-*` → several A-codes, e.g. `rp-002` covers both A03 and A05) and would have required rewriting the Chinese name/description of shared, catalog-wide rules to match the customer doc — degrading those rules in every other context they appear.

Instead, every A-code gets its **own** fresh `dsa-aNN` rule. The rule ID *is* the A-code, the mapping is strictly 1:1, and each rule owns the PDF's exact wording. Existing `rp-*` rules are untouched. Some new rules overlap in spirit with existing detectors (e.g. `dsa-a19` prompt-injection vs. `rp-001`); this duplication is intentional and acceptable — the package is a coherent, PDF-faithful customer baseline, using simpler predicates aligned to the customer's framing rather than the elaborate research-grade logic of the `rp-*` set.

## 2. Background: why some rules can't be MVs

AgentGuard's normalized event stream `agentguard_cim_event` exposes: `event_type`, `event_time`, `agent_id`, `agent_type`, `deployment_id`, `session_id`, `run_id`, `provider`, `model`, `user_message`, `assistant_message`, `tool_name`, `tool_input`, `tool_result`, `tool_success`, `tool_duration_ms`, `hook_decision`, `block_reason`, `raw_hook_name`, `raw_event_data`. Note: the model-text column is `assistant_message` (`llm_response` is an `event_type` value, not a column), and there is **no** `user_id` column — attribution is via `agent_id`/`session_id`.

The customer checklist (PDF §8) assumes a richer minimum-field set — `tenant_id`, `role_id`, `content_direction`, `sensitive_types`, `policy_decision`, `policy_rule_id`, `prompt_version`, `model_version`, `tool_version`, `protocol`, `tls_version`. Many of these do **not** exist in the current model, and the PDF's own §2 applicability assessment already flags several rules as "conditional" or "not currently applicable."

Consequently each A-code falls into one of two buckets:

- ✅ **Authored** — the rule reduces to a single-event (or simple-window) predicate over available CIM fields. It becomes a new `dsa-aNN` seed rule and a package member.
- 🔍 **Investigate-only** — it cannot be a single MV (missing field, cross-stream correlation, multi-turn stateful comparison, or governance concern). Documented in the skill with an investigation recipe; **not** a package member. Unblocked later by #261.

## 3. The A01–A27 coverage map

| A-code | 中文（PDF 原文名） | English gloss | Bucket | Rule ID |
|--------|-----------------|---------------|--------|---------|
| A01 | 用户输入敏感数据检测 | Sensitive PII in user input | ✅ authored | `dsa-a01` |
| A02 | 输入模型前未脱敏 | Not masked before model | 🔍 investigate | — |
| A03 | 模型输出敏感信息 | Sensitive info in model output | ✅ authored | `dsa-a03` |
| A04 | 工具参数携带非必要敏感数据 | Unnecessary sensitive data in tool params | ✅ authored | `dsa-a04` |
| A05 | 工具返回结果未脱敏 | Tool result not masked | ✅ authored | `dsa-a05` |
| A06 | 身份鉴别信息进入对话链路 | Credentials in the conversation chain | ✅ authored | `dsa-a06` |
| A07 | 数据汇聚后风险升级 | Risk escalation after aggregation | ✅ authored | `dsa-a07` |
| A08 | 跨用户或跨租户数据泄露 | Cross-user / cross-tenant leak | ✅ authored | `dsa-a08` |
| A09 | 未授权工具调用 | Unauthorized tool call | ✅ authored | `dsa-a09` |
| A10 | 越权参数调用 | Privilege-parameter call | ✅ authored | `dsa-a10` |
| A11 | 高风险工具缺少身份信息 | High-risk tool call missing identity | ✅ authored | `dsa-a11` |
| A12 | 策略拒绝后绕过调用 | Bypass after policy denial | ✅ authored | `dsa-a12` |
| A13 | 未登记工具或外部地址 | Unregistered tool / external address | 🔍 investigate | — |
| A14 | 敏感数据传向外部工具 | Sensitive data to external tool | ✅ authored | `dsa-a14` |
| A15 | 外传保护等级下降 | Egress protection-level downgrade | 🔍 investigate | — |
| A16 | 外部传输通道不安全（条件规则） | Insecure transport channel (conditional) | 🔍 investigate | — |
| A17 | 批量提取或枚举数据 | Bulk extraction / enumeration | ✅ authored | `dsa-a17` |
| A18 | 隐蔽外传行为 | Covert exfiltration (encoding / split) | ✅ authored | `dsa-a18` |
| A19 | 提示词注入导致数据访问 | Prompt injection → data access | ✅ authored | `dsa-a19` |
| A20 | 系统提示词或隐藏上下文泄露 | System-prompt / hidden-context leak | ✅ authored | `dsa-a20` |
| A21 | 智能体行为与声明能力不一致 | Behavior vs. declared capability | 🔍 investigate | — |
| A22 | 异常调用频率和调用链 | Anomalous call frequency / chain | ✅ authored | `dsa-a22` |
| A23 | 安全策略命中但未阻断 | Policy hit but not blocked | 🔍 investigate | — |
| A24 | 审计字段缺失 | Missing audit fields | 🔍 investigate | — |
| A25 | 调用链断裂 | Broken call chain | 🔍 investigate | — |
| A26 | 模型和策略版本不可追溯 | Untraceable model / policy versions | 🔍 investigate | — |
| A27 | 日志内容过度留存 | Excessive log retention | 🔍 investigate | — |

**Tally:** 17 authored `dsa-aNN` rules + 10 investigate-only A-codes = 27. The package covers **all 10** of the 12 PDF-recommended priority rules (§9: A03, A06, A08, A09, A10, A12, A14, A18, A19, A20, A23, A25) that are machine-detectable; the remaining two (A23, A25) are investigate-only in the skill until #261.

## 4. Deliverable 1 — the rule package

### 4.1 Package manifest

`backend/internal/catalog/packages/data-security-audit.yaml`:

- `id: data-security-audit`
- `name: "Data-Security Audit (Customer Baseline)"`, `default_enabled: false`
- `description`: covers the customer's conversation / tool-call data-security audit checklist — foundational PII, credential, egress, permission, injection, and anomaly checks (A01–A27).
- `rules`: the 17 authored `dsa-aNN` IDs from §3, and nothing else.
- `translations.zh-CN`: localized `name`/`description` (guarded by `TestEmbeddedPackagesHaveChineseTranslations`).

Membership is guarded by `TestEmbeddedPackageRulesExist` (no dangling IDs).

### 4.2 Visibility

Add `data-security-audit` to the `rules.visible_packages` config default so the customer sees the package on `/rules` without operator setup. Default becomes `["core", "data-security-audit"]`. This is the existing `TypeList` config key; no schema change, hot-applied.

### 4.3 The 17 authored seed rules

Each is a standard seed `Definition` YAML under `backend/internal/catalog/seed/`, following the existing schema exactly: `id` (`dsa-aNN`), `name` (English, prefixed `[A0x]`), `version: "1.0.0"`, bilingual `description`, `translations.zh-CN` (**name = the PDF's 原文名 verbatim; description = the PDF's rule text verbatim**), `severity`, `category`, `hook_sources`, `tags` (including `audit-aNN`, `data-security`, `customer-baseline`), and a `sql_template` selecting into `agentguard_security_events` via the standard column list (`detected_at, rule_id, rule_name, severity, agent_id, deployment_id, session_id, run_id, hook_name, tool_name, message, event_data`). Predicates use `match()` / `position()` / `multi_search_any()` (never wildcard-escaped `LIKE` on user data). Rules are **zero-parameter** unless a threshold is naturally tunable (windowed rules A17/A22 expose a count/window param).

| ID | A | severity | category | hook_sources | Detection sketch |
|----|---|----------|----------|--------------|------------------|
| `dsa-a01` | A01 | warning | sensitive_data_exposure | `user_input` | ID-card numbers, bank-card (13–19 digit) sequences, password/verification-code/secret-key markers in `user_message`. |
| `dsa-a03` | A03 | critical | sensitive_data_exposure | `llm_response` | PII / account / auth / customer-data markers in `llm_response`. |
| `dsa-a04` | A04 | warning | sensitive_data_exposure | `tool_invoke` | Sensitive-field markers (password/token/ssn/card/secret/api_key) in `tool_input` where the tool is not an auth/identity tool. |
| `dsa-a05` | A05 | warning | sensitive_data_exposure | `tool_complete` | Full customer-data / PII signatures in `tool_result` with no masking markers present. |
| `dsa-a06` | A06 | critical | credential_theft_and_harvesting | `user_input,llm_request,tool_invoke,tool_complete,llm_response` | Authentication material — `Bearer` tokens, `AKIA…` AccessKeys, `-----BEGIN … PRIVATE KEY-----`, `password=`, session cookies — anywhere in the chain. |
| `dsa-a07` | A07 | warning | sensitive_data_exposure | `llm_response,tool_complete` | Co-occurrence of multiple distinct sensitive-data types (name + account + ID/transaction) in one event — the aggregation signal. |
| `dsa-a08` | A08 | critical | data_exfiltration_and_leakage | `tool_invoke,tool_complete,llm_response` | Tenant/org/user identifier manipulation or foreign-subject markers in `tool_input`/`tool_result` (IDOR/BOLA-style), best-effort pending `tenant_id` (#261). |
| `dsa-a09` | A09 | warning | agent_privilege_escalation | `tool_invoke` | Invocation of high-privilege / admin / dev-ops tool-name patterns outside a declared allow-list heuristic. |
| `dsa-a10` | A10 | warning | agent_privilege_escalation | `tool_invoke` | User/org/tenant/resource-ID or file-path manipulation (`../`, absolute-path escape, id-swap params) in `tool_input`. |
| `dsa-a11` | A11 | warning | data_exfiltration_and_leakage | `tool_invoke,tool_complete` | High-risk verbs (export/download/send/delete/modify/query-sensitive) in `tool_name`/`tool_input` while `user_id = ''` (unattributable). |
| `dsa-a12` | A12 | warning | prompt_injection_and_hijacking | `tool_invoke,tool_complete` | Encoded/split retry patterns and monitor-bypass phrasing indicating a re-attempt after a denial (single-event heuristic; cross-event correlation is investigate-only). |
| `dsa-a14` | A14 | critical | data_exfiltration_and_leakage | `tool_invoke` | Sensitive-data markers in `tool_input` to an external/network tool (http/webhook/upload/domain/IP targets). |
| `dsa-a17` | A17 | warning | resource_exhaustion_and_dos | `tool_invoke,tool_complete` | Windowed: high count of query/search/paging/export calls per session over a tumbling window (tunable threshold). |
| `dsa-a18` | A18 | critical | data_exfiltration_and_leakage | `tool_invoke` | Covert egress: long base64/hex blobs, `data:`/percent-encoded payloads, high-entropy strings in outbound `tool_input`. |
| `dsa-a19` | A19 | critical | prompt_injection_and_hijacking | `user_input,tool_complete` | Injection markers (ignore-rules / reveal-system-prompt / call-internal-tool / read-other-user-data) in `user_message`/`tool_result`. |
| `dsa-a20` | A20 | critical | data_exfiltration_and_leakage | `llm_response` | System-prompt / internal-policy / tool-definition / hidden-context / KB-key leakage markers in `llm_response`. |
| `dsa-a22` | A22 | warning | resource_exhaustion_and_dos | `tool_invoke,tool_complete,llm_response` | Windowed: per-session call spike, repeated failed retries, or high-token/long-context signal over a tumbling window (tunable threshold). |

Exact regexes/thresholds are finalized during implementation and validated by candidate preflight; the sketches bound scope. FP-rate discipline follows the existing rules — start `warning` where ambiguous, `critical` only for unambiguous high-risk signals (credentials, system-prompt leak, covert exfil).

### 4.4 zh-CN fidelity

For every authored rule, `translations.zh-CN.name` and `translations.zh-CN.description` are the **PDF's verbatim text** for that A-code (see §3 for names; descriptions come from PDF §3–§7). This is the mechanism by which the customer's Chinese wording enters the product — cleanly, without editing any shared `rp-*` rule.

## 5. Deliverable 2 — the Sentry investigation skill

### 5.1 Layout

New knowledge skill, auto-discovered by the embed loader (`backend/internal/sentry/skills/knowledge.go`):

```
backend/internal/sentry/skills/knowledge/data-audit-guide/
  SKILL.md
  references/
    SENSITIVE_DATA.md   # A01–A07
    PERMISSIONS.md      # A08–A12
    EXFIL.md            # A13–A18
    ATTACK.md           # A19–A23
    AUDIT_TRAIL.md      # A24–A27
```

### 5.2 SKILL.md contents

- **What this skill is:** the customer's data-security audit checklist as an interactive playbook; load before auditing conversation/tool-call data against A01–A27.
- **Audit boundary** (PDF §1): runtime conversation/gateway/tool-call data only — not infrastructure/DB/backup config.
- **The A01–A27 catalog table** (from §3): A-code, 中文 name, English gloss, bucket, and the mapped `dsa-aNN` rule ID or "investigate-only." This is the map that makes Sentry's answers accurate — including telling the operator which A-codes have a live rule vs. which require manual investigation.
- **Minimum-field reality** (PDF §8 vs. CIM): which of the 34 checklist fields exist in `agentguard_cim_event` and which are absent — so Sentry never claims to check a field it cannot see. Names the gaps explicitly (`tenant_id`, `policy_decision`, `tls_version`, version fields) and points at #261.
- **Evidence requirements** (PDF §9): the checklist every finding must carry — rule code, risk level, user/tenant, session & call chain, masked hit fragment, sensitive-data type, source→destination, tool/model version, policy decision, event time, event hash, reason. Sentry fills what CIM supports and marks the rest "not captured."
- **How to investigate:** for an authored A-code, query the rule's detections in `agentguard_security_events` first; use `dlp_search` for meaning-based content checks; use `run_sql` (`table(...)`, read-only) for structured predicates; use `get_rule_catalog`/`get_rule_status` for rule state. Points at the per-category reference for recipes.

### 5.3 Reference files

Each reference covers its A-codes with a uniform block per rule: **definition** (PDF verbatim), **bucket + mapped `dsa-aNN` / security-event**, **CIM fields available vs. gaps**, a **ready `run_sql` / `dlp_search` recipe** (following `agentguard-guide` conventions — `table(stream)`, `arg_max` where needed), and the **evidence fields** to record for a finding.

### 5.4 Visibility gate

The skill is about investigating AgentGuard data, so it is gated behind the same switch as `agentguard-guide`: in `backend/internal/handlers/sentry.go`, when `!cc.AgentGuardToolsEnabled`, append `"data-audit-guide"` to `hiddenSkills` alongside `"agentguard-guide"` (one line). Off → not listed, not loadable; on → available via `load_skill("data-audit-guide")`.

The loop's system-prompt hint (`loop.go` `agRules`) is extended to mention `data-audit-guide` for customer-audit questions.

## 6. Testing

- **Package integrity:** existing `TestEmbeddedPackageRulesExist` and `TestEmbeddedPackagesHaveChineseTranslations` now also cover `data-security-audit` — asserting all 17 members exist and the manifest has zh-CN.
- **New-rule compilation:** a test compiles each `dsa-aNN` definition (via `rules.ValidateDefinition` + the compile/preflight path used by `CompileCatalogRule`) to prove each `sql_template` renders and is structurally valid.
- **Rule zh-CN:** assert each authored rule carries `translations.zh-CN` name + description (mirrors the seed-rule convention).
- **Skill loads:** a test mirroring `guide_sql_test.go` asserts `load_skill("data-audit-guide")` returns SKILL.md and each `references/*.md` resolves via `load_skill_reference`, and that both are hidden when `agentguard_tools_enabled` is false.
- **Build/lint:** `CGO_ENABLED=0 go build ./...`, `go test ./...`, backend lint.

## 7. Out of scope / non-goals

- **No CIM schema changes** here — that is #261. The skill documents the field gaps rather than papering over them; the 10 investigate-only A-codes wait on #261 before they can become live rules.
- **No new agent-plugin telemetry.** Rules operate over existing CIM fields only.
- **No modification of existing `rp-*` rules.** Detection overlap between `dsa-*` and `rp-*` is intentional; the customer package is self-contained.
- **A16/A26/A27** stay investigate-only: transport-security, version-traceability, and log-retention-governance are not derivable from current runtime data.
- **No auto-install.** The package is `default_enabled: false`; visible but opt-in, consistent with every non-core package.

## 8. Rollout

1. Author the 17 `dsa-aNN` seed rules (PDF-verbatim zh-CN); confirm each compiles via preflight.
2. Add the package manifest + zh-CN; add `data-security-audit` to `visible_packages` default.
3. Add the `data-audit-guide` skill (SKILL.md + 5 references); wire the visibility gate + loop hint.
4. Tests green; `CGO_ENABLED=0 go build ./...` clean.
5. Operator enables the package on `/rules` (or `POST /api/rules/packages/data-security-audit/enable`) and turns on `agentguard_tools_enabled` to expose the skill to Sentry.
