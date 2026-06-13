# Configuration UI — Design

**Issue:** [#180 — Configuration UI: edit all AgentGuard server configuration from the web app](https://github.com/timeplus-io/AgentGuard/issues/180)
**Date:** 2026-06-12
**Status:** Approved

## Problem

AgentGuard's server configuration (`config.yaml`) can only be changed by editing the
file on the host (or `docker exec` into the container) and restarting the server.
The motivating example: enabling Semantic DLP on a Docker deployment requires shell
access to append a `dlp:` section and restart the container.

A logged-in user should be able to view the full effective configuration (with its
source), edit editable keys from a settings page, persist them, and be told clearly
when a restart or re-provision is required — without shell access.

## Decisions (from design review)

| Question | Decision |
|---|---|
| Apply model | Save to `config.yaml`, hot-apply what is safe via the live `*config.Config` pointer, show a persistent "restart required" banner for the rest. No self-restart. |
| Secrets | `dlp.api_key` becomes editable but **write-only** (never echoed; `config.Save` extended to persist it). Timeplus user/password remain env-only, shown read-only as "from environment". |
| DLP model changes | Save + warn. The UI detects drift between saved DLP model config and what was provisioned, and links to the existing setup/provision flow. No new destructive recreate flow in this issue. |
| Precedence | Every key shows its effective value plus a source chip (default / file / env / flag). Keys overridden by env or flag are **read-only** with an explanation, preventing silent no-op saves. |
| Auth & nav | Page sits behind the existing session auth (protected group); any logged-in user may edit (AgentGuard has no role system). New "Settings" item at the bottom of the sidebar, route `/settings`. |
| Test actions | Timeplus connectivity test (new authenticated endpoint using session credentials) and DLP endpoint reachability test, both runnable against unsaved form values. |
| Architecture | Single key registry with metadata drives both the API and a mostly-generic frontend renderer. |

## Backend

### Key registry (`backend/internal/config/registry.go`)

One declarative table describing every config key:

```go
type KeyMeta struct {
    Key         string // viper key, e.g. "dlp.api_key"
    Section     string // "server", "timeplus", "agentguard", "catalog", "holds", "dlp", "setup"
    Label       string
    Description string
    Type        string // "string" | "int" | "bool" | "secret" | "enum"
    EnumValues  []string
    EnvVar      string // e.g. "DLP_API_KEY"; empty if none
    FlagName    string // e.g. "port"; empty if none
    Restart     bool   // change requires server restart
    Reprovision bool   // change requires DLP re-provision
    ReadOnly    bool   // shown but never editable (setup.complete, timeplus.user/password)
    Validate    func(v any) error
}
```

Covered keys (matching `config.go`): `server.port`*, `catalog.dir`*, `agentguard.url`,
`timeplus.host`, `timeplus.port`, `timeplus.native-port`, `holds.mv_wait_ms`,
`holds.timeout_seconds`, `holds.fail_policy` (enum: `deny|allow`), `holds.disabled`,
`dlp.enabled`, `dlp.base_url`, `dlp.api_key` (secret), `dlp.embedding_model`†,
`dlp.embedding_dim`†, `dlp.risk_model`†, `dlp.summary_model`†, `setup.complete` (read-only),
`timeplus.user` / `timeplus.password` (read-only, env-only).
\* = restart required. † = re-provision required.

Validators: port ranges 1–65535, `embedding_dim ≥ 1`, `holds.fail_policy` enum,
URL syntax for `agentguard.url` / `dlp.base_url`, non-negative hold timings.

### Source detection

Precedence (highest first): CLI flag > env var > config file > default.

- **flag** — `main.go` records explicitly-passed flags (`cmd.Flags().Changed(name)`)
  into a set passed to the handlers.
- **env** — `os.Getenv(meta.EnvVar) != ""`.
- **file** — the YAML at `cfg.ConfigPath` is parsed into a map; key presence checked.
- **default** — otherwise.

A key whose effective source is `env` or `flag` is reported `editable: false` with the
masking variable/flag named, because a file write would be silently masked.

### Save semantics — merge, not dump

`PUT /api/config` reads the existing `config.yaml` into a map, sets **only** the keys
the user changed, and writes back with file mode `0600`. Effective values derived from
env/flags are never frozen into the file. YAML comments are lost on round-trip
(accepted). The existing `config.Save()` full-dump remains for the provision flow but
gains `dlp.api_key` persistence; its doc comment is updated accordingly.

### Apply semantics and the restart banner

- **Hot keys** (everything except restart-flagged): after a successful file write the
  new values are applied to the live `*config.Config` under the existing `config.Mu`
  write lock, taking effect on the next request.
- **Restart keys** (`server.port`, `catalog.dir`): file write only; the live config is
  left untouched.
- `restart_required` in `GET /api/config` = any restart-flagged key whose freshly
  resolved effective value (full flag > env > file > default precedence, i.e. what the
  server would use after a restart) differs from the live value. Comparing resolved
  values rather than raw file values avoids a false banner when an env var masks a
  stale file entry. Because the live value only changes at startup, the banner
  naturally persists across page reloads and disappears after restart.

### Re-provision drift

DLP model settings are baked into UDFs (and `embedding_dim` into the
`agentguard_dlp_enriched` stream) at provision time, so a config edit alone changes
nothing at query time.

- When DLP provisioning succeeds, the Provision handler writes a snapshot of the four
  DLP model keys under `provisioned.dlp.*` in `config.yaml`.
- `reprovision_required` = effective `dlp.embedding_model|embedding_dim|risk_model|summary_model`
  differ from the snapshot (only evaluated when a snapshot exists and `dlp.enabled`).
- Deployments provisioned before this feature have no snapshot: no drift is shown until
  their next provision records one. Documented limitation.
- The UI banner links to the existing setup/provision flow; `embedding_dim` changes
  additionally warn that re-provisioning recreates the DLP stream.

### API endpoints (all in the protected group; new `internal/handlers/settings.go`)

| Method | Path | Behavior |
|---|---|---|
| GET | `/api/config` | `{restart_required, reprovision_required, sections: [{id, title, keys: [{key, label, description, type, value, set, source, env_var, flag, editable, restart, reprovision, enum_values}]}]}`. Secrets report `set: true/false` and never the value. |
| PUT | `/api/config` | Body: flat map `{"dlp.enabled": true, ...}`. Validates the whole batch first — any invalid, unknown, read-only, or env/flag-overridden key rejects the entire request with per-key errors (atomic). On success: merge-write file, hot-apply, return `{saved: [...], restart_required, reprovision_required}`. |
| POST | `/api/config/test-timeplus` | Body `{host, port}`. Runs `SELECT 1` against the candidate host/port using the **session cookie's** Timeplus credentials (`auth.RequestCfg` pattern). The existing unauthenticated `/api/setup/test-connection` is unsuitable because it requires explicit credentials the settings page does not hold. |
| POST | `/api/config/test-dlp` | Body `{base_url, api_key?}`. GETs `base_url + "/models"` with the bearer key; absent key falls back to the currently-configured one so users can test without re-entering the secret. Returns `{ok}` or `{ok: false, error}`. |

### Secret write semantics (`dlp.api_key`)

In `PUT /api/config`: key absent → unchanged; non-empty string → set; empty string → clear.
`GET` exposes only `{type: "secret", set: bool, source, editable}`.

## Frontend

### Route & navigation

- `pages/Settings.jsx`, route `/settings` in `App.jsx`, standard
  Sidebar/TopNav/Footer layout (`pl-64 pt-16 pb-8`).
- "Settings" nav item with gear icon appended to the sidebar nav list.

### Page structure

Metadata-driven: renders whatever sections/keys `GET /api/config` returns. Each
section is a tonal surface card per the Digital Sentry rules (background-tier
separation, no borders/shadows, 4px radius, named Tailwind tokens).

Per key:

- Label + description; input by type — text, number, `Toggle` for bools, select for
  enums, password input for secrets showing "•••••• (set)" / "not set" with a clear action.
- Source chip: `default` / `config file` / `env` / `flag`. Env/flag-overridden keys
  render disabled with the explanation "Set by `DLP_API_KEY` — file edits have no
  effect until the variable is removed."
- Restart/re-provision-flagged keys carry an inline hint ("requires restart" /
  "requires re-provision").

### Saving & feedback

- Edits accumulate in local dirty state; a sticky bottom save bar appears
  ("N unsaved changes — Save / Discard") using the gradient CTA
  (`bg-gradient-to-br from-[#ffb0cf] to-[#f256a3]`, `text-[#3d0023]`).
- On save, `PUT` only the changed keys; per-key validation errors map back to their inputs.
- `restart_required` → persistent top banner: "Saved configuration differs from the
  running server — restart AgentGuard to apply."
- `reprovision_required` → warning banner: "DLP model settings differ from what's
  provisioned — re-run provisioning", linking to the setup/provision flow.

### Test actions

- Timeplus section: "Test connection" button → `POST /api/config/test-timeplus` with
  current (possibly unsaved) form values; inline success/failure.
- DLP section: "Test endpoint" button → `POST /api/config/test-dlp` likewise.

### API client additions (`api/client.js`)

`api.getConfig()`, `api.updateConfig(changes)`, `api.testTimeplusConfig({host, port})`,
`api.testDLPConfig({base_url, api_key})`.

## Error handling

- `PUT` with any invalid key → 400 with `{errors: {"key": "message"}}`; nothing written.
- File write failure (read-only filesystem, missing path) → 500 with the OS error;
  live config untouched.
- Test endpoints surface upstream errors verbatim in `{ok: false, error}` — they never 5xx
  for unreachable targets.
- Concurrent PUTs serialize on `config.Mu`; last write wins (no optimistic locking — single
  small team UI, acceptable).

## Testing

Go table tests (`internal/config/registry_test.go`, `internal/handlers/settings_test.go`):

- Source detection across default/file/env/flag combinations (env vars set via `t.Setenv`).
- Merge-write preserves unrelated file keys; does not write env-derived values.
- Secret semantics: absent/set/clear; GET never echoes the value.
- Validation: atomic batch rejection, per-key errors, unknown/read-only/overridden key rejection.
- `restart_required` diff computation (file vs live values).
- Re-provision drift: snapshot present/absent, matching/differing values.

Frontend: ESLint + manual walkthrough (no FE test harness exists in the repo).

## Out of scope

- Self-restart / process management.
- Editing Timeplus service credentials from the UI.
- A destructive DLP re-provision (stream recreate) flow triggered from the settings page.
- Roles/permissions beyond the existing session auth.
- Optimistic concurrency control on config writes.
