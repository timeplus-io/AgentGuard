package timeplus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/timeplus/agentguard/internal/config"
)

// DLPConfigRow is one row of the agentguard_dlp_config mutable stream.
type DLPConfigRow struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

// DLPConfigKeys returns the recognised config keys (one per UDF).
func DLPConfigKeys() []string {
	return []string{
		DLPCfgKeyShared, DLPCfgKeyEntities, DLPCfgKeyEmbed, DLPCfgKeyRisk,
		DLPCfgKeyUnderstand, DLPCfgKeyRerank, DLPCfgKeySummarize,
	}
}

// IsDLPConfigKey reports whether key is a recognised config key.
func IsDLPConfigKey(key string) bool {
	for _, k := range DLPConfigKeys() {
		if k == key {
			return true
		}
	}
	return false
}

// ReadDLPConfig returns all current config rows (latest value per key).
func ReadDLPConfig(cfg *config.TimeplusConfig) ([]DLPConfigRow, error) {
	rows, err := Query(cfg, `SELECT key, value, to_string(updated_at) AS updated_at, updated_by
FROM table(agentguard_dlp_config) ORDER BY key`)
	if err != nil {
		return nil, err
	}
	out := make([]DLPConfigRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, DLPConfigRow{
			Key:       asDLPString(r["key"]),
			Value:     asDLPString(r["value"]),
			UpdatedAt: asDLPString(r["updated_at"]),
			UpdatedBy: asDLPString(r["updated_by"]),
		})
	}
	return out, nil
}

// ReadDLPConfigValue returns the current value for one key, or "" if absent.
func ReadDLPConfigValue(cfg *config.TimeplusConfig, key string) (string, error) {
	rows, err := ReadDLPConfig(cfg)
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

// WriteDLPConfig upserts one config row. The stream is a mutable stream keyed on
// `key`, so re-inserting the same key replaces the value. The native batch path
// avoids SQL-literal escaping of the JSON value (regex patterns contain
// backslashes that an INSERT … VALUES literal would mangle).
func WriteDLPConfig(cfg *config.TimeplusConfig, key, value, updatedBy string) error {
	conn, err := connectOnce(cfg)
	if err != nil {
		return fmt.Errorf("dlp config write: connect: %w", err)
	}
	defer conn.Close()
	batch, err := conn.PrepareBatch(context.Background(), "INSERT INTO agentguard_dlp_config (key, value, updated_by)")
	if err != nil {
		return fmt.Errorf("dlp config write: prepare: %w", err)
	}
	defer func() { _ = batch.Abort() }()
	if err := batch.Append(key, value, updatedBy); err != nil {
		return fmt.Errorf("dlp config write: append: %w", err)
	}
	return batch.Send()
}

// ValidateDLPConfigValue checks that value is well-formed JSON for the given
// key. For the entity catalog it structurally validates each entry. It does NOT
// compile regex patterns: the UDF runs Python `re` (with lookbehind/lookahead),
// which Go's RE2 cannot faithfully compile, so a Go compile check would falsely
// reject valid patterns. A malformed pattern is skipped by the fail-open
// extractor at runtime (it simply never matches).
//
// For the LLM UDF keys a present `prompt` must be a string (#197). Placeholder
// presence is NOT enforced: a missing placeholder just means that data isn't
// sent, and parse failures fail open — the load-bearing parts are documented,
// not blocked.
func ValidateDLPConfigValue(key, value string) error {
	var generic any
	if err := json.Unmarshal([]byte(value), &generic); err != nil {
		return fmt.Errorf("value is not valid JSON: %w", err)
	}
	switch key {
	case DLPCfgKeyRisk, DLPCfgKeyUnderstand, DLPCfgKeyRerank, DLPCfgKeySummarize:
		var row struct {
			Prompt any `json:"prompt"`
		}
		if err := json.Unmarshal([]byte(value), &row); err != nil {
			return fmt.Errorf("%s config: %w", key, err)
		}
		if row.Prompt != nil {
			if _, ok := row.Prompt.(string); !ok {
				return fmt.Errorf("%s: prompt must be a string", key)
			}
		}
	}
	if key != DLPCfgKeyEntities {
		return nil
	}
	var cat struct {
		Entities []dlpEntitySpec `json:"entities"`
	}
	if err := json.Unmarshal([]byte(value), &cat); err != nil {
		return fmt.Errorf("entities config: %w", err)
	}
	for i, e := range cat.Entities {
		if e.Type == "" {
			return fmt.Errorf("entities[%d]: type is required", i)
		}
		switch e.Kind {
		case "", "regex":
			if e.Pattern == "" {
				return fmt.Errorf("entities[%d] (%s): regex kind needs a non-empty pattern", i, e.Type)
			}
		case "dictionary":
			if len(e.Terms) == 0 {
				return fmt.Errorf("entities[%d] (%s): dictionary kind needs terms", i, e.Type)
			}
		default:
			return fmt.Errorf("entities[%d] (%s): kind must be regex or dictionary", i, e.Type)
		}
		if e.Store != "" && e.Store != "raw" && e.Store != "masked" {
			return fmt.Errorf("entities[%d] (%s): store must be raw or masked", i, e.Type)
		}
	}
	return nil
}

func asDLPString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
