package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/mock"
	"github.com/timeplus/agentshield/internal/models"
)

func GetDashboard(c *gin.Context) {
	// Compute KPIs from mock data
	totalAgents := len(mock.Agents)

	criticalThreats := 0
	for _, t := range mock.Threats {
		if t.Severity == "Critical" {
			criticalThreats++
		}
	}

	// Threat categories
	categoryCount := map[string]int{}
	for _, t := range mock.Threats {
		categoryCount[t.Category]++
	}
	total := len(mock.Threats)
	categories := []models.ThreatCategory{}
	order := []string{"Prompt Injection", "Data Exfiltration", "Privilege Escalation", "Memory Poisoning", "Supply Chain"}
	for _, name := range order {
		count := categoryCount[name]
		if count > 0 {
			pct := int(float64(count) / float64(total) * 100)
			categories = append(categories, models.ThreatCategory{Name: name, Percentage: pct})
		}
	}

	// Latest 10 threats
	liveFeed := mock.Threats
	if len(liveFeed) > 10 {
		liveFeed = liveFeed[:10]
	}

	// 5 most recent agents
	recentAgents := mock.Agents
	if len(recentAgents) > 5 {
		recentAgents = recentAgents[:5]
	}

	resp := models.DashboardResponse{
		KPIs: models.KPIData{
			TotalAgents:         totalAgents,
			AgentsDelta:         "",
			ActiveThreats:       len(mock.Threats),
			CriticalEscalations: criticalThreats,
			TotalCost24h:        "$0.00",
			CostStatus:          "within budget",
			ComplianceCoverage:  94.7,
			ComplianceLabel:     "SOC2 + HIPAA",
		},
		LiveThreatFeed:   liveFeed,
		ThreatCategories: categories,
		RecentAgents:     recentAgents,
	}

	c.JSON(http.StatusOK, resp)
}
