# Skills Metadata Enrichment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enrich `agentguard_skills` with 5 new metadata columns (`skill_namespace`, `skill_source_type`, `skill_location`, `skill_description`, `skill_args`) and update `SkillsTab` to display them.

**Architecture:** The existing `agentguard_skills` stream gains 5 columns; all 4 source MVs are updated to populate them. Because Timeplus does not support `ALTER STREAM ADD COLUMN`, a one-time migration function drops old MVs and the stream on first startup, allowing the normal create path to recreate them with the new schema and backfill from `agentguard_hook_events` via `earliest_ts()`. The frontend `SkillsTab` component gains a source-type badge and expand-to-reveal rows for location, description, and args.

**Tech Stack:** Go 1.23 (backend `init.go`), React 18 + Tailwind CSS (frontend `AgentDetail.jsx`), Timeplus Enterprise (streaming SQL, ClickHouse-compatible functions including `arrayZip`, `arrayResize`, `split_by_string`, `extract_all`).

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/timeplus/init.go` | Add `migrateSkillsStreamIfNeeded()`. Update `ddlSkillsStream` (5 new columns). Update 4 MV DDL constants. Wire migration into `Init()`. |
| `frontend/src/pages/AgentDetail.jsx` | Update `SkillsTab` SQL + state shape. Add `SOURCE_TYPE_COLORS`. Add source badge. Add expand-to-reveal rows. |

No changes to `models.go`, `main.go`, `api/client.js`, `Sidebar.jsx`, or any handler.

---

## Task 1: Validate SQL functions in live Timeplus

Before writing any Go code, confirm that `arrayZip`, `arrayResize`, and `split_by_string` work as expected in the running Timeplus instance. This avoids discovering a function gap after the Go code is committed.

**Files:** none (validation only)

- [ ] **Step 1: Run validation queries**

POST these queries to `http://localhost:3218/` with credentials `proton:timeplus@t+` (Content-Type: text/plain):

```sql
-- Test arrayZip
SELECT arrayZip(['a','b'], ['x','y']) FORMAT JSON
```
Expected: `[["a","x"],["b","y"]]` or tuple representation — no error.

```sql
-- Test arrayResize with padding
SELECT arrayResize(['x'], 3, '') FORMAT JSON
```
Expected: `["x","",""]` — no error.

```sql
-- Test split_by_string 1-based indexing
SELECT split_by_string(':', 'superpowers:brainstorming')[1] FORMAT JSON
```
Expected: `"superpowers"`.

```sql
-- Test extract_all with location tag
SELECT extract_all('<location>~/.openclaw/skills/github/SKILL.md</location>', '<location>([^<]+)</location>') FORMAT JSON
```
Expected: `["~/.openclaw/skills/github/SKILL.md"]`.

- [ ] **Step 2: Confirm results match expectations**

If `arrayResize` fails or returns unexpected results, the `mv_skills_loaded_openclaw` MV must use the 2-element fallback (name + location only, no description padding):

```sql
-- Fallback arrayZip if arrayResize unavailable:
ARRAY JOIN arrayZip(
  extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>'),
  extract_all(event_data, '<location>([^<]+)</location>')
) AS skill_tuple
-- skill_tuple.1 = name, skill_tuple.2 = location
-- Use '' AS skill_description for all rows
```

Record whether `arrayResize` works — it determines which MV variant to use in Task 2.

---

## Task 2: Backend — stream migration and updated DDL

**Files:**
- Modify: `backend/internal/timeplus/init.go` (lines 107–112 for `Init()` wiring, lines 1030–1113 for DDL constants, after line 1263 for new function)

- [ ] **Step 1: Update `ddlSkillsStream` constant**

In `init.go`, replace the existing `ddlSkillsStream` constant (lines 1030–1040) with:

```go
const ddlSkillsStream = `
CREATE STREAM IF NOT EXISTS agentguard_skills
(
  event_time        datetime64(3),
  agent_id          string,
  session_id        string,
  deployment_id     string,
  agent_type        low_cardinality(string),
  skill_name        string,
  skill_action      low_cardinality(string),
  skill_namespace   low_cardinality(string),
  skill_source_type low_cardinality(string),
  skill_location    string,
  skill_description string,
  skill_args        string
)`
```

- [ ] **Step 2: Update `ddlMVSkillsLoadedOpenClaw` constant**

Replace the existing `ddlMVSkillsLoadedOpenClaw` constant (lines 1097–1113) with (use `arrayResize` variant if Task 1 confirmed it works; use fallback otherwise):

```go
const ddlMVSkillsLoadedOpenClaw = `
CREATE MATERIALIZED VIEW mv_skills_loaded_openclaw
INTO agentguard_skills AS
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  trim(skill_tuple.1)    AS skill_name,
  'loaded'               AS skill_action,
  if(position(trim(skill_tuple.1), ':') > 0,
     split_by_string(':', trim(skill_tuple.1))[1], '') AS skill_namespace,
  CASE
    WHEN position(skill_tuple.3, '~/.openclaw/')        = 1 THEN 'managed'
    WHEN position(skill_tuple.3, '~/.config/openclaw/') = 1 THEN 'managed'
    WHEN position(skill_tuple.3, '~/.agents/')          = 1 THEN 'personal'
    WHEN position(skill_tuple.3, 'node_modules')        > 0 THEN 'bundled'
    WHEN length(skill_tuple.3)                          > 0 THEN 'workspace'
    ELSE 'unknown'
  END                    AS skill_source_type,
  trim(skill_tuple.3)    AS skill_location,
  trim(skill_tuple.2)    AS skill_description,
  ''                     AS skill_args
FROM agentguard_hook_events
ARRAY JOIN arrayZip(
  extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>'),
  arrayResize(
    extract_all(event_data, '<description>([^<]+)</description>'),
    length(extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>')),
    ''
  ),
  extract_all(event_data, '<location>([^<]+)</location>')
) AS skill_tuple
WHERE _tp_time > earliest_ts()
  AND hook_name = 'llm_input'
  AND agent_type = 'openclaw'
  AND length(trim(skill_tuple.1)) > 0`
```

- [ ] **Step 3: Update `ddlMVSkillsInvokedOpenClaw` constant**

Replace the existing `ddlMVSkillsInvokedOpenClaw` constant (lines 1060–1077) with:

```go
const ddlMVSkillsInvokedOpenClaw = `
CREATE MATERIALIZED VIEW mv_skills_invoked_openclaw
INTO agentguard_skills AS
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  extract(json_value(event_data, '$.params.path'),
          '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') AS skill_name,
  'invoked' AS skill_action,
  ''         AS skill_namespace,
  CASE
    WHEN position(json_value(event_data, '$.params.path'), '~/.openclaw/')        = 1 THEN 'managed'
    WHEN position(json_value(event_data, '$.params.path'), '~/.config/openclaw/') = 1 THEN 'managed'
    WHEN position(json_value(event_data, '$.params.path'), '~/.agents/')          = 1 THEN 'personal'
    WHEN position(json_value(event_data, '$.params.path'), 'node_modules')        > 0 THEN 'bundled'
    WHEN length(json_value(event_data, '$.params.path'))                          > 0 THEN 'workspace'
    ELSE 'unknown'
  END        AS skill_source_type,
  json_value(event_data, '$.params.path') AS skill_location,
  ''         AS skill_description,
  ''         AS skill_args
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND tool_name = 'read'
  AND agent_type = 'openclaw'
  AND position(event_data, 'SKILL.md') > 0
  AND extract(json_value(event_data, '$.params.path'),
              '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') != ''`
```

- [ ] **Step 4: Update `ddlMVSkillsLoadedClaudeCode` constant**

Replace the existing `ddlMVSkillsLoadedClaudeCode` constant (lines 1079–1095) with (note: the regex now uses `[a-zA-Z0-9:-]*` to capture `:` in namespaced skill names):

```go
const ddlMVSkillsLoadedClaudeCode = `
CREATE MATERIALIZED VIEW mv_skills_loaded_claudecode
INTO agentguard_skills AS
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  trim(sn) AS skill_name,
  'loaded'  AS skill_action,
  if(position(trim(sn), ':') > 0,
     split_by_string(':', trim(sn))[1], '') AS skill_namespace,
  if(position(trim(sn), ':') > 0, 'plugin', 'user') AS skill_source_type,
  '' AS skill_location,
  '' AS skill_description,
  '' AS skill_args
FROM agentguard_hook_events
ARRAY JOIN extract_all(event_data, '---..name: ([a-zA-Z][a-zA-Z0-9:-]*)') AS sn
WHERE _tp_time > earliest_ts()
  AND hook_name = 'prompt_context'
  AND agent_type = 'claudecode'
  AND length(trim(sn)) > 0`
```

- [ ] **Step 5: Update `ddlMVSkillsInvokedClaudeCode` constant**

Replace the existing `ddlMVSkillsInvokedClaudeCode` constant (lines 1042–1058) with:

```go
const ddlMVSkillsInvokedClaudeCode = `
CREATE MATERIALIZED VIEW mv_skills_invoked_claudecode
INTO agentguard_skills AS
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  json_value(event_data, '$.tool_input.skill') AS skill_name,
  'invoked' AS skill_action,
  if(position(json_value(event_data, '$.tool_input.skill'), ':') > 0,
     split_by_string(':', json_value(event_data, '$.tool_input.skill'))[1],
     '') AS skill_namespace,
  if(position(json_value(event_data, '$.tool_input.skill'), ':') > 0,
     'plugin', 'user') AS skill_source_type,
  ''  AS skill_location,
  ''  AS skill_description,
  json_value(event_data, '$.tool_input.args') AS skill_args
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND tool_name = 'Skill'
  AND agent_type = 'claudecode'
  AND json_value(event_data, '$.tool_input.skill') != ''`
```

- [ ] **Step 6: Add `migrateSkillsStreamIfNeeded` function**

Add this function after the existing `createSkillsMVs` function (after line 1263):

```go
// migrateSkillsStreamIfNeeded drops the old agentguard_skills stream and its
// four source MVs when the stream exists but lacks the skill_namespace column.
// The normal createSkillsStream / createSkillsMVs calls that follow will
// recreate them with the enriched schema and backfill via earliest_ts().
func migrateSkillsStreamIfNeeded(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	q := "SELECT count() FROM system.columns WHERE table = 'agentguard_skills' AND name = 'skill_namespace' AND database = current_database()"
	row := conn.QueryRow(ctx, q)
	var count uint64
	if err := row.Scan(&count); err != nil {
		// Stream may not exist yet — nothing to migrate.
		return nil
	}
	if count > 0 {
		return nil // Already on the new schema.
	}
	emit("running", "$ Migrating agentguard_skills to enriched schema…")
	for _, mv := range []string{
		"mv_skills_invoked_claudecode",
		"mv_skills_invoked_openclaw",
		"mv_skills_loaded_claudecode",
		"mv_skills_loaded_openclaw",
	} {
		if err := conn.Exec(ctx, fmt.Sprintf("DROP VIEW IF EXISTS %s", mv)); err != nil {
			emit("ok", "  ✓ dropped "+mv+" (or did not exist)")
		}
	}
	if err := conn.Exec(ctx, "DROP STREAM IF EXISTS agentguard_skills"); err != nil {
		return fmt.Errorf("dropping agentguard_skills for migration: %w", err)
	}
	emit("ok", "  ✓ Dropped old agentguard_skills — will recreate with enriched schema")
	return nil
}
```

- [ ] **Step 7: Wire migration into `Init()`**

In `Init()`, replace the existing two-line skills block (lines 107–112):

```go
	if err := createSkillsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating skills stream: %v", err)
	}
	if err := createSkillsMVs(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating skills MVs: %v", err)
	}
```

with:

```go
	if err := migrateSkillsStreamIfNeeded(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning migrating skills stream: %v", err)
	}
	if err := createSkillsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating skills stream: %v", err)
	}
	if err := createSkillsMVs(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating skills MVs: %v", err)
	}
```

- [ ] **Step 8: Compile check**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/backend
CGO_ENABLED=0 go build ./...
```

Expected: no errors, no output.

- [ ] **Step 9: Commit**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard
git add backend/internal/timeplus/init.go
git commit -m "feat(skills): enrich agentguard_skills with namespace, source type, location, description, args"
```

---

## Task 3: Backend — integration smoke test

Verify the migration and new schema work end-to-end against the running Timeplus instance. The server must be running at `:8080` and Timeplus at `:3218`.

**Files:** none (integration test only)

- [ ] **Step 1: Start the backend**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/backend
CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server
```

- [ ] **Step 2: Trigger provisioning**

In a second terminal:

```bash
curl -s -X POST http://localhost:8080/api/setup/provision | jq .
```

Watch the server log for:
```
[provision:ok]   ✓ Migrating agentguard_skills to enriched schema…   (if old stream existed)
[provision:ok]   ✓ agentguard_skills
[provision:ok]   ✓ mv_skills_invoked_claudecode
[provision:ok]   ✓ mv_skills_invoked_openclaw
[provision:ok]   ✓ mv_skills_loaded_claudecode
[provision:ok]   ✓ mv_skills_loaded_openclaw
```

- [ ] **Step 3: Verify new schema in Timeplus**

```bash
curl -s -u proton:timeplus@t+ \
  -d "SELECT name FROM system.columns WHERE table = 'agentguard_skills' AND database = current_database() ORDER BY position FORMAT JSON" \
  http://localhost:3218/
```

Expected: response includes `skill_namespace`, `skill_source_type`, `skill_location`, `skill_description`, `skill_args` as column names.

- [ ] **Step 4: Verify backfilled data**

```bash
curl -s -u proton:timeplus@t+ \
  -d "SELECT skill_name, skill_action, skill_namespace, skill_source_type, skill_location, skill_args FROM table(agentguard_skills) LIMIT 10 FORMAT JSON" \
  http://localhost:3218/
```

Expected: rows returned. For Claude Code invocation rows (`skill_action = 'invoked'`): `skill_namespace` is `'superpowers'` for skills like `superpowers:brainstorming`, or `''` for unnamespaced skills. For OpenClaw loaded rows: `skill_location` contains the SKILL.md path, `skill_source_type` reflects the path prefix.

---

## Task 4: Frontend — SkillsTab metadata display

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx` — `SkillsTab` component (lines 429–517)

- [ ] **Step 1: Add `SOURCE_TYPE_COLORS` constant**

Immediately before the `SkillsTab` function definition (before line 429), add:

```jsx
const SOURCE_TYPE_COLORS = {
  workspace: 'text-red-400',
  managed:   'text-emerald-400',
  personal:  'text-blue-400',
  bundled:   'text-zinc-400',
  plugin:    'text-purple-400',
  user:      'text-zinc-500',
  unknown:   'text-zinc-500',
}
```

- [ ] **Step 2: Update `SkillsTab` state and SQL query**

Replace the existing `SkillsTab` function body (the `useState` declarations and the `sql` constant) from line 429 to line 456 with:

```jsx
function SkillsTab({ sessionId }) {
  const [loaded, setLoaded] = useState([])
  const [invoked, setInvoked] = useState([])
  const [connecting, setConnecting] = useState(true)
  const [expanded, setExpanded] = useState({})

  useEffect(() => {
    if (!sessionId) return
    if (!/^[a-zA-Z0-9_-]{1,128}$/.test(sessionId)) return
    setLoaded([])
    setInvoked([])
    setConnecting(true)
    setExpanded({})
    const ctrl = new AbortController()
    const sql = `SELECT event_time, skill_name, skill_action, skill_namespace, skill_source_type, skill_location, skill_description, skill_args FROM agentguard_skills WHERE session_id = '${sessionId}' ORDER BY event_time ASC SETTINGS seek_to = 'earliest'`
    async function stream() {
      for await (const row of api.protonQuery(sql, { signal: ctrl.signal })) {
        if (ctrl.signal.aborted) break
        setConnecting(false)
        if (row.skill_action === 'loaded') {
          setLoaded(prev => prev.some(s => s.name === row.skill_name) ? prev : [...prev, {
            name: row.skill_name,
            firstSeen: row.event_time,
            namespace: row.skill_namespace || '',
            sourceType: row.skill_source_type || 'unknown',
            location: row.skill_location || '',
            description: row.skill_description || '',
          }])
        } else if (row.skill_action === 'invoked') {
          setInvoked(prev => [...prev, {
            time: row.event_time,
            name: row.skill_name,
            args: row.skill_args || '',
          }])
        }
      }
      setConnecting(false)
    }
    stream().catch(e => { if (e?.name !== 'AbortError') setConnecting(false) })
    return () => ctrl.abort()
  }, [sessionId])
```

- [ ] **Step 3: Update the Loaded section render**

Replace the loaded section render (the `{loaded.map(...)}` block, approximately lines 485–493) with:

```jsx
          <div className="space-y-2">
            {loaded.map(({ name, firstSeen, sourceType, location, description }) => (
              <div key={name}>
                <div
                  className="flex items-center gap-2 w-full cursor-pointer"
                  onClick={() => setExpanded(prev => ({ ...prev, [name]: !prev[name] }))}
                >
                  <span className="material-symbols-outlined text-[14px] text-[#ffb0cf]" style={{ fontVariationSettings: "'FILL' 0" }}>hexagon</span>
                  <span className="text-xs font-mono text-[#e7dff0]">{name}</span>
                  {sourceType && (
                    <span className={`text-[10px] font-mono px-1.5 py-0.5 rounded-full border border-current ${SOURCE_TYPE_COLORS[sourceType] || 'text-zinc-500'}`}>
                      {sourceType}
                    </span>
                  )}
                  <span className="ml-auto text-[10px] font-mono text-zinc-500">first seen&nbsp;&nbsp;{fmtTime(firstSeen)}</span>
                </div>
                {expanded[name] && (location || description) && (
                  <div className="ml-6 mt-1 space-y-1">
                    {location && (
                      <p className="text-[10px] font-mono text-zinc-500 break-all">
                        <span className="text-zinc-600">location </span>{location}
                      </p>
                    )}
                    {description && (
                      <p className="text-[10px] font-mono text-zinc-400 break-words">
                        <span className="text-zinc-600">desc </span>{description}
                      </p>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
```

- [ ] **Step 4: Update the Invoked section render**

Replace the invoked section render (the `{invoked.map(...)}` block, approximately lines 505–513) with:

```jsx
          <div className="space-y-2">
            {invoked.map((item, i) => (
              <div key={i}>
                <div
                  className="flex items-center gap-3 cursor-pointer"
                  onClick={() => item.args ? setExpanded(prev => ({ ...prev, [`inv-${i}`]: !prev[`inv-${i}`] })) : undefined}
                >
                  <span className="material-symbols-outlined text-[14px] text-[#ffb2b8]">play_arrow</span>
                  <span className="text-[10px] font-mono text-zinc-500 w-16 shrink-0">{fmtTime(item.time)}</span>
                  <span className="text-xs font-mono text-[#e7dff0]">{item.name}</span>
                  {item.args && (
                    <span className="ml-auto text-[10px] font-mono text-zinc-600">args ▾</span>
                  )}
                </div>
                {expanded[`inv-${i}`] && item.args && (
                  <div className="ml-10 mt-1">
                    <p className="text-[10px] font-mono text-zinc-400 break-words">
                      <span className="text-zinc-600">args </span>{item.args}
                    </p>
                  </div>
                )}
              </div>
            ))}
          </div>
```

- [ ] **Step 5: ESLint check**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/frontend
npm run lint
```

Expected: no errors.

- [ ] **Step 6: Start dev servers and verify in browser**

```bash
# Terminal 1 — backend
cd /Users/gangtao/Code/timeplus/AgentGuard/backend
CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server

# Terminal 2 — frontend
cd /Users/gangtao/Code/timeplus/AgentGuard/frontend
npm run dev
```

Open `http://localhost:5173`, navigate to an agent that has skill activity, open the Skills tab. Verify:
- Each loaded skill card shows a source type badge (e.g. `plugin` in purple, `managed` in green)
- Clicking a loaded skill row reveals location and description (when non-empty)
- Clicking an invoked row with args reveals the args text

- [ ] **Step 7: Commit**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard
git add frontend/src/pages/AgentDetail.jsx
git commit -m "feat(skills): show source type badge and expandable metadata in SkillsTab"
```

---

## Self-Review Checklist (completed inline)

**Spec coverage:**
- ✅ Section 1 (stream schema, 5 new columns) → Task 2 Steps 1
- ✅ Section 2.1 (mv_skills_loaded_openclaw with arrayZip+arrayResize) → Task 2 Step 2
- ✅ Section 2.2 (mv_skills_invoked_openclaw with location+source_type) → Task 2 Step 3
- ✅ Section 2.3 (mv_skills_loaded_claudecode namespace regex fix) → Task 2 Step 4
- ✅ Section 2.4 (mv_skills_invoked_claudecode namespace+args) → Task 2 Step 5
- ✅ Section 3 (migration strategy) → Task 2 Steps 6–7
- ✅ Section 4 (frontend badge + expand-to-reveal) → Task 4 Steps 1–4
- ✅ Known limitation: arrayResize validation → Task 1 Step 2 (fallback documented)

**Placeholder scan:** No TBDs. All code blocks are complete and runnable.

**Type consistency:** `SOURCE_TYPE_COLORS` defined in Step 1 is referenced in Step 3. `expanded` state defined in Step 2 is used in Steps 3 and 4. `skill_tuple.1/.2/.3` in MV SQL matches the `arrayZip` argument order (name, description, location).
