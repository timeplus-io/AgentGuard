package sentry

import (
	"encoding/json"

	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// MemoryCfg is the turn-time summary of the `memory` row of
// agentguard_sentry_config, used by SelfContext (#233) so Sentry can answer
// questions about its own memory configuration. Deliberately excludes
// base_url and api_key — nothing endpoint-shaped enters the prompt.
type MemoryCfg struct {
	Configured   bool // an embedding model is set
	Enabled      bool
	Model        string
	EmbeddingDim int
}

// LoadMemoryCfg reads and parses the memory row. Fail-open: any read/parse
// failure yields the zero value ("not configured") rather than an error —
// self-context must never block a turn.
func LoadMemoryCfg(tpCfg *config.TimeplusConfig) MemoryCfg {
	v, err := tp.ReadSentryConfigValue(tpCfg, tp.SentryCfgKeyMemory)
	if err != nil || v == "" {
		return MemoryCfg{}
	}
	var m struct {
		Enabled      bool   `json:"enabled"`
		Model        string `json:"model"`
		EmbeddingDim int    `json:"embedding_dim"`
	}
	if json.Unmarshal([]byte(v), &m) != nil {
		return MemoryCfg{}
	}
	return MemoryCfg{
		Configured:   m.Model != "",
		Enabled:      m.Enabled,
		Model:        m.Model,
		EmbeddingDim: m.EmbeddingDim,
	}
}
