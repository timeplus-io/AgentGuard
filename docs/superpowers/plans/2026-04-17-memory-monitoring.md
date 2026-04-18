# Memory Monitoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `agentguard_memory_ops` stream + 3 materialized views to Timeplus, and a Memory tab (session-scoped) plus a Memory panel (agent-scoped) in AgentDetail that shows what memory operations the agent performed.

**Architecture:** No new REST endpoint — data flows through the existing `/api/proton/query` streaming proxy. Backend adds DDL constants + init functions in `init.go`. Frontend adds a `MemoryTab` component (session-scoped, streaming query) and an agent-level Memory panel in the no-session-selected state of `AgentDetail.jsx`. Three MVs cover Claude Code auto-memory (file path pattern), Claude Code MCP memory plugins (tool name keywords), and OpenClaw file memory (path pattern).

**Tech Stack:** Go (Timeplus DDL via `proton-go-driver`), React 18, Tailwind CSS, `api.protonQuery()` async generator, Timeplus SQL (`json_value`, `split_by_string`, `match`, `position`)

---

## File Map

| File | Role |
|------|------|
| `backend/internal/timeplus/init.go` | Add 4 DDL constants + 2 helper functions + wire into `Init()` |
| `frontend/src/pages/AgentDetail.jsx` | Add `MemoryTab` component; add Memory tab to tab bar; replace no-session placeholder with agent-level Memory panel |

No other files change.

---

## Task 1: Backend — DDL constants and helper functions

**Files:**
- Modify: `backend/internal/timeplus/init.go`

- [ ] **Step 1: Add the stream DDL constant**

Open `backend/internal/timeplus/init.go`. Find the line:
```go
type pricingSeed struct {
```
(currently around line 1109). Insert the following block **immediately before** that line:

```go
const ddlMemoryOpsStream = `
CREATE STREAM IF NOT EXISTS agentguard_memory_ops
(
  event_time      datetime64(3),
  session_id      string,
  agent_id        string,
  agent_type      low_cardinality(string),
  deployment_id   low_cardinality(string),
  memory_source   low_cardinality(string),
  memory_op       low_cardinality(string),
  tool_name       string,
  server_name     string,
  memory_path     string,
  content_payload string
)`

const ddlMVMemoryClaudeCodeAutoMem = `
CREATE MATERIALIZED VIEW mv_memory_claudecode_automem
INTO agentguard_memory_ops AS
SELECT
  event_time,
  session_id,
  agent_id,
  agent_type,
  deployment_id,
  'auto_memory' AS memory_source,
  CASE
    WHEN tool_name = 'Write' THEN 'save'
    WHEN tool_name = 'Edit'  THEN 'update'
    WHEN tool_name = 'Read'  THEN 'read'
    ELSE 'other'
  END AS memory_op,
  tool_name,
  '' AS server_name,
  json_value(event_data, '$.tool_input.file_path') AS memory_path,
  json_value(event_data, '$.tool_input') AS content_payload
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND agent_type = 'claudecode'
  AND tool_name IN ('Write', 'Edit', 'Read')
  AND position(json_value(event_data, '$.tool_input.file_path'), '/.claude/projects/') > 0
  AND position(json_value(event_data, '$.tool_input.file_path'), '/memory/') > 0`

const ddlMVMemoryClaudeCodeMCP = `
CREATE MATERIALIZED VIEW mv_memory_claudecode_mcp
INTO agentguard_memory_ops AS
SELECT
  event_time,
  session_id,
  agent_id,
  agent_type,
  deployment_id,
  'mcp' AS memory_source,
  CASE
    WHEN match(lower(tool_name), 'save|store|remember|upsert|embed|write') THEN 'save'
    WHEN match(lower(tool_name), 'search|query|recall|retrieve|find')      THEN 'search'
    WHEN match(lower(tool_name), 'read|load|get|fetch|observation')        THEN 'read'
    WHEN match(lower(tool_name), 'delete|forget|remove')                   THEN 'delete'
    WHEN match(lower(tool_name), 'update|edit|patch')                      THEN 'update'
    ELSE 'other'
  END AS memory_op,
  tool_name,
  split_by_string('__', tool_name)[2] AS server_name,
  '' AS memory_path,
  json_value(event_data, '$.tool_input') AS content_payload
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND agent_type = 'claudecode'
  AND match(tool_name, '^mcp__')
  AND match(lower(tool_name), 'memory|mem|recall|remember|store|knowledge|forget|embedding')`

const ddlMVMemoryOpenClawFile = `
CREATE MATERIALIZED VIEW mv_memory_openclaw_file
INTO agentguard_memory_ops AS
SELECT
  event_time,
  session_id,
  agent_id,
  agent_type,
  deployment_id,
  'openclaw_file' AS memory_source,
  CASE
    WHEN tool_name = 'write' THEN 'save'
    WHEN tool_name = 'read'  THEN 'read'
    ELSE 'other'
  END AS memory_op,
  tool_name,
  '' AS server_name,
  json_value(event_data, '$.params.path') AS memory_path,
  json_value(event_data, '$.params') AS content_payload
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND agent_type = 'openclaw'
  AND tool_name IN ('write', 'read')
  AND match(json_value(event_data, '$.params.path'), '\.openclaw/workspace/memory/')`
```

- [ ] **Step 2: Add the helper functions**

Find the `createSkillsMVs` function (currently ending around line 1158). Add the following two functions **immediately after** the closing brace of `createSkillsMVs`:

```go
func createMemoryOpsStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_memory_ops", ddlMemoryOpsStream, emit)
}

func createMemoryMVs(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	mvs := []struct {
		name string
		ddl  string
	}{
		{"mv_memory_claudecode_automem", ddlMVMemoryClaudeCodeAutoMem},
		{"mv_memory_claudecode_mcp", ddlMVMemoryClaudeCodeMCP},
		{"mv_memory_openclaw_file", ddlMVMemoryOpenClawFile},
	}
	for _, mv := range mvs {
		if err := execIfNotExists(ctx, conn, "view", mv.name, mv.ddl, emit); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 3: Wire into `Init()`**

Find this block in `Init()` (currently around lines 110–112):

```go
	if err := createSkillsMVs(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating skills MVs: %v", err)
	}
```

Add the two memory calls **immediately after** it:

```go
	if err := createMemoryOpsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating memory ops stream: %v", err)
	}
	if err := createMemoryMVs(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating memory MVs: %v", err)
	}
```

- [ ] **Step 4: Compile check**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 5: Commit**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard
git add backend/internal/timeplus/init.go
git commit -m "feat(memory): add agentguard_memory_ops stream and 3 materialized views"
```

---

## Task 2: Provision the stream and MVs in the running Timeplus instance

`Init()` runs at server startup, so restarting the backend provisions the new stream and MVs.

- [ ] **Step 1: Rebuild and restart the backend**

Stop any running backend (Ctrl-C or `pkill -f "./server"`), then:

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/backend
CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server
```

Expected: server starts, logs show `timeplus init:` lines including memory ops stream/MV creation.

- [ ] **Step 2: Verify objects exist in Timeplus**

```bash
curl -s "http://localhost:3218/" -u "proton:timeplus@t+" \
  --data-binary "SELECT name FROM system.tables WHERE name IN ('agentguard_memory_ops','mv_memory_claudecode_automem','mv_memory_claudecode_mcp','mv_memory_openclaw_file') ORDER BY name FORMAT JSON"
```

Expected: 4 rows, one per object name.

- [ ] **Step 3: Verify auto-memory data backfilled**

```bash
curl -s "http://localhost:3218/" -u "proton:timeplus@t+" \
  --data-binary "SELECT event_time, memory_op, memory_source, memory_path FROM table(agentguard_memory_ops) LIMIT 10 FORMAT JSON"
```

Expected: at least 3 rows (the Write/Edit/Read to `MEMORY.md` and `reference_timeplus_credentials.md` confirmed during design validation).

---

## Task 3: Frontend — `MemoryTab` component

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx`

- [ ] **Step 1: Add the `MemoryTab` component**

Find the `SkillsTab` function definition (currently starting around line 427 with `// ── SkillsTab`). Add the `MemoryTab` component **immediately after** the closing `}` of `SkillsTab` (currently around line 517):

```jsx
// ── MemoryTab ─────────────────────────────────────────────────────────────────

const OP_COLORS = {
  save:   'text-amber-400',
  search: 'text-blue-400',
  read:   'text-emerald-400',
  delete: 'text-red-400',
  update: 'text-purple-400',
  other:  'text-zinc-500',
}

function MemoryTab({ sessionId }) {
  const [ops, setOps] = useState([])
  const [connecting, setConnecting] = useState(true)
  const [expanded, setExpanded] = useState({})

  useEffect(() => {
    if (!sessionId) return
    if (!/^[a-zA-Z0-9_-]{1,128}$/.test(sessionId)) return
    setOps([])
    setConnecting(true)
    setExpanded({})
    const ctrl = new AbortController()
    const sql = `SELECT event_time, memory_op, memory_source, tool_name, server_name, memory_path, content_payload FROM agentguard_memory_ops WHERE session_id = '${sessionId}' ORDER BY event_time ASC SETTINGS seek_to = 'earliest'`
    async function stream() {
      for await (const row of api.protonQuery(sql, { signal: ctrl.signal })) {
        if (ctrl.signal.aborted) break
        setConnecting(false)
        setOps(prev => [...prev, row])
      }
      setConnecting(false)
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

  function displayLabel(op) {
    const server = op.server_name
    const path = op.memory_path
    if (server) return server
    if (path) return path.split('/').pop()
    return op.tool_name
  }

  if (connecting) {
    return (
      <div className="flex items-center justify-center h-full">
        <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
      </div>
    )
  }

  return (
    <div className="p-5 space-y-1">
      <p className="text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-3">
        Memory Operations
      </p>
      {ops.length === 0 ? (
        <p className="text-xs text-zinc-600">No memory operations detected for this session.</p>
      ) : (
        ops.map((op, i) => (
          <div key={i}>
            <div
              className="flex items-center gap-3 cursor-pointer hover:bg-[#2a1e24] rounded px-2 py-1 -mx-2"
              onClick={() => setExpanded(prev => ({ ...prev, [i]: !prev[i] }))}
            >
              <span className="text-[10px] font-mono text-zinc-500 w-16 shrink-0">{fmtTime(op.event_time)}</span>
              <span className={`text-[10px] font-bold uppercase w-14 shrink-0 ${OP_COLORS[op.memory_op] ?? 'text-zinc-500'}`}>
                {op.memory_op}
              </span>
              <span className="text-[10px] text-zinc-500 w-24 shrink-0">{op.memory_source}</span>
              <span className="text-xs font-mono text-[#e7dff0] truncate">{displayLabel(op)}</span>
              <span className="material-symbols-outlined text-[12px] text-zinc-600 ml-auto shrink-0">
                {expanded[i] ? 'expand_less' : 'expand_more'}
              </span>
            </div>
            {expanded[i] && (
              <div className="ml-2 mt-1 mb-2 pl-3 border-l border-[#574149]/40 space-y-1">
                {op.memory_path && (
                  <p className="text-[10px] font-mono text-zinc-400 break-all">{op.memory_path}</p>
                )}
                {op.content_payload && (
                  <pre className="text-[10px] font-mono text-zinc-400 bg-[#1a1218] rounded p-2 overflow-x-auto whitespace-pre-wrap break-all max-h-40">
                    {(() => { try { return JSON.stringify(JSON.parse(op.content_payload), null, 2) } catch { return op.content_payload } })()}
                  </pre>
                )}
              </div>
            )}
          </div>
        ))
      )}
    </div>
  )
}
```

- [ ] **Step 2: Add the Memory tab to the tab bar**

Find the tab array in the tab bar (currently around line 1163):

```jsx
                {[
                  { id: 'timeline', label: 'Timeline' },
                  { id: 'events',   label: 'Events' },
                  { id: 'trace',    label: 'Trace' },
                  { id: 'skills',   label: 'Skills' },
                ].map(tab => (
```

Replace with:

```jsx
                {[
                  { id: 'timeline', label: 'Timeline' },
                  { id: 'events',   label: 'Events' },
                  { id: 'trace',    label: 'Trace' },
                  { id: 'skills',   label: 'Skills' },
                  { id: 'memory',   label: 'Memory' },
                ].map(tab => (
```

- [ ] **Step 3: Add the Memory tab content branch**

Find this block in the content switch (currently around line 1193):

```jsx
              ) : activeTab === 'skills' ? (
                <SkillsTab sessionId={selectedSession} />
              ) : !selectedRun ? (
```

Replace with:

```jsx
              ) : activeTab === 'skills' ? (
                <SkillsTab sessionId={selectedSession} />
              ) : activeTab === 'memory' ? (
                <MemoryTab sessionId={selectedSession} />
              ) : !selectedRun ? (
```

- [ ] **Step 4: Lint check**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/frontend
npm run lint
```

Expected: 0 errors.

- [ ] **Step 5: Commit**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard
git add frontend/src/pages/AgentDetail.jsx
git commit -m "feat(memory): add MemoryTab component and Memory session tab"
```

---

## Task 4: Frontend — Agent-level Memory panel

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx`

The no-session-selected placeholder (currently lines 1188–1192) is replaced with a Memory panel showing aggregate stats and recent ops across all sessions for the current agent.

- [ ] **Step 1: Add `AgentMemoryPanel` component**

Find the `MemoryTab` component you just added. Add the `AgentMemoryPanel` component **immediately after** its closing `}`:

```jsx
function AgentMemoryPanel({ agentId }) {
  const [stats, setStats] = useState(null)
  const [recentOps, setRecentOps] = useState([])
  const [loading, setLoading] = useState(true)
  const [expanded, setExpanded] = useState({})

  useEffect(() => {
    if (!agentId) return
    setLoading(true)
    setStats(null)
    setRecentOps([])

    const safeId = agentId.replace(/'/g, '')

    const statsSQL = `SELECT count() AS total_ops, count_if(memory_op = 'save') AS saves, count_if(memory_op = 'search') AS searches, count_if(memory_op = 'read') AS reads FROM table(agentguard_memory_ops) WHERE agent_id = '${safeId}'`
    const recentSQL = `SELECT event_time, session_id, memory_op, memory_source, tool_name, server_name, memory_path, content_payload FROM table(agentguard_memory_ops) WHERE agent_id = '${safeId}' ORDER BY event_time DESC LIMIT 50`

    Promise.all([
      (async () => {
        for await (const row of api.protonQuery(statsSQL)) {
          setStats(row)
          break
        }
      })(),
      (async () => {
        const rows = []
        for await (const row of api.protonQuery(recentSQL)) {
          rows.push(row)
        }
        setRecentOps(rows)
      })(),
    ]).finally(() => setLoading(false))
  }, [agentId])

  function fmtTime(ts) {
    if (!ts) return '—'
    try {
      return new Date(typeof ts === 'string' ? ts.replace(' ', 'T') : ts)
        .toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    } catch { return String(ts) }
  }

  function displayLabel(op) {
    if (op.server_name) return op.server_name
    if (op.memory_path) return op.memory_path.split('/').pop()
    return op.tool_name
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <span className="material-symbols-outlined animate-spin text-zinc-600 text-3xl">refresh</span>
      </div>
    )
  }

  const totalOps = Number(stats?.total_ops ?? 0)

  return (
    <div className="p-5 space-y-6">
      <div>
        <p className="text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-3">Memory Footprint</p>
        {totalOps === 0 ? (
          <p className="text-xs text-zinc-600">No memory operations recorded for this agent.</p>
        ) : (
          <div className="grid grid-cols-4 gap-3 mb-6">
            {[
              { label: 'Total', value: stats?.total_ops ?? 0, color: 'text-[#e7dff0]' },
              { label: 'Saves',   value: stats?.saves   ?? 0, color: 'text-amber-400' },
              { label: 'Searches', value: stats?.searches ?? 0, color: 'text-blue-400' },
              { label: 'Reads',   value: stats?.reads   ?? 0, color: 'text-emerald-400' },
            ].map(({ label, value, color }) => (
              <div key={label} className="bg-[#1a1218] rounded p-3 text-center">
                <p className={`text-lg font-bold font-mono ${color}`}>{value}</p>
                <p className="text-[10px] text-zinc-500 uppercase tracking-wider mt-0.5">{label}</p>
              </div>
            ))}
          </div>
        )}
      </div>

      {recentOps.length > 0 && (
        <div>
          <p className="text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-3">Recent Operations</p>
          <div className="space-y-1">
            {recentOps.map((op, i) => (
              <div key={i}>
                <div
                  className="flex items-center gap-3 cursor-pointer hover:bg-[#2a1e24] rounded px-2 py-1 -mx-2"
                  onClick={() => setExpanded(prev => ({ ...prev, [i]: !prev[i] }))}
                >
                  <span className="text-[10px] font-mono text-zinc-500 w-16 shrink-0">{fmtTime(op.event_time)}</span>
                  <span className={`text-[10px] font-bold uppercase w-14 shrink-0 ${OP_COLORS[op.memory_op] ?? 'text-zinc-500'}`}>
                    {op.memory_op}
                  </span>
                  <span className="text-[10px] text-zinc-500 w-24 shrink-0">{op.memory_source}</span>
                  <span className="text-xs font-mono text-[#e7dff0] truncate">{displayLabel(op)}</span>
                  <span className="material-symbols-outlined text-[12px] text-zinc-600 ml-auto shrink-0">
                    {expanded[i] ? 'expand_less' : 'expand_more'}
                  </span>
                </div>
                {expanded[i] && (
                  <div className="ml-2 mt-1 mb-2 pl-3 border-l border-[#574149]/40 space-y-1">
                    {op.memory_path && (
                      <p className="text-[10px] font-mono text-zinc-400 break-all">{op.memory_path}</p>
                    )}
                    {op.content_payload && (
                      <pre className="text-[10px] font-mono text-zinc-400 bg-[#1a1218] rounded p-2 overflow-x-auto whitespace-pre-wrap break-all max-h-40">
                        {(() => { try { return JSON.stringify(JSON.parse(op.content_payload), null, 2) } catch { return op.content_payload } })()}
                      </pre>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Replace the no-session placeholder**

Find the no-session placeholder block (currently around lines 1188–1192):

```jsx
              {!selectedSession ? (
                <div className="flex flex-col items-center justify-center h-full text-zinc-600 gap-2">
                  <span className="material-symbols-outlined text-4xl">timeline</span>
                  <p className="text-sm">Select a session from the left to begin</p>
                </div>
              ) : activeTab === 'skills' ? (
```

Replace with:

```jsx
              {!selectedSession ? (
                <AgentMemoryPanel agentId={agent?.agent_id} />
              ) : activeTab === 'skills' ? (
```

- [ ] **Step 3: Lint check**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/frontend
npm run lint
```

Expected: 0 errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard
git add frontend/src/pages/AgentDetail.jsx
git commit -m "feat(memory): add agent-level Memory panel to AgentDetail"
```

---

## Task 5: End-to-end verification

- [ ] **Step 1: Start both servers**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard
make dev
```

Expected: backend on `:8080`, frontend on `:5173`.

- [ ] **Step 2: Verify agent-level Memory panel**

Open `http://localhost:5173/agents`. Click an agent. With **no session selected**, verify:
- Memory panel renders (not the old "Select a session" placeholder)
- Stats row shows Total / Saves / Searches / Reads counts
- Recent ops table shows chronological ops with op badge, source, and label
- Clicking a row expands to show `memory_path` and `content_payload`

- [ ] **Step 3: Verify session Memory tab**

Click a session in the left panel. Verify:
- Tab bar shows `[Timeline] [Events] [Trace] [Skills] [Memory]`
- Click **Memory** tab — spinner appears, then memory ops for that session load
- Each row shows time / op badge (colored) / source / label
- Clicking a row expands to show the full file path and tool_input JSON
- Sessions with no memory ops show the empty-state message

- [ ] **Step 4: Verify existing tabs still work**

With a run selected, verify Timeline, Events, Trace, and Skills tabs all still render correctly.

- [ ] **Step 5: Final commit**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard
git add -A
git status  # confirm nothing unexpected staged
git commit -m "feat(memory): memory monitoring complete — stream, MVs, session tab, agent panel"
```

---

## Self-Review

**Spec coverage:**

| Spec requirement | Task |
|-----------------|------|
| `agentguard_memory_ops` stream DDL | Task 1 Step 1 |
| `mv_memory_claudecode_automem` | Task 1 Step 1 |
| `mv_memory_claudecode_mcp` | Task 1 Step 1 |
| `mv_memory_openclaw_file` | Task 1 Step 1 |
| `createMemoryOpsStream()` + `createMemoryMVs()` helpers | Task 1 Step 2 |
| Wire into `Init()` | Task 1 Step 3 |
| Provision in running Timeplus + verify backfill | Task 2 |
| `MemoryTab` component (session-scoped, streaming) | Task 3 Step 1 |
| Memory tab in tab bar | Task 3 Step 2 |
| Op badge colors (save=amber, search=blue, read=green, delete=red, update=purple) | Task 3 Step 1 — `OP_COLORS` map |
| Expand-to-reveal content_payload | Task 3 Step 1 — `expanded` state + `onClick` |
| `AgentMemoryPanel` (agent-scoped, 4 stat cards + recent ops) | Task 4 Step 1 |
| Replace no-session placeholder with Memory panel | Task 4 Step 2 |
| `earliest_ts()` backfill in all MVs | Task 1 Step 1 — all three MV DDLs include `_tp_time > earliest_ts()` |

**Placeholder scan:** No TBDs, no "similar to above", all code blocks complete.

**Type consistency:**
- `OP_COLORS` defined once before `MemoryTab`, referenced in both `MemoryTab` and `AgentMemoryPanel` — consistent.
- `displayLabel(op)` defined separately in each component (not shared) — no cross-reference issue.
- `agentId` prop in `AgentMemoryPanel` receives `agent?.agent_id` (string | undefined) — the `safeId` guard handles undefined by skipping the effect when `agentId` is falsy.
- Stream query uses `agentguard_memory_ops` (streaming, with `seek_to = 'earliest'`); panel uses `table(agentguard_memory_ops)` (historical snapshot) — correct distinction per Timeplus semantics.
