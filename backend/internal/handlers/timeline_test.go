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
