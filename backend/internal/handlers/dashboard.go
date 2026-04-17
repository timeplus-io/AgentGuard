package handlers

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

func GetDashboard(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		// Total distinct agents ever seen (exclude rows with empty agent_id).
		agentRows, err := tp.Query(&cfg.Timeplus,
			`SELECT count(distinct agent_id) AS total FROM table(agentguard_hook_events) WHERE agent_id != ''`)
		if err != nil {
			log.Printf("[dashboard] agent count query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		totalAgents := 0
		if len(agentRows) > 0 {
			totalAgents = int(parseInt64(agentRows[0]["total"]))
		}

		// Active threats: deduplicated threats in open or acknowledged status
		// with activity in the last 24 h. Queries agentguard_threats (the
		// mutable stream of threat statuses) rather than agentguard_security_events
		// (raw detection events) so the count reflects threats, not event firings.
		threatRows, err := tp.Query(&cfg.Timeplus,
			`SELECT count() AS total FROM table(agentguard_threats) WHERE status IN ('open', 'acknowledged') AND last_seen >= now() - interval 1 day`)
		if err != nil {
			log.Printf("[dashboard] threat count query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		criticalRows, err := tp.Query(&cfg.Timeplus,
			`SELECT count() AS critical FROM table(agentguard_threats) WHERE status IN ('open', 'acknowledged') AND last_seen >= now() - interval 1 day AND severity = 'critical'`)
		if err != nil {
			log.Printf("[dashboard] critical threat count query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		activeThreats, criticalEscalations := 0, 0
		if len(threatRows) > 0 {
			activeThreats = int(parseInt64(threatRows[0]["total"]))
		}
		if len(criticalRows) > 0 {
			criticalEscalations = int(parseInt64(criticalRows[0]["critical"]))
		}

		// Estimated LLM cost over the last 24 h.
		costRows, err := tp.Query(&cfg.Timeplus, `
SELECT
  (
    sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'input')))
      * any(p.input_cost_per_million)
    + sum(to_int64_or_zero(json_extract_string(e.event_data, 'usage', 'output')))
      * any(p.output_cost_per_million)
    + sum(to_int64_or_zero(json_extract_string(e.event_data, 'lastAssistant', 'usage', 'cacheRead')))
      * any(p.cache_read_cost_per_million)
  ) / 1000000.0 AS cost
FROM table(agentguard_hook_events) AS e
LEFT JOIN table(agentguard_pricing) AS p
  ON e.provider = p.provider AND e.model = p.model
WHERE e.hook_name = 'llm_output'
  AND e.event_time >= now() - interval 1 day
GROUP BY e.provider, e.model`)
		if err != nil {
			log.Printf("[dashboard] cost query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		var totalCost float64
		for _, r := range costRows {
			totalCost += parseFloat64(r["cost"])
		}

		// Active rules — latest state per rule_id via arg_max.
		ruleRows, err := tp.Query(&cfg.Timeplus, `
SELECT count() AS total
FROM (
  SELECT rule_id, arg_max(enabled, updated_at) AS active
  FROM table(agentguard_rules)
  GROUP BY rule_id
  HAVING active = true
)`)
		if err != nil {
			log.Printf("[dashboard] active rules query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		activeRules := 0
		if len(ruleRows) > 0 {
			activeRules = int(parseInt64(ruleRows[0]["total"]))
		}

		// Threat categories — count by rule_name in last 24 h, compute percentages.
		catRows, err := tp.Query(&cfg.Timeplus, `
SELECT rule_name, count() AS cnt
FROM table(agentguard_security_events)
WHERE detected_at >= now() - interval 1 day
GROUP BY rule_name
ORDER BY cnt DESC`)
		if err != nil {
			log.Printf("[dashboard] threat categories query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		var catTotal int64
		for _, r := range catRows {
			catTotal += parseInt64(r["cnt"])
		}
		threatCategories := make([]models.ThreatCategory, 0, len(catRows))
		for _, r := range catRows {
			pct := 0
			if catTotal > 0 {
				pct = int(parseInt64(r["cnt"]) * 100 / catTotal)
			}
			threatCategories = append(threatCategories, models.ThreatCategory{
				Name:       parseStr(r["rule_name"]),
				Percentage: pct,
			})
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
			ThreatCategories: threatCategories,
			RecentAgents:     []models.Agent{},
		})
	}
}
