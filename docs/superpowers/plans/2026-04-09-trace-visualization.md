# Trace Visualization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Trace" tab to the AgentDetail run view that renders a Jaeger-style waterfall diagram synthesized from `agentshield_hook_events` (run bounds + tool spans) and `otel_traces` (LLM call spans with exact timing).

**Architecture:** A new backend endpoint queries both Proton streams, assembles a flat `[]TraceSpan` list with parent-id references, and returns it as JSON. The frontend adds a third tab to the existing tab bar and renders the waterfall using a new `RunWaterfall` component co-located in `AgentDetail.jsx`. OTel query failures are non-fatal — the trace renders with run + tool spans only.

**Tech Stack:** Go/Gin backend, React/Tailwind frontend, Timeplus Proton SQL (two streams: `agentshield_hook_events` and `otel_traces`).

---

## File Structure

| File | Change |
|------|--------|
| `backend/internal/models/models.go` | Add `TraceSpan`, `TraceResponse` structs |
| `backend/internal/handlers/agent_sessions.go` | Add `GetRunTrace` handler + `parseInt64` helper |
| `backend/cmd/server/main.go` | Register `GET /agents/:id/sessions/:session_id/runs/:run_id/trace` |
| `frontend/src/api/client.js` | Add `getRunTrace` function |
| `frontend/src/pages/AgentDetail.jsx` | Add `RunWaterfall` component, Trace tab, state, fetch effect |

---

### Task 1: Backend Models

**Files:**
- Modify: `backend/internal/models/models.go` (append after `EventsResponse`)

- [ ] **Step 1: Add TraceSpan and TraceResponse to models.go**

Open `backend/internal/models/models.go`. After the `EventsResponse` struct (around line 205), add:

```go
// TraceSpan is one node in the run trace waterfall.
// Kind is "run" | "llm" | "tool".
// Status is "ok" | "error" | "blocked".
// ParentID is "" for the root run span; run_id for all children.
type TraceSpan struct {
	SpanID     string            `json:"span_id"`
	ParentID   string            `json:"parent_id"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	StartMs    int64             `json:"start_ms"`
	DurationMs int64             `json:"duration_ms"`
	Status     string            `json:"status"`
	Attributes map[string]string `json:"attributes"`
}

// TraceResponse is returned by GET /api/agents/:id/sessions/:sid/runs/:rid/trace.
// Spans are sorted by StartMs ascending; root span always has ParentID == "".
type TraceResponse struct {
	Spans []TraceSpan `json:"spans"`
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output (clean compile).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/models/models.go
git commit -m "feat(trace): add TraceSpan and TraceResponse models"
```

---

### Task 2: Backend Handler

**Files:**
- Modify: `backend/internal/handlers/agent_sessions.go`

**Context:**
- `idPartRe` is defined in `backend/internal/handlers/agents.go` as `^[a-zA-Z0-9._-]+$` — already visible to this package.
- `runIDRe` is defined in this same file as `^[a-zA-Z0-9._:/-]+$` — use it for run_id validation.
- `timeplus.Query` returns `[]map[string]interface{}`. Map columns (`span_attributes`) come back as `map[string]interface{}` where values are strings.
- The existing helpers `parseTime`, `parseInt`, `parseBool`, `parseStr` are all in this file.

- [ ] **Step 1: Add parseInt64 helper**

At the end of the helpers block (after `parseStr`, around line 98), add:

```go
// parseInt64 handles large integer columns (uint64 OTel nanosecond timestamps).
func parseInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
	case int64:
		return n
	case uint64:
		return int64(n)
	}
	return 0
}
```

- [ ] **Step 2: Add GetRunTrace handler**

Add the following imports to the import block if not already present: `log`, `sort`.

The full import block for `agent_sessions.go` should be:

```go
import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	"github.com/timeplus/agentshield/internal/timeplus"
)
```

Append the `GetRunTrace` function at the end of `agent_sessions.go`:

```go
// GetRunTrace handles GET /api/agents/:id/sessions/:session_id/runs/:run_id/trace
// It synthesizes a waterfall span tree from agentshield_hook_events (run bounds +
// tool spans) and otel_traces (LLM call spans). OTel query failure is non-fatal.
func GetRunTrace(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		runID := c.Param("run_id")
		if !runIDRe.MatchString(runID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run_id format"})
			return
		}

		// ── Query 1: run time bounds ──────────────────────────────────────────
		boundsSQL := fmt.Sprintf(`
SELECT
    min(event_time) AS start_time,
    max(event_time) AS end_time
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'
  AND run_id        = '%s'`,
			deploymentID, agentID, sessionID, runID)

		boundsRows, err := timeplus.Query(&cfg.Timeplus, boundsSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		if len(boundsRows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}

		startTime := parseTime(boundsRows[0]["start_time"])
		endTime := parseTime(boundsRows[0]["end_time"])
		if startTime.IsZero() {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}

		startMs := startTime.UnixMilli()
		endMs := endTime.UnixMilli()
		durationMs := endMs - startMs
		if durationMs <= 0 {
			durationMs = 1
		}

		spans := []models.TraceSpan{
			{
				SpanID:     runID,
				ParentID:   "",
				Kind:       "run",
				Name:       "run",
				StartMs:    startMs,
				DurationMs: durationMs,
				Status:     "ok",
				Attributes: map[string]string{},
			},
		}

		// ── Query 2: OTel LLM spans (non-fatal) ──────────────────────────────
		startNs := startTime.UnixNano()
		endNs := endTime.UnixNano()

		otelSQL := fmt.Sprintf(`
SELECT
    span_id,
    start_time_ns,
    duration,
    span_attributes
FROM table(otel_traces)
WHERE span_attributes['openclaw.sessionId'] = '%s'
  AND span_name = 'openclaw.model.usage'
  AND start_time_ns >= %d
  AND start_time_ns <= %d
ORDER BY start_time_ns ASC`,
			sessionID, startNs, endNs)

		otelRows, otelErr := timeplus.Query(&cfg.Timeplus, otelSQL)
		if otelErr != nil {
			log.Printf("GetRunTrace: otel_traces query failed (non-fatal): %v", otelErr)
		} else {
			for i, row := range otelRows {
				spanID := parseStr(row["span_id"])
				if spanID == "" {
					spanID = fmt.Sprintf("llm-%d", i)
				}
				spanStartMs := parseInt64(row["start_time_ns"]) / 1_000_000
				spanDurationMs := parseInt64(row["duration"]) / 1_000_000

				attrs := map[string]string{}
				// span_attributes is a map column; Proton returns it as map[string]interface{}
				if rawMap, ok := row["span_attributes"].(map[string]interface{}); ok {
					for k, v := range rawMap {
						if s, ok2 := v.(string); ok2 {
							attrs[k] = s
						}
					}
				}

				spans = append(spans, models.TraceSpan{
					SpanID:     spanID,
					ParentID:   runID,
					Kind:       "llm",
					Name:       "LLM call",
					StartMs:    spanStartMs,
					DurationMs: spanDurationMs,
					Status:     "ok",
					Attributes: attrs,
				})
			}
		}

		// ── Query 3: tool call spans ──────────────────────────────────────────
		toolSQL := fmt.Sprintf(`
SELECT hook_name, event_time, tool_name, hook_decision, block_reason, event_data
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'
  AND run_id        = '%s'
  AND hook_name     IN ('before_tool_call', 'after_tool_call')
ORDER BY event_time ASC`,
			deploymentID, agentID, sessionID, runID)

		toolRows, err := timeplus.Query(&cfg.Timeplus, toolSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Collect before/after events; key by toolCallId when present.
		type toolEvt struct {
			hookName     string
			eventTime    time.Time
			toolName     string
			hookDecision string
			blockReason  string
			eventData    string
			toolCallID   string
		}

		var befores []toolEvt
		afterByID := map[string]toolEvt{}
		var aftersNoID []toolEvt

		for _, row := range toolRows {
			evt := toolEvt{
				hookName:     parseStr(row["hook_name"]),
				eventTime:    parseTime(row["event_time"]),
				toolName:     parseStr(row["tool_name"]),
				hookDecision: parseStr(row["hook_decision"]),
				blockReason:  parseStr(row["block_reason"]),
				eventData:    parseStr(row["event_data"]),
			}
			var data map[string]interface{}
			if json.Unmarshal([]byte(evt.eventData), &data) == nil {
				if tcid, ok := data["toolCallId"].(string); ok {
					evt.toolCallID = tcid
				}
			}
			if evt.hookName == "before_tool_call" {
				befores = append(befores, evt)
			} else {
				if evt.toolCallID != "" {
					afterByID[evt.toolCallID] = evt
				} else {
					aftersNoID = append(aftersNoID, evt)
				}
			}
		}

		noIDIdx := 0
		for i, before := range befores {
			var after *toolEvt
			if before.toolCallID != "" {
				if a, ok := afterByID[before.toolCallID]; ok {
					after = &a
				}
			} else if noIDIdx < len(aftersNoID) {
				a := aftersNoID[noIDIdx]
				after = &a
				noIDIdx++
			}

			spanStartMs := before.eventTime.UnixMilli()
			var spanDurationMs int64
			status := "ok"
			attrs := map[string]string{"tool_name": before.toolName}

			if before.hookDecision == "block" {
				status = "blocked"
				if before.blockReason != "" {
					attrs["block_reason"] = before.blockReason
				}
			} else if after != nil {
				spanDurationMs = after.eventTime.UnixMilli() - spanStartMs
				var data map[string]interface{}
				if json.Unmarshal([]byte(after.eventData), &data) == nil {
					if d, ok := data["durationMs"].(float64); ok {
						spanDurationMs = int64(d)
					}
					if errMsg, ok := data["error"].(string); ok && errMsg != "" {
						status = "error"
						attrs["error"] = errMsg
					}
				}
			}

			spans = append(spans, models.TraceSpan{
				SpanID:     fmt.Sprintf("tool-%d", i),
				ParentID:   runID,
				Kind:       "tool",
				Name:       before.toolName,
				StartMs:    spanStartMs,
				DurationMs: spanDurationMs,
				Status:     status,
				Attributes: attrs,
			})
		}

		// Sort all spans by StartMs ascending (root first, then children).
		sort.Slice(spans, func(i, j int) bool {
			if spans[i].ParentID == "" {
				return true
			}
			if spans[j].ParentID == "" {
				return false
			}
			return spans[i].StartMs < spans[j].StartMs
		})

		c.JSON(http.StatusOK, models.TraceResponse{Spans: spans})
	}
}
```

- [ ] **Step 3: Verify compilation**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output (clean compile).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/handlers/agent_sessions.go
git commit -m "feat(trace): add GetRunTrace handler with hook + OTel span synthesis"
```

---

### Task 3: Route Registration

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Register the new route**

In `backend/cmd/server/main.go`, find the existing sessions route (around line 95):

```go
api.GET("/agents/:id/sessions/:session_id", handlers.GetSession(cfg))
```

Add the trace route immediately after it:

```go
api.GET("/agents/:id/sessions/:session_id", handlers.GetSession(cfg))
api.GET("/agents/:id/sessions/:session_id/runs/:run_id/trace", handlers.GetRunTrace(cfg))
```

- [ ] **Step 2: Verify compilation and start server**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/backend
CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server
```

Expected output:
```
AgentShield starting on :8080
Proton endpoint: localhost:3218 (native: 8463)
```

In a second terminal, curl a test (replace IDs with any valid values — a 404 is acceptable, a 500 or connection error is not):

```bash
curl -s http://localhost:8080/api/agents/test__test/sessions/test-session/runs/test-run/trace
```

Expected: `{"error":"run not found"}` with HTTP 404 (not a 500 or 404 from the SPA fallback).

- [ ] **Step 3: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(trace): register GET /agents/:id/sessions/:sid/runs/:rid/trace"
```

---

### Task 4: API Client

**Files:**
- Modify: `frontend/src/api/client.js`

- [ ] **Step 1: Add getRunTrace to the api object**

In `frontend/src/api/client.js`, find the `getSession` line:

```js
getSession: (agentId, sessionId) => get(`/agents/${agentId}/sessions/${sessionId}`),
```

Add `getRunTrace` immediately after it:

```js
getSession: (agentId, sessionId) => get(`/agents/${agentId}/sessions/${sessionId}`),
getRunTrace: (agentId, sessionId, runId) =>
  get(`/agents/${agentId}/sessions/${sessionId}/runs/${encodeURIComponent(runId)}/trace`),
```

Note: `encodeURIComponent` is needed because `run_id` values can contain colons (e.g. `announce:v1:agent:...:run:<uuid>`).

- [ ] **Step 2: Verify the frontend builds**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/frontend
npm run build
```

Expected: build succeeds with no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.js
git commit -m "feat(trace): add getRunTrace API client function"
```

---

### Task 5: Frontend — RunWaterfall Component and Trace Tab

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx`

**Context:** The existing tab bar is at the bottom of the `AgentDetail` component JSX, rendered when `selectedRun` is set. It currently has two tabs: `timeline` and `raw`. State variables include `activeTab` (default `'timeline'`), `events`, `loadingEvents`. The `KVRow` component already exists in this file and can be reused in the attribute panel.

- [ ] **Step 1: Add RunWaterfall component**

Find the `// ── RunTimeline ───────────────────────────────────────────────────────────────` comment (around line 400). Add the `RunWaterfall` component immediately before it:

```jsx
// ── RunWaterfall ──────────────────────────────────────────────────────────────

function RunWaterfall({ spans }) {
  const [expandedSpan, setExpandedSpan] = useState(null)

  if (!spans || spans.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
        <span className="material-symbols-outlined text-4xl">account_tree</span>
        <p className="text-sm">No trace data available for this run</p>
      </div>
    )
  }

  const root = spans.find(s => s.parent_id === '')
  if (!root) return null

  const rootStart = root.start_ms
  const totalMs = root.duration_ms || 1

  function barColor(span) {
    if (span.kind === 'run') return '#ffb0cf'
    if (span.kind === 'llm') return '#93c5fd'
    if (span.status === 'ok') return '#6ee7b7'
    return '#ffb4ab' // blocked or error
  }

  function fmtDuration(ms) {
    if (ms == null || ms === 0) return '—'
    if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`
    return `${ms}ms`
  }

  return (
    <div className="p-3 space-y-0.5">
      {spans.map(span => {
        const depth = span.parent_id === '' ? 0 : 1
        const leftPct = Math.max(0, ((span.start_ms - rootStart) / totalMs) * 100)
        const widthPct = Math.max(0.5, (span.duration_ms / totalMs) * 100)
        const color = barColor(span)
        const isExpanded = expandedSpan === span.span_id
        const hasAttrs = span.attributes && Object.keys(span.attributes).length > 0

        return (
          <div key={span.span_id}>
            <button
              onClick={() => hasAttrs && setExpandedSpan(isExpanded ? null : span.span_id)}
              className={`w-full text-left flex items-center gap-3 px-2 py-1.5 rounded-sm transition-colors ${hasAttrs ? 'hover:bg-[#2c2834]/50 cursor-pointer' : 'cursor-default'}`}
            >
              {/* Name column */}
              <div
                className="w-36 shrink-0 flex items-center gap-1"
                style={{ paddingLeft: depth * 16 }}
              >
                {span.kind === 'tool' && span.status === 'blocked' && (
                  <span className="material-symbols-outlined text-[12px] text-[#ffb4ab]">block</span>
                )}
                {span.kind === 'tool' && span.status === 'error' && (
                  <span className="material-symbols-outlined text-[12px] text-[#ffb4ab]">error</span>
                )}
                <span className="text-[11px] text-[#e7dff0] truncate">{span.name}</span>
              </div>

              {/* Bar column */}
              <div className="flex-1 relative h-4 bg-[#0f0c17] rounded-sm overflow-hidden">
                <div
                  className="absolute top-0 h-full rounded-sm opacity-80"
                  style={{
                    left: `${leftPct}%`,
                    width: `${widthPct}%`,
                    backgroundColor: color,
                    minWidth: 2,
                  }}
                />
              </div>

              {/* Duration column */}
              <span className="text-[10px] text-zinc-500 w-12 text-right shrink-0 tabular-nums">
                {fmtDuration(span.duration_ms)}
              </span>

              {/* Expand indicator */}
              {hasAttrs && (
                <span className={`material-symbols-outlined text-[12px] text-zinc-600 transition-transform ${isExpanded ? 'rotate-180' : ''}`}>
                  expand_more
                </span>
              )}
            </button>

            {/* Attribute panel */}
            {isExpanded && hasAttrs && (
              <div
                className="mx-2 mb-1 bg-[#0f0c17] rounded-sm px-3 py-2"
                style={{ marginLeft: depth * 16 + 8 }}
              >
                {Object.entries(span.attributes).map(([k, v]) => (
                  <KVRow key={k} label={k} value={v} />
                ))}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}
```

- [ ] **Step 2: Add traceData state and fetch effect**

In the `AgentDetail` component body, find the existing state declarations (around line 504–516). Add two new state variables after `eventsTruncated`:

```js
const [eventsTruncated, setEventsTruncated] = useState(false)
const [traceData, setTraceData] = useState(null)
const [loadingTrace, setLoadingTrace] = useState(false)
```

After the existing events `useEffect` (the one that depends on `[id, selectedSession, selectedRun, timePreset, refreshTick]`), add the trace fetch effect:

```js
useEffect(() => {
  setTraceData(null)
  if (!selectedRun || !selectedSession || activeTab !== 'trace') return
  setLoadingTrace(true)
  api.getRunTrace(id, selectedSession, selectedRun)
    .then(setTraceData)
    .catch(() => setTraceData(null))
    .finally(() => setLoadingTrace(false))
}, [id, selectedSession, selectedRun, activeTab])
```

- [ ] **Step 3: Add Trace tab to the tab bar**

Find the tab bar array in the JSX (around line 879):

```jsx
{[
  { id: 'timeline', label: 'Timeline' },
  { id: 'raw', label: 'Raw Events' },
].map(tab => (
```

Replace it with:

```jsx
{[
  { id: 'timeline', label: 'Timeline' },
  { id: 'trace',    label: 'Trace' },
  { id: 'raw',      label: 'Raw Events' },
].map(tab => (
```

- [ ] **Step 4: Add Trace tab content to the event list**

Find this block in the event list area (around line 915):

```jsx
) : activeTab === 'raw' ? (
```

Add the trace branch between the loading check and the raw branch:

```jsx
) : activeTab === 'trace' ? (
  loadingTrace ? (
    <div className="flex items-center justify-center h-full">
      <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
    </div>
  ) : (
    <RunWaterfall spans={traceData?.spans ?? []} />
  )
) : activeTab === 'raw' ? (
```

- [ ] **Step 5: Verify in browser**

Start both servers:
```bash
# Terminal 1
cd /Users/gangtao/Code/timeplus/agentshield/backend
CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server

# Terminal 2
cd /Users/gangtao/Code/timeplus/agentshield/frontend
npm run dev
```

Open `http://localhost:5173`, navigate to an agent, select a session, expand a run, select the run. Verify:
1. The tab bar shows three tabs: Timeline · Trace · Raw Events
2. Clicking "Trace" triggers a fetch (check Network tab in DevTools for the `/trace` request)
3. If Proton has data: run bar appears in pink, LLM bars in blue, tool bars in green/red
4. If no OTel data: only the run bar and tool bars appear (no error)
5. Clicking a span with attributes expands the attribute panel inline
6. Switching back to Timeline does not re-fetch trace data

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/AgentDetail.jsx
git commit -m "feat(trace): add RunWaterfall component and Trace tab to AgentDetail"
```

---

## Self-Review

**Spec coverage:**
- ✅ New endpoint `GET /api/agents/:id/sessions/:sid/runs/:rid/trace` — Task 2 + Task 3
- ✅ `TraceSpan` / `TraceResponse` models — Task 1
- ✅ Run root span from hook event time bounds — Task 2 Query 1
- ✅ LLM spans from `otel_traces` time-matched to run — Task 2 Query 2
- ✅ Tool spans from paired before/after hook events — Task 2 Query 3
- ✅ OTel failure is non-fatal — Task 2 (`log.Printf` + continue)
- ✅ Tool pairing by `toolCallId` with positional fallback — Task 2
- ✅ `getRunTrace` API client function — Task 4
- ✅ Trace tab in tab bar — Task 5 Step 3
- ✅ `RunWaterfall` waterfall renderer — Task 5 Step 1
- ✅ Lazy fetch (only when tab is active) — Task 5 Step 2
- ✅ Click to expand attributes — Task 5 Step 1
- ✅ Bar colors: run=pink, llm=blue, tool ok=green, tool blocked/error=red — Task 5 Step 1
- ✅ Minimum bar width 2px — Task 5 Step 1 (`minWidth: 2`)
- ✅ Duration label right-aligned — Task 5 Step 1

**Type consistency:** `TraceSpan.SpanID`/`ParentID`/`Kind`/`Name`/`StartMs`/`DurationMs`/`Status`/`Attributes` defined in Task 1 and used consistently in Task 2 handler and Task 5 frontend (`span.span_id`, `span.parent_id`, `span.kind`, `span.start_ms`, `span.duration_ms`, `span.status`, `span.attributes` — matching Go JSON snake_case output).
