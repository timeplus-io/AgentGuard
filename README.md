# AgentGuard

[![CI](https://github.com/timeplus-io/agentguard/actions/workflows/ci.yml/badge.svg)](https://github.com/timeplus-io/agentguard/actions/workflows/ci.yml)
[![Release](https://github.com/timeplus-io/agentguard/actions/workflows/release.yml/badge.svg)](https://github.com/timeplus-io/agentguard/actions/workflows/release.yml)
[![Latest Release](https://img.shields.io/github/v/release/timeplus-io/agentguard)](https://github.com/timeplus-io/agentguard/releases/latest)
[![Docker Hub](https://img.shields.io/docker/v/timeplus/agentguard?label=dockerhub&logo=docker)](https://hub.docker.com/r/timeplus/agentguard)
[![GHCR](https://img.shields.io/badge/ghcr.io-agentguard-blue?logo=docker)](https://github.com/timeplus-io/agentguard/pkgs/container/agentguard)
[![Go Version](https://img.shields.io/badge/go-1.23-00ADD8?logo=go)](https://golang.org)
[![npm: Claude Code plugin](https://img.shields.io/npm/v/%40timeplus%2Fagentguard-claudecode-plugin?label=claudecode-plugin&logo=npm)](https://www.npmjs.com/package/@timeplus/agentguard-claudecode-plugin)
[![npm: OpenClaw plugin](https://img.shields.io/npm/v/%40timeplus%2Fagentguard-openclaw-plugin?label=openclaw-plugin&logo=npm)](https://www.npmjs.com/package/@timeplus/agentguard-openclaw-plugin)
[![PyPI: Hermes plugin](https://img.shields.io/pypi/v/agentguard-hermes-plugin?label=hermes-plugin&logo=pypi)](https://pypi.org/project/agentguard-hermes-plugin/)

Real-time security monitoring, governance, **and synchronous tool-call blocking** for AI coding agents. AgentGuard ingests telemetry from **OpenClaw**, **Claude Code**, and **Hermes Agent**, detects threats (prompt injection, data exfiltration, privilege escalation, supply chain attacks), **pauses risky tool calls for human Approve/Deny review**, enforces cost budgets, and surfaces everything through a streaming security dashboard.

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

timeplus:
  host: localhost
  port: 3218
  native_port: 8463
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP port the server listens on |
| `TIMEPLUS_HOST` | `localhost` | Timeplus hostname |
| `TIMEPLUS_PORT` | `3218` | HTTP query port |
| `TIMEPLUS_NATIVE_PORT` | `8463` | native TCP port (used for streaming inserts) |
| `TIMEPLUS_USER` | _(unset)_ | **Service credentials** for plugin/server-to-server endpoints (`/api/holds`, `/api/holds/_abandon-session`, `/api/holds/_drain`) and the backend notify watcher (`internal/notify`, #258). Required for the synchronous tool-call blocking flow — endpoints return `HTTP 503 service credentials not configured` when unset. **Not** used for user-facing UI requests, which always borrow the logged-in user's session credentials from the wizard / login form. The bundled `docker-compose.yaml` pre-sets this to `proton`. |
| `TIMEPLUS_PASSWORD` | _(unset)_ | Password for `TIMEPLUS_USER`. Same constraints as above. The bundled `docker-compose.yaml` pre-sets this to `timeplus@t+`. |

> User credentials (the ones you see in the dashboard) are not accepted via environment variables — use the setup wizard or login form. The `TIMEPLUS_USER` / `TIMEPLUS_PASSWORD` env vars above are a **separate** server-to-server service identity, used only by plugin endpoints and the backend notify watcher.

### CLI flags

```
./server --help

Flags:
  --config string                config file path
  --port int                     server port (default 8080)
  --timeplus-host string         Timeplus host (default "localhost")
  --timeplus-port int            Timeplus HTTP port (default 3218)
  --timeplus-native-port int     Timeplus native TCP port (default 8463)
```

### Alert delivery

When a security rule fires, its materialized view writes a row to `agentguard_notify_events`. A backend watcher (`internal/notify`, #258) tails that stream directly via a streaming SQL query — no Timeplus→AgentGuard network callback is involved — and fans matches out over the existing `/api/notifications/stream` SSE pipeline. Seen-state is persisted per threat identity in the `agentguard_notifications` mutable stream so the browser can backfill unconfirmed notifications (`GET /api/notifications/unconfirmed`) on reconnect and confirm them explicitly (`POST /api/notifications/confirm`).

---

## Features

| Page | Route | Description |
|------|-------|-------------|
| Setup | `/setup` | 3-step first-launch wizard: connect to Timeplus, enter credentials, provision streams and rules |
| Login | `/login` | Session-based login with Timeplus credentials (required on every launch after setup) |
| Security Posture | `/dashboard` | KPI summary, live threat feed, threat categories, recent agent activity |
| Agent Fleet | `/agents` | All monitored agents with event counts, session counts, token usage, last-seen |
| Agent Detail | `/agents/:id` | Per-agent metrics, session list, run waterfall trace, hook event timeline |
| Threats | `/threats` | Filterable threat feed by severity and category; **Pending Holds** section surfaces tool calls awaiting human review |
| Threat Detail | `/threats/:id` | Per-threat detail with rule context, agent/session links, and an **Approve / Deny** banner when a hold is pending |
| Cost Governance | `/cost` | Daily spend trends, per-model token breakdown, configurable pricing table |
| Rule Packs | `/rules` | Install, enable/disable, and upgrade detection rules; real-time MV health status |
| Rule Detail | `/rules/:id` | Per-rule configuration including the **Block Policy** panel (`log_only` / `auto_block` / `hold`) |
| Semantic DLP | `/dlp` | Natural-language forensic auditing of conversation logs: ask a question, get a cited report with risk profiles and an enriched-message browser |
| Onboarding | `/onboarding` | 4-step wizard to connect an OpenClaw, Claude Code, or Hermes agent |

### Tool-call blocking (NEW)

AgentGuard can synchronously **pause an agent's tool call** while a human reviews it in the UI. Per-rule `block_policy`:

- `log_only` — default; rule fires, threat is recorded, agent is never paused (current behavior).
- `auto_block` — rule fires and the tool call is denied immediately, no human prompt.
- `hold` — rule fires, the agent's hot path is paused, a Pending Hold appears under `/threats` and on the threat detail page (`/threats/:id`); the agent resumes (or rejects) within ~1 s of an Approve / Deny click. No LLM tokens are burned while waiting.

Set the policy on the rule detail page (`/rules/:id` → Block Policy panel). All three plugins ([OpenClaw](agents/openclaw/agentguard-plugin/README.md), [Claude Code](agents/claudecode/agentguard-plugin/README.md), [Hermes](agents/hermes/agentguard-plugin/README.md)) implement the synchronous hold-and-wait path in their `PreToolUse` / `pre_tool_call` / `before_tool_call` hook. See each plugin's README for the env vars (`AGENTGUARD_URL`, `AGENTGUARD_HOLDS_FAIL_POLICY`, `AGENTGUARD_HOLDS_ENABLED`) and the end-to-end walkthrough.

> **Server-side prerequisite:** the `/api/holds` endpoint requires `TIMEPLUS_USER` and `TIMEPLUS_PASSWORD` env vars on the AgentGuard process — see [Environment variables](#environment-variables) below.

### Semantic DLP (NEW)

Ask natural-language forensic questions about agent conversation logs — *"Who sent customer contact data to an external model?"* — and get a sourced answer with cited messages. All enrichment (PII/entity extraction, embeddings, risk classification) is pre-computed at ingest by **Timeplus Python UDFs** that call a configurable OpenAI-compatible endpoint; the large model only ever reasons over the final ~20 candidates. Configure it under the `dlp:` section of `config.yaml` (`enabled`, `base_url`, `embedding_model`, `embedding_dim`, `risk_model`, `summary_model`), then provision via the setup wizard.

To try it without a real model endpoint, AgentGuard bundles a deterministic mock and an end-to-end test:

```bash
make compose-up        # bring up Timeplus + AgentGuard
make dlp-integration   # mock model server + provision + enrich + query round-trip
make dlp-seed          # (optional) load demo data, then open /dlp
```

See [`docs/dlp.md`](docs/dlp.md) for the full feature documentation (and [`docs/cim.md`](docs/cim.md#6-downstream-semantic-dlp) for how it builds on the CIM layer).

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
