# AgentGuard — Skills Monitoring

AgentGuard tracks which **skills** are available to an AI agent during a session and which are actively invoked. This document explains how that data is collected, processed, and surfaced.

---

## What Are Skills?

Skills are modular packages of specialist knowledge and workflow instructions that extend an agent's capabilities (e.g., `test-driven-development`, `systematic-debugging`, `brainstorming`). They are defined as Markdown files with YAML frontmatter and installed into the agent's environment.

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

All detection is derived from the raw hook event stream that agent plugins write to. The relevant hook names are:

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
  event_time    datetime64(3),
  agent_id      string,
  session_id    string,
  deployment_id string,
  agent_type    low_cardinality(string),   -- 'claudecode' | 'openclaw'
  skill_name    string,
  skill_action  low_cardinality(string)    -- 'loaded' | 'invoked'
)
```

Each row is one skill activity event. The stream is written exclusively by the four materialized views below.

---

## Materialized Views

### Invoked — Claude Code (`mv_skills_invoked_claudecode`)

Claude Code has a dedicated `Skill` tool. When invoked, the skill name is at `$.tool_input.skill` in the event data.

```sql
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  json_value(event_data, '$.tool_input.skill') AS skill_name,
  'invoked' AS skill_action
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND tool_name = 'Skill'
  AND agent_type = 'claudecode'
  AND json_value(event_data, '$.tool_input.skill') != ''
```

### Invoked — OpenClaw (`mv_skills_invoked_openclaw`)

OpenClaw has no dedicated Skill tool. Instead, the agent reads a `SKILL.md` file via the `read` tool to activate a skill. The skill name is extracted from the directory component of the file path (e.g. `/skills/searxng-web-search/SKILL.md` → `searxng-web-search`).

```sql
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  extract(json_value(event_data, '$.params.path'), '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') AS skill_name,
  'invoked' AS skill_action
FROM agentguard_hook_events
WHERE _tp_time > earliest_ts()
  AND hook_name = 'before_tool_call'
  AND tool_name = 'read'
  AND agent_type = 'openclaw'
  AND position(event_data, 'SKILL.md') > 0
  AND extract(json_value(event_data, '$.params.path'), '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') != ''
```

### Loaded — Claude Code (`mv_skills_loaded_claudecode`)

Source: `prompt_context` hook events — synthetic events emitted by the Claude Code plugin alongside each `UserPromptSubmit`. The `event_data` contains a JSON-serialized transcript; the system message within it holds the full system prompt, which includes YAML frontmatter for each available skill.

The regex `---..name:` uses `---` as the YAML frontmatter delimiter anchor and `..` (any 2 chars) to match the literal two-byte sequence `\n` as it appears JSON-encoded in the payload. The intermediate alias `sn` is required because Timeplus raises `MULTIPLE_EXPRESSIONS_FOR_ALIAS` when an `ARRAY JOIN` alias and an output column share the same name.

```sql
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  trim(sn) AS skill_name,
  'loaded' AS skill_action
FROM agentguard_hook_events
ARRAY JOIN extract_all(event_data, '---..name: ([a-zA-Z][a-zA-Z0-9-]*)') AS sn
WHERE _tp_time > earliest_ts()
  AND hook_name = 'prompt_context'
  AND agent_type = 'claudecode'
  AND length(trim(sn)) > 0
```

### Loaded — OpenClaw (`mv_skills_loaded_openclaw`)

Source: `llm_input` hook events. OpenClaw injects available skills into the system prompt as an XML block (`<available_skills><skill><name>…</name></skill></available_skills>`). The regex targets the `<name>` tag inside each skill entry.

```sql
SELECT
  event_time, agent_id, session_id, deployment_id, agent_type,
  trim(sn) AS skill_name,
  'loaded' AS skill_action
FROM agentguard_hook_events
ARRAY JOIN extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>') AS sn
WHERE _tp_time > earliest_ts()
  AND hook_name = 'llm_input'
  AND agent_type = 'openclaw'
  AND length(trim(sn)) > 0
```

---

## Frontend: Skills Tab

The Skills tab in the Agent Detail session view queries `agentguard_skills` directly via the `/api/proton/query` streaming proxy — no dedicated REST endpoint is needed.

```sql
SELECT event_time, skill_name, skill_action
FROM agentguard_skills
WHERE session_id = '<session_id>'
ORDER BY event_time ASC
SETTINGS seek_to = 'earliest'
```

The tab renders two sections:

- **Loaded** — deduplicated list of skills available in the system prompt, with first-seen timestamp
- **Invoked** — chronological list of explicit skill activations with timestamps

---

## Known Limitations

| Item | Note |
|------|------|
| Loaded regex false positives | The Claude Code YAML regex may match frontmatter from memory or doc files (e.g. `viper`, `Skill-Name-With-Hyphens`). Acceptable for observability. |
| Plugin-namespaced skill names | The regex captures `code-reviewer` but not `superpowers:code-reviewer`. Namespaced names appear truncated. |
| OpenClaw `<name>` false positives | Any other XML `<name>` tags in the `llm_input` payload would match. Low risk in current data. |
| Loaded deduplication | The same skill appears once per `UserPromptSubmit` (Claude Code) or LLM turn (OpenClaw) in the stream. Frontend deduplicates for display. |
