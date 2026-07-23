package config_test

import (
	"path/filepath"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

func TestSave_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")

	cfg := &config.Config{
		Server:        config.ServerConfig{Port: 9090},
		Timeplus:      config.TimeplusConfig{Host: "myhost", Port: 3218, NativePort: 8463},
		AgentGuardURL: "http://example.com:8080",
		SetupComplete: true,
		Sentry: config.SentryConfig{
			Enabled:               true,
			SkillDir:              "/custom/skills",
			ChatBaseURL:           "http://c/v1",
			ChatAPIKey:            "sk-c",
			ChatModel:             "gpt-4o",
			RequestTimeoutSeconds: 45,
			MaxOutputTokens:       2048,
			MemoryEnabled:         true,
			EmbeddingBaseURL:      "http://e/v1",
			EmbeddingAPIKey:       "sk-e",
			EmbeddingModel:        "nomic",
			EmbeddingDim:          768,
		},
	}
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if loaded.Timeplus.Host != "myhost" {
		t.Errorf("host: want myhost, got %s", loaded.Timeplus.Host)
	}
	if !loaded.SetupComplete {
		t.Error("want setup_complete=true")
	}
	if loaded.Timeplus.User != "" {
		t.Errorf("user should not be saved, got %q", loaded.Timeplus.User)
	}
	if loaded.Timeplus.Password != "" {
		t.Errorf("password should not be saved, got %q", loaded.Timeplus.Password)
	}
	if loaded.Server.Port != 9090 {
		t.Errorf("port: want 9090, got %d", loaded.Server.Port)
	}
	if loaded.Timeplus.NativePort != 8463 {
		t.Errorf("native_port: want 8463, got %d", loaded.Timeplus.NativePort)
	}
	if loaded.AgentGuardURL != "http://example.com:8080" {
		t.Errorf("agentguard_url: want http://example.com:8080, got %s", loaded.AgentGuardURL)
	}
	if !loaded.Sentry.Enabled {
		t.Error("want sentry.enabled=true")
	}
	if loaded.Sentry.SkillDir != "/custom/skills" {
		t.Errorf("sentry.skill_dir: want /custom/skills, got %s", loaded.Sentry.SkillDir)
	}
	if loaded.Sentry.ChatBaseURL != "http://c/v1" {
		t.Errorf("sentry.chat_base_url: want http://c/v1, got %s", loaded.Sentry.ChatBaseURL)
	}
	if loaded.Sentry.ChatAPIKey != "" {
		t.Errorf("sentry.chat_api_key: want empty, got %s", loaded.Sentry.ChatAPIKey)
	} // chat_api_key is a secret — never written to config.yaml by Save() (matches dlp.api_key)
	if loaded.Sentry.ChatModel != "gpt-4o" {
		t.Errorf("sentry.chat_model: want gpt-4o, got %s", loaded.Sentry.ChatModel)
	}
	if !loaded.Sentry.MemoryEnabled {
		t.Error("want sentry.memory_enabled=true")
	}
	if loaded.Sentry.EmbeddingBaseURL != "http://e/v1" {
		t.Errorf("sentry.embedding_base_url: want http://e/v1, got %s", loaded.Sentry.EmbeddingBaseURL)
	}
	if loaded.Sentry.EmbeddingAPIKey != "" {
		t.Errorf("sentry.embedding_api_key: want empty, got %s", loaded.Sentry.EmbeddingAPIKey)
	} // embedding_api_key is a secret — never written to config.yaml by Save()
	if loaded.Sentry.EmbeddingModel != "nomic" {
		t.Errorf("sentry.embedding_model: want nomic, got %s", loaded.Sentry.EmbeddingModel)
	}
	if loaded.Sentry.EmbeddingDim != 768 {
		t.Errorf("sentry.embedding_dim: want 768, got %d", loaded.Sentry.EmbeddingDim)
	}
	if loaded.Sentry.RequestTimeoutSeconds != 45 {
		t.Errorf("sentry.request_timeout_seconds: want 45, got %d", loaded.Sentry.RequestTimeoutSeconds)
	}
	if loaded.Sentry.MaxOutputTokens != 2048 {
		t.Errorf("sentry.max_output_tokens: want 2048, got %d", loaded.Sentry.MaxOutputTokens)
	}
}
