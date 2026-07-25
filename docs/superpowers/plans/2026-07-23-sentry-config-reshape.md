# Sentry Config Reshape Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reshape Sentry's config to two self-contained rows — `chat` (own endpoint + budgets, no system_prompt) and `memory` (enable toggle + own embedding endpoint) — dropping the `shared` row / model-catalog / `dlp_config_resolve` indirection.

**Architecture:** `agentguard_sentry_config` keeps its DDL but its rows change to `chat` + `memory`, each carrying a concrete `{base_url, api_key, model, …}`. `LoadChatCfg` reads the `chat` row directly (no resolver). The chat system prompt becomes a built-in constant (not config). Embedding config moves under a `memory` row gated by `memory.enabled`; the Settings editor gains a memory enable toggle and test-connection skips the embedding check when disabled. Spec: `docs/superpowers/specs/2026-07-23-sentry-config-reshape-design.md`. Branch: `feature/207-sentry-agent-design`. Issues #207/#209.

**Tech Stack:** Go + Gin + proton-go-driver, React 18 + Vite, Python stdlib mock model.

## Global Constraints

- Go builds/tests from `backend/` with `CGO_ENABLED=0`; gofmt-clean; "Timeplus Enterprise" never "Proton".
- DDL uses `IF NOT EXISTS`; config seeding is seed-if-absent per key (never clobber runtime edits); native `PrepareBatch` for writes.
- Both `*_api_key` boot keys are secrets: registry `Type: TypeSecret`, `BindEnv`, `EffectiveValue`/`ApplyChange` cases, BUT **never** written to `config.yaml` by `Save()` (matches `dlp.api_key`).
- `model` in each row is a literal model id (no catalog-name resolution). Sentry no longer calls `dlp_config_resolve`.
- Frontend: design tokens from `tailwind.config.js`; no border-line sectioning; no shadows; radius ≤4px; gradient CTA `bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023]`.
- Config UI reachable whenever `sentry.enabled` regardless of `dlp.enabled`.
- Commit after each task on the current branch.

---

### Task 1: Config store — two-row model (`chat` + `memory`)

**Files:**
- Modify: `backend/internal/timeplus/sentry_config_store.go` (consts, `SentryConfigKeys`, `SentryConfigRows`, `DefaultSentryChatRow`, `ValidateSentryConfigValue`)
- Modify: `backend/internal/timeplus/sentry_config_store_test.go` (update tests to the new shapes)

**Interfaces:**
- Consumes: `config.SentryConfig` (its new fields land in Task 2; for THIS task, reference the field names the plan will add — `ChatBaseURL, ChatAPIKey, ChatModel, RequestTimeoutSeconds, MaxOutputTokens, MemoryEnabled, EmbeddingBaseURL, EmbeddingAPIKey, EmbeddingModel, EmbeddingDim`. Task 1 and Task 2 must land together to compile; implement Task 2's struct fields first if compiling in isolation, OR do Tasks 1+2 in one commit. This plan does them as one task boundary — see note.)
- Produces: `tp.SentryCfgKeyChat = "chat"`, `tp.SentryCfgKeyMemory = "memory"` (drop `SentryCfgKeyShared`/`SentryCfgKeyEmbed`); `SentryConfigKeys() = ["chat","memory"]`; `SentryConfigRows(config.SentryConfig) map[string]string` emits `chat`+`memory`; `DefaultSentryChatRow` / new `DefaultSentryMemoryRow`.

> **NOTE:** Tasks 1 and 2 are mutually dependent (the store reads new `config.SentryConfig` fields; the config struct exists to feed the store). Implement Task 2's `config.SentryConfig` field changes FIRST within this task's working set so the package compiles, then Task 1's store changes — or treat Tasks 1+2 as a single commit. Kept as two review-gates because the wiring (Task 2) and the row semantics (Task 1) can be judged independently. The implementer of Task 1 should apply the Task 2 struct diff (below, copied) to compile, and note it; the Task 2 implementer then owns the full config wiring + tests.

Task 2 struct (apply to compile Task 1):
```go
type SentryConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	SkillDir string `mapstructure:"skill_dir"`
	// chat endpoint boot defaults → agentguard_sentry_config `chat` row
	ChatBaseURL           string `mapstructure:"chat_base_url"`
	ChatAPIKey            string `mapstructure:"chat_api_key"`
	ChatModel             string `mapstructure:"chat_model"`
	RequestTimeoutSeconds int    `mapstructure:"request_timeout_seconds"`
	MaxOutputTokens       int    `mapstructure:"max_output_tokens"`
	// memory(embedding) endpoint boot defaults → `memory` row (Phase-2 feature)
	MemoryEnabled   bool   `mapstructure:"memory_enabled"`
	EmbeddingBaseURL string `mapstructure:"embedding_base_url"`
	EmbeddingAPIKey  string `mapstructure:"embedding_api_key"`
	EmbeddingModel   string `mapstructure:"embedding_model"`
	EmbeddingDim     int    `mapstructure:"embedding_dim"`
}
```

- [ ] **Step 1: Update the failing tests** in `sentry_config_store_test.go`. Replace the three-row/shared-catalog assertions with:

```go
func TestSentryConfigRowsSeedsChatAndMemory(t *testing.T) {
	rows := SentryConfigRows(config.SentryConfig{
		ChatBaseURL: "https://api.openai.com/v1", ChatAPIKey: "sk", ChatModel: "gpt-4o",
		RequestTimeoutSeconds: 60, MaxOutputTokens: 1024,
		MemoryEnabled: true, EmbeddingBaseURL: "http://localhost:11434/v1",
		EmbeddingAPIKey: "", EmbeddingModel: "nomic-embed-text", EmbeddingDim: 768,
	})
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d (%v)", len(rows), rows)
	}
	// chat: self-contained endpoint, NO system_prompt, literal model.
	var chat map[string]any
	if err := json.Unmarshal([]byte(rows[SentryCfgKeyChat]), &chat); err != nil {
		t.Fatal(err)
	}
	if chat["base_url"] != "https://api.openai.com/v1" || chat["model"] != "gpt-4o" {
		t.Fatalf("chat endpoint wrong: %v", chat)
	}
	if _, has := chat["system_prompt"]; has {
		t.Fatal("chat row must NOT contain system_prompt")
	}
	if chat["max_iterations"].(float64) != 12 {
		t.Fatalf("chat budgets missing: %v", chat)
	}
	// memory: enable flag + own embedding endpoint.
	var mem map[string]any
	if err := json.Unmarshal([]byte(rows[SentryCfgKeyMemory]), &mem); err != nil {
		t.Fatal(err)
	}
	if mem["enabled"] != true || mem["base_url"] != "http://localhost:11434/v1" ||
		mem["model"] != "nomic-embed-text" || mem["embedding_dim"].(float64) != 768 {
		t.Fatalf("memory row wrong: %v", mem)
	}
}

func TestSentryConfigKeysAreChatMemory(t *testing.T) {
	got := SentryConfigKeys()
	if len(got) != 2 || !IsSentryConfigKey("chat") || !IsSentryConfigKey("memory") ||
		IsSentryConfigKey("shared") || IsSentryConfigKey("embed") {
		t.Fatalf("keys wrong: %v", got)
	}
}

func TestValidateSentryConfigValue(t *testing.T) {
	cases := []struct{ key, value, wantErr string }{
		{"chat", `{"base_url":"x","model":"y"}`, ""},
		{"chat", `not json`, "valid JSON"},
		{"memory", `{"enabled":false}`, ""},
		{"memory", `{"enabled":"yes"}`, "enabled must be a bool"},
		{"memory", `{"embedding_dim":"big"}`, "embedding_dim must be a number"},
		{"memory", `{"embedding_dim":768}`, ""},
	}
	for _, c := range cases {
		err := ValidateSentryConfigValue(c.key, c.value)
		if c.wantErr == "" && err != nil {
			t.Fatalf("%s %q: unexpected %v", c.key, c.value, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Fatalf("%s %q: want %q got %v", c.key, c.value, c.wantErr, err)
		}
	}
}
```

Also delete any old `TestSentryConfigRowsSeedsThreeRows`/shared-catalog test.

- [ ] **Step 2: Run → FAIL** (`CGO_ENABLED=0 go test ./internal/timeplus/ -run 'Sentry' -v`) — undefined `SentryCfgKeyMemory`, wrong row count.

- [ ] **Step 3: Implement.** In `sentry_config_store.go`:

Consts:
```go
const (
	SentryCfgKeyChat   = "chat"
	SentryCfgKeyMemory = "memory"
)
```
`SentryConfigKeys`:
```go
func SentryConfigKeys() []string { return []string{SentryCfgKeyChat, SentryCfgKeyMemory} }
```
`ValidateSentryConfigValue` (replace the whole switch):
```go
func ValidateSentryConfigValue(key, value string) error {
	var generic any
	if err := json.Unmarshal([]byte(value), &generic); err != nil {
		return fmt.Errorf("value is not valid JSON: %w", err)
	}
	switch key {
	case SentryCfgKeyChat:
		// self-contained endpoint + budgets; no field is mandatory here (empty
		// base_url surfaces at LoadChatCfg), but the JSON must be an object.
		if _, ok := generic.(map[string]any); !ok {
			return fmt.Errorf("%s: value must be a JSON object", key)
		}
	case SentryCfgKeyMemory:
		var row struct {
			Enabled any `json:"enabled"`
			Dim     any `json:"embedding_dim"`
		}
		_ = json.Unmarshal([]byte(value), &row)
		if row.Enabled != nil {
			if _, ok := row.Enabled.(bool); !ok {
				return fmt.Errorf("%s: enabled must be a bool", key)
			}
		}
		if row.Dim != nil {
			if _, ok := row.Dim.(float64); !ok {
				return fmt.Errorf("%s: embedding_dim must be a number", key)
			}
		}
	}
	return nil
}
```
`SentryConfigRows` + `DefaultSentryChatRow` + new `DefaultSentryMemoryRow` (replace `SentryConfigRows`/`DefaultSentryChatRow`, delete the shared/models/embed logic):
```go
// SentryConfigRows renders the two seed rows from boot config. Each row is a
// self-contained endpoint (literal base_url/api_key/model) — no shared catalog.
func SentryConfigRows(sCfg config.SentryConfig) map[string]string {
	return map[string]string{
		SentryCfgKeyChat:   DefaultSentryChatRow(sCfg),
		SentryCfgKeyMemory: DefaultSentryMemoryRow(sCfg),
	}
}

// DefaultSentryChatRow renders the `chat` row: the loop's endpoint + budgets.
// No system_prompt — the loop uses the built-in DefaultSentrySystemPrompt.
func DefaultSentryChatRow(sCfg config.SentryConfig) string {
	timeout := sCfg.RequestTimeoutSeconds
	if timeout <= 0 {
		timeout = 60
	}
	maxTokens := sCfg.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	b, _ := json.Marshal(map[string]any{
		"base_url":              sCfg.ChatBaseURL,
		"api_key":               sCfg.ChatAPIKey,
		"model":                 sCfg.ChatModel,
		"timeout":               timeout,
		"max_tokens":            maxTokens,
		"max_iterations":        12,
		"turn_token_budget":     32000,
		"sql_timeout_ms":        30000,
		"sql_max_rows":          1000,
		"tool_result_max_bytes": 16384,
	})
	return string(b)
}

// DefaultSentryMemoryRow renders the `memory` row: enable flag + the embedding
// endpoint. Consumed by Sentry memory in Phase 2; config-only today.
func DefaultSentryMemoryRow(sCfg config.SentryConfig) string {
	timeout := sCfg.RequestTimeoutSeconds
	if timeout <= 0 {
		timeout = 60
	}
	dim := sCfg.EmbeddingDim
	if dim <= 0 {
		dim = 1536
	}
	b, _ := json.Marshal(map[string]any{
		"enabled":       sCfg.MemoryEnabled,
		"base_url":      sCfg.EmbeddingBaseURL,
		"api_key":       sCfg.EmbeddingAPIKey,
		"model":         sCfg.EmbeddingModel,
		"embedding_dim": dim,
		"timeout":       timeout,
	})
	return string(b)
}
```

Keep `DefaultSentrySystemPrompt` where it is (still exported for the loop). Remove any now-unused import.

- [ ] **Step 4: Run → GREEN** (`CGO_ENABLED=0 go test ./internal/timeplus/ -run Sentry -v`). Full package build may fail until Task 2 struct lands — apply the Task 2 struct block above to `config.go` to compile, and note it in the report. **Commit** both together: `refactor(sentry): two-row config model (chat + memory), drop shared catalog`.

---

### Task 2: Boot defaults — split chat/embedding keys + `memory_enabled`

**Files:**
- Modify: `backend/internal/config/config.go` (SentryConfig struct — already applied in Task 1; here finalize SetDefault/read/BindEnv/Save)
- Modify: `backend/internal/config/registry.go` (replace the sentry model entries)
- Modify: `backend/internal/config/sources.go` (replace the sentry model cases)
- Modify: `backend/internal/config/config_test.go` (roundtrip covers new keys; api_key keys NOT persisted), `registry_test.go` (key list)

**Interfaces:**
- Produces: `config.SentryConfig` with the fields listed in Task 1's struct block. Viper keys: `sentry.chat_base_url`, `sentry.chat_api_key`, `sentry.chat_model`, `sentry.request_timeout_seconds`, `sentry.max_output_tokens`, `sentry.memory_enabled`, `sentry.embedding_base_url`, `sentry.embedding_api_key`, `sentry.embedding_model`, `sentry.embedding_dim`. Removed: `sentry.base_url`, `sentry.api_key`.

- [ ] **Step 1: SetDefault + reads + env + save** in `config.go`. Replace the current `sentry.base_url`/`sentry.api_key`/`sentry.chat_model`/… default block with:
```go
	v.SetDefault("sentry.chat_base_url", "")
	v.SetDefault("sentry.chat_api_key", "")
	v.SetDefault("sentry.chat_model", "")
	v.SetDefault("sentry.request_timeout_seconds", 60)
	v.SetDefault("sentry.max_output_tokens", 1024)
	v.SetDefault("sentry.memory_enabled", false)
	v.SetDefault("sentry.embedding_base_url", "")
	v.SetDefault("sentry.embedding_api_key", "")
	v.SetDefault("sentry.embedding_model", "")
	v.SetDefault("sentry.embedding_dim", 1536)
```
Reads (replace the `cfg.Sentry.BaseURL`/`APIKey`/… block):
```go
	cfg.Sentry.ChatBaseURL = v.GetString("sentry.chat_base_url")
	cfg.Sentry.ChatAPIKey = v.GetString("sentry.chat_api_key")
	cfg.Sentry.ChatModel = v.GetString("sentry.chat_model")
	cfg.Sentry.RequestTimeoutSeconds = v.GetInt("sentry.request_timeout_seconds")
	cfg.Sentry.MaxOutputTokens = v.GetInt("sentry.max_output_tokens")
	cfg.Sentry.MemoryEnabled = v.GetBool("sentry.memory_enabled")
	cfg.Sentry.EmbeddingBaseURL = v.GetString("sentry.embedding_base_url")
	cfg.Sentry.EmbeddingAPIKey = v.GetString("sentry.embedding_api_key")
	cfg.Sentry.EmbeddingModel = v.GetString("sentry.embedding_model")
	cfg.Sentry.EmbeddingDim = v.GetInt("sentry.embedding_dim")
```
BindEnv (replace the sentry model binds, keep enabled/skill_dir):
```go
	_ = v.BindEnv("sentry.chat_base_url", "SENTRY_CHAT_BASE_URL")
	_ = v.BindEnv("sentry.chat_api_key", "SENTRY_CHAT_API_KEY")
	_ = v.BindEnv("sentry.chat_model", "SENTRY_CHAT_MODEL")
	_ = v.BindEnv("sentry.request_timeout_seconds", "SENTRY_REQUEST_TIMEOUT_SECONDS")
	_ = v.BindEnv("sentry.max_output_tokens", "SENTRY_MAX_OUTPUT_TOKENS")
	_ = v.BindEnv("sentry.memory_enabled", "SENTRY_MEMORY_ENABLED")
	_ = v.BindEnv("sentry.embedding_base_url", "SENTRY_EMBEDDING_BASE_URL")
	_ = v.BindEnv("sentry.embedding_api_key", "SENTRY_EMBEDDING_API_KEY")
	_ = v.BindEnv("sentry.embedding_model", "SENTRY_EMBEDDING_MODEL")
	_ = v.BindEnv("sentry.embedding_dim", "SENTRY_EMBEDDING_DIM")
```
Save() (replace the sentry model `v.Set` block — NOTE: **omit** both `*_api_key` keys, matching how `sentry.api_key`/`dlp.api_key` are excluded):
```go
	v.Set("sentry.chat_base_url", cfg.Sentry.ChatBaseURL)
	v.Set("sentry.chat_model", cfg.Sentry.ChatModel)
	v.Set("sentry.request_timeout_seconds", cfg.Sentry.RequestTimeoutSeconds)
	v.Set("sentry.max_output_tokens", cfg.Sentry.MaxOutputTokens)
	v.Set("sentry.memory_enabled", cfg.Sentry.MemoryEnabled)
	v.Set("sentry.embedding_base_url", cfg.Sentry.EmbeddingBaseURL)
	v.Set("sentry.embedding_model", cfg.Sentry.EmbeddingModel)
	v.Set("sentry.embedding_dim", cfg.Sentry.EmbeddingDim)
	// sentry.chat_api_key / sentry.embedding_api_key are secrets — never written.
```
(Keep the existing `sentry.enabled`/`sentry.skill_dir` Set lines.)

- [ ] **Step 2: Registry** in `registry.go`. Replace the `sentry.base_url`/`sentry.api_key`/`sentry.chat_model`/`sentry.embedding_model`/`sentry.embedding_dim` entries (lines ~134-146) with (match the existing entry-literal field style — `Type`, `EnvVar`, `TypeSecret` for keys):
```go
		{Key: "sentry.chat_base_url", Section: "sentry", Label: "Sentry chat base URL",
			Description: "OpenAI-compatible endpoint for the Sentry chat model (boot default; edit live under Settings → Sentry Agent).", Type: TypeString, EnvVar: "SENTRY_CHAT_BASE_URL"},
		{Key: "sentry.chat_api_key", Section: "sentry", Label: "Sentry chat API key",
			Description: "API key for the Sentry chat endpoint.", Type: TypeSecret, EnvVar: "SENTRY_CHAT_API_KEY"},
		{Key: "sentry.chat_model", Section: "sentry", Label: "Sentry chat model",
			Description: "Tool-calling-capable chat model id for the Sentry agent loop.", Type: TypeString, EnvVar: "SENTRY_CHAT_MODEL"},
		{Key: "sentry.memory_enabled", Section: "sentry", Label: "Enable Sentry memory (Phase 2)",
			Description: "Enables Sentry's embedding-backed memory (Phase 2). When off, no embedding endpoint is needed.", Type: TypeBool, EnvVar: "SENTRY_MEMORY_ENABLED"},
		{Key: "sentry.embedding_base_url", Section: "sentry", Label: "Sentry embedding base URL",
			Description: "OpenAI-compatible endpoint for the Sentry memory embedding model (may differ from chat, e.g. local Ollama).", Type: TypeString, EnvVar: "SENTRY_EMBEDDING_BASE_URL"},
		{Key: "sentry.embedding_api_key", Section: "sentry", Label: "Sentry embedding API key",
			Description: "API key for the Sentry embedding endpoint.", Type: TypeSecret, EnvVar: "SENTRY_EMBEDDING_API_KEY"},
		{Key: "sentry.embedding_model", Section: "sentry", Label: "Sentry embedding model",
			Description: "Embedding model id for Sentry memory (Phase 2).", Type: TypeString, EnvVar: "SENTRY_EMBEDDING_MODEL"},
		{Key: "sentry.embedding_dim", Section: "sentry", Label: "Sentry embedding dimension",
			Description: "Vector dimension of the Sentry embedding model (Phase 2 memory).", Type: TypeInt, EnvVar: "SENTRY_EMBEDDING_DIM"},
```
(Keep `sentry.request_timeout_seconds`/`sentry.max_output_tokens` entries unchanged. Verify the exact `Type` constant names — `TypeString`/`TypeBool`/`TypeInt`/`TypeSecret` — by reading the top of registry.go; the current file already uses them.)

- [ ] **Step 3: sources.go** — replace the sentry model cases (~lines 199-207 and 267-273) in BOTH `EffectiveValue` and `ApplyChange`, keeping enabled/skill_dir/request_timeout_seconds/max_output_tokens:
```go
	// EffectiveValue:
	case "sentry.chat_base_url":
		return cfg.Sentry.ChatBaseURL
	case "sentry.chat_api_key":
		return cfg.Sentry.ChatAPIKey
	case "sentry.chat_model":
		return cfg.Sentry.ChatModel
	case "sentry.memory_enabled":
		return cfg.Sentry.MemoryEnabled
	case "sentry.embedding_base_url":
		return cfg.Sentry.EmbeddingBaseURL
	case "sentry.embedding_api_key":
		return cfg.Sentry.EmbeddingAPIKey
	case "sentry.embedding_model":
		return cfg.Sentry.EmbeddingModel
	case "sentry.embedding_dim":
		return cfg.Sentry.EmbeddingDim
```
```go
	// ApplyChange (mirror the setString/setBool/setInt helper usage already in the file):
	case "sentry.chat_base_url":
		return setString(&cfg.Sentry.ChatBaseURL, value)
	case "sentry.chat_api_key":
		return setString(&cfg.Sentry.ChatAPIKey, value)
	case "sentry.chat_model":
		return setString(&cfg.Sentry.ChatModel, value)
	case "sentry.memory_enabled":
		return setBool(&cfg.Sentry.MemoryEnabled, value)
	case "sentry.embedding_base_url":
		return setString(&cfg.Sentry.EmbeddingBaseURL, value)
	case "sentry.embedding_api_key":
		return setString(&cfg.Sentry.EmbeddingAPIKey, value)
	case "sentry.embedding_model":
		return setString(&cfg.Sentry.EmbeddingModel, value)
	case "sentry.embedding_dim":
		return setInt(&cfg.Sentry.EmbeddingDim, value)
```

- [ ] **Step 4: Tests.** In `config_test.go` `TestSave_RoundTrip`: set the new fields (`ChatBaseURL:"http://c/v1", ChatAPIKey:"sk-c", ChatModel:"gpt-4o", MemoryEnabled:true, EmbeddingBaseURL:"http://e/v1", EmbeddingAPIKey:"sk-e", EmbeddingModel:"nomic", EmbeddingDim:768`) and assert after save→load: the non-secret fields survive, `MemoryEnabled==true`, and BOTH `ChatAPIKey`/`EmbeddingAPIKey` come back `""` (secrets not persisted). Remove old `BaseURL`/`APIKey` assertions. In `registry_test.go`, update the `want` key list: drop `sentry.base_url`/`sentry.api_key`, add the new keys.

- [ ] **Step 5: Verify + commit**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/config/... ./internal/timeplus/...`
Expected: PASS. Commit: `feat(sentry): split chat/embedding boot defaults + memory_enabled`.

---

### Task 3: `LoadChatCfg` reads the `chat` row directly (no resolver); system prompt is a constant

**Files:**
- Modify: `backend/internal/sentry/llm.go` (`chatCfgResolveSQL` → direct read; `ChatCfg.SystemPrompt` no longer JSON-decoded)
- Modify: `backend/internal/sentry/llm_test.go` (update the SQL-target test)

**Interfaces:**
- Consumes: `tp.SentryCfgKeyChat`, `agentguard_sentry_config`.
- Produces: `LoadChatCfg` unchanged signature; reads the `chat` row without `dlp_config_resolve`; `ChatCfg.SystemPrompt` populated by `applyDefaults` from `tp.DefaultSentrySystemPrompt` (never from config JSON).

- [ ] **Step 1: Update the test** in `llm_test.go` (`TestLoadChatCfgSQLTargetsSentryStream`):
```go
func TestLoadChatCfgSQLTargetsSentryStream(t *testing.T) {
	sql := chatCfgResolveSQL()
	if !strings.Contains(sql, "agentguard_sentry_config") || !strings.Contains(sql, "key='chat'") {
		t.Fatalf("expected chat row read, got: %s", sql)
	}
	if strings.Contains(sql, "dlp_config_resolve") || strings.Contains(sql, "agentguard_dlp_config") {
		t.Fatalf("must not use the DLP resolver/stream: %s", sql)
	}
}

func TestChatCfgIgnoresConfigSystemPrompt(t *testing.T) {
	// A system_prompt in the row JSON must NOT override the built-in constant.
	var cc ChatCfg
	_ = json.Unmarshal([]byte(`{"base_url":"x","model":"y","system_prompt":"HACKED"}`), &cc)
	cc.applyDefaults()
	if cc.SystemPrompt != tp.DefaultSentrySystemPrompt {
		t.Fatalf("system prompt must be the built-in constant, got %q", cc.SystemPrompt)
	}
}
```
(Add `encoding/json` + `tp` imports to the test if absent.)

- [ ] **Step 2: Run → FAIL** — current SQL wraps in `dlp_config_resolve`; current `ChatCfg` decodes `system_prompt` so the second test sees "HACKED".

- [ ] **Step 3: Implement.** In `llm.go`:
Change the `SystemPrompt` json tag so config never sets it:
```go
	SystemPrompt       string `json:"-"`
```
Direct-read SQL (replace `chatCfgResolveSQL`):
```go
// chatCfgResolveSQL reads the self-contained `chat` row directly — the row is a
// concrete endpoint (base_url/api_key/model/budgets), so no dlp_config_resolve
// fold/catalog resolution is needed (unlike DLP).
func chatCfgResolveSQL() string {
	return fmt.Sprintf(
		"SELECT coalesce((SELECT value FROM table(agentguard_sentry_config) WHERE key='%s'), '') AS cfg",
		tp.SentryCfgKeyChat)
}
```
`applyDefaults` is unchanged — it already sets `SystemPrompt = tp.DefaultSentrySystemPrompt` when empty, and now it's always empty from JSON. `LoadChatCfg` body is otherwise unchanged (it unmarshals `rows[0]["cfg"]` into `ChatCfg`, calls `applyDefaults`, errors on empty base_url). Update the stale comments at llm.go ~lines 43/94 to say "chat row of agentguard_sentry_config" (no "sentry_chat"/resolver).

- [ ] **Step 4: Verify + commit**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/sentry/...`
Expected: PASS. Commit: `feat(sentry): read chat row directly; system prompt is a built-in constant`.

---

### Task 4: Provisioning + test-connection for the two-row model, memory-gated embedding

**Files:**
- Modify: `backend/internal/timeplus/sentry.go` (`seedSentryConfig` seeds `chat`+`memory` — should already work via `SentryConfigRows`; verify + adjust the emit message/count)
- Modify: `backend/internal/handlers/sentry_config.go` (`sentryTestBody`, `resolveSentryTestCreds`, `storedSentrySharedRow`→per-row reads, `TestSentryConfig`: chat always, embedding only when memory enabled)
- Modify: `backend/internal/handlers/sentry_config_test.go` (resolve-creds + memory-skip tests)

**Interfaces:**
- Consumes: `tp.SentryCfgKeyChat`, `tp.SentryCfgKeyMemory`, `tp.ReadSentryConfigValue`, `config.SentryConfig` new fields.
- Produces: `TestSentryConfig` returns `checks.embedding = {"skipped": true}` (excluded from `ok`) when memory disabled; chat + embedding creds resolved from their own rows.

- [ ] **Step 1: `seedSentryConfig`** — it iterates `SentryConfigRows(sCfg)` seed-if-absent, so it already seeds `chat`+`memory` once Task 1 lands. Read it and confirm; if it hard-codes row names or a count in an emit/log string, update to the 2-row reality. No behavior change beyond the row set.

- [ ] **Step 1b: FIX `mergeSentryValue` api_key preservation for the new keys.** Read `mergeSentryValue` in `sentry_config.go`. It currently preserves the stored `api_key` (blank input → keep stored) ONLY when `key == "shared"` (the removed row). Since `chat` and `memory` now each carry a top-level `api_key`, update the preservation to fire for `key == SentryCfgKeyChat || key == SentryCfgKeyMemory` (top-level `api_key` only — there is no `models` catalog anymore, so drop the per-model preservation). Add a test: PUT `chat` with a blank/absent `api_key` and a stored `chat` row containing `"api_key":"kept"` → the merged value retains `"kept"` (use the same table style as the existing merge tests; if a live-DB read is needed, at least assert the merge helper preserves when given a stored JSON with an api_key). Without this, saving the chat panel with the (always-blank, redacted) api_key field WIPES the stored key — a data-loss regression.

- [ ] **Step 2: Failing tests** in `sentry_config_test.go`. Replace the shared-row resolve test with per-endpoint tests:
```go
func TestResolveSentryChatCreds_Precedence(t *testing.T) {
	// body → boot → stored chat row.
	got := resolveSentryChatCreds(
		sentryTestBody{}, // empty body
		config.SentryConfig{ChatBaseURL: "", ChatAPIKey: "", ChatModel: ""},
		`{"base_url":"http://stored/v1","api_key":"stored-key","model":"stored-model"}`,
	)
	if got.BaseURL != "http://stored/v1" || got.APIKey != "stored-key" || got.Model != "stored-model" {
		t.Fatalf("stored-row fallback failed: %+v", got)
	}
	got2 := resolveSentryChatCreds(
		sentryTestBody{BaseURL: "http://body/v1"},
		config.SentryConfig{ChatBaseURL: "http://boot/v1"},
		`{"base_url":"http://stored/v1"}`,
	)
	if got2.BaseURL != "http://body/v1" {
		t.Fatalf("body should win: %+v", got2)
	}
}

func TestResolveSentryEmbedCreds_Precedence(t *testing.T) {
	got := resolveSentryEmbedCreds(
		sentryTestBody{},
		config.SentryConfig{EmbeddingBaseURL: "http://boot-emb/v1", EmbeddingModel: "boot-emb"},
		`{"base_url":"http://stored-emb/v1","api_key":"emb-key","model":"stored-emb","embedding_dim":768}`,
	)
	// boot wins over stored for base_url/model here; stored fills api_key/dim.
	if got.BaseURL != "http://boot-emb/v1" || got.Model != "boot-emb" ||
		got.APIKey != "emb-key" || got.EmbeddingDim != 768 {
		t.Fatalf("embed resolve wrong: %+v", got)
	}
}
```
(Keep the existing unknown-key/bad-JSON 400 tests for `UpsertSentryConfig`.)

- [ ] **Step 3: Implement.** In `sentry_config.go`, split the single shared resolver into two per-endpoint resolvers reading their own rows. Define:
```go
type resolvedChatCreds struct{ BaseURL, APIKey, Model string }
type resolvedEmbedCreds struct {
	BaseURL, APIKey, Model string
	EmbeddingDim           int
}

func resolveSentryChatCreds(body sentryTestBody, boot config.SentryConfig, storedChatJSON string) resolvedChatCreds {
	var stored struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
		Model   string `json:"model"`
	}
	if storedChatJSON != "" {
		_ = json.Unmarshal([]byte(storedChatJSON), &stored)
	}
	return resolvedChatCreds{
		BaseURL: orStr(body.BaseURL, orStr(boot.ChatBaseURL, stored.BaseURL)),
		APIKey:  orStr(body.APIKey, orStr(boot.ChatAPIKey, stored.APIKey)),
		Model:   orStr(body.ChatModel, orStr(boot.ChatModel, stored.Model)),
	}
}

func resolveSentryEmbedCreds(body sentryTestBody, boot config.SentryConfig, storedMemoryJSON string) resolvedEmbedCreds {
	var stored struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
		Model   string `json:"model"`
		Dim     int    `json:"embedding_dim"`
	}
	if storedMemoryJSON != "" {
		_ = json.Unmarshal([]byte(storedMemoryJSON), &stored)
	}
	dim := body.EmbeddingDim
	if dim == 0 {
		dim = boot.EmbeddingDim
	}
	if dim == 0 {
		dim = stored.Dim
	}
	return resolvedEmbedCreds{
		BaseURL:      orStr(body.EmbeddingBaseURL, orStr(boot.EmbeddingBaseURL, stored.BaseURL)),
		APIKey:       orStr(body.EmbeddingAPIKey, orStr(boot.EmbeddingAPIKey, stored.APIKey)),
		Model:        orStr(body.EmbeddingModel, orStr(boot.EmbeddingModel, stored.Model)),
		EmbeddingDim: dim,
	}
}
```
Extend `sentryTestBody` with `EmbeddingBaseURL string json:"embedding_base_url"`, `EmbeddingAPIKey string json:"embedding_api_key"`, and a `MemoryEnabled *bool json:"memory_enabled"` (pointer so absent ≠ false). Delete `storedSentrySharedRow`/`resolveSentryTestCreds`/`resolvedSentryTestCreds`.

Rewrite `TestSentryConfig`'s body:
```go
func TestSentryConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body sentryTestBody
		_ = c.ShouldBindJSON(&body)
		config.Mu.RLock()
		s := cfg.Sentry
		config.Mu.RUnlock()
		rc := auth.RequestCfg(c, cfg)

		// Chat creds: body → boot → stored `chat` row.
		storedChat, err := tp.ReadSentryConfigValue(&rc.Timeplus, tp.SentryCfgKeyChat)
		if err != nil {
			log.Printf("[sentry] TestSentryConfig: read chat row: %v", err)
		}
		chat := resolveSentryChatCreds(body, s, storedChat)

		client := &http.Client{Timeout: 10 * time.Second}
		chatBase := strings.TrimRight(chat.BaseURL, "/")
		checks := gin.H{}
		reach := dlpCheckReachable(c, client, chatBase, chat.APIKey)
		checks["reachable"] = reach
		ok := reach["ok"] == true
		if ok {
			chatCheck := sentryCheckChatToolCalling(c, client, chatBase, chat.APIKey, chat.Model)
			checks["chat"] = chatCheck
			ok = chatCheck["ok"] == true
		}

		// Embedding: only when memory is enabled (body override → stored/boot memory row).
		storedMem, _ := tp.ReadSentryConfigValue(&rc.Timeplus, tp.SentryCfgKeyMemory)
		memEnabled := s.MemoryEnabled
		if body.MemoryEnabled != nil {
			memEnabled = *body.MemoryEnabled
		} else if storedMem != "" {
			var m struct{ Enabled bool `json:"enabled"` }
			_ = json.Unmarshal([]byte(storedMem), &m)
			memEnabled = m.Enabled
		}
		if memEnabled {
			emb := resolveSentryEmbedCreds(body, s, storedMem)
			embCheck := dlpCheckEmbedding(c, client, strings.TrimRight(emb.BaseURL, "/"), emb.APIKey, emb.Model, emb.EmbeddingDim)
			checks["embedding"] = embCheck
			if ok {
				ok = embCheck["ok"] == true
			}
		} else {
			checks["embedding"] = gin.H{"skipped": true}
		}
		c.JSON(http.StatusOK, gin.H{"ok": ok, "checks": checks})
	}
}
```
(Confirm `dlpCheckReachable`/`dlpCheckEmbedding`/`sentryCheckChatToolCalling` signatures against the current file and adjust arg order; they already exist. Keep `orStr`.)

- [ ] **Step 4: Verify + commit**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/handlers/ -run Sentry && CGO_ENABLED=0 go test ./internal/timeplus/...`
Expected: PASS. Commit: `feat(sentry): per-endpoint test-connection, embedding skipped when memory off`.

---

### Task 5: Frontend — two-panel editor (Chat + Memory toggle), drop system_prompt

**Files:**
- Modify: `frontend/src/components/SentryConfigEditor.jsx` (rebuild around `chat`+`memory` rows)
- Modify: `frontend/src/api/client.js` (no new methods; verify `putSentryConfig`/`testSentryConfig` unchanged)
- Modify: `frontend/src/components/settings/SettingsSection.jsx` (SENTRY_SERVER_KEYS filter — new key names)

**Interfaces:**
- Consumes: `GET /api/sentry/config` now returns rows keyed `chat`/`memory`; `PUT /api/sentry/config/chat|memory`; `POST /api/sentry/config/test` accepts `{base_url,api_key,chat_model,embedding_base_url,embedding_api_key,embedding_model,embedding_dim,memory_enabled}`.

- [ ] **Step 1: Rebuild `SentryConfigEditor.jsx`.** Read the current file first. Replace the `shared`/`chat`(catalog-name)/`embed` model with two panels driven by `byKey.chat` and `byKey.memory`:
  - Remove `modelsToRows`/`rowsToModels`/`ModelSelect`/`resolveModelId` and all `shared`/catalog state.
  - **Chat panel** (`chat` row): inputs for `base_url`, `api_key` (blank preserves; show "•••• set" when `api_key_set` for the chat row), `model`, and the 6 budget knobs with the existing ceiling hints. **Remove the `system_prompt` field entirely.** Save → `putSentryConfig('chat', {base_url, api_key, model, timeout, max_tokens, max_iterations, turn_token_budget, sql_timeout_ms, sql_max_rows, tool_result_max_bytes})` (send `api_key` from the input; blank preserves server-side).
  - **Memory panel** (`memory` row): an **enable toggle** bound to `memory.enabled`. When off, hide the embedding fields; Save → `putSentryConfig('memory', {enabled:false})`. When on, show `base_url`, `api_key` (blank preserves), `model`, `embedding_dim`; Save → `putSentryConfig('memory', {enabled:true, base_url, api_key, model, embedding_dim})`. Label the panel "Memory (embedding) — used by Sentry memory (Phase 2)".
  - **Test connection** button (top-level or on the Chat panel): call `testSentryConfig({ base_url: chat.base_url, api_key: chat.api_key, chat_model: chat.model, memory_enabled: memory.enabled, embedding_base_url: memory.base_url, embedding_api_key: memory.api_key, embedding_model: memory.model, embedding_dim: Number(memory.embedding_dim)||0 })`. Render `checks.reachable`, `checks.chat` (with `tool_calling`), and `checks.embedding` — when `checks.embedding.skipped` show "skipped (memory disabled)" rather than pass/fail.
  - Design tokens only; keep the dirty-tracking + per-panel Save pattern from the current file. Preserve the blank-api_key-preserve behavior for BOTH panels.

- [ ] **Step 2: `SettingsSection.jsx`** — update the `SENTRY_SERVER_KEYS` set that hides model keys from the raw server-config list: drop `sentry.base_url`/`sentry.api_key`; add `sentry.chat_base_url`, `sentry.chat_api_key`, `sentry.chat_model`, `sentry.memory_enabled`, `sentry.embedding_base_url`, `sentry.embedding_api_key`, `sentry.embedding_model`, `sentry.embedding_dim`, `sentry.request_timeout_seconds`, `sentry.max_output_tokens` (all edited via the editor). Keep `sentry.enabled`/`sentry.skill_dir` visible. Do not touch `DLP_SERVER_KEYS`.

- [ ] **Step 3: Verify + commit**

Run: `cd frontend && npm run lint && npm run build`
Expected: clean (0 lint errors; pre-existing warnings only). Commit: `feat(sentry): two-panel config editor (chat + memory toggle), drop system-prompt field`.

---

### Task 6: Integration script + docs

**Files:**
- Modify: `backend/scripts/sentry_integration.sh` (point `PUT /api/sentry/config/chat` at the mock; test-connection asserts chat ok + embedding skipped; optional memory-on case)
- Modify: `docs/sentry.md` (two-row model)
- Modify: `CLAUDE.md` (config-key + row references)

- [ ] **Step 1: Integration.** Read the current `sentry_integration.sh`. Replace the `PUT /api/sentry/config/shared` (or `/chat` with catalog) step with a self-contained chat row:
```bash
curl -sf -b "$JAR" -X PUT $AG/api/sentry/config/chat -H 'Content-Type: application/json' \
  -d "{\"value\":{\"base_url\":\"http://localhost:$MOCK/v1\",\"api_key\":\"x\",\"model\":\"mock-chat\",\"timeout\":60}}"
```
Add a test-connection assertion (memory off by default → embedding skipped):
```bash
TEST=$(curl -sf -b "$JAR" -X POST $AG/api/sentry/config/test -H 'Content-Type: application/json' \
  -d "{\"base_url\":\"http://localhost:$MOCK/v1\",\"api_key\":\"x\",\"chat_model\":\"mock-chat\",\"memory_enabled\":false}")
echo "$TEST" | grep -q '"tool_calling":true' && echo "$TEST" | grep -q '"skipped":true' \
  || { echo "FAIL: test-connection (want chat tool_calling + embedding skipped): $TEST"; exit 1; }
```
Keep the existing chat round-trip + conversation-replay assertions. Preserve the harness conventions (exec server launch, poll loops, cleanup traps). Optionally add a memory-on case: `PUT /api/sentry/config/memory {enabled:true, base_url→mock, model:mock-embed, embedding_dim:8}` then a test-connection asserting `checks.embedding.ok:true` (the mock serves `/embeddings`). Run `make sentry-integration` against a live Timeplus and get PASS. If no live Timeplus here, make it correct-by-construction and report where it stopped — do NOT fake a pass.

- [ ] **Step 2: Docs — `docs/sentry.md`.** Rewrite the config section for the two-row model: a **Chat model** row (own endpoint: base_url/api_key/model + budgets; NO system prompt — it's a built-in constant) and a **Memory (embedding)** row (enable toggle + own embedding endpoint, may differ from chat e.g. local Ollama; Phase-2 feature). Update the API examples to `PUT /api/sentry/config/chat` and `.../memory` with the new JSON shapes; update the defaults table (drop `system_prompt`; split the boot keys into `sentry.chat_*` and `sentry.embedding_*` + `sentry.memory_enabled`); note test-connection skips the embedding check when memory is off. Remove references to the `shared` row / model catalog / `dlp_config_resolve` for Sentry.

- [ ] **Step 3: `CLAUDE.md`.** Update the Sentry subsection + config references: two-row `agentguard_sentry_config` (`chat`/`memory`), system prompt is a built-in constant (not config), embedding lives under memory gated by `memory.enabled`, Sentry no longer uses `dlp_config_resolve`. Update the streams table description if it enumerates the old rows.

- [ ] **Step 4: Full verification**

```bash
cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./... \
  && cd ../frontend && npm run lint && npm run build
```
Expected: all green. Commit: `feat(sentry): two-row config integration test + docs`.

---

## Self-Review Notes (resolved inline)

- Spec coverage: §2 rows → Task 1; §3 system prompt → Task 3 (`json:"-"` + applyDefaults constant); §4 boot keys → Task 2; §5 backend (store/llm/handler) → Tasks 1/3/4; §6 frontend → Task 5; §7 migration (clean reshape, orphaned rows harmless) → Task 4 Step 1 note; §8 tests → each task; §9 docs → Task 6.
- Tasks 1↔2 mutual dependency (store reads new config fields) is called out explicitly with the struct block duplicated into Task 1 so it compiles; they may be committed together.
- `SentryCfgKeyChat` VALUE is unchanged (`"chat"`) — only `SentryCfgKeyShared`/`Embed` are removed and `SentryCfgKeyMemory` added; no stale-key hazard like the prior plan.
- System-prompt handling: chosen the least-churn path — keep `ChatCfg.SystemPrompt` field but tag `json:"-"` so config can't set it; `applyDefaults` (unchanged) populates the constant; the loop (`loop.go`) needs NO change. Test `TestChatCfgIgnoresConfigSystemPrompt` guards it.
- Secret handling: both `*_api_key` keys excluded from `Save()` (Task 2 Step 1), `TypeSecret` in registry, redacted on GET (unchanged handler), preserved on PUT blank (unchanged `mergeSentryValue` — still keyed on the row's top-level `api_key`; confirm it applies to `chat`/`memory` rows, not just the old `shared`). **Task 4 must verify `mergeSentryValue` preserves api_key for the `chat` and `memory` keys** (it currently special-cased `shared`) — if it only preserved for `key=="shared"`, update it to preserve for `chat`/`memory` too. Added as an explicit check in Task 4.
