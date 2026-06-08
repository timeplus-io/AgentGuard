package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/timeplus/agentguard/internal/config"
)

func isolateConfigEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"TIMEPLUS", "TIMEPLUS_HOST", "TIMEPLUS_PORT", "TIMEPLUS_NATIVE_PORT",
		"TIMEPLUS_USER", "TIMEPLUS_PASSWORD",
		"SERVER_PORT", "CATALOG_DIR", "AGENTGUARD_URL", "SETUP_COMPLETE",
		"AGENTGUARD_HOLD_MV_WAIT_MS", "AGENTGUARD_HOLD_TIMEOUT_SECONDS",
		"AGENTGUARD_HOLD_FAIL_POLICY", "AGENTGUARD_HOLDS_DISABLED",
	} {
		if v, ok := os.LookupEnv(k); ok {
			os.Unsetenv(k)
			kk, vv := k, v
			t.Cleanup(func() { os.Setenv(kk, vv) })
		}
	}
	viper.Reset()
	t.Cleanup(viper.Reset)
}

func TestRootCommandDefaultPFlagsDoNotOverrideExplicitConfig(t *testing.T) {
	isolateConfigEnv(t)

	path := filepath.Join(t.TempDir(), "agentguard.yaml")
	content := "" +
		"server:\n  port: 9999\n" +
		"catalog:\n  dir: /custom/catalog\n" +
		"timeplus:\n  host: tphost\n  port: 1234\n  native-port: 5678\n" +
		"agentguard:\n  url: http://agentguard.test\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := newRootCmd()
	if err := cmd.Flags().Set("config", path); err != nil {
		t.Fatalf("set config flag: %v", err)
	}
	if err := cmd.PersistentPreRunE(cmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 9999 {
		t.Errorf("Server.Port = %d, want config file value 9999", cfg.Server.Port)
	}
	if cfg.CatalogDir != "/custom/catalog" {
		t.Errorf("CatalogDir = %q, want config file value /custom/catalog", cfg.CatalogDir)
	}
	if cfg.Timeplus.Host != "tphost" {
		t.Errorf("Timeplus.Host = %q, want config file value tphost", cfg.Timeplus.Host)
	}
	if cfg.Timeplus.Port != 1234 {
		t.Errorf("Timeplus.Port = %d, want config file value 1234", cfg.Timeplus.Port)
	}
	if cfg.Timeplus.NativePort != 5678 {
		t.Errorf("Timeplus.NativePort = %d, want config file value 5678", cfg.Timeplus.NativePort)
	}
	if cfg.AgentGuardURL != "http://agentguard.test" {
		t.Errorf("AgentGuardURL = %q, want config file value http://agentguard.test", cfg.AgentGuardURL)
	}
}
