# AgentGuard — Pre-Tool-Call Blocking (Holds)

AgentGuard ingests agent telemetry asynchronously by default — events are fired and forgotten, and threats appear seconds later. That is fine for observability, but inadequate when you want to **prevent** a dangerous tool call (e.g. `rm -rf /`, `git push --force`, an `Edit` against a sensitive file) from executing in the first place.

The **Holds** feature adds a synchronous gate on the `PreToolUse` hook: the agent's tool invocation is paused while AgentGuard ingests the event, waits for streaming SQL rules to evaluate it, and returns one of three decisions — `allow`, `block`, or `hold` (a human-decided pending state). Only when the decision arrives does the agent proceed with — or skip — the tool call.

The mechanism is designed to be:
- **Opt-in per plugin** — all plugins default `holdsEnabled` to `false`; enabling adds round-trip latency to every tool call.
- **Fail-safe** — when the AgentGuard server is unreachable, a per-plugin `holdFailPolicy` decides whether to deny or allow.
- **Crash-tolerant** — pending holds are persisted in Timeplus; backend restarts mark stale entries as `abandoned` instead of leaving them stuck forever.

---

## Table of Contents

1. [Motivation](#1-motivation)
2. [Architecture](#2-architecture)
3. [End-to-End Sequence](#3-end-to-end-sequence)
4. [Decision Logic](#4-decision-logic)
5. [Storage Model](#5-storage-model)
6. [Configuration](#6-configuration)
7. [Failure Modes](#7-failure-modes)
8. [API Surface](#8-api-surface)
9. [Operational Notes](#9-operational-notes)

---

## 1. Motivation

The default ingest path (`agentguard_hook_events`) is fire-and-forget: the plugin POSTs to Timeplus and immediately returns control to the agent. By the time a streaming rule fires, the tool call may have already executed. That is acceptable for **detection** (record the threat, alert the operator), but unacceptable for **prevention**.

Agent CLIs (Claude Code, OpenClaw, Hermes) all expose a synchronous pre-tool-call hook that, if it returns within the hook timeout, can deny the tool call. AgentGuard exploits this by routing only the `PreToolUse` hook through a synchronous endpoint (`/api/holds`) that returns once the decision is known.

Three decision outcomes:

| Decision | Effect | Example block_policy |
|---|---|---|
| `allow` | Tool call proceeds normally | `log_only` (record, don't pause) |
| `block` | Tool call is denied immediately, no human prompt | `auto_block` (deny on rule match) |
| `hold` | Agent waits up to 540 s for a human Approve/Deny in the UI | `hold` (require approval) |

---

## 2. Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│  Agent (Claude Code / OpenClaw / Hermes)                                │
│                                                                         │
│   PreToolUse hook fires                                                 │
│        │                                                                │
│        ▼                                                                │
│   Plugin: requestHold()  ────────────────►  POST /api/holds (sync)      │
│        ▲                                            │                   │
│        │                                            ▼                   │
│        │                       ┌─────────────────────────────────────┐  │
│        │                       │ AgentGuard backend                  │  │
│        │                       │                                     │  │
│        │                       │  1. Insert into agentguard_hook_events
│        │                       │  2. Query pre-existing open threats │  │
│        │                       │  3. Poll agentguard_security_events │  │
│        │                       │     for ≤ holds.mv_wait_ms (500 ms) │  │
│        │                       │  4. Decide(allow / block / hold)    │  │
│        │                       │                                     │  │
│        │                       │  if hold:                           │  │
│        │                       │    insert pending row → agentguard_holds
│        │                       │    register channel in holdRegistry │  │
│        │                       │    publish SSE → UI                 │  │
│        │                       │    select { ch | timeout }          │  │
│        │                       └────┬───────────────────┬────────────┘  │
│        │                            │                   │               │
│        │  HoldResponse              │                   │ POST /api/holds/:id/decide
│        │  {decision, reason}        │                   │   (UI operator)
│        ◄────────────────────────────┘                   │               │
│                                                         ▼               │
│   Plugin writes permissionDecision               Decide goroutine wakes │
│   to agent stdout (allow|deny)                   via channel signal    │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

Key components:

| Component | File | Role |
|---|---|---|
| Plugin hold client | `agents/<agent>/agentguard-plugin/src/holds.ts` (claudecode, openclaw); `agentguard_hermes/plugin.py` (hermes) | POSTs to `/api/holds`, applies `holdFailPolicy` on network errors, formats the agent-specific permission response |
| Hold handler | `backend/internal/handlers/holds.go` | Ingests event, queries threats, polls MV results, builds `DecisionInput`, long-polls for human decision |
| Pure decision function | `backend/internal/handlers/holds_decision.go` | `Decide(DecisionInput) DecisionOutput` — strictly: `hold > auto_block > log_only` |
| In-memory hold registry | `backend/internal/holdregistry/registry.go` | Maps `hold_id → buffered chan Decision`; lost on restart, but timeout + fail-policy take over |
| Persistence | `backend/internal/timeplus/holds_init.go` | `agentguard_holds` append-only stream. Latest state per `hold_id` is computed **inline at query time** via `arg_max(field, updated_at) GROUP BY hold_id` (see `holdsLatestSubquery` in `handlers/holds.go`). No stateful materialized view. |
| Cleanup | `AbandonStalePendingHolds` (called from `auth.MigrateHoldsSchema` on startup) | Marks orphaned `pending` rows as `abandoned` |

---

## 3. End-to-End Sequence

The following walks through a single `PreToolUse` hook from agent to decision, taking the `hold` (human-decided) branch.

```
Time   Actor                  Action
───── ───────────────────── ────────────────────────────────────────
t=0    Agent (Claude Code)   PreToolUse hook fires; spawns plugin process
                              with hook payload on stdin

t+5ms  Plugin (index.ts)     Detects hookEventName === "PreToolUse"
                              Reads holdsEnabled — if false, write
                                permissionDecision="allow" and exit.
                              Else: import holds.js, call requestHold()

t+10ms Plugin (holds.ts)     POST {agentguardUrl}/api/holds
                              body: {deployment_id, agent_id, session_id,
                                     run_id, tool_name, event_time,
                                     tool_input, hook_payload}
                              AbortController set to 540 s

t+12ms Backend (holds.go)    pluginCfg() — verify TIMEPLUS_USER set
                              ingestHookEvent() — INSERT row into
                                agentguard_hook_events with
                                hook_name='before_tool_call',
                                hook_decision='observe'

t+50ms Timeplus              MVs (mv_rule_*, mv_cim_*) consume the
                              new event; matching rules write to
                              agentguard_security_events

t+50ms Backend               queryPreExistingOpenThreats() — look up
                              any open threats already on this session
                              and join with current rule block_policy

t+100ms-                     waitForNewMVDetections() — poll
t+600ms                       agentguard_security_events every 50 ms
                              for up to holds.mv_wait_ms (default 500),
                              filtering on (deployment, agent, session)
                              and detected_at >= event_time, joined
                              with block_policy. Stop early on first
                              non-empty result.

t+600ms Backend (Decide)     Pure function: strictest policy across
                              new matches + open threats.
                              hold > auto_block > log_only
                              → DecisionOutput.Decision = "hold"

t+605ms Backend              uuid.New() → hold_id
                              writeHoldRow() — INSERT pending row into
                                agentguard_holds
                              registry.Register(hold_id) — buffered chan
                              broadcaster.Publish() — SSE to /api/notifications/stream
                              select { case d := <-ch | case <-time.After(timeoutSeconds) }

t+606ms UI (NotificationsProvider)
                             Receives SSE, shows toast + refreshes
                             /threats Pending Holds list

t+30s   Operator             Clicks Approve/Deny in UI

t+30.1s Frontend             POST /api/holds/{hold_id}/decide
                              body: {decision: "approved", decided_by, decided_reason}

t+30.2s Backend (DecideHold) writeHoldStatus() — INSERT new row into
                              agentguard_holds with status='approved'
                              registry.Signal(hold_id, {Allow:true})

t+30.21s Backend (CreateHold goroutine wakes)
                             Sends HoldResponse {decision:"allow",hold_id}
                             on the open HTTP connection

t+30.22s Plugin              Receives JSON, writes to stdout:
                              { hookSpecificOutput: { hookEventName: "PreToolUse",
                                  permissionDecision: "allow", permissionDecisionReason: ... }}

t+30.23s Agent               Reads plugin's stdout; tool call proceeds
```

The same sequence applies to OpenClaw and Hermes — only the plugin-side permission-response shape differs.

---

## 4. Decision Logic

`Decide` is a pure function — no I/O — that takes everything the handler has collected and returns a single decision. Pure separation makes it trivial to unit-test (`backend/internal/handlers/holds_decision_test.go`).

### Inputs

```go
type DecisionInput struct {
    KillSwitch         bool                   // cfg.Holds.Disabled — server-wide skip
    Matches            []RuleMatch            // newly detected rules from MV-poll window
    PreExistingThreats []models.Threat        // already-open threats on this session
    ThreatRulePolicies map[string]string      // rule_id → block_policy for above
}

type RuleMatch struct {
    RuleID      string
    BlockPolicy string  // hold | auto_block | log_only
    Severity    string
    Message     string
}
```

### Algorithm

1. If `KillSwitch` → `allow`.
2. Concatenate `Matches` with pre-existing threats whose policy is non-trivial (`hold` or `auto_block`); skip `log_only`.
3. Pick the **strictest** policy:
   - Any `hold` match → `Decision="hold"` (oldest match wins for `RuleID`/`Reason`)
   - Else any `auto_block` match → `Decision="block"`
   - Else → `Decision="allow"`

### Why pre-existing threats matter

The MV-wait window is bounded (default 500 ms); some rules — e.g. cumulative ones that need multiple events — won't fire on the first matching tool call. Once they have fired, however, **every subsequent tool call in the same session** should be gated by that rule's policy. Including pre-existing open threats achieves this without re-querying historical data on every call.

---

## 5. Storage Model

### `agentguard_holds` — append-only event log

| Column | Type | Notes |
|---|---|---|
| `hold_id` | string | UUID, primary key for state-folding |
| `deployment_id`, `agent_id`, `session_id`, `run_id` | string | Identity quad |
| `tool_name` | string | Tool that triggered the hold |
| `rule_id`, `severity` | string | First rule whose policy triggered the hold |
| `args_summary` | string | First 2 KB of `tool_input` (truncated for UI display) |
| `message` | string | Human-readable reason, surfaced in UI |
| `created_at` | datetime64(3) | When the hold was opened |
| `status` | string | `pending` \| `approved` \| `denied` \| `timeout` \| `abandoned` |
| `decided_by`, `decided_at`, `decided_reason` | string / datetime64 / string | Filled on transition out of `pending` |
| `updated_at` | datetime64(3) | Required by `arg_max()` for state-folding |

Every state change appends a new row. There is no `UPDATE`. This matches the rest of AgentGuard's mutation-log style (`agentguard_rules`, `agentguard_threats`).

### Inline latest-state subquery

There is no stateful materialized view for holds. Latest-state-per-`hold_id` is computed inline at query time, matching the pattern used by `agentguard_rules` and other mutation logs:

```sql
SELECT hold_id,
       arg_max(deployment_id,  updated_at) AS deployment_id,
       arg_max(agent_id,       updated_at) AS agent_id,
       ...
       arg_max(status,         updated_at) AS status,
       arg_max(decided_by,     updated_at) AS decided_by,
       ...
FROM table(agentguard_holds)
GROUP BY hold_id
```

This subquery is exported as the `holdsLatestSubquery` constant in `handlers/holds.go` and embedded into every read path (`ListHolds`, `GetHold`, `AbandonSession`, `DrainHolds`, plus the pending-hold lookup in `GetThreat`).

> **Why no MV:** A streaming `MATERIALIZED VIEW … GROUP BY` in Proton emits one snapshot row per source-stream input — so the MV's bounded snapshot contains *all historical aggregates*, not "one row per key." A naive `WHERE status='pending'` against such an MV picks up stale snapshots from before a status change. Computing arg_max inline at query time is correct by construction and trivially fast for a low-volume stream like `agentguard_holds` (1 row per create + 1 per decision).

The full timeline (for `GET /api/holds/:id`) queries the raw `agentguard_holds` stream with `ORDER BY updated_at ASC` to render the audit trail.

---

## 6. Configuration

### Server side (`backend/internal/config/config.go`)

| Key | Env var | Default | Purpose |
|---|---|---|---|
| `holds.mv_wait_ms` | `AGENTGUARD_HOLD_MV_WAIT_MS` | `500` | How long to poll `agentguard_security_events` for fresh MV detections before deciding |
| `holds.timeout_seconds` | `AGENTGUARD_HOLD_TIMEOUT_SECONDS` | `300` | How long to long-poll waiting for a human decision. Clamped to a 540 s ceiling — under Claude Code's 600 s hook cap |
| `holds.fail_policy` | `AGENTGUARD_HOLDS_FAIL_POLICY` | `deny` | What to return on hold timeout: `deny` (block) or `allow` (proceed) |
| `holds.disabled` | `AGENTGUARD_HOLDS_DISABLED` | `false` | Master kill-switch — bypass all decision logic, always `allow` |

### Plugin side (per agent)

| Env var | Purpose |
|---|---|
| `AGENTGUARD_URL` | Where to POST the `/api/holds` request (e.g. `http://localhost:8080`) |
| `AGENTGUARD_HOLDS_ENABLED` | Per-plugin opt-in. All plugins now default to `false` (the app's data pipeline is observability-first). Set to `true` to opt in. |
| `AGENTGUARD_HOLDS_FAIL_POLICY` | What the plugin returns to the agent if the `/api/holds` call itself fails (network error, 5xx) — `deny` or `allow` |

When `AGENTGUARD_HOLDS_ENABLED=false`, the Claude Code plugin still fires `PreToolUse` synchronously (the hook entry has no `async:true`), but short-circuits inside the plugin: it writes a permissive `permissionDecision` to stdout and falls through to the regular async `send()` so the event still lands in `agentguard_hook_events` for observation. **No `/api/holds` call is made.** This keeps observability intact while removing all blocking latency.

### Why two `fail_policy` knobs

There are two distinct failure modes:

1. **Plugin → server unreachable** (network error before AgentGuard sees the request) → handled by **plugin-side** `holdFailPolicy`.
2. **Server long-poll times out** (no human decision within `timeoutSeconds`) → handled by **server-side** `holds.fail_policy`.

The two are conceptually independent and can be set differently. In practice, deployments tend to set both to `deny` (fail-closed) or both to `allow` (observation-friendly).

---

## 7. Failure Modes

| Failure | Behavior |
|---|---|
| AgentGuard server unreachable | Plugin's fetch fails → `holdFailPolicy`: `deny` returns block to agent, `allow` returns allow |
| `TIMEPLUS_USER` unset on AgentGuard | `/api/holds` returns HTTP 503 with actionable message; plugin treats as unreachable → `holdFailPolicy` |
| Timeplus query fails (ingest, threats query, MV poll) | Handler returns HTTP 503; plugin → `holdFailPolicy` |
| MV-wait window expires before any rule matches | `Decide` sees no matches → `allow` (no false positives from timeouts) |
| Human never decides (long-poll timeout) | `select` falls through `time.After`; backend writes `status='timeout'` row, returns server `holds.fail_policy` |
| Backend crashes mid-hold | In-memory channel lost. On restart, `AbandonStalePendingHolds` writes `status='abandoned'` for any `pending` rows → UI no longer shows them. The plugin's HTTP connection drops and AbortController fires → plugin → `holdFailPolicy` |
| Session ends with pending holds | Plugin's `SessionEnd` hook fire-and-forgets `POST /api/holds/_abandon-session/:session_id`; backend writes `status='abandoned'` for those holds and signals waiting goroutines so the agent (if still listening) gets a quick decision via fail-policy |
| Operator wants to clear all pending holds | `POST /api/holds/_drain` (authenticated): writes `status='abandoned'` for every pending hold and signals all waiting goroutines |
| Same hold decided twice (race) | `DecideHold` checks the latest-state subquery first; if status is non-`pending` it returns `{"already_decided_as": ...}` without writing a new row. Channel buffer (size 1) drops second `Signal` |

---

## 8. API Surface

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `POST` | `/api/holds` | Server creds (TIMEPLUS_USER) | Plugin → server. Synchronous: returns `{decision, reason, hold_id}` once decided. Long-polls up to `timeoutSeconds`. |
| `POST` | `/api/holds/_abandon-session/:session_id` | Server creds | Plugin SessionEnd → server. Fire-and-forget cleanup of session's pending holds. |
| `GET`  | `/api/holds` | Session cookie | UI list. Supports `?status=pending&limit=100`. |
| `GET`  | `/api/holds/:hold_id` | Session cookie | UI detail + full timeline (raw stream history). |
| `POST` | `/api/holds/:hold_id/decide` | Session cookie | UI Approve/Deny. Body: `{decision: approved|denied, decided_by, decided_reason}`. Idempotent. |
| `POST` | `/api/holds/_drain` | Session cookie | Operator: abandon all pending holds. |

The split between unauthenticated server-creds endpoints (`/api/holds`, `_abandon-session`) and cookie-protected operator endpoints (`/decide`, `/_drain`, list, detail) mirrors how alert webhooks work elsewhere in the system. See [`docs/design.md`](design.md) for the broader auth model.

---

## 9. Operational Notes

### Latency budget

Every enabled `PreToolUse` adds at minimum:

```
network RTT (plugin → server)              ~1-5 ms
ingest INSERT                              ~5-20 ms
queryPreExistingOpenThreats                ~10-30 ms
waitForNewMVDetections (1 poll, no match)  ~50 ms
network RTT (server → plugin)              ~1-5 ms
                                          ─────────
Best case (no rule fires)                  ~70-110 ms
Worst case (full mv_wait_ms)               ~500 ms + I/O
hold (human-decided)                        seconds to minutes
```

Most observability deployments are not willing to pay this on every tool call. That is why `AGENTGUARD_HOLDS_ENABLED` defaults to `false` across all plugins — the AgentGuard app's data pipeline is observability-first. Set the env var (or plugin config flag) to `true` to opt in to synchronous tool-call blocking.

### Tuning `mv_wait_ms`

- **Too low** (< 100 ms): rules that need a few stream-engine ticks to fire will miss the window — they still record threats, but the hold won't catch them on first match.
- **Too high** (> 1000 ms): adds latency to the *negative* path (no rule fires) without much benefit, since most rules that fire do so within ~100 ms of ingest.
- The default 500 ms is a compromise that catches single-event rules reliably and most cumulative rules on the second matching event.

### Restart safety

Pending holds are persisted (`agentguard_holds` is a Timeplus stream backed by disk). The in-memory `holdRegistry` is **not** persisted — on restart, all in-flight long-polls are dead. The plugin's HTTP connection drops when the backend restarts; `AbortController` fires and `holdFailPolicy` decides.

> **Known limitation (no migrations before 1.0):** rows that were `pending` at the moment of restart remain `pending` in `agentguard_holds` because there is no startup cleanup. The frontend will re-display them as pending toasts after reconnect even though the originating agent has long since given up. The fix is a clean reprovision (drop the `agentguard_holds` stream) — there is intentionally no automatic recovery path. A future release may filter pending holds by `created_at >= now() - timeout_seconds` on the read path.

A new hold for a re-tried tool call after restart starts fresh, gets a new `hold_id`, and goes through the normal flow.

### Schema lifecycle

The `agentguard_holds` stream and the `agentguard_rules.block_policy` column are part of the regular setup-wizard provisioning (`tp.Init()`). There is no in-place schema migration — schema changes between releases require a clean reprovision. This is a deliberate pre-1.0 policy: drop your data volume / `config.yaml` and re-run setup when upgrading to a release with schema changes.

---

For configuration walkthroughs, see [`INSTALL.md` Section 6 (Claude Code)](../INSTALL.md#6-connect-claude-code) and [Section 8 (Configure rule blocking)](../INSTALL.md#8-configure-rule-blocking-optional). For the broader rule + MV model holds builds on, see [`docs/design.md`](design.md).
