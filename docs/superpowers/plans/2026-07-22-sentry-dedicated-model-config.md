# Sentry Dedicated Model Config — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Sentry its own chat + embedding model configuration in a dedicated `agentguard_sentry_config` stream (decoupled from DLP), with a Settings-page editor reachable independent of DLP and a test-connection endpoint.

**Architecture:** New mutable stream `agentguard_sentry_config` with `shared`/`chat`/`embed` rows, resolved by the existing pure `dlp_config_resolve` UDF; a config store mirroring `dlp_config_store.go`; `sentry.*` config.yaml boot defaults (seed-if-absent); `LoadChatCfg` re-pointed at the new stream; new `/api/sentry/config*` handlers; a `SentryConfigEditor` in the "Sentry Agent" Settings section. Sentry stops reading `agentguard_dlp_config` entirely; `dlp_search` keeps DLP's embedding.

**Tech Stack:** Go + Gin + proton-go-driver (existing), React 18 + Vite (existing), Python stdlib mock model (existing). Spec: `docs/superpowers/specs/2026-07-22-sentry-dedicated-model-config-design.md`. Branch: `feature/207-sentry-agent-design`. Issues #207/#209.

## Global Constraints

- All Go builds/tests from `backend/` with `CGO_ENABLED=0` (macOS dyld abort otherwise): `CGO_ENABLED=0 go build ./...`, `CGO_ENABLED=0 go test ./...`.
- "Timeplus Enterprise", never "Proton", in user-facing text.
- All new/changed DDL uses `IF NOT EXISTS`; config seeding is seed-if-absent (never clobber operator edits); reads escape WHERE args, writes use native `PrepareBatch` (JSON-safe).
- The pure UDF `dlp_config_resolve(shared, config)` is reused unchanged — do NOT create a new resolver.
- Frontend: design tokens from `tailwind.config.js`; no border-line sectioning; no shadows; radius ≤4px; gradient CTA `bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023]`.
- Feature invisible/inert when `sentry.enabled: false`; config UI reachable whenever `sentry.enabled: true` regardless of `dlp.enabled`.
- gofmt-clean; commit after each task on the current branch.

---

### Task 1: `sentry.*` config boot defaults

**Files:**
- Modify: `backend/internal/config/config.go` (SentryConfig struct ~line 84; SetDefault block ~155; explicit read block ~220; BindEnv ~271; Save ~306)
- Modify: `backend/internal/config/registry.go` (sentry entries ~127; update the `sentry.enabled` description that mentions `sentry_chat`)
- Modify: `backend/internal/config/sources.go` (EffectiveValue + ApplyChange switches — add cases for every new key)
- Modify: `backend/internal/config/config_test.go` (extend TestSave_RoundTrip with the new fields) and `registry_test.go` (add new keys to the spot-check `want` list if present)

**Interfaces:**
- Produces: `config.SentryConfig` gains `BaseURL, APIKey, ChatModel, EmbeddingModel string; EmbeddingDim, RequestTimeoutSeconds, MaxOutputTokens int` (mapstructure `base_url`/`api_key`/`chat_model`/`embedding_model`/`embedding_dim`/`request_timeout_seconds`/`max_output_tokens`). Viper keys `sentry.base_url` etc. all hot-apply (no restart).

- [ ] **Step 1: Extend the struct.** In `config.go` replace `SentryConfig`:

```go
// SentryConfig configures the Sentry conversational agent and its dedicated
// chat + embedding models (spec 2026-07-22-sentry-dedicated-model-config).
type SentryConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	SkillDir string `mapstructure:"skill_dir"` // optional on-disk override for the vendored timeplus-sql-guide skill
	// Boot defaults seeded seed-if-absent into agentguard_sentry_config; the
	// stream is authoritative after first provision.
	BaseURL               string `mapstructure:"base_url"`
	APIKey                string `mapstructure:"api_key"`
	ChatModel             string `mapstructure:"chat_model"`
	EmbeddingModel        string `mapstructure:"embedding_model"`
	EmbeddingDim          int    `mapstructure:"embedding_dim"`
	RequestTimeoutSeconds int    `mapstructure:"request_timeout_seconds"`
	MaxOutputTokens       int    `mapstructure:"max_output_tokens"`
}
```

- [ ] **Step 2: Defaults + reads + env + save.** In `config.go`:
  - After `v.SetDefault("sentry.skill_dir", "")`:
    ```go
    v.SetDefault("sentry.base_url", "")
    v.SetDefault("sentry.api_key", "")
    v.SetDefault("sentry.chat_model", "")
    v.SetDefault("sentry.embedding_model", "")
    v.SetDefault("sentry.embedding_dim", 1536)
    v.SetDefault("sentry.request_timeout_seconds", 60)
    v.SetDefault("sentry.max_output_tokens", 1024)
    ```
  - After `cfg.Sentry.SkillDir = v.GetString("sentry.skill_dir")`:
    ```go
    cfg.Sentry.BaseURL = v.GetString("sentry.base_url")
    cfg.Sentry.APIKey = v.GetString("sentry.api_key")
    cfg.Sentry.ChatModel = v.GetString("sentry.chat_model")
    cfg.Sentry.EmbeddingModel = v.GetString("sentry.embedding_model")
    cfg.Sentry.EmbeddingDim = v.GetInt("sentry.embedding_dim")
    cfg.Sentry.RequestTimeoutSeconds = v.GetInt("sentry.request_timeout_seconds")
    cfg.Sentry.MaxOutputTokens = v.GetInt("sentry.max_output_tokens")
    ```
  - After the two existing `v.BindEnv("sentry.*")` lines:
    ```go
    _ = v.BindEnv("sentry.base_url", "SENTRY_BASE_URL")
    _ = v.BindEnv("sentry.api_key", "SENTRY_API_KEY")
    _ = v.BindEnv("sentry.chat_model", "SENTRY_CHAT_MODEL")
    _ = v.BindEnv("sentry.embedding_model", "SENTRY_EMBEDDING_MODEL")
    _ = v.BindEnv("sentry.embedding_dim", "SENTRY_EMBEDDING_DIM")
    _ = v.BindEnv("sentry.request_timeout_seconds", "SENTRY_REQUEST_TIMEOUT_SECONDS")
    _ = v.BindEnv("sentry.max_output_tokens", "SENTRY_MAX_OUTPUT_TOKENS")
    ```
  - After the two existing `v.Set("sentry.*")` lines in `Save()`:
    ```go
    v.Set("sentry.base_url", cfg.Sentry.BaseURL)
    v.Set("sentry.api_key", cfg.Sentry.APIKey)
    v.Set("sentry.chat_model", cfg.Sentry.ChatModel)
    v.Set("sentry.embedding_model", cfg.Sentry.EmbeddingModel)
    v.Set("sentry.embedding_dim", cfg.Sentry.EmbeddingDim)
    v.Set("sentry.request_timeout_seconds", cfg.Sentry.RequestTimeoutSeconds)
    v.Set("sentry.max_output_tokens", cfg.Sentry.MaxOutputTokens)
    ```

- [ ] **Step 3: Registry entries.** In `registry.go`, change the `sentry.enabled` Description (drop the `sentry_chat` mention) to `"Conversational security analyst agent (chat, SQL skills). Model settings live in agentguard_sentry_config, editable under Settings → Sentry Agent."` and add after the `sentry.skill_dir` entry (match the neighboring entry literal's exact fields — check whether they use `EnvVar`/`Secret`/`Type` and mirror; the `dlp.api_key` entry shows the `Secret: true` pattern for api keys):

```go
		{Key: "sentry.base_url", Section: "sentry", Label: "Sentry LLM base URL",
			Description: "OpenAI-compatible endpoint for Sentry's chat + embedding models (boot default; edit live under Settings → Sentry Agent).", Type: "string", EnvVar: "SENTRY_BASE_URL"},
		{Key: "sentry.api_key", Section: "sentry", Label: "Sentry API key",
			Description: "API key for the Sentry LLM endpoint.", Type: "string", Secret: true, EnvVar: "SENTRY_API_KEY"},
		{Key: "sentry.chat_model", Section: "sentry", Label: "Sentry chat model",
			Description: "Tool-calling-capable chat model for the Sentry agent loop.", Type: "string", EnvVar: "SENTRY_CHAT_MODEL"},
		{Key: "sentry.embedding_model", Section: "sentry", Label: "Sentry embedding model",
			Description: "Embedding model for Sentry memory (Phase 2).", Type: "string", EnvVar: "SENTRY_EMBEDDING_MODEL"},
		{Key: "sentry.embedding_dim", Section: "sentry", Label: "Sentry embedding dimension",
			Description: "Vector dimension of the Sentry embedding model (Phase 2 memory).", Type: "int", EnvVar: "SENTRY_EMBEDDING_DIM"},
		{Key: "sentry.request_timeout_seconds", Section: "sentry", Label: "Sentry model request timeout (s)",
			Description: "Per-call timeout for Sentry model requests.", Type: "int", EnvVar: "SENTRY_REQUEST_TIMEOUT_SECONDS"},
		{Key: "sentry.max_output_tokens", Section: "sentry", Label: "Sentry max output tokens",
			Description: "Per-completion token cap for the Sentry chat model.", Type: "int", EnvVar: "SENTRY_MAX_OUTPUT_TOKENS"},
```

(If the `KeyMeta` struct has no `Secret`/`EnvVar` field, drop that key from these literals — inspect the struct at the top of registry.go first and match exactly, as Phase 1 did.)

- [ ] **Step 4: Settings-API wiring** in `sources.go`. Add a case for each new key to BOTH `EffectiveValue()` and `ApplyChange()`, following the existing `sentry.enabled`/`sentry.skill_dir` cases (use the `setBool`/`setString`/`setInt` helpers the file already uses for dlp keys; string keys → `cfg.Sentry.BaseURL` etc., int keys → `cfg.Sentry.EmbeddingDim`/`RequestTimeoutSeconds`/`MaxOutputTokens`). All hot-apply (no `Restart`).

- [ ] **Step 5: Tests.** Extend `config_test.go`'s `TestSave_RoundTrip` to set the new `cfg.Sentry.*` fields (e.g. `BaseURL:"http://x/v1", ChatModel:"gpt-4o", EmbeddingDim:1536`) and assert they survive save→load. Add the new keys to `registry_test.go`'s `want` spot-check list if it enumerates keys.

- [ ] **Step 6: Verify + commit**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/config/...`
Expected: PASS. Then `git commit -m "feat(sentry): dedicated model config boot defaults (base_url, chat/embedding model, ...)"`

---

### Task 2: `agentguard_sentry_config` stream, store, and provisioning

**Files:**
- Create: `backend/internal/timeplus/sentry_config_store.go`
- Create: `backend/internal/timeplus/sentry_config_store_test.go`
- Modify: `backend/internal/timeplus/sentry.go` (new stream DDL + key consts; rewrite seeding; drop `sentry_chat`; keep `DefaultSentrySystemPrompt`)
- Modify: `backend/internal/timeplus/dlp_config_store.go` (remove `SentryCfgKeyChat` from `DLPConfigKeys()` line 24 and the `case SentryCfgKeyChat` in `ValidateDLPConfigValue` line 121)
- Modify: `backend/internal/timeplus/sentry_test.go` (replace the `SentryChatDefaults`/`IsDLPConfigKey(SentryCfgKeyChat)` tests)

**Interfaces:**
- Consumes: `config.SentryConfig` (Task 1); existing `Query`, `connectOnce`/`Connect`, `ddlDLPConfigResolveUDF`, `seedDLPConfig` batch idiom.
- Produces (package `timeplus`):
  - consts `SentryCfgKeyShared = "shared"`, `SentryCfgKeyChat = "chat"`, `SentryCfgKeyEmbed = "embed"` (NOTE: `SentryCfgKeyChat`'s VALUE changes from `"sentry_chat"` to `"chat"`, and it now keys `agentguard_sentry_config`, not the DLP stream).
  - `type SentryConfigRow struct { Key, Value, UpdatedAt, UpdatedBy string }` (json tags `key`/`value`/`updated_at`/`updated_by`).
  - `SentryConfigKeys() []string`, `IsSentryConfigKey(key string) bool`.
  - `ReadSentryConfig(cfg *config.TimeplusConfig) ([]SentryConfigRow, error)`, `ReadSentryConfigValue(cfg *config.TimeplusConfig, key string) (string, error)`.
  - `WriteSentryConfig(cfg *config.TimeplusConfig, key, value, updatedBy string) error`.
  - `ValidateSentryConfigValue(key, value string) error`.
  - `SentryConfigRows(sCfg config.SentryConfig) map[string]string` (renders the 3 seed rows).
  - `DefaultSentryChatRow(sCfg config.SentryConfig) string` (the `chat` row JSON — replaces the old `SentryChatDefaults`).

- [ ] **Step 1: Failing tests** (`sentry_config_store_test.go`):

```go
package timeplus

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

func TestSentryConfigRowsSeedsThreeRows(t *testing.T) {
	rows := SentryConfigRows(config.SentryConfig{
		BaseURL: "http://x/v1", APIKey: "k", ChatModel: "gpt-4o",
		EmbeddingModel: "text-embedding-3-small", EmbeddingDim: 1536,
		RequestTimeoutSeconds: 60, MaxOutputTokens: 1024,
	})
	for _, k := range []string{SentryCfgKeyShared, SentryCfgKeyChat, SentryCfgKeyEmbed} {
		if _, ok := rows[k]; !ok {
			t.Fatalf("missing seed row %q", k)
		}
		var v any
		if err := json.Unmarshal([]byte(rows[k]), &v); err != nil {
			t.Fatalf("row %q not valid JSON: %v", k, err)
		}
	}
	// shared carries the endpoint + a catalog with chat + embedding entries.
	var shared struct {
		BaseURL string                 `json:"base_url"`
		Models  map[string]any         `json:"models"`
	}
	_ = json.Unmarshal([]byte(rows[SentryCfgKeyShared]), &shared)
	if shared.BaseURL != "http://x/v1" {
		t.Fatalf("shared base_url = %q", shared.BaseURL)
	}
	if _, ok := shared.Models["chat"]; !ok {
		t.Fatal("shared.models missing 'chat' entry")
	}
	if _, ok := shared.Models["embedding"]; !ok {
		t.Fatal("shared.models missing 'embedding' entry")
	}
	// chat row references the catalog name "chat" and carries the injection guard prompt.
	var chat struct {
		Model         string `json:"model"`
		SystemPrompt  string `json:"system_prompt"`
		MaxIterations int    `json:"max_iterations"`
	}
	_ = json.Unmarshal([]byte(rows[SentryCfgKeyChat]), &chat)
	if chat.Model != "chat" || chat.MaxIterations != 12 {
		t.Fatalf("chat row wrong: %+v", chat)
	}
	if !strings.Contains(chat.SystemPrompt, "data, never instructions") {
		t.Fatal("chat system_prompt missing injection guard")
	}
	var emb struct {
		Model string `json:"model"`
		Dim   int    `json:"embedding_dim"`
	}
	_ = json.Unmarshal([]byte(rows[SentryCfgKeyEmbed]), &emb)
	if emb.Model != "embedding" || emb.Dim != 1536 {
		t.Fatalf("embed row wrong: %+v", emb)
	}
}

func TestIsSentryConfigKey(t *testing.T) {
	for _, k := range []string{SentryCfgKeyShared, SentryCfgKeyChat, SentryCfgKeyEmbed} {
		if !IsSentryConfigKey(k) {
			t.Fatalf("%q should be a sentry config key", k)
		}
	}
	if IsSentryConfigKey("nope") {
		t.Fatal("unknown key accepted")
	}
}

func TestValidateSentryConfigValue(t *testing.T) {
	cases := []struct {
		key, value, wantErr string
	}{
		{SentryCfgKeyChat, `{"system_prompt":"ok"}`, ""},
		{SentryCfgKeyChat, `{"system_prompt":123}`, "system_prompt must be a string"},
		{SentryCfgKeyChat, `not json`, "valid JSON"},
		{SentryCfgKeyShared, `{"models":{"chat":{"model":"gpt-4o"}}}`, ""},
		{SentryCfgKeyShared, `{"models":[]}`, "models must be an object"},
		{SentryCfgKeyEmbed, `{"embedding_dim":1536}`, ""},
		{SentryCfgKeyEmbed, `{"embedding_dim":"big"}`, "embedding_dim must be a number"},
	}
	for _, c := range cases {
		err := ValidateSentryConfigValue(c.key, c.value)
		if c.wantErr == "" && err != nil {
			t.Fatalf("%s %q: unexpected %v", c.key, c.value, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Fatalf("%s %q: want err %q got %v", c.key, c.value, c.wantErr, err)
		}
	}
}

func TestSentryConfigStreamDDL(t *testing.T) {
	if !strings.Contains(ddlSentryConfig, "MUTABLE STREAM IF NOT EXISTS agentguard_sentry_config") ||
		!strings.Contains(ddlSentryConfig, "PRIMARY KEY (key)") {
		t.Fatal("sentry config DDL wrong")
	}
}
```

- [ ] **Step 2: Run → FAIL** (`CGO_ENABLED=0 go test ./internal/timeplus/ -run 'Sentry' -v`) — undefined symbols.

- [ ] **Step 3: Implement `sentry_config_store.go`:**

```go
package timeplus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/timeplus/agentguard/internal/config"
)

// agentguard_sentry_config holds Sentry's dedicated model configuration as
// key→JSON rows, independent of the DLP config stream. Rows: `shared` (endpoint
// + model catalog), `chat` (LLM model/prompt/budgets), `embed` (embedding model
// for Phase 2 memory). Resolved with the pure dlp_config_resolve UDF, same as
// DLP. Mutable: an operator upserts a row to change models at runtime.
const ddlSentryConfig = `
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_sentry_config
(
  key        string,
  value      string,
  updated_at datetime64(3) DEFAULT now64(3),
  updated_by string
)
PRIMARY KEY (key)`

const (
	SentryCfgKeyShared = "shared"
	SentryCfgKeyChat   = "chat"
	SentryCfgKeyEmbed  = "embed"
)

type SentryConfigRow struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

func SentryConfigKeys() []string {
	return []string{SentryCfgKeyShared, SentryCfgKeyChat, SentryCfgKeyEmbed}
}

func IsSentryConfigKey(key string) bool {
	for _, k := range SentryConfigKeys() {
		if k == key {
			return true
		}
	}
	return false
}

func ReadSentryConfig(cfg *config.TimeplusConfig) ([]SentryConfigRow, error) {
	rows, err := Query(cfg, `SELECT key, value, to_string(updated_at) AS updated_at, updated_by
FROM table(agentguard_sentry_config) ORDER BY key`)
	if err != nil {
		return nil, err
	}
	out := make([]SentryConfigRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, SentryConfigRow{
			Key: asDLPString(r["key"]), Value: asDLPString(r["value"]),
			UpdatedAt: asDLPString(r["updated_at"]), UpdatedBy: asDLPString(r["updated_by"]),
		})
	}
	return out, nil
}

func ReadSentryConfigValue(cfg *config.TimeplusConfig, key string) (string, error) {
	rows, err := ReadSentryConfig(cfg)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if r.Key == key {
			return r.Value, nil
		}
	}
	return "", nil
}

// WriteSentryConfig upserts one row (mutable stream keyed on `key`). Native batch
// avoids SQL-literal escaping of arbitrary JSON (mirrors WriteDLPConfig).
func WriteSentryConfig(cfg *config.TimeplusConfig, key, value, updatedBy string) error {
	conn, err := Connect(cfg)
	if err != nil {
		return fmt.Errorf("sentry config write: connect: %w", err)
	}
	defer conn.Close()
	batch, err := conn.PrepareBatch(context.Background(), "INSERT INTO agentguard_sentry_config (key, value, updated_by)")
	if err != nil {
		return fmt.Errorf("sentry config write: prepare: %w", err)
	}
	defer func() { _ = batch.Abort() }()
	if err := batch.Append(key, value, updatedBy); err != nil {
		return fmt.Errorf("sentry config write: append: %w", err)
	}
	return batch.Send()
}

func ValidateSentryConfigValue(key, value string) error {
	var generic any
	if err := json.Unmarshal([]byte(value), &generic); err != nil {
		return fmt.Errorf("value is not valid JSON: %w", err)
	}
	switch key {
	case SentryCfgKeyChat:
		var row struct {
			SystemPrompt any `json:"system_prompt"`
		}
		_ = json.Unmarshal([]byte(value), &row)
		if row.SystemPrompt != nil {
			if _, ok := row.SystemPrompt.(string); !ok {
				return fmt.Errorf("%s: system_prompt must be a string", key)
			}
		}
	case SentryCfgKeyShared:
		var row struct {
			Models any `json:"models"`
		}
		_ = json.Unmarshal([]byte(value), &row)
		if row.Models != nil {
			if _, ok := row.Models.(map[string]any); !ok {
				return fmt.Errorf("%s: models must be an object of name→{model,...}", key)
			}
		}
	case SentryCfgKeyEmbed:
		var row struct {
			Dim any `json:"embedding_dim"`
		}
		_ = json.Unmarshal([]byte(value), &row)
		if row.Dim != nil {
			if _, ok := row.Dim.(float64); !ok {
				return fmt.Errorf("%s: embedding_dim must be a number", key)
			}
		}
	}
	return nil
}

// SentryConfigRows renders the three seed rows from boot config. A model NAME
// ("chat"/"embedding") in the chat/embed rows resolves against shared.models.
func SentryConfigRows(sCfg config.SentryConfig) map[string]string {
	timeout := sCfg.RequestTimeoutSeconds
	if timeout <= 0 {
		timeout = 60
	}
	models := map[string]any{}
	if sCfg.ChatModel != "" {
		models["chat"] = map[string]any{"model": sCfg.ChatModel}
	} else {
		models["chat"] = map[string]any{"model": ""}
	}
	if sCfg.EmbeddingModel != "" {
		models["embedding"] = map[string]any{"model": sCfg.EmbeddingModel}
	} else {
		models["embedding"] = map[string]any{"model": ""}
	}
	shared, _ := json.Marshal(map[string]any{
		"base_url": sCfg.BaseURL, "api_key": sCfg.APIKey,
		"timeout": timeout, "models": models,
	})
	maxTokens := sCfg.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	dim := sCfg.EmbeddingDim
	if dim <= 0 {
		dim = 1536
	}
	embed, _ := json.Marshal(map[string]any{"model": "embedding", "embedding_dim": dim})
	return map[string]string{
		SentryCfgKeyShared: string(shared),
		SentryCfgKeyChat:   DefaultSentryChatRow(sCfg),
		SentryCfgKeyEmbed:  string(embed),
	}
}

// DefaultSentryChatRow renders the `chat` row (model name + prompt + budgets).
func DefaultSentryChatRow(sCfg config.SentryConfig) string {
	maxTokens := sCfg.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	b, _ := json.Marshal(map[string]any{
		"model":                 "chat",
		"system_prompt":         DefaultSentrySystemPrompt,
		"max_tokens":            maxTokens,
		"max_iterations":        12,
		"turn_token_budget":     32000,
		"sql_timeout_ms":        30000,
		"sql_max_rows":          1000,
		"tool_result_max_bytes": 16384,
	})
	return string(b)
}
```

- [ ] **Step 4: Rewrite `sentry.go` provisioning.** Delete the `SentryCfgKeyChat` const (line 15) and the whole `SentryChatDefaults` func (lines 61-77) — they move to the store with new meaning. Keep `DefaultSentrySystemPrompt`, `ddlSentryConversations`, `ddlSentryMessages`. Replace `InitSentry`'s DDL map + seed calls:

```go
	for name, ddl := range map[string]string{
		"dlp_config_resolve UDF":            ddlDLPConfigResolveUDF,
		"agentguard_sentry_config":          ddlSentryConfig,
		"agentguard_sentry_conversations":   ddlSentryConversations,
		"agentguard_sentry_messages":        ddlSentryMessages,
	} {
		if err := conn.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("sentry: creating %s: %w", name, err)
		}
	}
	if err := seedSentryConfig(ctx, conn, sCfg, emit); err != nil {
		return err
	}
```

(Remove the `seedDLPConfig` call — Sentry no longer touches DLP config. `dlpCfg` param stays in the signature but is now unused; keep it for caller compatibility or drop it and update callers — simplest: keep the signature, mark `_ = dlpCfg` if the linter complains, OR drop the param and update `SyncSentry` + the `Init()` caller in `setup.go`/wherever. Prefer dropping it: change `InitSentry(ctx, conn, sCfg, emit)` and `SyncSentry(timeplusCfg, sCfg)`, then update callers — grep `InitSentry(`/`SyncSentry(`.)

Rewrite `seedSentryConfig` to seed the three rows into the new stream, seed-if-absent per key:

```go
func seedSentryConfig(ctx context.Context, conn proton.Conn, sCfg config.SentryConfig, emit func(status, msg string)) error {
	rows := SentryConfigRows(sCfg)
	existing := map[string]bool{}
	rs, err := conn.Query(ctx, "SELECT key FROM table(agentguard_sentry_config)")
	if err != nil {
		return fmt.Errorf("sentry: reading config keys: %w", err)
	}
	for rs.Next() {
		var k string
		if err := rs.Scan(&k); err != nil {
			_ = rs.Close()
			return err
		}
		existing[k] = true
	}
	_ = rs.Close()
	var missing []string
	for k := range rows {
		if !existing[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		emit("ok", "  - Sentry config already seeded; preserving runtime edits")
		return nil
	}
	batch, err := conn.PrepareBatch(ctx, "INSERT INTO agentguard_sentry_config (key, value, updated_by)")
	if err != nil {
		return fmt.Errorf("sentry: prepare config seed: %w", err)
	}
	defer func() { _ = batch.Abort() }()
	for _, k := range missing {
		if err := batch.Append(k, rows[k], "system"); err != nil {
			return err
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("sentry: send config seed: %w", err)
	}
	emit("ok", fmt.Sprintf("  ✓ seeded %d Sentry config rows", len(missing)))
	return nil
}
```

- [ ] **Step 5: Un-wire `sentry_chat` from DLP config.** In `dlp_config_store.go`: remove `SentryCfgKeyChat,` from the `DLPConfigKeys()` slice (line 24) and delete the `case SentryCfgKeyChat:` block from `ValidateDLPConfigValue` (lines 121-132). (The const `SentryCfgKeyChat` now lives in the store with value `"chat"`; these references must go or they won't compile against the DLP stream semantics.)

- [ ] **Step 6: Fix `sentry_test.go`.** Replace `TestSentryChatDefaultsIsValidJSON` (calls the deleted `SentryChatDefaults`) and the `IsDLPConfigKey(SentryCfgKeyChat)` assertion. Point them at the new store: e.g. assert `DefaultSentryChatRow(config.SentryConfig{MaxOutputTokens:1024})` is valid JSON with `model=="chat"`, and that `IsSentryConfigKey(SentryCfgKeyChat)` is true. (The `SentryConfigRows`/`IsSentryConfigKey`/validate coverage already lives in `sentry_config_store_test.go`; keep `sentry_test.go` focused on the conversation-stream DDL + prompt constant.)

- [ ] **Step 7: Update callers.** `grep -rn "InitSentry(\|SyncSentry(\|SentryChatDefaults\|seedSentryConfig(" backend/` and update every call to the new signatures (the `Init()` provisioner caller, `rules.go` sync path). Update the `SyncMVs`/rules-sync call site to `SyncSentry(reqCfg.Timeplus, reqCfg.Sentry)`.

- [ ] **Step 8: Verify + commit**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/timeplus/...`
Expected: PASS (new store tests + updated sentry_test; DLP config tests still pass with the key removed — if a DLP test asserted `sentry_chat` in the key list, update it).
Commit: `feat(sentry): dedicated agentguard_sentry_config stream + store, decoupled from DLP`

---

### Task 3: Re-point `LoadChatCfg` at the new stream

**Files:**
- Modify: `backend/internal/sentry/llm.go` (`LoadChatCfg`)
- Modify: `backend/internal/sentry/llm_test.go` (add a test asserting the SQL targets the new stream)

**Interfaces:**
- Consumes: `tp.SentryCfgKeyShared`, `tp.SentryCfgKeyChat` (new values, Task 2).
- Produces: `LoadChatCfg` unchanged signature/return; reads `agentguard_sentry_config`.

- [ ] **Step 1: Failing test** — extract the SQL builder so it's assertable without a DB. Add a test:

```go
func TestLoadChatCfgSQLTargetsSentryStream(t *testing.T) {
	sql := chatCfgResolveSQL()
	if !strings.Contains(sql, "agentguard_sentry_config") {
		t.Fatalf("expected sentry config stream, got: %s", sql)
	}
	if strings.Contains(sql, "agentguard_dlp_config") {
		t.Fatal("must not read DLP config")
	}
	if !strings.Contains(sql, "key='shared'") || !strings.Contains(sql, "key='chat'") {
		t.Fatalf("expected shared+chat keys: %s", sql)
	}
}
```

- [ ] **Step 2: Run → FAIL** (`chatCfgResolveSQL` undefined).

- [ ] **Step 3: Implement.** Refactor `LoadChatCfg` to build the SQL via a small pure helper and point it at the new stream:

```go
func chatCfgResolveSQL() string {
	sub := func(key string) string {
		return fmt.Sprintf("coalesce((SELECT value FROM table(agentguard_sentry_config) WHERE key='%s'), '')", key)
	}
	return fmt.Sprintf("SELECT dlp_config_resolve(%s, %s) AS cfg",
		sub(tp.SentryCfgKeyShared), sub(tp.SentryCfgKeyChat))
}

func LoadChatCfg(tpCfg *config.TimeplusConfig) (ChatCfg, error) {
	rows, err := tp.Query(tpCfg, chatCfgResolveSQL())
	if err != nil {
		return ChatCfg{}, fmt.Errorf("sentry: load chat config: %w", err)
	}
	var cc ChatCfg
	if len(rows) == 1 {
		if s, ok := rows[0]["cfg"].(string); ok {
			_ = json.Unmarshal([]byte(s), &cc)
		}
	}
	cc.applyDefaults()
	if cc.BaseURL == "" {
		return cc, fmt.Errorf("sentry: no base_url in resolved config — configure Settings → Sentry Agent")
	}
	return cc, nil
}
```

- [ ] **Step 4: Verify + commit**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/sentry/...`
Expected: PASS. Commit: `feat(sentry): resolve chat config from agentguard_sentry_config`

---

### Task 4: `/api/sentry/config` handlers + routes

**Files:**
- Create: `backend/internal/handlers/sentry_config.go`
- Create: `backend/internal/handlers/sentry_config_test.go`
- Modify: `backend/internal/handlers/sentry.go` (`SentryStatus` — read the new stream)
- Modify: `backend/cmd/server/main.go` (register 3 routes in the `protected` group next to the other `/sentry/*` routes)

**Interfaces:**
- Consumes: `tp.ReadSentryConfig`, `tp.WriteSentryConfig`, `tp.ValidateSentryConfigValue`, `tp.IsSentryConfigKey`, `tp.SentryConfigKeys`; `auth.RequestCfg`, `sessionUser`; the DLP handler helpers `redactAPIKey` (reuse — it's package-level in `dlp_config.go`).
- Produces: `handlers.GetSentryConfig(cfg)`, `handlers.UpsertSentryConfig(cfg)`, `handlers.TestSentryConfig(cfg)`.

- [ ] **Step 1: Handler tests** (no-DB parts, via `gin.CreateTestContext` + httptest, mirroring `sentry_test.go`):

```go
func TestUpsertSentryConfig_RejectsUnknownKey(t *testing.T) {
	// PUT /api/sentry/config/bogus → 400
}
func TestUpsertSentryConfig_RejectsBadJSON(t *testing.T) {
	// PUT /api/sentry/config/chat {"value": <invalid>} → 400
}
```

(Write these against the handler with `IsSentryConfigKey`/`ValidateSentryConfigValue` guards; the DB write path isn't exercised without Timeplus — assert the 400s before any write, following how existing handler tests gate.)

- [ ] **Step 2: Implement `sentry_config.go`:**

```go
package handlers

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

func GetSentryConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rc := auth.RequestCfg(c, cfg)
		rows, err := tp.ReadSentryConfig(&rc.Timeplus)
		if err != nil {
			log.Printf("[sentry] GetSentryConfig read failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Sentry config unavailable; is Sentry provisioned?"})
			return
		}
		out := make([]dlpConfigRowView, 0, len(rows))
		for _, r := range rows {
			redacted, hadKey := redactAPIKey(r.Value)
			out = append(out, dlpConfigRowView{
				Key: r.Key, Value: redacted, APIKeySet: hadKey,
				UpdatedAt: r.UpdatedAt, UpdatedBy: r.UpdatedBy,
			})
		}
		c.JSON(http.StatusOK, gin.H{"config": out, "keys": tp.SentryConfigKeys()})
	}
}

func UpsertSentryConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.Param("key")
		if !tp.IsSentryConfigKey(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown sentry config key"})
			return
		}
		var body struct {
			Value any `json:"value"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "value is required"})
			return
		}
		value, err := mergeSentryValue(cfg, c, key, body.Value) // preserves stored api_key when blank
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := tp.ValidateSentryConfigValue(key, value); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		rc := auth.RequestCfg(c, cfg)
		if err := tp.WriteSentryConfig(&rc.Timeplus, key, value, sessionUser(c)); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
```

Implement `mergeSentryValue`: marshal `body.Value` to JSON; if `key=="shared"` and the incoming `api_key` is empty/absent, read the stored `shared` row via `tp.ReadSentryConfigValue` and preserve its `api_key` (mirror the DLP editor's blank-preserves-key rule — check how `UpsertDLPConfig` does this and reuse the same helper if one exists; otherwise a small local merge on the `api_key` and any catalog-entry `api_key` fields). Then `TestSentryConfig` — port `TestDLPConfig` (`dlp.go`/wherever `dlpCheckReachable`/`dlpCheckEmbedding` live), resolving the endpoint from `cfg.Sentry` (or the posted body) and adding a **tool-calling probe**: a chat completion with a trivial tool defined, asserting the response contains a `tool_calls`; return `{ok, checks:{reachable, chat:{ok, tool_calling}, embedding}}`. Reuse `dlpCheckReachable`/`dlpCheckEmbedding` for the shared checks; add a `sentryCheckChatToolCalling` helper.

- [ ] **Step 3: `SentryStatus`** — change its provisioning/config probe to read `agentguard_sentry_config` (via `tp.ReadSentryConfig` or a `system.tables` check for the stream) instead of the DLP config; keep the `tables_check_error` field pattern; report `base_url_set` from the resolved shared row and `model` from `LoadChatCfg`.

- [ ] **Step 4: Routes** in `main.go` `protected` group:

```go
			protected.GET("/sentry/config", handlers.GetSentryConfig(cfg))
			protected.PUT("/sentry/config/:key", handlers.UpsertSentryConfig(cfg))
			protected.POST("/sentry/config/test", handlers.TestSentryConfig(cfg))
```

- [ ] **Step 5: Verify + commit**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/handlers/ -run Sentry`
Expected: PASS. Commit: `feat(sentry): config GET/PUT/test-connection handlers + routes`

---

### Task 5: Frontend — `SentryConfigEditor` in the Sentry Settings section

**Files:**
- Create: `frontend/src/components/SentryConfigEditor.jsx`
- Modify: `frontend/src/api/client.js` (add `getSentryConfig`, `putSentryConfig`, `testSentryConfig`)
- Modify: `frontend/src/components/settings/SettingsSection.jsx` (render `SentryConfigEditor` for the `sentry` section, mirroring how the `dlp` section renders `DLPConfigEditor`)
- Modify: `frontend/src/pages/Sentry.jsx` (empty-state hint → link to Settings when `!base_url_set`)

**Interfaces:**
- Consumes: `GET/PUT /api/sentry/config`, `POST /api/sentry/config/test`, `GET /api/sentry/status`.
- Produces (api): `getSentryConfig()`, `putSentryConfig(key, value)`, `testSentryConfig(payload)`.

- [ ] **Step 1: API client.** After the Sentry section in `client.js`:

```js
  getSentryConfig: () => get('/sentry/config'),
  putSentryConfig: (key, value) => put(`/sentry/config/${encodeURIComponent(key)}`, { value }),
  testSentryConfig: (payload) => post('/sentry/config/test', payload),
```

- [ ] **Step 2: Build `SentryConfigEditor.jsx`.** Adapt `DLPConfigEditor.jsx` (read it first — reuse its `modelsToRows`/`rowsToModels` catalog widget and blank-api_key-preserves logic). Three panels driven by `getSentryConfig()`'s `shared`/`chat`/`embed` rows:
  - **Endpoint** (`shared`): base_url, api_key (blank preserves), timeout, add/remove models catalog rows.
  - **Chat** (`chat`): a `<select>`/input for the catalog model name, system_prompt textarea, and number inputs for max_tokens/max_iterations/turn_token_budget/sql_timeout_ms/sql_max_rows/tool_result_max_bytes with the clamp ceilings shown as hints.
  - **Embedding** (`embed`): catalog model name + embedding_dim, labeled "used by Sentry memory (Phase 2)".
  - **Test connection** button → `testSentryConfig({})`, render per-check pass/fail (reachable / chat + tool-calling / embedding).
  Each panel saves its row via `putSentryConfig(key, value)`; dirty-tracking + a Save action per the DLP editor's pattern. Design tokens only.

- [ ] **Step 3: Wire into Settings.** In `SettingsSection.jsx`, where it special-cases the `dlp` section to render `DLPConfigEditor`, add a branch for `section.id === 'sentry'` (or the equivalent key) rendering a "Model configuration" block with `<SentryConfigEditor />`. Ensure it renders regardless of `dlp.enabled`.

- [ ] **Step 4: `/sentry` hint.** In `Sentry.jsx`, when `sentryStatus()` returns `!base_url_set` (or `config_error`), show a small notice in the empty state: "Sentry has no chat model configured — set it in Settings → Sentry Agent" linking to `/settings`.

- [ ] **Step 5: Verify + commit**

Run: `cd frontend && npm run lint && npm run build`
Expected: clean. Commit: `feat(sentry): model-config editor in Settings + /sentry setup hint`

---

### Task 6: Integration script + docs

**Files:**
- Modify: `backend/scripts/sentry_integration.sh` (point the NEW stream's shared row at the mock; add a test-connection assertion)
- Modify: `docs/sentry.md` (rewrite the config section for the dedicated stream)
- Modify: `CLAUDE.md` (streams table + API table + Sentry subsection)

- [ ] **Step 1: Integration.** In `sentry_integration.sh`, replace the step that did `PUT /api/dlp/config/shared` with `PUT /api/sentry/config/shared` pointing `base_url` at `http://localhost:$MOCK/v1` with a `models.chat` entry (`{"model":"mock-chat"}`) and `PUT /api/sentry/config/chat` if needed (default already names `chat`). Add an assertion: `POST /api/sentry/config/test` returns `ok:true` (or at least `reachable.ok` + `chat.tool_calling`) against the mock. Keep the existing chat round-trip + replay assertions. Run `make sentry-integration` against a live Timeplus and get `PASS`. (If no live Timeplus in this environment, make it correct-by-construction mirroring the working prior version and report where it stopped — do NOT fake a pass.)

- [ ] **Step 2: Docs.** In `docs/sentry.md`, replace the "Configuring the chat endpoint (the `shared` row)" section: Sentry config now lives in `agentguard_sentry_config` (`shared`/`chat`/`embed` rows), seeded from `sentry.*` config.yaml boot defaults, edited under **Settings → Sentry Agent** (reachable without DLP). Document the three rows, the `PUT /api/sentry/config/:key` + `POST /api/sentry/config/test` API, that the chat model must be tool-calling-capable, and that the embedding model is for Phase 2 memory. Update the `sentry_chat`/DLP-shared-row references and the troubleshooting `connection refused` entry (now → Settings → Sentry Agent).

- [ ] **Step 3: CLAUDE.md.** Streams table: add `agentguard_sentry_config` row; update the `agentguard_dlp_config` row to drop the `sentry_chat` mention. API table: add `GET /api/sentry/config`, `PUT /api/sentry/config/:key`, `POST /api/sentry/config/test`. Sentry subsection: note the dedicated config stream + Settings editor.

- [ ] **Step 4: Full verification**

```bash
cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./... \
  && cd ../frontend && npm run lint && npm run build
```
Expected: all green. Commit: `feat(sentry): dedicated-config integration test + docs`

---

## Self-Review Notes (resolved inline)

- Spec coverage: §2 stream → Task 2; §3 store → Task 2; §4 boot defaults → Task 1; §5 LoadChatCfg → Task 3; §6 handlers → Task 4; §7 UI → Task 5; §8 migration (remove sentry_chat from DLP; clean intro) → Task 2 Step 5; §9 testing → each task + Task 6; §10 docs → Task 6.
- `SentryCfgKeyChat` semantic change (value `"sentry_chat"`→`"chat"`, DLP stream→sentry stream) is called out explicitly in Task 2 Interfaces + Step 5 so no consumer silently reads the old key. `LoadChatCfg` (Task 3) and handlers (Task 4) consume the new value.
- The Phase-1 `InitSentry(…, dlpCfg, …)` signature is changed in Task 2 Step 4/7; callers updated in Step 7 — flagged so a reviewer checks the grep covered them.
- Settings-API wiring (`sources.go` EffectiveValue/ApplyChange + `Save()` + BindEnv) is explicitly in Task 1 Steps 2/4 — the exact three-point wiring Phase 1 Task 1 proved is required for new config keys.
- DLP config-store key-list tests: Task 2 Step 8 notes updating any DLP test asserting `sentry_chat` in `DLPConfigKeys()`.
