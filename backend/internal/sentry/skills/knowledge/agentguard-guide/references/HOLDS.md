# Human-in-the-loop Holds

A **hold** is a paused agent action awaiting a human Approve/Deny decision.
`agentguard_holds` is an **append-only mutation log**: creation appends a
`pending` row; a decision (or abandon/drain) appends another row for the same
`hold_id`. Current state via `arg_max(col, updated_at) GROUP BY hold_id`.

Columns: `hold_id`, `deployment_id`, `agent_id`, `session_id`, `run_id`,
`tool_name`, `rule_id`, `severity`, `args_summary`, `message`, `created_at`,
`status`, `decided_by`, `decided_at`, `decided_reason`, `updated_at`.

`status`: `pending` → `approved` / `denied` / abandoned.

## The reusable "current holds" subquery

Wrap this as a derived table in every hold query:

```sql
SELECT hold_id,
       arg_max(deployment_id, updated_at)  AS deployment_id,
       arg_max(agent_id, updated_at)       AS agent_id,
       arg_max(session_id, updated_at)     AS session_id,
       arg_max(run_id, updated_at)         AS run_id,
       arg_max(tool_name, updated_at)      AS tool_name,
       arg_max(rule_id, updated_at)        AS rule_id,
       arg_max(severity, updated_at)       AS severity,
       arg_max(args_summary, updated_at)   AS args_summary,
       arg_max(message, updated_at)        AS message,
       arg_max(created_at, updated_at)     AS created_at,
       arg_max(status, updated_at)         AS status,
       arg_max(decided_by, updated_at)     AS decided_by,
       arg_max(decided_at, updated_at)     AS decided_at,
       arg_max(decided_reason, updated_at) AS decided_reason
FROM table(agentguard_holds)
GROUP BY hold_id
```

## Pending holds (what needs a human right now)

```sql
SELECT hold_id, agent_id, session_id, tool_name, rule_id, severity,
       args_summary, message, created_at
FROM ( <current-holds subquery> )
WHERE status = 'pending'
ORDER BY created_at DESC LIMIT 50
```

## One hold's full timeline (audit)

```sql
SELECT status, decided_by, decided_at, decided_reason, updated_at
FROM table(agentguard_holds)
WHERE hold_id = '<id>' ORDER BY updated_at ASC
```

## Holds with rule names

Rule names live in the rules snapshot log (see `RULES.md`):

```sql
SELECT h.hold_id, h.tool_name, h.status, h.created_at,
       coalesce(r.rule_name, '') AS rule_name
FROM ( <current-holds subquery> ) AS h
LEFT JOIN (
  SELECT rule_id, arg_max(name, revision) AS rule_name
  FROM table(agentguard_rules) GROUP BY rule_id
) AS r ON h.rule_id = r.rule_id
ORDER BY h.created_at DESC LIMIT 50
```

## Decision analytics

```sql
-- Approval rate per tool, last 7 days
SELECT tool_name, count() AS holds,
       count_if(status = 'approved') AS approved,
       count_if(status = 'denied')   AS denied,
       count_if(status = 'pending')  AS still_pending
FROM ( <current-holds subquery> )
WHERE created_at >= now() - INTERVAL 7 DAY
GROUP BY tool_name ORDER BY holds DESC
```

The curated `get_holds` tool returns current hold records directly; use SQL
for timelines, joins, and analytics.
