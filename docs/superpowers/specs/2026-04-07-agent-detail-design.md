# Agent Detail Page — Design Spec

## Goal

Replace the static AgentDetail page with a live, drill-down monitoring view: sessions list in a left nav panel, event timeline on the right, filtered by session and run via query params.

---

## URL Scheme

Single route, no nested page navigation. All drill-down state lives in query params so views are deep-linkable and shareable.

| URL | View |
|-----|------|
| `/agents/:id` | Agent overview — metric cards + sessions list, no session selected |
| `/agents/:id?session=<session_id>` | Session selected — runs expand under the session in the nav, timeline shows all events for the session |
| `/agents/:id?session=<session_id>&run_id=<run_id>` | Run selected — timeline filtered to that run's events |

`:id` continues using the existing `deploymentId__agentId` compound format.

`session_id` is the UUID from the `session_id` column in `agentshield_hook_events` (not `session_key`). `session_key` is displayed as a human-readable label only.

---

## Session Type Classification

Derive `session_type` from `session_key` pattern for display labels:

| Pattern | Type label | Icon |
|---------|-----------|------|
| `agent:<id>:main` | Web UI | `language` |
| `agent:<id>:openai-user:<x>` | HTTP API | `api` |
| `agent:<id>:cron:<x>` | Cron | `schedule` |
| `agent:<id>:subagent:<uuid>` | Sub-agent | `account_tree` |
| anything else | Unknown | `help_outline` |

---

## Backend — New Endpoints

> **Note:** Extending `GET /api/agents/:id` with `run_count`, `block_count`, `model`, `provider` is deferred to a future iteration when metric cards are wired to live data.

### `GET /api/agents/:id/sessions`

Returns sessions for the agent, newest first.

Response:
```json
[
  {
    "session_id": "abc-123",
    "session_key": "agent:main:openai-user:test-s1",
    "session_type": "HTTP API",
    "run_count": 3,
    "event_count": 45,
    "first_seen": "2026-04-07T23:32:46Z",
    "last_seen": "2026-04-07T23:33:17Z"
  }
]
```

SQL: `GROUP BY session_id, session_key` with `WHERE agent_id = X AND deployment_id = Y AND session_id != ''`, ordered by `last_seen DESC`.

### `GET /api/agents/:id/sessions/:session_id`

Returns metadata and runs list for one session.

Response:
```json
{
  "session_id": "abc-123",
  "session_key": "agent:main:openai-user:test-s1",
  "session_type": "HTTP API",
  "runs": [
    {
      "run_id": "e566fb8c-...",
      "start_time": "2026-04-07T21:41:17Z",
      "end_time": "2026-04-07T21:41:19Z",
      "event_count": 6,
      "model": "kimi-k2.5:cloud",
      "tool_count": 2,
      "has_block": false
    }
  ]
}
```

SQL: `GROUP BY run_id` with `WHERE session_id = X AND run_id != ''`. Compute: `min(event_time)` as `start_time`, `max(event_time)` as `end_time`, `count()`, `any(model)`, `countIf(hook_name = 'before_tool_call')` as `tool_count`, `countIf(hook_decision = 'block') > 0` as `has_block`. Ordered by `start_time ASC`.

### `GET /api/agents/:id/events`

Returns paginated events for the timeline.

Query params:
- `session_id=<uuid>` — filter to session (required unless run_id provided)
- `run_id=<uuid>` — additionally filter to a specific run
- `since=<RFC3339>` — only events after this time (time range start)
- `until=<RFC3339>` — only events before this time (time range end)
- `page=<n>` — 1-based page number, default 1
- `limit=<n>` — events per page, default 100, max 500

Response:
```json
{
  "events": [
    {
      "hook_name": "llm_output",
      "event_time": "2026-04-07T21:41:19Z",
      "run_id": "e566fb8c-...",
      "session_id": "abc-123",
      "tool_name": "",
      "provider": "ollama",
      "model": "kimi-k2.5:cloud",
      "hook_decision": "",
      "block_reason": "",
      "event_data": "{...}"
    }
  ],
  "total": 145,
  "page": 1,
  "limit": 100
}
```

SQL: `SELECT ... FROM table(agentshield_hook_events) WHERE deployment_id=X AND agent_id=Y [AND session_id=Z] [AND run_id=W] [AND event_time >= since] [AND event_time <= until] ORDER BY event_time ASC LIMIT limit OFFSET (page-1)*limit`.

---

## Frontend — UI Layout

```
┌─────────┬──────────────────────────────────────────────────────────────┐
│ Sidebar │  [← Agents]  main  [Local Dev]    [↺ Refresh]               │
│  (nav)  ├───────────────────────────────────────────────────────────────┤
│         │  [Events: 835]  [Sessions: 5]  [Runs: 42]  [Blocks: 3]       │
│         ├──────────────┬────────────────────────────────────────────────┤
│         │ Sessions     │  Timeline                     [← prev] [next →]│
│         │ ─────────    │                                                 │
│         │ 🌐 Web UI    │  ● llm_output           14:22:01.442           │
│         │  › Run #1    │    ollama / kimi-k2.5:cloud                    │
│         │  › Run #2 ◀  │    ▸ LLM Output  (click to expand detail panel)│
│         │  › Run #3    │                                                 │
│         │ 🔌 HTTP API  │  ● before_tool_call      14:22:00.181          │
│         │ ⏰ Cron      │    write_file                                   │
│         │ 🌿 Sub-agent │    ▸ Tool Call                                  │
│         │              │                                                 │
│         │              │  (empty: "Select a session")                   │
└─────────┴──────────────┴────────────────────────────────────────────────┘
                                         ↓ clicking ▸ expands detail panel
                          ┌──────────────────────────────────┐
                          │ LLM Output                        │
                          │ Provider   ollama                 │
                          │ Model      kimi-k2.5:cloud        │
                          │ Run ID     e566fb8c-...           │
                          │ Time       2026-04-07 14:22:01    │
                          └──────────────────────────────────┘
```

### Sessions Panel

- Fixed width `w-52`, scrollable, no horizontal resize
- Each session row: icon + session_type label + run_count badge
- Session label: `session_type` (e.g. "Web UI", "HTTP API") — `session_key` shown as subtitle in smaller text
- Clicking session: accordion expands showing run list beneath, sets `?session=` param, loads events for that session
- Each run row: `Run #N` label + start time + tool/block badges if applicable
- Clicking run: sets `?run_id=` param, filters timeline
- Active selection: `border-l-2 border-[#ffb0cf] text-[#ffb0cf]` highlight
- Session type icons via Material Symbols: language / api / schedule / account_tree

### Metric Cards (top row, 4 compact cards)

Kept as static placeholders in this implementation round — live data wiring deferred to a future iteration. The 4 cards remain visible in the layout:
1. **Total Events** / icon: `bolt`
2. **Sessions** / icon: `layers`
3. **Runs** / icon: `sync_alt`
4. **Blocks** / icon: `block`

The `GET /api/agents/:id` extension (adding `run_count`, `block_count`, `model`, `provider`) is also deferred.

### Refresh Button

- In the top-right of the page header: `↺ Refresh` button
- On click: re-fetches agent stats, sessions list, and current events (whichever are loaded)
- Shows a spinning icon while any fetch is in-flight

### Timeline Panel

Each event is a row in a vertical timeline list:

**Row structure:**
```
  ● hook_name                           HH:mm:ss.SSS
    subtitle line (model / tool_name / decision — non-empty only)
    ▸ Event Details  (expandable)
```

**Dot colors** (left dot and accent):
```
llm_input / llm_output                         → #ffb0cf (pink)
before_tool_call / after_tool_call
  / tool_result_persist                         → #93c5fd (blue-300)
hook_decision = 'block'                         → #ffb4ab (red)
before_agent_start / agent_end                  → zinc-400
everything else                                 → zinc-600
```

**Expandable detail panel** (inline below the event row, not a modal):

Clicking `▸ Event Details` expands a structured panel. Content varies by hook type:

| Hook type | Fields shown |
|-----------|-------------|
| `llm_input` | Provider, Model, Run ID, Session ID, Message count (from event_data) |
| `llm_output` | Provider, Model, Run ID, finish reason (from event_data) |
| `before_tool_call` / `after_tool_call` | Tool Name, Run ID, tool input args (from event_data, formatted as key-value pairs) |
| `tool_result_persist` | Tool Name, result preview (truncated to 200 chars from event_data) |
| `hook_decision = 'block'` | Block Reason, Rule that fired (from event_data) |
| `before_agent_start` / `agent_end` | Run ID, Session ID, duration if both present |
| anything else | Key-value pairs of all non-empty top-level fields from event_data |

`event_data` is parsed as JSON. Fields are displayed as a clean key-value table (`text-[10px]` labels, `text-xs font-mono` values), not raw JSON.

**Pagination controls** (bottom of timeline):
- Shows `Showing X–Y of Z events`
- Prev / Next buttons
- Page size selector: 25 / 50 / 100

**Time range filter** (above timeline):
- Two inputs: `From` and `To` (datetime-local HTML inputs)
- Default: empty (no time filter)
- On change: triggers re-fetch of events with `since=` and `until=` params
- Clearing both resets to no filter

**Empty / loading states:**
- No session selected: "Select a session from the left to view its event timeline"
- Loading: spinner in place of timeline
- Session selected, 0 events: "No events recorded for this session"

### Removed

- Agent Identity panel (static model, session ID, security tags, Neural Health Pulse, Update Config / Terminate Agent buttons)

---

## File Structure

**New backend files:**
- `backend/internal/handlers/agent_sessions.go` — `ListSessions(cfg)`, `GetSession(cfg)` handlers
- `backend/internal/handlers/agent_events.go` — `ListEvents(cfg)` handler

**Modified backend files:**
- `backend/internal/models/models.go` — add `Session`, `Run`, `Event`, `EventsResponse` structs
- `backend/cmd/server/main.go` — register 3 new routes

**Modified frontend files:**
- `frontend/src/pages/AgentDetail.jsx` — full rewrite
- `frontend/src/api/client.js` — add `listSessions(id)`, `getSession(id, sessionId)`, `listEvents(id, params)` methods

---

## Validation

- `session_id` and `run_id` path/query params validated with `uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)` before SQL interpolation
- `deploymentId` and `agentId` continue using existing `idPartRe`
- `since` / `until` parsed with `time.Parse(time.RFC3339, ...)` — rejected with 400 if malformed
- `limit` capped at 500, `page` minimum 1
