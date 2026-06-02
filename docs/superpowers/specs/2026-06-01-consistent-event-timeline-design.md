# Consistent Event Timeline — Design

**Issue:** [#51](https://github.com/timeplus-io/AgentGuard/issues/51) · **Date:** 2026-06-01

## Goal

Replace the AgentGuard **Timeline tab** with a single, agent-agnostic view that shows
the same ordered sequence of event kinds for every agent type (Claude Code, OpenClaw,
Hermes, and any future agent):

1. **User input** — the user message submitted to the agent
2. **LLM response** — the assistant's reply
3. **Tool call request** — a tool invocation initiated by the model
4. **Tool call result** — the tool's output / completion

The view is driven by the normalized `agentguard_cim_event` stream instead of raw,
agent-specific `agentguard_hook_events`. This removes the per-agent hook interpretation
that is currently duplicated across the backend timeline analyzers and the frontend.

## Scope

**In scope**

- A new CIM-backed backend endpoint that returns a normalized, ordered timeline for a session.
- A rewrite of the **Timeline tab** in `AgentDetail.jsx` to consume it.

**Explicitly out of scope (untouched)**

- The **Trace tab** waterfall, `GetRunTrace`, the `TraceSpan` model, and the three
  analyzers (`timeline_openclaw.go`, `timeline_hermes.go`, `timeline_fallback.go`).
  These keep powering the waterfall as-is.
- The `ListEvents` endpoint (`agent_events.go`). It remains for other consumers; the
  Timeline tab simply stops using it.
- No CIM schema / MV / DDL changes. `tool_call_id` is parsed in Go from `raw_event_data`
  (Approach A).
- No metrics enrichment. The timeline uses `cim_event` fields only — no tokens/latency
  from `agentguard_cim_metrics`.

## Background — why this change

`agentguard_cim_event` already collapses every agent's hook vocabulary into canonical
`event_type` values (see `docs/cim.md` §2.2). The four required kinds map 1:1:

| Timeline kind | CIM `event_type` | Source fields |
|---|---|---|
| User input | `user_input` | `user_message` |
| LLM response | `llm_response` | `assistant_message`, `model`, `provider` |
| Tool call request | `tool_invoke` | `tool_name`, `tool_input`, `hook_decision`, `block_reason` |
| Tool call result | `tool_complete` | `tool_result`, `tool_success` |

Today the Timeline tab ignores CIM: `ListEvents` returns raw hook rows and
`AgentDetail.jsx` (~L190–461) re-implements agent-specific normalization in JavaScript,
branching on `user_prompt_submit` / `llm_output` / `pre_llm_call` / `pre_tool_call` / etc.
This is a fourth copy of logic that CIM was built to centralize. Building the Timeline tab
on `cim_event` makes it agent-agnostic and deletes that branching.

## Architecture

### 1. Backend endpoint

`GET /api/agents/:id/sessions/:session_id/timeline`

Query params:
- `run_id` (optional) — narrow to a single run/turn, matching the existing run-selection sidebar.

Single query against the normalized stream:

```sql
SELECT event_time, event_type, run_id, tool_name, tool_input, tool_result,
       tool_success, user_message, assistant_message, model, provider,
       hook_decision, block_reason, raw_event_data
FROM table(agentguard_cim_event)
WHERE deployment_id = ? AND agent_id = ? AND session_id = ?
  AND event_type IN ('user_input','llm_response','tool_invoke','tool_complete','session_start','session_end')
ORDER BY event_time ASC
```

When `run_id` is supplied, add `AND run_id = ?`.

Notes:
- No `agent_type` switch and no hook names anywhere in this path — the only agent-specific
  concession is parsing `tool_call_id` (see §3), and that is key-name tolerant.
- Uses the native binary protocol (`timeplus.QueryNative`), consistent with `ListEvents` and
  `fetchHookEvents`, because `user_message` / `assistant_message` / `tool_*` carry arbitrary
  content that can break the HTTP+JSON serializer.
- `id` is split into `deployment_id` + `agent_id` via the existing `splitAgentID`; `session_id`
  and `run_id` are validated with the existing `idPartRe` / `runIDRe` regexes.

### 2. Response shape — flat list + grouping metadata

```go
// TimelineEvent is one normalized item in the session timeline.
// Kind is "user_input" | "llm_response" | "tool_request" | "tool_result"
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

// TimelineResponse is returned by
// GET /api/agents/:id/sessions/:session_id/timeline.
// Events are sorted by EventTime ascending.
type TimelineResponse struct {
    Events []TimelineEvent `json:"events"`
}
```

A flat, time-ordered list (not a tree) is the most direct match for the four kinds. The
`turn_index` field lets the frontend group into turns without the backend imposing nesting.

### 3. The builder (`buildTimeline`) — agent-agnostic

Pure function: `[]map[string]any` (CIM rows, time-ordered) → `[]models.TimelineEvent`.
No DB access, no agent switch — unit-testable in isolation.

**`event_type` → kind / field mapping**

| `event_type` | `kind` | `title` | `body` | extra | `status` |
|---|---|---|---|---|---|
| `user_input` | `user_input` | `"User"` | `user_message` | — | `ok` |
| `llm_response` | `llm_response` | model or `"Assistant"` | `assistant_message` | `model`, `provider` | `ok` |
| `tool_invoke` | `tool_request` | `tool_name` | `tool_input` | `tool_name`, `tool_call_id` | `blocked` if `hook_decision='block'`, else `ok` (`block_reason` → attributes) |
| `tool_complete` | `tool_result` | `tool_name` | `tool_result` | `tool_name`, `tool_call_id` | `error` if `tool_success` is false, else `ok` |
| `session_start` | `session_start` | `"Session start"` | `""` | — | `ok` |
| `session_end` | `session_end` | `"Session end"` | `""` | — | `ok` |

Rows with any other `event_type` are skipped (the SQL `IN (...)` already filters them; the
builder double-checks and ignores unknowns rather than emitting a generic node).

**Turn grouping.** A turn = one `user_input` plus everything until the next `user_input`.
Iterate the ordered rows with a counter starting at 0; increment it the moment a `user_input`
row is seen, then assign. Concretely: any leading `session_start` / events before the first
`user_input` are `turn_index = 0`; the first `user_input` and its following events are
`turn_index = 1`; and so on. This yields a per-turn structure for Claude Code and Hermes
(which set `run_id = session_id`) identical to what OpenClaw gets from its per-turn `run_id`,
without depending on agent-specific run semantics.

**Tool pairing (Approach A).** `tool_call_id` is extracted in Go from `raw_event_data` by a
small helper that tries the known key spellings in order: `toolCallId` (OpenClaw) then
`tool_call_id` (Hermes). Claude Code hook payloads carry no stable tool-call id, so the field
is left empty and the frontend pairs a `tool_result` to the preceding `tool_request` by order
within the same turn. Request and result remain two distinct events — never collapsed — per
the four-kind requirement.

### 4. Frontend — replace the Timeline tab

- Add `api.getSessionTimeline(agentId, sessionId, { run_id } = {})` to `frontend/src/api/client.js`.
- In `frontend/src/pages/AgentDetail.jsx`:
  - Fetch the normalized timeline in the effect that currently calls `api.listEvents` for the
    Timeline tab (keyed on `id`, `selectedSession`, `selectedRun`, `timePreset`, `refreshTick`).
  - Delete the per-hook/per-agent normalization: the `RunTimeline` component and the
    `user_prompt_submit` / `llm_output` / `pre_llm_call` / `post_llm_call` / `pre_api_request`
    / `pre_tool_call` / `post_tool_call` branching at ~L190–461.
  - Render events grouped by `turn_index`: a user bubble, then the LLM response, then
    tool request/result pairs, using `status` for blocked/error styling. Reuse existing
    "Digital Sentry" styling tokens and the JSON pretty-printer already in the file.
- Untouched: the Trace tab and its `api.getRunTrace` effect, `traceData`, and the waterfall
  renderer.

### 5. Route registration

Register the new handler in `cmd/server/main.go` alongside the existing session routes
(near `GetRunTrace`):

```
GET /api/agents/:id/sessions/:session_id/timeline → GetSessionTimeline(cfg)
```

## Data flow

```
agentguard_hook_events
   │  (mv_cim_claudecode / mv_cim_openclaw / mv_cim_hermes — normalize once)
   ▼
agentguard_cim_event ──► GET …/sessions/:sid/timeline ──► buildTimeline()
                                                              │  (event_type → kind,
                                                              │   turn_index, tool_call_id)
                                                              ▼
                                                     TimelineResponse{ events[] }
                                                              │
                                                              ▼
                                          AgentDetail Timeline tab (group by turn_index)
```

## Error handling

- Invalid `id` / `session_id` / `run_id` → `400` (reuse `splitAgentID`, `idPartRe`, `runIDRe`).
- Query failure → log + `503 {"error":"data store unavailable"}` (matches `ListEvents` /
  `GetRunTrace`).
- Zero rows → `200` with `{"events": []}` (an empty session is not an error; the frontend
  renders an empty-state). This differs from `GetRunTrace`'s `404`, because the Timeline tab
  is session-scoped and an empty result is a normal "nothing yet" state.
- Unparseable `raw_event_data` → `tool_call_id` falls back to empty string; the event is still
  emitted.

## Testing

Table-driven Go test for `buildTimeline` in `handlers/session_timeline_test.go`, following the
existing `timeline_openclaw_test.go` pattern. One fixture set per agent type covering:

- `event_type` → `kind` mapping for all six handled types.
- `turn_index` increments correctly across multiple `user_input` rows, including a leading
  `session_start` landing in turn 0.
- `tool_call_id` extraction for both `toolCallId` (OpenClaw) and `tool_call_id` (Hermes), and
  empty fallback for Claude Code rows that omit it.
- `status`: `blocked` when `tool_invoke` has `hook_decision='block'`; `error` when
  `tool_complete` has `tool_success=false`; `ok` otherwise.
- Ordering preserved (output is time-ordered as queried).

## Files

| File | Change |
|---|---|
| `backend/internal/models/models.go` | Add `TimelineEvent`, `TimelineResponse` |
| `backend/internal/handlers/session_timeline.go` | New: `GetSessionTimeline`, `buildTimeline`, `parseToolCallID` |
| `backend/internal/handlers/session_timeline_test.go` | New: table-driven builder tests |
| `backend/cmd/server/main.go` | Register the `…/timeline` route |
| `frontend/src/api/client.js` | Add `getSessionTimeline` |
| `frontend/src/pages/AgentDetail.jsx` | Rewrite Timeline tab; remove per-hook branching |

## Acceptance criteria

- For Claude Code, OpenClaw, and Hermes, the Timeline tab shows the four kinds (user input,
  LLM response, tool request, tool result) in correct chronological order, sourced from
  `agentguard_cim_event`.
- No agent-specific hook names appear in the Timeline tab code path (backend or frontend),
  except the two `tool_call_id` key spellings in `parseToolCallID`.
- Adding a new agent type requires only its CIM MV — no Timeline tab changes.
- The Trace tab, `GetRunTrace`, `TraceSpan`, and the three analyzers are unchanged and still
  function.
```
