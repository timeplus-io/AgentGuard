package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// timelineEventTypes are the canonical CIM event types the timeline renders.
const timelineEventTypes = "'user_input','llm_response','tool_invoke','tool_complete','session_start','session_end'"

// GetSessionTimeline handles
// GET /api/agents/:id/sessions/:session_id/timeline (optional ?run_id=).
// It queries the normalized agentguard_cim_event stream and returns an ordered,
// agent-agnostic list of timeline events.
func GetSessionTimeline(cfg *config.Config) gin.HandlerFunc {
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

		where := []string{
			fmt.Sprintf("deployment_id = '%s'", escapeSQL(deploymentID)),
			fmt.Sprintf("agent_id = '%s'", escapeSQL(agentID)),
			fmt.Sprintf("session_id = '%s'", escapeSQL(sessionID)),
			"event_type IN (" + timelineEventTypes + ")",
		}

		if rid := c.Query("run_id"); rid != "" {
			if !runIDRe.MatchString(rid) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run_id format"})
				return
			}
			where = append(where, fmt.Sprintf("run_id = '%s'", escapeSQL(rid)))
		}

		sql := fmt.Sprintf(`
SELECT event_time, event_type, run_id, tool_name, tool_input, tool_result,
       tool_success, user_message, assistant_message, model, provider,
       hook_decision, block_reason, raw_event_data
FROM table(agentguard_cim_event)
WHERE %s
ORDER BY event_time ASC`, strings.Join(where, "\n  AND "))

		// Native binary protocol: user/assistant/tool content can break the
		// HTTP+JSON serializer.
		rows, err := timeplus.QueryNative(&cfg.Timeplus, sql)
		if err != nil {
			log.Printf("timeline: GetSessionTimeline query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		c.JSON(http.StatusOK, models.TimelineResponse{Events: buildTimeline(rows)})
	}
}

// buildTimeline maps time-ordered CIM-event rows into normalized timeline events.
// It is agent-agnostic: it switches on the canonical event_type only. A turn is
// one user_input plus everything until the next user_input; events before the
// first user_input are turn 0.
func buildTimeline(rows []map[string]any) []models.TimelineEvent {
	events := make([]models.TimelineEvent, 0, len(rows))
	turnIndex := 0

	for _, r := range rows {
		eventType := parseStr(r["event_type"])

		ev := models.TimelineEvent{
			EventTime: parseTime(r["event_time"]),
			RunID:     parseStr(r["run_id"]),
			Status:    "ok",
		}

		switch eventType {
		case "user_input":
			turnIndex++
			ev.Kind = "user_input"
			ev.Title = "User"
			ev.Body = parseStr(r["user_message"])

		case "llm_response":
			ev.Kind = "llm_response"
			ev.Model = parseStr(r["model"])
			ev.Provider = parseStr(r["provider"])
			ev.Title = ev.Model
			if ev.Title == "" {
				ev.Title = "Assistant"
			}
			ev.Body = parseStr(r["assistant_message"])

		case "tool_invoke":
			ev.Kind = "tool_request"
			ev.ToolName = parseStr(r["tool_name"])
			ev.Title = ev.ToolName
			ev.Body = parseStr(r["tool_input"])
			ev.ToolCallID = parseToolCallID(parseStr(r["raw_event_data"]))
			if parseStr(r["hook_decision"]) == "block" {
				ev.Status = "blocked"
				if reason := parseStr(r["block_reason"]); reason != "" {
					ev.Attributes = map[string]string{"block_reason": reason}
				}
			}

		case "tool_complete":
			ev.Kind = "tool_result"
			ev.ToolName = parseStr(r["tool_name"])
			ev.Title = ev.ToolName
			ev.Body = parseStr(r["tool_result"])
			ev.ToolCallID = parseToolCallID(parseStr(r["raw_event_data"]))
			if v, ok := r["tool_success"]; ok && !parseBool(v) {
				ev.Status = "error"
			}

		case "session_start":
			ev.Kind = "session_start"
			ev.Title = "Session start"

		case "session_end":
			ev.Kind = "session_end"
			ev.Title = "Session end"

		default:
			// Unknown / non-timeline event type — skip.
			continue
		}

		ev.TurnIndex = turnIndex
		events = append(events, ev)
	}

	return events
}

// parseToolCallID extracts a tool-call id from a raw CIM event_data JSON string.
// It tries the known agent spellings in order: toolCallId (OpenClaw), then
// tool_call_id (Hermes). Returns "" when absent or unparseable (e.g. Claude Code).
func parseToolCallID(raw string) string {
	if raw == "" {
		return ""
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return ""
	}
	for _, key := range []string{"toolCallId", "tool_call_id"} {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
