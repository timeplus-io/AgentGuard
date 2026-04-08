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
       llm_call_count, total_tokens,
       coalesce(session_count, 0)   AS session_count,
       coalesce(run_count, 0)       AS run_count,
       coalesce(tool_call_count, 0) AS tool_call_count
FROM (
    SELECT agent_id, deployment_id,
           any(deployment_name)                                                  AS deployment_name,
           max(event_time)                                                       AS last_seen,
           count()                                                               AS event_count,
           count_if(hook_name = 'llm_output')                                   AS llm_call_count,
           sum(if(hook_name = 'llm_output',
                  to_int64(json_extract_string(event_data, 'usage', 'total')),
                  0))                                                            AS total_tokens
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

// GetAgent handles GET /api/agents/:id
// Query params: since (RFC3339), until (RFC3339) — filter all counts to the time window.
func GetAgent(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		// Q1: identity — no time filter, returns 404 only if agent truly doesn't exist.
		identitySQL := fmt.Sprintf(`
SELECT agent_id, deployment_id,
       any(deployment_name) AS deployment_name,
       max(event_time)      AS last_seen
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
GROUP BY agent_id, deployment_id
LIMIT 1`, deploymentID, agentID)

		irows, err := timeplus.Query(&cfg.Timeplus, identitySQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		if len(irows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		// Q2: time-filtered counts (event_count, session_count, run_count, tool_call_count).
		// Returns 0 rows when no events fall in the time window — counts default to 0.
		countsSQL := fmt.Sprintf(`
SELECT event_count,
       coalesce(session_count, 0)   AS session_count,
       coalesce(run_count, 0)       AS run_count,
       coalesce(tool_call_count, 0) AS tool_call_count
FROM (
    SELECT count() AS event_count
    FROM table(agentshield_hook_events)
    WHERE deployment_id = '%s'
      AND agent_id      = '%s'%s
) AS ev
LEFT JOIN (
    SELECT count()       AS session_count,
           sum(run_count)       AS run_count,
           sum(tool_call_count) AS tool_call_count
    FROM (
        SELECT session_id,
               count_distinct(if(run_id != '', run_id, null)) AS run_count,
               count_if(hook_name = 'before_tool_call')       AS tool_call_count
        FROM table(agentshield_hook_events)
        WHERE deployment_id = '%s'
          AND agent_id      = '%s'
          AND session_id    != ''%s
        GROUP BY session_id
        HAVING any_if(session_key, session_key != '') NOT LIKE 'temp:%%'
          AND session_id NOT LIKE 'slug-generator-%%'
          AND count_if(run_id != '') > 0
    )
) AS s ON 1 = 1
LIMIT 1`, deploymentID, agentID, timeWhere, deploymentID, agentID, timeWhere)

		crows, err := timeplus.Query(&cfg.Timeplus, countsSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		agent := models.Agent{
			AgentID:        parseStr(irows[0]["agent_id"]),
			DeploymentID:   parseStr(irows[0]["deployment_id"]),
			DeploymentName: parseStr(irows[0]["deployment_name"]),
			LastSeen:       parseTime(irows[0]["last_seen"]),
		}
		if len(crows) > 0 {
			agent.EventCount = parseInt(crows[0]["event_count"])
			agent.SessionCount = parseInt(crows[0]["session_count"])
			agent.RunCount = parseInt(crows[0]["run_count"])
			agent.ToolCallCount = parseInt(crows[0]["tool_call_count"])
		}

		c.JSON(http.StatusOK, agent)
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
		TotalTokens:    parseInt64(row["total_tokens"]),
		LLMCallCount:   parseInt(row["llm_call_count"]),
	}
}
