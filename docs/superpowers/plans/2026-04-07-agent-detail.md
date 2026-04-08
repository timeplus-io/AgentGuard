# Agent Detail Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the static AgentDetail page with a live drill-down view: sessions panel on the left, event timeline on the right, filtered by session/run via query params.

**Architecture:** Three new backend endpoints (ListSessions, GetSession, ListEvents) backed by Proton queries. Frontend is a single `/agents/:id` route with `?session=` and `?run_id=` query params controlling the left panel accordion and timeline content.

**Tech Stack:** Go/Gin backend, React 18 + React Router v6 + Tailwind CSS frontend, Timeplus Proton via HTTP.

---

### Task 1: Add Session, Run, Event models

**Files:**
- Modify: `backend/internal/models/models.go`

- [ ] **Step 1: Add the new structs** after the existing `AgentListResponse` struct

```go
// Session represents one chat/API/cron/sub-agent session for an agent.
type Session struct {
	SessionID   string    `json:"session_id"`
	SessionKey  string    `json:"session_key"`
	SessionType string    `json:"session_type"`
	RunCount    int       `json:"run_count"`
	EventCount  int       `json:"event_count"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// Run represents one user→agent turn within a session.
type Run struct {
	RunID      string    `json:"run_id"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	EventCount int       `json:"event_count"`
	Model      string    `json:"model"`
	ToolCount  int       `json:"tool_count"`
	HasBlock   bool      `json:"has_block"`
}

// SessionDetail is returned by GET /agents/:id/sessions/:session_id.
type SessionDetail struct {
	SessionID   string    `json:"session_id"`
	SessionKey  string    `json:"session_key"`
	SessionType string    `json:"session_type"`
	Runs        []Run     `json:"runs"`
}

// HookEvent is one row returned by GET /agents/:id/events.
type HookEvent struct {
	HookName     string    `json:"hook_name"`
	EventTime    time.Time `json:"event_time"`
	RunID        string    `json:"run_id"`
	SessionID    string    `json:"session_id"`
	ToolName     string    `json:"tool_name"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	HookDecision string    `json:"hook_decision"`
	BlockReason  string    `json:"block_reason"`
	EventData    string    `json:"event_data"`
}

// EventsResponse is returned by GET /agents/:id/events.
type EventsResponse struct {
	Events []HookEvent `json:"events"`
	Total  int         `json:"total"`
	Page   int         `json:"page"`
	Limit  int         `json:"limit"`
}
```

- [ ] **Step 2: Compile-check**

```bash
cd backend && CGO_ENABLED=0 go build ./...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/models/models.go
git commit -m "feat: add Session, Run, HookEvent, EventsResponse models"
```

---

### Task 2: ListSessions and GetSession handlers

**Files:**
- Create: `backend/internal/handlers/agent_sessions.go`

- [ ] **Step 1: Write the file**

```go
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	"github.com/timeplus/agentshield/internal/timeplus"
)

// uuidRe validates session_id and run_id path/query params before SQL interpolation.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// sessionType derives a human-readable type label from the session_key pattern.
func sessionType(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) < 3 {
		return "Unknown"
	}
	switch parts[2] {
	case "main":
		return "Web UI"
	case "openai-user":
		return "HTTP API"
	case "cron":
		return "Cron"
	case "subagent":
		return "Sub-agent"
	default:
		return "Unknown"
	}
}

// parseTime handles the multiple time formats Proton returns.
func parseTime(v any) time.Time {
	switch s := v.(type) {
	case string:
		if s == "" {
			return time.Time{}
		}
		if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", s); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	case float64:
		return time.Unix(int64(s), 0)
	case json.Number:
		if n, err := s.Float64(); err == nil {
			return time.Unix(int64(n), 0)
		}
	}
	return time.Time{}
}

// parseInt handles json.Number / float64 / int from Proton rows.
func parseInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	case int:
		return n
	}
	return 0
}

// parseBool handles Proton's countIf(...) > 0 result (returned as 0/1 uint8).
func parseBool(v any) bool {
	return parseInt(v) != 0
}

// parseStr safely casts any to string.
func parseStr(v any) string {
	s, _ := v.(string)
	return s
}

// splitAgentID splits the compound "deploymentId__agentId" path param.
// Returns ("", "", false) on invalid format.
func splitAgentID(compoundID string) (deploymentID, agentID string, ok bool) {
	parts := strings.SplitN(compoundID, "__", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	if !idPartRe.MatchString(parts[0]) || !idPartRe.MatchString(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// ListSessions handles GET /api/agents/:id/sessions
func ListSessions(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format, expected deploymentId__agentId"})
			return
		}

		sql := fmt.Sprintf(`
SELECT
    session_id,
    session_key,
    count(DISTINCT run_id) AS run_count,
    count()                AS event_count,
    min(event_time)        AS first_seen,
    max(event_time)        AS last_seen
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    != ''
GROUP BY session_id, session_key
ORDER BY last_seen DESC`,
			deploymentID, agentID)

		rows, err := timeplus.Query(&cfg.Timeplus, sql)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		sessions := make([]models.Session, 0, len(rows))
		for _, row := range rows {
			key := parseStr(row["session_key"])
			sessions = append(sessions, models.Session{
				SessionID:   parseStr(row["session_id"]),
				SessionKey:  key,
				SessionType: sessionType(key),
				RunCount:    parseInt(row["run_count"]),
				EventCount:  parseInt(row["event_count"]),
				FirstSeen:   parseTime(row["first_seen"]),
				LastSeen:    parseTime(row["last_seen"]),
			})
		}

		c.JSON(http.StatusOK, sessions)
	}
}

// GetSession handles GET /api/agents/:id/sessions/:session_id
func GetSession(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format"})
			return
		}

		sessionID := c.Param("session_id")
		if !uuidRe.MatchString(sessionID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session_id format"})
			return
		}

		// Fetch session metadata (key) for the session_type label.
		metaSQL := fmt.Sprintf(`
SELECT session_key
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'
LIMIT 1`,
			deploymentID, agentID, sessionID)

		metaRows, err := timeplus.Query(&cfg.Timeplus, metaSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		if len(metaRows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		sessionKey := parseStr(metaRows[0]["session_key"])

		// Fetch runs for this session.
		runsSQL := fmt.Sprintf(`
SELECT
    run_id,
    min(event_time)                         AS start_time,
    max(event_time)                         AS end_time,
    count()                                 AS event_count,
    any(model)                              AS model,
    countIf(hook_name = 'before_tool_call') AS tool_count,
    countIf(hook_decision = 'block')        AS block_count
FROM table(agentshield_hook_events)
WHERE session_id = '%s'
  AND run_id     != ''
GROUP BY run_id
ORDER BY start_time ASC`,
			sessionID)

		runsRows, err := timeplus.Query(&cfg.Timeplus, runsSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		runs := make([]models.Run, 0, len(runsRows))
		for _, row := range runsRows {
			runs = append(runs, models.Run{
				RunID:      parseStr(row["run_id"]),
				StartTime:  parseTime(row["start_time"]),
				EndTime:    parseTime(row["end_time"]),
				EventCount: parseInt(row["event_count"]),
				Model:      parseStr(row["model"]),
				ToolCount:  parseInt(row["tool_count"]),
				HasBlock:   parseBool(row["block_count"]),
			})
		}

		c.JSON(http.StatusOK, models.SessionDetail{
			SessionID:   sessionID,
			SessionKey:  sessionKey,
			SessionType: sessionType(sessionKey),
			Runs:        runs,
		})
	}
}
```

- [ ] **Step 2: Compile-check**

```bash
cd backend && CGO_ENABLED=0 go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/handlers/agent_sessions.go
git commit -m "feat: add ListSessions and GetSession handlers"
```

---

### Task 3: ListEvents handler

**Files:**
- Create: `backend/internal/handlers/agent_events.go`

- [ ] **Step 1: Write the file**

```go
package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	"github.com/timeplus/agentshield/internal/timeplus"
)

// ListEvents handles GET /api/agents/:id/events
// Query params: session_id, run_id, since (RFC3339), until (RFC3339), page, limit
func ListEvents(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format"})
			return
		}

		// Pagination
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
		if limit < 1 {
			limit = 100
		}
		if limit > 500 {
			limit = 500
		}

		// Build WHERE clauses
		where := []string{
			fmt.Sprintf("deployment_id = '%s'", deploymentID),
			fmt.Sprintf("agent_id = '%s'", agentID),
		}

		if sid := c.Query("session_id"); sid != "" {
			if !uuidRe.MatchString(sid) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session_id"})
				return
			}
			where = append(where, fmt.Sprintf("session_id = '%s'", sid))
		}

		if rid := c.Query("run_id"); rid != "" {
			if !uuidRe.MatchString(rid) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run_id"})
				return
			}
			where = append(where, fmt.Sprintf("run_id = '%s'", rid))
		}

		if since := c.Query("since"); since != "" {
			t, err := time.Parse(time.RFC3339, since)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid since: use RFC3339"})
				return
			}
			where = append(where, fmt.Sprintf("event_time >= '%s'", t.UTC().Format("2006-01-02 15:04:05")))
		}

		if until := c.Query("until"); until != "" {
			t, err := time.Parse(time.RFC3339, until)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid until: use RFC3339"})
				return
			}
			where = append(where, fmt.Sprintf("event_time <= '%s'", t.UTC().Format("2006-01-02 15:04:05")))
		}

		whereClause := strings.Join(where, "\n  AND ")

		// Fetch all matching rows for server-side pagination
		sql := fmt.Sprintf(`
SELECT
    hook_name,
    event_time,
    run_id,
    session_id,
    tool_name,
    provider,
    model,
    hook_decision,
    block_reason,
    event_data
FROM table(agentshield_hook_events)
WHERE %s
ORDER BY event_time ASC`,
			whereClause)

		rows, err := timeplus.Query(&cfg.Timeplus, sql)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		total := len(rows)
		start := (page - 1) * limit
		if start >= total {
			c.JSON(http.StatusOK, models.EventsResponse{
				Events: []models.HookEvent{},
				Total:  total,
				Page:   page,
				Limit:  limit,
			})
			return
		}
		end := start + limit
		if end > total {
			end = total
		}

		events := make([]models.HookEvent, 0, end-start)
		for _, row := range rows[start:end] {
			events = append(events, models.HookEvent{
				HookName:     parseStr(row["hook_name"]),
				EventTime:    parseTime(row["event_time"]),
				RunID:        parseStr(row["run_id"]),
				SessionID:    parseStr(row["session_id"]),
				ToolName:     parseStr(row["tool_name"]),
				Provider:     parseStr(row["provider"]),
				Model:        parseStr(row["model"]),
				HookDecision: parseStr(row["hook_decision"]),
				BlockReason:  parseStr(row["block_reason"]),
				EventData:    parseStr(row["event_data"]),
			})
		}

		c.JSON(http.StatusOK, models.EventsResponse{
			Events: events,
			Total:  total,
			Page:   page,
			Limit:  limit,
		})
	}
}
```

- [ ] **Step 2: Compile-check**

```bash
cd backend && CGO_ENABLED=0 go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/handlers/agent_events.go
git commit -m "feat: add ListEvents handler with session/run/time-range filtering and pagination"
```

---

### Task 4: Register routes and build the server

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add the three new routes** inside the `api` group, after `api.GET("/agents/:id", ...)`:

```go
api.GET("/agents/:id/sessions", handlers.ListSessions(cfg))
api.GET("/agents/:id/sessions/:session_id", handlers.GetSession(cfg))
api.GET("/agents/:id/events", handlers.ListEvents(cfg))
```

The full `api` block becomes:

```go
api := r.Group("/api")
{
    api.GET("/dashboard", handlers.GetDashboard)
    api.GET("/agents", handlers.ListAgents(cfg))
    api.GET("/agents/:id", handlers.GetAgent(cfg))
    api.GET("/agents/:id/sessions", handlers.ListSessions(cfg))
    api.GET("/agents/:id/sessions/:session_id", handlers.GetSession(cfg))
    api.GET("/agents/:id/events", handlers.ListEvents(cfg))
    api.GET("/threats", handlers.ListThreats)
    api.GET("/cost", handlers.GetCost)
    api.GET("/rules", handlers.ListRules)
    api.POST("/rules/:id/toggle", handlers.ToggleRule)
    api.GET("/investigate/tables", handlers.ListTables)
    api.POST("/investigate/query", handlers.RunQuery)
    api.GET("/settings", handlers.GetSettings)
    api.PUT("/settings", handlers.UpdateSettings)
    api.POST("/onboarding/generate", handlers.GenerateConfig)
    api.POST("/proton/query", handlers.ProtonQueryHandler(cfg))
}
```

- [ ] **Step 2: Build the binary**

```bash
cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go
```

Expected: produces `./server` with no errors.

- [ ] **Step 3: Smoke-test the endpoints**

```bash
./server &
sleep 1
curl -s "http://localhost:8080/api/agents/local__main/sessions" | python3 -m json.tool | head -30
curl -s "http://localhost:8080/api/agents/local__main/events?limit=5" | python3 -m json.tool | head -40
kill %1
```

Expected: JSON arrays (may be empty if Proton is not running locally, but no 404/500 from routing).

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat: register /agents/:id/sessions and /agents/:id/events routes"
```

---

### Task 5: Frontend API client methods

**Files:**
- Modify: `frontend/src/api/client.js`

- [ ] **Step 1: Add the three new methods** inside the `api` export object, after `getAgent`:

```js
listSessions: (id) => get(`/agents/${id}/sessions`),

getSession: (id, sessionId) => get(`/agents/${id}/sessions/${sessionId}`),

listEvents: (id, params = {}) => {
  const q = new URLSearchParams()
  if (params.session_id) q.set('session_id', params.session_id)
  if (params.run_id)     q.set('run_id',     params.run_id)
  if (params.since)      q.set('since',      params.since)
  if (params.until)      q.set('until',      params.until)
  if (params.page)       q.set('page',       String(params.page))
  if (params.limit)      q.set('limit',      String(params.limit))
  return get(`/agents/${id}/events${q.toString() ? '?' + q.toString() : ''}`)
},
```

- [ ] **Step 2: Verify the frontend builds**

```bash
cd frontend && npm run build 2>&1 | tail -5
```

Expected: build succeeds (no JS errors).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.js
git commit -m "feat: add listSessions, getSession, listEvents to API client"
```

---

### Task 6: Rewrite AgentDetail.jsx

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx`

This is a full rewrite. Replace the entire file with the following:

- [ ] **Step 1: Write the new AgentDetail.jsx**

```jsx
import { useState, useEffect, useCallback } from 'react'
import { useParams, Link, useSearchParams } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import Footer from '../components/Footer'
import { api } from '../api/client'

// ── Helpers ──────────────────────────────────────────────────────────────────

function sessionTypeIcon(type) {
  switch (type) {
    case 'Web UI':    return 'language'
    case 'HTTP API':  return 'api'
    case 'Cron':      return 'schedule'
    case 'Sub-agent': return 'account_tree'
    default:          return 'help_outline'
  }
}

function hookColor(hookName, hookDecision) {
  if (hookDecision === 'block') return { dot: 'bg-[#ffb4ab]', text: 'text-[#ffb4ab]' }
  if (hookName === 'llm_input' || hookName === 'llm_output')
    return { dot: 'bg-[#ffb0cf]', text: 'text-[#ffb0cf]' }
  if (['before_tool_call', 'after_tool_call', 'tool_result_persist'].includes(hookName))
    return { dot: 'bg-[#93c5fd]', text: 'text-[#93c5fd]' }
  if (hookName === 'before_agent_start' || hookName === 'agent_end')
    return { dot: 'bg-zinc-400', text: 'text-zinc-400' }
  return { dot: 'bg-zinc-600', text: 'text-zinc-500' }
}

function fmtTime(iso) {
  if (!iso) return '—'
  const d = new Date(iso)
  return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' }) +
    '.' + String(d.getMilliseconds()).padStart(3, '0')
}

function fmtShortTime(iso) {
  if (!iso) return '—'
  return new Date(iso).toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function parseEventData(raw) {
  if (!raw) return {}
  try { return JSON.parse(raw) } catch { return {} }
}

// ── Event detail panel ────────────────────────────────────────────────────────

function EventDetail({ event }) {
  const data = parseEventData(event.event_data)
  const rows = []

  function addRow(label, value) {
    if (value !== undefined && value !== null && value !== '') {
      rows.push({ label, value: String(value) })
    }
  }

  const hn = event.hook_name
  const isBlock = event.hook_decision === 'block'

  if (hn === 'llm_input') {
    addRow('Provider', event.provider)
    addRow('Model', event.model)
    addRow('Run ID', event.run_id)
    addRow('Session ID', event.session_id)
    const msgs = data.messages || data.input || []
    if (Array.isArray(msgs)) addRow('Messages', msgs.length)
  } else if (hn === 'llm_output') {
    addRow('Provider', event.provider)
    addRow('Model', event.model)
    addRow('Run ID', event.run_id)
    addRow('Finish Reason', data.finishReason || data.finish_reason)
  } else if (hn === 'before_tool_call' || hn === 'after_tool_call') {
    addRow('Tool', event.tool_name)
    addRow('Run ID', event.run_id)
    const args = data.input || data.args || data.arguments
    if (args && typeof args === 'object') {
      Object.entries(args).forEach(([k, v]) =>
        addRow(k, typeof v === 'object' ? JSON.stringify(v) : v)
      )
    }
  } else if (hn === 'tool_result_persist') {
    addRow('Tool', event.tool_name)
    const result = data.result || data.output || ''
    addRow('Result', typeof result === 'string' ? result.slice(0, 200) : JSON.stringify(result).slice(0, 200))
  } else if (isBlock) {
    addRow('Block Reason', event.block_reason)
    addRow('Hook Decision', event.hook_decision)
    addRow('Rule', data.rule || data.ruleName)
  } else if (hn === 'before_agent_start' || hn === 'agent_end') {
    addRow('Run ID', event.run_id)
    addRow('Session ID', event.session_id)
  } else {
    // Generic: all non-empty top-level fields from event_data
    Object.entries(data).forEach(([k, v]) => {
      if (k.startsWith('_')) return
      const val = typeof v === 'object' ? JSON.stringify(v) : v
      addRow(k, val)
    })
    addRow('Run ID', event.run_id)
    addRow('Session ID', event.session_id)
  }

  return (
    <div className="mt-2 mb-1 ml-6 bg-[#0f0c17] rounded-sm border border-[#574149]/20 p-3">
      <table className="w-full">
        <tbody>
          {rows.map(r => (
            <tr key={r.label} className="align-top">
              <td className="text-[10px] text-zinc-500 uppercase tracking-widest pr-4 py-0.5 whitespace-nowrap w-32">{r.label}</td>
              <td className="text-xs font-mono text-[#e7dff0] break-all py-0.5">{r.value}</td>
            </tr>
          ))}
          {rows.length === 0 && (
            <tr><td colSpan={2} className="text-[10px] text-zinc-600">No detail fields</td></tr>
          )}
        </tbody>
      </table>
    </div>
  )
}

// ── Main component ────────────────────────────────────────────────────────────

const METRIC_CARDS = [
  { label: 'Total Events', icon: 'bolt' },
  { label: 'Sessions',     icon: 'layers' },
  { label: 'Runs',         icon: 'sync_alt' },
  { label: 'Blocks',       icon: 'block' },
]

export default function AgentDetail() {
  const { id } = useParams()
  const [searchParams, setSearchParams] = useSearchParams()
  const selectedSession = searchParams.get('session')
  const selectedRun     = searchParams.get('run_id')

  const [agent,         setAgent]         = useState(null)
  const [sessions,      setSessions]      = useState([])
  const [sessionDetail, setSessionDetail] = useState(null)  // {session_id, session_key, runs}
  const [eventsResp,    setEventsResp]    = useState({ events: [], total: 0, page: 1, limit: 100 })
  const [loadingAgent,  setLoadingAgent]  = useState(true)
  const [loadingSess,   setLoadingSess]   = useState(true)
  const [loadingEvents, setLoadingEvents] = useState(false)
  const [refreshing,    setRefreshing]    = useState(false)
  const [expandedRows,  setExpandedRows]  = useState(new Set())
  const [page,          setPage]          = useState(1)
  const [limit,         setLimit]         = useState(100)
  const [since,         setSince]         = useState('')
  const [until,         setUntil]         = useState('')

  // Fetch agent + sessions
  const fetchBase = useCallback(() => {
    setLoadingAgent(true)
    setLoadingSess(true)
    api.getAgent(id)
      .then(setAgent)
      .catch(() => {})
      .finally(() => setLoadingAgent(false))
    api.listSessions(id)
      .then(data => setSessions(Array.isArray(data) ? data : []))
      .catch(() => {})
      .finally(() => setLoadingSess(false))
  }, [id])

  useEffect(() => { fetchBase() }, [fetchBase])

  // Fetch session detail when selected session changes
  useEffect(() => {
    if (!selectedSession) { setSessionDetail(null); return }
    api.getSession(id, selectedSession)
      .then(setSessionDetail)
      .catch(() => setSessionDetail(null))
  }, [id, selectedSession])

  // Fetch events when filters change
  useEffect(() => {
    if (!selectedSession && !selectedRun) {
      setEventsResp({ events: [], total: 0, page: 1, limit })
      return
    }
    setLoadingEvents(true)
    setExpandedRows(new Set())
    const params = { page, limit }
    if (selectedSession) params.session_id = selectedSession
    if (selectedRun)     params.run_id     = selectedRun
    if (since)           params.since      = new Date(since).toISOString()
    if (until)           params.until      = new Date(until).toISOString()
    api.listEvents(id, params)
      .then(data => setEventsResp(data || { events: [], total: 0, page, limit }))
      .catch(() => setEventsResp({ events: [], total: 0, page, limit }))
      .finally(() => setLoadingEvents(false))
  }, [id, selectedSession, selectedRun, page, limit, since, until])

  function selectSession(sessionId) {
    setPage(1)
    setExpandedRows(new Set())
    if (selectedSession === sessionId) {
      setSearchParams({})
    } else {
      setSearchParams({ session: sessionId })
    }
  }

  function selectRun(runId) {
    setPage(1)
    setExpandedRows(new Set())
    if (selectedRun === runId) {
      setSearchParams({ session: selectedSession })
    } else {
      setSearchParams({ session: selectedSession, run_id: runId })
    }
  }

  function toggleExpand(idx) {
    setExpandedRows(prev => {
      const next = new Set(prev)
      next.has(idx) ? next.delete(idx) : next.add(idx)
      return next
    })
  }

  async function refresh() {
    setRefreshing(true)
    try {
      await Promise.all([
        api.getAgent(id).then(setAgent).catch(() => {}),
        api.listSessions(id).then(d => setSessions(Array.isArray(d) ? d : [])).catch(() => {}),
        selectedSession && api.getSession(id, selectedSession).then(setSessionDetail).catch(() => {}),
        (selectedSession || selectedRun) && (() => {
          const params = { page, limit }
          if (selectedSession) params.session_id = selectedSession
          if (selectedRun)     params.run_id     = selectedRun
          if (since)           params.since      = new Date(since).toISOString()
          if (until)           params.until      = new Date(until).toISOString()
          return api.listEvents(id, params).then(setEventsResp).catch(() => {})
        })(),
      ].filter(Boolean))
    } finally {
      setRefreshing(false)
    }
  }

  const [deploymentId, agentId] = id.split('__')
  const agentLabel = agent?.agent_id || agentId || id

  const { events, total } = eventsResp
  const totalPages = Math.max(1, Math.ceil(total / limit))
  const showing = total === 0 ? '0' : `${(page - 1) * limit + 1}–${Math.min(page * limit, total)}`

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <Footer />

      {/* Top nav */}
      <header className="fixed top-0 right-0 left-64 h-16 bg-[#15121d] z-50 flex justify-between items-center px-8 shadow-[0_0_12px_rgba(213,63,140,0.12)] backdrop-blur-md">
        <div className="flex items-center gap-3">
          <Link to="/agents" className="text-zinc-400 hover:text-[#ffb0cf] transition-colors">
            <span className="material-symbols-outlined">arrow_back</span>
          </Link>
          <span className="font-medium text-lg text-[#e7dff0]">{agentLabel}</span>
          {agent?.deployment_name && (
            <span className="text-[10px] text-zinc-500 bg-[#2c2834] px-2 py-0.5 rounded-full border border-[#574149]/20">
              {agent.deployment_name}
            </span>
          )}
          <span className="bg-[#ffb2b8]/10 text-[#ffb2b8] px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-tighter">
            Live Monitor
          </span>
        </div>
        <button
          onClick={refresh}
          disabled={refreshing}
          className="flex items-center gap-2 px-3 py-1.5 text-xs text-zinc-400 hover:text-[#e7dff0] bg-[#2c2834] rounded-sm border border-[#574149]/20 transition-colors disabled:opacity-50"
        >
          <span className={`material-symbols-outlined text-sm ${refreshing ? 'animate-spin' : ''}`}>refresh</span>
          Refresh
        </button>
      </header>

      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-8 space-y-6">

          {/* Metric cards (static placeholders) */}
          <section className="grid grid-cols-4 gap-4">
            {METRIC_CARDS.map(m => (
              <div key={m.label} className="bg-[#2c2834] p-5 rounded-sm relative overflow-hidden group">
                <div className="absolute top-0 right-0 p-2 opacity-10">
                  <span className="material-symbols-outlined text-4xl">{m.icon}</span>
                </div>
                <p className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest mb-1">{m.label}</p>
                <h2 className="text-2xl font-medium text-[#e7dff0]">—</h2>
              </div>
            ))}
          </section>

          {/* Sessions + Timeline split */}
          <div className="flex gap-6 min-h-[600px]">

            {/* Sessions panel */}
            <aside className="w-52 flex-shrink-0 bg-[#1d1a25] rounded-sm overflow-y-auto">
              <div className="px-4 py-3 border-b border-[#574149]/10">
                <h3 className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Sessions</h3>
              </div>
              {loadingSess ? (
                <div className="flex justify-center py-8">
                  <span className="material-symbols-outlined animate-spin text-[#ffb0cf]">refresh</span>
                </div>
              ) : sessions.length === 0 ? (
                <p className="text-[10px] text-zinc-600 p-4">No sessions found</p>
              ) : sessions.map(sess => {
                const isActive = selectedSession === sess.session_id
                const runs = isActive && sessionDetail?.session_id === sess.session_id
                  ? (sessionDetail.runs || [])
                  : []
                return (
                  <div key={sess.session_id}>
                    {/* Session row */}
                    <button
                      onClick={() => selectSession(sess.session_id)}
                      className={`w-full flex items-start gap-2 px-3 py-2.5 text-left hover:bg-[#2c2834] transition-colors border-l-2 ${
                        isActive ? 'border-[#ffb0cf] bg-[#2c2834]' : 'border-transparent'
                      }`}
                    >
                      <span className={`material-symbols-outlined text-[14px] mt-0.5 flex-shrink-0 ${isActive ? 'text-[#ffb0cf]' : 'text-zinc-500'}`}>
                        {sessionTypeIcon(sess.session_type)}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className={`text-[11px] font-medium truncate ${isActive ? 'text-[#ffb0cf]' : 'text-[#e7dff0]'}`}>
                          {sess.session_type}
                        </div>
                        <div className="text-[9px] text-zinc-600 truncate">{sess.run_count} runs</div>
                      </div>
                      <span className={`material-symbols-outlined text-[12px] flex-shrink-0 mt-0.5 ${isActive ? 'text-[#ffb0cf]' : 'text-zinc-600'}`}>
                        {isActive ? 'expand_more' : 'chevron_right'}
                      </span>
                    </button>

                    {/* Run list (expanded when session active) */}
                    {isActive && runs.map((run, i) => {
                      const runActive = selectedRun === run.run_id
                      return (
                        <button
                          key={run.run_id}
                          onClick={() => selectRun(run.run_id)}
                          className={`w-full flex items-center gap-2 pl-8 pr-3 py-1.5 text-left hover:bg-[#2c2834] transition-colors border-l-2 ${
                            runActive ? 'border-[#ffb0cf] bg-[#211e29]' : 'border-transparent'
                          }`}
                        >
                          <span className={`material-symbols-outlined text-[10px] ${runActive ? 'text-[#ffb0cf]' : 'text-zinc-600'}`}>
                            arrow_right
                          </span>
                          <div className="min-w-0">
                            <div className={`text-[10px] font-mono truncate ${runActive ? 'text-[#ffb0cf]' : 'text-zinc-400'}`}>
                              Run #{i + 1}
                            </div>
                            <div className="text-[9px] text-zinc-600">{fmtShortTime(run.start_time)}</div>
                          </div>
                          {run.has_block && (
                            <span className="material-symbols-outlined text-[10px] text-[#ffb4ab] ml-auto">block</span>
                          )}
                          {run.tool_count > 0 && (
                            <span className="text-[9px] text-[#93c5fd] ml-auto">{run.tool_count}t</span>
                          )}
                        </button>
                      )
                    })}
                  </div>
                )
              })}
            </aside>

            {/* Timeline panel */}
            <div className="flex-1 min-w-0 bg-[#1d1a25] rounded-sm flex flex-col">

              {/* Time range filter */}
              {(selectedSession || selectedRun) && (
                <div className="flex items-center gap-3 px-5 py-3 border-b border-[#574149]/10">
                  <span className="text-[10px] text-zinc-500 uppercase tracking-widest">Range</span>
                  <input
                    type="datetime-local"
                    value={since}
                    onChange={e => { setSince(e.target.value); setPage(1) }}
                    className="bg-[#0f0c17] border border-[#574149]/20 rounded-sm text-[10px] text-zinc-400 px-2 py-1 focus:outline-none focus:border-[#ffb0cf]/40"
                  />
                  <span className="text-[10px] text-zinc-600">to</span>
                  <input
                    type="datetime-local"
                    value={until}
                    onChange={e => { setUntil(e.target.value); setPage(1) }}
                    className="bg-[#0f0c17] border border-[#574149]/20 rounded-sm text-[10px] text-zinc-400 px-2 py-1 focus:outline-none focus:border-[#ffb0cf]/40"
                  />
                  {(since || until) && (
                    <button
                      onClick={() => { setSince(''); setUntil(''); setPage(1) }}
                      className="text-[10px] text-zinc-500 hover:text-[#e7dff0] transition-colors"
                    >
                      Clear
                    </button>
                  )}
                </div>
              )}

              {/* Event list */}
              <div className="flex-1 overflow-y-auto px-5 py-4">
                {!selectedSession && !selectedRun ? (
                  <div className="flex flex-col items-center justify-center h-64 text-zinc-600">
                    <span className="material-symbols-outlined text-4xl mb-3">timeline</span>
                    <p className="text-xs">Select a session to view its event timeline</p>
                  </div>
                ) : loadingEvents ? (
                  <div className="flex justify-center py-16">
                    <span className="material-symbols-outlined animate-spin text-[#ffb0cf] text-3xl">refresh</span>
                  </div>
                ) : events.length === 0 ? (
                  <div className="flex flex-col items-center justify-center h-64 text-zinc-600">
                    <span className="material-symbols-outlined text-4xl mb-3">inbox</span>
                    <p className="text-xs">No events recorded for this selection</p>
                  </div>
                ) : (
                  <div className="space-y-0">
                    {events.map((ev, i) => {
                      const color = hookColor(ev.hook_name, ev.hook_decision)
                      const expanded = expandedRows.has(i)
                      const subtitle = [ev.provider, ev.model, ev.tool_name, ev.hook_decision]
                        .filter(Boolean).join(' · ')
                      return (
                        <div key={i} className="group">
                          <div className="flex items-start gap-3 py-2 hover:bg-[#211e29] rounded-sm px-2 -mx-2 transition-colors">
                            <div className="flex flex-col items-center mt-1.5 flex-shrink-0">
                              <div className={`w-1.5 h-1.5 rounded-full ${color.dot}`} />
                              {i < events.length - 1 && <div className="w-px h-4 bg-[#574149]/20 mt-1" />}
                            </div>
                            <div className="flex-1 min-w-0">
                              <div className="flex items-baseline justify-between gap-2">
                                <span className={`text-[11px] font-mono font-medium ${color.text}`}>{ev.hook_name}</span>
                                <span className="text-[10px] text-zinc-600 font-mono flex-shrink-0">{fmtTime(ev.event_time)}</span>
                              </div>
                              {subtitle && (
                                <div className="text-[10px] text-zinc-500 truncate mt-0.5">{subtitle}</div>
                              )}
                              <button
                                onClick={() => toggleExpand(i)}
                                className="mt-1 flex items-center gap-1 text-[10px] text-zinc-600 hover:text-zinc-400 transition-colors"
                              >
                                <span className="material-symbols-outlined text-[10px]">
                                  {expanded ? 'expand_less' : 'expand_more'}
                                </span>
                                {expanded ? 'Hide details' : 'Details'}
                              </button>
                            </div>
                          </div>
                          {expanded && <EventDetail event={ev} />}
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>

              {/* Pagination */}
              {(selectedSession || selectedRun) && total > 0 && (
                <div className="flex items-center justify-between px-5 py-3 border-t border-[#574149]/10 text-[10px] text-zinc-500">
                  <div className="flex items-center gap-3">
                    <span>Showing {showing} of {total}</span>
                    <select
                      value={limit}
                      onChange={e => { setLimit(Number(e.target.value)); setPage(1) }}
                      className="bg-[#0f0c17] border border-[#574149]/20 rounded-sm text-[10px] text-zinc-400 px-1 py-0.5"
                    >
                      <option value={25}>25</option>
                      <option value={50}>50</option>
                      <option value={100}>100</option>
                    </select>
                  </div>
                  <div className="flex items-center gap-1">
                    <button
                      onClick={() => setPage(p => Math.max(1, p - 1))}
                      disabled={page === 1}
                      className="w-7 h-7 flex items-center justify-center bg-[#211e29] rounded-sm border border-[#574149]/10 disabled:opacity-40 hover:bg-[#2c2834] transition-colors"
                    >
                      <span className="material-symbols-outlined text-xs">chevron_left</span>
                    </button>
                    <span className="px-2">{page} / {totalPages}</span>
                    <button
                      onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                      disabled={page === totalPages}
                      className="w-7 h-7 flex items-center justify-center bg-[#211e29] rounded-sm border border-[#574149]/10 disabled:opacity-40 hover:bg-[#2c2834] transition-colors"
                    >
                      <span className="material-symbols-outlined text-xs">chevron_right</span>
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Build the frontend**

```bash
cd frontend && npm run build 2>&1 | tail -10
```

Expected: build succeeds with no errors.

- [ ] **Step 3: Start dev server and verify manually**

```bash
cd frontend && npm run dev &
cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server &
```

Open `http://localhost:5173/agents/local__main`. Verify:
- 4 static metric cards render
- Sessions list loads on the left
- Clicking a session expands run list and loads timeline on the right
- Clicking a run filters to that run's events
- Each event shows hook_name + timestamp + subtitle
- Clicking "Details" expands the structured detail panel
- Refresh button spins and re-fetches
- Pagination controls show when events > 0

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/AgentDetail.jsx
git commit -m "feat: rewrite AgentDetail with live sessions panel, event timeline, and run drill-down"
```
