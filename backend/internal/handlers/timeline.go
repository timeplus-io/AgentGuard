package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// GetRunTrace handles GET /api/agents/:id/sessions/:session_id/runs/:run_id/trace.
// It fetches hook events from Proton, detects the agent type, and routes to the
// appropriate timeline analyzer (OpenClaw hierarchical or flat gap-fill fallback).
func GetRunTrace(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format"})
			return
		}

		sessionID := c.Param("session_id")
		if !idPartRe.MatchString(sessionID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session_id format"})
			return
		}

		runID := c.Param("run_id")
		if !runIDRe.MatchString(runID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run_id format"})
			return
		}

		rows, err := fetchHookEvents(cfg, deploymentID, agentID, sessionID, runID)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		if len(rows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}

		agentType := detectAgentType(rows)

		var spans []models.TraceSpan
		switch agentType {
		case "openclaw":
			spans = analyzeOpenClaw(runID, rows)
		case "hermes":
			spans = analyzeHermes(runID, rows)
		default:
			spans = analyzeFallback(runID, rows)
		}

		c.JSON(http.StatusOK, models.TraceResponse{Spans: spans})
	}
}

// fetchHookEvents queries Proton for all hook events for a specific run,
// ordered by event_time ascending.
func fetchHookEvents(cfg *config.Config, deploymentID, agentID, sessionID, runID string) ([]map[string]any, error) {
	sql := fmt.Sprintf(`
SELECT hook_name, event_time, tool_name, provider, model,
       hook_decision, block_reason, event_data, agent_type
FROM table(agentguard_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'
  AND run_id        = '%s'
ORDER BY event_time ASC`,
		escapeSQL(deploymentID), escapeSQL(agentID), escapeSQL(sessionID), escapeSQL(runID))

	return timeplus.Query(&cfg.Timeplus, sql)
}

// detectAgentType returns the agent_type value from the first event that has
// a non-empty value, or "" to trigger the fallback analyzer.
func detectAgentType(rows []map[string]any) string {
	for _, row := range rows {
		if t := parseStr(row["agent_type"]); t != "" {
			return t
		}
	}
	return ""
}
