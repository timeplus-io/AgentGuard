# Threats Page Live Streaming Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Threats page's hardcoded static data with two live Proton streaming queries — the threats table streams from `agentshield_security_events`, and the Live Data Stream panel streams from `agentshield_hook_events`.

**Architecture:** Use the existing `api.protonQuery(sql, { signal })` async generator (already proven in `Investigate.jsx`). Each panel opens a single Proton streaming query with `SETTINGS seek_to='-24h'` (or `'-1h'` for activity) — Proton delivers historical rows first as a fast backfill, then continues streaming live events on the same connection. Both streams are cancelled via AbortController on unmount. No backend changes needed — the proxy at `POST /api/proton/query` already streams Proton's JSONEachRow NDJSON to the browser.

**Tech Stack:** React 18, `api.protonQuery` (via `@timeplus/proton-javascript-driver` NDJSON parser), Tailwind CSS, Proton streaming SQL

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `frontend/src/pages/Threats.jsx` | Modify | Full rewrite — remove static data, wire both streams, remove Threat Intelligence panel |

No other files change.

---

## Stream Schemas (reference)

**`agentshield_security_events`** fields used:
`detected_at` (datetime64), `rule_id` (string), `rule_name` (string), `severity` (string: `critical`/`warning`), `agent_id` (string), `session_id` (string), `hook_name` (string), `tool_name` (string), `message` (string)

**`agentshield_hook_events`** fields used:
`hook_name` (string), `event_time` (datetime64), `agent_id` (string), `tool_name` (string), `model` (string), `hook_decision` (string)

---

## SQL Queries (reference)

Both queries use `SETTINGS seek_to` — Proton replays historical rows first, then continues streaming live events on the **same connection**. No chained queries, no phase transition.

```sql
-- SQL_THREATS: backfill last 24 hours then stream live (single connection)
SELECT detected_at, rule_id, rule_name, severity, agent_id,
       session_id, hook_name, tool_name, message
FROM agentshield_security_events
SETTINGS seek_to='-24h'

-- SQL_ACTIVITY: backfill last 1 hour then stream live (single connection)
SELECT hook_name, event_time, agent_id, tool_name, model, hook_decision
FROM agentshield_hook_events
WHERE hook_name IN ('llm_input', 'llm_output', 'before_tool_call', 'after_tool_call')
SETTINGS seek_to='-1h'
```

---

## Task 1: Remove static data and Threat Intelligence panel, scaffold new state

**Files:**
- Modify: `frontend/src/pages/Threats.jsx`

- [ ] **Step 1: Delete all static data and add new state**

Replace the top of `Threats.jsx` (everything before `export default`) with:

```jsx
import { useState, useEffect } from 'react'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { SeverityBadge } from '../components/Badge'
import { api } from '../api/client'

// Single streaming query per panel: Proton delivers historical backfill first,
// then continues streaming live events on the same connection.
const SQL_THREATS = `
SELECT detected_at, rule_id, rule_name, severity, agent_id,
       session_id, hook_name, tool_name, message
FROM agentshield_security_events
SETTINGS seek_to='-24h'`

const SQL_ACTIVITY = `
SELECT hook_name, event_time, agent_id, tool_name, model, hook_decision
FROM agentshield_hook_events
WHERE hook_name IN ('llm_input', 'llm_output', 'before_tool_call', 'after_tool_call')
SETTINGS seek_to='-1h'`
```

- [ ] **Step 2: Replace component state and add streaming useEffect**

Replace the entire function body of `export default function Threats()` with:

```jsx
export default function Threats() {
  const [threats, setThreats] = useState([])
  const [liveActivity, setLiveActivity] = useState([])
  const [connected, setConnected] = useState(false)
  const [severity, setSeverity] = useState('All')
  const [category, setCategory] = useState('All Categories')

  useEffect(() => {
    const threatCtrl = new AbortController()
    const activityCtrl = new AbortController()

    async function streamThreats() {
      setConnected(true)
      // seek_to='-24h' causes Proton to replay last 24h first, then continue live —
      // all on the same connection, no phase switch needed.
      for await (const row of api.protonQuery(SQL_THREATS, { signal: threatCtrl.signal })) {
        setThreats(prev => [row, ...prev].slice(0, 100))
      }
    }

    async function streamActivity() {
      for await (const row of api.protonQuery(SQL_ACTIVITY, { signal: activityCtrl.signal })) {
        setLiveActivity(prev => [row, ...prev].slice(0, 20))
      }
    }

    streamThreats().catch(e => { if (e?.name !== 'AbortError') setConnected(false) })
    streamActivity().catch(() => {})

    return () => {
      threatCtrl.abort()
      activityCtrl.abort()
    }
  }, [])

  const filtered = threats.filter(t => {
    if (severity !== 'All' && t.severity?.toLowerCase() !== severity.toLowerCase()) return false
    if (category !== 'All Categories') {
      const catMap = { 'Injection': 'rp-001', 'Exfiltration': 'rp-002', 'Supply Chain': 'rp-004' }
      if (catMap[category] && t.rule_id !== catMap[category]) return false
    }
    return true
  })

  // ... render below (Task 2)
}
```

- [ ] **Step 3: Verify the app still compiles — open browser, check no console errors on `/threats`**

The page will be blank/broken until Task 2 adds the render. That's fine — just confirm no import errors.

- [ ] **Step 4: Commit**

```bash
cd frontend
git add src/pages/Threats.jsx
git commit -m "feat(Threats): add seek_to streaming state and SQL queries, remove static data"
```

---

## Task 2: Render the threats table with LIVE indicator

**Files:**
- Modify: `frontend/src/pages/Threats.jsx`

- [ ] **Step 1: Replace the render return with the full JSX**

Add the return statement to the component (replacing any existing return):

```jsx
  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-64 pt-16 pb-8 min-h-screen">
        <div className="p-8">
          <PageHeader
            title="Threats"
            subtitle="Real-time surveillance of AI agent activity and anomalous patterns."
          />

          {/* Filter Bar */}
          <section className="bg-[#211e29] rounded p-6 mb-6 flex flex-wrap items-center gap-6">
            <div className="flex flex-col gap-2 flex-1 min-w-[200px]">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Severity</label>
              <div className="flex gap-1">
                {['Critical', 'Warning', 'Info'].map(s => (
                  <button
                    key={s}
                    onClick={() => setSeverity(prev => prev === s ? 'All' : s)}
                    className={`px-3 py-1.5 text-[11px] font-semibold rounded-full transition-colors ${
                      severity === s
                        ? s === 'Critical' ? 'bg-[#93000a] text-[#ffb4ab]'
                          : s === 'Warning' ? 'bg-[#37333f] text-[#ffb2b8]'
                          : 'bg-[#2c2834] text-zinc-300'
                        : 'bg-[#2c2834] text-[#ddbfc8] border border-[#574149]/10'
                    }`}
                  >
                    {s}
                  </button>
                ))}
              </div>
            </div>
            <div className="flex flex-col gap-2 flex-1 min-w-[200px]">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Threat Category</label>
              <select
                value={category}
                onChange={e => setCategory(e.target.value)}
                className="bg-[#0f0c17] border-none text-xs font-medium text-[#e7dff0] rounded-sm py-2 px-3 focus:ring-1 focus:ring-[#ffb0cf]/50"
              >
                <option>All Categories</option>
                <option>Injection</option>
                <option>Exfiltration</option>
                <option>Supply Chain</option>
              </select>
            </div>
          </section>

          {/* Threats Table */}
          <section className="bg-[#211e29] rounded-sm overflow-hidden mb-8">
            <div className="px-6 py-4 flex items-center justify-between border-b border-[#574149]/10">
              <span className="text-xs font-semibold text-zinc-400 uppercase tracking-widest">
                Security Events
                <span className="ml-2 text-zinc-600 font-normal normal-case">({filtered.length})</span>
              </span>
              {connected && (
                <span className="flex items-center gap-1.5 text-[10px] font-semibold text-[#ffb0cf] uppercase tracking-widest">
                  <span className="w-1.5 h-1.5 rounded-full bg-[#ffb0cf] animate-pulse" />
                  Live
                </span>
              )}
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-left border-collapse">
                <thead className="bg-[#2c2834]/50">
                  <tr>
                    {['Severity', 'Rule / Message', 'Agent', 'Session', 'Hook', 'Time'].map((h, i) => (
                      <th key={h} className="px-6 py-4 text-[10px] font-bold text-zinc-400 uppercase tracking-widest">{h}</th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#574149]/10">
                  {filtered.length === 0 ? (
                    <tr>
                      <td colSpan={6} className="px-6 py-12 text-center text-zinc-500 text-xs">
                        {connected ? 'No threats detected yet.' : 'Connecting to threat stream…'}
                      </td>
                    </tr>
                  ) : filtered.map((t, i) => (
                    <tr key={i} className="hover:bg-[#2c2834] transition-colors">
                      <td className="px-6 py-4"><SeverityBadge severity={t.severity} /></td>
                      <td className="px-6 py-4">
                        <p className="text-xs font-semibold text-[#e7dff0]">{t.rule_name}</p>
                        <p className="text-[10px] text-zinc-500 font-medium max-w-xs truncate">{t.message}</p>
                      </td>
                      <td className="px-6 py-4">
                        <code className="text-xs font-mono text-[#ffb0cf] px-2 py-0.5 bg-[#ffb0cf]/5 rounded-sm">{t.agent_id}</code>
                      </td>
                      <td className="px-6 py-4">
                        <span className="text-xs text-zinc-400 font-mono">{t.session_id ? t.session_id.slice(0, 12) + '…' : '—'}</span>
                      </td>
                      <td className="px-6 py-4">
                        <span className="text-[10px] text-zinc-500">{t.hook_name}</span>
                      </td>
                      <td className="px-6 py-4">
                        <span className="text-xs text-zinc-400">{formatTs(t.detected_at)}</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>

          {/* Live Activity Panel */}
          <section className="bg-[#2c2834] p-8 rounded-sm relative overflow-hidden">
            <div className="absolute top-0 right-0 p-4 opacity-5">
              <span className="material-symbols-outlined text-[120px]">shield_with_heart</span>
            </div>
            <h3 className="text-sm font-semibold text-zinc-400 uppercase tracking-widest mb-6 flex items-center gap-3">
              <span className="w-2 h-2 rounded-full bg-[#ffb0cf] shadow-[0_0_10px_#ffb0cf] animate-pulse" />
              Live Agent Activity
            </h3>
            <div className="space-y-3">
              {liveActivity.length === 0 ? (
                <p className="text-xs text-zinc-600">Waiting for agent activity…</p>
              ) : liveActivity.map((evt, i) => (
                <div key={i} className="flex items-start gap-4">
                  <span className="text-[10px] font-mono text-[#ffb0cf] py-0.5 whitespace-nowrap flex-shrink-0">
                    {formatEvtTime(evt.event_time)}
                  </span>
                  <p className="text-xs text-zinc-300">
                    <span className="font-semibold text-white">{evt.agent_id}</span>
                    {' '}{formatActivity(evt)}
                    {evt.hook_decision && evt.hook_decision !== 'allow' && (
                      <span className={`ml-2 text-[10px] px-1.5 py-0.5 rounded-full font-semibold uppercase ${
                        evt.hook_decision === 'block' ? 'bg-red-500/20 text-red-400' : 'bg-amber-500/20 text-amber-400'
                      }`}>{evt.hook_decision}</span>
                    )}
                  </p>
                </div>
              ))}
            </div>
          </section>

        </div>
      </main>
    </div>
  )
```

- [ ] **Step 2: Add helper functions above the component**

Add these three helpers between the SQL constants and the `export default function Threats()` line:

```jsx
function formatTs(ts) {
  if (!ts) return '—'
  try { return new Date(ts.replace(' ', 'T')).toLocaleString() } catch { return ts }
}

function formatEvtTime(ts) {
  if (!ts) return ''
  try { return new Date(ts.replace(' ', 'T')).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) } catch { return ts }
}

function formatActivity(evt) {
  if (evt.hook_name === 'llm_input')  return `called LLM${evt.model ? ` [${evt.model}]` : ''}`
  if (evt.hook_name === 'llm_output') return `received LLM response${evt.model ? ` [${evt.model}]` : ''}`
  if (evt.hook_name === 'before_tool_call') return `invoked tool ${evt.tool_name || '(unknown)'}`
  if (evt.hook_name === 'after_tool_call')  return `completed tool ${evt.tool_name || '(unknown)'}`
  return evt.hook_name
}
```

- [ ] **Step 3: Open browser at `/threats`, confirm the page renders without errors**

Expected: LIVE indicator appears immediately (set on stream start). Historical backfill rows appear quickly in both table and activity panel. New live events continue to prepend as they arrive.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/Threats.jsx
git commit -m "feat(Threats): live streaming threats table and hook activity panel

- Loads 50 historical security events then switches to live tail
- Streams hook events (llm/tool calls) in real time in activity panel
- Client-side severity and category filtering
- LIVE indicator when streaming connected
- Removed Threat Intelligence panel and static mock data"
```

---

## Verification Checklist

- [ ] Navigate to `/threats` — no JS errors in browser console
- [ ] Table header row renders with 6 columns: Severity / Rule / Message / Agent / Session / Hook / Time
- [ ] Filter buttons (Critical / Warning / Info) toggle and narrow the table
- [ ] Category dropdown filters by rule_id mapping
- [ ] LIVE pulsing dot appears after the historical query completes
- [ ] Navigate away from `/threats` and back — no duplicate streams (check Network tab: old SSE/fetch connections closed)
- [ ] If Proton has data in `agentshield_security_events`: rows appear in table
- [ ] If Proton has data in `agentshield_hook_events`: rows appear in Live Activity panel
- [ ] Threat Intelligence panel is gone entirely
- [ ] Import Manifest and Block Pattern buttons are gone (done in prior commit)
