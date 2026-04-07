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

The plugin is a TypeScript npm package compiled to `agentshield-plugin/dist/`. The compiled output is committed to git so it is available for Docker volume mount without a build step at runtime.

### Plugin targets

```bash
make install-plugin   # npm install in agentshield-plugin/
make build-plugin     # compile TypeScript → dist/
make deploy-plugin    # build-plugin + start (full redeploy)
```

### Plugin configuration (in openclaw.json)

```json
{
  "plugins": {
    "load": {
      "paths": ["/home/node/.openclaw/agentshield-plugin"]
    },
    "entries": {
      "agentshield": {
        "enabled": true,
        "config": {
          "timeplusUrl": "http://host.docker.internal:3218",
          "stream": "agentshield_hook_events",
          "flushMs": 2000,
          "batchSize": 50,
          "username": "proton",
          "password": "timeplus@t+"
        }
      }
    }
  }
}
```

All fields can be overridden with environment variables:

| Env var | Config field |
|---|---|
| `AGENTSHIELD_TIMEPLUS_URL` | `timeplusUrl` |
| `AGENTSHIELD_STREAM` | `stream` |
| `AGENTSHIELD_FLUSH_MS` | `flushMs` |
| `AGENTSHIELD_BATCH_SIZE` | `batchSize` |
| `AGENTSHIELD_USERNAME` | `username` |
| `AGENTSHIELD_PASSWORD` | `password` |

### Verify the plugin loaded

```bash
docker logs openclaw 2>&1 | grep agentshield
```

Expected output:

```
[agentshield] active — streaming to http://host.docker.internal:3218/proton/v1/ingest/streams/agentshield_hook_events (flush every 2000ms, batch size 50)
```

### Query hook events in Timeplus

```bash
curl -s -u "proton:timeplus@t+" \
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
