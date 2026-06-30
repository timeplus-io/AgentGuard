# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

AgentGuard is a real-time security monitoring and governance platform for AI coding agents (OpenClaw and Claude Code). It consists of a React SPA frontend and a Go/Gin backend. The backend connects to Timeplus Enterprise (a streaming SQL engine) for all data — agents, sessions, events, metrics, costs, threats, and rules. There is no mock data layer; every endpoint queries Timeplus directly.

## Development Commands

### Quickstart (both servers)
```bash
make dev                         # Start backend :8080 + frontend :5173 in parallel
```

Or with Docker Compose (includes Timeplus Proton):
```bash
docker compose up --build        # AgentGuard + Timeplus at http://localhost:8080
```

### Backend (Go/Gin)
```bash
cd backend
CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server   # Start server on :8080
CGO_ENABLED=0 go build ./...     # Compile check
go mod tidy                      # Sync dependencies
CGO_ENABLED=0 go test ./...      # Run all backend tests
CGO_ENABLED=0 go test ./internal/handlers/... -run TestTimeline  # Run a single test
```

> **macOS 26+ (Apple Silicon):** `go run` and regular `go build` trigger a dyld `missing LC_UUID load command` abort. Always use `CGO_ENABLED=0` to produce a fully static binary.

CLI flags (all optional, can also be set via config.yaml or env vars):
```bash
./server --port 8080 --timeplus-host localhost --timeplus-port 3218
# Credentials come from env vars: TIMEPLUS_USER, TIMEPLUS_PASSWORD
```

### Frontend (React/Vite)
```bash
cd frontend
npm run dev                      # Start dev server on :5173 (proxies /api → :8080)
npm run build                    # Production build to dist/
npm run preview                  # Preview the production build
npm run lint                     # ESLint
```

Both must run simultaneously for the app to work. The frontend Vite dev server proxies all `/api/*` requests to `http://localhost:8080`.

The Go binary also serves the compiled frontend as embedded static assets (`internal/web/`), so a single `./server` binary can serve both API and UI in production.

### Agent Plugins (TypeScript/Node)
Each agent plugin lives in `agents/<agent>/agentguard-plugin/` and has its own build:
```bash
cd agents/claudecode/agentguard-plugin && npm install && npm run build
cd agents/openclaw/agentguard-plugin  && npm install && npm run build
# Install/configure hooks into ~/.claude/settings.json (claudecode):
cd agents/claudecode && make install
# Run plugin unit tests:
cd agents/claudecode/agentguard-plugin && npm test
cd agents/openclaw/agentguard-plugin  && npm test
```

### Production build
```bash
make build    # frontend-build → web-sync → backend-build (single binary with embedded UI)
make lint     # golangci-lint (backend) + ESLint (frontend)
```

## Architecture

### Backend (`backend/`)

Four-layer structure:

- **`cmd/server/main.go`** — Entry point. Uses Cobra CLI + Viper for config. Sets up Gin router, CORS (allows localhost:5173 and :3000), registers all routes under `/api`, and serves the embedded SPA via `NoRoute`.
- **`internal/config/config.go`** — Config loading: defaults → `config.yaml` → env vars (`TIMEPLUS_HOST`, `TIMEPLUS_PORT`, `TIMEPLUS_USER`, `TIMEPLUS_PASSWORD`, `SERVER_PORT`) → CLI flags.
- **`internal/models/models.go`** — All shared Go structs.
- **`internal/handlers/`** — One file per resource. All handlers receive `cfg *config.Config` via a closure and query Timeplus Enterprise directly. There is no mock data layer.
- **`internal/timeplus/`** — Proton integration layer:
  - `client.go` — `Query()` and `ExecHTTP()` for SELECT and DDL/DML against the Proton HTTP interface.
  - `init.go` — Provisioning: called by the setup wizard (`POST /api/setup/provision`) to create OTel streams, `agentguard_rules` stream, and `agentguard_security_events` stream if they don't exist; syncs MV state with installed rules. Not called at server startup.
  - `rules.go` — MV management (`CreateRuleMV`, `PauseRuleMV`, `ResumeRuleMV`, `DropRuleMV`, `MVName`) and builtin rule `Catalog()`.
- **`internal/web/`** — `embed.go` (embed frontend dist) / `noembed.go` (serve from disk) build-tag variants.

Adding a new API resource: add structs to `models.go`, create a handler file in `internal/handlers/`, and register routes in `main.go`. There is no mock data layer — all handlers query Timeplus Enterprise directly.

### API Endpoints

All endpoints return JSON. List endpoints support `?page=` and `?limit=` query params.

| Method | Path | Handler | Notes |
|--------|------|---------|-------|
| GET | `/api/auth/status` | `AuthStatus` | Returns session state |
| POST | `/api/auth/login` | `Login` | |
| POST | `/api/auth/logout` | `Logout` | |
| POST | `/api/setup/test-connection` | `TestConnection` | Validates Timeplus reachability |
| POST | `/api/setup/provision` | `Provision` | Runs `timeplus.Init()`; completes setup wizard |
| GET | `/api/dashboard` | `GetDashboard` | Summary stats |
| GET | `/api/agents` | `ListAgents` | Paginatable |
| GET | `/api/agents/:id` | `GetAgent` | |
| GET | `/api/agents/:id/sessions` | `ListSessions` | |
| GET | `/api/agents/:id/sessions/:session_id` | `GetSession` | |
| GET | `/api/agents/:id/sessions/:session_id/runs/:run_id/trace` | `GetRunTrace` | Waterfall trace for a single run |
| GET | `/api/agents/:id/events` | `ListEvents` | |
| GET | `/api/metrics/llm` | `GetLLMMetrics` | |
| GET | `/api/metrics/tools` | `GetToolMetrics` | |
| GET | `/api/threats` | `ListThreats` | Supports `?severity=`, `?status=`, `?agent_id=` filters |
| GET | `/api/threats/:id` | `GetThreat` | |
| POST | `/api/threats/:id/acknowledge` | `AcknowledgeThreat` | Upserts status to agentguard_threats |
| POST | `/api/threats/:id/clear` | `ClearThreat` | Upserts status to agentguard_threats |
| GET | `/api/cost` | `GetCost` | Computed from agentguard_hook_events + agentguard_pricing. Month-to-date windows are UTC calendar months; prior-period delta compares the same elapsed window in the previous month, clipped to the start of the current month when that month was shorter. Budget-derived fields populate only when at least one budget is configured via `/api/cost/budget`. |
| GET | `/api/cost/pricing` | `ListPricing` | Reads from agentguard_pricing stream |
| POST | `/api/cost/pricing` | `UpsertPricing` | Inserts into agentguard_pricing |
| DELETE | `/api/cost/pricing/:provider/:model` | `DeletePricing` | Deletes from agentguard_pricing |
| GET | `/api/cost/budget` | `GetCostBudgets` | Current per-deployment monthly budgets via arg_max over agentguard_cost_budgets |
| POST | `/api/cost/budget` | `UpsertCostBudget` | Appends a row to agentguard_cost_budgets; `updated_by` is the session user. Send `deleted: true` to retire a budget. |
| GET | `/api/rules` | `ListRules` | Reads from `agentguard_rules` stream |
| GET | `/api/rules/catalog` | `ListCatalog` | Builtin catalog with installed status |
| POST | `/api/rules/catalog/import` | `ImportCatalog` | Import rules from JSON |
| GET | `/api/rules/catalog/export` | `ExportCatalog` | Export catalog as JSON |
| GET | `/api/rules/mv-status` | `MVStatus` | Health of Timeplus materialized views |
| POST | `/api/rules/sync` | `SyncMVs` | Reconcile MV state without restart |
| POST | `/api/rules` | `CreateRule` | Create a custom rule |
| GET | `/api/rules/:id` | `GetRule` | |
| POST | `/api/rules/:id/install` | `InstallRule` | Install from builtin catalog |
| POST | `/api/rules/:id/toggle` | `ToggleRule` | Flips `enabled`; pauses/resumes MV |
| POST | `/api/rules/:id/upgrade` | `UpgradeRule` | Upgrades version; rebuilds MV |
| POST | `/api/rules/:id/policy` | `SetRuleBlockPolicy` | Sets `log_only`/`auto_block`/`hold` without toggling enabled |
| DELETE | `/api/rules/:id` | `UninstallRule` | Soft-delete + drops MV |
| POST | `/api/holds` | `CreateHold` | Synchronous hold-and-wait; called by agent plugins (unauthenticated) |
| GET | `/api/holds` | `ListHolds` | |
| GET | `/api/holds/:hold_id` | `GetHold` | |
| POST | `/api/holds/:hold_id/decide` | `DecideHold` | Human Approve/Deny |
| POST | `/api/holds/_drain` | `DrainHolds` | Operator: abandon all pending holds |
| POST | `/api/holds/_abandon-session/:session_id` | `AbandonSession` | Plugin fire-and-forget on SessionEnd (unauthenticated) |
| GET | `/api/activity` | `ListActivity` | Recent tool calls across all sessions |
| POST | `/api/alerts/webhook` | `AlertWebhook` | Receives rule-triggered alerts (unauthenticated) |
| GET | `/api/notifications/stream` | `NotificationsSSEHandler` | SSE stream of live alerts |
| GET | `/api/investigate/tables` | `ListTables` | Discovers streams from system.tables |
| POST | `/api/onboarding/generate` | `GenerateConfig` | |
| GET | `/api/onboarding/install-script` | `ServeInstallScript` | Returns agent install script |
| POST | `/api/proton/query` | `ProtonQueryHandler` | Streaming proxy to Timeplus (NDJSON) |
| GET | `/api/config` | `GetConfig` | Full effective config with per-key source (default/file/env/flag) + restart/reprovision flags; secrets report `set` not value |
| PUT | `/api/config` | `UpdateConfig` | Atomic validate + merge-write changed keys to config.yaml; hot-applies non-restart keys to the live config |
| POST | `/api/config/test-timeplus` | `TestTimeplusConfig` | Tests a candidate Timeplus host/port using session credentials |
| POST | `/api/config/test-dlp` | `TestDLPConfig` | Tests DLP `base_url` reachability (GET `/models`); never 5xx on unreachable |
| POST | `/api/dlp/query` | `DLPQuery` | Semantic DLP forensic funnel (understand → recall → rerank → summarize); streams NDJSON stage events. 503 if `dlp.enabled=false` or unprovisioned |
| GET | `/api/dlp/messages` | `ListEnrichedMessages` | Browse `agentguard_dlp_enriched`; filters `risk_tag`, `entity_type`, `agent_id`, `session_id`, `role`, `since`/`until`, `min_score`; paginated (max 500) |
| GET | `/api/dlp/messages/:id` | `GetEnrichedMessage` | Full enriched record by `message_id` |
| GET | `/api/dlp/profiles` | `DLPProfilesHandler` | Top risk agents / sessions / tags over a `?days=` window |
| GET | `/api/dlp/status` | `DLPStatusHandler` | DLP provisioning + enrichment health (stream/MV/UDF presence, row counts, embedding coverage) |
| GET | `/api/dlp/config` | `GetDLPConfig` | Current per-UDF config rows from `agentguard_dlp_config` (api_key redacted) |
| PUT | `/api/dlp/config/:key` | `UpsertDLPConfig` | Upsert one UDF's config (`{value}`); validates JSON/entity catalog, preserves stored api_key; live, no re-provision |

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
| `/dlp` | `SemanticDLP` |
| `/settings` | `Settings` |
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

### Agent Plugins (`agents/`)

Three subdirectories, one per telemetry source:

- **`agents/claudecode/`** — TypeScript Claude Code hook plugin. Configured via `make install` which injects hooks into `~/.claude/settings.json` for events: `SessionStart`, `PreToolUse`, `PostToolUse`, etc. Writes raw events to the `agentguard_hook_events` Proton stream. Also configures OTel env vars (`CLAUDE_CODE_ENABLE_TELEMETRY`, `OTEL_EXPORTER_OTLP_ENDPOINT`) so Claude Code emits metrics/logs to Timeplus.
- **`agents/openclaw/`** — TypeScript OpenClaw hook plugin. Similar structure; `make install-plugin` installs it. Includes integration test scenarios in `tests/`.
- **`agents/litellm/`** — LiteLLM proxy config (`config/litellm_config.yaml`) for routing LLM calls through a proxy that also emits telemetry.

## Timeplus Proton Integration

### Raw → CIM Pipeline

AgentGuard uses a **Common Information Model (CIM)** to normalize heterogeneous agent events into a single schema before querying. See `docs/cim.md` for the full spec.

| Stream | Purpose |
|--------|---------|
| `agentguard_hook_events` | Raw hook payloads from agent plugins |
| `agentguard_otel_logs` / `agentguard_otel_metrics` | Raw OTel data from Claude Code |
| `agentguard_cim_event` | Normalized lifecycle events (session, LLM call, tool call) |
| `agentguard_cim_metrics` | Pre-computed scalar metrics per call (tokens, cost, latency) |
| `agentguard_rules` | Rule mutation log; current state via `arg_max(…updated_at) GROUP BY` |
| `agentguard_security_events` | Rule-triggered detections written by MVs |
| `agentguard_cost_budgets` | Per-deployment monthly budget mutation log; current state via `arg_max(…updated_at) GROUP BY deployment_id HAVING NOT deleted` |
| `agentguard_dlp_enriched` | One enriched row per conversation message (entities, risk tags/score, inline embedding); written by `mv_dlp_enrich`. See **Semantic DLP** below. |
| `agentguard_dlp_config` | Mutable `key → JSON value` stream (one row per DLP UDF) holding all UDF config. Read live by the ingest MV (LEFT JOIN) and the funnel (scalar sub-query); operators upsert a row to change models/endpoint/timeouts/entity catalog at runtime with no re-provision. Seeded seed-if-absent from `config.yaml`. |

Materialized views normalize raw streams into `agentguard_cim_event` and `agentguard_cim_metrics`. Backend metric handlers (`GetLLMMetrics`, `GetToolMetrics`) query `agentguard_cim_metrics` directly.

### Rules and MVs

Each enabled rule has a corresponding materialized view named `mv_rule_<id>` (e.g. `mv_rule_rp_001`). The MV queries `hook_events` and writes matches to `agentguard_security_events`. Toggle/install/uninstall operations create, pause, resume, or drop these MVs atomically alongside the rule state mutation.

`timeplus.Init()` is called by the setup wizard (`POST /api/setup/provision`) to create missing streams and sync MV state. The server starts independently of Timeplus availability — if Timeplus Enterprise is unreachable, provisioning will fail but the binary itself starts cleanly.

### Semantic DLP

A natural-language forensic layer over conversation logs (full doc: `docs/dlp.md`; design: `docs/superpowers/specs/2026-06-11-semantic-dlp-design.md`; parent issue #159). All ML inference runs as **Timeplus Python UDFs** that call a configurable OpenAI-compatible endpoint (`dlp.base_url`) — no sidecar, no model client in Go.

- **Config-driven UDFs (#189)** — every DLP UDF is a *static* body taking a trailing `config` string (JSON) parsed at call time; all settings (endpoint, key, model, timeout, tokens, entity catalog, risk keywords/tags) live in the `agentguard_dlp_config` mutable stream. The ingest MV reads each UDF's config via a streaming **LEFT JOIN** on a literal key (live per event; `coalesce` + LEFT JOIN so a missing row never drops an event); the funnel reads it via a coalesced scalar sub-query (re-planned each run). A scalar sub-query is **frozen at MV creation**, so the MV must use the JOIN — do not "simplify" it to a sub-query. `DLPConfigRows(cfg)` renders the defaults; `seedDLPConfig` seeds seed-if-absent. No API key in `SHOW FUNCTIONS` anymore.
- **Ingest enrichment** (`backend/internal/timeplus/dlp.go`): `mv_dlp_enrich` reads `agentguard_cim_event` (`user_input`/`llm_response`/`tool_complete`) and writes `agentguard_dlp_enriched`, calling three UDFs per message — `dlp_extract_entities(content, config)` (regex/dictionary PII + masked secrets, no network), `dlp_embed(content, role, config)` (batched embeddings; tool rows get `[]`), `dlp_risk(content, entity_types, config)` (gated risk classification → tags + score). All UDFs **fail open**: a model-endpoint outage yields empty/zero columns, never stalls ingest.
- **Query funnel** (`backend/internal/handlers/dlp_query.go`): `dlp_understand_query` → two-channel recall (structured predicates + brute-force `cosine_distance` vector) → `dlp_rerank` → `dlp_summarize`, each UDF taking a config sub-query; streamed as NDJSON stages.
- **Config** — `dlp:` section in `config.yaml` (`enabled`, `base_url`, `api_key`, `embedding_model`, `embedding_dim`, `risk_model`, `summary_model`) provides **boot defaults** seeded into `agentguard_dlp_config`. At runtime the config stream is authoritative — change models/endpoint/timeouts/catalog by upserting a row (no re-provision). Only `embedding_dim` is still baked (stream/index) and requires re-provisioning. Provisioning is wired into `timeplus.Init()` (skipped when `dlp.enabled: false`) and `POST /api/rules/sync`.
- **Scripts & verification** (`backend/scripts/`):
  - `dlp_mock_model.py` — deterministic mock OpenAI-compatible server (embeddings/completions/`/models`) for CI/local, no model weights. `make dlp-mock`.
  - `dlp_sample_events.json` + `dlp_load_events.py` — demo hook events (customer-PII leak, secret paste, source-code exfil, benign) inserted into `agentguard_hook_events`.
  - `dlp_seed.sh` — seed a provisioned/running stack for the `/dlp` page. `make dlp-seed`.
  - `dlp_integration.sh` — end-to-end test (mock → provision → enrich → query round-trip) against a live Timeplus. `make dlp-integration`.

### Timeline handlers

`internal/handlers/timeline*.go` provides the agent session timeline. It has three variants selected at runtime: `timeline.go` (CIM-based), `timeline_openclaw.go` (OpenClaw-specific span builder), `timeline_fallback.go` (gap-fill waterfall for unknown agent types). Each has a corresponding `*_test.go`.
