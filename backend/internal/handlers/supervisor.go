package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/supervisor"
)

// SupervisorController is the slice of *supervisor.Supervisor the handlers need
// (kept as an interface so tests can supply a fake).
type SupervisorController interface {
	Status() supervisor.Status
	Retry() error
}

// SupervisorStatus returns the supervisor state. Unauthenticated but the
// underlying Status carries only redacted detail (spec §5.6/§5.7).
func SupervisorStatus(ctrl SupervisorController) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, ctrl.Status())
	}
}

// SupervisorRetry re-runs a failed boot transition. Auth is phase-based: open
// during setup (so a first-boot failure is recoverable before any session
// exists), session-protected once setup.complete (spec §5.7).
func SupervisorRetry(ctrl SupervisorController, cfg *config.Config, store *auth.SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		config.Mu.RLock()
		complete := cfg.SetupComplete
		config.Mu.RUnlock()

		if complete && !auth.HasValidSession(c, store) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
			return
		}
		if err := ctrl.Retry(); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "retrying"})
	}
}
