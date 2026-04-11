---
title: OpenClaw Timeline Analysis Design
date: 2026-04-11
status: approved
---

# OpenClaw Timeline Analysis Design

## Goal

Replace the flat gap-fill trace endpoint with a backend-analyzed, agent-type-routed timeline that pairs OpenClaw pre/post hook events into true hierarchical spans with real timing data — producing a Run → LLM Call → Tool Call tree visible in the existing RunWaterfall component.

## Background

The current `GET /api/agents/:id/sessions/:session_id/runs/:run_id/trace` endpoint produces a flat list of spans by gap-filling consecutive events. This loses real execution timing (actual durationMs fields are ignored), collapses tool calls into generic "tool call" blobs, and cannot express hierarchy. OpenClaw's hook system emits rich timing and pairing data that we should use.

---

## Section 1: Stream Schema + Plugin Change

### `agentshield_hook_events` schema addition

Add `agent_type low_cardinality(string)` as a new column in the CREATE STREAM DDL. This field identifies which agent produced the event and is used by the backend to route to the correct timeline analyzer.

- Default value: `''` (empty string — tolerated by the fallback analyzer)
- No migration needed: deploying on a clean environment

Updated CREATE STREAM (relevant new column only):

```sql
CREATE STREAM IF NOT EXISTS agentshield_hook_events (
  -- existing columns --
  agent_type low_cardinality(string),
  -- existing columns --
)
```

Full stream definition remains otherwise unchanged.

### OpenClaw plugin change

In `agents/openclaw/agentshield-plugin/src/index.ts` (the hook event publisher), add `agent_type: "openclaw"` to every emitted `HookEvent` payload before it is sent to the Timeplus ingest API.

The value `"openclaw"` is a stable identifier for this agent type. Future agents (Claude Code, Cursor, etc.) will use their own identifiers.

---

## Section 2: Backend Timeline Handler

### File structure

Three new files, one modified:

| File | Action | Responsibility |
|------|--------|---------------|
| `backend/internal/handlers/timeline.go` | **Replace** `GetRunTrace` | Route by `agent_type`; fetch raw events; dispatch to analyzer |
| `backend/internal/handlers/timeline_openclaw.go` | **Create** | OpenClaw 6-step hierarchical algorithm |
| `backend/internal/handlers/timeline_fallback.go` | **Create** | Existing flat gap-fill logic (extracted verbatim) |

The existing `handlers/agents.go` or wherever `GetRunTrace` currently lives loses that handler; the route registration in `main.go` is updated to point to the new `GetRunTrace` in `timeline.go`.

### Router file: `timeline.go`

```go
// GetRunTrace routes to the correct analyzer based on agent_type in the run's events.
func GetRunTrace(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        agentID    := c.Param("id")
        sessionID  := c.Param("session_id")
        runID      := c.Param("run_id")

        events, err := fetchHookEvents(cfg, agentID, sessionID, runID)
        if err != nil { /* 500 */ return }

        agentType := detectAgentType(events)   // reads agent_type field from first non-empty event

        var spans []models.TraceSpan
        switch agentType {
        case "openclaw":
            spans = analyzeOpenClaw(events)
        default:
            spans = analyzeFallback(events)
        }

        c.JSON(200, gin.H{"spans": spans})
    }
}
```

`fetchHookEvents` runs a Proton SELECT on `agentshield_hook_events` filtered by agentID + sessionID + runID, ordered by `_tp_time ASC`.

`detectAgentType` returns the `agent_type` value from the first event that has a non-empty value, or `""` to trigger the fallback.

### OpenClaw analyzer: `timeline_openclaw.go`

Six-step algorithm that produces a `[]models.TraceSpan` slice:

**Step 1 — Run root span**

Scan all events. The root span is:
- `kind: "run"`, `name: "Agent Run"`
- `start_time`: timestamp of the first event
- `end_time`:
  - If an `agent_end` event exists and has `durationMs > 0`: `start_time + durationMs`
  - Otherwise: timestamp of the last event
- `parent_id: ""`

**Step 2 — LLM Call spans**

Find all `llm_input` events. Each `llm_input` defines one LLM Call span:
- `kind: "llm"`, `name: "LLM Call"`
- `start_time`: `llm_input.timestamp`
- `end_time`:
  - Find the matching `llm_output` event with the same `runId` that occurs after this `llm_input`
  - If found: `llm_output.timestamp`
  - If not found (stream cut): `llm_input.timestamp` (zero-duration placeholder)
- `parent_id`: run root span ID

**Step 3 — Tool Call pair spans**

Group events by `toolCallId` (from `event_data`). For each group that contains a `before_tool_call`:
- `kind: "tool"`, `name`: use `tool_name` field if present, else `"Tool Call"`
- `start_time`: `before_tool_call.timestamp`
- `end_time`:
  - If matching `after_tool_call` exists and has `durationMs > 0`: `before_tool_call.timestamp + durationMs`
  - If matching `after_tool_call` exists without `durationMs`: `after_tool_call.timestamp`
  - If no `after_tool_call` (blocked call): gap-fill from next event's timestamp (see Step 4)
- `parent_id`: the LLM Call span that contains this tool call's time window; if none, use run root span ID

**Step 4 — Blocked tool calls (unmatched `before_tool_call`)**

For any `before_tool_call` with no matching `after_tool_call`:
- `kind: "tool"`, `name: tool_name + " (blocked)"`
- `start_time`: event timestamp
- `end_time`: timestamp of the next event in the sorted list; if last event, use start_time (zero-duration)
- `parent_id`: enclosing LLM Call span or run root

**Step 5 — Tool result persist spans**

For any `tool_result_persist` event:
- `kind: "tool"`, `name: "Persist Tool Result"`
- `start_time`: event timestamp
- `end_time`: gap-fill from next event's timestamp
- `parent_id`: the tool call span whose time window contains this event; otherwise the enclosing LLM Call

**Step 6 — Remaining lifecycle events**

All events not consumed in Steps 1-5 (e.g. `agent_start`, `subagent_start`, `subagent_end`, `session_start`, orphaned lifecycle hooks):
- `kind: "lifecycle"` (or `"subagent"` for subagent_start/end)
- `name`: human-readable label (see Name Map below)
- `start_time`: event timestamp
- `end_time`: gap-fill from next event's timestamp; if last, use start_time
- `parent_id`: run root span ID

### Human-readable name map

| hook_name | Span name |
|-----------|-----------|
| `agent_start` | `"Agent Start"` |
| `agent_end` | `"Agent End"` |
| `llm_input` | `"LLM Call"` |
| `llm_output` | `"LLM Response"` |
| `before_tool_call` | tool_name or `"Tool Call"` |
| `after_tool_call` | tool_name or `"Tool Call"` |
| `tool_result_persist` | `"Persist Tool Result"` |
| `subagent_start` | `"Subagent Start"` |
| `subagent_end` | `"Subagent End"` |
| `session_start` | `"Session Start"` |
| `session_end` | `"Session End"` |
| _(unknown)_ | hook_name verbatim |

### Fallback analyzer: `timeline_fallback.go`

Extract the current flat gap-fill logic from wherever it lives today into this file, unchanged. It handles any `agent_type` that is not `"openclaw"`. This ensures no regression for existing data or future unknown agent types.

---

## Section 3: Data Model + Frontend

### `models.TraceSpan` — unchanged

The existing struct is sufficient:

```go
type TraceSpan struct {
    ID        string    `json:"id"`
    ParentID  string    `json:"parent_id"`
    Name      string    `json:"name"`
    Kind      string    `json:"kind"`
    StartTime time.Time `json:"start_time"`
    EndTime   time.Time `json:"end_time"`
    Tags      map[string]string `json:"tags,omitempty"`
}
```

No model changes needed.

### New `kind` value: `"subagent"`

The `RunWaterfall` component already renders spans by `kind`. Adding `"subagent"` only requires a color entry in the frontend color map. No structural changes needed.

Current kind → color map (extend with):

```js
subagent: "bg-violet-500/20 border-violet-500/40 text-violet-300"
```

### Frontend RunWaterfall — no structural changes

The component already:
- Accepts `spans` as a flat array
- Reconstructs tree from `parent_id`
- Renders indented bars with relative widths

No changes to the component's props, data shape, or rendering logic are needed. Only the color map entry for `"subagent"` is added if it doesn't exist.

---

## API Contract

The existing endpoint is replaced in-place:

```
GET /api/agents/:id/sessions/:session_id/runs/:run_id/trace
```

**Response (unchanged shape):**

```json
{
  "spans": [
    {
      "id": "run-abc123",
      "parent_id": "",
      "name": "Agent Run",
      "kind": "run",
      "start_time": "2026-04-11T10:00:00Z",
      "end_time": "2026-04-11T10:00:45Z",
      "tags": {}
    },
    {
      "id": "llm-001",
      "parent_id": "run-abc123",
      "name": "LLM Call",
      "kind": "llm",
      "start_time": "2026-04-11T10:00:02Z",
      "end_time": "2026-04-11T10:00:08Z"
    },
    {
      "id": "tool-001",
      "parent_id": "llm-001",
      "name": "bash",
      "kind": "tool",
      "start_time": "2026-04-11T10:00:09Z",
      "end_time": "2026-04-11T10:00:12Z"
    }
  ]
}
```

---

## Testing Approach

- Unit test for `detectAgentType`: empty events → `""`, openclaw event → `"openclaw"`
- Unit test for `analyzeOpenClaw`: table-driven tests covering:
  - Complete run with paired tool calls
  - Blocked tool call (no `after_tool_call`)
  - LLM call with no matching `llm_output`
  - Subagent events producing `"subagent"` kind spans
  - Empty event list → single zero-duration run span
- Integration test: round-trip through `GetRunTrace` with mock Proton response returning `agent_type: "openclaw"` events
- Regression test: `agent_type: ""` events route to fallback and produce same output as before

---

## Deployment Notes

- Clean deployment assumed: no migration of `agentshield_hook_events`; the stream will be dropped and recreated with the new `agent_type` column
- The plugin change and backend change should be deployed together; old plugin data (without `agent_type`) will route to fallback and continue to work
- No environment variable or config changes needed
