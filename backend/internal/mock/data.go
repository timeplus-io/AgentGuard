package mock

import "github.com/timeplus/agentshield/internal/models"

var Agents = []models.Agent{}

var Threats = []models.Threat{}

var DailySpend = []models.DailySpend{
	{Day: "Mar 29", Amount: 1842.50},
	{Day: "Mar 30", Amount: 2105.75},
	{Day: "Mar 31", Amount: 1987.30},
	{Day: "Apr 01", Amount: 2341.00},
	{Day: "Apr 02", Amount: 2890.45},
	{Day: "Apr 03", Amount: 3102.80},
	{Day: "Apr 04", Amount: 1456.20},
}

var TokenBreakdown = []models.TokenBreakdown{
	{Label: "Input Tokens", Percentage: 42, Amount: 5880.00},
	{Label: "Output Tokens", Percentage: 48, Amount: 6720.00},
	{Label: "Cache Tokens", Percentage: 10, Amount: 1400.00},
}

var AgentModelCosts = []models.AgentModelCost{
	{Model: "claude-3-5-sonnet", AgentName: "AS-ALPHA-3030", Cost: 3210.75, Requests: "14.2K"},
	{Model: "claude-3-5-sonnet", AgentName: "AS-LAMBDA-4456", Cost: 2890.30, Requests: "12.1K"},
	{Model: "gpt-4-turbo", AgentName: "AS-OMEGA-1102", Cost: 2105.00, Requests: "8.7K"},
	{Model: "claude-3-haiku", AgentName: "AS-ETA-8834", Cost: 1567.80, Requests: "21.4K"},
	{Model: "claude-3-5-sonnet", AgentName: "AS-DELTA-9012", Cost: 1204.50, Requests: "5.3K"},
	{Model: "gpt-4o", AgentName: "AS-GAMMA-5503", Cost: 890.10, Requests: "3.9K"},
	{Model: "claude-3-haiku", AgentName: "AS-IOTA-3309", Cost: 890.00, Requests: "11.2K"},
	{Model: "gpt-4o-mini", AgentName: "AS-SIGMA-4421", Cost: 842.30, Requests: "28.6K"},
}

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
