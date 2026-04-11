# Threat Detail Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a dedicated `/threats/:id` page that replaces the inline expand-row in `ThreatsList`, giving each threat its own URL with a full event card history.

**Architecture:** Extract the shared query/merge logic from `ListThreats` into `buildThreatsList`, add `GetThreat` that filters by computed MD5 ID, wire the new `ThreatDetail` page following the `AgentDetail` pattern (Sidebar + `top-nav` header + `pl-sidebar` main), and redirect row clicks in `ThreatsList` to the new route.

**Tech Stack:** Go/Gin (backend), React 18 + React Router v6 + Tailwind CSS (frontend). No new dependencies.

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/handlers/threats.go` | Extract `buildThreatsList` helper; add `GetThreat` handler; refactor `ListThreats` |
| `backend/cmd/server/main.go` | Register `GET /api/threats/:id` |
| `frontend/src/api/client.js` | Add `getThreat(id)` |
| `frontend/src/components/Badge.jsx` | Export `ThreatStatusBadge` (separate from existing `StatusBadge` for agent states) |
| `frontend/src/components/ThreatsList.jsx` | Remove expand-row; rows navigate to detail page; import `ThreatStatusBadge` from Badge |
| `frontend/src/pages/ThreatDetail.jsx` | New page component |
| `frontend/src/App.jsx` | Add `/threats/:id` route |

---

### Task 1: Extract `buildThreatsList` helper and add `GetThreat`

**Files:**
- Modify: `backend/internal/handlers/threats.go`

The current `ListThreats` runs three concurrent queries, merges the results, and fires a goroutine for auto-reopened threats. Both `ListThreats` and the new `GetThreat` need this logic. Extract it into `buildThreatsList`, then rewrite `ListThreats` to call it.

- [ ] **Step 1: Verify the current backend compiles cleanly**

```bash
cd backend && CGO_ENABLED=0 go build ./...
```

Expected: no output (exit 0).

- [ ] **Step 2: Replace the body of `threats.go` with the refactored version**

Replace the entire content of `backend/internal/handlers/threats.go` with:

```go
package handlers

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	tp "github.com/timeplus/agentshield/internal/timeplus"
)

// threatKey uniquely identifies a deduplicated threat.
type threatKey struct{ agentID, deploymentID, sessionID, ruleID string }

// computeThreatID returns the MD5 hex string of the four key fields.
// Uses length-prefix encoding to prevent hash collisions when field values contain hyphens.
// Used as the API path parameter (:id) — never stored in Proton.
func computeThreatID(agentID, deploymentID, sessionID, ruleID string) string {
	encoded := fmt.Sprintf("%d:%s\x00%d:%s\x00%d:%s\x00%d:%s",
		len(agentID), agentID, len(deploymentID), deploymentID,
		len(sessionID), sessionID, len(ruleID), ruleID)
	h := md5.Sum([]byte(encoded))
	return hex.EncodeToString(h[:])
}

// parseProtonTime parses a datetime64(3) value as returned by the Proton HTTP interface.
// Proton may return "2006-01-02 15:04:05.000 UTC" or "2006-01-02 15:04:05.000 +0000".
func parseProtonTime(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02 15:04:05.000 UTC",
		"2006-01-02 15:04:05.000 MST",
		"2006-01-02 15:04:05.000 -0700",
		"2006-01-02 15:04:05.000",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

const sqlListThreats = `
SELECT agent_id, deployment_id, session_id, rule_id,
       rule_name, severity, last_seen, status, status_updated_at
FROM table(agentshield_threats)`

const sqlThreatStats = `
SELECT agent_id, deployment_id, session_id, rule_id,
       count()          AS event_count,
       min(detected_at) AS first_seen
FROM table(agentshield_security_events)
GROUP BY agent_id, deployment_id, session_id, rule_id`

const sqlThreatEvents = `
SELECT agent_id, deployment_id, session_id, rule_id,
       detected_at, run_id, hook_name, tool_name, message, event_data
FROM table(agentshield_security_events)
ORDER BY detected_at DESC`

// buildThreatsList runs the three Proton snapshot queries concurrently, merges the results,
// applies re-open logic (fires goroutines for cleared threats that have new events),
// and returns all threats sorted by last_seen descending.
// Returns an error if any query fails.
func buildThreatsList(cfg *config.Config) ([]models.Threat, error) {
	type queryResult struct {
		rows []tp.Row
		err  error
	}

	ch1 := make(chan queryResult, 1)
	ch2 := make(chan queryResult, 1)
	ch3 := make(chan queryResult, 1)

	go func() {
		rows, err := tp.Query(&cfg.Timeplus, sqlListThreats)
		ch1 <- queryResult{rows, err}
	}()
	go func() {
		rows, err := tp.Query(&cfg.Timeplus, sqlThreatStats)
		ch2 <- queryResult{rows, err}
	}()
	go func() {
		rows, err := tp.Query(&cfg.Timeplus, sqlThreatEvents)
		ch3 <- queryResult{rows, err}
	}()

	r1, r2, r3 := <-ch1, <-ch2, <-ch3

	if r1.err != nil {
		return nil, r1.err
	}
	if r2.err != nil {
		return nil, r2.err
	}
	if r3.err != nil {
		return nil, r3.err
	}

	// Build stats map from Query 2 (event_count + first_seen per key).
	type statsEntry struct {
		eventCount int64
		firstSeen  string
	}
	statsMap := make(map[threatKey]statsEntry, len(r2.rows))
	for _, row := range r2.rows {
		k := threatKey{
			parseStr(row["agent_id"]),
			parseStr(row["deployment_id"]),
			parseStr(row["session_id"]),
			parseStr(row["rule_id"]),
		}
		statsMap[k] = statsEntry{
			eventCount: parseInt64(row["event_count"]),
			firstSeen:  parseStr(row["first_seen"]),
		}
	}

	// Build events map from Query 3 ([]ThreatEvent per key, newest first from ORDER BY).
	// Cap at 50 events per threat to bound memory usage.
	const maxEventsPerThreat = 50
	eventsMap := make(map[threatKey][]models.ThreatEvent, len(r1.rows))
	for _, row := range r3.rows {
		k := threatKey{
			parseStr(row["agent_id"]),
			parseStr(row["deployment_id"]),
			parseStr(row["session_id"]),
			parseStr(row["rule_id"]),
		}
		if len(eventsMap[k]) >= maxEventsPerThreat {
			continue
		}
		eventsMap[k] = append(eventsMap[k], models.ThreatEvent{
			DetectedAt: parseStr(row["detected_at"]),
			RunID:      parseStr(row["run_id"]),
			HookName:   parseStr(row["hook_name"]),
			ToolName:   parseStr(row["tool_name"]),
			Message:    parseStr(row["message"]),
			EventData:  parseStr(row["event_data"]),
		})
	}

	// Collect threats that need re-opening.
	var toReopen []threatKey

	threats := make([]models.Threat, 0, len(r1.rows))
	for _, row := range r1.rows {
		k := threatKey{
			parseStr(row["agent_id"]),
			parseStr(row["deployment_id"]),
			parseStr(row["session_id"]),
			parseStr(row["rule_id"]),
		}

		status := parseStr(row["status"])
		if status == "" {
			status = "open"
		}
		statusUpdatedAt := parseStr(row["status_updated_at"])
		lastSeen := parseStr(row["last_seen"])

		// Re-open: if cleared but new events arrived after the clear.
		if status == "cleared" && statusUpdatedAt != "" && lastSeen != "" {
			ls, errLS := parseProtonTime(lastSeen)
			su, errSU := parseProtonTime(statusUpdatedAt)
			if errLS == nil && errSU == nil && ls.After(su) {
				status = "open"
				toReopen = append(toReopen, k)
			}
		}

		stats := statsMap[k]
		events := eventsMap[k]
		if events == nil {
			events = []models.ThreatEvent{}
		}

		threats = append(threats, models.Threat{
			ThreatID:        computeThreatID(k.agentID, k.deploymentID, k.sessionID, k.ruleID),
			AgentID:         k.agentID,
			DeploymentID:    k.deploymentID,
			SessionID:       k.sessionID,
			RuleID:          k.ruleID,
			RuleName:        parseStr(row["rule_name"]),
			Severity:        parseStr(row["severity"]),
			EventCount:      stats.eventCount,
			FirstSeen:       stats.firstSeen,
			LastSeen:        lastSeen,
			Status:          status,
			StatusUpdatedAt: statusUpdatedAt,
			Events:          events,
		})
	}

	// Fire-and-forget: upsert re-opened threats back to agentshield_threats CF2.
	if len(toReopen) > 0 {
		go func(keys []threatKey) {
			for _, k := range keys {
				sql := fmt.Sprintf(
					`INSERT INTO agentshield_threats (agent_id, deployment_id, session_id, rule_id, status, status_updated_at) VALUES ('%s', '%s', '%s', '%s', 'open', now64(3))`,
					escapeSQL(k.agentID), escapeSQL(k.deploymentID),
					escapeSQL(k.sessionID), escapeSQL(k.ruleID),
				)
				_ = tp.ExecHTTP(&cfg.Timeplus, sql)
			}
		}(toReopen)
	}

	// Sort by last_seen descending (string comparison works for ISO timestamps).
	for i := 0; i < len(threats)-1; i++ {
		for j := i + 1; j < len(threats); j++ {
			if threats[j].LastSeen > threats[i].LastSeen {
				threats[i], threats[j] = threats[j], threats[i]
			}
		}
	}

	return threats, nil
}

// ListThreats handles GET /api/threats.
// Runs three Proton snapshot queries concurrently, merges results, applies re-open logic.
// Query params: ?severity=, ?status=, ?agent_id=, ?page=, ?limit=
func ListThreats(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		severityFilter := c.Query("severity")
		statusFilter := c.Query("status")
		agentIDFilter := c.Query("agent_id")
		pageStr := c.DefaultQuery("page", "1")
		limitStr := c.DefaultQuery("limit", "20")

		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			limit = 20
		}

		threats, err := buildThreatsList(cfg)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Apply filters.
		filtered := make([]models.Threat, 0, len(threats))
		for _, t := range threats {
			if severityFilter != "" && t.Severity != severityFilter {
				continue
			}
			if statusFilter != "" && t.Status != statusFilter {
				continue
			}
			if agentIDFilter != "" && t.AgentID != agentIDFilter {
				continue
			}
			filtered = append(filtered, t)
		}

		// Paginate.
		total := len(filtered)
		start := (page - 1) * limit
		if start >= total {
			c.JSON(http.StatusOK, models.ThreatListResponse{
				Threats: []models.Threat{},
				Total:   total, Page: page, Limit: limit,
			})
			return
		}
		end := start + limit
		if end > total {
			end = total
		}

		c.JSON(http.StatusOK, models.ThreatListResponse{
			Threats: filtered[start:end],
			Total:   total, Page: page, Limit: limit,
		})
	}
}

// GetThreat handles GET /api/threats/:id.
// Returns the single threat whose computed threat_id matches the :id path parameter.
// Returns 404 if no match, 503 if Proton is unavailable.
func GetThreat(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		threats, err := buildThreatsList(cfg)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		for _, t := range threats {
			if t.ThreatID == id {
				c.JSON(http.StatusOK, t)
				return
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "threat not found"})
	}
}

// ThreatStatusBody is the request body for acknowledge/clear endpoints.
// The client sends the full threat identity to avoid a read-before-write.
type ThreatStatusBody struct {
	AgentID      string `json:"agent_id"       binding:"required"`
	DeploymentID string `json:"deployment_id"   binding:"required"`
	SessionID    string `json:"session_id"      binding:"required"`
	RuleID       string `json:"rule_id"         binding:"required"`
}

func upsertThreatStatus(cfg *config.Config, body ThreatStatusBody, status string) error {
	if status != "acknowledged" && status != "cleared" {
		return fmt.Errorf("invalid status %q", status)
	}
	sql := fmt.Sprintf(
		`INSERT INTO agentshield_threats (agent_id, deployment_id, session_id, rule_id, status, status_updated_at) VALUES ('%s', '%s', '%s', '%s', '%s', now64(3))`,
		escapeSQL(body.AgentID),
		escapeSQL(body.DeploymentID),
		escapeSQL(body.SessionID),
		escapeSQL(body.RuleID),
		escapeSQL(status),
	)
	return tp.ExecHTTP(&cfg.Timeplus, sql)
}

// AcknowledgeThreat handles POST /api/threats/:id/acknowledge.
// Upserts cf_status with status='acknowledged'. :id is the computed threat_id and must match the body.
func AcknowledgeThreat(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		routeID := c.Param("id")
		var body ThreatStatusBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id, deployment_id, session_id, rule_id are required"})
			return
		}
		if computeThreatID(body.AgentID, body.DeploymentID, body.SessionID, body.RuleID) != routeID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "body identity does not match threat id"})
			return
		}
		if err := upsertThreatStatus(cfg, body, "acknowledged"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upsert failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": "acknowledged"})
	}
}

// ClearThreat handles POST /api/threats/:id/clear.
// Upserts cf_status with status='cleared'. :id is the computed threat_id and must match the body.
func ClearThreat(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		routeID := c.Param("id")
		var body ThreatStatusBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id, deployment_id, session_id, rule_id are required"})
			return
		}
		if computeThreatID(body.AgentID, body.DeploymentID, body.SessionID, body.RuleID) != routeID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "body identity does not match threat id"})
			return
		}
		if err := upsertThreatStatus(cfg, body, "cleared"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upsert failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": "cleared"})
	}
}
```

- [ ] **Step 3: Verify the backend compiles cleanly**

```bash
cd backend && CGO_ENABLED=0 go build ./...
```

Expected: no output (exit 0). If it fails, fix the compile error before continuing.

- [ ] **Step 4: Commit**

```bash
cd backend && git add internal/handlers/threats.go
git commit -m "refactor(threats): extract buildThreatsList helper; add GetThreat handler"
```

---

### Task 2: Register `GET /api/threats/:id` route

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add the route registration**

In `backend/cmd/server/main.go`, find the existing threats block:

```go
		api.GET("/threats", handlers.ListThreats(cfg))
		api.POST("/threats/:id/acknowledge", handlers.AcknowledgeThreat(cfg))
		api.POST("/threats/:id/clear", handlers.ClearThreat(cfg))
```

Replace it with:

```go
		api.GET("/threats", handlers.ListThreats(cfg))
		api.GET("/threats/:id", handlers.GetThreat(cfg))
		api.POST("/threats/:id/acknowledge", handlers.AcknowledgeThreat(cfg))
		api.POST("/threats/:id/clear", handlers.ClearThreat(cfg))
```

- [ ] **Step 2: Build and smoke-test**

```bash
cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server
```

In a second terminal:
```bash
curl -s http://localhost:8080/api/threats | python3 -m json.tool | head -20
curl -s http://localhost:8080/api/threats/nonexistent | python3 -m json.tool
```

Expected: first returns `{"threats":[...],"total":...}`. Second returns `{"error":"threat not found"}` with HTTP 404.

Kill the server with Ctrl-C.

- [ ] **Step 3: Commit**

```bash
cd backend && git add cmd/server/main.go
git commit -m "feat(api): register GET /api/threats/:id"
```

---

### Task 3: Add `getThreat` to the frontend API client

**Files:**
- Modify: `frontend/src/api/client.js`

- [ ] **Step 1: Add the method**

In `frontend/src/api/client.js`, find:

```js
  clearThreat: (id, body) => post(`/threats/${id}/clear`, body),
```

Add after it:

```js
  getThreat: (id) => get(`/threats/${id}`),
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/api/client.js
git commit -m "feat(api): add getThreat(id) to frontend API client"
```

---

### Task 4: Add `ThreatStatusBadge` to `Badge.jsx`

**Files:**
- Modify: `frontend/src/components/Badge.jsx`

The existing `StatusBadge` in `Badge.jsx` is for agent connection states (ACTIVE / THROTTLED / SUSPENDED). Threat statuses (open / acknowledged / cleared) need a separate export named `ThreatStatusBadge` to avoid collision. The inline `StatusBadge` in `ThreatsList.jsx` will be deleted and replaced with this export.

- [ ] **Step 1: Append `ThreatStatusBadge` to `Badge.jsx`**

In `frontend/src/components/Badge.jsx`, after the closing brace of the `StatusBadge` export, add:

```jsx
const threatStatusStyles = {
  open:         { label: 'Open',         cls: 'bg-blue-500/10 text-blue-400 border border-blue-400/30' },
  acknowledged: { label: 'Acknowledged', cls: 'bg-yellow-500/10 text-yellow-400 border border-yellow-400/30' },
  cleared:      { label: 'Cleared',      cls: 'bg-green-500/10 text-green-400 border border-green-400/30' },
}

export function ThreatStatusBadge({ status }) {
  const { label, cls } = threatStatusStyles[status] || { label: status, cls: 'bg-zinc-500/10 text-zinc-400 border border-zinc-400/30' }
  return (
    <span className={`text-[10px] font-bold uppercase rounded-full px-2 py-0.5 ${cls}`}>
      {label}
    </span>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/Badge.jsx
git commit -m "feat(badge): add ThreatStatusBadge for open/acknowledged/cleared states"
```

---

### Task 5: Create `ThreatDetail.jsx`

**Files:**
- Create: `frontend/src/pages/ThreatDetail.jsx`

- [ ] **Step 1: Create the file**

Create `frontend/src/pages/ThreatDetail.jsx` with this content:

```jsx
import { useState, useEffect } from 'react'
import { useParams, Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import { SeverityBadge, ThreatStatusBadge } from '../components/Badge'
import { api } from '../api/client'

function formatTs(ts) {
  if (!ts) return '—'
  try {
    const normalized = typeof ts === 'string'
      ? ts.replace(' ', 'T').replace(/\s+UTC$/, 'Z').replace(/\s+\+0000$/, 'Z').replace(/\s+[\w+\-:]+$/, 'Z')
      : ts
    const d = new Date(normalized)
    if (isNaN(d.getTime())) return String(ts)
    return d.toLocaleString()
  } catch { return String(ts) }
}

export default function ThreatDetail() {
  const { id } = useParams()
  const [threat, setThreat] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  useEffect(() => {
    setLoading(true)
    setError(null)
    api.getThreat(id)
      .then(setThreat)
      .catch(err => setError(err.message || 'Failed to load threat'))
      .finally(() => setLoading(false))
  }, [id])

  async function handleAcknowledge() {
    const prev = threat.status
    setThreat(t => ({ ...t, status: 'acknowledged' }))
    try {
      await api.acknowledgeThreat(threat.threat_id, {
        agent_id: threat.agent_id,
        deployment_id: threat.deployment_id,
        session_id: threat.session_id,
        rule_id: threat.rule_id,
      })
    } catch {
      setThreat(t => ({ ...t, status: prev }))
    }
  }

  async function handleClear() {
    const prev = threat.status
    setThreat(t => ({ ...t, status: 'cleared' }))
    try {
      await api.clearThreat(threat.threat_id, {
        agent_id: threat.agent_id,
        deployment_id: threat.deployment_id,
        session_id: threat.session_id,
        rule_id: threat.rule_id,
      })
    } catch {
      setThreat(t => ({ ...t, status: prev }))
    }
  }

  const agentDetailId = threat ? `${threat.deployment_id}__${threat.agent_id}` : null

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />

      <header className="top-nav fixed top-0 right-0 h-16 bg-[#15121d]/95 z-50 flex justify-between items-center px-8 backdrop-blur-md shadow-[0_0_12px_rgba(213,63,140,0.12)]">
        <div className="flex items-center gap-3 min-w-0">
          <Link to="/threats" className="text-zinc-400 hover:text-[#ffb0cf] transition-colors shrink-0">
            <span className="material-symbols-outlined">arrow_back</span>
          </Link>
          {threat ? (
            <>
              <Link
                to={`/rules/${threat.rule_id}`}
                className="font-medium tracking-tight text-lg text-[#e7dff0] hover:text-[#ffb0cf] transition-colors truncate"
              >
                {threat.rule_name}
              </Link>
              <SeverityBadge severity={threat.severity} />
              <ThreatStatusBadge status={threat.status} />
              <Link
                to={`/agents/${agentDetailId}`}
                className="bg-[#2c2834] text-[#ffb0cf] px-2 py-0.5 rounded-full text-[10px] font-semibold font-mono hover:bg-[#374151] transition-colors shrink-0"
              >
                {threat.agent_id}
              </Link>
            </>
          ) : (
            <span className="font-medium text-lg text-zinc-500">Threat Detail</span>
          )}
        </div>

        {threat && (
          <div className="flex items-center gap-2 shrink-0 ml-4">
            {threat.status === 'open' && (
              <>
                <button
                  onClick={handleAcknowledge}
                  className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-yellow-500/10 text-yellow-400 hover:bg-yellow-500/20 transition-colors"
                >
                  Acknowledge
                </button>
                <button
                  onClick={handleClear}
                  className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-green-500/10 text-green-400 hover:bg-green-500/20 transition-colors"
                >
                  Clear
                </button>
              </>
            )}
            {threat.status === 'acknowledged' && (
              <button
                onClick={handleClear}
                className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-green-500/10 text-green-400 hover:bg-green-500/20 transition-colors"
              >
                Clear
              </button>
            )}
            {threat.status === 'cleared' && (
              <span className="px-3 py-1.5 text-xs font-semibold rounded-sm bg-green-500/10 text-green-400">
                Cleared
              </span>
            )}
          </div>
        )}
      </header>

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        {loading ? (
          <div className="p-8 space-y-4">
            {[1, 2, 3].map(i => (
              <div key={i} className="bg-[#211e29] rounded h-32 animate-pulse" />
            ))}
          </div>
        ) : error ? (
          <div className="flex flex-col items-center justify-center min-h-[60vh] gap-3">
            <p className="text-zinc-400 text-sm">
              {error === 'HTTP 404' ? 'Threat not found.' : error}
            </p>
            <Link to="/threats" className="text-[#ffb0cf] text-xs hover:underline">
              ← Back to Threats
            </Link>
          </div>
        ) : threat ? (
          <div className="p-8 max-w-5xl">
            {/* Stat strip */}
            <div className="grid grid-cols-4 gap-4 mb-8">
              {[
                { label: 'Event Count', value: threat.event_count },
                { label: 'First Seen',  value: formatTs(threat.first_seen) },
                { label: 'Last Seen',   value: formatTs(threat.last_seen) },
                { label: 'Session ID',  value: threat.session_id || '—', mono: true, title: threat.session_id, truncate: true },
              ].map(({ label, value, mono, title, truncate }) => (
                <div key={label} className="bg-[#211e29] rounded px-4 py-3">
                  <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-1">{label}</p>
                  {mono ? (
                    <code className="text-xs font-mono text-[#e7dff0]" title={title}>
                      {truncate && typeof value === 'string' && value.length > 12
                        ? value.slice(0, 12) + '…'
                        : value}
                    </code>
                  ) : (
                    <p className="text-sm text-[#e7dff0]">{value}</p>
                  )}
                </div>
              ))}
            </div>

            {/* Event cards */}
            <h2 className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-4">
              Event History ({(threat.events || []).length})
            </h2>

            {threat.events && threat.events.length > 0 ? (
              <div className="space-y-3">
                {threat.events.map((ev, i) => {
                  let parsedData = null
                  try { parsedData = JSON.parse(ev.event_data) } catch { /* use raw string */ }
                  return (
                    <div
                      key={i}
                      className="bg-[#211e29] border border-[#574149]/10 rounded p-5"
                    >
                      <div className="flex items-center gap-3 mb-3 flex-wrap">
                        <span className="text-xs font-mono text-zinc-500 whitespace-nowrap">
                          {formatTs(ev.detected_at)}
                        </span>
                        {ev.hook_name && (
                          <span className="text-[10px] font-bold uppercase bg-[#2c2834] text-zinc-300 px-2 py-0.5 rounded-full">
                            {ev.hook_name}
                          </span>
                        )}
                        {ev.tool_name && (
                          <span className="text-xs text-[#93c5fd] font-mono">{ev.tool_name}</span>
                        )}
                      </div>
                      {ev.message && (
                        <p className="text-sm text-[#e7dff0] mb-2">{ev.message}</p>
                      )}
                      {ev.run_id && (
                        <p className="text-[10px] font-mono text-zinc-600 mb-3">{ev.run_id}</p>
                      )}
                      {ev.event_data && (
                        <pre className="bg-[#0f0c17] rounded p-3 text-[11px] font-mono text-zinc-300 overflow-auto max-h-64 whitespace-pre-wrap break-all">
                          {parsedData !== null
                            ? JSON.stringify(parsedData, null, 2)
                            : ev.event_data}
                        </pre>
                      )}
                    </div>
                  )
                })}
              </div>
            ) : (
              <p className="text-zinc-600 text-sm">No event history available.</p>
            )}
          </div>
        ) : null}
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/ThreatDetail.jsx
git commit -m "feat(ui): add ThreatDetail page"
```

---

### Task 6: Update `ThreatsList.jsx` — remove expand-row, navigate on row click

**Files:**
- Modify: `frontend/src/components/ThreatsList.jsx`

Changes:
1. Add `useNavigate` to the React Router import.
2. Import `ThreatStatusBadge` from Badge (replaces the local inline `StatusBadge`).
3. Delete the local `StatusBadge` function (lines 18–30).
4. Remove `expandedId` state.
5. Add `const navigate = useNavigate()` inside the component.
6. Change the row `onClick` from `setExpandedId` to `navigate(`/threats/${t.threat_id}`)`.
7. Replace `<StatusBadge status={t.status} />` with `<ThreatStatusBadge status={t.status} />`.
8. Remove the expanded-row `<tr>` fragment (the `{expandedId === t.threat_id && ...}` block).

- [ ] **Step 1: Apply all changes to `ThreatsList.jsx`**

Replace the entire file content with:

```jsx
import React, { useState, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { SeverityBadge, ThreatStatusBadge } from './Badge'
import { api } from '../api/client'

function formatTs(ts) {
  if (!ts) return '—'
  try {
    const normalized = typeof ts === 'string'
      ? ts.replace(' ', 'T').replace(/\s+UTC$/, 'Z').replace(/\s+\+0000$/, 'Z').replace(/\s+[\w+\-:]+$/, 'Z')
      : ts
    const d = new Date(normalized)
    if (isNaN(d.getTime())) return String(ts)
    return d.toLocaleString()
  } catch { return String(ts) }
}

/**
 * ThreatsList — reusable deduplicated threats table.
 *
 * Props:
 *   ruleId      — if set, only shows threats for that rule (client-side filter)
 *   hideRuleCol — hides the "Rule / Message" column (useful when already on a rule page)
 *   showFilters — renders the severity + status filter bar (default true)
 */
export default function ThreatsList({ ruleId, hideRuleCol = false, showFilters = true }) {
  const navigate = useNavigate()
  const [threats, setThreats] = useState([])
  const [loading, setLoading] = useState(true)
  const [severityFilter, setSeverityFilter] = useState('All')
  const [statusFilter, setStatusFilter] = useState('All')

  useEffect(() => {
    let cancelled = false
    async function fetchThreats() {
      try {
        const data = await api.getThreats()
        if (!cancelled) setThreats(data.threats || [])
      } catch {
        // silently ignore, keep stale data
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    fetchThreats()
    const timer = setInterval(fetchThreats, 30_000)
    return () => { cancelled = true; clearInterval(timer) }
  }, [])

  const filtered = threats.filter(t => {
    if (ruleId && t.rule_id !== ruleId) return false
    if (severityFilter !== 'All' && t.severity?.toLowerCase() !== severityFilter.toLowerCase()) return false
    if (statusFilter !== 'All' && t.status !== statusFilter.toLowerCase()) return false
    return true
  })

  async function handleAcknowledge(threat) {
    setThreats(prev => prev.map(t =>
      t.threat_id === threat.threat_id ? { ...t, status: 'acknowledged' } : t
    ))
    try {
      await api.acknowledgeThreat(threat.threat_id, {
        agent_id: threat.agent_id,
        deployment_id: threat.deployment_id,
        session_id: threat.session_id,
        rule_id: threat.rule_id,
      })
    } catch {
      setThreats(prev => prev.map(t =>
        t.threat_id === threat.threat_id ? { ...t, status: threat.status } : t
      ))
    }
  }

  async function handleClear(threat) {
    setThreats(prev => prev.map(t =>
      t.threat_id === threat.threat_id ? { ...t, status: 'cleared' } : t
    ))
    try {
      await api.clearThreat(threat.threat_id, {
        agent_id: threat.agent_id,
        deployment_id: threat.deployment_id,
        session_id: threat.session_id,
        rule_id: threat.rule_id,
      })
    } catch {
      setThreats(prev => prev.map(t =>
        t.threat_id === threat.threat_id ? { ...t, status: threat.status } : t
      ))
    }
  }

  const colSpan = hideRuleCol ? 7 : 8
  const headers = ['Severity', 'Status', ...(!hideRuleCol ? ['Rule / Message'] : []), 'Agent', 'Session', 'Events', 'Last Seen', 'Actions']

  return (
    <>
      {showFilters && (
        <section className="bg-[#211e29] rounded p-6 mb-6 flex flex-wrap items-center gap-6">
          <div className="flex flex-col gap-2">
            <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Severity</label>
            <div className="flex gap-1">
              {[
                { label: 'Critical', bg: 'bg-[#93000a]', text: 'text-[#ffb4ab]', dot: 'bg-[#ffb4ab]', ring: 'ring-[#ffb4ab]/50', pulse: true  },
                { label: 'Warning',  bg: 'bg-[#37333f]', text: 'text-[#ffb2b8]', dot: 'bg-[#ffb2b8]', ring: 'ring-[#ffb2b8]/50', pulse: false },
                { label: 'Info',     bg: 'bg-[#2c2834]', text: 'text-zinc-400',  dot: 'bg-zinc-400',  ring: 'ring-zinc-400/50',   pulse: false },
              ].map(({ label, bg, text, dot, ring, pulse }) => {
                const isActive = severityFilter === label
                return (
                  <button
                    key={label}
                    onClick={() => setSeverityFilter(prev => prev === label ? 'All' : label)}
                    className={`flex items-center gap-1.5 px-2.5 py-1 text-[10px] font-black uppercase rounded-full transition-all ${bg} ${text} ${
                      isActive ? `ring-2 ${ring} ring-offset-1 ring-offset-[#211e29]` : 'opacity-50 hover:opacity-80'
                    }`}
                  >
                    <span className={`w-1.5 h-1.5 rounded-full ${dot} ${isActive && pulse ? 'animate-pulse' : ''}`} />
                    {label}
                  </button>
                )
              })}
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Status</label>
            <div className="flex gap-1">
              {['All', 'open', 'acknowledged', 'cleared'].map(s => (
                <button
                  key={s}
                  onClick={() => setStatusFilter(s)}
                  className={`px-3 py-1.5 text-[11px] font-semibold rounded-full transition-colors capitalize ${
                    statusFilter === s
                      ? 'bg-[#ffb0cf]/20 text-[#ffb0cf] border border-[#ffb0cf]/30'
                      : 'bg-[#2c2834] text-zinc-400 border border-[#574149]/10 hover:text-white'
                  }`}
                >
                  {s === 'All' ? 'All' : s.charAt(0).toUpperCase() + s.slice(1)}
                </button>
              ))}
            </div>
          </div>
        </section>
      )}

      <section className="bg-[#211e29] rounded-sm overflow-hidden mb-8">
        <div className="px-6 py-4 flex items-center justify-between border-b border-[#574149]/10">
          <span className="text-xs font-semibold text-zinc-400 uppercase tracking-widest">
            Threats
            <span className="ml-2 text-zinc-600 font-normal normal-case">({filtered.length})</span>
          </span>
        </div>
        <div className="overflow-auto">
          <table className="w-full text-left border-collapse">
            <thead className="bg-[#2c2834] sticky top-0 z-10">
              <tr>
                {headers.map(h => (
                  <th key={h} className="px-6 py-4 text-[10px] font-bold text-zinc-400 uppercase tracking-widest whitespace-nowrap">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-[#574149]/10">
              {loading ? (
                <tr>
                  <td colSpan={colSpan} className="px-6 py-12 text-center text-zinc-500 text-xs">
                    Loading threats…
                  </td>
                </tr>
              ) : filtered.length === 0 ? (
                <tr>
                  <td colSpan={colSpan} className="px-6 py-12 text-center text-zinc-500 text-xs">
                    {threats.length > 0 ? 'No threats match the current filters.' : 'No threats detected.'}
                  </td>
                </tr>
              ) : filtered.map(t => (
                <tr
                  key={t.threat_id}
                  onClick={() => navigate(`/threats/${t.threat_id}`)}
                  className="hover:bg-[#2c2834] transition-colors cursor-pointer"
                >
                  <td className="px-6 py-4">
                    <SeverityBadge severity={t.severity} />
                  </td>
                  <td className="px-6 py-4">
                    <ThreatStatusBadge status={t.status} />
                  </td>
                  {!hideRuleCol && (
                    <td className="px-6 py-4 max-w-xs">
                      {t.rule_id ? (
                        <Link
                          to={`/rules/${t.rule_id}`}
                          onClick={e => e.stopPropagation()}
                          className="text-xs font-semibold text-[#e7dff0] hover:text-[#ffb0cf] transition-colors"
                        >
                          {t.rule_name}
                        </Link>
                      ) : (
                        <p className="text-xs font-semibold text-[#e7dff0]">{t.rule_name}</p>
                      )}
                      {t.events && t.events[0]?.message && (
                        <p className="text-[10px] text-zinc-500 font-medium truncate">{t.events[0].message}</p>
                      )}
                    </td>
                  )}
                  <td className="px-6 py-4">
                    <code className="text-xs font-mono text-[#ffb0cf] px-2 py-0.5 bg-[#ffb0cf]/5 rounded-sm">
                      {t.agent_id}
                    </code>
                    {t.deployment_id && (
                      <p className="text-[10px] text-zinc-500 font-medium mt-0.5 truncate max-w-[160px]">
                        {t.deployment_id}
                      </p>
                    )}
                  </td>
                  <td className="px-6 py-4">
                    {t.session_id ? (
                      <code className="text-[10px] font-mono text-zinc-400 px-1.5 py-0.5 bg-[#2c2834] rounded-sm truncate block max-w-[120px]" title={t.session_id}>
                        {t.session_id.length > 12 ? t.session_id.slice(0, 12) + '…' : t.session_id}
                      </code>
                    ) : (
                      <span className="text-zinc-600 text-xs">—</span>
                    )}
                  </td>
                  <td className="px-6 py-4">
                    <span className="text-xs text-zinc-300 font-semibold">{t.event_count} events</span>
                    <p className="text-[10px] text-zinc-600 whitespace-nowrap">{formatTs(t.first_seen)}</p>
                  </td>
                  <td className="px-6 py-4">
                    <span className="text-xs text-zinc-400 whitespace-nowrap">{formatTs(t.last_seen)}</span>
                  </td>
                  <td className="px-6 py-4" onClick={e => e.stopPropagation()}>
                    <div className="flex items-center gap-1.5">
                      {t.status === 'open' && (
                        <>
                          <button
                            onClick={() => handleAcknowledge(t)}
                            className="px-2.5 py-1 text-[10px] font-semibold rounded-sm bg-yellow-500/10 text-yellow-400 hover:bg-yellow-500/20 transition-colors whitespace-nowrap"
                          >
                            Acknowledge
                          </button>
                          <button
                            onClick={() => handleClear(t)}
                            className="px-2.5 py-1 text-[10px] font-semibold rounded-sm bg-green-500/10 text-green-400 hover:bg-green-500/20 transition-colors"
                          >
                            Clear
                          </button>
                        </>
                      )}
                      {t.status === 'acknowledged' && (
                        <button
                          onClick={() => handleClear(t)}
                          className="px-2.5 py-1 text-[10px] font-semibold rounded-sm bg-green-500/10 text-green-400 hover:bg-green-500/20 transition-colors"
                        >
                          Clear
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/components/ThreatsList.jsx
git commit -m "feat(threats): navigate to detail page on row click; remove expand-row"
```

---

### Task 7: Add `/threats/:id` route to `App.jsx`

**Files:**
- Modify: `frontend/src/App.jsx`

- [ ] **Step 1: Import `ThreatDetail` and add the route**

In `frontend/src/App.jsx`, add the import after the `Threats` import:

```js
import ThreatDetail from './pages/ThreatDetail'
```

Then add the route after `<Route path="/threats" element={<Threats />} />`:

```jsx
<Route path="/threats/:id" element={<ThreatDetail />} />
```

The updated routes block:

```jsx
        <Route path="/" element={<Navigate to="/dashboard" replace />} />
        <Route path="/dashboard" element={<Dashboard />} />
        <Route path="/agents" element={<AgentList />} />
        <Route path="/agents/:id" element={<AgentDetail />} />
        <Route path="/threats" element={<Threats />} />
        <Route path="/threats/:id" element={<ThreatDetail />} />
        <Route path="/cost" element={<CostGovernance />} />
        <Route path="/rules" element={<RulePacks />} />
        <Route path="/rules/new" element={<RuleCreate />} />
        <Route path="/rules/:id" element={<RuleDetail />} />
        <Route path="/investigate" element={<Investigate />} />
        <Route path="/onboarding" element={<Onboarding />} />
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/App.jsx
git commit -m "feat(routing): add /threats/:id route for ThreatDetail page"
```

---

### Task 8: End-to-end smoke test

- [ ] **Step 1: Start backend and frontend**

Terminal 1:
```bash
cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server
```

Terminal 2:
```bash
cd frontend && npm run dev
```

- [ ] **Step 2: Test the threats list page**

Open http://localhost:5173/threats in a browser.

Check:
- Threats table rows are clickable (cursor changes)
- Clicking a row navigates to `/threats/<id>`
- The "Acknowledge" and "Clear" buttons still work without navigating away (click one; the status badge should change in-place)
- Status badges show the correct color (blue=Open, yellow=Acknowledged, green=Cleared)

- [ ] **Step 3: Test the threat detail page**

On the `/threats/<id>` page, check:
- Back arrow navigates to `/threats`
- Header shows: rule name (linked to `/rules/<rule_id>`), severity badge, status badge, agent chip (linked to `/agents/<deployment_id>__<agent_id>`)
- Stat strip shows four chips: Event Count, First Seen, Last Seen, Session ID (truncated)
- Event cards show newest-first, always expanded, with detected_at / hook_name badge / tool_name / message / run_id / event_data JSON block
- Action buttons (Acknowledge / Clear) work with optimistic update
- Cleared status shows "Cleared" chip, no action buttons
- Navigating directly to `/threats/nonexistent` shows "Threat not found." with a back link

- [ ] **Step 4: Verify sidebar collapse**

Collapse the sidebar and verify the threat detail header tracks the width correctly (uses `top-nav` CSS class, not `left-64`).
