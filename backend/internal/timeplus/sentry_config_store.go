package timeplus

import (
	"context"
	"encoding/json"
	"fmt"

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
		// Single switch for ALL gated-write tools (#222). Absent == enabled;
		// seeded explicitly so operators can discover the knob. Reads are
		// always on and have no toggle.
		"write_tools_enabled": true,
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
