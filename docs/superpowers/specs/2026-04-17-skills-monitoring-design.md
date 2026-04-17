# Skills Monitoring — Design Spec

> **For agentic workers:** This spec is ready for implementation planning. Invoke `superpowers:writing-plans` to produce the implementation plan.

## Goal

Add skills monitoring to AgentGuard: track which skills are available to an AI coding agent during a session (**loaded**) and which skills are actively invoked (**invoked**), for both Claude Code and OpenClaw agents. Surface this data in the Agent Detail session view.

---

## Background

AI coding agents are extended with **skills** — modular packages of specialist knowledge and workflow instructions (e.g., `test-driven-development`, `systematic-debugging`, `brainstorming`). Skills are defined as Markdown files with YAML frontmatter.

Two distinct events matter for observability:

- **Loaded**: the skill is present in the agent's system prompt and available to call. Detected by parsing the system prompt content that arrives in hook events.
- **Invoked**: the agent explicitly activated a skill (Claude Code: called the `Skill` tool; OpenClaw: read the SKILL.md file via the `read` tool). Detected from `before_tool_call` hook events.

This distinction is valuable for:
- **Audit**: knowing what instructions an agent was operating under during a security incident
- **Optimization**: identifying skills loaded but never used (prompt bloat)
- **Security rules**: detecting when specific high-risk skills are invoked
- **Observability**: correlating skill usage with tool calls, LLM costs, and threat detections

---

## Architecture

### Data Flow

```
agentguard_hook_events
        │
        ├─ hook_name='before_tool_call', tool_name='Skill'   (claudecode)
        │       └─► mv_skills_invoked_claudecode ────────────┐
        │                                                    │
        ├─ hook_name='before_tool_call', tool_name='read',   │
        │   $.params.path contains 'SKILL.md'  (openclaw)   │
        │       └─► mv_skills_invoked_openclaw  ─────────────┤
        │                                                    │
        ├─ hook_name='prompt_context', agent_type='claudecode'  │
        │       └─► mv_skills_loaded_claudecode ─────────────┤
        │                                                    │
        └─ hook_name='llm_input', agent_type='openclaw'     │
                └─► mv_skills_loaded_openclaw  ─────────────┘
                                                             │
                                                             ▼
                                                   agentguard_skills
                                                             │
                                                             ▼
                                              Agent Detail → Skills tab
                                              (queried via protonQuery)
```

No new REST endpoint is added. The `agentguard_skills` stream is queried directly by the frontend via the existing `/api/proton/query` streaming proxy.

---

## Section 1: Stream — `agentguard_skills`

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

Each row records a single skill activity event. Skills loaded once per session appear once per `UserPromptSubmit` (Claude Code) or once per LLM turn (OpenClaw). Deduplication for display is done at query time with `GROUP BY session_id, skill_name`.

---

## Section 2: Materialized Views

### 2.1 `mv_skills_invoked_claudecode` — Claude Code invoked

Fires when the `Skill` tool is explicitly called in Claude Code. The skill name is at `$.tool_input.skill` in the event data.

**Verified**: `json_value(event_data, '$.tool_input.skill')` returns `writing-skills`, `find-skills` from real Claude Code data.

```sql
CREATE MATERIALIZED VIEW mv_skills_invoked_claudecode
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
  AND json_value(event_data, '$.tool_input.skill') != ''
```

### 2.2 `mv_skills_invoked_openclaw` — OpenClaw invoked

OpenClaw has no dedicated Skill tool. Instead, the agent reads SKILL.md files via the `read` tool to activate a skill. Detected from `before_tool_call` events where `tool_name = 'read'` and `$.params.path` contains `SKILL.md`. The skill name is extracted from the directory component of the path (e.g. `/path/to/skills/searxng-web-search/SKILL.md` → `searxng-web-search`).

**Verified**: Real OpenClaw data shows `tool_name='read'` with `$.params.path = '/home/node/.openclaw/workspace/skills/searxng-web-search/SKILL.md'`.

```sql
CREATE MATERIALIZED VIEW mv_skills_invoked_openclaw
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
  AND extract(json_value(event_data, '$.params.path'), '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') != ''
```

### 2.3 `mv_skills_loaded_claudecode` — Claude Code loaded

Source: `prompt_context` hook events. These are synthetic events emitted by the Claude Code plugin alongside `UserPromptSubmit`. `event_data` contains a JSON-serialized transcript. The system message contains the full system prompt, which includes SKILL.md frontmatter entries for each available skill.

**Regex note:** Newlines in the JSON-serialized event data are stored as the literal 2-char sequence `\n` (0x5C 0x6E). The pattern `---..name:` uses `---` as the YAML frontmatter delimiter anchor and `..` (any 2 chars) to skip the 2-byte `\n` sequence. The intermediate alias `sn` is required — Timeplus raises `MULTIPLE_EXPRESSIONS_FOR_ALIAS` if the ARRAY JOIN alias and the output column share the same name.

**Verified**: Extracts `brainstorming`, `systematic-debugging`, `test-driven-development` from real Claude Code data.

```sql
CREATE MATERIALIZED VIEW mv_skills_loaded_claudecode
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
  AND length(trim(sn)) > 0
```

### 2.4 `mv_skills_loaded_openclaw` — OpenClaw loaded

Source: `llm_input` hook events. OpenClaw injects available skills into the system prompt as an XML block, not as YAML frontmatter. The regex targets the `<name>` XML tag inside `<skill>` entries.

**Verified**: `extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>')` extracts `healthcheck`, `node-connect`, `skill-creator`, `weather`, `searxng-web-search` from real OpenClaw `llm_input` events.

```sql
CREATE MATERIALIZED VIEW mv_skills_loaded_openclaw
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
  AND length(trim(sn)) > 0
```

---

## Section 3: Source Hook Events — Reference

| hook_name | agent_type | detection | action |
|-----------|-----------|-----------|--------|
| `before_tool_call` (tool_name=`Skill`) | `claudecode` | `$.tool_input.skill` | `invoked` |
| `before_tool_call` (tool_name=`read`, path contains `SKILL.md`) | `openclaw` | path segment before `/SKILL.md` | `invoked` |
| `prompt_context` | `claudecode` | YAML frontmatter in transcript system message | `loaded` |
| `llm_input` | `openclaw` | `<name>` XML tags in system prompt | `loaded` |

The `prompt_context` event is a synthetic hook emitted by the Claude Code plugin alongside `UserPromptSubmit`. It reads the transcript JSONL file at `transcript_path` and sends the last `AGENTGUARD_TRANSCRIPT_MESSAGES` messages (default 50) as `event_data`.

---

## Section 4: UI — Skills Tab in Agent Detail

A new **Skills** tab is added to the session content panel in `AgentDetail.jsx`, alongside the existing Timeline / Events / Trace tabs.

### Placement

```
[Timeline] [Events] [Trace] [Skills]   ← new tab
```

The tab is visible whenever a session is selected in the left panel. It does **not** require a specific run to be selected — it shows skills across all runs in the session.

### Layout

```
┌─────────────────────────────────────────────────────────┐
│  LOADED  —  available in system prompt                   │
│                                                         │
│  ⬡ brainstorming               first seen  10:02:01    │
│  ⬡ systematic-debugging         first seen  10:02:01    │
│  ⬡ test-driven-development      first seen  10:02:01    │
│                                                         │
│  INVOKED  —  Skill activations                          │
│                                                         │
│  ▸  10:05:33   parallel-feature-development             │
│  ▸  10:12:14   parallel-feature-development             │
│  ▸  10:31:07   find-skills                              │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

### Query

The Skills tab queries `agentguard_skills` directly via `api.protonQuery()`:

```sql
SELECT event_time, skill_name, skill_action
FROM agentguard_skills
WHERE session_id = '<selected_session_id>'
ORDER BY event_time ASC
SETTINGS seek_to = 'earliest'
```

Loaded skills are deduplicated in the frontend (same skill appears once per `UserPromptSubmit` or LLM turn). Invoked skills are shown as a chronological list.

---

## Section 5: Implementation Files

| file | change |
|------|--------|
| `backend/internal/timeplus/init.go` | Add `ddlSkillsStream`, `createSkillsStream()`, 4 MV DDL constants, `createSkillsMVs()`. Wire into `Init()`. |
| `frontend/src/pages/AgentDetail.jsx` | Add `'skills'` tab to the tab list. Add `SkillsTab` component that calls `api.protonQuery()` and renders loaded/invoked sections. |

No new handler file. No changes to `models.go`, `main.go`, `api/client.js`, or `Sidebar.jsx`.

---

## Section 6: Verification Status

**All patterns verified 2026-04-17 against live Timeplus data.**

| pattern | query | result |
|---------|-------|--------|
| Claude Code loaded | `extract_all(event_data, '---..name: ([a-zA-Z][a-zA-Z0-9-]*)')` on `prompt_context` | `brainstorming`, `systematic-debugging`, `test-driven-development` ✅ |
| Claude Code invoked | `json_value(event_data, '$.tool_input.skill')` on `before_tool_call` + `tool_name='Skill'` | `writing-skills`, `find-skills` ✅ |
| OpenClaw loaded | `extract_all(event_data, '<name>([a-zA-Z][a-zA-Z0-9-]*)</name>')` on `llm_input` | `healthcheck`, `node-connect`, `skill-creator`, `weather`, `searxng-web-search` ✅ |
| OpenClaw invoked | `tool_name='read'` + path contains `SKILL.md` + `extract(path, '/skills/(...)/SKILL')` | `searxng-web-search` ✅ |

---

## Section 7: Known Risks

| item | status | note |
|------|--------|------|
| Loaded regex false positives | ⚠️ Known | Claude Code: `viper`, `Skill-Name-With-Hyphens` from memory/doc frontmatter appear alongside real skills. Acceptable for observability. |
| OpenClaw `<name>` false positives | ⚠️ Monitor | Any other XML `<name>` tags in the event payload would match. Low risk — confirmed clean in current data. |
| Deduplication | ✅ By design | Same skill appears multiple times per event. Frontend deduplicates by `skill_name` for the Loaded section. |
| Plugin-namespaced names | ⚠️ Known gap | Regex captures `code-reviewer` but not `superpowers:code-reviewer`. Namespaced names appear truncated. Follow-up. |
| OpenClaw SKILL.md path variants | ⚠️ Assumption | Regex `/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL` covers the verified path format. Other installation paths may differ. |
