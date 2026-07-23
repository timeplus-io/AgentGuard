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
