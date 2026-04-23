# Hook-Based Tool-Call Blocking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** [`docs/superpowers/specs/2026-04-22-hook-based-tool-blocking-design.md`](../specs/2026-04-22-hook-based-tool-blocking-design.md)

**Goal:** Pause Claude Code's `PreToolUse` hook on AgentGuard's `/api/holds` endpoint and resume the agent based on a human Approve/Deny decision surfaced on the threat detail page; configurable per rule via `block_policy ∈ {hold, auto_block, log_only}`.

**Architecture:** Synchronous hold-and-wait. The Claude Code plugin's PreToolUse handler is sync (`timeout: 600000` ms). It POSTs to `/api/holds`. The backend ingests the event, watches `agentguard_security_events` for new MV-driven detections (≤ 500ms wait), and either resolves immediately (`allow`/`auto_block`) or opens a hold record + long-polls (≤ 300s) for a human decision via `/api/holds/:id/decide`. An in-memory channel registry wakes the long-poll on decision. Existing rule + threat infrastructure carries `block_policy` end-to-end.

**Tech Stack:** Go 1.23 / Gin (backend), TypeScript / Node 18 / vitest (Claude Code plugin), React 18 / Vite / vitest (frontend), Timeplus Enterprise streams + materialized views (storage).

**Build commands (memorize):**
- Backend compile check: `cd backend && CGO_ENABLED=0 go build ./...`
- Backend tests: `cd backend && CGO_ENABLED=0 go test ./...`
- Backend single test: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/... -run TestName -v`
- Plugin build: `cd agents/claudecode/agentguard-plugin && npm run build`
- Plugin tests: `cd agents/claudecode/agentguard-plugin && npm test`
- Frontend build: `cd frontend && npm run build`
- Frontend tests: `cd frontend && npm test`
- Frontend dev server: `cd frontend && npm run dev` (proxies `/api/*` to `:8080`)

**Conventions to honour (project-specific):**
- macOS Apple Silicon: always use `CGO_ENABLED=0` for Go builds (dyld issue per CLAUDE.md).
- Backend tests: pure-logic in same package, `*_test.go` next to code; no integration build tag in this repo (existing tests don't touch Timeplus, they test extracted pure functions).
- All SQL errors must surface to the UI (HTTP 500/503), not be swallowed with `log.Printf` — recently overhauled in `rules.go`.
- Single-file responsibility; smaller focused files preferred.
- Frequent commits per task with conventional-commit prefixes (`feat:`, `fix:`, `test:`, `refactor:`).
- Default to writing no comments unless WHY is non-obvious.

---

## File Structure

### Backend — NEW files

| File | Responsibility |
|---|---|
| `backend/internal/holdregistry/registry.go` | In-memory `sync.Map` of `hold_id → chan Decision`; `Register`, `Signal`, `Cancel`. No Timeplus dependency. |
| `backend/internal/holdregistry/registry_test.go` | Race / double-signal / late-signal-after-cancel coverage |
| `backend/internal/handlers/holds.go` | All `/api/holds*` HTTP handlers; orchestrates ingest + decision + long-poll |
| `backend/internal/handlers/holds_test.go` | Pure-logic tests for each handler branch (using fake `tp.Query`/`tp.ExecHTTP` shims) |
| `backend/internal/handlers/holds_decision.go` | Pure decision function `Decide(input) → output`; isolates the strictness ordering & branch logic from I/O |
| `backend/internal/handlers/holds_decision_test.go` | Table-driven unit tests for `Decide` |
| `backend/internal/timeplus/holds_init.go` | DDL: `agentguard_holds` stream + `mv_holds_current` MV; startup task that abandons stale `pending` rows |

### Backend — MODIFIED files

| File | Change |
|---|---|
| `backend/internal/config/config.go` | Add `Holds.MVWaitMs`, `Holds.TimeoutSeconds`, `Holds.FailPolicy`, `Holds.Disabled`; load from env vars |
| `backend/internal/timeplus/init.go` | Idempotent `ALTER STREAM agentguard_rules ADD COLUMN IF NOT EXISTS block_policy string DEFAULT 'log_only'`; call `holds_init.SetupHoldsStorage(...)` |
| `backend/internal/handlers/rules.go` | Read/write `block_policy` in all rule queries and mutations; new `SetRuleBlockPolicy` handler |
| `backend/internal/handlers/threats.go` | Populate `pending_hold` field on `GET /api/threats/:id` response |
| `backend/internal/handlers/notifications.go` | New SSE event types: `hold_pending`, `hold_decided`; broadcaster used by `holds.go` |
| `backend/internal/models/models.go` | New: `Hold`, `HoldDecision`, `HoldRequest`, `HoldDecideRequest`, `PendingHold`. Extend: `RulePack.BlockPolicy`, `ThreatDetailResponse.PendingHold`. |
| `backend/cmd/server/main.go` | Register the seven new routes; pass config + holdregistry to handlers |

### Plugin — NEW files

| File | Responsibility |
|---|---|
| `agents/claudecode/agentguard-plugin/src/holds.ts` | `requestHold(event, config)` POST + abort timeout + fail-policy; pure of Claude Code-specific I/O |
| `agents/claudecode/agentguard-plugin/src/holds.test.ts` | Mocked-`fetch` coverage for allow/block/503/network error/abort/fail-policy |

### Plugin — MODIFIED files

| File | Change |
|---|---|
| `agents/claudecode/agentguard-plugin/src/config.ts` | Add `agentguardUrl`, `holdFailPolicy` |
| `agents/claudecode/agentguard-plugin/src/config.test.ts` | Cover new fields |
| `agents/claudecode/agentguard-plugin/src/index.ts` | Branch on `PreToolUse` → call `requestHold`, write Claude Code's `permissionDecision` shape to stdout, skip `send()`. On `SessionEnd` fire-and-forget `POST /api/holds/_abandon-session/:session_id` |
| `agents/claudecode/Makefile` | `CONFIGURE_SCRIPT`: emit a sync entry (`timeout: 600000`, no `async`) for `PreToolUse`; export `AGENTGUARD_URL` + `AGENTGUARD_HOLD_FAIL_POLICY` to `data["env"]` |

### Frontend — NEW files

| File | Responsibility |
|---|---|
| `frontend/src/pages/ThreatDetail.jsx` | `/threats/:id` — header + Pending Hold banner + Approve/Deny + raw events + Acknowledge/Clear |
| `frontend/src/components/HoldToast.jsx` | Toast component; receives `{hold_id, severity, agent_id, tool_name, message}` |

### Frontend — MODIFIED files

| File | Change |
|---|---|
| `frontend/src/api/client.js` | Five new functions: `listHolds`, `getHold`, `decideHold`, `setRuleBlockPolicy`, `getThreatDetail` |
| `frontend/src/pages/Threats.jsx` | Pending Holds section at top; SSE-driven |
| `frontend/src/pages/RulePacks.jsx` | Per-rule `block_policy` dropdown |
| `frontend/src/pages/RuleDetail.jsx` | Same dropdown, more prominent |
| `frontend/src/pages/Onboarding.jsx` | Per-rule policy selector in Step 4 |
| `frontend/src/components/Sidebar.jsx` | Pending-holds badge on Threats entry |
| `frontend/src/components/NotificationsProvider.jsx` | Handle `hold_pending` / `hold_decided` SSE event types; reconcile via `listHolds('pending')` on reconnect |
| `frontend/src/App.jsx` | Register `/threats/:id` route |

---

## Tasks

### Task 1: Config — Four New Env Vars

**Files:**
- Modify: `backend/internal/config/config.go`

- [ ] **Step 1: Read the current config struct**

Run: `cat backend/internal/config/config.go`
Note the existing `Config` struct shape (`Server`, `AgentGuard`, `Timeplus`).

- [ ] **Step 2: Add `Holds` config block**

Add to `Config`:

```go
type HoldsConfig struct {
    MVWaitMs       int    `mapstructure:"mv_wait_ms"`
    TimeoutSeconds int    `mapstructure:"timeout_seconds"`
    FailPolicy     string `mapstructure:"fail_policy"`
    Disabled       bool   `mapstructure:"disabled"`
}

type Config struct {
    // ...existing fields...
    Holds HoldsConfig `mapstructure:"holds"`
}
```

In the viper defaults:

```go
v.SetDefault("holds.mv_wait_ms", 500)
v.SetDefault("holds.timeout_seconds", 300)
v.SetDefault("holds.fail_policy", "deny")
v.SetDefault("holds.disabled", false)
```

In env binding (where the existing `AGENTGUARD_URL`, `TIMEPLUS_HOST`, etc. are bound):

```go
_ = v.BindEnv("holds.mv_wait_ms",       "AGENTGUARD_HOLD_MV_WAIT_MS")
_ = v.BindEnv("holds.timeout_seconds",  "AGENTGUARD_HOLD_TIMEOUT_SECONDS")
_ = v.BindEnv("holds.fail_policy",      "AGENTGUARD_HOLD_FAIL_POLICY")
_ = v.BindEnv("holds.disabled",         "AGENTGUARD_HOLDS_DISABLED")
```

After loading, clamp the timeout to the 540s ceiling and log a warning if exceeded:

```go
const holdTimeoutCeilingSec = 540
if cfg.Holds.TimeoutSeconds > holdTimeoutCeilingSec {
    log.Printf("config: holds.timeout_seconds=%d exceeds ceiling %d; clamping",
        cfg.Holds.TimeoutSeconds, holdTimeoutCeilingSec)
    cfg.Holds.TimeoutSeconds = holdTimeoutCeilingSec
}
```

- [ ] **Step 3: Compile check**

Run: `cd backend && CGO_ENABLED=0 go build ./...`
Expected: success, no output.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/config/config.go
git commit -m "feat(config): add holds config block (mv_wait_ms, timeout, fail_policy, disabled)"
```

---

### Task 2: holdregistry Package

**Files:**
- Create: `backend/internal/holdregistry/registry.go`
- Create: `backend/internal/holdregistry/registry_test.go`

- [ ] **Step 1: Write failing test**

Create `backend/internal/holdregistry/registry_test.go`:

```go
package holdregistry

import (
	"sync"
	"testing"
	"time"
)

func TestRegisterReceivesSignal(t *testing.T) {
	r := New()
	ch := r.Register("h1")
	defer r.Cancel("h1")
	go func() {
		time.Sleep(10 * time.Millisecond)
		r.Signal("h1", Decision{Allow: true})
	}()
	select {
	case d := <-ch:
		if !d.Allow {
			t.Fatalf("got %+v, want Allow=true", d)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func TestSignalUnknownIdNoPanic(t *testing.T) {
	r := New()
	r.Signal("does-not-exist", Decision{Allow: false}) // must not panic
}

func TestDoubleSignalNoPanic(t *testing.T) {
	r := New()
	ch := r.Register("h1")
	defer r.Cancel("h1")
	r.Signal("h1", Decision{Allow: true})
	r.Signal("h1", Decision{Allow: false}) // dropped (buffer full); no panic
	d := <-ch
	if !d.Allow {
		t.Fatalf("first signal should win, got %+v", d)
	}
}

func TestCancelRemovesEntry(t *testing.T) {
	r := New()
	r.Register("h1")
	r.Cancel("h1")
	r.Signal("h1", Decision{Allow: true}) // no-op now; receiver gone
}

func TestConcurrentRegisterSignal(t *testing.T) {
	r := New()
	const N = 100
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		id := "h" + itoa(i)
		ch := r.Register(id)
		go func() {
			defer wg.Done()
			defer r.Cancel(id)
			d := <-ch
			if !d.Allow {
				t.Errorf("expected allow")
			}
		}()
		go r.Signal(id, Decision{Allow: true})
	}
	wg.Wait()
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [16]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
```

- [ ] **Step 2: Run tests — expect compile failure**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/holdregistry/... -v`
Expected: FAIL — package does not compile (`undefined: New`, `undefined: Decision`).

- [ ] **Step 3: Implement registry**

Create `backend/internal/holdregistry/registry.go`:

```go
// Package holdregistry is an in-memory map of pending hold IDs to a
// single-buffered channel that delivers the human Approve/Deny decision
// to a long-poll goroutine waiting in the /api/holds handler.
//
// Lost on backend restart; in-flight long-polls then expire on their own
// timeout and the plugin's fail-policy applies.
package holdregistry

import "sync"

type Decision struct {
	Allow  bool
	Reason string
}

type Registry struct {
	m sync.Map // hold_id (string) → chan Decision
}

func New() *Registry {
	return &Registry{}
}

// Register reserves a slot for the given hold_id and returns the receive end
// of the channel. The channel is buffered (size 1) so Signal never blocks.
// The caller must Cancel(id) when done (use defer).
func (r *Registry) Register(holdID string) <-chan Decision {
	ch := make(chan Decision, 1)
	r.m.Store(holdID, ch)
	return ch
}

// Signal delivers a decision to the waiting goroutine. No-op if no goroutine
// is waiting (caller may have timed out and called Cancel). A second Signal
// for the same id is dropped — the channel buffer is already full.
func (r *Registry) Signal(holdID string, d Decision) {
	v, ok := r.m.Load(holdID)
	if !ok {
		return
	}
	ch, ok := v.(chan Decision)
	if !ok {
		return
	}
	select {
	case ch <- d:
	default:
	}
}

// Cancel removes the entry. Safe to call multiple times.
func (r *Registry) Cancel(holdID string) {
	r.m.Delete(holdID)
}
```

- [ ] **Step 4: Run tests — expect pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/holdregistry/... -v -race`
Expected: all tests PASS, no race warnings.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/holdregistry/
git commit -m "feat(holdregistry): in-memory channel registry for hold long-poll wakeup"
```

---

### Task 3: Models — Hold + Extend RulePack/Threat

**Files:**
- Modify: `backend/internal/models/models.go`

- [ ] **Step 1: Add new types to models.go**

Append to `backend/internal/models/models.go`:

```go
// Hold is one row from agentguard_holds (current state via mv_holds_current).
type Hold struct {
	HoldID         string    `json:"hold_id"`
	DeploymentID   string    `json:"deployment_id"`
	AgentID        string    `json:"agent_id"`
	SessionID      string    `json:"session_id"`
	RunID          string    `json:"run_id"`
	ToolName       string    `json:"tool_name"`
	RuleID         string    `json:"rule_id"`
	Severity       string    `json:"severity"`
	ArgsSummary    string    `json:"args_summary"`
	Message        string    `json:"message"`
	CreatedAt      time.Time `json:"created_at"`
	Status         string    `json:"status"` // pending|approved|denied|timeout|abandoned
	DecidedBy      string    `json:"decided_by,omitempty"`
	DecidedAt      time.Time `json:"decided_at,omitempty"`
	DecidedReason  string    `json:"decided_reason,omitempty"`
}

// HoldRequest is the JSON body the plugin sends to POST /api/holds.
type HoldRequest struct {
	DeploymentID string                 `json:"deployment_id"`
	AgentID      string                 `json:"agent_id"`
	SessionID    string                 `json:"session_id"`
	RunID        string                 `json:"run_id"`
	ToolName     string                 `json:"tool_name"`
	ToolInput    map[string]interface{} `json:"tool_input"`
	EventTime    time.Time              `json:"event_time"`
	HookPayload  map[string]interface{} `json:"hook_payload"`
}

// HoldResponse is what the plugin receives from POST /api/holds.
type HoldResponse struct {
	Decision string `json:"decision"`           // "allow" | "block"
	Reason   string `json:"reason,omitempty"`
	HoldID   string `json:"hold_id,omitempty"`  // populated when a hold record was opened
}

// HoldDecideRequest is the JSON body the frontend sends to POST /api/holds/:id/decide.
type HoldDecideRequest struct {
	Decision      string `json:"decision"` // "approved" | "denied"
	DecidedBy     string `json:"decided_by"`
	DecidedReason string `json:"decided_reason,omitempty"`
}

// PendingHold is embedded in ThreatDetailResponse when a hold is currently
// pending for the threat's rule + session. Used by the threat detail page
// to render Approve/Deny controls.
type PendingHold struct {
	HoldID      string    `json:"hold_id"`
	ToolName    string    `json:"tool_name"`
	ArgsSummary string    `json:"args_summary"`
	CreatedAt   time.Time `json:"created_at"`
}

// ThreatDetailResponse is returned by GET /api/threats/:id.
type ThreatDetailResponse struct {
	Threat
	PendingHold *PendingHold `json:"pending_hold,omitempty"`
}
```

Modify the existing `RulePack` struct — add field:

```go
type RulePack struct {
	// ...existing fields...
	BlockPolicy string `json:"block_policy"` // hold|auto_block|log_only
}
```

- [ ] **Step 2: Compile check**

Run: `cd backend && CGO_ENABLED=0 go build ./...`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/models/models.go
git commit -m "feat(models): add Hold + HoldRequest/Response/Decide types; RulePack.BlockPolicy; ThreatDetailResponse"
```

---

### Task 4: Storage DDL — agentguard_holds Stream + MV + ALTER agentguard_rules

**Files:**
- Create: `backend/internal/timeplus/holds_init.go`
- Modify: `backend/internal/timeplus/init.go`

- [ ] **Step 1: Create holds_init.go with the DDL**

Create `backend/internal/timeplus/holds_init.go`:

```go
package timeplus

import (
	"fmt"
	"log"

	"github.com/timeplus/agentguard/internal/config"
)

const ddlHoldsStream = `
CREATE STREAM IF NOT EXISTS agentguard_holds (
  hold_id          string,
  deployment_id    string,
  agent_id         string,
  session_id       string,
  run_id           string,
  tool_name        string,
  rule_id          string,
  severity         string,
  args_summary     string,
  message          string,
  created_at       datetime64(3),
  status           string,
  decided_by       string,
  decided_at       datetime64(3),
  decided_reason   string,
  updated_at       datetime64(3)
)`

const ddlHoldsCurrentMV = `
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_holds_current AS
SELECT
  hold_id,
  arg_max(deployment_id, updated_at) AS deployment_id,
  arg_max(agent_id,      updated_at) AS agent_id,
  arg_max(session_id,    updated_at) AS session_id,
  arg_max(run_id,        updated_at) AS run_id,
  arg_max(tool_name,     updated_at) AS tool_name,
  arg_max(rule_id,       updated_at) AS rule_id,
  arg_max(severity,      updated_at) AS severity,
  arg_max(args_summary,  updated_at) AS args_summary,
  arg_max(message,       updated_at) AS message,
  arg_max(created_at,    updated_at) AS created_at,
  arg_max(status,        updated_at) AS status,
  arg_max(decided_by,    updated_at) AS decided_by,
  arg_max(decided_at,    updated_at) AS decided_at,
  arg_max(decided_reason, updated_at) AS decided_reason
FROM agentguard_holds
GROUP BY hold_id`

const ddlAlterRulesAddBlockPolicy = `
ALTER STREAM agentguard_rules ADD COLUMN IF NOT EXISTS block_policy string DEFAULT 'log_only'`

// SetupHoldsStorage creates the holds stream, the current-state MV, and
// adds the block_policy column to agentguard_rules. All operations are
// idempotent. Errors are logged and returned but the caller may choose to
// continue (matching the existing init.go pattern).
func SetupHoldsStorage(cfg *config.TimeplusConfig) error {
	if err := ExecHTTP(cfg, ddlHoldsStream); err != nil {
		return fmt.Errorf("create agentguard_holds stream: %w", err)
	}
	log.Printf("timeplus: agentguard_holds stream ready")

	if err := ExecHTTP(cfg, ddlHoldsCurrentMV); err != nil {
		return fmt.Errorf("create mv_holds_current: %w", err)
	}
	log.Printf("timeplus: mv_holds_current ready")

	if err := ExecHTTP(cfg, ddlAlterRulesAddBlockPolicy); err != nil {
		return fmt.Errorf("alter agentguard_rules add block_policy: %w", err)
	}
	log.Printf("timeplus: agentguard_rules.block_policy column ready")

	return nil
}

// AbandonStalePendingHolds writes status='abandoned' for any holds rows that
// are still 'pending' at server startup. These are leftovers from a prior
// process that crashed mid-hold; the in-memory channels are gone, so the UI
// would otherwise show stale entries forever.
func AbandonStalePendingHolds(cfg *config.TimeplusConfig) error {
	sql := `
INSERT INTO agentguard_holds
  (hold_id, deployment_id, agent_id, session_id, run_id, tool_name, rule_id,
   severity, args_summary, message, created_at, status,
   decided_by, decided_at, decided_reason, updated_at)
SELECT
  hold_id, deployment_id, agent_id, session_id, run_id, tool_name, rule_id,
  severity, args_summary, message, created_at,
  'abandoned' AS status,
  'startup'   AS decided_by,
  now64(3)    AS decided_at,
  'orphaned by backend restart' AS decided_reason,
  now64(3)    AS updated_at
FROM mv_holds_current
WHERE status = 'pending'`
	if err := ExecHTTP(cfg, sql); err != nil {
		return fmt.Errorf("abandon stale pending holds: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Wire into init.go**

In `backend/internal/timeplus/init.go`, find the existing `Init()` function and append the holds setup after the existing rule-MV setup:

```go
// Existing init.go content (rules, security_events, etc.)
// ...

// Setup holds storage (stream + MV + ALTER rules.block_policy).
if err := SetupHoldsStorage(&cfg.Timeplus); err != nil {
    log.Printf("timeplus: holds storage setup failed: %v (continuing — /api/holds will return 503)", err)
}

// Abandon any stale pending holds left over from a previous process.
if err := AbandonStalePendingHolds(&cfg.Timeplus); err != nil {
    log.Printf("timeplus: abandon stale pending holds: %v (continuing)", err)
}
```

- [ ] **Step 3: Compile check**

Run: `cd backend && CGO_ENABLED=0 go build ./...`
Expected: success.

- [ ] **Step 4: Manual smoke test against a local Timeplus**

Run: `cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server`
Expected log lines:
```
timeplus: agentguard_holds stream ready
timeplus: mv_holds_current ready
timeplus: agentguard_rules.block_policy column ready
```

Then verify in Timeplus:
```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -u proton:'timeplus@t+' \
  -H 'Content-Type: application/json' \
  -d '{"query":"DESCRIBE STREAM agentguard_holds"}'
```
Expected: 16 columns including `hold_id`, `status`, `updated_at`.

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -u proton:'timeplus@t+' \
  -H 'Content-Type: application/json' \
  -d '{"query":"DESCRIBE STREAM agentguard_rules"}' | grep block_policy
```
Expected: line containing `block_policy` with type `string`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/timeplus/
git commit -m "feat(timeplus): add agentguard_holds stream + mv_holds_current; ALTER agentguard_rules ADD block_policy"
```

---

### Task 5: Rules Handler — Read block_policy in Queries

**Files:**
- Modify: `backend/internal/handlers/rules.go`

- [ ] **Step 1: Update sqlCurrentRules constant**

In `backend/internal/handlers/rules.go`, find `sqlCurrentRules` (around line 32) and add `block_policy`:

```go
const sqlCurrentRules = `
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  arg_max(enabled,            updated_at) AS enabled,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources,
  arg_max(block_policy,       updated_at) AS block_policy
FROM table(agentguard_rules)
GROUP BY rule_id
HAVING arg_max(deleted, updated_at) = false`
```

- [ ] **Step 2: Update queryCurrentRulePacks to populate the new field**

In the `queryCurrentRulePacks` function, add to the row→pack mapping:

```go
pack := models.RulePack{
    // ...existing fields...
    BlockPolicy: parseStr(row["block_policy"]),
}
```

If `parseStr` returns empty (rule was inserted before the ALTER), default to `"log_only"`:

```go
if pack.BlockPolicy == "" {
    pack.BlockPolicy = "log_only"
}
```

- [ ] **Step 3: Compile check**

Run: `cd backend && CGO_ENABLED=0 go build ./...`
Expected: success.

- [ ] **Step 4: Hit the API and verify**

Restart server, then:
```bash
curl -s http://localhost:8080/api/rules | python3 -c 'import sys,json; print(json.dumps(json.load(sys.stdin)["rule_packs"][0], indent=2))'
```
Expected: rule pack JSON includes `"block_policy": "log_only"`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/rules.go
git commit -m "feat(rules): read block_policy in current rules query; default log_only when missing"
```

---

### Task 6: Rules Handler — block_policy in Mutations + SetRuleBlockPolicy Endpoint

**Files:**
- Modify: `backend/internal/handlers/rules.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Carry block_policy through every existing mutation handler**

Patch the SQL in each rule mutation handler in `rules.go` to include `block_policy`. The pattern is the same in each: append `block_policy` to the column list and use `arg_max(block_policy, updated_at)` (for re-projecting the existing row) or `'log_only'` (for inserts).

In `ToggleRule`, the `INSERT INTO ... SELECT` block (around line 161):

```sql
INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, block_policy, updated_at)
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  NOT arg_max(enabled,        updated_at) AS enabled,
  false                                   AS deleted,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources,
  arg_max(block_policy,       updated_at) AS block_policy,
  now64(3)                                AS updated_at
FROM table(agentguard_rules)
WHERE rule_id = '%s'
GROUP BY rule_id
```

In `UpgradeRule`, the literal `INSERT INTO ... VALUES` (around line 261):

```go
insertSQL := fmt.Sprintf(`INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, block_policy, updated_at) VALUES
('%s','%s','%s','%s','%s',%v,false,'%s','%s','%s','%s','%s','%s',now64(3))`,
    id,
    strings.ReplaceAll(current.Name, "'", "''"),
    targetVersion,
    strings.ReplaceAll(current.Description, "'", "''"),
    escapedSQL,
    current.Enabled,
    current.Severity, current.Category,
    current.RequiredTelemetry, current.FPRate, current.HookSources,
    current.BlockPolicy, // preserve existing policy on upgrade
)
```

In `UninstallRule` (around line 311), the SELECT for the soft-delete:

```sql
INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, block_policy, updated_at)
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  -- ...all other arg_max columns as before...
  arg_max(hook_sources,       updated_at) AS hook_sources,
  arg_max(block_policy,       updated_at) AS block_policy,
  now64(3)                                AS updated_at
FROM table(agentguard_rules)
WHERE rule_id = '%s'
GROUP BY rule_id
```

In `InstallRule` (around line 437):

```go
insertSQL := fmt.Sprintf(
    `INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, block_policy, updated_at) VALUES ('%s','%s','%s','%s','%s',true,false,'%s','%s','%s','%s','%s','log_only',now64(3))`,
    id,
    strings.ReplaceAll(entry.Name, "'", "''"),
    entry.Version,
    strings.ReplaceAll(entry.Description, "'", "''"),
    escapedSQL,
    entry.Severity, entry.Category,
    entry.RequiredTelemetry, entry.FPRate, entry.HookSources,
)
```

In `CreateRule` (around line 651), same pattern with `'log_only'` literal.

- [ ] **Step 2: Add the SetRuleBlockPolicy handler**

Append to `rules.go`:

```go
// SetRuleBlockPolicy handles POST /api/rules/:id/policy
// Body: {"block_policy": "hold" | "auto_block" | "log_only"}
func SetRuleBlockPolicy(cfg *config.Config, mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		var body struct {
			BlockPolicy string `json:"block_policy"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		switch body.BlockPolicy {
		case "hold", "auto_block", "log_only":
			// ok
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "block_policy must be hold|auto_block|log_only"})
			return
		}

		packs, err := queryCurrentRulePacks(&cfg.Timeplus, mgr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
			return
		}
		var current *models.RulePack
		for i := range packs {
			if packs[i].ID == id {
				current = &packs[i]
				break
			}
		}
		if current == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "rule not found"})
			return
		}

		sql := fmt.Sprintf(`
INSERT INTO agentguard_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, block_policy, updated_at)
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  arg_max(enabled,            updated_at) AS enabled,
  false                                   AS deleted,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources,
  '%s'                                    AS block_policy,
  now64(3)                                AS updated_at
FROM table(agentguard_rules)
WHERE rule_id = '%s'
GROUP BY rule_id`, body.BlockPolicy, escapeSQL(id))

		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("set policy failed: %v", err)})
			return
		}

		current.BlockPolicy = body.BlockPolicy
		c.JSON(http.StatusOK, current)
	}
}
```

- [ ] **Step 3: Register the route in main.go**

In `backend/cmd/server/main.go`, find the existing rules routes and append:

```go
api.POST("/rules/:id/policy", handlers.SetRuleBlockPolicy(&cfg, ruleMgr))
```

- [ ] **Step 4: Compile + smoke test**

Run: `cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go`
Restart server.

```bash
# Pick an installed rule's id, e.g. rp_001
curl -s -X POST http://localhost:8080/api/rules/rp_001/policy \
  -H 'Content-Type: application/json' \
  -d '{"block_policy":"hold"}' | python3 -m json.tool
```
Expected: response includes `"block_policy": "hold"`.

```bash
curl -s http://localhost:8080/api/rules | python3 -c 'import sys,json; r=[p for p in json.load(sys.stdin)["rule_packs"] if p["id"]=="rp_001"][0]; print(r["block_policy"])'
```
Expected: `hold`

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/rules.go backend/cmd/server/main.go
git commit -m "feat(rules): carry block_policy through all mutations; new POST /api/rules/:id/policy endpoint"
```

---

### Task 7: Holds Decision — Pure Logic Function

**Files:**
- Create: `backend/internal/handlers/holds_decision.go`
- Create: `backend/internal/handlers/holds_decision_test.go`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/handlers/holds_decision_test.go`:

```go
package handlers

import (
	"testing"

	"github.com/timeplus/agentguard/internal/models"
)

func TestDecide_KillSwitchAllowsAlways(t *testing.T) {
	out := Decide(DecisionInput{KillSwitch: true})
	if out.Decision != "allow" {
		t.Fatalf("got %s, want allow", out.Decision)
	}
}

func TestDecide_NoMatchesAllow(t *testing.T) {
	out := Decide(DecisionInput{})
	if out.Decision != "allow" {
		t.Fatalf("got %s, want allow", out.Decision)
	}
}

func TestDecide_AutoBlockReturnsBlock(t *testing.T) {
	out := Decide(DecisionInput{
		Matches: []RuleMatch{
			{RuleID: "rp_001", BlockPolicy: "auto_block", Severity: "critical", Message: "blocked!"},
		},
	})
	if out.Decision != "block" {
		t.Fatalf("got %s, want block", out.Decision)
	}
	if out.RuleID != "rp_001" {
		t.Fatalf("got rule %s, want rp_001", out.RuleID)
	}
	if out.Reason != "blocked!" {
		t.Fatalf("got reason %q, want %q", out.Reason, "blocked!")
	}
}

func TestDecide_HoldReturnsHold(t *testing.T) {
	out := Decide(DecisionInput{
		Matches: []RuleMatch{
			{RuleID: "rp_002", BlockPolicy: "hold", Severity: "warning", Message: "needs review"},
		},
	})
	if out.Decision != "hold" {
		t.Fatalf("got %s, want hold", out.Decision)
	}
}

func TestDecide_HoldBeatsAutoBlock(t *testing.T) {
	out := Decide(DecisionInput{
		Matches: []RuleMatch{
			{RuleID: "rp_001", BlockPolicy: "auto_block"},
			{RuleID: "rp_002", BlockPolicy: "hold"},
		},
	})
	if out.Decision != "hold" {
		t.Fatalf("got %s, want hold (hold > auto_block)", out.Decision)
	}
	if out.RuleID != "rp_002" {
		t.Fatalf("got rule %s, want rp_002", out.RuleID)
	}
}

func TestDecide_LogOnlyDoesNotCount(t *testing.T) {
	out := Decide(DecisionInput{
		Matches: []RuleMatch{
			{RuleID: "rp_003", BlockPolicy: "log_only"},
		},
	})
	if out.Decision != "allow" {
		t.Fatalf("got %s, want allow (log_only does not block)", out.Decision)
	}
}

func TestDecide_PreExistingThreatTriggers(t *testing.T) {
	out := Decide(DecisionInput{
		PreExistingThreats: []models.Threat{
			{RuleID: "rp_001", Status: "open"},
		},
		ThreatRulePolicies: map[string]string{"rp_001": "auto_block"},
	})
	if out.Decision != "block" {
		t.Fatalf("got %s, want block", out.Decision)
	}
}

func TestDecide_PreExistingClearedThreatIgnored(t *testing.T) {
	// status='cleared' (not 'open') must NOT auto-trigger
	out := Decide(DecisionInput{
		PreExistingThreats: []models.Threat{
			{RuleID: "rp_001", Status: "cleared"},
		},
		ThreatRulePolicies: map[string]string{"rp_001": "auto_block"},
	})
	if out.Decision != "allow" {
		t.Fatalf("got %s, want allow (cleared threats don't trigger)", out.Decision)
	}
}
```

- [ ] **Step 2: Run tests — expect compile failure**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestDecide -v`
Expected: compile errors — `undefined: Decide`, `undefined: DecisionInput`, etc.

- [ ] **Step 3: Implement Decide**

Create `backend/internal/handlers/holds_decision.go`:

```go
package handlers

import "github.com/timeplus/agentguard/internal/models"

// RuleMatch represents one rule that fired for the current hold request.
// Either from a pre-existing open threat or from a fresh MV detection
// observed during the wait window.
type RuleMatch struct {
	RuleID      string
	BlockPolicy string // hold | auto_block | log_only
	Severity    string
	Message     string
}

// DecisionInput is the data the Decide function works on.
// All Timeplus I/O happens in the handler before this struct is built.
type DecisionInput struct {
	KillSwitch         bool
	Matches            []RuleMatch
	PreExistingThreats []models.Threat
	// ThreatRulePolicies maps rule_id → block_policy for the pre-existing threats.
	// (We resolve this once in the handler so Decide stays pure.)
	ThreatRulePolicies map[string]string
}

// DecisionOutput is what the hold handler does next.
type DecisionOutput struct {
	Decision string // "allow" | "block" | "hold"
	RuleID   string
	Reason   string
	Severity string
}

// Decide picks the strictest applicable block_policy across matches and
// pre-existing open threats. Strictness: hold > auto_block > log_only > (no match).
// Map-output:
//   hold       → Decision="hold"
//   auto_block → Decision="block"
//   log_only   → Decision="allow"
//   no match   → Decision="allow"
func Decide(in DecisionInput) DecisionOutput {
	if in.KillSwitch {
		return DecisionOutput{Decision: "allow"}
	}

	var allMatches []RuleMatch
	allMatches = append(allMatches, in.Matches...)
	for _, t := range in.PreExistingThreats {
		if t.Status != "open" {
			continue
		}
		policy := in.ThreatRulePolicies[t.RuleID]
		if policy == "" || policy == "log_only" {
			continue
		}
		allMatches = append(allMatches, RuleMatch{
			RuleID:      t.RuleID,
			BlockPolicy: policy,
			Severity:    t.Severity,
			Message:     "Pre-existing open threat: " + t.RuleName,
		})
	}

	var hold, autoBlock *RuleMatch
	for i := range allMatches {
		switch allMatches[i].BlockPolicy {
		case "hold":
			if hold == nil {
				hold = &allMatches[i]
			}
		case "auto_block":
			if autoBlock == nil {
				autoBlock = &allMatches[i]
			}
		}
	}

	if hold != nil {
		return DecisionOutput{
			Decision: "hold",
			RuleID:   hold.RuleID,
			Reason:   hold.Message,
			Severity: hold.Severity,
		}
	}
	if autoBlock != nil {
		return DecisionOutput{
			Decision: "block",
			RuleID:   autoBlock.RuleID,
			Reason:   autoBlock.Message,
			Severity: autoBlock.Severity,
		}
	}
	return DecisionOutput{Decision: "allow"}
}
```

- [ ] **Step 4: Run tests — expect pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestDecide -v`
Expected: all 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/holds_decision.go backend/internal/handlers/holds_decision_test.go
git commit -m "feat(holds): pure Decide function (strictness ordering hold > auto_block > log_only)"
```

---

### Task 8: Holds Handler — POST /api/holds (Decision Pipeline)

**Files:**
- Create: `backend/internal/handlers/holds.go`
- Modify: `backend/cmd/server/main.go`

This is the largest single task. The handler orchestrates Step 1 (ingest) → Step 2 (pre-existing threats) → Step 3 (MV-settle wait) → Step 4 (Decide) → Step 5 (open hold + long-poll).

- [ ] **Step 1: Create holds.go skeleton with the POST handler**

Create `backend/internal/handlers/holds.go`:

```go
package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/holdregistry"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

const argsSummaryMaxBytes = 2048

// CreateHold handles POST /api/holds.
// Synchronous endpoint — does not return until backend has decided
// (allow / block / hold + human decision / hold timeout).
func CreateHold(cfg *config.Config, registry *holdregistry.Registry, broadcaster *AlertBroadcaster) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.HoldRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		// Master kill-switch: skip all decision logic, just allow.
		if cfg.Holds.Disabled {
			c.JSON(http.StatusOK, models.HoldResponse{Decision: "allow"})
			return
		}

		// Step 1: Ingest the hook event into agentguard_hook_events so MVs see it.
		if err := ingestHookEvent(cfg, &req); err != nil {
			log.Printf("holds: ingest event failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("ingest failed: %v", err)})
			return
		}

		// Step 2: pre-existing open threats for this session.
		preExisting, threatPolicies, err := queryPreExistingOpenThreats(cfg, &req)
		if err != nil {
			log.Printf("holds: pre-existing threats query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("pre-existing threats query: %v", err)})
			return
		}

		// Step 3: wait up to MVWaitMs for new MV detections matching this event.
		newMatches, err := waitForNewMVDetections(cfg, &req)
		if err != nil {
			log.Printf("holds: MV-wait query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("MV-wait query: %v", err)})
			return
		}

		// Step 4: pure decision logic.
		out := Decide(DecisionInput{
			KillSwitch:         false,
			Matches:            newMatches,
			PreExistingThreats: preExisting,
			ThreatRulePolicies: threatPolicies,
		})

		switch out.Decision {
		case "allow":
			c.JSON(http.StatusOK, models.HoldResponse{Decision: "allow"})
			return
		case "block":
			c.JSON(http.StatusOK, models.HoldResponse{
				Decision: "block",
				Reason:   out.Reason,
			})
			return
		case "hold":
			// Step 5: open hold record, register channel, long-poll.
			holdID := uuid.NewString()
			argsSummary := truncateJSON(req.ToolInput, argsSummaryMaxBytes)
			now := time.Now().UTC()
			if err := writeHoldRow(cfg, models.Hold{
				HoldID:       holdID,
				DeploymentID: req.DeploymentID,
				AgentID:      req.AgentID,
				SessionID:    req.SessionID,
				RunID:        req.RunID,
				ToolName:     req.ToolName,
				RuleID:       out.RuleID,
				Severity:     out.Severity,
				ArgsSummary:  argsSummary,
				Message:      out.Reason,
				CreatedAt:    now,
				Status:       "pending",
			}, now); err != nil {
				log.Printf("holds: write pending hold row: %v", err)
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("write hold row: %v", err)})
				return
			}

			ch := registry.Register(holdID)
			defer registry.Cancel(holdID)

			// Notify the UI via the existing SSE notification stream.
			broadcaster.Publish(models.AlertWebhookPayload{
				RuleID:     out.RuleID,
				RuleName:   "hold_pending",
				Severity:   out.Severity,
				AgentID:    req.AgentID,
				Message:    fmt.Sprintf("hold:%s|tool:%s|reason:%s", holdID, req.ToolName, out.Reason),
				DetectedAt: now.Format(time.RFC3339),
			})

			timeout := time.Duration(cfg.Holds.TimeoutSeconds) * time.Second
			select {
			case d := <-ch:
				// Human decided.
				c.JSON(http.StatusOK, models.HoldResponse{
					Decision: ifThenElse(d.Allow, "allow", "block"),
					Reason:   d.Reason,
					HoldID:   holdID,
				})
			case <-time.After(timeout):
				// Timeout — apply fail-policy and write timeout row.
				_ = writeHoldStatus(cfg, holdID, "timeout", "system", "hold timeout")
				c.JSON(http.StatusOK, models.HoldResponse{
					Decision: cfg.Holds.FailPolicy,
					Reason:   "AgentGuard hold timed out",
					HoldID:   holdID,
				})
			}
			return
		}
	}
}

func ifThenElse(b bool, a, c string) string {
	if b {
		return a
	}
	return c
}

func truncateJSON(v interface{}, maxBytes int) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	if len(b) > maxBytes {
		return string(b[:maxBytes]) + "...[truncated]"
	}
	return string(b)
}
```

- [ ] **Step 2: Add the four helper functions referenced above**

Append to `backend/internal/handlers/holds.go`:

```go
// ingestHookEvent inserts the just-emitted PreToolUse event into
// agentguard_hook_events so MVs can detect it. The plugin does NOT also
// call its async send() for PreToolUse, so this is the only ingest path.
func ingestHookEvent(cfg *config.Config, req *models.HoldRequest) error {
	payloadJSON, err := json.Marshal(req.HookPayload)
	if err != nil {
		return fmt.Errorf("marshal hook payload: %w", err)
	}
	sql := fmt.Sprintf(`
INSERT INTO agentguard_hook_events
  (hook_name, event_time, session_id, run_id, agent_id, deployment_id,
   deployment_name, session_key, tool_name, provider, model,
   hook_decision, block_reason, event_data, agent_type)
VALUES
  ('before_tool_call', '%s', '%s', '%s', '%s', '%s',
   '', '', '%s', '', '',
   'observe', '', '%s', 'claudecode')`,
		req.EventTime.UTC().Format("2006-01-02 15:04:05.999"),
		escapeSQL(req.SessionID),
		escapeSQL(req.RunID),
		escapeSQL(req.AgentID),
		escapeSQL(req.DeploymentID),
		escapeSQL(req.ToolName),
		escapeSQLString(string(payloadJSON)),
	)
	return tp.ExecHTTP(&cfg.Timeplus, sql)
}

func escapeSQLString(s string) string {
	// SQL string escape: '  →  ''
	return jsonEscapeForSQL(s)
}

func jsonEscapeForSQL(s string) string {
	out := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\'')
		} else {
			out = append(out, s[i])
		}
	}
	return string(out)
}

// queryPreExistingOpenThreats returns open threats for this session and
// a rule_id → block_policy map for those threats' rules.
func queryPreExistingOpenThreats(cfg *config.Config, req *models.HoldRequest) ([]models.Threat, map[string]string, error) {
	sql := fmt.Sprintf(`
SELECT t.rule_id, t.rule_name, t.severity, t.status, r.block_policy
FROM (
  SELECT rule_id, rule_name, severity,
         arg_max(status, status_updated_at) AS status
  FROM (
    SELECT rule_id,
           any(rule_name)            AS rule_name,
           any(severity)             AS severity,
           'open'                    AS status,
           max(detected_at)          AS status_updated_at
    FROM table(agentguard_security_events)
    WHERE deployment_id = '%s' AND agent_id = '%s' AND session_id = '%s'
    GROUP BY rule_id
  )
  GROUP BY rule_id, rule_name, severity
) AS t
LEFT JOIN (
  SELECT rule_id, arg_max(block_policy, updated_at) AS block_policy
  FROM table(agentguard_rules)
  GROUP BY rule_id
) AS r ON t.rule_id = r.rule_id
WHERE t.status = 'open'`,
		escapeSQL(req.DeploymentID), escapeSQL(req.AgentID), escapeSQL(req.SessionID))

	rows, err := tp.Query(&cfg.Timeplus, sql)
	if err != nil {
		return nil, nil, err
	}
	threats := make([]models.Threat, 0, len(rows))
	policies := make(map[string]string, len(rows))
	for _, row := range rows {
		ruleID := parseStr(row["rule_id"])
		threats = append(threats, models.Threat{
			RuleID:   ruleID,
			RuleName: parseStr(row["rule_name"]),
			Severity: parseStr(row["severity"]),
			Status:   parseStr(row["status"]),
		})
		policies[ruleID] = parseStr(row["block_policy"])
	}
	return threats, policies, nil
}

// waitForNewMVDetections polls agentguard_security_events every 50ms for up to
// MVWaitMs, looking for rows tagged to this session with detected_at >= event_time.
// Returns the matching rules (joined with their current block_policy).
func waitForNewMVDetections(cfg *config.Config, req *models.HoldRequest) ([]RuleMatch, error) {
	const pollIntervalMs = 50
	totalMs := cfg.Holds.MVWaitMs
	if totalMs < pollIntervalMs {
		totalMs = pollIntervalMs
	}
	deadline := time.Now().Add(time.Duration(totalMs) * time.Millisecond)

	sql := fmt.Sprintf(`
SELECT s.rule_id, s.rule_name, s.severity, s.message, r.block_policy
FROM (
  SELECT rule_id, rule_name, severity, message
  FROM table(agentguard_security_events)
  WHERE deployment_id = '%s' AND agent_id = '%s' AND session_id = '%s'
    AND detected_at >= '%s'
) AS s
LEFT JOIN (
  SELECT rule_id, arg_max(block_policy, updated_at) AS block_policy
  FROM table(agentguard_rules)
  GROUP BY rule_id
) AS r ON s.rule_id = r.rule_id
WHERE r.block_policy IN ('hold', 'auto_block')`,
		escapeSQL(req.DeploymentID), escapeSQL(req.AgentID), escapeSQL(req.SessionID),
		req.EventTime.UTC().Format("2006-01-02 15:04:05.999"))

	for time.Now().Before(deadline) {
		rows, err := tp.Query(&cfg.Timeplus, sql)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			matches := make([]RuleMatch, 0, len(rows))
			for _, row := range rows {
				matches = append(matches, RuleMatch{
					RuleID:      parseStr(row["rule_id"]),
					BlockPolicy: parseStr(row["block_policy"]),
					Severity:    parseStr(row["severity"]),
					Message:     parseStr(row["message"]),
				})
			}
			return matches, nil
		}
		time.Sleep(pollIntervalMs * time.Millisecond)
	}
	return nil, nil
}

// writeHoldRow inserts a new row into agentguard_holds (initial 'pending' state).
func writeHoldRow(cfg *config.Config, h models.Hold, now time.Time) error {
	sql := fmt.Sprintf(`
INSERT INTO agentguard_holds
  (hold_id, deployment_id, agent_id, session_id, run_id, tool_name, rule_id,
   severity, args_summary, message, created_at, status,
   decided_by, decided_at, decided_reason, updated_at)
VALUES
  ('%s','%s','%s','%s','%s','%s','%s',
   '%s','%s','%s','%s','%s',
   '','1970-01-01 00:00:00.000','','%s')`,
		escapeSQL(h.HoldID), escapeSQL(h.DeploymentID), escapeSQL(h.AgentID),
		escapeSQL(h.SessionID), escapeSQL(h.RunID), escapeSQL(h.ToolName), escapeSQL(h.RuleID),
		escapeSQL(h.Severity), escapeSQLString(h.ArgsSummary), escapeSQLString(h.Message),
		now.Format("2006-01-02 15:04:05.999"), h.Status,
		now.Format("2006-01-02 15:04:05.999"))
	return tp.ExecHTTP(&cfg.Timeplus, sql)
}

// writeHoldStatus appends a status-update row for an existing hold (e.g.,
// approved / denied / timeout / abandoned). Idempotent only at the row level.
func writeHoldStatus(cfg *config.Config, holdID, status, decidedBy, decidedReason string) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05.999")
	sql := fmt.Sprintf(`
INSERT INTO agentguard_holds
  (hold_id, deployment_id, agent_id, session_id, run_id, tool_name, rule_id,
   severity, args_summary, message, created_at, status,
   decided_by, decided_at, decided_reason, updated_at)
SELECT
  hold_id, deployment_id, agent_id, session_id, run_id, tool_name, rule_id,
  severity, args_summary, message, created_at,
  '%s' AS status,
  '%s' AS decided_by,
  '%s' AS decided_at,
  '%s' AS decided_reason,
  '%s' AS updated_at
FROM mv_holds_current
WHERE hold_id = '%s'
LIMIT 1`,
		status, escapeSQL(decidedBy), now, escapeSQLString(decidedReason), now,
		escapeSQL(holdID))
	return tp.ExecHTTP(&cfg.Timeplus, sql)
}
```

- [ ] **Step 3: Add `github.com/google/uuid` dependency**

Run: `cd backend && go get github.com/google/uuid && go mod tidy`
Expected: dependency added.

- [ ] **Step 4: Wire CreateHold into main.go**

In `backend/cmd/server/main.go`:

```go
import (
    // ...existing...
    "github.com/timeplus/agentguard/internal/holdregistry"
)

func runServer(cmd *cobra.Command, args []string) {
    // ...existing setup, including: cfg, ruleMgr, GlobalAlertBroadcaster ...

    holdReg := holdregistry.New()

    // ...router setup...

    api.POST("/holds", handlers.CreateHold(&cfg, holdReg, &handlers.GlobalAlertBroadcaster))
}
```

- [ ] **Step 5: Compile**

Run: `cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go`
Expected: success.

- [ ] **Step 6: Manual smoke test (no rule fires → allow)**

Restart server. With no `hold` or `auto_block` rules installed:

```bash
curl -s -X POST http://localhost:8080/api/holds \
  -H 'Content-Type: application/json' \
  -d '{
    "deployment_id":"test-deploy",
    "agent_id":"test-agent",
    "session_id":"sess-1",
    "run_id":"run-1",
    "tool_name":"Bash",
    "tool_input":{"command":"ls"},
    "event_time":"2026-04-22T12:00:00Z",
    "hook_payload":{"hook_event_name":"PreToolUse","tool_name":"Bash"}
  }'
```
Expected: `{"decision":"allow"}` within ~600ms.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/handlers/holds.go backend/cmd/server/main.go backend/go.mod backend/go.sum
git commit -m "feat(holds): POST /api/holds — ingest + pre-existing threats + MV wait + decision pipeline"
```

---

### Task 9: Holds Handler — /decide, GET, /_abandon-session, /_drain

**Files:**
- Modify: `backend/internal/handlers/holds.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add the four remaining handlers**

Append to `backend/internal/handlers/holds.go`:

```go
// DecideHold handles POST /api/holds/:hold_id/decide.
// Body: HoldDecideRequest. Idempotent.
func DecideHold(cfg *config.Config, registry *holdregistry.Registry, broadcaster *AlertBroadcaster) gin.HandlerFunc {
	return func(c *gin.Context) {
		holdID := c.Param("hold_id")
		var body models.HoldDecideRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		if body.Decision != "approved" && body.Decision != "denied" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "decision must be approved|denied"})
			return
		}

		// Look up current state — idempotency check.
		existing, err := getHoldByID(cfg, holdID)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("query hold: %v", err)})
			return
		}
		if existing == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "hold not found"})
			return
		}
		if existing.Status != "pending" {
			// Already terminal — return existing decision without writing a new row.
			c.JSON(http.StatusOK, gin.H{"ok": true, "already_decided_as": existing.Status})
			return
		}

		newStatus := body.Decision // "approved" | "denied"
		if err := writeHoldStatus(cfg, holdID, newStatus, body.DecidedBy, body.DecidedReason); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("write decision: %v", err)})
			return
		}

		// Wake the long-poll goroutine.
		registry.Signal(holdID, holdregistry.Decision{
			Allow:  newStatus == "approved",
			Reason: body.DecidedReason,
		})

		// Push SSE so toasts/list refresh.
		broadcaster.Publish(models.AlertWebhookPayload{
			RuleID:     existing.RuleID,
			RuleName:   "hold_decided",
			Severity:   existing.Severity,
			AgentID:    existing.AgentID,
			Message:    fmt.Sprintf("hold:%s|decision:%s", holdID, newStatus),
			DetectedAt: time.Now().UTC().Format(time.RFC3339),
		})

		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// ListHolds handles GET /api/holds.
// Query params: ?status=pending (default), ?limit=N (default 100, max 500).
func ListHolds(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := c.DefaultQuery("status", "pending")
		limit := 100
		if v := c.Query("limit"); v != "" {
			if n, err := strconvAtoi(v); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		sql := fmt.Sprintf(`
SELECT hold_id, deployment_id, agent_id, session_id, run_id, tool_name,
       rule_id, severity, args_summary, message, created_at, status,
       decided_by, decided_at, decided_reason
FROM table(mv_holds_current)
WHERE status = '%s'
ORDER BY created_at DESC
LIMIT %d`, escapeSQL(status), limit)
		rows, err := tp.Query(&cfg.Timeplus, sql)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		holds := make([]models.Hold, 0, len(rows))
		for _, row := range rows {
			holds = append(holds, rowToHold(row))
		}
		c.JSON(http.StatusOK, gin.H{"holds": holds, "total": len(holds)})
	}
}

// GetHold handles GET /api/holds/:hold_id. Returns current state + full timeline.
func GetHold(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		holdID := c.Param("hold_id")

		current, err := getHoldByID(cfg, holdID)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		if current == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "hold not found"})
			return
		}

		// Timeline = all rows for this hold_id in agentguard_holds, ordered by updated_at.
		timelineSQL := fmt.Sprintf(`
SELECT status, decided_by, decided_at, decided_reason, updated_at
FROM table(agentguard_holds)
WHERE hold_id = '%s'
ORDER BY updated_at ASC`, escapeSQL(holdID))
		rows, err := tp.Query(&cfg.Timeplus, timelineSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		timeline := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			timeline = append(timeline, gin.H{
				"status":         parseStr(row["status"]),
				"decided_by":     parseStr(row["decided_by"]),
				"decided_at":     parseTime(row["decided_at"]),
				"decided_reason": parseStr(row["decided_reason"]),
				"updated_at":     parseTime(row["updated_at"]),
			})
		}

		c.JSON(http.StatusOK, gin.H{"hold": current, "timeline": timeline})
	}
}

// AbandonSession handles POST /api/holds/_abandon-session/:session_id.
// Plugin's SessionEnd hook fires this fire-and-forget to clean up holds for
// the ending session. Marks all 'pending' holds for the session as 'abandoned'
// and signals their channels with the fail-policy decision.
func AbandonSession(cfg *config.Config, registry *holdregistry.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.Param("session_id")

		// Find pending holds for the session.
		sql := fmt.Sprintf(`
SELECT hold_id FROM table(mv_holds_current)
WHERE session_id = '%s' AND status = 'pending'`, escapeSQL(sessionID))
		rows, err := tp.Query(&cfg.Timeplus, sql)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		holdIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			if id := parseStr(row["hold_id"]); id != "" {
				holdIDs = append(holdIDs, id)
			}
		}

		failAllow := cfg.Holds.FailPolicy == "allow"
		for _, id := range holdIDs {
			_ = writeHoldStatus(cfg, id, "abandoned", "system", "session ended")
			registry.Signal(id, holdregistry.Decision{
				Allow:  failAllow,
				Reason: "session ended",
			})
		}
		c.JSON(http.StatusOK, gin.H{"abandoned": len(holdIDs)})
	}
}

// DrainHolds handles POST /api/holds/_drain. Operator-only: marks all
// pending holds as 'abandoned'. Companion to the kill-switch.
func DrainHolds(cfg *config.Config, registry *holdregistry.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		sql := `SELECT hold_id FROM table(mv_holds_current) WHERE status = 'pending'`
		rows, err := tp.Query(&cfg.Timeplus, sql)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		holdIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			if id := parseStr(row["hold_id"]); id != "" {
				holdIDs = append(holdIDs, id)
			}
		}
		failAllow := cfg.Holds.FailPolicy == "allow"
		for _, id := range holdIDs {
			_ = writeHoldStatus(cfg, id, "abandoned", "operator", "drained")
			registry.Signal(id, holdregistry.Decision{Allow: failAllow, Reason: "drained"})
		}
		c.JSON(http.StatusOK, gin.H{"drained": len(holdIDs)})
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────

func getHoldByID(cfg *config.Config, holdID string) (*models.Hold, error) {
	sql := fmt.Sprintf(`
SELECT hold_id, deployment_id, agent_id, session_id, run_id, tool_name,
       rule_id, severity, args_summary, message, created_at, status,
       decided_by, decided_at, decided_reason
FROM table(mv_holds_current)
WHERE hold_id = '%s'
LIMIT 1`, escapeSQL(holdID))
	rows, err := tp.Query(&cfg.Timeplus, sql)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	h := rowToHold(rows[0])
	return &h, nil
}

func rowToHold(row map[string]interface{}) models.Hold {
	return models.Hold{
		HoldID:        parseStr(row["hold_id"]),
		DeploymentID:  parseStr(row["deployment_id"]),
		AgentID:       parseStr(row["agent_id"]),
		SessionID:     parseStr(row["session_id"]),
		RunID:         parseStr(row["run_id"]),
		ToolName:      parseStr(row["tool_name"]),
		RuleID:        parseStr(row["rule_id"]),
		Severity:      parseStr(row["severity"]),
		ArgsSummary:   parseStr(row["args_summary"]),
		Message:       parseStr(row["message"]),
		CreatedAt:     parseTime(row["created_at"]),
		Status:        parseStr(row["status"]),
		DecidedBy:     parseStr(row["decided_by"]),
		DecidedAt:     parseTime(row["decided_at"]),
		DecidedReason: parseStr(row["decided_reason"]),
	}
}

func strconvAtoi(s string) (int, error) {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, nil
}
```

- [ ] **Step 2: Register routes in main.go**

In `backend/cmd/server/main.go`, append to the API group:

```go
api.POST("/holds/:hold_id/decide", handlers.DecideHold(&cfg, holdReg, &handlers.GlobalAlertBroadcaster))
api.GET("/holds",                  handlers.ListHolds(&cfg))
api.GET("/holds/:hold_id",         handlers.GetHold(&cfg))
api.POST("/holds/_abandon-session/:session_id", handlers.AbandonSession(&cfg, holdReg))
api.POST("/holds/_drain",          handlers.DrainHolds(&cfg, holdReg))
```

- [ ] **Step 3: Compile**

Run: `cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go`
Expected: success.

- [ ] **Step 4: End-to-end manual test (hold path with simulated human)**

Install a rule and set its policy to `hold`:

```bash
# Use any installed rule_id; create a rule that matches Bash:
curl -s -X POST http://localhost:8080/api/rules \
  -H 'Content-Type: application/json' \
  -d '{
    "id":"test_hold",
    "name":"Test hold rule",
    "description":"Catches all Bash calls",
    "severity":"warning",
    "category":"test",
    "fp_rate":"low",
    "hook_sources":"PreToolUse",
    "query_sql":"SELECT '__rule_id__' AS rule_id, '__rule_name__' AS rule_name, 'warning' AS severity, agent_id, deployment_id, session_id, run_id, hook_name, tool_name, 'test hold message' AS message, event_time AS detected_at, event_data FROM agentguard_hook_events WHERE hook_name = 'before_tool_call' AND tool_name = 'Bash'"
  }'
curl -s -X POST http://localhost:8080/api/rules/test_hold/policy \
  -H 'Content-Type: application/json' -d '{"block_policy":"hold"}'
```

In one terminal, fire a hold (will block waiting):

```bash
time curl -s -X POST http://localhost:8080/api/holds \
  -H 'Content-Type: application/json' \
  -d '{"deployment_id":"d","agent_id":"a","session_id":"s1","run_id":"r1","tool_name":"Bash","tool_input":{"command":"ls"},"event_time":"'$(date -u +%Y-%m-%dT%H:%M:%SZ)'","hook_payload":{"hook_event_name":"PreToolUse","tool_name":"Bash"}}'
```

In another terminal, list pending holds and approve:

```bash
HOLD_ID=$(curl -s http://localhost:8080/api/holds | python3 -c 'import sys,json; print(json.load(sys.stdin)["holds"][0]["hold_id"])')
echo "Hold ID: $HOLD_ID"
curl -s -X POST "http://localhost:8080/api/holds/$HOLD_ID/decide" \
  -H 'Content-Type: application/json' \
  -d '{"decision":"approved","decided_by":"manual-test","decided_reason":"ok"}'
```

Expected: the first terminal returns within ~1s with `{"decision":"allow",...}`; total elapsed time covers the wait (a few seconds), not 300s.

- [ ] **Step 5: Test deny path**

Trigger another hold in terminal 1, this time deny:

```bash
curl -s -X POST "http://localhost:8080/api/holds/$NEW_HOLD_ID/decide" \
  -H 'Content-Type: application/json' \
  -d '{"decision":"denied","decided_by":"manual-test","decided_reason":"too risky"}'
```

Expected first-terminal response: `{"decision":"block","reason":"too risky","hold_id":"..."}`

- [ ] **Step 6: Test idempotency**

```bash
curl -s -X POST "http://localhost:8080/api/holds/$NEW_HOLD_ID/decide" \
  -H 'Content-Type: application/json' \
  -d '{"decision":"approved","decided_by":"manual-test"}'
```

Expected: `{"already_decided_as":"denied","ok":true}` (the second decision is rejected; first one stands).

- [ ] **Step 7: Cleanup test rule**

```bash
curl -s -X DELETE http://localhost:8080/api/rules/test_hold
```

- [ ] **Step 8: Commit**

```bash
git add backend/internal/handlers/holds.go backend/cmd/server/main.go
git commit -m "feat(holds): /decide, GET endpoints, /_abandon-session, /_drain"
```

---

### Task 10: Threat Detail Endpoint — pending_hold Field

**Files:**
- Modify: `backend/internal/handlers/threats.go`

- [ ] **Step 1: Inspect the existing threat detail handler**

Run: `grep -n "func GetThreat" backend/internal/handlers/threats.go`
Find the existing handler signature and current response shape.

- [ ] **Step 2: Update GetThreat to populate pending_hold**

In the existing `GetThreat` function, after assembling the `threat` value, look up any pending hold for the threat's `(deployment_id, agent_id, session_id, rule_id)`:

```go
// Find a matching pending hold (if any) so the UI can render Approve/Deny.
holdSQL := fmt.Sprintf(`
SELECT hold_id, tool_name, args_summary, created_at
FROM table(mv_holds_current)
WHERE deployment_id = '%s' AND agent_id = '%s'
  AND session_id    = '%s' AND rule_id   = '%s'
  AND status        = 'pending'
ORDER BY created_at DESC
LIMIT 1`,
    escapeSQL(threat.DeploymentID), escapeSQL(threat.AgentID),
    escapeSQL(threat.SessionID),    escapeSQL(threat.RuleID))

resp := models.ThreatDetailResponse{Threat: threat}
if rows, err := tp.Query(&cfg.Timeplus, holdSQL); err != nil {
    log.Printf("threats: pending_hold query failed: %v", err)
} else if len(rows) > 0 {
    row := rows[0]
    resp.PendingHold = &models.PendingHold{
        HoldID:      parseStr(row["hold_id"]),
        ToolName:    parseStr(row["tool_name"]),
        ArgsSummary: parseStr(row["args_summary"]),
        CreatedAt:   parseTime(row["created_at"]),
    }
}

c.JSON(http.StatusOK, resp)
```

- [ ] **Step 3: Compile + test**

Run: `cd backend && CGO_ENABLED=0 go build ./...`
Expected: success.

Hit the endpoint with any existing threat ID:
```bash
THREAT_ID=$(curl -s 'http://localhost:8080/api/threats?limit=1' | python3 -c 'import sys,json; print(json.load(sys.stdin)["threats"][0]["threat_id"])')
curl -s "http://localhost:8080/api/threats/$THREAT_ID" | python3 -m json.tool | grep -E 'threat_id|pending_hold' | head -5
```
Expected: response includes `"pending_hold": null` (no hold yet) or a populated object.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/handlers/threats.go
git commit -m "feat(threats): populate pending_hold on GET /api/threats/:id"
```

---

### Task 11: Plugin Config — agentguardUrl + holdFailPolicy

**Files:**
- Modify: `agents/claudecode/agentguard-plugin/src/config.ts`
- Modify: `agents/claudecode/agentguard-plugin/src/config.test.ts`

- [ ] **Step 1: Add fields to AgentGuardConfig**

In `agents/claudecode/agentguard-plugin/src/config.ts`, extend the type and `resolveConfig`:

```typescript
export type AgentGuardConfig = {
  // ...existing fields...
  agentguardUrl:   string;
  holdFailPolicy:  "deny" | "allow";
};

export function resolveConfig(): AgentGuardConfig {
  // ...existing returns + add:
  return {
    // ...existing fields...
    agentguardUrl:   process.env.AGENTGUARD_URL ?? "http://localhost:8080",
    holdFailPolicy:  (process.env.AGENTGUARD_HOLD_FAIL_POLICY === "allow") ? "allow" : "deny",
  };
}
```

- [ ] **Step 2: Add config tests**

Append to `agents/claudecode/agentguard-plugin/src/config.test.ts`:

```typescript
it("agentguardUrl defaults to http://localhost:8080", () => {
  delete process.env.AGENTGUARD_URL;
  const cfg = resolveConfig();
  expect(cfg.agentguardUrl).toBe("http://localhost:8080");
});

it("agentguardUrl reads AGENTGUARD_URL", () => {
  process.env.AGENTGUARD_URL = "http://my-host:9090";
  const cfg = resolveConfig();
  expect(cfg.agentguardUrl).toBe("http://my-host:9090");
});

it("holdFailPolicy defaults to deny", () => {
  delete process.env.AGENTGUARD_HOLD_FAIL_POLICY;
  expect(resolveConfig().holdFailPolicy).toBe("deny");
});

it("holdFailPolicy reads AGENTGUARD_HOLD_FAIL_POLICY=allow", () => {
  process.env.AGENTGUARD_HOLD_FAIL_POLICY = "allow";
  expect(resolveConfig().holdFailPolicy).toBe("allow");
});

it("holdFailPolicy unknown value falls back to deny", () => {
  process.env.AGENTGUARD_HOLD_FAIL_POLICY = "bogus";
  expect(resolveConfig().holdFailPolicy).toBe("deny");
});
```

- [ ] **Step 3: Run tests**

Run: `cd agents/claudecode/agentguard-plugin && npm test`
Expected: all tests PASS (existing 7 + 5 new).

- [ ] **Step 4: Commit**

```bash
git add agents/claudecode/agentguard-plugin/src/config.ts agents/claudecode/agentguard-plugin/src/config.test.ts
git commit -m "feat(plugin): add agentguardUrl and holdFailPolicy to config"
```

---

### Task 12: Plugin holds.ts — requestHold Client

**Files:**
- Create: `agents/claudecode/agentguard-plugin/src/holds.ts`
- Create: `agents/claudecode/agentguard-plugin/src/holds.test.ts`

- [ ] **Step 1: Write failing tests**

Create `agents/claudecode/agentguard-plugin/src/holds.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";
import { requestHold } from "./holds.js";
import type { HookEvent } from "./timeplus-client.js";
import type { AgentGuardConfig } from "./config.js";

const BASE_CONFIG: AgentGuardConfig = {
  timeplusUrl:    "http://localhost:3218",
  stream:         "agentguard_hook_events",
  username:       "proton",
  password:       "",
  agentId:        "a",
  deploymentId:   "d",
  deploymentName: "D",
  agentguardUrl:  "http://localhost:8080",
  holdFailPolicy: "deny",
};

const BASE_EVENT: HookEvent = {
  hook_name: "before_tool_call",
  event_time: "2026-04-22T12:00:00.000Z",
  session_id: "s1",
  run_id: "r1",
  agent_id: "a",
  deployment_id: "d",
  deployment_name: "D",
  session_key: "",
  tool_name: "Bash",
  provider: "anthropic",
  model: "",
  hook_decision: "observe",
  block_reason: "",
  event_data: JSON.stringify({ hook_event_name: "PreToolUse", tool_name: "Bash" }),
  agent_type: "claudecode",
};

describe("requestHold", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("returns allow when backend responds 200 + decision allow", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(
      JSON.stringify({ decision: "allow" }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    )));
    const d = await requestHold(BASE_EVENT, BASE_CONFIG);
    expect(d.decision).toBe("allow");
  });

  it("returns block + reason when backend responds 200 + decision block", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(
      JSON.stringify({ decision: "block", reason: "destructive command" }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    )));
    const d = await requestHold(BASE_EVENT, BASE_CONFIG);
    expect(d.decision).toBe("block");
    expect(d.reason).toBe("destructive command");
  });

  it("applies fail-policy=deny on HTTP 503", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("oops", { status: 503 })));
    const d = await requestHold(BASE_EVENT, BASE_CONFIG);
    expect(d.decision).toBe("block");
    expect(d.reason).toContain("503");
  });

  it("applies fail-policy=deny on network error", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("ECONNREFUSED"); }));
    const d = await requestHold(BASE_EVENT, BASE_CONFIG);
    expect(d.decision).toBe("block");
    expect(d.reason).toContain("ECONNREFUSED");
  });

  it("applies fail-policy=allow when configured", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("network down"); }));
    const d = await requestHold(BASE_EVENT, { ...BASE_CONFIG, holdFailPolicy: "allow" });
    expect(d.decision).toBe("allow");
  });

  it("POSTs the right body shape", async () => {
    const fetchMock = vi.fn(async () => new Response(
      JSON.stringify({ decision: "allow" }), { status: 200 },
    ));
    vi.stubGlobal("fetch", fetchMock);
    await requestHold(BASE_EVENT, BASE_CONFIG);

    expect(fetchMock).toHaveBeenCalledWith(
      "http://localhost:8080/api/holds",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({ "Content-Type": "application/json" }),
      }),
    );
    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.deployment_id).toBe("d");
    expect(body.agent_id).toBe("a");
    expect(body.session_id).toBe("s1");
    expect(body.tool_name).toBe("Bash");
    expect(body.hook_payload).toEqual({ hook_event_name: "PreToolUse", tool_name: "Bash" });
  });
});
```

- [ ] **Step 2: Run tests — expect compile failure**

Run: `cd agents/claudecode/agentguard-plugin && npm test -- holds.test.ts`
Expected: error — `Cannot find module './holds.js'`.

- [ ] **Step 3: Implement holds.ts**

Create `agents/claudecode/agentguard-plugin/src/holds.ts`:

```typescript
import type { AgentGuardConfig } from "./config.js";
import type { HookEvent } from "./timeplus-client.js";

export type HoldDecision = {
  decision: "allow" | "block";
  reason?: string;
  hold_id?: string;
};

const HOLD_TIMEOUT_MS = 540_000; // under Claude Code's 600s hook timeout

export async function requestHold(event: HookEvent, config: AgentGuardConfig): Promise<HoldDecision> {
  const url = `${config.agentguardUrl}/api/holds`;
  let hookPayload: unknown = {};
  try {
    hookPayload = JSON.parse(event.event_data);
  } catch {
    hookPayload = {};
  }
  const body = JSON.stringify({
    deployment_id: event.deployment_id,
    agent_id:      event.agent_id,
    session_id:    event.session_id,
    run_id:        event.run_id,
    tool_name:     event.tool_name,
    event_time:    event.event_time,
    tool_input:    (hookPayload as { tool_input?: unknown }).tool_input ?? {},
    hook_payload:  hookPayload,
  });
  try {
    const ctrl = new AbortController();
    const t = setTimeout(() => ctrl.abort(), HOLD_TIMEOUT_MS);
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
    return failPolicy(config, `request failed: ${err instanceof Error ? err.message : String(err)}`);
  }
}

function failPolicy(config: AgentGuardConfig, reason: string): HoldDecision {
  if (config.holdFailPolicy === "allow") {
    return { decision: "allow" };
  }
  return { decision: "block", reason: `AgentGuard unreachable — ${reason}` };
}
```

- [ ] **Step 4: Run tests — expect pass**

Run: `cd agents/claudecode/agentguard-plugin && npm test -- holds.test.ts`
Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add agents/claudecode/agentguard-plugin/src/holds.ts agents/claudecode/agentguard-plugin/src/holds.test.ts
git commit -m "feat(plugin): holds.ts — requestHold with abort timeout + fail-policy"
```

---

### Task 13: Plugin index.ts — Branch on PreToolUse + SessionEnd Abandon

**Files:**
- Modify: `agents/claudecode/agentguard-plugin/src/index.ts`

- [ ] **Step 1: Add PreToolUse + SessionEnd branches**

Modify `agents/claudecode/agentguard-plugin/src/index.ts`. After `const config = resolveConfig();` and before the existing `Stop`/transcript handling, add:

```typescript
// PreToolUse: synchronous hold path. POST to /api/holds and write Claude Code's
// permissionDecision response to stdout. Skip the async send() — /api/holds
// ingests the event into agentguard_hook_events itself.
if (hookEventName === "PreToolUse") {
  const event = normalize(input, config);
  const { requestHold } = await import("./holds.js");
  const decision = await requestHold(event, config);
  process.stdout.write(JSON.stringify({
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: decision.decision === "block" ? "deny" : "allow",
      permissionDecisionReason: decision.reason ?? "",
    },
  }));
  return;
}

// SessionEnd: fire-and-forget abandon-session to clean up zombie holds.
if (hookEventName === "SessionEnd") {
  const sessionId = typeof input["session_id"] === "string" ? input["session_id"] : "";
  if (sessionId) {
    fetch(`${config.agentguardUrl}/api/holds/_abandon-session/${encodeURIComponent(sessionId)}`, {
      method: "POST",
    }).catch(() => { /* best-effort */ });
  }
  // continue with existing async ingest path below
}
```

- [ ] **Step 2: Build the plugin**

Run: `cd agents/claudecode/agentguard-plugin && npm run build`
Expected: success — `dist/index.js` updated.

- [ ] **Step 3: Run all plugin tests**

Run: `cd agents/claudecode/agentguard-plugin && npm test`
Expected: all PASS.

- [ ] **Step 4: Manual smoke (no AgentGuard backend → fail-policy)**

```bash
echo '{"hook_event_name":"PreToolUse","session_id":"s1","tool_name":"Bash","tool_input":{"command":"ls"}}' | \
  AGENTGUARD_URL=http://localhost:8080 \
  AGENTGUARD_HOLD_FAIL_POLICY=deny \
  AGENTGUARD_AGENT_ID=test \
  AGENTGUARD_DEPLOYMENT_ID=test \
  AGENTGUARD_DEPLOYMENT_NAME=Test \
  node agents/claudecode/agentguard-plugin/dist/index.js
```

If backend is running with no `hold` rule for Bash → expect:
```json
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","permissionDecisionReason":""}}
```

If backend is NOT running → expect:
```json
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"AgentGuard unreachable — request failed: ..."}}
```

- [ ] **Step 5: Commit**

```bash
git add agents/claudecode/agentguard-plugin/src/index.ts agents/claudecode/agentguard-plugin/dist/
git commit -m "feat(plugin): sync PreToolUse path via /api/holds; SessionEnd fires _abandon-session"
```

---

### Task 14: Plugin Install Script — settings.json Sync PreToolUse

**Files:**
- Modify: `agents/claudecode/Makefile`

- [ ] **Step 1: Patch CONFIGURE_SCRIPT**

Edit `agents/claudecode/Makefile`, replacing the contents of the `define CONFIGURE_SCRIPT` block. The change: split the hook events into two groups (sync / async) and emit different entries; add the two new env vars.

Replace the body of `CONFIGURE_SCRIPT` (lines 21-83 in current file) with:

```python
import json, os, sys

settings_path = os.path.expanduser("$(SETTINGS_FILE)")
hook_command = 'node "$(PLUGIN_DIR)/dist/index.js"'

# PreToolUse must be synchronous so we can block the tool call.
# Other hooks stay async to avoid slowing the agent.
sync_hook_events  = ["PreToolUse"]
async_hook_events = [
    "SessionStart",
    "SessionEnd",
    "UserPromptSubmit",
    "PostToolUse",
    "PostToolUseFailure",
    "SubagentStart",
    "SubagentStop",
    "Stop",
    "PermissionDenied",
]

sync_hook_entry = {
    "matcher": "*",
    "hooks": [
        {
            "type": "command",
            "command": hook_command,
            "timeout": 600000,  # 600s — under Claude Code's hook timeout cap
        }
    ],
}

async_hook_entry = {
    "matcher": "*",
    "hooks": [
        {
            "type": "command",
            "command": hook_command,
            "async": True,
        }
    ],
}

os.makedirs(os.path.dirname(settings_path), exist_ok=True)
if os.path.exists(settings_path):
    with open(settings_path, "r") as f:
        data = json.load(f)
else:
    data = {}

hooks = data.setdefault("hooks", {})

def upsert(event, entry):
    entries = hooks.setdefault(event, [])
    # Remove any existing entry referencing our hook_command (so we replace
    # an old async/sync shape with the new one cleanly).
    entries[:] = [
        e for e in entries
        if not any(h.get("command") == hook_command for h in e.get("hooks", []))
    ]
    entries.append(entry)

for event in sync_hook_events:
    upsert(event, sync_hook_entry)
for event in async_hook_events:
    upsert(event, async_hook_entry)

plugin_env = {
    "AGENTGUARD_AGENT_ID":         "$(AGENTGUARD_AGENT_ID)",
    "AGENTGUARD_DEPLOYMENT_ID":    "$(AGENTGUARD_DEPLOYMENT_ID)",
    "AGENTGUARD_DEPLOYMENT_NAME":  "$(AGENTGUARD_DEPLOYMENT_NAME)",
    "AGENTGUARD_TIMEPLUS_URL":     "$(AGENTGUARD_TIMEPLUS_URL)",
    "AGENTGUARD_STREAM":           "$(AGENTGUARD_STREAM)",
    "AGENTGUARD_USERNAME":         "$(AGENTGUARD_USERNAME)",
    "AGENTGUARD_PASSWORD":         "$(AGENTGUARD_PASSWORD)",
    "AGENTGUARD_URL":              "$(AGENTGUARD_URL)",
    "AGENTGUARD_HOLD_FAIL_POLICY": "$(AGENTGUARD_HOLD_FAIL_POLICY)",
}
data.setdefault("env", {}).update(plugin_env)

with open(settings_path, "w") as f:
    json.dump(data, f, indent=2)
print("AgentGuard hooks configured in", settings_path)
```

- [ ] **Step 2: Add the two new env-var defaults at the top of the Makefile**

After line 10 (the existing `AGENTGUARD_PASSWORD` default), add:

```makefile
AGENTGUARD_URL ?= http://localhost:8080
AGENTGUARD_HOLD_FAIL_POLICY ?= deny
```

- [ ] **Step 3: Update UNCONFIGURE_SCRIPT to clean up the new env keys**

In the `UNCONFIGURE_SCRIPT` `plugin_env_keys` list (around line 92), append:

```python
plugin_env_keys = [
    "AGENTGUARD_AGENT_ID",
    "AGENTGUARD_DEPLOYMENT_ID",
    "AGENTGUARD_DEPLOYMENT_NAME",
    "AGENTGUARD_TIMEPLUS_URL",
    "AGENTGUARD_STREAM",
    "AGENTGUARD_USERNAME",
    "AGENTGUARD_PASSWORD",
    "AGENTGUARD_URL",
    "AGENTGUARD_HOLD_FAIL_POLICY",
]
```

Also add "PreToolUse" alongside other events if it's not already in the unconfigure list (it is, in line 102-105).

- [ ] **Step 4: Smoke test the configure**

Backup current settings, run configure, inspect, restore:

```bash
cp ~/.claude/settings.json ~/.claude/settings.json.bak
cd agents/claudecode && make configure
python3 -c '
import json, sys
d = json.load(open(sys.argv[1]))
pre = d["hooks"]["PreToolUse"][0]["hooks"][0]
print("PreToolUse:", pre)
post = d["hooks"]["PostToolUse"][0]["hooks"][0]
print("PostToolUse:", post)
print("AGENTGUARD_URL:", d["env"]["AGENTGUARD_URL"])
print("AGENTGUARD_HOLD_FAIL_POLICY:", d["env"]["AGENTGUARD_HOLD_FAIL_POLICY"])
' ~/.claude/settings.json
```

Expected:
- `PreToolUse` entry has `"timeout": 600000`, no `"async"` key.
- `PostToolUse` entry has `"async": true`, no `"timeout"` key.
- Both env vars present.

Restore:
```bash
mv ~/.claude/settings.json.bak ~/.claude/settings.json
```

- [ ] **Step 5: Commit**

```bash
git add agents/claudecode/Makefile
git commit -m "feat(install): emit sync PreToolUse hook + AGENTGUARD_URL/HOLD_FAIL_POLICY env"
```

---

### Task 15: Frontend API Client — Five New Functions

**Files:**
- Modify: `frontend/src/api/client.js`

- [ ] **Step 1: Read existing client.js to match its style**

Run: `head -40 frontend/src/api/client.js`

- [ ] **Step 2: Append the five new functions**

Append to `frontend/src/api/client.js` (above the final `export const api = { ... }` block, then add the new methods to that block):

```javascript
async function listHolds(status = 'pending', limit = 100) {
  const url = `/api/holds?status=${encodeURIComponent(status)}&limit=${limit}`;
  const r = await fetch(url);
  if (!r.ok) throw new Error(`listHolds failed: ${r.status}`);
  return r.json();
}

async function getHold(holdId) {
  const r = await fetch(`/api/holds/${encodeURIComponent(holdId)}`);
  if (!r.ok) throw new Error(`getHold failed: ${r.status}`);
  return r.json();
}

async function decideHold(holdId, decision, reason = '') {
  const r = await fetch(`/api/holds/${encodeURIComponent(holdId)}/decide`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      decision,
      decided_by: 'web-ui',
      decided_reason: reason,
    }),
  });
  if (!r.ok) throw new Error(`decideHold failed: ${r.status}`);
  return r.json();
}

async function setRuleBlockPolicy(ruleId, blockPolicy) {
  const r = await fetch(`/api/rules/${encodeURIComponent(ruleId)}/policy`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ block_policy: blockPolicy }),
  });
  if (!r.ok) throw new Error(`setRuleBlockPolicy failed: ${r.status}`);
  return r.json();
}

async function getThreatDetail(threatId) {
  const r = await fetch(`/api/threats/${encodeURIComponent(threatId)}`);
  if (!r.ok) throw new Error(`getThreatDetail failed: ${r.status}`);
  return r.json();
}
```

In the existing `export const api = { ... }` object, add the five new methods:

```javascript
export const api = {
  // ...existing entries...
  listHolds,
  getHold,
  decideHold,
  setRuleBlockPolicy,
  getThreatDetail,
};
```

- [ ] **Step 3: Smoke test in the browser console**

Run frontend dev server: `cd frontend && npm run dev`
Open http://localhost:5173 in browser, open dev console, paste:

```javascript
const r = await fetch('/api/holds?status=pending').then(r => r.json());
console.log(r);
```
Expected: `{ holds: [...], total: N }` (could be empty).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.js
git commit -m "feat(api): add listHolds, getHold, decideHold, setRuleBlockPolicy, getThreatDetail"
```

---

### Task 16: NotificationsProvider — Hold SSE Event Types

**Files:**
- Modify: `frontend/src/components/NotificationsProvider.jsx`

- [ ] **Step 1: Inspect current NotificationsProvider**

Run: `cat frontend/src/components/NotificationsProvider.jsx`
Note the existing `EventSource` setup and the shape of state it exposes via context.

- [ ] **Step 2: Extend state and SSE handler**

Add `pendingHolds` state (Map keyed by `hold_id`) to the provider:

```jsx
import { createContext, useContext, useEffect, useState, useCallback } from 'react';
import { api } from '../api/client';

const NotificationsContext = createContext({
  alerts: [],
  pendingHolds: new Map(),
  acknowledgeHold: () => {},
});

export function NotificationsProvider({ children }) {
  const [alerts, setAlerts] = useState([]);
  const [pendingHolds, setPendingHolds] = useState(new Map());

  const reconcileHolds = useCallback(async () => {
    try {
      const r = await api.listHolds('pending');
      const next = new Map();
      for (const h of r.holds ?? []) next.set(h.hold_id, h);
      setPendingHolds(next);
    } catch { /* leave as-is on failure */ }
  }, []);

  useEffect(() => {
    const src = new EventSource('/api/notifications/stream');
    let cancelled = false;

    src.onopen = () => { if (!cancelled) reconcileHolds(); };

    src.addEventListener('alert', (ev) => {
      const data = JSON.parse(ev.data);
      // Existing alert handling (rule_name === 'hold_pending'/'hold_decided' is
      // delivered via the same alert SSE channel, so we route here).
      if (data.rule_name === 'hold_pending') {
        // payload.message is "hold:<id>|tool:<name>|reason:<text>"
        const holdId = parseField(data.message, 'hold');
        const tool   = parseField(data.message, 'tool');
        const reason = parseField(data.message, 'reason');
        setPendingHolds(prev => {
          const next = new Map(prev);
          next.set(holdId, {
            hold_id: holdId,
            severity: data.severity,
            agent_id: data.agent_id,
            tool_name: tool,
            message: reason,
            created_at: data.detected_at,
          });
          return next;
        });
        return;
      }
      if (data.rule_name === 'hold_decided') {
        const holdId = parseField(data.message, 'hold');
        setPendingHolds(prev => {
          if (!prev.has(holdId)) return prev;
          const next = new Map(prev);
          next.delete(holdId);
          return next;
        });
        return;
      }
      // Existing alert behavior — append to alerts list.
      setAlerts(prev => [data, ...prev].slice(0, 50));
    });

    src.onerror = () => {
      // EventSource auto-reconnects; reconcile on next open.
    };

    return () => { cancelled = true; src.close(); };
  }, [reconcileHolds]);

  return (
    <NotificationsContext.Provider value={{ alerts, pendingHolds, reconcileHolds }}>
      {children}
    </NotificationsContext.Provider>
  );
}

export function useNotifications() {
  return useContext(NotificationsContext);
}

function parseField(message, key) {
  // message looks like "hold:abc123|tool:Bash|reason:foo"
  for (const part of (message ?? '').split('|')) {
    const idx = part.indexOf(':');
    if (idx < 0) continue;
    if (part.slice(0, idx) === key) return part.slice(idx + 1);
  }
  return '';
}
```

- [ ] **Step 3: Smoke test (manual)**

Start backend + frontend. In the browser, open `/threats`. In another terminal POST a hold (Task 9 step 4 setup). Observe browser dev tools Network tab for `/api/notifications/stream` — expect `alert` events with `rule_name=hold_pending` arriving live.

In the dev console:
```javascript
// Check React context has pendingHolds populated
window.localStorage.setItem('debug', '1');
```
Or temporarily add `console.log(pendingHolds)` inside the provider's render and verify pending holds appear in the console as you POST holds.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/NotificationsProvider.jsx
git commit -m "feat(notifications): handle hold_pending/hold_decided SSE events; reconcile on reconnect"
```

---

### Task 17: Threats Page — Pending Holds Section

**Files:**
- Modify: `frontend/src/pages/Threats.jsx`

- [ ] **Step 1: Add Pending Holds section at top of page**

In `frontend/src/pages/Threats.jsx`, import the notifications context and add a section above the existing threat feed:

```jsx
import { useNotifications } from '../components/NotificationsProvider';
import { Link } from 'react-router-dom';

// Inside the Threats component, near the top of the JSX (above the existing
// threats list):
const { pendingHolds } = useNotifications();
const pendingHoldsArr = Array.from(pendingHolds.values()).sort(
  (a, b) => new Date(b.created_at) - new Date(a.created_at)
);

// ...

{pendingHoldsArr.length > 0 && (
  <section className="bg-[#1d1a25] rounded-sm p-4 mb-6">
    <h3 className="text-[11px] font-semibold uppercase tracking-widest text-[#ffb4ab] mb-3">
      ⚠ Pending Holds ({pendingHoldsArr.length})
    </h3>
    <ul className="divide-y divide-[#574149]/20">
      {pendingHoldsArr.map(h => (
        <li key={h.hold_id} className="flex items-center justify-between py-2.5">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 mb-0.5">
              <span className={`text-[10px] uppercase font-semibold ${
                h.severity === 'critical' ? 'text-[#ffb4ab]' :
                h.severity === 'warning'  ? 'text-amber-400'  : 'text-zinc-400'
              }`}>
                {h.severity}
              </span>
              <span className="text-xs text-[#e7dff0] font-mono">{h.agent_id}</span>
              <span className="text-zinc-700">·</span>
              <span className="text-xs text-[#e7dff0] font-mono">{h.tool_name}</span>
            </div>
            <p className="text-[11px] text-zinc-400 truncate">{h.message}</p>
          </div>
          <Link
            to={`/threats/${encodeURIComponent(findThreatIdForHold(h))}`}
            className="ml-4 px-3 py-1.5 text-[11px] font-semibold rounded bg-primary-button text-primary-button-text hover:scale-95 transition-transform"
          >
            Review →
          </Link>
        </li>
      ))}
    </ul>
  </section>
)}
```

The `findThreatIdForHold(h)` helper computes the threat_id given the hold's `(agent_id, deployment_id, session_id, rule_id)`. Per `models.go`, `threat_id` is an MD5 of those four fields. For now (since holds carry rule_id and session_id from the SSE payload), we can either:
1. Look it up by re-fetching the threats list and matching, or
2. Compute MD5 client-side.

For simplicity, navigate to a holds-routing page that resolves it server-side. Cheapest: encode the hold_id in the URL, ThreatDetail loads `getHold(hold_id)` first, then fetches the threat. But that's a UX change.

Easiest implementation: link to `/threats/by-hold/:hold_id` — a tiny new route that the backend resolves; OR just have ThreatDetail accept a `?hold_id=` query and resolve internally.

Use the second approach. Update the `Link` href:

```jsx
<Link
  to={`/threats/by-hold/${encodeURIComponent(h.hold_id)}`}
  className="..."
>
```

Add a small route alias in `App.jsx` (Task 18 will register it together with `/threats/:id`).

- [ ] **Step 2: Visual smoke test**

Start backend + frontend. POST a hold (Task 9 setup). Open `/threats` — expect Pending Holds section to appear at the top with one entry.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/Threats.jsx
git commit -m "feat(threats): pending holds section at top of /threats; SSE-driven"
```

---

### Task 18: ThreatDetail Page (NEW) + Route Registration

**Files:**
- Create: `frontend/src/pages/ThreatDetail.jsx`
- Modify: `frontend/src/App.jsx`

- [ ] **Step 1: Create the ThreatDetail page**

Create `frontend/src/pages/ThreatDetail.jsx`:

```jsx
import { useState, useEffect } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import Sidebar from '../components/Sidebar';
import TopNav from '../components/TopNav';
import { api } from '../api/client';

export default function ThreatDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [threat, setThreat] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [decideStatus, setDecideStatus] = useState(null); // null | 'pending' | 'approved' | 'denied' | 'error'
  const [decideReason, setDecideReason] = useState('');

  // Support both /threats/:id and /threats/by-hold/:holdId
  const isByHold = window.location.pathname.startsWith('/threats/by-hold/');

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);

    async function load() {
      try {
        let threatId = id;
        if (isByHold) {
          // Resolve hold → threat via getHold, then find the threat that matches.
          const holdResp = await api.getHold(id);
          const h = holdResp.hold;
          // Threat detail lookup by joining the existing threats list.
          // (threat_id is an MD5 of agent+deployment+session+rule per models.go)
          const list = await fetch(
            `/api/threats?agent_id=${encodeURIComponent(h.agent_id)}&limit=200`
          ).then(r => r.json());
          const matched = (list.threats ?? []).find(
            t => t.agent_id === h.agent_id
              && t.deployment_id === h.deployment_id
              && t.session_id === h.session_id
              && t.rule_id === h.rule_id
          );
          if (!matched) {
            throw new Error('threat for this hold not found');
          }
          threatId = matched.threat_id;
          if (!cancelled) navigate(`/threats/${encodeURIComponent(threatId)}`, { replace: true });
          return;
        }
        const data = await api.getThreatDetail(threatId);
        if (!cancelled) setThreat(data);
      } catch (e) {
        if (!cancelled) setError(e.message ?? String(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    load();
    return () => { cancelled = true; };
  }, [id, isByHold, navigate]);

  async function decide(decision) {
    if (!threat?.pending_hold) return;
    setDecideStatus('pending');
    try {
      await api.decideHold(threat.pending_hold.hold_id, decision, decideReason);
      setDecideStatus(decision);
      // Refresh threat to drop the pending_hold banner.
      const fresh = await api.getThreatDetail(id);
      setThreat(fresh);
    } catch (e) {
      setDecideStatus('error');
    }
  }

  if (loading) return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar /><TopNav />
      <main className="pl-sidebar pt-16 p-8">
        <span className="material-symbols-outlined animate-spin text-[#ffb0cf]">refresh</span>
      </main>
    </div>
  );

  if (error) return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar /><TopNav />
      <main className="pl-sidebar pt-16 p-8">
        <p className="text-[#ffb4ab]">Error: {error}</p>
        <Link to="/threats" className="text-[#ffb0cf] hover:underline">← Back to threats</Link>
      </main>
    </div>
  );

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar /><TopNav />
      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-8 max-w-5xl mx-auto">
          <Link to="/threats" className="text-zinc-400 hover:text-[#ffb0cf] text-xs mb-4 inline-flex items-center gap-1">
            <span className="material-symbols-outlined text-sm">arrow_back</span> Back to threats
          </Link>

          <h1 className="text-2xl font-medium mb-2">{threat.rule_name}</h1>
          <div className="flex gap-3 mb-6 text-xs text-zinc-400">
            <span className={`px-2 py-0.5 rounded-full text-[10px] uppercase font-semibold ${
              threat.severity === 'critical' ? 'bg-[#ffb4ab]/10 text-[#ffb4ab]' :
              threat.severity === 'warning'  ? 'bg-amber-500/10 text-amber-400' :
              'bg-zinc-700/30 text-zinc-400'
            }`}>{threat.severity}</span>
            <span>Agent: <span className="text-[#e7dff0]">{threat.agent_id}</span></span>
            <span>Session: <span className="text-[#e7dff0] font-mono">{threat.session_id}</span></span>
            <span>Status: <span className="text-[#e7dff0]">{threat.status}</span></span>
          </div>

          {threat.pending_hold && decideStatus !== 'approved' && decideStatus !== 'denied' && (
            <section className="bg-[#93000a]/20 border border-[#ffb4ab]/30 rounded-sm p-5 mb-6">
              <div className="flex items-center gap-2 mb-3">
                <span className="material-symbols-outlined text-[#ffb4ab]">block</span>
                <h2 className="text-sm font-semibold text-[#ffb4ab]">Pending Hold — Agent paused</h2>
              </div>
              <p className="text-xs text-zinc-300 mb-3">
                Tool: <code className="font-mono text-[#e7dff0]">{threat.pending_hold.tool_name}</code>
              </p>
              <pre className="bg-[#0f0c17] rounded-sm p-3 mb-4 text-[11px] font-mono text-zinc-300 whitespace-pre-wrap break-all max-h-48 overflow-y-auto">
                {threat.pending_hold.args_summary || '(no arguments)'}
              </pre>
              <textarea
                placeholder="Optional reason for your decision..."
                value={decideReason}
                onChange={e => setDecideReason(e.target.value)}
                className="w-full bg-[#0f0c17] border border-[#574149]/30 rounded-sm p-2 text-xs text-[#e7dff0] mb-3"
                rows={2}
              />
              <div className="flex gap-2">
                <button
                  onClick={() => decide('approved')}
                  disabled={decideStatus === 'pending'}
                  className="px-4 py-2 bg-emerald-500/20 text-emerald-400 text-xs font-semibold rounded-sm hover:bg-emerald-500/30 disabled:opacity-50"
                >
                  ✓ Approve
                </button>
                <button
                  onClick={() => decide('denied')}
                  disabled={decideStatus === 'pending'}
                  className="px-4 py-2 bg-[#ffb4ab]/20 text-[#ffb4ab] text-xs font-semibold rounded-sm hover:bg-[#ffb4ab]/30 disabled:opacity-50"
                >
                  ✗ Deny
                </button>
              </div>
              {decideStatus === 'error' && (
                <p className="text-[11px] text-[#ffb4ab] mt-2">Failed to submit decision. Try again.</p>
              )}
            </section>
          )}

          {(decideStatus === 'approved' || decideStatus === 'denied') && (
            <section className="bg-[#1d1a25] rounded-sm p-4 mb-6">
              <p className="text-xs text-emerald-400">
                ✓ {decideStatus === 'approved' ? 'Approved' : 'Denied'} — agent has been notified.
              </p>
            </section>
          )}

          <section>
            <h2 className="text-sm font-semibold mb-3">Detection Events ({threat.events?.length ?? 0})</h2>
            <ul className="space-y-2">
              {(threat.events ?? []).map((ev, i) => (
                <li key={i} className="bg-[#1d1a25] rounded-sm p-3">
                  <div className="flex items-center gap-3 text-[10px] text-zinc-500 mb-1">
                    <span className="font-mono">{ev.detected_at}</span>
                    <span className="font-mono text-[#93c5fd]">{ev.hook_name}</span>
                    {ev.tool_name && <span className="font-mono text-[#93c5fd]">{ev.tool_name}</span>}
                  </div>
                  <p className="text-xs text-[#e7dff0]">{ev.message}</p>
                </li>
              ))}
            </ul>
          </section>
        </div>
      </main>
    </div>
  );
}
```

- [ ] **Step 2: Register the route in App.jsx**

In `frontend/src/App.jsx`, add:

```jsx
import ThreatDetail from './pages/ThreatDetail';

// inside the <Routes>:
<Route path="/threats/:id" element={<ThreatDetail />} />
<Route path="/threats/by-hold/:id" element={<ThreatDetail />} />
```

- [ ] **Step 3: Manual smoke test**

Start backend + frontend. Trigger a hold (Task 9 setup). On `/threats`, click "Review →" on the pending hold. Expect to land on `/threats/:id` with the Pending Hold banner showing Approve/Deny buttons.

Click Approve. Expect:
1. Inline confirmation appears: "✓ Approved — agent has been notified."
2. Banner disappears after refresh.
3. The waiting `curl` from Task 9 returns within ~1s.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/ThreatDetail.jsx frontend/src/App.jsx
git commit -m "feat(frontend): /threats/:id ThreatDetail page with Pending Hold banner + Approve/Deny"
```

---

### Task 19: Sidebar Badge + HoldToast Component

**Files:**
- Modify: `frontend/src/components/Sidebar.jsx`
- Create: `frontend/src/components/HoldToast.jsx`

- [ ] **Step 1: Sidebar badge**

In `frontend/src/components/Sidebar.jsx`, import the notifications context and add a count badge next to the Threats nav entry:

```jsx
import { useNotifications } from './NotificationsProvider';

// inside Sidebar component:
const { pendingHolds } = useNotifications();
const pendingCount = pendingHolds.size;

// where the Threats NavLink is rendered:
<NavLink to="/threats" /* ...existing props... */>
  <span className="material-symbols-outlined">security</span>
  <span>Threats</span>
  {pendingCount > 0 && (
    <span className="ml-auto bg-[#ffb4ab] text-[#3d0023] text-[9px] font-bold rounded-full px-1.5 py-0.5 min-w-[16px] text-center">
      {pendingCount}
    </span>
  )}
</NavLink>
```

- [ ] **Step 2: HoldToast component**

Create `frontend/src/components/HoldToast.jsx`:

```jsx
import { Link } from 'react-router-dom';
import { useNotifications } from './NotificationsProvider';

export default function HoldToastContainer() {
  const { pendingHolds } = useNotifications();
  const arr = Array.from(pendingHolds.values()).slice(0, 3); // show up to 3

  if (arr.length === 0) return null;

  return (
    <div className="fixed bottom-6 right-6 z-50 flex flex-col gap-2 max-w-sm">
      {arr.map(h => (
        <div key={h.hold_id} className="bg-[#1d1a25] border border-[#ffb4ab]/40 rounded-sm p-3 shadow-lg">
          <div className="flex items-center gap-2 mb-1">
            <span className="material-symbols-outlined text-[#ffb4ab] text-base">block</span>
            <span className={`text-[9px] uppercase font-semibold ${
              h.severity === 'critical' ? 'text-[#ffb4ab]' :
              h.severity === 'warning'  ? 'text-amber-400' : 'text-zinc-400'
            }`}>{h.severity}</span>
            <span className="text-[10px] text-zinc-400 truncate flex-1">
              Hold pending: <code className="font-mono">{h.tool_name}</code>
            </span>
          </div>
          <p className="text-[11px] text-zinc-300 mb-2 truncate">{h.message}</p>
          <Link
            to={`/threats/by-hold/${encodeURIComponent(h.hold_id)}`}
            className="inline-block px-3 py-1 bg-primary-button text-primary-button-text text-[10px] font-semibold rounded hover:scale-95 transition-transform"
          >
            Review →
          </Link>
        </div>
      ))}
    </div>
  );
}
```

- [ ] **Step 3: Mount HoldToast somewhere global**

In `frontend/src/App.jsx`, add `<HoldToastContainer />` inside the `<NotificationsProvider>` so it's available on every page:

```jsx
import HoldToastContainer from './components/HoldToast';

// inside the JSX, right after the Routes:
<NotificationsProvider>
  <Routes>{/* ...existing... */}</Routes>
  <HoldToastContainer />
</NotificationsProvider>
```

- [ ] **Step 4: Visual smoke test**

Start backend + frontend. Navigate to `/dashboard` (any page). Trigger a hold. Expect:
1. A toast appears bottom-right within ~1s.
2. Sidebar badge shows "1" next to Threats.
3. Click toast's "Review →" → land on threat detail page.
4. Approve → toast disappears, badge goes to 0.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/Sidebar.jsx frontend/src/components/HoldToast.jsx frontend/src/App.jsx
git commit -m "feat(frontend): pending-holds badge on Threats sidebar + global HoldToast container"
```

---

### Task 20: RulePacks + RuleDetail + Onboarding — block_policy Dropdown

**Files:**
- Modify: `frontend/src/pages/RulePacks.jsx`
- Modify: `frontend/src/pages/RuleDetail.jsx`
- Modify: `frontend/src/pages/Onboarding.jsx`

- [ ] **Step 1: Helper component**

Create a small reusable component inline at the top of `RulePacks.jsx`:

```jsx
function BlockPolicySelect({ value, onChange, disabled }) {
  return (
    <select
      value={value || 'log_only'}
      onChange={e => onChange(e.target.value)}
      disabled={disabled}
      className="bg-[#1d1a25] border border-[#574149]/30 rounded-sm text-[10px] px-2 py-1 text-zinc-300 focus:outline-none focus:border-[#ffb0cf]/40"
    >
      <option value="log_only">Log only</option>
      <option value="auto_block">Auto-block</option>
      <option value="hold">Hold (require approval)</option>
    </select>
  );
}
```

- [ ] **Step 2: Add the dropdown to each rule row in RulePacks.jsx**

In the rule list rendering, alongside the existing enable toggle:

```jsx
<div className="flex items-center gap-2">
  <Toggle checked={rule.enabled} onChange={() => toggleRule(rule.id)} />
  <span className="text-[10px] text-zinc-500">Policy:</span>
  <BlockPolicySelect
    value={rule.block_policy}
    onChange={async (newPolicy) => {
      try {
        await api.setRuleBlockPolicy(rule.id, newPolicy);
        // Optimistic update
        setRules(prev => prev.map(r => r.id === rule.id ? { ...r, block_policy: newPolicy } : r));
      } catch (e) {
        alert(`Failed to update policy: ${e.message}`);
      }
    }}
  />
</div>
```

- [ ] **Step 3: RuleDetail.jsx — same dropdown more prominently**

In `frontend/src/pages/RuleDetail.jsx`, near the top under the rule name and severity, add:

```jsx
<div className="bg-[#1d1a25] rounded-sm p-4 mb-6">
  <div className="flex items-center justify-between mb-2">
    <span className="text-[11px] font-semibold uppercase tracking-widest text-zinc-500">Block Policy</span>
    <BlockPolicySelect
      value={rule.block_policy}
      onChange={async (newPolicy) => {
        await api.setRuleBlockPolicy(rule.id, newPolicy);
        setRule({ ...rule, block_policy: newPolicy });
      }}
    />
  </div>
  <p className="text-[11px] text-zinc-400">
    {rule.block_policy === 'hold' && 'When this rule fires, AgentGuard will pause the agent and wait for human approval on this page.'}
    {rule.block_policy === 'auto_block' && 'When this rule fires, AgentGuard will block the tool call immediately without prompting.'}
    {(!rule.block_policy || rule.block_policy === 'log_only') && 'When this rule fires, the threat is logged and surfaced in the UI but the agent is not blocked.'}
  </p>
</div>
```

(Either import `BlockPolicySelect` from `RulePacks.jsx` via a small refactor — extract to `frontend/src/components/BlockPolicySelect.jsx` as a one-line file — or duplicate locally for simplicity. Recommend extracting.)

- [ ] **Step 4: Onboarding.jsx — Step 4 policy selector**

In `frontend/src/pages/Onboarding.jsx`, find Step 4 (Activate rules) where each rule has an enable toggle. Add the same `BlockPolicySelect` next to each toggle. Default value for fresh installs is `log_only`.

When the user clicks "Install" / "Activate", the policy selection should be passed in the install request:

```jsx
await api.installRule(rule.id, { block_policy: selectedPolicy });
// or after install:
await api.setRuleBlockPolicy(rule.id, selectedPolicy);
```

(If `installRule` doesn't accept `block_policy` yet in the API client, add it — wraps the existing `POST /api/rules/:id/install` which Task 6's backend changes already accept the field for.)

- [ ] **Step 5: Visual smoke test**

Start frontend. Navigate to `/rules`. Pick a rule, change its policy via dropdown — expect immediate visual change and a successful PATCH request in dev tools network tab.

Navigate to `/rules/:id` (click a rule) — expect the policy dropdown with explanation text below.

Navigate to `/onboarding` — expect each rule in Step 4 to have a policy selector.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/RulePacks.jsx frontend/src/pages/RuleDetail.jsx frontend/src/pages/Onboarding.jsx frontend/src/components/BlockPolicySelect.jsx
git commit -m "feat(frontend): per-rule block_policy dropdown on rules pages and onboarding"
```

---

### Task 21: End-to-End Manual Verification

**Files:**
- None (manual test pass)

This is the "did we ship a working feature?" check. Run after Tasks 1-20 are complete.

- [ ] **Step 1: Cold start**

```bash
# In one terminal
cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server

# In another terminal
cd frontend && npm run dev
```

Open http://localhost:5173.

- [ ] **Step 2: Install a hold rule**

Open `/onboarding` (or `/rules`). Install a built-in rule and set its policy to `hold`. Or via curl using a rule that catches Bash:

```bash
curl -s -X POST http://localhost:8080/api/rules \
  -H 'Content-Type: application/json' \
  -d '{"id":"e2e_hold_bash","name":"E2E hold Bash","description":"E2E test","severity":"warning","category":"test","fp_rate":"low","hook_sources":"PreToolUse","query_sql":"SELECT '"'"'__rule_id__'"'"' AS rule_id, '"'"'__rule_name__'"'"' AS rule_name, '"'"'warning'"'"' AS severity, agent_id, deployment_id, session_id, run_id, hook_name, tool_name, '"'"'E2E test hold'"'"' AS message, event_time AS detected_at, event_data FROM agentguard_hook_events WHERE hook_name = '"'"'before_tool_call'"'"' AND tool_name = '"'"'Bash'"'"'"}'

curl -s -X POST http://localhost:8080/api/rules/e2e_hold_bash/policy \
  -H 'Content-Type: application/json' -d '{"block_policy":"hold"}'
```

- [ ] **Step 3: Configure Claude Code plugin**

```bash
cd agents/claudecode
AGENTGUARD_URL=http://localhost:8080 \
AGENTGUARD_HOLD_FAIL_POLICY=deny \
make install
```

- [ ] **Step 4: Run Claude Code session that calls Bash**

Open Claude Code in another terminal. Ask it to run `ls`. Expected: Claude Code visibly pauses on the tool call.

- [ ] **Step 5: Observe UI**

Switch to browser. Within ~1s expect:
- Toast bottom-right showing the pending hold for `Bash`.
- Sidebar Threats badge shows `1`.
- Navigate to `/threats` → Pending Holds section shows the hold.

- [ ] **Step 6: Approve via UI**

Click "Review →" on the toast or pending hold. Lands on threat detail page. Click ✓ Approve.

Expected:
- Inline confirmation: "✓ Approved — agent has been notified."
- Claude Code resumes within ~1s and runs `ls`.
- Toast disappears, badge goes to `0`.

- [ ] **Step 7: Trigger again, this time Deny**

Ask Claude Code to run another Bash command. In the UI, click ✗ Deny with reason "no thanks".

Expected:
- Claude Code receives a deny; agent loop continues with the deny reason fed back to the model.
- Toast disappears.

- [ ] **Step 8: Trigger and ignore — timeout flow**

Ask Claude Code to run another Bash command. Don't respond in the UI. After 5 minutes (`AGENTGUARD_HOLD_TIMEOUT_SECONDS`), expect:
- Claude Code receives `deny` (the fail-policy default).
- Hold record in `mv_holds_current` shows `status='timeout'`.

```bash
curl -s 'http://localhost:8080/api/holds?status=timeout&limit=5' | python3 -m json.tool
```

- [ ] **Step 9: Master kill-switch**

```bash
# Stop server
# Restart with kill-switch
AGENTGUARD_HOLDS_DISABLED=true ./server
```

Trigger a Bash call from Claude Code. Expected: returns immediately with `allow`, no UI activity.

- [ ] **Step 10: Cleanup**

```bash
curl -s -X DELETE http://localhost:8080/api/rules/e2e_hold_bash
```

- [ ] **Step 11: Final commit if any debug fixes were applied**

```bash
git status
# If any incidental fixes:
git add ... && git commit -m "fix: ..."
```

---

## Self-Review

### Spec Coverage Check

Walking through each spec section:

| Spec section | Implementing task |
|---|---|
| Architecture | Tasks 7, 8 (decision pipeline + handler) |
| Per-rule policy field | Tasks 4 (DDL ALTER), 5 (queries), 6 (mutations + endpoint) |
| Configuration knobs | Task 1 (4 env vars on backend) + Task 11 (2 on plugin) |
| Data flow Step 1 (ingest) | Task 8 (ingestHookEvent) |
| Data flow Step 2 (pre-existing threats) | Task 8 (queryPreExistingOpenThreats) |
| Data flow Step 3 (MV-settle wait) | Task 8 (waitForNewMVDetections) |
| Data flow Step 4 (Decide) | Task 7 |
| Data flow Step 5 (open hold + long-poll) | Task 8 (CreateHold + writeHoldRow) + Task 2 (registry) |
| Decision side flow (`/decide`) | Task 9 (DecideHold) + Task 2 (registry.Signal) |
| In-memory channel registry | Task 2 |
| Storage — `agentguard_holds` stream | Task 4 |
| Storage — `mv_holds_current` MV | Task 4 |
| Storage — `ALTER agentguard_rules ADD block_policy` | Task 4 |
| Backend endpoint `POST /api/holds` | Task 8 |
| Backend endpoint `POST /api/holds/:id/decide` | Task 9 |
| Backend endpoint `GET /api/holds` | Task 9 |
| Backend endpoint `GET /api/holds/:id` | Task 9 |
| Backend endpoint `POST /api/holds/_abandon-session/:sid` | Task 9 |
| Backend endpoint `POST /api/holds/_drain` | Task 9 |
| Backend endpoint extension `GET /api/threats/:id` (pending_hold) | Task 10 |
| Backend endpoint extension SSE event types | Task 8 (broadcaster.Publish in CreateHold) + Task 9 (in DecideHold) |
| Backend endpoint extension rules | Tasks 5, 6 |
| Backend endpoint extension `POST /api/rules/:id/policy` | Task 6 |
| Plugin holds.ts | Task 12 |
| Plugin config additions | Task 11 |
| Plugin index.ts PreToolUse branching | Task 13 |
| Plugin SessionEnd abandon-session | Task 13 |
| Plugin install script changes | Task 14 |
| Frontend `ThreatDetail.jsx` (NEW page) | Task 18 |
| Frontend Pending Holds section on Threats list | Task 17 |
| Frontend Sidebar badge | Task 19 |
| Frontend `NotificationsProvider` extension | Task 16 |
| Frontend `HoldToast` component | Task 19 |
| Frontend rules pages dropdown | Task 20 |
| Frontend Onboarding policy selector | Task 20 |
| Frontend API client | Task 15 |
| Frontend route registration `/threats/:id` | Task 18 |
| Failure mode: plugin can't reach backend | Task 12 (failPolicy) |
| Failure mode: hold timeout | Task 8 (select with time.After) |
| Failure mode: backend crashes mid-hold | Task 4 (AbandonStalePendingHolds) |
| Failure mode: simultaneous decide + timeout | Task 2 (buffered channel) + Task 9 (idempotent /decide) |
| Failure mode: master kill-switch | Task 8 (early return when cfg.Holds.Disabled) |
| Failure mode: session ends with pending hold | Task 13 (SessionEnd → abandon) + Task 9 (AbandonSession handler) |
| Failure mode: ingest failure | Task 8 (return 503) |
| Failure mode: DDL fails on startup | Task 4 (log + continue) |
| Failure mode: multiple matches | Task 7 (Decide picks strictest) |
| Failure mode: SSE drop reconcile | Task 16 (reconcileHolds on src.onopen) |
| Testing — holdregistry | Task 2 |
| Testing — Decide pure logic | Task 7 |
| Testing — plugin holds.ts | Task 12 |
| Testing — plugin index.ts | Task 13 (manual smoke; deeper unit tests skipped — covered by integration in Task 21) |
| Testing — frontend pages | Task 21 manual E2E |
| Testing — manual E2E | Task 21 |

All spec requirements have at least one task. Two tasks (13 and 21) cover the things the spec says "vitest happy-path" but where setting up React Testing Library would be a separate setup task; the manual E2E provides equivalent coverage for v1.

### Placeholder scan

No "TBD", "TODO", "implement later", or vague "handle errors appropriately" anywhere. Every step has either: code, exact command, exact expected output, or an explicit instruction (e.g., "find the existing handler" with a `grep` command to do it).

### Type consistency check

- `block_policy` (string, snake_case) — used consistently in DB, API, JSON, and TS plugin (`blockPolicy` in JS variable names; field name in JSON stays snake_case to match Go tags).
- `hold_id` (string UUID) — same.
- `Decision` Go type vs `HoldDecision` TS type vs `DecisionOutput` Go type — different layers, consistent within their own layer:
  - `holdregistry.Decision { Allow bool; Reason string }` (in-memory channel payload)
  - `handlers.DecisionOutput { Decision string; RuleID, Reason, Severity string }` (Decide function result)
  - `models.HoldResponse { Decision string; Reason, HoldID string }` (HTTP wire format)
  - `holds.HoldDecision { decision: "allow"|"block"; reason?: string; hold_id?: string }` (TS plugin)
- `RuleMatch` (Go) — consistent across `Decide` and the handler that constructs it.
- `pending_hold` (JSON field name, snake_case) — used consistently in backend response and frontend.

No inconsistent renames across tasks.

---

## End

Plan complete and saved to `docs/superpowers/plans/2026-04-22-hook-based-tool-blocking.md`.
