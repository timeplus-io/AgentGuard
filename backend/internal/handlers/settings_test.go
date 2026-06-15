package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
)

func newConfigForTest(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("dlp:\n  base_url: http://seed:4000/v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConfigPath = path
	return cfg
}

func TestGetConfigShapeAndSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newConfigForTest(t)
	cfg.DLP.APIKey = "sk-secret"

	r := gin.New()
	r.GET("/api/config", GetConfig(cfg, map[string]bool{}))

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		RestartRequired     bool `json:"restart_required"`
		ReprovisionRequired bool `json:"reprovision_required"`
		Sections            []struct {
			ID   string `json:"id"`
			Keys []struct {
				Key      string `json:"key"`
				Type     string `json:"type"`
				Value    any    `json:"value"`
				Set      bool   `json:"set"`
				Source   string `json:"source"`
				Editable bool   `json:"editable"`
			} `json:"keys"`
		} `json:"sections"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var sawSecret, sawFileSource bool
	for _, s := range resp.Sections {
		for _, k := range s.Keys {
			if k.Key == "dlp.api_key" {
				sawSecret = true
				if k.Value != nil {
					t.Error("secret value must not be echoed")
				}
				if !k.Set {
					t.Error("secret should report set=true")
				}
			}
			if k.Key == "dlp.base_url" {
				if k.Source != "file" {
					t.Errorf("dlp.base_url source = %q, want file", k.Source)
				}
				sawFileSource = true
			}
			if k.Key == "setup.complete" && k.Editable {
				t.Error("setup.complete must not be editable")
			}
		}
	}
	if !sawSecret || !sawFileSource {
		t.Error("expected dlp.api_key and dlp.base_url in response")
	}
}

func TestGetConfigEnvLocksKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newConfigForTest(t)
	t.Setenv("DLP_API_KEY", "sk-env")

	r := gin.New()
	r.GET("/api/config", GetConfig(cfg, map[string]bool{}))
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp struct {
		Sections []struct {
			Keys []struct {
				Key      string `json:"key"`
				Source   string `json:"source"`
				Editable bool   `json:"editable"`
				EnvVar   string `json:"env_var"`
			} `json:"keys"`
		} `json:"sections"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	for _, s := range resp.Sections {
		for _, k := range s.Keys {
			if k.Key == "dlp.api_key" {
				if k.Source != "env" || k.Editable {
					t.Errorf("env-overridden key: source=%q editable=%v", k.Source, k.Editable)
				}
				if k.EnvVar != "DLP_API_KEY" {
					t.Errorf("env_var = %q", k.EnvVar)
				}
			}
		}
	}
}
