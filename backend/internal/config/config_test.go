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
}
