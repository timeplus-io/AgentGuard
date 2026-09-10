# AgentGuard core pipeline as a Timeplus app (`.tpapp`) — design

**Issues:** timeplus-io/AgentGuard#291 (scope, export), timeplus-io/apps#50 (publish).
**Status:** approved design, 2026-09-10.

## 1. Goal

Package the AgentGuard core streaming pipeline — raw ingest → CIM → metrics → security events/threats, plus the four Core Protection rules — as an installable Timeplus app (`apps/agentguard/`, `id: io.timeplus.agentguard`, `db_name: ag`), generated from the backend's own provisioning DDL so the app and `timeplus.Init()` cannot drift, and make the AgentGuard UI able to attach to an app-installed pipeline.

Non-goals (explicitly excluded, see #291): user management/RBAC (`agentguard_roles`, `agentguard_user_roles`), notifications (`agentguard_notify_events`, `mv_notify_filter`, `agentguard_notifications`), holds, Sentry, Semantic DLP, catalog rules beyond rp-001..rp-004, pricing seed rows, retention/TTL config.

## 2. Key constraints discovered

- **The app must own its database.** The apps installer creates `db_name` and refuses to install when the database already exists. `default` always exists, so the issue's "ship with `db_name: default`" is not viable. The app uses `db_name: ag` and every resource reference is qualified with `{{ .DB }}.` so the app is relocatable.
- **Backend attach requires a database setting.** AgentGuard handlers use unqualified stream names. A new `timeplus.database` config key (default `default`) is applied as the *connection's default database* (HTTP `database=` parameter, native `Auth.Database`), so no handler SQL changes. Setting it to `ag` makes the UI read the app-installed pipeline.
- **MV sync is safe.** `startupSyncMVs` / `rules.Service.Sync` only act on rules that have a snapshot row in `agentguard_rules`. App-installed `mv_rule_rp00N` views with no row are left alone by the wizard.
- **UDFs are global.** rp-002 ships a JavaScript UDF (`ag_dlp_jwt_check`); it is emitted as a `udf` resource with `CREATE OR REPLACE FUNCTION` and no database prefix.
- **Installer semantics.** One statement per file; every `CREATE` must be `IF NOT EXISTS` (upgrades re-run all files); `type: system` resources carry `SYSTEM PAUSE MATERIALIZED VIEW` (pattern from `apps/cisco-asa-ddos/ddl/007b_pause_attack_mv.sql`).
- **Local verification limit.** The local Timeplus Enterprise 3.2.1 does not expose the apps API (`/…/api/v1beta2/apps` → 404). Live verification executes the rendered DDL in manifest order over the HTTP port into a scratch database; a real `.tpapp` install is verified on a newer instance when available.

## 3. Components

### 3.1 `timeplus.AppResources()` — the ordered resource list (single source of truth)

New file `backend/internal/timeplus/app_resources.go`:

```go
type AppResourceKind string // "stream" | "mutable_stream" | "materialized_view" | "input"

type AppResource struct {
    Kind AppResourceKind
    Name string   // object name without database, e.g. "agentguard_cim_event"
    DDL  string   // the existing ddl* constant, verbatim
}

// AppResources returns the pipeline subset of what Init() provisions, in
// Init()'s dependency order. Nothing is copied: each entry references the
// same constant Init() executes.
func AppResources() []AppResource
```

Order (mirrors `Init()`): `otel_traces`, `otel_logs`, `otel_metrics_gauge`, `otel_metrics_sum`, `otel_metrics_histogram`, `otel_metrics_exponential_histogram`, `otel_metrics_summary`, `agentguard_otel_input`, `agentguard_hook_events`, `agentguard_cim_event`, `mv_cim_claudecode`, `mv_cim_openclaw`, `mv_cim_hermes`, `mv_cim_openinference`, `mv_hook_bridge_openinference`, `agentguard_session_agents`, `agentguard_run_latency`, `agentguard_cim_metrics`, `mv_session_agents`, `mv_run_latency`, `mv_cim_metrics_openclaw_llm`, `mv_cim_metrics_openclaw_tools`, `mv_cim_metrics_claudecode_otel`, `mv_cim_metrics_claudecode_tools`, `mv_cim_metrics_hermes_llm`, `mv_cim_metrics_hermes_tools`, `mv_cim_metrics_openinference_llm`, `agentguard_security_events`, `agentguard_threats`, `mv_threats`, `agentguard_pricing`, `agentguard_cost_budgets`, `agentguard_skills`, `mv_skills_invoked_claudecode`, `mv_skills_invoked_openclaw`, `mv_skills_invoked_hermes`, `mv_skills_loaded_claudecode`, `mv_skills_loaded_openclaw`, `agentguard_memory_ops`, `mv_memory_claudecode_automem`, `mv_memory_claudecode_mcp`, `mv_memory_openclaw_file`, `mv_memory_hermes`, `agentguard_rules`.

`AppExcludedDDL()` returns the names of the `ddl*` constants deliberately left out (`ddlNotifyEventsStream`, `ddlNotifyFilterMV`, `ddlNotificationsStream`). A test parses `init.go` with `go/parser`, collects every top-level `ddl*` const/var, and fails if one is in neither list — adding a stream to `Init()` without deciding whether the app gets it becomes a test failure.

### 3.2 `internal/tpapp` — the exporter

New package `backend/internal/tpapp/` with:

- `Transform(ddl string, names []string) (string, error)` — pure text transformation:
  1. Insert `IF NOT EXISTS` after `CREATE STREAM`, `CREATE MUTABLE STREAM`, `CREATE MATERIALIZED VIEW`, `CREATE INPUT` when absent (regex on the statement head only).
  2. Qualify every occurrence of a known resource name (`names` = all `AppResources()` names + rule MV names) as `{{ .DB }}.<name>` using `\b` boundaries; occurrences already preceded by `{{ .DB }}.` are skipped. This also rewrites quoted targets such as `traces_target_stream = 'otel_traces'` → `'{{ .DB }}.otel_traces'` and the rule SQL's `FROM agentguard_cim_event`.
  3. For `agentguard_otel_input` only: replace `tcp_port = 4318` with `tcp_port = {{ .Config.otel_port }}` and `listen_host = '0.0.0.0'` with `'{{ .Config.otel_listen_host }}'`.
- `Rules(defs []catalog.Definition) ([]RuleArtifact, error)` — for rp-001..rp-004 (loaded from `catalog.SeedFS`), `rules.Compile(def, nil)` renders with defaults; emits per rule: optional `udf` resources (one per `def.UDFs` entry, name via `catalog.ExtractUDFNames`), the MV `CREATE MATERIALIZED VIEW IF NOT EXISTS {{ .DB }}.mv_rule_rp00N INTO {{ .DB }}.agentguard_security_events AS <Transform(compiled.SQL)>`, and a `system` resource `SYSTEM PAUSE MATERIALIZED VIEW {{ .DB }}.mv_rule_rp00N`.
- `Build() (*App, error)` — assembles `AppResources()` + rules into an ordered list of files: `ddl/NNN_<name>.sql` (3-digit zero-padded, manifest order), `manifest.yaml` (rendered from a Go template with the resource list, config keys, icon, categories, dashboards entry), plus the static files copied verbatim from `internal/tpapp/static/` (`dashboards/main.json`, `README.md`, `Makefile`, `icon.svg` source).
- `Write(app *App, dir string) error` — writes the tree, deleting stale `ddl/*.sql` first so removed resources do not linger.

Manifest fields: `package_format_version: 1`, `id: io.timeplus.agentguard`, `name: AgentGuard`, `version` (from a constant in `tpapp`, bumped with releases; initial `0.1.0`), `author: Timeplus`, `description`, `icon` (SVG data URI, generated from `static/icon.svg` at build), `categories: [security, observability]`, `db_name: ag`, `config: [otel_port (integer, default "4318"), otel_listen_host (string, default "0.0.0.0")]`, `resources` (ordered), `dashboards: [main.json]`.

Rule MV file names: `NNN_mv_rule_rp001.sql`, followed by `NNN_udf_ag_dlp_jwt_check.sql` *before* rp-002's MV, and `NNN_pause_mv_rule_rp00N.sql` after each MV. Manifest `name:` for a system resource is `pause_mv_rule_rp00N`.

### 3.3 `cmd/tpapp-export`

`backend/cmd/tpapp-export/main.go`: flags `--out <dir>` (default `../apps/agentguard` relative to `backend/`) and `--check` (render to a temp dir, diff against `--out`, exit 1 on difference, printing the changed paths). No Timeplus connection.

Makefile (repo root): `tpapp` (export + `zip -r apps/agentguard.tpapp` from inside the dir, matching the apps repo Makefile), `tpapp-check`. `lint`/CI run `tpapp-check` via the Go test, not the Makefile.

### 3.4 Backend `timeplus.database`

- `config.TimeplusConfig.Database string`; default `"default"`; env `TIMEPLUS_DATABASE`; flag `--timeplus-database`; registry entry in section `timeplus`, hot-applied like `timeplus.host` (connections are built per request; the long-lived notify watcher picks it up on restart, same as host); persisted by `UpdateConfig` like other keys.
- Applied in exactly the places that build a Timeplus connection: `client.go` `Query`/`ExecHTTP` and `stream.go` (append `database=<name>` to the URL query when non-empty and not `default`), and `init.go` `connectOnce` (`Auth.Database`). `auth.RequestCfg` copies the whole struct, so per-session configs inherit it.
- Provisioning with `database: ag` after an app install reports every resource as already existing; provisioning into an empty `ag` would create the same objects there (the wizard does not create databases — the operator creates `ag` or installs the app first; documented).

### 3.5 Static files

- `dashboards/main.json` — "AgentGuard Overview": time-range selector; events/min by `agent_type` (line, from `[[ .DB ]].agentguard_cim_event`), tokens and cost by model (column/table, from `[[ .DB ]].agentguard_cim_metrics`), open threats by severity (column, from `[[ .DB ]].agentguard_threats WHERE status = 'open'`), recent threats table. Follows the apps repo panel format (`position`, `viz_type`, `viz_config.chartType`, catalog palette).
- `README.md` — what is installed (layer table), build/install, config table, pointing agents at the app (OTel exporter → `otel_port`; hook plugins → `agentguard_hook_events` in database `ag`), attaching the AgentGuard UI (`timeplus.database: ag`), resuming a rule MV, uninstall drops the data streams.
- `Makefile` — apps repo template (`build`, `install`, `NEUTRON_URL`, `TENANT`).
- `icon.svg` — 48×48, catalog gradient background, white shield outline with `</>` brackets.

## 4. Data flow after install

```
Claude Code / OpenClaw / Hermes plugins ──INSERT──▶ ag.agentguard_hook_events ─┐
OTLP/HTTP exporters ──▶ ag.agentguard_otel_input ──▶ ag.otel_* ────────────────┤
                                                                               ▼
                                                    mv_cim_* ──▶ ag.agentguard_cim_event
                                                                               │
                        mv_cim_metrics_* ──▶ ag.agentguard_cim_metrics ◀───────┤
                        mv_skills_* / mv_memory_* ──▶ ag.agentguard_skills / _memory_ops
                                                                               │
                        mv_rule_rp001..004 (paused) ──▶ ag.agentguard_security_events
                                                                 └── mv_threats ──▶ ag.agentguard_threats
AgentGuard UI (timeplus.database: ag) ── reads all of the above with unqualified names
```

## 5. Error handling

- Exporter: any unknown `CREATE` head, a resource name that fails to qualify, a rule that fails `Compile`, or a UDF DDL whose name cannot be extracted is a hard error — the CLI exits non-zero and the check test fails. No partial writes: `Write` renders everything in memory first.
- Backend: an empty `timeplus.database` is treated as `default`. Names are validated against `^[a-z][a-z0-9_]{0,31}$` in `CoerceAndValidate` (same rule the installer uses for `db_name`).

## 6. Testing

Unit (`go test ./...`, no Timeplus):
- `tpapp`: `Transform` golden cases (IF NOT EXISTS insertion for each kind; qualification incl. quoted input targets; no double-qualification; OTel port/host templating); `Build` produces exactly one statement per file (no `;` outside strings/`$$` bodies), every file's known names are qualified, manifest resource order equals file order, rp-002's UDF precedes its MV, each rule MV is followed by its pause resource.
- `timeplus`: `go/parser` coverage test over `init.go` `ddl*` constants vs `AppResources()` ∪ `AppExcludedDDL()`; `AppResources()` names are unique and match the `CREATE … <name>` head of their DDL.
- Drift: `TestAppExportUpToDate` renders `Build()` and diffs against the committed `apps/agentguard/` tree (path resolved from the package dir); failure message says to run `make tpapp`.
- `config`: `timeplus.database` default, env/flag binding, validation.

Integration (`go test -tags integration`, existing convention in `internal/handlers/integration_test.go`; env `TIMEPLUS_HOST/PORT/USER/PASSWORD`):
- `TestIntegration_TpappInstall`: pick a unique scratch db (`ag_test_<unix>`), `CREATE DATABASE`, render the app with that DB and a free `otel_port`, execute every DDL file in manifest order over HTTP, assert every resource exists in `system.tables`/`system.inputs`, assert the four rule MVs are paused, then `DROP DATABASE … NO DELAY`. Exercises exactly what the installer does minus the zip.

Manual, when an apps-API-capable Timeplus is available: `make tpapp && make -C apps/agentguard install`, then wizard-attach with `timeplus.database: ag`.

## 7. Files touched

New: `backend/internal/timeplus/app_resources.go` (+test), `backend/internal/tpapp/{transform.go,rules.go,build.go,write.go,manifest.go.tmpl,static/*}` (+tests), `backend/cmd/tpapp-export/main.go`, `backend/internal/handlers/tpapp_integration_test.go` (or under `tpapp`), `apps/agentguard/**` (generated + static), `docs/tpapp.md`.
Modified: `backend/internal/config/{config.go,registry.go}`, `backend/internal/timeplus/{client.go,stream.go,init.go}`, `Makefile`, `CLAUDE.md` (commands + streams note), `backend/cmd/server/main.go` (flag).
