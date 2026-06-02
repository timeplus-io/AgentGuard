package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
)

// AuthRequired is a Gin middleware that requires a valid agentguard_session cookie.
// On success it injects "timeplus_user" and "timeplus_password" into the context.
func AuthRequired(store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("agentguard_session")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
			return
		}
		sess, ok := store.Get(cookie)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "session expired"})
			return
		}
		c.Set("timeplus_user", sess.User)
		c.Set("timeplus_password", sess.Password)
		c.Next()
	}
}

// HasValidSession reports whether the request carries a valid session cookie.
// Used by endpoints whose auth requirement depends on runtime phase
// (e.g. /api/supervisor/retry: open during setup, protected afterward).
func HasValidSession(c *gin.Context, store *SessionStore) bool {
	cookie, err := c.Cookie("agentguard_session")
	if err != nil {
		return false
	}
	_, ok := store.Get(cookie)
	return ok
}

// RequestCfg returns a copy of cfg with Timeplus credentials populated from
// the current request's session context. Call this at the top of every
// handler closure that queries Timeplus.
func RequestCfg(c *gin.Context, cfg *config.Config) *config.Config {
	out := *cfg
	if user, exists := c.Get("timeplus_user"); exists {
		if s, ok := user.(string); ok {
			out.Timeplus.User = s
		}
	}
	if pass, exists := c.Get("timeplus_password"); exists {
		if s, ok := pass.(string); ok {
			out.Timeplus.Password = s
		}
	}
	return &out
}
