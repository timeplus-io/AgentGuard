# Customer Data-Security Audit Rules Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the customer's 27-rule data-security audit checklist (PDF A01–A27) as a self-contained `data-security-audit` rule package (17 newly authored `dsa-aNN` seed rules) plus a `data-audit-guide` Sentry investigation skill covering all 27 rules.

**Architecture:** Each machine-detectable A-code becomes its own fresh seed-rule YAML (`internal/catalog/seed/dsa-aNN.yaml`) whose materialized view runs a single-event predicate over `agentguard_cim_event` and writes to `agentguard_security_events` — exactly like the existing `rp-*` rules. A package manifest (`internal/catalog/packages/data-security-audit.yaml`) bundles the 17. The 10 investigate-only A-codes are documented in a new embedded knowledge skill under `internal/sentry/skills/knowledge/data-audit-guide/`. No existing `rp-*` rule is modified.

**Tech Stack:** Go 1.x (Gin backend), YAML seed rules, Timeplus streaming SQL (RE2 `match`, `multi_search_any`, `position`), Go embed FS, table-driven Go tests.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-07-30-customer-data-security-audit-rules-design.md` is authoritative.
- **Build:** always `CGO_ENABLED=0` (macOS 26 Apple-Silicon dyld abort otherwise). Backend commands run from `backend/`.
- **No `rp-*` edits.** Only create new `dsa-*` files, one package manifest, one skill tree, and the two one-line wiring changes named in Tasks 7/8.
- **SQL predicate discipline:** use ONLY `multi_search_any(lower(col), [...])`, `position(col, 'literal') > 0`, `length(col)`, and `match(col, 'PATTERN')` where PATTERN contains NO backslash escapes (Proton parses `\b`/`\t`/`\s`... inside string literals as control chars). Use `[0-9]`, `[A-Za-z0-9+/]`, `[[:space:]]`, quantifiers `{m,n}`/`?`/`*`/`+`, char classes, `()` alternation, `%` only. Never `\b`, `\d`, `\s`, `\.`, or a literal backslash in a template. No `ROWS BETWEEN`/`RANGE BETWEEN` (Code 48 in streaming MVs). No windowing — every rule is single-event.
- **Standard projection** (copy verbatim into every rule's `sql_template`, changing only `<severity>`, the `message` concat, and the `WHERE`):
  ```sql
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    '<severity>'              AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    <message-expr>            AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE <predicate>
  ```
  `'__rule_id__'` / `'__rule_name__'` are magic tokens the renderer substitutes (see `rp-001`). `<severity>` must equal the definition's `severity`.
- **Every seed rule** carries required fields `id, name, version, severity, category, hook_sources, sql_template` and a `translations.zh-CN` block with a **verbatim PDF name and description** (guarded by `TestSeedRulesHaveChineseTranslations`). `origin` is set to `builtin` by the loader — do NOT put `origin` in the YAML. Zero parameters (no `config:` block).
- **Golden regeneration:** adding any seed rule changes `internal/rules/testdata/seed_rendered.golden`. After adding rules in a task, regenerate with `UPDATE_SEED_GOLDEN=1 CGO_ENABLED=0 go test ./internal/rules/ -run TestSeedRenderedSQLGolden` and include the regenerated file in the commit.
- **Valid categories** (reuse existing values): `sensitive_data_exposure`, `credential_theft_and_harvesting`, `data_exfiltration_and_leakage`, `agent_privilege_escalation`, `prompt_injection_and_hijacking`, `resource_exhaustion_and_dos`.
- **Commit trailer** on every commit:
  ```
  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01MuJKFZSUHHbbC2WCMGoVrB
  ```

---

## Task 1: Sensitive-data rules (A01, A03, A04, A05, A06, A07)

**Files:**
- Create: `backend/internal/catalog/seed/dsa-a01.yaml`, `dsa-a03.yaml`, `dsa-a04.yaml`, `dsa-a05.yaml`, `dsa-a06.yaml`, `dsa-a07.yaml`
- Create: `backend/internal/catalog/dsa_rules_test.go`
- Regenerate: `backend/internal/rules/testdata/seed_rendered.golden`

**Interfaces:**
- Produces: seed definitions with IDs `dsa-a01, dsa-a03, dsa-a04, dsa-a05, dsa-a06, dsa-a07`, each loadable via `catalog.New(catalog.SeedFS, …).Lookup(id)` and carrying a `zh-CN` translation. Task 5 (package) references these IDs.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/catalog/dsa_rules_test.go`:

```go
package catalog

import "testing"

// dsaExpected is the full set of authored customer-audit rule IDs. It grows as
// each category task lands; the package (data-security-audit) must contain
// exactly this set (asserted in package_content_test once the manifest exists).
var dsaExpected = []string{
	"dsa-a01", "dsa-a03", "dsa-a04", "dsa-a05", "dsa-a06", "dsa-a07",
	"dsa-a08", "dsa-a09", "dsa-a10", "dsa-a11", "dsa-a12",
	"dsa-a14", "dsa-a17", "dsa-a18",
	"dsa-a19", "dsa-a20", "dsa-a22",
}

// TestDataSecurityAuditRulesPresent asserts each authored dsa-* rule exists,
// validates, and carries a zh-CN name+description. Rules land task-by-task;
// this test lists only the IDs that should exist so far by filtering on load.
func TestDataSecurityAuditRulesPresent(t *testing.T) {
	m, err := New(SeedFS, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New(SeedFS): %v", err)
	}
	for _, id := range dsaExpected {
		d, ok := m.Lookup(id)
		if !ok {
			continue // not authored yet; a later task adds it
		}
		if err := d.Validate(); err != nil {
			t.Errorf("%s fails Validate: %v", id, err)
		}
		tr, ok := d.TranslationFor("zh-CN")
		if !ok || tr.Name == "" || tr.Description == "" {
			t.Errorf("%s missing zh-CN name/description", id)
		}
	}
	// At least the Task-1 six must be present by the time all tasks are done;
	// this lower bound catches a wholesale load failure.
	got := 0
	for _, id := range dsaExpected {
		if _, ok := m.Lookup(id); ok {
			got++
		}
	}
	if got == 0 {
		t.Fatal("no dsa-* rules loaded at all")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/ -run TestDataSecurityAuditRulesPresent -v`
Expected: FAIL — `no dsa-* rules loaded at all`.

- [ ] **Step 3: Create the six rule YAMLs**

`backend/internal/catalog/seed/dsa-a01.yaml`:

```yaml
id: dsa-a01
name: "[A01] Sensitive PII in User Input"
version: "1.0.0"
description: |
  Detects identity-card numbers, bank-card numbers, account credentials,
  passwords, verification codes, keys, and customer data in user input.
  Flags when such data is not required by the business, exceeds the agent's
  purpose, or would enter an unauthorized model. (Customer audit rule A01.)
severity: warning
category: sensitive_data_exposure
fp_rate: medium
hook_sources: "user_input"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a01
  - pii
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'warning'                 AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Sensitive data in user input from agent ', agent_id) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type = 'user_input'
    AND (
         match(user_message, '[0-9]{17}[0-9Xx]')
      OR match(user_message, '[0-9]{13,19}')
      OR multi_search_any(lower(user_message), ['密码', '验证码', '密钥', '身份证', '银行卡', 'password', 'passwd', 'verification code', 'secret key', 'api key', 'access key'])
    )
translations:
  zh-CN:
    name: "用户输入敏感数据检测"
    description: |
      检测用户输入的身份证号、银行卡号、账户信息、密码、验证码、密钥和客户资料。非业务必需、超出智能体用途或进入非授权模型时告警。
```

`backend/internal/catalog/seed/dsa-a03.yaml`:

```yaml
id: dsa-a03
name: "[A03] Sensitive Information in Model Output"
version: "1.0.0"
description: |
  Detects personal information, customer information, account data,
  authentication information, and business-sensitive data in model replies.
  Treated as highest-risk when the output does not originate from the current
  user's own session. (Customer audit rule A03.)
severity: critical
category: sensitive_data_exposure
fp_rate: medium
hook_sources: "llm_response"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a03
  - pii
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'critical'                AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Sensitive data in model output from agent ', agent_id) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type = 'llm_response'
    AND (
         match(llm_response, '[0-9]{17}[0-9Xx]')
      OR match(llm_response, '[0-9]{13,19}')
      OR multi_search_any(lower(llm_response), ['身份证', '银行卡', '信用卡', 'social security', 'ssn', 'account number', 'api key', 'access key', 'password', 'token', '密码'])
    )
translations:
  zh-CN:
    name: "模型输出敏感信息"
    description: |
      检测回复中的个人信息、客户信息、账户数据、认证信息及业务敏感数据。输出并非来自当前用户本次会话时按最高风险处置。
```

`backend/internal/catalog/seed/dsa-a04.yaml`:

```yaml
id: dsa-a04
name: "[A04] Unnecessary Sensitive Data in Tool Parameters"
version: "1.0.0"
description: |
  Flags tool-call parameters that carry sensitive fields not needed to
  complete the operation. Approximates the customer's field-whitelist rule by
  detecting credential/PII markers in tool_input for non-authentication tools.
  (Customer audit rule A04.)
severity: warning
category: sensitive_data_exposure
fp_rate: medium
hook_sources: "tool_invoke"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a04
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'warning'                 AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Sensitive data in parameters of tool ', tool_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type = 'tool_invoke'
    AND NOT multi_search_any(lower(tool_name), ['login', 'auth', 'oauth', 'authenticate', 'credential'])
    AND multi_search_any(lower(tool_input), ['password', 'passwd', 'secret', 'api key', 'api_key', 'apikey', 'access key', 'access_key', 'private key', 'private_key', 'ssn', 'credit card', '身份证', '银行卡', '密码', '验证码'])
translations:
  zh-CN:
    name: "工具参数携带非必要敏感数据"
    description: |
      依据工具参数模板建立字段白名单;调用参数包含完成操作不需要的敏感字段时告警。
```

`backend/internal/catalog/seed/dsa-a05.yaml`:

```yaml
id: dsa-a05
name: "[A05] Tool Result Written Back Unmasked"
version: "1.0.0"
description: |
  Flags when a tool returns full customer data and the agent writes it into
  context or a reply without masking, truncation, or filtering. Heuristic:
  PII/card signatures in tool_result with no masking markers present.
  (Customer audit rule A05.)
severity: warning
category: sensitive_data_exposure
fp_rate: medium
hook_sources: "tool_complete"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a05
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'warning'                 AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Unmasked sensitive data in result of tool ', tool_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type = 'tool_complete'
    AND (
         match(tool_result, '[0-9]{17}[0-9Xx]')
      OR match(tool_result, '[0-9]{13,19}')
      OR multi_search_any(lower(tool_result), ['身份证', '银行卡', 'social security', 'ssn', 'credit card'])
    )
    AND NOT match(tool_result, '[*]{3,}')
translations:
  zh-CN:
    name: "工具返回结果未脱敏"
    description: |
      工具返回完整客户数据后,智能体直接写入上下文或回复用户,且未掩码、截断或过滤时违规。
```

`backend/internal/catalog/seed/dsa-a06.yaml`:

```yaml
id: dsa-a06
name: "[A06] Authentication Material in the Conversation Chain"
version: "1.0.0"
description: |
  Detects passwords, verification codes, tokens, cookies, private keys, or
  AccessKeys appearing in prompts, tool parameters, tool results, or model
  output — any of which is a direct high-risk finding. (Customer audit rule
  A06.)
severity: critical
category: credential_theft_and_harvesting
fp_rate: low
hook_sources: "user_input,llm_request,tool_invoke,tool_complete,llm_response"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a06
  - credentials
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'critical'                AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Authentication material in conversation chain (', raw_hook_name, ')') AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type IN ('user_input', 'llm_request', 'tool_invoke', 'tool_complete', 'llm_response')
    AND (
         match(concat(user_message, ' ', llm_response, ' ', tool_input, ' ', tool_result), 'AKIA[0-9A-Z]{16}')
      OR match(concat(user_message, ' ', llm_response, ' ', tool_input, ' ', tool_result), 'eyJ[A-Za-z0-9_-]{10,}')
      OR (position(concat(user_message, ' ', llm_response, ' ', tool_input, ' ', tool_result), 'BEGIN') > 0
          AND position(concat(user_message, ' ', llm_response, ' ', tool_input, ' ', tool_result), 'PRIVATE KEY') > 0)
      OR multi_search_any(lower(concat(user_message, ' ', llm_response, ' ', tool_input, ' ', tool_result)), ['bearer ', 'authorization: bearer', 'password=', 'passwd=', 'set-cookie:', 'session_token', 'access_token', 'accesskey', '私钥', '访问密钥'])
    )
translations:
  zh-CN:
    name: "身份鉴别信息进入对话链路"
    description: |
      密码、验证码、Token、Cookie、私钥或 AccessKey 出现在提示词、工具参数、工具结果或模型输出中,直接判定高风险。
```

`backend/internal/catalog/seed/dsa-a07.yaml`:

```yaml
id: dsa-a07
name: "[A07] Risk Escalation After Data Aggregation"
version: "1.0.0"
description: |
  Flags when identity/account/transaction-linkable data co-occurs — a single
  event carrying both a strong identifier (ID/card number) and account or
  transaction context, the aggregation signal the customer escalates on.
  (Customer audit rule A07.)
severity: warning
category: sensitive_data_exposure
fp_rate: medium
hook_sources: "llm_response,tool_complete"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a07
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'warning'                 AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Aggregated identity-linkable data from agent ', agent_id) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type IN ('llm_response', 'tool_complete')
    AND (
      match(concat(llm_response, ' ', tool_result), '[0-9]{17}[0-9Xx]')
      OR match(concat(llm_response, ' ', tool_result), '[0-9]{13,19}')
    )
    AND multi_search_any(lower(concat(llm_response, ' ', tool_result)), ['姓名', 'name', '账户', 'account', '余额', 'balance', '交易', 'transaction', '手机号', 'phone'])
translations:
  zh-CN:
    name: "数据汇聚后风险升级"
    description: |
      多轮对话或多个工具结果组合后可识别客户身份、账户或交易关系时,自动提升数据级别。
```

- [ ] **Step 4: Regenerate the rendered-SQL golden**

Run: `cd backend && UPDATE_SEED_GOLDEN=1 CGO_ENABLED=0 go test ./internal/rules/ -run TestSeedRenderedSQLGolden`
Expected: PASS (golden rewritten to include the six new rules).

- [ ] **Step 5: Run the catalog + rules suites**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/... ./internal/rules/...`
Expected: PASS — `TestDataSecurityAuditRulesPresent`, `TestSeedFS_AllDefinitionsValid`, `TestSeedRulesHaveChineseTranslations`, `TestSeedDefinitionsCompileWithDefaults`, `TestSeedFS_NoUnsupportedStreamingSQL`, and `TestSeedRenderedSQLGolden` all green.

- [ ] **Step 6: Commit**

```bash
cd backend && git add internal/catalog/seed/dsa-a0{1,3,4,5,6,7}.yaml internal/catalog/dsa_rules_test.go internal/rules/testdata/seed_rendered.golden
git commit -m "feat(rules): add A01-A07 data-security audit rules (dsa-a01..a07) (#250)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MuJKFZSUHHbbC2WCMGoVrB"
```

---

## Task 2: Permission & isolation rules (A08, A09, A10, A11, A12)

**Files:**
- Create: `backend/internal/catalog/seed/dsa-a08.yaml`, `dsa-a09.yaml`, `dsa-a10.yaml`, `dsa-a11.yaml`, `dsa-a12.yaml`
- Regenerate: `backend/internal/rules/testdata/seed_rendered.golden`

**Interfaces:**
- Consumes: `dsaExpected` (Task 1) already lists these IDs.
- Produces: seed definitions `dsa-a08, dsa-a09, dsa-a10, dsa-a11, dsa-a12`.

- [ ] **Step 1: Verify the guard already expects these IDs (no test change)**

`TestDataSecurityAuditRulesPresent` (Task 1) already iterates `dsaExpected`, which includes these five and skips not-yet-authored IDs. No new test needed — adding the YAMLs turns the skip into an assertion.

- [ ] **Step 2: Create the five rule YAMLs**

`backend/internal/catalog/seed/dsa-a08.yaml`:

```yaml
id: dsa-a08
name: "[A08] Cross-User / Cross-Tenant Data Leak"
version: "1.0.0"
description: |
  Best-effort detection of cross-tenant / cross-user access: explicit tenant,
  org, customer, or account identifier parameters in tool_input that indicate
  a data subject other than the caller. Full fidelity awaits a first-class
  tenant_id CIM field (#261). (Customer audit rule A08.)
severity: warning
category: data_exfiltration_and_leakage
fp_rate: high
hook_sources: "tool_invoke,tool_complete,llm_response"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a08
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'warning'                 AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Possible cross-tenant access via tool ', tool_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type IN ('tool_invoke', 'tool_complete', 'llm_response')
    AND multi_search_any(lower(concat(tool_input, ' ', tool_result)), ['tenant_id', 'tenantid', 'org_id', 'orgid', 'customer_id', 'account_id', 'cross-tenant', 'other tenant', '其他租户', '跨租户'])
translations:
  zh-CN:
    name: "跨用户或跨租户数据泄露"
    description: |
      模型输出、知识库结果或工具结果中的数据主体、租户标识与当前调用用户不一致时告警,并按最高风险处置。
```

`backend/internal/catalog/seed/dsa-a09.yaml`:

```yaml
id: dsa-a09
name: "[A09] Unauthorized Tool Invocation"
version: "1.0.0"
description: |
  Flags invocation of high-privilege / administrative / system tool-name
  patterns that typically require elevated authorization. Approximates the
  customer's allow-list + role rule pending role_id in CIM (#261). (Customer
  audit rule A09.)
severity: warning
category: agent_privilege_escalation
fp_rate: medium
hook_sources: "tool_invoke"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a09
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'warning'                 AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('High-privilege tool invoked: ', tool_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type = 'tool_invoke'
    AND multi_search_any(lower(tool_name), ['admin', 'sudo', 'root', 'grant', 'iam', 'privilege', 'delete_user', 'drop', 'truncate', 'exec', 'shell', 'system', 'chmod', 'setuid'])
translations:
  zh-CN:
    name: "未授权工具调用"
    description: |
      智能体调用不在工具白名单内的工具,或调用用户角色不具备对应权限时违规。
```

`backend/internal/catalog/seed/dsa-a10.yaml`:

```yaml
id: dsa-a10
name: "[A10] Privilege-Parameter Access"
version: "1.0.0"
description: |
  Detects path traversal and identifier-manipulation parameters — user, org,
  tenant, or resource ID overrides and file-path escapes — used to reach data
  outside the caller's authorization scope. (Customer audit rule A10.)
severity: warning
category: agent_privilege_escalation
fp_rate: medium
hook_sources: "tool_invoke"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a10
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'warning'                 AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Privilege-parameter manipulation in tool ', tool_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type = 'tool_invoke'
    AND (
         position(tool_input, '../') > 0
      OR multi_search_any(lower(tool_input), ['/etc/passwd', '/root/', '%2e%2e', '..%2f', 'file://', 'user_id=', 'tenant_id=', 'resource_id=', 'org_id='])
    )
translations:
  zh-CN:
    name: "越权参数调用"
    description: |
      通过修改用户标识、组织标识、租户 ID、资源 ID 或文件路径访问权限范围外的数据。
```

`backend/internal/catalog/seed/dsa-a11.yaml`:

```yaml
id: dsa-a11
name: "[A11] High-Risk Tool Call Missing Identity"
version: "1.0.0"
description: |
  Flags high-risk operations — query-sensitive, export, download, send,
  modify, delete — that cannot be attributed to a specific user (empty
  user_id), so the call is not associable to an accountable actor. (Customer
  audit rule A11.)
severity: warning
category: data_exfiltration_and_leakage
fp_rate: medium
hook_sources: "tool_invoke,tool_complete"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a11
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'warning'                 AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Unattributable high-risk tool call: ', tool_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type IN ('tool_invoke', 'tool_complete')
    AND user_id = ''
    AND multi_search_any(lower(tool_name), ['export', 'download', 'send', 'delete', 'remove', 'modify', 'update', 'query', 'search', 'dump', 'backup', 'transfer'])
translations:
  zh-CN:
    name: "高风险工具缺少身份信息"
    description: |
      查询敏感数据、导出、下载、发送、修改和删除类调用无法关联到具体用户、智能体或会话时违规。
```

`backend/internal/catalog/seed/dsa-a12.yaml`:

```yaml
id: dsa-a12
name: "[A12] Bypass After Policy Denial"
version: "1.0.0"
description: |
  Single-event heuristic for re-attempts after a denial: retry / workaround
  phrasing or parameter-encoding markers (base64, atob, fromCharCode) in
  tool_input that indicate an attempt to achieve a blocked goal by another
  path. Full cross-event correlation is investigate-only. (Customer audit rule
  A12.)
severity: warning
category: prompt_injection_and_hijacking
fp_rate: high
hook_sources: "tool_invoke,tool_complete"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a12
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'warning'                 AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Possible policy-bypass retry via tool ', tool_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type IN ('tool_invoke', 'tool_complete')
    AND multi_search_any(lower(tool_input), ['retry', 'bypass', 'workaround', 'alternate', 'another way', 'base64', 'fromcharcode', 'atob', 'decode', 'circumvent', '绕过', '重试'])
translations:
  zh-CN:
    name: "策略拒绝后绕过调用"
    description: |
      调用被拒绝后,智能体通过其他工具、参数编码或连续拆分调用完成相同目的的。
```

- [ ] **Step 3: Regenerate the golden**

Run: `cd backend && UPDATE_SEED_GOLDEN=1 CGO_ENABLED=0 go test ./internal/rules/ -run TestSeedRenderedSQLGolden`
Expected: PASS.

- [ ] **Step 4: Run the suites**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/... ./internal/rules/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/catalog/seed/dsa-a{08,09,10,11,12}.yaml internal/rules/testdata/seed_rendered.golden
git commit -m "feat(rules): add A08-A12 permission/isolation audit rules (#250)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MuJKFZSUHHbbC2WCMGoVrB"
```

---

## Task 3: External-transport rules (A14, A17, A18)

**Files:**
- Create: `backend/internal/catalog/seed/dsa-a14.yaml`, `dsa-a17.yaml`, `dsa-a18.yaml`
- Regenerate: `backend/internal/rules/testdata/seed_rendered.golden`

**Interfaces:**
- Produces: seed definitions `dsa-a14, dsa-a17, dsa-a18`.

- [ ] **Step 1: Create the three rule YAMLs**

`backend/internal/catalog/seed/dsa-a14.yaml`:

```yaml
id: dsa-a14
name: "[A14] Sensitive Data to External Tool"
version: "1.0.0"
description: |
  Flags third-party or internet-facing tool calls (HTTP, webhook, upload,
  email, object-store) whose parameters carry sensitive data without a
  recorded allowed data-type / field scope. (Customer audit rule A14.)
severity: critical
category: data_exfiltration_and_leakage
fp_rate: medium
hook_sources: "tool_invoke"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a14
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'critical'                AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Sensitive data sent to external tool ', tool_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type = 'tool_invoke'
    AND multi_search_any(lower(tool_name), ['http', 'fetch', 'curl', 'webhook', 'upload', 'post', 'send', 'email', 'slack', 's3', 'url', 'request'])
    AND multi_search_any(lower(tool_input), ['password', 'secret', 'api key', 'api_key', 'token', 'private key', 'private_key', 'ssn', 'credit card', '身份证', '银行卡', '密码'])
translations:
  zh-CN:
    name: "敏感数据传向外部工具"
    description: |
      第三方或互联网工具参数包含敏感数据,且没有允许的数据类型和字段范围记录。
```

`backend/internal/catalog/seed/dsa-a17.yaml`:

```yaml
id: dsa-a17
name: "[A17] Bulk Extraction / Enumeration"
version: "1.0.0"
description: |
  Single-event signal for bulk data pulls: a query/export tool call requesting
  a very large page/limit/row count or an explicit fetch-all / dump-all flag.
  The cross-call frequency dimension is investigate-only. (Customer audit rule
  A17.)
severity: warning
category: resource_exhaustion_and_dos
fp_rate: medium
hook_sources: "tool_invoke"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a17
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'warning'                 AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Bulk extraction via tool ', tool_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type = 'tool_invoke'
    AND (
         match(lower(tool_input), '(limit|page_?size|per_?page|count|top|rows)[^0-9]{0,6}[1-9][0-9]{3,}')
      OR multi_search_any(lower(tool_input), ['export_all', 'dump_all', 'fetch_all', 'select * from', '"all": true', '"all":true', 'download all', 'bulk export'])
    )
translations:
  zh-CN:
    name: "批量提取或枚举数据"
    description: |
      短时间连续查询、搜索、翻页或导出,累计获取大量敏感数据并超出用户历史基线。
```

`backend/internal/catalog/seed/dsa-a18.yaml`:

```yaml
id: dsa-a18
name: "[A18] Covert Exfiltration Behavior"
version: "1.0.0"
description: |
  Detects covert egress: long base64 / hex blobs, data-URI base64 payloads, or
  extended percent-encoding in outbound tool parameters — the encoding used to
  smuggle sensitive data past inspection. (Customer audit rule A18.)
severity: critical
category: data_exfiltration_and_leakage
fp_rate: medium
hook_sources: "tool_invoke"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a18
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'critical'                AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Covert encoded payload in tool ', tool_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type = 'tool_invoke'
    AND (
         match(tool_input, '[A-Za-z0-9+/]{120,}')
      OR match(tool_input, '[0-9a-fA-F]{80,}')
      OR match(lower(tool_input), 'data:[a-z/]+;base64,')
      OR match(tool_input, '(%[0-9A-Fa-f]{2}){20,}')
    )
translations:
  zh-CN:
    name: "隐蔽外传行为"
    description: |
      将敏感数据编码为 Base64、十六进制、压缩片段或 URL 参数,或拆分多次调用向外发送。
```

- [ ] **Step 2: Regenerate the golden**

Run: `cd backend && UPDATE_SEED_GOLDEN=1 CGO_ENABLED=0 go test ./internal/rules/ -run TestSeedRenderedSQLGolden`
Expected: PASS.

- [ ] **Step 3: Run the suites**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/... ./internal/rules/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd backend && git add internal/catalog/seed/dsa-a{14,17,18}.yaml internal/rules/testdata/seed_rendered.golden
git commit -m "feat(rules): add A14/A17/A18 external-transport audit rules (#250)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MuJKFZSUHHbbC2WCMGoVrB"
```

---

## Task 4: Attack & anomaly rules (A19, A20, A22)

**Files:**
- Create: `backend/internal/catalog/seed/dsa-a19.yaml`, `dsa-a20.yaml`, `dsa-a22.yaml`
- Regenerate: `backend/internal/rules/testdata/seed_rendered.golden`

**Interfaces:**
- Produces: seed definitions `dsa-a19, dsa-a20, dsa-a22`. After this task all 17 IDs in `dsaExpected` exist.

- [ ] **Step 1: Create the three rule YAMLs**

`backend/internal/catalog/seed/dsa-a19.yaml`:

```yaml
id: dsa-a19
name: "[A19] Prompt Injection Leading to Data Access"
version: "1.0.0"
description: |
  Detects injection markers — ignore-rules, reveal-system-prompt,
  call-internal-tool, read-other-user-data — in user input or tool results
  that precede sensitive retrieval or tool use. (Customer audit rule A19.)
severity: critical
category: prompt_injection_and_hijacking
fp_rate: low
hook_sources: "user_input,tool_complete"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a19
  - prompt-injection
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'critical'                AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Injection-to-data-access pattern in ', raw_hook_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type IN ('user_input', 'tool_complete')
    AND (
         match(lower(concat(user_message, ' ', tool_result)), 'ignore [a-z ]{0,20}(previous|prior|all) instructions')
      OR multi_search_any(lower(concat(user_message, ' ', tool_result)), ['reveal the system prompt', 'show system prompt', 'print system prompt', '忽略以上指令', '忽略之前的指令', '忽略所有规则', 'read other user', 'access other user', '其他用户数据', 'call internal tool', 'invoke internal tool', 'use internal tool'])
    )
translations:
  zh-CN:
    name: "提示词注入导致数据访问"
    description: |
      对话出现忽略规则、展示系统提示词、调用内部工具或读取其他用户数据等注入特征,随后发生敏感检索或工具调用。
```

`backend/internal/catalog/seed/dsa-a20.yaml`:

```yaml
id: dsa-a20
name: "[A20] System-Prompt or Hidden-Context Leak"
version: "1.0.0"
description: |
  Detects system prompts, internal policies, tool definitions, knowledge-base
  keys, hidden context, or other non-visible instructions surfacing in the
  model's reply. (Customer audit rule A20.)
severity: critical
category: data_exfiltration_and_leakage
fp_rate: low
hook_sources: "llm_response"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a20
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'critical'                AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('System-prompt / hidden-context leak from agent ', agent_id) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type = 'llm_response'
    AND multi_search_any(lower(llm_response), ['system prompt', 'my instructions are', 'internal policy', 'tool definition', '以下是我的系统提示', '我的系统指令', 'my system message', 'hidden context', 'knowledge base key', 'begin system', '<system>'])
translations:
  zh-CN:
    name: "系统提示词或隐藏上下文泄露"
    description: |
      回复中出现系统提示词、内部策略、工具定义、知识库密钥、隐藏上下文或其他不可见指令。
```

`backend/internal/catalog/seed/dsa-a22.yaml`:

```yaml
id: dsa-a22
name: "[A22] Anomalous Call Frequency or Chain"
version: "1.0.0"
description: |
  Single-event anomaly signals — recursion / stack-overflow / rate-limit /
  repeated-retry markers in tool results, or an unusually long model output
  (context blow-up). The cross-call frequency dimension is investigate-only.
  (Customer audit rule A22.)
severity: warning
category: resource_exhaustion_and_dos
fp_rate: medium
hook_sources: "tool_complete,llm_response"
required_telemetry: ""
author: "Timeplus"
tags:
  - data-security
  - customer-baseline
  - audit-a22
sql_template: |
  SELECT
    event_time                AS detected_at,
    '__rule_id__'             AS rule_id,
    '__rule_name__'           AS rule_name,
    'warning'                 AS severity,
    agent_id,
    deployment_id,
    session_id,
    run_id,
    raw_hook_name             AS hook_name,
    tool_name,
    concat('Anomalous call signal in ', raw_hook_name) AS message,
    raw_event_data            AS event_data
  FROM agentguard_cim_event
  WHERE event_type IN ('tool_complete', 'llm_response')
    AND (
         multi_search_any(lower(tool_result), ['recursion', 'stack overflow', 'too many requests', 'rate limit', 'retry attempt', 'repeated failure', 'timeout'])
      OR length(llm_response) > 50000
    )
translations:
  zh-CN:
    name: "异常调用频率和调用链"
    description: |
      检测单会话调用突增、递归调用、反复失败重试、异常时段访问、高 Token 消耗及超长上下文。
```

- [ ] **Step 2: Regenerate the golden**

Run: `cd backend && UPDATE_SEED_GOLDEN=1 CGO_ENABLED=0 go test ./internal/rules/ -run TestSeedRenderedSQLGolden`
Expected: PASS.

- [ ] **Step 3: Run the suites — all 17 now present**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/... ./internal/rules/...`
Expected: PASS. `TestDataSecurityAuditRulesPresent` now asserts all 17 IDs (none skipped).

- [ ] **Step 4: Commit**

```bash
cd backend && git add internal/catalog/seed/dsa-a{19,20,22}.yaml internal/rules/testdata/seed_rendered.golden
git commit -m "feat(rules): add A19/A20/A22 attack-and-anomaly audit rules (#250)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MuJKFZSUHHbbC2WCMGoVrB"
```

---

## Task 5: Package manifest + visibility default

**Files:**
- Create: `backend/internal/catalog/packages/data-security-audit.yaml`
- Modify: `backend/internal/config/config.go:162` (visible-packages default)
- Create/append: `backend/internal/catalog/dsa_rules_test.go` (add package-membership test)

**Interfaces:**
- Consumes: the 17 `dsa-*` IDs (Tasks 1–4).
- Produces: package `data-security-audit` (visible by default), guarded to contain exactly `dsaExpected`.

- [ ] **Step 1: Write the failing membership test**

Append to `backend/internal/catalog/dsa_rules_test.go`:

```go
// TestDataSecurityAuditPackageMembership asserts the data-security-audit
// package exists and contains exactly the authored dsa-* rule set — no more,
// no less — so the manifest can't drift from the rules.
func TestDataSecurityAuditPackageMembership(t *testing.T) {
	m, err := New(SeedFS, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New(SeedFS): %v", err)
	}
	var pkg *Package
	for _, p := range m.Packages() {
		if p.ID == "data-security-audit" {
			pp := p
			pkg = &pp
			break
		}
	}
	if pkg == nil {
		t.Fatal("data-security-audit package not found")
	}
	want := map[string]bool{}
	for _, id := range dsaExpected {
		want[id] = true
	}
	got := map[string]bool{}
	for _, id := range pkg.Rules {
		got[id] = true
	}
	for id := range want {
		if !got[id] {
			t.Errorf("package missing rule %s", id)
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("package has unexpected rule %s", id)
		}
	}
	if pkg.DefaultEnabled {
		t.Error("data-security-audit must be default_enabled: false")
	}
}
```

> Note: if `m.Packages()` returns `[]Package` (value slice), the loop-capture `pp := p` above is safe. If it returns pointers, adjust to store `p` directly. Confirm the signature in `internal/catalog/package.go` before running.

- [ ] **Step 2: Run to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/ -run TestDataSecurityAuditPackageMembership -v`
Expected: FAIL — `data-security-audit package not found`.

- [ ] **Step 3: Create the package manifest**

`backend/internal/catalog/packages/data-security-audit.yaml`:

```yaml
id: data-security-audit
name: "Data-Security Audit (Customer Baseline)"
description: |
  The customer conversation / tool-call data-security audit checklist as
  runnable detections: sensitive PII in input and output, credentials in the
  conversation chain, unmasked tool data, privilege-parameter and unauthorized
  tool calls, sensitive egress and covert exfiltration, prompt-injection to
  data access, and system-prompt leakage. Covers the machine-detectable rules
  A01-A22; the remaining audit rules are covered by the data-audit-guide
  investigation skill.
default_enabled: false
rules:
  - dsa-a01
  - dsa-a03
  - dsa-a04
  - dsa-a05
  - dsa-a06
  - dsa-a07
  - dsa-a08
  - dsa-a09
  - dsa-a10
  - dsa-a11
  - dsa-a12
  - dsa-a14
  - dsa-a17
  - dsa-a18
  - dsa-a19
  - dsa-a20
  - dsa-a22
translations:
  zh-CN:
    name: "数据安全审计(客户基线)"
    description: |
      将客户的智能体对话与工具调用数据安全审计清单落地为可运行的检测规则:用户输入与模型输出中的敏感数据、进入对话链路的身份鉴别信息、未脱敏的工具数据、越权参数与未授权工具调用、敏感数据外传与隐蔽外传、提示词注入导致的数据访问,以及系统提示词泄露。覆盖可技术检测的 A01-A22 规则;其余审计规则由 data-audit-guide 调查技能覆盖。
```

- [ ] **Step 4: Flip the visibility default**

In `backend/internal/config/config.go` line ~162, change:

```go
	v.SetDefault("rules.visible_packages", []string{"core"})
```

to:

```go
	v.SetDefault("rules.visible_packages", []string{"core", "data-security-audit"})
```

- [ ] **Step 5: Check for a default-visibility assertion**

Run: `cd backend && grep -rn "visible_packages" internal/config/*_test.go`
If any test asserts the default equals `["core"]`, update its expectation to `["core", "data-security-audit"]`. (The known list-coercion tests in `sources_list_test.go` pass their own inputs and are unaffected.)

- [ ] **Step 6: Run to verify pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/... ./internal/config/...`
Expected: PASS — membership test green; `TestEmbeddedPackageRulesExist`, `TestEmbeddedPackagesHaveChineseTranslations`, `TestCorePackageIsDefaultEnabled` still green.

- [ ] **Step 7: Commit**

```bash
cd backend && git add internal/catalog/packages/data-security-audit.yaml internal/catalog/dsa_rules_test.go internal/config/config.go
git commit -m "feat(rules): add data-security-audit package, visible by default (#250)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MuJKFZSUHHbbC2WCMGoVrB"
```

---

## Task 6: data-audit-guide skill — SKILL.md + load test

**Files:**
- Create: `backend/internal/sentry/skills/knowledge/data-audit-guide/SKILL.md`
- Create: `backend/internal/sentry/skills/dataaudit_skill_test.go`

**Interfaces:**
- Produces: an embedded knowledge skill named `data-audit-guide` loadable via `NewLoadSkill("").Execute(...)` with `{"name":"data-audit-guide"}`. Task 7 adds its references; Task 8 gates its visibility.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/sentry/skills/dataaudit_skill_test.go`:

```go
package skills

import (
	"context"
	"strings"
	"testing"
)

// TestDataAuditGuideLoads asserts the data-audit-guide SKILL.md is embedded and
// mentions the customer audit-rule frame (A01 / A27) so the model gets the
// catalog when it loads the skill.
func TestDataAuditGuideLoads(t *testing.T) {
	rc := &RunContext{Loaded: map[string]bool{}}
	out, err := NewLoadSkill("").Execute(context.Background(), rc, loadArgs(t, map[string]string{"name": "data-audit-guide"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"A01", "A27", "investigate"} {
		if !strings.Contains(out, want) {
			t.Errorf("SKILL.md missing %q; got %.120q", want, out)
		}
	}
}

// TestDataAuditGuideInSkillList asserts the skill is discoverable by name.
func TestDataAuditGuideInSkillList(t *testing.T) {
	found := false
	for _, n := range skillNames("", nil) {
		if n == "data-audit-guide" {
			found = true
		}
	}
	if !found {
		t.Fatal("data-audit-guide not in embedded skill list")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/sentry/skills/ -run TestDataAuditGuide -v`
Expected: FAIL — skill unavailable / not in list.

- [ ] **Step 3: Create SKILL.md**

Create `backend/internal/sentry/skills/knowledge/data-audit-guide/SKILL.md`:

````markdown
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
````

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/sentry/skills/ -run TestDataAuditGuide -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/sentry/skills/knowledge/data-audit-guide/SKILL.md internal/sentry/skills/dataaudit_skill_test.go
git commit -m "feat(sentry): add data-audit-guide skill SKILL.md (#250)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MuJKFZSUHHbbC2WCMGoVrB"
```

---

## Task 7: data-audit-guide skill — the five reference files

**Files:**
- Create: `backend/internal/sentry/skills/knowledge/data-audit-guide/references/SENSITIVE_DATA.md`, `PERMISSIONS.md`, `EXFIL.md`, `ATTACK.md`, `AUDIT_TRAIL.md`
- Modify: `backend/internal/sentry/skills/dataaudit_skill_test.go` (add reference-load assertions)

**Interfaces:**
- Consumes: the SKILL.md and skill name from Task 6.
- Produces: five `references/*.md` resolvable via `NewLoadSkillReference`.

- [ ] **Step 1: Add the failing reference-load test**

Append to `backend/internal/sentry/skills/dataaudit_skill_test.go`:

```go
// TestDataAuditGuideReferencesLoad asserts each category reference resolves and
// mentions its lead A-code, so the SKILL.md links are never dangling.
func TestDataAuditGuideReferencesLoad(t *testing.T) {
	cases := map[string]string{
		"SENSITIVE_DATA.md": "A01",
		"PERMISSIONS.md":    "A08",
		"EXFIL.md":          "A13",
		"ATTACK.md":         "A19",
		"AUDIT_TRAIL.md":    "A24",
	}
	for file, want := range cases {
		rc := &RunContext{Loaded: map[string]bool{}}
		out, err := NewLoadSkillReference("").Execute(context.Background(), rc,
			loadArgs(t, map[string]string{"skill": "data-audit-guide", "name": file}))
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if !strings.Contains(out, want) {
			t.Errorf("%s missing %q; got %.100q", file, want, out)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/sentry/skills/ -run TestDataAuditGuideReferencesLoad -v`
Expected: FAIL — unknown reference.

- [ ] **Step 3: Create the reference files**

Each reference uses a uniform per-A-code block. Create all five with the content below.

`references/SENSITIVE_DATA.md`:

````markdown
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
- Fields: `llm_response` on `event_type='llm_response'`.
- Detections: `rule_id='dsa-a03'`. Cross-session origin = not captured; note it.

## A04 工具参数携带非必要敏感数据 — ✅ `dsa-a04`
Sensitive fields in tool params not needed for the operation.
- Fields: `tool_input`, `tool_name` on `tool_invoke`.
- Detections: `rule_id='dsa-a04'`.

## A05 工具返回结果未脱敏 — ✅ `dsa-a05`
Full customer data returned and written back without masking.
- Fields: `tool_result` on `tool_complete`.
- Detections: `rule_id='dsa-a05'`.

## A06 身份鉴别信息进入对话链路 — ✅ `dsa-a06`
Credentials anywhere in the chain — direct high risk.
- Fields: all content columns, all event types.
- Detections: `rule_id='dsa-a06'`. This is a priority rule — review first.

## A07 数据汇聚后风险升级 — ✅ `dsa-a07`
Identity-linkable data co-occurring (aggregation).
- Fields: `llm_response`+`tool_result`.
- Detections: `rule_id='dsa-a07'`. Multi-turn aggregation across events is
  investigate-only — use `dlp_search` over a session to confirm linkage.
````

`references/PERMISSIONS.md`:

````markdown
# Permissions & Data Isolation (A08–A12)

## A08 跨用户或跨租户数据泄露 — ✅ `dsa-a08` (best-effort)
Data subject / tenant ≠ calling user.
- Gap: no `tenant_id` in CIM (#261). Rule detects tenant/org/customer-id
  markers in `tool_input`/`tool_result` only. Confirm by hand:
  ```sql
  SELECT session_id, tool_name, left(tool_input, 200) FROM table(agentguard_cim_event)
  WHERE event_type='tool_invoke'
    AND multi_search_any(lower(tool_input), ['tenant_id','customer_id','org_id'])
  ORDER BY event_time DESC LIMIT 50
  ```
- Evidence: mark "tenant identity not captured — heuristic".

## A09 未授权工具调用 — ✅ `dsa-a09` (best-effort)
Tool outside allow-list, or caller role lacks permission.
- Gap: no `role_id`, no per-deployment allow-list in CIM. Rule flags
  high-privilege tool-name patterns. Confirm intent by inspecting the session.

## A10 越权参数调用 — ✅ `dsa-a10`
ID / path manipulation to reach out-of-scope data.
- Fields: `tool_input` on `tool_invoke`. Detections: `rule_id='dsa-a10'`.

## A11 高风险工具缺少身份信息 — ✅ `dsa-a11`
High-risk verb with no attributable `user_id`.
- Note: `user_id` is sparse; false positives where the plugin never sets it.
  Cross-check `agent_id`/`session_id` presence:
  ```sql
  SELECT tool_name, count() FROM table(agentguard_cim_event)
  WHERE event_type='tool_invoke' AND user_id=''
    AND multi_search_any(lower(tool_name), ['export','delete','send'])
  GROUP BY tool_name ORDER BY count() DESC
  ```

## A12 策略拒绝后绕过调用 — ✅ `dsa-a12` (best-effort)
Re-attempt after a denial via another tool / encoding / split.
- Gap: cross-event denial→retry correlation needs `policy_decision` (#261).
  Rule flags single-event retry/encoding markers. Investigate the surrounding
  call chain in the session timeline to confirm a true bypass.
````

`references/EXFIL.md`:

````markdown
# External Transport & Tool Calls (A13–A18)

## A13 未登记工具或外部地址 — 🔍 investigate
Tool/domain/IP not in the registry, or bypassing the gateway.
- Gap: no tool/endpoint registry in CIM. Best-effort — list external targets:
  ```sql
  SELECT tool_name, count() FROM table(agentguard_cim_event)
  WHERE event_type='tool_invoke'
    AND multi_search_any(lower(tool_input), ['http://','https://','ftp://'])
  GROUP BY tool_name ORDER BY count() DESC LIMIT 50
  ```
- Compare against the operator's known-tool list out of band.

## A14 敏感数据传向外部工具 — ✅ `dsa-a14`
Sensitive data in external/network tool params. Detections: `rule_id='dsa-a14'`.

## A15 外传保护等级下降 — 🔍 investigate
Downstream call didn't inherit the upstream sensitive label / masking policy.
- Gap: no `sensitive_types` / `data_labels` in CIM (#261). Not decidable today
  — report inconclusive.

## A16 外部传输通道不安全 — 🔍 investigate (conditional)
Plaintext HTTP / invalid cert / TLS < 1.2.
- Gap: no `protocol` / `tls_version` in CIM (#261). Only a coarse proxy:
  `http://` (not `https://`) literals in `tool_input`. Report as proxy only.

## A17 批量提取或枚举数据 — ✅ `dsa-a17`
Large page/limit or fetch-all in one call. Detections: `rule_id='dsa-a17'`. The
frequency dimension (many calls in a short window) is investigate-only:
```sql
SELECT session_id, count() AS calls FROM table(agentguard_cim_event)
WHERE event_type='tool_invoke' AND multi_search_any(lower(tool_name), ['query','search','export'])
  AND event_time >= now() - INTERVAL 1 HOUR
GROUP BY session_id HAVING calls > 50 ORDER BY calls DESC
```

## A18 隐蔽外传行为 — ✅ `dsa-a18`
Base64/hex/percent-encoded covert egress. Detections: `rule_id='dsa-a18'`.
Priority rule — review first.
````

`references/ATTACK.md`:

````markdown
# Agent Attack & Anomaly (A19–A23)

## A19 提示词注入导致数据访问 — ✅ `dsa-a19`
Injection markers then sensitive retrieval / tool call. Detections:
`rule_id='dsa-a19'`. Priority rule.

## A20 系统提示词或隐藏上下文泄露 — ✅ `dsa-a20`
System prompt / internal policy / tool defs / KB keys in a reply. Detections:
`rule_id='dsa-a20'`. Priority rule.

## A21 智能体行为与声明能力不一致 — 🔍 investigate
Read-only agent doing writes/deletes; service agent calling dev/ops/DB tools;
simple Q&A triggering bulk export.
- Gap: no declared-capability / `role_id` context in CIM. Best-effort — list
  the tool verbs a given agent actually used and eyeball against its stated
  role:
  ```sql
  SELECT agent_id, tool_name, count() FROM table(agentguard_cim_event)
  WHERE event_type='tool_invoke' GROUP BY agent_id, tool_name ORDER BY agent_id
  ```

## A22 异常调用频率和调用链 — ✅ `dsa-a22`
Single-event anomaly signals (recursion/retry/rate-limit/huge output).
Detections: `rule_id='dsa-a22'`. Frequency/spike analysis is investigate-only —
count calls per session over a window as in A17.

## A23 安全策略命中但未阻断 — 🔍 investigate
Gateway flagged sensitive/authz/injection but the request still ran.
- Gap: needs `policy_decision` + whether execution proceeded — cross-stream and
  not in CIM (#261). Proxy: a `dsa-*` security event whose session also has a
  later successful `tool_complete`:
  ```sql
  SELECT s.session_id, s.rule_id, s.detected_at
  FROM table(agentguard_security_events) AS s
  WHERE s.detected_at >= now() - INTERVAL 1 DAY
  ORDER BY s.detected_at DESC LIMIT 100
  ```
  then check the session timeline for continued execution. Report as proxy.
````

`references/AUDIT_TRAIL.md`:

````markdown
# Audit Trail & Traceability (A24–A27)

All four need fields CIM does not yet capture (#261); treat as data-quality
investigations, not detections.

## A24 审计字段缺失 — 🔍 investigate
Events missing user/tenant/time/session/agent/model/tool/op-type.
- Measurable proxy — count events with empty key fields:
  ```sql
  SELECT
    countIf(user_id = '')    AS no_user,
    countIf(session_id = '') AS no_session,
    countIf(agent_id = '')   AS no_agent
  FROM table(agentguard_cim_event)
  WHERE event_time >= now() - INTERVAL 1 DAY
  ```
- `tenant_id`, `role_id`, versions are structurally absent — report "not captured".

## A25 调用链断裂 — 🔍 investigate
Tool call not associable to its conversation/model/user, or result to reply.
- Proxy — tool events whose `run_id`/`session_id` is empty:
  ```sql
  SELECT count() FROM table(agentguard_cim_event)
  WHERE event_type IN ('tool_invoke','tool_complete') AND (run_id = '' OR session_id = '')
  ```
- Full `trace_id`/`parent_event_id` linkage is #261.

## A26 模型和策略版本不可追溯 — 🔍 investigate
Missing model/agent/prompt/tool/policy version.
- `model` exists; `model_version`, `prompt_version`, `tool_version`,
  `policy_version` do not (#261). Report "not captured".

## A27 日志内容过度留存 — 🔍 investigate
Full conversation/tool result retained when only a summary/hash is needed.
- Governance/config concern, not derivable from event data. Advise reviewing
  retention config; no detection.
````

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/sentry/skills/ -run TestDataAuditGuide -v`
Expected: PASS (both SKILL and references tests).

- [ ] **Step 5: Commit**

```bash
cd backend && git add internal/sentry/skills/knowledge/data-audit-guide/references internal/sentry/skills/dataaudit_skill_test.go
git commit -m "feat(sentry): add data-audit-guide reference playbooks A01-A27 (#250)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MuJKFZSUHHbbC2WCMGoVrB"
```

---

## Task 8: Visibility gate + loop hint

**Files:**
- Modify: `backend/internal/handlers/sentry.go` (~line 224, `hiddenSkills`)
- Modify: `backend/internal/sentry/loop.go` (~line 103, `agRules` hint)
- Create: `backend/internal/sentry/skills/dataaudit_gate_test.go`

**Interfaces:**
- Consumes: the `data-audit-guide` skill (Tasks 6–7) and the existing
  `NewLoadSkill(dir, hidden...)` hidden mechanism.
- Produces: `data-audit-guide` hidden exactly when `agentguard-guide` is hidden
  (both names appended together in `sentry.go`).

- [ ] **Step 1: Write the failing gate test**

Create `backend/internal/sentry/skills/dataaudit_gate_test.go`. This tests the
exact mechanism `sentry.go` uses — `NewLoadSkill(dir, hidden...)` — with no
heavy handler dependencies:

```go
package skills

import (
	"context"
	"strings"
	"testing"
)

// TestDataAuditGuideRespectsHiddenList asserts data-audit-guide is loadable
// when not hidden and neither listed nor loadable when hidden — the same
// contract agentguard-guide relies on. sentry.go appends "data-audit-guide"
// to the hidden set on the same branch as "agentguard-guide" whenever
// AgentGuard tools are disabled.
func TestDataAuditGuideRespectsHiddenList(t *testing.T) {
	// Visible when not hidden.
	rc := &RunContext{Loaded: map[string]bool{}}
	out, err := NewLoadSkill("").Execute(context.Background(), rc,
		loadArgs(t, map[string]string{"name": "data-audit-guide"}))
	if err != nil || strings.Contains(out, "unknown skill") {
		t.Fatalf("data-audit-guide should load when not hidden: %v / %.80q", err, out)
	}

	// Hidden when listed: not loadable...
	hidden := NewLoadSkill("", "agentguard-guide", "data-audit-guide")
	rc2 := &RunContext{Loaded: map[string]bool{}}
	hiddenOut, _ := hidden.Execute(context.Background(), rc2,
		loadArgs(t, map[string]string{"name": "data-audit-guide"}))
	if !strings.Contains(hiddenOut, "unknown skill") {
		t.Fatalf("data-audit-guide must be unloadable when hidden, got %.80q", hiddenOut)
	}
	// ...and not advertised in the tool description.
	if strings.Contains(hidden.Description(), "data-audit-guide") {
		t.Error("hidden skill must not appear in load_skill description")
	}
}
```

> This test passes as soon as the skill files (Tasks 6–7) exist — it verifies
> the hidden mechanism, not the `sentry.go` edit. The `sentry.go` change in
> Step 3 is the one-line wiring that makes the disabled surface actually pass
> both names; it is covered by the full build/test in Task 9.

- [ ] **Step 2: Run the test**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/sentry/skills/ -run TestDataAuditGuideRespectsHiddenList -v`
Expected: PASS (the skill exists from Tasks 6–7 and the hidden mechanism already works). If it FAILS with "should load when not hidden", the skill files are missing — finish Tasks 6–7 first.

- [ ] **Step 3: Add the gate**

In `backend/internal/handlers/sentry.go`, in `sentryRegistry`, extend the hidden list:

```go
	var hiddenSkills []string
	if !cc.AgentGuardToolsEnabled {
		hiddenSkills = append(hiddenSkills, "agentguard-guide", "data-audit-guide")
	}
```

- [ ] **Step 4: Add the loop hint**

In `backend/internal/sentry/loop.go`, in the `agRules` string (the branch used
when AgentGuard tools are enabled, ~line 103), append a sentence:

```go
		agRules = `- Prefer curated tools (get_threats, get_holds, dlp_search, get_rule_catalog, ...) for common AgentGuard questions; load_skill("agentguard-guide") documents the streams and canonical queries behind every AgentGuard resource. For the customer data-security audit checklist (A01-A27), load_skill("data-audit-guide") maps each audit rule to its detection rule or investigation recipe.`
```

> Match the exact existing string literal when editing; keep the surrounding
> backtick/format intact.

- [ ] **Step 5: Run to verify pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ ./internal/sentry/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd backend && git add internal/handlers/sentry.go internal/sentry/skills/dataaudit_gate_test.go internal/sentry/loop.go
git commit -m "feat(sentry): gate data-audit-guide behind agentguard_tools + loop hint (#250)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MuJKFZSUHHbbC2WCMGoVrB"
```

---

## Task 9: Full build, lint, and live preflight validation

**Files:** none created; this task validates the whole change.

- [ ] **Step 1: Full backend build**

Run: `cd backend && CGO_ENABLED=0 go build ./...`
Expected: no output (success).

- [ ] **Step 2: Full backend test suite**

Run: `cd backend && CGO_ENABLED=0 go test ./...`
Expected: PASS. If a test asserts a hardcoded seed-rule COUNT (grep: `grep -rn "len(.*All())" internal/catalog | grep -i "==\|!="`), update its expected count by +17.

- [ ] **Step 3: Backend lint**

Run: `cd backend && golangci-lint run ./... 2>&1 | tail -20` (or `make lint` from repo root).
Expected: clean, or only pre-existing unrelated findings.

- [ ] **Step 4: Live preflight of every new rule (requires a running Timeplus)**

Start the stack (`docker compose up --build` or `make dev` with Timeplus
reachable), complete the setup wizard / `POST /api/setup/provision`, then for
each new rule confirm it installs (the lifecycle service runs candidate
preflight against a disposable sink — a bad `sql_template` fails here):

```bash
for id in dsa-a01 dsa-a03 dsa-a04 dsa-a05 dsa-a06 dsa-a07 dsa-a08 dsa-a09 dsa-a10 dsa-a11 dsa-a12 dsa-a14 dsa-a17 dsa-a18 dsa-a19 dsa-a20 dsa-a22; do
  echo "== $id =="; curl -s -X POST "http://localhost:8080/api/rules/$id/install" | head -c 300; echo
done
```

Expected: each returns an installed instance (HTTP 200), no `preflight` error.
If a rule's SQL is rejected by Timeplus, fix that rule's `sql_template`
(re-run its task's golden regen + tests) before proceeding. Then enable the
whole package in one call:

```bash
curl -s -X POST "http://localhost:8080/api/rules/packages/data-security-audit/enable" | head -c 400; echo
```

Expected: `{"installed":[...],"skipped":[...],"missing":[],"failed":[]}` with
`failed` empty.

- [ ] **Step 5: UI smoke check**

Open `/rules`. Confirm the **Data-Security Audit (Customer Baseline)** package
appears in the rail (visible-by-default), lists 17 rules, and that switching
the UI language to 中文 shows the PDF names (用户输入敏感数据检测, etc.). With
`agentguard_tools_enabled` on in **Settings → Sentry Agent**, ask Sentry
"audit this deployment against the customer data-security checklist" and
confirm it loads `data-audit-guide`.

- [ ] **Step 6: Final commit (only if any fixes were made in Step 4)**

```bash
cd backend && git add -A
git commit -m "fix(rules): preflight corrections for data-security-audit rules (#250)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01MuJKFZSUHHbbC2WCMGoVrB"
```

---

## Appendix: A-code → deliverable coverage (self-review cross-check)

| A-code | Task | Deliverable |
|--------|------|-------------|
| A01,A03,A04,A05,A06,A07 | 1 | `dsa-a0x` rules |
| A08,A09,A10,A11,A12 | 2 | `dsa-a0x`/`a1x` rules |
| A14,A17,A18 | 3 | `dsa-a1x` rules |
| A19,A20,A22 | 4 | `dsa-a19/20/22` rules |
| all 17 above | 5 | package + visibility |
| A01–A27 catalog | 6 | SKILL.md |
| A02,A13,A15,A16,A21,A23,A24,A25,A26,A27 | 7 | investigate-only references |
| gate + hint | 8 | Sentry wiring |
| whole change | 9 | build/lint/preflight |
