# OpenClaw Timeline Analysis Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the flat gap-fill run trace endpoint with a backend-analyzed, agent-type-routed timeline that produces true hierarchical spans (Run → LLM Call → Tool Call) using actual durationMs timing from OpenClaw hook events.

**Architecture:** The `agentshield_hook_events` Timeplus stream gains an `agent_type` column; the OpenClaw plugin populates it with `"openclaw"`. The existing `GetRunTrace` handler is replaced by a router that reads `agent_type` from the fetched events and dispatches to either a new OpenClaw 6-step hierarchical analyzer or the existing flat gap-fill logic (extracted into its own file). The response shape is unchanged, so the frontend RunWaterfall component requires only a color addition.

**Tech Stack:** Go 1.23, Gin, Timeplus Proton (HTTP query API), TypeScript (plugin), React 18

---

## File Map

| File | Change | Responsibility |
|------|--------|---------------|
| `backend/internal/timeplus/init.go` | Modify `ddlAgentShieldHookEvents` | Add `agent_type` column to CREATE STREAM |
| `agents/openclaw/agentshield-plugin/src/timeplus-client.ts` | Modify `HookEvent` type + `COLUMNS` | Add `agent_type` field |
| `agents/openclaw/agentshield-plugin/src/hooks.ts` | Modify `baseEvent()` | Populate `agent_type: "openclaw"` |
| `backend/internal/handlers/timeline.go` | **Create** | Route handler: fetch events, detect agent_type, dispatch |
| `backend/internal/handlers/timeline_openclaw.go` | **Create** | 6-step OpenClaw hierarchical analyzer |
| `backend/internal/handlers/timeline_fallback.go` | **Create** | Extracted flat gap-fill logic |
| `backend/internal/handlers/agent_sessions.go` | Modify | Remove `GetRunTrace` + `hookKind`; keep all other handlers |
| `frontend/src/pages/AgentDetail.jsx` | Modify `barColor()` | Add `"subagent"` kind color |

---

## Task 1: Add `agent_type` to stream schema + plugin

**Files:**
- Modify: `backend/internal/timeplus/init.go:468-485`
- Modify: `agents/openclaw/agentshield-plugin/src/timeplus-client.ts`
- Modify: `agents/openclaw/agentshield-plugin/src/hooks.ts`

No test for the DDL change itself (it's applied on startup). Tests for the analyzer (Task 3) will consume events with `agent_type`, implicitly validating the field.

- [ ] **Step 1: Update the stream DDL constant in `init.go`**

In `backend/internal/timeplus/init.go`, replace the `ddlAgentShieldHookEvents` constant (lines 468–485):

```go
const ddlAgentShieldHookEvents = `
CREATE STREAM agentshield_hook_events
(
  hook_name        low_cardinality(string),
  event_time       datetime64(3),
  session_id       string,
  run_id           string,
  agent_id         string,
  deployment_id    low_cardinality(string),
  deployment_name  low_cardinality(string),
  session_key      string,
  tool_name        string,
  provider         string,
  model            string,
  hook_decision    low_cardinality(string),
  block_reason     string,
  event_data       string,
  agent_type       low_cardinality(string)
)`
```

- [ ] **Step 2: Add `agent_type` to the `HookEvent` type in the plugin**

In `agents/openclaw/agentshield-plugin/src/timeplus-client.ts`, add the field to the type and the `COLUMNS` array:

```typescript
export type HookEvent = {
  hook_name: string;
  event_time: string;
  session_id: string;
  run_id: string;
  agent_id: string;
  deployment_id: string;
  deployment_name: string;
  session_key: string;
  tool_name: string;
  provider: string;
  model: string;
  hook_decision: string;
  block_reason: string;
  event_data: string;
  agent_type: string;   // "openclaw" | "" for unknown/legacy
};

const COLUMNS: (keyof HookEvent)[] = [
  "hook_name", "event_time", "session_id", "run_id", "agent_id",
  "deployment_id", "deployment_name",
  "session_key", "tool_name", "provider", "model",
  "hook_decision", "block_reason", "event_data", "agent_type",
];
```

- [ ] **Step 3: Populate `agent_type` in `baseEvent()` in `hooks.ts`**

In `agents/openclaw/agentshield-plugin/src/hooks.ts`, update the `baseEvent` function return value to include `agent_type`:

```typescript
export function baseEvent(hookName: string, ctx: BaseCtx, config: AgentShieldConfig): HookEvent {
  return {
    hook_name: hookName,
    event_time: new Date().toISOString(),
    session_id: ctx.sessionId ?? "",
    run_id: ctx.runId ?? "",
    agent_id: ctx.agentId ?? "",
    deployment_id: config.deploymentId,
    deployment_name: config.deploymentName,
    session_key: ctx.sessionKey ?? "",
    tool_name: "",
    provider: "",
    model: "",
    hook_decision: "observe",
    block_reason: "",
    event_data: "",
    agent_type: "openclaw",
  };
}
```

- [ ] **Step 4: Compile-check the plugin**

```bash
cd agents/openclaw/agentshield-plugin
npx tsc --noEmit
```

Expected: no errors. The `agent_type` field is now required on `HookEvent` and `baseEvent` provides it; all `enqueue` calls spread `base` so they inherit it automatically.

- [ ] **Step 5: Compile-check the backend**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: exits 0. The DDL string is just a constant; no Go type changes yet.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/timeplus/init.go \
        agents/openclaw/agentshield-plugin/src/timeplus-client.ts \
        agents/openclaw/agentshield-plugin/src/hooks.ts
git commit -m "feat(stream): add agent_type column to hook_events stream and openclaw plugin"
```

---

## Task 2: Extract fallback (flat gap-fill) into `timeline_fallback.go`

**Files:**
- Create: `backend/internal/handlers/timeline_fallback.go`
- Modify: `backend/internal/handlers/agent_sessions.go:249-259` (remove `hookKind`) and lines 261–401 (remove `GetRunTrace`)

This task extracts existing logic verbatim — no behavior change.

- [ ] **Step 1: Create `timeline_fallback.go` with the extracted logic**

Create `backend/internal/handlers/timeline_fallback.go`:

```go
package handlers

import (
	"encoding/json"
	"fmt"

	"github.com/timeplus/agentshield/internal/models"
)

// hookKind maps a hook name to a span kind for waterfall colouring.
// Kept internal to the timeline package — not exposed to other handlers.
func hookKind(hookName string) string {
	switch hookName {
	case "llm_input", "llm_output", "before_model_resolve", "before_prompt_build", "before_agent_reply":
		return "llm"
	case "before_tool_call", "after_tool_call", "tool_result_persist":
		return "tool"
	default:
		return "lifecycle"
	}
}

// analyzeFallback builds a flat waterfall from raw hook event rows using
// gap-fill timing: each event's duration is the gap to the next event.
// Used for unknown agent types and as a regression-safe fallback.
func analyzeFallback(runID string, rows []map[string]any) []models.TraceSpan {
	if len(rows) == 0 {
		return []models.TraceSpan{}
	}

	firstMs := parseTime(rows[0]["event_time"]).UnixMilli()
	lastMs := parseTime(rows[len(rows)-1]["event_time"]).UnixMilli()
	runDurationMs := lastMs - firstMs
	if runDurationMs <= 0 {
		runDurationMs = 1
	}

	spans := make([]models.TraceSpan, 0, len(rows)+1)

	// Root span covers the full run window.
	spans = append(spans, models.TraceSpan{
		SpanID:     runID,
		ParentID:   "",
		Kind:       "run",
		Name:       "run",
		StartMs:    firstMs,
		DurationMs: runDurationMs,
		Status:     "ok",
		Attributes: map[string]string{},
	})

	// One child span per hook event; duration = gap to the next event.
	for i, row := range rows {
		hookName := parseStr(row["hook_name"])
		evtMs := parseTime(row["event_time"]).UnixMilli()

		var durationMs int64
		if i+1 < len(rows) {
			nextMs := parseTime(rows[i+1]["event_time"]).UnixMilli()
			durationMs = nextMs - evtMs
		} else {
			durationMs = lastMs - evtMs
		}
		if durationMs < 0 {
			durationMs = 0
		}

		status := "ok"
		hookDecision := parseStr(row["hook_decision"])
		if hookDecision == "block" {
			status = "blocked"
		}

		attrs := map[string]string{}
		if tn := parseStr(row["tool_name"]); tn != "" {
			attrs["tool_name"] = tn
		}
		if m := parseStr(row["model"]); m != "" {
			attrs["model"] = m
		}
		if p := parseStr(row["provider"]); p != "" {
			attrs["provider"] = p
		}
		if hookDecision != "" && hookDecision != "observe" {
			attrs["hook_decision"] = hookDecision
		}
		if br := parseStr(row["block_reason"]); br != "" {
			attrs["block_reason"] = br
		}
		if ed := parseStr(row["event_data"]); ed != "" {
			var data map[string]interface{}
			if json.Unmarshal([]byte(ed), &data) == nil {
				if errMsg, ok := data["error"].(string); ok && errMsg != "" {
					status = "error"
					attrs["error"] = errMsg
				}
				if dur, ok := data["durationMs"].(float64); ok {
					attrs["duration_ms"] = fmt.Sprintf("%.0f", dur)
				}
				if tcid, ok := data["toolCallId"].(string); ok {
					attrs["tool_call_id"] = tcid
				}
			}
		}

		spans = append(spans, models.TraceSpan{
			SpanID:     fmt.Sprintf("evt-%d", i),
			ParentID:   runID,
			Kind:       hookKind(hookName),
			Name:       hookName,
			StartMs:    evtMs,
			DurationMs: durationMs,
			Status:     status,
			Attributes: attrs,
		})
	}

	return spans
}
```

- [ ] **Step 2: Remove `hookKind` and `GetRunTrace` from `agent_sessions.go`**

In `backend/internal/handlers/agent_sessions.go`, delete lines 249–401 (the `hookKind` function and the entire `GetRunTrace` function). The file retains all other session handlers (`ListSessions`, `GetSession`, `ListRuns`, etc.). The imports remain unchanged as they are used by the other handlers; verify no unused imports remain after deletion.

- [ ] **Step 3: Compile-check**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: compilation failure — `GetRunTrace` is referenced in `cmd/server/main.go:110` and is no longer defined. This is expected and will be fixed in Task 3.

- [ ] **Step 4: Write failing test for `analyzeFallback`**

Create `backend/internal/handlers/timeline_fallback_test.go`:

```go
package handlers

import (
	"testing"
)

func TestAnalyzeFallback_EmptyRows(t *testing.T) {
	spans := analyzeFallback("run-1", nil)
	if len(spans) != 0 {
		t.Fatalf("expected 0 spans for nil rows, got %d", len(spans))
	}
}

func TestAnalyzeFallback_SingleEvent(t *testing.T) {
	rows := []map[string]any{
		{
			"hook_name":     "agent_start",
			"event_time":    "2026-04-11 10:00:00.000",
			"hook_decision": "observe",
			"tool_name":     "",
			"provider":      "",
			"model":         "",
			"block_reason":  "",
			"event_data":    "",
		},
	}
	spans := analyzeFallback("run-abc", rows)
	// 1 root + 1 event = 2 spans
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	if spans[0].Kind != "run" {
		t.Errorf("first span kind = %q, want run", spans[0].Kind)
	}
	if spans[0].SpanID != "run-abc" {
		t.Errorf("root span_id = %q, want run-abc", spans[0].SpanID)
	}
	if spans[1].ParentID != "run-abc" {
		t.Errorf("child parent_id = %q, want run-abc", spans[1].ParentID)
	}
}

func TestAnalyzeFallback_BlockedEvent(t *testing.T) {
	rows := []map[string]any{
		{
			"hook_name":     "before_tool_call",
			"event_time":    "2026-04-11 10:00:00.000",
			"hook_decision": "block",
			"block_reason":  "policy violation",
			"tool_name":     "bash",
			"provider":      "",
			"model":         "",
			"event_data":    "",
		},
		{
			"hook_name":     "agent_end",
			"event_time":    "2026-04-11 10:00:01.000",
			"hook_decision": "observe",
			"tool_name":     "",
			"provider":      "",
			"model":         "",
			"block_reason":  "",
			"event_data":    "",
		},
	}
	spans := analyzeFallback("run-xyz", rows)
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans (root + 2 events), got %d", len(spans))
	}
	// blocked event is span index 1
	blocked := spans[1]
	if blocked.Status != "blocked" {
		t.Errorf("blocked event status = %q, want blocked", blocked.Status)
	}
	if blocked.Attributes["block_reason"] != "policy violation" {
		t.Errorf("block_reason = %q, want 'policy violation'", blocked.Attributes["block_reason"])
	}
	// duration = gap to next event = 1000ms
	if blocked.DurationMs != 1000 {
		t.Errorf("blocked event duration_ms = %d, want 1000", blocked.DurationMs)
	}
}
```

- [ ] **Step 5: Run the test (compile fails — expected)**

```bash
cd backend
CGO_ENABLED=0 go test ./internal/handlers/... -run TestAnalyzeFallback -v 2>&1 | head -20
```

Expected: compile error about missing `GetRunTrace`. That is fine — Task 3 adds it. Verify the test file itself has no syntax errors by checking the error message is about `GetRunTrace` not found, not a parse error.

---

## Task 3: Create `timeline.go` (route handler + fetch)

**Files:**
- Create: `backend/internal/handlers/timeline.go`

This file provides the new `GetRunTrace` that `main.go` already references (fixing the compile error), plus `fetchHookEvents` and `detectAgentType`.

- [ ] **Step 1: Create `timeline.go`**

Create `backend/internal/handlers/timeline.go`:

```go
package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	"github.com/timeplus/agentshield/internal/timeplus"
)

// GetRunTrace handles GET /api/agents/:id/sessions/:session_id/runs/:run_id/trace.
// It fetches hook events from Proton, detects the agent type, and routes to the
// appropriate timeline analyzer (OpenClaw hierarchical or flat gap-fill fallback).
func GetRunTrace(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format"})
			return
		}

		sessionID := c.Param("session_id")
		if !idPartRe.MatchString(sessionID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session_id format"})
			return
		}

		runID := c.Param("run_id")
		if !runIDRe.MatchString(runID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run_id format"})
			return
		}

		rows, err := fetchHookEvents(cfg, deploymentID, agentID, sessionID, runID)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		if len(rows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}

		agentType := detectAgentType(rows)

		var spans []models.TraceSpan
		switch agentType {
		case "openclaw":
			spans = analyzeOpenClaw(runID, rows)
		default:
			spans = analyzeFallback(runID, rows)
		}

		c.JSON(http.StatusOK, models.TraceResponse{Spans: spans})
	}
}

// fetchHookEvents queries Proton for all hook events for a specific run,
// ordered by event_time ascending.
func fetchHookEvents(cfg *config.Config, deploymentID, agentID, sessionID, runID string) ([]map[string]any, error) {
	sql := fmt.Sprintf(`
SELECT hook_name, event_time, tool_name, provider, model,
       hook_decision, block_reason, event_data, agent_type
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'
  AND run_id        = '%s'
ORDER BY event_time ASC`,
		escapeSQL(deploymentID), escapeSQL(agentID), escapeSQL(sessionID), escapeSQL(runID))

	return timeplus.Query(&cfg.Timeplus, sql)
}

// detectAgentType returns the agent_type value from the first event that has
// a non-empty value, or "" to trigger the fallback analyzer.
func detectAgentType(rows []map[string]any) string {
	for _, row := range rows {
		if t := parseStr(row["agent_type"]); t != "" {
			return t
		}
	}
	return ""
}
```

- [ ] **Step 2: Write tests for `detectAgentType`**

Create `backend/internal/handlers/timeline_test.go`:

```go
package handlers

import (
	"testing"
)

func TestDetectAgentType_Empty(t *testing.T) {
	got := detectAgentType(nil)
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestDetectAgentType_AllEmpty(t *testing.T) {
	rows := []map[string]any{
		{"agent_type": ""},
		{"agent_type": ""},
	}
	got := detectAgentType(rows)
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestDetectAgentType_OpenClaw(t *testing.T) {
	rows := []map[string]any{
		{"agent_type": ""},
		{"agent_type": "openclaw"},
		{"agent_type": "openclaw"},
	}
	got := detectAgentType(rows)
	if got != "openclaw" {
		t.Errorf("got %q, want openclaw", got)
	}
}
```

- [ ] **Step 3: Compile and run tests**

```bash
cd backend
CGO_ENABLED=0 go test ./internal/handlers/... -run "TestDetectAgentType|TestAnalyzeFallback" -v
```

Expected: all 5 tests pass. The `GetRunTrace` compile error is now resolved.

- [ ] **Step 4: Verify full backend compiles**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: exits 0. The `analyzeOpenClaw` call in `timeline.go` will fail until Task 4.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/timeline.go \
        backend/internal/handlers/timeline_test.go \
        backend/internal/handlers/timeline_fallback.go \
        backend/internal/handlers/timeline_fallback_test.go \
        backend/internal/handlers/agent_sessions.go
git commit -m "refactor(trace): extract fallback analyzer and add routing handler"
```

---

## Task 4: Create `timeline_openclaw.go` (6-step hierarchical analyzer)

**Files:**
- Create: `backend/internal/handlers/timeline_openclaw.go`

- [ ] **Step 1: Write the failing test first**

Create `backend/internal/handlers/timeline_openclaw_test.go`:

```go
package handlers

import (
	"testing"

	"github.com/timeplus/agentshield/internal/models"
)

// makeRow is a test helper that builds a minimal hook event row.
func makeRow(hookName, eventTime, toolName, hookDecision, eventData string) map[string]any {
	return map[string]any{
		"hook_name":     hookName,
		"event_time":    eventTime,
		"tool_name":     toolName,
		"hook_decision": hookDecision,
		"block_reason":  "",
		"provider":      "",
		"model":         "",
		"event_data":    eventData,
		"agent_type":    "openclaw",
	}
}

func TestAnalyzeOpenClaw_EmptyRows(t *testing.T) {
	spans := analyzeOpenClaw("run-1", nil)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span for empty rows, got %d", len(spans))
	}
	if spans[0].Kind != "run" || spans[0].ParentID != "" {
		t.Errorf("unexpected root span: %+v", spans[0])
	}
}

func TestAnalyzeOpenClaw_LLMPair(t *testing.T) {
	rows := []map[string]any{
		makeRow("agent_start", "2026-04-11 10:00:00.000", "", "observe", ""),
		makeRow("llm_input", "2026-04-11 10:00:01.000", "", "observe", `{"runId":"r1"}`),
		makeRow("llm_output", "2026-04-11 10:00:05.000", "", "observe", `{"runId":"r1"}`),
		makeRow("agent_end", "2026-04-11 10:00:06.000", "", "observe", `{"durationMs":6000}`),
	}
	spans := analyzeOpenClaw("run-1", rows)

	var runSpan, llmSpan, lifecycleSpan *models.TraceSpan
	for i := range spans {
		switch spans[i].Kind {
		case "run":
			s := spans[i]
			runSpan = &s
		case "llm":
			s := spans[i]
			llmSpan = &s
		case "lifecycle":
			s := spans[i]
			lifecycleSpan = &s
		}
	}

	if runSpan == nil {
		t.Fatal("no run span")
	}
	if runSpan.DurationMs != 6000 {
		t.Errorf("run duration_ms = %d, want 6000 (from agent_end.durationMs)", runSpan.DurationMs)
	}
	if llmSpan == nil {
		t.Fatal("no llm span")
	}
	if llmSpan.ParentID != runSpan.SpanID {
		t.Errorf("llm parent_id = %q, want %q", llmSpan.ParentID, runSpan.SpanID)
	}
	if llmSpan.DurationMs != 4000 {
		t.Errorf("llm duration_ms = %d, want 4000", llmSpan.DurationMs)
	}
	if lifecycleSpan == nil {
		t.Fatal("no lifecycle span for agent_start")
	}
	if lifecycleSpan.Name != "Agent Start" {
		t.Errorf("lifecycle name = %q, want Agent Start", lifecycleSpan.Name)
	}
}

func TestAnalyzeOpenClaw_ToolPair(t *testing.T) {
	tcID := `{"toolCallId":"tc-001"}`
	rows := []map[string]any{
		makeRow("llm_input", "2026-04-11 10:00:00.000", "", "observe", `{"runId":"r1"}`),
		makeRow("before_tool_call", "2026-04-11 10:00:02.000", "bash", "observe", tcID),
		makeRow("after_tool_call", "2026-04-11 10:00:05.000", "bash", "observe",
			`{"toolCallId":"tc-001","durationMs":3000}`),
		makeRow("llm_output", "2026-04-11 10:00:06.000", "", "observe", `{"runId":"r1"}`),
	}
	spans := analyzeOpenClaw("run-2", rows)

	var toolSpan, llmSpan *models.TraceSpan
	for i := range spans {
		if spans[i].Kind == "tool" && spans[i].Name == "bash" {
			s := spans[i]
			toolSpan = &s
		}
		if spans[i].Kind == "llm" {
			s := spans[i]
			llmSpan = &s
		}
	}
	if toolSpan == nil {
		t.Fatal("no tool span named bash")
	}
	if toolSpan.DurationMs != 3000 {
		t.Errorf("tool duration_ms = %d, want 3000", toolSpan.DurationMs)
	}
	if llmSpan == nil {
		t.Fatal("no llm span")
	}
	if toolSpan.ParentID != llmSpan.SpanID {
		t.Errorf("tool parent_id = %q, want llm span_id %q", toolSpan.ParentID, llmSpan.SpanID)
	}
}

func TestAnalyzeOpenClaw_BlockedTool(t *testing.T) {
	rows := []map[string]any{
		makeRow("llm_input", "2026-04-11 10:00:00.000", "", "observe", `{"runId":"r1"}`),
		makeRow("before_tool_call", "2026-04-11 10:00:02.000", "bash", "block",
			`{"toolCallId":"tc-blocked"}`),
		// no after_tool_call — gap-filled from llm_output 1s later
		makeRow("llm_output", "2026-04-11 10:00:03.000", "", "observe", `{"runId":"r1"}`),
	}
	spans := analyzeOpenClaw("run-3", rows)

	var blockedSpan *models.TraceSpan
	for i := range spans {
		if spans[i].Status == "blocked" {
			s := spans[i]
			blockedSpan = &s
		}
	}
	if blockedSpan == nil {
		t.Fatal("no blocked tool span")
	}
	if blockedSpan.Name != "bash (blocked)" {
		t.Errorf("blocked name = %q, want 'bash (blocked)'", blockedSpan.Name)
	}
	if blockedSpan.DurationMs != 1000 {
		t.Errorf("blocked duration_ms = %d, want 1000", blockedSpan.DurationMs)
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails (analyzeOpenClaw not defined)**

```bash
cd backend
CGO_ENABLED=0 go test ./internal/handlers/... -run TestAnalyzeOpenClaw -v 2>&1 | head -10
```

Expected: compile error `undefined: analyzeOpenClaw`.

- [ ] **Step 3: Implement `analyzeOpenClaw`**

Create `backend/internal/handlers/timeline_openclaw.go`:

```go
package handlers

import (
	"encoding/json"
	"fmt"

	"github.com/timeplus/agentshield/internal/models"
)

// spanNameMap translates hook_name values to human-readable span names.
var spanNameMap = map[string]string{
	"agent_start":         "Agent Start",
	"agent_end":           "Agent End",
	"llm_input":           "LLM Call",
	"llm_output":          "LLM Response",
	"before_tool_call":    "Tool Call",
	"after_tool_call":     "Tool Call",
	"tool_result_persist": "Persist Tool Result",
	"subagent_spawning":   "Subagent Start",
	"subagent_spawned":    "Subagent Start",
	"subagent_ended":      "Subagent End",
	"session_start":       "Session Start",
	"session_end":         "Session End",
}

func humanName(hookName, toolName string) string {
	if n, ok := spanNameMap[hookName]; ok {
		return n
	}
	return hookName
}

// analyzeOpenClaw builds a hierarchical trace from OpenClaw hook event rows.
// It produces: root run span → LLM call spans → tool call spans (children of LLM).
// Unpaired events are gap-filled and attached to the nearest parent.
func analyzeOpenClaw(runID string, rows []map[string]any) []models.TraceSpan {
	if len(rows) == 0 {
		return []models.TraceSpan{{
			SpanID:     runID,
			ParentID:   "",
			Kind:       "run",
			Name:       "Agent Run",
			StartMs:    0,
			DurationMs: 0,
			Status:     "ok",
			Attributes: map[string]string{},
		}}
	}

	// Parse event_data JSON for every row once — reused across multiple steps.
	type parsedRow struct {
		raw      map[string]any
		evtMs    int64
		hookName string
		toolName string
		data     map[string]any // parsed event_data; nil if unparseable
		decision string
		consumed bool
	}

	prows := make([]parsedRow, len(rows))
	for i, row := range rows {
		var data map[string]any
		if ed := parseStr(row["event_data"]); ed != "" {
			_ = json.Unmarshal([]byte(ed), &data)
		}
		prows[i] = parsedRow{
			raw:      row,
			evtMs:    parseTime(row["event_time"]).UnixMilli(),
			hookName: parseStr(row["hook_name"]),
			toolName: parseStr(row["tool_name"]),
			data:     data,
			decision: parseStr(row["hook_decision"]),
		}
	}

	firstMs := prows[0].evtMs
	lastMs := prows[len(prows)-1].evtMs

	// ── Step 1: Run root span ────────────────────────────────────────────────
	runDurationMs := lastMs - firstMs
	for _, pr := range prows {
		if pr.hookName == "agent_end" {
			if dur, ok := pr.data["durationMs"].(float64); ok && dur > 0 {
				runDurationMs = int64(dur)
			}
			break
		}
	}
	if runDurationMs <= 0 {
		runDurationMs = 1
	}

	spans := []models.TraceSpan{{
		SpanID:     runID,
		ParentID:   "",
		Kind:       "run",
		Name:       "Agent Run",
		StartMs:    firstMs,
		DurationMs: runDurationMs,
		Status:     "ok",
		Attributes: map[string]string{},
	}}

	// ── Step 2 + 3 + 4: LLM spans and tool call spans ───────────────────────
	// Build a lookup of after_tool_call rows by toolCallId for O(1) pairing.
	afterToolByID := map[string]int{} // toolCallId → index in prows
	for i, pr := range prows {
		if pr.hookName == "after_tool_call" {
			if tcID, ok := pr.data["toolCallId"].(string); ok && tcID != "" {
				afterToolByID[tcID] = i
			}
		}
	}

	llmSpanCount := 0
	for i := range prows {
		if prows[i].hookName != "llm_input" {
			continue
		}
		prows[i].consumed = true
		llmStartMs := prows[i].evtMs
		llmSpanID := fmt.Sprintf("llm-%d", llmSpanCount)
		llmSpanCount++

		// Find matching llm_output (same runId, after this llm_input in time).
		llmEndMs := llmStartMs // zero-duration default
		llmRunID, _ := prows[i].data["runId"].(string)
		for j := i + 1; j < len(prows); j++ {
			if prows[j].hookName == "llm_output" {
				outRunID, _ := prows[j].data["runId"].(string)
				if llmRunID == "" || outRunID == "" || llmRunID == outRunID {
					llmEndMs = prows[j].evtMs
					prows[j].consumed = true
					break
				}
			}
		}

		model := parseStr(prows[i].raw["model"])
		provider := parseStr(prows[i].raw["provider"])
		llmAttrs := map[string]string{}
		if model != "" {
			llmAttrs["model"] = model
		}
		if provider != "" {
			llmAttrs["provider"] = provider
		}

		spans = append(spans, models.TraceSpan{
			SpanID:     llmSpanID,
			ParentID:   runID,
			Kind:       "llm",
			Name:       "LLM Call",
			StartMs:    llmStartMs,
			DurationMs: llmEndMs - llmStartMs,
			Status:     "ok",
			Attributes: llmAttrs,
		})

		// ── Step 3 + 4: Tool calls that occurred during this LLM window ─────
		toolCount := 0
		for j := i + 1; j < len(prows); j++ {
			if prows[j].evtMs >= llmEndMs && llmEndMs > llmStartMs {
				break // past the LLM window
			}
			if prows[j].hookName != "before_tool_call" || prows[j].consumed {
				continue
			}
			prows[j].consumed = true

			toolName := prows[j].toolName
			if toolName == "" {
				toolName = "Tool Call"
			}
			toolStartMs := prows[j].evtMs
			toolSpanID := fmt.Sprintf("%s-tool-%d", llmSpanID, toolCount)
			toolCount++

			// Look up matching after_tool_call by toolCallId.
			var toolDurationMs int64
			status := "ok"
			if prows[j].decision == "block" {
				status = "blocked"
			}

			tcID, _ := prows[j].data["toolCallId"].(string)
			if afterIdx, ok := afterToolByID[tcID]; ok && !prows[afterIdx].consumed {
				prows[afterIdx].consumed = true
				afterData := prows[afterIdx].data
				if dur, ok := afterData["durationMs"].(float64); ok && dur > 0 {
					toolDurationMs = int64(dur)
				} else {
					toolDurationMs = prows[afterIdx].evtMs - toolStartMs
				}
			} else {
				// Blocked or unmatched: gap-fill from next unconsumed event.
				status = "blocked"
				for k := j + 1; k < len(prows); k++ {
					if !prows[k].consumed {
						toolDurationMs = prows[k].evtMs - toolStartMs
						break
					}
				}
			}
			if toolDurationMs < 0 {
				toolDurationMs = 0
			}

			spanName := toolName
			if status == "blocked" {
				spanName = toolName + " (blocked)"
			}

			spans = append(spans, models.TraceSpan{
				SpanID:     toolSpanID,
				ParentID:   llmSpanID,
				Kind:       "tool",
				Name:       spanName,
				StartMs:    toolStartMs,
				DurationMs: toolDurationMs,
				Status:     status,
				Attributes: map[string]string{"tool_name": toolName},
			})

			// ── Step 5: tool_result_persist for this tool call ───────────
			for k := j + 1; k < len(prows); k++ {
				if prows[k].consumed {
					continue
				}
				if prows[k].hookName != "tool_result_persist" {
					continue
				}
				// Attribute to this tool call if it falls within the tool window.
				persistStartMs := prows[k].evtMs
				if persistStartMs < toolStartMs+toolDurationMs || toolDurationMs == 0 {
					prows[k].consumed = true
					var persistDuration int64
					if k+1 < len(prows) {
						persistDuration = prows[k+1].evtMs - persistStartMs
					}
					if persistDuration < 0 {
						persistDuration = 0
					}
					spans = append(spans, models.TraceSpan{
						SpanID:     fmt.Sprintf("%s-persist", toolSpanID),
						ParentID:   toolSpanID,
						Kind:       "tool",
						Name:       "Persist Tool Result",
						StartMs:    persistStartMs,
						DurationMs: persistDuration,
						Status:     "ok",
						Attributes: map[string]string{},
					})
					break
				}
			}
		}
	}

	// ── Step 6: Remaining lifecycle events ───────────────────────────────────
	for i, pr := range prows {
		if pr.consumed {
			continue
		}
		// Skip after_tool_call (already consumed or belongs to no known before_tool_call)
		if pr.hookName == "after_tool_call" {
			continue
		}

		var durationMs int64
		for k := i + 1; k < len(prows); k++ {
			if !prows[k].consumed {
				durationMs = prows[k].evtMs - pr.evtMs
				break
			}
		}
		if durationMs < 0 {
			durationMs = 0
		}

		kind := "lifecycle"
		if pr.hookName == "subagent_spawning" || pr.hookName == "subagent_spawned" || pr.hookName == "subagent_ended" {
			kind = "subagent"
		}

		name := humanName(pr.hookName, pr.toolName)

		spans = append(spans, models.TraceSpan{
			SpanID:     fmt.Sprintf("lifecycle-%d", i),
			ParentID:   runID,
			Kind:       kind,
			Name:       name,
			StartMs:    pr.evtMs,
			DurationMs: durationMs,
			Status:     "ok",
			Attributes: map[string]string{},
		})
	}

	return spans
}
```

- [ ] **Step 4: Run the OpenClaw tests**

```bash
cd backend
CGO_ENABLED=0 go test ./internal/handlers/... -run TestAnalyzeOpenClaw -v
```

Expected: all 4 `TestAnalyzeOpenClaw_*` tests pass.

- [ ] **Step 5: Run all handler tests**

```bash
cd backend
CGO_ENABLED=0 go test ./internal/handlers/... -v
```

Expected: all tests pass (including the fallback and detectAgentType tests from Tasks 2–3).

- [ ] **Step 6: Full compile check**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: exits 0.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/handlers/timeline_openclaw.go \
        backend/internal/handlers/timeline_openclaw_test.go
git commit -m "feat(trace): add OpenClaw hierarchical timeline analyzer"
```

---

## Task 5: Add `"subagent"` kind color to frontend RunWaterfall

**Files:**
- Modify: `frontend/src/pages/AgentDetail.jsx:442-448` (`barColor` function)

- [ ] **Step 1: Add `subagent` branch to `barColor`**

In `frontend/src/pages/AgentDetail.jsx`, update the `barColor` function (around line 442):

```js
function barColor(span) {
  if (span.kind === 'run') return '#ffb0cf'
  if (span.status === 'blocked' || span.status === 'error') return '#ffb4ab'
  if (span.kind === 'llm') return '#93c5fd'
  if (span.kind === 'tool') return '#6ee7b7'
  if (span.kind === 'subagent') return '#c084fc'   // violet — subagent spans
  return '#c4b5fd'                                  // purple — lifecycle
}
```

- [ ] **Step 2: Verify no other kind-based rendering needs updating**

Search for any other `kind` checks in the frontend:

```bash
grep -n "span\.kind\|\.kind ==\|\.kind ===" frontend/src/pages/AgentDetail.jsx
```

Expected: only the `barColor` function references `span.kind`. If any other location is found, add `"subagent"` handling there too.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/AgentDetail.jsx
git commit -m "feat(frontend): add subagent span kind color to RunWaterfall"
```

---

## Task 6: End-to-end compile and test verification

- [ ] **Step 1: Full backend build**

```bash
cd backend
CGO_ENABLED=0 go build -tags embed ./...
```

Expected: exits 0.

- [ ] **Step 2: Full backend test suite**

```bash
cd backend
CGO_ENABLED=0 go test ./... -v 2>&1 | tail -20
```

Expected: all tests pass, no compile errors.

- [ ] **Step 3: Frontend lint**

```bash
cd frontend
npm run build 2>&1 | tail -10
```

Expected: build succeeds with no errors.

- [ ] **Step 4: Final commit (if any fixups needed)**

If any issues were found and fixed in steps 1–3:

```bash
git add -p
git commit -m "fix(trace): address build/lint issues from end-to-end verification"
```

---

## Regression Notes

- Any run from an OpenClaw agent without `agent_type` (legacy data before this deployment) will have `agent_type = ""` and route to `analyzeFallback` — behavior identical to the old endpoint.
- The Proton stream must be recreated (clean deployment) to pick up the `agent_type` column; the DDL constant is correct and `execIfNotExists` will create it on first startup.
- The response JSON shape (`spans`, `span_id`, `parent_id`, `kind`, `name`, `start_ms`, `duration_ms`, `status`, `attributes`) is unchanged. The frontend RunWaterfall reads these fields and requires no structural changes.
