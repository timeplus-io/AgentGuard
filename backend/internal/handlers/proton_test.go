package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
)

// TestProtonQueryHandlerCancelBeforeFirstRowIsSilent proves that a client
// cancelling the request after Timeplus responded 200 but before the first
// row is streamed does not get misclassified as a 502 "cannot reach Proton"
// error. Gin's c.Writer.Written() stays false until bytes are written (a
// bare c.Status(http.StatusOK) doesn't count), so context cancellation must
// be checked before the !Written() branch, not after.
func TestProtonQueryHandlerCancelBeforeFirstRowIsSilent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tpCfg := mockTimeplus(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until the client cancels, writing no rows.
		<-r.Context().Done()
	})
	cfg := &config.Config{Timeplus: tpCfg}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/proton/query",
		strings.NewReader(`{"sql":"SELECT 1"}`)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	ProtonQueryHandler(cfg)(c)

	body := w.Body.String()
	if strings.Contains(body, "cannot reach Proton") {
		t.Fatalf("body must not contain a 502 cannot-reach-Proton error on cancellation, got: %s", body)
	}
	if body != "" {
		t.Fatalf("body should be empty on silent cancellation, got: %q", body)
	}
}
