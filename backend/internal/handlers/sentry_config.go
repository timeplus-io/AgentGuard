package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// GetSentryConfig handles GET /api/sentry/config — returns the live Sentry
// config rows (chat/memory) with api keys redacted. Reachable regardless of
// dlp.enabled: Sentry's config stream is independent of DLP's.
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

// dbReadError wraps an error from a Timeplus config read, signaling that it's
// an infrastructure failure (503) not a client error (400).
type dbReadError struct {
	err error
}

func (e *dbReadError) Error() string {
	return e.err.Error()
}

// mergeSentryValue marshals the posted value and, for the `chat`/`memory`
// rows, preserves the stored top-level api_key when the incoming value
// leaves it blank/absent — mirroring how UpsertDLPConfig's preserveAPIKey
// keeps a GET→edit→PUT round-trip from wiping a stored key, reusing the same
// fillAPIKey primitive. Each row is now a flat, self-contained endpoint (no
// "models" catalog), so there is nothing nested left to preserve.
func mergeSentryValue(cfg *config.Config, c *gin.Context, key string, bodyValue any) (string, error) {
	b, err := json.Marshal(bodyValue)
	if err != nil {
		return "", fmt.Errorf("invalid value: %w", err)
	}
	if key != tp.SentryCfgKeyChat && key != tp.SentryCfgKeyMemory {
		return string(b), nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return string(b), nil // non-object value: nothing to preserve
	}

	rc := auth.RequestCfg(c, cfg)
	existing, err := tp.ReadSentryConfigValue(&rc.Timeplus, key)
	if err != nil {
		return "", &dbReadError{err} // wrap to signal infrastructure error → 503
	}
	if existing == "" {
		return string(b), nil
	}
	var em map[string]any
	if err := json.Unmarshal([]byte(existing), &em); err != nil {
		return string(b), nil
	}

	fillAPIKey(m, em)
	out, err := json.Marshal(m)
	if err != nil {
		return string(b), nil
	}
	return string(out), nil
}

// UpsertSentryConfig handles PUT /api/sentry/config/:key — upserts one Sentry
// config row. Unknown keys and malformed bodies 400 before any write.
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
			// Distinguish infra failures (DB read) from client errors (bad input/validation)
			var dbe *dbReadError
			if errors.As(err, &dbe) {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			} else {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			}
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

// sentryTestBody is the request body for POST /api/sentry/config/test.
type sentryTestBody struct {
	BaseURL          string `json:"base_url"`
	APIKey           string `json:"api_key"`
	ChatModel        string `json:"chat_model"`
	EmbeddingBaseURL string `json:"embedding_base_url"`
	EmbeddingAPIKey  string `json:"embedding_api_key"`
	EmbeddingModel   string `json:"embedding_model"`
	EmbeddingDim     int    `json:"embedding_dim"`
	// MemoryEnabled overrides whether the embedding endpoint is probed at
	// all. Pointer so "absent" (use stored/boot state) is distinguishable
	// from an explicit false.
	MemoryEnabled *bool `json:"memory_enabled"`
}

// resolvedChatCreds is the fully-resolved chat-endpoint candidate a
// test-connection probe runs against.
type resolvedChatCreds struct{ BaseURL, APIKey, Model string }

// resolvedEmbedCreds is the fully-resolved embedding-endpoint candidate a
// test-connection probe runs against.
type resolvedEmbedCreds struct {
	BaseURL, APIKey, Model string
	EmbeddingDim           int
}

// resolveSentryChatCreds resolves the effective base_url/api_key/model for
// the chat endpoint. Precedence per field: request body (non-blank) → boot
// cfg.Sentry defaults (non-blank) → the runtime-stored `chat` row of
// agentguard_sentry_config (non-blank).
//
// The stored-row fallback exists because the Settings editor never echoes
// the api_key back (it's redacted in GetSentryConfig), so testConnection()
// always posts api_key:"". And UpsertSentryConfig never writes back to
// config.yaml, so after a restart cfg.Sentry.ChatAPIKey is empty whenever
// Sentry was configured entirely through the runtime editor — the ONLY
// durable copy of the key is the `chat` row. Without this fallback, "Test
// connection" would falsely fail against any auth-requiring endpoint in that
// (common) setup. storedChatJSON may be "" (row absent, or the caller chose
// not to fetch it) — resolution degrades to body → boot in that case.
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

// resolveSentryEmbedCreds resolves the effective base_url/api_key/model/
// embedding_dim for the memory (embedding) endpoint. Same precedence as
// resolveSentryChatCreds, but reading the `memory` row instead of `chat` —
// each endpoint is now a fully independent, self-contained row.
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

// TestSentryConfig handles POST /api/sentry/config/test — validates the
// candidate Sentry model endpoints. The chat endpoint (reachability + a chat
// completion WITH a tool defined, asserting the model actually returns
// tool_calls — Sentry's chat loop is useless without tool calling) is always
// probed. The embedding endpoint is probed only when memory is enabled
// (body override → stored `memory` row → boot cfg.Sentry.MemoryEnabled);
// when disabled, checks.embedding reports {"skipped": true} and is excluded
// from the ok roll-up. Always HTTP 200; failures reported in-band.
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

		maxTokens := s.MaxOutputTokens
		if maxTokens <= 0 {
			maxTokens = 1024
		}
		chatBase := strings.TrimRight(chat.BaseURL, "/")
		if _, err := url.ParseRequestURI(chatBase + "/models"); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "checks": gin.H{
				"reachable": gin.H{"ok": false, "error": "invalid base_url"}}})
			return
		}

		client := &http.Client{Timeout: 10 * time.Second}
		checks := gin.H{}
		reach := dlpCheckReachable(c, client, chatBase, chat.APIKey)
		checks["reachable"] = reach
		ok := reach["ok"] == true
		if ok {
			chatCheck := sentryCheckChatToolCalling(c, client, chatBase, chat.APIKey, chat.Model, maxTokens)
			checks["chat"] = chatCheck
			ok = chatCheck["ok"] == true
		}

		// Embedding: only when memory is enabled (body override → stored
		// memory row → boot default).
		storedMem, err := tp.ReadSentryConfigValue(&rc.Timeplus, tp.SentryCfgKeyMemory)
		if err != nil {
			log.Printf("[sentry] TestSentryConfig: read memory row: %v", err)
		}
		memEnabled := s.MemoryEnabled
		if body.MemoryEnabled != nil {
			memEnabled = *body.MemoryEnabled
		} else if storedMem != "" {
			var m struct {
				Enabled bool `json:"enabled"`
			}
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

// sentryCheckChatToolCalling posts a chat completion that defines a trivial
// tool and forces the model to use it (tool_choice=required), asserting the
// response actually carries tool_calls — the property Sentry's chat loop
// depends on, distinct from plain chat capability.
func sentryCheckChatToolCalling(c *gin.Context, client *http.Client, baseURL, key, model string, maxTokens int) gin.H {
	tool := gin.H{
		"type": "function",
		"function": gin.H{
			"name":        "get_current_time",
			"description": "Returns the current UTC time. Call this to answer the user's question.",
			"parameters": gin.H{
				"type":       "object",
				"properties": gin.H{},
			},
		},
	}
	payload, _ := json.Marshal(gin.H{
		"model":       model,
		"messages":    []gin.H{{"role": "user", "content": "What is the current time? You must call the get_current_time tool to answer."}},
		"tools":       []gin.H{tool},
		"tool_choice": "required",
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
		return gin.H{"ok": false, "tool_calling": false, "error": err.Error()}
	}
	defer resp.Body.Close()
	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil || len(parsed.Choices) == 0 {
		return gin.H{"ok": false, "tool_calling": false, "error": "no chat response"}
	}
	toolCalls := parsed.Choices[0].Message.ToolCalls
	if len(toolCalls) == 0 {
		return gin.H{"ok": false, "tool_calling": false, "error": "model did not return tool_calls"}
	}
	return gin.H{"ok": true, "tool_calling": true, "tool_call_name": toolCalls[0].Function.Name}
}
