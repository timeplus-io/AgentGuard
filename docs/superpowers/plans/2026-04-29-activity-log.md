# Activity Log Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an Activity log to the Threats UI showing full history and current status of tool call hold events, with inline approve/deny and a detail drawer.

**Architecture:** Single shared `ActivityLog` React component driven by a new `GET /api/activity` backend endpoint that queries `agentguard_holds` (default) or unions with `agentguard_hook_events` (include_allowed mode). The component embeds in both the Threats list page (global tab) and ThreatDetail page (session-scoped tab).

**Tech Stack:** Go/Gin backend, React 18 + Tailwind CSS frontend, Timeplus Proton SQL, existing SSE notification stream.

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Modify | `backend/internal/models/models.go` | Add `ActivityEvent`, `ActivityResponse` structs |
| Create | `backend/internal/handlers/activity.go` | `ListActivity` handler + SQL helpers |
| Create | `backend/internal/handlers/activity_test.go` | Unit tests for pure logic |
| Modify | `backend/cmd/server/main.go` | Register `GET /api/activity` route |
| Modify | `frontend/src/api/client.js` | Add `getActivity(params)` method |
| Create | `frontend/src/components/ActivityLog.jsx` | Shared list component with SSE, polling, inline actions |
| Create | `frontend/src/components/ActivityDrawer.jsx` | Slide-in detail + approve/deny form |
| Modify | `frontend/src/pages/Threats.jsx` | Add "Threats \| Activity" tab bar |
| Modify | `frontend/src/pages/ThreatDetail.jsx` | Add "Overview \| Activity" tab bar |

---

## Task 1: ActivityEvent model

**Files:**
- Modify: `backend/internal/models/models.go`

- [ ] **Step 1: Add ActivityEvent and ActivityResponse structs**

Open `backend/internal/models/models.go`. After the `Hold` struct (around line 161), add:

```go
// ActivityEvent is one row returned by GET /api/activity.
// It is sourced from agentguard_holds (is_hold_event=true) or from
// agentguard_hook_events when include_allowed=true (is_hold_event=false).
type ActivityEvent struct {
	ID            string     `json:"id"`           // hold_id for hold rows; synthetic hex for hook-only rows
	EventTime     time.Time  `json:"event_time"`
	ToolName      string     `json:"tool_name"`
	Status        string     `json:"status"`       // pending|approved|denied|timeout|abandoned|auto_block|allowed
	RuleID        string     `json:"rule_id"`
	RuleName      string     `json:"rule_name"`
	Severity      string     `json:"severity"`
	AgentID       string     `json:"agent_id"`
	SessionID     string     `json:"session_id"`
	RunID         string     `json:"run_id"`
	ArgsSummary   string     `json:"args_summary"`
	Message       string     `json:"message"`
	DecidedBy     string     `json:"decided_by,omitempty"`
	DecidedAt     *time.Time `json:"decided_at,omitempty"`
	DecidedReason string     `json:"decided_reason,omitempty"`
	HookPayload   string     `json:"hook_payload,omitempty"` // raw JSON; populated for hook-only rows
	IsHoldEvent   bool       `json:"is_hold_event"`          // true when sourced from agentguard_holds
}

// ActivityResponse is returned by GET /api/activity.
type ActivityResponse struct {
	Events []ActivityEvent `json:"events"`
	Total  int             `json:"total"`
}
```

- [ ] **Step 2: Compile-check**

```bash
cd backend && CGO_ENABLED=0 go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/models/models.go
git commit -m "feat: add ActivityEvent and ActivityResponse models"
```

---

## Task 2: Backend handler — holds-only path

**Files:**
- Create: `backend/internal/handlers/activity.go`
- Create: `backend/internal/handlers/activity_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/handlers/activity_test.go`:

```go
package handlers

import (
	"fmt"
	"testing"
	"time"

	"github.com/timeplus/agentguard/internal/models"
)

func TestHoldRowToActivityEvent_BasicFields(t *testing.T) {
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	row := map[string]interface{}{
		"hold_id":        "test-hold-1",
		"deployment_id":  "dep-1",
		"agent_id":       "agent-1",
		"session_id":     "sess-1",
		"run_id":         "run-1",
		"tool_name":      "bash",
		"rule_id":        "rp_001",
		"rule_name":      "Dangerous Shell Commands",
		"severity":       "critical",
		"args_summary":   `{"command":"rm -rf /"}`,
		"message":        "dangerous command detected",
		"created_at":     now,
		"status":         "pending",
		"decided_by":     "",
		"decided_at":     time.Time{},
		"decided_reason": "",
	}

	ev := holdRowToActivityEvent(row)

	if ev.ID != "test-hold-1" {
		t.Fatalf("ID: got %q, want %q", ev.ID, "test-hold-1")
	}
	if ev.ToolName != "bash" {
		t.Fatalf("ToolName: got %q, want %q", ev.ToolName, "bash")
	}
	if ev.Status != "pending" {
		t.Fatalf("Status: got %q, want %q", ev.Status, "pending")
	}
	if ev.RuleName != "Dangerous Shell Commands" {
		t.Fatalf("RuleName: got %q, want %q", ev.RuleName, "Dangerous Shell Commands")
	}
	if !ev.IsHoldEvent {
		t.Fatal("IsHoldEvent: want true for hold row")
	}
	if ev.DecidedAt != nil {
		t.Fatal("DecidedAt: want nil for zero time")
	}
}

func TestHoldRowToActivityEvent_DecidedAt_Populated(t *testing.T) {
	decided := time.Date(2026, 4, 29, 11, 0, 0, 0, time.UTC)
	row := map[string]interface{}{
		"hold_id":        "h2",
		"deployment_id":  "", "agent_id": "", "session_id": "", "run_id": "",
		"tool_name":      "read_file",
		"rule_id":        "", "rule_name": "", "severity": "",
		"args_summary":   "", "message":   "",
		"created_at":     time.Time{},
		"status":         "approved",
		"decided_by":     "web-ui",
		"decided_at":     decided,
		"decided_reason": "looks safe",
	}

	ev := holdRowToActivityEvent(row)

	if ev.DecidedAt == nil {
		t.Fatal("DecidedAt: want non-nil for non-zero time")
	}
	if !ev.DecidedAt.Equal(decided) {
		t.Fatalf("DecidedAt: got %v, want %v", *ev.DecidedAt, decided)
	}
	if ev.DecidedReason != "looks safe" {
		t.Fatalf("DecidedReason: got %q, want %q", ev.DecidedReason, "looks safe")
	}
}

func TestSyntheticActivityID(t *testing.T) {
	id1 := syntheticActivityID("sess-1", "2026-04-29T10:00:00Z", "bash")
	id2 := syntheticActivityID("sess-1", "2026-04-29T10:00:00Z", "bash")
	id3 := syntheticActivityID("sess-1", "2026-04-29T10:00:01Z", "bash")

	if id1 != id2 {
		t.Fatal("same inputs must produce same id")
	}
	if id1 == id3 {
		t.Fatal("different event_time must produce different id")
	}
	if len(id1) != 16 {
		t.Fatalf("expected 16 hex chars, got %d", len(id1))
	}
}

func TestMergeAndSortActivityEvents_OrderedByEventTime(t *testing.T) {
	t1 := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 29, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	holds := []models.ActivityEvent{{ID: "h1", EventTime: t2}}
	allowed := []models.ActivityEvent{{ID: "a1", EventTime: t3}, {ID: "a2", EventTime: t1}}

	merged := mergeAndSortActivityEvents(holds, allowed, 10)

	if len(merged) != 3 {
		t.Fatalf("got %d events, want 3", len(merged))
	}
	if merged[0].ID != "a1" {
		t.Fatalf("first (newest): got %q, want a1", merged[0].ID)
	}
	if merged[1].ID != "h1" {
		t.Fatalf("second: got %q, want h1", merged[1].ID)
	}
	if merged[2].ID != "a2" {
		t.Fatalf("third (oldest): got %q, want a2", merged[2].ID)
	}
}

func TestMergeAndSortActivityEvents_RespectsLimit(t *testing.T) {
	evs := make([]models.ActivityEvent, 5)
	for i := range evs {
		evs[i] = models.ActivityEvent{ID: fmt.Sprintf("e%d", i), EventTime: time.Now()}
	}
	merged := mergeAndSortActivityEvents(evs, nil, 3)
	if len(merged) != 3 {
		t.Fatalf("got %d, want 3", len(merged))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/handlers/... -run TestHoldRowToActivityEvent -v 2>&1 | head -20
```

Expected: compile error — `holdRowToActivityEvent` not defined.

- [ ] **Step 3: Create activity.go**

Create `backend/internal/handlers/activity.go`:

```go
package handlers

import (
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// ListActivity handles GET /api/activity.
//
// Query params:
//   session_id      — filter to a single session (optional)
//   status          — filter by hold status (optional; ignored when include_allowed=true)
//   include_allowed — "true" to also show hook events that were auto-allowed (default false)
//   limit           — max rows (default 100; capped at 500 when include_allowed=true)
//   page            — 1-based page number (default 1)
func ListActivity(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)

		sessionID := c.Query("session_id")
		statusFilter := c.Query("status")
		includeAllowed := c.Query("include_allowed") == "true"

		limit := 100
		maxLimit := 100
		if includeAllowed {
			maxLimit = 500
		}
		if v := c.Query("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxLimit {
				limit = n
			}
		}

		holdEvents, err := queryHoldEvents(cfg, sessionID, statusFilter, limit)
		if err != nil {
			log.Printf("activity: queryHoldEvents: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		var allowedEvents []models.ActivityEvent
		if includeAllowed {
			holdKeys := buildHoldKeySet(holdEvents)
			allowedEvents, err = queryAllowedHookEvents(cfg, sessionID, holdKeys, limit)
			if err != nil {
				log.Printf("activity: queryAllowedHookEvents: %v", err)
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
				return
			}
		}

		events := mergeAndSortActivityEvents(holdEvents, allowedEvents, limit)
		c.JSON(http.StatusOK, models.ActivityResponse{Events: events, Total: len(events)})
	}
}

// queryHoldEvents fetches holds from agentguard_holds with optional session and status filters.
// rule_name is resolved via a LEFT JOIN on agentguard_rules at query time.
func queryHoldEvents(cfg *config.Config, sessionID, statusFilter string, limit int) ([]models.ActivityEvent, error) {
	whereClauses := "1=1"
	if sessionID != "" {
		whereClauses += fmt.Sprintf(" AND h.session_id = '%s'", escapeSQLStr(sessionID))
	}
	if statusFilter != "" {
		whereClauses += fmt.Sprintf(" AND h.status = '%s'", escapeSQLStr(statusFilter))
	}

	sql := fmt.Sprintf(`
SELECT h.hold_id, h.deployment_id, h.agent_id, h.session_id, h.run_id,
       h.tool_name, h.rule_id, COALESCE(r.rule_name, '') AS rule_name,
       h.severity, h.args_summary, h.message,
       h.created_at, h.status, h.decided_by, h.decided_at, h.decided_reason
FROM (
  SELECT hold_id,
         arg_max(deployment_id,  updated_at) AS deployment_id,
         arg_max(agent_id,       updated_at) AS agent_id,
         arg_max(session_id,     updated_at) AS session_id,
         arg_max(run_id,         updated_at) AS run_id,
         arg_max(tool_name,      updated_at) AS tool_name,
         arg_max(rule_id,        updated_at) AS rule_id,
         arg_max(severity,       updated_at) AS severity,
         arg_max(args_summary,   updated_at) AS args_summary,
         arg_max(message,        updated_at) AS message,
         arg_max(created_at,     updated_at) AS created_at,
         arg_max(status,         updated_at) AS status,
         arg_max(decided_by,     updated_at) AS decided_by,
         arg_max(decided_at,     updated_at) AS decided_at,
         arg_max(decided_reason, updated_at) AS decided_reason
  FROM table(agentguard_holds)
  GROUP BY hold_id
) AS h
LEFT JOIN (
  SELECT rule_id, arg_max(name, updated_at) AS rule_name
  FROM table(agentguard_rules)
  GROUP BY rule_id
) AS r ON h.rule_id = r.rule_id
WHERE %s
ORDER BY h.created_at DESC
LIMIT %d`, whereClauses, limit)

	rows, err := tp.Query(&cfg.Timeplus, sql)
	if err != nil {
		return nil, err
	}
	events := make([]models.ActivityEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, holdRowToActivityEvent(row))
	}
	return events, nil
}

// queryAllowedHookEvents fetches PreToolUse/before_tool_call hook events that are
// NOT already represented in the holdKeys set (i.e., events that were auto-allowed
// without triggering any rule or hold).
func queryAllowedHookEvents(cfg *config.Config, sessionID string, holdKeys map[holdKey]struct{}, limit int) ([]models.ActivityEvent, error) {
	whereClause := "hook_name IN ('before_tool_call', 'PreToolUse')"
	if sessionID != "" {
		whereClause += fmt.Sprintf(" AND session_id = '%s'", escapeSQLStr(sessionID))
	}

	sql := fmt.Sprintf(`
SELECT session_id, run_id, tool_name, event_time, event_data, agent_id
FROM table(agentguard_hook_events)
WHERE %s
ORDER BY event_time DESC
LIMIT %d`, whereClause, limit)

	rows, err := tp.Query(&cfg.Timeplus, sql)
	if err != nil {
		return nil, err
	}

	events := make([]models.ActivityEvent, 0)
	for _, row := range rows {
		sess := parseStr(row["session_id"])
		run := parseStr(row["run_id"])
		tool := parseStr(row["tool_name"])
		key := holdKey{sessionID: sess, runID: run, toolName: tool}
		if _, inHolds := holdKeys[key]; inHolds {
			continue // already represented as a hold event
		}
		evTime := parseTime(row["event_time"])
		events = append(events, models.ActivityEvent{
			ID:          syntheticActivityID(sess, evTime.UTC().Format("2006-01-02T15:04:05.000Z"), tool),
			EventTime:   evTime,
			ToolName:    tool,
			Status:      "allowed",
			AgentID:     parseStr(row["agent_id"]),
			SessionID:   sess,
			RunID:       run,
			HookPayload: parseStr(row["event_data"]),
			IsHoldEvent: false,
		})
	}
	return events, nil
}

// holdKey is the deduplication key between holds and hook events.
type holdKey struct {
	sessionID string
	runID     string
	toolName  string
}

// buildHoldKeySet returns a set of holdKey values from hold events for O(1) lookup.
func buildHoldKeySet(holdEvents []models.ActivityEvent) map[holdKey]struct{} {
	keys := make(map[holdKey]struct{}, len(holdEvents))
	for _, e := range holdEvents {
		keys[holdKey{sessionID: e.SessionID, runID: e.RunID, toolName: e.ToolName}] = struct{}{}
	}
	return keys
}

// mergeAndSortActivityEvents combines hold and allowed events, sorts newest-first, and caps at limit.
func mergeAndSortActivityEvents(holds, allowed []models.ActivityEvent, limit int) []models.ActivityEvent {
	all := make([]models.ActivityEvent, 0, len(holds)+len(allowed))
	all = append(all, holds...)
	all = append(all, allowed...)
	sort.Slice(all, func(i, j int) bool {
		return all[i].EventTime.After(all[j].EventTime)
	})
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

// holdRowToActivityEvent converts a Timeplus query row (from the holds+rules JOIN) into an ActivityEvent.
func holdRowToActivityEvent(row map[string]interface{}) models.ActivityEvent {
	ev := models.ActivityEvent{
		ID:          parseStr(row["hold_id"]),
		EventTime:   parseTime(row["created_at"]),
		ToolName:    parseStr(row["tool_name"]),
		Status:      parseStr(row["status"]),
		RuleID:      parseStr(row["rule_id"]),
		RuleName:    parseStr(row["rule_name"]),
		Severity:    parseStr(row["severity"]),
		AgentID:     parseStr(row["agent_id"]),
		SessionID:   parseStr(row["session_id"]),
		RunID:       parseStr(row["run_id"]),
		ArgsSummary: parseStr(row["args_summary"]),
		Message:     parseStr(row["message"]),
		DecidedBy:   parseStr(row["decided_by"]),
		DecidedReason: parseStr(row["decided_reason"]),
		IsHoldEvent: true,
	}
	if t := parseTime(row["decided_at"]); !t.IsZero() {
		ev.DecidedAt = &t
	}
	return ev
}

// syntheticActivityID returns a 16-char hex string derived from the inputs.
// Used as a stable client-side ID for hook-only (allowed) events that have no hold_id.
func syntheticActivityID(sessionID, eventTimeISO, toolName string) string {
	h := sha256.Sum256([]byte(sessionID + "|" + eventTimeISO + "|" + toolName))
	return fmt.Sprintf("%x", h[:8])
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/handlers/... -run "TestHoldRowToActivityEvent|TestSyntheticActivityID|TestMergeAndSort" -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Compile-check**

```bash
cd backend && CGO_ENABLED=0 go build ./...
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/handlers/activity.go backend/internal/handlers/activity_test.go
git commit -m "feat: add ListActivity handler with holds and include_allowed modes"
```

---

## Task 3: Register the route

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Register GET /api/activity in the protected group**

In `backend/cmd/server/main.go`, find the protected routes block (around line 179 where `protected.GET("/holds", ...)` is). Add immediately after the last `/holds` route:

```go
protected.GET("/activity", handlers.ListActivity(cfg))
```

The relevant block after your edit should look like:
```go
protected.GET("/holds", handlers.ListHolds(cfg))
protected.GET("/holds/:hold_id", handlers.GetHold(cfg))
protected.POST("/holds/:hold_id/decide", handlers.DecideHold(cfg, holdReg, handlers.GlobalAlertBroadcaster))
protected.POST("/holds/_drain", handlers.DrainHolds(cfg, holdReg))
protected.GET("/activity", handlers.ListActivity(cfg))
```

- [ ] **Step 2: Build and verify**

```bash
cd backend && CGO_ENABLED=0 go build -o /tmp/agentguard-test cmd/server/main.go && echo "BUILD OK"
```

Expected: `BUILD OK`

- [ ] **Step 3: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat: register GET /api/activity route"
```

---

## Task 4: API client method

**Files:**
- Modify: `frontend/src/api/client.js`

- [ ] **Step 1: Add getActivity method**

In `frontend/src/api/client.js`, after the `listHolds` line (around line 143) in the Holds section, add:

```js
getActivity: (params = {}) => {
  const q = new URLSearchParams(
    Object.fromEntries(Object.entries(params).filter(([, v]) => v !== undefined && v !== '' && v !== false))
  ).toString()
  return get(`/activity${q ? '?' + q : ''}`)
},
```

- [ ] **Step 2: Verify no lint errors**

```bash
cd frontend && npm run lint -- --quiet 2>&1 | grep -v "^$" | head -20
```

Expected: no errors related to client.js.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.js
git commit -m "feat: add getActivity API client method"
```

---

## Task 5: ActivityLog component

**Files:**
- Create: `frontend/src/components/ActivityLog.jsx`

- [ ] **Step 1: Create ActivityLog.jsx**

Create `frontend/src/components/ActivityLog.jsx`:

```jsx
import { useState, useEffect, useRef, useCallback } from 'react'
import { api } from '../api/client'
import { useNotifications } from './NotificationsProvider'
import ActivityDrawer from './ActivityDrawer'

const STATUS_STYLES = {
  pending:    { bg: 'bg-amber-900/40',   text: 'text-amber-300',  label: 'Pending' },
  approved:   { bg: 'bg-emerald-900/40', text: 'text-emerald-400', label: 'Approved' },
  denied:     { bg: 'bg-[#93000a]/40',   text: 'text-[#ffb4ab]',  label: 'Denied' },
  timeout:    { bg: 'bg-zinc-800/60',    text: 'text-zinc-400',   label: 'Timed Out' },
  abandoned:  { bg: 'bg-zinc-800/60',    text: 'text-zinc-500',   label: 'Abandoned' },
  auto_block: { bg: 'bg-orange-900/40',  text: 'text-orange-300', label: 'Auto-blocked' },
  allowed:    { bg: 'bg-zinc-800/40',    text: 'text-zinc-500',   label: 'Allowed' },
}

function StatusBadge({ status }) {
  const s = STATUS_STYLES[status] ?? STATUS_STYLES.allowed
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-semibold uppercase tracking-wide ${s.bg} ${s.text}`}>
      {s.label}
    </span>
  )
}

function SeverityChip({ severity }) {
  if (!severity) return <span className="text-zinc-600 text-[10px]">—</span>
  const color = severity === 'critical' ? 'text-[#ffb4ab]' : severity === 'warning' ? 'text-amber-400' : 'text-zinc-400'
  return <span className={`text-[10px] font-semibold uppercase ${color}`}>{severity}</span>
}

function formatTs(ts) {
  if (!ts) return '—'
  try {
    const d = new Date(typeof ts === 'string' ? ts.replace(' ', 'T').replace(/\s+UTC$/, 'Z') : ts)
    if (isNaN(d)) return String(ts)
    return d.toLocaleString()
  } catch { return String(ts) }
}

// mode: "global" | "session"
export default function ActivityLog({ mode = 'global', sessionId, threatId }) {
  const [events, setEvents] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [includeAllowed, setIncludeAllowed] = useState(false)
  const [statusFilter, setStatusFilter] = useState('')
  const [autoRefresh, setAutoRefresh] = useState(mode === 'global')
  const [selectedEvent, setSelectedEvent] = useState(null)
  const [optimisticUpdates, setOptimisticUpdates] = useState({}) // holdId → status
  const [inlineErrors, setInlineErrors] = useState({})
  const timerRef = useRef(null)
  const notifications = useNotifications()

  const fetchEvents = useCallback(async () => {
    try {
      const params = {}
      if (sessionId) params.session_id = sessionId
      if (statusFilter) params.status = statusFilter
      if (includeAllowed) params.include_allowed = 'true'
      const data = await api.getActivity(params)
      setEvents(data.events ?? [])
      setError(null)
    } catch (err) {
      setError(err.message)
      setAutoRefresh(false)
    } finally {
      setLoading(false)
    }
  }, [sessionId, statusFilter, includeAllowed])

  // Initial fetch + polling
  useEffect(() => {
    setLoading(true)
    fetchEvents()
  }, [fetchEvents])

  useEffect(() => {
    if (!autoRefresh || selectedEvent) return
    timerRef.current = setInterval(fetchEvents, 5000)
    return () => clearInterval(timerRef.current)
  }, [autoRefresh, selectedEvent, fetchEvents])

  // SSE trigger: immediate refresh on hold_pending / hold_decided
  useEffect(() => {
    if (!notifications) return
    // notifications.pendingHolds changes when SSE fires — use it as a trigger
    fetchEvents()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [notifications?.pendingHolds])

  async function handleDecide(event, decision) {
    setOptimisticUpdates(prev => ({ ...prev, [event.id]: decision === 'approved' ? 'approved' : 'denied' }))
    setInlineErrors(prev => { const next = { ...prev }; delete next[event.id]; return next })
    try {
      await api.decideHold(event.id, decision)
      await fetchEvents()
    } catch (err) {
      setOptimisticUpdates(prev => { const next = { ...prev }; delete next[event.id]; return next })
      setInlineErrors(prev => ({ ...prev, [event.id]: err.message }))
    }
  }

  const displayEvents = events.map(ev => ({
    ...ev,
    status: optimisticUpdates[ev.id] ?? ev.status,
  }))

  const globalColumns = ['Time', 'Tool', 'Status', 'Rule', 'Severity', 'Agent', 'Session', 'Decided By']
  const sessionColumns = ['Time', 'Tool', 'Status', 'Rule', 'Severity', 'Decided By']
  const columns = mode === 'global' ? globalColumns : sessionColumns

  return (
    <div>
      {/* Toolbar */}
      <div className="flex items-center gap-3 mb-4 flex-wrap">
        {/* Status filter */}
        <select
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value)}
          className="bg-[#1d1a25] border border-[#574149]/20 text-[#e7dff0] text-xs rounded-sm px-2 py-1.5 focus:outline-none"
        >
          <option value="">All statuses</option>
          <option value="pending">Pending</option>
          <option value="approved">Approved</option>
          <option value="denied">Denied</option>
          <option value="auto_block">Auto-blocked</option>
          <option value="timeout">Timed Out</option>
          <option value="abandoned">Abandoned</option>
        </select>

        {/* include_allowed toggle */}
        <label className="flex items-center gap-2 cursor-pointer select-none">
          <div
            onClick={() => setIncludeAllowed(v => !v)}
            className={`w-8 h-4 rounded-full transition-colors ${includeAllowed ? 'bg-[#f256a3]' : 'bg-zinc-700'} relative`}
          >
            <span className={`absolute top-0.5 left-0.5 w-3 h-3 bg-white rounded-full transition-transform ${includeAllowed ? 'translate-x-4' : ''}`} />
          </div>
          <span className="text-[11px] text-zinc-400">Show allowed</span>
        </label>

        <div className="ml-auto flex items-center gap-2">
          {/* Refresh button */}
          <button
            onClick={fetchEvents}
            className="text-[11px] text-zinc-400 hover:text-[#e7dff0] transition-colors px-2 py-1"
          >
            <span className="material-symbols-outlined text-[14px] align-middle">refresh</span>
          </button>

          {/* Live pill */}
          <button
            onClick={() => setAutoRefresh(v => !v)}
            className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[10px] font-semibold transition-colors ${
              autoRefresh ? 'bg-emerald-900/40 text-emerald-400' : 'bg-zinc-800 text-zinc-500'
            }`}
          >
            <span className={`w-1.5 h-1.5 rounded-full ${autoRefresh ? 'bg-emerald-400 animate-pulse' : 'bg-zinc-600'}`} />
            {autoRefresh ? 'Live' : 'Paused'}
          </button>
        </div>
      </div>

      {/* High-volume warning */}
      {includeAllowed && (
        <div className="flex items-center gap-2 px-3 py-2 bg-amber-900/20 border border-amber-700/30 rounded-sm text-amber-300 text-[11px] mb-4">
          <span className="material-symbols-outlined text-[14px]">warning</span>
          Showing all tool call events including allowed — may be high volume
        </div>
      )}

      {/* Error banner */}
      {error && (
        <div className="flex items-center gap-2 px-3 py-2 bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm text-[#ffb4ab] text-[11px] mb-4">
          <span className="material-symbols-outlined text-[14px]">error</span>
          <span className="flex-1">{error}</span>
          <button onClick={() => { setError(null); setAutoRefresh(true); fetchEvents() }} className="font-semibold hover:underline">Retry</button>
        </div>
      )}

      {/* Table */}
      {loading ? (
        <div className="text-zinc-500 text-xs py-8 text-center">Loading…</div>
      ) : displayEvents.length === 0 ? (
        <div className="text-zinc-500 text-xs py-12 text-center">
          {sessionId
            ? 'No tool call activity for this session.'
            : includeAllowed
              ? 'No tool call events recorded yet.'
              : 'No tool call holds recorded yet. Holds appear here when a security rule triggers on an agent tool call.'}
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-zinc-500 text-[10px] uppercase tracking-widest border-b border-[#574149]/20">
                {columns.map(col => (
                  <th key={col} className="text-left py-2 px-3 font-semibold whitespace-nowrap">{col}</th>
                ))}
                <th className="py-2 px-3" />
              </tr>
            </thead>
            <tbody>
              {displayEvents.map(ev => (
                <tr
                  key={ev.id}
                  onClick={() => setSelectedEvent(ev)}
                  className={`border-b border-[#574149]/10 cursor-pointer transition-colors hover:bg-[#1d1a25] ${
                    ev.status === 'pending' ? 'border-l-2 border-l-amber-500/60' : ''
                  }`}
                >
                  <td className="py-2.5 px-3 text-zinc-500 whitespace-nowrap font-mono text-[10px]">{formatTs(ev.event_time)}</td>
                  <td className="py-2.5 px-3 font-mono text-[#93c5fd] whitespace-nowrap">{ev.tool_name || '—'}</td>
                  <td className="py-2.5 px-3"><StatusBadge status={ev.status} /></td>
                  <td className="py-2.5 px-3 text-zinc-400 max-w-[160px] truncate" title={ev.rule_name || ev.rule_id}>
                    {ev.rule_name || ev.rule_id || '—'}
                  </td>
                  <td className="py-2.5 px-3"><SeverityChip severity={ev.severity} /></td>
                  {mode === 'global' && (
                    <>
                      <td className="py-2.5 px-3 font-mono text-[10px] text-zinc-400 max-w-[100px] truncate">{ev.agent_id || '—'}</td>
                      <td className="py-2.5 px-3 font-mono text-[10px] text-zinc-600 max-w-[80px] truncate" title={ev.session_id}>
                        {ev.session_id ? ev.session_id.slice(0, 8) + '…' : '—'}
                      </td>
                    </>
                  )}
                  <td className="py-2.5 px-3 text-zinc-500 text-[10px] whitespace-nowrap">{ev.decided_by || '—'}</td>
                  <td className="py-2.5 px-3">
                    {ev.status === 'pending' && ev.is_hold_event && (
                      <div className="flex items-center gap-1.5" onClick={e => e.stopPropagation()}>
                        <button
                          onClick={() => handleDecide(ev, 'approved')}
                          className="px-2.5 py-1 bg-emerald-900/40 text-emerald-400 text-[10px] font-semibold rounded-sm hover:bg-emerald-900/60 transition-colors"
                        >
                          Approve
                        </button>
                        <button
                          onClick={() => handleDecide(ev, 'denied')}
                          className="px-2.5 py-1 bg-[#93000a]/40 text-[#ffb4ab] text-[10px] font-semibold rounded-sm hover:bg-[#93000a]/60 transition-colors"
                        >
                          Deny
                        </button>
                        {inlineErrors[ev.id] && (
                          <span className="text-[10px] text-[#ffb4ab]" title={inlineErrors[ev.id]}>!</span>
                        )}
                      </div>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Detail drawer */}
      {selectedEvent && (
        <ActivityDrawer
          event={selectedEvent}
          onClose={() => setSelectedEvent(null)}
          onDecided={fetchEvents}
        />
      )}
    </div>
  )
}
```

- [ ] **Step 2: Compile check (lint)**

```bash
cd frontend && npm run lint -- --quiet 2>&1 | grep -i "error" | head -20
```

Expected: no errors from ActivityLog.jsx.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ActivityLog.jsx
git commit -m "feat: add ActivityLog component with polling, SSE, inline approve/deny"
```

---

## Task 6: ActivityDrawer component

**Files:**
- Create: `frontend/src/components/ActivityDrawer.jsx`

- [ ] **Step 1: Create ActivityDrawer.jsx**

Create `frontend/src/components/ActivityDrawer.jsx`:

```jsx
import { useState, useEffect } from 'react'
import { api } from '../api/client'

function formatTs(ts) {
  if (!ts) return '—'
  try {
    const d = new Date(typeof ts === 'string' ? ts.replace(' ', 'T').replace(/\s+UTC$/, 'Z') : ts)
    if (isNaN(d)) return String(ts)
    return d.toLocaleString()
  } catch { return String(ts) }
}

function JsonBlock({ raw }) {
  if (!raw) return <span className="text-zinc-600 text-[11px]">(empty)</span>
  try {
    return (
      <pre className="text-[11px] font-mono text-zinc-300 whitespace-pre-wrap break-all leading-relaxed">
        {JSON.stringify(JSON.parse(raw), null, 2)}
      </pre>
    )
  } catch {
    return <pre className="text-[11px] font-mono text-zinc-300 whitespace-pre-wrap break-all">{raw}</pre>
  }
}

export default function ActivityDrawer({ event: initialEvent, onClose, onDecided }) {
  const [event, setEvent] = useState(initialEvent)
  const [timeline, setTimeline] = useState([])
  const [decideStatus, setDecideStatus] = useState('idle') // idle | pending | done | error
  const [decideReason, setDecideReason] = useState('')
  const [decideError, setDecideError] = useState('')

  // Fetch fresh hold state on open (handles stale pending rows)
  useEffect(() => {
    if (!initialEvent.is_hold_event) return
    api.getHold(initialEvent.id).then(data => {
      if (data?.hold) setEvent(prev => ({ ...prev, ...data.hold, id: data.hold.hold_id ?? prev.id }))
      if (data?.timeline) setTimeline(data.timeline)
    }).catch(() => { /* best-effort */ })
  }, [initialEvent.id, initialEvent.is_hold_event])

  async function handleDecide(decision) {
    setDecideStatus('pending')
    setDecideError('')
    try {
      await api.decideHold(event.id, decision, decideReason)
      setDecideStatus('done')
      onDecided?.()
      // Refresh hold state to show final decision
      const data = await api.getHold(event.id).catch(() => null)
      if (data?.hold) setEvent(prev => ({ ...prev, ...data.hold, id: data.hold.hold_id ?? prev.id }))
      if (data?.timeline) setTimeline(data.timeline)
    } catch (err) {
      setDecideStatus('error')
      setDecideError(err.message)
    }
  }

  const isPending = event.status === 'pending' && event.is_hold_event && decideStatus !== 'done'

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/40 z-40"
        onClick={onClose}
      />

      {/* Drawer */}
      <div className="fixed right-0 top-0 h-full w-[480px] max-w-full bg-[#15121d] border-l border-[#574149]/20 z-50 flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-[#574149]/20">
          <div>
            <h2 className="text-sm font-semibold text-[#e7dff0]">Tool Call Detail</h2>
            <p className="text-[10px] font-mono text-zinc-500 mt-0.5">{event.id}</p>
          </div>
          <button onClick={onClose} className="text-zinc-500 hover:text-zinc-300 transition-colors">
            <span className="material-symbols-outlined text-[18px]">close</span>
          </button>
        </div>

        {/* Body — scrollable */}
        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-5">

          {/* Summary row */}
          <div className="grid grid-cols-2 gap-3 text-[11px]">
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Tool</p>
              <p className="font-mono text-[#93c5fd]">{event.tool_name || '—'}</p>
            </div>
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Status</p>
              <p className="text-[#e7dff0]">{event.status}</p>
            </div>
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Rule</p>
              <p className="text-[#e7dff0]">{event.rule_name || event.rule_id || '—'}</p>
            </div>
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Severity</p>
              <p className={event.severity === 'critical' ? 'text-[#ffb4ab]' : event.severity === 'warning' ? 'text-amber-400' : 'text-zinc-400'}>
                {event.severity || '—'}
              </p>
            </div>
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Agent</p>
              <p className="font-mono text-[10px] text-zinc-400 truncate">{event.agent_id || '—'}</p>
            </div>
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1">Time</p>
              <p className="text-zinc-400">{formatTs(event.event_time)}</p>
            </div>
          </div>

          {/* Message */}
          {event.message && (
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1.5">Message</p>
              <p className="text-[11px] text-zinc-300">{event.message}</p>
            </div>
          )}

          {/* Arguments */}
          <div>
            <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-1.5">Arguments</p>
            <div className="bg-[#0f0c17] rounded-sm p-3 max-h-64 overflow-y-auto">
              <JsonBlock raw={event.args_summary || event.hook_payload} />
            </div>
          </div>

          {/* Decision timeline (hold events only) */}
          {event.is_hold_event && (
            <div>
              <p className="text-zinc-500 text-[10px] uppercase tracking-widest mb-2">Decision Timeline</p>
              {timeline.length > 0 ? (
                <div className="space-y-2">
                  {timeline.map((entry, i) => (
                    <div key={i} className="flex items-start gap-2.5 text-[11px]">
                      <span className="w-1.5 h-1.5 rounded-full bg-zinc-600 mt-1.5 shrink-0" />
                      <div>
                        <span className="text-zinc-400 font-medium capitalize">{entry.status}</span>
                        {entry.decided_by && <span className="text-zinc-600"> · {entry.decided_by}</span>}
                        {entry.decided_reason && <span className="text-zinc-600 italic"> — {entry.decided_reason}</span>}
                        <p className="text-zinc-600 text-[10px]">{formatTs(entry.updated_at)}</p>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <p className="text-[11px] text-zinc-600">No timeline entries yet.</p>
              )}
            </div>
          )}

          {/* Approve/Deny form — pending holds only */}
          {isPending && (
            <div className="bg-amber-900/20 border border-amber-700/30 rounded-sm p-4">
              <p className="text-[11px] font-semibold text-amber-300 mb-3">Agent is paused — waiting for decision</p>
              <textarea
                placeholder="Optional reason…"
                value={decideReason}
                onChange={e => setDecideReason(e.target.value)}
                className="w-full bg-[#0f0c17] border border-[#574149]/30 rounded-sm p-2 text-xs text-[#e7dff0] focus:border-zinc-500 focus:outline-none mb-3 resize-none"
                rows={2}
              />
              <div className="flex gap-2">
                <button
                  onClick={() => handleDecide('approved')}
                  disabled={decideStatus === 'pending'}
                  className="px-4 py-1.5 bg-emerald-500/20 text-emerald-400 text-xs font-semibold rounded-sm hover:bg-emerald-500/30 disabled:opacity-50 transition-colors"
                >
                  ✓ Approve
                </button>
                <button
                  onClick={() => handleDecide('denied')}
                  disabled={decideStatus === 'pending'}
                  className="px-4 py-1.5 bg-[#ffb4ab]/20 text-[#ffb4ab] text-xs font-semibold rounded-sm hover:bg-[#ffb4ab]/30 disabled:opacity-50 transition-colors"
                >
                  ✗ Deny
                </button>
              </div>
              {decideStatus === 'error' && (
                <p className="text-[11px] text-[#ffb4ab] mt-2">{decideError || 'Failed to submit. Try again.'}</p>
              )}
            </div>
          )}

          {/* Decision confirmed */}
          {decideStatus === 'done' && (
            <div className="bg-emerald-900/20 border border-emerald-700/30 rounded-sm p-3">
              <p className="text-xs text-emerald-400">Decision submitted — agent has been notified.</p>
            </div>
          )}
        </div>
      </div>
    </>
  )
}
```

- [ ] **Step 2: Lint check**

```bash
cd frontend && npm run lint -- --quiet 2>&1 | grep -i "error" | head -20
```

Expected: no errors from ActivityDrawer.jsx.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ActivityDrawer.jsx
git commit -m "feat: add ActivityDrawer component with detail view and approve/deny form"
```

---

## Task 7: Add Activity tab to Threats.jsx

**Files:**
- Modify: `frontend/src/pages/Threats.jsx`

- [ ] **Step 1: Rewrite Threats.jsx with tab bar**

Replace the full contents of `frontend/src/pages/Threats.jsx` with:

```jsx
import { useState } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import ThreatsList from '../components/ThreatsList'
import ActivityLog from '../components/ActivityLog'
import { useNotifications } from '../components/NotificationsProvider'
import { Link } from 'react-router-dom'

export default function Threats() {
  const [activeTab, setActiveTab] = useState('threats')
  const notifications = useNotifications()
  const pendingHoldsArr = Array.from(notifications?.pendingHolds?.values() ?? []).sort(
    (a, b) => new Date(b.created_at) - new Date(a.created_at)
  )

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />

      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-8">
          <PageHeader
            title="Threats"
            subtitle="Deduplicated security threats detected across AI agents."
          />

          {/* Pending holds banner — only on Threats tab */}
          {activeTab === 'threats' && pendingHoldsArr.length > 0 && (
            <section className="bg-[#1d1a25] rounded-sm p-4 mb-6">
              <h3 className="text-[11px] font-semibold uppercase tracking-widest text-[#ffb4ab] mb-3">
                ⚠ Pending Holds ({pendingHoldsArr.length})
              </h3>
              <ul className="divide-y divide-[#574149]/20">
                {pendingHoldsArr.map(h => (
                  <li key={h.hold_id} className="flex items-center justify-between py-2.5">
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 mb-0.5">
                        <span className={`text-[10px] uppercase font-semibold ${
                          h.severity === 'critical' ? 'text-[#ffb4ab]' :
                          h.severity === 'warning'  ? 'text-amber-400' : 'text-zinc-400'
                        }`}>
                          {h.severity}
                        </span>
                        <span className="text-xs text-[#e7dff0] font-mono">{h.agent_id}</span>
                        <span className="text-zinc-700">·</span>
                        <span className="text-xs text-[#e7dff0] font-mono">{h.tool_name}</span>
                      </div>
                      <p className="text-[11px] text-zinc-400 truncate">{h.message}</p>
                    </div>
                    <Link
                      to={`/threats/by-hold/${encodeURIComponent(h.hold_id)}`}
                      className="ml-4 px-3 py-1.5 text-[11px] font-semibold rounded bg-primary-button text-primary-button-text hover:scale-95 transition-transform"
                    >
                      Review →
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          )}

          {/* Tab bar */}
          <div className="flex items-center gap-1 mb-6 border-b border-[#574149]/20 pb-0">
            {['threats', 'activity'].map(tab => (
              <button
                key={tab}
                onClick={() => setActiveTab(tab)}
                className={`px-4 py-2 text-xs font-semibold capitalize transition-colors border-b-2 -mb-px ${
                  activeTab === tab
                    ? 'text-[#ffb0cf] border-[#ffb0cf]'
                    : 'text-zinc-500 border-transparent hover:text-zinc-300'
                }`}
              >
                {tab}
                {tab === 'activity' && pendingHoldsArr.length > 0 && (
                  <span className="ml-1.5 px-1.5 py-0.5 rounded-full bg-amber-900/60 text-amber-300 text-[9px] font-bold">
                    {pendingHoldsArr.length}
                  </span>
                )}
              </button>
            ))}
          </div>

          {activeTab === 'threats' && <ThreatsList showFilters />}
          {activeTab === 'activity' && <ActivityLog mode="global" />}
        </div>
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Lint check**

```bash
cd frontend && npm run lint -- --quiet 2>&1 | grep -i "error" | head -20
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/Threats.jsx
git commit -m "feat: add Threats | Activity tab bar to Threats page"
```

---

## Task 8: Add Activity tab to ThreatDetail.jsx

**Files:**
- Modify: `frontend/src/pages/ThreatDetail.jsx`

- [ ] **Step 1: Add import and tab state**

In `frontend/src/pages/ThreatDetail.jsx`, add to the import block at the top:

```js
import ActivityLog from '../components/ActivityLog'
```

Then find the first `useState` inside the `ThreatDetail` function (around line 150 of the original file) and add a tab state alongside the existing state:

```js
const [activeTab, setActiveTab] = useState('overview')
```

- [ ] **Step 2: Find and wrap existing content with tab bar**

In `ThreatDetail.jsx`, find the opening `<div className="p-8 space-y-8">` block (around line 406 of the original file). Replace it with:

```jsx
<div className="p-8">
  {/* Tab bar */}
  <div className="flex items-center gap-1 mb-8 border-b border-[#574149]/20 pb-0">
    {['overview', 'activity'].map(tab => (
      <button
        key={tab}
        onClick={() => setActiveTab(tab)}
        className={`px-4 py-2 text-xs font-semibold capitalize transition-colors border-b-2 -mb-px ${
          activeTab === tab
            ? 'text-[#ffb0cf] border-[#ffb0cf]'
            : 'text-zinc-500 border-transparent hover:text-zinc-300'
        }`}
      >
        {tab}
      </button>
    ))}
  </div>

  {activeTab === 'activity' && (
    <ActivityLog mode="session" sessionId={threat.session_id} threatId={threat.threat_id} />
  )}

  {activeTab === 'overview' && (
    <div className="space-y-8">
```

Then find the closing `</div>` of the `<div className="p-8 space-y-8">` block (at the very end of the JSX content branch, just before the `</main>`) and change it to:

```jsx
    </div>
  )}
</div>
```

- [ ] **Step 3: Lint check**

```bash
cd frontend && npm run lint -- --quiet 2>&1 | grep -i "error" | head -20
```

Expected: no errors.

- [ ] **Step 4: Build check**

```bash
cd frontend && npm run build 2>&1 | tail -20
```

Expected: build completes without errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/ThreatDetail.jsx
git commit -m "feat: add Overview | Activity tab bar to ThreatDetail page"
```

---

## Task 9: End-to-end verification

- [ ] **Step 1: Start backend and frontend**

```bash
# Terminal 1
cd backend && CGO_ENABLED=0 go build -o /tmp/agserver cmd/server/main.go && /tmp/agserver

# Terminal 2
cd frontend && npm run dev
```

- [ ] **Step 2: Verify global Activity tab**

Open `http://localhost:5173/threats` and click the "Activity" tab. Expected:
- Tab bar renders with "threats" and "activity" options
- Activity tab shows the ActivityLog component
- If no holds: empty state message appears
- Toolbar shows status filter dropdown, "Show allowed" toggle, Live pill, refresh button

- [ ] **Step 3: Verify include_allowed toggle**

Toggle "Show allowed" on. Expected:
- High-volume warning banner appears
- Backend receives `include_allowed=true` query param (check network tab)

- [ ] **Step 4: Verify ThreatDetail Activity tab**

Open any threat at `http://localhost:5173/threats/:id`. Expected:
- Tab bar with "overview" and "activity" appears below the threat header
- Activity tab renders `<ActivityLog mode="session">` with session_id filter

- [ ] **Step 5: Verify inline approve/deny (requires a pending hold)**

If a pending hold exists, the row in the Activity list should:
- Have amber left border
- Show "Approve" and "Deny" buttons
- Clicking Approve updates the row status optimistically before API response

- [ ] **Step 6: Verify detail drawer**

Click any row in the Activity list. Expected:
- Drawer slides in from right
- Tool name, status, rule, severity, agent, time shown in summary grid
- Arguments section shows JSON
- Timeline section appears for hold events
- Approve/Deny form appears only for pending hold events
- Clicking backdrop closes drawer

- [ ] **Step 7: Run backend tests**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/handlers/... -v 2>&1 | tail -30
```

Expected: all tests PASS.
