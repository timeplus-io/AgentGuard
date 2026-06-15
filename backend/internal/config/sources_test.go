package config

import (
	"os"
	"path/filepath"
	"testing"
)


func TestCoerceAndValidate(t *testing.T) {
	cases := []struct {
		key     string
		raw     any
		wantErr bool
		want    any
	}{
		{"server.port", float64(9090), false, 9090},      // JSON numbers arrive as float64
		{"server.port", float64(0), true, nil},           // out of range
		{"server.port", float64(70000), true, nil},       // out of range
		{"server.port", "abc", true, nil},                // wrong type
		{"dlp.enabled", true, false, true},
		{"dlp.enabled", "yes", true, nil},                // wrong type
		{"holds.fail_policy", "allow", false, "allow"},
		{"holds.fail_policy", "maybe", true, nil},        // not in enum
		{"dlp.embedding_dim", float64(768), false, 768},
		{"dlp.embedding_dim", float64(0), true, nil},     // must be >= 1
		{"dlp.base_url", "http://x:4000/v1", false, "http://x:4000/v1"},
		{"dlp.base_url", "not a url", true, nil},
		{"agentguard.url", "https://h:8080", false, "https://h:8080"},
		{"dlp.api_key", "sk-123", false, "sk-123"},
		{"holds.timeout_seconds", float64(-1), true, nil},
		{"nope.key", "x", true, nil},                     // unknown key
	}
	for _, c := range cases {
		got, err := CoerceAndValidate(c.key, c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s=%v: expected error, got nil", c.key, c.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s=%v: unexpected error %v", c.key, c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s=%v: got %v (%T), want %v (%T)", c.key, c.raw, got, got, c.want, c.want)
		}
	}
}

func TestResolveSource(t *testing.T) {
	fileKeys := map[string]bool{"dlp.base_url": true}
	flags := map[string]bool{"port": true}

	m, _ := MetaFor("server.port")
	if got := ResolveSource(m, fileKeys, flags); got != SourceFlag {
		t.Errorf("server.port: got %v, want flag", got)
	}

	t.Setenv("DLP_API_KEY", "sk-env")
	mk, _ := MetaFor("dlp.api_key")
	if got := ResolveSource(mk, fileKeys, flags); got != SourceEnv {
		t.Errorf("dlp.api_key: got %v, want env", got)
	}

	mb, _ := MetaFor("dlp.base_url")
	if got := ResolveSource(mb, fileKeys, flags); got != SourceFile {
		t.Errorf("dlp.base_url: got %v, want file", got)
	}

	mh, _ := MetaFor("timeplus.host")
	if got := ResolveSource(mh, fileKeys, flags); got != SourceDefault {
		t.Errorf("timeplus.host: got %v, want default", got)
	}
}

func TestEffectiveAndApply(t *testing.T) {
	cfg := &Config{}
	cfg.DLP.Enabled = false
	cfg.Server.Port = 8080

	if got := EffectiveValue(cfg, "server.port"); got != 8080 {
		t.Errorf("EffectiveValue server.port: got %v", got)
	}
	if err := ApplyChange(cfg, "server.port", 9090); err != nil {
		t.Fatalf("ApplyChange: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("ApplyChange server.port: cfg=%d", cfg.Server.Port)
	}
	if err := ApplyChange(cfg, "dlp.enabled", true); err != nil {
		t.Fatalf("ApplyChange dlp.enabled: %v", err)
	}
	if !cfg.DLP.Enabled {
		t.Error("ApplyChange dlp.enabled: cfg still false")
	}
	if err := ApplyChange(cfg, "dlp.api_key", "sk-x"); err != nil {
		t.Fatalf("ApplyChange dlp.api_key: %v", err)
	}
	if cfg.DLP.APIKey != "sk-x" {
		t.Error("ApplyChange dlp.api_key not applied")
	}
}

func TestFileKeysAndSaveChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	seed := "" +
		"server:\n  port: 8080\n" +
		"dlp:\n  enabled: false\n" +
		"provisioned:\n  dlp:\n    recorded: true\n    embedding_dim: 1536\n"
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	keys, err := FileKeys(path)
	if err != nil {
		t.Fatalf("FileKeys: %v", err)
	}
	if !keys["server.port"] || !keys["dlp.enabled"] {
		t.Errorf("FileKeys missing expected keys: %v", keys)
	}

	// Merge-write only changed keys; unrelated keys must survive.
	err = SaveChanges(path, map[string]any{
		"dlp.enabled": true,
		"dlp.api_key": "sk-merge",
	})
	if err != nil {
		t.Fatalf("SaveChanges: %v", err)
	}

	reloaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !reloaded.DLP.Enabled {
		t.Error("dlp.enabled not persisted")
	}
	if reloaded.DLP.APIKey != "sk-merge" {
		t.Error("dlp.api_key not persisted")
	}
	if reloaded.Server.Port != 8080 {
		t.Errorf("unrelated server.port lost: %d", reloaded.Server.Port)
	}
	// provisioned.dlp.* must survive a merge-write.
	k2, _ := FileKeys(path)
	if !k2["provisioned.dlp.recorded"] {
		t.Error("provisioned.dlp.recorded lost on merge-write")
	}

	// File mode must be 0600.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
}
