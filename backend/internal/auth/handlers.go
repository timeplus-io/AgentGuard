package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// AuthStatus returns the current setup and login state.
// This is the first call the frontend makes on load.
func AuthStatus(cfg *config.Config, store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := gin.H{
			"setup_complete": cfg.SetupComplete,
			"logged_in":      false,
			"username":       "",
		}
		if cookie, err := c.Cookie("agentguard_session"); err == nil {
			if sess, ok := store.Get(cookie); ok {
				status["logged_in"] = true
				status["username"] = sess.User
			}
		}
		c.JSON(http.StatusOK, status)
	}
}

// Login validates Timeplus credentials and creates a session.
// Body: {"username": "...", "password": "..."}
func Login(cfg *config.Config, store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Username string `json:"username" binding:"required"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "username is required"})
			return
		}

		// Validate by running a lightweight query against Timeplus with these creds.
		testCfg := config.TimeplusConfig{
			Host:     cfg.Timeplus.Host,
			Port:     cfg.Timeplus.Port,
			User:     body.Username,
			Password: body.Password,
		}
		if _, err := tp.Query(&testCfg, "SELECT 1 AS ok"); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}

		sess := store.Create(body.Username, body.Password)
		// httpOnly=true keeps the cookie out of JavaScript; SameSite=Strict prevents CSRF.
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     "agentguard_session",
			Value:    sess.ID,
			MaxAge:   int(SessionMaxAge.Seconds()),
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		})
		c.JSON(http.StatusOK, gin.H{"username": body.Username})
	}
}

// Logout destroys the current session and clears the cookie.
func Logout(store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cookie, err := c.Cookie("agentguard_session"); err == nil {
			store.Delete(cookie)
		}
		// MaxAge=-1 instructs the browser to delete the cookie immediately.
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     "agentguard_session",
			Value:    "",
			MaxAge:   -1,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
		})
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
