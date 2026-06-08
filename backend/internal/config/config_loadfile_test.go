package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/timeplus/agentguard/internal/config"
)

// isolate makes config.Load() deterministic regardless of the host environment:
//   - HOME and CWD point at empty temp dirs so the default search (".",
//     "$HOME/.agentguard") finds nothing.
//   - supported config env vars are cleared (and restored) unless a test sets
//     them explicitly.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, k := range []string{
		"TIMEPLUS", "TIMEPLUS_HOST", "TIMEPLUS_PORT", "TIMEPLUS_NATIVE_PORT",
		"TIMEPLUS_USER", "TIMEPLUS_PASSWORD",
		"SERVER_PORT", "CATALOG_DIR", "AGENTGUARD_URL", "SETUP_COMPLETE",
		"AGENTGUARD_HOLD_MV_WAIT_MS", "AGENTGUARD_HOLD_TIMEOUT_SECONDS",
		"AGENTGUARD_HOLDS_FAIL_POLICY", "AGENTGUARD_HOLDS_DISABLED",
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
	t.Setenv("TIMEPLUS", "proton://default@localhost:8463/default")

	dir := t.TempDir()
	path := filepath.Join(dir, "agentguard.yaml")
	content := "" +
		"server:\n  port: 9999\n" +
		"catalog:\n  dir: /custom/catalog\n" +
		"timeplus:\n  host: tphost\n  port: 1234\n  native-port: 5678\n" +
		"agentguard:\n  url: http://agentguard.test\n" +
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
	if cfg.Timeplus.NativePort != 5678 {
		t.Errorf("Timeplus.NativePort = %d, want 5678 (explicit --config ignored)", cfg.Timeplus.NativePort)
	}
	if cfg.AgentGuardURL != "http://agentguard.test" {
		t.Errorf("AgentGuardURL = %q, want http://agentguard.test (explicit --config ignored)", cfg.AgentGuardURL)
	}
	if !cfg.SetupComplete {
		t.Errorf("SetupComplete = false, want true (explicit --config ignored)")
	}
}

func TestLoad_ExplicitEnvOverridesConfig(t *testing.T) {
	isolate(t)
	t.Setenv("TIMEPLUS", "proton://default@localhost:8463/default")
	t.Setenv("TIMEPLUS_HOST", "envhost")
	t.Setenv("TIMEPLUS_PORT", "2222")
	t.Setenv("TIMEPLUS_NATIVE_PORT", "3333")
	t.Setenv("SERVER_PORT", "4444")
	t.Setenv("CATALOG_DIR", "/env/catalog")
	t.Setenv("AGENTGUARD_URL", "http://env-agentguard.test")
	t.Setenv("SETUP_COMPLETE", "true")
	t.Setenv("AGENTGUARD_HOLD_MV_WAIT_MS", "1500")
	t.Setenv("AGENTGUARD_HOLD_TIMEOUT_SECONDS", "120")
	t.Setenv("AGENTGUARD_HOLDS_FAIL_POLICY", "allow")
	t.Setenv("AGENTGUARD_HOLDS_DISABLED", "true")
	t.Setenv("TIMEPLUS_USER", "svc")
	t.Setenv("TIMEPLUS_PASSWORD", "secret")

	dir := t.TempDir()
	path := filepath.Join(dir, "agentguard.yaml")
	content := "" +
		"server:\n  port: 9999\n" +
		"catalog:\n  dir: /file/catalog\n" +
		"timeplus:\n  host: filehost\n  port: 1234\n  native-port: 5678\n" +
		"agentguard:\n  url: http://file-agentguard.test\n" +
		"setup:\n  complete: false\n" +
		"holds:\n  mv_wait_ms: 500\n  timeout_seconds: 300\n  fail_policy: deny\n  disabled: false\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	viper.SetConfigFile(path)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.Port != 4444 {
		t.Errorf("Server.Port = %d, want env override 4444", cfg.Server.Port)
	}
	if cfg.CatalogDir != "/env/catalog" {
		t.Errorf("CatalogDir = %q, want env override /env/catalog", cfg.CatalogDir)
	}
	if cfg.AgentGuardURL != "http://env-agentguard.test" {
		t.Errorf("AgentGuardURL = %q, want env override http://env-agentguard.test", cfg.AgentGuardURL)
	}
	if cfg.Timeplus.Host != "envhost" {
		t.Errorf("Timeplus.Host = %q, want env override envhost", cfg.Timeplus.Host)
	}
	if cfg.Timeplus.Port != 2222 {
		t.Errorf("Timeplus.Port = %d, want env override 2222", cfg.Timeplus.Port)
	}
	if cfg.Timeplus.NativePort != 3333 {
		t.Errorf("Timeplus.NativePort = %d, want env override 3333", cfg.Timeplus.NativePort)
	}
	if !cfg.SetupComplete {
		t.Errorf("SetupComplete = false, want env override true")
	}
	if cfg.Holds.MVWaitMs != 1500 {
		t.Errorf("Holds.MVWaitMs = %d, want env override 1500", cfg.Holds.MVWaitMs)
	}
	if cfg.Holds.TimeoutSeconds != 120 {
		t.Errorf("Holds.TimeoutSeconds = %d, want env override 120", cfg.Holds.TimeoutSeconds)
	}
	if cfg.Holds.FailPolicy != "allow" {
		t.Errorf("Holds.FailPolicy = %q, want env override allow", cfg.Holds.FailPolicy)
	}
	if !cfg.Holds.Disabled {
		t.Errorf("Holds.Disabled = false, want env override true")
	}
	if cfg.Timeplus.User != "svc" {
		t.Errorf("Timeplus.User = %q, want env override svc", cfg.Timeplus.User)
	}
	if cfg.Timeplus.Password != "secret" {
		t.Errorf("Timeplus.Password = %q, want env override secret", cfg.Timeplus.Password)
	}
}

func TestLoad_MissingExplicitConfigFileErrors(t *testing.T) {
	isolate(t)

	path := filepath.Join(t.TempDir(), "missing.yaml")
	viper.SetConfigFile(path)
	_, err := config.Load()
	if err == nil {
		t.Fatal("Load returned nil error for missing explicit config file")
	}
	if !strings.Contains(err.Error(), "reading explicit config file") {
		t.Fatalf("Load error = %q, want explicit config file context", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("Load error = %q, want missing path %q", err, path)
	}
}

func TestLoad_ClampsHoldTimeoutSeconds(t *testing.T) {
	isolate(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "agentguard.yaml")
	content := "holds:\n  timeout_seconds: 999\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	viper.SetConfigFile(path)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Holds.TimeoutSeconds != 540 {
		t.Errorf("Holds.TimeoutSeconds = %d, want clamp to 540", cfg.Holds.TimeoutSeconds)
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
