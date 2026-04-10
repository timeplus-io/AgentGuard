# Threat Deduplication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deduplicate security threats by `(agent_id, deployment_id, session_id, rule_id)` using a coalesced Proton mutable stream, add open/acknowledged/cleared status lifecycle, and surface event history per threat in the UI.

**Architecture:** A new coalesced mutable stream `agentshield_threats` has two column families — `cf_events` (rule_name, severity, last_seen) written by a passthrough MV from `agentshield_security_events`, and `cf_status` (status, status_updated_at) written by user actions. `GET /api/threats` runs three concurrent Proton snapshot queries (threats table, aggregation for count/first_seen, raw events), merges them in Go, and applies re-open logic. The Threats page switches from direct Proton streaming to 30-second REST polling with action buttons per row.

**Tech Stack:** Go/Gin, Timeplus Proton (coalesced mutable stream + MV), React 18, Tailwind CSS.

---

## File Structure

| File | Change |
|------|--------|
| `backend/internal/models/models.go` | Replace `Threat` struct; add `ThreatEvent` |
| `backend/internal/timeplus/init.go` | Add DDL constants + `createThreatsStream()` + `createThreatsMV()` + call both in `Init()` |
| `backend/internal/handlers/threats.go` | Full rewrite — `ListThreats(cfg)`, `AcknowledgeThreat(cfg)`, `ClearThreat(cfg)` |
| `backend/cmd/server/main.go` | Update `ListThreats` call signature; add acknowledge/clear routes |
| `frontend/src/api/client.js` | Add `getThreats()`, `acknowledgeThreat()`, `clearThreat()` |
| `frontend/src/pages/Threats.jsx` | Replace streaming with polling; add status filter + action buttons + expandable event history |

---

## Task 1: Update Go Models

**Files:**
- Modify: `backend/internal/models/models.go`

- [ ] **Step 1: Read the current Threat struct**

Open `backend/internal/models/models.go` and find the `Threat` struct (around line 112). Note it has fields like `ID`, `Name`, `Vector`, `TimeAgo`, `Category` — these are mock-oriented and will be replaced.

- [ ] **Step 2: Replace the Threat struct and add ThreatEvent**

Replace the existing `Threat` struct with:

```go
// ThreatEvent is one raw security event belonging to a deduplicated threat.
type ThreatEvent struct {
	DetectedAt string `json:"detected_at"`
	RunID      string `json:"run_id"`
	HookName   string `json:"hook_name"`
	ToolName   string `json:"tool_name"`
	Message    string `json:"message"`
	EventData  string `json:"event_data"`
}

// Threat is a deduplicated security threat keyed by (agent_id, deployment_id, session_id, rule_id).
// ThreatID is computed in Go (MD5 of the four key fields) and is not stored in Proton.
// Status lifecycle: "open" → "acknowledged" → "cleared" (auto-reopens to "open" on new events).
type Threat struct {
	ThreatID        string        `json:"threat_id"`
	AgentID         string        `json:"agent_id"`
	DeploymentID    string        `json:"deployment_id"`
	SessionID       string        `json:"session_id"`
	RuleID          string        `json:"rule_id"`
	RuleName        string        `json:"rule_name"`
	Severity        string        `json:"severity"`
	EventCount      int64         `json:"event_count"`
	FirstSeen       string        `json:"first_seen"`
	LastSeen        string        `json:"last_seen"`
	Status          string        `json:"status"` // "open", "acknowledged", "cleared"
	StatusUpdatedAt string        `json:"status_updated_at,omitempty"`
	Events          []ThreatEvent `json:"events"`
}
```

`ThreatListResponse` and `ThreatCategory` structs stay unchanged.

- [ ] **Step 3: Verify compile**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: no errors (the mock package references `Threat` with old fields — that will break in the next step; continue).

- [ ] **Step 4: Update mock data to use new Threat fields**

Open `backend/internal/mock/data.go`. Find the `Threats` slice. The mock threats used old fields (`ID`, `Name`, `Vector`, `TimeAgo`, `Category`). Since `ListThreats` will no longer use mock data after Task 3, remove the `Threats` variable entirely (or leave it empty — whatever compiles). Also remove any `ThreatCategory` mock if it exists since it's no longer part of the Threat struct.

If `mock.Threats` is referenced only in `threats.go` (which we'll fully replace in Task 3), the simplest fix is to make it an empty slice with the new type:

```go
var Threats = []models.Threat{}
```

- [ ] **Step 5: Verify compile again**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/models/models.go backend/internal/mock/data.go
git commit -m "feat(threats): new Threat model with ThreatEvent and status lifecycle"
```

---

## Task 2: Create agentshield_threats Stream and mv_threats MV

**Files:**
- Modify: `backend/internal/timeplus/init.go`

- [ ] **Step 1: Add DDL constants for the threats stream and MV**

At the bottom of `backend/internal/timeplus/init.go`, add two new DDL constants after the existing ones:

```go
const ddlThreatsStream = `
CREATE MUTABLE STREAM IF NOT EXISTS agentshield_threats
(
    agent_id          string,
    deployment_id     string,
    session_id        string,
    rule_id           string,
    rule_name         string,
    severity          string,
    last_seen         datetime64(3),
    status            string,
    status_updated_at datetime64(3),
    FAMILY cf_events(rule_name, severity, last_seen),
    FAMILY cf_status(status, status_updated_at)
) PRIMARY KEY (agent_id, deployment_id, session_id, rule_id)
SETTINGS coalesced = true`

const ddlThreatsMV = `
CREATE MATERIALIZED VIEW mv_threats
INTO agentshield_threats (agent_id, deployment_id, session_id, rule_id, rule_name, severity, last_seen)
AS SELECT
    agent_id,
    deployment_id,
    session_id,
    rule_id,
    rule_name,
    severity,
    detected_at AS last_seen
FROM agentshield_security_events`
```

- [ ] **Step 2: Add createThreatsStream and createThreatsMV functions**

Add these two functions to `backend/internal/timeplus/init.go`:

```go
func createThreatsStream(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "stream", "agentshield_threats", ddlThreatsStream)
}

func createThreatsMV(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "view", "mv_threats", ddlThreatsMV)
}
```

- [ ] **Step 3: Call both functions in Init()**

In the `Init()` function, add the two calls after `createSecurityStreams` and before `createPricingStream`:

```go
if err := createThreatsStream(ctx, conn); err != nil {
    log.Printf("timeplus init: warning creating threats stream: %v", err)
}
if err := createThreatsMV(ctx, conn); err != nil {
    log.Printf("timeplus init: warning creating threats MV: %v", err)
}
```

Both are non-fatal (log warning, do not return error) — the server starts even if Proton is unavailable, following the pattern of `createPricingStream`.

- [ ] **Step 4: Verify compile**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/timeplus/init.go
git commit -m "feat(threats): create agentshield_threats coalesced stream and mv_threats MV"
```

---

## Task 3: Rewrite ListThreats Handler

**Files:**
- Modify: `backend/internal/handlers/threats.go`

- [ ] **Step 1: Replace threats.go entirely**

Replace the full content of `backend/internal/handlers/threats.go` with:

```go
package handlers

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	tp "github.com/timeplus/agentshield/internal/timeplus"
)

// threatKey uniquely identifies a deduplicated threat.
type threatKey struct{ agentID, deploymentID, sessionID, ruleID string }

// computeThreatID returns the MD5 hex string of the four key fields.
// Used as the API path parameter (:id) — never stored in Proton.
func computeThreatID(agentID, deploymentID, sessionID, ruleID string) string {
	h := md5.Sum([]byte(agentID + "-" + deploymentID + "-" + sessionID + "-" + ruleID))
	return hex.EncodeToString(h[:])
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

// ListThreats handles GET /api/threats.
// Runs three Proton snapshot queries concurrently, merges results, applies re-open logic.
// Query params: ?severity=, ?status=, ?page=, ?limit=
func ListThreats(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		severityFilter := c.Query("severity")
		statusFilter := c.Query("status")
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
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Build stats map from Query 2 (event_count + first_seen per key).
		type statsEntry struct {
			eventCount int64
			firstSeen  string
		}
		statsMap := make(map[threatKey]statsEntry, len(r2.rows))
		if r2.err == nil {
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
		}

		// Build events map from Query 3 ([]ThreatEvent per key, newest first).
		eventsMap := make(map[threatKey][]models.ThreatEvent, len(r1.rows))
		if r3.err == nil {
			for _, row := range r3.rows {
				k := threatKey{
					parseStr(row["agent_id"]),
					parseStr(row["deployment_id"]),
					parseStr(row["session_id"]),
					parseStr(row["rule_id"]),
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
		}

		// Collect threats that need re-opening (status was cleared but new events arrived).
		var reopenMu sync.Mutex
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

			// Re-open logic: if cleared but last_seen is more recent than status_updated_at.
			if status == "cleared" && statusUpdatedAt != "" && lastSeen != "" {
				ls, errLS := time.Parse("2006-01-02 15:04:05.000", lastSeen)
				su, errSU := time.Parse("2006-01-02 15:04:05.000", statusUpdatedAt)
				if errLS == nil && errSU == nil && ls.After(su) {
					status = "open"
					reopenMu.Lock()
					toReopen = append(toReopen, k)
					reopenMu.Unlock()
				}
			}

			stats := statsMap[k]
			t := models.Threat{
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
				Events:          eventsMap[k],
			}
			if t.Events == nil {
				t.Events = []models.ThreatEvent{}
			}
			threats = append(threats, t)
		}

		// Fire-and-forget: upsert re-opened threats back to agentshield_threats CF2.
		if len(toReopen) > 0 {
			go func() {
				for _, k := range toReopen {
					sql := fmt.Sprintf(
						`INSERT INTO agentshield_threats (agent_id, deployment_id, session_id, rule_id, status, status_updated_at) VALUES ('%s', '%s', '%s', '%s', 'open', now64(3))`,
						escapeSQL(k.agentID), escapeSQL(k.deploymentID),
						escapeSQL(k.sessionID), escapeSQL(k.ruleID),
					)
					if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
						// Non-fatal: will be detected again on next read.
						_ = err
					}
				}
			}()
		}

		// Sort by last_seen descending.
		for i := 0; i < len(threats)-1; i++ {
			for j := i + 1; j < len(threats); j++ {
				if threats[j].LastSeen > threats[i].LastSeen {
					threats[i], threats[j] = threats[j], threats[i]
				}
			}
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
```

- [ ] **Step 2: Verify compile**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: compiles cleanly. (The route in main.go currently calls `handlers.ListThreats` without cfg — that will be fixed in Task 4.)

- [ ] **Step 3: Commit**

```bash
git add backend/internal/handlers/threats.go
git commit -m "feat(threats): rewrite ListThreats with three-query Proton merge and re-open logic"
```

---

## Task 4: Add AcknowledgeThreat and ClearThreat Handlers + Update Routes

**Files:**
- Modify: `backend/internal/handlers/threats.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add request body struct and upsertThreatStatus helper**

Append to `backend/internal/handlers/threats.go`:

```go
// ThreatStatusBody is the request body for acknowledge/clear endpoints.
// The client sends the full threat identity to avoid a read-before-write.
type ThreatStatusBody struct {
	AgentID      string `json:"agent_id"      binding:"required"`
	DeploymentID string `json:"deployment_id"  binding:"required"`
	SessionID    string `json:"session_id"     binding:"required"`
	RuleID       string `json:"rule_id"        binding:"required"`
}

func upsertThreatStatus(cfg *config.Config, body ThreatStatusBody, status string) error {
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
// Upserts cf_status with status='acknowledged'. :id is the computed threat_id (routing only).
func AcknowledgeThreat(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body ThreatStatusBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id, deployment_id, session_id, rule_id are required"})
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
// Upserts cf_status with status='cleared'. :id is the computed threat_id (routing only).
func ClearThreat(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body ThreatStatusBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id, deployment_id, session_id, rule_id are required"})
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

- [ ] **Step 2: Update route registrations in main.go**

In `backend/cmd/server/main.go`, find the threats routes section and replace:

```go
api.GET("/threats", handlers.ListThreats)
```

with:

```go
api.GET("/threats", handlers.ListThreats(cfg))
api.POST("/threats/:id/acknowledge", handlers.AcknowledgeThreat(cfg))
api.POST("/threats/:id/clear", handlers.ClearThreat(cfg))
```

- [ ] **Step 3: Verify compile**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 4: Build and smoke test**

```bash
cd backend
CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server
```

In another terminal:

```bash
curl -s http://localhost:8080/api/threats | jq '.total'
```

Expected: `0` (empty — no data yet) or a number if Proton has security events. No 500 error.

```bash
curl -s -X POST http://localhost:8080/api/threats/test-id/acknowledge \
  -H 'Content-Type: application/json' \
  -d '{"agent_id":"a","deployment_id":"d","session_id":"s","rule_id":"r"}' | jq .
```

Expected: `{"ok": true, "status": "acknowledged"}` or a 500 if Proton is not running (acceptable).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/threats.go backend/cmd/server/main.go
git commit -m "feat(threats): add AcknowledgeThreat and ClearThreat handlers, update routes"
```

---

## Task 5: Frontend API Client

**Files:**
- Modify: `frontend/src/api/client.js`

- [ ] **Step 1: Add getThreats, acknowledgeThreat, clearThreat**

In `frontend/src/api/client.js`, add three new methods to the `api` object after `listThreats`:

```js
  getThreats: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/threats${q ? '?' + q : ''}`)
  },
  acknowledgeThreat: (id, body) => post(`/threats/${id}/acknowledge`, body),
  clearThreat: (id, body) => post(`/threats/${id}/clear`, body),
```

The existing `listThreats` can remain (it is used by nothing currently and could be an alias). Keep it as-is to avoid breaking anything.

- [ ] **Step 2: Verify no syntax errors**

```bash
cd frontend
npm run build 2>&1 | tail -5
```

Expected: build succeeds (or only pre-existing warnings).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.js
git commit -m "feat(threats): add getThreats, acknowledgeThreat, clearThreat API client methods"
```

---

## Task 6: Rewrite Threats.jsx

**Files:**
- Modify: `frontend/src/pages/Threats.jsx`

- [ ] **Step 1: Replace Threats.jsx entirely**

Replace the full content of `frontend/src/pages/Threats.jsx` with:

```jsx
import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { SeverityBadge } from '../components/Badge'
import { api } from '../api/client'
import LiveAgentActivity from '../components/LiveAgentActivity'

const POLL_INTERVAL = 30_000 // 30 seconds

const STATUS_TABS = ['All', 'open', 'acknowledged', 'cleared']
const STATUS_LABELS = { open: 'Open', acknowledged: 'Acknowledged', cleared: 'Cleared' }

const STATUS_BADGE = {
  open:         'bg-blue-900/40 text-blue-300 border border-blue-500/30',
  acknowledged: 'bg-yellow-900/40 text-yellow-300 border border-yellow-500/30',
  cleared:      'bg-green-900/40 text-green-300 border border-green-500/30',
}

function StatusBadge({ status }) {
  if (!status || status === 'open') return null
  return (
    <span className={`text-[9px] font-bold uppercase tracking-widest px-2 py-0.5 rounded-full ${STATUS_BADGE[status] ?? ''}`}>
      {STATUS_LABELS[status] ?? status}
    </span>
  )
}

function formatTs(ts) {
  if (!ts) return '—'
  try {
    return new Date(typeof ts === 'string' ? ts.replace(' ', 'T') : ts).toLocaleString()
  } catch { return String(ts) }
}

export default function Threats() {
  const [threats, setThreats]         = useState([])
  const [loading, setLoading]         = useState(true)
  const [error, setError]             = useState(null)
  const [severity, setSeverity]       = useState('')
  const [statusTab, setStatusTab]     = useState('All')
  const [expandedId, setExpandedId]   = useState(null)
  const [actionPending, setActionPending] = useState(null) // threat_id being acted on

  const fetchThreats = useCallback(async () => {
    try {
      const params = {}
      if (severity) params.severity = severity
      if (statusTab !== 'All') params.status = statusTab
      const data = await api.getThreats(params)
      setThreats(data.threats ?? [])
      setError(null)
    } catch (e) {
      setError('Failed to load threats. Check your backend connection.')
    } finally {
      setLoading(false)
    }
  }, [severity, statusTab])

  useEffect(() => {
    setLoading(true)
    fetchThreats()
    const interval = setInterval(fetchThreats, POLL_INTERVAL)
    return () => clearInterval(interval)
  }, [fetchThreats])

  async function handleAcknowledge(threat) {
    setActionPending(threat.threat_id)
    // Optimistic update
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
      // Revert on failure
      setThreats(prev => prev.map(t =>
        t.threat_id === threat.threat_id ? { ...t, status: threat.status } : t
      ))
    } finally {
      setActionPending(null)
      fetchThreats()
    }
  }

  async function handleClear(threat) {
    setActionPending(threat.threat_id)
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
    } finally {
      setActionPending(null)
      fetchThreats()
    }
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-8">
          <PageHeader
            title="Threats"
            subtitle="Deduplicated security threats detected across your AI agent fleet."
          />

          {/* Filter Bar */}
          <section className="bg-[#211e29] rounded p-6 mb-6 flex flex-wrap items-center gap-6">
            {/* Status tabs */}
            <div className="flex flex-col gap-2">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Status</label>
              <div className="flex gap-1">
                {STATUS_TABS.map(tab => (
                  <button
                    key={tab}
                    onClick={() => setStatusTab(tab)}
                    className={`px-3 py-1.5 text-[11px] font-semibold rounded-full transition-colors ${
                      statusTab === tab
                        ? 'bg-[#ffb0cf]/20 text-[#ffb0cf] border border-[#ffb0cf]/30'
                        : 'bg-[#2c2834] text-zinc-400 border border-[#574149]/10 hover:text-white'
                    }`}
                  >
                    {tab === 'All' ? 'All' : STATUS_LABELS[tab]}
                  </button>
                ))}
              </div>
            </div>

            {/* Severity filter */}
            <div className="flex flex-col gap-2 flex-1 min-w-[200px]">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Severity</label>
              <div className="flex gap-1">
                {[
                  { label: 'Critical', value: 'critical', bg: 'bg-[#93000a]', text: 'text-[#ffb4ab]', dot: 'bg-[#ffb4ab]', ring: 'ring-[#ffb4ab]/50', pulse: true  },
                  { label: 'Warning',  value: 'warning',  bg: 'bg-[#37333f]', text: 'text-[#ffb2b8]', dot: 'bg-[#ffb2b8]', ring: 'ring-[#ffb2b8]/50', pulse: false },
                  { label: 'Info',     value: 'info',     bg: 'bg-[#2c2834]', text: 'text-zinc-400',  dot: 'bg-zinc-400',  ring: 'ring-zinc-400/50',   pulse: false },
                ].map(({ label, value, bg, text, dot, ring, pulse }) => {
                  const isActive = severity === value
                  return (
                    <button
                      key={value}
                      onClick={() => setSeverity(prev => prev === value ? '' : value)}
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
          </section>

          {/* Threats Table */}
          <section className="bg-[#211e29] rounded-sm overflow-hidden mb-8">
            <div className="px-6 py-4 flex items-center justify-between border-b border-[#574149]/10">
              <span className="text-xs font-semibold text-zinc-400 uppercase tracking-widest">
                Threats
                <span className="ml-2 text-zinc-600 font-normal normal-case">({threats.length})</span>
              </span>
            </div>

            {error && (
              <div className="px-6 py-4 text-xs text-red-400">{error}</div>
            )}

            {loading && threats.length === 0 && (
              <div className="px-6 py-12 text-center text-zinc-500 text-xs">Loading threats…</div>
            )}

            {!loading && threats.length === 0 && !error && (
              <div className="px-6 py-12 text-center text-zinc-500 text-xs">No threats detected.</div>
            )}

            {threats.length > 0 && (
              <div className="overflow-auto">
                <table className="w-full text-left border-collapse">
                  <thead className="bg-[#2c2834] sticky top-0 z-10">
                    <tr>
                      {['Severity', 'Rule / Message', 'Agent', 'Session', 'Events', 'Last Seen', 'Status', 'Actions'].map(h => (
                        <th key={h} className="px-4 py-4 text-[10px] font-bold text-zinc-400 uppercase tracking-widest whitespace-nowrap">{h}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-[#574149]/10">
                    {threats.map(t => (
                      <>
                        <tr
                          key={t.threat_id}
                          className="hover:bg-[#2c2834] transition-colors cursor-pointer"
                          onClick={() => setExpandedId(prev => prev === t.threat_id ? null : t.threat_id)}
                        >
                          <td className="px-4 py-4"><SeverityBadge severity={t.severity} /></td>
                          <td className="px-4 py-4">
                            {t.rule_id ? (
                              <Link
                                to={`/rules/${t.rule_id}`}
                                className="text-xs font-semibold text-[#e7dff0] hover:text-[#ffb0cf] transition-colors"
                                onClick={e => e.stopPropagation()}
                              >
                                {t.rule_name}
                              </Link>
                            ) : (
                              <p className="text-xs font-semibold text-[#e7dff0]">{t.rule_name}</p>
                            )}
                            {t.events?.[0]?.message && (
                              <p className="text-[10px] text-zinc-500 font-medium max-w-xs truncate">{t.events[0].message}</p>
                            )}
                          </td>
                          <td className="px-4 py-4">
                            <code className="text-xs font-mono text-[#ffb0cf] px-2 py-0.5 bg-[#ffb0cf]/5 rounded-sm">{t.agent_id}</code>
                          </td>
                          <td className="px-4 py-4">
                            <span className="text-xs text-zinc-400 font-mono">
                              {t.session_id
                                ? t.session_id.length > 12 ? t.session_id.slice(0, 12) + '…' : t.session_id
                                : '—'}
                            </span>
                          </td>
                          <td className="px-4 py-4">
                            <span className="text-xs font-semibold text-[#e7dff0]">{t.event_count ?? 0}</span>
                            {t.first_seen && (
                              <p className="text-[10px] text-zinc-500">since {formatTs(t.first_seen)}</p>
                            )}
                          </td>
                          <td className="px-4 py-4">
                            <span className="text-xs text-zinc-400">{formatTs(t.last_seen)}</span>
                          </td>
                          <td className="px-4 py-4">
                            <StatusBadge status={t.status} />
                            {t.status === 'open' && (
                              <span className="text-[9px] font-bold uppercase tracking-widest px-2 py-0.5 rounded-full bg-[#2c2834] text-zinc-400 border border-[#574149]/20">
                                Open
                              </span>
                            )}
                          </td>
                          <td className="px-4 py-4" onClick={e => e.stopPropagation()}>
                            <div className="flex gap-2">
                              {t.status === 'open' && (
                                <button
                                  disabled={actionPending === t.threat_id}
                                  onClick={() => handleAcknowledge(t)}
                                  className="text-[10px] font-semibold px-2.5 py-1 rounded-sm bg-yellow-900/30 text-yellow-300 hover:bg-yellow-900/50 disabled:opacity-40 transition-colors whitespace-nowrap"
                                >
                                  Acknowledge
                                </button>
                              )}
                              {(t.status === 'open' || t.status === 'acknowledged') && (
                                <button
                                  disabled={actionPending === t.threat_id}
                                  onClick={() => handleClear(t)}
                                  className="text-[10px] font-semibold px-2.5 py-1 rounded-sm bg-green-900/30 text-green-300 hover:bg-green-900/50 disabled:opacity-40 transition-colors"
                                >
                                  Clear
                                </button>
                              )}
                            </div>
                          </td>
                        </tr>

                        {/* Expanded event history row */}
                        {expandedId === t.threat_id && (
                          <tr key={`${t.threat_id}-events`} className="bg-[#18151f]">
                            <td colSpan={8} className="px-6 py-4">
                              <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-3">
                                Event History ({t.events?.length ?? 0})
                              </p>
                              {(!t.events || t.events.length === 0) ? (
                                <p className="text-xs text-zinc-600">No events recorded.</p>
                              ) : (
                                <table className="w-full text-left border-collapse">
                                  <thead>
                                    <tr>
                                      {['Time', 'Hook', 'Tool', 'Message'].map(h => (
                                        <th key={h} className="pb-2 pr-6 text-[9px] font-bold text-zinc-500 uppercase tracking-widest">{h}</th>
                                      ))}
                                    </tr>
                                  </thead>
                                  <tbody className="divide-y divide-[#574149]/10">
                                    {t.events.map((ev, idx) => (
                                      <tr key={idx} className="hover:bg-[#211e29]">
                                        <td className="py-2 pr-6 text-[10px] text-zinc-400 whitespace-nowrap">{formatTs(ev.detected_at)}</td>
                                        <td className="py-2 pr-6 text-[10px] text-zinc-500">{ev.hook_name}</td>
                                        <td className="py-2 pr-6 text-[10px] text-zinc-500">{ev.tool_name || '—'}</td>
                                        <td className="py-2 pr-6 text-[10px] text-zinc-400 max-w-lg truncate">{ev.message}</td>
                                      </tr>
                                    ))}
                                  </tbody>
                                </table>
                              )}
                            </td>
                          </tr>
                        )}
                      </>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <LiveAgentActivity timeRange="-24h" />
        </div>
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Run the frontend dev server and verify the page loads**

```bash
cd frontend
npm run dev
```

Open http://localhost:5173/threats. Expected:
- Page loads without console errors
- "No threats detected." message if no data
- Filter bar shows Status and Severity filters

- [ ] **Step 3: Build frontend to check for type errors**

```bash
cd frontend
npm run build 2>&1 | tail -10
```

Expected: build succeeds with no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/Threats.jsx
git commit -m "feat(threats): replace streaming with polling, add status filter, action buttons, event history"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task |
|-----------------|------|
| `agentshield_threats` coalesced mutable stream with CF1 + CF2 | Task 2 |
| `mv_threats` passthrough MV from `agentshield_security_events` | Task 2 |
| `threat_id` computed in Go, not stored | Task 3 |
| Three concurrent Proton queries merged in Go | Task 3 |
| Re-open logic: cleared + new events → open | Task 3 |
| `GET /api/threats` with severity/status/page/limit filters | Task 3 |
| `POST /api/threats/:id/acknowledge` | Task 4 |
| `POST /api/threats/:id/clear` | Task 4 |
| Route registrations updated in main.go | Task 4 |
| Frontend API client methods | Task 5 |
| Threats page polling (30s) | Task 6 |
| Status filter tabs | Task 6 |
| Action buttons per row (context-sensitive) | Task 6 |
| Expandable event history per threat | Task 6 |
| Status badges | Task 6 |
| `ThreatEvent` and updated `Threat` Go model | Task 1 |
