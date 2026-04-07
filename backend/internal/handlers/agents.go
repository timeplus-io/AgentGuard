package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	"github.com/timeplus/agentshield/internal/timeplus"
)

var idPartRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func ListAgents(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		pageStr := c.DefaultQuery("page", "1")
		limitStr := c.DefaultQuery("limit", "15")

		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			limit = 15
		}
		if limit > 200 {
			limit = 200
		}

		sql := `
SELECT
    agent_id,
    deployment_id,
    deployment_name,
    max(event_time)             AS last_seen,
    count()                     AS event_count,
    count(DISTINCT session_key) AS session_count
FROM table(agentshield_hook_events)
WHERE agent_id != ''
GROUP BY agent_id, deployment_id, deployment_name
ORDER BY last_seen DESC`

		rows, err := timeplus.Query(&cfg.Timeplus, sql)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		agents := make([]models.Agent, 0, len(rows))
		for _, row := range rows {
			agents = append(agents, rowToAgent(row))
		}

		total := len(agents)
		start := (page - 1) * limit
		if start >= total {
			c.JSON(http.StatusOK, models.AgentListResponse{
				Agents: []models.Agent{},
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

		c.JSON(http.StatusOK, models.AgentListResponse{
			Agents: agents[start:end],
			Total:  total,
			Page:   page,
			Limit:  limit,
		})
	}
}

func GetAgent(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		parts := strings.SplitN(id, "__", 2)
		if len(parts) != 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format, expected deploymentId__agentId"})
			return
		}
		deploymentID := parts[0]
		agentID := parts[1]

		if !idPartRe.MatchString(deploymentID) || !idPartRe.MatchString(agentID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format, expected deploymentId__agentId"})
			return
		}

		sql := fmt.Sprintf(`
SELECT
    agent_id,
    deployment_id,
    deployment_name,
    max(event_time)             AS last_seen,
    count()                     AS event_count,
    count(DISTINCT session_key) AS session_count
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
GROUP BY agent_id, deployment_id, deployment_name
LIMIT 1`,
			deploymentID, agentID)

		rows, err := timeplus.Query(&cfg.Timeplus, sql)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		if len(rows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		a := rowToAgent(rows[0])
		c.JSON(http.StatusOK, a)
	}
}

func rowToAgent(row map[string]any) models.Agent {
	agentID, _ := row["agent_id"].(string)
	deploymentID, _ := row["deployment_id"].(string)
	deploymentName, _ := row["deployment_name"].(string)

	var lastSeen time.Time
	switch v := row["last_seen"].(type) {
	case float64:
		// Unix seconds (rare format)
		lastSeen = time.Unix(int64(v), 0)
	case json.Number:
		// Numeric timestamp — try as seconds
		if n, err := v.Float64(); err == nil {
			lastSeen = time.Unix(int64(n), 0)
		}
	case string:
		if v != "" {
			t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", v)
			if err != nil {
				// Proton returns various time formats; try RFC3339 fallback.
				t, err = time.Parse(time.RFC3339Nano, v)
				if err != nil {
					lastSeen = time.Time{}
				} else {
					lastSeen = t
				}
			} else {
				lastSeen = t
			}
		}
	}

	eventCount := 0
	switch v := row["event_count"].(type) {
	case float64:
		eventCount = int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			eventCount = int(n)
		}
	case int:
		eventCount = v
	}

	sessionCount := 0
	switch v := row["session_count"].(type) {
	case float64:
		sessionCount = int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			sessionCount = int(n)
		}
	case int:
		sessionCount = v
	}

	return models.Agent{
		AgentID:        agentID,
		DeploymentID:   deploymentID,
		DeploymentName: deploymentName,
		LastSeen:       lastSeen,
		EventCount:     eventCount,
		SessionCount:   sessionCount,
	}
}
