package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	"github.com/timeplus/agentshield/internal/timeplus"
)

// ListEvents handles GET /api/agents/:id/events
// Query params: session_id, run_id, since (RFC3339), until (RFC3339), page, limit
func ListEvents(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format"})
			return
		}

		// Pagination
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
		if limit < 1 {
			limit = 100
		}
		if limit > 500 {
			limit = 500
		}

		// Build WHERE clauses
		where := []string{
			fmt.Sprintf("deployment_id = '%s'", deploymentID),
			fmt.Sprintf("agent_id = '%s'", agentID),
		}

		if sid := c.Query("session_id"); sid != "" {
			if !uuidRe.MatchString(sid) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session_id"})
				return
			}
			where = append(where, fmt.Sprintf("session_id = '%s'", sid))
		}

		if rid := c.Query("run_id"); rid != "" {
			if !uuidRe.MatchString(rid) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run_id"})
				return
			}
			where = append(where, fmt.Sprintf("run_id = '%s'", rid))
		}

		if since := c.Query("since"); since != "" {
			t, err := time.Parse(time.RFC3339, since)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid since: use RFC3339"})
				return
			}
			where = append(where, fmt.Sprintf("event_time >= '%s'", t.UTC().Format("2006-01-02 15:04:05")))
		}

		if until := c.Query("until"); until != "" {
			t, err := time.Parse(time.RFC3339, until)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid until: use RFC3339"})
				return
			}
			where = append(where, fmt.Sprintf("event_time <= '%s'", t.UTC().Format("2006-01-02 15:04:05")))
		}

		whereClause := strings.Join(where, "\n  AND ")

		// Fetch all matching rows for server-side pagination
		sql := fmt.Sprintf(`
SELECT
    hook_name,
    event_time,
    run_id,
    session_id,
    tool_name,
    provider,
    model,
    hook_decision,
    block_reason,
    event_data
FROM table(agentshield_hook_events)
WHERE %s
ORDER BY event_time ASC`,
			whereClause)

		rows, err := timeplus.Query(&cfg.Timeplus, sql)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		total := len(rows)
		start := (page - 1) * limit
		if start >= total {
			c.JSON(http.StatusOK, models.EventsResponse{
				Events: []models.HookEvent{},
				Total:  total,
				Page:   page,
				Limit:  limit,
			})
			return
		}
		end := start + limit
		if end > total {
			end = total
		}

		events := make([]models.HookEvent, 0, end-start)
		for _, row := range rows[start:end] {
			events = append(events, models.HookEvent{
				HookName:     parseStr(row["hook_name"]),
				EventTime:    parseTime(row["event_time"]),
				RunID:        parseStr(row["run_id"]),
				SessionID:    parseStr(row["session_id"]),
				ToolName:     parseStr(row["tool_name"]),
				Provider:     parseStr(row["provider"]),
				Model:        parseStr(row["model"]),
				HookDecision: parseStr(row["hook_decision"]),
				BlockReason:  parseStr(row["block_reason"]),
				EventData:    parseStr(row["event_data"]),
			})
		}

		c.JSON(http.StatusOK, models.EventsResponse{
			Events: events,
			Total:  total,
			Page:   page,
			Limit:  limit,
		})
	}
}
