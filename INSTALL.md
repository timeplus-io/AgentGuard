# AgentGuard Installation Guide

This guide walks through a complete AgentGuard setup from scratch:

1. [Prerequisites](#1-prerequisites)
2. [Install and run Timeplus](#2-install-and-run-timeplus)
3. [Create a Timeplus user for AgentGuard](#3-create-a-timeplus-user-for-agentguard)
4. [Install and run AgentGuard](#4-install-and-run-agentguard)
5. [Connect OpenClaw](#5-connect-openclaw)
6. [Connect Claude Code](#6-connect-claude-code)
7. [Verify everything is working](#7-verify-everything-is-working)

---

## 1. Prerequisites

| Requirement | Minimum version | Notes |
|---|---|---|
| Docker | 24+ | Required for Timeplus; optional for AgentGuard |
| Node.js | 18+ | Required for agent plugins |
| Go | 1.23+ | Only if building AgentGuard from source |
| Claude Code CLI | any | Only for Claude Code integration |
| OpenClaw | 2026.4.0+ | Only for OpenClaw integration |

---

## 2. Install and run Timeplus

AgentGuard requires [Timeplus Enterprise](https://timeplus.com) as its streaming SQL backend.

### Option A — Docker (recommended)

```bash
docker run -d \
  --name timeplus \
  -p 3218:3218 \
  -p 8463:8463 \
  -p 4317:4317 \
  -p 4318:4318 \
  -p 8000:8000 \
  -v timeplus-data:/timeplus/data \
  --restart unless-stopped \
  timeplus/timeplus-enterprise:3.2.1
```

| Port | Purpose |
|---|---|
| `3218` | HTTP query API — used by AgentGuard |
| `8463` | Native TCP protocol — used by AgentGuard |
| `4317` | OTLP gRPC — agent OTel telemetry |
| `4318` | OTLP HTTP — agent OTel telemetry |
| `8000` | Timeplus web console |

Wait for Timeplus to be ready (takes ~30 s on first start):

```bash
until curl -sf http://localhost:3218/ping > /dev/null 2>&1; do
  echo "Waiting for Timeplus..."; sleep 5
done
echo "Timeplus is ready"
```

### Option B — Docker Compose (Timeplus + AgentGuard together)

If you want to run both services in one step, skip to [section 4B](#option-b--docker-compose-recommended-for-new-installs) — Docker Compose handles Timeplus, user creation, and AgentGuard startup automatically.

---

## 3. Create a Timeplus user for AgentGuard

AgentGuard connects to Timeplus using dedicated credentials. Skip this section if you are using the default `default` user with no password (acceptable for local dev).

### Via Timeplus web console

1. Open [http://localhost:8000](http://localhost:8000) in your browser.
2. Log in with the default admin credentials (set during first-run setup).
3. Navigate to **Settings → Users → Add User**.
4. Create a user — for example:
   - **Username:** `proton`
   - **Password:** `timeplus@t+`
   - **Role:** Admin (AgentGuard needs DDL permissions to create streams and views on startup)
5. Click **Save**.

### Via SQL (alternative)

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"query": "CREATE USER proton IDENTIFIED WITH plaintext_password BY '"'"'timeplus@t+'"'"'"}'
```

> **Note:** The password `timeplus@t+` is the example used in `docker-compose.yaml`. Use a strong password in any non-local deployment and pass it to AgentGuard via `TIMEPLUS_PASSWORD`.

---

## 4. Install and run AgentGuard

### Option A — Pre-built binary

Download the latest binary for your platform from the [Releases page](https://github.com/timeplus-io/agentguard/releases/latest):

| Platform | Binary |
|---|---|
| macOS (Apple Silicon) | `agentguard-darwin-arm64` |
| macOS (Intel) | `agentguard-darwin-amd64` |
| Linux (x86-64) | `agentguard-linux-amd64` |
| Linux (ARM64) | `agentguard-linux-arm64` |

```bash
# Example: macOS Apple Silicon
curl -L -o agentguard \
  https://github.com/timeplus-io/agentguard/releases/latest/download/agentguard-darwin-arm64
chmod +x agentguard

# Run, pointing at your Timeplus instance
./agentguard \
  --timeplus-host localhost \
  --timeplus-user proton \
  --timeplus-password "timeplus@t+" \
  --agentguard-url http://localhost:8080
```

Open [http://localhost:8080](http://localhost:8080).

AgentGuard creates all required Timeplus streams and materialized views on first startup. This is non-fatal — the server starts even if Timeplus is temporarily unavailable.

### Option B — Docker Compose (recommended for new installs)

This starts Timeplus and AgentGuard together with the correct networking:

```bash
git clone https://github.com/timeplus-io/AgentGuard.git
cd AgentGuard
docker compose up --build
```

Open [http://localhost:8080](http://localhost:8080).

The `docker-compose.yaml` pre-configures:
- Timeplus credentials (`proton` / `timeplus@t+`)
- `AGENTGUARD_URL=http://agentguard:8080` so Proton can call back AgentGuard for alert webhooks

To stop and remove volumes:
```bash
docker compose down -v
```

### Option C — Docker image (standalone)

```bash
docker run -d \
  --name agentguard \
  -p 8080:8080 \
  -e TIMEPLUS_HOST=<timeplus-host> \
  -e TIMEPLUS_USER=proton \
  -e TIMEPLUS_PASSWORD="timeplus@t+" \
  -e AGENTGUARD_URL=http://<agentguard-host>:8080 \
  --restart unless-stopped \
  ghcr.io/timeplus-io/agentguard:latest
```

Replace `<timeplus-host>` with the hostname or IP where Timeplus is running (use `host.docker.internal` on macOS/Windows when Timeplus runs on the host machine).

Replace `<agentguard-host>` with the address Timeplus will use to call back AgentGuard for alert webhooks — see [Alert UDF webhook](#alert-udf-webhook) below.

### Option D — Build from source

```bash
git clone https://github.com/timeplus-io/AgentGuard.git
cd AgentGuard
make build    # builds frontend → embeds into backend → single binary
./backend/server \
  --timeplus-host localhost \
  --timeplus-user proton \
  --timeplus-password "timeplus@t+"
```

> **macOS 26+ (Apple Silicon):** The Makefile already passes `CGO_ENABLED=0`. If running `go build` directly, always include `CGO_ENABLED=0` to avoid a dyld abort.

### Alert UDF webhook

AgentGuard creates a Timeplus Python UDF (`notify_agentguard_udf`) at startup. When a security rule fires, Timeplus calls this UDF, which HTTP-POSTs the alert back to AgentGuard at:

```
<AGENTGUARD_URL>/api/alerts/webhook
```

This is a **Timeplus → AgentGuard** call, so `AGENTGUARD_URL` must be the address where AgentGuard is reachable _from Timeplus's network perspective_:

| Deployment | `AGENTGUARD_URL` |
|---|---|
| Both on host | `http://localhost:8080` _(default)_ |
| Docker Compose | `http://agentguard:8080` _(pre-configured)_ |
| AgentGuard in Docker, Timeplus on host | `http://host.docker.internal:8080` |
| Remote / cloud | `http://<public-ip-or-hostname>:8080` |

---

## 5. Connect OpenClaw

### 5a. Install the plugin

```bash
npm install @timeplus/agentguard-openclaw-plugin
```

### 5b. Configure the plugin

Add the plugin to your `openclaw.json`:

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
          "batchSize": 50,
          "username": "proton",
          "password": "timeplus@t+"
        }
      }
    }
  }
}
```

Or set environment variables before starting OpenClaw:

```bash
export AGENTGUARD_TIMEPLUS_URL=http://localhost:3218
export AGENTGUARD_DEPLOYMENT_ID=my-team
export AGENTGUARD_DEPLOYMENT_NAME="My Team"
export AGENTGUARD_USERNAME=proton
export AGENTGUARD_PASSWORD="timeplus@t+"
```

> **Docker (OpenClaw in container, Timeplus on host):** Use `http://host.docker.internal:3218` instead of `localhost`.

### 5c. Optional — OTel diagnostics

To export LLM traces, metrics, and logs from OpenClaw to Timeplus, add the `diagnostics-otel` section to `openclaw.json`:

```json
{
  "diagnostics": {
    "enabled": true,
    "otel": {
      "enabled": true,
      "endpoint": "http://localhost:4318",
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

> **Docker:** Use `http://host.docker.internal:4318` for the endpoint.

### 5d. Docker-based setup (this repo)

If running OpenClaw via Docker using the Makefile in `agents/openclaw/`:

```bash
cd agents/openclaw

make workspace      # one-time: create .openclaw/ with correct permissions
make init           # one-time: run the OpenClaw onboarding wizard
make configure      # inject AgentGuard plugin config into openclaw.json
make deploy-plugin  # build plugin + start container
make url            # print the dashboard URL with auth token
```

`make configure` automates steps 5b and 5c above for the Docker environment.

---

## 6. Connect Claude Code

### 6a. Install the plugin globally

```bash
npm install -g @timeplus/agentguard-claudecode-plugin
```

This installs the `agentguard-hook` command on your `PATH`.

### 6b. Set environment variables

Add these to your shell profile (`~/.zshrc` or `~/.bashrc`) **before** launching Claude Code:

```bash
export AGENTGUARD_AGENT_ID="your-hostname"        # identifies this machine
export AGENTGUARD_DEPLOYMENT_ID="local"
export AGENTGUARD_DEPLOYMENT_NAME="Local Dev"
export AGENTGUARD_TIMEPLUS_URL="http://localhost:3218"
export AGENTGUARD_USERNAME="proton"
export AGENTGUARD_PASSWORD="timeplus@t+"
```

Then reload your shell:

```bash
source ~/.zshrc   # or ~/.bashrc
```

### 6c. Register hooks in Claude Code

Merge the following into `~/.claude/settings.json`. If the file does not exist, create it.

```json
{
  "hooks": {
    "SessionStart":       [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "SessionEnd":         [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "UserPromptSubmit":   [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "PreToolUse":         [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "PostToolUse":        [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "PostToolUseFailure": [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "SubagentStart":      [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "SubagentStop":       [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "Stop":               [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "PermissionDenied":   [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}]
  }
}
```

> **Merge, do not replace.** If you already have a `settings.json`, add only the `hooks` key. Existing keys are unaffected.

### 6d. Optional — OTel telemetry

To capture LLM token usage, cost, and latency from Claude Code, add the following `env` block to `~/.claude/settings.json` (merge with any existing `env` keys):

```json
{
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY":        "1",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
    "OTEL_METRICS_EXPORTER":               "otlp",
    "OTEL_LOGS_EXPORTER":                  "otlp",
    "OTEL_TRACES_EXPORTER":                "otlp",
    "OTEL_EXPORTER_OTLP_PROTOCOL":         "http/protobuf",
    "OTEL_EXPORTER_OTLP_ENDPOINT":         "http://localhost:4318",
    "OTEL_METRIC_EXPORT_INTERVAL":         "10000",
    "OTEL_LOGS_EXPORT_INTERVAL":           "5000",
    "OTEL_LOG_TOOL_DETAILS":               "1"
  }
}
```

> **Protocol note:** Claude Code requires `http/protobuf`. Do not use `grpc`.

Hook events work without OTel. Skipping this step only omits LLM token/cost/latency metrics.

---

## 7. Verify everything is working

### Check Timeplus is reachable

```bash
curl -s http://localhost:3218/ping
# expected: "Ok."
```

### Check AgentGuard started cleanly

```bash
# Binary / local
./backend/server 2>&1 | grep -E "timeplus|AgentGuard"

# Docker Compose
docker logs agentguard 2>&1 | grep -E "timeplus|AgentGuard"
```

Look for lines like:
```
AgentGuard starting on :8080
Proton endpoint: localhost:3218 (native: 8463)
AgentGuard webhook URL: http://localhost:8080
timeplus: UDF notify_agentguard_udf created/updated (webhook: http://localhost:8080)
```

### Check Claude Code hook events are arriving

Start a Claude Code session, run a tool call, then query Timeplus:

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"SELECT hook_name, agent_id, event_time FROM table(agentguard_hook_events) WHERE agent_type='"'"'claudecode'"'"' ORDER BY event_time DESC LIMIT 5"}'
```

### Check OpenClaw hook events are arriving

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"SELECT hook_name, agent_id, event_time FROM table(agentguard_hook_events) WHERE agent_type='"'"'openclaw'"'"' ORDER BY event_time DESC LIMIT 5"}'
```

### Open the dashboard

Navigate to [http://localhost:8080/onboarding](http://localhost:8080/onboarding) for the guided setup wizard, or go straight to [http://localhost:8080/dashboard](http://localhost:8080/dashboard) to see live agent activity.
