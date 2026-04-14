# OpenClaw AgentGuard Test Scenarios

Validates that hook events, OTel traces, and OTel metrics are correctly collected in Timeplus after various OpenClaw activity patterns.

---

## Prerequisites

- OpenClaw running via `make start` (port 18789)
- AgentGuard backend running (stream `agentguard_hook_events` exists)
- Timeplus running at `http://localhost:3218` (user `proton`, password `timeplus@t+`)
- At least one model available (e.g. `qwen3:8b` or `kimi-k2.5:cloud` via Ollama)
- Dashboard accessible: run `make url` to get the token URL

### Helper: query Timeplus

```bash
tpq() {
  curl -s -u "proton:timeplus@t+" \
    -X POST "http://localhost:3218/proton/v1/sql" \
    -H "Content-Type: application/json" \
    -d "{\"sql\": \"$1\"}" | python3 -m json.tool
}
```

### Helper: query OTel traces

```bash
tpq "SELECT timestamp, service_name, name, duration_ns FROM table(otel_traces) WHERE service_name='openclaw' ORDER BY timestamp DESC LIMIT 20"
```

### Helper: query hook events

```bash
tpq "SELECT hook_name, event_time, session_id, agent_id, tool_name, provider, model FROM table(agentguard_hook_events) ORDER BY event_time DESC LIMIT 30"
```

---

## Scenario 1 — Basic Conversation (Baseline)

**Purpose:** Verify the core per-turn hook sequence fires correctly for a simple single-turn exchange. This is the baseline — all other scenarios build on it.

**Expected hooks (in order):**
`session_start` → `message_received` → `inbound_claim` → `before_dispatch` → `before_model_resolve` → `before_prompt_build` → `before_agent_start` → `llm_input` → `llm_output` → `before_agent_reply` → `before_message_write` → `message_sending` → `message_sent` → `agent_end`

**Steps:**

1. Open the OpenClaw dashboard (get URL via `make url`).
2. Start a new chat session with the default agent.
3. Send the message:
   ```
   What is 2 + 2? Reply in one sentence.
   ```
4. Wait for the reply.
5. Wait 3 seconds for the batch to flush.

**Verify hook events:**
```sql
SELECT hook_name, event_time, session_id, agent_id, provider, model
FROM table(agentguard_hook_events)
WHERE event_time > now() - interval 2 minute
ORDER BY event_time ASC
```

Expected: at least one row each for `session_start`, `message_received`, `llm_input`, `llm_output`, `agent_end`.

**Verify OTel traces:**
```sql
SELECT name, duration_ns, timestamp
FROM table(otel_traces)
WHERE service_name = 'openclaw'
  AND timestamp > now() - interval 2 minute
ORDER BY timestamp DESC
LIMIT 10
```

Expected: spans for the agent turn appear with non-zero `duration_ns`.

**Verify OTel metrics:**
```sql
SELECT metric_name, value, timestamp
FROM table(otel_metrics_gauge)
WHERE service_name = 'openclaw'
ORDER BY timestamp DESC
LIMIT 10
```

Expected: gateway process metrics (memory, event loop, etc.) are present.

---

## Scenario 2 — Multi-Turn Conversation with Tool Calls (File System)

**Purpose:** Exercise `before_tool_call`, `after_tool_call`, and `tool_result_persist` hooks across multiple turns with real tool invocations. The coding tool profile enables bash and file tools.

**Steps:**

1. Start a new chat session (or `/reset` to clear context).
2. Send:
   ```
   Create a file called hello.txt in my workspace with the text "Hello from AgentGuard test". Then read it back and confirm its contents.
   ```
3. Wait for completion (expect 2–3 tool calls: write, then read).
4. Send a follow-up:
   ```
   Now append the current date and time to the same file, then show me the full file contents.
   ```
5. Wait 3 seconds after completion.

**Verify tool hooks fired:**
```sql
SELECT hook_name, tool_name, event_time, session_id
FROM table(agentguard_hook_events)
WHERE hook_name IN ('before_tool_call', 'after_tool_call', 'tool_result_persist')
  AND event_time > now() - interval 5 minute
ORDER BY event_time ASC
```

Expected: `before_tool_call` / `after_tool_call` pairs for each tool invocation (e.g. `write_file`, `read_file`, `bash`). `tool_name` column populated.

**Verify event_data contains tool parameters:**
```sql
SELECT hook_name, tool_name, event_data
FROM table(agentguard_hook_events)
WHERE hook_name = 'before_tool_call'
  AND event_time > now() - interval 5 minute
```

Expected: `event_data` JSON blob contains the tool call parameters (file path, content, etc.).

**Verify multi-turn session continuity:**
```sql
SELECT hook_name, count() as cnt
FROM table(agentguard_hook_events)
WHERE event_time > now() - interval 5 minute
GROUP BY hook_name
ORDER BY cnt DESC
```

Expected: `llm_input` / `llm_output` count >= 2 (one per turn), `before_tool_call` count >= 3.

---

## Scenario 3 — Sub-Agent Spawn Chain

**Purpose:** Exercise all four subagent hooks (`subagent_spawning`, `subagent_delivery_target`, `subagent_spawned`, `subagent_ended`) and verify the `childSessionKey`/`requesterSessionKey` relationship is captured in `event_data`.

OpenClaw spawns sub-agents via the `sessions_spawn` internal tool when prompted to delegate a task. The orchestrator agent must have sub-agent spawning in scope (it does by default at depth 0).

**Steps:**

1. Start a new chat session.
2. Send:
   ```
   I want you to act as an orchestrator. Spawn a sub-agent to do the following task independently:
   "Count the number of words in the sentence: 'The quick brown fox jumps over the lazy dog'. Return only the number."
   Use the sessions_spawn tool to delegate this task and report back the result when the sub-agent finishes.
   ```
3. Wait for the orchestrator to complete (it will spawn a sub-agent, wait, then report the result back).
4. Wait 3 seconds for flush.

**Verify subagent hooks:**
```sql
SELECT hook_name, event_time, session_id, session_key, event_data
FROM table(agentguard_hook_events)
WHERE hook_name IN ('subagent_spawning', 'subagent_delivery_target', 'subagent_spawned', 'subagent_ended')
  AND event_time > now() - interval 5 minute
ORDER BY event_time ASC
```

Expected: four rows in sequence — `subagent_spawning` → `subagent_delivery_target` → `subagent_spawned` → `subagent_ended`.

**Verify parent/child relationship in event_data:**
```sql
SELECT
  hook_name,
  json_extract_string(event_data, '$._ctx.childSessionKey') AS child_session,
  json_extract_string(event_data, '$._ctx.requesterSessionKey') AS parent_session
FROM table(agentguard_hook_events)
WHERE hook_name = 'subagent_spawning'
  AND event_time > now() - interval 5 minute
```

Expected: `child_session` contains `agent:main:subagent:<uuid>`, `parent_session` contains the orchestrator's session key.

**Verify both sessions have their own hook events:**
```sql
SELECT session_key, hook_name, count() as cnt
FROM table(agentguard_hook_events)
WHERE event_time > now() - interval 5 minute
GROUP BY session_key, hook_name
ORDER BY session_key, hook_name
```

Expected: two distinct `session_key` values — one for the orchestrator, one for the sub-agent — each with their own `llm_input`, `llm_output`, `agent_end` events.

---

## Scenario 4 — Scheduled (Cron) Task

**Purpose:** Exercise `gateway_start`-adjacent hooks and verify that cron-triggered isolated sessions generate their own complete hook sequences (including `session_start`/`session_end`) independently from any interactive session.

**Steps:**

1. From the terminal, add a one-shot cron job that fires in 2 minutes using the gateway's CLI:
   ```bash
   docker exec openclaw node dist/index.js cron add \
     --at "2m" \
     --session isolated \
     --label "agentguard-test-cron" \
     "List the files in the current directory and count them. Reply with only the count."
   ```
2. Wait 2 minutes (or slightly longer). The job fires in its own isolated session (`cron:<jobId>`).
3. Wait 3 seconds after expected fire time.

**Verify isolated cron session appeared:**
```sql
SELECT hook_name, session_key, event_time, agent_id
FROM table(agentguard_hook_events)
WHERE session_key LIKE 'cron:%'
ORDER BY event_time ASC
```

Expected: rows with `session_key` starting with `cron:` containing the full turn sequence including `session_start`, `llm_input`, `llm_output`, `agent_end`, `session_end`.

**Verify cron session is separate from interactive sessions:**
```sql
SELECT
  multiIf(session_key LIKE 'cron:%', 'cron', session_key LIKE 'agent:%', 'interactive', 'other') AS session_type,
  count() AS event_count,
  min(event_time) AS first_event,
  max(event_time) AS last_event
FROM table(agentguard_hook_events)
WHERE event_time > now() - interval 10 minute
GROUP BY session_type
```

Expected: both `cron` and `interactive` rows present with distinct event counts.

**Verify OTel trace spans for cron turn:**
```sql
SELECT name, duration_ns, timestamp
FROM table(otel_traces)
WHERE service_name = 'openclaw'
  AND timestamp > now() - interval 10 minute
ORDER BY timestamp DESC
LIMIT 20
```

Expected: new spans appear at the cron fire time, distinct from the earlier interactive session spans.

**Cleanup:**
```bash
docker exec openclaw node dist/index.js cron list
docker exec openclaw node dist/index.js cron remove <jobId>
```

---

## Scenario 5 — Complex Multi-Step Coding Task (Context Compaction)

**Purpose:** Exercise `before_compaction`/`after_compaction` hooks by running a long multi-step conversation that accumulates enough history to trigger context compaction. Also exercises repeated `before_tool_call`/`after_tool_call` cycles and the `before_prompt_build` hook on every turn.

Context compaction fires automatically when the session token count approaches the model's context limit, or can be triggered manually with `/compact`.

**Steps:**

1. Start a new chat session.
2. Send a sequence of prompts one after another, waiting for each reply before sending the next:

   **Turn 1:**
   ```
   Create a Python file called fizzbuzz.py in my workspace that prints FizzBuzz from 1 to 100.
   ```

   **Turn 2:**
   ```
   Now add a function called `fizzbuzz_range(start, end)` that takes a range and returns a list of FizzBuzz strings instead of printing them.
   ```

   **Turn 3:**
   ```
   Write a second Python file called test_fizzbuzz.py with 5 unit tests for the fizzbuzz_range function using unittest. Run the tests and show me the output.
   ```

   **Turn 4:**
   ```
   Refactor fizzbuzz.py to also support a custom divisor pair — e.g. fizzbuzz_range(1, 20, fizz_div=3, buzz_div=5). Update the tests to cover the new parameter.
   ```

   **Turn 5:**
   ```
   Add a command-line interface to fizzbuzz.py using argparse so it can be run as `python fizzbuzz.py --start 1 --end 50 --fizz-div 3 --buzz-div 5`. Test it by running the script with those arguments.
   ```

3. After turn 5, manually trigger compaction:
   ```
   /compact
   ```
4. Send one more turn to confirm the agent still works after compaction:
   ```
   What files have we created in this session? List them.
   ```
5. Wait 3 seconds.

**Verify compaction hooks:**
```sql
SELECT hook_name, event_time, session_id
FROM table(agentguard_hook_events)
WHERE hook_name IN ('before_compaction', 'after_compaction')
  AND event_time > now() - interval 20 minute
ORDER BY event_time ASC
```

Expected: `before_compaction` followed by `after_compaction` pair.

**Verify tool call volume:**
```sql
SELECT tool_name, count() AS call_count
FROM table(agentguard_hook_events)
WHERE hook_name = 'before_tool_call'
  AND event_time > now() - interval 20 minute
GROUP BY tool_name
ORDER BY call_count DESC
```

Expected: tool calls across multiple tool names (file writes, reads, bash executions) with total count >= 10.

**Verify full turn count:**
```sql
SELECT count() AS llm_turns
FROM table(agentguard_hook_events)
WHERE hook_name = 'llm_input'
  AND event_time > now() - interval 20 minute
```

Expected: at least 6 turns (5 conversation turns + 1 post-compaction turn).

**Verify post-compaction turn still has full hook sequence:**
```sql
SELECT hook_name, event_time
FROM table(agentguard_hook_events)
WHERE event_time > (
  SELECT max(event_time) FROM table(agentguard_hook_events)
  WHERE hook_name = 'after_compaction'
    AND event_time > now() - interval 20 minute
)
ORDER BY event_time ASC
LIMIT 10
```

Expected: `before_agent_start`, `llm_input`, `llm_output`, `agent_end` appear after the `after_compaction` timestamp.

---

## Scenario 6 — GitHub Issues Skill (Sub-Agent Fan-Out)

**Purpose:** Exercise the `gh-issues` skill which spawns one sub-agent per GitHub issue — a fan-out pattern that generates multiple concurrent sub-agent lifecycle sequences. Requires `gh` CLI authenticated in the workspace, or a public repo with open issues.

**Steps:**

1. Ensure the `gh` CLI is available and authenticated in the OpenClaw workspace, or use a public repo.
2. Start a new chat session.
3. Send:
   ```
   Use the gh-issues skill to fetch the 3 most recently opened issues from the public repo `cli/cli` with label `bug`. For each issue, spawn a sub-agent to summarize the issue in one sentence. Use --limit 3 --label bug.
   ```
4. Wait for all three sub-agents to complete and the orchestrator to report results.
5. Wait 3 seconds.

**Verify fan-out: multiple sub-agent spawn sequences:**
```sql
SELECT
  hook_name,
  count() AS cnt,
  count(DISTINCT json_extract_string(event_data, '$._ctx.childSessionKey')) AS unique_children
FROM table(agentguard_hook_events)
WHERE hook_name IN ('subagent_spawning', 'subagent_spawned', 'subagent_ended')
  AND event_time > now() - interval 15 minute
GROUP BY hook_name
```

Expected: `cnt = 3` and `unique_children = 3` for each subagent hook type.

**Verify each child session has its own LLM turn:**
```sql
SELECT session_key, count() AS turns
FROM table(agentguard_hook_events)
WHERE hook_name = 'llm_input'
  AND event_time > now() - interval 15 minute
GROUP BY session_key
ORDER BY turns DESC
```

Expected: at least 4 rows — 1 orchestrator session + 3 child sessions.

**Verify tool calls inside sub-agents:**
```sql
SELECT session_key, tool_name, count() AS cnt
FROM table(agentguard_hook_events)
WHERE hook_name = 'before_tool_call'
  AND event_time > now() - interval 15 minute
GROUP BY session_key, tool_name
ORDER BY session_key
```

Expected: `gh` or `bash` tool calls appear in the sub-agent session keys.

---

## Aggregate Validation Queries

Run these after completing all scenarios to get a holistic view.

### Hook coverage summary

```sql
SELECT hook_name, count() AS total_events
FROM table(agentguard_hook_events)
GROUP BY hook_name
ORDER BY total_events DESC
```

Expected: at least 20 distinct hook types covered.

### Events per session (all scenarios combined)

```sql
SELECT session_key, count() AS events, min(event_time) AS first, max(event_time) AS last
FROM table(agentguard_hook_events)
GROUP BY session_key
ORDER BY first ASC
```

### OTel trace span count by scenario window

```sql
SELECT
  toStartOfMinute(timestamp) AS minute,
  count() AS spans,
  avg(duration_ns) / 1e6 AS avg_duration_ms
FROM table(otel_traces)
WHERE service_name = 'openclaw'
GROUP BY minute
ORDER BY minute ASC
```

### OTel logs volume

```sql
SELECT
  toStartOfMinute(timestamp) AS minute,
  severity_text,
  count() AS log_count
FROM table(otel_logs)
WHERE service_name = 'openclaw'
GROUP BY minute, severity_text
ORDER BY minute ASC
```

### Model usage breakdown

```sql
SELECT model, provider, count() AS turns
FROM table(agentguard_hook_events)
WHERE hook_name = 'llm_input'
GROUP BY model, provider
ORDER BY turns DESC
```

---

## Notes

- **Flush delay**: All hook events are batched. Wait at least 3 seconds (configured `flushMs: 2000`) after activity before querying.
- **No events?** Check `docker logs openclaw 2>&1 | grep agentguard` — look for the `[agentguard] active` line and absence of `Failed to flush` errors.
- **Cron job timing**: The `--at "2m"` syntax is relative to gateway time inside the container. If the system clock differs, adjust accordingly.
- **Model availability**: All test prompts are model-agnostic. If a model returns an error, OpenClaw will still fire the hook sequence up to the failure point — `llm_output` may be absent if the LLM call fails.
- **Context compaction** (Scenario 5): If the model's context window is large (e.g. kimi-k2.5 at 262k), compaction may not trigger automatically. Use `/compact` explicitly as described.
- **Sub-agent depth limit**: By default `maxSpawnDepth: 1` means sub-agents cannot themselves spawn sub-agents. Scenarios 3 and 6 operate within this limit (depth-0 orchestrator spawns depth-1 workers).
