package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/mock"
	"github.com/timeplus/agentshield/internal/models"
)

// packageRules is a mutable copy of mock rules that persists toggle state during server session
var packageRules []models.RulePack

func init() {
	packageRules = make([]models.RulePack, len(mock.Rules))
	copy(packageRules, mock.Rules)
}

func ListRules(c *gin.Context) {
	totalBlocks := 0
	activeCount := 0
	for _, r := range packageRules {
		totalBlocks += r.Blocks24h
		if r.Enabled {
			activeCount++
		}
	}
	// Active rule density: percentage of rules enabled
	density := 0
	if len(packageRules) > 0 {
		density = activeCount * 100 / len(packageRules)
	}

	resp := models.RulesResponse{
		RulePacks:         packageRules,
		TotalBlocks24h:    totalBlocks,
		ActiveRuleDensity: density,
		RecentEnforcement: mock.EnforcementEvents,
	}

	c.JSON(http.StatusOK, resp)
}

func ToggleRule(c *gin.Context) {
	id := c.Param("id")
	for i := range packageRules {
		if packageRules[i].ID == id {
			packageRules[i].Enabled = !packageRules[i].Enabled
			c.JSON(http.StatusOK, packageRules[i])
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
}
