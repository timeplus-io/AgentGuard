package models

import "time"

// ── Metrics models ────────────────────────────────────────────────────────────

// MetricsPeriod describes the requested time range (both fields are RFC3339).
type MetricsPeriod struct {
	Since string `json:"since,omitempty"`
	Until string `json:"until,omitempty"`
}

// MetricsFilters echoes back the active dimension filters in the response.
type MetricsFilters struct {
	AgentID  string `json:"agent_id,omitempty"`
	Model    string `json:"model,omitempty"`
	ToolName string `json:"tool_name,omitempty"`
}

// TokenStats is cumulative token usage for LLM calls.
type TokenStats struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	Total      int64 `json:"total"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// LLMCallStats is the success/fail breakdown for LLM API calls.
// SuccessRate is nil when there are no calls (avoids a misleading 100%).
type LLMCallStats struct {
	Total       int      `json:"total"`
	Success     int      `json:"success"`
	Failed      int      `json:"failed"`
	SuccessRate *float64 `json:"success_rate"`
}

// LatencyStats holds percentile and summary latency values (in milliseconds).
type LatencyStats struct {
	P50     float64 `json:"p50"`
	P95     float64 `json:"p95"`
	P99     float64 `json:"p99"`
	Avg     float64 `json:"avg"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Samples int     `json:"samples"`
}

// LLMThroughputBucket is one time-bucket in the throughput time series.
type LLMThroughputBucket struct {
	Bucket       time.Time `json:"bucket"`
	LLMCalls     int       `json:"llm_calls"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	TotalTokens  int64     `json:"total_tokens"`
}

// LLMMetrics is returned by GET /api/metrics/llm.
type LLMMetrics struct {
	Period      MetricsPeriod         `json:"period"`
	Filters     MetricsFilters        `json:"filters"`
	Tokens      TokenStats            `json:"tokens"`
	Calls       LLMCallStats          `json:"calls"`
	StopReasons map[string]int        `json:"stop_reasons"`
	LatencyMs   LatencyStats          `json:"latency_ms"`
	Throughput  []LLMThroughputBucket `json:"throughput"`
}

// ToolCallStats is the success/fail summary for tool calls.
// SuccessRate is nil when there are no calls (avoids a misleading 100%).
type ToolCallStats struct {
	TotalCalls   int      `json:"total_calls"`
	SuccessCalls int      `json:"success_calls"`
	FailedCalls  int      `json:"failed_calls"`
	SuccessRate  *float64 `json:"success_rate"`
}

// ToolStat is per-tool metrics including count, success/fail, and latency.
// SuccessRate is nil when there are no calls.
type ToolStat struct {
	ToolName     string       `json:"tool_name"`
	TotalCalls   int          `json:"total_calls"`
	SuccessCalls int          `json:"success_calls"`
	FailedCalls  int          `json:"failed_calls"`
	SuccessRate  *float64     `json:"success_rate"`
	LatencyMs    LatencyStats `json:"latency_ms"`
}

// ToolMetrics is returned by GET /api/metrics/tools.
type ToolMetrics struct {
	Period  MetricsPeriod  `json:"period"`
	Filters MetricsFilters `json:"filters"`
	Summary ToolCallStats  `json:"summary"`
	ByTool  []ToolStat     `json:"by_tool"`
}

// ─────────────────────────────────────────────────────────────────────────────

type Agent struct {
	AgentID        string    `json:"agent_id"`
	DeploymentID   string    `json:"deployment_id"`
	DeploymentName string    `json:"deployment_name"`
	LastSeen       time.Time `json:"last_seen"`
	EventCount     int       `json:"event_count"`
	SessionCount   int       `json:"session_count"`
	RunCount       int       `json:"run_count"`
	ToolCallCount  int       `json:"tool_call_count"`
	TotalTokens    int64     `json:"total_tokens"`
	LLMCallCount   int       `json:"llm_call_count"`
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
	TotalAgents         int    `json:"total_agents"`
	AgentsDelta         string `json:"agents_delta"`
	ActiveThreats       int    `json:"active_threats"`
	CriticalEscalations int    `json:"critical_escalations"`
	TotalCost24h        string `json:"total_cost_24h"`
	CostStatus          string `json:"cost_status"`
	ActiveRules         int    `json:"active_rules"`
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
	Tokens     int64   `json:"tokens"`
	Amount     float64 `json:"amount"`
}

type AgentModelCost struct {
	Model        string  `json:"model"`
	AgentName    string  `json:"agent_name"`
	Cost         float64 `json:"cost"`
	Requests     string  `json:"requests"`
	Provider     string  `json:"provider"`
	AgentID      string  `json:"agent_id"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheTokens  int64   `json:"cache_tokens"`
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

// PricingEntry is one row in the agentshield_pricing mutable stream.
type PricingEntry struct {
	Provider                string  `json:"provider"`
	Model                   string  `json:"model"`
	InputCostPerMillion     float64 `json:"input_cost_per_million"`
	OutputCostPerMillion    float64 `json:"output_cost_per_million"`
	CacheReadCostPerMillion float64 `json:"cache_read_cost_per_million"`
	UpdatedAt               string  `json:"updated_at"`
}

// PricingListResponse is returned by GET /api/cost/pricing.
type PricingListResponse struct {
	Pricing []PricingEntry `json:"pricing"`
}

type RulePack struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Version           string `json:"version"`
	Description       string `json:"description"`
	QuerySQL          string `json:"query_sql"`
	Enabled           bool   `json:"enabled"`
	Severity          string `json:"severity"`
	Category          string `json:"category"`
	RequiredTelemetry string `json:"required_telemetry"`
	FPRate            string `json:"fp_rate"`
	HookSources       string `json:"hook_sources"`
	Detections24h     int    `json:"detections_24h"`
	LatestVersion     string `json:"latest_version,omitempty"`
}

type RulesResponse struct {
	RulePacks          []RulePack      `json:"rule_packs"`
	TotalDetections24h int             `json:"total_detections_24h"`
	ActiveRuleDensity  int             `json:"active_rule_density"`
	RecentDetections   []SecurityEvent `json:"recent_detections"`
}

type SecurityEvent struct {
	RuleID     string `json:"rule_id"`
	RuleName   string `json:"rule_name"`
	Severity   string `json:"severity"`
	AgentID    string `json:"agent_id"`
	HookName   string `json:"hook_name"`
	Message    string `json:"message"`
	DetectedAt string `json:"detected_at"`
}

type RuleDetailResponse struct {
	RulePack
	RecentDetections []SecurityEvent `json:"recent_detections"`
}

type AlertWebhookPayload struct {
	RuleID     string `json:"rule_id"`
	RuleName   string `json:"rule_name"`
	Severity   string `json:"severity"`
	AgentID    string `json:"agent_id"`
	Message    string `json:"message"`
	DetectedAt string `json:"detected_at"`
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
