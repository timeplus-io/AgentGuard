# Trace Visualization Design

## Goal

Add a **Trace** tab to the AgentDetail run view that renders a Jaeger-style waterfall diagram synthesized from both `agentshield_hook_events` and `otel_traces`. Users can see exact wall-clock timing for LLM calls (from OTel) alongside tool executions and run boundaries (from hook events), in a single coherent view.

## Scope

- **New backend endpoint:** `GET /api/agents/:id/sessions/:session_id/runs/:run_id/trace`
- **New models:** `TraceSpan`, `TraceResponse` in `models.go`
- **New frontend tab:** "Trace" tab in `AgentDetail.jsx` with `RunWaterfall` component
- **No new Proton streams or materialized views**
- **No changes to existing tabs** (Timeline, Raw Events)

---

## Span Model

The backend returns a flat list of spans with parent references (OTel-style). The frontend builds the tree from `parent_id` links.

Three span kinds:

### Run root span
Derived from `agentshield_hook_events` for the selected run:
- `start_ms` = Unix ms of earliest `event_time` in the run
- `duration_ms` = latest `event_time` − earliest `event_time`
- `span_id` = the run's `run_id`
- `parent_id` = `""` (root)
- `kind` = `"run"`

### LLM call spans
From `otel_traces` where `span_attributes['openclaw.sessionId'] = session_id` and `span_name = 'openclaw.model.usage'`, time-window matched to the run (`start_time_ns` within `[run_start_ns, run_end_ns]`):
- `start_ms` and `duration_ms` from OTel nanosecond fields (÷ 1,000,000)
- `parent_id` = `run_id`
- `kind` = `"llm"`
- `attributes`: `model`, `provider`, `tokens.input`, `tokens.output`, `tokens.total` extracted from `span_attributes`

If `otel_traces` is unavailable or returns no rows, this step is skipped silently. The trace renders with run + tools only.

### Tool call spans
From paired `before_tool_call` / `after_tool_call` events in `agentshield_hook_events`:
- `start_ms` = `before_tool_call.event_time`
- `duration_ms` = `event_data.durationMs` from `after_tool_call` if present; else `after_tool_call.event_time − before_tool_call.event_time`
- Pairing: by `toolCallId` from `event_data`; fall back to positional order
- `parent_id` = `run_id` (direct children of run root — tools execute after the LLM responds, not inside the LLM span)
- `kind` = `"tool"`
- `status` = `"blocked"` if `hook_decision = 'block'`; `"error"` if `event_data.error` is set; else `"ok"`
- `attributes`: `tool_name`, `error` (if any), `block_reason` (if blocked)

### Go types

```go
// models.go

type TraceSpan struct {
    SpanID     string            `json:"span_id"`
    ParentID   string            `json:"parent_id"`
    Kind       string            `json:"kind"`        // "run" | "llm" | "tool"
    Name       string            `json:"name"`
    StartMs    int64             `json:"start_ms"`
    DurationMs int64             `json:"duration_ms"`
    Status     string            `json:"status"`      // "ok" | "error" | "blocked"
    Attributes map[string]string `json:"attributes"`
}

type TraceResponse struct {
    Spans []TraceSpan `json:"spans"`
}
```

Spans in the response are sorted by `start_ms` ascending.

---

## Backend

### Route

```
GET /api/agents/:id/sessions/:session_id/runs/:run_id/trace
```

Registered in `cmd/server/main.go` alongside the existing session routes. Handler: `GetRunTrace` in `backend/internal/handlers/agent_sessions.go`.

### Handler Logic

Three sequential SQL queries against Proton.

**Query 1 — run time bounds:**
```sql
SELECT
    min(event_time) AS start_time,
    max(event_time) AS end_time
FROM table(agentshield_hook_events)
WHERE deployment_id = '<id>'
  AND session_id    = '<session_id>'
  AND run_id        = '<run_id>'
```
Produces the run root span. Returns 404 if no rows found.

**Query 2 — OTel LLM spans:**
```sql
SELECT
    span_id,
    start_time_ns,
    duration,
    span_attributes
FROM table(otel_traces)
WHERE span_attributes['openclaw.sessionId'] = '<session_id>'
  AND span_name = 'openclaw.model.usage'
  AND start_time_ns >= <run_start_ns>
  AND start_time_ns <= <run_end_ns>
ORDER BY start_time_ns ASC
```
`run_start_ns` and `run_end_ns` are derived from Query 1 result (Unix ms × 1,000,000). Failure is non-fatal: log the error and continue with an empty LLM span list.

**Query 3 — tool call spans:**
```sql
SELECT hook_name, event_time, tool_name, hook_decision, block_reason, event_data
FROM table(agentshield_hook_events)
WHERE deployment_id = '<id>'
  AND session_id    = '<session_id>'
  AND run_id        = '<run_id>'
  AND hook_name     IN ('before_tool_call', 'after_tool_call')
ORDER BY event_time ASC
```
Pairs `before`/`after` by `event_data.toolCallId`; falls back to positional pairing if `toolCallId` is absent. Failure returns 500.

### Error Handling

| Condition | Behaviour |
|-----------|-----------|
| No hook events for run | 404 |
| `otel_traces` query fails | Log + continue; no LLM spans in response |
| `agentshield_hook_events` tool query fails | 500 |

---

## Frontend

### New Tab

"Trace" added to the tab bar in `AgentDetail.jsx`, positioned between Timeline and Raw Events:

```
[ Timeline ]  [ Trace ]  [ Raw Events ]
```

Visible only when `selectedRun` is set (same condition as existing tabs). `activeTab` resets to `'timeline'` when a new run is selected.

### API Client

New function in `frontend/src/api/client.js`:

```js
getRunTrace: (agentId, sessionId, runId) =>
  fetch(`/api/agents/${agentId}/sessions/${sessionId}/runs/${runId}/trace`)
    .then(r => r.json())
```

### State

Two new state variables in `AgentDetail`:

```js
const [traceData, setTraceData] = useState(null)   // TraceResponse | null
const [loadingTrace, setLoadingTrace] = useState(false)
```

Fetch triggered when `activeTab === 'trace'` and `selectedRun` changes (lazy — only fetches when the tab is first opened for a run). Clears on run change.

### `RunWaterfall` Component

New component co-located in `AgentDetail.jsx`. Props: `spans: TraceSpan[]`.

**Waterfall layout:**

The run root span defines the time axis (0–100%). For each span:
- `left` = `(span.start_ms − root.start_ms) / root.duration_ms × 100%`
- `width` = `span.duration_ms / root.duration_ms × 100%`
- Minimum rendered width: 2px (so zero-duration spans remain visible)

**Per-row layout:**
```
[indent]  [name + attributes summary]  [████████░░░░]  [duration]
```
- Root spans: no indent
- Children: `padding-left: 16px`
- Duration label: to the right of the bar; `< 1000ms` → `Xms`; `≥ 1000ms` → `X.Xs`

**Bar colors:**

| Kind | Status | Color |
|------|--------|-------|
| `run` | any | `#ffb0cf` |
| `llm` | any | `#93c5fd` |
| `tool` | `ok` | `#6ee7b7` |
| `tool` | `blocked` | `#ffb4ab` |
| `tool` | `error` | `#ffb4ab` |

**Click to expand:** clicking a span row toggles an inline attributes panel below it showing `span.attributes` as a key-value list (same `KVRow` component used in existing event detail panels).

**Empty / degraded states:**
- OTel spans absent, tool spans present → renders normally; LLM rows simply absent
- No spans at all → centred message: "No trace data available for this run"
- `loadingTrace` → spinner

---

## Data Flow

```
selectRun(rid)
  → activeTab switches to 'trace'
  → GET /api/agents/:id/sessions/:sid/runs/:rid/trace
      → Query 1: hook events → run root span (start/end time)
      → Query 2: otel_traces → LLM spans (time-matched to run)
      → Query 3: hook events → tool spans (paired before/after)
      → return []TraceSpan sorted by start_ms
  → RunWaterfall renders waterfall from flat span list
```

---

## What Is Not Changing

- `agentshield_hook_events` schema — untouched
- `otel_traces` schema — untouched
- Existing Timeline and Raw Events tabs — untouched
- `ListEvents`, `GetSession`, `ListSessions` handlers — untouched
- No new Proton streams or materialized views

---

## Success Criteria

1. Selecting a run and opening the Trace tab shows a waterfall with at least the run root bar.
2. LLM call bars appear with correct relative timing when OTel data is present.
3. Tool call bars appear with correct duration and color (green/red).
4. Clicking any span row expands its attributes inline.
5. When OTel is unavailable, the tab renders without error (run + tools only).
6. Switching to Trace tab does not re-fetch if data is already loaded for the current run.
