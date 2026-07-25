# Agents, Sessions, Runs, Events, Activity

Everything here reads `agentguard_hook_events` — the raw hook stream. An
**agent** is a distinct `(agent_id, deployment_id)` pair derived from events;
there is no agents stream. The UI's composite agent id is
`<deployment_id>__<agent_id>`.

## Columns (agentguard_hook_events)

`hook_name`, `event_time`, `session_id`, `run_id`, `agent_id`,
`deployment_id`, `deployment_name`, `session_key`, `tool_name`, `provider`,
`model`, `hook_decision`, `block_reason`, `event_data`, `agent_type`

## Hook-name vocabulary (two naming schemes coexist)

| Meaning | hook_name values |
|---------|------------------|
| LLM call completed | `llm_output`, `post_llm_call` |
| Tool call about to run | `before_tool_call`, `pre_tool_call` (activity feed also matches `PreToolUse`) |
| Session ended | `session_end`, `on_session_end` |

Always match both spellings with `hook_name IN (...)`.

## Session filtering rules (apply to every session-level query)

A "real" session excludes noise:
- `session_key NOT LIKE 'temp:%'`
- `session_id NOT LIKE 'slug-generator-%'`
- has at least one non-empty `run_id`: `count_if(run_id != '') > 0`

A session is **active** if it has no session-end hook; **ended** otherwise.

## List agents (aggregate + session rollup)

```sql
SELECT agent_id, deployment_id, deployment_name, agent_type, last_seen, event_count,
       llm_call_count, coalesce(session_count, 0) AS session_count,
       coalesce(active_session_count, 0) AS active_session_count
FROM (
  SELECT agent_id, deployment_id, any(deployment_name) AS deployment_name,
         any(agent_type) AS agent_type, max(event_time) AS last_seen,
         count() AS event_count,
         count_if(hook_name IN ('llm_output', 'post_llm_call')) AS llm_call_count
  FROM table(agentguard_hook_events) WHERE agent_id != ''
  GROUP BY agent_id, deployment_id
) AS agents
LEFT JOIN (
  SELECT agent_id, deployment_id, count() AS session_count,
         sum(is_active) AS active_session_count
  FROM (
    SELECT agent_id, deployment_id, session_id,
           if(count_if(hook_name IN ('session_end', 'on_session_end')) = 0, 1, 0) AS is_active
    FROM table(agentguard_hook_events) WHERE session_id != ''
    GROUP BY agent_id, deployment_id, session_id
    HAVING any_if(session_key, session_key != '') NOT LIKE 'temp:%'
       AND session_id NOT LIKE 'slug-generator-%'
       AND count_if(run_id != '') > 0
  ) GROUP BY agent_id, deployment_id
) AS s USING (agent_id, deployment_id)
ORDER BY last_seen DESC
```

Per-agent token totals come from the metrics stream (keep it a separate
query):

```sql
SELECT agent_id, deployment_id,
       to_int64(sum_if(metric_value, metric_name = 'llm.total_tokens')) AS total_tokens
FROM table(agentguard_cim_metrics)
WHERE metric_name = 'llm.total_tokens'
GROUP BY agent_id, deployment_id
```

## Sessions of one agent

```sql
SELECT session_id,
       any_if(session_key, session_key != '') AS session_key,
       any_if(model, model != '') AS model,
       any_if(provider, provider != '') AS provider,
       count_distinct(if(run_id != '', run_id, null)) AS run_count,
       count() AS event_count,
       min(event_time) AS first_seen, max(event_time) AS last_seen,
       count_if(hook_name IN ('session_end', 'on_session_end')) AS ended_event_count
FROM table(agentguard_hook_events)
WHERE deployment_id = '<dep>' AND agent_id = '<agent>' AND session_id != ''
GROUP BY session_id
HAVING session_key NOT LIKE 'temp:%' AND session_id NOT LIKE 'slug-generator-%'
   AND count_if(run_id != '') > 0
ORDER BY last_seen DESC
```

`ended_event_count = 0` → session still active. Note: **model/provider are
session-level properties** (an agent can use different models across
sessions).

## Runs within one session

```sql
SELECT run_id, min(event_time) AS started, max(event_time) AS ended,
       count() AS event_count,
       count_if(hook_name IN ('before_tool_call', 'pre_tool_call')) AS tool_count,
       count_if(hook_decision = 'block') AS block_count
FROM table(agentguard_hook_events)
WHERE session_id = '<session>' AND run_id != ''
GROUP BY run_id ORDER BY started ASC
```

## Raw events (drill-down)

```sql
SELECT hook_name, event_time, run_id, session_id, tool_name, provider, model,
       hook_decision, block_reason, agent_type
FROM table(agentguard_hook_events)
WHERE session_id = '<session>'
ORDER BY event_time ASC LIMIT 100
```

`event_data` holds the raw payload JSON — select it only when you need it
(large, arbitrary content). `hook_decision = 'block'` marks blocked actions.

## Recent tool activity across all sessions

```sql
SELECT session_id, run_id, tool_name, event_time, agent_id
FROM table(agentguard_hook_events)
WHERE hook_name IN ('before_tool_call', 'PreToolUse')
ORDER BY event_time DESC LIMIT 50
```

The Activity page merges this with pending/decided holds — see `HOLDS.md`.

## Normalized alternative: agentguard_cim_event

For lifecycle-shaped questions the normalized stream is simpler. Columns:
`event_time`, `event_type`, `agent_type`, `agent_id`, `deployment_id`,
`session_id`, `run_id`, `provider`, `model`, `tool_name`, `tool_input`,
`tool_result`, `tool_success`, `tool_duration_ms`, `user_message`,
`assistant_message`, `hook_decision`, `block_reason`.

`event_type` values: `session_start`, `session_end`, `user_input`,
`llm_response`, `tool_invoke`, `tool_complete`, `prompt_context`,
`llm_context`, `other`.

```sql
-- What did the user ask and the agent answer in a session?
SELECT event_time, event_type, user_message, assistant_message, tool_name
FROM table(agentguard_cim_event)
WHERE session_id = '<session>'
  AND event_type IN ('user_input', 'llm_response', 'tool_complete')
ORDER BY event_time ASC LIMIT 200
```

For an ordered per-session waterfall the curated `get_session_timeline` tool
already does this (with span-building logic SQL can't express) — prefer it.
