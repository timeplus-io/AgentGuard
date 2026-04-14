package handlers

import (
	"testing"

	"github.com/timeplus/agentguard/internal/models"
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
		makeRow("before_agent_start", "2026-04-11 10:00:00.000", "", "observe", ""),
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
