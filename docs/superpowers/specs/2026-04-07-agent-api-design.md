# Agent API — Implementation Design

**Goal:** Replace the mock `GET /api/agents` and `GET /api/agents/:id` endpoints with real data queried from the `agentshield_hook_events` Timeplus stream, and extend the stream + plugin to carry `deployment_id` / `deployment_name` so AgentShield can distinguish agents across multiple OpenClaw deployments.

**Architecture:** Direct-query approach — each API request runs a `GROUP BY` SQL query against the Proton HTTP endpoint. A new thin Proton client package in the backend handles the HTTP call. No caching layer. No backward compatibility with the old stream schema — stream is recreated.

**Tech Stack:** Go/Gin backend, Timeplus Proton (ClickHouse HTTP interface), TypeScript OpenClaw plugin, Python test runner.

---

## 1. Stream Schema Extension

Add two new columns to `agentshield_hook_events`. The old stream must be dropped **manually once** before deploying this version — this is a one-time operator action, not done in code. After that, the backend creates it with `CREATE STREAM IF NOT EXISTS` on startup as before.

### Updated DDL

```sql
CREATE STREAM IF NOT EXISTS agentshield_hook_events
(
  hook_name        low_cardinality(string),
  event_time       datetime64(3),
  session_id       string,
  run_id           string,
  agent_id         string,
  deployment_id    low_cardinality(string),
  deployment_name  low_cardinality(string),
  session_key      string,
  tool_name        string,
  provider         string,
  model            string,
  hook_decision    low_cardinality(string),
  block_reason     string,
  event_data       string
)
```

The `ddlAgentShieldHookEvents` constant in `backend/internal/timeplus/init.go` is updated to include the two new columns. `Init()` continues to use `CREATE STREAM IF NOT EXISTS` — no drop logic is added to code.

---

## 2. Plugin Changes

### 2a. `config.ts`

Add `deploymentId` and `deploymentName` to `AgentShieldConfig`. These are config-only — no env var overrides. Set once at plugin installation time via `openclaw.json`.

```typescript
export type AgentShieldConfig = {
  timeplusUrl:    string
  stream:         string
  flushMs:        number
  batchSize:      number
  username:       string
  password:       string
  deploymentId:   string   // new — default: "default"
  deploymentName: string   // new — default: "Default"
}

const DEFAULTS: AgentShieldConfig = {
  // ... existing ...
  deploymentId:   "default",
  deploymentName: "Default",
}
```

`resolveConfig()` reads both from `pluginConfig?.deploymentId` / `pluginConfig?.deploymentName` with fallback to DEFAULTS. No env var path.

### 2b. `hooks.ts`

`baseEvent()` stamps `deployment_id` and `deployment_name` on every `HookEvent`. Both fields are passed into `baseEvent()` from the `AgentShieldConfig` held in the plugin closure.

### 2c. `timeplus-client.ts`

`HookEvent` type gains `deployment_id: string` and `deployment_name: string`. The `COLUMNS` array is updated to include both (inserted after `agent_id`, before `session_key`). The ingest payload columns and row ordering must match exactly.

### 2d. `openclaw.plugin.json` configSchema

```json
"deploymentId":   { "type": "string" },
"deploymentName": { "type": "string" }
```

### 2e. `.openclaw/openclaw.json` plugin config

```json
"agentshield": {
  "enabled": true,
  "config": {
    "timeplusUrl":    "http://host.docker.internal:3218",
    "stream":         "agentshield_hook_events",
    "flushMs":        2000,
    "batchSize":      50,
    "username":       "proton",
    "password":       "timeplus@t+",
    "deploymentId":   "local",
    "deploymentName": "Local Dev"
  }
}
```

### 2f. Makefile `configure` target

Step 6 (agentshield plugin config block) is updated to write `deploymentId: "local"` and `deploymentName: "Local Dev"` into the generated config. Operators override these to match their environment when deploying.

---

## 3. Backend Changes

### 3a. New Proton client — `backend/internal/timeplus/client.go`

A thin package-level client that POSTs SQL to the Proton HTTP interface.

```go
package timeplus

// Config read from env at startup:
//   PROTON_URL      (default: http://localhost:3218)
//   PROTON_USER     (default: proton)
//   PROTON_PASSWORD (default: timeplus@t+)

type Row = map[string]any

func Query(sql string) ([]Row, error)
```

`Query` appends `FORMAT JSON` to the SQL string, POSTs to `$PROTON_URL/` with HTTP Basic auth, parses the `{"data": [...]}` response, and returns rows as `[]map[string]any`. Returns a wrapped error on non-2xx or parse failure.

### 3b. Updated `Agent` struct — `backend/internal/models/models.go`

Replace the existing `Agent` struct (which had mock fields like `HealthIndex`, `Region`, `InstanceType`) with:

```go
type Agent struct {
    AgentID        string    `json:"agent_id"`
    DeploymentID   string    `json:"deployment_id"`
    DeploymentName string    `json:"deployment_name"`
    LastSeen       time.Time `json:"last_seen"`
    EventCount     int       `json:"event_count"`
    SessionCount   int       `json:"session_count"`
}
```

`AgentListResponse` remains: `Agents []Agent`, `Total int`, `Page int`, `Limit int`.

### 3c. Updated agents handler — `backend/internal/handlers/agents.go`

**`ListAgents` (`GET /api/agents`):**

Runs:
```sql
SELECT
    agent_id,
    deployment_id,
    deployment_name,
    max(event_time)              AS last_seen,
    count()                      AS event_count,
    count(DISTINCT session_key)  AS session_count
FROM table(agentshield_hook_events)
GROUP BY agent_id, deployment_id, deployment_name
ORDER BY last_seen DESC
```

Applies `?page=` / `?limit=` pagination in Go by slicing the result slice. Returns 500 with an error message if Proton is unreachable.

**`GetAgent` (`GET /api/agents/:id`):**

The `:id` path parameter uses the format `<deploymentId>__<agentId>` (double-underscore separator, chosen because `agent_id` values like `main` and `deployment_id` values like `prod-eu` never contain `__`). Splits on `__`, then runs:

```sql
SELECT
    agent_id,
    deployment_id,
    deployment_name,
    max(event_time)              AS last_seen,
    count()                      AS event_count,
    count(DISTINCT session_key)  AS session_count
FROM table(agentshield_hook_events)
WHERE deployment_id = '<deploymentId>'
  AND agent_id      = '<agentId>'
GROUP BY agent_id, deployment_id, deployment_name
LIMIT 1
```

Returns 404 if no rows. Returns 400 if `:id` does not contain `__`.

All mock data imports are removed from `agents.go`. The `mock` package import is dropped from this file only (other handlers still use it).

### 3d. Fix investigate table metadata — `backend/internal/handlers/investigate.go`

The `hook_events` entry in `availableTables` is updated to list the real columns:

```
hook_name, event_time, session_id, run_id, agent_id,
deployment_id, deployment_name, session_key, tool_name,
provider, model, hook_decision, block_reason, event_data
```

---

## 4. Test Runner

`tests/run_scenarios.py` reads Proton credentials (`timeplusUrl`, `username`, `password`) from the plugin config block in `.openclaw/openclaw.json` — the same way `OPENCLAW_TOKEN` is already read. Removes the three hardcoded constants at the top of the file.

---

## File Map

| File | Action |
|---|---|
| `backend/internal/timeplus/init.go` | Update DDL constant; drop+recreate stream |
| `backend/internal/timeplus/client.go` | **Create** — Proton HTTP client |
| `backend/internal/models/models.go` | Replace `Agent` struct |
| `backend/internal/handlers/agents.go` | Replace mock with Proton queries |
| `backend/internal/handlers/investigate.go` | Fix `hook_events` column list |
| `agents/openclaw/agentshield-plugin/src/config.ts` | Add `deploymentId`, `deploymentName` |
| `agents/openclaw/agentshield-plugin/src/hooks.ts` | Stamp fields in `baseEvent()` |
| `agents/openclaw/agentshield-plugin/src/timeplus-client.ts` | Add columns to `HookEvent` + `COLUMNS` |
| `agents/openclaw/agentshield-plugin/openclaw.plugin.json` | Extend configSchema |
| `agents/openclaw/.openclaw/openclaw.json` | Add deployment fields to plugin config |
| `agents/openclaw/Makefile` | Update `configure` target |
| `agents/openclaw/tests/run_scenarios.py` | Read Proton credentials from config |

---

## Error Handling

- Proton unreachable → `GET /api/agents` returns HTTP 503 with `{"error": "data store unavailable"}`
- Proton returns non-2xx → same 503
- `:id` malformed (no `__`) → HTTP 400 with `{"error": "invalid agent id format, expected deploymentId__agentId"}`
- Agent not found → HTTP 404

---

## Not In Scope

- Hook event feed or analytics endpoints (future work)
- Caching / in-memory polling
- Dashboard, threats, cost, rules — remain on mock data
- Frontend changes to accommodate new `Agent` fields (the frontend currently reads `agent.id`, `agent.status`, etc. — a separate frontend update is needed to consume `agent_id`, `deployment_id`, `last_seen`, etc.)
