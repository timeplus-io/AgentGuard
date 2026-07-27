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
	ToolCallID string          // role=tool: which call this answers
	ToolCalls  []ToolCall      // role=assistant: calls it made
	Thinking   []ThinkingBlock // role=assistant: reasoning blocks (anthropic only)
}

// ToolDef describes a callable tool exposed to the model.
type ToolDef struct {
	Name, Description string
	Params            map[string]any // JSON schema
}

// ThinkingBlock is one reasoning block from an Anthropic adaptive-thinking
// response: the (possibly empty) summarized text plus the opaque signature.
// Blocks are carried in-memory through the turn's tool-use loop and echoed
// back verbatim on the continuation request — the Anthropic API requires
// an assistant tool_use turn to retain its thinking blocks. They are NOT
// persisted across turns (dropping completed turns' thinking is standard
// context-trimming behavior). The OpenAI provider ignores them.
type ThinkingBlock struct {
	Text      string
	Signature string
}

// ChatResult is the model's response to a single ChatStream call.
type ChatResult struct {
	Content   string
	ToolCalls []ToolCall
	Thinking  []ThinkingBlock // anthropic adaptive thinking (empty otherwise)
	TokensIn  int
	TokensOut int
	// Prompt-cache usage (anthropic only; zero elsewhere). Read = tokens
	// served from cache, Write = tokens written to cache this call.
	CacheReadTokens  int
	CacheWriteTokens int
}

// ChatCfg is parsed directly from the self-contained `chat` row of
// agentguard_sentry_config (own base_url/api_key/model + budgets).
type ChatCfg struct {
	BaseURL            string `json:"base_url"`
	APIKey             string `json:"api_key"`
	Model              string `json:"model"`
	APIType            string `json:"api_type"`
	SystemPrompt       string `json:"-"`
	MaxTokens          int    `json:"max_tokens"`
	MaxIterations      int    `json:"max_iterations"`
	TurnTokenBudget    int    `json:"turn_token_budget"`
	SQLTimeoutMs       int    `json:"sql_timeout_ms"`
	SQLMaxRows         int    `json:"sql_max_rows"`
	ToolResultMaxBytes int    `json:"tool_result_max_bytes"`
	// ApprovalTimeoutMs bounds how long a GatedWrite proposal waits for a
	// human Approve/Deny before auto-denying (#221). Default 5 minutes.
	ApprovalTimeoutMs int `json:"approval_timeout_ms"`
	// Thinking selects the Anthropic reasoning mode: "" leaves the request's
	// thinking parameter unset (the provider's default — off on Opus 4.7/4.8,
	// adaptive on Sonnet 5 / Fable 5), "adaptive" explicitly enables adaptive
	// thinking with summarized display (streamed to the chat UI as reasoning).
	// Ignored by the OpenAI provider.
	Thinking string `json:"thinking"`
	// Effort sets output_config.effort on Anthropic requests: "" (provider
	// default) or low/medium/high/xhigh/max. Ignored by the OpenAI provider.
	Effort string `json:"effort"`
	// PromptCacheDisabled turns OFF Anthropic prompt caching (cache_control
	// on the system block). Caching is on by default: the system prompt +
	// schema card is resent on every loop iteration, so breakpointing it
	// pays for itself within a single multi-tool turn.
	PromptCacheDisabled bool `json:"prompt_cache_disabled"`
	// AgentGuardToolsEnabled is the single opt-in switch for Sentry's
	// AgentGuard interaction surface: the agentguard-guide knowledge skill
	// plus every AgentGuard-specific tool, read (get_threats, get_holds,
	// get_rule_status, get_cost_summary, get_session_timeline,
	// get_rule_catalog, dlp_search) and write (acknowledge_threat,
	// clear_threat, decide_hold — still individually approval-gated).
	// DISABLED by default (absent key = false): out of the box Sentry is a
	// generic Timeplus SQL analyst (knowledge skills, schema introspection,
	// run_sql only); operators turn this on to let Sentry interact with
	// AgentGuard features.
	AgentGuardToolsEnabled bool `json:"agentguard_tools_enabled"`
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
	def(&c.ApprovalTimeoutMs, 300000, 1800000)
	if c.APIType == "" {
		c.APIType = "openai"
	}
	if c.Thinking != "adaptive" {
		c.Thinking = "" // only ""/adaptive are valid; never send "disabled" (400 on Fable 5)
	}
	switch c.Effort {
	case "low", "medium", "high", "xhigh", "max":
	default:
		c.Effort = ""
	}
	if c.SystemPrompt == "" {
		c.SystemPrompt = tp.DefaultSentrySystemPrompt
	}
}

// LLM is the interface Sentry's conversation loop uses to talk to a model.
// onDelta streams incremental output: kind is "token" for answer text and
// "thinking" for summarized reasoning (anthropic adaptive thinking only).
type LLM interface {
	ChatStream(ctx context.Context, cc ChatCfg, msgs []Msg, tools []ToolDef, onDelta func(kind, text string)) (ChatResult, error)
}

// NewLLM returns the LLM implementation for the configured api_type.
// Unknown/empty api_type defaults to the OpenAI-compatible client.
func NewLLM(cc ChatCfg) LLM {
	if cc.APIType == "anthropic" {
		return anthropicLLM{}
	}
	return openaiLLM{}
}

// chatCfgResolveSQL reads the self-contained `chat` row directly — the row is
// a concrete endpoint (base_url/api_key/model/budgets), so no
// dlp_config_resolve fold/catalog resolution is needed (unlike DLP).
func chatCfgResolveSQL() string {
	return fmt.Sprintf(
		"SELECT coalesce((SELECT value FROM table(agentguard_sentry_config) WHERE key='%s'), '') AS cfg",
		tp.SentryCfgKeyChat)
}

// LoadChatCfg reads the `chat` row of agentguard_sentry_config live.
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
