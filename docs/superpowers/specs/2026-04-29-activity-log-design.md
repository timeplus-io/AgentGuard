# Activity Log Design

**Date:** 2026-04-29  
**Status:** Approved  
**Scope:** New Activity tab inside the Threats UI showing tool call hold history and current status

---

## Problem

The existing blocking/approval system captures rich data about tool call holds (pending, approved, denied, timeout, abandoned, auto_block) but has no dedicated history view. The only approval UI is a banner embedded inside `ThreatDetail.jsx` for a single threat. There is no way to see a timeline of what happened across all holds, which rules triggered them, who decided, or what the current pending queue looks like.

---

## Goal

Add an Activity log that gives operators:
1. A global view of all hold events across all agents/sessions (live + historical)
2. A session-scoped view inside each ThreatDetail showing activity for that specific session
3. Inline approve/deny for pending holds without leaving the list
4. A detail drawer with full event data and decision history for any row

---

## Architecture

### Backend

**New endpoint:** `GET /api/activity`

Query parameters:
| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `session_id` | string | — | Filter to a single session |
| `include_allowed` | bool | false | When false: query `agentguard_holds` only. When true: join with `agentguard_hook_events` (hook_name IN ('PreToolUse','before_tool_call')) and include rows with status "allowed" |
| `status` | string | — | Filter by hold status: pending, approved, denied, timeout, abandoned, auto_block |
| `limit` | int | 100 | Max rows. Capped at 500 when include_allowed=true |
| `page` | int | 1 | Pagination |

Response row shape:
```json
{
  "id": "hold_id or synthetic id for hook-only events",
  "event_time": "RFC3339",
  "tool_name": "string",
  "status": "pending|approved|denied|timeout|abandoned|auto_block|allowed",
  "rule_id": "string",
  "rule_name": "string",
  "severity": "string",
  "agent_id": "string",
  "session_id": "string",
  "args_summary": "string (max 2048 bytes)",
  "message": "string",
  "decided_by": "string",
  "decided_at": "RFC3339 or null",
  "decided_reason": "string",
  "hook_payload": "string (raw JSON)"
}
```

**Existing endpoint reused:** `POST /api/holds/:hold_id/decide` — no changes needed for approve/deny actions.

**Query logic (include_allowed=false):**
```sql
SELECT arg_max(hold_id, updated_at), arg_max(tool_name, updated_at), arg_max(status, updated_at), ...
FROM agentguard_holds
[WHERE session_id = ?]
[AND status = ?]
GROUP BY hold_id
ORDER BY max(created_at) DESC
LIMIT ?
```

**Query logic (include_allowed=true):**
Union of holds (above) and hook events that have no matching hold (auto-allowed), ordered by event_time DESC.

### Frontend

**New files:**
- `frontend/src/components/ActivityLog.jsx` — shared list component
- `frontend/src/components/ActivityDrawer.jsx` — detail drawer

**Modified files:**
- `frontend/src/pages/Threats.jsx` — add "Threats | Activity" tab bar
- `frontend/src/pages/ThreatDetail.jsx` — add "Overview | Activity" tab bar
- `frontend/src/api/client.js` — add `getActivity(params)` method

---

## Components

### ActivityLog

**Props:**
- `mode`: `"global"` | `"session"`
- `sessionId`: string (session mode only)
- `threatId`: string (session mode only, for context)

**State:**
- `events[]`, `loading`, `error`
- `includeAllowed`: bool (default false) — toggle between holds-only and all tool calls
- `statusFilter`: string | null
- `autoRefresh`: bool (default true in global mode, false in session mode)
- `selectedEvent`: event | null — controls drawer open state

**Columns (global mode):** Time, Tool Name, Status, Rule Triggered, Severity, Agent, Session, Decided By

**Columns (session mode):** Time, Tool Name, Status, Rule Triggered, Severity, Decided By

**Behavior:**
- Pending hold rows highlighted with accent border (no shadows — design system rule)
- Inline Approve / Deny buttons on `status === "pending"` rows only
- Clicking any row opens `ActivityDrawer`
- Auto-refresh polls every 5s; pauses when drawer is open
- Subscribes to `NotificationsProvider` context — `hold_pending` and `hold_decided` SSE events trigger immediate refresh
- "Live" pill indicator toggles auto-refresh on/off
- `include_allowed` toggle shows warning: "Showing all tool call events including allowed — may be high volume"

**Optimistic UI:**
- Inline Approve/Deny updates row status immediately
- Reverts with error toast on API failure
- Full list refresh on success to populate `decided_by` / `decided_at`

### ActivityDrawer

A slide-in panel triggered by clicking any row.

**Content:**
- Tool name, full args (pretty-printed JSON from `hook_payload`)
- Rule that triggered it, severity, message
- Timeline: created_at → decided_at with decided_by and decided_reason
- If `status === "pending"`: shows Approve/Deny form with optional reason textarea
- Fetches fresh data via `GET /api/holds/:hold_id` on open to handle stale state

### Threats.jsx changes

Adds a tab bar at the top of the Threats page:
- **Threats** tab: existing threat list (default)
- **Activity** tab: `<ActivityLog mode="global" />`

### ThreatDetail.jsx changes

Adds a tab bar below the threat header:
- **Overview** tab: existing detail content including the pending hold approval banner (unchanged)
- **Activity** tab: `<ActivityLog mode="session" sessionId={threat.session_id} threatId={threat.id} />`

---

## Data Flow

```
SSE (hold_pending / hold_decided)
  ↓
NotificationsProvider context
  ↓
ActivityLog subscribes → triggers immediate list refresh
  ↓
GET /api/activity → agentguard_holds (+ hook_events if include_allowed)
  ↓
Render rows; pending rows show inline Approve/Deny

Operator clicks Approve/Deny
  ↓
Optimistic status update in UI
  ↓
POST /api/holds/:hold_id/decide
  ↓
On success: full list refresh
On failure: revert row + error toast
```

---

## Error Handling

| Scenario | Behavior |
|----------|----------|
| `GET /api/activity` fails | Inline error banner with retry button; auto-refresh pauses |
| Auto-refresh resumes | After successful manual retry |
| Drawer opened on stale pending row | Drawer fetches fresh hold state on open; shows decision result if already decided |
| Timeout hold | Row shows "Timed out — auto-[allowed/denied] by policy"; no action buttons |
| Abandoned hold | Display-only, no action buttons |
| include_allowed=true, high volume | Warning banner; backend caps at 500 rows |

---

## Empty States

| Context | Message |
|---------|---------|
| Global, holds-only, no data | "No tool call holds recorded yet. Holds appear here when a security rule triggers on an agent tool call." |
| Global, include_allowed, no data | "No tool call events recorded yet." |
| Session mode, no data | "No tool call activity for this session." |

---

## Out of Scope

- Adding Activity as a top-level sidebar nav item (intentionally kept inside Threats)
- Real-time streaming of individual event updates via SSE (polling + SSE trigger is sufficient)
- Exporting activity log to CSV/JSON
- Filtering by date range (future enhancement)
- Filtering by agent_id in global mode (future enhancement — current design shows agent_id as a column only)
