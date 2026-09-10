package timeplus

// AppResourceKind is a Timeplus app manifest resource type
// (https://github.com/timeplus-io/apps, package format v1).
type AppResourceKind string

const (
	AppKindStream        AppResourceKind = "stream"
	AppKindMutableStream AppResourceKind = "mutable_stream"
	AppKindMV            AppResourceKind = "materialized_view"
	AppKindInput         AppResourceKind = "input"
)

// AppResource is one provisioned object, referenced by the same DDL constant
// Init() executes. Nothing is copied — the .tpapp exporter (internal/tpapp)
// transforms these in place, so the app and the setup wizard cannot drift.
type AppResource struct {
	Kind AppResourceKind
	Name string // object name without database, e.g. "agentguard_cim_event"
	DDL  string // the ddl* constant, verbatim (bare CREATE, unqualified names)

	ddlConst string // Go identifier of DDL; used by the coverage test only
}

func res(kind AppResourceKind, name, ddlConst, ddl string) AppResource {
	return AppResource{Kind: kind, Name: name, DDL: ddl, ddlConst: ddlConst}
}

// AppResources returns the data-pipeline subset of what Init() provisions, in
// Init()'s dependency order (sinks before the MVs that write into them).
// Control-plane objects (notifications, holds, RBAC, Sentry, DLP) are NOT
// here — see AppExcludedDDL and
// docs/superpowers/specs/2026-09-10-tpapp-export-design.md.
func AppResources() []AppResource {
	return []AppResource{
		// raw OTel
		res(AppKindStream, "otel_traces", "ddlOTelTraces", ddlOTelTraces),
		res(AppKindStream, "otel_logs", "ddlOTelLogs", ddlOTelLogs),
		res(AppKindStream, "otel_metrics_gauge", "ddlOTelMetricsGauge", ddlOTelMetricsGauge),
		res(AppKindStream, "otel_metrics_sum", "ddlOTelMetricsSum", ddlOTelMetricsSum),
		res(AppKindStream, "otel_metrics_histogram", "ddlOTelMetricsHistogram", ddlOTelMetricsHistogram),
		res(AppKindStream, "otel_metrics_exponential_histogram", "ddlOTelMetricsExponentialHistogram", ddlOTelMetricsExponentialHistogram),
		res(AppKindStream, "otel_metrics_summary", "ddlOTelMetricsSummary", ddlOTelMetricsSummary),
		res(AppKindInput, "agentguard_otel_input", "ddlOTelInput", ddlOTelInput),
		// raw hooks
		res(AppKindStream, "agentguard_hook_events", "ddlAgentGuardHookEvents", ddlAgentGuardHookEvents),
		// CIM
		res(AppKindStream, "agentguard_cim_event", "ddlCIMEvent", ddlCIMEvent),
		res(AppKindMV, "mv_cim_claudecode", "ddlCIMClaudeCodeMV", ddlCIMClaudeCodeMV),
		res(AppKindMV, "mv_cim_openclaw", "ddlCIMOpenClawMV", ddlCIMOpenClawMV),
		res(AppKindMV, "mv_cim_hermes", "ddlCIMHermesMV", ddlCIMHermesMV),
		res(AppKindMV, "mv_cim_openinference", "ddlCIMOpenInferenceMV", ddlCIMOpenInferenceMV),
		res(AppKindMV, "mv_hook_bridge_openinference", "ddlHookBridgeOpenInferenceMV", ddlHookBridgeOpenInferenceMV),
		// metrics (lookup feeders first — the metrics MVs JOIN against them)
		res(AppKindMutableStream, "agentguard_session_agents", "ddlSessionAgentsStream", ddlSessionAgentsStream),
		res(AppKindMutableStream, "agentguard_run_latency", "ddlRunLatencyStream", ddlRunLatencyStream),
		res(AppKindStream, "agentguard_cim_metrics", "ddlCIMMetrics", ddlCIMMetrics),
		res(AppKindMV, "mv_session_agents", "ddlSessionAgentsMV", ddlSessionAgentsMV),
		res(AppKindMV, "mv_run_latency", "ddlRunLatencyMV", ddlRunLatencyMV),
		res(AppKindMV, "mv_cim_metrics_openclaw_llm", "ddlCIMMetricsOpenClawLlmMV", ddlCIMMetricsOpenClawLlmMV),
		res(AppKindMV, "mv_cim_metrics_openclaw_tools", "ddlCIMMetricsOpenClawToolsMV", ddlCIMMetricsOpenClawToolsMV),
		res(AppKindMV, "mv_cim_metrics_claudecode_otel", "ddlCIMMetricsClaudeCodeOtelMV", ddlCIMMetricsClaudeCodeOtelMV),
		res(AppKindMV, "mv_cim_metrics_claudecode_tools", "ddlCIMMetricsClaudeCodeToolsMV", ddlCIMMetricsClaudeCodeToolsMV),
		res(AppKindMV, "mv_cim_metrics_hermes_llm", "ddlCIMMetricsHermesLlmMV", ddlCIMMetricsHermesLlmMV),
		res(AppKindMV, "mv_cim_metrics_hermes_tools", "ddlCIMMetricsHermesToolsMV", ddlCIMMetricsHermesToolsMV),
		res(AppKindMV, "mv_cim_metrics_openinference_llm", "ddlCIMMetricsOpenInferenceLlmMV", ddlCIMMetricsOpenInferenceLlmMV),
		// security
		res(AppKindStream, "agentguard_security_events", "ddlSecurityEvents", ddlSecurityEvents),
		res(AppKindMutableStream, "agentguard_threats", "ddlThreatsStream", ddlThreatsStream),
		res(AppKindMV, "mv_threats", "ddlThreatsMV", ddlThreatsMV),
		// cost (schema only; no seed rows)
		res(AppKindMutableStream, "agentguard_pricing", "ddlPricingStream", ddlPricingStream),
		res(AppKindStream, "agentguard_cost_budgets", "ddlCostBudgetsStream", ddlCostBudgetsStream),
		// skills
		res(AppKindStream, "agentguard_skills", "ddlSkillsStream", ddlSkillsStream),
		res(AppKindMV, "mv_skills_invoked_claudecode", "ddlMVSkillsInvokedClaudeCode", ddlMVSkillsInvokedClaudeCode),
		res(AppKindMV, "mv_skills_invoked_openclaw", "ddlMVSkillsInvokedOpenClaw", ddlMVSkillsInvokedOpenClaw),
		res(AppKindMV, "mv_skills_invoked_hermes", "ddlMVSkillsInvokedHermes", ddlMVSkillsInvokedHermes),
		res(AppKindMV, "mv_skills_loaded_claudecode", "ddlMVSkillsLoadedClaudeCode", ddlMVSkillsLoadedClaudeCode),
		res(AppKindMV, "mv_skills_loaded_openclaw", "ddlMVSkillsLoadedOpenClaw", ddlMVSkillsLoadedOpenClaw),
		// memory ops
		res(AppKindStream, "agentguard_memory_ops", "ddlMemoryOpsStream", ddlMemoryOpsStream),
		res(AppKindMV, "mv_memory_claudecode_automem", "ddlMVMemoryClaudeCodeAutoMem", ddlMVMemoryClaudeCodeAutoMem),
		res(AppKindMV, "mv_memory_claudecode_mcp", "ddlMVMemoryClaudeCodeMCP", ddlMVMemoryClaudeCodeMCP),
		res(AppKindMV, "mv_memory_openclaw_file", "ddlMVMemoryOpenClawFile", ddlMVMemoryOpenClawFile),
		res(AppKindMV, "mv_memory_hermes", "ddlMVMemoryHermes", ddlMVMemoryHermes),
		// rules snapshot log (instances are NOT seeded; rule MVs come from internal/tpapp)
		res(AppKindStream, "agentguard_rules", "ddlRulesStream", ddlRulesStream),
	}
}

// AppExcludedDDL lists the ddl* constants in init.go that the app deliberately
// does not ship (backend-only control plane). Kept explicit so the coverage
// test forces a decision for every new constant.
func AppExcludedDDL() []string {
	return []string{
		"ddlNotifyEventsStream",  // notifications: feeds the backend toast watcher only
		"ddlNotifyFilterMV",      // notifications
		"ddlNotificationsStream", // notifications seen-state
	}
}
