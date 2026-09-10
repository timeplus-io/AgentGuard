# Installing the AgentGuard pipeline as a Timeplus app

AgentGuard's core streaming pipeline can be installed two ways:

| | Setup wizard (`POST /api/setup/provision`) | Timeplus app (`apps/agentguard.tpapp`) |
|---|---|---|
| Runs | `timeplus.Init()` from the AgentGuard server | Timeplus apps installer (UI or `POST /<tenant>/api/v1beta2/apps/install`) |
| Database | `default` (unqualified names) | `ag` (`db_name` in the manifest) |
| Contents | everything, incl. control plane (holds, notifications, RBAC, Sentry, DLP) | data pipeline only + 4 Core Protection rules, paused |
| Rules | installed via `/api/rules` (lifecycle service) | `mv_rule_rp001..rp004` pre-created and paused; no `agentguard_rules` rows |

The app is published to the Timeplus apps catalog from `apps/agentguard/` (timeplus-io/apps#50); the export itself is tracked in timeplus-io/AgentGuard#291.

## Generating the package

```bash
make tpapp          # regenerates apps/agentguard/ from backend DDL and zips apps/agentguard.tpapp
make tpapp-check    # CI: fail if apps/agentguard/ is stale
```

The package is generated from the same DDL constants `timeplus.Init()` executes:

- `backend/internal/timeplus/app_resources.go` — `AppResources()` lists the shipped objects in dependency order, each referencing its `ddl*` constant; `AppExcludedDDL()` names the constants deliberately left out.
- `backend/internal/tpapp` — inserts `IF NOT EXISTS`, qualifies every resource reference with `{{ .DB }}.`, templates the OTLP port/host as `{{ .Config.* }}`, compiles rules rp-001..rp-004 with default parameters (each MV followed by a `SYSTEM PAUSE MATERIALIZED VIEW` resource; rp-002's JavaScript UDF is emitted as a global `udf` resource), renders `manifest.yaml`, and copies the static dashboard, README, Makefile and icon from `internal/tpapp/static/`.
- `backend/cmd/tpapp-export` — the CLI (`--out`, `--check`).

Two tests keep it honest: `TestAppExportUpToDate` fails if the committed `apps/agentguard/` differs from the generator, and `TestAppResources_CoverEveryDDLConstant` fails if a new `ddl*` constant in `init.go` is neither shipped nor explicitly excluded. Never hand-edit `apps/agentguard/`; change the backend DDL (or the static files) and run `make tpapp`.

## Attaching the AgentGuard UI to an app install

1. Install the app (database `ag`).
2. Set `timeplus.database: ag` in `config.yaml` (or `TIMEPLUS_DATABASE=ag`, `--timeplus-database ag`, or **Settings → Timeplus Connection → Database**). The value is applied as the connection's default database (HTTP `database=` parameter, native `Auth.Database`), so handler SQL keeps its unqualified stream names.
3. Run the setup wizard. It reports the app's resources as already existing and creates the control-plane streams (holds, notifications, RBAC, …) inside `ag`.
4. Rules: the four app-installed MVs have no `agentguard_rules` snapshot, so `/rules` shows rp-001..004 as not installed and the MV sync leaves them alone. Installing rp-001 from the catalog replaces the app's paused MV with a live one owned by the lifecycle service; uninstalling the app afterwards drops it again.

## Verifying

```bash
cd backend && CGO_ENABLED=0 go test -tags integration ./internal/tpapp/ -run TestIntegration_TpappInstall -v
```

renders the app into a scratch database (`ag_test_<unix>`) on a live Timeplus (env `TIMEPLUS_HOST/PORT/USER/PASSWORD`; OTLP port `TPAPP_TEST_OTEL_PORT`, default 14318), executes every file in manifest order, asserts every object exists and the rule MVs report `Paused` in `system.local_system_states`, re-runs all files (upgrade path), and drops the database with `CASCADE`. A real `.tpapp` install needs a Timeplus Enterprise build that exposes the apps API (`/<tenant>/api/v1beta2/apps`); 3.2.1 does not.

## Publishing

The apps catalog PR copies `apps/agentguard/` verbatim. Bump `AppVersion` in `backend/internal/tpapp/build.go` with each published change and regenerate.
