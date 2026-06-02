package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// TestLoadCLIConfig_HonorsConfigFile verifies the shared --config semantics used
// by both `serve` and `up`: setup.complete is read from the file (past viper's
// AutomaticEnv nested-key limitation) and ConfigPath points at that same file.
func TestLoadCLIConfig_HonorsConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "agentguard.yaml")
	if err := os.WriteFile(cfgPath, []byte("setup:\n  complete: true\nserver:\n  port: 1234\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	viper.Reset()
	t.Cleanup(viper.Reset)

	cmd := &cobra.Command{}
	cmd.Flags().String("config", cfgPath, "")

	cfg, err := loadCLIConfig(cmd)
	if err != nil {
		t.Fatalf("loadCLIConfig: %v", err)
	}
	if !cfg.SetupComplete {
		t.Error("setup.complete from --config file should be read as true (would re-provision otherwise)")
	}
	if cfg.ConfigPath != cfgPath {
		t.Errorf("ConfigPath: want %q, got %q (Provision would write the wrong file)", cfgPath, cfg.ConfigPath)
	}
}
