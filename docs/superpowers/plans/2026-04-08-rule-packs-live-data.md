# Rule Packs Live Data Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace hardcoded mock rule data with a dynamic system where rule metadata lives in an `agentshield_rules` Proton stream, per-rule Materialized Views are created/paused/resumed dynamically, and a global Proton ALERT fans out detections to a webhook.

**Architecture:** Rule metadata is stored append-only in `agentshield_rules`. Current state is resolved via `arg_max` GROUP BY. When a rule is enabled, a MV (`mv_rule_rp001`) is created; when disabled, it is PAUSED PERMANENT (not dropped). A single Proton ALERT reads from `agentshield_security_events` and calls a Python UDF that POSTs to `POST /api/alerts/webhook`.

**Tech Stack:** Go/Gin backend, Proton HTTP API (SELECT/DDL), Proton native driver (init DDL), React/Vite with React Router.

---

## File Map

**Modify:**
- `backend/internal/models/models.go` — replace old rule models with spec types
- `backend/internal/timeplus/client.go` — add `ExecHTTP` helper for DDL over HTTP
- `backend/internal/timeplus/init.go` — remove old hardcoded MVs, add rules stream + UDF + ALERT + startup sync
- `backend/internal/handlers/rules.go` — full rewrite: live Proton queries + MV management
- `backend/cmd/server/main.go` — add new route registrations
- `backend/internal/mock/data.go` — remove Rules and EnforcementEvents
- `frontend/src/api/client.js` — add getRule, upgradeRule, uninstallRule
- `frontend/src/pages/RulePacks.jsx` — fix field names, add View Details links
- `frontend/src/App.jsx` — add /rules/:id route

**Create:**
- `backend/internal/timeplus/rules.go` — MV name helper + MV lifecycle operations
- `frontend/src/pages/RuleDetail.jsx` — new rule detail page

---

## Task 1: Update Go Models

**Files:**
- Modify: `backend/internal/models/models.go`

- [ ] **Step 1: Replace rule-related models**

Find the `RulePack`, `EnforcementEvent`, and `RulesResponse` structs (lines 261–284) and replace them:

```go
type RulePack struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Version           string `json:"version"`
	Description       string `json:"description"`
	QuerySQL          string `json:"query_sql"`
	Enabled           bool   `json:"enabled"`
	Severity          string `json:"severity"`
	Category          string `json:"category"`
	RequiredTelemetry string `json:"required_telemetry"`
	FPRate            string `json:"fp_rate"`
	HookSources       string `json:"hook_sources"`
	Detections24h     int    `json:"detections_24h"`
	LatestVersion     string `json:"latest_version,omitempty"`
}

type RulesResponse struct {
	RulePacks          []RulePack      `json:"rule_packs"`
	TotalDetections24h int             `json:"total_detections_24h"`
	ActiveRuleDensity  int             `json:"active_rule_density"`
	RecentDetections   []SecurityEvent `json:"recent_detections"`
}

type SecurityEvent struct {
	RuleID     string `json:"rule_id"`
	RuleName   string `json:"rule_name"`
	Severity   string `json:"severity"`
	AgentID    string `json:"agent_id"`
	HookName   string `json:"hook_name"`
	Message    string `json:"message"`
	DetectedAt string `json:"detected_at"`
}

type RuleDetailResponse struct {
	RulePack
	RecentDetections []SecurityEvent `json:"recent_detections"`
}

type AlertWebhookPayload struct {
	RuleID     string `json:"rule_id"`
	RuleName   string `json:"rule_name"`
	Severity   string `json:"severity"`
	AgentID    string `json:"agent_id"`
	Message    string `json:"message"`
	DetectedAt string `json:"detected_at"`
}
```

Delete `EnforcementEvent` entirely (no longer used).

- [ ] **Step 2: Verify compile**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend && CGO_ENABLED=0 go build ./...
```

Expected: compile errors in `handlers/rules.go` and `mock/data.go` referencing old types — that's expected. `models.go` itself compiles.

- [ ] **Step 3: Commit**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend
git add internal/models/models.go
git commit -m "feat(models): replace rule models with spec types (RulePack, SecurityEvent, RuleDetailResponse)"
```

---

## Task 2: Add HTTP Exec Helper

**Files:**
- Modify: `backend/internal/timeplus/client.go`

Handlers need to execute DDL (CREATE MV, PAUSE, RESUME, DROP) using `*config.TimeplusConfig`, not a native driver connection.

- [ ] **Step 1: Add ExecHTTP after the Query function**

```go
// ExecHTTP executes a DDL or DML statement against the Proton HTTP interface.
// Use for CREATE/DROP/PAUSE/RESUME statements from handler code.
func ExecHTTP(cfg *config.TimeplusConfig, sql string) error {
	url := fmt.Sprintf("http://%s:%d/", cfg.Host, cfg.Port)
	body := strings.NewReader(sql)

	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("proton exec: build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	if cfg.User != "" {
		req.SetBasicAuth(cfg.User, cfg.Password)
	}

	resp, err := protonHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("proton exec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("proton exec: status %d: %s", resp.StatusCode, string(errBody))
	}
	return nil
}
```

- [ ] **Step 2: Verify compile**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend && CGO_ENABLED=0 go build ./...
```

Expected: same errors in handlers/rules.go and mock/data.go — client.go itself compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/timeplus/client.go
git commit -m "feat(timeplus): add ExecHTTP helper for DDL over HTTP interface"
```

---

## Task 3: Create timeplus/rules.go with MV Helpers

**Files:**
- Create: `backend/internal/timeplus/rules.go`

- [ ] **Step 1: Create the file**

```go
package timeplus

import (
	"fmt"
	"log"
	"strings"

	"github.com/timeplus/agentshield/internal/config"
)

// MVName returns the materialized view name for a rule ID.
// e.g. "rp-001" → "mv_rule_rp001"
func MVName(ruleID string) string {
	return "mv_rule_" + strings.ReplaceAll(ruleID, "-", "")
}

// CreateRuleMV creates a new materialized view for the rule.
// querySQLBody is the full SELECT ... FROM agentshield_hook_events WHERE ... body.
func CreateRuleMV(cfg *config.TimeplusConfig, ruleID, querySQLBody string) error {
	mvName := MVName(ruleID)
	ddl := fmt.Sprintf(
		"CREATE MATERIALIZED VIEW %s INTO agentshield_security_events AS %s",
		mvName, querySQLBody,
	)
	if err := ExecHTTP(cfg, ddl); err != nil {
		return fmt.Errorf("create MV %s: %w", mvName, err)
	}
	log.Printf("rules: created MV %s", mvName)
	return nil
}

// PauseRuleMV permanently pauses the MV (survives Proton restarts).
func PauseRuleMV(cfg *config.TimeplusConfig, ruleID string) error {
	mvName := MVName(ruleID)
	sql := fmt.Sprintf("SYSTEM PAUSE MATERIALIZED VIEW %s PERMANENT", mvName)
	if err := ExecHTTP(cfg, sql); err != nil {
		return fmt.Errorf("pause MV %s: %w", mvName, err)
	}
	log.Printf("rules: paused MV %s", mvName)
	return nil
}

// ResumeRuleMV permanently resumes the MV (survives Proton restarts).
func ResumeRuleMV(cfg *config.TimeplusConfig, ruleID string) error {
	mvName := MVName(ruleID)
	sql := fmt.Sprintf("SYSTEM RESUME MATERIALIZED VIEW %s PERMANENT", mvName)
	if err := ExecHTTP(cfg, sql); err != nil {
		return fmt.Errorf("resume MV %s: %w", mvName, err)
	}
	log.Printf("rules: resumed MV %s", mvName)
	return nil
}

// DropRuleMV drops the MV entirely (used on upgrade and uninstall).
func DropRuleMV(cfg *config.TimeplusConfig, ruleID string) error {
	mvName := MVName(ruleID)
	sql := fmt.Sprintf("DROP VIEW IF EXISTS %s", mvName)
	if err := ExecHTTP(cfg, sql); err != nil {
		return fmt.Errorf("drop MV %s: %w", mvName, err)
	}
	log.Printf("rules: dropped MV %s", mvName)
	return nil
}
```

- [ ] **Step 2: Verify compile**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend && CGO_ENABLED=0 go build ./...
```

Expected: same errors in handlers/rules.go and mock/data.go — the new file compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/timeplus/rules.go
git commit -m "feat(timeplus): add rule MV lifecycle helpers (Create/Pause/Resume/Drop)"
```

---

## Task 4: Rewrite init.go Security Resources

**Files:**
- Modify: `backend/internal/timeplus/init.go`

Replace the hardcoded MV approach with: agentshield_rules stream creation, built-in rule seeding, legacy MV migration, Python UDF + ALERT creation, and startup sync.

- [ ] **Step 1: Update Init() to call new functions**

Replace the current `Init` function body:

```go
func Init(cfg *config.TimeplusConfig) error {
	conn, err := waitForTimeplus(cfg)
	if err != nil {
		return fmt.Errorf("timeplus init: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()

	if err := createOTelStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating otel streams: %w", err)
	}
	if err := createOTelInput(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating otel input: %w", err)
	}
	if err := createAgentShieldStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating agentshield streams: %w", err)
	}
	if err := createSecurityStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating security streams: %w", err)
	}
	if err := seedBuiltinRules(ctx, conn); err != nil {
		log.Printf("timeplus init: warning seeding rules: %v", err)
	}
	if err := dropLegacyMVs(ctx, conn); err != nil {
		log.Printf("timeplus init: warning dropping legacy MVs: %v", err)
	}
	if err := createNotifyUDF(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating UDF: %v", err)
	}
	if err := createNotifierAlert(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating alert: %v", err)
	}
	if err := startupSyncMVs(cfg, ctx, conn); err != nil {
		log.Printf("timeplus init: warning syncing MVs: %v", err)
	}
	return nil
}
```

- [ ] **Step 2: Replace createSecurityResources with createSecurityStreams**

Delete the `createSecurityResources` function and replace with:

```go
func createSecurityStreams(ctx context.Context, conn proton.Conn) error {
	if err := execIfNotExists(ctx, conn, "stream", "agentshield_security_events", ddlSecurityEvents); err != nil {
		return err
	}
	return execIfNotExists(ctx, conn, "stream", "agentshield_rules", ddlRulesStream)
}
```

- [ ] **Step 3: Add ddlRulesStream constant after ddlSecurityEvents**

```go
const ddlRulesStream = `
CREATE STREAM agentshield_rules
(
  rule_id            low_cardinality(string),
  name               string,
  version            string,
  description        string,
  query_sql          string,
  enabled            bool,
  deleted            bool,
  severity           low_cardinality(string),
  category           low_cardinality(string),
  required_telemetry string,
  fp_rate            low_cardinality(string),
  hook_sources       string,
  updated_at         datetime64(3)
)`
```

- [ ] **Step 4: Add builtinRule struct and data**

Add before `seedBuiltinRules` (the query SQL bodies match what was in the old hardcoded MVs, now stored as rule metadata):

```go
type builtinRule struct {
	ruleID            string
	name              string
	version           string
	description       string
	querySQL          string
	severity          string
	category          string
	requiredTelemetry string
	fpRate            string
	hookSources       string
}

var builtinRuleData = []builtinRule{
	{
		ruleID:      "rp-001",
		name:        "Prompt Injection Shield",
		version:     "v2.4.1",
		description: "Detects direct and indirect prompt injection attacks, jailbreak attempts, and goal hijacking across all message surfaces.",
		severity:    "critical",
		category:    "prompt_injection",
		fpRate:      "medium",
		hookSources: "llm_input,llm_output",
		querySQL: `SELECT
  event_time                AS detected_at,
  'rp-001'                  AS rule_id,
  'Prompt Injection Shield' AS rule_name,
  'critical'                AS severity,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  hook_name,
  ''                        AS tool_name,
  concat('Potential prompt injection in ', hook_name, ' from agent ', agent_id) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name IN ('llm_input', 'llm_output')
  AND (
       lower(event_data) LIKE '%ignore previous instructions%'
    OR lower(event_data) LIKE '%ignore all previous%'
    OR lower(event_data) LIKE '%jailbreak%'
    OR lower(event_data) LIKE '%disregard your%'
    OR lower(event_data) LIKE '%you are now a%'
    OR lower(event_data) LIKE '%forget everything%'
    OR lower(event_data) LIKE '%new instructions:%'
  )`,
	},
	{
		ruleID:            "rp-002",
		name:              "DLP Sentinel",
		version:           "v1.9.0",
		description:       "Scans outbound agent communication for PII, API keys, and sensitive internal documentation leaks.",
		severity:          "critical",
		category:          "dlp",
		requiredTelemetry: "content_capture",
		fpRate:            "low",
		hookSources:       "tool_result_persist,after_tool_call,llm_output",
		querySQL: `SELECT
  event_time     AS detected_at,
  'rp-002'       AS rule_id,
  'DLP Sentinel' AS rule_name,
  'critical'     AS severity,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  hook_name,
  tool_name,
  concat('Sensitive data in ', hook_name, if(tool_name != '', concat(' (', tool_name, ')'), ''), ' from agent ', agent_id) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name IN ('tool_result_persist', 'after_tool_call', 'llm_output')
  AND (
       match(event_data, '[0-9]{3}-[0-9]{2}-[0-9]{4}')
    OR lower(event_data) LIKE '%api_key%'
    OR lower(event_data) LIKE '%secret_key%'
    OR lower(event_data) LIKE '%bearer %'
    OR lower(event_data) LIKE '%sk-ant-%'
    OR lower(event_data) LIKE '%ghp_%'
    OR lower(event_data) LIKE '%private_key%'
    OR lower(event_data) LIKE '%aws_secret%'
  )`,
	},
	{
		ruleID:      "rp-003",
		name:        "Privilege Guard",
		version:     "v1.2.0",
		description: "Monitors privilege escalation attempts through tool abuse, shell escapes, and dangerous system commands.",
		severity:    "warning",
		category:    "privilege",
		fpRate:      "low",
		hookSources: "before_tool_call,after_tool_call",
		querySQL: `SELECT
  event_time        AS detected_at,
  'rp-003'          AS rule_id,
  'Privilege Guard' AS rule_name,
  'warning'         AS severity,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  hook_name,
  tool_name,
  concat('Dangerous tool call: ', tool_name, ' by agent ', agent_id) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name IN ('before_tool_call', 'after_tool_call')
  AND (
       lower(tool_name) IN ('bash', 'shell', 'exec', 'run_terminal_cmd', 'computer')
    OR lower(event_data) LIKE '%sudo %'
    OR lower(event_data) LIKE '%rm -rf%'
    OR lower(event_data) LIKE '%chmod %'
    OR lower(event_data) LIKE '%/etc/passwd%'
    OR lower(event_data) LIKE '%/etc/shadow%'
  )`,
	},
	{
		ruleID:      "rp-004",
		name:        "Supply Chain Watch",
		version:     "v3.1.2",
		description: "Monitors external model dependencies and third-party library calls for anomalous behavior.",
		severity:    "warning",
		category:    "supply_chain",
		fpRate:      "high",
		hookSources: "before_install,before_tool_call,after_tool_call",
		querySQL: `SELECT
  event_time           AS detected_at,
  'rp-004'             AS rule_id,
  'Supply Chain Watch' AS rule_name,
  'warning'            AS severity,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  hook_name,
  tool_name,
  concat('Package install by agent ', agent_id, ' via ', if(tool_name != '', tool_name, hook_name)) AS message,
  event_data
FROM agentshield_hook_events
WHERE hook_name = 'before_install'
   OR (
       hook_name IN ('before_tool_call', 'after_tool_call')
    AND (
           lower(event_data) LIKE '%npm install%'
        OR lower(event_data) LIKE '%pip install%'
        OR lower(event_data) LIKE '%yarn add%'
        OR lower(event_data) LIKE '%cargo install%'
        OR lower(event_data) LIKE '%apt-get install%'
        OR lower(event_data) LIKE '%brew install%'
    )
  )`,
	},
}
```

- [ ] **Step 5: Add seedBuiltinRules**

```go
func seedBuiltinRules(ctx context.Context, conn proton.Conn) error {
	row := conn.QueryRow(ctx, "SELECT count() FROM table(agentshield_rules)")
	var count uint64
	if err := row.Scan(&count); err != nil {
		return fmt.Errorf("seed check: %w", err)
	}
	if count > 0 {
		log.Printf("timeplus: rules already seeded (%d records)", count)
		return nil
	}

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO agentshield_rules")
	if err != nil {
		return fmt.Errorf("seed prepare: %w", err)
	}

	now := time.Now()
	for _, r := range builtinRuleData {
		if err := batch.Append(
			r.ruleID, r.name, r.version, r.description, r.querySQL,
			true, false, r.severity, r.category, r.requiredTelemetry,
			r.fpRate, r.hookSources, now,
		); err != nil {
			return fmt.Errorf("seed append %s: %w", r.ruleID, err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("seed send: %w", err)
	}
	log.Printf("timeplus: seeded %d built-in rules", len(builtinRuleData))
	return nil
}
```

- [ ] **Step 6: Add dropLegacyMVs**

```go
func dropLegacyMVs(ctx context.Context, conn proton.Conn) error {
	legacy := []string{
		"mv_security_prompt_injection",
		"mv_security_dlp",
		"mv_security_privilege_guard",
		"mv_security_supply_chain",
	}
	for _, name := range legacy {
		exists, err := streamExists(ctx, conn, name)
		if err != nil || !exists {
			continue
		}
		if err := conn.Exec(ctx, fmt.Sprintf("DROP VIEW IF EXISTS %s", name)); err != nil {
			log.Printf("timeplus: warning dropping legacy MV %s: %v", name, err)
		} else {
			log.Printf("timeplus: dropped legacy MV %s", name)
		}
	}
	return nil
}
```

- [ ] **Step 7: Add ddlNotifyUDF constant and createNotifyUDF**

```go
const ddlNotifyUDF = `CREATE OR REPLACE FUNCTION notify_agentshield_udf(
  rule_id string, rule_name string, severity string,
  agent_id string, deployment_id string, session_id string,
  hook_name string, tool_name string, message string, detected_at string
) RETURNS string LANGUAGE PYTHON AS $$
import requests
def notify_agentshield_udf(rule_ids, rule_names, severities,
                            agent_ids, deployment_ids, session_ids,
                            hook_names, tool_names, messages, detected_ats):
    for i in range(len(rule_ids)):
        try:
            requests.post("http://agentshield:8080/api/alerts/webhook", json={
                "rule_id": rule_ids[i],
                "rule_name": rule_names[i],
                "severity": severities[i],
                "agent_id": agent_ids[i],
                "message": messages[i],
                "detected_at": detected_ats[i]
            }, timeout=3)
        except Exception:
            pass
    return rule_ids
$$`

func createNotifyUDF(ctx context.Context, conn proton.Conn) error {
	if err := conn.Exec(ctx, ddlNotifyUDF); err != nil {
		return fmt.Errorf("create UDF: %w", err)
	}
	log.Printf("timeplus: UDF notify_agentshield_udf created/updated")
	return nil
}
```

- [ ] **Step 8: Add ddlNotifierAlert constant and createNotifierAlert**

```go
const ddlNotifierAlert = `CREATE OR REPLACE ALERT agentshield_notifier
  BATCH 1 EVENTS WITH TIMEOUT 5s
  LIMIT 10 ALERTS PER 1m
  CALL notify_agentshield_udf
AS SELECT
  rule_id, rule_name, severity, agent_id, deployment_id,
  session_id, hook_name, tool_name, message, detected_at
FROM agentshield_security_events`

func createNotifierAlert(ctx context.Context, conn proton.Conn) error {
	if err := conn.Exec(ctx, ddlNotifierAlert); err != nil {
		return fmt.Errorf("create alert: %w", err)
	}
	log.Printf("timeplus: alert agentshield_notifier created/updated")
	return nil
}
```

- [ ] **Step 9: Add startupSyncMVs**

```go
const sqlStartupRules = `
SELECT
  rule_id,
  arg_max(query_sql, updated_at) AS query_sql,
  arg_max(enabled,   updated_at) AS enabled
FROM table(agentshield_rules)
GROUP BY rule_id
HAVING arg_max(deleted, updated_at) = false`

func startupSyncMVs(cfg *config.TimeplusConfig, ctx context.Context, conn proton.Conn) error {
	rows, err := conn.Query(ctx, sqlStartupRules)
	if err != nil {
		return fmt.Errorf("startup sync query: %w", err)
	}
	defer rows.Close()

	type ruleRow struct {
		ruleID   string
		querySQL string
		enabled  bool
	}
	var rules []ruleRow
	for rows.Next() {
		var r ruleRow
		if err := rows.Scan(&r.ruleID, &r.querySQL, &r.enabled); err != nil {
			log.Printf("timeplus: startup sync scan error: %v", err)
			continue
		}
		rules = append(rules, r)
	}

	for _, r := range rules {
		mvName := MVName(r.ruleID)
		exists, _ := streamExists(ctx, conn, mvName)
		if r.enabled {
			if !exists {
				ddl := fmt.Sprintf("CREATE MATERIALIZED VIEW %s INTO agentshield_security_events AS %s", mvName, r.querySQL)
				if err := conn.Exec(ctx, ddl); err != nil {
					log.Printf("timeplus: startup create MV %s: %v", mvName, err)
				} else {
					log.Printf("timeplus: startup created MV %s", mvName)
				}
			} else {
				if err := conn.Exec(ctx, fmt.Sprintf("SYSTEM RESUME MATERIALIZED VIEW %s PERMANENT", mvName)); err != nil {
					log.Printf("timeplus: startup resume MV %s: %v", mvName, err)
				}
			}
		} else if exists {
			if err := conn.Exec(ctx, fmt.Sprintf("SYSTEM PAUSE MATERIALIZED VIEW %s PERMANENT", mvName)); err != nil {
				log.Printf("timeplus: startup pause MV %s: %v", mvName, err)
			}
		}
	}
	log.Printf("timeplus: startup sync complete for %d rules", len(rules))
	return nil
}
```

- [ ] **Step 10: Delete old DDL constants and createSecurityResources**

Remove from init.go:
- `createSecurityResources` function
- `ddlMVPromptInjection` constant
- `ddlMVDLP` constant
- `ddlMVPrivilegeGuard` constant
- `ddlMVSupplyChain` constant

Keep `ddlSecurityEvents` (still used by `createSecurityStreams`).

- [ ] **Step 11: Verify compile**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend && CGO_ENABLED=0 go build ./...
```

Expected: errors in `handlers/rules.go` and `mock/data.go` still present — init.go itself compiles cleanly.

- [ ] **Step 12: Commit**

```bash
git add internal/timeplus/init.go
git commit -m "feat(init): replace hardcoded MVs with dynamic rule stream, UDF, alert, and startup sync"
```

---

## Task 5: Rewrite handlers/rules.go

**Files:**
- Modify: `backend/internal/handlers/rules.go`

Full rewrite. All rule data comes from live Proton queries. MV lifecycle uses the helpers from `timeplus/rules.go`.

- [ ] **Step 1: Replace the entire file**

```go
package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	tp "github.com/timeplus/agentshield/internal/timeplus"
)

// latestVersions is the catalog of the latest built-in rule versions.
// If the live version differs, the UI shows an "Upgrade" button.
var latestVersions = map[string]string{
	"rp-001": "v2.4.1",
	"rp-002": "v1.9.0",
	"rp-003": "v1.2.0",
	"rp-004": "v3.1.2",
}

const sqlCurrentRules = `
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  arg_max(enabled,            updated_at) AS enabled,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources
FROM table(agentshield_rules)
GROUP BY rule_id
HAVING arg_max(deleted, updated_at) = false`

func ListRules(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		packs, err := queryCurrentRulePacks(&cfg.Timeplus)
		if err != nil {
			log.Printf("rules: list query failed: %v", err)
			packs = []models.RulePack{}
		}

		if counts, err := queryDetectionCounts(&cfg.Timeplus); err == nil {
			for i := range packs {
				packs[i].Detections24h = counts[packs[i].ID]
			}
		}

		recentDetections := queryRecentDetections(&cfg.Timeplus)

		totalDetections := 0
		activeCount := 0
		for _, r := range packs {
			totalDetections += r.Detections24h
			if r.Enabled {
				activeCount++
			}
		}
		density := 0
		if len(packs) > 0 {
			density = activeCount * 100 / len(packs)
		}

		c.JSON(http.StatusOK, models.RulesResponse{
			RulePacks:          packs,
			TotalDetections24h: totalDetections,
			ActiveRuleDensity:  density,
			RecentDetections:   recentDetections,
		})
	}
}

func GetRule(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		packs, err := queryCurrentRulePacks(&cfg.Timeplus)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}
		var pack *models.RulePack
		for i := range packs {
			if packs[i].ID == id {
				pack = &packs[i]
				break
			}
		}
		if pack == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		countRows, err := tp.Query(&cfg.Timeplus, fmt.Sprintf(`
			SELECT count() AS cnt
			FROM table(agentshield_security_events)
			WHERE rule_id = '%s'
			  AND detected_at >= now() - interval 24 hour
		`, id))
		if err == nil && len(countRows) > 0 {
			if num, ok := countRows[0]["cnt"].(json.Number); ok {
				cnt, _ := num.Int64()
				pack.Detections24h = int(cnt)
			}
		}

		recentRows, _ := tp.Query(&cfg.Timeplus, fmt.Sprintf(`
			SELECT rule_id, rule_name, severity, agent_id, hook_name, message, detected_at
			FROM table(agentshield_security_events)
			WHERE rule_id = '%s'
			ORDER BY detected_at DESC
			LIMIT 50
		`, id))

		c.JSON(http.StatusOK, models.RuleDetailResponse{
			RulePack:         *pack,
			RecentDetections: rowsToSecurityEvents(recentRows),
		})
	}
}

func ToggleRule(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		packs, err := queryCurrentRulePacks(&cfg.Timeplus)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}
		var current *models.RulePack
		for i := range packs {
			if packs[i].ID == id {
				current = &packs[i]
				break
			}
		}
		if current == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		// Flip enabled via INSERT ... SELECT — reads current state and flips it atomically
		sql := fmt.Sprintf(`
INSERT INTO agentshield_rules
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  NOT arg_max(enabled,        updated_at) AS enabled,
  false                                   AS deleted,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources,
  now64(3)                                AS updated_at
FROM table(agentshield_rules)
WHERE rule_id = '%s'
GROUP BY rule_id`, id)

		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "toggle failed"})
			return
		}

		newEnabled := !current.Enabled
		if newEnabled {
			exists, _ := mvExists(&cfg.Timeplus, id)
			if exists {
				if err := tp.ResumeRuleMV(&cfg.Timeplus, id); err != nil {
					log.Printf("rules: toggle resume MV %s: %v", id, err)
				}
			} else {
				if err := tp.CreateRuleMV(&cfg.Timeplus, id, current.QuerySQL); err != nil {
					log.Printf("rules: toggle create MV %s: %v", id, err)
				}
			}
		} else {
			if err := tp.PauseRuleMV(&cfg.Timeplus, id); err != nil {
				log.Printf("rules: toggle pause MV %s: %v", id, err)
			}
		}

		current.Enabled = newEnabled
		c.JSON(http.StatusOK, current)
	}
}

func UpgradeRule(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		latestVer, ok := latestVersions[id]
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no upgrade available"})
			return
		}

		packs, err := queryCurrentRulePacks(&cfg.Timeplus)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}
		var current *models.RulePack
		for i := range packs {
			if packs[i].ID == id {
				current = &packs[i]
				break
			}
		}
		if current == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if current.Version == latestVer {
			c.JSON(http.StatusOK, gin.H{"message": "already at latest version"})
			return
		}

		var body struct {
			QuerySQL string `json:"query_sql"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.QuerySQL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "query_sql required in body"})
			return
		}

		escapedSQL := strings.ReplaceAll(body.QuerySQL, "'", "''")
		insertSQL := fmt.Sprintf(`INSERT INTO agentshield_rules VALUES
('%s','%s','%s','%s','%s',%v,false,'%s','%s','%s','%s','%s',now64(3))`,
			id,
			strings.ReplaceAll(current.Name, "'", "''"),
			latestVer,
			strings.ReplaceAll(current.Description, "'", "''"),
			escapedSQL,
			current.Enabled,
			current.Severity, current.Category,
			current.RequiredTelemetry, current.FPRate, current.HookSources,
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, insertSQL); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upgrade insert failed"})
			return
		}

		if err := tp.DropRuleMV(&cfg.Timeplus, id); err != nil {
			log.Printf("rules: upgrade drop MV %s: %v", id, err)
		}
		if current.Enabled {
			if err := tp.CreateRuleMV(&cfg.Timeplus, id, body.QuerySQL); err != nil {
				log.Printf("rules: upgrade create MV %s: %v", id, err)
			}
		}

		c.JSON(http.StatusOK, gin.H{"version": latestVer})
	}
}

func UninstallRule(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		sql := fmt.Sprintf(`
INSERT INTO agentshield_rules
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  arg_max(enabled,            updated_at) AS enabled,
  true                                    AS deleted,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources,
  now64(3)                                AS updated_at
FROM table(agentshield_rules)
WHERE rule_id = '%s'
GROUP BY rule_id`, id)

		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "uninstall failed"})
			return
		}

		if err := tp.DropRuleMV(&cfg.Timeplus, id); err != nil {
			log.Printf("rules: uninstall drop MV %s: %v", id, err)
		}

		c.JSON(http.StatusOK, gin.H{"deleted": true})
	}
}

func AlertWebhook(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload models.AlertWebhookPayload
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
			return
		}
		log.Printf("alert: rule=%s severity=%s agent=%s msg=%s",
			payload.RuleID, payload.Severity, payload.AgentID, payload.Message)
		// Future: fan out to Slack/PagerDuty via settings integrations
		c.JSON(http.StatusOK, gin.H{"received": true})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func queryCurrentRulePacks(cfg *config.TimeplusConfig) ([]models.RulePack, error) {
	rows, err := tp.Query(cfg, sqlCurrentRules)
	if err != nil {
		return nil, err
	}
	packs := make([]models.RulePack, 0, len(rows))
	for _, row := range rows {
		id := strVal(row, "rule_id")
		enabled, _ := row["enabled"].(bool)
		pack := models.RulePack{
			ID:                id,
			Name:              strVal(row, "name"),
			Version:           strVal(row, "version"),
			Description:       strVal(row, "description"),
			QuerySQL:          strVal(row, "query_sql"),
			Enabled:           enabled,
			Severity:          strVal(row, "severity"),
			Category:          strVal(row, "category"),
			RequiredTelemetry: strVal(row, "required_telemetry"),
			FPRate:            strVal(row, "fp_rate"),
			HookSources:       strVal(row, "hook_sources"),
		}
		if latest, ok := latestVersions[id]; ok && latest != pack.Version {
			pack.LatestVersion = latest
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

func queryDetectionCounts(cfg *config.TimeplusConfig) (map[string]int, error) {
	rows, err := tp.Query(cfg, `
		SELECT rule_id, count() AS cnt
		FROM table(agentshield_security_events)
		WHERE detected_at >= now() - interval 24 hour
		GROUP BY rule_id
	`)
	if err != nil {
		return nil, err
	}
	result := make(map[string]int, len(rows))
	for _, row := range rows {
		ruleID := strVal(row, "rule_id")
		if num, ok := row["cnt"].(json.Number); ok {
			cnt, _ := num.Int64()
			result[ruleID] = int(cnt)
		}
	}
	return result, nil
}

func queryRecentDetections(cfg *config.TimeplusConfig) []models.SecurityEvent {
	rows, err := tp.Query(cfg, `
		SELECT rule_id, rule_name, severity, agent_id, hook_name, message, detected_at
		FROM table(agentshield_security_events)
		ORDER BY detected_at DESC
		LIMIT 10
	`)
	if err != nil || len(rows) == 0 {
		return nil
	}
	return rowsToSecurityEvents(rows)
}

func rowsToSecurityEvents(rows []tp.Row) []models.SecurityEvent {
	events := make([]models.SecurityEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, models.SecurityEvent{
			RuleID:     strVal(row, "rule_id"),
			RuleName:   strVal(row, "rule_name"),
			Severity:   strVal(row, "severity"),
			AgentID:    strVal(row, "agent_id"),
			HookName:   strVal(row, "hook_name"),
			Message:    strVal(row, "message"),
			DetectedAt: strVal(row, "detected_at"),
		})
	}
	return events
}

func mvExists(cfg *config.TimeplusConfig, ruleID string) (bool, error) {
	mvName := tp.MVName(ruleID)
	rows, err := tp.Query(cfg, fmt.Sprintf(
		"SELECT count() AS cnt FROM system.tables WHERE name = '%s' AND database = current_database()",
		mvName,
	))
	if err != nil || len(rows) == 0 {
		return false, err
	}
	if num, ok := rows[0]["cnt"].(json.Number); ok {
		cnt, _ := num.Int64()
		return cnt > 0, nil
	}
	return false, nil
}

func strVal(row map[string]any, key string) string {
	v, _ := row[key].(string)
	return v
}
```

- [ ] **Step 2: Verify compile**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend && CGO_ENABLED=0 go build ./...
```

Expected: compile errors in `mock/data.go` — handlers/rules.go itself compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/handlers/rules.go
git commit -m "feat(handlers): rewrite rules handler with live Proton queries and MV lifecycle"
```

---

## Task 6: Update main.go Routes

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Replace rules route registrations**

Find the existing rules routes block and replace:

```go
// Old (remove these two lines):
api.GET("/rules", handlers.ListRules(cfg))
api.POST("/rules/:id/toggle", handlers.ToggleRule)

// New (add these six lines):
api.GET("/rules", handlers.ListRules(cfg))
api.GET("/rules/:id", handlers.GetRule(cfg))
api.POST("/rules/:id/toggle", handlers.ToggleRule(cfg))
api.POST("/rules/:id/upgrade", handlers.UpgradeRule(cfg))
api.DELETE("/rules/:id", handlers.UninstallRule(cfg))
api.POST("/alerts/webhook", handlers.AlertWebhook(cfg))
```

- [ ] **Step 2: Verify compile**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend && CGO_ENABLED=0 go build ./...
```

Expected: compile errors in `mock/data.go` — main.go itself compiles.

- [ ] **Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(routes): add GetRule, ToggleRule(cfg), UpgradeRule, UninstallRule, AlertWebhook routes"
```

---

## Task 7: Update mock/data.go

**Files:**
- Modify: `backend/internal/mock/data.go`

- [ ] **Step 1: Remove Rules and EnforcementEvents**

Delete the `Rules` variable (the `[]models.RulePack{...}` block) and the `EnforcementEvents` variable (the `[]models.EnforcementEvent{...}` block) from `mock/data.go`. Both are now sourced from Proton.

- [ ] **Step 2: Verify clean compile**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend && CGO_ENABLED=0 go build ./...
```

Expected: **clean — zero errors**.

- [ ] **Step 3: Build binary**

```bash
CGO_ENABLED=0 go build -o /tmp/agentshield-test ./cmd/server/main.go
echo "Build OK: $?"
rm /tmp/agentshield-test
```

Expected: `Build OK: 0`

- [ ] **Step 4: Commit**

```bash
git add internal/mock/data.go
git commit -m "feat(mock): remove Rules and EnforcementEvents (now sourced from Proton agentshield_rules stream)"
```

---

## Task 8: Update frontend api/client.js

**Files:**
- Modify: `frontend/src/api/client.js`

- [ ] **Step 1: Add new API methods after toggleRule**

```js
  getRule: (id) => get(`/rules/${id}`),
  upgradeRule: (id, querySql) => post(`/rules/${id}/upgrade`, { query_sql: querySql }),
  uninstallRule: (id) =>
    fetch(`${BASE}/rules/${id}`, { method: 'DELETE' })
      .then(res => { if (!res.ok) throw new Error(`HTTP ${res.status}`); return res.json() }),
```

- [ ] **Step 2: Verify parse**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/frontend
node -e "import('./src/api/client.js').then(() => console.log('OK')).catch(e => { console.error(e); process.exit(1) })" 2>/dev/null || echo "Parse OK (ESM)"
```

Expected: `Parse OK (ESM)` (or a module error is fine — we just need no syntax errors).

- [ ] **Step 3: Commit**

```bash
cd /Users/gangtao/Code/timeplus/agentshield
git add frontend/src/api/client.js
git commit -m "feat(api-client): add getRule, upgradeRule, uninstallRule methods"
```

---

## Task 9: Rewrite RulePacks.jsx

**Files:**
- Modify: `frontend/src/pages/RulePacks.jsx`

Key changes: fix field names (`blocks_24h` → `detections_24h`, `recent_enforcement` → `recent_detections`, `total_blocks_24h` → `total_detections_24h`), make "View Details" a React Router `Link`.

- [ ] **Step 1: Replace the file contents**

```jsx
import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import Toggle from '../components/Toggle'
import { api } from '../api/client'

const RULE_ICONS = {
  'rp-001': 'security_update_good',
  'rp-002': 'data_loss_prevention',
  'rp-003': 'admin_panel_settings',
  'rp-004': 'account_tree',
  'rp-005': 'paid',
  'rp-006': 'description',
}

function severityDot(severity) {
  if (severity === 'critical') return 'bg-[#ffb0cf] shadow-[0_0_8px_rgba(255,176,207,0.6)]'
  if (severity === 'warning') return 'bg-[#ffb0cf]/30'
  return 'bg-zinc-500'
}

function formatTs(ts) {
  if (!ts) return ''
  try {
    return new Date(ts.replace(' ', 'T')).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } catch { return ts }
}

const staticRules = [
  { id: 'rp-001', icon: 'security_update_good', name: 'Prompt Injection Shield', desc: 'Advanced heuristic filtering to prevent jailbreaking attempts and unauthorized behavioral overrides in LLM agents.', enabled: true, version: 'v2.4.1', active: true, detections24h: 0 },
  { id: 'rp-002', icon: 'data_loss_prevention', name: 'DLP Sentinel', desc: 'Scans outbound agent communication for PII, API keys, and sensitive internal documentation leaks.', enabled: true, version: 'v1.9.0', active: true, detections24h: 0 },
  { id: 'rp-003', icon: 'admin_panel_settings', name: 'Privilege Guard', desc: 'Enforces strict Least Privilege access for agents. Limits tool execution to predefined namespaces.', enabled: false, version: null, active: false, detections24h: 0 },
  { id: 'rp-004', icon: 'account_tree', name: 'Supply Chain Watch', desc: 'Monitors external model dependencies and third-party library calls for anomalous behavior.', enabled: true, version: 'v3.1.2', active: true, detections24h: 0 },
]

export default function RulePacks() {
  const [rules, setRules] = useState(staticRules)
  const [loading, setLoading] = useState(true)
  const [totalDetections, setTotalDetections] = useState(0)
  const [density, setDensity] = useState(0)
  const [recentDetections, setRecentDetections] = useState([])

  useEffect(() => {
    api.listRules()
      .then(data => {
        if (data.rule_packs && data.rule_packs.length > 0) {
          setRules(data.rule_packs.map(r => ({
            id: r.id,
            icon: RULE_ICONS[r.id] || 'shield',
            name: r.name,
            desc: r.description,
            enabled: r.enabled,
            version: r.enabled ? (r.version || null) : null,
            active: r.enabled,
            detections24h: r.detections_24h || 0,
          })))
        }
        if (data.total_detections_24h !== undefined) setTotalDetections(data.total_detections_24h)
        if (data.active_rule_density !== undefined) setDensity(data.active_rule_density)
        if (data.recent_detections) setRecentDetections(data.recent_detections)
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  const handleToggle = async (id, newVal) => {
    setRules(prev => prev.map(r => r.id === id ? { ...r, enabled: newVal, active: newVal } : r))
    try {
      await api.toggleRule(id)
    } catch {
      setRules(prev => prev.map(r => r.id === id ? { ...r, enabled: !newVal, active: !newVal } : r))
    }
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">
          <PageHeader
            title="Rule Packs"
            subtitle="Configure and manage security policies enforced across all monitored AI agents."
            actions={<>
              <button className="px-4 py-2 bg-[#2c2834] text-[#e7dff0] text-xs font-medium rounded-sm border border-[#574149]/10 hover:bg-[#37333f] transition-colors">
                Import Manifest
              </button>
              <button className="px-4 py-2 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] text-xs font-semibold rounded-sm">
                New Custom Rule
              </button>
            </>}
          />

          <section className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-6">
            {loading ? (
              [1,2,3,4].map(i => <div key={i} className="bg-[#1d1a25] p-6 rounded-sm h-64 animate-pulse" />)
            ) : rules.map(rule => (
              <div key={rule.id} className="group bg-[#1d1a25] p-6 rounded-sm flex flex-col gap-6 hover:bg-[#2c2834] transition-all duration-300 relative overflow-hidden">
                {rule.active && (
                  <div className="absolute top-0 right-0 w-24 h-24 bg-[#ffb0cf]/5 blur-3xl rounded-full -translate-y-12 translate-x-12"></div>
                )}
                <div className="flex justify-between items-start z-10">
                  <div className={`w-12 h-12 bg-[#0f0c17] flex items-center justify-center rounded-sm ${rule.active ? 'text-[#ffb0cf]' : 'text-zinc-500'}`}>
                    <span className="material-symbols-outlined" style={rule.active ? { fontVariationSettings: "'FILL' 1" } : {}}>{rule.icon}</span>
                  </div>
                  <Toggle checked={rule.enabled} onChange={val => handleToggle(rule.id, val)} />
                </div>
                <div className="z-10">
                  <h3 className="text-lg font-semibold tracking-tight text-white mb-2">{rule.name}</h3>
                  <p className="text-xs text-zinc-400 leading-relaxed min-h-[4.5rem]">{rule.desc}</p>
                  {rule.detections24h > 0 && (
                    <p className="text-[10px] font-mono text-[#ffb0cf] mt-2">{rule.detections24h.toLocaleString()} events detected (24h)</p>
                  )}
                </div>
                <div className="mt-auto flex items-center justify-between z-10">
                  <Link to={`/rules/${rule.id}`} className="text-xs font-semibold text-[#ffb0cf] tracking-wider uppercase hover:underline decoration-2 underline-offset-4">
                    View Details
                  </Link>
                  <span className="text-[10px] text-zinc-500 flex items-center gap-1">
                    {rule.version ? (
                      <>
                        <span className="material-symbols-outlined text-[12px] text-[#ffb0cf]" style={{ fontVariationSettings: "'FILL' 1" }}>bolt</span>
                        {rule.version}
                      </>
                    ) : (
                      <>
                        <span className="material-symbols-outlined text-[12px]">pause_circle</span>
                        Inactive
                      </>
                    )}
                  </span>
                </div>
              </div>
            ))}
          </section>

          <section className="grid grid-cols-1 lg:grid-cols-3 gap-8">
            <div className="lg:col-span-2 bg-[#211e29] p-8 rounded-sm">
              <div className="flex justify-between items-center mb-8">
                <h4 className="text-base font-semibold tracking-tight">Recent Detections</h4>
              </div>
              <div className="space-y-6">
                {recentDetections.length > 0 ? recentDetections.map((evt, i) => (
                  <div key={i} className="flex items-start gap-4">
                    <div className={`w-2 h-2 rounded-full mt-2 flex-shrink-0 ${severityDot(evt.severity)}`}></div>
                    <div className="flex-1 min-w-0">
                      <p className="text-xs text-[#e7dff0]">
                        <span className="font-semibold text-white">{evt.rule_name}</span>{' '}
                        {evt.message}
                      </p>
                      <p className="text-[10px] text-zinc-500 mt-1 uppercase tracking-tight">
                        {formatTs(evt.detected_at)} • Agent: {evt.agent_id}
                      </p>
                    </div>
                  </div>
                )) : (
                  <p className="text-xs text-zinc-500">No detections yet. Security events detected by rule MVs will appear here.</p>
                )}
              </div>
            </div>

            <div className="flex flex-col gap-6">
              <div className="bg-gradient-to-br from-[#2c2834] to-[#211e29] p-8 rounded-sm relative overflow-hidden group">
                <div className="absolute top-0 left-0 w-1 h-full bg-[#ffb0cf]"></div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest font-semibold mb-4">Total Detections (24h)</p>
                <h5 className="text-3xl font-bold text-white">{totalDetections.toLocaleString()}</h5>
                <p className="text-xs text-zinc-500 mt-2 font-medium">from active rule MVs</p>
              </div>
              <div className="bg-[#2c2834] p-8 rounded-sm border-l border-[#574149]/20">
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest font-semibold mb-4">Active Rule Density</p>
                <h5 className="text-3xl font-bold text-white">{density}<span className="text-base text-zinc-500">%</span></h5>
                <div className="w-full bg-[#0f0c17] h-1.5 mt-6 rounded-full overflow-hidden">
                  <div className="bg-[#ffb0cf] h-full transition-all duration-500" style={{ width: `${density}%` }}></div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/frontend && npm run build 2>&1 | tail -5
```

Expected: `built in Xs` — no errors. (The `/rules/:id` route won't work yet until Task 11, but RulePacks.jsx itself is valid.)

- [ ] **Step 3: Commit**

```bash
cd /Users/gangtao/Code/timeplus/agentshield
git add frontend/src/pages/RulePacks.jsx
git commit -m "feat(RulePacks): fix field names to detections_24h, add View Details link to /rules/:id"
```

---

## Task 10: Create RuleDetail.jsx

**Files:**
- Create: `frontend/src/pages/RuleDetail.jsx`

- [ ] **Step 1: Create the file**

```jsx
import { useState, useEffect } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import { api } from '../api/client'

const RULE_ICONS = {
  'rp-001': 'security_update_good',
  'rp-002': 'data_loss_prevention',
  'rp-003': 'admin_panel_settings',
  'rp-004': 'account_tree',
}

function severityBadge(severity) {
  if (severity === 'critical') return 'bg-[#ffb0cf]/10 text-[#ffb0cf] border border-[#ffb0cf]/20'
  if (severity === 'warning') return 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
  return 'bg-zinc-700/40 text-zinc-400 border border-zinc-600/20'
}

function formatTs(ts) {
  if (!ts) return '—'
  try { return new Date(ts.replace(' ', 'T')).toLocaleString() } catch { return ts }
}

export default function RuleDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [rule, setRule] = useState(null)
  const [recentDetections, setRecentDetections] = useState([])
  const [detections24h, setDetections24h] = useState(0)
  const [loading, setLoading] = useState(true)
  const [copied, setCopied] = useState(false)
  const [uninstallConfirm, setUninstallConfirm] = useState(false)
  const [actionLoading, setActionLoading] = useState(false)

  useEffect(() => {
    api.getRule(id)
      .then(data => {
        setRule(data)
        setRecentDetections(data.recent_detections || [])
        setDetections24h(data.detections_24h || 0)
      })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [id])

  const handleToggle = async () => {
    if (!rule || actionLoading) return
    setActionLoading(true)
    try {
      await api.toggleRule(id)
      setRule(prev => ({ ...prev, enabled: !prev.enabled }))
    } catch (e) {
      console.error('Toggle failed', e)
    } finally {
      setActionLoading(false)
    }
  }

  const handleUpgrade = async () => {
    if (!rule || actionLoading) return
    setActionLoading(true)
    try {
      await api.upgradeRule(id, rule.query_sql)
      setRule(prev => ({ ...prev, version: prev.latest_version, latest_version: undefined }))
    } catch (e) {
      console.error('Upgrade failed', e)
    } finally {
      setActionLoading(false)
    }
  }

  const handleUninstall = async () => {
    if (!rule || actionLoading) return
    setActionLoading(true)
    try {
      await api.uninstallRule(id)
      navigate('/rules')
    } catch (e) {
      console.error('Uninstall failed', e)
      setActionLoading(false)
      setUninstallConfirm(false)
    }
  }

  const copySQL = () => {
    if (!rule?.query_sql) return
    navigator.clipboard.writeText(rule.query_sql)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  if (loading) {
    return (
      <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
        <Sidebar /><TopNav /><Footer />
        <main className="pl-64 pt-16 pb-8 min-h-screen">
          <div className="p-6 lg:p-10 space-y-6">
            {[1,2,3].map(i => <div key={i} className="bg-[#1d1a25] rounded-sm h-32 animate-pulse" />)}
          </div>
        </main>
      </div>
    )
  }

  if (!rule) {
    return (
      <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
        <Sidebar /><TopNav /><Footer />
        <main className="pl-64 pt-16 pb-8 min-h-screen flex items-center justify-center">
          <p className="text-zinc-500">Rule not found.</p>
        </main>
      </div>
    )
  }

  const icon = RULE_ICONS[id] || 'shield'

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar /><TopNav /><Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 space-y-8">

          {/* Back link */}
          <Link to="/rules" className="inline-flex items-center gap-1 text-xs text-zinc-500 hover:text-white transition-colors uppercase tracking-widest font-semibold">
            <span className="material-symbols-outlined text-[14px]">arrow_back</span>
            Rule Packs
          </Link>

          {/* Header row */}
          <div className="flex items-start justify-between gap-6 flex-wrap">
            <div className="flex items-center gap-4">
              <div className={`w-14 h-14 bg-[#0f0c17] flex items-center justify-center rounded-sm ${rule.enabled ? 'text-[#ffb0cf]' : 'text-zinc-500'}`}>
                <span className="material-symbols-outlined text-[28px]" style={rule.enabled ? { fontVariationSettings: "'FILL' 1" } : {}}>{icon}</span>
              </div>
              <div>
                <h1 className="text-2xl font-bold text-white tracking-tight">{rule.name}</h1>
                <div className="flex items-center gap-3 mt-1">
                  <span className="text-xs text-zinc-500">
                    <span className="material-symbols-outlined text-[12px] text-[#ffb0cf] align-middle" style={{ fontVariationSettings: "'FILL' 1" }}>bolt</span>
                    {' '}{rule.version}
                  </span>
                  <span className={`text-[10px] px-2 py-0.5 rounded-full font-semibold uppercase tracking-wide ${severityBadge(rule.severity)}`}>
                    {rule.severity}
                  </span>
                </div>
              </div>
            </div>

            {/* Action buttons */}
            <div className="flex items-center gap-3 flex-wrap">
              <button
                onClick={handleToggle}
                disabled={actionLoading}
                className={`px-4 py-2 text-xs font-semibold rounded-sm transition-colors disabled:opacity-50 ${
                  rule.enabled
                    ? 'bg-zinc-700 text-white hover:bg-zinc-600'
                    : 'bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023]'
                }`}
              >
                {rule.enabled ? 'Disable' : 'Enable'}
              </button>

              {rule.latest_version && (
                <button
                  onClick={handleUpgrade}
                  disabled={actionLoading}
                  className="px-4 py-2 text-xs font-semibold rounded-sm border border-[#ffb0cf]/30 text-[#ffb0cf] hover:bg-[#ffb0cf]/10 transition-colors disabled:opacity-50"
                >
                  Upgrade to {rule.latest_version}
                </button>
              )}

              {!uninstallConfirm ? (
                <button
                  onClick={() => setUninstallConfirm(true)}
                  disabled={actionLoading}
                  className="px-4 py-2 text-xs font-semibold rounded-sm border border-red-500/30 text-red-400 hover:bg-red-500/10 transition-colors disabled:opacity-50"
                >
                  Uninstall
                </button>
              ) : (
                <div className="flex items-center gap-2">
                  <span className="text-xs text-red-400">Are you sure?</span>
                  <button
                    onClick={handleUninstall}
                    disabled={actionLoading}
                    className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-red-500/20 text-red-400 hover:bg-red-500/30 transition-colors"
                  >
                    Confirm
                  </button>
                  <button
                    onClick={() => setUninstallConfirm(false)}
                    className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors"
                  >
                    Cancel
                  </button>
                </div>
              )}
            </div>
          </div>

          {/* Rule Definition */}
          <div className="bg-[#1d1a25] p-8 rounded-sm">
            <h2 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest mb-6">Rule Definition</h2>
            <div className="grid grid-cols-2 md:grid-cols-3 gap-6">
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Rule ID</p>
                <p className="text-sm font-mono text-white">{rule.id}</p>
              </div>
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Version</p>
                <p className="text-sm text-white">{rule.version}</p>
              </div>
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Category</p>
                <p className="text-sm text-white capitalize">{rule.category?.replace(/_/g, ' ')}</p>
              </div>
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Severity</p>
                <p className={`text-sm font-semibold capitalize ${rule.severity === 'critical' ? 'text-[#ffb0cf]' : 'text-amber-400'}`}>
                  {rule.severity}
                </p>
              </div>
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">False Positive Rate</p>
                <p className="text-sm text-white capitalize">{rule.fp_rate}</p>
              </div>
              <div>
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Hook Sources</p>
                <p className="text-xs font-mono text-zinc-300">{rule.hook_sources}</p>
              </div>
              {rule.required_telemetry && (
                <div className="col-span-2 md:col-span-3">
                  <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Required Telemetry</p>
                  <span className="text-xs bg-amber-500/10 text-amber-400 border border-amber-500/20 px-2 py-0.5 rounded-full">
                    {rule.required_telemetry}
                  </span>
                </div>
              )}
              <div className="col-span-2 md:col-span-3">
                <p className="text-[10px] text-zinc-500 uppercase tracking-widest mb-1">Description</p>
                <p className="text-sm text-zinc-300">{rule.description}</p>
              </div>
            </div>
          </div>

          {/* Detection SQL */}
          <div className="bg-[#1d1a25] p-8 rounded-sm">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest">Detection SQL</h2>
              <button
                onClick={copySQL}
                className="flex items-center gap-1.5 text-xs text-zinc-400 hover:text-white transition-colors"
              >
                <span className="material-symbols-outlined text-[14px]">{copied ? 'check' : 'content_copy'}</span>
                {copied ? 'Copied' : 'Copy'}
              </button>
            </div>
            <pre className="text-xs font-mono text-zinc-300 bg-[#0f0c17] p-6 rounded-sm overflow-x-auto whitespace-pre leading-relaxed">
              {rule.query_sql}
            </pre>
          </div>

          {/* Recent Detections */}
          <div className="bg-[#1d1a25] p-8 rounded-sm">
            <div className="flex items-center justify-between mb-6">
              <h2 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest">
                Recent Detections
                <span className="ml-2 text-[10px] text-zinc-500 normal-case">(24h: {detections24h})</span>
              </h2>
            </div>
            {recentDetections.length === 0 ? (
              <p className="text-xs text-zinc-500">No detections for this rule yet.</p>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-xs">
                  <thead>
                    <tr className="text-[10px] text-zinc-500 uppercase tracking-widest border-b border-zinc-800">
                      <th className="text-left pb-3 pr-4 font-medium">Time</th>
                      <th className="text-left pb-3 pr-4 font-medium">Agent</th>
                      <th className="text-left pb-3 pr-4 font-medium">Hook</th>
                      <th className="text-left pb-3 pr-4 font-medium">Message</th>
                      <th className="text-left pb-3 font-medium">Severity</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-zinc-800/50">
                    {recentDetections.map((evt, i) => (
                      <tr key={i} className="hover:bg-[#2c2834]/50">
                        <td className="py-3 pr-4 text-zinc-400 whitespace-nowrap">{formatTs(evt.detected_at)}</td>
                        <td className="py-3 pr-4 font-mono text-zinc-300">{evt.agent_id}</td>
                        <td className="py-3 pr-4 text-zinc-400">{evt.hook_name}</td>
                        <td className="py-3 pr-4 text-zinc-300 max-w-xs truncate">{evt.message}</td>
                        <td className="py-3">
                          <span className={`text-[10px] px-2 py-0.5 rounded-full font-semibold uppercase tracking-wide ${severityBadge(evt.severity)}`}>
                            {evt.severity}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>

        </div>
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
cd /Users/gangtao/Code/timeplus/agentshield
git add frontend/src/pages/RuleDetail.jsx
git commit -m "feat(RuleDetail): add rule detail page with metadata, SQL viewer, detections table, and actions"
```

---

## Task 11: Update App.jsx Routing

**Files:**
- Modify: `frontend/src/App.jsx`

- [ ] **Step 1: Add import and route**

Add at the top with other imports:
```jsx
import RuleDetail from './pages/RuleDetail'
```

Add inside `<Routes>` after the `/rules` route:
```jsx
<Route path="/rules/:id" element={<RuleDetail />} />
```

- [ ] **Step 2: Verify full build**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/frontend && npm run build 2>&1 | tail -5
```

Expected: `built in Xs` — fully clean build.

- [ ] **Step 3: Verify backend**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend && CGO_ENABLED=0 go build ./...
```

Expected: clean — zero errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/gangtao/Code/timeplus/agentshield
git add frontend/src/App.jsx
git commit -m "feat(routing): add /rules/:id route for RuleDetail page"
```

---

## Implementation Notes

**ToggleRule INSERT SELECT:** `INSERT INTO agentshield_rules SELECT ... NOT arg_max(enabled, ...) ...` reads current state and flips `enabled` in one operation. This is idiomatic for append-only state in Proton — no separate read needed.

**Seeding check:** `SELECT count() FROM table(agentshield_rules)` uses `table()` to read all historical rows (not streaming). If count > 0, skip seeding. Safe across server restarts.

**SYSTEM RESUME idempotency:** Resuming an already-running MV is a no-op in Proton. The startup sync safely calls RESUME for all enabled rules without checking current running state.

**Legacy MV migration:** The old MVs (`mv_security_prompt_injection` etc.) write to the same `agentshield_security_events` stream. They continue writing until `dropLegacyMVs` runs — placing this before `startupSyncMVs` prevents duplicate detections during the first restart after upgrade.

**AlertWebhook fan-out:** The handler currently logs and returns 200. Slack/PagerDuty fan-out (via settings integrations) is a follow-up task.
