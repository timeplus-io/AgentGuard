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
	blocked := spans[1]
	if blocked.Status != "blocked" {
		t.Errorf("blocked event status = %q, want blocked", blocked.Status)
	}
	if blocked.Attributes["block_reason"] != "policy violation" {
		t.Errorf("block_reason = %q, want 'policy violation'", blocked.Attributes["block_reason"])
	}
	if blocked.DurationMs != 1000 {
		t.Errorf("blocked event duration_ms = %d, want 1000", blocked.DurationMs)
	}
}
