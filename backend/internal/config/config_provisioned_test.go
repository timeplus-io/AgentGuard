package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndSaveProvisionedSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := &Config{}
	cfg.Server.Port = 8080
	cfg.ProvisionedDLP.Recorded = true
	cfg.ProvisionedDLP.EmbeddingModel = "text-embedding-3-small"
	cfg.ProvisionedDLP.EmbeddingDim = 1536
	cfg.ProvisionedDLP.RiskModel = "gpt-4o-mini"
	cfg.ProvisionedDLP.SummaryModel = "gpt-4o"
	cfg.ProvisionedDLP.RequestTimeoutSeconds = 45
	cfg.ProvisionedDLP.MaxOutputTokens = 300

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !got.ProvisionedDLP.Recorded {
		t.Error("Recorded not round-tripped")
	}
	if got.ProvisionedDLP.EmbeddingDim != 1536 {
		t.Errorf("EmbeddingDim = %d, want 1536", got.ProvisionedDLP.EmbeddingDim)
	}
	if got.ProvisionedDLP.RiskModel != "gpt-4o-mini" {
		t.Errorf("RiskModel = %q", got.ProvisionedDLP.RiskModel)
	}
	if got.ProvisionedDLP.RequestTimeoutSeconds != 45 {
		t.Errorf("RequestTimeoutSeconds = %d, want 45", got.ProvisionedDLP.RequestTimeoutSeconds)
	}
	if got.ProvisionedDLP.MaxOutputTokens != 300 {
		t.Errorf("MaxOutputTokens = %d, want 300", got.ProvisionedDLP.MaxOutputTokens)
	}

	// Save must NOT persist dlp.api_key (avoid duplicating env secret).
	cfg.DLP.APIKey = "sk-should-not-write"
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	keys, _ := FileKeys(path)
	if keys["dlp.api_key"] {
		t.Error("Save wrote dlp.api_key; it must not")
	}
}

func TestLoadFromMissingSnapshotDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProvisionedDLP.Recorded {
		t.Error("Recorded should default to false when absent")
	}
}
