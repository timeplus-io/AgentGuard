# Single-Binary AgentGuard (C1: AgentGuard supervises timeplusd) — Design

- **Date:** 2026-06-01
- **Status:** Approved for planning (v3) — security/auth/output-handling tightened per review
- **Author:** yun (with Claude)
- **Related discussion:** Slack thread (Gang Tao ↔ Haohang) — "single binary of agentguard, like embed agentguard into a timeplusd"

## 1. Background & motivation

AgentGuard today ships as **two artifacts**: the `agentguard` container (a static Go
binary with the React SPA embedded) and the `timeplus/timeplus-enterprise` container
(the `timeplusd` streaming engine). `docker compose up` wires them together over the
compose network; AgentGuard talks to timeplusd over HTTP `:3218` / native `:8463`.

The goal is **operational simplicity — "one thing to deploy/run."** Not a literal single
file, not in-engine logic. After evaluating the option space (see §9), we chose **C1**:

> **C1** — AgentGuard is the launcher/parent and starts `timeplusd` as a supervised
> child. **C2** (timeplusd is the parent that starts AgentGuard) was rejected: it forces
> a fork of the 600 MB proton-enterprise C++ codebase, makes the DBMS own AgentGuard's
> lifecycle/packaging, and points process supervision the wrong direction.

Both C1 and C2 are still **two OS processes at runtime**; C1's defining trait is that the
*lightweight Go app owns lifecycle and packaging*, requiring **zero C++/proton-enterprise
changes**. Go is well-suited to process supervision (`os/exec`, signal handling).

### Constraints from the discussion

1. **Additive / dual delivery during transition** — the existing 2-container compose
   deployment must keep working unchanged. C1 is a *new mode*, not a replacement.
2. **Onboarding enhancement** — because AgentGuard now controls timeplusd startup and
   credentials, the setup wizard's connection + provisioning steps should auto-complete.
3. **License handling deferred** — design the seam to pass a Timeplus license to the
   child timeplusd, but defer enforcement.

## 2. Goals / Non-goals

**Goals**
- A single `agentguard` invocation brings up the whole stack (AgentGuard + a supervised
  `timeplusd`) with correct startup ordering, health gating, and graceful shutdown.
- The existing external-timeplusd mode (and the compose package) is byte-for-byte
  unchanged.
- AgentGuard resolves the `timeplusd` binary flexibly (flag → env → `$PATH`), with an
  optional build tag that embeds timeplusd for a true single-file artifact.
- First-boot onboarding (connection config + provisioning) completes automatically in the
  all-in-one mode.
- The UI/API is reachable from the start of boot, exposing supervisor state so startup,
  license, and provisioning failures can be diagnosed and retried in the browser — not only
  in logs.

**Non-goals**
- No changes to proton-enterprise / timeplusd C++.
- No in-engine (C++) reimplementation of AgentGuard logic; no removal of the HTTP hop.
- No change to how agent plugins reach the engine (they still hit timeplusd directly for
  hook-event inserts and OTLP — see §6).
- License handling — deferred entirely. (The v1 placeholder `--timeplusd-license` flag was
  removed because it was a no-op; the flag returns wired when enforcement lands.)
- Restart-in-place of a crashed child (v1 is fail-fast; see §5.4).

## 3. High-level approach

Add an **all-in-one mode** to the AgentGuard binary. A new `internal/supervisor` package
encapsulates child-process management; `cmd/server/main.go` gains a subcommand that wires
it in. Existing `runServer` behavior is untouched and remains the default.

```
agentguard up            # all-in-one: supervise timeplusd, then serve
agentguard               # (unchanged) serve against an external timeplusd
```

Runtime topology in all-in-one mode (one unit, two processes):

```
        ┌──────────────────────────── agentguard (parent, PID 1 in container) ───────────┐
        │  1. start AgentGuard HTTP server :8080 immediately (state = starting)           │
        │     → UI/API + /api/supervisor/status reachable before the engine is up         │
        │  2. [background] resolve timeplusd binary + config; exec child (watchdog off)    │
        │  3. [background] poll TCP :3218 until healthy ───────► [timeplusd] :3218/:8463   │
        │     state: starting → healthy                                      :4317/:4318   │
        │  4. [background] auto-provision (timeplus.Init) if not complete                  │
        │     state: healthy → provisioning → ready  (or → failed{reason} + retry in UI)   │
        │  signals: SIGTERM/SIGINT → drain :8080 → SIGTERM child → wait → SIGKILL          │
        └─────────────────────────────────────────────────────────────────────────────────┘
```

## 4. timeplusd binary resolution

The launcher resolves the `timeplusd` executable in this order:

1. `--timeplusd-bin <path>` flag
2. `TIMEPLUSD_BIN` env var
3. `$PATH` lookup (`timeplusd`)
4. **Embedded** copy (only when built with `-tags embed_timeplusd`): extracted once to the
   data dir (e.g. `<data>/bin/timeplusd`), with a checksum/size guard to avoid re-extracting
   on every boot, then executed from there.

This yields **two build flavors from one codebase**:
- **Default build** (`go build`): small binary; expects `timeplusd` present (Docker image
  with timeplusd in the base layer, or installed on PATH). Powers the "all-in-one image".
- **`embed_timeplusd` build**: ~600 MB self-contained file; powers the "scp-and-run"
  single-file artifact.

Embedding uses `go:embed` behind the build tag so the default build never pays the size cost.

### 4.1 Packaging

- **All-in-one image (new):** a new image variant
  `FROM timeplus/timeplus-enterprise:${TIMEPLUS_ENTERPRISE_TAG}` (so `timeplusd` is already
  present), with the default-build `agentguard` binary copied in and `agentguard up` as the
  entrypoint. The tag is a **build arg** — `ARG TIMEPLUS_ENTERPRISE_TAG=3.2.11` — so builds
  are reproducible by default while release automation can bump the engine via
  `--build-arg TIMEPLUS_ENTERPRISE_TAG=…` without editing Dockerfile text. This is a *new*
  Dockerfile/target; the existing `Dockerfile` (`FROM alpine`, agentguard only) and
  `docker-compose.yaml` are left unchanged so the current 2-container package keeps shipping.
- **Single-file (new):** the `embed_timeplusd` build produces the portable ~600 MB binary.
- **Existing package (unchanged):** `Dockerfile` + `docker-compose.yaml` + the prebuilt
  `timeplus/agentguard:<sha>` image continue as-is during the transition.

## 5. Lifecycle & process supervision (`internal/supervisor`)

### 5.1 Boot sequence (HTTP-first) & state machine
The AgentGuard HTTP server comes up **first**, before timeplusd, so the UI/API is always
reachable to diagnose and retry startup problems (engine won't boot, bad license, port
occupied, provisioning failed) — instead of failures being visible only in logs.

0. **Pre-flight:** resolve the `timeplusd` binary path (§4) — including extraction for the
   embed build. If unresolvable, fail fast with guidance **before binding the HTTP server**:
   a *pre-supervisor initialization failure* (§8), outside the state machine below.
1. Start the AgentGuard HTTP server (`:8080`) immediately via the existing router, plus the
   supervisor status/retry routes (§5.7). Supervisor state = `starting`.
2. In a **background goroutine**, the supervisor resolves/generates the timeplusd config
   (§5.5), then `exec.CommandContext`s the child with `TIMEPLUSD_WATCHDOG_ENABLE=0` (using
   the pre-flighted binary path).
3. Health-gate: dial TCP `localhost:3218` on an interval until ready or a startup timeout
   elapses (mirrors the compose healthcheck `echo > /dev/tcp/localhost/3218`). On success,
   state `starting → healthy`.
4. If `setup.complete` is false, auto-provision (§7): state `healthy → provisioning`, then
   on success `provisioning → ready`. If already provisioned, go straight to `ready`.

The supervisor state machine the UI reflects:

```
starting ──► healthy ──► provisioning ──► ready
   │            │              │
   └────────────┴──────────────┴────► failed{reason}  ──(retry via UI/API)──► starting
```

`failed{reason}` carries a machine- and human-readable reason (`engine_unhealthy`,
`engine_exited`, `provision_failed`) plus detail text. Data endpoints that need the engine
degrade gracefully (or return a "warming up" response keyed off supervisor state) until
`ready`; the app already starts independently of Timeplus availability.

### 5.2 Disable the child's watchdog
timeplusd has a self-restart watchdog (`Server.cpp`: gated by `TIMEPLUSD_WATCHDOG_ENABLE`,
and auto-enabled when stdio is not a TTY — i.e. in containers). We set
`TIMEPLUSD_WATCHDOG_ENABLE=0` so AgentGuard is the **sole** supervisor and the process tree
stays flat (no re-exec'd grandchild to track/reap).

### 5.3 Signals & graceful shutdown
- Parent traps `SIGINT`/`SIGTERM`.
- On signal: stop accepting new work and drain the AgentGuard HTTP server (existing
  `http.Server.Shutdown`), then send `SIGTERM` to the child, wait up to a grace timeout,
  then `SIGKILL`.
- The child is started in its own process group so the parent can signal it precisely.
- `cmd.Wait()` reaps the child (correct even when AgentGuard is PID 1 in a container).

### 5.4 Failure & crash policy (phase-split)
- **During startup (before `ready`)** — any failure (engine never healthy, bad license,
  port occupied, provisioning error, or the child exiting) transitions the supervisor to
  `failed{reason}` and **keeps the HTTP server/UI up** so the operator can read the reason
  and retry (§5.7). The process does **not** exit.
- **After `ready` (runtime child exit)** — v1 is **fail-fast**: AgentGuard logs the exit
  code, shuts its server down cleanly, and exits **non-zero** so Docker/k8s restarts the
  whole unit. (Surfacing a post-`ready` `failed` state with in-UI restart, and
  restart-in-place, are deferred — §10.)

### 5.5 timeplusd configuration & child credentials
v1 launches `timeplusd server` and lets the engine use its **image-default config**: the
standard ports (`3218/8123/8463/4317/4318/8000`) and the base image's default data dir
(`/timeplus/timeplusd-data`). AgentGuard only forces watchdog-off via the child's
environment (§5.2); it does **not** render a config or relocate the data dir — `timeplusd
server` has no `--path` flag, so relocation would need a full config template (deferred).
`--data-dir` is therefore AgentGuard's own state dir (and where the embed build extracts the
bundled binary); persisting engine state is a volume at the engine data dir, documented in
`Dockerfile.aio`.

**Child credentials (v1 = built-in `default` user + future-ready seam).** All-in-one mode
is **local child ownership**: the supervisor owns the co-located timeplusd's credential
lifecycle. Verified against the real image (3.2.11): AgentGuard launches `timeplusd server`
directly, bypassing the timeplus-enterprise image's `entrypoint.sh` that would provision the
`proton` user — so the only account that exists is the built-in **`default` user with an
empty password**, which is what v1 uses for both the child and AgentGuard's own client. We
do **not** build custom-user setup unless a stable Timeplus bootstrap API/config path
already exists (C1's risk is supervision/packaging shape, not auth rotation). A no-password
`default` user behind exposed engine ports is wide open — strictly dev/gated-preview; GA
hardening (generated creds and/or loopback bind) is in §10. To keep this replaceable without
touching the supervisor contract, credentials flow through a single seam:

```go
// SupervisorTimeplusCredentials yields the creds the child is launched with and that
// AgentGuard's client uses. v1: a static provider returning the stock dev creds.
// Later: a generated/rotated-creds provider can replace it without changing callers.
type SupervisorTimeplusCredentials interface {
    User() string
    Password() string
}
```

**Security constraint (engine exposure).** Because the engine ports (`:3218/:8463/…`, §6)
remain externally publishable, stock dev creds are acceptable **only** for local/dev or a
gated preview. A GA all-in-one deployment must do at least one of: generated per-deployment
creds (via the seam above), non-public bind defaults (engine ports bound to loopback unless
explicitly exposed), or a documented secure-deployment profile. This is GA-blocking work
(§10) — v1 must not be presented as production-ready while it pairs stock creds with
publicly exposed engine ports.

Exact `timeplusd` launch invocation (`timeplusd server --config …` vs env/args) and how a
non-default user would be configured remain implementation details to verify against the
real binary (§8).

### 5.6 Log handling & captured output
Child `stdout`/`stderr` are **streamed continuously** to the parent's (line-prefixed
`[timeplusd]`) so a slow/blocked reader never stalls the child. In parallel, the supervisor
retains the **last N lines in a bounded ring buffer** for the diagnostic UI. Any output
surfaced via `/api/supervisor/status` `detail` is **size-capped** and **redacted** of
license- and credential-looking strings (e.g. `password=…`, license keys/tokens) before it
leaves the process.

### 5.7 Status & diagnostic surface (API + UI)
Because the HTTP server is up from `starting`, AgentGuard exposes the supervisor state so
users can diagnose/retry from the browser instead of the logs:
- `GET /api/supervisor/status` → `{ state, reason?, detail?, since, engine: {pid?, healthy},
  provisioned }`. **Unauthenticated but sanitized** (no creds/license/internal paths) so the
  boot screen can poll it pre-login/pre-setup. Absent/`disabled` in the external-timeplusd
  mode.
- `POST /api/supervisor/retry` → re-runs the failed transition (re-provision, or restart the
  child if it exited during startup); valid only from a `failed` state. **Auth: setup-phase
  unauthenticated** (so a first-boot failure is recoverable before any session exists), then
  **requires an authenticated session once `setup.complete`** — it can restart a child
  process and must not be broadly callable.
- **UI:** a lightweight boot/status screen (reusing the full-page Onboarding shell) shown
  until state = `ready`, polling `/api/supervisor/status`, rendering the current phase and,
  on `failed`, the reason + a Retry button. Once `ready`, the app proceeds normally.

These routes exist only in all-in-one mode; the external-timeplusd serve path is unchanged.

## 6. Network topology (unchanged external surface)

A single deployable does **not** reduce the exposed port surface, because agent plugins
talk to timeplusd *directly*:
- AgentGuard `:8080` — UI, REST API, `/api/holds` (plugins), `/api/alerts/webhook`
  (Timeplus → AgentGuard callback; in all-in-one mode the callback URL is
  `http://localhost:8080`).
- timeplusd `:3218` (HTTP — hook-event inserts + queries), `:8463` (native),
  `:4317`/`:4318` (OTLP telemetry), `:8000` (TE console, optional), `:8123` (HTTP normal).

The all-in-one image/file therefore still publishes these ports; "single binary" changes
*who launches and supervises*, not the wire surface.

## 7. Onboarding enhancement

Today provisioning is wizard-triggered (`POST /api/setup/provision` → `timeplus.Init()`),
and `setup.complete` (in `config.yaml`) gates it. In all-in-one mode:

- **Auto-config:** AgentGuard sets the child's connection target to `localhost:3218` /
  native `:8463` and uses the credentials from the `SupervisorTimeplusCredentials` seam
  (§5.5) — so the wizard's connection step is pre-satisfied (no user entry).
- **Auto-provision:** once timeplusd is healthy on first boot and `setup.complete` is
  false, AgentGuard runs `timeplus.Init()` programmatically (creating OTel/rules/security
  streams + syncing MVs) as the `provisioning` state transition (§5.1), then persists
  `setup.complete = true` and moves to `ready`. A provisioning error →
  `failed{provision_failed}` with the reason shown in the UI and a Retry that re-runs
  `timeplus.Init()` (§5.7).
- **Wizard collapse:** in this mode the setup wizard becomes optional/no-op; the app boots
  straight to a working state (or to the boot/status screen on failure).

The wizard and manual provisioning remain fully functional in the external-timeplusd mode.

## 8. Error handling & edge cases

All **startup-phase** failures below transition the supervisor to `failed{reason}` and
**keep the HTTP server/UI up** so the operator can read the reason and Retry (§5.4, §5.7) —
they do **not** exit the process. Each surfaces the **bounded, redacted** child output
(§5.6) in the failure detail.

- **timeplusd never becomes healthy** within the startup timeout (incl. bad license) →
  `failed{engine_unhealthy}`.
- **Child exits during startup** → `failed{engine_exited}`.
- **Port already in use** (e.g. an external timeplusd already running) → detected via the
  failed bind / health probe → `failed{engine_unhealthy}` with a clear message.
- **Provisioning fails** → `failed{provision_failed}`; do not mark `setup.complete`; leave
  timeplusd running so Retry can re-run `timeplus.Init()`.
- **Binary not resolvable** (no flag/env/PATH, not an embed build) → a **pre-supervisor
  initialization failure**: caught by the pre-flight (§5.1) *before* the HTTP server binds
  and the state machine starts, so it is not a `failed{reason}` state but a fail-fast at
  process start with guidance on `--timeplusd-bin` / the embed build.
- **Signal during startup** → supervisor handles a signal arriving before `ready` (kill
  child, abort startup, exit cleanly).

**Open implementation details to resolve in the plan (verify against the real binary):**
exact `timeplusd` launch subcommand/args; how a non-default child user/password would be
configured (v1 uses stock dev creds via the §5.5 seam); the embedded-config template
contents; first-run extraction path + checksum guard.

## 9. Options considered (for the record)

| Option | Description | Verdict |
|---|---|---|
| B | Two containers, wrap `docker compose up` in one command | Viable fallback; "two things" remains |
| A | One image, two processes, init (`s6`/`tini`) supervises | Strong alternative; rejected in favor of C1's single-binary ownership |
| **C1** | **AgentGuard (Go) parent supervises timeplusd child** | **Chosen** |
| C2 | timeplusd (C++) parent supervises AgentGuard child | Rejected — forks proton C++, DB owns app lifecycle, wrong supervision direction |
| D1 | `cgo`-link timeplusd as a library into Go | Infeasible (timeplusd is an app, ~600 MB) |
| D2 | Rewrite AgentGuard backend as native C++ timeplusd handlers | Out of scope — highest effort, permanent upstream rebase |

## 10. Future work / transition

- **Transition:** ship both packages — the existing compose 2-container package *and* the
  new all-in-one mode — until the all-in-one path is proven.
- **License enforcement:** add a `--timeplusd-license` / env passthrough and enforce it
  end-to-end (file/key placement, validation). The v1 placeholder flag was removed because
  it was a no-op.
- **Restart-in-place:** optional supervisor policy to restart a crashed timeplusd instead
  of fail-fast.
- **Multi-platform timeplusd:** the embed build is tied to a specific OS/arch timeplusd
  (currently linux/amd64); document/guard this.
- **GA hardening (engine exposure) — GA-blocking:** generated per-deployment creds,
  loopback-by-default engine bind, and/or a documented secure-deployment profile before GA.
  Stock dev creds with publicly exposed `:3218/:8463` is not acceptable for production
  (§5.5).

## 11. Testing strategy

- **Unit (supervisor):** binary resolution order; **state-machine transitions**
  (`starting → healthy → provisioning → ready`, and each → `failed{reason}` + Retry back to
  `starting`); signal → shutdown ordering; post-`ready` fail-fast on child exit;
  startup-timeout path. Use a fake "child" executable (a small script/stub) to exercise
  supervision without launching real timeplusd.
- **Unit (HTTP-first):** `GET /api/supervisor/status` reflects the current state and is
  reachable while state = `starting` (before the child is healthy); `POST
  /api/supervisor/retry` only acts from `failed`.
- **Unit (onboarding):** auto-provision runs once when `setup.complete` is false and is
  skipped when true; provisioning error → `failed{provision_failed}` and Retry re-runs it;
  config auto-population points at `localhost` via the credentials seam.
- **Integration (smoke):** `agentguard up` against a real timeplusd (default build, binary
  on PATH) serves `:8080` immediately, transitions to `ready` after auto-provision, and
  `SIGTERM` shuts both down cleanly. Run in CI/Docker where timeplusd is available.
- **Regression:** existing handler/timeline tests and the compose path remain green; the
  default `agentguard` serve mode is unaffected.
