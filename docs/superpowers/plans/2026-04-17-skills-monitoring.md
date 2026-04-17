# Skills Monitoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `agentguard_skills` stream + 4 materialized views to Timeplus, and a Skills tab in AgentDetail that shows loaded and invoked skills per session.

**Architecture:** No new REST endpoint — all data flows through the existing `/api/proton/query` streaming proxy. The backend adds DDL constants + init functions; the frontend adds a `SkillsTab` component inside `AgentDetail.jsx` that streams from `agentguard_skills` using `api.protonQuery()`. The Skills tab is session-scoped (visible when a session is selected, even without a run).

**Tech Stack:** Go (Timeplus DDL via `proton-go-driver`), React 18, Tailwind CSS, `api.protonQuery()` async generator

---

## File Map

| file | role |
|------|------|
| `backend/internal/timeplus/init.go` | Add 5 DDL constants + 2 helper functions + wire into `Init()` |
| `frontend/src/pages/AgentDetail.jsx` | Add Skills tab to tab bar; add `SkillsTab` component |

No other files change.

---

## Task 1: Backend — DDL constants for `agentguard_skills` stream

**Files:**
- Modify: `backend/internal/timeplus/init.go`

- [ ] **Step 1: Add the stream DDL constant**

Open `backend/internal/timeplus/init.go`. After the `ddlPricingStream` constant (around line 1016), add:

```go
const ddlSkillsStream = `
CREATE STREAM IF NOT EXISTS agentguard_skills
(
  event_time    datetime64(3),
  agent_id      string,
  session_id    string,
  deployment_id string,
  agent_type    low_cardinality(string),
  skill_name    string,
  skill_action  low_cardinality(string)
)`
```

- [ ] **Step 2: Add the 4 MV DDL constants**

Immediately after `ddlSkillsStream`, add:

```go
const ddlMVSkillsInvokedClaudeCode = `
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_skills_invoked_claudecode
INTO agentguard_skills AS
SELECT
  event_time,
  agent_id,
  session_id,
  deployment_id,
  agent_type,
  json_value(event_data, '$.tool_input.skill') AS skill_name,
  'invoked' AS skill_action
FROM agentguard_hook_events
WHERE hook_name = 'before_tool_call'
  AND tool_name = 'Skill'
  AND agent_type = 'claudecode'
  AND json_value(event_data, '$.tool_input.skill') != ''`

const ddlMVSkillsInvokedOpenClaw = `
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_skills_invoked_openclaw
INTO agentguard_skills AS
SELECT
  event_time,
  agent_id,
  session_id,
  deployment_id,
  agent_type,
  extract(json_value(event_data, '$.params.path'), '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') AS skill_name,
  'invoked' AS skill_action
FROM agentguard_hook_events
WHERE hook_name = 'before_tool_call'
  AND tool_name = 'read'
  AND agent_type = 'openclaw'
  AND position(event_data, 'SKILL.md') > 0
  AND extract(json_value(event_data, '$.params.path'), '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') != ''`

const ddlMVSkillsLoadedClaudeCode = `
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_skills_loaded_claudecode
INTO agentguard_skills AS
SELECT
  event_time,
  agent_id,
  session_id,
  deployment_id,
  agent_type,
  trim(sn) AS skill_name,
  'loaded' AS skill_action
FROM agentguard_hook_events
ARRAY JOIN extract_all(event_data, '---..name: ([a-zA-Z][a-zA-Z0-9-]*)') AS sn
WHERE hook_name = 'prompt_context'
  AND agent_type = 'claudecode'
  AND length(trim(sn)) > 0`

const ddlMVSkillsLoadedOpenClaw = `
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_skills_loaded_openclaw
INTO agentguard_skills AS
SELECT
  event_time,
  agent_id,
  session_id,
  deployment_id,
  agent_type,
  trim(sn) AS skill_name,
  'loaded' AS skill_action
FROM agentguard_hook_events
ARRAY JOIN extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>') AS sn
WHERE hook_name = 'llm_input'
  AND agent_type = 'openclaw'
  AND length(trim(sn)) > 0`
```

- [ ] **Step 3: Add the helper functions**

After `createThreatsMV()` (around line 1045), add:

```go
func createSkillsStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_skills", ddlSkillsStream, emit)
}

func createSkillsMVs(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	mvs := []struct {
		name string
		ddl  string
	}{
		{"mv_skills_invoked_claudecode", ddlMVSkillsInvokedClaudeCode},
		{"mv_skills_invoked_openclaw", ddlMVSkillsInvokedOpenClaw},
		{"mv_skills_loaded_claudecode", ddlMVSkillsLoadedClaudeCode},
		{"mv_skills_loaded_openclaw", ddlMVSkillsLoadedOpenClaw},
	}
	for _, mv := range mvs {
		if err := execIfNotExists(ctx, conn, "view", mv.name, mv.ddl, emit); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Wire into `Init()`**

Inside `Init()`, after the `createPricingStream` / `seedPricingData` block (around line 104), add two non-fatal calls (same pattern as threats/pricing):

```go
	if err := createSkillsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating skills stream: %v", err)
	}
	if err := createSkillsMVs(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating skills MVs: %v", err)
	}
```

- [ ] **Step 5: Compile check**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 6: Commit**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard
git add backend/internal/timeplus/init.go
git commit -m "feat(skills): add agentguard_skills stream and 4 materialized views"
```

---

## Task 2: Provision the stream and MVs in the running Timeplus instance

The `Init()` function runs during the setup wizard provision step. Since Timeplus is already running, trigger reprovisioning via the AgentGuard API.

- [ ] **Step 1: Rebuild the backend binary**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/backend
CGO_ENABLED=0 go build -o server cmd/server/main.go
```

Expected: `server` binary created, no errors.

- [ ] **Step 2: Restart the backend**

Stop any running backend (`pkill -f "./server"` or Ctrl-C), then:

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/backend
./server
```

- [ ] **Step 3: Trigger re-provision via the Sync endpoint**

The `POST /api/rules/sync` endpoint re-runs MV sync. For the new streams/MVs, use the setup provision endpoint or log in and re-provision. Alternatively, verify directly in Timeplus:

```bash
curl -s "http://localhost:3218/" -u "proton:timeplus@t+" \
  --data-binary "SELECT count() FROM system.tables WHERE name IN ('agentguard_skills','mv_skills_invoked_claudecode','mv_skills_invoked_openclaw','mv_skills_loaded_claudecode','mv_skills_loaded_openclaw') FORMAT JSON"
```

Expected: `{"data":[{"count()":5}],...}` — all 5 objects exist.

- [ ] **Step 4: Verify data is flowing**

Wait 30 seconds after starting the backend (so the Claude Code agent generates a `prompt_context` event), then:

```bash
curl -s "http://localhost:3218/" -u "proton:timeplus@t+" \
  --data-binary "SELECT skill_action, skill_name, agent_type FROM table(agentguard_skills) LIMIT 10 FORMAT JSON"
```

Expected: rows with `loaded` skill names for the running agent sessions.

---

## Task 3: Frontend — `SkillsTab` component in `AgentDetail.jsx`

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx`

The Skills tab is session-scoped: it shows whenever a session is selected, whether or not a run is selected. This requires moving the tab bar to be visible at the session level (not just the run level).

- [ ] **Step 1: Add the `SkillsTab` component**

Find the `RunWaterfall` component definition in `AgentDetail.jsx` (search for `function RunWaterfall`). Add the `SkillsTab` component immediately **before** it:

```jsx
function SkillsTab({ sessionId }) {
  const [loaded, setLoaded] = useState([])
  const [invoked, setInvoked] = useState([])
  const [connecting, setConnecting] = useState(true)

  useEffect(() => {
    if (!sessionId) return
    const ctrl = new AbortController()
    const sql = `SELECT event_time, skill_name, skill_action FROM agentguard_skills WHERE session_id = '${sessionId}' ORDER BY event_time ASC SETTINGS seek_to = 'earliest'`
    async function stream() {
      for await (const row of api.protonQuery(sql, { signal: ctrl.signal })) {
        setConnecting(false)
        if (row.skill_action === 'loaded') {
          setLoaded(prev => prev.some(s => s === row.skill_name) ? prev : [...prev, row.skill_name])
        } else if (row.skill_action === 'invoked') {
          setInvoked(prev => [...prev, { time: row.event_time, name: row.skill_name }])
        }
      }
    }
    stream().catch(e => { if (e?.name !== 'AbortError') setConnecting(false) })
    return () => ctrl.abort()
  }, [sessionId])

  function fmtTime(ts) {
    if (!ts) return '—'
    try {
      return new Date(typeof ts === 'string' ? ts.replace(' ', 'T') : ts)
        .toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    } catch { return String(ts) }
  }

  if (connecting) {
    return (
      <div className="flex items-center justify-center h-full">
        <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
      </div>
    )
  }

  return (
    <div className="p-5 space-y-6">
      {/* Loaded section */}
      <div>
        <p className="text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-3">
          Loaded — available in system prompt
        </p>
        {loaded.length === 0 ? (
          <p className="text-xs text-zinc-600">No loaded skills detected for this session.</p>
        ) : (
          <div className="space-y-2">
            {loaded.map(name => (
              <div key={name} className="flex items-center gap-2">
                <span className="material-symbols-outlined text-[14px] text-[#ffb0cf]" style={{ fontVariationSettings: "'FILL' 0" }}>hexagon</span>
                <span className="text-xs font-mono text-[#e7dff0]">{name}</span>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Invoked section */}
      <div>
        <p className="text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-3">
          Invoked — skill activations
        </p>
        {invoked.length === 0 ? (
          <p className="text-xs text-zinc-600">No skill invocations detected for this session.</p>
        ) : (
          <div className="space-y-2">
            {invoked.map((item, i) => (
              <div key={i} className="flex items-center gap-3">
                <span className="material-symbols-outlined text-[14px] text-[#ffb2b8]">play_arrow</span>
                <span className="text-[10px] font-mono text-zinc-500 w-16 shrink-0">{fmtTime(item.time)}</span>
                <span className="text-xs font-mono text-[#e7dff0]">{item.name}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Expand the tab bar to session level and add the Skills tab**

Find this block (around line 1068–1091):

```jsx
            {/* Tab bar — only when a run is selected */}
            {selectedRun && (
              <div role="tablist" className="flex border-b border-[#574149]/20 shrink-0">
                {[
                  { id: 'timeline', label: 'Timeline' },
                  { id: 'events',   label: 'Events' },
                  { id: 'trace',    label: 'Trace' },
                ].map(tab => (
```

Replace it with (tab bar now shows when a session is selected, not just a run):

```jsx
            {/* Tab bar — visible when a session is selected */}
            {selectedSession && (
              <div role="tablist" className="flex border-b border-[#574149]/20 shrink-0">
                {[
                  { id: 'timeline', label: 'Timeline' },
                  { id: 'events',   label: 'Events' },
                  { id: 'trace',    label: 'Trace' },
                  { id: 'skills',   label: 'Skills' },
                ].map(tab => (
```

- [ ] **Step 3: Add the Skills tab content in the content switch**

Find the content switch block (around line 1093). Currently it starts with:

```jsx
            <div className="flex-1 overflow-y-auto">
              {!selectedRun ? (
                selectedSession && sessionDetail ? (
                  <SessionSummary sessionDetail={sessionDetail} />
                ) : (
```

Replace the entire content switch with:

```jsx
            <div className="flex-1 overflow-y-auto">
              {!selectedSession ? (
                <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
                  <span className="material-symbols-outlined text-4xl">timeline</span>
                  <p className="text-sm">Select a session from the left to begin</p>
                </div>
              ) : activeTab === 'skills' ? (
                <SkillsTab sessionId={selectedSession} />
              ) : !selectedRun ? (
                sessionDetail ? (
                  <SessionSummary sessionDetail={sessionDetail} />
                ) : (
                  <div className="flex items-center justify-center h-full">
                    <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
                  </div>
                )
              ) : activeTab === 'trace' ? (
                loadingTrace ? (
                  <div className="flex items-center justify-center h-full">
                    <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
                  </div>
                ) : (
                  <RunWaterfall spans={traceData?.spans ?? []} />
                )
              ) : activeTab === 'events' ? (
                loadingEvents ? (
                  <div className="flex items-center justify-center h-full">
                    <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
                  </div>
                ) : events.length === 0 ? (
                  <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
                    <span className="material-symbols-outlined text-4xl">inbox</span>
                    <p className="text-sm">No events for this run</p>
                  </div>
                ) : (
                  <div className="p-4 space-y-1.5">
                    <p className="text-[10px] text-zinc-600 mb-2">{events.length} events</p>
                    {events.map((evt, i) => (
                      <RunEventCard key={`${evt.hook_name}-${i}`} event={evt} />
                    ))}
                  </div>
                )
              ) : loadingEvents ? (
                <div className="flex items-center justify-center h-full">
                  <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
                </div>
              ) : (
                // Timeline tab
                (() => {
                  if (!sessionDetail) {
                    return (
                      <div className="flex items-center justify-center h-full">
                        <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
                      </div>
                    )
                  }
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
                  const byRun = {}
                  for (const e of events) {
                    if (!e.run_id) continue
                    if (!byRun[e.run_id]) byRun[e.run_id] = []
                    byRun[e.run_id].push(e)
                  }
                  for (const rid of Object.keys(byRun)) {
                    byRun[rid].sort((a, b) => {
                      const ta = new Date(typeof a.event_time === 'string' ? a.event_time.replace(' ', 'T') : a.event_time)
                      const tb = new Date(typeof b.event_time === 'string' ? b.event_time.replace(' ', 'T') : b.event_time)
                      return ta - tb
                    })
                  }
                  return (
                    <div className="p-4">
                      {runs.map((run) => (
                        <RunTimeline
                          key={run.run_id}
                          runIndex={sessionDetail.runs.findIndex(r => r.run_id === run.run_id)}
                          run={run}
                          events={byRun[run.run_id] ?? []}
                        />
                      ))}
                    </div>
                  )
                })()
              )}
            </div>
```

- [ ] **Step 4: Fix the `SessionHeader` guard**

The `SessionHeader` should still only show when a run is selected. Verify the block at line 1063 is still gated on `selectedRun`:

```jsx
            {/* Session header — only when a run is selected */}
            {selectedRun && sessionDetail && (
              <SessionHeader sessionDetail={sessionDetail} />
            )}
```

This should be unchanged — leave it as-is.

- [ ] **Step 5: Check lint**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/frontend
npm run lint
```

Expected: 0 errors.

- [ ] **Step 6: Start the dev server and test manually**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard
make dev
```

Open `http://localhost:5173/agents`. Click an agent → click a session (without selecting a run). Verify:
- Tab bar shows `[Timeline] [Events] [Trace] [Skills]`
- Skills tab is active (or click it) and shows loaded/invoked skills
- Timeline/Events/Trace tabs still work correctly when a run is selected

- [ ] **Step 7: Commit**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard
git add frontend/src/pages/AgentDetail.jsx
git commit -m "feat(skills): add Skills tab to AgentDetail session view"
```

---

## Self-Review

**Spec coverage:**

| spec requirement | task |
|-----------------|------|
| `agentguard_skills` stream DDL | Task 1 Step 1 |
| `mv_skills_invoked_claudecode` | Task 1 Step 2 |
| `mv_skills_invoked_openclaw` | Task 1 Step 2 |
| `mv_skills_loaded_claudecode` | Task 1 Step 2 |
| `mv_skills_loaded_openclaw` | Task 1 Step 2 |
| Wire into `Init()` | Task 1 Step 4 |
| Provision in running Timeplus | Task 2 |
| Skills tab at session level | Task 3 Step 2 |
| Loaded section (deduplicated) | Task 3 Step 1 — `setLoaded` filters duplicates |
| Invoked section (chronological) | Task 3 Step 1 — `setInvoked` appends in order |
| Tab visible without run selected | Task 3 Step 2–3 — tab bar moved to `selectedSession` gate |

**Placeholder scan:** No TBDs, no "similar to above", all code blocks complete.

**Type consistency:** `sessionId` prop string → used in SQL template; `loaded` is `string[]`; `invoked` is `{time, name}[]` — consistent across SkillsTab.
