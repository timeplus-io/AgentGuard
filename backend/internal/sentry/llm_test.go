package sentry

import (
	"encoding/json"
	"strings"
	"testing"

	tp "github.com/timeplus/agentguard/internal/timeplus"
)

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

func TestLoadChatCfgSQLTargetsSentryStream(t *testing.T) {
	sql := chatCfgResolveSQL()
	if !strings.Contains(sql, "agentguard_sentry_config") || !strings.Contains(sql, "key='chat'") {
		t.Fatalf("expected chat row read, got: %s", sql)
	}
	if strings.Contains(sql, "dlp_config_resolve") || strings.Contains(sql, "agentguard_dlp_config") {
		t.Fatalf("must not use the DLP resolver/stream: %s", sql)
	}
}

func TestChatCfgIgnoresConfigSystemPrompt(t *testing.T) {
	// A system_prompt in the row JSON must NOT override the built-in constant.
	var cc ChatCfg
	_ = json.Unmarshal([]byte(`{"base_url":"x","model":"y","system_prompt":"HACKED"}`), &cc)
	cc.applyDefaults()
	if cc.SystemPrompt != tp.DefaultSentrySystemPrompt {
		t.Fatalf("system prompt must be the built-in constant, got %q", cc.SystemPrompt)
	}
}
