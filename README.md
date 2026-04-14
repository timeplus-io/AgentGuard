# AgentGuard

[![CI](https://github.com/timeplus-io/agentguard/actions/workflows/ci.yml/badge.svg)](https://github.com/timeplus-io/agentguard/actions/workflows/ci.yml)
[![Release](https://github.com/timeplus-io/agentguard/actions/workflows/release.yml/badge.svg)](https://github.com/timeplus-io/agentguard/actions/workflows/release.yml)
[![Latest Release](https://img.shields.io/github/v/release/timeplus-io/agentguard)](https://github.com/timeplus-io/agentguard/releases/latest)
[![Docker Image](https://img.shields.io/badge/docker-ghcr.io-blue?logo=docker)](https://github.com/timeplus-io/agentguard/pkgs/container/agentguard)
[![Go Version](https://img.shields.io/badge/go-1.23-00ADD8?logo=go)](https://golang.org)

Real-time security monitoring and governance platform for AI coding agents. AgentGuard ingests telemetry from **OpenClaw** and **Claude Code** agents, detects threats (prompt injection, data exfiltration, privilege escalation, supply chain attacks), enforces cost budgets, and surfaces everything through a streaming security dashboard.

Built on [Timeplus](https://timeplus.com) — a streaming SQL engine for real-time data.

---

## Quick Start

### Docker Compose (recommended)

Starts AgentGuard + Timeplus together. AgentGuard waits for Timeplus to be healthy before starting.

```bash
docker compose up --build
```

Open [http://localhost:8080](http://localhost:8080).

To stop and remove volumes:
```bash
docker compose down -v
```

### Docker (pre-built image)

Pull the latest image from GitHub Container Registry and run it against an existing Timeplus instance:

```bash
docker run -p 8080:8080 \
  -e TIMEPLUS_HOST=timeplus.example.com \
  -e TIMEPLUS_USER=default \
  ghcr.io/timeplus-io/agentguard:latest
```

Pin to a specific release:

```bash
docker run -p 8080:8080 \
  -e TIMEPLUS_HOST=timeplus.example.com \
  ghcr.io/timeplus-io/agentguard:v0.1.0
```

### Docker (build from source)

Builds a single image with the frontend compiled in. Requires a separately running Timeplus instance.

```bash
docker build -t agentguard .
docker run -p 8080:8080 \
  -e TIMEPLUS_HOST=timeplus.example.com \
  -e TIMEPLUS_USER=default \
  agentguard
```

### Local Development

You need two terminals.

**Backend** (Go/Gin, port 8080):
```bash
cd backend
CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server
```

> **macOS 26+ / Apple Silicon:** `go run` and regular `go build` trigger a dyld `missing LC_UUID load command` abort. Always use `CGO_ENABLED=0` to produce a fully static binary.

**Frontend** (React/Vite, port 5173):
```bash
cd frontend
npm install
npm run dev
```

Open [http://localhost:5173](http://localhost:5173) — it redirects to `/dashboard`.

---

## Configuration

AgentGuard is configured via **config file**, **environment variables**, or **CLI flags** (highest precedence wins).

### Config file

Copy `config.yaml` to the working directory (or pass `--config <path>`):

```yaml
server:
  port: 8080

agentguard:
  url: "http://localhost:8080"   # URL Proton uses to call back AgentGuard for alerts

timeplus:
  host: localhost
  port: 3218
  native_port: 8463
  user: default
  password: ""
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP port the server listens on |
| `AGENTGUARD_URL` | `http://localhost:8080` | URL Proton uses to call back AgentGuard for alert webhooks — set to the externally-reachable address (e.g. `http://agentguard:8080` in Docker) |
| `TIMEPLUS_HOST` | `localhost` | Timeplus hostname |
| `TIMEPLUS_PORT` | `3218` | HTTP query port |
| `TIMEPLUS_NATIVE_PORT` | `8463` | native TCP port (used for streaming inserts) |
| `TIMEPLUS_USER` | `default` | username |
| `TIMEPLUS_PASSWORD` | _(empty)_ | password |

### CLI flags

```
./server --help

Flags:
  --config string                config file path
  --port int                     server port (default 8080)
  --agentguard-url string        URL Proton uses to call back AgentGuard for alert webhooks (default "http://localhost:8080")
  --timeplus-host string         Timeplus host (default "localhost")
  --timeplus-port int            Timeplus HTTP port (default 3218)
  --timeplus-native-port int     Timeplus native TCP port (default 8463)
  --timeplus-user string         Timeplus username (default "default")
  --timeplus-password string     Timeplus password
```

---

## Features

| Page | Route | Description |
|------|-------|-------------|
| Security Posture | `/dashboard` | KPI summary, live threat feed, threat categories, recent agent activity |
| Agent Fleet | `/agents` | All monitored agents with event counts, session counts, token usage, last-seen |
| Agent Detail | `/agents/:id` | Per-agent metrics, session list, run waterfall trace, hook event timeline |
| Threats | `/threats` | Filterable threat feed by severity and category, newest-first |
| Cost Governance | `/cost` | Daily spend trends, per-model token breakdown, configurable pricing table |
| Rule Packs | `/rules` | Install, enable/disable, and upgrade detection rules; real-time MV health status |
| Investigate | `/investigate` | Ad-hoc streaming SQL console backed by Timeplus |
| Onboarding | `/onboarding` | 4-step wizard to connect an OpenClaw or Claude Code agent |
| Settings | `/settings` | Application configuration |

---

## Connecting an Agent

Use the **Onboarding** wizard (`/onboarding`) to connect your first agent. The wizard walks through:

1. **Select agent** — OpenClaw or Claude Code
2. **Install plugin** — agent-specific install command and configuration (deployment ID, name, Timeplus URL, and for Claude Code an agent ID)
3. **OTel config** — optional OpenTelemetry export to Timeplus for LLM token / cost / latency metrics (skippable; hook events still work without it)
4. **Activate rules** — select which security rules to enable; rules are disabled by default and must be explicitly activated

Agent-specific setup guides:
- [`agents/openclaw/README.md`](agents/openclaw/README.md) — OpenClaw + AgentGuard full setup
- [`agents/claudecode/agentguard-plugin/README.md`](agents/claudecode/agentguard-plugin/README.md) — Claude Code hook plugin

---

## Tech Stack

**Frontend:** React 18, Vite, React Router v6, Tailwind CSS, Recharts, `@timeplus/proton-javascript-driver`

**Backend:** Go 1.23, Gin, Cobra, Viper, `proton-go-driver`

**Database:** [Timeplus](https://timeplus.com) (streaming SQL engine)
