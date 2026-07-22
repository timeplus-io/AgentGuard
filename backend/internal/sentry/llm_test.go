package sentry

import "testing"

func TestChatCfgDefaultsApplied(t *testing.T) {
	cc := ChatCfg{}
	cc.applyDefaults()
	if cc.MaxIterations != 12 || cc.SQLMaxRows != 1000 || cc.SQLTimeoutMs != 30000 ||
		cc.ToolResultMaxBytes != 16384 || cc.TurnTokenBudget != 32000 || cc.MaxTokens != 1024 {
		t.Fatalf("defaults not applied: %+v", cc)
	}
	if cc.SystemPrompt == "" {
		t.Fatalf("expected default system prompt to be filled in")
	}
}

func TestChatCfgClamps(t *testing.T) {
	cc := ChatCfg{MaxIterations: 500, SQLMaxRows: 100000, SQLTimeoutMs: 600000}
	cc.applyDefaults()
	if cc.MaxIterations != 32 || cc.SQLMaxRows != 5000 || cc.SQLTimeoutMs != 55000 {
		t.Fatalf("clamps not applied: %+v", cc)
	}
}
