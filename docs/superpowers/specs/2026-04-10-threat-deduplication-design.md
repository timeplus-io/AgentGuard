# Threat Deduplication Design

## Goal

Deduplicate security threats so that repeated rule matches for the same `(agent_id, deployment_id, session_id, rule_id)` produce a single aggregated threat with a running event count, rather than a flood of duplicate rows.

## Architecture

Two Proton streams work in tandem:

- `agentshield_security_events` — append-only, one row per rule match, unchanged
- `agentshield_threats` — **coalesced mutable stream** with two independent column families, updated by two different writers that never interfere with each other:
  - **CF1 `cf_events`** (`rule_name`, `severity`, `last_seen`) — written continuously by a simple MV that pipes each new security event in as an upsert; no aggregation
  - **CF2 `cf_status`** (`status`, `status_updated_at`) — written only by user actions (acknowledge / clear) and the re-open logic

`GET /api/threats` runs two queries: one snapshot read from `TABLE(agentshield_threats)` for identity + status + last_seen, and one aggregation query on `TABLE(agentshield_security_events)` for `event_count` and `first_seen`. Results are merged in Go by the composite key before returning.

## Tech Stack

Go/Gin backend, Timeplus Proton (append-only stream + coalesced mutable stream + MV), React frontend.

---

## Data Model

### `agentshield_security_events` (unchanged)

Append-only stream. One row per rule MV match. No changes.

Schema (existing): `detected_at`, `rule_id`, `rule_name`, `severity`, `agent_id`, `deployment_id`, `session_id`, `run_id`, `hook_name`, `tool_name`, `message`, `event_data`.

### `agentshield_threats` (new, coalesced mutable stream)

Primary key: `(agent_id, deployment_id, session_id, rule_id)`. Two column families allow the MV and user actions to update their respective columns independently without overwriting each other.

`threat_id` is not stored — it is computed in Go from the four PK fields and used only as the API path parameter.

| Column | Type | Family | Writer |
|--------|------|--------|--------|
| `agent_id` | STRING | PK | — |
| `deployment_id` | STRING | PK | — |
| `session_id` | STRING | PK | — |
| `rule_id` | STRING | PK | — |
| `rule_name` | STRING | `cf_events` | MV |
| `severity` | STRING | `cf_events` | MV |
| `last_seen` | DATETIME64(3) | `cf_events` | MV |
| `status` | STRING | `cf_status` | User/re-open |
| `status_updated_at` | DATETIME64(3) | `cf_status` | User/re-open |

DDL:
```sql
CREATE STREAM IF NOT EXISTS agentshield_threats (
    agent_id           string,
    deployment_id      string,
    session_id         string,
    rule_id            string,
    rule_name          string,
    severity           string,
    last_seen          datetime64(3),
    status             string,
    status_updated_at  datetime64(3),
    FAMILY cf_events(rule_name, severity, last_seen),
    FAMILY cf_status(status, status_updated_at)
) PRIMARY KEY (agent_id, deployment_id, session_id, rule_id)
SETTINGS coalesced = true
ENGINE = MutableStream
```

### Materialized View `mv_threats`

Pipes each new security event directly into `agentshield_threats` as a CF1 upsert. No aggregation. The mutable stream PK handles deduplication — each new event for the same key overwrites `cf_events` with the latest values. Does not touch `cf_status`.

```sql
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_threats
INTO agentshield_threats (agent_id, deployment_id, session_id, rule_id, rule_name, severity, last_seen)
AS
SELECT
    agent_id,
    deployment_id,
    session_id,
    rule_id,
    rule_name,
    severity,
    detected_at AS last_seen
FROM agentshield_security_events
```

Each incoming security event upserts `cf_events` for that threat key — `last_seen` advances to the latest `detected_at`, `rule_name` and `severity` reflect the latest event. `cf_status` is untouched.

### `threat_id` computation in Go

Used only as API path parameter, never stored:

```go
func threatID(agentID, deploymentID, sessionID, ruleID string) string {
    h := md5.Sum([]byte(agentID + "-" + deploymentID + "-" + sessionID + "-" + ruleID))
    return hex.EncodeToString(h[:])
}
```

### Go model (`internal/models/models.go`)

Replace the existing `Threat` struct:

```go
type ThreatEvent struct {
    DetectedAt string `json:"detected_at"`
    RunID      string `json:"run_id"`
    HookName   string `json:"hook_name"`
    ToolName   string `json:"tool_name"`
    Message    string `json:"message"`
    EventData  string `json:"event_data"`
}

type Threat struct {
    ThreatID        string       `json:"threat_id"`        // computed, not stored
    AgentID         string       `json:"agent_id"`
    DeploymentID    string       `json:"deployment_id"`
    SessionID       string       `json:"session_id"`
    RuleID          string       `json:"rule_id"`
    RuleName        string       `json:"rule_name"`
    Severity        string       `json:"severity"`
    EventCount      int64        `json:"event_count"`      // from aggregation query
    FirstSeen       string       `json:"first_seen"`        // from aggregation query
    LastSeen        string       `json:"last_seen"`          // from agentshield_threats cf_events
    Status          string       `json:"status"`            // "open", "acknowledged", "cleared"
    StatusUpdatedAt string       `json:"status_updated_at,omitempty"`
    Events          []ThreatEvent `json:"events"`           // all raw matching events, newest first
}
```

`ThreatListResponse` stays the same shape (`threats`, `total`, `page`, `limit`).

---

## Status Flow

Statuses and their meaning:

| Status | Meaning |
|--------|---------|
| `open` | Security event detected; user has not yet reviewed it. Also the default when `status` is empty (CF2 not yet written). |
| `acknowledged` | User has reviewed and acknowledged the threat |
| `cleared` | User confirms the threat has been resolved |

```
Rule MV fires
      │
      ▼
agentshield_security_events (append-only)
      │
      ▼
mv_threats_aggregation (MV)
      │  upserts cf_events only
      ▼
agentshield_threats (coalesced mutable)
      │                        │
  cf_events                cf_status
  (event_count,            (status,
   first_seen,              status_updated_at)
   last_seen, ...)          written by user/re-open only
```

Status transitions:

```
  (first MV upsert → status='')
           │
           ▼  treated as 'open' by backend
         open
    (user acknowledges)
           │
           ▼
      acknowledged
    (user clears)
           │
           ▼
        cleared
    (new event arrives → last_seen > status_updated_at)
           │
           ▼  backend auto-upserts cf_status → 'open'
         open  ← re-opened
```

- **`status=''` (empty default)** → backend normalises to `'open'`; CF2 not written until user acts
- **Acknowledge** → upsert CF2: `status='acknowledged'`, `status_updated_at=now()`
- **Clear** → upsert CF2: `status='cleared'`, `status_updated_at=now()`
- **Re-open** → detected at read time when `status='cleared'` and `last_seen > status_updated_at`; backend upserts CF2: `status='open'`, `status_updated_at=now()`

---

## Backend Changes

### Stream and MV creation (`internal/timeplus/init.go`)

Add to `Init()`:
1. Create `agentshield_threats` coalesced mutable stream (DDL above)
2. Create `mv_threats` MV (DDL above)

Both are `IF NOT EXISTS` — safe to re-run on restart.

### `GET /api/threats` handler (`internal/handlers/threats.go`)

Replace the current mock-data + Proton streaming handler with two parallel snapshot queries merged in Go:

**Query 1** — threat identity + status from `agentshield_threats`:
```sql
SELECT
    agent_id, deployment_id, session_id, rule_id,
    rule_name, severity, last_seen,
    status, status_updated_at
FROM TABLE(agentshield_threats)
```

**Query 2** — event count and first seen from `agentshield_security_events`:
```sql
SELECT
    agent_id, deployment_id, session_id, rule_id,
    count()          AS event_count,
    min(detected_at) AS first_seen
FROM TABLE(agentshield_security_events)
GROUP BY agent_id, deployment_id, session_id, rule_id
```

**Query 3** — all raw matching events from `agentshield_security_events`, newest first:
```sql
SELECT
    agent_id, deployment_id, session_id, rule_id,
    detected_at, run_id, hook_name, tool_name, message, event_data
FROM TABLE(agentshield_security_events)
ORDER BY detected_at DESC
```

Backend logic:
1. Run all three queries concurrently with goroutines
2. Build a map from Query 2 keyed by `(agent_id, deployment_id, session_id, rule_id)` → `{event_count, first_seen}`
3. Build a map from Query 3 keyed by `(agent_id, deployment_id, session_id, rule_id)` → `[]ThreatEvent` (already newest-first from ORDER BY)
4. For each row from Query 1: merge `event_count`, `first_seen`, and `events` from the maps
5. For each row: if `status == ""`, set `status = "open"`
6. For each row: if `status == "cleared"` and `last_seen > status_updated_at`, set `status = "open"`, collect for re-open upserts
7. Batch-upsert re-opened threats back to `agentshield_threats` CF2 (fire-and-forget, non-blocking)
8. Compute `threat_id` for each row
9. Sort by `last_seen` DESC; apply `?severity=`, `?status=`, `?page=`, `?limit=` filters
10. Return `ThreatListResponse`

### `POST /api/threats/:id/acknowledge`

`:id` is the computed `threat_id` (routing only). Upsert CF2 only:

```sql
INSERT INTO agentshield_threats
    (agent_id, deployment_id, session_id, rule_id, status, status_updated_at)
VALUES (?, ?, ?, ?, 'acknowledged', now64(3))
```

Request body (client sends full identity to avoid read-before-write):
```json
{
  "agent_id": "...",
  "deployment_id": "...",
  "session_id": "...",
  "rule_id": "..."
}
```

### `POST /api/threats/:id/clear`

Same as acknowledge but `status='cleared'`.

### Route registration (`cmd/server/main.go`)

```go
api.GET("/threats", handlers.ListThreats(cfg))
api.POST("/threats/:id/acknowledge", handlers.AcknowledgeThreat(cfg))
api.POST("/threats/:id/clear", handlers.ClearThreat(cfg))
```

`ListThreats` gains a `cfg` parameter (currently takes none — uses mock data).

---

## Frontend Changes

### `api/client.js`

Replace the Proton streaming call for threats with REST calls:

```js
getThreats(params = {})          // GET /api/threats?severity=&status=&page=&limit=
acknowledgeThreat(id, body)      // POST /api/threats/:id/acknowledge
clearThreat(id, body)            // POST /api/threats/:id/clear
```

### `pages/Threats.jsx`

- Replace `api.protonQuery()` streaming with `api.getThreats()` polling every 30 seconds
- Add `status` filter tab alongside existing severity/category filters (`All`, `Open`, `Acknowledged`, `Cleared`)
- Per-threat row: action buttons contextual to current status:
  - `open` → show **Acknowledge** and **Clear** buttons
  - `acknowledged` → show **Clear** button only
  - `cleared` → no action buttons (re-open is automatic)
- Status badge alongside severity badge: `open` (blue), `acknowledged` (yellow), `cleared` (green)
- Optimistic UI: immediately update local state on action, then re-fetch

---

## Error Handling

- If `agentshield_threats` query fails, return 502 — no fallback to mock, this is live data
- If `agentshield_threats` stream does not yet exist (first boot race), return empty list with a warning log
- Acknowledge/clear: if Proton upsert fails, return 502; frontend shows error toast
- Re-open upsert failure: log warning, do not fail the GET request — the re-open will be detected again on next read

---

## Out of Scope

- Real-time push (SSE/WebSocket) for threat updates — polling every 30s is sufficient
- Bulk acknowledge/clear
- Threat assignment to users
- Auto-close when session ends (status is user-driven only)
