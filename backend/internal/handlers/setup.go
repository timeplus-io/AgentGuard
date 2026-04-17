package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// TestConnection validates Timeplus credentials without creating a session.
// Body: {"host":"localhost","port":3218,"username":"default","password":""}
func TestConnection() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Host     string `json:"host"     binding:"required"`
			Port     int    `json:"port"     binding:"required"`
			Username string `json:"username" binding:"required"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}

		testCfg := config.TimeplusConfig{
			Host:     body.Host,
			Port:     body.Port,
			User:     body.Username,
			Password: body.Password,
		}
		if _, err := tp.Query(&testCfg, "SELECT 1 AS ok"); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

type provisionEvent struct {
	Status string `json:"status"` // "running" | "ok" | "error" | "done"
	Msg    string `json:"msg"`
}

// Provision runs Init() in-process and streams progress as SSE events.
// On success it writes config.yaml; the frontend then calls /api/auth/login.
//
// Body: {"host":"...","port":3218,"native_port":8463,"username":"...","password":"...","agentguard_url":"..."}
func Provision(cfg *config.Config, mgr *catalog.Manager, store *auth.SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Host          string `json:"host"           binding:"required"`
			Port          int    `json:"port"           binding:"required"`
			NativePort    int    `json:"native_port"    binding:"required"`
			Username      string `json:"username"       binding:"required"`
			Password      string `json:"password"`
			AgentGuardURL string `json:"agentguard_url"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		config.Mu.RLock()
		alreadySetUp := cfg.SetupComplete
		config.Mu.RUnlock()
		if alreadySetUp {
			c.JSON(http.StatusConflict, gin.H{"error": "already set up"})
			return
		}

		// Check SSE support first, before writing any headers.
		w := c.Writer
		flusher, ok := w.(http.Flusher)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
			return
		}

		// SSE headers must be set before the first write.
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no") // disable nginx buffering

		sendEvent := func(status, msg string) {
			data, _ := json.Marshal(provisionEvent{Status: status, Msg: msg})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		agentGuardURL := body.AgentGuardURL
		if agentGuardURL == "" {
			agentGuardURL = cfg.AgentGuardURL
		}

		timeplusCfg := config.TimeplusConfig{
			Host:       body.Host,
			Port:       body.Port,
			NativePort: body.NativePort,
			User:       body.Username,
			Password:   body.Password,
		}

		if err := tp.Init(timeplusCfg, agentGuardURL, mgr.All(), sendEvent); err != nil {
			sendEvent("error", "Setup failed: "+err.Error())
			return
		}

		// Persist non-credential config so server knows setup is done after restart.
		newCfg := *cfg
		newCfg.Timeplus.Host = body.Host
		newCfg.Timeplus.Port = body.Port
		newCfg.Timeplus.NativePort = body.NativePort
		newCfg.AgentGuardURL = agentGuardURL
		newCfg.SetupComplete = true
		if err := config.Save(&newCfg, cfg.ConfigPath); err != nil {
			sendEvent("error", "Failed to save config: "+err.Error())
			return
		}

		// Update the live cfg pointer so /api/auth/status immediately reflects setup_complete=true.
		config.Mu.Lock()
		cfg.Timeplus.Host = body.Host
		cfg.Timeplus.Port = body.Port
		cfg.Timeplus.NativePort = body.NativePort
		cfg.AgentGuardURL = agentGuardURL
		cfg.SetupComplete = true
		config.Mu.Unlock()

		// Session is created by the frontend calling /api/auth/login after receiving "done".
		sendEvent("done", "Setup complete")
	}
}
