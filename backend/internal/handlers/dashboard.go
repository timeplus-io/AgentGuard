package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	tp "github.com/timeplus/agentshield/internal/timeplus"
)

func GetDashboard(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Total distinct agents ever seen (exclude rows with empty agent_id).
		agentRows, _ := tp.Query(&cfg.Timeplus,
			`SELECT count(distinct agent_id) AS total FROM table(agentshield_hook_events) WHERE agent_id != ''`)
		totalAgents := 0
		if len(agentRows) > 0 {
			totalAgents = int(parseInt64(agentRows[0]["total"]))
		}

		// Active threats in the last 24 h.
		// Uses detected_at (the timestamp column on agentshield_security_events).
		// countIf is unsupported in Proton — run two separate counts.
		threatRows, _ := tp.Query(&cfg.Timeplus,
			`SELECT count() AS total FROM table(agentshield_security_events) WHERE detected_at >= now() - interval 1 day`)
		criticalRows, _ := tp.Query(&cfg.Timeplus,
			`SELECT count() AS critical FROM table(agentshield_security_events) WHERE detected_at >= now() - interval 1 day AND severity = 'critical'`)
		activeThreats, criticalEscalations := 0, 0
		if len(threatRows) > 0 {
			activeThreats = int(parseInt64(threatRows[0]["total"]))
		}
		if len(criticalRows) > 0 {
			criticalEscalations = int(parseInt64(criticalRows[0]["critical"]))
		}

		// Estimated LLM cost over the last 24 h.
		// Groups by (provider, model) so any(price) is deterministic; sums in Go.
		costRows, _ := tp.Query(&cfg.Timeplus, `
SELECT
  (
    sum(to_int64(json_extract_string(e.event_data, 'usage', 'input')))
      * any(p.input_cost_per_million)
    + sum(to_int64(json_extract_string(e.event_data, 'usage', 'output')))
      * any(p.output_cost_per_million)
    + sum(to_int64(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))
      * any(p.cache_read_cost_per_million)
  ) / 1000000.0 AS cost
FROM table(agentshield_hook_events) AS e
LEFT JOIN table(agentshield_pricing) AS p
  ON e.provider = p.provider AND e.model = p.model
WHERE e.hook_name = 'llm_output'
  AND e.event_time >= now() - interval 1 day
GROUP BY e.provider, e.model`)
		var totalCost float64
		for _, r := range costRows {
			totalCost += parseFloat64(r["cost"])
		}
		// Active rules — latest state per rule_id via arg_max.
		ruleRows, _ := tp.Query(&cfg.Timeplus, `
SELECT count() AS total
FROM (
  SELECT rule_id, arg_max(enabled, updated_at) AS active
  FROM table(agentshield_rules)
  GROUP BY rule_id
  HAVING active = true
)`)
		activeRules := 0
		if len(ruleRows) > 0 {
			activeRules = int(parseInt64(ruleRows[0]["total"]))
		}

		costStr := fmt.Sprintf("$%.2f", totalCost)

		c.JSON(http.StatusOK, models.DashboardResponse{
			KPIs: models.KPIData{
				TotalAgents:         totalAgents,
				AgentsDelta:         "",
				ActiveThreats:       activeThreats,
				CriticalEscalations: criticalEscalations,
				TotalCost24h:        costStr,
				CostStatus:          "within budget",
				ActiveRules:         activeRules,
			},
			LiveThreatFeed:   []models.Threat{},
			ThreatCategories: []models.ThreatCategory{},
			RecentAgents:     []models.Agent{},
		})
	}
}
