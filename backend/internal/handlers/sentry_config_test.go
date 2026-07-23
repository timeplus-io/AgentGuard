package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
)

// TestResolveSentryTestCreds_BodyWins asserts a non-blank request body field
// always wins over boot config and the stored shared row.
func TestResolveSentryTestCreds_BodyWins(t *testing.T) {
	body := sentryTestBody{
		BaseURL: "https://body.example/v1", APIKey: "body-key",
		ChatModel: "body-chat", EmbeddingModel: "body-embed", EmbeddingDim: 42,
	}
	boot := config.SentryConfig{
		BaseURL: "https://boot.example/v1", APIKey: "boot-key",
		ChatModel: "boot-chat", EmbeddingModel: "boot-embed", EmbeddingDim: 99,
	}
	stored := `{"base_url":"https://stored.example/v1","api_key":"stored-key",
		"models":{"chat":{"model":"stored-chat"},"embedding":{"model":"stored-embed"}}}`

	got := resolveSentryTestCreds(body, boot, stored)

	want := resolvedSentryTestCreds{
		BaseURL: "https://body.example/v1", APIKey: "body-key",
		ChatModel: "body-chat", EmbeddingModel: "body-embed", EmbeddingDim: 42,
	}
	if got != want {
		t.Fatalf("resolveSentryTestCreds() = %+v, want %+v", got, want)
	}
}

// TestResolveSentryTestCreds_BootFallback asserts a blank body field falls
// back to the boot cfg.Sentry defaults when the body doesn't supply it.
func TestResolveSentryTestCreds_BootFallback(t *testing.T) {
	body := sentryTestBody{}
	boot := config.SentryConfig{
		BaseURL: "https://boot.example/v1", APIKey: "boot-key",
		ChatModel: "boot-chat", EmbeddingModel: "boot-embed", EmbeddingDim: 99,
	}
	stored := `{"base_url":"https://stored.example/v1","api_key":"stored-key",
		"models":{"chat":{"model":"stored-chat"},"embedding":{"model":"stored-embed"}}}`

	got := resolveSentryTestCreds(body, boot, stored)

	want := resolvedSentryTestCreds{
		BaseURL: "https://boot.example/v1", APIKey: "boot-key",
		ChatModel: "boot-chat", EmbeddingModel: "boot-embed", EmbeddingDim: 99,
	}
	if got != want {
		t.Fatalf("resolveSentryTestCreds() = %+v, want %+v", got, want)
	}
}

// TestResolveSentryTestCreds_StoredRowFallback is the regression test for the
// bug: when both body and boot config leave api_key (and base_url/models)
// blank — the state after "configured entirely via the runtime Settings
// editor, then server restarted" — resolution must fall back to the
// runtime-stored `shared` row of agentguard_sentry_config rather than
// resolving to "" and false-failing the probe.
func TestResolveSentryTestCreds_StoredRowFallback(t *testing.T) {
	body := sentryTestBody{}
	boot := config.SentryConfig{} // nothing durable after restart
	stored := `{"base_url":"https://stored.example/v1","api_key":"stored-key",
		"models":{"chat":{"model":"stored-chat"},"embedding":{"model":"stored-embed"}}}`

	got := resolveSentryTestCreds(body, boot, stored)

	want := resolvedSentryTestCreds{
		BaseURL: "https://stored.example/v1", APIKey: "stored-key",
		ChatModel: "stored-chat", EmbeddingModel: "stored-embed", EmbeddingDim: 0,
	}
	if got != want {
		t.Fatalf("resolveSentryTestCreds() = %+v, want %+v", got, want)
	}
}

// TestResolveSentryTestCreds_AllEmpty asserts that when body, boot, and the
// stored row are all empty/absent, resolution degrades to the zero value
// (empty base_url — the handler then 400s the "invalid base_url" check)
// rather than panicking on a missing stored row.
func TestResolveSentryTestCreds_AllEmpty(t *testing.T) {
	got := resolveSentryTestCreds(sentryTestBody{}, config.SentryConfig{}, "")

	want := resolvedSentryTestCreds{}
	if got != want {
		t.Fatalf("resolveSentryTestCreds() = %+v, want %+v", got, want)
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
