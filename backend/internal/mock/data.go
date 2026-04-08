package mock

import "github.com/timeplus/agentshield/internal/models"

var Agents = []models.Agent{}

var Threats = []models.Threat{
	{ID: "THR-001", Severity: "Critical", Name: "Prompt Injection via User Input", Vector: "user_message", AgentID: "AS-OMEGA-1102", SessionID: "sess-9f3a2", Timestamp: "2026-04-04T10:42:00Z", TimeAgo: "3m ago", Category: "Prompt Injection"},
	{ID: "THR-002", Severity: "Critical", Name: "Data Exfiltration Attempt – PII Leak", Vector: "tool_call", AgentID: "AS-IOTA-3309", SessionID: "sess-7b1c4", Timestamp: "2026-04-04T10:30:00Z", TimeAgo: "15m ago", Category: "Data Exfiltration"},
	{ID: "THR-003", Severity: "Critical", Name: "Privilege Escalation via Tool Abuse", Vector: "tool_result", AgentID: "AS-SIGMA-4421", SessionID: "sess-2d8e9", Timestamp: "2026-04-04T10:15:00Z", TimeAgo: "30m ago", Category: "Privilege Escalation"},
	{ID: "THR-004", Severity: "Warning", Name: "Indirect Prompt Injection in Document", Vector: "file_read", AgentID: "AS-DELTA-9012", SessionID: "sess-1a4f7", Timestamp: "2026-04-04T09:58:00Z", TimeAgo: "47m ago", Category: "Prompt Injection"},
	{ID: "THR-005", Severity: "Warning", Name: "Unusual Token Consumption Pattern", Vector: "llm_response", AgentID: "AS-ZETA-2201", SessionID: "sess-6e3b1", Timestamp: "2026-04-04T09:45:00Z", TimeAgo: "1h ago", Category: "Data Exfiltration"},
	{ID: "THR-006", Severity: "Warning", Name: "Supply Chain Dependency Tampering", Vector: "package_fetch", AgentID: "AS-GAMMA-5503", SessionID: "sess-8c2d5", Timestamp: "2026-04-04T09:30:00Z", TimeAgo: "1h ago", Category: "Supply Chain"},
	{ID: "THR-007", Severity: "Warning", Name: "Memory Poisoning via Context Injection", Vector: "memory_write", AgentID: "AS-ETA-8834", SessionID: "sess-4f7a3", Timestamp: "2026-04-04T09:15:00Z", TimeAgo: "1h ago", Category: "Memory Poisoning"},
	{ID: "THR-008", Severity: "Info", Name: "Rate Limit Policy Triggered", Vector: "api_call", AgentID: "AS-SIGMA-4421", SessionID: "sess-3b9c6", Timestamp: "2026-04-04T09:00:00Z", TimeAgo: "2h ago", Category: "Prompt Injection"},
	{ID: "THR-009", Severity: "Info", Name: "Suspicious File Access Pattern", Vector: "file_read", AgentID: "AS-KAPPA-9981", SessionID: "sess-5d2e8", Timestamp: "2026-04-04T08:45:00Z", TimeAgo: "2h ago", Category: "Data Exfiltration"},
	{ID: "THR-010", Severity: "Critical", Name: "Jailbreak Attempt Detected", Vector: "user_message", AgentID: "AS-OMEGA-1102", SessionID: "sess-0a1b2", Timestamp: "2026-04-04T08:30:00Z", TimeAgo: "2h ago", Category: "Prompt Injection"},
	{ID: "THR-011", Severity: "Warning", Name: "Unauthorized API Endpoint Access", Vector: "http_request", AgentID: "AS-BETA-7721", SessionID: "sess-7c4d1", Timestamp: "2026-04-04T08:15:00Z", TimeAgo: "2h ago", Category: "Privilege Escalation"},
	{ID: "THR-012", Severity: "Critical", Name: "Exfiltration via Encoded Output", Vector: "llm_response", AgentID: "AS-IOTA-3309", SessionID: "sess-2e5f9", Timestamp: "2026-04-04T08:00:00Z", TimeAgo: "3h ago", Category: "Data Exfiltration"},
	{ID: "THR-013", Severity: "Info", Name: "Malicious Package Detected in Deps", Vector: "package_fetch", AgentID: "AS-ALPHA-3030", SessionID: "sess-9d3c7", Timestamp: "2026-04-04T07:45:00Z", TimeAgo: "3h ago", Category: "Supply Chain"},
	{ID: "THR-014", Severity: "Warning", Name: "Cross-Agent Memory Contamination", Vector: "memory_read", AgentID: "AS-THETA-6612", SessionID: "sess-6f1b4", Timestamp: "2026-04-04T07:30:00Z", TimeAgo: "3h ago", Category: "Memory Poisoning"},
	{ID: "THR-015", Severity: "Info", Name: "Compliance Boundary Probe", Vector: "user_message", AgentID: "AS-LAMBDA-4456", SessionID: "sess-3a8e2", Timestamp: "2026-04-04T07:15:00Z", TimeAgo: "3h ago", Category: "Prompt Injection"},
	{ID: "THR-016", Severity: "Critical", Name: "Root Privilege via Shell Escape", Vector: "tool_call", AgentID: "AS-OMEGA-1102", SessionID: "sess-1c7d5", Timestamp: "2026-04-04T07:00:00Z", TimeAgo: "4h ago", Category: "Privilege Escalation"},
	{ID: "THR-017", Severity: "Warning", Name: "Poisoned Vector Store Entry", Vector: "memory_write", AgentID: "AS-ETA-8834", SessionID: "sess-8b2f6", Timestamp: "2026-04-04T06:45:00Z", TimeAgo: "4h ago", Category: "Memory Poisoning"},
	{ID: "THR-018", Severity: "Info", Name: "High Token Burn Rate Observed", Vector: "llm_response", AgentID: "AS-ZETA-2201", SessionID: "sess-4e9a1", Timestamp: "2026-04-04T06:30:00Z", TimeAgo: "4h ago", Category: "Data Exfiltration"},
}

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

var Rules = []models.RulePack{
	{ID: "rp-001", Name: "Prompt Injection Shield", Description: "Detects and blocks direct and indirect prompt injection attacks, jailbreak attempts, and goal hijacking across all message surfaces.", Icon: "shield", Enabled: true, Version: "v2.4.1", Blocks24h: 47},
	{ID: "rp-002", Name: "DLP Sentinel", Description: "Prevents exfiltration of PII, credentials, source code, and other sensitive data through LLM outputs, tool calls, and memory writes.", Icon: "lock", Enabled: true, Version: "v1.8.3", Blocks24h: 12},
	{ID: "rp-003", Name: "Privilege Guard", Description: "Monitors and restricts privilege escalation attempts through tool abuse, shell escapes, and unauthorized API access.", Icon: "key", Enabled: false, Version: "v1.2.0", Blocks24h: 0},
	{ID: "rp-004", Name: "Supply Chain Watch", Description: "Validates package integrity, detects dependency tampering, and flags malicious or unexpected third-party code execution.", Icon: "link", Enabled: true, Version: "v1.5.7", Blocks24h: 3},
	{ID: "rp-005", Name: "Cost Sentinel", Description: "Enforces token budgets, detects anomalous spend patterns, and prevents runaway LLM cost from unbounded agent loops.", Icon: "dollar", Enabled: false, Version: "v1.1.2", Blocks24h: 0},
	{ID: "rp-006", Name: "Compliance Logger", Description: "Ensures comprehensive audit trail coverage for SOC2, HIPAA, and ISO 27001 compliance with tamper-proof event logging.", Icon: "clipboard", Enabled: true, Version: "v3.0.1", Blocks24h: 0},
}

var EnforcementEvents = []models.EnforcementEvent{
	{RuleName: "Prompt Injection Shield", Message: "Blocked jailbreak attempt on AS-OMEGA-1102 via crafted system prompt override", Source: "AS-OMEGA-1102", TimeAgo: "3m ago", Severity: "critical"},
	{RuleName: "DLP Sentinel", Message: "Prevented PII exfiltration: SSN pattern detected in tool output for AS-IOTA-3309", Source: "AS-IOTA-3309", TimeAgo: "15m ago", Severity: "critical"},
	{RuleName: "Prompt Injection Shield", Message: "Indirect injection via uploaded PDF neutralized on AS-DELTA-9012", Source: "AS-DELTA-9012", TimeAgo: "47m ago", Severity: "warning"},
	{RuleName: "Supply Chain Watch", Message: "Flagged npm package 'lodash-utils-v2' with known malicious payload on AS-GAMMA-5503", Source: "AS-GAMMA-5503", TimeAgo: "1h ago", Severity: "warning"},
	{RuleName: "DLP Sentinel", Message: "Source code exfiltration blocked: 3 files matched proprietary code patterns", Source: "AS-SIGMA-4421", TimeAgo: "2h ago", Severity: "warning"},
	{RuleName: "Compliance Logger", Message: "Audit record created for all 18 threat events in the last 24h", Source: "global", TimeAgo: "6h ago", Severity: "info"},
}

var MockSettings = models.SettingsResponse{
	OrgName:        "Timeplus Security",
	OtelEndpoint:   "https://otel.timeplus.io:4317",
	RetentionDays:  30,
	ContentCapture: true,
	Integrations: []models.Integration{
		{ID: "int-001", Name: "Slack", Type: "alerting", Enabled: true, URL: "https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"},
		{ID: "int-002", Name: "PagerDuty", Type: "incident", Enabled: false, URL: "https://events.pagerduty.com/v2/enqueue"},
	},
	Users: []models.User{
		{ID: "usr-001", Name: "Alice Chen", Email: "alice@timeplus.io", Role: "admin"},
		{ID: "usr-002", Name: "Bob Martinez", Email: "bob@timeplus.io", Role: "analyst"},
		{ID: "usr-003", Name: "Carol Kim", Email: "carol@timeplus.io", Role: "analyst"},
		{ID: "usr-004", Name: "David Osei", Email: "david@timeplus.io", Role: "viewer"},
	},
}
