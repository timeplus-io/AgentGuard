package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/timeplus/agentguard/internal/config"
)

// isolate makes config.Load() deterministic regardless of the host environment:
//   - HOME and CWD point at empty temp dirs so the default search (".",
//     "$HOME/.agentguard") finds nothing.
//   - interfering env vars are cleared (and restored). Notably a bare TIMEPLUS
//     env var — viper's AutomaticEnv treats it as shadowing the whole
//     "timeplus.*" subtree, zeroing host/port — and the per-key overrides.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, k := range []string{
		"TIMEPLUS", "TIMEPLUS_HOST", "TIMEPLUS_PORT", "TIMEPLUS_NATIVE-PORT",
		"SERVER_PORT", "CATALOG_DIR", "AGENTGUARD_URL", "SETUP_COMPLETE",
	} {
		if v, ok := os.LookupEnv(k); ok {
			os.Unsetenv(k)
			kk, vv := k, v
			t.Cleanup(func() { os.Setenv(kk, vv) })
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	viper.Reset()
	t.Cleanup(viper.Reset)
}

// TestLoad_HonorsExplicitConfigFile reproduces the runServer path:
// viper.SetConfigFile(--config) followed by config.Load(). All fields in the
// file must be honored, not just setup.complete.
func TestLoad_HonorsExplicitConfigFile(t *testing.T) {
	isolate(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "agentguard.yaml")
	content := "" +
		"server:\n  port: 9999\n" +
		"catalog:\n  dir: /custom/catalog\n" +
		"timeplus:\n  host: tphost\n  port: 1234\n" +
		"setup:\n  complete: true\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	viper.SetConfigFile(path)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Port != 9999 {
		t.Errorf("Server.Port = %d, want 9999 (explicit --config ignored)", cfg.Server.Port)
	}
	if cfg.CatalogDir != "/custom/catalog" {
		t.Errorf("CatalogDir = %q, want /custom/catalog (explicit --config ignored)", cfg.CatalogDir)
	}
	if cfg.Timeplus.Host != "tphost" {
		t.Errorf("Timeplus.Host = %q, want tphost (explicit --config ignored)", cfg.Timeplus.Host)
	}
	if cfg.Timeplus.Port != 1234 {
		t.Errorf("Timeplus.Port = %d, want 1234 (explicit --config ignored)", cfg.Timeplus.Port)
	}
	if !cfg.SetupComplete {
		t.Errorf("SetupComplete = false, want true (explicit --config ignored)")
	}
}

// TestLoad_DefaultsWithoutConfigFile guards the no-explicit-file path: the fix
// must not break default loading (returns defaults, no error).
func TestLoad_DefaultsWithoutConfigFile(t *testing.T) {
	isolate(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want default 8080", cfg.Server.Port)
	}
	if cfg.Timeplus.Port != 3218 {
		t.Errorf("Timeplus.Port = %d, want default 3218", cfg.Timeplus.Port)
	}
	if cfg.CatalogDir != "./catalog" {
		t.Errorf("CatalogDir = %q, want default ./catalog", cfg.CatalogDir)
	}
}
