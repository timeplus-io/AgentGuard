package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
)

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
