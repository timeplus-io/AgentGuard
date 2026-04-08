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

| Pattern | Type label |
|---------|-----------|
| `agent:<id>:main` | Web UI |
| `agent:<id>:openai-user:<x>` | HTTP API |
| `agent:<id>:cron:<x>` | Cron |
| `agent:<id>:subagent:<uuid>` | Sub-agent |
| anything else | Unknown |

---

## Backend — New Endpoints

### Extend `GET /api/agents/:id`

Add to existing response (no breaking change):
```json
{
  "agent_id": "main",
  "deployment_id": "local",
  "deployment_name": "Local Dev",
  "last_seen": "...",
  "event_count": 835,
  "session_count": 5,
  "run_count": 42,
  "block_count": 3,
  "model": "kimi-k2.5:cloud",
  "provider": "ollama"
}
```

New fields from Proton: `run_count` = `count(DISTINCT run_id)`, `block_count` = `countIf(hook_decision='block')`, `model` = `any(model)` filtered to non-empty, `provider` = `any(provider)` filtered to non-empty.

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

Returns runs within a session, oldest first.

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
      "tool_count": 0,
      "has_block": false
    }
  ]
}
```

SQL: `GROUP BY run_id` with `WHERE session_id = X AND run_id != ''`, compute `min(event_time)` as `start_time`, `max(event_time)` as `end_time`, `count()`, `any(model)`, `countIf(hook_name = 'before_tool_call')`, `countIf(hook_decision = 'block') > 0`.

### `GET /api/agents/:id/events`

Returns events for the timeline. Supports query params `session_id` and `run_id` (both optional, stackable).

Query params:
- `session_id=<uuid>` — filter to session
- `run_id=<uuid>` — filter to run (implies session)
- `limit=<n>` — default 100, max 500

Response:
```json
[
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
]
```

SQL: `SELECT ... FROM table(agentshield_hook_events) WHERE deployment_id=X AND agent_id=Y [AND session_id=Z] [AND run_id=W] ORDER BY event_time ASC LIMIT N`.

---

## Frontend — UI Layout

```
┌─────────┬─────────────────────────────────────────────────────────┐
│ Sidebar │  [← Agents]  main  [Local Dev]       [Live Monitor]     │
│  (nav)  ├──────────────────────────────────────────────────────────┤
│         │  Events  Sessions  Runs  Blocks  (metric cards row)      │
│         ├──────────────────┬───────────────────────────────────────┤
│         │ Sessions panel   │  Timeline                             │
│         │ (w-56 fixed)     │                                       │
│         │                  │  ● hook_name        14:22:01         │
│         │ ▾ Web UI         │    model / tool details              │
│         │   › Run 1  ←sel  │                                       │
│         │   › Run 2        │  ● hook_name        14:21:58         │
│         │   › Run 3        │    ...                               │
│         │                  │                                       │
│         │ ▸ HTTP API       │  (empty state if no session          │
│         │ ▸ HTTP API       │   selected: "Select a session        │
│         │ ▸ Cron           │   from the left to view events")     │
│         │                  │                                       │
└─────────┴──────────────────┴───────────────────────────────────────┘
```

### Sessions Panel

- Fixed width `w-56`, scrollable
- Groups: each session is a row showing session_type icon + label (last segment of session_key) + run_count badge
- Clicking a session: expands runs list beneath it (accordion), updates `?session=` query param, loads events
- Clicking a run: updates `?run_id=` query param, filters timeline
- Selected state: pink accent (`text-[#ffb0cf]`, left border highlight)
- Session type icons: `web` → `language`, `openai-user` → `api`, `cron` → `schedule`, `subagent` → `account_tree`

### Metric Cards (top row, 4 cards)

Replace static cards with live data from `GET /api/agents/:id`:
1. **Total Events** — `event_count`
2. **Sessions** — `session_count`
3. **Runs** — `run_count`
4. **Blocks** — `block_count` (accent color `text-[#ffb4ab]` if > 0)

### Timeline Panel

Each event is a row in a vertical timeline:
- Left dot: color-coded by hook category (llm=pink, tool=blue, security/block=red, lifecycle=zinc)
- Hook name (monospace, small)
- Timestamp (right-aligned, `HH:mm:ss.SSS`)
- Secondary line: relevant fields — `model`, `tool_name`, `hook_decision`, `block_reason` (only non-empty ones shown)
- `event_data` shown as a collapsed expandable JSON block on click

Hook color categories:
```
llm_input / llm_output         → #ffb0cf (pink)
before_tool_call / after_tool_call / tool_result_persist → #93c5fd (blue)
hook_decision = 'block'        → #ffb4ab (red)
before_agent_start / agent_end → zinc-400
everything else                → zinc-600
```

Empty states:
- No session selected: centered message "Select a session to view its event timeline"
- Session selected, no events: "No events recorded for this session"
- Loading: spinner replacing timeline content

### Removed

- Agent Identity panel (static hardcoded model, session ID, security tags, Neural Health Pulse, Update Config / Terminate Agent buttons)

---

## File Structure

**New backend files:**
- `backend/internal/handlers/agent_sessions.go` — ListSessions, GetSession handlers
- `backend/internal/handlers/agent_events.go` — ListEvents handler

**Modified backend files:**
- `backend/internal/models/models.go` — add Session, Run, Event, extended Agent structs
- `backend/internal/handlers/agents.go` — extend GetAgent SQL + rowToAgent
- `backend/cmd/server/main.go` — register new routes

**Modified frontend files:**
- `frontend/src/pages/AgentDetail.jsx` — full rewrite
- `frontend/src/api/client.js` — add listSessions, getSession, listEvents methods

---

## Validation

- `session_id` and `run_id` path/query params validated with same UUID regex before SQL interpolation
- All SQL uses parameterized-style construction with the existing `idPartRe` guard for `deploymentId`/`agentId`; UUIDs validated separately with `uuidRe = regexp.MustCompile(^[0-9a-f-]{36}$)`
- `limit` capped at 500
