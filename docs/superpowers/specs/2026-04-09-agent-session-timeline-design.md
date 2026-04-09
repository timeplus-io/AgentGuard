# Agent Session Timeline Design

## Goal

Enhance the AgentDetail page to give users a structured, readable view of what happened inside each session and run — replacing the flat raw-event list as the default view while keeping it accessible as a secondary "Raw Events" tab.

## Scope

- **No new Proton streams or materialized views.** All data comes from the existing `agentshield_hook_events` stream via existing API endpoints.
- Backend: minor additions to `GetSession` response.
- Frontend: new Timeline tab in `AgentDetail.jsx`.

---

## Backend Changes

### File: `backend/internal/handlers/agent_sessions.go`

**`GetSession`** adds one extra SQL query alongside the existing per-run query:

```sql
SELECT
    count_if(hook_name = 'session_end')      AS has_ended,
    count_if(hook_name = 'before_tool_call') AS total_tool_calls
FROM table(agentshield_hook_events)
WHERE deployment_id = '<deploymentID>'
  AND agent_id      = '<agentID>'
  AND session_id    = '<sessionID>'
```

This runs against the full event set (no `run_id != ''` filter), so `session_end` events (which have an empty `run_id`) are counted correctly.

The two values are used to populate `Status` and `TotalToolCalls` on the response.

### File: `backend/internal/models/models.go`

Add two fields to `SessionDetail`:

```go
type SessionDetail struct {
    SessionID     string `json:"session_id"`
    SessionKey    string `json:"session_key"`
    SessionType   string `json:"session_type"`
    Status        string `json:"status"`         // "active" | "ended"
    TotalToolCalls int   `json:"total_tool_calls"`
    Runs          []Run  `json:"runs"`
}
```

`Status` is `"ended"` when `has_ended > 0`, otherwise `"active"`.

---

## Frontend Changes

### File: `frontend/src/pages/AgentDetail.jsx`

#### New state

```js
const [activeTab, setActiveTab] = useState('timeline') // 'timeline' | 'raw'
```

Reset to `'timeline'` when `selectedSession` changes.

#### Session header

Rendered at the top of the timeline panel whenever `selectedSession` is set and `sessionDetail` is loaded:

```
┌─────────────────────────────────────────────────────┐
│ abc123…def  ● Active   3 Runs   12 Tool Calls        │
└─────────────────────────────────────────────────────┘
```

- **Session ID**: monospace, first 20 chars + `…` if longer
- **Status badge**: green pulsing dot + "Active" (`text-emerald-400`) when `sessionDetail.status === 'active'`; grey dot + "Ended" (`text-zinc-500`) otherwise
- **Run count**: from `sessionDetail.runs.length`
- **Tool calls**: from `sessionDetail.total_tool_calls`

#### Tab bar

Immediately below the session header (or at the top when no session is selected yet):

```
[ Timeline ]  [ Raw Events ]
```

Active tab uses `border-b-2 border-[#ffb0cf] text-[#ffb0cf]`; inactive uses `text-zinc-500 hover:text-zinc-300`.

#### Timeline tab

**Event fetch:** Whenever a session or run is selected, events are fetched with `limit=500` regardless of which tab is active. This means both tabs share the same fetched data — switching tabs never triggers a re-fetch. The existing `limit` state and pagination bar are removed; all returned events are visible in Raw Events via scroll.

**Run grouping:** Group the fetched `events` by `run_id` client-side. Iterate over `sessionDetail.runs` (already sorted by `start_time ASC` from backend) to preserve canonical order. Events for a run_id not present in `sessionDetail.runs` are ignored.

If a specific run is selected from the sidebar (`selectedRun`), only that run's card is shown.

**`RunTimeline` card structure:**

```
┌─ Run #1  chatcmpl_abc…  10:23:01 → 10:23:47  🔧 3  ──────────┐
│                                                                  │
│  ○ User Input     "Please list all files in the current…"       │
│  ○ LLM Input      claude-sonnet-4-6 · 12 messages               │
│  ◆ Tool Called    Bash  ✓                                        │
│  ◆ Tool Called    Read  ✗ blocked                                │
│  ○ LLM Output     1,240 in · 380 out · stop                     │
│  ○ Final Response "Here are the files I found: …"               │
└──────────────────────────────────────────────────────────────────┘
```

Run card header contains:
- **Run #N** (index from `sessionDetail.runs` order)
- **run_id** truncated to 16 chars + `…`
- **Time range**: `start_time → end_time` (short HH:MM:SS format)
- **Tool count badge** (blue, same as existing sidebar)
- **Block indicator** (red `block` icon if `run.has_block`)

**Step extraction** (from events for that run_id, sorted by `event_time` ASC):

| Step | Trigger | Content |
|------|---------|---------|
| User Input | First `llm_input` event | Parse `event_data.messages`, find last entry with `role === 'user'`, show `content` truncated to 200 chars |
| LLM Input | `llm_input` event | `model`, `provider`, message count (`data.messages.length`) |
| Tool Called | Each `before_tool_call` event | `tool_name`; green check if `hook_decision !== 'block'`, red "blocked" badge if blocked |
| LLM Output | `llm_output` event | Token usage from `event_data.usage` (input/output/total); `finishReason` or `finish_reason` |
| Final Response | `llm_output` event | Parse `event_data.assistantTexts` (array) → last item, truncated to 300 chars |

If a field is missing or unparseable, that step is omitted silently (no placeholder shown).

Step dot colors:
- User Input / LLM Input / LLM Output / Final Response: `#ffb0cf`
- Tool Called (allowed): `#93c5fd`
- Tool Called (blocked): `#ffb4ab`

The vertical connector line between steps uses `bg-[#574149]/20` (same as existing `EventRow`).

#### Raw Events tab

Existing `EventRow` list, showing all returned events (up to 500) in a scrollable list. The per-page limit selector and pagination bar are removed — they are replaced by the single `limit=500` fetch. `EventRow` component itself is unchanged.

---

## Data Flow

```
selectSession(sid)
  → GET /api/agents/:id/sessions/:sid  (adds status + total_tool_calls)
  → GET /api/agents/:id/events?session_id=sid&limit=500  (shared by both tabs)
  → render session header + tab bar
  → Timeline tab: group events by run_id → render RunTimeline cards
  → Raw Events tab: render EventRow list with pagination
```

---

## What Is Not Changing

- `LiveAgentActivity` component (Dashboard, Threats pages) — untouched
- `agentshield_hook_events` schema — untouched
- `agentshield_security_events` schema — untouched
- No new Proton streams, MVs, or backend routes
- The session sidebar (left panel) and run accordion — untouched
- `ListSessions` handler — untouched
- `ListEvents` handler — untouched

---

## Success Criteria

1. Clicking a session shows a header with correct status ("Active" / "Ended"), run count, and tool call count.
2. Timeline tab (default) groups events by run and renders structured step cards for each run.
3. User Input and Final Response show actual message text (truncated).
4. Blocked tool calls are visually distinct (red badge).
5. Raw Events tab shows existing event list unchanged.
6. Switching tabs does not re-fetch events.
7. Selecting a specific run from the sidebar scopes both tabs to that run.
