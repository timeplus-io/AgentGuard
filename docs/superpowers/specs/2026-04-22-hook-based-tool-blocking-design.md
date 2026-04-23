# Hook-Based Tool-Call Blocking — Design

**Status:** Approved (2026-04-22). Awaiting implementation plan.
**Branch:** `feature/52-support-tool-call-blocking`
**Prior work:** [`docs/research/hook-blocking.md`](../../research/hook-blocking.md) — cross-agent investigation.

---

## Goal

Let AgentGuard pause an AI coding agent's tool call mid-flight, surface it to a human as a "Pending Hold" on the threat detail page, and resume the agent based on the human's Approve / Deny decision. Configurable per rule; first iteration covers Claude Code only.

---

## Architecture

Synchronous hold-and-wait, MV-driven. Claude Code's `PreToolUse` hook is configured as a synchronous `command`-type hook with a long timeout (`600000` ms = 600s) pointing at AgentGuard's `agentguard-hook` binary. The binary POSTs to `/api/holds` and does not return until the backend has a decision — pausing the agent's hot path.

The decision is driven by the existing rule pipeline. When the plugin POSTs `/api/holds`, the backend:

1. **Ingests** the just-emitted hook event into `agentguard_hook_events` so MVs can see it.
2. **Checks pre-existing open threats** for this `(deployment_id, agent_id, session_id)` whose `status = 'open'`.
3. **Waits up to 500ms** (configurable) for any rule MV with `block_policy != 'log_only'` to write a new row to `agentguard_security_events` with `detected_at >= event_time`. The timestamp anchor proves the detection came from *this* hook call.
4. **Resolves** based on the strictest matching rule's `block_policy` (priority `hold > auto_block > log_only`).

Long-poll only kicks in when `block_policy = 'hold'`. Otherwise the response returns within ~500ms with `allow` or `block`. For `hold`, the HTTP response stays open up to 300s waiting for a human decision via the threat detail page.

**Why this works:** the agent loop is paused exactly once per risky call, the human gets full threat context (rule, message, args, session history), and there is zero token burn during the wait because no LLM call can fire while the tool hook is still pending.

---

## Per-Rule Policy Field

One new field on `agentguard_rules`: `block_policy string DEFAULT 'log_only'`.

| Mode | When the rule fires |
|---|---|
| `hold` | Open hold; long-poll plugin response up to 300s waiting for human Approve / Deny on threat detail page. Timeout → fail-policy. |
| `auto_block` | Return `block` immediately to plugin with the rule's reason. Threat still appears in feed for after-the-fact review. |
| `log_only` | Security event written as today; plugin gets `allow` (current behavior — surface threat, don't block). |

`enabled: false` (existing knob) silences a rule entirely — no detections, no events, no UI rows. Use this for shadow-mode validation; once confident, enable with `block_policy: log_only`, then promote to `auto_block` or `hold`.

**Migration:** new field defaults to `"log_only"` for all existing rules so behavior is unchanged after the upgrade.

**Multiple matching rules:** strictest policy wins (`hold > auto_block > log_only`). Fail-closed.

---

## Configuration Knobs

| Knob | Env var | Default | Notes |
|---|---|---|---|
| MV-settle wait window | `AGENTGUARD_HOLD_MV_WAIT_MS` | `500` | How long the backend waits in Step 3 for a new security event. Range: 100–5000 ms. |
| Hold timeout | `AGENTGUARD_HOLD_TIMEOUT_SECONDS` | `300` | Long-poll cap waiting for human Approve / Deny. Hard ceiling 540s (must stay under Claude Code's 600s HTTP timeout). |
| Plugin fail-policy | `AGENTGUARD_HOLD_FAIL_POLICY` | `deny` | Backend-unreachable behavior on plugin side. `deny` or `allow`. |
| Master kill-switch | `AGENTGUARD_HOLDS_DISABLED` | `false` | Backend skips Steps 2–5 entirely; every hold POST returns `allow` immediately. Incident-response escape hatch. |

The Step-3 inner-poll cadence (50ms) stays hard-coded — implementation detail, not user-facing.

---

## Data Flow

### Backend — `POST /api/holds` handler

```
Plugin (Claude Code PreToolUse, command-type hook, timeout: 600000ms)
   │
   │  POST /api/holds
   │  { deployment_id, agent_id, session_id, run_id, tool_name,
   │    tool_input, event_time = T0, hook_payload }
   ▼
┌──────────────────────────────────────────────────────────────────┐
│ Backend handler:                                                 │
│                                                                  │
│  Step 1. Ingest hook_payload into agentguard_hook_events         │
│          (so MVs can see this event)                             │
│                                                                  │
│  Step 2. Pre-existing open threats check:                        │
│          SELECT s.rule_id, r.block_policy                        │
│          FROM agentguard_security_events s                       │
│          JOIN agentguard_rules (current via arg_max) r           │
│          JOIN agentguard_threats (current via arg_max) t         │
│          WHERE s.session_id = $1 AND s.agent_id = $2             │
│            AND t.status = 'open'                                 │
│            AND r.block_policy IN ('hold','auto_block')           │
│          LIMIT 1                                                 │
│          → if hit, skip to Step 4                                │
│                                                                  │
│  Step 3. Wait for new MV detection (up to AGENTGUARD_HOLD_MV_WAIT_MS):│
│          Poll agentguard_security_events every ~50ms for rows    │
│          where session_id matches AND detected_at >= T0          │
│          AND rule.block_policy IN ('hold','auto_block')          │
│          → if a row appears, proceed to Step 4                   │
│          → if window elapses with no hit, return {decision:"allow"}│
│                                                                  │
│  Step 4. Pick strictest matching rule (hold > auto_block):       │
│          • auto_block → return {decision:"block", reason}         │
│          • hold       → proceed to Step 5                        │
│                                                                  │
│  Step 5. Open hold record, register channel, long-poll:          │
│          INSERT INTO agentguard_holds                            │
│            (hold_id, session_id, agent_id, run_id, tool_name,    │
│             rule_id, severity, args_summary, created_at,         │
│             status='pending')                                    │
│          Publish SSE: hold_pending → frontend updates badge      │
│          select {                                                │
│            case d := <- holdChannel[hold_id]: return d.decision  │
│            case <- time.After(AGENTGUARD_HOLD_TIMEOUT_SECONDS):  │
│              INSERT … status='timeout'                           │
│              return fail-policy (default: block)                 │
│          }                                                       │
└──────────────────────────────────────────────────────────────────┘
   │
   ▼
Plugin receives { decision, reason } and writes Claude Code's
permissionDecision response to stdout.
```

### Backend — `POST /api/holds/:hold_id/decide` handler (human side)

```
Threat detail page (frontend)
   │
   │  POST /api/holds/:hold_id/decide
   │  { decision: "approved" | "denied", decided_by, decided_reason? }
   ▼
┌──────────────────────────────────────────────────────────────────┐
│ Backend handler:                                                 │
│  1. INSERT INTO agentguard_holds                                 │
│       (hold_id, ..., status='approved'|'denied',                 │
│        decided_by, decided_at, decided_reason)                   │
│  2. Look up holdChannel[hold_id], send decision into it          │
│     (wakes the long-poll goroutine in Step 5 above)              │
│  3. Return { ok: true }. Idempotent: if already terminal,        │
│     return existing decision without writing a new row.          │
└──────────────────────────────────────────────────────────────────┘
```

### Key design choices

1. **In-memory channel** wakes the long-poll instead of polling the holds stream — sub-millisecond response when the human clicks. The stream is the durable record (audit trail). Tradeoff: single backend instance — if process restarts mid-hold, in-flight long-polls die and the agent's HTTP timeout takes over (fail-policy applies). Acceptable for v1; documented as a limitation.

2. **Step 3 polling at ~50ms** is a coarse loop, not strict block. Cheap enough at our scale.

3. **Pre-existing open threats (Step 2)** only auto-trigger when the threat is `status = 'open'`. Once a human acknowledges or clears a threat, subsequent calls in the same session are no longer auto-blocked by it. If the underlying behavior recurs, the MV writes a new event, which is detected via Step 3.

4. **`args_summary`** in the hold record is a truncated JSON (~2KB) of `tool_input` — enough for the human to decide, not the full payload. Full payload lives in `agentguard_hook_events`.

---

## Storage

### New stream — `agentguard_holds`

```sql
CREATE STREAM IF NOT EXISTS agentguard_holds (
  hold_id          string,         -- UUID, stable across status updates
  deployment_id    string,
  agent_id         string,
  session_id       string,
  run_id           string,
  tool_name        string,
  rule_id          string,         -- the rule whose block_policy triggered the hold
  severity         string,         -- copied from rule for fast UI display
  args_summary     string,         -- truncated JSON of tool_input (~2KB cap)
  message          string,         -- the rule's detection message
  created_at       datetime64(3),
  status           string,         -- 'pending' | 'approved' | 'denied' | 'timeout' | 'abandoned'
  decided_by       string,         -- empty until decided
  decided_at       datetime64(3),  -- zero until decided
  decided_reason   string,         -- optional human note
  updated_at       datetime64(3)
)
```

Current state for a single hold: `arg_max(status, updated_at) GROUP BY hold_id` — same pattern as `agentguard_rules` and `agentguard_threats`.

### Extension to `agentguard_rules`

```sql
ALTER STREAM agentguard_rules ADD COLUMN IF NOT EXISTS block_policy string DEFAULT 'log_only'
```

The `sqlCurrentRules` query in `rules.go` gains `arg_max(block_policy, updated_at) AS block_policy`. The catalog JSON gains the same field. All rule mutation handlers (`InstallRule`, `ToggleRule`, `UpgradeRule`, `CreateRule`) carry it through.

### Materialized view — `mv_holds_current`

```sql
CREATE MATERIALIZED VIEW mv_holds_current AS
SELECT
  hold_id,
  arg_max(deployment_id, updated_at) AS deployment_id,
  arg_max(agent_id,      updated_at) AS agent_id,
  arg_max(session_id,    updated_at) AS session_id,
  arg_max(tool_name,     updated_at) AS tool_name,
  arg_max(rule_id,       updated_at) AS rule_id,
  arg_max(severity,      updated_at) AS severity,
  arg_max(args_summary,  updated_at) AS args_summary,
  arg_max(message,       updated_at) AS message,
  arg_max(created_at,    updated_at) AS created_at,
  arg_max(status,        updated_at) AS status,
  arg_max(decided_by,    updated_at) AS decided_by,
  arg_max(decided_at,    updated_at) AS decided_at
FROM agentguard_holds
GROUP BY hold_id
```

Pending-holds badge query: `SELECT count() FROM mv_holds_current WHERE status='pending'`.

`agentguard_security_events` is unchanged — the existing rule detection writes there unmodified. Holds are a separate concern keyed by `hold_id`, not `(rule_id, agent_id, session_id)`.

---

## Backend Endpoints

### New endpoints

| Method | Path | Caller | Purpose |
|---|---|---|---|
| `POST` | `/api/holds` | Plugin (long-poll, up to 540s) | Synchronous hold endpoint. Body: `{deployment_id, agent_id, session_id, run_id, tool_name, tool_input, event_time, hook_payload}`. Returns `{decision: "allow"\|"block", reason?: string, hold_id?: string}` once decided. |
| `POST` | `/api/holds/:hold_id/decide` | Frontend | Body: `{decision: "approved"\|"denied", decided_by, decided_reason?}`. Writes the decision row, signals the in-memory channel, returns `{ok: true}`. Idempotent: if hold already decided, returns existing decision. |
| `GET` | `/api/holds` | Frontend | Query params: `?status=pending` (default), `?limit=N`. Returns current state from `mv_holds_current`. |
| `GET` | `/api/holds/:hold_id` | Frontend | Single hold's full state including timeline (all status transitions). |
| `POST` | `/api/holds/_abandon-session/:session_id` | Plugin (`SessionEnd` hook, fire-and-forget) | Backend marks all `pending` holds for the session as `abandoned` and signals their channels with the fail-policy decision. Avoids zombie holds when user kills Claude Code. |
| `POST` | `/api/holds/_drain` | Operator (admin) | Mass-resolves all `pending` holds with `status='abandoned'` and signals each channel with the fail-policy decision. Companion to the kill-switch for incident response. |

### Extended endpoints

| Endpoint | Change |
|---|---|
| `GET /api/threats/:id` | Response gains `pending_hold` field — populated when there's an open hold whose `(deployment_id, agent_id, session_id, rule_id)` matches the threat. The threat detail page uses this to render Approve/Deny buttons. |
| `GET /api/notifications/stream` (SSE) | Adds two new event types: `hold_pending` (a hold opened — payload: hold_id, severity, agent, tool, message) and `hold_decided` (a hold resolved — payload: hold_id, decision). Existing `alert` events unchanged. |
| `GET /api/rules` and `GET /api/rules/:id` | Response gains `block_policy` field. |
| `GET /api/rules/catalog` | Each catalog item gains `block_policy`. |
| `POST /api/rules` (`CreateRule`) | Body accepts optional `block_policy` (defaults to `"log_only"`). |
| `POST /api/rules/:id/install` | Accepts `block_policy` override; defaults to catalog value or `"log_only"`. |
| `POST /api/rules/:id/policy` (NEW) | Updates `block_policy` only. Body: `{block_policy: "hold"\|"auto_block"\|"log_only"}`. Writes a new mutation row to `agentguard_rules` like `ToggleRule` does for `enabled`. Follows the existing `POST /api/rules/:id/<action>` pattern. |

### Backend file layout

| File | Status | Responsibility |
|---|---|---|
| `internal/handlers/holds.go` | NEW | All `/api/holds*` handlers |
| `internal/handlers/holds_test.go` | NEW | Decision-pipeline tests (auto-allow, auto-block, hold + decide, hold + timeout) |
| `internal/holdregistry/registry.go` | NEW | In-memory channel registry: `Register(hold_id) <-chan Decision`, `Signal(hold_id, decision)`, `Cancel(hold_id)`. Goroutine-safe. |
| `internal/holdregistry/registry_test.go` | NEW | Race / double-signal / late-signal coverage |
| `internal/timeplus/holds.go` | NEW | DDL for `agentguard_holds` stream + `mv_holds_current` MV; called from `Init()`. Also writes `status='abandoned'` for pre-startup `pending` rows. |
| `internal/timeplus/init.go` | EXTEND | Add `block_policy` column to `agentguard_rules` (idempotent `ALTER`); call into `holds.go` setup |
| `internal/handlers/rules.go` | EXTEND | `block_policy` field in all rule queries and mutations |
| `internal/handlers/notifications.go` | EXTEND | Add `hold_pending` / `hold_decided` SSE event types |
| `internal/handlers/threats.go` | EXTEND | Populate `pending_hold` field in detail response |
| `internal/config/config.go` | EXTEND | Read four new env vars (MV wait, hold timeout, fail-policy, kill-switch) |
| `cmd/server/main.go` | EXTEND | Register the new routes; wire config knobs |

### Concurrency model — `holdregistry`

One global `sync.Map` keyed by `hold_id`, value is a buffered `chan Decision` (buffer size 1 so signal never blocks). On `/decide` call: `Load(hold_id)` → `ch <- decision`. On long-poll wait: `Register` adds the channel to the map and returns it; `defer Cancel(hold_id)` removes it after the wait completes (whether decided or timed out).

---

## Plugin Changes (Claude Code only)

### `~/.claude/settings.json` — `PreToolUse` becomes synchronous

```json
"PreToolUse": [{
  "matcher": "*",
  "hooks": [{
    "type": "command",
    "command": "agentguard-hook",
    "timeout": 600000
  }]
}],
```

`async: true` removed; `timeout: 600000` ms = 600s, matching the backend's hold timeout ceiling with margin. All other hook entries unchanged (still async).

### `agentguard-plugin/src/index.ts` — branch on `PreToolUse`

```typescript
if (hookEventName === "PreToolUse") {
  // Synchronous path: long-poll the backend, write Claude Code's
  // permissionDecision response to stdout. The hook does not return
  // until the backend responds (allow/block) or fail-policy fires.
  const decision = await requestHold(event, config);
  process.stdout.write(JSON.stringify({
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: decision.decision === "block" ? "deny" : "allow",
      permissionDecisionReason: decision.reason ?? "",
    },
  }));
  return;  // do NOT also call send() — /api/holds ingested the event
}

// Async path (all other hooks): existing send(event, config) flow
```

### New file — `agentguard-plugin/src/holds.ts`

```typescript
export type HoldDecision = { decision: "allow" | "block"; reason?: string };

export async function requestHold(event: HookEvent, config: AgentGuardConfig): Promise<HoldDecision> {
  const url = `${config.agentguardUrl}/api/holds`;
  const body = JSON.stringify({
    deployment_id: event.deployment_id,
    agent_id:      event.agent_id,
    session_id:    event.session_id,
    run_id:        event.run_id,
    tool_name:     event.tool_name,
    event_time:    event.event_time,
    hook_payload:  JSON.parse(event.event_data),
  });
  try {
    const ctrl = new AbortController();
    const t = setTimeout(() => ctrl.abort(), 540_000);  // under Claude Code's 600s
    const resp = await fetch(url, {
      method:  "POST",
      headers: { "Content-Type": "application/json" },
      body,
      signal:  ctrl.signal,
    });
    clearTimeout(t);
    if (!resp.ok) {
      return failPolicy(config, `backend returned ${resp.status}`);
    }
    return await resp.json() as HoldDecision;
  } catch (err) {
    return failPolicy(config, `request failed: ${String(err)}`);
  }
}

function failPolicy(config: AgentGuardConfig, reason: string): HoldDecision {
  return config.holdFailPolicy === "deny"
    ? { decision: "block", reason: `AgentGuard unreachable — ${reason}` }
    : { decision: "allow" };
}
```

### Plugin config additions in `agentguard-plugin/src/config.ts`

| Field | Env var | Default |
|---|---|---|
| `agentguardUrl` | `AGENTGUARD_URL` | `http://localhost:8080` |
| `holdFailPolicy` | `AGENTGUARD_HOLD_FAIL_POLICY` | `deny` |

### `SessionEnd` handler — best-effort abandon

When `SessionEnd` fires, the plugin issues a fire-and-forget `POST /api/holds/_abandon-session/{session_id}` to clean up zombie holds. Failure is silently swallowed (existing async path).

### Install script

`agents/claudecode/Makefile` `make install` patches the user's `~/.claude/settings.json` to set the new `PreToolUse` shape (timeout, no async). Existing merge logic continues to work; we just emit the new shape.

---

## Frontend Changes

### New page — `/threats/:id` (Threat Detail)

Currently doesn't exist; `/threats` only shows a flat list. The new page consolidates a single threat's full context plus the Approve/Deny UI for any related hold.

| Section | Source |
|---|---|
| Header: rule name, severity badge, agent, session, first-seen / last-seen | `GET /api/threats/:id` |
| **Pending Hold** banner (only when `pending_hold` is non-null) — tool name, args summary, message, two large buttons: **Approve** / **Deny**, optional reason field | `pending_hold` field in threat response |
| Raw security events list | events array in threat response |
| Status controls: existing Acknowledge / Clear buttons | `POST /api/threats/:id/{acknowledge,clear}` |

Clicking Approve or Deny POSTs `/api/holds/:hold_id/decide`, shows inline confirmation ("Approved by you at 14:32 — agent resumed"), then re-fetches the threat detail; the Pending Hold banner disappears.

### Threats list page (`/threats`) — Pending Holds section

A new section at the top of the page, above the existing threat feed:

```
┌─────────────────────────────────────────────────────────────────┐
│ ⚠ Pending Holds (3)                                             │
├─────────────────────────────────────────────────────────────────┤
│ ⚪ critical · my-agent · Bash · "rm -rf /tmp/cache"             │
│   Rule: Destructive shell command   ⏱ 2m ago    [View →]       │
│ ⚪ warning · my-agent · WebFetch · ...                          │
│   ...                                                           │
└─────────────────────────────────────────────────────────────────┘
```

Each row links to `/threats/:id`. Auto-refreshes via the new SSE event types — `hold_pending` adds rows, `hold_decided` removes them. Section hidden when count is zero.

### Sidebar — pending holds badge

The Threats sidebar entry gains a small count badge (only when > 0). Driven by the same SSE stream so it updates without polling.

### Toast notification on `hold_pending`

`NotificationsProvider` listens to `/api/notifications/stream`. Extended to handle:

- `hold_pending` → show a toast: severity icon + "Hold pending: `Bash` on `my-agent`" + a "Review" button that navigates to `/threats/:id`. Toast persists until the hold is decided or dismissed.
- `hold_decided` → if a `hold_pending` toast for the same `hold_id` is on screen, replace with a brief "Approved" / "Denied" confirmation, auto-dismiss after 3s.
- SSE reconnect → re-fetch `GET /api/holds?status=pending` and reconcile toast state with server truth.

### Rules page (`/rules`) — per-rule block_policy dropdown

For each rule row, alongside the existing enable toggle:

```
[Enabled ✓]    Policy: [ Hold ▼ ]    [Edit] [Uninstall]
                       ├ Hold (require human approval)
                       ├ Auto-block (deny without prompting)
                       └ Log only (current default)
```

Dropdown changes POST to `POST /api/rules/:id/policy` with `{block_policy: "hold"}`. Optimistically update the row, roll back on error.

### Rule detail page (`/rules/:id`)

Same dropdown shown more prominently with helper text explaining each mode.

### Onboarding wizard (`/onboarding`)

Step 4 (Activate rules) currently only toggles `enabled`. Add a per-rule policy selector defaulting to `log_only` so first-time users don't accidentally turn on blocking before they're ready.

### API client (`frontend/src/api/client.js`)

Five new functions:

```javascript
api.listHolds(status = 'pending')
api.getHold(holdId)
api.decideHold(holdId, decision, reason)       // 'approved' | 'denied'
api.setRuleBlockPolicy(ruleId, blockPolicy)    // wraps POST /api/rules/:id/policy
api.getThreatDetail(threatId)                  // wraps GET /api/threats/:id
```

### Frontend file map

| File | Status | Responsibility |
|---|---|---|
| `frontend/src/pages/ThreatDetail.jsx` | NEW | Detail page including pending-hold banner |
| `frontend/src/pages/Threats.jsx` | EXTEND | Pending Holds section at top |
| `frontend/src/pages/RulePacks.jsx` | EXTEND | Per-rule block_policy dropdown |
| `frontend/src/pages/RuleDetail.jsx` | EXTEND | Same dropdown, more prominent |
| `frontend/src/pages/Onboarding.jsx` | EXTEND | Per-rule policy selector in Step 4 |
| `frontend/src/components/Sidebar.jsx` | EXTEND | Pending-holds badge on Threats entry |
| `frontend/src/components/NotificationsProvider.jsx` | EXTEND | Handle `hold_pending` / `hold_decided` event types |
| `frontend/src/components/HoldToast.jsx` | NEW | Toast component for pending-hold notifications |
| `frontend/src/api/client.js` | EXTEND | Five new API functions |
| `frontend/src/App.jsx` | EXTEND | Register `/threats/:id` route |

---

## Failure Modes & Error Handling

| Scenario | Behavior | Notes |
|---|---|---|
| Plugin can't reach backend | Returns fail-policy (default `deny`) | Configurable per-deployment via `AGENTGUARD_HOLD_FAIL_POLICY`. Telemetry-ingest path stays fail-open even when hold path is fail-closed. |
| Backend reaches MV-settle window with no detection | Returns `allow` immediately | Normal path for safe tool calls. |
| Backend hold times out (300s default, no human) | Writes `status='timeout'` row, returns fail-policy | Surfaces in audit log; threat detail page shows the timed-out outcome. |
| Backend process crashes mid-hold | All in-flight long-polls die — plugin's HTTP timeout (600s) fires next, applies plugin fail-policy | Pre-startup `pending` rows in `agentguard_holds` get `status='abandoned'` written by an `Init()` task so the UI doesn't show stale entries. |
| User decides simultaneously with timeout | `/decide` handler is idempotent: if hold is already terminal, returns existing decision without writing a new row | In-memory channel signal is non-blocking (buffered 1) so a late `Signal` after `Cancel` is dropped without panic. |
| Master kill-switch flipped on while holds in flight | New hold POSTs return `allow` immediately. In-flight long-polls drain naturally on their own timeout | Operator script can call `POST /api/holds/_drain` (admin) to mass-resolve all pending holds with `status='abandoned'` if needed. |
| Session ends while a hold is pending | `SessionEnd` hook fire-and-forgets `POST /api/holds/_abandon-session/{session_id}` — backend marks all `pending` holds for the session as `abandoned` and signals their channels with fail-policy decision | Avoids zombie holds when user kills Claude Code. |
| `/api/holds` ingest fails (Step 1) | Return HTTP 503 to plugin → plugin applies fail-policy | Hook event ingest is in the synchronous path, so an ingest failure must surface, not be swallowed. Consistent with the rules-handler error-handling overhaul. |
| Hold record DDL fails on startup | Server logs error and continues without holds support; `/api/holds` returns HTTP 503 | Same fail-open pattern as existing `Init()` code. |
| Same `(session_id, run_id, tool_name)` triggers two MV detections within wait window | Pick the strictest `block_policy`; record one hold linked to one of the matching rules in `agentguard_holds.rule_id` (the strictest one) | Other detections still in `agentguard_security_events`; only one hold opened (one human decision covers the call). |
| SSE drops while a `hold_pending` toast is showing | Toast persists; reconnect logic in `NotificationsProvider` re-fetches `GET /api/holds?status=pending` on reconnect and reconciles | Existing reconnect logic already there for alerts; we just call the holds endpoint too. |

**Logging:** every hold lifecycle event is logged at INFO with `hold_id`, `session_id`, `agent_id`, `tool_name`, `decision`. Failures (channel signal lost, DDL errors, ingest errors) logged at ERROR with full context. No silent failures.

---

## Testing Strategy

| Layer | What we test | How |
|---|---|---|
| **`internal/holdregistry`** | `Register/Signal/Cancel` race conditions, double-signal safety, late-signal-after-cancel | Pure Go unit tests with `t.Parallel`, `go test -race` |
| **`internal/handlers/holds.go`** | Each branch of the decision tree: existing-open-threat hit, MV-settle hit (auto_block / hold), MV-settle miss (allow), kill-switch on, `_abandon-session`, `_drain` | Table-driven tests using a fake `tp.Query` / `tp.ExecHTTP`, no real Timeplus |
| **`internal/handlers/holds_test.go` — long-poll integration** | End-to-end with a real Timeplus instance: POST `/api/holds` from one goroutine, simulate human deciding via `/decide` from another, verify the first goroutine returns the right decision | Gated behind `-tags integration` |
| **`internal/handlers/rules.go`** | `block_policy` round-trip on every rule mutation handler, strictness ordering when multiple rules fire | Extend existing `*_test.go` files |
| **`internal/timeplus/holds.go`** | DDL idempotency: running `Init()` twice doesn't error; `ALTER STREAM ... ADD COLUMN IF NOT EXISTS` is idempotent | Integration test against a real Timeplus instance |
| **Claude Code plugin — `holds.ts`** | `requestHold` returns expected decision for HTTP 200 allow / 200 block / 503 / network error / abort timeout. `failPolicy` honours both env-var values | `vitest` with `fetch` mocked |
| **Claude Code plugin — `index.ts`** | `PreToolUse` writes the right Claude Code response shape; does NOT call `send()` for PreToolUse | `vitest` capturing `process.stdout.write` |
| **Frontend — `ThreatDetail.jsx`** | Pending-hold banner renders only when `pending_hold` is non-null; clicking Approve calls `api.decideHold`; loading and error states render | `vitest` + happy-path |
| **Frontend — `NotificationsProvider`** | `hold_pending` event adds a toast; matching `hold_decided` replaces it; SSE reconnect fetches `listHolds('pending')` to reconcile | `vitest` with mocked `EventSource` |

### End-to-end manual test plan

1. Install plugin, set up a rule with `block_policy: "hold"`, run a Claude Code session that triggers it.
2. Verify Claude Code visibly pauses on the tool call.
3. Verify a `hold_pending` toast appears within 1s.
4. Verify the Threats page shows a Pending Holds entry.
5. Click into the threat detail page; verify Approve and Deny buttons render with the rule message.
6. Click Approve → Claude Code resumes within ~1s, the tool runs, the toast updates to "Approved".
7. Repeat with Deny → Claude Code receives the block and the agent loop continues with the deny reason fed back to the model.
8. Repeat with the human ignoring the hold → after 300s, fail-policy kicks in, hold marked `timeout` in the audit log.
9. Set `AGENTGUARD_HOLDS_DISABLED=true`, restart backend, verify all PreToolUse calls go straight through.

**Unit test policy:** every new file ships with tests in the same PR (TDD per project superpowers guidance). Integration tests are split out with a build tag so the default `go test ./...` stays fast.

---

## Out of Scope (Deferred)

- **OpenClaw plugin** — `before_tool_call` returning `{requireApproval: ...}` long-polling AgentGuard. Backend changes are zero (same `/api/holds` endpoint); only OpenClaw plugin code + a backend gateway-side `plugin.approval.request`/`plugin.approval.waitDecision` adapter is needed. Separate iteration after Claude Code is validated end-to-end.
- **Hermes plugin** — `pre_tool_call` Python callback long-polling AgentGuard via `httpx`. Tricky because Hermes provides no hook timeout; the plugin must self-impose `httpx.post(..., timeout=540)`. Lowest-priority because of small user base. Separate iteration.
- **Approve-with-edit** — letting the human modify `tool_input` before approving. Claude Code's `permissionDecision: "allow"` doesn't natively carry a mutated input. Skip for v1; add via Claude Code's `hookSpecificOutput.modifyToolInput` (if/when supported) or via OpenClaw's `requireApproval` `params` field when that plugin lands.
- **Per-tool override** — extending `block_policy` from per-rule to per-(rule × tool_name). Not needed for v1: rule SQL can already discriminate by `tool_name` and `event_data`, so a tighter rule expression covers the same use case.
- **Hold-engaging heartbeat** — a "human is reviewing this hold" signal that extends the 300s timeout while the threat detail page is open. 300s default is comfortable for most decisions.
- **Multi-instance backend** — the in-memory `holdregistry` channel doesn't survive a backend restart and doesn't span multiple AgentGuard instances behind a load balancer. Documented as a v1 limitation. Multi-instance support would require sticky sessions or a Redis pub/sub layer; defer until needed.
- **Mid-turn LLM-call gate** — confirmed unavailable in any of the three agents per the research doc. Would require a network-layer proxy (LiteLLM) instead of hooks. Out of scope; could be a separate "AgentGuard Proxy" project later.
