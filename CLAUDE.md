# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

AgentShield is a real-time security monitoring and governance platform for AI coding agents (OpenClaw and Claude Code). It consists of a React SPA frontend and a Go/Gin backend that currently serves mock data. The backend is designed to eventually connect to Timeplus Proton (a streaming SQL engine), but all data is in-memory for now.

## Development Commands

### Backend (Go/Gin)
```bash
cd backend
CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server   # Start server on :8080
CGO_ENABLED=0 go build ./...     # Compile check
go mod tidy                      # Sync dependencies
```

> **macOS 26+ (Apple Silicon):** `go run` and regular `go build` trigger a dyld `missing LC_UUID load command` abort. Always use `CGO_ENABLED=0` to produce a fully static binary.

### Frontend (React/Vite)
```bash
cd frontend
npm run dev                      # Start dev server on :5173 (proxies /api → :8080)
npm run build                    # Production build to dist/
npm run preview                  # Preview the production build
```

Both must run simultaneously for the app to work. The frontend Vite dev server proxies all `/api/*` requests to `http://localhost:8080`.

## Architecture

### Backend (`backend/`)
Three-layer structure with no database:

- **`cmd/server/main.go`** — Entry point. Sets up Gin router, CORS (allows localhost:5173 and :3000), and registers all routes under `/api`.
- **`internal/models/models.go`** — All shared Go structs. Every handler input/output type lives here.
- **`internal/mock/data.go`** — Package-level `var` slices containing all mock data seeded at startup. Rules data is intentionally mutable (to support toggle persistence within a server session). All other mock data is read-only.
- **`internal/handlers/`** — One file per resource. Each handler reads from `mock` package data, applies filtering/pagination where relevant, and responds with `c.JSON(http.StatusOK, ...)`.

Adding a new API resource means: add structs to `models.go`, add data to `mock/data.go`, create a handler file, and register routes in `main.go`.

### Frontend (`frontend/src/`)

- **`api/client.js`** — Single API client module. All `fetch` calls go through `api.xxx()` functions here. Pages never call `fetch` directly.
- **`components/`** — Five shared components used across all pages: `Sidebar` (fixed left nav with active route highlighting via NavLink), `TopNav` (fixed top bar with glassmorphism), `Footer` (fixed status bar), `Toggle` (accessible switch), `Badge` (`SeverityBadge` and `StatusBadge`).
- **`pages/`** — One file per route. Each page follows the same pattern: fetch data in `useEffect` → `useState` for data/loading/error → render Sidebar + TopNav + Footer layout with `<main className="pl-64 pt-16 pb-8 min-h-screen">`.
- **`App.jsx`** — React Router v6 routes. `/` redirects to `/dashboard`.

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

## API Contracts

All endpoints return JSON. List endpoints support `?page=` and `?limit=` query params. The threats endpoint additionally supports `?severity=` and `?category=` filters. The rules toggle endpoint (`POST /api/rules/:id/toggle`) mutates the in-memory `mock.Rules` slice and persists for the server session.

Mock query execution (`POST /api/investigate/query`) inspects the query string to route results to the appropriate mock table (otel_traces, otel_logs, otel_metrics, hook_events).
