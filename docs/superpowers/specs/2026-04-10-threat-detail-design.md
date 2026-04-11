# Threat Detail Page — Design Spec

**Date:** 2026-04-10
**Status:** Approved

## Summary

Replace the inline expand-row in `ThreatsList` with a dedicated full-page threat detail view at `/threats/:id`. Each threat gets its own URL, enabling deep-linking, bookmarking, and a richer event timeline than the table drawer allowed.

---

## 1. Routing & Navigation

### New route
`/threats/:id` → `ThreatDetail` page component, added to `App.jsx`.

### ThreatsList changes
- Remove the inline expand-row behaviour (`expandedId` state + the `<tr>` fragment that renders event history).
- The entire table row remains clickable and navigates to `/threats/:id`.
- The "Acknowledge" and "Clear" action buttons in the row are **kept** so users can act without leaving the list.
- The row cursor changes from `cursor-pointer` (expand) to a React Router `<Link>` wrapping the row, or `useNavigate` on row click (stop-propagation for action buttons).

---

## 2. Backend — `GET /api/threats/:id`

### New handler: `GetThreat`
- File: `backend/internal/handlers/threats.go`
- Route registered in `backend/cmd/server/main.go`: `GET /api/threats/:id`

### Implementation
Reuses the existing three-query concurrent fetch + merge + re-open logic already used by `ListThreats`. After building the full threats list, filters to the single entry whose `computeThreatID(agentID, deploymentID, sessionID, ruleID)` matches the `:id` path parameter.

- Returns `200` with the full `models.Threat` object (including `events[]`, capped at 50 per threat as today).
- Returns `404 {"error": "threat not found"}` if no match.
- Returns `503` if Proton is unavailable (same as `ListThreats`).

No new Proton queries, streams, or schema changes required.

### Frontend API client
Add to `frontend/src/api/client.js`:
```js
getThreat: (id) => get(`/threats/${id}`),
```

---

## 3. Page Layout — `ThreatDetail.jsx`

Follows the `AgentDetail` pattern: `<Sidebar />` + custom `top-nav` header + `<main className="pl-sidebar pt-16">`.

### 3a. Top header band (fixed, `top-nav` CSS class)

Left side:
- Back arrow → `/threats`
- **Page title**: rule name, linked to `/rules/:rule_id`
- Severity badge (existing `SeverityBadge` component)
- Status badge (existing `StatusBadge` inline component)
- Agent chip: `agent_id` text, entire chip linked to `/agents/{deployment_id}__{agent_id}`

Right side (action buttons, optimistic update same as `ThreatsList`):
- `status === 'open'` → "Acknowledge" (yellow) + "Clear" (green)
- `status === 'acknowledged'` → "Clear" (green) only
- `status === 'cleared'` → no action buttons; show a static "Cleared" chip (`bg-green-500/10 text-green-400`)

### 3b. Stat strip

Four stat chips in a single row immediately below the header:

| Chip | Value |
|------|-------|
| Event Count | `event_count` integer |
| First Seen | formatted `first_seen` timestamp |
| Last Seen | formatted `last_seen` timestamp |
| Session ID | truncated monospace (first 12 chars + `…`), full value in `title` tooltip |

### 3c. Event cards (full-width, vertically scrolling)

Section heading: `Event History (N)` where N = `events.length`.

One card per `ThreatEvent`, **always fully expanded** (no expand/collapse), ordered **newest-first**:

```
┌──────────────────────────────────────────────────────────┐
│  [detected_at]          [hook_name badge]  [tool_name]   │
│                                                          │
│  message (full text)                                     │
│  run_id  (small monospace)                               │
│                                                          │
│  ┌──────────────────────────────────────────────────┐   │
│  │  { pretty-printed event_data JSON }              │   │
│  └──────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────┘
```

- Card background: `bg-[#211e29]`, border: `border border-[#574149]/10`
- `event_data` block: `bg-[#0f0c17]`, monospace font, `JSON.stringify(JSON.parse(event_data), null, 2)` with graceful fallback to raw string if parse fails
- If `events` is empty: show "No event history available." placeholder

### 3d. Loading & error states

- Loading: skeleton placeholder cards (pulse animation), same count as `AgentDetail` uses (3 skeleton blocks)
- 404: centred "Threat not found." message with a back link
- 503/error: centred error message

---

## 4. Files Changed

| File | Change |
|------|--------|
| `backend/internal/handlers/threats.go` | Add `GetThreat` handler |
| `backend/cmd/server/main.go` | Register `GET /api/threats/:id` |
| `frontend/src/api/client.js` | Add `getThreat(id)` |
| `frontend/src/App.jsx` | Add `/threats/:id` route |
| `frontend/src/pages/ThreatDetail.jsx` | New page component |
| `frontend/src/components/ThreatsList.jsx` | Remove expand-row; make rows link to detail page; extract `StatusBadge` for reuse |
| `frontend/src/components/Badge.jsx` | Add `StatusBadge` export (moved from `ThreatsList` inline) |

---

## 5. Out of Scope

- Pagination of events beyond the existing 50-event cap (no change)
- Editing threat metadata
- Comments or audit log on status changes
- Push/SSE updates on the detail page (user manually refreshes)
