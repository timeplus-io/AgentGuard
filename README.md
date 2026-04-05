# AgentShield

Real-time security monitoring and governance platform for AI coding agents. AgentShield ingests telemetry from **OpenClaw** and **Claude Code** agents, detects threats (prompt injection, data exfiltration, privilege escalation, supply chain attacks), enforces cost budgets, and surfaces everything through a streaming security dashboard.

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

> **macOS 26+ note:** `go run` triggers a dyld `missing LC_UUID` abort on macOS 26 / Apple Silicon. Always build with `CGO_ENABLED=0` first, then run the binary.

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
  user: default
  password: ""
```

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `SERVER_PORT` | `8080` | HTTP port the server listens on |
| `TIMEPLUS_HOST` | `localhost` | Proton/Timeplus hostname |
| `TIMEPLUS_PORT` | `3218` | Proton HTTP query port |
| `TIMEPLUS_USER` | `default` | Proton username |
| `TIMEPLUS_PASSWORD` | _(empty)_ | Proton password |

### CLI flags

```
./server --help

Flags:
  --config string             config file path
  --port int                  server port (default 8080)
  --timeplus-host string      Proton/Timeplus host (default "localhost")
  --timeplus-port int         Proton/Timeplus HTTP port (default 3218)
  --timeplus-user string      Proton username (default "default")
  --timeplus-password string  Proton password
```

---

## Features

| Page | Route | Description |
|------|-------|-------------|
| Security Posture | `/dashboard` | KPI bento grid, live threat feed, threat categories by OWASP, recent agent activity |
| Agent Fleet | `/agents` | All monitored agents with health index, throughput, status |
| Agent Detail | `/agents/:id` | Per-agent metrics, event timeline, hook events |
| Threats | `/threats` | Filterable threat feed by severity and category |
| Cost Governance | `/cost` | Spend trends, token breakdown, per-model costs, budget policies, anomaly history |
| Rule Packs | `/rules` | Toggle pre-built detection rules (Prompt Injection Shield, DLP Sentinel, etc.) |
| Investigate | `/investigate` | Ad-hoc streaming SQL console backed by Proton |
| Onboarding | `/onboarding` | 4-step wizard to connect an OpenClaw or Claude Code agent |
| Settings | `/settings` | Organization config, integrations (Slack, PagerDuty), user RBAC |

---

## Architecture

```
agentshield/
  backend/              Go/Gin REST API (port 8080)
  frontend/             React + Vite SPA (port 5173 in dev)
  config.yaml           Default configuration file
  Dockerfile            Multi-stage build — frontend + backend in a single image
  docker-compose.yaml   Full stack: AgentShield + Proton
```

### Backend

```
backend/
  cmd/server/main.go            Cobra CLI, Viper config, Gin router
  internal/config/config.go     Config struct loaded from file / env / flags
  internal/models/models.go     Shared struct definitions
  internal/mock/data.go         In-memory mock data (agents, threats, costs, rules, settings)
  internal/handlers/            One handler file per resource
    proton.go                   Proton query proxy — streams NDJSON to the browser
```

Mock data is used for all pages except the SQL Console, which proxies queries directly to Proton. The proxy avoids browser CORS restrictions — the frontend sends SQL to `/api/proton/query` and the backend forwards it to Proton using the server-side connection config.

### Frontend

```
frontend/src/
  api/client.js       All fetch calls — pages never call fetch directly
  components/         Sidebar, TopNav, Footer, Toggle, Badge, PageHeader
  pages/              One component per route
  App.jsx             React Router v6 — / redirects to /dashboard
```

The Vite dev server proxies `/api/*` to `http://localhost:8080`. The SQL Console uses `@timeplus/proton-javascript-driver`'s `ndjsonStreamParser` to consume the streamed response.

---

## API Reference

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/dashboard` | KPIs, live threat feed, threat categories, recent agents |
| `GET` | `/api/agents` | Agent list (`?page=1&limit=15`) |
| `GET` | `/api/agents/:id` | Single agent detail |
| `GET` | `/api/threats` | Threat list (`?severity=Critical&category=Prompt+Injection&page=1`) |
| `GET` | `/api/cost` | Cost governance data |
| `GET` | `/api/rules` | Rule packs |
| `POST` | `/api/rules/:id/toggle` | Toggle a rule pack on/off |
| `GET` | `/api/investigate/tables` | Available stream schemas |
| `POST` | `/api/investigate/query` | Execute mock SQL query `{"query": "SELECT ..."}` |
| `POST` | `/api/proton/query` | Proxy streaming SQL to Proton `{"sql": "SELECT ..."}` — returns NDJSON |
| `GET` | `/api/settings` | Settings |
| `PUT` | `/api/settings` | Update settings |
| `POST` | `/api/onboarding/generate` | Generate agent config `{"agent_type": "openclaw"\|"claude_code", "endpoint": "..."}` |

---

## Tech Stack

**Frontend:** React 18, Vite, React Router v6, Tailwind CSS, Recharts, `@timeplus/proton-javascript-driver`, Inter

**Backend:** Go 1.23, Gin, Cobra, Viper

**Database:** [Timeplus Proton](https://github.com/timeplus-io/proton) (streaming SQL engine)

---

## Design System

The UI follows the **"Digital Sentry"** dark aesthetic — a command-center look inspired by high-end technical journals. Key rules:

- Surface hierarchy via background color shifts (no border lines for sectioning)
- Primary accent: pink gradient `#ffb0cf → #f256a3`
- Glassmorphism on navigation bars (`backdrop-filter: blur(12px)`)
- Inter font with dramatic scale contrast between headings and body text

Reference designs for each page live in `spec/application/<page>/code.html`. The full design system is documented in `spec/application/DESIGN.md`.

---

## Threat Coverage

AgentShield detection rules map to OWASP LLM Top 10 (2025) and OWASP Agentic Applications Top 10:

| Rule Pack | OWASP Coverage |
|-----------|----------------|
| Prompt Injection Shield | LLM01: Prompt Injection |
| DLP Sentinel | LLM02: Sensitive Information Disclosure, LLM07: System Prompt Leakage |
| Privilege Guard | LLM08: Excessive Agency |
| Supply Chain Watch | LLM09: Supply Chain Vulnerabilities |
| Cost Sentinel | Anomalous token consumption / runaway loops |
| Compliance Logger | EU AI Act Article 12, NIST AI RMF, ISO/IEC 42001 |
