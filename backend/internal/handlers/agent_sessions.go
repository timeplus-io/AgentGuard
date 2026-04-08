package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	"github.com/timeplus/agentshield/internal/timeplus"
)

// uuidRe validates session_id and run_id path/query params before SQL interpolation.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// sessionType derives a human-readable type label from the session_key pattern.
func sessionType(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) < 3 {
		return "Unknown"
	}
	switch parts[2] {
	case "main":
		return "Web UI"
	case "openai-user":
		return "HTTP API"
	case "cron":
		return "Cron"
	case "subagent":
		return "Sub-agent"
	default:
		return "Unknown"
	}
}

// parseTime handles the multiple time formats Proton returns.
func parseTime(v any) time.Time {
	switch s := v.(type) {
	case string:
		if s == "" {
			return time.Time{}
		}
		if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", s); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	case float64:
		return time.Unix(int64(s), 0)
	case json.Number:
		if n, err := s.Float64(); err == nil {
			return time.Unix(int64(n), 0)
		}
	}
	return time.Time{}
}

// parseInt handles json.Number / float64 / int from Proton rows.
func parseInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	case int:
		return n
	}
	return 0
}

// parseBool handles Proton's countIf(...) result (returned as 0/1).
func parseBool(v any) bool {
	return parseInt(v) != 0
}

// parseStr safely casts any to string.
func parseStr(v any) string {
	s, _ := v.(string)
	return s
}

// splitAgentID splits the compound "deploymentId__agentId" path param.
func splitAgentID(compoundID string) (deploymentID, agentID string, ok bool) {
	parts := strings.SplitN(compoundID, "__", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	if !idPartRe.MatchString(parts[0]) || !idPartRe.MatchString(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// ListSessions handles GET /api/agents/:id/sessions
func ListSessions(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format, expected deploymentId__agentId"})
			return
		}

		sql := fmt.Sprintf(`
SELECT
    session_id,
    anyIf(session_key, session_key != '') AS session_key,
    count(DISTINCT run_id)                AS run_count,
    count()                               AS event_count,
    min(event_time)                       AS first_seen,
    max(event_time)                       AS last_seen
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    != ''
GROUP BY session_id
HAVING session_key NOT LIKE 'temp:%%'
   AND session_id  NOT LIKE 'slug-generator-%%'
ORDER BY last_seen DESC`,
			deploymentID, agentID)

		rows, err := timeplus.Query(&cfg.Timeplus, sql)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		sessions := make([]models.Session, 0, len(rows))
		for _, row := range rows {
			key := parseStr(row["session_key"])
			sessions = append(sessions, models.Session{
				SessionID:   parseStr(row["session_id"]),
				SessionKey:  key,
				SessionType: sessionType(key),
				RunCount:    parseInt(row["run_count"]),
				EventCount:  parseInt(row["event_count"]),
				FirstSeen:   parseTime(row["first_seen"]),
				LastSeen:    parseTime(row["last_seen"]),
			})
		}

		c.JSON(http.StatusOK, sessions)
	}
}

// GetSession handles GET /api/agents/:id/sessions/:session_id
func GetSession(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		// Single query: fetch session_key alongside per-run aggregates.
		// any(session_key) returns the same value for all rows in the group (session_key
		// is constant within a session), so this is safe and eliminates a round-trip.
		runsSQL := fmt.Sprintf(`
SELECT
    any(session_key)                        AS session_key,
    run_id,
    min(event_time)                         AS start_time,
    max(event_time)                         AS end_time,
    count()                                 AS event_count,
    any(model)                              AS model,
    countIf(hook_name = 'before_tool_call') AS tool_count,
    countIf(hook_decision = 'block')        AS block_count
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'
  AND run_id        != ''
GROUP BY run_id
ORDER BY start_time ASC`,
			deploymentID, agentID, sessionID)

		runsRows, err := timeplus.Query(&cfg.Timeplus, runsSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		if len(runsRows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}

		sessionKey := parseStr(runsRows[0]["session_key"])

		runs := make([]models.Run, 0, len(runsRows))
		for _, row := range runsRows {
			runs = append(runs, models.Run{
				RunID:      parseStr(row["run_id"]),
				StartTime:  parseTime(row["start_time"]),
				EndTime:    parseTime(row["end_time"]),
				EventCount: parseInt(row["event_count"]),
				Model:      parseStr(row["model"]),
				ToolCount:  parseInt(row["tool_count"]),
				HasBlock:   parseBool(row["block_count"]),
			})
		}

		c.JSON(http.StatusOK, models.SessionDetail{
			SessionID:   sessionID,
			SessionKey:  sessionKey,
			SessionType: sessionType(sessionKey),
			Runs:        runs,
		})
	}
}
