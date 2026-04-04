# AgentShield

Real-time security monitoring and governance platform for AI coding agents. AgentShield ingests telemetry from **OpenClaw** and **Claude Code** agents, detects threats (prompt injection, data exfiltration, privilege escalation, supply chain attacks), enforces cost budgets, and surfaces everything through a streaming security dashboard.

Built on [Timeplus Proton](https://github.com/timeplus-io/proton) — a streaming SQL engine processing 90M events/second at 4ms latency.

---

## Quick Start

### Docker (recommended)

```bash
docker build -t agentshield .
docker run -p 8080:8080 agentshield
```

Open [http://localhost:8080](http://localhost:8080) — the Go server serves both the API and the compiled frontend.

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

## Features

| Page | Route | Description |
|------|-------|-------------|
| Security Posture | `/dashboard` | KPI bento grid, live threat feed, threat categories by OWASP, recent agent activity |
| Agent Fleet | `/agents` | All monitored agents with health index, throughput, status |
| Agent Detail | `/agents/:id` | Per-agent metrics, event timeline, hook events |
| Threats | `/threats` | Filterable threat feed by severity and category |
| Cost Governance | `/cost` | Spend trends, token breakdown, per-model costs, budget policies, anomaly history |
| Rule Packs | `/rules` | Toggle pre-built detection rules (Prompt Injection Shield, DLP Sentinel, etc.) |
| Investigate | `/investigate` | Ad-hoc streaming SQL console against agent telemetry streams |
| Onboarding | `/onboarding` | 4-step wizard to connect an OpenClaw or Claude Code agent |
| Settings | `/settings` | Organization config, integrations (Slack, PagerDuty), user RBAC |

---

## Architecture

```
agentshield/
  backend/        Go/Gin REST API (mock data, port 8080)
  frontend/       React + Vite SPA (port 5173 in dev)
  spec/           UI designs (code.html + screen.png per page) and PRD
  docs/           Design specs and implementation plans
  Dockerfile      Multi-stage build — frontend + backend in a single image
```

### Backend

```
backend/
  cmd/server/main.go          Gin router, CORS, route registration
  internal/models/models.go   All shared struct definitions
  internal/mock/data.go       In-memory mock data (agents, threats, costs, rules, settings)
  internal/handlers/          One handler file per resource
```

The backend currently serves **mock data** — no database or Timeplus connection required. All data lives in package-level vars in `internal/mock/data.go`. Rule pack toggles persist for the server session.

### Frontend

```
frontend/src/
  api/client.js       All fetch calls — pages never call fetch directly
  components/         Sidebar, TopNav, Footer, Toggle, Badge
  pages/              One component per route
  App.jsx             React Router v6 — / redirects to /dashboard
```

The Vite dev server proxies `/api/*` to `http://localhost:8080`, so no CORS configuration is needed during development.

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
| `GET` | `/api/settings` | Settings |
| `PUT` | `/api/settings` | Update settings |
| `POST` | `/api/onboarding/generate` | Generate agent config `{"agent_type": "openclaw"\|"claude_code", "endpoint": "..."}` |

---

## Tech Stack

**Frontend:** React 18, Vite, React Router v6, Tailwind CSS, Recharts, Material Symbols Outlined, Inter

**Backend:** Go 1.21, Gin, gin-contrib/cors

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
