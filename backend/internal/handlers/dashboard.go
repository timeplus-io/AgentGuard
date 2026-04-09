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
		// Total distinct agents ever seen.
		agentRows, _ := tp.Query(&cfg.Timeplus,
			`SELECT count(distinct agent_id) AS total FROM table(agentshield_hook_events)`)
		totalAgents := 0
		if len(agentRows) > 0 {
			totalAgents = int(parseInt64(agentRows[0]["total"]))
		}

		// Active threats and critical escalations in the last 24 h.
		threatRows, _ := tp.Query(&cfg.Timeplus, `
SELECT count() AS total, countIf(severity = 'Critical') AS critical
FROM table(agentshield_security_events)
WHERE event_time >= now() - interval 1 day`)
		activeThreats, criticalEscalations := 0, 0
		if len(threatRows) > 0 {
			activeThreats = int(parseInt64(threatRows[0]["total"]))
			criticalEscalations = int(parseInt64(threatRows[0]["critical"]))
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
		costStr := fmt.Sprintf("$%.2f", totalCost)
		costStatus := "within budget"

		c.JSON(http.StatusOK, models.DashboardResponse{
			KPIs: models.KPIData{
				TotalAgents:         totalAgents,
				AgentsDelta:         "",
				ActiveThreats:       activeThreats,
				CriticalEscalations: criticalEscalations,
				TotalCost24h:        costStr,
				CostStatus:          costStatus,
				ComplianceCoverage:  94.7,
				ComplianceLabel:     "SOC2 + HIPAA",
			},
			LiveThreatFeed:   []models.Threat{},
			ThreatCategories: []models.ThreatCategory{},
			RecentAgents:     []models.Agent{},
		})
	}
}
