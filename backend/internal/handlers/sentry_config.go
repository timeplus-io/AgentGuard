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
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	ChatModel      string `json:"chat_model"`
	EmbeddingModel string `json:"embedding_model"`
	EmbeddingDim   int    `json:"embedding_dim"`
}

// resolvedSentryTestCreds is the fully-resolved candidate a test-connection
// probe runs against.
type resolvedSentryTestCreds struct {
	BaseURL        string
	APIKey         string
	ChatModel      string
	EmbeddingModel string
	EmbeddingDim   int
}

// storedSentryChatRow is the subset of the `chat` row's JSON this resolution
// cares about. The row is now a flat, self-contained endpoint — no "models"
// catalog.
type storedSentryChatRow struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

// resolveSentryTestCreds resolves the effective base_url/api_key/chat_model/
// embedding_model for a test-connection probe. Precedence per field: request
// body (non-blank) → boot cfg.Sentry defaults (non-blank) → the
// runtime-stored `chat` row of agentguard_sentry_config (non-blank) for the
// chat endpoint fields.
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
//
// NOTE: embedding resolution here still only falls back to boot config, not
// the `memory` row — per-endpoint resolution (including a memory-row
// fallback and the memory-enabled gate) is reworked in the config-reshape
// plan's next task.
func resolveSentryTestCreds(body sentryTestBody, boot config.SentryConfig, storedChatJSON string) resolvedSentryTestCreds {
	var stored storedSentryChatRow
	if storedChatJSON != "" {
		_ = json.Unmarshal([]byte(storedChatJSON), &stored)
	}
	dim := body.EmbeddingDim
	if dim == 0 {
		dim = boot.EmbeddingDim
	}
	return resolvedSentryTestCreds{
		BaseURL:        orStr(body.BaseURL, orStr(boot.ChatBaseURL, stored.BaseURL)),
		APIKey:         orStr(body.APIKey, orStr(boot.ChatAPIKey, stored.APIKey)),
		ChatModel:      orStr(body.ChatModel, orStr(boot.ChatModel, stored.Model)),
		EmbeddingModel: orStr(body.EmbeddingModel, boot.EmbeddingModel),
		EmbeddingDim:   dim,
	}
}

// TestSentryConfig handles POST /api/sentry/config/test — validates a
// candidate Sentry model endpoint: reachability, a chat completion WITH a
// tool defined (asserting the model actually returns tool_calls — Sentry's
// chat loop is useless without tool calling), and a real embedding call.
// Resolves the candidate endpoint from the posted body, falling back to the
// live cfg.Sentry boot defaults and then the runtime-stored `chat` row (see
// resolveSentryTestCreds). Always HTTP 200; failures reported in-band.
func TestSentryConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body sentryTestBody
		_ = c.ShouldBindJSON(&body)

		config.Mu.RLock()
		s := cfg.Sentry
		config.Mu.RUnlock()

		// Only hit Timeplus for the stored `chat` row if body+boot leave
		// something we'd otherwise need it for — avoids an extra query for
		// deployments that are fully configured via body/config.yaml.
		needsStored := (body.APIKey == "" && s.ChatAPIKey == "") ||
			(body.BaseURL == "" && s.ChatBaseURL == "") ||
			(body.ChatModel == "" && s.ChatModel == "") ||
			(body.EmbeddingModel == "" && s.EmbeddingModel == "")
		var storedChat string
		if needsStored {
			rc := auth.RequestCfg(c, cfg)
			if v, err := tp.ReadSentryConfigValue(&rc.Timeplus, tp.SentryCfgKeyChat); err != nil {
				log.Printf("[sentry] TestSentryConfig: stored chat-row fallback read failed: %v", err)
			} else {
				storedChat = v
			}
		}

		creds := resolveSentryTestCreds(body, s, storedChat)
		baseURL := strings.TrimRight(creds.BaseURL, "/")
		key := creds.APIKey
		chatModel := creds.ChatModel
		embModel := creds.EmbeddingModel
		expectedDim := creds.EmbeddingDim
		maxTokens := s.MaxOutputTokens
		if maxTokens <= 0 {
			maxTokens = 1024
		}

		if _, err := url.ParseRequestURI(baseURL + "/models"); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "checks": gin.H{
				"reachable": gin.H{"ok": false, "error": "invalid base_url"}}})
			return
		}
		client := &http.Client{Timeout: 10 * time.Second}

		reach := dlpCheckReachable(c, client, baseURL, key)
		checks := gin.H{"reachable": reach}
		ok := reach["ok"] == true
		if ok {
			chat := sentryCheckChatToolCalling(c, client, baseURL, key, chatModel, maxTokens)
			checks["chat"] = chat
			if chat["ok"] != true {
				ok = false
			}

			emb := dlpCheckEmbedding(c, client, baseURL, key, embModel, expectedDim)
			checks["embedding"] = emb
			if emb["ok"] != true {
				ok = false
			}
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
