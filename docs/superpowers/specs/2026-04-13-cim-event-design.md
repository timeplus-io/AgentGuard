# AgentShield CIM Event Stream — Design Spec

**Date:** 2026-04-13  
**Status:** Approved  
**Scope:** Common Information Model for unified agent telemetry across Claude Code and OpenClaw

---

## Problem

`agentshield_hook_events` receives telemetry from two agents with fundamentally different hook naming conventions, field naming (snake_case vs camelCase), and event semantics. All 4 security rules query this raw stream directly, meaning every rule must handle both agents' quirks. Adding a third agent would require updating every rule.

---

## Goal

Introduce `agentshield_cim_event` — a normalized stream that:

- Unifies hook events from Claude Code and OpenClaw into a single, consistent schema
- Covers the full event lifecycle: session, user input, LLM request/response, tool invocation/completion
- Becomes the authoritative query surface for all security rules (replacing direct queries on `agentshield_hook_events`)
- Is created automatically by the AgentShield backend on startup

---

## Architecture

```
agentshield_hook_events
  ├── mv_cim_claudecode ──┐
  └── mv_cim_openclaw ────┴──► agentshield_cim_event
                                      │
                          ┌───────────┼───────────┐
                     mv_rule_rp001  mv_rule_rp002  ...
                          │
                    agentshield_security_events
```

Two dedicated MVs — one per agent — both write into `agentshield_cim_event`. Security rule MVs are then rewritten to query `agentshield_cim_event` instead of `agentshield_hook_events`. The rule output stream (`agentshield_security_events`) and all downstream APIs are unchanged.

---

## Stream: `agentshield_cim_event`

### DDL

```sql
CREATE STREAM IF NOT EXISTS agentshield_cim_event
(
  -- Identity & timing
  event_time        datetime64(3),
  event_type        low_cardinality(string),
  agent_type        low_cardinality(string),
  agent_id          string,
  deployment_id     low_cardinality(string),
  session_id        string,
  run_id            string,

  -- LLM context
  provider          low_cardinality(string),
  model             low_cardinality(string),

  -- Tool & content
  tool_name         string,
  tool_input        string,
  tool_result       string,
  tool_success      bool,
  tool_duration_ms  int64,
  user_message      string,
  assistant_message string,

  -- Governance
  hook_decision     low_cardinality(string),
  block_reason      string,

  -- Raw passthrough
  raw_hook_name     low_cardinality(string),
  raw_event_data    string
);
```

### `event_type` Values

| Value | Meaning | Claude Code source | OpenClaw source |
|---|---|---|---|
| `session_start` | Session or sub-session begins | `session_start`, `subagent_spawning` | `session_start` |
| `session_end` | Session terminates | *(none)* | `session_end` |
| `user_input` | User submits a message | `user_prompt_submit` | `message_received` |
| `llm_request` | LLM API call initiated | *(none — not exposed by hook)* | `llm_input` |
| `llm_response` | LLM API call completed | `llm_output` | `llm_output` |
| `tool_invoke` | Tool call about to execute | `before_tool_call` | `before_tool_call` |
| `tool_complete` | Tool call finished | `after_tool_call` | `after_tool_call` |

Note: `tool_result_persist` (OpenClaw) is dropped — it duplicates `after_tool_call` content and has no Claude Code equivalent.

### Column Semantics

| Column | Notes |
|---|---|
| `event_type` | Normalized event category — rules filter on this instead of `hook_name` |
| `agent_type` | `claudecode` or `openclaw` |
| `tool_name` | Normalized from `tool_name` col (CC) or `json_value(event_data, '$.toolName')` (OC) |
| `tool_input` | JSON object string: `json_query(event_data, '$.tool_input')` (CC) or `json_query(event_data, '$.params')` (OC) |
| `tool_result` | `json_value(event_data, '$.tool_response.stdout')` (CC) or `json_query(event_data, '$.result')` (OC) |
| `tool_success` | `true` unless interrupted (CC) or `$.error != ''` (OC) |
| `tool_duration_ms` | OpenClaw provides `$.durationMs`; Claude Code defaults to `0` |
| `user_message` | From `$.prompt` (CC) or `$.content` (OC) |
| `assistant_message` | From `$.last_assistant_message` (CC) or `$.assistantTexts[0]` (OC) |
| `hook_decision` | Passthrough from outer column: `observe`, `block`, `allow` |
| `raw_hook_name` | Original hook name for debugging and backward compatibility |
| `raw_event_data` | Full original JSON — used by LIKE pattern rules as fallback |

---

## MV 1: `mv_cim_claudecode`

Filters `agentshield_hook_events` where `agent_type = 'claudecode'`. Maps 6 hook names.

```sql
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_cim_claudecode
INTO agentshield_cim_event AS
SELECT
  event_time,
  multi_if(
    hook_name = 'session_start',      'session_start',
    hook_name = 'subagent_spawning',  'session_start',
    hook_name = 'user_prompt_submit', 'user_input',
    hook_name = 'llm_output',         'llm_response',
    hook_name = 'before_tool_call',   'tool_invoke',
    hook_name = 'after_tool_call',    'tool_complete',
    'other'
  )                                                                AS event_type,
  agent_type,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  ''                                                               AS provider,
  if(model != '', model, json_value(event_data, '$.model'))       AS model,
  if(tool_name != '', tool_name,
     json_value(event_data, '$.tool_name'))                       AS tool_name,
  json_query(event_data, '$.tool_input')                          AS tool_input,
  json_value(event_data, '$.tool_response.stdout')                AS tool_result,
  json_value(event_data, '$.tool_response.interrupted') != 'true' AS tool_success,
  0                                                               AS tool_duration_ms,
  json_value(event_data, '$.prompt')                              AS user_message,
  json_value(event_data, '$.last_assistant_message')              AS assistant_message,
  hook_decision,
  block_reason,
  hook_name                                                       AS raw_hook_name,
  event_data                                                      AS raw_event_data
FROM agentshield_hook_events
WHERE agent_type = 'claudecode'
  AND hook_name IN (
    'session_start', 'subagent_spawning',
    'user_prompt_submit',
    'before_tool_call', 'after_tool_call',
    'llm_output'
  );
```

---

## MV 2: `mv_cim_openclaw`

Filters `agentshield_hook_events` where `agent_type = 'openclaw'`. Maps 7 hook names. `tool_result_persist` is excluded.

```sql
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_cim_openclaw
INTO agentshield_cim_event AS
SELECT
  event_time,
  multi_if(
    hook_name = 'session_start',    'session_start',
    hook_name = 'session_end',      'session_end',
    hook_name = 'message_received', 'user_input',
    hook_name = 'llm_input',        'llm_request',
    hook_name = 'llm_output',       'llm_response',
    hook_name = 'before_tool_call', 'tool_invoke',
    hook_name = 'after_tool_call',  'tool_complete',
    'other'
  )                                                                        AS event_type,
  agent_type,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  if(provider != '', provider,
     json_value(event_data, '$.provider'))                                 AS provider,
  if(model != '', model,
     json_value(event_data, '$.model'))                                    AS model,
  if(tool_name != '', tool_name,
     json_value(event_data, '$.toolName'))                                 AS tool_name,
  json_query(event_data, '$.params')                                       AS tool_input,
  json_query(event_data, '$.result')                                       AS tool_result,
  json_value(event_data, '$.error') = ''                                   AS tool_success,
  json_extract_int(event_data, 'durationMs')                               AS tool_duration_ms,
  json_value(event_data, '$.content')                                      AS user_message,
  json_value(event_data, '$.assistantTexts[0]')                            AS assistant_message,
  hook_decision,
  block_reason,
  hook_name                                                                AS raw_hook_name,
  event_data                                                               AS raw_event_data
FROM agentshield_hook_events
WHERE agent_type = 'openclaw'
  AND hook_name IN (
    'session_start', 'session_end',
    'message_received',
    'llm_input', 'llm_output',
    'before_tool_call', 'after_tool_call'
  );
```

---

## Rule Migration

All 4 rules in `internal/catalog/seed/` are updated to query `agentshield_cim_event`. The `hook_name` filter becomes an `event_type` filter. Content scanning moves to `tool_input`, `tool_result`, `user_message`, `assistant_message`, and `raw_event_data`.

| Rule | Old `hook_name` filter | New `event_type` filter | Content scan fields |
|---|---|---|---|
| rp-001 Prompt Injection | `llm_input` | `user_input`, `llm_request` | `user_message`, `raw_event_data` |
| rp-002 DLP Sentinel | `tool_result_persist`, `after_tool_call`, `llm_output` | `tool_complete`, `llm_response` | `tool_result`, `assistant_message`, `raw_event_data` |
| rp-003 Privilege Guard | `before_tool_call` | `tool_invoke` | `tool_name`, `tool_input`, `raw_event_data` |
| rp-004 Supply Chain | `before_install`, `before_tool_call` | `tool_invoke` | `tool_input`, `raw_event_data` |

---

## Backend Changes

### `internal/timeplus/init.go`

Add three `CREATE ... IF NOT EXISTS` statements to the startup initialization sequence, after the existing streams are created and before MV sync:

1. Create `agentshield_cim_event` stream
2. Create `mv_cim_claudecode` MV
3. Create `mv_cim_openclaw` MV

Pattern: same idempotent `ExecHTTP()` calls already used for `agentshield_rules` and `agentshield_security_events`.

### `internal/catalog/seed/rp-00*.yaml`

Update `query_sql` in each of the 4 seed files:
- Source table: `agentshield_cim_event` (was `agentshield_hook_events`)
- `hook_name` → `event_type` in WHERE clauses
- Content LIKE patterns: add checks on normalized fields; keep `raw_event_data` check as fallback

Note: seed file updates affect **future rule installations only**. The currently running `mv_rule_rp00*` MVs were created from the old SQL and must be explicitly uninstalled and reinstalled (via the existing toggle/upgrade mechanism) to pick up the new CIM-based SQL. This operational step is separate from the code change.

### No other changes

- `agentshield_security_events` schema: unchanged
- All backend API handlers: unchanged
- OTEL streams: unchanged
- Frontend: unchanged

---

## Example Queries Against the CIM

```sql
-- All tool invocations across both agents (live)
SELECT event_time, agent_type, agent_id, tool_name, tool_input, hook_decision
FROM agentshield_cim_event
WHERE event_type = 'tool_invoke';

-- Tool error rate by agent and tool (historical)
SELECT agent_type, tool_name,
       count_if(NOT tool_success) AS errors,
       count_if(tool_success)     AS successes
FROM table(agentshield_cim_event)
WHERE event_type = 'tool_complete'
GROUP BY agent_type, tool_name
ORDER BY errors DESC;

-- Session activity across both agents (historical)
SELECT agent_type, count() AS sessions
FROM table(agentshield_cim_event)
WHERE event_type = 'session_start'
GROUP BY agent_type;

-- Blocked events across both agents (live)
SELECT event_time, agent_type, event_type, tool_name, block_reason
FROM agentshield_cim_event
WHERE hook_decision = 'block';
```

---

## What Is Explicitly Out of Scope

- Backfilling historical `agentshield_hook_events` into the CIM (MVs are forward-only)
- Migrating the existing rule MVs atomically in the same PR (rule SQL updates can follow as a separate change once the CIM stream is live)
- Changes to the OTEL streams (`otel_logs`, `otel_metrics_sum`, etc.)
- Frontend or API changes
