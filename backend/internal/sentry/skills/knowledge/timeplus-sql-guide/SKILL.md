---
name: timeplus-sql-guide
description: >
  Write and execute Timeplus streaming SQL for real-time analytics. Use this skill
  when the user wants to query streams, analyze historical data, or understand
  materialized views, UDFs, and streaming pipelines. Read-only queries are executed
  through the built-in run_sql tool; write-path SQL (CREATE, INSERT, sinks, UDFs)
  is covered as knowledge for explaining pipelines and drafting SQL the operator
  runs themselves. Covers full Timeplus SQL syntax including window functions,
  JOINs, CTEs, UDFs, data types, aggregations, and all DDL/DML statements.
license: Apache-2.0
compatibility: >
  Adapted for AgentGuard Sentry: SQL executes through the internal read-only
  run_sql tool — no shell, no curl, no environment variables, no direct network
  access to Timeplus.
metadata:
  author: timeplus-io
  version: "1.1.1"
  docs: https://docs.timeplus.com
  github: https://github.com/timeplus-io/proton
  upstream: https://github.com/timeplus-io/AgentSkills/tree/main/timeplus-sql-guide
  adapted-for: AgentGuard Sentry (execution channel rewritten for run_sql; knowledge content kept complete)
  tags:
    - timeplus
    - streaming-sql
    - sql
    - real-time-analytics
    - materialized-views
    - udf
---

# Timeplus Streaming SQL Guide

You are an expert in **Timeplus** — a high-performance real-time streaming analytics
platform built on a streaming SQL engine. You write correct, efficient
Timeplus SQL and execute read-only queries via the built-in `run_sql` tool.

## Quick Reference

| Task | Reference |
|------|-----------|
| Get data in | `references/INGESTION.md` |
| Transform data | `references/TRANSFORMATIONS.md` |
| Send data out | `references/SINKS.md` |
| Full SQL syntax, types, functions | `references/SQL_REFERENCE.md` |
| Random streams (simulated data) | `references/RANDOM_STREAMS.md` |
| Python & JavaScript UDFs | `references/UDFS.md` |
| Python Table Functions | `references/PYTHON_TABLE_FUNCTION.md` |
| EMIT clauses & UDAF emit control | `references/EMIT_AND_UDAF.md` |
| Scheduled tasks (periodic bounded queries) | `references/TASK.md` |
| Real-time alerts | `references/ALERT.md` |

---

## Executing SQL

### The `run_sql` tool

All SQL you execute goes through the `run_sql` tool: one statement per call,
results returned as JSON rows. There is no shell, no curl, and no credential
setup — the tool talks to Timeplus for you.

```
run_sql({"sql": "SELECT count() FROM table(agentguard_cim_event)"})
```

The tool enforces a **read-only, bounded** contract. A query is accepted only if:

- It is a **single statement** starting with `SELECT` or `WITH` (no `;`-separated
  batches, no `DESCRIBE`/`SHOW`/`EXPLAIN` — use the `system.*` tables below instead).
- Every `FROM`/`JOIN` source is **bounded**: `table(stream)` for historical reads,
  a sub-select, a CTE name, or one of the allowlisted system tables
  `system.tables`, `system.columns`, `system.functions`,
  `system.v_failed_mat_views`. A **bare stream name is rejected** — that would be
  an unbounded streaming read.
- It contains none of the forbidden keywords: `INSERT`, `ALTER`, `CREATE`,
  `DROP`, `TRUNCATE`, `RENAME`, `OPTIMIZE`, `GRANT`, `REVOKE`, `KILL`,
  `SETTINGS`, `OUTFILE`, `INFILE`, or a bare `SYSTEM` command.
- `table(...)` wraps a **plain stream name only** — `table(url(...))`,
  `table(file(...))` etc. are rejected.

Additional behavior:

- A `LIMIT` is **injected automatically** (default 100) if you omit one, and an
  oversized `LIMIT` is clamped to the server maximum. Still write explicit,
  sensible `LIMIT`s.
- Queries run under a **timeout**; an expensive full scan may return
  `error: query timed out` — narrow the time range (`_tp_time >= now() - 1h`)
  or aggregate instead of selecting raw rows.
- An **empty result** (`[]`) means no rows matched — it is not an error.
- Failures come back as a result string starting with `error: ...` — read it,
  fix the SQL, and retry.

### Schema discovery

Prefer the dedicated tools: `list_streams` (stream/MV names) and
`describe_stream` (columns and types). Equivalent `run_sql` queries:

```sql
-- List streams (SHOW STREAMS is not accepted; query system.tables)
SELECT name, engine FROM system.tables WHERE database = 'default' LIMIT 100

-- Inspect a stream's columns (DESCRIBE is not accepted; query system.columns)
SELECT name, type FROM system.columns WHERE table = 'sensor_data'

-- Check materialized-view health
SELECT * FROM system.v_failed_mat_views
```

### Write-path SQL is knowledge, not execution

This guide (and its references) keeps the full write path — `CREATE STREAM`,
`INSERT`, materialized views, sinks, random streams, UDFs, scheduled tasks,
alerts — because you need it to **explain existing pipelines and draft SQL for
the operator**. `run_sql` cannot execute any of it. When a task needs DDL or
writes, produce the SQL in a code block for the operator to run in the
Timeplus console, and say clearly that you did not execute it.

---

## Core Concepts

### Streaming vs Historical Queries

```sql
-- STREAMING: Continuous, never ends. Default behavior in Timeplus.
-- (Not executable via run_sql — unbounded.)
SELECT device_id, temperature FROM sensor_data;

-- HISTORICAL: Bounded, returns immediately. Use table().
-- (This is the form run_sql executes.)
SELECT device_id, temperature FROM table(sensor_data) LIMIT 100;

-- HISTORICAL + FUTURE: All past events + all future events
-- (Not executable via run_sql — unbounded.)
SELECT * FROM sensor_data WHERE _tp_time >= earliest_timestamp();
```

Everything you run through `run_sql` is the historical form: wrap the stream
in `table(...)` and bound it with a `WHERE` on `_tp_time` and/or a `LIMIT`.

### ORDER BY and LIMIT on Streaming Queries

A streaming query result is **unbounded** — new rows arrive forever — so the
familiar `ORDER BY` / `LIMIT` semantics from batch SQL do not apply directly:

- **`ORDER BY` is not allowed on a non-windowed streaming query.** Sorting
  requires a finite input. You can only use `ORDER BY` when the upstream is
  bounded: a `table(...)` historical query, or a windowed aggregation
  (`tumble`/`hop`/`session`) where each window closure produces a finite batch
  that gets sorted per window.
- **`LIMIT n` on a streaming query terminates the query after `n` rows.** It is
  not "top n by some order" — it is a hard stop. Pair `LIMIT` with a window
  (`LIMIT n BY window_start`) or use streaming-native top-k aggregates
  (`top_k`, `min_k`, `max_k`) for per-window ranking.
- For historical queries (`SELECT ... FROM table(stream)`), `ORDER BY` and
  `LIMIT` work the normal way.

See `references/TRANSFORMATIONS.md` → **Top-N per Window** for the recommended
patterns.

### The `_tp_time` Column

Every stream has a built-in `_tp_time datetime64(3, 'UTC')` event-time column.
It defaults to ingestion time. You can set a custom event-time column via
`SETTINGS event_time_column='your_column'` when creating the stream.

### Stream Modes

| Mode | Created With | Behavior |
|------|-------------|----------|
| `append` | `CREATE STREAM` (default) | Immutable log, new rows only |
| `versioned_kv` | `+ SETTINGS mode='versioned_kv'` | Latest value per primary key |
| `changelog_kv` | `+ SETTINGS mode='changelog_kv'` | Insert/Update/Delete tracking |
| `mutable` | `CREATE MUTABLE STREAM` | Row-level UPDATE/DELETE (Enterprise) |

---

## Common Patterns

### Pattern 1: Explore then query (run_sql)
```sql
-- 1. Find the stream
SELECT name FROM system.tables WHERE name LIKE '%orders%'

-- 2. Check its schema
SELECT name, type FROM system.columns WHERE table = 'orders'

-- 3. Query historical data
SELECT region, sum(amount) FROM table(orders) GROUP BY region LIMIT 100
```

### Pattern 2: Bounded time-window analysis (run_sql)
```sql
SELECT to_start_of_interval(_tp_time, INTERVAL 1 MINUTE) AS minute,
       region, sum(amount) AS revenue
FROM table(orders)
WHERE _tp_time >= now() - INTERVAL 1 HOUR
GROUP BY minute, region
ORDER BY minute DESC
LIMIT 100
```

### Pattern 3: Window aggregation (streaming — knowledge only)
```sql
-- Continuous per-minute revenue; runs forever. Draft for the operator,
-- not executable via run_sql.
SELECT window_start, region, sum(amount) AS revenue
FROM tumble(orders, 1m)
GROUP BY window_start, region
EMIT AFTER WATERMARK AND DELAY 5s
```

### Pattern 4: Materialized view pipeline (DDL — knowledge only)
```sql
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_revenue_by_region
INTO revenue_by_region AS
SELECT window_start, region, sum(amount) AS total
FROM tumble(orders, 5m)
GROUP BY window_start, region
```

### Pattern 5: Random stream for testing (DDL — knowledge only)
```sql
CREATE RANDOM STREAM IF NOT EXISTS mock_sensors (
  device_id string DEFAULT 'device-' || to_string(rand() % 10),
  temperature float32 DEFAULT 20 + (rand() % 30),
  status string DEFAULT ['ok','warn','error'][rand() % 3 + 1]
) SETTINGS eps=5
```

---

## Error Handling

Common `run_sql` errors and fixes:

| Error | Cause | Fix |
|-------|-------|-----|
| `stream "x" must be read via table(x)` | Bare stream name in FROM/JOIN | Wrap it: `FROM table(x)` |
| `forbidden keyword ...` | DDL/write/`SETTINGS` in the query | run_sql is read-only; drop the keyword or draft the SQL for the operator |
| `query must start with SELECT or WITH` | `SHOW`/`DESCRIBE`/`EXPLAIN`/DDL | Use `system.tables` / `system.columns` instead |
| `only system.tables/columns/functions/v_failed_mat_views are readable` | Other `system.*` table | Stay within the allowlisted four |
| `query timed out` | Expensive scan | Narrow `_tp_time` range, aggregate, lower LIMIT |
| `Unknown column` | Typo or wrong stream | Check `system.columns` or `describe_stream` |
| `Type mismatch` | Wrong data type | Use explicit cast: `cast(val, 'float32')` |
| `Watermark with delay is only supported with streaming window function` | `EMIT ... WITH DELAY` on a plain `GROUP BY` | Drop the EMIT clause (plain GROUP BY already emits on update), or wrap the source in `tumble()`/`hop()`/`session()` |

---

## When to Read Reference Files

Load the relevant reference file when the user's request requires deeper knowledge:

- Creating or modifying **streams, external streams, sources** → `references/INGESTION.md`
- **Window functions, JOINs, CTEs, materialized views, aggregations** → `references/TRANSFORMATIONS.md`
- **Sinks, external tables, Kafka output, webhooks** → `references/SINKS.md`
- **Data types, full function catalog, query settings, all DDL** → `references/SQL_REFERENCE.md`
- **Simulating data, random streams, test data generation** → `references/RANDOM_STREAMS.md`
- **Writing Python UDFs, JavaScript UDFs, remote UDFs, SQL lambdas** → `references/UDFS.md`
- **Python Table Functions** → `references/PYTHON_TABLE_FUNCTION.md`
- **EMIT clause vocabulary, `EMIT AFTER SESSION CLOSE` / `KEY EXPIRE`, event-terminated session windows, UDAF `has_customized_emit`, `MAXSPAN`/`TIMEOUT` constraints** → `references/EMIT_AND_UDAF.md`
- **Scheduled Tasks** → `references/TASK.md`
- **Alerts** → `references/ALERT.md`

Reference files describe the **full** Timeplus SQL surface, including DDL and
write paths that `run_sql` cannot execute — use them to understand and draft
such SQL, then hand it to the operator.
