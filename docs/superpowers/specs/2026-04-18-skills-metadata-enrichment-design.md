# Skills Metadata Enrichment — Design Spec

> **For agentic workers:** This spec is ready for implementation planning. Invoke `superpowers:writing-plans` to produce the implementation plan.

## Goal

Enrich `agentguard_skills` with five new metadata columns extracted from agent hook events, giving future threat-detection rules structured signals about skill provenance, namespace, description, and invocation context — without modifying any threat detection or security event streams.

---

## Background

The current `agentguard_skills` stream captures only `skill_name` and `skill_action` (`loaded` | `invoked`). Two critical signals are missing:

1. **Provenance** — where did the skill come from? A skill loaded from the working repository's `skills/` directory is a meaningful security signal (attacker-planted SKILL.md). Skills from `~/.openclaw/` or `~/.agents/` are user-managed and trusted.

2. **Namespace** — Claude Code skills can be namespaced (`superpowers:brainstorming`, `plugin:foo`). The current regex truncates at `:`, losing the namespace entirely.

3. **Invocation args** — Claude Code's `Skill` tool accepts an optional free-text `args` parameter. This context is currently discarded.

All three signals are already present in `agentguard_hook_events`. No plugin changes are required.

---

## Architecture

### Data Flow (unchanged)

```
agentguard_hook_events
        │
        ├─ hook='before_tool_call', tool='Skill', agent='claudecode'
        │       └─► mv_skills_invoked_claudecode ──────────────┐
        │                                                      │
        ├─ hook='before_tool_call', tool='read', agent='openclaw'
        │   path matches /skills/*/SKILL.md                    │
        │       └─► mv_skills_invoked_openclaw ────────────────┤
        │                                                      │
        ├─ hook='prompt_context', agent='claudecode'           │
        │   event_data contains ---\nname: pattern             │
        │       └─► mv_skills_loaded_claudecode ───────────────┤
        │                                                      │
        └─ hook='llm_input', agent='openclaw'                  │
            event_data contains <available_skills> XML         │
                └─► mv_skills_loaded_openclaw ─────────────────┘
                                                               │
                                                               ▼
                                                   agentguard_skills (enriched)
                                                               │
                                                               ▼
                                                   SkillsTab in AgentDetail.jsx
```

No new streams. No REST endpoints. Frontend queries `agentguard_skills` directly via `/api/proton/query`.

---

## Section 1: Stream Schema

`agentguard_skills` gains five columns. Because Timeplus does not support `ALTER STREAM ADD COLUMN`, the migration path is: drop all 4 source MVs → drop stream → recreate stream → recreate MVs. `Init()` handles this automatically on first start after the upgrade by checking for the presence of `skill_namespace` in `system.columns`.

```sql
CREATE STREAM IF NOT EXISTS agentguard_skills
(
  event_time        datetime64(3),
  agent_id          string,
  session_id        string,
  deployment_id     string,
  agent_type        low_cardinality(string),
  skill_name        string,
  skill_action      low_cardinality(string),   -- 'loaded' | 'invoked'
  -- NEW:
  skill_namespace   low_cardinality(string),   -- 'superpowers' | 'plugin' | '' (user skill)
  skill_source_type low_cardinality(string),   -- 'managed' | 'personal' | 'workspace' | 'bundled' | 'unknown'
  skill_location    string,                    -- full SKILL.md path (OpenClaw); '' for Claude Code
  skill_description string,                   -- description from XML (OpenClaw full format); '' otherwise
  skill_args        string                     -- args string from Skill tool input (Claude Code); '' otherwise
)
```

**Column semantics:**

| Column | Values | Notes |
|--------|--------|-------|
| `skill_namespace` | `'superpowers'`, `'plugin'`, `''` | Prefix before `:` in skill name. Empty for user skills with no namespace. |
| `skill_source_type` | `'managed'`, `'personal'`, `'workspace'`, `'bundled'`, `'unknown'` | See source type derivation below. |
| `skill_location` | SKILL.md path | Home-dir compacted (`~/…`). Only populated by OpenClaw MVs. |
| `skill_description` | One-line description | From `<description>` XML tag (OpenClaw full format only). Empty in compact format. |
| `skill_args` | Free-text string | From `tool_input.args` in Claude Code `Skill` invocations. Empty if the LLM did not pass args. |

**`skill_source_type` derivation from `skill_location`:**

```sql
CASE
  WHEN position(skill_location, '~/.openclaw/')        = 1 THEN 'managed'
  WHEN position(skill_location, '~/.config/openclaw/') = 1 THEN 'managed'
  WHEN position(skill_location, '~/.agents/')          = 1 THEN 'personal'
  WHEN position(skill_location, 'node_modules')        > 0 THEN 'bundled'
  WHEN length(skill_location)                          > 0 THEN 'workspace'
  ELSE 'unknown'
END
```

The key signal: any non-empty `skill_location` that does not match the known trusted prefixes (`~/.*`) is classified as `'workspace'` — meaning the SKILL.md lives inside the working repository and could have been planted by an attacker.

---

## Section 2: Updated Materialized Views

### 2.1 `mv_skills_loaded_openclaw`

OpenClaw's `llm_input` hook carries the full `<available_skills>` XML in `event_data`. Three tags are present per skill: `<name>` (always), `<description>` (full format only), `<location>` (always).

The challenge: in compact format, `<description>` tags are absent, so `extract_all(...description...)` returns an empty array while name/location arrays have N elements. Solution: pad the descriptions array to the name array length using `arrayResize`.

```sql
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
  AND length(trim(skill_tuple.1)) > 0
```

### 2.2 `mv_skills_invoked_openclaw`

The `read` tool path (`$.params.path`) is the SKILL.md location. Use it directly for `skill_location` and derive `skill_source_type` from it.

```sql
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
              '/skills/([a-zA-Z][a-zA-Z0-9-]*)/SKILL') != ''
```

### 2.3 `mv_skills_loaded_claudecode`

**Bug fix included:** the current regex `[a-zA-Z][a-zA-Z0-9-]*` cannot match `:` in namespaced skill names like `superpowers:brainstorming`. Extended to `[a-zA-Z][a-zA-Z0-9:-]*`.

```sql
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
  AND length(trim(sn)) > 0
```

### 2.4 `mv_skills_invoked_claudecode`

Adds `skill_namespace` (extracted from the skill name before `:`), `skill_source_type` (derived from namespace presence), and `skill_args` (from `tool_input.args` — empty string if absent since `json_value` returns `''` for missing keys).

```sql
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
  AND json_value(event_data, '$.tool_input.skill') != ''
```

---

## Section 3: Migration Strategy

Since `agentguard_skills` gains new columns, `Init()` must detect and migrate the old schema:

1. Query `system.columns` for `agentguard_skills` to check if `skill_namespace` exists.
2. If missing (old schema): drop all 4 skill MVs (`DROP VIEW IF EXISTS`), drop the stream (`DROP STREAM IF EXISTS agentguard_skills`).
3. Proceed to `createSkillsStream()` → `createSkillsMVs()` as normal.

This runs once on first start after upgrade. Historical data in `agentguard_hook_events` is backfilled because all MVs use `WHERE _tp_time > earliest_ts()`.

```go
func migrateSkillsStreamIfNeeded(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
    // Check if skill_namespace column exists
    // If not: drop 4 MVs + drop stream
    // Return nil (createSkillsStream will recreate)
}
```

---

## Section 4: Frontend — `SkillsTab` Updates

The existing query gains the 5 new columns:

```sql
SELECT event_time, skill_name, skill_action,
       skill_namespace, skill_source_type, skill_location, skill_description, skill_args
FROM agentguard_skills
WHERE session_id = '<session_id>'
ORDER BY event_time ASC
SETTINGS seek_to = 'earliest'
```

**Loaded section** — each skill card adds a `skill_source_type` badge:

| `skill_source_type` | Badge color |
|---------------------|-------------|
| `workspace`         | `text-red-400` / red border |
| `managed`           | `text-emerald-400` |
| `personal`          | `text-blue-400` |
| `bundled`           | `text-zinc-400` |
| `plugin`            | `text-purple-400` |
| `user` / `unknown`  | `text-zinc-500` |

Expand-to-reveal (click row) shows `skill_location` and `skill_description` when non-empty.

**Invoked section** — expand-to-reveal adds `skill_args` when non-empty, labeled "Args".

---

## Section 5: Implementation Files

| File | Change |
|------|--------|
| `backend/internal/timeplus/init.go` | Update `ddlSkillsStream` (5 new columns). Update 4 MV DDL constants. Add `migrateSkillsStreamIfNeeded()`. Call it from `Init()` before `createSkillsStream`. |
| `frontend/src/pages/AgentDetail.jsx` | Update `SkillsTab` query to select new columns. Add source type badge. Add expand-to-reveal for location/description/args. |

No changes to `models.go`, `main.go`, `api/client.js`, or any handler.

---

## Section 6: Known Limitations

| Item | Note |
|------|------|
| Claude Code has no `skill_location` | Claude Code hook events don't carry the SKILL.md file path. `skill_source_type` is inferred only from namespace presence (`plugin` vs `user`), not from filesystem location. |
| OpenClaw compact format loses descriptions | When OpenClaw trims the prompt for token budget, `<description>` tags are omitted. `skill_description` will be empty in those rows. |
| `arrayResize` availability | `arrayResize` is a ClickHouse function — needs validation against the deployed Timeplus version before shipping. If unavailable, the OpenClaw loaded MV can fall back to extracting name+location only (2-element `arrayZip`). |
| Namespace regex truncation (legacy data) | Historical `agentguard_skills` rows created before this migration captured truncated skill names (e.g. `superpowers` instead of `superpowers:brainstorming`). Backfill from `agentguard_hook_events` via `earliest_ts()` will correct new rows; old rows in the stream are lost when the stream is dropped. |
| `split_by_string` indexing | `split_by_string(':', skill_name)[1]` returns the first segment (1-based). If a skill name has no `:`, the array has one element and `[1]` returns the full name — handled by the `position(..., ':') > 0` guard. |
