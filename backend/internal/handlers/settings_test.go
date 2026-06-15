package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func putConfig(t *testing.T, cfg *config.Config, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.PUT("/api/config", UpdateConfig(cfg, map[string]bool{}))
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestUpdateConfigHotApplyAndPersist(t *testing.T) {
	cfg := newConfigForTest(t)
	w := putConfig(t, cfg, `{"dlp.enabled": true, "dlp.api_key": "sk-new"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	// Hot-applied to live cfg.
	if !cfg.DLP.Enabled || cfg.DLP.APIKey != "sk-new" {
		t.Errorf("live cfg not hot-applied: %+v", cfg.DLP)
	}
	// Persisted to file.
	reloaded, _ := config.LoadFrom(cfg.ConfigPath)
	if !reloaded.DLP.Enabled || reloaded.DLP.APIKey != "sk-new" {
		t.Error("changes not persisted to file")
	}
}

func TestUpdateConfigRestartKeyNotHotApplied(t *testing.T) {
	cfg := newConfigForTest(t)
	orig := cfg.Server.Port
	w := putConfig(t, cfg, `{"server.port": 9099}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	// Restart key persisted but NOT hot-applied to the live cfg.
	if cfg.Server.Port != orig {
		t.Errorf("restart key was hot-applied: live port = %d", cfg.Server.Port)
	}
	reloaded, _ := config.LoadFrom(cfg.ConfigPath)
	if reloaded.Server.Port != 9099 {
		t.Errorf("restart key not persisted: %d", reloaded.Server.Port)
	}
	var resp struct {
		RestartRequired bool `json:"restart_required"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.RestartRequired {
		t.Error("expected restart_required=true after changing server.port")
	}
}

func TestUpdateConfigRejectsInvalidAtomically(t *testing.T) {
	cfg := newConfigForTest(t)
	w := putConfig(t, cfg, `{"dlp.enabled": true, "server.port": 0}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var resp struct {
		Errors map[string]string `json:"errors"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp.Errors["server.port"]; !ok {
		t.Errorf("expected server.port error, got %v", resp.Errors)
	}
	// Atomic: the valid key in the batch must NOT have been applied.
	if cfg.DLP.Enabled {
		t.Error("partial apply: dlp.enabled changed despite batch error")
	}
}

func TestUpdateConfigRejectsReadOnlyAndOverridden(t *testing.T) {
	cfg := newConfigForTest(t)
	t.Setenv("DLP_BASE_URL", "http://env:4000/v1")

	w := putConfig(t, cfg, `{"setup.complete": false}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("read-only key: status = %d, want 400", w.Code)
	}
	w2 := putConfig(t, cfg, `{"dlp.base_url": "http://x:1/v1"}`)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("env-overridden key: status = %d, want 400", w2.Code)
	}
}

func TestUpdateConfigClearSecret(t *testing.T) {
	cfg := newConfigForTest(t)
	cfg.DLP.APIKey = "sk-old"
	w := putConfig(t, cfg, `{"dlp.api_key": ""}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if cfg.DLP.APIKey != "" {
		t.Error("empty secret should clear the value")
	}
}

func TestTestDLPConfig(t *testing.T) {
	// Stub an OpenAI-compatible /models endpoint.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	cfg := newConfigForTest(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/config/test-dlp", TestDLPConfig(cfg))

	body := `{"base_url":"` + upstream.URL + `","api_key":"sk-test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/config/test-dlp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.OK {
		t.Errorf("expected ok=true, body = %s", w.Body.String())
	}

	// Wrong key → ok:false, still HTTP 200.
	bad := `{"base_url":"` + upstream.URL + `","api_key":"wrong"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/config/test-dlp", strings.NewReader(bad))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("unreachable/auth-fail should be 200 with ok:false, got %d", w2.Code)
	}
	var resp2 struct {
		OK bool `json:"ok"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
	if resp2.OK {
		t.Error("expected ok=false for wrong key")
	}
}
