package models

import "time"

type Agent struct {
	AgentID        string    `json:"agent_id"`
	DeploymentID   string    `json:"deployment_id"`
	DeploymentName string    `json:"deployment_name"`
	LastSeen       time.Time `json:"last_seen"`
	EventCount     int       `json:"event_count"`
	SessionCount   int       `json:"session_count"`
}

type Threat struct {
	ID        string `json:"id"`
	Severity  string `json:"severity"` // Critical, Warning, Info
	Name      string `json:"name"`
	Vector    string `json:"vector"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Timestamp string `json:"timestamp"`
	TimeAgo   string `json:"time_ago"`
	Category  string `json:"category"`
}

type ThreatCategory struct {
	Name       string `json:"name"`
	Percentage int    `json:"percentage"`
}

type KPIData struct {
	TotalAgents         int     `json:"total_agents"`
	AgentsDelta         string  `json:"agents_delta"`
	ActiveThreats       int     `json:"active_threats"`
	CriticalEscalations int     `json:"critical_escalations"`
	TotalCost24h        string  `json:"total_cost_24h"`
	CostStatus          string  `json:"cost_status"`
	ComplianceCoverage  float64 `json:"compliance_coverage"`
	ComplianceLabel     string  `json:"compliance_label"`
}

type DashboardResponse struct {
	KPIs             KPIData          `json:"kpis"`
	LiveThreatFeed   []Threat         `json:"live_threat_feed"`
	ThreatCategories []ThreatCategory `json:"threat_categories"`
	RecentAgents     []Agent          `json:"recent_agents"`
}

type AgentListResponse struct {
	Agents []Agent `json:"agents"`
	Total  int     `json:"total"`
	Page   int     `json:"page"`
	Limit  int     `json:"limit"`
}

// Session represents one chat/API/cron/sub-agent session for an agent.
type Session struct {
	SessionID   string    `json:"session_id"`
	SessionKey  string    `json:"session_key"`
	SessionType string    `json:"session_type"`
	RunCount    int       `json:"run_count"`
	EventCount  int       `json:"event_count"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// Run represents one user→agent turn within a session.
type Run struct {
	RunID      string    `json:"run_id"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	EventCount int       `json:"event_count"`
	Model      string    `json:"model"`
	ToolCount  int       `json:"tool_count"`
	HasBlock   bool      `json:"has_block"`
}

// SessionDetail is returned by GET /agents/:id/sessions/:session_id.
type SessionDetail struct {
	SessionID   string `json:"session_id"`
	SessionKey  string `json:"session_key"`
	SessionType string `json:"session_type"`
	Runs        []Run  `json:"runs"`
}

// HookEvent is one row returned by GET /agents/:id/events.
type HookEvent struct {
	HookName     string    `json:"hook_name"`
	EventTime    time.Time `json:"event_time"`
	RunID        string    `json:"run_id"`
	SessionID    string    `json:"session_id"`
	ToolName     string    `json:"tool_name"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	HookDecision string    `json:"hook_decision"`
	BlockReason  string    `json:"block_reason"`
	EventData    string    `json:"event_data"`
}

// EventsResponse is returned by GET /agents/:id/events.
type EventsResponse struct {
	Events []HookEvent `json:"events"`
	Total  int         `json:"total"`
	Page   int         `json:"page"`
	Limit  int         `json:"limit"`
}

type ThreatListResponse struct {
	Threats []Threat `json:"threats"`
	Total   int      `json:"total"`
	Page    int      `json:"page"`
	Limit   int      `json:"limit"`
}

type DailySpend struct {
	Day    string  `json:"day"`
	Amount float64 `json:"amount"`
}

type TokenBreakdown struct {
	Label      string  `json:"label"`
	Percentage int     `json:"percentage"`
	Amount     float64 `json:"amount"`
}

type AgentModelCost struct {
	Model     string  `json:"model"`
	AgentName string  `json:"agent_name"`
	Cost      float64 `json:"cost"`
	Requests  string  `json:"requests"`
}

type BudgetPolicy struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Trigger     string `json:"trigger"`
	Enforcement string `json:"enforcement"`
	Status      string `json:"status"` // active, paused
}

type CostAnomaly struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	TimeAgo     string `json:"time_ago"`
	Severity    string `json:"severity"` // error, warning, info
}

type CostResponse struct {
	MonthToDate     float64          `json:"month_to_date"`
	Forecasted      float64          `json:"forecasted"`
	AvgTokenCost    float64          `json:"avg_token_cost"`
	PolicyBreaches  int              `json:"policy_breaches"`
	DailySpend      []DailySpend     `json:"daily_spend"`
	TokenBreakdown  []TokenBreakdown `json:"token_breakdown"`
	CacheSavings    float64          `json:"cache_savings"`
	WastedSpend     float64          `json:"wasted_spend"`
	AgentModelCosts []AgentModelCost `json:"agent_model_costs"`
	BudgetPolicies  []BudgetPolicy   `json:"budget_policies"`
	Anomalies       []CostAnomaly    `json:"anomalies"`
}

type RulePack struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Enabled     bool   `json:"enabled"`
	Version     string `json:"version"`
	Blocks24h   int    `json:"blocks_24h"`
}

type EnforcementEvent struct {
	RuleName string `json:"rule_name"`
	Message  string `json:"message"`
	Source   string `json:"source"`
	TimeAgo  string `json:"time_ago"`
	Severity string `json:"severity"`
}

type RulesResponse struct {
	RulePacks         []RulePack         `json:"rule_packs"`
	TotalBlocks24h    int                `json:"total_blocks_24h"`
	ActiveRuleDensity int                `json:"active_rule_density"`
	RecentEnforcement []EnforcementEvent `json:"recent_enforcement"`
}

type QueryResult struct {
	Columns []string            `json:"columns"`
	Rows    []map[string]string `json:"rows"`
	Total   int                 `json:"total"`
}

type TableInfo struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Columns []string `json:"columns"`
}

type Integration struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"` // admin, analyst, viewer
}

type SettingsResponse struct {
	OrgName        string        `json:"org_name"`
	OtelEndpoint   string        `json:"otel_endpoint"`
	RetentionDays  int           `json:"retention_days"`
	ContentCapture bool          `json:"content_capture"`
	Integrations   []Integration `json:"integrations"`
	Users          []User        `json:"users"`
}
