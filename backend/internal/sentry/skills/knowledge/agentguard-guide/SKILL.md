---
name: agentguard-guide
description: >
  AgentGuard's data model and the canonical read-only queries behind every
  API resource: agents, sessions, events, metrics, threats, cost, rules,
  holds, and DLP. Load this before querying AgentGuard data with run_sql —
  these are the same query shapes the AgentGuard UI runs, so your numbers
  match what the operator sees.
metadata:
  author: timeplus-io
  scope: AgentGuard Sentry (read-only, executed via the run_sql tool)
---

# AgentGuard Data Guide

You are querying **AgentGuard**, a security monitoring platform for AI coding
agents. All data lives in Timeplus streams. This guide gives you the exact
streams, columns, and canonical query per resource. Everything here fits the
`run_sql` contract: single read-only `SELECT`/`WITH`, sources wrapped in
`table(...)`, no `SETTINGS`, LIMIT enforced.

## Reference files

| Domain | Reference |
|--------|-----------|
| Agents, sessions, runs, raw events, activity | `references/SESSIONS.md` |
| LLM & tool metrics (tokens, latency, throughput) | `references/METRICS.md` |
| Threats & security detections | `references/THREATS.md` |
| Cost, pricing, budgets | `references/COST.md` |
| Rules & MV health | `references/RULES.md` |
| Human-in-the-loop holds | `references/HOLDS.md` |
| DLP enriched messages & risk profiles | `references/DLP.md` |

## Stream inventory

| Stream | Kind | What it holds |
|--------|------|---------------|
| `agentguard_hook_events` | append | Raw hook payloads from agent plugins — the workhorse for agents/sessions/events |
| `agentguard_cim_event` | append | Normalized lifecycle events (`session_start`, `user_input`, `llm_response`, `tool_invoke`, `tool_complete`, `session_end`, …) |
| `agentguard_cim_metrics` | append | Scalar metrics (`llm.*`, `tool.*`) with dimensions in a `tags` map |
| `agentguard_security_events` | append | Rule-triggered detections written by `mv_rule_*` materialized views |
| `agentguard_threats` | mutable (PK agent_id, deployment_id, session_id, rule_id) | Current threat status per (agent, deployment, session, rule) |
| `agentguard_pricing` | mutable (PK provider, model) | Per-model token prices |
| `agentguard_cost_budgets` | append log | Budget mutations; current state via `arg_max(col, updated_at)` |
| `agentguard_rules` | append log | Rule instance snapshots; current state via `arg_max(col, revision)` — revision, NOT updated_at |
| `agentguard_holds` | append log | Hold lifecycle; current state via `arg_max(col, updated_at)` |
| `agentguard_dlp_enriched` | append | One enriched row per conversation message (entities, risk tags/score, embedding) |

## The four patterns that make every query work

**1. Always `table(stream)`.** A bare `FROM agentguard_x` is an unbounded
streaming subscription that never returns (and `run_sql` rejects it).
`table(...)` is a bounded historical scan.

**2. Log streams need `arg_max` reconstruction.** `agentguard_rules`,
`agentguard_holds`, and `agentguard_cost_budgets` are append-only mutation
logs — reading them raw returns full history. Current state is:

```sql
SELECT key_col, arg_max(some_col, ordering_col) AS some_col, ...
FROM table(the_log) GROUP BY key_col
```

Ordering key: **`revision` for rules** (monotonic per rule; never use
`updated_at` — wall-clock backsteps could resurrect stale snapshots),
**`updated_at` for holds and budgets**.

**3. Mutable streams are already current.** `agentguard_threats`,
`agentguard_pricing` dedup by primary key — `table(...)` returns one current
row per key, no arg_max needed.

**4. Alias-shadowing hazard.** Timeplus makes SELECT-list aliases visible to
same-scope WHERE/HAVING. Never alias an aggregate to the same name as a
column feeding a sibling `arg_max` (e.g. `max(revision) AS revision` next to
`arg_max(deleted, revision)`), and don't filter in HAVING on an alias of an
aggregate — you'll hit ILLEGAL_AGGREGATION (error 184). Use distinct aliases
(`max_revision`) and wrap in an outer `SELECT ... WHERE` for filtering, as
the canonical queries in the references do.

## Quick answers without loading references

```sql
-- How many agents? (an "agent" is a distinct (agent_id, deployment_id) pair
-- derived from events — there is no agents stream)
SELECT count() FROM (
  SELECT agent_id, deployment_id FROM table(agentguard_hook_events)
  WHERE agent_id != '' GROUP BY agent_id, deployment_id)

-- Active threats in the last 24h ('' status means open)
SELECT count() FROM table(agentguard_threats)
WHERE (status = '' OR status IN ('open', 'acknowledged'))
  AND last_seen >= now() - INTERVAL 1 DAY

-- Threat categories, last 24h
SELECT rule_name, count() AS cnt FROM table(agentguard_security_events)
WHERE detected_at >= now() - INTERVAL 1 DAY
GROUP BY rule_name ORDER BY cnt DESC

-- Currently-enabled rules
SELECT count() FROM (
  SELECT rule_id, arg_max(enabled, revision) AS active
  FROM table(agentguard_rules) GROUP BY rule_id
  HAVING active = true AND arg_max(deleted, revision) = false)
```

For anything deeper — session drill-downs, token/latency metrics, cost math,
hold timelines, DLP forensics — load the matching reference file first. For
semantic (meaning-based) search over conversation content, prefer the
`dlp_search` tool over hand-written SQL.

## What SQL cannot reach

- **The rule catalog** (installable rule definitions) is filesystem-backed,
  not a stream — use the `get_rule_catalog` tool.
- **Credential-bearing config streams** (`agentguard_dlp_config`,
  `agentguard_sentry_config`) are blocked in run_sql; their API endpoints
  redact secrets and there is nothing analytically useful in them.
