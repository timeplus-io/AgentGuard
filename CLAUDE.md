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
  - `rules.go` — Timeplus MV primitives: `MVName`, candidate preflight (`PreflightCandidate` against a disposable sink), `NO DELAY` replacement (`DropMVNoDelay`, `CreateRuleMVStrict`), `ApplyUDFs`, `MVExists`, and failed-MV health (`FailedMVs`). (Legacy `CreateRuleMV`/`PauseRuleMV`/`ResumeRuleMV` remain but are unused by the lifecycle service.)
- **`internal/rules/`** — Editable rule parameters: the strict parameter compiler (`validate.go`/`render.go`/`compatibility.go` — typed normalization, `{{ .Config.<key> }}` rendering, upgrade planning), the revision-ordered snapshot store (`store.go`), and the per-rule-serialized lifecycle service (`service.go`) that all rule mutations delegate through. The catalog (`internal/catalog/`) owns versioned `Definition`s; `internal/rules` owns installed `RuleInstance` snapshots.
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
| GET | `/api/rules` | `ListRules` | Installed instances (current state via `arg_max(col, revision)`) + upgradeable flag |
| GET | `/api/rules/catalog` | `ListCatalog` | Builtin catalog with installed status |
| POST | `/api/rules/catalog` | `CreateCatalogRule` | Create a custom `Definition` (origin forced `custom`); validates + publishes, does NOT install |
| POST | `/api/rules/catalog/compile` | `CompileCatalogRule` | Pure validate+compile preview → `{rendered_sql, definition_hash, effective, overrides}`; no catalog write, no MV, no Timeplus |
| GET | `/api/rules/catalog/:id` | `GetCatalogRule` | Full catalog `Definition` (incl. `config[]` schema) or 404 |
| PUT | `/api/rules/catalog/:id` | `UpdateCatalogRule` | Edit a custom definition; atomic publish+redeploy of the installed MV, rolls back both definition and MV on failure (builtin/imported → 409, clone first) |
| POST | `/api/rules/catalog/:id/clone` | `CloneCatalogRule` | Clone a definition to a new custom rule with rescoped UDFs (not installed) |
| POST | `/api/rules/catalog/import` | `ImportCatalog` | Import rules from JSON |
| GET | `/api/rules/catalog/export` | `ExportCatalog` | Export catalog as JSON |
| GET | `/api/rules/mv-status` | `MVStatus` | MV health: `system.tables` + `system.v_failed_mat_views`, cross-referenced with enabled |
| POST | `/api/rules/sync` | `SyncMVs` | Reconcile MVs with snapshots (create missing/drop disabled) via the lifecycle service |
| POST | `/api/rules` | `CreateRule` | Thin create-custom: zero-param custom definition (template ≡ rendered SQL) installed via the service |
| GET | `/api/rules/:id` | `GetRule` | Full read model: instance + schema/defaults/overrides/effective/rendered_sql/upgrade plan/MV health |
| POST | `/api/rules/:id/install` | `InstallRule` | Install from catalog with optional typed `overrides` (defaults otherwise) |
| PUT | `/api/rules/:id/configuration` | `ConfigureRule` | Replace the full parameter override map; omitted keys reset to defaults |
| POST | `/api/rules/:id/toggle` | `ToggleRule` | Flips `enabled`; drop/create the MV (no pause/resume) |
| POST | `/api/rules/:id/upgrade` | `UpgradeRule` | Bodyless, catalog-only; preserves compatible overrides, 409 on conflict |
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
| POST | `/api/config/test-dlp` | `TestDLPConfig` | Tests DLP `base_url` reachability (GET `/models`), a real embedding call, and a chat completion per `chat_models[]` entry (falls back to the risk model). `embedding_dim<=0` accepts any non-empty vector. Never 5xx on unreachable. |
| POST | `/api/dlp/query` | `DLPQuery` | Semantic DLP forensic funnel (understand → recall → rerank → summarize); streams NDJSON stage events. 503 if `dlp.enabled=false` or unprovisioned |
| GET | `/api/dlp/messages` | `ListEnrichedMessages` | Browse `agentguard_dlp_enriched`; filters `risk_tag`, `entity_type`, `agent_id`, `session_id`, `role`, `since`/`until`, `min_score`; paginated (max 500) |
| GET | `/api/dlp/messages/:id` | `GetEnrichedMessage` | Full enriched record by `message_id` |
| GET | `/api/dlp/profiles` | `DLPProfilesHandler` | Top risk agents / sessions / tags over a `?days=` window |
| GET | `/api/dlp/status` | `DLPStatusHandler` | DLP provisioning + enrichment health (stream/MV/UDF presence, row counts, embedding coverage) |
| GET | `/api/dlp/config` | `GetDLPConfig` | Current per-UDF config rows from `agentguard_dlp_config` (api_key redacted) |
| PUT | `/api/dlp/config/:key` | `UpsertDLPConfig` | Upsert one UDF's config (`{value}`); validates JSON/entity catalog, preserves stored api_key; live, no re-provision |
| POST | `/api/sentry/chat` | `SentryChat` | `{conversation_id?, message}` → NDJSON stream of `sentry.Event` lines (`meta`/`token`/`tool_call`/`tool_result`/`done`/`error`). 503 if `sentry.enabled=false` or no `base_url` in resolved config |
| GET | `/api/sentry/conversations` | `ListSentryConversations` | Session user's own **active** conversations, most-recently-updated first |
| GET | `/api/sentry/conversations/:id` | `GetSentryConversation` | Conversation + full message replay, owner-only (403/404; non-active → 404) |
| PATCH | `/api/sentry/conversations/:id` | `RenameSentryConversation` | Owner-only title replacement (`{title}`, 80-rune truncation). A user-set title is permanent — LLM auto-naming never overwrites it |
| DELETE | `/api/sentry/conversations/:id` | `DeleteSentryConversation` | Owner-only soft-delete (`status` → `deleted`); message rows stay in the append-only log but become unreachable |
| GET | `/api/sentry/status` | `SentryStatus` | Enabled/provisioned flags, resolved chat model, whether a `base_url` is configured |
| GET | `/api/sentry/config` | `GetSentryConfig` | Current `chat`/`memory` rows from `agentguard_sentry_config` (api_key redacted); 503 until Sentry provisioned |
| PUT | `/api/sentry/config/:key` | `UpsertSentryConfig` | Upsert one Sentry config row (`chat`/`memory`, `{value}`); preserves stored api_key, validates JSON per key; live, no re-provision |
| POST | `/api/sentry/config/test` | `TestSentryConfig` | Tests a candidate Sentry endpoint: chat reachability + a chat completion with `tool_choice:"required"` asserting `tool_calls` come back; the embedding check runs only when memory is enabled (else `checks.embedding.skipped:true`). Falls back to live `sentry.*` boot config for omitted fields. Never 5xx |

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
| `/sentry` | `Sentry` |
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
| `agentguard_rules` | Append-only Rule Instance snapshot log; current state via `arg_max(col, revision) GROUP BY rule_id` (ordered by per-rule monotonic `revision`, NOT `updated_at`) |
| `agentguard_security_events` | Rule-triggered detections written by MVs |
| `agentguard_cost_budgets` | Per-deployment monthly budget mutation log; current state via `arg_max(…updated_at) GROUP BY deployment_id HAVING NOT deleted` |
| `agentguard_dlp_enriched` | One enriched row per conversation message (entities, risk tags/score, inline embedding); written by `mv_dlp_enrich`. See **Semantic DLP** below. |
| `agentguard_dlp_config` | Mutable `key → JSON value` stream: a `shared` row (base_url/api_key/timeout/model-catalog common to all network UDFs) + one row per UDF (model/max_tokens/prompt). A pure `dlp_config_resolve(shared, config)` UDF folds `shared ⊕ own` (own wins) and resolves the model catalog into ONE flat config, so a network UDF only ever sees its own resolved endpoint — never `shared` or other models. Read live by the ingest MV (LEFT JOIN, wrapped per-row in `dlp_config_resolve`) and the funnel (scalar sub-queries wrapped in `dlp_config_resolve`); operators upsert a row to change endpoint/models/timeouts/entity catalog/prompts at runtime with no re-provision. Seeded seed-if-absent from `config.yaml`. Sentry has its own separate config stream (`agentguard_sentry_config` below) and no longer reads this one. |
| `agentguard_sentry_config` | Mutable `key → JSON value` stream dedicated to Sentry, now just TWO self-contained rows: `chat` (the ReAct loop's own literal endpoint — base_url/api_key/model — + per-turn budgets; NO system_prompt, which is a built-in constant) and `memory` (an `enabled` toggle + its own embedding endpoint base_url/api_key/model/embedding_dim, reserved for Phase-2 memory). Each row is a flat literal endpoint — no `shared` row, no model catalog, and Sentry does NOT use `dlp_config_resolve`. Seeded seed-if-absent from the `sentry.*` section of `config.yaml`. Edited via `PUT /api/sentry/config/:key` / **Settings → Sentry Agent**, independent of `agentguard_dlp_config` and reachable with `dlp.enabled: false`. See **Sentry agent** below. |
| `agentguard_sentry_conversations` | Mutable stream keyed on `conversation_id` (re-insert = upsert): `user`, `title`, `status` (`active`/`deleted`; legacy `archived` rows are treated as deleted), `created_at`, `updated_at`. Written by `internal/sentry.Store`. A conversation's title starts as the truncated first user message; after a successful turn, while the title is still that auto-derived value, `sentry.MaybeAutoTitle` asks the chat LLM for a short summary name (fail-open, fire-and-forget, never overwrites a rename). |
| `agentguard_sentry_messages` | Append-only per-turn message log: `conversation_id`, `seq`, `role` (`user`/`assistant`/`tool`), `content`, `tool_name`/`tool_args`/`tool_result`, `model`, `tokens_in`/`tokens_out`. Replayed in `seq` order to rebuild a conversation. |

Materialized views normalize raw streams into `agentguard_cim_event` and `agentguard_cim_metrics`. Backend metric handlers (`GetLLMMetrics`, `GetToolMetrics`) query `agentguard_cim_metrics` directly.

### Rules and MVs

Each enabled rule has a corresponding materialized view named `mv_rule_<id>` (e.g. `mv_rule_rp001`). The MV runs the instance's `rendered_sql` (the definition's `sql_template` compiled with the instance's parameter overrides) against `agentguard_cim_event` and writes matches to `agentguard_security_events`. The lifecycle service (`internal/rules/service.go`) serializes mutations per `rule_id` and uses a **drop/create** model (not pause/resume): validate-first (candidate preflight), replace-second (`DROP VIEW … NO DELAY` then `CREATE … ` without `IF NOT EXISTS`), persist-last (append the complete snapshot), restoring the previous MV from the snapshot's last-known-good `rendered_sql`/`deployed_udfs_json` on any failure. A disabled rule has no live MV. Parameters are typed (`integer`/`float`/`bool`/`string`/`choice`/`list`/`multi_choice` (+ `regex`/`duration`)); the renderer escapes quotes/backslashes only — string params in `LIKE` are NOT wildcard-escaped (use `position()`/`multi_search_any`).

`timeplus.Init()` is called by the setup wizard (`POST /api/setup/provision`) to create missing streams and sync MVs from installed instance snapshots (`rendered_sql` + `deployed_udfs_json`, never current catalog artifacts). It does NOT seed instances from the catalog — a definition existing in the catalog never makes it an installed instance. Provisioning fails fast with a clean-reprovision message if it finds a legacy/incompatible `agentguard_rules` (it compares the full expected column set and key column types). The server starts independently of Timeplus availability — if Timeplus Enterprise is unreachable, provisioning will fail but the binary itself starts cleanly.

**No backward compatibility:** the new `agentguard_rules` snapshot schema is not compatible with the pre-parameter one. Existing deployments must cleanly reprovision rule resources (drop the old stream + `mv_rule_*` views, re-provision, reinstall). No `ALTER STREAM`, compatibility readers, dual writes, or `query_sql` aliases. See `docs/design.md` §5.1.3.

### Semantic DLP

A natural-language forensic layer over conversation logs (full doc: `docs/dlp.md`; design: `docs/superpowers/specs/2026-06-11-semantic-dlp-design.md`; parent issue #159). All ML inference runs as **Timeplus Python UDFs** that call a configurable OpenAI-compatible endpoint (`dlp.base_url`) — no sidecar, no model client in Go.

- **Config-driven UDFs (#189, #195)** — every DLP UDF is a *static* body parsing its settings from a `config` param at call time; all settings live in the `agentguard_dlp_config` mutable stream. Endpoint defaults (base_url/api_key/timeout) AND a named-model catalog (`shared.models`: name→endpoint object `{model[,base_url,api_key]}`) live in ONE `shared` row; each per-UDF row carries just `model` (a catalog NAME) + knobs (`max_tokens`, …). A single pure UDF `dlp_config_resolve(shared, config)` folds `shared ⊕ own` (own wins) and resolves the model NAME against the catalog into a concrete (base_url, api_key, model) — each model can have its OWN base_url/api_key (falls back to the shared top-level; literal passthrough if the name isn't in the catalog) — then **strips the catalog**, emitting ONE flat config. Network UDFs (`dlp_embed`, `dlp_risk`, `dlp_understand_query`, `dlp_rerank`, `dlp_summarize`) take only that resolved `config` — they never receive `shared` or see other models (#195). The four LLM UDFs (risk/understand/rerank/summarize) also read their **prompt** from config (#197): each row seeds the full prompt template (placeholders like `{question}`, `{message}`, `{evidence}` are substituted by the UDF in a single `re.sub` pass, values truncated first); empty/absent prompt falls back to the built-in `DEFAULT_PROMPT`, and `TestDLPDefaultPromptsMatchUDFs` keeps the Go seed and the Python fallback byte-identical. The strict-JSON instruction and `TASK:` markers in the defaults are load-bearing (reply parsing / mock-model dispatch). A model is defined once and referenced by several UDFs (risk/understand/rerank all use `"chat"`). The ingest MV reads config via streaming **LEFT JOIN**s on literal keys and wraps each call in `dlp_config_resolve(cfg_shared, cfg_udf)` — a per-row function over the JOINed columns, so it stays live per event (`coalesce` + LEFT JOIN so a missing row never drops an event); the funnel wraps the two coalesced scalar sub-queries in `dlp_config_resolve` (re-planned each run). A scalar sub-query is **frozen at MV creation**, so the MV must use the JOIN — do not "simplify" it to a sub-query, and keep the resolver as a per-row function, not a sub-query. `DLPConfigRows(cfg)` renders defaults; `seedDLPConfig` seeds seed-if-absent. No API key in `SHOW FUNCTIONS`.
- **Ingest enrichment** (`backend/internal/timeplus/dlp.go`): `mv_dlp_enrich` reads `agentguard_cim_event` (`user_input`/`llm_response`/`tool_complete`) and writes `agentguard_dlp_enriched`, calling three UDFs per message — `dlp_extract_entities(content, config)` (regex/dictionary PII + masked secrets, no network), `dlp_embed(content, role, dlp_config_resolve(cfg_shared, cfg_embed))` (batched embeddings; tool rows get `[]`), `dlp_risk(content, entity_types, dlp_config_resolve(cfg_shared, cfg_risk))` (gated risk classification → tags + score). All UDFs **fail open**: a model-endpoint outage yields empty/zero columns, never stalls ingest.
- **Query funnel** (`backend/internal/handlers/dlp_query.go`): `dlp_understand_query` → two-channel recall (structured predicates + brute-force `cosine_distance` vector) → `dlp_rerank` → `dlp_summarize`, each UDF taking a single resolved config via `dlpResolvedCfg(key)` (= `dlp_config_resolve(shared-subquery, per-UDF-subquery)`); streamed as NDJSON stages.
- **Config** — `dlp:` section in `config.yaml` (`enabled`, `base_url`, `api_key`, `embedding_model`, `risk_model`, `summary_model`) provides **boot defaults** seeded into `agentguard_dlp_config`. At runtime the config stream is authoritative — change models/endpoint/timeouts/catalog by upserting a row (no re-provision). `embedding_dim` is **auto-detected** at provision by probing the model via `dlp_embed` (`ProbeEmbeddingDim`; config value is a fallback, shown read-only) — it sizes the best-effort HNSW index and is recorded in the provision snapshot by the setup wizard and `/api/rules/sync`. `reprovisionRequired` is now always false; re-provision on demand after switching the embedding model. Provisioning is wired into `timeplus.Init()` (skipped when `dlp.enabled: false`) and `POST /api/rules/sync`.
- **First-time enable (UI)** — the `/dlp` page requires configuring the endpoint + models and passing a live validation of every distinct model (embedding + each chat model) before it writes the seed config keys, enables `dlp.enabled`, and provisions. Post-provision editing stays in Settings (`DLPConfigEditor`).
- **Scripts & verification** (`backend/scripts/`):
  - `dlp_mock_model.py` — deterministic mock OpenAI-compatible server (embeddings/completions/`/models`) for CI/local, no model weights. `make dlp-mock`.
  - `dlp_sample_events.json` + `dlp_load_events.py` — demo hook events (customer-PII leak, secret paste, source-code exfil, benign) inserted into `agentguard_hook_events`.
  - `dlp_seed.sh` — seed a provisioned/running stack for the `/dlp` page. `make dlp-seed`.
  - `dlp_integration.sh` — end-to-end test (mock → provision → enrich → query round-trip) against a live Timeplus. `make dlp-integration`.

### Sentry agent

Sentry is AgentGuard's conversational security-analyst agent (`/sentry` page, `/api/sentry/*`; full design: `docs/superpowers/specs/2026-07-22-sentry-agent-design.md`; operator doc: `docs/sentry.md`). An LLM-driven ReAct loop (`internal/sentry/loop.go`) answers operator questions by calling a fixed, read-only tool surface (`internal/sentry/skills/`) — schema introspection, ad-hoc SQL, DLP semantic search, curated cross-resource reads over threats/holds/rules/cost/sessions, and the rule catalog (`get_rule_catalog` — the catalog is filesystem-backed, unreachable via SQL) — streaming `meta`/`token`/`tool_call`/`tool_result`/`done`/`error` events as NDJSON. The chat loop's system prompt is a **built-in constant** (`DefaultSentrySystemPrompt`), not config. Sentry has its own dedicated config stream, `agentguard_sentry_config`, holding TWO self-contained rows — `chat` (the loop's own literal endpoint + budgets) and `memory` (an enable toggle + its own embedding endpoint, gated by `memory.enabled`, reserved for Phase-2 memory) — seeded from the `sentry.*` section of `config.yaml`. Each row is a flat literal endpoint: Sentry does **not** use `dlp_config_resolve`, there is no `shared` row or model catalog, and it's entirely decoupled from `agentguard_dlp_config` — Sentry never reads DLP's config, so it works with `dlp.enabled: false` as long as `sentry.enabled: true` and its `chat` row has a `base_url`. Edited live via `PUT /api/sentry/config/:key` (`chat`/`memory`) + `POST /api/sentry/config/test` (chat reachability/tool-calling always; embedding only when memory enabled), or in the UI under **Settings → Sentry Agent** — reachable regardless of DLP provisioning status. This is **Phase 1** (chat core only) — memory, background tasks, and gated (write) tool access are later phases and are intentionally absent. The chat LLM layer has two providers selected by the `chat` row's `api_type` (boot default `sentry.chat_api_type`, default `openai`): the original OpenAI-compatible client, or `anthropic` — the Anthropic Messages API via `anthropic-sdk-go` over a Bearer-token endpoint (`internal/sentry/anthropic.go`), used for Claude on AWS Bedrock (Bedrock Mantle); both share the same `base_url`, normalized per-provider (`openai` keeps `/v1`, `anthropic` strips it and appends `/v1/messages`).

### Timeline handlers

`internal/handlers/timeline*.go` provides the agent session timeline. It has three variants selected at runtime: `timeline.go` (CIM-based), `timeline_openclaw.go` (OpenClaw-specific span builder), `timeline_fallback.go` (gap-fill waterfall for unknown agent types). Each has a corresponding `*_test.go`.
