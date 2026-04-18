# Memory Monitoring — Design Spec

> **For agentic workers:** This spec is ready for implementation planning. Invoke `superpowers:writing-plans` to produce the implementation plan.

## Goal

Add memory monitoring to AgentGuard: track what AI coding agents read from and write to persistent memory — covering Claude Code's built-in auto-memory, any MCP memory plugin, and OpenClaw's file-based memory workspace. Surface this data as a session-scoped **Memory tab** in Agent Detail and an agent-level **Memory panel** visible across all sessions.

---

## Background

AI coding agents increasingly use persistent memory to carry context across sessions. This creates security-relevant risks:

- **Memory poisoning**: an attacker plants malicious content in an agent's memory store, influencing future behavior
- **Exfiltration via recall**: an agent is prompted to retrieve and leak sensitive memories
- **Accumulation attacks**: across many sessions, an agent gradually writes a corpus of sensitive data to memory

Three distinct memory mechanisms exist in the current agent ecosystem:

| Mechanism | Agent | How it works |
|-----------|-------|--------------|
| **Auto-memory** | Claude Code | Claude uses its standard `Write`/`Edit`/`Read` tools on `~/.claude/projects/<repo>/memory/*.md` |
| **MCP memory plugins** | Claude Code | Any MCP server providing memory-like tools, called as `mcp__<server>__<tool>` |
| **OpenClaw file memory** | OpenClaw | Agent reads/writes Markdown files under `.openclaw/workspace/memory/` |

No plugin changes are required — all three signals are already present in `agentguard_hook_events`.

---

## Architecture

### Data Flow

```
agentguard_hook_events
        │
        ├─ hook_name='before_tool_call', tool_name IN ('Write','Edit','Read')
        │   file_path matches /.claude/projects/*/memory/   (claudecode)
        │       └─► mv_memory_claudecode_automem ───────────┐
        │                                                   │
        ├─ hook_name='before_tool_call', tool_name matches mcp__*
        │   tool_name matches memory keywords               │
        │       └─► mv_memory_claudecode_mcp ───────────────┤
        │                                                   │
        └─ hook_name='before_tool_call', tool_name IN ('write','read')
            path matches .openclaw/workspace/memory/        │
                └─► mv_memory_openclaw_file ────────────────┘
                                                            │
                                                            ▼
                                                agentguard_memory_ops
                                                            │
                                          ┌─────────────────┴──────────────────┐
                                          ▼                                     ▼
                              Agent Detail → Memory tab            Agent Detail → Memory panel
                              (session-scoped)                     (agent-scoped, no session needed)
                              queried via protonQuery              queried via protonQuery
```

No new REST endpoint. `agentguard_memory_ops` is queried directly by the frontend via `/api/proton/query`.

---

## Section 1: Stream — `agentguard_memory_ops`

```sql
CREATE STREAM agentguard_memory_ops
(
  event_time      datetime64(3),
  session_id      string,
  agent_id        string,
  agent_type      low_cardinality(string),   -- 'claudecode' | 'openclaw'
  deployment_id   low_cardinality(string),
  memory_source   low_cardinality(string),   -- 'auto_memory' | 'mcp' | 'openclaw_file'
  memory_op       low_cardinality(string),   -- 'save' | 'search' | 'read' | 'delete' | 'update' | 'other'
  tool_name       string,                    -- full tool name as emitted in hook event
  server_name     string,                    -- MCP server name (split_by_string('__',tool_name)[2]); empty for file ops
  memory_path     string,                    -- file path for auto_memory / openclaw_file; empty for mcp
  content_payload string                     -- raw tool_input JSON; shown on demand in UI
)
```

Each row is one memory operation. The `content_payload` column stores the full `tool_input` JSON so operators can inspect what content was being saved or queried, but it is hidden by default in the UI (expand-to-reveal).

---

## Section 2: Materialized Views

### 2.1 `mv_memory_claudecode_automem` — Claude Code auto-memory

Claude Code's built-in memory writes Markdown files using its standard file tools. There is no dedicated hook or tool type — detection is by path pattern.

**Path pattern**: `/.claude/projects/` AND `/memory/` both present in `file_path`.

**Verified**: Real data shows `Write` → `reference_timeplus_credentials.md`, `Read` and `Edit` → `MEMORY.md` at `~/.claude/projects/-Users-.../memory/`. 3 rows confirmed 2026-04-17.

**Operation classification**: tool name is unambiguous — `Write` → `save`, `Edit` → `update`, `Read` → `read`.

```sql
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
WHERE hook_name = 'before_tool_call'
  AND agent_type = 'claudecode'
  AND tool_name IN ('Write', 'Edit', 'Read')
  AND position(json_value(event_data, '$.tool_input.file_path'), '/.claude/projects/') > 0
  AND position(json_value(event_data, '$.tool_input.file_path'), '/memory/') > 0
  AND _tp_time > earliest_ts()
```

### 2.2 `mv_memory_claudecode_mcp` — Claude Code MCP memory plugins

Any MCP memory server (not just `claude-mem`) is detected by matching the `mcp__*` tool naming convention combined with memory-related keywords in the tool name. The server name is extracted via `split_by_string`.

**Tool naming**: `mcp__<server>__<tool>` — e.g. `mcp__plugin_claude-mem_mcp-search__save_memory`. `split_by_string('__', tool_name)[2]` reliably extracts the server name; `[3]` extracts the tool name.

**Keyword filter**: `match(lower(tool_name), 'memory|mem|recall|remember|store|knowledge|forget|embedding')` — applied to the full tool name to reduce false positives from non-memory MCP tools.

**Verified**: SQL syntax validated 2026-04-17 against live Timeplus (0 rows — no MCP memory events in current data, but query executes without error). `split_by_string` extraction confirmed correct with synthetic example.

```sql
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
WHERE hook_name = 'before_tool_call'
  AND agent_type = 'claudecode'
  AND match(tool_name, '^mcp__')
  AND match(lower(tool_name), 'memory|mem|recall|remember|store|knowledge|forget|embedding')
  AND _tp_time > earliest_ts()
```

### 2.3 `mv_memory_openclaw_file` — OpenClaw file-based memory

OpenClaw uses a dedicated memory directory at `.openclaw/workspace/memory/` containing timestamped Markdown files. Memory reads/writes are standard `read`/`write` tool calls; detection is by path pattern.

**Path pattern**: `match(path, '\\.openclaw/workspace/memory/')` — covers both container path (`/home/node/.openclaw/workspace/memory/`) and host-mounted paths.

**Verified**: Memory directory confirmed at `/Users/gangtao/Code/timeplus/AgentGuard/agents/openclaw/.openclaw/workspace/memory/` with files dated 2026-04-06 to 2026-04-12. SQL returns 0 rows in current data (no recent OpenClaw sessions accessed memory) but executes without error.

```sql
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
WHERE hook_name = 'before_tool_call'
  AND agent_type = 'openclaw'
  AND tool_name IN ('write', 'read')
  AND match(json_value(event_data, '$.params.path'), '\\.openclaw/workspace/memory/')
  AND _tp_time > earliest_ts()
```

---

## Section 3: Source Hook Events — Reference

| hook_name | agent_type | tool_name | detection signal | memory_source |
|-----------|-----------|-----------|-----------------|---------------|
| `before_tool_call` | `claudecode` | `Write`, `Edit`, `Read` | `file_path` contains `/.claude/projects/` + `/memory/` | `auto_memory` |
| `before_tool_call` | `claudecode` | `mcp__*` | tool name matches memory keywords | `mcp` |
| `before_tool_call` | `openclaw` | `write`, `read` | path matches `.openclaw/workspace/memory/` | `openclaw_file` |

---

## Section 4: UI — Memory Tab + Memory Panel

### 4.1 Session Memory Tab

A **Memory** tab is added to the session content panel in `AgentDetail.jsx`, alongside the existing Timeline / Events / Trace / Skills tabs:

```
[Timeline] [Events] [Trace] [Skills] [Memory]   ← new tab
```

**Component**: `MemoryTab({ sessionId })` — streams from `agentguard_memory_ops` via `api.protonQuery()`:

```sql
SELECT event_time, memory_op, memory_source, tool_name, server_name, memory_path, content_payload
FROM agentguard_memory_ops
WHERE session_id = '<session_id>'
ORDER BY event_time ASC
SETTINGS seek_to = 'earliest'
```

**Layout** — chronological event list:

```
┌─────────────────────────────────────────────────────────────────┐
│  MEMORY OPERATIONS                                               │
│                                                                 │
│  18:31:43  [save]    auto_memory   MEMORY.md                    │
│  18:31:47  [read]    auto_memory   MEMORY.md          ▼ expand  │
│             file_path: /Users/.../.claude/projects/.../MEMORY.md│
│             content: {"file_path": "..."}                       │
│  18:31:51  [update]  auto_memory   reference_timeplus...        │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**Op badge colors**:
- `save` → amber (`text-amber-400`)
- `search` → blue (`text-blue-400`)
- `read` → green (`text-emerald-400`)
- `delete` → red (`text-red-400`)
- `update` → purple (`text-purple-400`)
- `other` → zinc (`text-zinc-500`)

**Expand-to-reveal**: each row is clickable; expanded state shows `memory_path` (if set) and `content_payload` JSON in a `<pre>` block. Collapsed by default. Same click toggles close.

### 4.2 Agent-level Memory Panel

Shown in the **no-session-selected** state of the Agent Detail right panel (replacing the current empty placeholder). Provides a cross-session memory footprint view.

**Summary stats row** (4 count cards):

```sql
SELECT
  count() AS total_ops,
  count_if(memory_op = 'save')   AS saves,
  count_if(memory_op = 'search') AS searches,
  count_if(memory_op = 'read')   AS reads
FROM table(agentguard_memory_ops)
WHERE agent_id = '<agent_id>'
```

**Recent ops table** — last 50 memory ops across all sessions, same columns as the session tab (time, op badge, source, tool/path). Clickable to expand payload.

```sql
SELECT event_time, session_id, memory_op, memory_source, tool_name, server_name, memory_path, content_payload
FROM table(agentguard_memory_ops)
WHERE agent_id = '<agent_id>'
ORDER BY event_time DESC
LIMIT 50
```

---

## Section 5: Implementation Files

| File | Change |
|------|--------|
| `backend/internal/timeplus/init.go` | Add `ddlMemoryOpsStream`, `createMemoryOpsStream()`, 3 MV DDL constants (`ddlMemoryClaudeCodeAutoMemMV`, `ddlMemoryClaudeCodeMcpMV`, `ddlMemoryOpenClawFileMV`), `createMemoryMVs()`. Wire both into `Init()` after skills block. |
| `frontend/src/pages/AgentDetail.jsx` | Add `'memory'` tab entry to tab list. Add `MemoryTab` component. Replace no-session placeholder with agent-level memory panel. |

No new handler file. No changes to `models.go`, `main.go`, `api/client.js`, or `Sidebar.jsx`.

---

## Section 6: Verification Status

**All SQL patterns validated 2026-04-17 against live Timeplus (localhost:3218).**

| Pattern | Result |
|---------|--------|
| Auto-memory path filter (`position(...,'/.claude/projects/')` + `position(...,'/memory/')`) | 3 real rows — Write/Edit/Read on `MEMORY.md` and `reference_timeplus_credentials.md` ✅ |
| Auto-memory op classification (`Write→save`, `Edit→update`, `Read→read`) | Correct on all 3 rows ✅ |
| MCP keyword filter (`match(tool_name, 'memory\|mem\|...')`) | 0 rows (no MCP memory events yet), query runs without error ✅ |
| `split_by_string('__', tool_name)[2]` for server name | Extracts `plugin_claude-mem_mcp-search` from synthetic example ✅ |
| OpenClaw path filter (`match(path, '\\.openclaw/workspace/memory/')`) | 0 rows (no recent OpenClaw memory access), query runs without error ✅ |

---

## Section 7: Known Risks

| Item | Status | Note |
|------|--------|------|
| MCP keyword false positives | ⚠️ Known | An MCP tool whose name contains a filter keyword but is unrelated to memory (e.g., `mcp__postgres__store_result` matching `store`, `mcp__files__knowledge_base` matching `knowledge`) would be captured. Acceptable for a security monitor — false positives are visible and identifiable via `tool_name`. |
| Auto-memory false negatives | ⚠️ Known gap | If Claude Code stores memory outside `/.claude/projects/*/memory/` in a future version, those writes would be missed. Path pattern should be revisited when Claude Code releases memory system changes. |
| OpenClaw no recent data | ⚠️ Unverified | OpenClaw memory MV SQL is syntactically valid but has not been tested against a session that actually uses memory. Verify after first OpenClaw memory-writing session. |
| Content payload size | ⚠️ Monitor | `content_payload` stores the full `tool_input` JSON. For large memory saves, this could be multi-KB per row. Monitor `agentguard_memory_ops` storage growth. |
| MCP server name format | ⚠️ Assumption | `split_by_string('__', tool_name)[2]` assumes exactly 3 `__`-delimited parts. If a server name itself contains `__`, extraction will be incorrect. Low probability given Claude Code conventions. |
