# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

AgentShield is a real-time security monitoring and governance platform for AI coding agents (OpenClaw and Claude Code). It consists of a React SPA frontend and a Go/Gin backend. The backend connects to Timeplus Proton (a streaming SQL engine) for rules and security event data, and falls back to in-memory mock data for cost, threats, and metrics.

## Development Commands

### Backend (Go/Gin)
```bash
cd backend
CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server   # Start server on :8080
CGO_ENABLED=0 go build ./...     # Compile check
go mod tidy                      # Sync dependencies
```

> **macOS 26+ (Apple Silicon):** `go run` and regular `go build` trigger a dyld `missing LC_UUID load command` abort. Always use `CGO_ENABLED=0` to produce a fully static binary.

CLI flags (all optional, can also be set via config.yaml or env vars):
```bash
./server --port 8080 --timeplus-host localhost --timeplus-port 3218 --timeplus-user default --timeplus-password ""
```

### Frontend (React/Vite)
```bash
cd frontend
npm run dev                      # Start dev server on :5173 (proxies /api → :8080)
npm run build                    # Production build to dist/
npm run preview                  # Preview the production build
```

Both must run simultaneously for the app to work. The frontend Vite dev server proxies all `/api/*` requests to `http://localhost:8080`.

The Go binary also serves the compiled frontend as embedded static assets (`internal/web/`), so a single `./server` binary can serve both API and UI in production.

## Architecture

### Backend (`backend/`)

Four-layer structure:

- **`cmd/server/main.go`** — Entry point. Uses Cobra CLI + Viper for config. Sets up Gin router, CORS (allows localhost:5173 and :3000), registers all routes under `/api`, and serves the embedded SPA via `NoRoute`.
- **`internal/config/config.go`** — Config loading: defaults → `config.yaml` → env vars (`TIMEPLUS_HOST`, `TIMEPLUS_PORT`, `TIMEPLUS_USER`, `TIMEPLUS_PASSWORD`, `SERVER_PORT`) → CLI flags.
- **`internal/models/models.go`** — All shared Go structs.
- **`internal/mock/data.go`** — In-memory mock data for cost, threats, metrics, and agents. Some handlers still use this.
- **`internal/handlers/`** — One file per resource. Handlers that need Proton receive `cfg *config.Config` via a closure.
- **`internal/timeplus/`** — Proton integration layer:
  - `client.go` — `Query()` and `ExecHTTP()` for SELECT and DDL/DML against the Proton HTTP interface.
  - `init.go` — Startup: creates OTel streams, `agentshield_rules` stream, and `agentshield_security_events` stream if they don't exist; syncs MV state with installed rules.
  - `rules.go` — MV management (`CreateRuleMV`, `PauseRuleMV`, `ResumeRuleMV`, `DropRuleMV`, `MVName`) and builtin rule `Catalog()`.
- **`internal/web/`** — `embed.go` (embed frontend dist) / `noembed.go` (serve from disk) build-tag variants.

Adding a new API resource: add structs to `models.go`, add mock data to `mock/data.go` if needed, create a handler file, and register routes in `main.go`.

### API Endpoints

All endpoints return JSON. List endpoints support `?page=` and `?limit=` query params.

| Method | Path | Handler | Notes |
|--------|------|---------|-------|
| GET | `/api/dashboard` | `GetDashboard` | Summary stats |
| GET | `/api/agents` | `ListAgents` | Paginatable |
| GET | `/api/agents/:id` | `GetAgent` | |
| GET | `/api/agents/:id/sessions` | `ListSessions` | |
| GET | `/api/agents/:id/sessions/:session_id` | `GetSession` | |
| GET | `/api/agents/:id/events` | `ListEvents` | |
| GET | `/api/metrics/llm` | `GetLLMMetrics` | |
| GET | `/api/metrics/tools` | `GetToolMetrics` | |
| GET | `/api/threats` | `ListThreats` | Supports `?severity=` and `?category=` filters |
| GET | `/api/cost` | `GetCost` | Computed from mock daily spend |
| GET | `/api/rules` | `ListRules` | Reads from Proton `agentshield_rules` stream |
| GET | `/api/rules/catalog` | `ListCatalog` | Builtin catalog with installed status |
| GET | `/api/rules/mv-status` | `MVStatus` | Health of Proton materialized views |
| POST | `/api/rules/sync` | `SyncMVs` | Reconcile MV state without restart |
| GET | `/api/rules/:id` | `GetRule` | |
| POST | `/api/rules/:id/install` | `InstallRule` | Install from builtin catalog |
| POST | `/api/rules/:id/toggle` | `ToggleRule` | Flips `enabled`; pauses/resumes MV |
| POST | `/api/rules/:id/upgrade` | `UpgradeRule` | Upgrades version; rebuilds MV |
| DELETE | `/api/rules/:id` | `UninstallRule` | Soft-delete + drops MV |
| POST | `/api/alerts/webhook` | `AlertWebhook` | Receives rule-triggered alerts |
| GET | `/api/notifications/stream` | `NotificationsSSEHandler` | SSE stream of live alerts |
| GET | `/api/investigate/tables` | `ListTables` | |
| POST | `/api/investigate/query` | `RunQuery` | Mock query execution |
| GET | `/api/settings` | `GetSettings` | |
| PUT | `/api/settings` | `UpdateSettings` | |
| POST | `/api/onboarding/generate` | `GenerateConfig` | |
| POST | `/api/proton/query` | `ProtonQueryHandler` | Streaming proxy to Proton (NDJSON) |

### Frontend (`frontend/src/`)

- **`api/client.js`** — Single API client module. All `fetch` calls go through `api.xxx()` functions here. Pages never call `fetch` directly. Uses `@timeplus/proton-javascript-driver` for `api.protonQuery()` (async generator, NDJSON streaming).
- **`components/`** — Shared components:
  - `Sidebar` — Fixed left nav with active route highlighting via NavLink.
  - `TopNav` — Fixed top bar with glassmorphism.
  - `Footer` — Fixed status bar.
  - `Toggle` — Accessible switch.
  - `Badge` — `SeverityBadge` and `StatusBadge`.
  - `NotificationsProvider` — Context provider that opens the SSE stream and exposes live alert notifications to the whole app.
  - `PageHeader` — Shared page section header.
- **`pages/`** — One file per route. Each page follows the same pattern: fetch data in `useEffect` → `useState` for data/loading/error → render Sidebar + TopNav + Footer layout with `<main className="pl-64 pt-16 pb-8 min-h-screen">`.
- **`App.jsx`** — React Router v6 routes. `/` redirects to `/dashboard`.

### Routes

| Path | Component |
|------|-----------|
| `/dashboard` | `Dashboard` |
| `/agents` | `AgentList` |
| `/agents/:id` | `AgentDetail` |
| `/threats` | `Threats` |
| `/cost` | `CostGovernance` |
| `/rules` | `RulePacks` |
| `/rules/:id` | `RuleDetail` |
| `/investigate` | `Investigate` |
| `/onboarding` | `Onboarding` |

The Onboarding page (`/onboarding`) is the only page that does **not** use the Sidebar/Footer layout (it's a full-page wizard).

### Design System

The UI follows the "Digital Sentry" dark theme. Key constraints enforced throughout:
- **No border lines for sectioning** — use background color shifts between surface tiers instead
- **No drop shadows** — use tonal layering (darker bg = lower, lighter bg = higher)
- **Gradient CTAs** — primary buttons use `bg-gradient-to-br from-[#ffb0cf] to-[#f256a3]` with `text-[#3d0023]`
- **Nav glassmorphism** — `backdrop-blur-md` on TopNav and sticky headers
- **Border radius max 4px** — except chips/badges which use `rounded-full`

All Tailwind color tokens matching the design system are configured in `frontend/tailwind.config.js`. Use the named tokens (`bg-surface-container-high`, `text-primary`, `text-error`, etc.) rather than raw hex values where possible.

## Spec Reference

`spec/application/` contains the authoritative UI designs:
- Each subdirectory has a `code.html` (reference implementation) and `screen.png` (screenshot)
- `spec/application/DESIGN.md` — design system principles ("Digital Sentry" aesthetic)
- `spec/application/guide.md` — Timeplus Console component reference (light-mode, used as secondary reference)
- `spec/application/agent_security_application_product_requirements_document.md` — full PRD

When implementing or modifying a page, the corresponding `code.html` is the source of truth for layout and styling decisions.

## Timeplus Proton Integration

Rules and security events are persisted in Proton streams:
- `agentshield_rules` — append-only log of rule mutations; current state queried via `arg_max(... updated_at)` GROUP BY.
- `agentshield_security_events` — security detections written by rule materialized views.

Each enabled rule has a corresponding materialized view named `mv_rule_<id>` (e.g. `mv_rule_rp_001`). The MV queries `hook_events` and writes matches to `agentshield_security_events`. Toggle/install/uninstall operations create, pause, resume, or drop these MVs atomically alongside the rule state mutation.

On startup, `timeplus.Init()` creates missing streams and syncs MV state. This is non-fatal — the server starts even if Proton is unavailable.
