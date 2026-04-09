# LiveAgentActivity Component Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the "Live Agent Activity" streaming panel from `Threats.jsx` into a reusable `LiveAgentActivity` table component, then replace the static agents table on the Dashboard with it.

**Architecture:** A new `LiveAgentActivity` component owns the Proton streaming query, connection state, and table rendering. `Threats.jsx` and `Dashboard.jsx` import it and pass `timeRange` / `limit` props. All streaming logic lives exclusively in the component.

**Tech Stack:** React, `api.protonQuery` (async generator via `@timeplus/proton-javascript-driver`), Tailwind CSS dark theme.

---

## File Structure

| File | Change |
|------|--------|
| `frontend/src/components/LiveAgentActivity.jsx` | **Create** — new reusable component |
| `frontend/src/pages/Threats.jsx` | **Modify** — remove inline activity panel + helpers, import component |
| `frontend/src/pages/Dashboard.jsx` | **Modify** — remove `staticAgents`/`statusColors`, import component |

---

### Task 1: Create `LiveAgentActivity` component

**Files:**
- Create: `frontend/src/components/LiveAgentActivity.jsx`

The component streams from `agentshield_hook_events`, maintains newest-first rows (capped at `limit`), and renders a table with columns: Time · Agent · Action · Tool/Model · Decision.

- [ ] **Step 1: Write the component**

Create `frontend/src/components/LiveAgentActivity.jsx` with this exact content:

```jsx
import { useState, useEffect } from 'react'
import { api } from '../api/client'

const sqlActivity = (range) => `
SELECT hook_name, event_time, agent_id, tool_name, model, hook_decision
FROM agentshield_hook_events
WHERE hook_name IN ('llm_input', 'llm_output', 'before_tool_call', 'after_tool_call')
SETTINGS seek_to='${range}'`

function formatEvtTime(ts) {
  if (!ts) return '—'
  try {
    return new Date(typeof ts === 'string' ? ts.replace(' ', 'T') : ts)
      .toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return String(ts) }
}

function actionLabel(evt) {
  if (evt.hook_name === 'llm_input')        return 'LLM call'
  if (evt.hook_name === 'llm_output')       return 'LLM response'
  if (evt.hook_name === 'before_tool_call') return 'Tool invoke'
  if (evt.hook_name === 'after_tool_call')  return 'Tool complete'
  return evt.hook_name
}

function toolOrModel(evt) {
  if (evt.hook_name === 'llm_input' || evt.hook_name === 'llm_output') return evt.model || '—'
  return evt.tool_name || '—'
}

export default function LiveAgentActivity({ timeRange = '-1h', limit = 20 }) {
  const [rows, setRows] = useState([])
  const [connected, setConnected] = useState(false)

  useEffect(() => {
    setRows([])
    setConnected(false)
    const ctrl = new AbortController()

    async function stream() {
      let first = true
      for await (const row of api.protonQuery(sqlActivity(timeRange), { signal: ctrl.signal })) {
        if (first) { setConnected(true); first = false }
        setRows(prev => [row, ...prev].slice(0, limit))
      }
      setConnected(false)
    }

    stream().catch(e => { if (e?.name !== 'AbortError') setConnected(false) })
    return () => ctrl.abort()
  }, [timeRange, limit])

  const sorted = [...rows].sort((a, b) => {
    const ta = new Date(typeof a.event_time === 'string' ? a.event_time.replace(' ', 'T') : a.event_time)
    const tb = new Date(typeof b.event_time === 'string' ? b.event_time.replace(' ', 'T') : b.event_time)
    return tb - ta
  })

  return (
    <div className="bg-[#2c2834] rounded-sm overflow-hidden">
      <div className="px-6 py-4 flex items-center justify-between border-b border-[#574149]/10">
        <h3 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest flex items-center gap-3">
          <span className={`w-2 h-2 rounded-full ${connected ? 'bg-[#ffb0cf] shadow-[0_0_10px_#ffb0cf] animate-pulse' : 'bg-zinc-600'}`} />
          Live Agent Activity
        </h3>
        {connected && (
          <span className="text-[10px] font-semibold text-[#ffb0cf] uppercase tracking-widest">Live</span>
        )}
      </div>
      <div className="overflow-auto max-h-80">
        <table className="w-full text-left border-collapse">
          <thead className="bg-[#211e29] sticky top-0 z-10">
            <tr>
              {['Time', 'Agent', 'Action', 'Tool / Model', 'Decision'].map(h => (
                <th key={h} className="px-6 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-widest">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-[#574149]/10">
            {sorted.length === 0 ? (
              <tr>
                <td colSpan={5} className="px-6 py-10 text-center text-zinc-600 text-xs">
                  {connected ? 'Waiting for agent activity…' : 'Connecting…'}
                </td>
              </tr>
            ) : sorted.map((evt, i) => (
              <tr key={`${evt.event_time}-${evt.agent_id}-${evt.hook_name}-${i}`} className="hover:bg-[#37333f] transition-colors">
                <td className="px-6 py-3 text-[10px] font-mono text-[#ffb0cf] whitespace-nowrap">{formatEvtTime(evt.event_time)}</td>
                <td className="px-6 py-3">
                  <code className="text-xs font-mono text-[#e7dff0]">{evt.agent_id}</code>
                </td>
                <td className="px-6 py-3 text-xs text-zinc-400">{actionLabel(evt)}</td>
                <td className="px-6 py-3 text-xs text-zinc-500 font-mono">{toolOrModel(evt)}</td>
                <td className="px-6 py-3">
                  {evt.hook_decision && evt.hook_decision !== 'allow' ? (
                    <span className={`text-[10px] px-1.5 py-0.5 rounded-full font-semibold uppercase ${
                      evt.hook_decision === 'block'
                        ? 'bg-red-500/20 text-red-400'
                        : 'bg-amber-500/20 text-amber-400'
                    }`}>{evt.hook_decision}</span>
                  ) : (
                    <span className="text-[10px] text-zinc-600">—</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Verify build**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/frontend && npm run build 2>&1 | tail -5
```

Expected: `✓ built in`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/LiveAgentActivity.jsx
git commit -m "feat: add LiveAgentActivity streaming table component"
```

---

### Task 2: Update `Threats.jsx` to use the component

**Files:**
- Modify: `frontend/src/pages/Threats.jsx`

Remove the inline activity streaming logic and the activity panel section. Replace with `<LiveAgentActivity timeRange={timeRange} />`. The `connected` state and threats streaming are untouched.

- [ ] **Step 1: Add the import**

At the top of `Threats.jsx`, after the existing imports, add:

```js
import LiveAgentActivity from '../components/LiveAgentActivity'
```

- [ ] **Step 2: Remove `sqlActivity`**

Delete this block (lines ~25-29):

```js
const sqlActivity = (range) => `
SELECT hook_name, event_time, agent_id, tool_name, model, hook_decision
FROM agentshield_hook_events
WHERE hook_name IN ('llm_input', 'llm_output', 'before_tool_call', 'after_tool_call')
SETTINGS seek_to='${range}'`
```

- [ ] **Step 3: Remove `formatEvtTime` and `formatActivity`**

Delete both functions (lines ~38-52):

```js
function formatEvtTime(ts) { ... }
function formatActivity(evt) { ... }
```

- [ ] **Step 4: Remove `liveActivity` state and `sortedActivity`**

Inside the component body, remove:

```js
const [liveActivity, setLiveActivity] = useState([])
```

and:

```js
const sortedActivity = [...liveActivity].sort((a, b) => { ... })
```

- [ ] **Step 5: Remove `streamActivity` and its call**

Inside `useEffect`, remove:

```js
const activityCtrl = new AbortController()
```

```js
async function streamActivity() {
  for await (const row of api.protonQuery(sqlActivity(timeRange), { signal: activityCtrl.signal })) {
    setLiveActivity(prev => [row, ...prev].slice(0, 20))
  }
}
```

```js
streamActivity().catch(() => {})
```

and the cleanup:

```js
return () => {
  threatCtrl.abort()
  activityCtrl.abort()
}
```

Replace with:

```js
return () => { threatCtrl.abort() }
```

- [ ] **Step 6: Replace the Live Activity Panel section**

Remove the entire `{/* Live Activity Panel */}` section (the `<section>` with `bg-[#2c2834] p-8 rounded-sm relative overflow-hidden` and everything inside it).

Replace it with:

```jsx
<LiveAgentActivity timeRange={timeRange} />
```

- [ ] **Step 7: Verify build**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/frontend && npm run build 2>&1 | tail -5
```

Expected: `✓ built in`

- [ ] **Step 8: Commit**

```bash
git add frontend/src/pages/Threats.jsx
git commit -m "refactor(threats): replace inline activity panel with LiveAgentActivity component"
```

---

### Task 3: Update `Dashboard.jsx` to use the component

**Files:**
- Modify: `frontend/src/pages/Dashboard.jsx`

Remove `staticAgents`, `statusColors`, and the "Recent Agent Activity" table. Replace with `<LiveAgentActivity timeRange="-1h" limit={10} />`.

- [ ] **Step 1: Add the import**

After existing imports in `Dashboard.jsx`, add:

```js
import LiveAgentActivity from '../components/LiveAgentActivity'
```

- [ ] **Step 2: Remove `staticAgents` and `statusColors`**

Delete both top-level constants:

```js
const staticAgents = [
  { id: 'Shield-Core-01', ... },
  ...
]

const statusColors = {
  ACTIVE: 'text-emerald-400 bg-emerald-400/10',
  SUSPENDED: 'text-[#ffb4ab] bg-[#ffb4ab]/10',
  THROTTLED: 'text-amber-500 bg-amber-500/10',
}
```

- [ ] **Step 3: Replace the "Recent Agent Activity" section**

Remove the entire `{/* Recent Agent Activity */}` div (the `<div className="bg-[#2c2834] rounded-sm overflow-hidden">` block containing the header and table with `staticAgents.map`).

Replace it with:

```jsx
<LiveAgentActivity timeRange="-1h" limit={10} />
```

- [ ] **Step 4: Verify build**

```bash
cd /Users/gangtao/Code/timeplus/agentshield/frontend && npm run build 2>&1 | tail -5
```

Expected: `✓ built in`

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/Dashboard.jsx
git commit -m "feat(dashboard): replace static agent table with LiveAgentActivity component"
```

---

## Self-Review

**Spec coverage:**
- ✅ `LiveAgentActivity` component created as table (Task 1)
- ✅ Used in `Threats.jsx` with `timeRange` prop wired to filter bar (Task 2)
- ✅ Used in `Dashboard.jsx` with `timeRange="-1h"` and `limit={10}` (Task 3)
- ✅ All inline activity streaming logic removed from `Threats.jsx`
- ✅ Static `staticAgents` and `statusColors` removed from `Dashboard.jsx`

**Placeholder scan:** None found — all steps have exact code.

**Type consistency:** `timeRange` and `limit` props are used consistently across Tasks 1–3. `sqlActivity`, `formatEvtTime`, `actionLabel`, `toolOrModel` defined in Task 1, not referenced elsewhere.
