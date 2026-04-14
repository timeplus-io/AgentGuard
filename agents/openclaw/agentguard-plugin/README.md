# AgentGuard Plugin for OpenClaw

Streams all OpenClaw lifecycle events to the AgentGuard [Timeplus Proton](https://www.timeplus.com/) backend for real-time security monitoring. Works alongside the Claude Code plugin — both write to the same `agentguard_hook_events` stream, tagged with `agent_type = "openclaw"` or `"claudecode"` respectively.

## How it works

OpenClaw supports a [Plugin SDK](https://openclaw.dev/docs/plugin-sdk) that exposes lifecycle hooks for every meaningful agent action. This plugin registers an `activate` callback that subscribes to all hook events via `api.on(...)`. Each event is normalized to the `HookEvent` schema and placed on an in-memory queue; a background timer (default 2 s) and a batch-size trigger (default 50 events) flush the queue to Timeplus over HTTP.

Two hooks trigger an immediate flush after enqueue:
- **`llm_output`** — end of every agent turn; flush ensures events appear in Timeplus without waiting for the next timer tick.
- **`subagent_ended`** — end of every sub-agent turn; same reason.

On `gateway_stop` the client drains any remaining events and cancels the timer cleanly.

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

### Plugin config (OpenClaw → Timeplus)

These settings control how the plugin sends events to Timeplus. They are read from two sources in priority order: **environment variables** override **`openclaw.json` plugin config**.

| `openclaw.json` key | Environment variable       | Default                    | Description                             |
|---------------------|----------------------------|----------------------------|-----------------------------------------|
| `timeplusUrl`       | `AGENTGUARD_TIMEPLUS_URL`  | `http://localhost:3218`    | Timeplus Proton HTTP API base URL       |
| `stream`            | `AGENTGUARD_STREAM`        | `agentguard_hook_events`   | Target stream name                      |
| `flushMs`           | `AGENTGUARD_FLUSH_MS`      | `2000`                     | Periodic flush interval (milliseconds)  |
| `batchSize`         | `AGENTGUARD_BATCH_SIZE`    | `50`                       | Flush when queue reaches this size      |
| `username`          | `AGENTGUARD_USERNAME`      | `proton`                   | Timeplus username                       |
| `password`          | `AGENTGUARD_PASSWORD`      | `""`                       | Timeplus password                       |
| `deploymentId`      | *(config-only)*            | `default`                  | Deployment group identifier             |
| `deploymentName`    | *(config-only)*            | `Default`                  | Human-readable deployment name          |

`deploymentId` and `deploymentName` are set at installation time in `openclaw.json` and cannot be overridden by env var — they identify which OpenClaw deployment the events come from.

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

Then add the plugin to your `openclaw.json`:

```json
{
  "plugins": {
    "entries": {
      "agentguard": {
        "enabled": true,
        "config": {
          "timeplusUrl": "http://localhost:3218",
          "deploymentId": "my-team",
          "deploymentName": "My Team",
          "stream": "agentguard_hook_events",
          "flushMs": 2000,
          "batchSize": 50
        }
      }
    }
  }
}
```

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
| `src/hooks.ts`          | Subscribe to all OpenClaw hook events via `api.on()`, build and enqueue `HookEvent` rows |
| `src/index.ts`          | Plugin entry point: wire `config → client → hooks` in `activate`   |
| `openclaw.plugin.json`  | Plugin manifest with JSON schema for `openclaw.json` config keys    |
