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

// stubModelServer mimics an OpenAI-compatible endpoint. thinking=true returns
// an empty content with a reasoning field to exercise thinking-model detection.
func stubModelServer(t *testing.T, dim int, thinking bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	mux.HandleFunc("/embeddings", func(w http.ResponseWriter, r *http.Request) {
		emb := make([]float64, dim)
		b, _ := json.Marshal(gin.H{"data": []gin.H{{"embedding": emb}}})
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		msg := gin.H{"content": "ok"}
		if thinking {
			msg = gin.H{"content": "", "reasoning": "thinking..."}
		}
		b, _ := json.Marshal(gin.H{"choices": []gin.H{{"message": msg}}})
		_, _ = w.Write(b)
	})
	return httptest.NewServer(mux)
}

func postTestDLP(t *testing.T, cfg *config.Config, body string) map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	TestDLPConfig(cfg)(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDLPPreflightAllPass(t *testing.T) {
	srv := stubModelServer(t, 1024, false)
	defer srv.Close()
	cfg := &config.Config{DLP: config.DLPConfig{
		BaseURL: srv.URL, EmbeddingModel: "e", EmbeddingDim: 1024,
		RiskModel: "r", MaxOutputTokens: 512,
	}}
	out := postTestDLP(t, cfg, `{"base_url":"`+srv.URL+`"}`)
	if out["ok"] != true {
		t.Fatalf("ok = %v, want true (out=%v)", out["ok"], out)
	}
}

func TestDLPPreflightDimMismatch(t *testing.T) {
	srv := stubModelServer(t, 384, false)
	defer srv.Close()
	cfg := &config.Config{DLP: config.DLPConfig{
		BaseURL: srv.URL, EmbeddingModel: "e", EmbeddingDim: 1024,
		RiskModel: "r", MaxOutputTokens: 512,
	}}
	out := postTestDLP(t, cfg, `{"base_url":"`+srv.URL+`"}`)
	if out["ok"] == true {
		t.Error("ok should be false on dim mismatch")
	}
	checks := out["checks"].(map[string]any)
	emb := checks["embedding"].(map[string]any)
	if emb["dim"].(float64) != 384 || emb["expected"].(float64) != 1024 {
		t.Errorf("embedding check = %v, want dim 384 expected 1024", emb)
	}
}

func TestDLPPreflightThinkingDetected(t *testing.T) {
	srv := stubModelServer(t, 1024, true)
	defer srv.Close()
	cfg := &config.Config{DLP: config.DLPConfig{
		BaseURL: srv.URL, EmbeddingModel: "e", EmbeddingDim: 1024,
		RiskModel: "r", MaxOutputTokens: 512,
	}}
	out := postTestDLP(t, cfg, `{"base_url":"`+srv.URL+`"}`)
	checks := out["checks"].(map[string]any)
	chat := checks["chat"].(map[string]any)
	if chat["thinking_detected"] != true {
		t.Errorf("chat = %v, want thinking_detected true", chat)
	}
	if chat["ok"] != false {
		t.Errorf("chat[ok] = %v, want false when thinking_detected", chat["ok"])
	}
}

func TestDLPPreflightReasoningContentDetected(t *testing.T) {
	// Inline stub: /models 200, /embeddings dim=1024, /chat returns reasoning_content with empty content.
	mux := http.NewServeMux()
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	mux.HandleFunc("/embeddings", func(w http.ResponseWriter, r *http.Request) {
		emb := make([]float64, 1024)
		b, _ := json.Marshal(gin.H{"data": []gin.H{{"embedding": emb}}})
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		// reasoning_content populated, reasoning absent, content empty.
		msg := gin.H{"content": "", "reasoning_content": "thinking..."}
		b, _ := json.Marshal(gin.H{"choices": []gin.H{{"message": msg}}})
		_, _ = w.Write(b)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := &config.Config{DLP: config.DLPConfig{
		BaseURL: srv.URL, EmbeddingModel: "e", EmbeddingDim: 1024,
		RiskModel: "r", MaxOutputTokens: 512,
	}}
	out := postTestDLP(t, cfg, `{"base_url":"`+srv.URL+`"}`)
	checks := out["checks"].(map[string]any)
	chat := checks["chat"].(map[string]any)
	if chat["thinking_detected"] != true {
		t.Errorf("chat = %v, want thinking_detected true for reasoning_content", chat)
	}
	if chat["ok"] != false {
		t.Errorf("chat[ok] = %v, want false when thinking_detected via reasoning_content", chat["ok"])
	}
}

func TestDLPPreflightReachableNon2xx(t *testing.T) {
	// Stand up a server whose /models returns 401 (reachable but non-2xx).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := &config.Config{DLP: config.DLPConfig{
		BaseURL: srv.URL, EmbeddingModel: "e", EmbeddingDim: 1024,
		RiskModel: "r", MaxOutputTokens: 512,
	}}
	out := postTestDLP(t, cfg, `{"base_url":"`+srv.URL+`"}`)

	// Top-level ok must be false.
	if out["ok"] != false {
		t.Errorf("ok = %v, want false for non-2xx /models", out["ok"])
	}
	// reachable check must report ok:false.
	checks := out["checks"].(map[string]any)
	reachable := checks["reachable"].(map[string]any)
	if reachable["ok"] != false {
		t.Errorf("reachable[ok] = %v, want false for 401 /models", reachable["ok"])
	}
	// embedding and chat checks are short-circuited when reachable fails.
	if _, hasEmbedding := checks["embedding"]; hasEmbedding {
		t.Error("embedding check should be absent when reachable fails")
	}
	if _, hasChat := checks["chat"]; hasChat {
		t.Error("chat check should be absent when reachable fails")
	}
}

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
	// Use stubModelServer (dim=1024, thinking=false) for a full-pass endpoint.
	upstream := stubModelServer(t, 1024, false)
	defer upstream.Close()

	cfg := newConfigForTest(t)
	cfg.DLP.EmbeddingModel = "e"
	cfg.DLP.EmbeddingDim = 1024
	cfg.DLP.RiskModel = "r"
	cfg.DLP.MaxOutputTokens = 512
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/config/test-dlp", TestDLPConfig(cfg))

	body := `{"base_url":"` + upstream.URL + `"}`
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

	// Unreachable URL → ok:false, still HTTP 200.
	bad := `{"base_url":"http://127.0.0.1:1"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/config/test-dlp", strings.NewReader(bad))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("unreachable should be 200 with ok:false, got %d", w2.Code)
	}
	var resp2 struct {
		OK bool `json:"ok"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
	if resp2.OK {
		t.Error("expected ok=false for unreachable URL")
	}
}

func TestDLPConfigMultipleChatModels(t *testing.T) {
	srv := stubModelServer(t, 1024, false)
	defer srv.Close()
	cfg := &config.Config{DLP: config.DLPConfig{
		BaseURL: srv.URL, EmbeddingModel: "e", EmbeddingDim: 1024,
		RiskModel: "r", MaxOutputTokens: 512,
	}}
	// Duplicate "a" must be deduped to a single chat check.
	out := postTestDLP(t, cfg, `{"base_url":"`+srv.URL+`","chat_models":["a","b","a"]}`)
	if out["ok"] != true {
		t.Fatalf("ok = %v, want true (out=%v)", out["ok"], out)
	}
	checks := out["checks"].(map[string]any)
	chats, ok := checks["chats"].([]any)
	if !ok {
		t.Fatalf("checks.chats missing or wrong type: %v", checks["chats"])
	}
	if len(chats) != 2 {
		t.Fatalf("len(chats) = %d, want 2 (deduped)", len(chats))
	}
	models := map[string]bool{}
	for _, ch := range chats {
		m := ch.(map[string]any)
		if m["ok"] != true {
			t.Errorf("chat %v not ok", m)
		}
		models[m["model"].(string)] = true
	}
	if !models["a"] || !models["b"] {
		t.Errorf("chats models = %v, want a and b", models)
	}
	// Back-compat: single checks.chat still present.
	if _, has := checks["chat"]; !has {
		t.Error("checks.chat must remain present for back-compat")
	}
}

func TestDLPConfigChatModelsFallback(t *testing.T) {
	// No chat_models → falls back to [risk_model]; checks.chats has 1 entry.
	srv := stubModelServer(t, 1024, false)
	defer srv.Close()
	cfg := &config.Config{DLP: config.DLPConfig{
		BaseURL: srv.URL, EmbeddingModel: "e", EmbeddingDim: 1024,
		RiskModel: "r", MaxOutputTokens: 512,
	}}
	out := postTestDLP(t, cfg, `{"base_url":"`+srv.URL+`"}`)
	checks := out["checks"].(map[string]any)
	chats := checks["chats"].([]any)
	if len(chats) != 1 {
		t.Fatalf("len(chats) = %d, want 1", len(chats))
	}
	if chats[0].(map[string]any)["model"] != "r" {
		t.Errorf("fallback chat model = %v, want r", chats[0])
	}
}

func TestDLPConfigEmbeddingUnknownDim(t *testing.T) {
	// embedding_dim:-1 → require a non-empty vector, not an exact match.
	// Server returns dim 768 while config says 1024; must still pass.
	srv := stubModelServer(t, 768, false)
	defer srv.Close()
	cfg := &config.Config{DLP: config.DLPConfig{
		BaseURL: srv.URL, EmbeddingModel: "e", EmbeddingDim: 1024,
		RiskModel: "r", MaxOutputTokens: 512,
	}}
	out := postTestDLP(t, cfg, `{"base_url":"`+srv.URL+`","embedding_dim":-1}`)
	if out["ok"] != true {
		t.Fatalf("ok = %v, want true for unknown-dim (out=%v)", out["ok"], out)
	}
	checks := out["checks"].(map[string]any)
	emb := checks["embedding"].(map[string]any)
	if emb["ok"] != true {
		t.Errorf("embedding.ok = %v, want true when dim>0 and expected<=0", emb["ok"])
	}
	if emb["dim"].(float64) != 768 {
		t.Errorf("embedding.dim = %v, want 768 (detected reported)", emb["dim"])
	}
}

// embedding_dim is now auto-detected from the model at provision, and every other
// DLP setting is hot via the config mutable stream, so nothing flags a config
// re-provision anymore — reprovisionRequired is always false.
func TestReprovisionRequiredAlwaysFalse(t *testing.T) {
	base := &config.Config{}
	base.DLP.Enabled = true
	base.DLP.EmbeddingModel, base.DLP.EmbeddingDim = "e", 1024
	base.ProvisionedDLP = config.ProvisionedDLP{Recorded: true, EmbeddingModel: "e", EmbeddingDim: 1024}

	if reprovisionRequired(base) {
		t.Fatal("no re-provision flag expected at baseline")
	}
	for name, mutate := range map[string]func(*config.Config){
		"embedding_dim":   func(c *config.Config) { c.DLP.EmbeddingDim = 1536 },
		"embedding_model": func(c *config.Config) { c.DLP.EmbeddingModel = "e2" },
		"risk_model":      func(c *config.Config) { c.DLP.RiskModel = "r2" },
		"max_tokens":      func(c *config.Config) { c.DLP.MaxOutputTokens = 4096 },
	} {
		d := *base
		mutate(&d)
		if reprovisionRequired(&d) {
			t.Errorf("%s change must NOT flag re-provision (dim auto-detected; others hot)", name)
		}
	}
}
