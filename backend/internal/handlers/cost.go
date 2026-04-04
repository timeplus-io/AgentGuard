package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/mock"
	"github.com/timeplus/agentshield/internal/models"
)

func GetCost(c *gin.Context) {
	// Compute month-to-date from daily spend
	mtd := 0.0
	for _, d := range mock.DailySpend {
		mtd += d.Amount
	}

	// Forecast: extrapolate based on avg daily spend over full month
	avgDaily := mtd / float64(len(mock.DailySpend))
	forecasted := avgDaily * 30

	// Total token amounts
	totalTokenAmount := 0.0
	for _, tb := range mock.TokenBreakdown {
		totalTokenAmount += tb.Amount
	}
	avgTokenCost := totalTokenAmount / 14000.0 // avg per thousand tokens

	resp := models.CostResponse{
		MonthToDate:     mtd,
		Forecasted:      forecasted,
		AvgTokenCost:    avgTokenCost,
		PolicyBreaches:  2,
		DailySpend:      mock.DailySpend,
		TokenBreakdown:  mock.TokenBreakdown,
		CacheSavings:    1247.60,
		WastedSpend:     342.80,
		AgentModelCosts: mock.AgentModelCosts,
		BudgetPolicies:  mock.BudgetPolicies,
		Anomalies:       mock.CostAnomalies,
	}

	c.JSON(http.StatusOK, resp)
}
