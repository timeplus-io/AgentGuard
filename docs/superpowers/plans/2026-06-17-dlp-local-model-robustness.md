# DLP Local-Model Robustness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Semantic DLP usable with real local LLMs (Ollama) by adding configurable model-call timeout + output-token budget, a thinking-model content fallback, deeper `test-dlp` preflight validation, and local-model setup docs.

**Architecture:** Two new global `dlp.*` config knobs flow through the existing config registry and are baked into the six DLP Python UDFs at provision time. The chat UDFs gain a `_msg_content` helper that falls back to the `reasoning` field when `content` is empty. `POST /api/config/test-dlp` is upgraded from a bare `GET /models` to structured embedding + chat checks. The Settings UI renders the structured result.

**Tech Stack:** Go 1.x (Gin, Viper), Timeplus Python UDFs (OpenAI-compatible HTTP), React/Vite frontend.

**Spec:** `docs/superpowers/specs/2026-06-17-dlp-local-model-robustness-design.md`

## Global Constraints

- Build/test Go with `CGO_ENABLED=0` (macOS 26+ dyld abort otherwise).
- All DLP UDFs remain **fail-open**: any exception falls back to existing defaults (`_fallback`, `_heuristic`, `0.5` rerank, `''` summary). Never raise.
- `POST /api/config/test-dlp` always returns HTTP 200; check failures are reported in-band, never 5xx.
- `dlp.request_timeout_seconds`: default `50`, clamped to **[5, 55]** (must stay under the 60s `protonHTTPClient` query ceiling in `internal/timeplus/client.go`).
- `dlp.max_output_tokens`: default `512`, clamped to **[16, 4096]**. `dlp_summarize` uses `max(value, 800)`.
- Both new keys are `Reprovision: true` (baked into UDF DDL; applied by `POST /api/rules/sync` or the setup wizard).
- Follow existing code patterns; the API key is interpolated into UDF DDL (accepted limitation, see `dlp.go`).

---

### Task 1: Config knobs (struct, load/save, registry, validation)

Add `dlp.request_timeout_seconds` and `dlp.max_output_tokens` through every config layer. `registry_test.go::TestRegistryCoversAllKeys` requires that any key in the registry is also handled by `EffectiveValue` and `ApplyChange`, so all layers move together in this task.

**Files:**
- Modify: `backend/internal/config/config.go` (struct ~43-51, defaults ~121-127, load ~180-186, bindEnv ~221-227, Save ~246-251, plus a load() clamp)
- Modify: `backend/internal/config/registry.go` (add 2 entries after line 118)
- Modify: `backend/internal/config/sources.go` (`validateInt` ~77-99, `EffectiveValue` dlp block ~169-182, `ApplyChange` dlp block ~213-226)
- Test: `backend/internal/config/config_dlp_test.go`, `backend/internal/config/sources_test.go`

**Interfaces:**
- Produces: `config.DLPConfig.RequestTimeoutSeconds int`, `config.DLPConfig.MaxOutputTokens int`; registry keys `dlp.request_timeout_seconds`, `dlp.max_output_tokens`; env vars `DLP_REQUEST_TIMEOUT_SECONDS`, `DLP_MAX_OUTPUT_TOKENS`. Task 2 reads the two struct fields.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/config/config_dlp_test.go`:

```go
func TestDLPTimeoutTokenDefaults(t *testing.T) {
	cfg, err := LoadFrom(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DLP.RequestTimeoutSeconds != 50 {
		t.Errorf("RequestTimeoutSeconds = %d, want 50", cfg.DLP.RequestTimeoutSeconds)
	}
	if cfg.DLP.MaxOutputTokens != 512 {
		t.Errorf("MaxOutputTokens = %d, want 512", cfg.DLP.MaxOutputTokens)
	}
}

func TestDLPTimeoutClampOnLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("dlp:\n  request_timeout_seconds: 999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DLP.RequestTimeoutSeconds != 55 {
		t.Errorf("RequestTimeoutSeconds = %d, want clamped to 55", cfg.DLP.RequestTimeoutSeconds)
	}
}
```

Add `"os"` and `"path/filepath"` to the test file imports if not present.

Append to `backend/internal/config/sources_test.go` inside a new function:

```go
func TestCoerceDLPTimeoutTokens(t *testing.T) {
	if _, err := CoerceAndValidate("dlp.request_timeout_seconds", float64(3)); err == nil {
		t.Error("expected error for timeout below 5")
	}
	if _, err := CoerceAndValidate("dlp.request_timeout_seconds", float64(99)); err == nil {
		t.Error("expected error for timeout above 55")
	}
	if v, err := CoerceAndValidate("dlp.request_timeout_seconds", float64(30)); err != nil || v.(int) != 30 {
		t.Errorf("got (%v,%v), want (30,nil)", v, err)
	}
	if _, err := CoerceAndValidate("dlp.max_output_tokens", float64(8)); err == nil {
		t.Error("expected error for tokens below 16")
	}
	if v, err := CoerceAndValidate("dlp.max_output_tokens", float64(512)); err != nil || v.(int) != 512 {
		t.Errorf("got (%v,%v), want (512,nil)", v, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/... -run 'TestDLPTimeoutTokenDefaults|TestDLPTimeoutClampOnLoad|TestCoerceDLPTimeoutTokens' -v`
Expected: FAIL (fields/keys don't exist; defaults are 0).

- [ ] **Step 3: Add struct fields**

In `backend/internal/config/config.go`, extend `DLPConfig`:

```go
type DLPConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	BaseURL        string `mapstructure:"base_url"`
	APIKey         string `mapstructure:"api_key"`
	EmbeddingModel string `mapstructure:"embedding_model"`
	EmbeddingDim   int    `mapstructure:"embedding_dim"`
	RiskModel      string `mapstructure:"risk_model"`
	SummaryModel   string `mapstructure:"summary_model"`
	// RequestTimeoutSeconds is the per-call timeout (seconds) on every DLP UDF
	// model request. Clamped to [5,55] so query-funnel UDFs return before the
	// 60s protonHTTPClient ceiling.
	RequestTimeoutSeconds int `mapstructure:"request_timeout_seconds"`
	// MaxOutputTokens caps chat-UDF completions (risk/understand/rerank);
	// dlp_summarize uses max(MaxOutputTokens, 800).
	MaxOutputTokens int `mapstructure:"max_output_tokens"`
}
```

- [ ] **Step 4: Add defaults, load reads, env binds, save, and load clamp**

In `load()` defaults block (after `dlp.summary_model`):

```go
	v.SetDefault("dlp.request_timeout_seconds", 50)
	v.SetDefault("dlp.max_output_tokens", 512)
```

In `load()` where DLP fields are read (after `cfg.DLP.SummaryModel = ...`):

```go
	cfg.DLP.RequestTimeoutSeconds = v.GetInt("dlp.request_timeout_seconds")
	cfg.DLP.MaxOutputTokens = v.GetInt("dlp.max_output_tokens")
```

In `load()`, after the existing `holdTimeoutCeilingSec` clamp block, add:

```go
	if cfg.DLP.RequestTimeoutSeconds < 5 {
		cfg.DLP.RequestTimeoutSeconds = 5
	} else if cfg.DLP.RequestTimeoutSeconds > 55 {
		log.Printf("config: dlp.request_timeout_seconds=%d exceeds 55; clamping", cfg.DLP.RequestTimeoutSeconds)
		cfg.DLP.RequestTimeoutSeconds = 55
	}
	if cfg.DLP.MaxOutputTokens < 16 {
		cfg.DLP.MaxOutputTokens = 16
	} else if cfg.DLP.MaxOutputTokens > 4096 {
		cfg.DLP.MaxOutputTokens = 4096
	}
```

In `bindEnv()` (after `dlp.summary_model`):

```go
	_ = v.BindEnv("dlp.request_timeout_seconds", "DLP_REQUEST_TIMEOUT_SECONDS")
	_ = v.BindEnv("dlp.max_output_tokens", "DLP_MAX_OUTPUT_TOKENS")
```

In `Save()` (after `v.Set("dlp.summary_model", ...)`):

```go
	v.Set("dlp.request_timeout_seconds", cfg.DLP.RequestTimeoutSeconds)
	v.Set("dlp.max_output_tokens", cfg.DLP.MaxOutputTokens)
```

- [ ] **Step 5: Add registry entries**

In `backend/internal/config/registry.go`, after the `dlp.summary_model` entry (line ~118):

```go
		{Key: "dlp.request_timeout_seconds", Section: "dlp", Label: "Model request timeout (s)",
			Description: "Per-call timeout for DLP model requests. 5–55 (must stay under the 60s query client). Baked into UDFs — change needs re-provision.",
			Type:        TypeInt, EnvVar: "DLP_REQUEST_TIMEOUT_SECONDS", Reprovision: true},
		{Key: "dlp.max_output_tokens", Section: "dlp", Label: "Max output tokens",
			Description: "Token budget for risk/understand/rerank model calls (summary uses at least 800). Raise for reasoning models. Baked into UDFs — change needs re-provision.",
			Type:        TypeInt, EnvVar: "DLP_MAX_OUTPUT_TOKENS", Reprovision: true},
```

- [ ] **Step 6: Add validation, EffectiveValue, ApplyChange cases**

In `sources.go` `validateInt`, add cases:

```go
	case "dlp.request_timeout_seconds":
		if v < 5 || v > 55 {
			return errors.New("must be between 5 and 55")
		}
	case "dlp.max_output_tokens":
		if v < 16 || v > 4096 {
			return errors.New("must be between 16 and 4096")
		}
```

In `EffectiveValue`, after `case "dlp.summary_model"`:

```go
	case "dlp.request_timeout_seconds":
		return cfg.DLP.RequestTimeoutSeconds
	case "dlp.max_output_tokens":
		return cfg.DLP.MaxOutputTokens
```

In `ApplyChange`, after `case "dlp.summary_model"`:

```go
	case "dlp.request_timeout_seconds":
		return setInt(&cfg.DLP.RequestTimeoutSeconds, value)
	case "dlp.max_output_tokens":
		return setInt(&cfg.DLP.MaxOutputTokens, value)
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/... -v`
Expected: PASS (new tests + existing `TestRegistryCoversAllKeys`, `TestDLPSaveRoundTrip`, etc.).

- [ ] **Step 8: Commit**

```bash
git add backend/internal/config/
git commit -m "feat(dlp): add request_timeout_seconds + max_output_tokens config knobs (#184)"
```

---

### Task 2: Parameterize UDFs (timeout, max_tokens, thinking-model fallback)

Thread the two knobs into the six DLP UDF templates and add the `_msg_content` fallback to the four chat UDFs.

**Files:**
- Modify: `backend/internal/timeplus/dlp.go` (`dlpEmbedUDFTemplate` ~106-139, `dlpRiskUDFTemplate` ~146-206, `DLPEmbedUDFDDL` ~141-143, `DLPRiskUDFDDL` ~208-210)
- Modify: `backend/internal/timeplus/dlp_query.go` (`dlpUnderstandUDFTemplate` ~22-78, `dlpRerankUDFTemplate` ~83-129, `dlpSummarizeUDFTemplate` ~134-168, `dlpReplacer` ~173-179, builders ~183-197)
- Test: `backend/internal/timeplus/dlp_test.go`, `backend/internal/timeplus/dlp_query_test.go`

**Interfaces:**
- Consumes: `config.DLPConfig.RequestTimeoutSeconds`, `config.DLPConfig.MaxOutputTokens` (Task 1).
- Produces: unchanged exported builder signatures (`DLPEmbedUDFDDL`, `DLPRiskUDFDDL`, `DLPUnderstandUDFDDL`, `DLPRerankUDFDDL`, `DLPSummarizeUDFDDL`, all `func(config.DLPConfig) string`). A new helper `dlpReplacerWith(cfg config.DLPConfig, model string, maxTokens int) *strings.Replacer`.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/timeplus/dlp_query_test.go`:

```go
func TestUDFParameterization(t *testing.T) {
	cfg := config.DLPConfig{
		BaseURL: "http://x/v1", EmbeddingModel: "e", RiskModel: "r",
		SummaryModel: "s", RequestTimeoutSeconds: 45, MaxOutputTokens: 300,
	}
	cases := map[string]string{
		"risk":       DLPRiskUDFDDL(cfg),
		"understand": DLPUnderstandUDFDDL(cfg),
		"rerank":     DLPRerankUDFDDL(cfg),
		"summarize":  DLPSummarizeUDFDDL(cfg),
	}
	for name, ddl := range cases {
		if !strings.Contains(ddl, "timeout=45") {
			t.Errorf("%s: missing timeout=45", name)
		}
		if !strings.Contains(ddl, "_msg_content") {
			t.Errorf("%s: missing _msg_content fallback helper", name)
		}
	}
	// risk/understand/rerank use the configured 300; summarize floors at 800.
	if !strings.Contains(cases["risk"], "'max_tokens': 300") {
		t.Error("risk: want max_tokens 300")
	}
	if !strings.Contains(cases["summarize"], "'max_tokens': 800") {
		t.Error("summarize: want max_tokens floored to 800")
	}
	// embed only needs the timeout knob.
	if !strings.Contains(DLPEmbedUDFDDL(cfg), "timeout=45") {
		t.Error("embed: missing timeout=45")
	}
}

func TestSummarizeTokenFloorHonorsLargerValue(t *testing.T) {
	cfg := config.DLPConfig{BaseURL: "http://x/v1", SummaryModel: "s",
		RequestTimeoutSeconds: 50, MaxOutputTokens: 1000}
	if !strings.Contains(DLPSummarizeUDFDDL(cfg), "'max_tokens': 1000") {
		t.Error("summarize: want max_tokens 1000 when configured above 800")
	}
}
```

Ensure the test file imports `"strings"` and `"github.com/timeplus/agentguard/internal/config"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/... -run 'TestUDFParameterization|TestSummarizeTokenFloorHonorsLargerValue' -v`
Expected: FAIL (templates have hardcoded `timeout=30`, no `_msg_content`, no `{{TIMEOUT}}`).

- [ ] **Step 3: Extend the replacer in `dlp_query.go`**

Replace `dlpReplacer` (lines ~173-179) with:

```go
// dlpReplacerWith fills the UDF template placeholders. maxTokens lets
// dlp_summarize raise its budget above the global dlp.max_output_tokens.
func dlpReplacerWith(cfg config.DLPConfig, model string, maxTokens int) *strings.Replacer {
	return strings.NewReplacer(
		"{{BASE_URL}}", cfg.BaseURL,
		"{{API_KEY}}", cfg.APIKey,
		"{{MODEL}}", model,
		"{{TIMEOUT}}", strconv.Itoa(cfg.RequestTimeoutSeconds),
		"{{MAX_TOKENS}}", strconv.Itoa(maxTokens),
	)
}

// dlpReplacer is the common case: timeout + the global max_output_tokens.
func dlpReplacer(cfg config.DLPConfig, model string) *strings.Replacer {
	return dlpReplacerWith(cfg, model, cfg.MaxOutputTokens)
}
```

Add `"strconv"` to the `dlp_query.go` imports.

- [ ] **Step 4: Point the summarize builder at the floored budget**

Replace `DLPSummarizeUDFDDL` (lines ~193-197) with:

```go
func DLPSummarizeUDFDDL(cfg config.DLPConfig) string {
	tokens := cfg.MaxOutputTokens
	if tokens < 800 {
		tokens = 800
	}
	return dlpReplacerWith(cfg, cfg.SummaryModel, tokens).Replace(dlpSummarizeUDFTemplate)
}
```

- [ ] **Step 5: Parameterize the query UDF templates + add fallback**

In `dlpUnderstandUDFTemplate`, change the request call:

```python
    resp = requests.post(BASE_URL + '/chat/completions', headers=headers, json={
        'model': MODEL,
        'messages': [{'role': 'user', 'content': prompt}],
        'temperature': 0,
        'max_tokens': {{MAX_TOKENS}},
    }, timeout={{TIMEOUT}})
    raw = _msg_content(resp)
```

and add the helper near the top of the template's Python body (after the `BASE_URL`/`API_KEY`/`MODEL` lines):

```python
def _msg_content(resp):
    m = resp.json()['choices'][0]['message']
    return (m.get('content') or '').strip() or \
           (m.get('reasoning') or m.get('reasoning_content') or '').strip()
```

Repeat the same two edits in `dlpRerankUDFTemplate` (replace `'max_tokens': 400` → `'max_tokens': {{MAX_TOKENS}}`, `timeout=30` → `timeout={{TIMEOUT}}`, `raw = resp.json()...['content']` → `raw = _msg_content(resp)`, add `_msg_content` def) and in `dlpSummarizeUDFTemplate` (replace `'max_tokens': 800` → `'max_tokens': {{MAX_TOKENS}}`, `timeout=60` → `timeout={{TIMEOUT}}`, change the final `return resp.json()['choices'][0]['message']['content']` to `return _msg_content(resp)`, add `_msg_content` def).

- [ ] **Step 6: Parameterize the ingest UDF templates in `dlp.go`**

In `dlpEmbedUDFTemplate`, change `timeout=30` → `timeout={{TIMEOUT}}` (embeddings have no `max_tokens`; no fallback needed).

In `dlpRiskUDFTemplate`, inside `_classify`: change `'max_tokens': 100` → `'max_tokens': {{MAX_TOKENS}}`, `timeout=30` → `timeout={{TIMEOUT}}`, `raw = resp.json()['choices'][0]['message']['content']` → `raw = _msg_content(resp)`, and add the `_msg_content` def after the `MODEL = "{{MODEL}}"` line.

(`DLPEmbedUDFDDL` and `DLPRiskUDFDDL` already call `dlpReplacer(cfg, model)`, which now fills `{{TIMEOUT}}`/`{{MAX_TOKENS}}` — no builder change needed.)

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/... -v`
Expected: PASS (new tests + existing DLP DDL tests).

- [ ] **Step 8: Compile-check the whole backend**

Run: `cd backend && CGO_ENABLED=0 go build ./...`
Expected: no output (success).

- [ ] **Step 9: Commit**

```bash
git add backend/internal/timeplus/
git commit -m "feat(dlp): parameterize UDF timeout/max_tokens + reasoning-field fallback (#184)"
```

---

### Task 3: Deeper test-dlp preflight validation

Upgrade `POST /api/config/test-dlp` from `GET /models` to structured reachability + embedding + chat checks.

**Files:**
- Modify: `backend/internal/handlers/settings.go` (`TestDLPConfig`)
- Test: `backend/internal/handlers/settings_test.go` (create if absent)

**Interfaces:**
- Consumes: live `cfg.DLP` (BaseURL, APIKey, EmbeddingModel, EmbeddingDim, RiskModel, MaxOutputTokens).
- Produces: response JSON `{"ok": bool, "checks": {"reachable": {...}, "embedding": {"ok": bool, "dim": int, "expected": int}, "chat": {"ok": bool, "thinking_detected": bool, "note": string}}}`. Optional request fields `embedding_model`, `embedding_dim`, `risk_model` override `cfg.DLP`.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/handlers/settings_test.go`:

```go
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/... -run 'TestDLPPreflight' -v`
Expected: FAIL (handler returns `{"ok":...}` with no `checks`).

- [ ] **Step 3: Rewrite `TestDLPConfig`**

Replace the `TestDLPConfig` function in `backend/internal/handlers/settings.go` with:

```go
// TestDLPConfig validates a candidate DLP model endpoint: reachability, a real
// embedding call (dimension match), and a real chat call (non-empty content,
// thinking-model detection). Always HTTP 200; failures are reported in-band.
func TestDLPConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			BaseURL        string `json:"base_url"`
			APIKey         string `json:"api_key"`
			EmbeddingModel string `json:"embedding_model"`
			EmbeddingDim   int    `json:"embedding_dim"`
			RiskModel      string `json:"risk_model"`
		}
		_ = c.ShouldBindJSON(&body)

		config.Mu.RLock()
		d := cfg.DLP
		config.Mu.RUnlock()

		baseURL := strings.TrimRight(orStr(body.BaseURL, d.BaseURL), "/")
		key := orStr(body.APIKey, d.APIKey)
		embModel := orStr(body.EmbeddingModel, d.EmbeddingModel)
		expectedDim := body.EmbeddingDim
		if expectedDim == 0 {
			expectedDim = d.EmbeddingDim
		}
		riskModel := orStr(body.RiskModel, d.RiskModel)
		maxTokens := d.MaxOutputTokens
		if maxTokens <= 0 {
			maxTokens = 512
		}

		if _, err := url.ParseRequestURI(baseURL + "/models"); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "checks": gin.H{
				"reachable": gin.H{"ok": false, "error": "invalid base_url"}}})
			return
		}
		client := &http.Client{Timeout: 10 * time.Second}

		reach := dlpCheckReachable(c, client, baseURL, key)
		checks := gin.H{"reachable": reach}
		ok := reach["ok"] == true
		if ok {
			emb := dlpCheckEmbedding(c, client, baseURL, key, embModel, expectedDim)
			chat := dlpCheckChat(c, client, baseURL, key, riskModel, maxTokens)
			checks["embedding"] = emb
			checks["chat"] = chat
			ok = emb["ok"] == true && chat["ok"] == true
		}
		c.JSON(http.StatusOK, gin.H{"ok": ok, "checks": checks})
	}
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func dlpCheckReachable(c *gin.Context, client *http.Client, baseURL, key string) gin.H {
	req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, baseURL+"/models", nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return gin.H{"ok": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return gin.H{"ok": false, "error": "HTTP " + resp.Status}
	}
	return gin.H{"ok": true}
}

func dlpCheckEmbedding(c *gin.Context, client *http.Client, baseURL, key, model string, expected int) gin.H {
	payload, _ := json.Marshal(gin.H{"model": model, "input": []string{"ping"}})
	req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, baseURL+"/embeddings", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return gin.H{"ok": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil || len(parsed.Data) == 0 {
		return gin.H{"ok": false, "error": "no embedding returned"}
	}
	dim := len(parsed.Data[0].Embedding)
	return gin.H{"ok": dim == expected, "dim": dim, "expected": expected}
}

func dlpCheckChat(c *gin.Context, client *http.Client, baseURL, key, model string, maxTokens int) gin.H {
	payload, _ := json.Marshal(gin.H{
		"model":       model,
		"messages":    []gin.H{{"role": "user", "content": "Reply with the single word OK."}},
		"temperature": 0,
		"max_tokens":  maxTokens,
	})
	req, _ := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return gin.H{"ok": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil || len(parsed.Choices) == 0 {
		return gin.H{"ok": false, "error": "no chat response"}
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if content != "" {
		return gin.H{"ok": true, "thinking_detected": false, "note": ""}
	}
	if strings.TrimSpace(parsed.Choices[0].Message.Reasoning) != "" {
		return gin.H{"ok": false, "thinking_detected": true,
			"note": "model returned empty content with reasoning — raise max_output_tokens or use a direct-response model"}
	}
	return gin.H{"ok": false, "thinking_detected": false, "note": "empty content"}
}
```

- [ ] **Step 4: Fix imports**

Ensure `backend/internal/handlers/settings.go` imports include `bytes`, `encoding/json`, `io`, `net/http`, `net/url`, `strings`, `time`. Add any missing.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/... -run 'TestDLPPreflight' -v`
Expected: PASS (all three).

- [ ] **Step 6: Compile-check**

Run: `cd backend && CGO_ENABLED=0 go build ./...`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/handlers/settings.go backend/internal/handlers/settings_test.go
git commit -m "feat(dlp): structured test-dlp preflight (embedding dim + thinking detection) (#184)"
```

---

### Task 4: Frontend — test-dlp payload + structured result rendering

Send the extra fields and render per-check results.

**Files:**
- Modify: `frontend/src/api/client.js` (line ~203)
- Modify: `frontend/src/pages/Settings.jsx` (`testDLP` ~153-158, result render ~247-249)

**Interfaces:**
- Consumes: `POST /api/config/test-dlp` response `{ok, checks:{reachable, embedding:{ok,dim,expected}, chat:{ok,thinking_detected,note}}}` (Task 3).

- [ ] **Step 1: Update the API client**

In `frontend/src/api/client.js`, replace the `testDLPConfig` line:

```js
  testDLPConfig: (payload) => post('/config/test-dlp', payload),
```

- [ ] **Step 2: Send the full payload from Settings**

In `frontend/src/pages/Settings.jsx`, update `testDLP` to gather the current drafts/values and pass them. Replace the `const r = await api.testDLPConfig(base, key)` call (and its argument assembly) with:

```js
    const payload = {
      base_url: base,
      api_key: key,
      embedding_model: drafts['dlp.embedding_model'] ?? findValue('dlp.embedding_model'),
      embedding_dim: Number(drafts['dlp.embedding_dim'] ?? findValue('dlp.embedding_dim')) || 0,
      risk_model: drafts['dlp.risk_model'] ?? findValue('dlp.risk_model'),
    }
    const r = await api.testDLPConfig(payload)
```

(Use the same `drafts`/`findValue` helpers already used for `base`/`key` at lines ~154-155.)

- [ ] **Step 3: Render the structured result**

Replace the result `<span>` block (lines ~247-249) with a block that prefers `checks` when present:

```jsx
                  {dlpTest && !dlpTest.pending && (
                    dlpTest.checks ? (
                      <div className="text-xs space-y-0.5">
                        <div className={dlpTest.checks.reachable?.ok ? 'text-emerald-400' : 'text-error'}>
                          {dlpTest.checks.reachable?.ok ? '✓ reachable' : `✗ reachable: ${dlpTest.checks.reachable?.error || ''}`}
                        </div>
                        {dlpTest.checks.embedding && (
                          <div className={dlpTest.checks.embedding.ok ? 'text-emerald-400' : 'text-error'}>
                            {dlpTest.checks.embedding.ok
                              ? `✓ embedding dim ${dlpTest.checks.embedding.dim}`
                              : `✗ embedding dim ${dlpTest.checks.embedding.dim ?? '?'} (expected ${dlpTest.checks.embedding.expected})`}
                          </div>
                        )}
                        {dlpTest.checks.chat && (
                          <div className={dlpTest.checks.chat.ok ? 'text-emerald-400' : 'text-error'}>
                            {dlpTest.checks.chat.ok ? '✓ chat' : `✗ chat: ${dlpTest.checks.chat.note || 'failed'}`}
                          </div>
                        )}
                      </div>
                    ) : (
                      <span className={`text-xs ${dlpTest.ok ? 'text-emerald-400' : 'text-error'}`}>
                        {dlpTest.ok ? 'Endpoint reachable' : `Failed: ${dlpTest.error}`}
                      </span>
                    )
                  )}
```

- [ ] **Step 4: Lint + build the frontend**

Run: `cd frontend && npm run lint && npm run build`
Expected: lint passes; build writes `dist/`.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/api/client.js frontend/src/pages/Settings.jsx
git commit -m "feat(dlp): render structured test-dlp preflight results in Settings (#184)"
```

---

### Task 5: Documentation — local models (Ollama)

Document the local-model setup and the knobs.

**Files:**
- Modify: `docs/dlp.md`

- [ ] **Step 1: Add the section**

Append a `## Local models (Ollama)` section to `docs/dlp.md` covering:

````markdown
## Local models (Ollama)

Semantic DLP works with any OpenAI-compatible endpoint, including a local
[Ollama](https://ollama.com) server. Because the model UDFs run **inside
Timeplus**, `dlp.base_url` must be reachable from the Timeplus process.

```yaml
dlp:
  enabled: true
  base_url: http://host.docker.internal:11434/v1   # Timeplus in Docker, Ollama on host
  # base_url: http://localhost:11434/v1            # Timeplus running natively
  api_key: ""                                       # Ollama ignores auth
  embedding_model: mxbai-embed-large                # 1024 dims
  embedding_dim: 1024
  risk_model: qwen2.5-coder:7b
  summary_model: qwen2.5-coder:7b
  request_timeout_seconds: 50                        # clamped to [5,55]
  max_output_tokens: 512                             # raise for verbose/reasoning models
```

**Use direct-response models, not reasoning models.** Reasoning models
(`gemma4`, `qwen3`, …) emit chain-of-thought into a separate `reasoning`
field and only fill `content` afterward, so under a small token budget the
UDF gets empty `content`. AgentGuard mitigates this by falling back to the
`reasoning` field and by the configurable `max_output_tokens`, but a
direct-response model (`qwen2.5-coder:7b`, `llama3.2:1b`) is recommended.
The Settings → Semantic DLP **Test endpoint** button now verifies the
embedding dimension and detects empty-content reasoning models.

**Embedding dimension must match the model** and is baked into the
`agentguard_dlp_enriched` stream. Changing `embedding_dim` requires
dropping and recreating the stream + MV before re-provisioning:

```sql
DROP VIEW IF EXISTS mv_dlp_enrich;
DROP STREAM IF EXISTS agentguard_dlp_enriched;
```
then `POST /api/rules/sync` (or re-run the setup wizard).

**Timeouts.** `request_timeout_seconds` is the per-call model timeout in the
UDFs. It is clamped to ≤ 55s so the query funnel returns before the 60s
internal query-client ceiling. Local 7B summaries over ~20 evidence
messages take ~15–20s warm.

**Cold start.** Ollama unloads idle models after ~5 min; the first query
after a gap pays a model-load penalty. Set `OLLAMA_KEEP_ALIVE=-1` (or a long
duration) to keep models resident.

**Vector index.** Timeplus builds where the `vector_similarity` (HNSW) index
type is unavailable fall back to brute-force `cosine_distance`. This is
correct, just slower at large row counts.
````

- [ ] **Step 2: Commit**

```bash
git add docs/dlp.md
git commit -m "docs(dlp): local-model (Ollama) setup + timeout/token knobs (#184)"
```

---

## Final verification

- [ ] **Backend tests + build**

Run: `cd backend && CGO_ENABLED=0 go test ./... && CGO_ENABLED=0 go build ./...`
Expected: all tests pass; build succeeds.

- [ ] **Frontend lint + build**

Run: `cd frontend && npm run lint && npm run build`
Expected: clean.

- [ ] **End-to-end smoke (optional, needs running stack + Ollama)**

Update config to a local model, drop+re-provision (`POST /api/rules/sync`),
reseed (`make dlp-seed`), then `POST /api/dlp/query` and confirm a non-empty
summary. Hit Settings → Test endpoint and confirm the three checks render.
