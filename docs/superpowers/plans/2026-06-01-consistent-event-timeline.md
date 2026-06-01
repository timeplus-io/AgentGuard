# Consistent Event Timeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the AgentGuard Timeline tab with a single, agent-agnostic view driven by the normalized `agentguard_cim_event` stream, showing user input, LLM response, tool request, and tool result for every agent type.

**Architecture:** A new session-scoped backend endpoint (`GET /api/agents/:id/sessions/:session_id/timeline`) queries `agentguard_cim_event`, and a pure Go builder maps canonical `event_type` values to display kinds, stamps a `turn_index`, and parses `tool_call_id` from `raw_event_data`. The frontend Timeline tab is rewritten to render the flat, ordered list grouped by turn. The Trace tab (waterfall), `GetRunTrace`, `TraceSpan`, and the three `timeline_*.go` analyzers are left untouched.

**Tech Stack:** Go (Gin), Timeplus Enterprise (`QueryNative` native binary protocol), React (Vite), Tailwind.

**Spec:** `docs/superpowers/specs/2026-06-01-consistent-event-timeline-design.md`

---

## File Structure

| File | Responsibility |
|---|---|
| `backend/internal/models/models.go` | Add `TimelineEvent` + `TimelineResponse` structs |
| `backend/internal/handlers/session_timeline.go` (new) | `GetSessionTimeline` handler, `buildTimeline` pure builder, `parseToolCallID` helper |
| `backend/internal/handlers/session_timeline_test.go` (new) | Table-driven tests for `buildTimeline` + `parseToolCallID` |
| `backend/cmd/server/main.go` | Register the new route |
| `frontend/src/api/client.js` | Add `getSessionTimeline` |
| `frontend/src/pages/AgentDetail.jsx` | Rewrite Timeline tab; remove `RunTimeline`/`buildRunData` |

---

## Task 1: Backend models

**Files:**
- Modify: `backend/internal/models/models.go` (append after `TraceResponse`, around line 344)

- [ ] **Step 1: Add the structs**

Append to `backend/internal/models/models.go` immediately after the `TraceResponse` struct (around line 344):

```go
// TimelineEvent is one normalized item in the session timeline, built from
// agentguard_cim_event. Kind is one of:
//   "user_input" | "llm_response" | "tool_request" | "tool_result"
//   | "session_start" | "session_end".
// Status is "ok" | "blocked" | "error".
type TimelineEvent struct {
	EventTime  time.Time         `json:"event_time"`
	Kind       string            `json:"kind"`
	TurnIndex  int               `json:"turn_index"`
	RunID      string            `json:"run_id"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	Title      string            `json:"title"`
	Body       string            `json:"body"`
	ToolName   string            `json:"tool_name,omitempty"`
	Model      string            `json:"model,omitempty"`
	Provider   string            `json:"provider,omitempty"`
	Status     string            `json:"status"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// TimelineResponse is returned by GET /api/agents/:id/sessions/:session_id/timeline.
// Events are ordered by EventTime ascending.
type TimelineResponse struct {
	Events []TimelineEvent `json:"events"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd backend && CGO_ENABLED=0 go build ./internal/models/`
Expected: no output (success). `time` is already imported by this file.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/models/models.go
git commit -m "feat(timeline): add TimelineEvent and TimelineResponse models (#51)"
```

---

## Task 2: The builder (`buildTimeline`) + `parseToolCallID` — TDD

**Files:**
- Create: `backend/internal/handlers/session_timeline.go`
- Test: `backend/internal/handlers/session_timeline_test.go`

The builder is a pure function over CIM rows (already time-ordered by the SQL query). It has no DB dependency, so it is fully unit-testable.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/handlers/session_timeline_test.go`:

```go
package handlers

import (
	"testing"
	"time"
)

// row is a tiny helper to build a CIM-event row map for tests.
func row(eventType string, fields map[string]any) map[string]any {
	r := map[string]any{
		"event_time": time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		"event_type": eventType,
	}
	for k, v := range fields {
		r[k] = v
	}
	return r
}

func TestBuildTimeline_KindMapping(t *testing.T) {
	rows := []map[string]any{
		row("session_start", nil),
		row("user_input", map[string]any{"user_message": "hello"}),
		row("llm_response", map[string]any{"assistant_message": "hi there", "model": "claude-sonnet-4-6", "provider": "anthropic"}),
		row("tool_invoke", map[string]any{"tool_name": "Bash", "tool_input": `{"command":"ls"}`, "raw_event_data": `{"toolCallId":"tc-1"}`}),
		row("tool_complete", map[string]any{"tool_name": "Bash", "tool_result": `{"stdout":"a"}`, "tool_success": true, "raw_event_data": `{"toolCallId":"tc-1"}`}),
		row("session_end", nil),
	}

	got := buildTimeline(rows)
	if len(got) != 6 {
		t.Fatalf("expected 6 events, got %d", len(got))
	}

	wantKinds := []string{"session_start", "user_input", "llm_response", "tool_request", "tool_result", "session_end"}
	for i, want := range wantKinds {
		if got[i].Kind != want {
			t.Errorf("event %d: kind = %q, want %q", i, got[i].Kind, want)
		}
	}

	if got[1].Body != "hello" {
		t.Errorf("user_input body = %q, want %q", got[1].Body, "hello")
	}
	if got[2].Body != "hi there" || got[2].Model != "claude-sonnet-4-6" {
		t.Errorf("llm_response body/model = %q/%q", got[2].Body, got[2].Model)
	}
	if got[3].Body != `{"command":"ls"}` || got[3].ToolName != "Bash" || got[3].ToolCallID != "tc-1" {
		t.Errorf("tool_request mismatch: %+v", got[3])
	}
	if got[4].Body != `{"stdout":"a"}` || got[4].ToolCallID != "tc-1" || got[4].Status != "ok" {
		t.Errorf("tool_result mismatch: %+v", got[4])
	}
}

func TestBuildTimeline_TurnIndex(t *testing.T) {
	rows := []map[string]any{
		row("session_start", nil),                                  // turn 0
		row("user_input", map[string]any{"user_message": "first"}), // turn 1
		row("llm_response", map[string]any{"assistant_message": "a"}),
		row("user_input", map[string]any{"user_message": "second"}), // turn 2
		row("llm_response", map[string]any{"assistant_message": "b"}),
	}
	got := buildTimeline(rows)
	wantTurns := []int{0, 1, 1, 2, 2}
	for i, want := range wantTurns {
		if got[i].TurnIndex != want {
			t.Errorf("event %d: turn_index = %d, want %d", i, got[i].TurnIndex, want)
		}
	}
}

func TestBuildTimeline_ToolCallIDSpellings(t *testing.T) {
	rows := []map[string]any{
		row("tool_invoke", map[string]any{"raw_event_data": `{"tool_call_id":"hermes-7"}`}),     // Hermes
		row("tool_invoke", map[string]any{"raw_event_data": `{"toolCallId":"openclaw-3"}`}),      // OpenClaw
		row("tool_invoke", map[string]any{"raw_event_data": `{"foo":"bar"}`}),                    // Claude Code: none
	}
	got := buildTimeline(rows)
	if got[0].ToolCallID != "hermes-7" {
		t.Errorf("hermes tool_call_id = %q, want hermes-7", got[0].ToolCallID)
	}
	if got[1].ToolCallID != "openclaw-3" {
		t.Errorf("openclaw tool_call_id = %q, want openclaw-3", got[1].ToolCallID)
	}
	if got[2].ToolCallID != "" {
		t.Errorf("claudecode tool_call_id = %q, want empty", got[2].ToolCallID)
	}
}

func TestBuildTimeline_Status(t *testing.T) {
	rows := []map[string]any{
		row("tool_invoke", map[string]any{"hook_decision": "block", "block_reason": "policy"}),
		row("tool_complete", map[string]any{"tool_success": false}),
		row("tool_complete", map[string]any{"tool_success": true}),
	}
	got := buildTimeline(rows)
	if got[0].Status != "blocked" {
		t.Errorf("blocked invoke status = %q, want blocked", got[0].Status)
	}
	if got[0].Attributes["block_reason"] != "policy" {
		t.Errorf("block_reason attr = %q, want policy", got[0].Attributes["block_reason"])
	}
	if got[1].Status != "error" {
		t.Errorf("failed tool status = %q, want error", got[1].Status)
	}
	if got[2].Status != "ok" {
		t.Errorf("ok tool status = %q, want ok", got[2].Status)
	}
}

func TestBuildTimeline_SkipsUnknownTypes(t *testing.T) {
	rows := []map[string]any{
		row("user_input", map[string]any{"user_message": "x"}),
		row("prompt_context", map[string]any{"raw_event_data": "{}"}),
		row("other", nil),
	}
	got := buildTimeline(rows)
	if len(got) != 1 {
		t.Fatalf("expected 1 event (unknowns skipped), got %d", len(got))
	}
	if got[0].Kind != "user_input" {
		t.Errorf("kept kind = %q, want user_input", got[0].Kind)
	}
}

func TestBuildTimeline_EmptyReturnsNonNil(t *testing.T) {
	got := buildTimeline(nil)
	if got == nil {
		t.Fatal("buildTimeline(nil) returned nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 events, got %d", len(got))
	}
}

func TestParseToolCallID(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"toolCallId":"a"}`, "a"},
		{`{"tool_call_id":"b"}`, "b"},
		{`{"toolCallId":"a","tool_call_id":"b"}`, "a"}, // toolCallId wins (tried first)
		{`{"other":"x"}`, ""},
		{``, ""},
		{`not json`, ""},
	}
	for _, c := range cases {
		if got := parseToolCallID(c.raw); got != c.want {
			t.Errorf("parseToolCallID(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run 'TestBuildTimeline|TestParseToolCallID' -v`
Expected: FAIL — `undefined: buildTimeline` / `undefined: parseToolCallID`.

- [ ] **Step 3: Write the implementation**

Create `backend/internal/handlers/session_timeline.go` with the builder + helper (the handler is added in Task 3, but lives in the same file — write the whole file now so imports settle once):

```go
package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// timelineEventTypes are the canonical CIM event types the timeline renders.
const timelineEventTypes = "'user_input','llm_response','tool_invoke','tool_complete','session_start','session_end'"

// GetSessionTimeline handles
// GET /api/agents/:id/sessions/:session_id/timeline (optional ?run_id=).
// It queries the normalized agentguard_cim_event stream and returns an ordered,
// agent-agnostic list of timeline events.
func GetSessionTimeline(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format"})
			return
		}

		sessionID := c.Param("session_id")
		if !idPartRe.MatchString(sessionID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session_id format"})
			return
		}

		where := []string{
			fmt.Sprintf("deployment_id = '%s'", escapeSQL(deploymentID)),
			fmt.Sprintf("agent_id = '%s'", escapeSQL(agentID)),
			fmt.Sprintf("session_id = '%s'", escapeSQL(sessionID)),
			"event_type IN (" + timelineEventTypes + ")",
		}

		if rid := c.Query("run_id"); rid != "" {
			if !runIDRe.MatchString(rid) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run_id format"})
				return
			}
			where = append(where, fmt.Sprintf("run_id = '%s'", escapeSQL(rid)))
		}

		sql := fmt.Sprintf(`
SELECT event_time, event_type, run_id, tool_name, tool_input, tool_result,
       tool_success, user_message, assistant_message, model, provider,
       hook_decision, block_reason, raw_event_data
FROM table(agentguard_cim_event)
WHERE %s
ORDER BY event_time ASC`, strings.Join(where, "\n  AND "))

		// Native binary protocol: user/assistant/tool content can break the
		// HTTP+JSON serializer.
		rows, err := timeplus.QueryNative(&cfg.Timeplus, sql)
		if err != nil {
			log.Printf("timeline: GetSessionTimeline query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		c.JSON(http.StatusOK, models.TimelineResponse{Events: buildTimeline(rows)})
	}
}

// buildTimeline maps time-ordered CIM-event rows into normalized timeline events.
// It is agent-agnostic: it switches on the canonical event_type only. A turn is
// one user_input plus everything until the next user_input; events before the
// first user_input are turn 0.
func buildTimeline(rows []map[string]any) []models.TimelineEvent {
	events := make([]models.TimelineEvent, 0, len(rows))
	turnIndex := 0

	for _, r := range rows {
		eventType := parseStr(r["event_type"])

		ev := models.TimelineEvent{
			EventTime: parseTime(r["event_time"]),
			RunID:     parseStr(r["run_id"]),
			Status:    "ok",
		}

		switch eventType {
		case "user_input":
			turnIndex++
			ev.Kind = "user_input"
			ev.Title = "User"
			ev.Body = parseStr(r["user_message"])

		case "llm_response":
			ev.Kind = "llm_response"
			ev.Model = parseStr(r["model"])
			ev.Provider = parseStr(r["provider"])
			ev.Title = ev.Model
			if ev.Title == "" {
				ev.Title = "Assistant"
			}
			ev.Body = parseStr(r["assistant_message"])

		case "tool_invoke":
			ev.Kind = "tool_request"
			ev.ToolName = parseStr(r["tool_name"])
			ev.Title = ev.ToolName
			ev.Body = parseStr(r["tool_input"])
			ev.ToolCallID = parseToolCallID(parseStr(r["raw_event_data"]))
			if parseStr(r["hook_decision"]) == "block" {
				ev.Status = "blocked"
				if reason := parseStr(r["block_reason"]); reason != "" {
					ev.Attributes = map[string]string{"block_reason": reason}
				}
			}

		case "tool_complete":
			ev.Kind = "tool_result"
			ev.ToolName = parseStr(r["tool_name"])
			ev.Title = ev.ToolName
			ev.Body = parseStr(r["tool_result"])
			ev.ToolCallID = parseToolCallID(parseStr(r["raw_event_data"]))
			if v, ok := r["tool_success"]; ok && !parseBool(v) {
				ev.Status = "error"
			}

		case "session_start":
			ev.Kind = "session_start"
			ev.Title = "Session start"

		case "session_end":
			ev.Kind = "session_end"
			ev.Title = "Session end"

		default:
			// Unknown / non-timeline event type — skip.
			continue
		}

		ev.TurnIndex = turnIndex
		events = append(events, ev)
	}

	return events
}

// parseToolCallID extracts a tool-call id from a raw CIM event_data JSON string.
// It tries the known agent spellings in order: toolCallId (OpenClaw), then
// tool_call_id (Hermes). Returns "" when absent or unparseable (e.g. Claude Code).
func parseToolCallID(raw string) string {
	if raw == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	for _, key := range []string{"toolCallId", "tool_call_id"} {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run 'TestBuildTimeline|TestParseToolCallID' -v`
Expected: PASS for all six builder tests + `TestParseToolCallID`.

- [ ] **Step 5: Confirm the whole package still compiles**

Run: `cd backend && CGO_ENABLED=0 go build ./...`
Expected: no output (success). Confirms `parseBool` / `parseStr` / `parseTime` / `escapeSQL` / `splitAgentID` / `idPartRe` / `runIDRe` references all resolve.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/handlers/session_timeline.go backend/internal/handlers/session_timeline_test.go
git commit -m "feat(timeline): add cim_event timeline builder and handler (#51)"
```

---

## Task 3: Register the route

**Files:**
- Modify: `backend/cmd/server/main.go:157-159`

- [ ] **Step 1: Add the route**

In `backend/cmd/server/main.go`, after the existing trace route (line 158), add the timeline route:

```go
			protected.GET("/agents/:id/sessions/:session_id", handlers.GetSession(cfg))
			protected.GET("/agents/:id/sessions/:session_id/runs/:run_id/trace", handlers.GetRunTrace(cfg))
			protected.GET("/agents/:id/sessions/:session_id/timeline", handlers.GetSessionTimeline(cfg))
			protected.GET("/agents/:id/events", handlers.ListEvents(cfg))
```

- [ ] **Step 2: Build the server binary**

Run: `cd backend && CGO_ENABLED=0 go build -o /tmp/ag-server cmd/server/main.go`
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(timeline): register session timeline route (#51)"
```

---

## Task 4: Frontend API client method

**Files:**
- Modify: `frontend/src/api/client.js:85-91`

- [ ] **Step 1: Add `getSessionTimeline`**

In `frontend/src/api/client.js`, add the method right after `getRunTrace` (line 87):

```js
  getRunTrace: (agentId, sessionId, runId) =>
    get(`/agents/${agentId}/sessions/${sessionId}/runs/${encodeURIComponent(runId)}/trace`),
  getSessionTimeline: (agentId, sessionId, params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/agents/${agentId}/sessions/${sessionId}/timeline${q ? '?' + q : ''}`)
  },
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/api/client.js
git commit -m "feat(timeline): add getSessionTimeline API client method (#51)"
```

---

## Task 5: Rewrite the Timeline tab in AgentDetail.jsx

This task swaps the Timeline tab's data source from `listEvents` (raw hooks, re-normalized in JS) to the new CIM-backed endpoint, adds a `TurnTimeline` renderer grouped by `turn_index`, and removes the now-dead `RunTimeline` + `buildRunData`.

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx`

- [ ] **Step 1: Add timeline state**

In `AgentDetail()` (after `const selectedRun = searchParams.get('run_id')`, near the other `useState` calls around line 1305), add:

```jsx
  const [timeline, setTimeline] = useState([])
  const [timelineError, setTimelineError] = useState(null)
  const [loadingTimeline, setLoadingTimeline] = useState(false)
```

- [ ] **Step 2: Replace the timeline fetch effect**

Replace the existing Timeline-tab effect (the `useEffect` that calls `api.listEvents`, currently around lines 1392–1414) with a CIM-backed fetch:

```jsx
  useEffect(() => {
    if (!selectedSession) { setTimeline([]); setTimelineError(null); return }
    const params = {}
    if (selectedRun) params.run_id = selectedRun

    setLoadingTimeline(true)
    api.getSessionTimeline(id, selectedSession, params)
      .then(r => { setTimeline(r.events ?? []); setTimelineError(null) })
      .catch(err => { setTimeline([]); setTimelineError(err?.message || 'Failed to load timeline') })
      .finally(() => setLoadingTimeline(false))
  }, [id, selectedSession, selectedRun, refreshTick])
```

> Note: the old effect also managed `events`, `eventsTruncated`, and `eventsError`. Those state setters may still exist; leave their declarations for now — Step 6's lint pass will flag any that are genuinely unused after this rewrite.

- [ ] **Step 3: Add the `TurnTimeline` component and kind metadata**

Add this above the `AgentDetail` component (e.g. just before `// ── Main page ──` around line 1297), replacing the `RunTimeline` function:

```jsx
const TIMELINE_KIND_META = {
  user_input:    { label: 'User Input',     dot: '#a5b4fc' },
  llm_response:  { label: 'LLM Response',   dot: '#ffb0cf' },
  tool_request:  { label: 'Tool Request',   dot: '#93c5fd' },
  tool_result:   { label: 'Tool Result',    dot: '#93c5fd' },
  session_start: { label: 'Session Start',  dot: '#6b7280' },
  session_end:   { label: 'Session End',    dot: '#6b7280' },
}

function TimelineBody({ kind, body }) {
  if (!body) return null
  const isCode = kind === 'tool_request' || kind === 'tool_result'
  const text = body.length > 800 ? body.slice(0, 800) + '…' : body
  if (isCode) {
    return (
      <pre className="mt-1.5 bg-[#0f0c17] rounded-sm px-2 py-1.5 text-[10px] font-mono text-gray-100 break-all whitespace-pre-wrap overflow-x-auto">
        {text}
      </pre>
    )
  }
  return (
    <p className="text-xs mt-0.5 break-words leading-relaxed whitespace-pre-wrap text-gray-100">
      {text}
    </p>
  )
}

function TurnTimeline({ turnIndex, events }) {
  return (
    <div className="bg-[#1d1a25] rounded-sm p-5 mb-3 last:mb-0">
      <div className="flex items-center gap-3 mb-3 flex-wrap">
        <span className="text-xs font-semibold text-gray-100">
          {turnIndex === 0 ? 'Session' : `Turn #${turnIndex}`}
        </span>
        <span className="text-[10px] text-gray-400">{events.length} events</span>
      </div>
      <div>
        {events.map((ev, i) => {
          const meta = TIMELINE_KIND_META[ev.kind] ?? { label: ev.kind, dot: '#6b7280' }
          const alert = ev.status === 'blocked' || ev.status === 'error'
          return (
            <div key={`${ev.kind}-${i}`} className="flex gap-3">
              <div className="flex flex-col items-center">
                <div
                  className="w-2 h-2 rounded-full shrink-0 mt-1"
                  style={{ backgroundColor: alert ? '#ffb4ab' : meta.dot }}
                />
                {i < events.length - 1 && (
                  <div className="w-px flex-1 bg-[#574149]/20 my-1" style={{ minHeight: '12px' }} />
                )}
              </div>
              <div className="pb-3 flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className={`text-[10px] font-semibold uppercase tracking-wide ${alert ? 'text-[#ffb4ab]' : 'text-gray-400'}`}>
                    {meta.label}
                  </span>
                  {ev.tool_name && <code className="text-[10px] font-mono text-[#93c5fd]">{ev.tool_name}</code>}
                  {ev.model && <span className="text-[10px] text-gray-400 font-mono">{ev.model}</span>}
                  {ev.status === 'blocked' && <span className="text-[10px] text-[#ffb4ab]">blocked</span>}
                  {ev.status === 'error' && <span className="text-[10px] text-[#ffb4ab]">error</span>}
                  <span className="ml-auto text-[10px] font-mono text-gray-400">{fmtShortTime(ev.event_time)}</span>
                </div>
                <TimelineBody kind={ev.kind} body={ev.body} />
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Replace the Timeline tab render block**

Find the Timeline tab render — the IIFE that builds `byRun` and maps `RunTimeline` (currently around lines 1790–1848, the `else` branch labeled `// Timeline tab`). Replace that entire IIFE body with:

```jsx
                // Timeline tab
                (() => {
                  if (timelineError) {
                    return (
                      <div className="flex flex-col items-center justify-center h-full text-[#ffb4ab] gap-2 p-6 text-center">
                        <span className="material-symbols-outlined text-4xl">error</span>
                        <p className="text-sm font-semibold">Failed to load timeline</p>
                        <p className="text-xs text-gray-400 max-w-md break-words">{timelineError}</p>
                      </div>
                    )
                  }
                  if (loadingTimeline && timeline.length === 0) {
                    return (
                      <div className="flex items-center justify-center h-full">
                        <span className="material-symbols-outlined animate-spin text-gray-400 text-3xl">refresh</span>
                      </div>
                    )
                  }
                  if (timeline.length === 0) {
                    return (
                      <div className="flex flex-col items-center justify-center h-full text-gray-400 gap-2">
                        <span className="material-symbols-outlined text-4xl">inbox</span>
                        <p className="text-sm">No timeline events for this session</p>
                      </div>
                    )
                  }
                  const turns = new Map()
                  for (const ev of timeline) {
                    if (!turns.has(ev.turn_index)) turns.set(ev.turn_index, [])
                    turns.get(ev.turn_index).push(ev)
                  }
                  const indexes = [...turns.keys()].sort((a, b) => a - b)
                  return (
                    <div className="p-4">
                      {indexes.map(idx => (
                        <TurnTimeline key={idx} turnIndex={idx} events={turns.get(idx)} />
                      ))}
                    </div>
                  )
                })()
```

- [ ] **Step 5: Remove dead code (`RunTimeline`, `buildRunData`, `stepDot`)**

Delete the now-unreferenced Timeline-tab helpers that fed the old per-hook rendering:
- the `RunTimeline` function (already replaced in Step 3),
- the `buildRunData` function (the per-hook normalizer, ~lines 190–461),
- the `stepDot` helper (used only by the old `RunTimeline`).

Do **not** touch `RunTrace`/waterfall code, `RunEventCard`, `hookBadgeColor`, or the span-color helper used by the Trace tab — those belong to other views.

- [ ] **Step 6: Lint and fix unused symbols**

Run: `cd frontend && npm run lint`
Expected: PASS. If ESLint reports unused variables (e.g. leftover `events` / `eventsTruncated` / `eventsError` state, or the old `listEvents` import usage, or `byRun`), remove exactly those unused declarations and re-run until clean. Do not remove anything still referenced by the Trace tab or other tabs.

- [ ] **Step 7: Build the frontend**

Run: `cd frontend && npm run build`
Expected: build succeeds, no errors.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/pages/AgentDetail.jsx
git commit -m "feat(timeline): render Timeline tab from cim_event endpoint (#51)"
```

---

## Task 6: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Backend tests + build**

Run: `cd backend && CGO_ENABLED=0 go test ./... && CGO_ENABLED=0 go build ./...`
Expected: all tests PASS, build succeeds. Confirms the untouched Trace-tab analyzer tests (`timeline_openclaw_test.go`, `timeline_fallback_test.go`) still pass.

- [ ] **Step 2: Frontend lint + build**

Run: `cd frontend && npm run lint && npm run build`
Expected: both succeed.

- [ ] **Step 3: Manual smoke test (requires Timeplus with data)**

Start the stack (`make dev`), open an agent with sessions, and for each available agent type (Claude Code, OpenClaw, Hermes):
- Open a session → Timeline tab.
- Confirm the four kinds appear in chronological order: User Input → LLM Response → Tool Request → Tool Result.
- Confirm a blocked tool shows the "blocked" marker and a failed tool shows "error".
- Confirm selecting a specific run in the sidebar narrows the timeline (passes `?run_id=`).
- Confirm the Trace tab still renders its waterfall unchanged.

> If no live Timeplus is available, verify the endpoint shape with a direct curl once a session id is known:
> `curl -s 'http://localhost:8080/api/agents/<dep>:<agent>/sessions/<sid>/timeline' | jq '.events[0]'`
> Expected keys: `event_time, kind, turn_index, run_id, title, body, status` (+ optional `tool_name`, `model`, `tool_call_id`).

- [ ] **Step 4: Final commit (if any lint/cleanup changes remain)**

```bash
git add -A
git commit -m "chore(timeline): verification cleanup (#51)"
```

---

## Self-Review Notes

- **Spec coverage:** endpoint (§1) → Task 2/3; response shape (§2) → Task 1; builder mapping + turn grouping + tool_call_id (§3) → Task 2; frontend rewrite (§4) → Task 4/5; route (§5) → Task 3; error handling (empty → `200 []`, query fail → `503`) → Task 2 (`buildTimeline` returns non-nil; handler returns 503); testing (§Testing) → Task 2 tests; "untouched" guarantees → verified in Task 6 Step 1/3.
- **Types consistent:** `TimelineEvent`/`TimelineResponse` field names and JSON tags used in Task 1 match the builder in Task 2 (`Kind`, `TurnIndex`, `ToolCallID`, `Status`, `Attributes`) and the frontend reads matching JSON keys (`kind`, `turn_index`, `tool_name`, `event_time`, `body`, `status`, `model`).
- **No placeholders:** every code step shows complete code; the only deletion-by-description step (Task 5 Step 5) names exact functions and is backstopped by the lint pass.
