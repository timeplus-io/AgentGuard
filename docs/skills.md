# AgentGuard — Skills Monitoring

AgentGuard tracks which **skills** are available to an AI agent during a session and which are actively invoked. Each event is enriched with metadata about the skill's namespace, provenance, description, and invocation arguments — giving future threat-detection rules structured signals.

---

## What Are Skills?

Skills are modular packages of specialist knowledge and workflow instructions that extend an agent's capabilities (e.g., `test-driven-development`, `superpowers:brainstorming`). They are defined as Markdown files with YAML frontmatter and installed into the agent's environment.

Two distinct events are tracked:

| Event | Meaning |
|-------|---------|
| **loaded** | The skill is present in the agent's system prompt and available to call |
| **invoked** | The agent explicitly activated the skill |

---

## Data Pipeline

```
agentguard_hook_events
        │
        ├─ before_tool_call / tool_name='Skill'        (claudecode)
        │       └─► mv_skills_invoked_claudecode ──────┐
        │                                              │
        ├─ before_tool_call / tool_name='read'         │
        │   path contains SKILL.md             (openclaw)
        │       └─► mv_skills_invoked_openclaw ─────────┤
        │                                              │
        ├─ prompt_context                    (claudecode)
        │       └─► mv_skills_loaded_claudecode ────────┤
        │                                              │
        └─ llm_input                           (openclaw)
                └─► mv_skills_loaded_openclaw ──────────┘
                                                       │
                                                       ▼
                                             agentguard_skills
                                                       │
                                                       ▼
                                        AgentDetail → Skills tab
                                        (queried via /api/proton/query)
```

All four materialized views use `WHERE _tp_time > earliest_ts()` so they backfill all historical hook events on creation, not just new ones.

---

## Source: `agentguard_hook_events`

| hook_name | agent_type | used for |
|-----------|------------|----------|
| `before_tool_call` | `claudecode` | skill invocation (tool_name = `Skill`) |
| `before_tool_call` | `openclaw` | skill invocation (tool_name = `read`, path contains `SKILL.md`) |
| `prompt_context` | `claudecode` | skills loaded in system prompt |
| `llm_input` | `openclaw` | skills loaded in system prompt |

---

## Output: `agentguard_skills` Stream

```sql
CREATE STREAM agentguard_skills
(
  event_time        datetime64(3),
  agent_id          string,
  session_id        string,
  deployment_id     string,
  agent_type        low_cardinality(string),
  skill_name        string,
  skill_action      low_cardinality(string),   -- 'loaded' | 'invoked'
  skill_namespace   low_cardinality(string),   -- 'superpowers' | 'plugin' | '' (user skill)
  skill_source_type low_cardinality(string),   -- 'managed' | 'personal' | 'workspace' | 'bundled' | 'unknown'
  skill_location    string,                    -- full SKILL.md path (OpenClaw); '' for Claude Code
  skill_description string,                   -- description from XML (OpenClaw full format); '' otherwise
  skill_args        string                     -- args string from Skill tool input (Claude Code); '' otherwise
)
```

### Column semantics

| Column | Values | Notes |
|--------|--------|-------|
| `skill_namespace` | `'superpowers'`, `'plugin'`, `''` | Prefix before `:` in skill name. Empty for user skills with no namespace. |
| `skill_source_type` | `'managed'`, `'personal'`, `'workspace'`, `'bundled'`, `'unknown'` | See source type derivation below. |
| `skill_location` | SKILL.md path | Absolute path as emitted by OpenClaw. Only populated by OpenClaw MVs; empty for Claude Code. |
| `skill_description` | One-line description | From `<description>` XML tag (OpenClaw full format only). Empty in compact format. |
| `skill_args` | Free-text string | From `tool_input.args` in Claude Code `Skill` invocations. Empty if omitted. |

### `skill_source_type` derivation

```sql
CASE
  WHEN position(skill_location, '/.openclaw/')        > 0 THEN 'managed'
  WHEN position(skill_location, '/.config/openclaw/') > 0 THEN 'managed'
  WHEN position(skill_location, '/.agents/')          > 0 THEN 'personal'
  WHEN position(skill_location, 'node_modules')       > 0 THEN 'bundled'
  WHEN length(skill_location)                         > 0 THEN 'workspace'
  ELSE 'unknown'
END
```

> **Path matching:** OpenClaw emits fully expanded **absolute paths** (e.g. `/home/user/.openclaw/skills/…`), never tilde-prefixed paths. The match therefore uses substring search (`> 0`) on the path segment (e.g. `'/.openclaw/'`) rather than a prefix check (`= 1`) on `'~/.openclaw/'`. Using `= 1` with a tilde prefix would silently misclassify all managed and personal skills as `workspace` in containerised or CI deployments.

The key security signal: any non-empty `skill_location` that does not match a known trusted path segment is classified as `'workspace'` — meaning the SKILL.md lives inside the working repository and could have been planted by an attacker.

| `skill_source_type` | Badge colour | Meaning |
|---------------------|-------------|---------|
| `workspace` | red | SKILL.md lives inside the working repo — highest risk |
| `managed` | emerald | Installed by openclaw into `~/.openclaw/` or `~/.config/openclaw/` |
| `personal` | blue | User-level `~/.agents/` install |
| `bundled` | zinc | Shipped inside an npm package (`node_modules`) |
| `plugin` | purple | Claude Code namespaced skill (has `:` in name) |
| `user` / `unknown` | muted | Claude Code user skill or location unavailable |

---

## Materialized Views

### Invoked — Claude Code (`mv_skills_invoked_claudecode`)

Claude Code has a dedicated `Skill` tool. The skill name is at `$.tool_input.skill`; the optional free-text `args` parameter is at `$.tool_input.args`.

```sql
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
  AND json_value(event_data, '$.tool_input.skill') != ''
```

### Invoked — OpenClaw (`mv_skills_invoked_openclaw`)

OpenClaw activates a skill by reading its `SKILL.md` file via the `read` tool. The file path is the skill location; the skill name is extracted from the directory component.

```sql
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  extract(json_value(event_data, '$.params.path'),
          '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') AS skill_name,
  'invoked' AS skill_action,
  '' AS skill_namespace,
  CASE
    WHEN position(json_value(event_data, '$.params.path'), '/.openclaw/')        > 0 THEN 'managed'
    WHEN position(json_value(event_data, '$.params.path'), '/.config/openclaw/') > 0 THEN 'managed'
    WHEN position(json_value(event_data, '$.params.path'), '/.agents/')          > 0 THEN 'personal'
    WHEN position(json_value(event_data, '$.params.path'), 'node_modules')       > 0 THEN 'bundled'
    WHEN length(json_value(event_data, '$.params.path'))                         > 0 THEN 'workspace'
    ELSE 'unknown'
  END AS skill_source_type,
  json_value(event_data, '$.params.path') AS skill_location,
  '' AS skill_description,
  '' AS skill_args
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND tool_name = 'read'
  AND agent_type = 'openclaw'
  AND position(event_data, 'SKILL.md') > 0
  AND extract(json_value(event_data, '$.params.path'),
              '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') != ''
```

### Loaded — Claude Code (`mv_skills_loaded_claudecode`)

Source: `prompt_context` hook events. The system prompt includes YAML frontmatter for each available skill. The regex `---..name:` uses `---` as the YAML frontmatter delimiter anchor and `..` (any 2 chars) to bridge the JSON-encoded newline.

The character class `[a-zA-Z0-9:-]*` (note the `:`) is required to capture namespaced skill names like `superpowers:brainstorming`. Earlier versions used `[a-zA-Z0-9-]*` which silently truncated at the colon.

```sql
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
  AND length(trim(sn)) > 0
```

### Loaded — OpenClaw (`mv_skills_loaded_openclaw`)

Source: `llm_input` hook events. OpenClaw injects available skills as an XML block with `<name>`, `<description>` (full format only), and `<location>` tags per skill. In compact mode (token budget exceeded) the `<description>` tags are omitted.

Both the description and location arrays are padded to the length of the name array using `array_resize` so that `array_zip` produces one row per skill even when some tags are absent.

```sql
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  trim(skill_tuple.1) AS skill_name,
  'loaded'            AS skill_action,
  if(position(trim(skill_tuple.1), ':') > 0,
     split_by_string(':', trim(skill_tuple.1))[1], '') AS skill_namespace,
  CASE
    WHEN position(skill_tuple.3, '/.openclaw/')        > 0 THEN 'managed'
    WHEN position(skill_tuple.3, '/.config/openclaw/') > 0 THEN 'managed'
    WHEN position(skill_tuple.3, '/.agents/')          > 0 THEN 'personal'
    WHEN position(skill_tuple.3, 'node_modules')       > 0 THEN 'bundled'
    WHEN length(skill_tuple.3)                         > 0 THEN 'workspace'
    ELSE 'unknown'
  END                 AS skill_source_type,
  trim(skill_tuple.3) AS skill_location,
  trim(skill_tuple.2) AS skill_description,
  ''                  AS skill_args
FROM agentguard_hook_events
ARRAY JOIN array_zip(
  extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>'),
  array_resize(
    extract_all(event_data, '<description>([^<]+)</description>'),
    length(extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>')),
    ''
  ),
  array_resize(
    extract_all(event_data, '<location>([^<]+)</location>'),
    length(extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>')),
    ''
  )
) AS skill_tuple
WHERE _tp_time > earliest_ts()
  AND hook_name = 'llm_input'
  AND agent_type = 'openclaw'
  AND length(trim(skill_tuple.1)) > 0
```

> **Timeplus function naming:** Timeplus 3.2.1 uses snake_case — `array_zip`, `array_resize`, `extract_all`, `split_by_string`. CamelCase variants (`arrayZip`, `arrayResize`) are not available.

---

## Schema Migration

Timeplus does not support `ALTER STREAM ADD COLUMN`. When the enriched schema is deployed over an existing installation, `Init()` runs `migrateSkillsStreamIfNeeded` before creating the stream:

1. Check `system.columns` for `agentguard_skills` — skip if stream does not exist (fresh install).
2. Check whether `skill_namespace` column exists — skip if already migrated.
3. Drop all four skill MVs — **fail immediately on any error** (a failed MV drop means the subsequent stream drop will also fail, leaving the stream on the old schema with some MVs already gone).
4. Drop `agentguard_skills`.
5. Proceed to `createSkillsStream` → `createSkillsMVs` as normal. Historical data is recovered from `agentguard_hook_events` via `earliest_ts()`.

---

## Frontend: Skills Tab

The Skills tab queries `agentguard_skills` directly via `/api/proton/query` — no dedicated REST endpoint.

```sql
SELECT event_time, skill_name, skill_action,
       skill_namespace, skill_source_type, skill_location, skill_description, skill_args
FROM agentguard_skills
WHERE session_id = '<session_id>'
ORDER BY event_time ASC
SETTINGS seek_to = 'earliest'
```

The tab renders two sections:

- **Loaded** — deduplicated list of skills available in the system prompt, with first-seen timestamp and `skill_source_type` badge. Clicking a row with a non-empty `skill_location` or `skill_description` expands to show those fields.
- **Invoked** — chronological list of explicit skill activations. Expanding a row shows `skill_args` when non-empty.

---

## Known Limitations

| Item | Note |
|------|------|
| Claude Code has no `skill_location` | Claude Code hook events don't carry the SKILL.md file path. `skill_source_type` is inferred only from namespace presence (`plugin` vs `user`), not filesystem location. |
| OpenClaw compact format loses descriptions | When OpenClaw trims the prompt for token budget, `<description>` tags are omitted. `skill_description` will be empty in those rows. |
| Loaded regex false positives | The Claude Code YAML regex may match frontmatter from memory or doc files. Acceptable for observability. |
| OpenClaw `<name>` false positives | Any other XML `<name>` tags in the `llm_input` payload would match. Low risk in current data. |
| Loaded deduplication | The same skill appears once per `UserPromptSubmit` (Claude Code) or LLM turn (OpenClaw). Frontend deduplicates for display. |
