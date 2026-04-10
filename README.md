# AgentShield

Real-time security monitoring and governance platform for AI coding agents. AgentShield ingests telemetry from **OpenClaw** agents (Claude Code support coming soon), detects threats (prompt injection, data exfiltration, privilege escalation, supply chain attacks), enforces cost budgets, and surfaces everything through a streaming security dashboard.

Built on [Timeplus Proton](https://github.com/timeplus-io/proton) — a streaming SQL engine processing 90M events/second at 4ms latency.

---

## Quick Start

### Docker Compose (recommended)

Starts AgentShield + Proton together. AgentShield waits for Proton to be healthy before starting.

```bash
docker compose up --build
```

Open [http://localhost:8080](http://localhost:8080).

To stop and remove volumes:
```bash
docker compose down -v
```

### Docker (standalone)

Builds a single image with the frontend compiled in. Requires a separately running Proton instance.

```bash
docker build -t agentshield .
docker run -p 8080:8080 \
  -e TIMEPLUS_HOST=proton.example.com \
  -e TIMEPLUS_USER=default \
  agentshield
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

AgentShield is configured via **config file**, **environment variables**, or **CLI flags** (highest precedence wins).

### Config file

Copy `config.yaml` to the working directory (or pass `--config <path>`):

```yaml
server:
  port: 8080

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
| `TIMEPLUS_HOST` | `localhost` | Proton/Timeplus hostname |
| `TIMEPLUS_PORT` | `3218` | Proton HTTP query port |
| `TIMEPLUS_NATIVE_PORT` | `8463` | Proton native TCP port (used for streaming inserts) |
| `TIMEPLUS_USER` | `default` | Proton username |
| `TIMEPLUS_PASSWORD` | _(empty)_ | Proton password |

### CLI flags

```
./server --help

Flags:
  --config string                config file path
  --port int                     server port (default 8080)
  --timeplus-host string         Proton/Timeplus host (default "localhost")
  --timeplus-port int            Proton/Timeplus HTTP port (default 3218)
  --timeplus-native-port int     Proton/Timeplus native TCP port (default 8463)
  --timeplus-user string         Proton username (default "default")
  --timeplus-password string     Proton password
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
| Investigate | `/investigate` | Ad-hoc streaming SQL console backed by Proton |
| Onboarding | `/onboarding` | 4-step wizard to connect an OpenClaw agent (Claude Code coming soon) |
| Settings | `/settings` | Application configuration |

---

## Connecting an Agent

Use the **Onboarding** wizard (`/onboarding`) to connect your first OpenClaw agent. The wizard walks through:

1. **Select agent** — OpenClaw (Claude Code integration coming soon)
2. **Install plugin** — `npm install @timeplus/agentshield-openclaw-plugin`, configurable deployment ID / name / Timeplus URL
3. **OTel config** — optional OpenTelemetry diagnostics export to Timeplus (skippable)
4. **Activate rules** — select which security rules to enable; rules are disabled by default and must be explicitly activated

See [`agents/openclaw/README.md`](agents/openclaw/README.md) for the full OpenClaw + AgentShield setup guide.

---

## Architecture

```
agentshield/
  backend/              Go/Gin REST API (port 8080)
  frontend/             React + Vite SPA (port 5173 in dev)
  agents/openclaw/      OpenClaw + AgentShield plugin setup
  config.yaml           Default configuration file
  Dockerfile            Multi-stage build — frontend + backend in a single image
  docker-compose.yaml   Full stack: AgentShield + Proton
```

### Backend

```
backend/
  cmd/server/main.go              Cobra CLI, Viper config, Gin router, /docs routes
  internal/config/config.go       Config struct (file / env / flags)
  internal/models/models.go       Shared Go structs
  internal/mock/data.go           In-memory fallback data (threats, settings)
  internal/handlers/
    agents.go                     Agent list and detail (reads agentshield_hook_events)
    agent_sessions.go             Sessions, runs, waterfall trace
    metrics.go                    LLM token and tool-call metrics
    threats.go                    Security threat feed (reads agentshield_security_events)
    rules.go                      Rule CRUD, install/toggle/upgrade, MV management
    cost.go                       Cost summary and pricing table
    notifications.go              SSE stream for live alerts
    alerts.go                     Webhook receiver (called by Proton UDF)
    proton.go                     Streaming SQL proxy — NDJSON to browser
    docs.go                       API docs (Swagger UI + Redoc)
    investigate.go                Ad-hoc query interface
    settings.go                   Application settings
    onboarding.go                 Config generation helper
  internal/timeplus/
    client.go                     Query() and ExecHTTP() for Proton HTTP interface
    init.go                       Startup: create streams, seed rules (disabled by default), sync MVs
    rules.go                      MV management and builtin rule catalog
  internal/web/
    embed.go / noembed.go         Embedded or disk-served frontend assets
```

All security data is live from Proton. Rules, hook events, security events, and pricing are stored in Proton streams. The server falls back gracefully to mock data for threats and settings when Proton is unavailable.

### Proton Streams

| Stream | Type | Purpose |
|--------|------|---------|
| `agentshield_hook_events` | Append-only | Raw hook events from the OpenClaw plugin |
| `agentshield_security_events` | Append-only | Security detections written by rule MVs |
| `agentshield_rules` | Append-only | Rule state log; current state via `arg_max(updated_at)` |
| `agentshield_pricing` | Mutable | Per-model token pricing (primary key: provider + model) |
| `otel_traces` / `otel_logs` / `otel_metrics_*` | Append-only | OpenTelemetry data from OpenClaw diagnostics plugin |

Each enabled rule has a corresponding materialized view named `mv_rule_<id>` (e.g. `mv_rule_rp_001`) that queries `agentshield_hook_events` and writes matches to `agentshield_security_events`.

### Frontend

```
frontend/src/
  api/client.js               All fetch/SSE calls — pages never call fetch directly
  components/
    Sidebar.jsx               Fixed left nav with active route highlighting
    TopNav.jsx                Fixed top bar with glassmorphism
    Footer.jsx                Fixed status bar
    NotificationsProvider.jsx SSE connection, live alert context for the whole app
    Toggle.jsx                Accessible toggle switch
    Badge.jsx                 SeverityBadge and StatusBadge
    PageHeader.jsx            Shared page section header
  pages/                      One component per route
  App.jsx                     React Router v6 — / redirects to /dashboard
```

The Vite dev server proxies `/api/*` to `http://localhost:8080`. The Investigate page uses `@timeplus/proton-javascript-driver`'s `ndjsonStreamParser` for streaming SQL results.

---

## API Reference

The server hosts interactive API documentation at two endpoints:

| URL | UI |
|-----|----|
| [`/docs`](http://localhost:8080/docs) | Swagger UI |
| [`/docs/redoc`](http://localhost:8080/docs/redoc) | Redoc |
| [`/docs/openapi.json`](http://localhost:8080/docs/openapi.json) | Raw OpenAPI 3.0 spec |

### Endpoint Summary

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/dashboard` | Summary stats |
| `GET` | `/api/agents` | Agent list (`?page&limit`) |
| `GET` | `/api/agents/:id` | Agent detail (`?since&until`) |
| `GET` | `/api/agents/:id/sessions` | Session list (`?since&until`) |
| `GET` | `/api/agents/:id/sessions/:session_id` | Session detail with runs |
| `GET` | `/api/agents/:id/sessions/:session_id/runs/:run_id/trace` | Waterfall trace |
| `GET` | `/api/agents/:id/events` | Hook events (`?page&limit&since&until`) |
| `GET` | `/api/metrics/llm` | LLM token metrics (`?since&until`) |
| `GET` | `/api/metrics/tools` | Tool usage metrics (`?since&until`) |
| `GET` | `/api/threats` | Threat feed (`?severity&category&page&limit`) |
| `GET` | `/api/cost` | Cost summary (`?since&until`) |
| `GET` | `/api/cost/pricing` | Model pricing table |
| `POST` | `/api/cost/pricing` | Upsert pricing entry |
| `DELETE` | `/api/cost/pricing/:provider/:model` | Delete pricing entry |
| `GET` | `/api/rules` | Installed rules |
| `POST` | `/api/rules` | Create custom rule |
| `GET` | `/api/rules/catalog` | Builtin catalog with installed status |
| `GET` | `/api/rules/mv-status` | Materialized view health |
| `POST` | `/api/rules/sync` | Reconcile MV state without restart |
| `GET` | `/api/rules/:id` | Rule detail |
| `POST` | `/api/rules/:id/install` | Install from catalog |
| `POST` | `/api/rules/:id/toggle` | Enable / disable rule |
| `POST` | `/api/rules/:id/upgrade` | Upgrade rule SQL |
| `DELETE` | `/api/rules/:id` | Uninstall rule |
| `POST` | `/api/alerts/webhook` | Receive alert from Proton UDF |
| `GET` | `/api/notifications/stream` | SSE stream of live alerts |
| `GET` | `/api/investigate/tables` | Available Proton streams |
| `POST` | `/api/investigate/query` | Ad-hoc SQL query |
| `GET` | `/api/settings` | Application settings |
| `PUT` | `/api/settings` | Update settings |
| `POST` | `/api/onboarding/generate` | Generate agent config snippet |
| `POST` | `/api/proton/query` | Streaming SQL proxy (NDJSON) |

Agent `:id` is a compound `{deploymentId}__{agentId}` path parameter. `since` / `until` accept RFC 3339 timestamps.

---

## Tech Stack

**Frontend:** React 18, Vite, React Router v6, Tailwind CSS, Recharts, `@timeplus/proton-javascript-driver`

**Backend:** Go 1.23, Gin, Cobra, Viper, `proton-go-driver`

**Database:** [Timeplus Proton](https://github.com/timeplus-io/proton) (streaming SQL engine)

---

## Design System

The UI follows the **"Digital Sentry"** dark aesthetic — a command-center look designed for high information density. Key rules:

- Surface hierarchy via background color shifts (no border lines, no drop shadows)
- Primary accent: pink gradient `#ffb0cf → #f256a3`
- Glassmorphism on navigation bars (`backdrop-filter: blur(12px)`)
- Border radius max 4px (chips/badges use `rounded-full`)

Reference designs for each page live in `spec/application/<page>/code.html`. The full design system is documented in `spec/application/DESIGN.md`.

---

## Threat Coverage

Detection rules map to OWASP LLM Top 10 (2025):

| Rule Pack | ID | OWASP Coverage |
|-----------|----|----------------|
| Prompt Injection Shield | `rp-001` | LLM01: Prompt Injection |
| DLP Sentinel | `rp-002` | LLM02: Sensitive Information Disclosure, LLM07: System Prompt Leakage |
| Privilege Guard | `rp-003` | LLM08: Excessive Agency |
| Supply Chain Watch | `rp-004` | LLM09: Supply Chain Vulnerabilities |

Rules are disabled by default and activated through the Onboarding wizard or the Rule Packs page. Each enabled rule runs as a Proton materialized view that streams matches in real time to `agentshield_security_events`.
