package handlers

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// uuidRe validates strict UUID-format params before SQL interpolation.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// runIDRe validates run_id values — OpenClaw uses multiple formats:
//   - chatcmpl_<uuid>   (HTTP API / OpenAI-compat)
//   - announce:v1:agent:...:run:<uuid>  (subagent runs)
//   - slug-gen-<timestamp>  (internal, filtered at session level)
// Allows alphanum, hyphens, underscores, dots, colons. Excludes quotes/backslash.
var runIDRe = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)

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

// parseRFC3339 parses an RFC3339 timestamp string into Proton-compatible format.
// Returns ("", nil) when s is empty. Returns an error when s is non-empty but invalid.
func parseRFC3339(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "", err
	}
	return t.UTC().Format("2006-01-02 15:04:05.999999999"), nil
}

// buildTimeWhere returns SQL AND clauses for since/until Proton timestamps.
// Each condition starts with a newline so it can be appended to an existing WHERE clause.
func buildTimeWhere(since, until string) string {
	out := ""
	if since != "" {
		out += fmt.Sprintf("\n      AND event_time >= '%s'", since)
	}
	if until != "" {
		out += fmt.Sprintf("\n      AND event_time <= '%s'", until)
	}
	return out
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
// Query params: since (RFC3339), until (RFC3339)
func ListSessions(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format, expected deploymentId__agentId"})
			return
		}

		sinceStr, err := parseRFC3339(c.Query("since"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid since: use RFC3339"})
			return
		}
		untilStr, err := parseRFC3339(c.Query("until"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid until: use RFC3339"})
			return
		}
		timeWhere := buildTimeWhere(sinceStr, untilStr)

		sql := fmt.Sprintf(`
SELECT
    session_id,
    any_if(session_key, session_key != '')               AS session_key,
    any_if(model,    model    != '')                     AS model,
    any_if(provider, provider != '')                     AS provider,
    count_distinct(if(run_id != '', run_id, null))       AS run_count,
    count()                                              AS event_count,
    min(event_time)                                      AS first_seen,
    max(event_time)                                      AS last_seen,
    count_if(hook_name = 'session_end')                  AS ended_event_count
FROM table(agentguard_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    != ''%s
GROUP BY session_id
HAVING session_key NOT LIKE 'temp:%%'
   AND session_id  NOT LIKE 'slug-generator-%%'
   AND count_if(run_id != '') > 0
ORDER BY last_seen DESC`,
			deploymentID, agentID, timeWhere)

		rows, err := timeplus.Query(&cfg.Timeplus, sql)
		if err != nil {
			log.Printf("[sessions] ListSessions query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		sessions := make([]models.Session, 0, len(rows))
		for _, row := range rows {
			key := parseStr(row["session_key"])
			status := "active"
			if parseInt(row["ended_event_count"]) > 0 {
				status = "ended"
			}
			sessions = append(sessions, models.Session{
				SessionID:   parseStr(row["session_id"]),
				SessionKey:  key,
				SessionType: sessionType(key),
				Status:      status,
				Model:       parseStr(row["model"]),
				Provider:    parseStr(row["provider"]),
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
    count_if(hook_name = 'before_tool_call') AS tool_count,
    count_if(hook_decision = 'block')        AS block_count
FROM table(agentguard_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'
  AND run_id        != ''
GROUP BY run_id
ORDER BY start_time ASC`,
			deploymentID, agentID, sessionID)

		runsRows, err := timeplus.Query(&cfg.Timeplus, runsSQL)
		if err != nil {
			log.Printf("[sessions] GetSession runs query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		if len(runsRows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}

		// Second query: session-level counters (no run_id filter, catches session_end).
		statsSQL := fmt.Sprintf(`
SELECT
    count_if(hook_name = 'session_end')      AS has_ended,
    count_if(hook_name = 'before_tool_call') AS total_tool_calls,
    any_if(model,    model    != '')         AS model,
    any_if(provider, provider != '')         AS provider
FROM table(agentguard_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'`,
			deploymentID, agentID, sessionID)

		statsRows, err := timeplus.Query(&cfg.Timeplus, statsSQL)
		if err != nil {
			// Non-fatal: degrade gracefully if stats query fails.
			statsRows = nil
		}

		status := "active"
		totalToolCalls := 0
		sessionModel := ""
		sessionProvider := ""
		if len(statsRows) > 0 {
			if parseBool(statsRows[0]["has_ended"]) {
				status = "ended"
			}
			totalToolCalls = parseInt(statsRows[0]["total_tool_calls"])
			sessionModel = parseStr(statsRows[0]["model"])
			sessionProvider = parseStr(statsRows[0]["provider"])
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
			SessionID:      sessionID,
			SessionKey:     sessionKey,
			SessionType:    sessionType(sessionKey),
			Status:         status,
			Model:          sessionModel,
			Provider:       sessionProvider,
			TotalToolCalls: totalToolCalls,
			Runs:           runs,
		})
	}
}

