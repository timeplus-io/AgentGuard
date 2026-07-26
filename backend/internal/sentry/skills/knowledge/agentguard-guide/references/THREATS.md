# Threats & Security Detections

Two streams cooperate:

- **`agentguard_security_events`** (append) — every individual detection
  written by the rule materialized views (`mv_rule_*`). Columns:
  `detected_at`, `rule_id`, `rule_name`, `severity`, `agent_id`,
  `deployment_id`, `session_id`, `run_id`, `hook_name`, `tool_name`,
  `message`, `event_data`.
- **`agentguard_threats`** (mutable, PK `(agent_id, deployment_id,
  session_id, rule_id)`) — the current status per threat. Columns:
  `agent_id`, `deployment_id`, `session_id`, `rule_id`, `rule_name`,
  `severity`, `last_seen`, `status`, `status_updated_at`.

A **threat** is the aggregation of detections by
`(agent_id, deployment_id, session_id, rule_id)`.

## Status semantics (important)

- `status = ''` (empty) means the threat was never touched → treat as
  **open**.
- Explicit values: `open`, `acknowledged`, `cleared`.
- **Auto-reopen rule**: if `status IN ('acknowledged', 'cleared')` but
  `last_seen > status_updated_at`, the rule fired again after the human
  decision — report it as **open**.
- Rows with empty `session_id` are skipped by the UI (PK integrity guard) —
  add `session_id != ''` to match.

Effective "open" filter:

```sql
WHERE session_id != ''
  AND ( status = '' OR status = 'open'
        OR (status IN ('acknowledged', 'cleared') AND last_seen > status_updated_at) )
```

## Current threat list (what the Threats page shows)

```sql
SELECT agent_id, deployment_id, session_id, rule_id, rule_name, severity,
       last_seen, status, status_updated_at
FROM table(agentguard_threats)
WHERE session_id != ''
ORDER BY last_seen DESC
```

Apply the open-filter above for "active threats"; add `severity = 'critical'`
etc. for severity cuts.

## Detection stats per threat

```sql
SELECT agent_id, deployment_id, session_id, rule_id,
       count() AS event_count, min(detected_at) AS first_seen
FROM table(agentguard_security_events)
GROUP BY agent_id, deployment_id, session_id, rule_id
```

## Recent detections (evidence for one threat)

```sql
SELECT detected_at, run_id, hook_name, tool_name, message
FROM table(agentguard_security_events)
WHERE rule_id = '<rule>' AND session_id = '<session>'
ORDER BY detected_at DESC LIMIT 50
```

## Which rules fire most (24h)

```sql
SELECT rule_id, rule_name, count() AS cnt
FROM table(agentguard_security_events)
WHERE detected_at >= now() - INTERVAL 1 DAY
GROUP BY rule_id, rule_name ORDER BY cnt DESC
```

## Joining threats to current rule policy

The rules stream is a snapshot log — reconstruct current policy with
`arg_max(col, revision)` (see `RULES.md`), then join:

```sql
SELECT t.rule_id, t.rule_name, t.severity, r.block_policy
FROM table(agentguard_threats) AS t
INNER JOIN (
  SELECT rule_id, arg_max(block_policy, revision) AS block_policy,
         arg_max(enabled, revision) AS enabled,
         arg_max(deleted, revision) AS deleted
  FROM table(agentguard_rules) GROUP BY rule_id
) AS r ON t.rule_id = r.rule_id
WHERE r.enabled AND NOT r.deleted AND t.session_id != ''
```

Caution: if you project a literal status (e.g. `'open' AS x`), never alias it
`status` — the alias would shadow `t.status` in the WHERE clause (Timeplus
makes SELECT aliases visible to same-scope WHERE).

The curated `get_threats` tool returns the fully-merged view (status
resolution + stats + recent events) — prefer it for "show me threats";
use SQL for custom cuts.
