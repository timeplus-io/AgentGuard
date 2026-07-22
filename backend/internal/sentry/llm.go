// Package sentry implements AgentGuard's "Sentry" conversational security
// analyst agent: an LLM-driven chat loop that answers operator questions
// about monitored agents using SQL-backed tools.
package sentry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// ToolCall is a single function-call requested by (or answered for) the
// model, in either direction of the conversation.
type ToolCall struct {
	ID, Name, Args string
}

// Msg is one turn in the conversation sent to/received from the LLM.
type Msg struct {
	Role       string // "system" | "user" | "assistant" | "tool"
	Content    string
	ToolCallID string     // role=tool: which call this answers
	ToolCalls  []ToolCall // role=assistant: calls it made
}

// ToolDef describes a callable tool exposed to the model.
type ToolDef struct {
	Name, Description string
	Params            map[string]any // JSON schema
}

// ChatResult is the model's response to a single ChatStream call.
type ChatResult struct {
	Content   string
	ToolCalls []ToolCall
	TokensIn  int
	TokensOut int
}

// ChatCfg is parsed from dlp_config_resolve(shared, sentry_chat).
type ChatCfg struct {
	BaseURL            string `json:"base_url"`
	APIKey             string `json:"api_key"`
	Model              string `json:"model"`
	SystemPrompt       string `json:"system_prompt"`
	MaxTokens          int    `json:"max_tokens"`
	MaxIterations      int    `json:"max_iterations"`
	TurnTokenBudget    int    `json:"turn_token_budget"`
	SQLTimeoutMs       int    `json:"sql_timeout_ms"`
	SQLMaxRows         int    `json:"sql_max_rows"`
	ToolResultMaxBytes int    `json:"tool_result_max_bytes"`
}

// applyDefaults fills zero values and clamps operator input to sane ceilings
// (the config row is operator-editable JSON; never trust it blindly).
func (c *ChatCfg) applyDefaults() {
	def := func(v *int, d, max int) {
		if *v <= 0 {
			*v = d
		}
		if *v > max {
			*v = max
		}
	}
	def(&c.MaxTokens, 1024, 16384)
	def(&c.MaxIterations, 12, 32)
	def(&c.TurnTokenBudget, 32000, 256000)
	def(&c.SQLTimeoutMs, 30000, 55000)
	def(&c.SQLMaxRows, 1000, 5000)
	def(&c.ToolResultMaxBytes, 16384, 131072)
	if c.SystemPrompt == "" {
		c.SystemPrompt = tp.DefaultSentrySystemPrompt
	}
}

// LLM is the interface Sentry's conversation loop uses to talk to a model.
type LLM interface {
	ChatStream(ctx context.Context, cc ChatCfg, msgs []Msg, tools []ToolDef, onToken func(string)) (ChatResult, error)
}

// LoadChatCfg reads shared + sentry_chat live and folds them with the
// dlp_config_resolve UDF (same expression the DLP funnel uses).
func LoadChatCfg(tpCfg *config.TimeplusConfig) (ChatCfg, error) {
	sub := func(key string) string {
		return fmt.Sprintf("coalesce((SELECT value FROM table(agentguard_dlp_config) WHERE key='%s'), '')", key)
	}
	rows, err := tp.Query(tpCfg, fmt.Sprintf("SELECT dlp_config_resolve(%s, %s) AS cfg",
		sub(tp.DLPCfgKeyShared), sub(tp.SentryCfgKeyChat)))
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
		return cc, fmt.Errorf("sentry: no base_url in resolved config — set the shared config row")
	}
	return cc, nil
}
