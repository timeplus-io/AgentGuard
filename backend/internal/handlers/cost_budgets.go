package handlers

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// deploymentIDRe limits deployment_id characters to a safe alphabet for SQL
// embedding. The same alphabet as pricingKeyRe in cost.go.
var deploymentIDRe = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)

// budgetOnTargetThresholdPct is the +/- band considered "on budget" rather than
// "over" or "under". Matches industry SOAR conventions of tolerating small
// forecast noise around the configured target.
const budgetOnTargetThresholdPct = 5.0

// GetCostBudgets handles GET /api/cost/budget.
// Returns the current set of active per-deployment monthly budgets,
// reconstructed from agentguard_cost_budgets via arg_max(..., updated_at).
func GetCostBudgets(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)

		// Idempotent: existing deployments that completed setup before budgets
		// shipped need the stream created on first use.
		if err := tp.EnsureCostBudgetsStream(&cfg.Timeplus); err != nil {
			log.Printf("[cost] EnsureCostBudgetsStream failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		rows, err := tp.Query(&cfg.Timeplus, `
SELECT deployment_id,
       arg_max(monthly_budget, updated_at) AS monthly_budget,
       arg_max(currency,       updated_at) AS currency,
       arg_max(updated_by,     updated_at) AS updated_by,
       arg_max(deleted,        updated_at) AS deleted,
       max(updated_at)                     AS last_updated
FROM table(agentguard_cost_budgets)
GROUP BY deployment_id
HAVING NOT deleted
ORDER BY deployment_id`)
		if err != nil {
			log.Printf("[cost] GetCostBudgets query failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		budgets := make([]models.CostBudget, 0, len(rows))
		for _, r := range rows {
			budgets = append(budgets, models.CostBudget{
				DeploymentID:  parseStr(r["deployment_id"]),
				MonthlyBudget: parseFloat64(r["monthly_budget"]),
				Currency:      parseStr(r["currency"]),
				UpdatedBy:     parseStr(r["updated_by"]),
				UpdatedAt:     parseStr(r["last_updated"]),
			})
		}
		c.JSON(http.StatusOK, models.CostBudgetsResponse{Budgets: budgets})
	}
}

// CostBudgetBody is the POST body for upserting a budget.
type CostBudgetBody struct {
	DeploymentID  string  `json:"deployment_id"`
	MonthlyBudget float64 `json:"monthly_budget"`
	Currency      string  `json:"currency"`
	Deleted       bool    `json:"deleted"`
}

// UpsertCostBudget handles POST /api/cost/budget.
// Appends a new row to agentguard_cost_budgets. updated_by is the logged-in
// Timeplus username from the session context — see auth.AuthRequired.
func UpsertCostBudget(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)

		var body CostBudgetBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "deployment_id and monthly_budget are required"})
			return
		}
		body.DeploymentID = strings.TrimSpace(body.DeploymentID)
		if body.DeploymentID == "" || !deploymentIDRe.MatchString(body.DeploymentID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment_id"})
			return
		}
		if body.MonthlyBudget < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "monthly_budget must be >= 0"})
			return
		}
		currency := strings.ToUpper(strings.TrimSpace(body.Currency))
		if currency == "" {
			currency = "USD"
		}

		actor := ""
		if v, ok := c.Get("timeplus_user"); ok {
			if s, ok := v.(string); ok {
				actor = s
			}
		}

		if err := tp.EnsureCostBudgetsStream(&cfg.Timeplus); err != nil {
			log.Printf("[cost] UpsertCostBudget ensure failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		sql := fmt.Sprintf(`
INSERT INTO agentguard_cost_budgets
  (deployment_id, monthly_budget, currency, updated_by, deleted, updated_at)
VALUES
  ('%s', %f, '%s', '%s', %t, now64(3))`,
			escapeSQLStr(body.DeploymentID),
			body.MonthlyBudget,
			escapeSQLStr(currency),
			escapeSQLStr(actor),
			body.Deleted,
		)
		if err := tp.ExecHTTP(&cfg.Timeplus, sql); err != nil {
			log.Printf("[cost] UpsertCostBudget insert failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upsert failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// queryForecastVsBudget reads the sum of active per-deployment budgets and
// derives the forecast position. Returns (nil, nil, "") when no budget is
// configured — the frontend treats that as "render no delta on Forecasted
// Total".
//
// The +/- 5% band around the budget total is the "on budget" zone; anything
// past it is classified "over" or "under". Frontend uses budgetStatus to pick
// the rendering template.
func queryForecastVsBudget(cfg *config.TimeplusConfig, forecasted float64) (*float64, *float64, string, error) {
	if err := tp.EnsureCostBudgetsStream(cfg); err != nil {
		return nil, nil, "", err
	}
	rows, err := tp.Query(cfg, `
SELECT sum(monthly_budget) AS total_budget
FROM (
  SELECT deployment_id,
         arg_max(monthly_budget, updated_at) AS monthly_budget,
         arg_max(deleted,        updated_at) AS deleted
  FROM table(agentguard_cost_budgets)
  GROUP BY deployment_id
  HAVING NOT deleted
)`)
	if err != nil {
		return nil, nil, "", err
	}
	if len(rows) == 0 {
		return nil, nil, "", nil
	}
	total := parseFloat64(rows[0]["total_budget"])
	if total <= 0 {
		return nil, nil, "", nil
	}
	deltaPct := (forecasted - total) / total * 100.0
	return &total, &deltaPct, classifyBudgetStatus(deltaPct), nil
}

// classifyBudgetStatus returns "over", "under", or "on" based on the percent
// of budget the forecast represents. The +/- 5% band around target is "on" —
// noise tolerance so small drift doesn't flicker between status labels.
func classifyBudgetStatus(deltaPct float64) string {
	switch {
	case deltaPct > budgetOnTargetThresholdPct:
		return "over"
	case deltaPct < -budgetOnTargetThresholdPct:
		return "under"
	default:
		return "on"
	}
}
