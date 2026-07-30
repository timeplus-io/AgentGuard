# Customer Data-Security Audit Rules — Design (Issue #250)

- **Issue:** [#250](https://github.com/timeplus-io/AgentGuard/issues/250) — "make security rules from customer part of built-in skills of agentguard"
- **Source:** `docs/research/智能体对话与工具调用数据安全审计规则清单.pdf` — a customer-authored checklist of 27 data-security audit rules (A01–A27) for agent conversations, AI gateways, and tool calls.
- **Date:** 2026-07-30
- **Status:** Design — awaiting review

## 1. Goal

Bring the customer's 27-rule audit checklist into AgentGuard as two coordinated deliverables:

1. **A rule package** (`data-security-audit`) that turns the checklist's *machine-detectable* rules into live streaming detections — some newly authored, most mapped to existing seed rules.
2. **A Sentry investigation skill** (`data-audit-guide`) so an operator can ask Sentry to interactively audit against *all 27 rules* — including the ones that cannot be reduced to a single materialized view — knowing exactly which CIM fields, queries, and evidence each rule requires.

The two deliverables are complementary: the package is the always-on detection layer; the skill is the on-demand analyst layer that also covers the rules the package structurally cannot.

## 2. Background: why a two-layer split

AgentGuard's normalized event stream `agentguard_cim_event` exposes: `event_type`, `event_time`, `agent_id`, `agent_type`, `deployment_id`, `session_id`, `run_id`, `model`, `user_message`, `llm_response`, `tool_name`, `tool_input`, `tool_result`, `raw_hook_name`, `raw_event_data`, and a sparse `user_id`/`target`/`destination`.

The customer checklist (PDF §8) assumes a richer minimum-field set — `tenant_id`, `role_id`, `content_direction`, `sensitive_types`, `policy_decision`, `policy_rule_id`, `prompt_version`, `model_version`, `tool_version`, `protocol`, `tls_version`. Many of these do **not** exist in the current model, and the PDF's own §2 applicability assessment already flags several rules as "conditional" or "not currently applicable."

Consequently each A-code falls into one of three buckets:

- ♻️ **Reuse** — an existing `rp-*` seed rule already detects this threat class over available fields.
- 🆕 **New** — a foundational data-handling check with no good existing match; author a new seed rule.
- 🔍 **Investigate-only** — cannot be a single MV (missing field, cross-stream correlation, stateful/multi-turn, or governance concern). Documented in the skill with an investigation recipe; **not** a package member.

This split is honest about what streaming SQL can and cannot assert, and it is exactly why the issue asks for both a package and a skill.

## 3. The A01–A27 coverage map

| A-code | 中文 | English gloss | Bucket | Rule(s) |
|--------|------|---------------|--------|---------|
| A01 | 用户输入敏感数据检测 | Sensitive PII in user input | 🆕 new | `dsa-a01` |
| A02 | 输入模型前未脱敏 | Not masked before model | 🔍 investigate | — |
| A03 | 模型输出敏感信息 | Sensitive info in model output | ♻️ reuse | `rp-002` |
| A04 | 工具参数携带非必要敏感数据 | Unnecessary sensitive data in tool params | 🆕 new | `dsa-a04` |
| A05 | 工具返回结果未脱敏 | Tool result not masked | ♻️ reuse | `rp-002` |
| A06 | 身份鉴别信息进入对话链路 | Credentials in the conversation chain | 🆕 new | `dsa-a06` |
| A07 | 数据汇聚后风险升级 | Risk escalation after aggregation | ♻️+🔍 | `rp-074` |
| A08 | 跨用户或跨租户数据泄露 | Cross-user / cross-tenant leak | ♻️+🔍 | `rp-035`, `rp-027` |
| A09 | 未授权工具调用 | Unauthorized tool call | ♻️+🔍 | `rp-003` |
| A10 | 越权参数调用 | Privilege-parameter call | ♻️ reuse | `rp-060`, `rp-035` |
| A11 | 高风险工具缺少身份信息 | High-risk tool call missing identity | 🆕 new | `dsa-a11` |
| A12 | 策略拒绝后绕过调用 | Bypass after policy denial | ♻️+🔍 | `rp-065`, `rp-068` |
| A13 | 未登记工具或外部地址 | Unregistered tool / external address | 🔍 investigate | (`rp-007`) |
| A14 | 敏感数据传向外部工具 | Sensitive data to external tool | ♻️ reuse | `rp-073`, `rp-007` |
| A15 | 外传保护等级下降 | Egress protection-level downgrade | 🔍 investigate | — |
| A16 | 外部传输通道不安全（条件规则） | Insecure transport channel (conditional) | 🔍 N/A | — |
| A17 | 批量提取或枚举数据 | Bulk extraction / enumeration | ♻️ reuse | `rp-043` |
| A18 | 隐蔽外传行为 | Covert exfiltration (encoding / split) | 🆕 new | `dsa-a18` |
| A19 | 提示词注入导致数据访问 | Prompt injection → data access | ♻️ reuse | `rp-001`, `rp-014` |
| A20 | 系统提示词或隐藏上下文泄露 | System-prompt / hidden-context leak | ♻️ reuse | `rp-012` |
| A21 | 智能体行为与声明能力不一致 | Behavior vs. declared capability | 🔍 investigate | — |
| A22 | 异常调用频率和调用链 | Anomalous call frequency / chain | ♻️ reuse | `rp-006`, `rp-018` |
| A23 | 安全策略命中但未阻断 | Policy hit but not blocked | 🔍 investigate | — |
| A24 | 审计字段缺失 | Missing audit fields | 🔍 investigate | — |
| A25 | 调用链断裂 | Broken call chain | 🔍 investigate | — |
| A26 | 模型和策略版本不可追溯 | Untraceable model / policy versions | 🔍 N/A | — |
| A27 | 日志内容过度留存 | Excessive log retention | 🔍 N/A | — |

**Tally:** 5 new rules (`dsa-a01/04/06/11/18`), covering 5 A-codes; 16 reused `rp-*` rules covering 12 A-codes; 10 investigate-only A-codes (5 + 12 + 10 = 27). This covers **10 of the 12** PDF-recommended priority rules (§9: A03, A06, A08, A09, A10, A12, A14, A18, A19, A20, A23, A25) with live MVs; the remaining two (A23, A25) are investigate-only in the skill.

### 3.1 Reused-rule package members (deduplicated)

`rp-001, rp-002, rp-003, rp-006, rp-007, rp-012, rp-014, rp-018, rp-027, rp-035, rp-043, rp-060, rp-065, rp-068, rp-073, rp-074`

A rule may map to several A-codes; the package lists each `rp-*` once. The A-code→rule mapping table above is the authoritative cross-reference and is reproduced in the skill.

## 4. Deliverable 1 — the rule package

### 4.1 Package manifest

`backend/internal/catalog/packages/data-security-audit.yaml`:

- `id: data-security-audit`
- `name: "Data-Security Audit (Customer Baseline)"`, `default_enabled: false`
- `description`: covers the customer's conversation/tool-call data-security audit checklist; foundational PII/credential/egress/permission checks.
- `rules`: the 5 new `dsa-*` IDs **+** the 16 reused `rp-*` IDs from §3.1.
- `translations.zh-CN`: localized `name`/`description` (guarded by `TestEmbeddedPackagesHaveChineseTranslations`).

Membership is guarded by `TestEmbeddedPackageRulesExist` (no dangling IDs), so this test also proves every mapped `rp-*` still exists.

### 4.2 Visibility

Add `data-security-audit` to the `rules.visible_packages` config default so the customer sees the package on `/rules` without operator setup. Default becomes `["core", "data-security-audit"]`. This is the existing `TypeList` config key; no schema change, hot-applied.

### 4.3 The 5 new seed rules

Each new rule is a standard seed `Definition` YAML under `backend/internal/catalog/seed/`, following the existing schema exactly: `id`, `name` (prefixed `[A0x]` for traceability), `version: "1.0.0"`, bilingual `description` + `translations.zh-CN`, `severity`, `category`, `hook_sources`, `tags` (including `audit-a0x`, `data-security`, `customer-baseline`), and a `sql_template` selecting into `agentguard_security_events` via the standard column list (`detected_at, rule_id, rule_name, severity, agent_id, deployment_id, session_id, run_id, hook_name, tool_name, message, event_data`). Predicates use `match()` / `position()` / `multi_search_any()` (never wildcard-escaped `LIKE` on user data), matching repo convention. All are **zero-parameter** rules (template ≡ rendered SQL) unless a threshold is naturally tunable.

| ID | A | severity | category | hook_sources | Detection sketch |
|----|---|----------|----------|--------------|------------------|
| `dsa-a01` | A01 | warning | sensitive_data_exposure | `user_input` | ID-card numbers, bank-card (13–19 digit) sequences, passwords/keys/verification-code markers in `user_message`. |
| `dsa-a04` | A04 | warning | sensitive_data_exposure | `tool_invoke` | Sensitive-field markers (password/token/ssn/card/secret/api_key) inside `tool_input` where the tool is not an auth/identity tool. |
| `dsa-a06` | A06 | critical | credential_theft_and_harvesting | `user_input,llm_request,tool_invoke,tool_complete,llm_response` | Authentication material — `Bearer` tokens, `AKIA…` AccessKeys, `-----BEGIN … PRIVATE KEY-----`, `password=`, session cookies — appearing anywhere in the chain. Broad, credential-in-any-channel; complements `rp-008`'s harvesting focus. |
| `dsa-a11` | A11 | warning | data_exfiltration_and_leakage | `tool_invoke,tool_complete` | High-risk verbs (export/download/send/delete/modify/query-sensitive) in `tool_name`/`tool_input` while `user_id = ''` (unattributable actor). |
| `dsa-a18` | A18 | critical | data_exfiltration_and_leakage | `tool_invoke` | Covert egress: long base64/hex blobs or `data:`/percent-encoded payloads in outbound `tool_input` to network tools; high-entropy string signature. Complements `rp-053` (DNS tunneling) / `rp-063` (log-to-leak). |

Exact regexes are finalized during implementation and validated by candidate preflight; the sketches above bound scope. FP-rate tuning follows the existing rules' `fp_rate` discipline (start `warning` where ambiguous).

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
- **The A01–A27 catalog table** (from §3 here): A-code, 中文 name, English gloss, bucket, and the mapped rule ID or "investigate-only." This is the map that makes Sentry's answers accurate.
- **Minimum-field reality** (PDF §8 vs. CIM): which of the 34 checklist fields exist in `agentguard_cim_event`, and which are absent — so Sentry never claims to check a field it cannot see. Names the gaps explicitly (`tenant_id`, `policy_decision`, `tls_version`, version fields).
- **Evidence requirements** (PDF §9): the checklist every finding must carry — rule code, risk level, user/tenant, session & call chain, masked hit fragment, sensitive-data type, source→destination, tool/model version, policy decision, event time, event hash, reason. Sentry fills what CIM supports and marks the rest "not captured."
- **How to investigate:** prefer the mapped rule's detections in `agentguard_security_events` first; use `dlp_search` for meaning-based content checks; use `run_sql` (`table(...)`, read-only) for structured predicates; use `get_rule_catalog`/`get_rule_status` for rule state. Points at the per-category reference for recipes.

### 5.3 Reference files

Each reference covers its A-codes with a uniform block per rule: **definition** (from the PDF), **bucket + mapped rule/security-event**, **CIM fields available vs. gaps**, a **ready `run_sql` / `dlp_search` recipe** (following `agentguard-guide` query conventions — `table(stream)`, `arg_max` where needed), and the **evidence fields** to record for a finding.

### 5.4 Visibility gate

The skill is about investigating AgentGuard data, so it is gated behind the same switch as `agentguard-guide`: in `backend/internal/handlers/sentry.go`, when `!cc.AgentGuardToolsEnabled`, append `"data-audit-guide"` to `hiddenSkills` alongside `"agentguard-guide"` (one line). Off → not listed, not loadable; on → available via `load_skill("data-audit-guide")`.

The loop's system-prompt hint (`loop.go` `agRules`) is extended to mention `data-audit-guide` for customer-audit questions.

## 6. Testing

- **Package integrity:** existing `TestEmbeddedPackageRulesExist` and `TestEmbeddedPackagesHaveChineseTranslations` now also cover `data-security-audit` — they assert every member (including the 16 reused `rp-*`) exists and the manifest has zh-CN.
- **New-rule compilation:** a test compiles each `dsa-*` definition (via `rules.ValidateDefinition` + the compile/preflight path used by `CompileCatalogRule`) to prove the `sql_template` renders and is structurally valid.
- **Rule zh-CN:** the new rules carry `translations.zh-CN`; assert presence (mirrors the seed-rule convention).
- **Skill loads:** a test mirroring `guide_sql_test.go` asserts `load_skill("data-audit-guide")` returns SKILL.md and each `references/*.md` resolves via `load_skill_reference`, and that both are hidden when `agentguard_tools_enabled` is false.
- **Build/lint:** `CGO_ENABLED=0 go build ./...`, `go test ./...`, backend lint.

## 7. Out of scope / non-goals

- **No CIM schema changes.** We do not add `tenant_id`, `policy_decision`, `tls_version`, or version fields to close the investigate-only gaps. Doing so is a separate, larger effort (gateway/plugin telemetry changes) and would be its own spec. The skill documents the gaps rather than papering over them.
- **No new agent-plugin telemetry.** Rules operate over existing CIM fields only.
- **A16/A26/A27** stay N/A: transport-security, version-traceability, and log-retention-governance are not derivable from current runtime data.
- **No auto-install.** The package is `default_enabled: false`; visible but opt-in, consistent with every non-core package.

## 8. Rollout

1. Add the 5 `dsa-*` seed rules; confirm each compiles.
2. Add the package manifest + zh-CN; add `data-security-audit` to `visible_packages` default.
3. Add the `data-audit-guide` skill (SKILL.md + 5 references); wire the visibility gate + loop hint.
4. Tests green; `go build ./...` clean.
5. Operator enables the package on `/rules` (or via `POST /api/rules/packages/data-security-audit/enable`) and turns on `agentguard_tools_enabled` to expose the skill to Sentry.
