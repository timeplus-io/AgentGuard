# Agent Session Timeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enhance the AgentDetail page with a structured Timeline tab that groups raw hook events by run and extracts human-readable steps, while keeping the existing Raw Events view as a secondary tab.

**Architecture:** Two backend fields (`status`, `total_tool_calls`) are added to the `GetSession` response via a second SQL query. The frontend gains a tab bar, a session header strip, and a new `RunTimeline` component that groups events by `run_id` and extracts 5 step types (User Input, LLM Input, Tool Called, LLM Output, Final Response) from raw event data. All event fetching shifts to a fixed `limit=500` so both tabs share the same data without re-fetching on tab switch. Pagination is removed.

**Tech Stack:** Go/Gin (backend), React + Tailwind (frontend), Timeplus Proton SQL (data), Material Symbols (icons)

---

## File Structure

| File | Change |
|------|--------|
| `backend/internal/models/models.go` | Add `Status string` and `TotalToolCalls int` to `SessionDetail` |
| `backend/internal/handlers/agent_sessions.go` | Add session-level stats query in `GetSession`; populate new fields |
| `frontend/src/pages/AgentDetail.jsx` | Remove pagination state; add `activeTab` state; add `fmtShortTime`, `SessionHeader`, `buildRunSteps`, `RunTimeline`; wire tab bar + timeline panel content |

No new files are created.

---

### Task 1: Backend — add `status` and `total_tool_calls` to `GetSession`

**Files:**
- Modify: `backend/internal/models/models.go:175-181`
- Modify: `backend/internal/handlers/agent_sessions.go:218-272`

**Context:** `SessionDetail` currently has `SessionID`, `SessionKey`, `SessionType`, `Runs`. The `GetSession` handler runs one SQL query grouped by `run_id` with `AND run_id != ''` — so `session_end` events (which have empty `run_id`) are invisible. We need a second, separate query against the full event set to count `session_end` and `before_tool_call` occurrences.

- [ ] **Step 1: Add fields to `SessionDetail` in `models.go`**

Replace the `SessionDetail` struct (lines 175–181) with:

```go
// SessionDetail is returned by GET /agents/:id/sessions/:session_id.
type SessionDetail struct {
	SessionID      string `json:"session_id"`
	SessionKey     string `json:"session_key"`
	SessionType    string `json:"session_type"`
	Status         string `json:"status"`           // "active" | "ended"
	TotalToolCalls int    `json:"total_tool_calls"`
	Runs           []Run  `json:"runs"`
}
```

- [ ] **Step 2: Add session-level stats query in `GetSession`**

In `backend/internal/handlers/agent_sessions.go`, after the block that fetches `runsRows` and before building the `runs` slice (after line 248 `if len(runsRows) == 0`), add:

```go
		// Second query: session-level counters (no run_id filter, catches session_end).
		statsSQL := fmt.Sprintf(`
SELECT
    count_if(hook_name = 'session_end')      AS has_ended,
    count_if(hook_name = 'before_tool_call') AS total_tool_calls
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'`,
			deploymentID, agentID, sessionID)

		statsRows, err := timeplus.Query(&cfg.Timeplus, statsSQL)
		if err != nil {
			// Non-fatal: degrade gracefully if stats query fails.
			statsRows = nil
		}

		status := "active"
		totalToolCalls := 0
		if len(statsRows) > 0 {
			if parseBool(statsRows[0]["has_ended"]) {
				status = "ended"
			}
			totalToolCalls = parseInt(statsRows[0]["total_tool_calls"])
		}
```

- [ ] **Step 3: Populate new fields in the `c.JSON` response**

Replace the final `c.JSON` call (currently at the end of `GetSession`, around line 265):

```go
		c.JSON(http.StatusOK, models.SessionDetail{
			SessionID:      sessionID,
			SessionKey:     sessionKey,
			SessionType:    sessionType(sessionKey),
			Status:         status,
			TotalToolCalls: totalToolCalls,
			Runs:           runs,
		})
```

- [ ] **Step 4: Verify the backend compiles**

```bash
cd /path/to/agentshield/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output (clean build).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/models/models.go backend/internal/handlers/agent_sessions.go
git commit -m "feat: add status and total_tool_calls to GetSession response"
```

---

### Task 2: Frontend — remove pagination, simplify event fetch

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx`

**Context:** The current code has `page`, `limit`, `eventsTotal` state and a pagination bar at the bottom of the timeline panel. The spec says both tabs share a fixed `limit=500` fetch, so pagination is removed entirely. The `listEvents` API call already accepts `limit` as a query param.

- [ ] **Step 1: Remove pagination-related state declarations**

In `AgentDetail.jsx`, remove these three state lines (currently around lines 231–233):

```js
// REMOVE these three lines:
const [eventsTotal, setEventsTotal] = useState(0)
const [page, setPage] = useState(1)
const [limit, setLimit] = useState(100)
```

- [ ] **Step 2: Remove derived pagination variables**

Remove these three lines (currently around lines 320–322):

```js
// REMOVE these three lines:
const totalPages = Math.max(1, Math.ceil(eventsTotal / limit))
const startEvent = eventsTotal === 0 ? 0 : (page - 1) * limit + 1
const endEvent = Math.min(page * limit, eventsTotal)
```

- [ ] **Step 3: Update the events `useEffect` to use `limit: 500`**

Replace the events `useEffect` (currently around lines 292–305) with:

```js
useEffect(() => {
  if (!selectedSession && !selectedRun) { setEvents([]); return }
  const params = { limit: 500 }
  if (selectedSession) params.session_id = selectedSession
  if (selectedRun) params.run_id = selectedRun
  const since = presetToISO(timePreset)
  if (since) params.since = since

  setLoadingEvents(true)
  api.listEvents(id, params)
    .then(r => { setEvents(r.events ?? []) })
    .catch(() => setEvents([]))
    .finally(() => setLoadingEvents(false))
}, [id, selectedSession, selectedRun, timePreset, refreshTick])
```

- [ ] **Step 4: Update `selectSession` — remove `setPage` call**

`selectSession` currently calls `setPage(1)`. Remove that call since `page` state no longer exists:

```js
const selectSession = (sid) => {
  setSearchParams({ session: sid })
}
```

(The `setPage(1)` call in `selectRun` should also be removed the same way.)

```js
const selectRun = (rid) => {
  const params = {}
  if (selectedSession) params.session = selectedSession
  params.run_id = rid
  setSearchParams(params)
}
```

- [ ] **Step 5: Remove the pagination bar JSX**

Delete the entire pagination `<div>` at the bottom of the timeline panel section (currently around lines 547–581):

```jsx
{/* DELETE this entire block: */}
{(selectedSession || selectedRun) && eventsTotal > 0 && (
  <div className="flex items-center justify-between px-6 py-3 border-t border-[#574149]/20 shrink-0">
    ...
  </div>
)}
```

- [ ] **Step 6: Build verify**

```bash
cd /path/to/agentshield/frontend
npm run build 2>&1 | tail -20
```

Expected: build succeeds, no errors about `page`, `limit`, `eventsTotal`, `totalPages`.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/AgentDetail.jsx
git commit -m "refactor: replace paginated event fetch with fixed limit=500, remove pagination UI"
```

---

### Task 3: Frontend — helpers, `SessionHeader`, and tab state

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx`

**Context:** We need a `fmtShortTime` helper for run card time display, a `SessionHeader` component showing session ID / status / counts, an `activeTab` state that resets to `'timeline'` when the session changes, and a tab bar rendered in the timeline panel.

- [ ] **Step 1: Add `fmtShortTime` helper**

Add after the existing `fmtTime` function (after line 37):

```js
function fmtShortTime(iso) {
  if (!iso) return ''
  try {
    return new Date(typeof iso === 'string' ? iso.replace(' ', 'T') : iso)
      .toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return '' }
}
```

- [ ] **Step 2: Add `SessionHeader` component**

Add after the `EventRow` component definition (after line 216, before the `// ── Main page` comment):

```jsx
// ── SessionHeader ─────────────────────────────────────────────────────────────

function SessionHeader({ sessionDetail }) {
  const isEnded = sessionDetail.status === 'ended'
  const sid = sessionDetail.session_id
  const shortId = sid.length > 20 ? sid.slice(0, 20) + '…' : sid
  return (
    <div className="px-5 py-3 border-b border-[#574149]/20 flex items-center gap-4 shrink-0 bg-[#1d1a25]">
      <code className="text-[11px] font-mono text-[#e7dff0]">{shortId}</code>
      <span className={`flex items-center gap-1.5 text-[10px] font-semibold ${isEnded ? 'text-zinc-500' : 'text-emerald-400'}`}>
        <span className={`w-1.5 h-1.5 rounded-full ${isEnded ? 'bg-zinc-500' : 'bg-emerald-400 animate-pulse'}`} />
        {isEnded ? 'Ended' : 'Active'}
      </span>
      <span className="text-[10px] text-zinc-500">{sessionDetail.runs.length} Runs</span>
      <span className="text-[10px] text-zinc-500">{sessionDetail.total_tool_calls ?? 0} Tool Calls</span>
    </div>
  )
}
```

- [ ] **Step 3: Add `activeTab` state**

In the `AgentDetail` component state declarations (around line 228), add:

```js
const [activeTab, setActiveTab] = useState('timeline')
```

- [ ] **Step 4: Reset `activeTab` when session changes**

Update `selectSession` to reset the tab:

```js
const selectSession = (sid) => {
  setActiveTab('timeline')
  setSearchParams({ session: sid })
}
```

- [ ] **Step 5: Add tab bar to timeline panel**

In the timeline panel section (the `<section className="flex-1 bg-[#211e29]...">` element, currently around line 521), add the session header and tab bar as the first children, before the `<div className="flex-1 overflow-y-auto">`:

```jsx
{/* Session header */}
{selectedSession && sessionDetail && (
  <SessionHeader sessionDetail={sessionDetail} />
)}

{/* Tab bar */}
{selectedSession && (
  <div className="flex border-b border-[#574149]/20 shrink-0">
    {[
      { id: 'timeline', label: 'Timeline' },
      { id: 'raw', label: 'Raw Events' },
    ].map(tab => (
      <button
        key={tab.id}
        onClick={() => setActiveTab(tab.id)}
        className={`px-5 py-2.5 text-[11px] font-semibold uppercase tracking-wider transition-colors border-b-2 ${
          activeTab === tab.id
            ? 'border-[#ffb0cf] text-[#ffb0cf]'
            : 'border-transparent text-zinc-500 hover:text-zinc-300'
        }`}
      >
        {tab.label}
      </button>
    ))}
  </div>
)}
```

- [ ] **Step 6: Build verify**

```bash
cd /path/to/agentshield/frontend
npm run build 2>&1 | tail -20
```

Expected: clean build. Open `http://localhost:5173/agents/<any-id>`, click a session — you should see the session header and two tabs appear.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/pages/AgentDetail.jsx
git commit -m "feat: add session header and Timeline/Raw Events tab bar to AgentDetail"
```

---

### Task 4: Frontend — `buildRunSteps` + `RunTimeline` component

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx`

**Context:** `buildRunSteps(events)` takes an array of hook events for a single run (sorted by `event_time` ASC) and returns an ordered array of step objects `{ type, label, content, sub?, blocked? }`. `RunTimeline` renders one run card using those steps. Both go above the `// ── Main page` comment, after `SessionHeader`.

The `safeParse` helper already exists in the file (line 58). `fmtTokens` also exists (line 9). Both are reused here.

- [ ] **Step 1: Add `buildRunSteps` function**

Add after `SessionHeader` (before the `// ── Main page` comment):

```js
// ── buildRunSteps ─────────────────────────────────────────────────────────────

function buildRunSteps(events) {
  const steps = []
  const llmIn  = events.find(e => e.hook_name === 'llm_input')
  const llmOut = events.find(e => e.hook_name === 'llm_output')
  const tools  = events.filter(e => e.hook_name === 'before_tool_call')

  // User Input: last user-role message from llm_input event_data
  if (llmIn) {
    const data = safeParse(llmIn.event_data) || {}
    const msgs = Array.isArray(data.messages) ? data.messages : []
    const userMsg = [...msgs].reverse().find(m => m.role === 'user')
    if (userMsg) {
      const raw = typeof userMsg.content === 'string'
        ? userMsg.content
        : Array.isArray(userMsg.content)
          ? userMsg.content.map(c => (typeof c === 'string' ? c : c?.text ?? '')).join('')
          : ''
      if (raw.trim()) {
        const t = raw.trim()
        steps.push({
          type: 'user_input',
          label: 'User Input',
          content: t.slice(0, 200) + (t.length > 200 ? '…' : ''),
        })
      }
    }
  }

  // LLM Input: model + provider from llm_input event
  if (llmIn) {
    const data = safeParse(llmIn.event_data) || {}
    const msgs = Array.isArray(data.messages) ? data.messages : []
    const msgCount = msgs.length || data.messageCount || data.message_count || 0
    const model    = llmIn.model    || data.model    || '—'
    const provider = llmIn.provider || data.provider || ''
    steps.push({
      type:     'llm_input',
      label:    'LLM Input',
      content:  [model, provider].filter(Boolean).join(' · '),
      sub:      msgCount ? `${msgCount} messages` : undefined,
    })
  }

  // Tool Called / Tool Blocked: one step per before_tool_call event
  for (const e of tools) {
    const blocked = e.hook_decision === 'block'
    steps.push({
      type:     'tool_call',
      label:    blocked ? 'Tool Blocked' : 'Tool Called',
      content:  e.tool_name || '—',
      blocked,
    })
  }

  // LLM Output: token usage + finish reason from llm_output event
  if (llmOut) {
    const data   = safeParse(llmOut.event_data) || {}
    const usage  = data.usage ?? data.usageTotals ?? {}
    const inTok  = usage.input  ?? usage.input_tokens  ?? usage.inputTokens
    const outTok = usage.output ?? usage.output_tokens ?? usage.outputTokens
    const finish = data.finishReason ?? data.finish_reason ?? data.stop_reason ?? ''
    const tokenStr = [
      inTok  != null ? `${fmtTokens(inTok)} in`  : null,
      outTok != null ? `${fmtTokens(outTok)} out` : null,
    ].filter(Boolean).join(' · ') || '—'
    steps.push({
      type:    'llm_output',
      label:   'LLM Output',
      content: tokenStr,
      sub:     finish || undefined,
    })

    // Final Response: last assistantTexts entry from llm_output event_data
    const texts   = data.assistantTexts ?? data.assistant_texts ?? []
    const lastTxt = Array.isArray(texts) ? texts[texts.length - 1] : ''
    if (lastTxt && String(lastTxt).trim()) {
      const t = String(lastTxt).trim()
      steps.push({
        type:    'final_response',
        label:   'Final Response',
        content: t.slice(0, 300) + (t.length > 300 ? '…' : ''),
      })
    }
  }

  return steps
}
```

- [ ] **Step 2: Add `RunTimeline` component**

Add immediately after `buildRunSteps`:

```jsx
// ── RunTimeline ───────────────────────────────────────────────────────────────

function RunTimeline({ runIndex, run, events }) {
  const steps = buildRunSteps(events)
  const start = fmtShortTime(run.start_time)
  const end   = fmtShortTime(run.end_time)
  const timeRange = start && end ? `${start} → ${end}` : start || ''

  function stepDot(step) {
    if (step.type === 'tool_call') return step.blocked ? '#ffb4ab' : '#93c5fd'
    return '#ffb0cf'
  }

  return (
    <div className="bg-[#1d1a25] rounded-sm p-5 mb-3 last:mb-0">
      {/* Run header */}
      <div className="flex items-center gap-3 mb-4 flex-wrap">
        <span className="text-xs font-semibold text-[#e7dff0]">Run #{runIndex + 1}</span>
        <code className="text-[10px] font-mono text-zinc-500">
          {run.run_id.length > 16 ? run.run_id.slice(0, 16) + '…' : run.run_id}
        </code>
        {timeRange && <span className="text-[10px] text-zinc-600">{timeRange}</span>}
        {run.tool_count > 0 && (
          <span className="text-[9px] bg-[#93c5fd]/10 text-[#93c5fd] px-1.5 py-0.5 rounded-full">
            {run.tool_count} tools
          </span>
        )}
        {run.has_block && (
          <span className="material-symbols-outlined text-[12px] text-[#ffb4ab]">block</span>
        )}
      </div>

      {/* Steps */}
      {steps.length === 0 ? (
        <p className="text-[11px] text-zinc-600">No structured events for this run.</p>
      ) : (
        <div>
          {steps.map((step, i) => (
            <div key={i} className="flex gap-3">
              <div className="flex flex-col items-center">
                <div
                  className="w-2 h-2 rounded-full shrink-0 mt-1"
                  style={{ backgroundColor: stepDot(step) }}
                />
                {i < steps.length - 1 && (
                  <div className="w-px flex-1 bg-[#574149]/20 my-1" style={{ minHeight: '12px' }} />
                )}
              </div>
              <div className="pb-3 flex-1 min-w-0">
                <span className={`text-[10px] font-semibold uppercase tracking-wide ${
                  step.blocked ? 'text-[#ffb4ab]' : 'text-zinc-400'
                }`}>
                  {step.label}
                </span>
                <p className="text-xs text-[#e7dff0] mt-0.5 break-words leading-relaxed">
                  {step.content}
                </p>
                {step.sub && (
                  <p className="text-[10px] text-zinc-500 mt-0.5">{step.sub}</p>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 3: Build verify**

```bash
cd /path/to/agentshield/frontend
npm run build 2>&1 | tail -20
```

Expected: clean build. The new components are defined but not yet wired into the page output.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/AgentDetail.jsx
git commit -m "feat: add buildRunSteps and RunTimeline component to AgentDetail"
```

---

### Task 5: Frontend — wire tab content into timeline panel

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx`

**Context:** The timeline panel's `<div className="flex-1 overflow-y-auto">` currently renders `EventRow` items unconditionally. We replace that content block to: show Timeline tab (run cards) or Raw Events tab (EventRow list) depending on `activeTab`. When Timeline tab is active, events are grouped by `run_id` and matched to `sessionDetail.runs` order.

- [ ] **Step 1: Replace the event list `<div>` content**

Find the `<div className="flex-1 overflow-y-auto">` inside the timeline `<section>` (currently around line 524). Replace its entire content with:

```jsx
{!selectedSession && !selectedRun ? (
  <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
    <span className="material-symbols-outlined text-4xl">timeline</span>
    <p className="text-sm">Select a session from the left to view its event timeline</p>
  </div>
) : loadingEvents ? (
  <div className="flex items-center justify-center h-full">
    <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
  </div>
) : activeTab === 'raw' ? (
  events.length === 0 ? (
    <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
      <span className="material-symbols-outlined text-4xl">inbox</span>
      <p className="text-sm">No events recorded for this session</p>
    </div>
  ) : (
    events.map((event, i) => (
      <EventRow key={`${event.run_id}-${event.hook_name}-${i}`} event={event} />
    ))
  )
) : (
  // Timeline tab
  (() => {
    const runs = selectedRun
      ? (sessionDetail?.runs ?? []).filter(r => r.run_id === selectedRun)
      : (sessionDetail?.runs ?? [])
    if (runs.length === 0) {
      return (
        <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
          <span className="material-symbols-outlined text-4xl">inbox</span>
          <p className="text-sm">No runs found for this session</p>
        </div>
      )
    }
    // Group events by run_id
    const byRun = {}
    for (const e of events) {
      if (!byRun[e.run_id]) byRun[e.run_id] = []
      byRun[e.run_id].push(e)
    }
    // Sort each run's events by event_time ASC
    for (const rid of Object.keys(byRun)) {
      byRun[rid].sort((a, b) => {
        const ta = new Date(typeof a.event_time === 'string' ? a.event_time.replace(' ', 'T') : a.event_time)
        const tb = new Date(typeof b.event_time === 'string' ? b.event_time.replace(' ', 'T') : b.event_time)
        return ta - tb
      })
    }
    return (
      <div className="p-4">
        {runs.map((run, idx) => (
          <RunTimeline
            key={run.run_id}
            runIndex={idx}
            run={run}
            events={byRun[run.run_id] ?? []}
          />
        ))}
      </div>
    )
  })()
)}
```

- [ ] **Step 2: Build verify**

```bash
cd /path/to/agentshield/frontend
npm run build 2>&1 | tail -20
```

Expected: clean build with no errors.

- [ ] **Step 3: Smoke test in browser**

Start the dev server:
```bash
cd /path/to/agentshield/frontend
npm run dev
```

Navigate to `/agents/<any-deployment__agentid>`.

Check:
1. Click a session → session header appears showing session ID, Active/Ended badge, run count, tool call count.
2. "Timeline" tab is selected by default. Run cards appear for each run.
3. Each run card shows: run #, run_id prefix, time range, tool count badge.
4. Step rows appear: User Input text, LLM Input model, Tool Called rows, LLM Output tokens, Final Response text.
5. Click "Raw Events" tab → existing EventRow list appears, no pagination bar.
6. Switch back to "Timeline" → no spinner (no re-fetch, instant switch).
7. Click a specific run from the sidebar → Timeline shows only that run's card; Raw Events shows only that run's events.
8. Status badge: if a session has a `session_end` hook event, badge shows "Ended" (grey); otherwise "Active" (green pulse).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/AgentDetail.jsx
git commit -m "feat: wire Timeline and Raw Events tabs in AgentDetail session view"
```
