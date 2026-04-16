# Paperclip + OpenClaw + AgentGuard Showcase

This document demonstrates how **Paperclip** (multi-agent orchestration), **OpenClaw** (AI agent gateway), and **AgentGuard** (real-time security monitoring) work together as a unified platform.

---

## The Story

When Paperclip orchestrates AI agents for engineering tasks, every agent request flows through OpenClaw as the gateway. OpenClaw intercepts all 29 lifecycle hook points — tool calls, LLM interactions, session boundaries — and streams them to Timeplus. AgentGuard queries Timeplus in real-time to show what every agent is doing, detect threats, and track costs across the entire fleet.

**Result:** You see exactly what your Paperclip-orchestrated agents are doing, the moment they do it.

---

## Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│  Paperclip  (http://localhost:3100)                                │
│  ┌───────────┐  ┌───────────┐  ┌──────────────┐                  │
│  │  CEO      │  │  CTO      │  │  Engineer    │  ← Agents        │
│  │ (manager) │  │(technical)│  │(openclaw_gw) │                  │
│  └───────────┘  └───────────┘  └──────┬───────┘                  │
│                                        │ task assignments          │
└────────────────────────────────────────┼───────────────────────────┘
                                         │ ws://localhost:18789
                                         ▼
┌────────────────────────────────────────────────────────────────────┐
│  OpenClaw Gateway  (http://localhost:18789)                        │
│                                                                    │
│  ┌──────────────────────────┐  ┌─────────────────────────────┐   │
│  │  AI Agent Runtime        │  │  AgentGuard Plugin          │   │
│  │  (Claude Sonnet via      │  │  (@timeplus/agentguard-     │   │
│  │   LiteLLM proxy)         │  │   openclaw-plugin)          │   │
│  └──────────────────────────┘  └────────────┬────────────────┘   │
│                                              │ hook events         │
└──────────────────────────────────────────────┼─────────────────────┘
                                               │ HTTP ingest
                                               ▼
┌────────────────────────────────────────────────────────────────────┐
│  Timeplus  (http://localhost:3218)                                 │
│                                                                    │
│  agentguard_hook_events → CIM MVs → agentguard_cim_event          │
│                                    → rule MVs                      │
│                                    → agentguard_security_events    │
└───────────────────────────────────────────┬────────────────────────┘
                                            │ SQL queries + SSE
                                            ▼
┌────────────────────────────────────────────────────────────────────┐
│  AgentGuard  (http://localhost:8080)                               │
│                                                                    │
│  Dashboard · Agents · Sessions · Threats · Cost · Rules           │
└────────────────────────────────────────────────────────────────────┘
```

**Key integration point:** Paperclip agents configured with the `openclaw_gateway` adapter connect to OpenClaw via WebSocket. OpenClaw routes their LLM calls and intercepts all hook events — which the AgentGuard plugin forwards to Timeplus for real-time monitoring.

---

## Prerequisites

| Component | Version | Port |
|---|---|---|
| [Timeplus Enterprise](https://timeplus.com) | latest | 3218 |
| [OpenClaw](https://github.com/timeplus-io/openclaw) | 2026.4.5+ | 18789 |
| [LiteLLM proxy](https://docs.litellm.ai) | latest | 4000 |
| AgentGuard backend | — | 8080 |
| Paperclip | latest | 3100 |

---

## Setup

### 1. Start Timeplus

Follow the [Timeplus Enterprise quickstart](https://docs.timeplus.com/quickstart). The streaming engine must be running before any other component starts.

```bash
# Verify Timeplus is accessible
curl http://localhost:3218/timeplusd/v1/info
```

### 2. Start AgentGuard

AgentGuard creates the required Timeplus streams on startup and serves the security dashboard.

```bash
# From the repo root
make dev
# Backend: http://localhost:8080  Frontend: http://localhost:5173

# Or with Docker Compose (includes Timeplus)
docker compose up --build
```

### 3. Start LiteLLM Proxy

OpenClaw routes LLM calls through LiteLLM, which proxies to the Claude API.

```bash
cd agents/litellm
docker compose up -d
# Proxy: http://localhost:4000
```

### 4. Start OpenClaw with AgentGuard Plugin

```bash
cd agents/openclaw

# First-time setup only
make workspace        # create .openclaw/ directory with correct permissions
make init             # interactive onboarding wizard (creates .openclaw/openclaw.json)
make configure        # inject AgentGuard + LiteLLM config
make build-plugin     # compile TypeScript plugin

# Start the gateway
make start

# Get the dashboard URL (includes auth token)
make url
# → http://localhost:18789/?token=<token>

# Approve Paperclip as a connected device (first run only)
make approve
```

Verify the AgentGuard plugin loaded:
```bash
docker logs openclaw 2>&1 | grep agentguard
# [agentguard] active — streaming to http://host.docker.internal:3218/...
```

### 5. Start Paperclip

```bash
cd agents/paperclip

# First-time setup
make init   # creates .paperclip/ with embedded Postgres + JWT secret

# Start the Paperclip server
make start
# Dashboard: http://localhost:3100
```

---

## Creating an OpenClaw-Backed Paperclip Agent

Paperclip agents that use the `openclaw_gateway` adapter route all their work through OpenClaw — making them fully visible in AgentGuard.

### Via the Paperclip board UI

1. Open `http://localhost:3100` and navigate to **Agents → Hire**
2. Choose adapter type: **openclaw_gateway**
3. Set the gateway URL: `ws://localhost:18789`
4. Save and assign the agent to a task

### Via the API (for automation)

```bash
curl -X POST http://localhost:3100/api/companies/$PAPERCLIP_COMPANY_ID/agent-hires \
  -H "Authorization: Bearer $PAPERCLIP_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Engineer",
    "role": "engineer",
    "title": "Software Engineer",
    "icon": "code",
    "reportsTo": "<cto-agent-id>",
    "capabilities": "Implements features, fixes bugs, writes tests. Runs via OpenClaw gateway.",
    "adapterType": "openclaw_gateway",
    "adapterConfig": {
      "url": "ws://localhost:18789",
      "autoPairOnFirstConnect": true,
      "sessionKeyStrategy": "issue"
    },
    "runtimeConfig": {
      "heartbeat": {
        "enabled": true,
        "intervalSec": 3600,
        "wakeOnDemand": true
      }
    }
  }'
```

**`autoPairOnFirstConnect: true`** means Paperclip will automatically approve the device pairing the first time it connects — no manual `make approve` needed.

**`sessionKeyStrategy: "issue"`** creates a new OpenClaw session per Paperclip issue, so sessions map 1:1 to tasks in both Paperclip and AgentGuard.

---

## Demo: The Full Flow

This demo walks through a Paperclip task being picked up by an OpenClaw agent and monitored in AgentGuard.

### Step 1 — Create a Paperclip task for the Engineer

```bash
npx paperclipai issue create \
  --company-id $PAPERCLIP_COMPANY_ID \
  --title "Investigate memory usage spike in Go backend" \
  --description "Run profiling on the /api/investigate/query endpoint and report findings." \
  --status todo \
  --assignee-agent-id <engineer-agent-id>
```

### Step 2 — Trigger the agent heartbeat

```bash
npx paperclipai heartbeat run --agent-id <engineer-agent-id>
```

Paperclip wakes the Engineer agent. Because the agent uses `openclaw_gateway`, Paperclip opens a WebSocket to `ws://localhost:18789` and sends the task prompt. OpenClaw routes it to Claude via LiteLLM.

### Step 3 — Watch events flow into AgentGuard

Open the AgentGuard dashboard at `http://localhost:5173`. Within seconds of the agent starting:

**Agents page** — the Engineer appears as an active agent with a live session count and token usage.

**Sessions page** (click the agent) — a waterfall view of every LLM call and tool invocation, timestamped.

**Threats page** — if the agent runs shell commands (likely for profiling), the **Privilege Guard** rule fires if enabled.

**Cost page** — token spend accumulates in real-time as the agent iterates.

### Step 4 — See Paperclip context in hook events

Query Timeplus directly to see the Paperclip task context embedded in every event:

```bash
curl -s -X POST "http://localhost:3218/proton/v1/sql" \
  -H "Content-Type: application/json" \
  -d '{
    "sql": "SELECT hook_name, session_id, agent_id, json_extract_string(event_data, '\''paperclip'\'') as paperclip_ctx, event_time FROM table(agentguard_hook_events) ORDER BY event_time DESC LIMIT 20"
  }'
```

The `paperclip_ctx` field contains the task ID, run ID, and workspace info that Paperclip injected into the gateway request — giving you a direct link from every hook event back to the Paperclip issue that triggered it.

---

## Security Rules in Action

Enable the built-in rules at `http://localhost:5173/rules` before running the demo to see threat detection:

| Rule | What triggers it | Expected in this demo |
|---|---|---|
| **Privilege Guard** | `bash`, `shell`, `sudo` tool calls | Profiling commands trigger a Warning alert |
| **Supply Chain Watch** | `npm install`, `pip install` | Any dependency installs during the task |
| **DLP Sentinel** | Secrets, API keys in tool results | If the agent reads any config files |

When a rule fires, the **Threats** page shows an alert with the agent name, session ID, matched content, and severity. The notification bell in the top nav rings with a live update via Server-Sent Events.

---

## What Paperclip Adds

Running agents through Paperclip adds organizational context visible across the entire platform:

| Without Paperclip | With Paperclip |
|---|---|
| Anonymous agent sessions | Sessions tagged to Paperclip issues and runs |
| No task context | Every hook event carries task ID and goal |
| Manual agent management | Automatic heartbeat scheduling and assignment |
| No audit trail | Full run history with who assigned what, when |

Paperclip's task hierarchy (goal → project → issue → subtask) maps directly to OpenClaw's session hierarchy and then into AgentGuard's session and event views.

---

## Troubleshooting

**Agent not appearing in AgentGuard**
→ Check AgentGuard backend is running: `curl http://localhost:8080/api/agents`
→ Verify the AgentGuard plugin loaded: `docker logs openclaw 2>&1 | grep agentguard`

**Device pairing required**
→ Run `make approve` in `agents/openclaw/`, or set `autoPairOnFirstConnect: true` in the agent's adapter config.

**No events in Timeplus**
→ Confirm Timeplus is accessible from inside the Docker container: `docker exec openclaw curl http://host.docker.internal:3218/timeplusd/v1/info`

**LLM calls failing**
→ Check LiteLLM proxy is running: `curl http://localhost:4000/health`
→ Verify `ANTHROPIC_API_KEY` is set in the LiteLLM environment.

---

## Related Resources

- [`agents/openclaw/README.md`](../openclaw/README.md) — OpenClaw setup guide
- [`agents/openclaw/agentguard-plugin/`](../openclaw/agentguard-plugin/) — Plugin source
- [`agents/litellm/`](../litellm/) — LiteLLM proxy config
- [`docs/cim.md`](../../docs/cim.md) — Common Information Model schema
- [Paperclip `openclaw_gateway` adapter docs](http://localhost:3100/llms/agent-configuration/openclaw_gateway.txt)
- [OpenClaw project](https://github.com/timeplus-io/openclaw)
- [AgentGuard on Docker Hub](https://hub.docker.com/r/timeplus/agentguard)
