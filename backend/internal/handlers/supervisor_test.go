package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/handlers"
	"github.com/timeplus/agentguard/internal/supervisor"
)

func init() { gin.SetMode(gin.TestMode) }

type fakeCtrl struct {
	status   supervisor.Status
	retryErr error
	retried  bool
}

func (f *fakeCtrl) Status() supervisor.Status { return f.status }
func (f *fakeCtrl) Retry() error              { f.retried = true; return f.retryErr }

func TestSupervisorStatus_ReturnsState(t *testing.T) {
	ctrl := &fakeCtrl{status: supervisor.Status{State: supervisor.StateProvisioning}}
	r := gin.New()
	r.GET("/api/supervisor/status", handlers.SupervisorStatus(ctrl))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/supervisor/status", nil))
	if w.Code != 200 {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var body map[string]any
	_ = json.NewDecoder(w.Body).Decode(&body)
	if body["state"] != "provisioning" {
		t.Errorf("state: want provisioning, got %v", body["state"])
	}
}

func TestSupervisorRetry_OpenDuringSetup(t *testing.T) {
	ctrl := &fakeCtrl{}
	cfg := &config.Config{SetupComplete: false}
	store := auth.NewSessionStore()
	r := gin.New()
	r.POST("/api/supervisor/retry", handlers.SupervisorRetry(ctrl, cfg, store))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/supervisor/retry", nil))
	if w.Code != 202 {
		t.Fatalf("want 202 during setup, got %d", w.Code)
	}
	if !ctrl.retried {
		t.Error("retry should have been invoked")
	}
}

func TestSupervisorRetry_ConflictWhenRetryErrors(t *testing.T) {
	ctrl := &fakeCtrl{retryErr: errors.New("retry is only valid from a failed state")}
	cfg := &config.Config{SetupComplete: false} // open phase so we reach Retry()
	store := auth.NewSessionStore()
	r := gin.New()
	r.POST("/api/supervisor/retry", handlers.SupervisorRetry(ctrl, cfg, store))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/supervisor/retry", nil))
	if w.Code != 409 {
		t.Fatalf("want 409 when Retry errors, got %d", w.Code)
	}
	if !ctrl.retried {
		t.Error("Retry should have been invoked")
	}
}

func TestSupervisorRetry_ProtectedAfterSetup(t *testing.T) {
	ctrl := &fakeCtrl{}
	cfg := &config.Config{SetupComplete: true}
	store := auth.NewSessionStore()
	r := gin.New()
	r.POST("/api/supervisor/retry", handlers.SupervisorRetry(ctrl, cfg, store))

	// No cookie -> 401
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/supervisor/retry", nil))
	if w.Code != 401 {
		t.Fatalf("want 401 after setup w/o session, got %d", w.Code)
	}
	if ctrl.retried {
		t.Error("retry must not run without auth after setup")
	}

	// Valid cookie -> 202
	sess := store.Create("u", "p")
	w2 := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/supervisor/retry", nil)
	req.AddCookie(&http.Cookie{Name: "agentguard_session", Value: sess.ID})
	r.ServeHTTP(w2, req)
	if w2.Code != 202 {
		t.Fatalf("want 202 with session, got %d", w2.Code)
	}
}
