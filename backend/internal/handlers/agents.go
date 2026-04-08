package handlers

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"

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
    count_distinct(session_key) AS session_count
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
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
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
    count_distinct(session_key) AS session_count
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
	return models.Agent{
		AgentID:        parseStr(row["agent_id"]),
		DeploymentID:   parseStr(row["deployment_id"]),
		DeploymentName: parseStr(row["deployment_name"]),
		LastSeen:       parseTime(row["last_seen"]),
		EventCount:     parseInt(row["event_count"]),
		SessionCount:   parseInt(row["session_count"]),
	}
}
