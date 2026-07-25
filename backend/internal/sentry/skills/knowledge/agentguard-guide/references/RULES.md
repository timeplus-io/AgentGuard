# Rules & MV Health

`agentguard_rules` is an **append-only snapshot log**: every rule mutation
appends a complete snapshot row. Current state is reconstructed with
`arg_max(col, revision) GROUP BY rule_id`.

**Ordering key is `revision`** — a per-rule monotonic uint64 — **never
`updated_at`** (arg_max on a wall-clock column could resurrect a stale
snapshot after a clock backstep).

Columns: `rule_id`, `snapshot_id`, `revision`, `name`, `definition_version`,
`definition_hash`, `description`, `rendered_sql`, `deployed_udfs_json`,
`parameter_overrides_json`, `enabled`, `deleted`, `severity`, `category`,
`required_telemetry`, `fp_rate`, `hook_sources`, `block_policy`,
`update_reason`, `updated_by`, `updated_at`.

`block_policy` values: `log_only`, `auto_block`, `hold`.

## Current state of all installed rules (the canonical projection)

```sql
SELECT * FROM (
  SELECT rule_id,
         max(revision) AS max_revision,
         arg_max(name, revision)               AS name,
         arg_max(definition_version, revision) AS definition_version,
         arg_max(description, revision)        AS description,
         arg_max(enabled, revision)            AS enabled,
         arg_max(deleted, revision)            AS deleted,
         arg_max(severity, revision)           AS severity,
         arg_max(category, revision)           AS category,
         arg_max(block_policy, revision)       AS block_policy,
         arg_max(updated_by, revision)         AS updated_by,
         arg_max(updated_at, revision)         AS updated_at
  FROM table(agentguard_rules)
  GROUP BY rule_id
) WHERE deleted = false
ORDER BY rule_id
```

Two hazards this shape avoids (copy it exactly):

1. Alias `max(revision)` as **`max_revision`**, never `revision` — the alias
   would shadow the base column inside the sibling `arg_max(col, revision)`
   calls.
2. Filter `deleted = false` in an **outer WHERE**, not HAVING — `deleted` is
   an alias of an aggregate, and referencing it in same-scope HAVING nests
   aggregates → ILLEGAL_AGGREGATION (error 184).

Add `arg_max(rendered_sql, revision) AS rendered_sql` /
`parameter_overrides_json` to the projection when inspecting what a rule
actually runs.

## Quick counts

```sql
-- enabled rules
SELECT count() FROM (
  SELECT rule_id, arg_max(enabled, revision) AS active
  FROM table(agentguard_rules) GROUP BY rule_id
  HAVING active = true AND arg_max(deleted, revision) = false)
```

(HAVING is safe here because nothing is aliased to a base-column name.)

## Rule change history

The log itself is the audit trail:

```sql
SELECT revision, update_reason, updated_by, updated_at, enabled, block_policy
FROM table(agentguard_rules)
WHERE rule_id = '<rule>' ORDER BY revision DESC LIMIT 20
```

## Detections per rule (link to threats)

```sql
SELECT rule_id, count() AS detections_24h
FROM table(agentguard_security_events)
WHERE detected_at >= now() - INTERVAL 1 DAY
GROUP BY rule_id ORDER BY detections_24h DESC
```

## MV health

Each enabled rule runs as a materialized view named `mv_rule_<rule_id>`.
Both system tables below are allowlisted for run_sql:

```sql
-- Which rule MVs exist?
SELECT name FROM system.tables WHERE name LIKE 'mv_rule_%'

-- Which MVs are failing?
SELECT * FROM system.v_failed_mat_views
```

An enabled rule with no `mv_rule_<id>` in system.tables, or one listed in
v_failed_mat_views, is not detecting — that's a finding worth reporting.

The curated `get_rule_status` tool returns the merged installed-rule view;
use SQL for history, rendered SQL inspection, or custom cuts.
