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

// filteredSessionCountSQL is a subquery returning per-agent session counts,
// run counts, and tool call counts using the same filters as ListSessions
// (excludes internal/temp sessions and sessions with no actual run activity).
const filteredSessionCountSQL = `
SELECT agent_id, deployment_id,
       count()                         AS session_count,
       sum(run_count)                  AS run_count,
       sum(tool_call_count)            AS tool_call_count
FROM (
    SELECT agent_id, deployment_id, session_id,
           count_distinct(if(run_id != '', run_id, null)) AS run_count,
           count_if(hook_name = 'before_tool_call')       AS tool_call_count
    FROM table(agentshield_hook_events)
    WHERE session_id != ''
    GROUP BY agent_id, deployment_id, session_id
    HAVING any_if(session_key, session_key != '') NOT LIKE 'temp:%%'
      AND session_id NOT LIKE 'slug-generator-%%'
      AND count_if(run_id != '') > 0
)
GROUP BY agent_id, deployment_id`

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

		sql := fmt.Sprintf(`
SELECT agent_id, deployment_id, deployment_name, last_seen, event_count,
       coalesce(session_count, 0)   AS session_count,
       coalesce(run_count, 0)       AS run_count,
       coalesce(tool_call_count, 0) AS tool_call_count
FROM (
    SELECT agent_id, deployment_id,
           any(deployment_name) AS deployment_name,
           max(event_time)      AS last_seen,
           count()              AS event_count
    FROM table(agentshield_hook_events)
    WHERE agent_id != ''
    GROUP BY agent_id, deployment_id
) AS agents
LEFT JOIN (%s) AS s USING (agent_id, deployment_id)
ORDER BY last_seen DESC`, filteredSessionCountSQL)

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
SELECT agent_id, deployment_id, deployment_name, last_seen, event_count,
       coalesce(session_count, 0)   AS session_count,
       coalesce(run_count, 0)       AS run_count,
       coalesce(tool_call_count, 0) AS tool_call_count
FROM (
    SELECT agent_id, deployment_id,
           any(deployment_name) AS deployment_name,
           max(event_time)      AS last_seen,
           count()              AS event_count
    FROM table(agentshield_hook_events)
    WHERE deployment_id = '%s'
      AND agent_id      = '%s'
    GROUP BY agent_id, deployment_id
) AS agents
LEFT JOIN (
    SELECT agent_id, deployment_id,
           count()                                         AS session_count,
           sum(run_count)                                  AS run_count,
           sum(tool_call_count)                            AS tool_call_count
    FROM (
        SELECT agent_id, deployment_id, session_id,
               count_distinct(if(run_id != '', run_id, null)) AS run_count,
               count_if(hook_name = 'before_tool_call')       AS tool_call_count
        FROM table(agentshield_hook_events)
        WHERE deployment_id = '%s'
          AND agent_id      = '%s'
          AND session_id    != ''
        GROUP BY agent_id, deployment_id, session_id
        HAVING any_if(session_key, session_key != '') NOT LIKE 'temp:%%'
          AND session_id NOT LIKE 'slug-generator-%%'
          AND count_if(run_id != '') > 0
    )
    GROUP BY agent_id, deployment_id
) AS s USING (agent_id, deployment_id)
LIMIT 1`,
			deploymentID, agentID, deploymentID, agentID)

		rows, err := timeplus.Query(&cfg.Timeplus, sql)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		if len(rows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		c.JSON(http.StatusOK, rowToAgent(rows[0]))
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
		RunCount:       parseInt(row["run_count"]),
		ToolCallCount:  parseInt(row["tool_call_count"]),
	}
}
