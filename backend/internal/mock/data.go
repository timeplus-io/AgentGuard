package mock

import "github.com/timeplus/agentshield/internal/models"

var BudgetPolicies = []models.BudgetPolicy{
	{ID: "bp-001", Name: "Daily Spend Cap", Trigger: "Daily spend > $5,000", Enforcement: "Alert + Throttle", Status: "active"},
	{ID: "bp-002", Name: "Per-Agent Budget", Trigger: "Agent cost > $3,500/month", Enforcement: "Suspend agent", Status: "active"},
	{ID: "bp-003", Name: "Token Rate Limit", Trigger: "Token burn > 50K/min", Enforcement: "Rate limit 50%", Status: "paused"},
}

var CostAnomalies = []models.CostAnomaly{
	{ID: "ca-001", Title: "Spike in Output Token Usage", Description: "AS-ALPHA-3030 consumed 3x normal output tokens between 02:00–04:00 UTC", TimeAgo: "6h ago", Severity: "error"},
	{ID: "ca-002", Title: "Unexpected Model Upgrade", Description: "AS-SIGMA-4421 switched from claude-3-haiku to claude-3-5-sonnet without policy approval", TimeAgo: "1d ago", Severity: "warning"},
	{ID: "ca-003", Title: "Cache Hit Rate Drop", Description: "Global cache hit rate dropped from 68% to 10%, increasing costs by ~$420", TimeAgo: "2d ago", Severity: "warning"},
}
