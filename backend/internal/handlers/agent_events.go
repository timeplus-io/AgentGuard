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
		page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
		if err != nil || page < 1 {
			page = 1
		}
		limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
		if err != nil || limit < 1 {
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

		sid := c.Query("session_id")
		rid := c.Query("run_id")
		if sid == "" && rid == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session_id is required unless run_id is provided"})
			return
		}

		if sid != "" {
			if !idPartRe.MatchString(sid) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session_id"})
				return
			}
			where = append(where, fmt.Sprintf("session_id = '%s'", sid))
		}

		if rid != "" {
			if !runIDRe.MatchString(rid) {
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
			where = append(where, fmt.Sprintf("event_time >= '%s'", t.UTC().Format("2006-01-02 15:04:05.999999999")))
		}

		if until := c.Query("until"); until != "" {
			t, err := time.Parse(time.RFC3339, until)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid until: use RFC3339"})
				return
			}
			where = append(where, fmt.Sprintf("event_time <= '%s'", t.UTC().Format("2006-01-02 15:04:05.999999999")))
		}

		whereClause := strings.Join(where, "\n  AND ")

		// Count total matching rows
		countSQL := fmt.Sprintf(`
SELECT count() AS total
FROM table(agentshield_hook_events)
WHERE %s`, whereClause)

		countRows, err := timeplus.Query(&cfg.Timeplus, countSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		total := 0
		if len(countRows) > 0 {
			total = parseInt(countRows[0]["total"])
		}

		if total == 0 || (page-1)*limit >= total {
			c.JSON(http.StatusOK, models.EventsResponse{
				Events: []models.HookEvent{},
				Total:  total,
				Page:   page,
				Limit:  limit,
			})
			return
		}

		// Fetch one page via SQL LIMIT/OFFSET
		pageSQL := fmt.Sprintf(`
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
ORDER BY event_time ASC
LIMIT %d OFFSET %d`,
			whereClause, limit, (page-1)*limit)

		rows, err := timeplus.Query(&cfg.Timeplus, pageSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		events := make([]models.HookEvent, 0, len(rows))
		for _, row := range rows {
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
