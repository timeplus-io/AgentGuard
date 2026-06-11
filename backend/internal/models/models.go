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
	AgentType      string    `json:"agent_type"` // "claudecode" | "openclaw" | ""
	Status         string    `json:"status"`     // "active" | "idle"
	LastSeen       time.Time `json:"last_seen"`
	EventCount     int       `json:"event_count"`
	SessionCount   int       `json:"session_count"`
	RunCount       int       `json:"run_count"`
	ToolCallCount  int       `json:"tool_call_count"`
	TotalTokens    int64     `json:"total_tokens"`
	LLMCallCount   int       `json:"llm_call_count"`
}

// ThreatEvent is one raw security event belonging to a deduplicated threat.
type ThreatEvent struct {
	DetectedAt string `json:"detected_at"`
	RunID      string `json:"run_id"`
	HookName   string `json:"hook_name"`
	ToolName   string `json:"tool_name"`
	Message    string `json:"message"`
	EventData  string `json:"event_data"`
}

// Threat is a deduplicated security threat keyed by (agent_id, deployment_id, session_id, rule_id).
// ThreatID is computed in Go (MD5 of the four key fields) and is not stored in Proton.
// Status lifecycle: "open" → "acknowledged" → "cleared" (auto-reopens to "open" on new events).
type Threat struct {
	ThreatID        string        `json:"threat_id"`
	AgentID         string        `json:"agent_id"`
	DeploymentID    string        `json:"deployment_id"`
	SessionID       string        `json:"session_id"`
	RuleID          string        `json:"rule_id"`
	RuleName        string        `json:"rule_name"`
	Severity        string        `json:"severity"`
	EventCount      int64         `json:"event_count"`
	FirstSeen       string        `json:"first_seen"`
	LastSeen        string        `json:"last_seen"`
	Status          string        `json:"status"` // "open", "acknowledged", "cleared"
	StatusUpdatedAt string        `json:"status_updated_at,omitempty"`
	Events          []ThreatEvent `json:"events"`
}

// Hold is one row from agentguard_holds (current state computed inline via
// arg_max(field, updated_at) GROUP BY hold_id at query time).
type Hold struct {
	HoldID        string    `json:"hold_id"`
	DeploymentID  string    `json:"deployment_id"`
	AgentID       string    `json:"agent_id"`
	SessionID     string    `json:"session_id"`
	RunID         string    `json:"run_id"`
	ToolName      string    `json:"tool_name"`
	RuleID        string    `json:"rule_id"`
	Severity      string    `json:"severity"`
	ArgsSummary   string    `json:"args_summary"`
	Message       string    `json:"message"`
	CreatedAt     time.Time `json:"created_at"`
	Status        string    `json:"status"` // pending|approved|denied|timeout|abandoned
	DecidedBy     string    `json:"decided_by,omitempty"`
	DecidedAt     time.Time `json:"decided_at,omitempty"`
	DecidedReason string    `json:"decided_reason,omitempty"`
}

// ActivityEvent is one row returned by GET /api/activity.
// It is sourced from agentguard_holds (is_hold_event=true) or from
// agentguard_hook_events when include_allowed=true (is_hold_event=false).
type ActivityEvent struct {
	ID            string     `json:"id"`           // hold_id for hold rows; synthetic hex for hook-only rows
	EventTime     time.Time  `json:"event_time"`
	ToolName      string     `json:"tool_name"`
	Status        string     `json:"status"`       // pending|approved|denied|timeout|abandoned|auto_block|allowed
	RuleID        string     `json:"rule_id"`
	RuleName      string     `json:"rule_name"`
	Severity      string     `json:"severity"`
	AgentID       string     `json:"agent_id"`
	SessionID     string     `json:"session_id"`
	RunID         string     `json:"run_id"`
	ArgsSummary   string     `json:"args_summary"`
	Message       string     `json:"message"`
	DecidedBy     string     `json:"decided_by,omitempty"`
	DecidedAt     *time.Time `json:"decided_at,omitempty"`
	DecidedReason string     `json:"decided_reason,omitempty"`
	HookPayload   string     `json:"hook_payload,omitempty"` // raw JSON; populated for hook-only rows
	IsHoldEvent   bool       `json:"is_hold_event"`          // true when sourced from agentguard_holds
}

// ActivityResponse is returned by GET /api/activity.
type ActivityResponse struct {
	Events []ActivityEvent `json:"events"`
	Total  int             `json:"total"`
}

// HoldRequest is the JSON body the plugin sends to POST /api/holds.
type HoldRequest struct {
	DeploymentID   string                 `json:"deployment_id"`
	AgentID        string                 `json:"agent_id"`
	SessionID      string                 `json:"session_id"`
	RunID          string                 `json:"run_id"`
	ToolName       string                 `json:"tool_name"`
	ToolInput      map[string]interface{} `json:"tool_input"`
	EventTime      time.Time              `json:"event_time"`
	HookPayload    map[string]interface{} `json:"hook_payload"`
	AgentType      string                 `json:"agent_type"`      // "claudecode" | "openclaw" — defaults to "claudecode" if empty for backward compat
	DeploymentName string                 `json:"deployment_name"` // optional, populated when known
}

// HoldResponse is what the plugin receives from POST /api/holds.
type HoldResponse struct {
	Decision string `json:"decision"`          // "allow" | "block"
	Reason   string `json:"reason,omitempty"`
	HoldID   string `json:"hold_id,omitempty"` // populated when a hold record was opened
}

// HoldDecideRequest is the JSON body the frontend sends to POST /api/holds/:id/decide.
type HoldDecideRequest struct {
	Decision      string `json:"decision"` // "approved" | "denied"
	DecidedBy     string `json:"decided_by"`
	DecidedReason string `json:"decided_reason,omitempty"`
}

// PendingHold is embedded in ThreatDetailResponse when a hold is currently
// pending for the threat's rule + session. Used by the threat detail page
// to render Approve/Deny controls.
type PendingHold struct {
	HoldID      string    `json:"hold_id"`
	ToolName    string    `json:"tool_name"`
	ArgsSummary string    `json:"args_summary"`
	CreatedAt   time.Time `json:"created_at"`
}

// ThreatDetailResponse is returned by GET /api/threats/:id.
type ThreatDetailResponse struct {
	Threat
	PendingHold *PendingHold `json:"pending_hold,omitempty"`
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
	Status      string    `json:"status"` // "active" | "ended"
	Model       string    `json:"model"`
	Provider    string    `json:"provider"`
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
	SessionID      string `json:"session_id"`
	SessionKey     string `json:"session_key"`
	SessionType    string `json:"session_type"`
	Status         string `json:"status"` // "active" | "ended"
	Model          string `json:"model"`
	Provider       string `json:"provider"`
	TotalToolCalls int    `json:"total_tool_calls"`
	Runs           []Run  `json:"runs"`
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
	AgentType    string    `json:"agent_type"`
}

// EventsResponse is returned by GET /agents/:id/events.
type EventsResponse struct {
	Events []HookEvent `json:"events"`
	Total  int         `json:"total"`
	Page   int         `json:"page"`
	Limit  int         `json:"limit"`
}

// TraceSpan is one node in the run trace waterfall.
// Kind is "run" | "llm" | "tool".
// Status is "ok" | "error" | "blocked".
// ParentID is "" for the root run span; run_id for all children.
type TraceSpan struct {
	SpanID     string            `json:"span_id"`
	ParentID   string            `json:"parent_id"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	StartMs    int64             `json:"start_ms"`
	DurationMs int64             `json:"duration_ms"`
	Status     string            `json:"status"`
	Attributes map[string]string `json:"attributes"`
}

// TraceResponse is returned by GET /api/agents/:id/sessions/:sid/runs/:rid/trace.
// Spans are sorted by StartMs ascending; root span always has ParentID == "".
type TraceResponse struct {
	Spans []TraceSpan `json:"spans"`
}

// TimelineEvent is one normalized item in the session timeline, built from
// agentguard_cim_event. Kind is one of:
//   "user_input" | "llm_response" | "tool_request" | "tool_result"
//   | "session_start" | "session_end".
// Status is "ok" | "blocked" | "error".
type TimelineEvent struct {
	EventTime  time.Time         `json:"event_time"`
	Kind       string            `json:"kind"`
	TurnIndex  int               `json:"turn_index"`
	RunID      string            `json:"run_id"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	Title      string            `json:"title"`
	Body       string            `json:"body"`
	ToolName   string            `json:"tool_name,omitempty"`
	Model      string            `json:"model,omitempty"`
	Provider   string            `json:"provider,omitempty"`
	Status     string            `json:"status"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// TimelineResponse is returned by GET /api/agents/:id/sessions/:session_id/timeline.
// Events are ordered by EventTime ascending.
type TimelineResponse struct {
	Events []TimelineEvent `json:"events"`
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

type CostResponse struct {
	MonthToDate     float64 `json:"month_to_date"`
	MonthToDatePrev float64 `json:"month_to_date_prev"`
	// MonthToDateDeltaPct is the percent change vs the prior month-to-date
	// window of the same elapsed duration. Nil when the prior denominator is
	// zero — the frontend treats nil as "no delta to display".
	MonthToDateDeltaPct *float64 `json:"month_to_date_delta_pct,omitempty"`

	Forecasted float64 `json:"forecasted"`
	// ForecastBudgetDeltaPct is the percent of the configured monthly budget
	// the projected month-end total represents (signed: positive = over budget).
	// Nil when no budget is configured.
	ForecastBudgetDeltaPct *float64 `json:"forecast_budget_delta_pct,omitempty"`
	// BudgetAmount is the sum of currently-active per-deployment monthly
	// budgets, in the canonical currency. Nil when no budget is configured.
	BudgetAmount *float64 `json:"budget_amount,omitempty"`
	// BudgetStatus is one of "under", "over", "on" when a budget is configured,
	// empty string otherwise. "on" indicates the forecast is within +/- 5% of
	// the budget.
	BudgetStatus string `json:"budget_status,omitempty"`

	AvgTokenCost     float64 `json:"avg_token_cost"`
	AvgTokenCostPrev float64 `json:"avg_token_cost_prev"`
	// AvgTokenCostDeltaPct is the percent change vs the prior month-to-date
	// window. Nil when the prior denominator is zero.
	AvgTokenCostDeltaPct *float64 `json:"avg_token_cost_delta_pct,omitempty"`

	DailySpend      []DailySpend     `json:"daily_spend"`
	TokenBreakdown  []TokenBreakdown `json:"token_breakdown"`
	CacheSavings    float64          `json:"cache_savings"`
	WastedSpend     float64          `json:"wasted_spend"`
	AgentModelCosts []AgentModelCost `json:"agent_model_costs"`
}

// CostBudget is the current state of a per-deployment monthly budget,
// reconstructed from the agentguard_cost_budgets append-only stream via
// arg_max(..., updated_at) GROUP BY deployment_id.
type CostBudget struct {
	DeploymentID  string  `json:"deployment_id"`
	MonthlyBudget float64 `json:"monthly_budget"`
	Currency      string  `json:"currency"`
	UpdatedBy     string  `json:"updated_by"`
	UpdatedAt     string  `json:"updated_at"`
}

// CostBudgetsResponse is returned by GET /api/cost/budget.
type CostBudgetsResponse struct {
	Budgets []CostBudget `json:"budgets"`
}

// PricingEntry is one row in the agentguard_pricing mutable stream.
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
	Detections24h     int      `json:"detections_24h"`
	LatestVersion     string   `json:"latest_version,omitempty"`
	UDFs              []string `json:"udfs,omitempty"`
	BlockPolicy       string   `json:"block_policy"` // hold|auto_block|log_only
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
	RuleID       string `json:"rule_id"`
	RuleName     string `json:"rule_name"`
	Severity     string `json:"severity"`
	AgentID      string `json:"agent_id"`
	DeploymentID string `json:"deployment_id"`
	Message      string `json:"message"`
	DetectedAt   string `json:"detected_at"`
}

type QueryResult struct {
	Columns []string            `json:"columns"`
	Rows    []map[string]string `json:"rows"`
	Total   int                 `json:"total"`
}

type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type TableInfo struct {
	Name    string       `json:"name"`
	Type    string       `json:"type"`
	Columns []ColumnInfo `json:"columns"`
}

// DLPStatus reports Semantic DLP provisioning and enrichment health.
type DLPStatus struct {
	Enabled           bool            `json:"enabled"`
	StreamExists      bool            `json:"stream_exists"`
	MVExists          bool            `json:"mv_exists"`
	UDFs              map[string]bool `json:"udfs"`
	TotalRows         int64           `json:"total_rows"`
	EmbeddedRows      int64           `json:"embedded_rows"`
	FlaggedRows       int64           `json:"flagged_rows"`
	EmbeddingCoverage float64         `json:"embedding_coverage"`
}

