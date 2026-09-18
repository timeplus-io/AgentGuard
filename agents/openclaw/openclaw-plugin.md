# AgentGuard OpenClaw Plugin

Real-time security event collection for OpenClaw AI agents via the OpenClaw plugin hook system.

---

## Overview

The AgentGuard OpenClaw plugin is a TypeScript npm package (`@agentguard/openclaw-plugin`) that intercepts all 29 OpenClaw lifecycle hook points and streams each event to a Timeplus stream (`agentguard_hook_events`) via the Proton Ingest REST API. This gives the AgentGuard backend a real-time feed of every security-relevant action taken by any AI agent running through the OpenClaw gateway.

**Phase 1 (current):** observe-only — every hook captures and forwards data; no blocking or mutation.

**Phase 2 (planned):** selected hooks enforce security policy by blocking, mutating, or requiring approval.

---

## Repository Layout

```
agents/openclaw/agentguard-plugin/
├── package.json           # name: "@agentguard/openclaw-plugin", type: "module"
├── tsconfig.json          # ESNext, moduleResolution: bundler
├── vitest.config.ts       # test runner config
├── src/
│   ├── index.ts           # OpenClawPluginDefinition default export
│   ├── config.ts          # resolveConfig() — merges openclaw.json config + env vars
│   ├── timeplus-client.ts # TimeplusClient — batch buffer + Proton Ingest POST
│   └── hooks.ts           # registerAllHooks(api, client) for all 29 hooks
└── dist/                  # compiled output (tsc), committed for Docker volume mount
```

---

## Configuration

Config is resolved by merging two sources, with **environment variables taking precedence** over `openclaw.json` plugin config.

### Default values

| Field | Default |
|---|---|
| `timeplusUrl` | `http://localhost:3218` |
| `ingestApi` | `timeplusd` (`rest` = Timeplus REST API on port 8000, same username/password) |
| `apiKey` | `` (empty) — optional API key alternative to Basic auth in `rest` mode; implies `rest` |
| `workspace` | `default` — REST API workspace id |
| `stream` | `agentguard_hook_events` |
| `flushMs` | `2000` |
| `batchSize` | `50` |
| `username` | `default` |
| `password` | `` (empty) |

All fields are optional. The plugin works out of the box with zero configuration.

### openclaw.json plugin config

```json
{
  "plugins": {
    "entries": {
      "agentguard": {
        "enabled": true,
        "path": "/home/node/.openclaw/agentguard-plugin/dist/index.js",
        "config": {
          "timeplusUrl": "http://host.docker.internal:3218",
          "stream": "agentguard_hook_events",
          "flushMs": 2000,
          "batchSize": 50
        }
      }
    }
  }
}
```

### Environment variable overrides

| Env var | Field |
|---|---|
| `AGENTGUARD_TIMEPLUS_URL` | `timeplusUrl` |
| `AGENTGUARD_INGEST_API` | `ingestApi` |
| `AGENTGUARD_API_KEY` | `apiKey` |
| `AGENTGUARD_WORKSPACE` | `workspace` |
| `AGENTGUARD_STREAM` | `stream` |
| `AGENTGUARD_FLUSH_MS` | `flushMs` |
| `AGENTGUARD_BATCH_SIZE` | `batchSize` |
| `AGENTGUARD_USERNAME` | `username` |
| `AGENTGUARD_PASSWORD` | `password` |

---

## Timeplus Stream Schema

The stream is pre-created by the AgentGuard backend (`timeplus.Init()`) on startup — the plugin assumes it exists. Common fields are top-level typed columns; hook-specific payload is stored as a JSON blob in `event_data`.

```sql
CREATE STREAM agentguard_hook_events
(
  hook_name     low_cardinality(string),  -- e.g. 'before_tool_call'
  event_time    datetime64(3),            -- wall-clock, millisecond precision
  session_id    string,
  run_id        string,
  agent_id      string,
  session_key   string,
  tool_name     string,                   -- tool hooks only, else empty
  provider      string,                   -- llm hooks only, else empty
  model         string,                   -- llm hooks only, else empty
  hook_decision low_cardinality(string),  -- 'observe' in Phase 1
  block_reason  string,                   -- empty in Phase 1
  event_data    string                    -- full event as JSON
)
```

Future views and materialized views can extract fields from `event_data` per hook type without schema changes.

---

## Event Transport

### Ingest flow

```
enqueue(event)
  └── push to in-memory queue
      ├── if queue.length >= batchSize → flush() immediately (async, non-blocking)
      └── background timer fires every flushMs → flush()

flush()
  └── snapshot + clear queue atomically (splice before first await)
      └── POST /proton/v1/ingest/streams/{stream}
          Content-Type: application/json
          { "columns": [...], "data": [[row], [row], ...] }
          ├── 2xx → done
          └── failure → retry once immediately
              └── still failing → log error, drop batch (never throw)

close()    ← called only from gateway_stop
  └── clearInterval(timer)
  └── await flush()   (drain remaining events before shutdown)
```

Every hook handler calls `client.enqueue(event)` and returns immediately — no `await`, never blocks the agent.

### Ingest payload format

```json
{
  "columns": [
    "hook_name", "event_time", "session_id", "run_id", "agent_id", "session_key",
    "tool_name", "provider", "model", "hook_decision", "block_reason", "event_data"
  ],
  "data": [
    ["before_tool_call", "2026-04-07T09:00:00.000Z", "sess-abc", "run-xyz",
     "main", "agent:main:main", "bash", "", "", "observe", "", "{\"params\":{\"cmd\":\"ls\"}}"]
  ]
}
```

---

## Hook Coverage

All 29 OpenClaw plugin hooks are registered. In Phase 1 all handlers observe-only — they capture the event and return `undefined` (pass-through).

### Tier 1 — Agent context hooks

| Hook | Extra columns populated |
|---|---|
| `llm_input` | `provider`, `model`, `run_id`, `session_id` (from event) |
| `llm_output` | `provider`, `model`, `run_id`, `session_id` (from event) |
| `before_model_resolve` | — |
| `before_prompt_build` | — |
| `before_agent_start` | — |
| `before_agent_reply` | — |
| `agent_end` | — |
| `before_compaction` | — |
| `after_compaction` | — |
| `before_reset` | — |

### Tier 2 — Tool hooks

| Hook | Extra columns populated |
|---|---|
| `before_tool_call` | `tool_name` (from ctx) |
| `after_tool_call` | `tool_name` (from ctx) |
| `tool_result_persist` | `tool_name` (from ctx, optional) |

`before_tool_call` returns `undefined` in Phase 1. In Phase 2 it will return a policy decision `{ block, blockReason, requireApproval }`.

### Tier 3 — Message, session, gateway hooks

`message_received`, `message_sending`, `message_sent`, `session_start`, `session_end`, `inbound_claim`, `before_dispatch`, `reply_dispatch`, `before_message_write`, `subagent_spawning`, `subagent_delivery_target`, `subagent_spawned`, `subagent_ended`, `gateway_start`, `gateway_stop`, `before_install`

All capture `event_data` as a JSON blob. Subagent hooks additionally capture `childSessionKey` and `requesterSessionKey` from the hook context into `event_data` for security traceability of agent-to-agent relationships.

`gateway_stop` is the only handler that calls `await client.close()` to drain buffered events before the process exits.

---

## Phase 2 Enforcement Candidates

| Hook | Threat mitigated |
|---|---|
| `before_tool_call` | Dangerous commands, data exfiltration via tool params |
| `before_install` | Malicious plugin/skill installation |
| `subagent_spawning` | Rogue agent-to-agent attack chains |
| `before_prompt_build` | Prompt injection in system prompt |
| `inbound_claim` | Forged inbound messages |
| `message_sending` | Data exfiltration via outbound messages |

---

## Running Locally

### Prerequisites

- OpenClaw running via `agents/openclaw/Makefile`
- AgentGuard backend running (creates the Timeplus stream on startup)
- Timeplus Proton accessible at `http://host.docker.internal:3218`

### Start OpenClaw with the plugin

```bash
cd agents/openclaw
make start
```

The Makefile mounts the plugin directory into the container:

```
-v "$(PWD)/agentguard-plugin":/home/node/.openclaw/agentguard-plugin
```

### Verify the plugin loaded

```bash
docker logs openclaw 2>&1 | grep agentguard
```

Expected:

```
[agentguard] active — streaming to http://host.docker.internal:3218/timeplusd/v2/ingest/ag/streams/agentguard_hook_events (timeplusd) (flush every 2000ms, batch size 50)
```

### Query events in Timeplus

After sending a message in OpenClaw, wait ~2s for the batch to flush, then query:

```sql
SELECT hook_name, event_time, session_id, agent_id, tool_name
FROM table(ag.agentguard_hook_events)
ORDER BY event_time DESC
LIMIT 20
```

Or via the HTTP API:

```bash
curl -s -X POST "http://localhost:3218/proton/v1/sql" \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT hook_name, event_time, session_id, agent_id FROM table(agentguard_hook_events) ORDER BY event_time DESC LIMIT 20"}'
```

### Rebuild after source changes

The compiled `dist/` is committed alongside `src/`. After editing source files, rebuild before restarting OpenClaw:

```bash
cd agents/openclaw/agentguard-plugin
npm run build
cd ..
make start
```

---

## Development

### Run tests

```bash
cd agents/openclaw/agentguard-plugin
npm test
```

20 tests across three suites:

- `config.test.ts` — 6 tests: defaults, pluginConfig override, env var override, numeric type coercion, empty-string fallback, NaN fallback
- `timeplus-client.test.ts` — 9 tests: no immediate POST on enqueue, correct URL/payload, idempotent double-flush, batch-size trigger, retry-once, never-throws, close() drains, multi-event batch, timer auto-flush
- `hooks.test.ts` — 5 tests: `baseEvent()` helper (hook_name, event_time, ctx fields, hook_decision/block_reason, event_data defaults)

### Build

```bash
npm run build   # outputs to dist/
```

---

## Backend Integration

`backend/internal/timeplus/init.go` — `Init()` calls `createAgentGuardStreams()` after the OTel streams are created. The DDL uses `IF NOT EXISTS` semantics via the `execIfNotExists` helper, so restarts are idempotent.

The stream name (`agentguard_hook_events`) is the same string in three places — keep them in sync if renaming:

1. Backend DDL constant `ddlAgentGuardHookEvents` in `init.go`
2. Plugin default config `DEFAULTS.stream` in `config.ts`
3. `openclaw.json` plugin config `stream` field
