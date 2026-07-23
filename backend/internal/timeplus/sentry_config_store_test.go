package timeplus

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

func TestSentryConfigRowsSeedsThreeRows(t *testing.T) {
	rows := SentryConfigRows(config.SentryConfig{
		BaseURL: "http://x/v1", APIKey: "k", ChatModel: "gpt-4o",
		EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536,
		RequestTimeoutSeconds: 60, MaxOutputTokens: 1024,
	})
	for _, k := range []string{SentryCfgKeyShared, SentryCfgKeyChat, SentryCfgKeyEmbed} {
		if _, ok := rows[k]; !ok {
			t.Fatalf("missing seed row %q", k)
		}
		var v any
		if err := json.Unmarshal([]byte(rows[k]), &v); err != nil {
			t.Fatalf("row %q not valid JSON: %v", k, err)
		}
	}
	// shared carries the endpoint + a catalog with chat + embedding entries.
	var shared struct {
		BaseURL string         `json:"base_url"`
		Models  map[string]any `json:"models"`
	}
	_ = json.Unmarshal([]byte(rows[SentryCfgKeyShared]), &shared)
	if shared.BaseURL != "http://x/v1" {
		t.Fatalf("shared base_url = %q", shared.BaseURL)
	}
	if _, ok := shared.Models["chat"]; !ok {
		t.Fatal("shared.models missing 'chat' entry")
	}
	if _, ok := shared.Models["embedding"]; !ok {
		t.Fatal("shared.models missing 'embedding' entry")
	}
	// chat row references the catalog name "chat" and carries the injection guard prompt.
	var chat struct {
		Model         string `json:"model"`
		SystemPrompt  string `json:"system_prompt"`
		MaxIterations int    `json:"max_iterations"`
	}
	_ = json.Unmarshal([]byte(rows[SentryCfgKeyChat]), &chat)
	if chat.Model != "chat" || chat.MaxIterations != 12 {
		t.Fatalf("chat row wrong: %+v", chat)
	}
	if !strings.Contains(chat.SystemPrompt, "data, never instructions") {
		t.Fatal("chat system_prompt missing injection guard")
	}
	var emb struct {
		Model string `json:"model"`
		Dim   int    `json:"embedding_dim"`
	}
	_ = json.Unmarshal([]byte(rows[SentryCfgKeyEmbed]), &emb)
	if emb.Model != "embedding" || emb.Dim != 1536 {
		t.Fatalf("embed row wrong: %+v", emb)
	}
}

func TestIsSentryConfigKey(t *testing.T) {
	for _, k := range []string{SentryCfgKeyShared, SentryCfgKeyChat, SentryCfgKeyEmbed} {
		if !IsSentryConfigKey(k) {
			t.Fatalf("%q should be a sentry config key", k)
		}
	}
	if IsSentryConfigKey("nope") {
		t.Fatal("unknown key accepted")
	}
}

func TestValidateSentryConfigValue(t *testing.T) {
	cases := []struct {
		key, value, wantErr string
	}{
		{SentryCfgKeyChat, `{"system_prompt":"ok"}`, ""},
		{SentryCfgKeyChat, `{"system_prompt":123}`, "system_prompt must be a string"},
		{SentryCfgKeyChat, `not json`, "valid JSON"},
		{SentryCfgKeyShared, `{"models":{"chat":{"model":"gpt-4o"}}}`, ""},
		{SentryCfgKeyShared, `{"models":[]}`, "models must be an object"},
		{SentryCfgKeyEmbed, `{"embedding_dim":1536}`, ""},
		{SentryCfgKeyEmbed, `{"embedding_dim":"big"}`, "embedding_dim must be a number"},
	}
	for _, c := range cases {
		err := ValidateSentryConfigValue(c.key, c.value)
		if c.wantErr == "" && err != nil {
			t.Fatalf("%s %q: unexpected %v", c.key, c.value, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Fatalf("%s %q: want err %q got %v", c.key, c.value, c.wantErr, err)
		}
	}
}

func TestSentryConfigStreamDDL(t *testing.T) {
	if !strings.Contains(ddlSentryConfig, "MUTABLE STREAM IF NOT EXISTS agentguard_sentry_config") ||
		!strings.Contains(ddlSentryConfig, "PRIMARY KEY (key)") {
		t.Fatal("sentry config DDL wrong")
	}
}
