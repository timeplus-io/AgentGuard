package timeplus

import (
	"strings"

	"context"
	"encoding/json"
	"fmt"
	"github.com/timeplus/agentguard/internal/secrets"

	"github.com/timeplus/agentguard/internal/config"
)

// agentguard_sentry_config holds Sentry's dedicated model configuration as
// key→JSON rows, independent of the DLP config stream. Rows: `chat` (the
// agent loop's own endpoint + budgets, no system_prompt) and `memory` (an
// enable toggle + its own embedding endpoint for Phase-2 memory). Each row is
// a self-contained, literal endpoint — no shared catalog, no
// dlp_config_resolve indirection. Mutable: an operator upserts a row to
// change a model/endpoint at runtime.
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
	SentryCfgKeyChat   = "chat"
	SentryCfgKeyMemory = "memory"
)

type SentryConfigRow struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
	// DecryptErr is set when this row's api_key could not be decrypted (wrong
	// or missing master key). Value then holds the raw ciphertext. Per-row so
	// one corrupt row doesn't fail the whole read (#237). Not serialized.
	DecryptErr error `json:"-"`
}

func SentryConfigKeys() []string { return []string{SentryCfgKeyChat, SentryCfgKeyMemory} }

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
		raw := asDLPString(r["value"])
		row := SentryConfigRow{
			Key:       asDLPString(r["key"]),
			UpdatedAt: asDLPString(r["updated_at"]),
			UpdatedBy: asDLPString(r["updated_by"]),
		}
		// Per-row tolerance (#237): a wrong/missing master key for ONE row
		// (e.g. the rarely-touched memory row) must not fail the whole read
		// and take down chat. Keep the ciphertext and flag the row; callers
		// needing this key's plaintext (ReadSentryConfigValue) surface the
		// error, while GetSentryConfig can still render the healthy rows.
		if value, derr := decryptSentryValue(raw); derr != nil {
			row.Value, row.DecryptErr = raw, derr
		} else {
			row.Value = value
		}
		out = append(out, row)
	}
	return out, nil
}

// encryptSentryValue seals the top-level api_key field of a config row's
// JSON value through the process codec (#234). No codec / no api_key /
// non-JSON → value passes through unchanged (legacy behavior). Idempotent:
// an already-encrypted key is left as-is.
func encryptSentryValue(value string) string {
	c := secrets.Default()
	if c == nil {
		return value
	}
	var m map[string]any
	if json.Unmarshal([]byte(value), &m) != nil {
		return value
	}
	k, ok := m["api_key"].(string)
	if !ok || k == "" {
		return value
	}
	m["api_key"] = c.Encrypt(k)
	b, err := json.Marshal(m)
	if err != nil {
		return value
	}
	return string(b)
}

// decryptSentryValue is encryptSentryValue's inverse: plaintext (legacy)
// values pass through; an enc:v1 api_key that cannot be opened is an error.
func decryptSentryValue(value string) (string, error) {
	var m map[string]any
	if json.Unmarshal([]byte(value), &m) != nil {
		return value, nil
	}
	k, ok := m["api_key"].(string)
	if !ok || !strings.HasPrefix(k, secrets.Prefix) {
		return value, nil
	}
	plain, err := secrets.Default().Decrypt(k)
	if err != nil {
		return "", err
	}
	m["api_key"] = plain
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func ReadSentryConfigValue(cfg *config.TimeplusConfig, key string) (string, error) {
	rows, err := ReadSentryConfig(cfg)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if r.Key == key {
			if r.DecryptErr != nil {
				// This specific key is corrupt: surface loudly so a bad
				// api_key is never used as a bearer token (#234).
				return "", fmt.Errorf("sentry config %q: %w", key, r.DecryptErr)
			}
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
	if err := batch.Append(key, encryptSentryValue(value), updatedBy); err != nil {
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
	apiType := sCfg.ChatAPIType
	if apiType == "" {
		apiType = "openai"
	}
	b, _ := json.Marshal(map[string]any{
		"api_type":              apiType,
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
		// Anthropic-only knobs (#212): thinking ""|"adaptive", effort
		// ""|low|medium|high|xhigh|max, prompt caching on unless disabled.
		// Ignored by the OpenAI provider.
		"thinking":              "",
		"effort":                "",
		"prompt_cache_disabled": false,
		// Opt-in switch for Sentry's AgentGuard interaction surface (the
		// agentguard-guide skill + all AgentGuard read/write tools).
		// DISABLED by default — turn on under Settings → Sentry Agent to
		// let Sentry interact with AgentGuard features.
		"agentguard_tools_enabled": false,
		// Opt-in switch for the data-audit-guide skill (the A01–A27
		// data-security audit playbook over agentguard_cim_event). Independent
		// of agentguard_tools_enabled; DISABLED by default.
		"data_audit_enabled": false,
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
