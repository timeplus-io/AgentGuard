# Streaming Threat Notifications — Design

**Date:** 2026-07-30
**Issue:** [#258](https://github.com/timeplus-io/AgentGuard/issues/258) — Replace Timeplus→AgentGuard alert callback with an AgentGuard-side streaming query + persisted notification state

## Problem

Today a new-threat notification travels Timeplus → AgentGuard via a `CREATE ALERT`
(`agentguard_notifier`) that calls a Python UDF (`notify_agentguard_udf`) which
HTTP-POSTs back into the server (`POST /api/alerts/webhook`). This inverts the
dependency direction (Timeplus must be able to route to the app server), requires a
Python-capable UDF runtime with outbound HTTP, adds four Timeplus objects purely for
notification transport, and fails open — silent notification loss with no signal.
Delivery state is entirely in-memory: a threat firing during an SSE reconnect, with no
browser attached, or across a server restart is lost.

## Decisions (resolved from the issue's open questions)

| # | Question | Decision |
|---|----------|----------|
| 1 | Seen-state storage | New Timeplus mutable stream `agentguard_notifications` |
| 2 | Confirmed vs acknowledged | Separate concepts — dismissing a popup does not triage the threat |
| 3 | Dedup identity | The `agentguard_threats` primary key `(agent_id, deployment_id, session_id, rule_id)`; no new occurrence id |
| 4 | Backfill on connect | Yes — unconfirmed notifications are re-delivered on browser connect |
| 5 | Loop location | Backend goroutine publishing to the existing SSE broadcaster |
| 6 | Streaming source | Keep `mv_notify_filter` + `agentguard_notify_events`; only the UDF and ALERT are removed |
| 7 | Rate limiting | None — seen-state dedup bounds delivery to once per threat identity; the frontend's max-5 toast cap handles bursts; the old `LIMIT 10 ALERTS PER 1m` protected the HTTP callback path, which no longer exists |
| — | Go streaming read | HTTP `JSONEachRow` streaming (extracted from `ProtonQueryHandler`), not the proton Go driver |
| — | Auto-dismiss semantics | Auto-dismiss (6s timer) does **not** confirm; only explicit dismiss or click-through does. An unseen toast reappears via backfill on the next connect |

## Architecture

```
mv_rule_* ──▶ agentguard_security_events ──▶ mv_notify_filter ──▶ agentguard_notify_events
                                                (unchanged)               │
                                                              backend threat watcher
                                                       (streaming SELECT, reconnect/backoff)
                                                                          │
                                              ┌── dedup via agentguard_notifications (mutable) ──┐
                                                                          │
                                                            GlobalAlertBroadcaster (unchanged)
                                                                          │
                                                       GET /api/notifications/stream (SSE, unchanged)
                                                                          │
                                                        NotificationsProvider → toasts
```

Removed entirely: `notify_agentguard_udf`, `agentguard_notifier` (ALERT),
`POST /api/alerts/webhook` + `AlertWebhook` handler, `isThreatAlreadyOpen` /
`threatOpenCheckSQL`. Holds notifications (`hold_pending` / `hold_decided`) and the SSE
transport are untouched.

## Components

### 1. `timeplus.StreamLines` — reusable streaming read

Extract the streaming core of `ProtonQueryHandler` (`backend/internal/handlers/proton.go`)
into `backend/internal/timeplus`:

```go
// StreamLines POSTs sql to the Timeplus query endpoint with
// default_format=JSONEachRow and invokes onLine for each non-empty NDJSON
// line until ctx is cancelled or the stream ends/errors. onConnected fires
// once after a 200 response.
func StreamLines(ctx context.Context, cfg *config.TimeplusConfig, sql string, onConnected func(), onLine func(line []byte) error) error
```

The callback passes raw line bytes (not parsed maps) so the browser proxy
stays byte-for-byte identical — re-marshalling through `map[string]any` would
reorder keys and risk float64 precision loss on large integers. The watcher
unmarshals lines itself.

`ProtonQueryHandler` is refactored to use the same helper so there is one streaming
implementation. Auth and endpoint format are identical to today's proxy
(`http://host:port/?default_format=JSONEachRow`, `x-timeplus-*` headers).

### 2. Threat watcher — `backend/internal/notify/watcher.go`

A long-lived goroutine started from `main.go` at boot when `cfg.SetupComplete` (same
gate as `ReconcileOrphanedHolds`). Loop:

1. `StreamRows` over:
   ```sql
   SELECT detected_at, rule_id, rule_name, severity, agent_id, deployment_id,
          session_id, hook_name, message
   FROM agentguard_notify_events
   SETTINGS seek_to='-24h'
   ```
   The 24h seek window means a server restart replays recent candidates; the
   seen-state store filters out already-notified rows.
2. On stream error / EOF: log, reconnect with capped exponential backoff (start 1s,
   cap 30s), forever. Watcher failures never crash the server.
3. Per row, apply the **notify decision** (§3) and, when it says notify, publish an
   `AlertWebhookPayload`-shaped envelope to `GlobalAlertBroadcaster` with a **stable
   `id`** — the joined identity key (`deployment_id|agent_id|session_id|rule_id`) —
   instead of today's random per-publish UUID, so tabs dedup naturally. Hold events
   keep their current envelope.

The decision logic lives behind a small store interface so it is unit-testable without
Timeplus.

### 3. Seen-state — `agentguard_notifications` mutable stream

Provisioned in `timeplus.Init()` (idempotent `CREATE MUTABLE STREAM IF NOT EXISTS`):

| Column | Type | Notes |
|--------|------|-------|
| `agent_id`, `deployment_id`, `session_id`, `rule_id` | `string` | **PRIMARY KEY**, same identity as `agentguard_threats` |
| `rule_name`, `severity`, `message` | `string` | last-notified snapshot for backfill rendering |
| `detected_at` | `datetime64(3)` | detection time of the last-notified event |
| `notified_at` | `datetime64(3)` | when the watcher published it |
| `confirmed` | `bool` | user has dismissed/clicked the toast |
| `confirmed_at` | `datetime64(3)` | |

`SETTINGS coalesced = true`, mirroring `agentguard_threats`.

**Notify decision per incoming row** (rows are pre-filtered by `mv_notify_filter` to
threats that are new or previously cleared):

1. Point-query the stream on the PK (no in-memory cache to keep coherent; threat
   notification volume is low).
2. **No row** → upsert `{notified_at: now, confirmed: false}` + publish.
3. **Row exists, unconfirmed** → skip. This absorbs the burst-duplicate race where
   several security events pass `mv_notify_filter` before `mv_threats` flips the
   threat status.
4. **Row exists, confirmed, and event `detected_at > confirmed_at`** → the threat
   re-emerged after the user confirmed; upsert `{notified_at: now, confirmed: false}`
   + publish again.
5. **Row exists, confirmed, event `detected_at <= confirmed_at`** → replayed old
   event (e.g. seek window after restart); skip.

If the seen-state upsert fails, the watcher still publishes (fail-open toward
notifying, matching current behavior) and logs the error.

### 4. API endpoints — `backend/internal/handlers/notifications.go`

Both in the `protected` route group:

- `GET /api/notifications/unconfirmed` — newest-first unconfirmed notifications,
  `limit` capped at 20. Backs browser backfill.
- `POST /api/notifications/confirm` — body `{agent_id, deployment_id, session_id,
  rule_id}` → upsert `confirmed = true, confirmed_at = now()`. Idempotent.

### 5. Frontend — `NotificationsProvider.jsx` + `api/client.js`

- On SSE `open` (where `reconcileHolds()` already runs): call
  `api.getUnconfirmedNotifications()` and toast the results (existing max-5 visible
  cap applies; use the stable identity `id` as the toast key so live + backfill dedup).
- On **explicit dismiss** (✕ button) and on **click-through** to `/threats`: call
  `api.confirmNotification({...})` fire-and-forget. The 6s auto-dismiss timer does
  **not** confirm.
- A failed confirm just means the toast may reappear on the next connect.

### 6. Removal & teardown

- `backend/internal/timeplus/init.go`:
  - Delete `createNotifyUDF`, `createNotifierAlert`, `ddlNotifyUDFTemplate`,
    `ddlNotifierAlert` and their call sites.
  - Add explicit teardown to provisioning: `DROP ALERT IF EXISTS agentguard_notifier`
    then `DROP FUNCTION IF EXISTS notify_agentguard_udf` (order matters: the alert
    references the UDF). No drop logic exists today, so without this, existing
    deployments would keep calling the webhook after upgrade.
  - `mv_notify_filter` and `agentguard_notify_events` stay as-is.
- `backend/internal/handlers/rules.go`: delete `AlertWebhook`, `isThreatAlreadyOpen`,
  `threatOpenCheckSQL`; delete `TestThreatOpenCheckSQL_EscapesBackslashQuoteBypass`.
- `backend/cmd/server/main.go`: remove the `POST /api/alerts/webhook` route; start the
  watcher; register the two new routes.
- `models.go`: `AlertWebhookPayload` stays (it is the broadcaster envelope, still used
  by holds and the watcher) — consider renaming later, out of scope.
- **Config**: retire `agentguard.url` (struct field, default, env bind `AGENTGUARD_URL`
  on the *server*, flag, registry entry) **provided** plan-time verification confirms
  nothing else in the backend reads `cfg.AgentGuardURL`. The `AGENTGUARD_URL` used by
  agent plugins / install scripts / docker-compose to reach the server is a separate,
  client-side concern and stays.
- Docs: update `CLAUDE.md` (drop the `/api/alerts/webhook` row, add the two new
  endpoints and the `agentguard_notifications` stream), `docs/spec/api.md`,
  `docs/spec/openapi.yaml`, and the webhook mention in `docs/holds.md`.

## Error handling summary

| Failure | Behavior |
|---------|----------|
| Timeplus unreachable at boot / mid-stream | Watcher retries forever with capped backoff; server unaffected |
| Malformed stream row | Log and skip the row |
| Seen-state read/write fails | Publish anyway (fail-open), log |
| Confirm endpoint fails | Toast may reappear on next connect; no loss |
| SSE client buffer full | Unchanged (drop for that client); backfill recovers on reconnect |

## Testing

- **Watcher decision logic**: table-driven unit tests against a fake store — new
  threat / pending unconfirmed / confirmed-then-reoccurred / replayed-old-event.
- **Handlers**: `unconfirmed` + `confirm` with a stubbed Timeplus client; input
  validation and limit capping.
- **Provisioning**: assert teardown DDL is emitted and notify UDF/ALERT DDL is gone;
  `agentguard_notifications` DDL created idempotently.
- **End-to-end (manual, docker-compose)**: threat fires → exactly one toast; dismiss →
  server restart → no re-toast; leave undismissed → reload → re-toast; holds
  notifications unaffected.

## Acceptance criteria (from the issue)

- No Timeplus object calls back into AgentGuard; `notify_agentguard_udf` and
  `agentguard_notifier` no longer exist after provisioning (dropped on upgrade).
- A newly detected threat produces exactly one user notification via AgentGuard's own
  streaming query.
- Seen/confirmed state persists across SSE reconnects, multiple tabs, and server
  restarts.
- Hold notifications and the rest of the SSE pipeline work unchanged.

## Non-goals

- Changing the SSE transport, toast UI, or hold-notification path.
- Reworking threat detection rules or the `agentguard_security_events` schema.
- Per-user seen-state (the stream is deployment-global, matching threat status today).
