package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

func TestDLPDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.DLP.Enabled {
		t.Error("DLP.Enabled default should be false (opt-in feature)")
	}
	if cfg.DLP.BaseURL != "http://localhost:4000/v1" {
		t.Errorf("BaseURL default = %q", cfg.DLP.BaseURL)
	}
	if cfg.DLP.EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("EmbeddingModel default = %q", cfg.DLP.EmbeddingModel)
	}
	if cfg.DLP.EmbeddingDim != 1536 {
		t.Errorf("EmbeddingDim default = %d", cfg.DLP.EmbeddingDim)
	}
	if cfg.DLP.RiskModel != "gpt-4o-mini" {
		t.Errorf("RiskModel default = %q", cfg.DLP.RiskModel)
	}
	if cfg.DLP.SummaryModel != "gpt-4o" {
		t.Errorf("SummaryModel default = %q", cfg.DLP.SummaryModel)
	}
}

func TestDLPFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
dlp:
  enabled: true
  base_url: http://ollama:11434/v1
  embedding_model: nomic-embed-text
  embedding_dim: 768
  risk_model: qwen2.5:3b
  summary_model: llama3:70b
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !cfg.DLP.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.DLP.BaseURL != "http://ollama:11434/v1" {
		t.Errorf("BaseURL = %q", cfg.DLP.BaseURL)
	}
	if cfg.DLP.EmbeddingDim != 768 {
		t.Errorf("EmbeddingDim = %d", cfg.DLP.EmbeddingDim)
	}
	if cfg.DLP.RiskModel != "qwen2.5:3b" || cfg.DLP.SummaryModel != "llama3:70b" {
		t.Errorf("models = %q / %q", cfg.DLP.RiskModel, cfg.DLP.SummaryModel)
	}
}

func TestDLPAPIKeyFromEnv(t *testing.T) {
	isolate(t)
	t.Setenv("DLP_API_KEY", "sk-test-123")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DLP.APIKey != "sk-test-123" {
		t.Errorf("APIKey = %q, want sk-test-123", cfg.DLP.APIKey)
	}
	if cfg.DLP.Enabled {
		t.Error("Enabled should default to false when only DLP_API_KEY is set")
	}
	if cfg.DLP.EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("EmbeddingModel = %q, want default", cfg.DLP.EmbeddingModel)
	}
}

func TestDLPSaveRoundTrip(t *testing.T) {
	// Provision rewrites config.yaml via Save; the dlp section must survive
	// (except api_key, which is never written). Save must also not drop the
	// holds section — both sections are written in the same call.
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := config.LoadFrom(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DLP.Enabled = true
	cfg.DLP.BaseURL = "http://litellm:4000/v1"
	cfg.DLP.APIKey = "sk-never-saved"
	cfg.Holds.TimeoutSeconds = 120
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !reloaded.DLP.Enabled || reloaded.DLP.BaseURL != "http://litellm:4000/v1" {
		t.Errorf("dlp section lost on Save round-trip: %+v", reloaded.DLP)
	}
	if reloaded.DLP.APIKey != "" {
		t.Error("api_key must never be written to config.yaml")
	}
	if reloaded.Holds.TimeoutSeconds != 120 {
		t.Errorf("Holds.TimeoutSeconds = %d after round-trip, want 120 (Save must not drop holds section)", reloaded.Holds.TimeoutSeconds)
	}
}
