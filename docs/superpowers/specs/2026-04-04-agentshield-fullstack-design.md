# AgentShield Full-Stack Implementation Design

**Date:** 2026-04-04  
**Project:** AgentShield — Real-time AI Agent Security Platform  
**Stack:** React (Vite) + Go (Gin)

---

## 1. Overview

AgentShield is a real-time security monitoring and governance platform for AI coding agents (OpenClaw and Claude Code). This document describes the full-stack implementation: a React single-page application (SPA) for the frontend and a Go/Gin backend providing REST/HTTP APIs with mock data.

---

## 2. Frontend Architecture

### Tech Stack
- **Framework:** React 18 with Vite
- **Routing:** React Router v6
- **Styling:** Tailwind CSS (configured with the AgentShield design token system)
- **Icons:** Material Symbols Outlined (Google Fonts)
- **Charts:** Recharts (for Cost Governance bar/line charts)
- **Font:** Inter (Google Fonts)
- **HTTP client:** fetch (native, no extra library needed for mock data)

### Design System
Dark-mode "Digital Sentry" aesthetic from `spec/application/shield_console/DESIGN.md`:
- Background: `#15121d`
- Sidebar: `#211e29`
- Surface containers: `#1d1a25` / `#211e29` / `#2c2834` / `#37333f`
- Primary accent: `#ffb0cf` → `#f256a3` (gradient)
- Error: `#ffb4ab`, Error container: `#93000a`
- Tertiary: `#ffb2b8`
- No border lines for sectioning — use background color shifts instead
- Border radius: 2px default, 4px large, no shadows (use tonal layering)

### Project Structure
```
frontend/
  src/
    api/           # API client functions (fetch wrappers)
    components/    # Shared components (Sidebar, TopNav, Footer, Toggle, etc.)
    pages/         # One file per route
      Dashboard.jsx
      AgentList.jsx
      AgentDetail.jsx
      Threats.jsx
      CostGovernance.jsx
      RulePacks.jsx
      Investigate.jsx
      Onboarding.jsx
      Settings.jsx
    App.jsx        # Router setup
    main.jsx       # Vite entry point
    index.css      # Tailwind base + custom scrollbar styles
  tailwind.config.js
  vite.config.js
  index.html
  package.json
```

### Routes
| Path | Component | Description |
|------|-----------|-------------|
| `/` | Redirect | Redirects to `/dashboard` |
| `/dashboard` | Dashboard | Security Posture — KPI bento grid, live threat feed, threat categories, recent agent activity table |
| `/agents` | AgentList | Agent Fleet — paginated table with health index, throughput, status badges |
| `/agents/:id` | AgentDetail | Single agent detail — metrics, events timeline, hook events |
| `/threats` | Threats | Threats Feed — filter bar (severity/category/date), sortable table, live data stream |
| `/cost` | CostGovernance | Cost Governance — spend KPIs, bar chart, token breakdown, per-model breakdown, budget policies, anomaly history |
| `/rules` | RulePacks | Rule Packs — card grid with toggle, recent enforcement activity, stats |
| `/investigate` | Investigate | SQL Console — query editor area, results table |
| `/onboarding` | Onboarding | Onboarding Wizard — 4-step wizard (agent type → telemetry → hooks → rule packs) |
| `/settings` | Settings | Settings — tabbed (General / Integrations / Users & RBAC) |

### Shared Components
- `Sidebar` — fixed left nav (w-64), logo, nav links with active state, "Deploy Agent" CTA button
- `TopNav` — fixed top bar, global search input, monitor/notifications icons, user avatar
- `Footer` — sticky status bar with system health, latency, secure link indicator
- `Toggle` — accessible toggle switch (role="switch")
- `Badge` / `StatusBadge` — severity and status chips
- `ProgressBar` — thin gradient progress bars used in health index and threat categories

---

## 3. Backend Architecture

### Tech Stack
- **Language:** Go 1.21+
- **Framework:** Gin
- **Data:** In-memory mock data (seeded at startup, no database)
- **CORS:** `github.com/gin-contrib/cors` — allow `http://localhost:5173`
- **Structure:** Standard Go project layout

### Project Structure
```
backend/
  cmd/
    server/
      main.go        # Gin setup, route registration, server start
  internal/
    handlers/        # One handler file per resource
      dashboard.go
      agents.go
      threats.go
      cost.go
      rules.go
      investigate.go
      onboarding.go
      settings.go
    mock/
      data.go        # All mock data seeded at startup
    models/
      models.go      # Shared struct definitions
  go.mod
  go.sum
```

### API Endpoints

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `GET` | `/api/dashboard` | `GetDashboard` | KPIs + live threat feed + agent activity |
| `GET` | `/api/agents` | `ListAgents` | Agent fleet, supports `?page=` `?limit=` |
| `GET` | `/api/agents/:id` | `GetAgent` | Single agent detail |
| `GET` | `/api/threats` | `ListThreats` | Threats, supports `?severity=` `?category=` `?page=` |
| `GET` | `/api/cost` | `GetCost` | Cost governance data |
| `GET` | `/api/rules` | `ListRules` | Rule packs |
| `POST` | `/api/rules/:id/toggle` | `ToggleRule` | Toggle rule pack enabled/disabled |
| `GET` | `/api/investigate/tables` | `ListTables` | Available stream/table names |
| `POST` | `/api/investigate/query` | `RunQuery` | Mock SQL query execution, returns rows |
| `GET` | `/api/settings` | `GetSettings` | Settings data |
| `PUT` | `/api/settings` | `UpdateSettings` | Update settings |
| `POST` | `/api/onboarding/generate` | `GenerateConfig` | Generate agent config (OpenClaw JSON or Claude Code env vars) |

All responses: `Content-Type: application/json`. Error format: `{"error": "message"}`.

### Mock Data Summary
- **Agents:** 12 agents across 3 regions with varying statuses (ACTIVE, THROTTLED, SUSPENDED)
- **Threats:** 50 threats across severity levels (Critical, Warning, Info), categories (Prompt Injection, Data Exfiltration, Supply Chain, Privilege Escalation)
- **Cost:** Monthly spend totals, 7-day bar chart data, token breakdown, per-agent/model cost table, budget policies, anomaly history
- **Rules:** 6 rule packs (Prompt Injection Shield, DLP Sentinel, Privilege Guard, Supply Chain Watch, Cost Sentinel, Compliance Logger) with enabled/disabled state
- **Dashboard:** KPI aggregates derived from mock agents and threats, live threat feed (latest 10 threats), recent agent activity (latest 5 agents)
- **Settings:** Organization name, OTel endpoint, retention days, integrations (Slack/PagerDuty), user/role list

---

## 4. Data Flow

```
React Page
  → api/client.js (fetch /api/...)
    → Gin handler
      → mock/data.go (in-memory structs)
        ← JSON response
          ← React renders UI
```

State is managed locally in each page component with `useState` / `useEffect`. No global state manager needed (data is read-mostly, no cross-page sharing).

---

## 5. Development Setup

```bash
# Backend
cd backend && go run cmd/server/main.go   # runs on :8080

# Frontend
cd frontend && npm install && npm run dev  # runs on :5173, proxies /api to :8080
```

Vite proxies `/api` → `http://localhost:8080` to avoid CORS during development.

---

## 6. Key Design Decisions

1. **Mock data in-memory (no DB):** The backend delivers realistic mock data without any infrastructure dependency. This matches the requirement explicitly.

2. **No global state manager:** Each page fetches its own data independently. For a security dashboard with page-level data, this is simpler and sufficient.

3. **Vite proxy for /api:** Eliminates CORS friction during development. In production, the Go server can serve the built frontend as static files.

4. **Recharts for charts:** Lightweight, React-native, no D3 complexity. The Cost page bar chart and token breakdown use Recharts bar/pie components styled with the design token colors.

5. **Gin chosen over chi/net/http:** Gin provides built-in JSON binding, middleware chaining, and route grouping with minimal boilerplate — ideal for a mock API server.

---

## 7. Out of Scope

- Real OTel/Timeplus integration (backend is mock only)
- Authentication/session management
- WebSocket real-time updates (live feed is simulated with static data)
- Database persistence
