# Rule Packs — Design Spec

**Date:** 2026-04-08  
**Status:** Approved

---

## Overview

Rule packs are pre-built streaming SQL detection rules that continuously monitor `agentshield_hook_events` for security threats. Each rule is stored as metadata in a Proton stream. When enabled, a Materialized View (MV) is created or resumed that writes detected events to a global alert stream. A single Proton ALERT then fans out notifications to the AgentShield UI and configured channels (Slack, PagerDuty, etc.).

---

## Data Architecture

### Streams

#### `agentshield_rules` — Rule metadata (source of truth)

Append-only stream. Toggle and upgrade operations insert new records; current state is resolved with `arg_max`.

```sql
CREATE STREAM agentshield_rules (
  rule_id            low_cardinality(string),
  name               string,
  version            string,
  description        string,
  query_sql          string,          -- full SELECT body for the MV
  enabled            bool,
  deleted            bool,            -- soft-delete via uninstall
  severity           low_cardinality(string),   -- critical / warning / info
  category           low_cardinality(string),   -- prompt_injection / dlp / privilege / supply_chain
  required_telemetry string,          -- e.g. "content_capture" or ""
  fp_rate            low_cardinality(string),   -- low / medium / high
  hook_sources       string,          -- comma-separated hook names for display
  updated_at         datetime64(3)
)
```

Current state query (used at startup and by ListRules API):

```sql
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  arg_max(enabled,            updated_at) AS enabled,
  arg_max(deleted,            updated_at) AS deleted,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources
FROM table(agentshield_rules)
GROUP BY rule_id
HAVING arg_max(deleted, updated_at) = false
```

#### `agentshield_security_events` — Global alert stream

Written to by each enabled rule's MV. Queried by the UI for enforcement log and block counts.

```sql
CREATE STREAM agentshield_security_events (
  detected_at   datetime64(3),
  rule_id       low_cardinality(string),
  rule_name     low_cardinality(string),
  severity      low_cardinality(string),
  agent_id      string,
  deployment_id low_cardinality(string),
  session_id    string,
  run_id        string,
  hook_name     low_cardinality(string),
  tool_name     string,
  message       string,
  event_data    string
)
```

### Per-Rule Materialized Views

MV naming convention: `mv_rule_` + `rule_id` with `-` stripped (e.g., `rp-001` → `mv_rule_rp001`).

Each MV is created from the `query_sql` stored in the rule:

```sql
CREATE MATERIALIZED VIEW mv_rule_rp001
INTO agentshield_security_events
AS {query_sql}
```

The `query_sql` is the full `SELECT ... FROM agentshield_hook_events WHERE ...` body. The `INTO` clause is always `agentshield_security_events` and is added by the backend, not stored in the rule.

### Global Alert Dispatcher

One ALERT created at startup. Reads from `agentshield_security_events`, calls a Python UDF that POSTs to the AgentShield API webhook. The API then fans out to configured notification channels.

```sql
CREATE OR REPLACE ALERT agentshield_notifier
  BATCH 1 EVENTS WITH TIMEOUT 5s
  LIMIT 10 ALERTS PER 1m
  CALL notify_agentshield_udf
AS SELECT
  rule_id, rule_name, severity, agent_id, deployment_id,
  session_id, hook_name, tool_name, message, detected_at
FROM agentshield_security_events
```

```sql
CREATE OR REPLACE FUNCTION notify_agentshield_udf(
  rule_id string, rule_name string, severity string,
  agent_id string, deployment_id string, session_id string,
  hook_name string, tool_name string, message string, detected_at string
) RETURNS string LANGUAGE PYTHON AS $$
import json, requests
def notify_agentshield_udf(rule_ids, rule_names, severities,
                            agent_ids, deployment_ids, session_ids,
                            hook_names, tool_names, messages, detected_ats):
    for i in range(len(rule_ids)):
        requests.post("http://agentshield:8080/api/alerts/webhook", json={
            "rule_id": rule_ids[i],
            "rule_name": rule_names[i],
            "severity": severities[i],
            "agent_id": agent_ids[i],
            "message": messages[i],
            "detected_at": detected_ats[i]
        }, timeout=3)
    return rule_ids
$$
```

---

## Rule Lifecycle

### MV state transitions

| Situation | Command |
|---|---|
| First enable (MV does not exist) | `CREATE MATERIALIZED VIEW mv_rule_rp001 INTO agentshield_security_events AS {query_sql}` |
| Disable (MV running) | `SYSTEM PAUSE MATERIALIZED VIEW mv_rule_rp001 PERMANENT` |
| Re-enable (MV paused) | `SYSTEM RESUME MATERIALIZED VIEW mv_rule_rp001 PERMANENT` |
| Upgrade (SQL changed) | Drop MV + create new MV with updated query_sql |
| Uninstall | `DROP VIEW mv_rule_rp001` + insert `deleted=true` record |

Both PAUSE and RESUME use `PERMANENT` so state survives Timeplus restarts.

### Startup sync

On agentshield startup:
1. Query `table(agentshield_rules)` with `arg_max` to get current state of all non-deleted rules
2. For each `enabled=true` rule:
   - If MV does not exist → `CREATE MATERIALIZED VIEW`
   - If MV exists but paused → `SYSTEM RESUME ... PERMANENT`
3. For each `enabled=false` rule:
   - If MV exists and running → `SYSTEM PAUSE ... PERMANENT`
4. Create `agentshield_notifier` ALERT and Python UDF if they do not exist

MV existence is checked via `system.tables` (same `streamExists` helper, MV names appear there).

---

## Data Flow

```
agentshield_hook_events  (source stream)
         │
         ├── mv_rule_rp001  (per rule, created when enabled)
         ├── mv_rule_rp002
         └── ...
                  │
                  ▼
     agentshield_security_events  (global alert stream — full history)
                  │
                  ▼
     ALERT: agentshield_notifier
       BATCH 1 EVENTS WITH TIMEOUT 5s
       LIMIT 10 ALERTS PER 1m
       CALL notify_agentshield_udf
                  │
          POST /api/alerts/webhook
                  │
          ┌───────┴────────────┐
          ▼                    ▼
    AgentShield UI        Slack / PagerDuty
    (enforcement log)     (from settings integrations)
```

---

## Backend API

### Initialization (timeplus.Init)

Creates in order:
1. `agentshield_security_events` stream (if not exists)
2. `agentshield_rules` stream (if not exists), seeded with built-in rule definitions
3. `notify_agentshield_udf` Python UDF (CREATE OR REPLACE)
4. `agentshield_notifier` ALERT (CREATE OR REPLACE)
5. Sync MV states from rule stream (startup sync above)

### Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/rules` | List all non-deleted rules with current `enabled` state and `blocks_24h` from `agentshield_security_events` |
| `GET` | `/api/rules/:id` | Single rule detail: metadata + recent detections from `agentshield_security_events WHERE rule_id = :id` |
| `POST` | `/api/rules/:id/toggle` | Insert `enabled=!current` record → PAUSE or RESUME MV |
| `POST` | `/api/rules/:id/upgrade` | Insert new record with updated `version`+`query_sql` → DROP + CREATE MV |
| `DELETE` | `/api/rules/:id` | Insert `deleted=true` record → DROP VIEW |
| `POST` | `/api/alerts/webhook` | Receives alert from Python UDF; fans out to enabled integrations from settings |

### Models

```go
type RulePack struct {
    ID                string  `json:"id"`
    Name              string  `json:"name"`
    Version           string  `json:"version"`
    Description       string  `json:"description"`
    QuerySQL          string  `json:"query_sql"`
    Enabled           bool    `json:"enabled"`
    Severity          string  `json:"severity"`
    Category          string  `json:"category"`
    RequiredTelemetry string  `json:"required_telemetry"`
    FPRate            string  `json:"fp_rate"`
    HookSources       string  `json:"hook_sources"`
    Blocks24h         int     `json:"blocks_24h"`
}

type RulesResponse struct {
    RulePacks         []RulePack         `json:"rule_packs"`
    TotalBlocks24h    int                `json:"total_blocks_24h"`
    ActiveRuleDensity int                `json:"active_rule_density"`
    RecentEnforcement []EnforcementEvent `json:"recent_enforcement"`
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

---

## Frontend

### Route: `/rules` — Rule Packs list page

**Card grid (4 columns):**
- Icon, toggle (enable/disable)
- Name, description
- `required_telemetry` badge (amber, only shown if non-empty)
- `fp_rate` chip (Low / Medium / High)
- "View Details" link → `/rules/:id`
- Version badge (`⚡ v2.4.1`) or "Inactive" if disabled

**Bottom section:**
- Recent Policy Enforcement log (from `agentshield_security_events`, latest 10 events)
- Total Blocks (24h) stat card
- Active Rule Density stat card with progress bar

### Route: `/rules/:id` — Rule Detail page

**Header:**
- Back link to `/rules`
- Rule name + version
- Action buttons: **Enable/Disable** (primary), **Upgrade to vX.X** (ghost, only if newer available), **Uninstall** (danger, confirmation required)

**Rule Definition panel:**
- Grid of metadata: ID, version, category, severity, telemetry requirement, FP rate, hook sources

**Detection SQL panel:**
- Monospace code block showing `query_sql`
- Copy button

**Recent Detections table:**
- Columns: Time, Agent, Hook, Message, Severity
- Data from `table(agentshield_security_events) WHERE rule_id = :id ORDER BY detected_at DESC LIMIT 50`
- Header shows `(24h: N)` count

### Action behaviors

| Button | API call | Backend effect |
|---|---|---|
| Enable | `POST /api/rules/:id/toggle` | Insert `enabled=true` → RESUME MV PERMANENT |
| Disable | `POST /api/rules/:id/toggle` | Insert `enabled=false` → PAUSE MV PERMANENT |
| Upgrade | `POST /api/rules/:id/upgrade` | Insert new version record → DROP + CREATE MV |
| Uninstall | `DELETE /api/rules/:id` | Insert `deleted=true` → DROP VIEW |

Uninstall requires a confirmation dialog before calling the API.

### Notification channels

No new Settings UI needed. The existing Settings → Integrations section (Slack, PagerDuty) provides the channel config. `POST /api/alerts/webhook` reads enabled integrations from settings and fans out accordingly.

---

## Built-in Rules (seeded at startup)

| ID | Name | Category | Severity | Monitors |
|---|---|---|---|---|
| rp-001 | Prompt Injection Shield | prompt_injection | critical | llm_input, llm_output |
| rp-002 | DLP Sentinel | dlp | critical | tool_result_persist, after_tool_call, llm_output |
| rp-003 | Privilege Guard | privilege | warning | before_tool_call, after_tool_call |
| rp-004 | Supply Chain Watch | supply_chain | warning | before_install, before_tool_call, after_tool_call |

Each rule's `query_sql` is a `SELECT ... FROM agentshield_hook_events WHERE ...` that outputs the `agentshield_security_events` schema columns.
