package handlers

import (
	"testing"
	"time"
)

// row is a tiny helper to build a CIM-event row map for tests.
func row(eventType string, fields map[string]any) map[string]any {
	r := map[string]any{
		"event_time": time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		"event_type": eventType,
	}
	for k, v := range fields {
		r[k] = v
	}
	return r
}

func TestBuildTimeline_KindMapping(t *testing.T) {
	rows := []map[string]any{
		row("session_start", nil),
		row("user_input", map[string]any{"user_message": "hello"}),
		row("llm_response", map[string]any{"assistant_message": "hi there", "model": "claude-sonnet-4-6", "provider": "anthropic"}),
		row("tool_invoke", map[string]any{"tool_name": "Bash", "tool_input": `{"command":"ls"}`, "raw_event_data": `{"toolCallId":"tc-1"}`}),
		row("tool_complete", map[string]any{"tool_name": "Bash", "tool_result": `{"stdout":"a"}`, "tool_success": true, "raw_event_data": `{"toolCallId":"tc-1"}`}),
		row("session_end", nil),
	}

	got := buildTimeline(rows)
	if len(got) != 6 {
		t.Fatalf("expected 6 events, got %d", len(got))
	}

	wantKinds := []string{"session_start", "user_input", "llm_response", "tool_request", "tool_result", "session_end"}
	for i, want := range wantKinds {
		if got[i].Kind != want {
			t.Errorf("event %d: kind = %q, want %q", i, got[i].Kind, want)
		}
	}

	if got[1].Body != "hello" {
		t.Errorf("user_input body = %q, want %q", got[1].Body, "hello")
	}
	if got[2].Body != "hi there" || got[2].Model != "claude-sonnet-4-6" {
		t.Errorf("llm_response body/model = %q/%q", got[2].Body, got[2].Model)
	}
	if got[3].Body != `{"command":"ls"}` || got[3].ToolName != "Bash" || got[3].ToolCallID != "tc-1" {
		t.Errorf("tool_request mismatch: %+v", got[3])
	}
	if got[4].Body != `{"stdout":"a"}` || got[4].ToolCallID != "tc-1" || got[4].Status != "ok" {
		t.Errorf("tool_result mismatch: %+v", got[4])
	}
}

func TestBuildTimeline_TurnIndex(t *testing.T) {
	rows := []map[string]any{
		row("session_start", nil),                                   // turn 0
		row("user_input", map[string]any{"user_message": "first"}),  // turn 1
		row("llm_response", map[string]any{"assistant_message": "a"}),
		row("user_input", map[string]any{"user_message": "second"}), // turn 2
		row("llm_response", map[string]any{"assistant_message": "b"}),
	}
	got := buildTimeline(rows)
	wantTurns := []int{0, 1, 1, 2, 2}
	for i, want := range wantTurns {
		if got[i].TurnIndex != want {
			t.Errorf("event %d: turn_index = %d, want %d", i, got[i].TurnIndex, want)
		}
	}
}

func TestBuildTimeline_ToolCallIDSpellings(t *testing.T) {
	rows := []map[string]any{
		row("tool_invoke", map[string]any{"raw_event_data": `{"tool_call_id":"hermes-7"}`}),  // Hermes
		row("tool_invoke", map[string]any{"raw_event_data": `{"toolCallId":"openclaw-3"}`}),   // OpenClaw
		row("tool_invoke", map[string]any{"raw_event_data": `{"foo":"bar"}`}),                 // Claude Code: none
	}
	got := buildTimeline(rows)
	if got[0].ToolCallID != "hermes-7" {
		t.Errorf("hermes tool_call_id = %q, want hermes-7", got[0].ToolCallID)
	}
	if got[1].ToolCallID != "openclaw-3" {
		t.Errorf("openclaw tool_call_id = %q, want openclaw-3", got[1].ToolCallID)
	}
	if got[2].ToolCallID != "" {
		t.Errorf("claudecode tool_call_id = %q, want empty", got[2].ToolCallID)
	}
}

func TestBuildTimeline_Status(t *testing.T) {
	rows := []map[string]any{
		row("tool_invoke", map[string]any{"hook_decision": "block", "block_reason": "policy"}),
		row("tool_complete", map[string]any{"tool_success": false}),
		row("tool_complete", map[string]any{"tool_success": true}),
	}
	got := buildTimeline(rows)
	if got[0].Status != "blocked" {
		t.Errorf("blocked invoke status = %q, want blocked", got[0].Status)
	}
	if got[0].Attributes["block_reason"] != "policy" {
		t.Errorf("block_reason attr = %q, want policy", got[0].Attributes["block_reason"])
	}
	if got[1].Status != "error" {
		t.Errorf("failed tool status = %q, want error", got[1].Status)
	}
	if got[2].Status != "ok" {
		t.Errorf("ok tool status = %q, want ok", got[2].Status)
	}
}

func TestBuildTimeline_SkipsUnknownTypes(t *testing.T) {
	rows := []map[string]any{
		row("user_input", map[string]any{"user_message": "x"}),
		row("prompt_context", map[string]any{"raw_event_data": "{}"}),
		row("other", nil),
	}
	got := buildTimeline(rows)
	if len(got) != 1 {
		t.Fatalf("expected 1 event (unknowns skipped), got %d", len(got))
	}
	if got[0].Kind != "user_input" {
		t.Errorf("kept kind = %q, want user_input", got[0].Kind)
	}
}

func TestBuildTimeline_EmptyReturnsNonNil(t *testing.T) {
	got := buildTimeline(nil)
	if got == nil {
		t.Fatal("buildTimeline(nil) returned nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 events, got %d", len(got))
	}
}

func TestParseToolCallID(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`{"toolCallId":"a"}`, "a"},
		{`{"tool_call_id":"b"}`, "b"},
		{`{"toolCallId":"a","tool_call_id":"b"}`, "a"}, // toolCallId wins (tried first)
		{`{"other":"x"}`, ""},
		{``, ""},
		{`not json`, ""},
	}
	for _, c := range cases {
		if got := parseToolCallID(c.raw); got != c.want {
			t.Errorf("parseToolCallID(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}
