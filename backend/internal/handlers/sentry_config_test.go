package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
)

// TestResolveSentryChatCreds_Precedence asserts the chat-endpoint resolver's
// precedence: request body (non-blank) → boot cfg.Sentry defaults (non-blank)
// → the runtime-stored `chat` row of agentguard_sentry_config.
func TestResolveSentryChatCreds_Precedence(t *testing.T) {
	// body → boot → stored chat row.
	got := resolveSentryChatCreds(
		sentryTestBody{}, // empty body
		config.SentryConfig{ChatBaseURL: "", ChatAPIKey: "", ChatModel: ""},
		`{"base_url":"http://stored/v1","api_key":"stored-key","model":"stored-model"}`,
	)
	if got.BaseURL != "http://stored/v1" || got.APIKey != "stored-key" || got.Model != "stored-model" {
		t.Fatalf("stored-row fallback failed: %+v", got)
	}
	got2 := resolveSentryChatCreds(
		sentryTestBody{BaseURL: "http://body/v1"},
		config.SentryConfig{ChatBaseURL: "http://boot/v1"},
		`{"base_url":"http://stored/v1"}`,
	)
	if got2.BaseURL != "http://body/v1" {
		t.Fatalf("body should win: %+v", got2)
	}
}

// TestResolveSentryEmbedCreds_Precedence asserts the embedding-endpoint
// resolver's precedence: body → boot → the runtime-stored `memory` row, with
// per-field fallback (a field left blank at one tier falls through to the
// next tier independently of its siblings).
func TestResolveSentryEmbedCreds_Precedence(t *testing.T) {
	got := resolveSentryEmbedCreds(
		sentryTestBody{},
		config.SentryConfig{EmbeddingBaseURL: "http://boot-emb/v1", EmbeddingModel: "boot-emb"},
		`{"base_url":"http://stored-emb/v1","api_key":"emb-key","model":"stored-emb","embedding_dim":768}`,
	)
	// boot wins over stored for base_url/model here; stored fills api_key/dim.
	if got.BaseURL != "http://boot-emb/v1" || got.Model != "boot-emb" ||
		got.APIKey != "emb-key" || got.EmbeddingDim != 768 {
		t.Fatalf("embed resolve wrong: %+v", got)
	}
}

// TestMergeSentryValue_PreservesAPIKeyOnBlank is the regression test for the
// data-loss bug: PUTting the `chat` row with a blank/absent api_key (the
// Settings editor never echoes a redacted key back) must retain the stored
// key, mirroring UpsertDLPConfig's preserveAPIKey behavior.
func TestMergeSentryValue_PreservesAPIKeyOnBlank(t *testing.T) {
	m := map[string]any{"base_url": "http://new/v1", "api_key": "", "model": "gpt-x"}
	existing := map[string]any{"base_url": "http://old/v1", "api_key": "kept", "model": "gpt-old"}

	fillAPIKey(m, existing)

	if m["api_key"] != "kept" {
		t.Fatalf("api_key = %v, want %q (preserved)", m["api_key"], "kept")
	}
	if m["base_url"] != "http://new/v1" {
		t.Fatalf("base_url = %v, want new value untouched", m["base_url"])
	}
}

// TestUpsertSentryConfig_RejectsUnknownKey exercises the IsSentryConfigKey
// guard: an unrecognized :key must 400 before any DB access is attempted (no
// Timeplus is reachable in this test).
func TestUpsertSentryConfig_RejectsUnknownKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}

	body := []byte(`{"value":{"base_url":"http://x"}}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/sentry/config/bogus", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "key", Value: "bogus"}}

	UpsertSentryConfig(cfg)(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
}

// TestUpsertSentryConfig_RejectsBadJSON exercises the ShouldBindJSON guard: a
// malformed request body must 400 before any DB access is attempted, even for
// a recognized key.
func TestUpsertSentryConfig_RejectsBadJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}

	body := []byte(`{"value": not-json}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/sentry/config/chat", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "key", Value: "chat"}}

	UpsertSentryConfig(cfg)(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
}

// TestUpsertSentryConfig_RejectsMissingValue covers the "value is required"
// branch (nil body.Value) distinct from malformed JSON.
func TestUpsertSentryConfig_RejectsMissingValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}

	body := []byte(`{}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/sentry/config/chat", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "key", Value: "chat"}}

	UpsertSentryConfig(cfg)(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
}
