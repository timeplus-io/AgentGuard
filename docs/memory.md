# AgentGuard — Memory Monitoring

AgentGuard tracks what AI agents read from and write to persistent memory — across all memory mechanisms in use. This document explains how that data is collected, processed, and surfaced.

---

## Why Monitor Memory?

Persistent memory is a high-value attack surface. An agent that reads from and writes to memory across sessions can be exploited in several ways:

| Threat | Description |
|--------|-------------|
| **Memory poisoning** | Attacker plants malicious content in the agent's memory store, influencing future behavior |
| **Exfiltration via recall** | Agent is prompted to retrieve and leak sensitive memories to an external target |
| **Accumulation attacks** | Across many sessions, an agent gradually writes a corpus of sensitive data to memory |

Memory monitoring gives operators visibility into exactly what is being stored and retrieved — in real time and historically.

---

## Memory Sources

Three distinct memory mechanisms are tracked, covering both Claude Code and OpenClaw agents:

| Source | Agent | Mechanism |
|--------|-------|-----------|
| **auto_memory** | Claude Code | Built-in auto-memory: Claude uses its standard `Write`/`Edit`/`Read` tools on `~/.claude/projects/<repo>/memory/*.md` |
| **mcp** | Claude Code | Any MCP memory plugin: tools appear as `mcp__<server>__<tool>` (e.g. `mcp__plugin_claude-mem__save_memory`) |
| **openclaw_file** | OpenClaw | File-based memory workspace: agent reads/writes Markdown files under `.openclaw/workspace/memory/` |

No plugin changes are required — all three signals are already present in `agentguard_hook_events`.

---

## Operation Types

All memory operations are normalized into a standard set of types:

| `memory_op` | Triggered by |
|-------------|-------------|
| `save` | Write to memory — `Write` tool (auto_memory), MCP tool matching `save\|store\|remember\|upsert\|embed\|write`, or `write` tool (OpenClaw) |
| `read` | Read from memory — `Read` tool (auto_memory), MCP tool matching `read\|load\|get\|fetch\|observation`, or `read` tool (OpenClaw) |
| `update` | In-place edit — `Edit` tool (auto_memory), MCP tool matching `update\|edit\|patch` |
| `search` | Query/recall — MCP tool matching `search\|query\|recall\|retrieve\|find` |
| `delete` | Remove from memory — MCP tool matching `delete\|forget\|remove` |
| `other` | Any operation that doesn't match the above patterns |

---

## Data Pipeline

```
agentguard_hook_events
        │
        ├─ before_tool_call / tool_name IN ('Write','Edit','Read')
        │   file_path contains /.claude/projects/ + /memory/   (claudecode)
        │       └─► mv_memory_claudecode_automem ──────────────┐
        │                                                      │
        ├─ before_tool_call / tool_name matches mcp__*         │
        │   + tool name matches memory keywords        (claudecode)
        │       └─► mv_memory_claudecode_mcp ──────────────────┤
        │                                                      │
        └─ before_tool_call / tool_name IN ('write','read')    │
            path matches .openclaw/workspace/memory/   (openclaw)
                └─► mv_memory_openclaw_file ───────────────────┘
                                                               │
                                                               ▼
                                                   agentguard_memory_ops
                                                               │
                                         ┌─────────────────────┴──────────────────┐
                                         ▼                                         ▼
                             AgentDetail → Memory tab               AgentDetail → Memory panel
                             (session-scoped)                       (agent-scoped, no session)
                             queried via /api/proton/query          queried via /api/proton/query
```

All three materialized views use `WHERE _tp_time > earliest_ts()` so they backfill all historical hook events on creation, not just new ones.

---

## Output: `agentguard_memory_ops` Stream

```sql
CREATE STREAM agentguard_memory_ops
(
  event_time      datetime64(3),
  session_id      string,
  agent_id        string,
  agent_type      low_cardinality(string),           -- 'claudecode' | 'openclaw'
  deployment_id   low_cardinality(string),
  memory_source   low_cardinality(string),           -- 'auto_memory' | 'mcp' | 'openclaw_file'
  memory_op       low_cardinality(string),           -- 'save' | 'search' | 'read' | 'delete' | 'update' | 'other'
  tool_name       string,                            -- full tool name as emitted in hook event
  server_name     low_cardinality(string),           -- MCP server name (e.g. 'plugin_claude-mem_mcp-search'); empty for file ops
  memory_path     string,                            -- file path for auto_memory / openclaw_file; empty for mcp
  content_payload string                             -- raw tool_input JSON; shown on demand in UI
)
```

Each row is one memory operation. `content_payload` stores the full `tool_input` JSON — the actual content being saved or the query being searched — and is hidden by default in the UI (expand-to-reveal).

---

## Materialized Views

### Claude Code Auto-Memory (`mv_memory_claudecode_automem`)

Claude Code's built-in memory writes Markdown files using standard file tools. Detection is by path: both `/.claude/projects/` and `/memory/` must appear in the file path. Operation type is derived from the tool name directly — no keyword matching needed.

```sql
SELECT
  event_time, session_id, agent_id, agent_type, deployment_id,
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
  json_query(event_data, '$.tool_input') AS content_payload
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND agent_type = 'claudecode'
  AND tool_name IN ('Write', 'Edit', 'Read')
  AND position(json_value(event_data, '$.tool_input.file_path'), '/.claude/projects/') > 0
  AND position(json_value(event_data, '$.tool_input.file_path'), '/memory/') > 0
```

### Claude Code MCP Memory Plugins (`mv_memory_claudecode_mcp`)

Any MCP memory server is detected generically — not just a specific plugin like `claude-mem`. The filter `match(lower(tool_name), 'memory|mem|recall|remember|store|knowledge|forget|embedding')` catches any MCP tool whose name suggests memory semantics. The server name is extracted from the `mcp__<server>__<tool>` naming convention using `split_by_string`.

```sql
SELECT
  event_time, session_id, agent_id, agent_type, deployment_id,
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
  json_query(event_data, '$.tool_input') AS content_payload
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND agent_type = 'claudecode'
  AND match(tool_name, '^mcp__')
  AND match(lower(tool_name), 'memory|mem|recall|remember|store|knowledge|forget|embedding')
```

### OpenClaw File Memory (`mv_memory_openclaw_file`)

OpenClaw stores memory as timestamped Markdown files under `.openclaw/workspace/memory/`. Standard `read`/`write` tool calls to this path are captured.

```sql
SELECT
  event_time, session_id, agent_id, agent_type, deployment_id,
  'openclaw_file' AS memory_source,
  CASE
    WHEN tool_name = 'write' THEN 'save'
    WHEN tool_name = 'read'  THEN 'read'
    ELSE 'other'
  END AS memory_op,
  tool_name,
  '' AS server_name,
  json_value(event_data, '$.params.path') AS memory_path,
  json_query(event_data, '$.params') AS content_payload
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND agent_type = 'openclaw'
  AND tool_name IN ('write', 'read')
  AND match(json_value(event_data, '$.params.path'), '\.openclaw/workspace/memory/')
```

---

## Frontend: Memory Tab (Session-Scoped)

The **Memory** tab in the Agent Detail session view streams from `agentguard_memory_ops` directly via `/api/proton/query` — no dedicated REST endpoint needed.

```sql
SELECT event_time, memory_op, memory_source, tool_name, server_name, memory_path, content_payload
FROM agentguard_memory_ops
WHERE session_id = '<session_id>'
ORDER BY event_time ASC
SETTINGS seek_to = 'earliest'
```

Each row shows:
- **Time** — when the operation occurred
- **Op badge** — colored by type: `save` (amber), `search` (blue), `read` (green), `delete` (red), `update` (purple), `other` (zinc)
- **Source** — `auto_memory`, `mcp`, or `openclaw_file`
- **Label** — MCP server name, filename (for file ops), or raw tool name
- **Expand** — click any row to reveal the full file path and `content_payload` JSON

---

## Frontend: Memory Panel (Agent-Scoped)

When no session is selected in Agent Detail, the right panel shows a cross-session **Memory Footprint** view for the current agent.

**Summary stats** (queried once as a historical snapshot):

```sql
SELECT
  count()                          AS total_ops,
  count_if(memory_op = 'save')    AS saves,
  count_if(memory_op = 'search')  AS searches,
  count_if(memory_op = 'read')    AS reads
FROM table(agentguard_memory_ops)
WHERE agent_id = '<agent_id>'
```

**Recent ops** — last 50 memory operations across all sessions, same columns as the session tab, expandable rows:

```sql
SELECT event_time, session_id, memory_op, memory_source, tool_name, server_name, memory_path, content_payload
FROM table(agentguard_memory_ops)
WHERE agent_id = '<agent_id>'
ORDER BY event_time DESC
LIMIT 50
```

---

## Known Limitations

| Item | Note |
|------|------|
| MCP keyword false positives | An MCP tool whose name contains a filter keyword but is unrelated to memory (e.g. `mcp__postgres__store_result` matching `store`) will be captured. Identifiable via `tool_name` in the UI. |
| Auto-memory path dependency | Detection relies on the `/.claude/projects/*/memory/` path convention. If Claude Code changes its memory layout in a future version, the MV will need updating. |
| MCP server name with `__` | `split_by_string('__', tool_name)[2]` assumes the three-segment `mcp__<server>__<tool>` format. A server name containing `__` will produce an incorrect `server_name` value (low probability given Claude Code conventions). |
| OpenClaw memory not yet observed | The OpenClaw memory MV is syntactically validated but has not been tested against a live session that writes to its memory directory. Verify after the first such session. |
| Content payload size | `content_payload` stores the full `tool_input` JSON. Large memory saves may be multi-KB per row. Monitor `agentguard_memory_ops` storage growth over time. |
