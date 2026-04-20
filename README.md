# AgentGuard

[![CI](https://github.com/timeplus-io/agentguard/actions/workflows/ci.yml/badge.svg)](https://github.com/timeplus-io/agentguard/actions/workflows/ci.yml)
[![Release](https://github.com/timeplus-io/agentguard/actions/workflows/release.yml/badge.svg)](https://github.com/timeplus-io/agentguard/actions/workflows/release.yml)
[![Latest Release](https://img.shields.io/github/v/release/timeplus-io/agentguard)](https://github.com/timeplus-io/agentguard/releases/latest)
[![Docker Image](https://img.shields.io/badge/docker-ghcr.io-blue?logo=docker)](https://github.com/timeplus-io/agentguard/pkgs/container/agentguard)
[![Go Version](https://img.shields.io/badge/go-1.23-00ADD8?logo=go)](https://golang.org)
[![npm: Claude Code plugin](https://img.shields.io/npm/v/%40timeplus%2Fagentguard-claudecode-plugin?label=claudecode-plugin&logo=npm)](https://www.npmjs.com/package/@timeplus/agentguard-claudecode-plugin)
[![npm: OpenClaw plugin](https://img.shields.io/npm/v/%40timeplus%2Fagentguard-openclaw-plugin?label=openclaw-plugin&logo=npm)](https://www.npmjs.com/package/@timeplus/agentguard-openclaw-plugin)
[![PyPI: Hermes plugin](https://img.shields.io/pypi/v/agentguard-hermes-plugin?label=hermes-plugin&logo=pypi)](https://pypi.org/project/agentguard-hermes-plugin/)

Real-time security monitoring and governance platform for AI coding agents. AgentGuard ingests telemetry from **OpenClaw**, **Claude Code**, and **Hermes Agent**, detects threats (prompt injection, data exfiltration, privilege escalation, supply chain attacks), enforces cost budgets, and surfaces everything through a streaming security dashboard.

Built on [Timeplus](https://timeplus.com) — a streaming SQL engine for real-time data.

---

## Quick Start

### Docker Compose (recommended)

Starts AgentGuard + Timeplus together. AgentGuard waits for Timeplus to be healthy before starting.

```bash
docker compose up --build
```

Open [http://localhost:8080](http://localhost:8080). On first launch, the browser opens to the **setup wizard**, which collects your Timeplus connection details and provisions all required resources. After setup completes, log in with your Timeplus credentials to access the dashboard.

To stop and remove volumes:
```bash
docker compose down -v
```

### Docker (pre-built image)

Pull the latest image from GitHub Container Registry and run it against an existing Timeplus instance:

```bash
docker run -p 8080:8080 \
  -e TIMEPLUS_HOST=timeplus.example.com \
  ghcr.io/timeplus-io/agentguard:latest
```

Pin to a specific release:

```bash
docker run -p 8080:8080 \
  -e TIMEPLUS_HOST=timeplus.example.com \
  ghcr.io/timeplus-io/agentguard:v0.1.0
```

On first launch, open [http://localhost:8080](http://localhost:8080) and complete the setup wizard. Credentials are entered in the wizard — not via environment variables.

### Docker (build from source)

Builds a single image with the frontend compiled in. Requires a separately running Timeplus instance.

```bash
docker build -t agentguard .
docker run -p 8080:8080 \
  -e TIMEPLUS_HOST=timeplus.example.com \
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

Open [http://localhost:5173](http://localhost:5173). On first launch it redirects to the setup wizard; on subsequent launches it redirects to the login page.

---

## Configuration

AgentGuard is configured via **config file**, **environment variables**, or **CLI flags** (highest precedence wins).

Credentials (username and password) are **never** stored in config files or environment variables. They are entered through the setup wizard on first launch, or the login form on subsequent launches. Sessions are server-side and in-memory — users re-login after a server restart.

### Config file

The server reads `config.yaml` from the working directory (or a path passed via `--config`). After the setup wizard completes, it writes `setup_complete: true` and the Timeplus connection details into this file automatically. You can also seed it manually before first launch:

```yaml
server:
  port: 8080

agentguard:
  url: "http://localhost:8080"   # URL Timeplus uses to call back AgentGuard for alerts

timeplus:
  host: localhost
  port: 3218
  native_port: 8463
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP port the server listens on |
| `AGENTGUARD_URL` | `http://localhost:8080` | AgentGuard's own base URL — see [Alert UDF webhook](#alert-udf-webhook) below |
| `TIMEPLUS_HOST` | `localhost` | Timeplus hostname |
| `TIMEPLUS_PORT` | `3218` | HTTP query port |
| `TIMEPLUS_NATIVE_PORT` | `8463` | native TCP port (used for streaming inserts) |

> Credentials are not accepted via environment variables. Use the setup wizard or login form.

### CLI flags

```
./server --help

Flags:
  --config string                config file path
  --port int                     server port (default 8080)
  --agentguard-url string        AgentGuard base URL used by the alert UDF (default "http://localhost:8080")
  --timeplus-host string         Timeplus host (default "localhost")
  --timeplus-port int            Timeplus HTTP port (default 3218)
  --timeplus-native-port int     Timeplus native TCP port (default 8463)
```

### Alert UDF webhook

When a security rule fires, Timeplus executes a Python UDF (`notify_agentguard_udf`) that HTTP-POSTs the alert to AgentGuard at:

```
<AGENTGUARD_URL>/api/alerts/webhook
```

This call goes from **Timeplus → AgentGuard**, so it must use the address where AgentGuard is reachable _from Timeplus's perspective_, which differs by deployment:

| Deployment | `AGENTGUARD_URL` |
|---|---|
| Local dev (everything on host) | `http://localhost:8080` _(default — no action needed)_ |
| Docker Compose | `http://agentguard:8080` _(pre-set in `docker-compose.yaml`)_ |
| Docker, custom host/port | `http://<host-or-ip>:<port>` |

If this URL is wrong, security rules will still detect events and write to the `agentguard_security_events` stream, but no live alerts will appear in the dashboard notification stream.

---

## Features

| Page | Route | Description |
|------|-------|-------------|
| Setup | `/setup` | 3-step first-launch wizard: connect to Timeplus, enter credentials, provision streams and rules |
| Login | `/login` | Session-based login with Timeplus credentials (required on every launch after setup) |
| Security Posture | `/dashboard` | KPI summary, live threat feed, threat categories, recent agent activity |
| Agent Fleet | `/agents` | All monitored agents with event counts, session counts, token usage, last-seen |
| Agent Detail | `/agents/:id` | Per-agent metrics, session list, run waterfall trace, hook event timeline |
| Threats | `/threats` | Filterable threat feed by severity and category, newest-first |
| Cost Governance | `/cost` | Daily spend trends, per-model token breakdown, configurable pricing table |
| Rule Packs | `/rules` | Install, enable/disable, and upgrade detection rules; real-time MV health status |
| Investigate | `/investigate` | Ad-hoc streaming SQL console backed by Timeplus |
| Onboarding | `/onboarding` | 4-step wizard to connect an OpenClaw, Claude Code, or Hermes agent |
| Settings | `/settings` | Application configuration |

---

## Connecting an Agent

Use the **Onboarding** wizard (`/onboarding`) to connect your first agent. The wizard walks through:

1. **Select agent** — OpenClaw, Claude Code, or Hermes Agent
2. **Install plugin** — agent-specific install command and configuration (deployment ID, name, Timeplus URL, and for Claude Code an agent ID)
3. **OTel config** — optional OpenTelemetry export to Timeplus for LLM token / cost / latency metrics (skippable; hook events still work without it; not available for Hermes)
4. **Activate rules** — navigate to the Rules page to enable detection rules; rules are disabled by default

Agent-specific setup guides:
- [`agents/openclaw/README.md`](agents/openclaw/README.md) — OpenClaw + AgentGuard full setup
- [`agents/claudecode/agentguard-plugin/README.md`](agents/claudecode/agentguard-plugin/README.md) — Claude Code hook plugin
- [`agents/hermes/agentguard-plugin/README.md`](agents/hermes/agentguard-plugin/README.md) — Hermes Agent plugin (PyPI: `agentguard-hermes-plugin`)

---

## Tech Stack

**Frontend:** React 18, Vite, React Router v6, Tailwind CSS, Recharts, `@timeplus/proton-javascript-driver`

**Backend:** Go 1.23, Gin, Cobra, Viper, `proton-go-driver`

**Database:** [Timeplus](https://timeplus.com) (streaming SQL engine)
