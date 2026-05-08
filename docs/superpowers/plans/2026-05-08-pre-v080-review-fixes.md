# Pre-v0.8.0 Code Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all 8 issues surfaced in the pre-v0.8.0 code review: one backend auth bug, one missing test suite, and six documentation/config corrections.

**Architecture:** Three independent work streams — (1) a one-line Go fix in the backend handlers, (2) new TypeScript tests for `readTranscriptUsage`, and (3) documentation corrections across CLAUDE.md, README.md, and Makefile.

**Tech Stack:** Go/Gin (backend), TypeScript/Vitest (plugin tests), Markdown (docs).

---

## Files Changed

| File | Change |
|------|--------|
| `backend/internal/handlers/holds.go` | Replace `pluginCfg` with `auth.RequestCfg` in `DrainHolds` |
| `agents/claudecode/agentguard-plugin/src/transcript.test.ts` | Add `describe("readTranscriptUsage", ...)` block |
| `CLAUDE.md` | Fix CLI example, fix `init.go` description, fix API endpoints table, remove mock/data.go reference |
| `README.md` | Remove non-existent `/settings` row from routes table |
| `Makefile` | Change `TAG ?= 0.0.1` to `TAG ?= 0.8.0` |

---

## Task 1: Fix `DrainHolds` using wrong credential resolver

**Files:**
- Modify: `backend/internal/handlers/holds.go:585`

Context: `DrainHolds` is registered on the `protected` route group (requires session cookie). All other protected handlers resolve Timeplus credentials from the session via `auth.RequestCfg(c, cfg)`. `DrainHolds` incorrectly calls `pluginCfg(cfg)`, which requires `TIMEPLUS_USER` env var and returns 503 if absent, ignoring the logged-in user's session credentials entirely.

- [ ] **Step 1: Make the fix**

In `backend/internal/handlers/holds.go`, find the `DrainHolds` handler and replace the `pluginCfg` call with `auth.RequestCfg`:

```go
func DrainHolds(cfg *config.Config, registry *holdregistry.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
```

The `if errMsg != ""` block that followed must also be removed (3 lines: the check, the log, and the c.JSON). The full replacement diff for lines 583–590:

```go
// DrainHolds handles POST /api/holds/_drain. Operator-only: marks all pending holds as 'abandoned'.
func DrainHolds(cfg *config.Config, registry *holdregistry.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		sql := `SELECT hold_id FROM ` + holdsLatestSubquery + ` WHERE status = 'pending'`
```

Verify `auth` is already imported (it is — other protected handlers in the same file use it).

- [ ] **Step 2: Compile-check**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/backend
CGO_ENABLED=0 go build ./...
```

Expected: no output (clean build).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/handlers/holds.go
git commit -m "fix(holds): DrainHolds should use auth.RequestCfg not pluginCfg"
```

---

## Task 2: Add tests for `readTranscriptUsage`

**Files:**
- Modify: `agents/claudecode/agentguard-plugin/src/transcript.test.ts`

Context: `readTranscriptUsage` accumulates token counts, `duration_ms`, model name, and LLM call count from a JSONL transcript. It is the sole source of cost/metrics data for the Claude Code plugin. It currently has zero test coverage.

Key implementation details from the source (`transcript.ts:96–141`):
- Reads a JSONL file, one JSON object per line
- Only processes lines where `entry.message.role === "assistant"`
- Accumulates `entry.durationMs` → `duration_ms`
- Accumulates `entry.message.usage.{input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens}`
- Sets `model` from the first assistant message that has `entry.message.model` set
- Returns zero-value struct for missing/unreadable file

- [ ] **Step 1: Add the import for `readTranscriptUsage` in the test file**

In `agents/claudecode/agentguard-plugin/src/transcript.test.ts`, change line 5:

```ts
import { readTranscript, readTranscriptUsage } from "./transcript.js";
```

- [ ] **Step 2: Add a helper function for building assistant entries**

Add this helper after the existing `entry()` function (after line 22):

```ts
function usageEntry(opts: {
  inputTokens?: number;
  outputTokens?: number;
  cacheCreation?: number;
  cacheRead?: number;
  durationMs?: number;
  model?: string;
}) {
  return JSON.stringify({
    type: "assistant",
    timestamp: "2026-01-01T00:00:00.000Z",
    durationMs: opts.durationMs ?? 0,
    message: {
      role: "assistant",
      model: opts.model ?? "",
      usage: {
        input_tokens: opts.inputTokens ?? 0,
        output_tokens: opts.outputTokens ?? 0,
        cache_creation_input_tokens: opts.cacheCreation ?? 0,
        cache_read_input_tokens: opts.cacheRead ?? 0,
      },
    },
  });
}
```

- [ ] **Step 3: Add the `readTranscriptUsage` describe block**

Append the following block at the end of the file, before the final `});` closing the existing describe, or as a sibling describe block:

```ts
describe("readTranscriptUsage", () => {
  let tmpPath: string;

  beforeEach(() => {
    tmpPath = makeTmpPath();
  });

  afterEach(() => {
    try { unlinkSync(tmpPath); } catch { /* ignore */ }
  });

  it("returns zero-value struct for a missing file", () => {
    const result = readTranscriptUsage("/nonexistent/path/transcript.jsonl");
    expect(result.input_tokens).toBe(0);
    expect(result.output_tokens).toBe(0);
    expect(result.total_tokens).toBe(0);
    expect(result.duration_ms).toBe(0);
    expect(result.model).toBe("");
    expect(result.llm_calls).toBe(0);
  });

  it("returns zero-value struct for an empty file", () => {
    writeFileSync(tmpPath, "");
    const result = readTranscriptUsage(tmpPath);
    expect(result.llm_calls).toBe(0);
    expect(result.total_tokens).toBe(0);
  });

  it("accumulates token counts from a single assistant message", () => {
    writeFileSync(tmpPath, usageEntry({ inputTokens: 100, outputTokens: 50, durationMs: 500, model: "claude-opus-4-5" }));
    const result = readTranscriptUsage(tmpPath);
    expect(result.input_tokens).toBe(100);
    expect(result.output_tokens).toBe(50);
    expect(result.total_tokens).toBe(150);
    expect(result.duration_ms).toBe(500);
    expect(result.model).toBe("claude-opus-4-5");
    expect(result.llm_calls).toBe(1);
  });

  it("accumulates token counts across multiple assistant messages", () => {
    const lines = [
      usageEntry({ inputTokens: 100, outputTokens: 50, durationMs: 300 }),
      usageEntry({ inputTokens: 200, outputTokens: 80, durationMs: 200 }),
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscriptUsage(tmpPath);
    expect(result.input_tokens).toBe(300);
    expect(result.output_tokens).toBe(130);
    expect(result.total_tokens).toBe(430);
    expect(result.duration_ms).toBe(500);
    expect(result.llm_calls).toBe(2);
  });

  it("uses the model from the first assistant message that has one", () => {
    const lines = [
      usageEntry({ model: "" }),          // no model on first message
      usageEntry({ model: "claude-sonnet-4-6" }),  // model present on second
      usageEntry({ model: "claude-opus-4-7" }),    // should be ignored
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscriptUsage(tmpPath);
    expect(result.model).toBe("claude-sonnet-4-6");
  });

  it("ignores user and system messages", () => {
    const lines = [
      JSON.stringify({ type: "user", timestamp: "2026-01-01T00:00:00.000Z", durationMs: 999,
        message: { role: "user", usage: { input_tokens: 999, output_tokens: 999 } } }),
      usageEntry({ inputTokens: 10, outputTokens: 5 }),
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscriptUsage(tmpPath);
    expect(result.input_tokens).toBe(10);
    expect(result.output_tokens).toBe(5);
    expect(result.llm_calls).toBe(1);
  });

  it("accumulates cache token fields", () => {
    writeFileSync(tmpPath, usageEntry({ inputTokens: 50, cacheCreation: 30, cacheRead: 20 }));
    const result = readTranscriptUsage(tmpPath);
    expect(result.cache_creation_input_tokens).toBe(30);
    expect(result.cache_read_input_tokens).toBe(20);
  });

  it("skips malformed JSON lines without throwing", () => {
    const lines = [
      usageEntry({ inputTokens: 10, outputTokens: 5 }),
      "this is not json {{{",
      usageEntry({ inputTokens: 20, outputTokens: 10 }),
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscriptUsage(tmpPath);
    expect(result.llm_calls).toBe(2);
    expect(result.total_tokens).toBe(45);
  });

  it("handles assistant messages without a usage field gracefully", () => {
    const noUsageEntry = JSON.stringify({
      type: "assistant",
      timestamp: "2026-01-01T00:00:00.000Z",
      durationMs: 100,
      message: { role: "assistant", model: "claude-haiku-4-5" },
    });
    writeFileSync(tmpPath, noUsageEntry);
    const result = readTranscriptUsage(tmpPath);
    expect(result.llm_calls).toBe(1);
    expect(result.input_tokens).toBe(0);
    expect(result.model).toBe("claude-haiku-4-5");
  });
});
```

- [ ] **Step 4: Run the tests**

```bash
cd /Users/gangtao/Code/timeplus/AgentGuard/agents/claudecode/agentguard-plugin
npm test
```

Expected: all tests pass, including the new `readTranscriptUsage` suite (8 new tests).

- [ ] **Step 5: Commit**

```bash
git add agents/claudecode/agentguard-plugin/src/transcript.test.ts
git commit -m "test(transcript): add readTranscriptUsage test coverage"
```

---

## Task 3: Fix CLAUDE.md — CLI example flags

**Files:**
- Modify: `CLAUDE.md:35`

The CLI example shows `--timeplus-user default --timeplus-password ""`. Neither flag exists in `main.go`. Remove them from the example.

- [ ] **Step 1: Edit CLAUDE.md line 35**

Replace:
```
./server --port 8080 --timeplus-host localhost --timeplus-port 3218 --timeplus-user default --timeplus-password ""
```
With:
```
./server --port 8080 --timeplus-host localhost --timeplus-port 3218
```

Add a note on the next line clarifying how credentials are supplied:
```
# Credentials come from env vars: TIMEPLUS_USER, TIMEPLUS_PASSWORD
```

---

## Task 4: Fix CLAUDE.md — `init.go` described as startup logic

**Files:**
- Modify: `CLAUDE.md:81` and `CLAUDE.md:208`

`timeplus.Init()` is only called by the setup wizard handler (`POST /api/setup/provision`), not at server startup.

- [ ] **Step 1: Fix line 81**

Replace:
```
  - `init.go` — Startup: creates OTel streams, `agentguard_rules` stream, and `agentguard_security_events` stream if they don't exist; syncs MV state with installed rules.
```
With:
```
  - `init.go` — Provisioning: called by the setup wizard (`POST /api/setup/provision`) to create OTel streams, `agentguard_rules` stream, and `agentguard_security_events` stream if they don't exist; syncs MV state with installed rules. Not called at server startup.
```

- [ ] **Step 2: Fix line 208**

Replace:
```
On startup, `timeplus.Init()` creates missing streams and syncs MV state. This is non-fatal — the server starts even if Proton is unavailable.
```
With:
```
`timeplus.Init()` is called by the setup wizard (`POST /api/setup/provision`) to create missing streams and sync MV state. The server starts independently of Timeplus availability — if Proton is unreachable, provisioning will fail but the binary itself starts cleanly.
```

---

## Task 5: Fix CLAUDE.md — remove reference to `mock/data.go`

**Files:**
- Modify: `CLAUDE.md:85`

Replace:
```
Adding a new API resource: add structs to `models.go`, add mock data to `mock/data.go` if needed, create a handler file, and register routes in `main.go`.
```
With:
```
Adding a new API resource: add structs to `models.go`, create a handler file in `internal/handlers/`, and register routes in `main.go`. There is no mock data layer — all handlers query Timeplus Enterprise directly.
```

---

## Task 6: Fix CLAUDE.md — API Endpoints table missing routes

**Files:**
- Modify: `CLAUDE.md:87–126`

The table is missing: auth routes, setup routes, holds API (6 routes), activity, install-script, and the rules policy endpoint.

- [ ] **Step 1: Add missing rows to the API Endpoints table**

After the existing `POST /api/rules/:id/upgrade` row, add the missing rows. The full corrected section (replace lines 91–126):

```markdown
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
| GET | `/api/cost` | `GetCost` | Computed from agentguard_hook_events + agentguard_pricing |
| GET | `/api/cost/pricing` | `ListPricing` | Reads from agentguard_pricing stream |
| POST | `/api/cost/pricing` | `UpsertPricing` | Inserts into agentguard_pricing |
| DELETE | `/api/cost/pricing/:provider/:model` | `DeletePricing` | Deletes from agentguard_pricing |
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
```

---

## Task 7: Fix README.md — remove non-existent `/settings` route

**Files:**
- Modify: `README.md:177`

- [ ] **Step 1: Remove the Settings row**

Delete the line:
```
| Settings | `/settings` | Application configuration |
```

---

## Task 8: Fix Makefile — update `TAG` default

**Files:**
- Modify: `Makefile:13`

- [ ] **Step 1: Update default TAG**

Replace:
```makefile
TAG         ?= 0.0.1
```
With:
```makefile
TAG         ?= 0.8.0
```

---

## Task 9: Commit all documentation fixes together

- [ ] **Step 1: Commit**

```bash
git add CLAUDE.md README.md Makefile
git commit -m "docs: fix pre-v0.8.0 review issues — CLI flags, init.go description, API table, mock/data.go ref, /settings route, TAG default"
```

---

## Verification

After all tasks:

```bash
# Backend still compiles
cd backend && CGO_ENABLED=0 go build ./... && echo "backend OK"

# Plugin tests still pass
cd ../agents/claudecode/agentguard-plugin && npm test
```
