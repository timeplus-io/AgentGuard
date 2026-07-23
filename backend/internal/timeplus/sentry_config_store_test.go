package timeplus

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

func TestSentryConfigRowsSeedsChatAndMemory(t *testing.T) {
	rows := SentryConfigRows(config.SentryConfig{
		ChatBaseURL: "https://api.openai.com/v1", ChatAPIKey: "sk", ChatModel: "gpt-4o",
		RequestTimeoutSeconds: 60, MaxOutputTokens: 1024,
		MemoryEnabled: true, EmbeddingBaseURL: "http://localhost:11434/v1",
		EmbeddingAPIKey: "", EmbeddingModel: "nomic-embed-text", EmbeddingDim: 768,
	})
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d (%v)", len(rows), rows)
	}
	// chat: self-contained endpoint, NO system_prompt, literal model.
	var chat map[string]any
	if err := json.Unmarshal([]byte(rows[SentryCfgKeyChat]), &chat); err != nil {
		t.Fatal(err)
	}
	if chat["base_url"] != "https://api.openai.com/v1" || chat["model"] != "gpt-4o" {
		t.Fatalf("chat endpoint wrong: %v", chat)
	}
	if _, has := chat["system_prompt"]; has {
		t.Fatal("chat row must NOT contain system_prompt")
	}
	if chat["max_iterations"].(float64) != 12 {
		t.Fatalf("chat budgets missing: %v", chat)
	}
	// memory: enable flag + own embedding endpoint.
	var mem map[string]any
	if err := json.Unmarshal([]byte(rows[SentryCfgKeyMemory]), &mem); err != nil {
		t.Fatal(err)
	}
	if mem["enabled"] != true || mem["base_url"] != "http://localhost:11434/v1" ||
		mem["model"] != "nomic-embed-text" || mem["embedding_dim"].(float64) != 768 {
		t.Fatalf("memory row wrong: %v", mem)
	}
}

func TestSentryConfigKeysAreChatMemory(t *testing.T) {
	got := SentryConfigKeys()
	if len(got) != 2 || !IsSentryConfigKey("chat") || !IsSentryConfigKey("memory") ||
		IsSentryConfigKey("shared") || IsSentryConfigKey("embed") {
		t.Fatalf("keys wrong: %v", got)
	}
}

func TestValidateSentryConfigValue(t *testing.T) {
	cases := []struct{ key, value, wantErr string }{
		{"chat", `{"base_url":"x","model":"y"}`, ""},
		{"chat", `not json`, "valid JSON"},
		{"memory", `{"enabled":false}`, ""},
		{"memory", `{"enabled":"yes"}`, "enabled must be a bool"},
		{"memory", `{"embedding_dim":"big"}`, "embedding_dim must be a number"},
		{"memory", `{"embedding_dim":768}`, ""},
	}
	for _, c := range cases {
		err := ValidateSentryConfigValue(c.key, c.value)
		if c.wantErr == "" && err != nil {
			t.Fatalf("%s %q: unexpected %v", c.key, c.value, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Fatalf("%s %q: want %q got %v", c.key, c.value, c.wantErr, err)
		}
	}
}

func TestSentryConfigStreamDDL(t *testing.T) {
	if !strings.Contains(ddlSentryConfig, "MUTABLE STREAM IF NOT EXISTS agentguard_sentry_config") ||
		!strings.Contains(ddlSentryConfig, "PRIMARY KEY (key)") {
		t.Fatal("sentry config DDL wrong")
	}
}
