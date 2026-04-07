# AgentShield OpenClaw Plugin — Design Spec

**Date:** 2026-04-06
**Feature:** FR-6 — OpenClaw Hook Event Collection
**Phase:** 1 (observe only — no blocking)

---

## 1. Overview

The AgentShield plugin is an OpenClaw plugin written in TypeScript that intercepts all 29 lifecycle hook points, serialises each event, and ships it to a Timeplus stream via the Proton Ingest REST API. This gives the AgentShield backend a real-time stream of every security-relevant action taken by any AI agent running through the OpenClaw gateway.

Phase 1 (this spec): **observe only** — every hook captures and forwards data; no blocking or mutation.
Phase 2 (future): selected hooks enforce security policy by blocking, mutating, or requiring approval.

---

## 2. Repository Layout

```
agents/openclaw/agentshield-plugin/
├── package.json           # name: "@agentshield/openclaw-plugin", type: "module"
├── tsconfig.json          # ESNext, moduleResolution: bundler
├── src/
│   ├── index.ts           # OpenClawPluginDefinition default export
│   ├── config.ts          # resolveConfig() — merges openclaw.json config + env vars
│   ├── timeplus-client.ts # TimeplusClient — batch buffer + Proton Ingest POST
│   └── hooks.ts           # registerAllHooks(api, client)
└── dist/                  # compiled output (tsc), gitignored
```

The plugin is structured as a proper npm package from day one so it can be published via `npm publish` without structural changes. Local loading during development uses the OpenClaw plugin directory config in `openclaw.json`.

---

## 3. Plugin Registration

OpenClaw plugins export an `OpenClawPluginDefinition` object. Hooks are registered via `api.on(hookName, handler)` inside `activate()`.

```ts
// src/index.ts
// Note: exact SDK import path to be confirmed during implementation
// (OpenClaw uses a subpath alias resolution system — likely "openclaw/plugin-sdk")
import type { OpenClawPluginDefinition } from "openclaw/plugin-sdk";
import { resolveConfig } from "./config.js";
import { createTimeplusClient } from "./timeplus-client.js";
import { registerAllHooks } from "./hooks.js";

const plugin: OpenClawPluginDefinition = {
  id: "agentshield",
  name: "AgentShield Security Monitor",
  version: "0.1.0",
  description: "Streams all OpenClaw lifecycle events to Timeplus for security monitoring.",

  activate: async (api) => {
    const config = resolveConfig(api.pluginConfig);
    const client = createTimeplusClient(config);
    registerAllHooks(api, client);
    api.logger.info(`AgentShield active — streaming to ${config.timeplusUrl}/${config.stream}`);
  },
};

export default plugin;
```

---

## 4. Configuration

Config is resolved by merging two sources, with **env vars taking precedence** over `openclaw.json` plugin config.

### 4.1 Default values

| Field | Default |
|---|---|
| `timeplusUrl` | `http://localhost:3218` |
| `stream` | `agentshield_hook_events` |
| `flushMs` | `2000` |
| `batchSize` | `50` |

All fields are optional. The plugin works out of the box with zero configuration.

### 4.2 openclaw.json plugin config

```json
{
  "plugins": {
    "entries": {
      "agentshield": {
        "enabled": true,
        "config": {
          "timeplusUrl": "http://host.docker.internal:3218",
          "stream": "agentshield_hook_events",
          "flushMs": 2000,
          "batchSize": 50
        }
      }
    }
  }
}
```

### 4.3 Environment variable overrides

| Env var | Overrides |
|---|---|
| `AGENTSHIELD_TIMEPLUS_URL` | `timeplusUrl` |
| `AGENTSHIELD_STREAM` | `stream` |
| `AGENTSHIELD_FLUSH_MS` | `flushMs` |
| `AGENTSHIELD_BATCH_SIZE` | `batchSize` |

### 4.4 resolveConfig()

```ts
// src/config.ts
export type AgentShieldConfig = {
  timeplusUrl: string;
  stream: string;
  flushMs: number;
  batchSize: number;
};

const DEFAULTS: AgentShieldConfig = {
  timeplusUrl: "http://localhost:3218",
  stream:      "agentshield_hook_events",
  flushMs:     2000,
  batchSize:   50,
};

export function resolveConfig(pluginConfig?: Record<string, unknown>): AgentShieldConfig {
  return {
    timeplusUrl: process.env.AGENTSHIELD_TIMEPLUS_URL
                 ?? (pluginConfig?.timeplusUrl as string)
                 ?? DEFAULTS.timeplusUrl,
    stream:      process.env.AGENTSHIELD_STREAM
                 ?? (pluginConfig?.stream as string)
                 ?? DEFAULTS.stream,
    flushMs:     Number(process.env.AGENTSHIELD_FLUSH_MS
                 ?? pluginConfig?.flushMs
                 ?? DEFAULTS.flushMs),
    batchSize:   Number(process.env.AGENTSHIELD_BATCH_SIZE
                 ?? pluginConfig?.batchSize
                 ?? DEFAULTS.batchSize),
  };
}
```

---

## 5. Timeplus Stream Schema

The stream is **pre-created by the AgentShield backend** (`timeplus.Init()`) — the plugin assumes it exists. Common fields are top-level typed columns; hook-specific payload is stored as a JSON blob in `event_data`.

```sql
CREATE STREAM agentshield_hook_events
(
  hook_name     low_cardinality(string),  -- e.g. 'before_tool_call'
  event_time    datetime64(3),            -- wall-clock, ms precision
  session_id    string,
  run_id        string,
  agent_id      string,
  session_key   string,
  tool_name     string,                   -- tool hooks only, else empty
  provider      string,                   -- llm hooks only, else empty
  model         string,                   -- llm hooks only, else empty
  hook_decision low_cardinality(string),  -- 'observe' in phase 1
  block_reason  string,                   -- empty in phase 1
  event_data    string                    -- full event as JSON
)
```

Future views/materialized views can extract fields from `event_data` per hook type without schema changes.

---

## 6. Ingest Transport — TimeplusClient

### 6.1 Behaviour

```
enqueue(event)
  └── push to in-memory queue
      ├── if queue.length >= batchSize → flush() immediately (async, non-blocking)
      └── background timer fires every flushMs → flush()

flush()
  └── snapshot + clear queue
      └── POST /proton/v1/ingest/streams/{stream}
          Content-Type: application/json
          { "columns": [...], "data": [[row], [row], ...] }
          ├── 2xx → done
          └── failure → retry once after 1 s
              └── still failing → log error, drop batch (never throw)

close()    ← called only from gateway_stop
  └── clearInterval(timer)
  └── await flush()   (synchronous drain before shutdown)
```

Every hook handler calls `client.enqueue(event)` and returns immediately — no `await`, never blocks.

### 6.2 Ingest payload format

```json
{
  "columns": [
    "hook_name", "event_time", "session_id", "run_id", "agent_id", "session_key",
    "tool_name", "provider", "model", "hook_decision", "block_reason", "event_data"
  ],
  "data": [
    ["before_tool_call", "2026-04-06T19:00:00.000Z", "sess-abc", "run-xyz",
     "main", "agent:main:main", "bash", "", "", "observe", "", "{\"params\":{\"cmd\":\"ls\"}}"]
  ]
}
```

### 6.3 Type signatures

```ts
export type HookEvent = {
  hook_name:     string;
  event_time:    string;   // ISO 8601 ms
  session_id:    string;
  run_id:        string;
  agent_id:      string;
  session_key:   string;
  tool_name:     string;
  provider:      string;
  model:         string;
  hook_decision: string;
  block_reason:  string;
  event_data:    string;   // JSON.stringify of full event object
};

export type TimeplusClient = {
  enqueue: (event: HookEvent) => void;
  flush:   () => Promise<void>;
  close:   () => Promise<void>;
};
```

---

## 7. Hook Handlers

All 29 hooks are registered in `registerAllHooks()`. They share a `baseEvent()` helper that fills common fields, with hook-specific overrides applied on top.

```ts
function baseEvent(
  hookName: string,
  ctx: { agentId?: string; sessionKey?: string; sessionId?: string }
): HookEvent {
  return {
    hook_name:     hookName,
    event_time:    new Date().toISOString(),
    session_id:    ctx.sessionId  ?? "",
    run_id:        "",
    agent_id:      ctx.agentId    ?? "",
    session_key:   ctx.sessionKey ?? "",
    tool_name:     "",
    provider:      "",
    model:         "",
    hook_decision: "observe",
    block_reason:  "",
    event_data:    "",
  };
}
```

### 7.1 Tier 1 — Agent context hooks (observe only)

Hooks: `llm_input`, `llm_output`, `before_prompt_build`, `before_agent_start`, `before_agent_reply`, `before_model_resolve`, `agent_end`, `before_compaction`, `after_compaction`, `before_reset`

Populated columns: `run_id`, `session_id`, `provider`, `model` (where available in event).

### 7.2 Tier 2 — Tool hooks (observe only in Phase 1)

Hooks: `before_tool_call`, `after_tool_call`, `tool_result_persist`

Populated columns: `tool_name`, `run_id`.

`before_tool_call` returns `undefined` (pass-through). `hook_decision` is `"observe"`.

Phase 2: `before_tool_call` will return `{ block, blockReason, requireApproval }` from policy engine.

### 7.3 Tier 3 — Message, session, gateway hooks (observe only)

Hooks: `message_received`, `message_sending`, `message_sent`, `session_start`, `session_end`, `inbound_claim`, `before_dispatch`, `reply_dispatch`, `before_message_write`, `subagent_spawning`, `subagent_delivery_target`, `subagent_spawned`, `subagent_ended`, `gateway_start`, `gateway_stop`, `before_install`

All fields except `hook_name`, `event_time`, `session_id`, `agent_id`, `session_key`, `event_data` are empty strings.

`gateway_stop` is the only handler that calls `await client.close()`.

### 7.4 Phase 2 enforcement candidates

| Hook | Threat mitigated |
|---|---|
| `before_tool_call` | Dangerous commands, data exfiltration via tool params |
| `before_install` | Malicious plugin/skill installation (scan findings) |
| `subagent_spawning` | Rogue agent-to-agent attack chains |
| `before_prompt_build` | Prompt injection in system prompt |
| `inbound_claim` | Forged inbound messages |
| `message_sending` | Data exfiltration via outbound messages |

---

## 8. Backend Changes

The `timeplus.Init()` function in `backend/internal/timeplus/init.go` must be extended to create `agentshield_hook_events` alongside the OTel streams. This keeps all DDL ownership in the backend.

---

## 9. Out of Scope (Phase 1)

- Policy engine integration (Phase 2)
- `requireApproval` UI in OpenClaw (Phase 2)
- Event replay on Timeplus downtime (disk persistence)
- Per-hook-category streams (single stream + views covers this)
- Publishing to npm (structure is ready, publishing deferred)
