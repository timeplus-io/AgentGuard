# OpenClaw + AgentShield

Runs OpenClaw as a local AI agent gateway with two plugins active:

- **diagnostics-otel** — exports OpenTelemetry traces, metrics, and logs to Timeplus via OTLP
- **agentshield** — intercepts all 29 OpenClaw lifecycle hooks and streams security events to Timeplus

---

## Prerequisites

- Docker
- Python 3 (for `make configure`)
- Timeplus running and accessible (default: `http://localhost:3218`)
- AgentShield backend running (creates the `agentshield_hook_events` stream on startup)

---

## First-time setup

```bash
# 1. Create the workspace directory with correct permissions
make workspace

# 2. Run the interactive onboarding wizard (creates .openclaw/openclaw.json)
make init

# 3. Apply AgentShield configuration on top of the generated config
make configure

# 4. Build the AgentShield hook plugin
make build-plugin
```

### What `make configure` does

Edits `.openclaw/openclaw.json` in place:

| Step | What it sets |
|---|---|
| Gateway bind | `gateway.bind = "lan"` so the container port mapping works |
| OTel diagnostics | `diagnostics.otel` — sends traces/metrics/logs to `host.docker.internal:4318` |
| diagnostics-otel plugin | Enables the built-in OpenClaw OTel exporter plugin |
| agentshield load path | Adds `/home/node/.openclaw/agentshield-plugin` to `plugins.load.paths` |
| agentshield plugin | Enables the hook plugin, pointing it at Timeplus on `host.docker.internal:3218` |

Run `make configure` again at any time to re-apply — it is idempotent.

---

## Start / stop

```bash
# Start (or restart) the gateway
make start

# Print the dashboard URL with auth token
make url

# Stop and remove the container
make clean
```

`make start` mounts three volumes into the container:

| Host path | Container path | Purpose |
|---|---|---|
| `.openclaw/` | `/home/node/.openclaw` | Config, workspace, logs |
| `.openclaw/workspace/` | `/home/node/.openclaw/workspace` | Agent file workspace |
| `agentshield-plugin/` | `/home/node/.openclaw/agentshield-plugin` | Hook plugin (compiled) |

---

## Device approval

When a client (e.g. Claude Code, Cursor) connects for the first time it appears as a pending device.

```bash
# List pending devices
make devices

# Approve all pending devices automatically
make approve
```

---

## AgentShield plugin

The plugin is published to npm as [`@timeplus/agentshield-openclaw-plugin`](https://www.npmjs.com/package/@timeplus/agentshield-openclaw-plugin).

### Installation (npm — recommended)

Install the plugin into your OpenClaw project:

```bash
npm install @timeplus/agentshield-openclaw-plugin
```

OpenClaw auto-discovers plugins from `node_modules` via the `openclaw.extensions` field in the package — no manual load path needed.

Configure by setting environment variables before starting OpenClaw:

```bash
export AGENTSHIELD_TIMEPLUS_URL=http://localhost:3218
export AGENTSHIELD_DEPLOYMENT_ID=my-team
export AGENTSHIELD_DEPLOYMENT_NAME="My Team"
# optional — only needed if Proton has auth enabled
export AGENTSHIELD_USERNAME=proton
export AGENTSHIELD_PASSWORD=your-password
```

Or set the equivalent fields under `plugins.entries.agentshield.config` in `openclaw.json` (see [Plugin configuration](#plugin-configuration-in-openclaw.json) below).

### Installation (Docker volume mount — this repo)

This repository uses the local source tree mounted into the OpenClaw container. Use the Make targets to build and deploy:

```bash
make install-plugin   # npm install in agentshield-plugin/
make build-plugin     # compile TypeScript → dist/
make deploy-plugin    # build-plugin + restart container
```

### Plugin configuration (in openclaw.json)

```json
{
  "plugins": {
    "entries": {
      "agentshield": {
        "enabled": true,
        "config": {
          "timeplusUrl": "http://host.docker.internal:3218",
          "deploymentId": "my-team",
          "deploymentName": "My Team",
          "stream": "agentshield_hook_events",
          "flushMs": 2000,
          "batchSize": 50,
          "username": "proton",
          "password": ""
        }
      }
    }
  }
}
```

When using the npm install approach the `plugins.load.paths` entry is not needed — OpenClaw discovers the plugin from `node_modules` automatically.

### Environment variables

All config fields except `deploymentId` and `deploymentName` can be overridden with environment variables:

| Env var | Config field | Default |
|---|---|---|
| `AGENTSHIELD_TIMEPLUS_URL` | `timeplusUrl` | `http://localhost:3218` |
| `AGENTSHIELD_STREAM` | `stream` | `agentshield_hook_events` |
| `AGENTSHIELD_FLUSH_MS` | `flushMs` | `2000` |
| `AGENTSHIELD_BATCH_SIZE` | `batchSize` | `50` |
| `AGENTSHIELD_USERNAME` | `username` | `proton` |
| `AGENTSHIELD_PASSWORD` | `password` | _(empty)_ |

### Verify the plugin loaded

```bash
docker logs openclaw 2>&1 | grep agentshield
```

Expected output:

```
[agentshield] active — streaming to http://host.docker.internal:3218/proton/v1/ingest/streams/agentshield_hook_events (flush every 2000ms, batch size 50) deployment=my-team
```

### Query hook events in Timeplus

```bash
curl -s -u "proton:" \
  -X POST "http://localhost:3218/proton/v1/sql" \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT hook_name, event_time, session_id, agent_id, tool_name FROM table(agentshield_hook_events) ORDER BY event_time DESC LIMIT 20"}'
```

---

## OTel diagnostics plugin

The built-in `diagnostics-otel` plugin exports OpenTelemetry data from the gateway process itself (not from agents). The OTLP endpoint is `http://host.docker.internal:4318`, which maps to the Timeplus OTLP receiver running on the host.

Configuration set by `make configure`:

```json
{
  "diagnostics": {
    "enabled": true,
    "otel": {
      "enabled": true,
      "endpoint": "http://host.docker.internal:4318",
      "traces": true,
      "metrics": true,
      "logs": true,
      "serviceName": "openclaw",
      "sampleRate": 1,
      "flushIntervalMs": 3000
    }
  },
  "plugins": {
    "entries": {
      "diagnostics-otel": { "enabled": true }
    }
  }
}
```

---

## Typical workflow

```
make workspace        # once, sets up directory permissions
make init             # once, runs onboarding wizard
make configure        # once (re-run if config is reset)
make deploy-plugin    # once, or after editing plugin source
make url              # get the dashboard URL
```

After source changes to the plugin:

```
make deploy-plugin    # rebuilds and restarts in one step
```
