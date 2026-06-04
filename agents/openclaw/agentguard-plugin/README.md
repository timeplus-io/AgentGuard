# AgentGuard Plugin for OpenClaw

Streams all OpenClaw lifecycle events to the AgentGuard [Timeplus Proton](https://www.timeplus.com/) backend for real-time security monitoring, and **synchronously blocks risky tool calls** based on rule-driven decisions from the AgentGuard server. Works alongside the Claude Code plugin — both write to the same `agentguard_hook_events` stream, tagged with `agent_type = "openclaw"` or `"claudecode"` respectively.

## How it works

OpenClaw supports a [Plugin SDK](https://openclaw.dev/docs/plugin-sdk) that exposes lifecycle hooks for every meaningful agent action. This plugin registers an `activate` callback that subscribes to all hook events via `api.on(...)`.

There are **two paths** depending on the hook type:

### Async observation path (most hooks)

For every event except `before_tool_call`, the plugin normalizes the event to the `HookEvent` schema and places it on an in-memory queue. A background timer (default 2 s) and a batch-size trigger (default 50 events) flush the queue to Timeplus over HTTP.

Two hooks trigger an immediate flush after enqueue:
- **`llm_output`** — end of every agent turn; flush ensures events appear in Timeplus without waiting for the next timer tick.
- **`subagent_ended`** — end of every sub-agent turn; same reason.

On `gateway_stop` the client drains any remaining events and cancels the timer cleanly.

### Synchronous hold-and-wait path (`before_tool_call` only)

When `holdsEnabled` is `true` (the default), the `before_tool_call` hook follows a different path:

1. Plugin POSTs the event to the AgentGuard server's `/api/holds` endpoint **synchronously** (does NOT enqueue — `/api/holds` ingests the event itself).
2. AgentGuard's backend ingests the event, then waits up to 500 ms for any rule's materialized view to detect a match against this exact tool call. It also checks for any pre-existing open threats from earlier in the session.
3. Backend resolves to one of three outcomes based on the matched rule's `block_policy`:
   - `allow` (no rule fired, or rule policy is `log_only`) → returned immediately
   - `block` (rule policy is `auto_block`) → returned immediately with reason
   - `hold` (rule policy is `hold`) → opens a hold record, long-polls for human Approve/Deny in the AgentGuard UI (up to 5 minutes), returns the human's decision
4. Plugin returns `{ block: true, blockReason: "..." }` from the hook on `block`, or `undefined` on `allow` — OpenClaw's tool dispatcher then either short-circuits the call with the reason or proceeds.

The agent's hot path is paused for the duration of the hold — no LLM tokens are burned waiting for human review.

If `holdsEnabled` is `false`, `before_tool_call` falls back to the async observation path and never blocks. Useful for toggling blocking on/off during testing without uninstalling the plugin.

## Events captured

### Agent lifecycle

| Hook event            | Stored as `hook_name`     | Purpose                                      |
|-----------------------|---------------------------|----------------------------------------------|
| `before_model_resolve`| `before_model_resolve`    | Model selection                              |
| `before_prompt_build` | `before_prompt_build`     | Prompt assembly                              |
| `before_agent_start`  | `before_agent_start`      | Agent turn start                             |
| `before_agent_reply`  | `before_agent_reply`      | Reply generation                             |
| `agent_end`           | `agent_end`               | Agent turn complete                          |
| `before_compaction`   | `before_compaction`       | Context compaction                           |
| `after_compaction`    | `after_compaction`        | Context compaction                           |
| `before_reset`        | `before_reset`            | Session reset                                |

### LLM calls

| Hook event   | Stored as `hook_name` | Notes                                                 |
|--------------|-----------------------|-------------------------------------------------------|
| `llm_input`  | `llm_input`           | Records `provider`, `model`, `run_id`                 |
| `llm_output` | `llm_output`          | Records `provider`, `model`, `run_id`; triggers flush |

### Tool calls

| Hook event          | Stored as `hook_name`  | Notes                  |
|---------------------|------------------------|------------------------|
| `before_tool_call`  | `before_tool_call`     | Records `tool_name`    |
| `after_tool_call`   | `after_tool_call`      | Records `tool_name`    |
| `tool_result_persist` | `tool_result_persist` | Records `tool_name`   |

### Session & message lifecycle

| Hook event            | Stored as `hook_name`   |
|-----------------------|-------------------------|
| `session_start`       | `session_start`         |
| `session_end`         | `session_end`           |
| `before_message_write`| `before_message_write`  |
| `message_received`    | `message_received`      |
| `message_sending`     | `message_sending`       |
| `message_sent`        | `message_sent`          |

### Dispatch & inbound

| Hook event         | Stored as `hook_name` |
|--------------------|-----------------------|
| `inbound_claim`    | `inbound_claim`       |
| `before_dispatch`  | `before_dispatch`     |
| `reply_dispatch`   | `reply_dispatch`      |

### Subagents

Subagent context fields (`childSessionKey`, `requesterSessionKey`) are merged into `event_data` so security rules can trace the subagent call graph.

| Hook event                  | Stored as `hook_name`        | Triggers flush? |
|-----------------------------|------------------------------|-----------------|
| `subagent_spawning`         | `subagent_spawning`          |                 |
| `subagent_delivery_target`  | `subagent_delivery_target`   |                 |
| `subagent_spawned`          | `subagent_spawned`           |                 |
| `subagent_ended`            | `subagent_ended`             | Yes             |

### Gateway

| Hook event      | Stored as `hook_name` | Notes                           |
|-----------------|-----------------------|---------------------------------|
| `gateway_start` | `gateway_start`       |                                 |
| `gateway_stop`  | `gateway_stop`        | Drains queue and closes client  |
| `before_install`| `before_install`      |                                 |

## Prerequisites

- Node.js ≥ 18
- OpenClaw ≥ 2026.4.0
- AgentGuard backend running with Timeplus Proton accessible

## Build

The compiled `dist/` is committed to the repository, so a build step is only needed after modifying source files.

```bash
cd agents/openclaw/agentguard-plugin
npm install
npm run build
```

Or via the Makefile in the `agents/openclaw/` directory:

```bash
make build-plugin
```

## Configuration

### Plugin config (OpenClaw → Timeplus / AgentGuard)

These settings control how the plugin talks to Timeplus (for async event ingest) and to the AgentGuard server (for synchronous hold decisions). They are read from two sources in priority order: **environment variables** override **`openclaw.json` plugin config**.

| `openclaw.json` key | Environment variable          | Default                    | Description                                                                 |
|---------------------|-------------------------------|----------------------------|-----------------------------------------------------------------------------|
| `timeplusUrl`       | `AGENTGUARD_TIMEPLUS_URL`     | `http://localhost:3218`    | Timeplus Proton HTTP API base URL (async event ingest)                      |
| `stream`            | `AGENTGUARD_STREAM`           | `agentguard_hook_events`   | Target stream name for async event ingest                                   |
| `flushMs`           | `AGENTGUARD_FLUSH_MS`         | `2000`                     | Periodic flush interval (milliseconds)                                      |
| `batchSize`         | `AGENTGUARD_BATCH_SIZE`       | `50`                       | Flush when queue reaches this size                                          |
| `username`          | `AGENTGUARD_USERNAME`         | `proton`                   | Timeplus username (for async ingest)                                        |
| `password`          | `AGENTGUARD_PASSWORD`         | `""`                       | Timeplus password (for async ingest)                                        |
| `agentguardUrl`     | `AGENTGUARD_URL`              | `http://localhost:8080`    | AgentGuard server base URL (for `/api/holds` synchronous decisions)         |
| `holdFailPolicy`    | `AGENTGUARD_HOLDS_FAIL_POLICY`| `deny`                     | What to return when `/api/holds` is unreachable: `deny` blocks, `allow` lets call through |
| `holdsEnabled`      | `AGENTGUARD_HOLDS_ENABLED`    | `true`                     | When `false`, `before_tool_call` reverts to async observation-only mode     |
| `deploymentId`      | *(config-only)*               | `default`                  | Deployment group identifier                                                 |
| `deploymentName`    | *(config-only)*               | `Default`                  | Human-readable deployment name                                              |

`deploymentId` and `deploymentName` are set at installation time in `openclaw.json` and cannot be overridden by env var — they identify which OpenClaw deployment the events come from.

> **Docker note:** The `make configure` script in `agents/openclaw/Makefile` writes `agentguardUrl: http://host.docker.internal:8080` into `openclaw.json`, since OpenClaw runs in a container and reaches the host AgentGuard via that alias. Standalone (non-Docker) plugin installs use the `localhost:8080` default.

### Server-side prerequisite — Timeplus service credentials

The plugin's synchronous `/api/holds` call hits the AgentGuard server with no user session cookie (it's a server-to-server call). The AgentGuard server in turn needs Timeplus credentials to ingest the event and query rules. **You must set `TIMEPLUS_USER` and `TIMEPLUS_PASSWORD` env vars on the AgentGuard process** — these are the canonical service credentials for plugin endpoints.

Without these, `/api/holds` returns:

```
HTTP 503
{"error":"service credentials not configured: set TIMEPLUS_USER and TIMEPLUS_PASSWORD env vars on the AgentGuard server"}
```

For the standard `docker-compose.yaml` setup these are pre-set to `proton` / `timeplus@t+`. Edit them if your wizard credentials differ, then `docker compose up -d --build agentguard`.

### AgentGuard server URL (Proton → AgentGuard)

Security rule alerts flow in the opposite direction: Proton (Timeplus) calls back to the AgentGuard server via a Python UDF when a rule fires. AgentGuard must be told its own externally-reachable URL so the UDF is created with the correct address.

Set `AGENTGUARD_URL` on the **AgentGuard server** (not the plugin):

| Environment variable | CLI flag            | Default                   | When to change                                     |
|----------------------|---------------------|---------------------------|----------------------------------------------------|
| `AGENTGUARD_URL`     | `--agentguard-url`  | `http://localhost:8080`   | Any deployment where Proton cannot reach localhost |

Common values:

| Deployment                     | `AGENTGUARD_URL`               |
|--------------------------------|--------------------------------|
| Local dev (everything on host) | `http://localhost:8080` *(default — no action needed)* |
| Docker Compose (`docker-compose.yaml`) | `http://agentguard:8080` *(pre-configured)* |
| Remote / cloud server          | `http://<host-or-ip>:8080`     |

> **Docker note:** `docker-compose.yaml` already sets `AGENTGUARD_URL=http://agentguard:8080` so Proton (running in the `timeplus` container) can reach AgentGuard (running in the `agentguard` container) by Docker service name. No manual change is needed for the standard Compose setup.

## Installation

### Option A — Docker (recommended)

The `agents/openclaw/Makefile` automates the Docker-based setup. From the `agents/openclaw/` directory:

```bash
# 1. One-time workspace setup (creates .openclaw/ with correct permissions)
make workspace

# 2. Run the OpenClaw onboarding wizard to generate .openclaw/openclaw.json
make init

# 3. Inject the AgentGuard plugin config into .openclaw/openclaw.json
make configure

# 4. Build the plugin and start (or restart) the OpenClaw container
make deploy-plugin
```

`make configure` patches `.openclaw/openclaw.json` to:
- Enable the AgentGuard plugin and point it at `http://host.docker.internal:3218`
- Enable OTel diagnostics (traces, metrics, logs to `http://host.docker.internal:4318`)
- Mount the plugin directory into the container at `/home/node/.openclaw/agentguard-plugin`

### Option B — Manual (non-Docker)

Install the plugin package into your OpenClaw project:

```bash
npm install @timeplus/agentguard-openclaw-plugin
```

Then add **two** sections to your `openclaw.json` — `plugins.load.paths` (so OpenClaw discovers the plugin on disk) and `plugins.entries.agentguard` (so it actually runs):

```json
{
  "plugins": {
    "load": {
      "paths": [
        "/absolute/path/to/node_modules/@timeplus/agentguard-openclaw-plugin"
      ]
    },
    "entries": {
      "agentguard": {
        "enabled": true,
        "config": {
          "timeplusUrl": "http://localhost:3218",
          "username": "proton",
          "password": "timeplus@t+",
          "deploymentId": "my-team",
          "deploymentName": "My Team",
          "stream": "agentguard_hook_events",
          "flushMs": 2000,
          "batchSize": 50,
          "agentguardUrl": "http://localhost:8080",
          "holdFailPolicy": "deny",
          "holdsEnabled": true
        }
      }
    }
  }
}
```

> **`plugins.load.paths` is required.** OpenClaw does not auto-discover plugins from `node_modules`; you must list the absolute path to the plugin directory (the one containing its `package.json` and `openclaw.plugin.json`). Use `npm root` to find your project's `node_modules` prefix. Without this entry, `entries.agentguard` is ignored and no events flow.
>
> The Docker setup in `agents/openclaw/Makefile` writes `/home/node/.openclaw/agentguard-plugin` here, since the plugin source is bind-mounted into the container at that path.

Set `holdsEnabled: false` to keep all the observability but disable synchronous tool blocking — useful for early validation before any rule has `block_policy: "hold"` or `auto_block` configured.

## Verification

After starting OpenClaw with the plugin active, check that events are arriving in Timeplus:

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"SELECT hook_name, agent_id, tool_name, event_time FROM table(agentguard_hook_events) WHERE agent_type='"'"'openclaw'"'"' ORDER BY event_time DESC LIMIT 10"}'
```

You should see rows for `session_start`, `llm_input`, `before_tool_call`, `after_tool_call`, `llm_output`, etc.

To confirm LLM metadata is being captured:

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"SELECT hook_name, provider, model, run_id, event_time FROM table(agentguard_hook_events) WHERE agent_type='"'"'openclaw'"'"' AND hook_name IN ('"'"'llm_input'"'"', '"'"'llm_output'"'"') ORDER BY event_time DESC LIMIT 5"}'
```

### Verifying the synchronous hold flow

To exercise the blocking path end-to-end:

1. **Open the AgentGuard UI** at `http://localhost:8080`, log in, and install a rule that matches OpenClaw tool calls (e.g. Privilege Guard).
2. **Set its `block_policy` to `hold`** on the rule detail page (`/rules/:id` → Block Policy panel).
3. **Trigger a matching tool call** from OpenClaw — anything that fires `before_tool_call` and matches the rule.
4. **In the AgentGuard UI** you should see within ~1 s:
   - A toast notification bottom-right: "Hold pending: `<tool>` on `<agent>`"
   - A pending-holds badge on the Threats sidebar entry
   - The threat detail page shows an Approve / Deny banner with the rule message and a truncated `args_summary`
5. **Click Approve or Deny** → OpenClaw resumes (or rejects) the tool call within ~1 s.
6. **Audit trail:** every hold lifecycle row is in the `agentguard_holds` stream:
   ```bash
   curl -s -X POST http://localhost:3218/proton/v1/query \
     -u proton:'timeplus@t+' -H 'Content-Type: application/json' \
     -d '{"query":"SELECT hold_id, tool_name, status, decided_by, decided_at FROM table(mv_holds_current) ORDER BY created_at DESC LIMIT 10"}'
   ```

If you don't see the toast, check `docker logs agentguard 2>&1 | grep holds` — every error path on the server logs with prefix `holds:`.

If the plugin reports `AgentGuard unreachable — backend returned 503`, the AgentGuard server is missing service credentials — see [Server-side prerequisite](#server-side-prerequisite--timeplus-service-credentials).

To temporarily disable hold blocking without changing rules or uninstalling:

```bash
# In openclaw.json:
"holdsEnabled": false

# Or as env var when starting OpenClaw:
AGENTGUARD_HOLDS_ENABLED=false
```

## Development

```bash
npm test          # Run unit tests
npm run build     # Compile TypeScript → dist/
npm run dev       # Watch mode
```

Source files:

| File                    | Responsibility                                                      |
|-------------------------|---------------------------------------------------------------------|
| `src/config.ts`         | Resolve `AgentGuardConfig` from env vars and plugin config          |
| `src/timeplus-client.ts`| `HookEvent` type, batching queue, HTTP POST ingest to Timeplus      |
| `src/holds.ts`          | `requestHold` synchronous client to AgentGuard `/api/holds` (abort-timeout + fail-policy) |
| `src/hooks.ts`          | Subscribe to OpenClaw hook events via `api.on()`. Most hooks enqueue async; `before_tool_call` calls `requestHold` and returns `{ block, blockReason }` on deny. |
| `src/index.ts`          | Plugin entry point: wire `config → client → hooks` in `activate`    |
| `openclaw.plugin.json`  | Plugin manifest with JSON schema for `openclaw.json` config keys    |
