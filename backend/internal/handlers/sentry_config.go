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
// config rows (shared/chat/embed) with api keys redacted. Reachable
// regardless of dlp.enabled: Sentry's config stream is independent of DLP's.
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

// mergeSentryValue marshals the posted value and, for the `shared` row,
// preserves the stored api_key (top-level and any per-model one under
// "models") when the incoming value leaves it blank/absent — mirroring how
// UpsertDLPConfig's preserveAPIKey keeps a GET→edit→PUT round-trip from
// wiping a stored key, reusing the same fillAPIKey primitive.
func mergeSentryValue(cfg *config.Config, c *gin.Context, key string, bodyValue any) (string, error) {
	b, err := json.Marshal(bodyValue)
	if err != nil {
		return "", fmt.Errorf("invalid value: %w", err)
	}
	if key != tp.SentryCfgKeyShared {
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
	if models, ok := m["models"].(map[string]any); ok {
		emodels, _ := em["models"].(map[string]any)
		for name, mv := range models {
			entry, ok := mv.(map[string]any)
			if !ok {
				continue
			}
			var eentry map[string]any
			if emodels != nil {
				eentry, _ = emodels[name].(map[string]any)
			}
			fillAPIKey(entry, eentry)
		}
	}
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

// TestSentryConfig handles POST /api/sentry/config/test — validates a
// candidate Sentry model endpoint: reachability, a chat completion WITH a
// tool defined (asserting the model actually returns tool_calls — Sentry's
// chat loop is useless without tool calling), and a real embedding call.
// Resolves the candidate endpoint from the posted body, falling back to the
// live cfg.Sentry boot defaults. Always HTTP 200; failures reported in-band.
func TestSentryConfig(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			BaseURL        string `json:"base_url"`
			APIKey         string `json:"api_key"`
			ChatModel      string `json:"chat_model"`
			EmbeddingModel string `json:"embedding_model"`
			EmbeddingDim   int    `json:"embedding_dim"`
		}
		_ = c.ShouldBindJSON(&body)

		config.Mu.RLock()
		s := cfg.Sentry
		config.Mu.RUnlock()

		baseURL := strings.TrimRight(orStr(body.BaseURL, s.BaseURL), "/")
		key := orStr(body.APIKey, s.APIKey)
		chatModel := orStr(body.ChatModel, s.ChatModel)
		embModel := orStr(body.EmbeddingModel, s.EmbeddingModel)
		expectedDim := body.EmbeddingDim
		if expectedDim == 0 {
			expectedDim = s.EmbeddingDim
		}
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
