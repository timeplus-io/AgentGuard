package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/timeplus/agentguard/internal/dlpquery"
	"github.com/timeplus/agentguard/internal/rules"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// clampInt substitutes def when n <= 0, then clamps the result to [lo, hi].
// Shared by every curated skill's limit/days parameter so an agent-supplied
// value can never grow a query unboundedly or slip a non-positive value
// (e.g. LIMIT 0 dropping rows silently, or a negative day count).
func clampInt(n, def, lo, hi int) int {
	if n <= 0 {
		n = def
	}
	if n < lo {
		n = lo
	}
	if n > hi {
		n = hi
	}
	return n
}

// ── get_threats ─────────────────────────────────────────────────────────────

type getThreats struct{}

// NewGetThreats returns the read-only "get_threats" skill. It mirrors
// buildThreatsList's base query in internal/handlers/threats.go
// (sqlListThreats): agentguard_threats is a mutable stream, so a plain
// SELECT already yields the latest row per (agent_id, deployment_id,
// session_id, rule_id) key — no arg_max needed. This skill intentionally
// skips the handler's extra joins against agentguard_security_events
// (event_count/first_seen/per-event history) and its reopen-on-refire
// side effect (an INSERT), since those are presentation/write concerns
// the read-only chat adapter has no business performing; the returned
// severity/status/last_seen match the Threats page for the same rows.
func NewGetThreats() Skill { return getThreats{} }

func (getThreats) Name() string { return "get_threats" }
func (getThreats) Description() string {
	return "List current AgentGuard threats (deduplicated latest state) from agentguard_threats — the same rows the Threats page shows. " +
		"Optional filters: severity, status (open/acknowledged/cleared), agent_id. Ordered by last_seen desc."
}
func (getThreats) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"severity": map[string]any{"type": "string", "description": "Filter by severity (e.g. critical, high, medium, low)."},
			"status":   map[string]any{"type": "string", "description": "Filter by status (open, acknowledged, cleared)."},
			"agent_id": map[string]any{"type": "string", "description": "Filter to one agent's threats."},
			"limit":    map[string]any{"type": "integer", "description": "Max rows to return (default 50, max 500)."},
		},
	}
}
func (getThreats) Access() AccessClass { return ReadOnly }

func (getThreats) Execute(_ context.Context, rc *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		Severity string `json:"severity"`
		Status   string `json:"status"`
		AgentID  string `json:"agent_id"`
		Limit    int    `json:"limit"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &a) // every field optional — parse errors leave zero values
	}
	limit := clampInt(a.Limit, 50, 1, 500)

	var where []string
	if s := strings.TrimSpace(a.Severity); s != "" {
		where = append(where, fmt.Sprintf("severity = '%s'", dlpquery.EscapeSQLStr(s)))
	}
	if s := strings.TrimSpace(a.Status); s != "" {
		where = append(where, fmt.Sprintf("status = '%s'", dlpquery.EscapeSQLStr(s)))
	}
	if s := strings.TrimSpace(a.AgentID); s != "" {
		where = append(where, fmt.Sprintf("agent_id = '%s'", dlpquery.EscapeSQLStr(s)))
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "\nWHERE " + strings.Join(where, " AND ")
	}

	sql := fmt.Sprintf(`
SELECT agent_id, deployment_id, session_id, rule_id,
       rule_name, severity, last_seen, status, status_updated_at
FROM table(agentguard_threats)%s
ORDER BY last_seen DESC
LIMIT %d`, whereClause, limit)

	rows, err := tp.Query(&rc.Cfg.Timeplus, sql)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	out, _ := Truncate(string(b), rc.ResultMaxBytes)
	return out, nil
}

// ── get_holds ────────────────────────────────────────────────────────────

// curatedHoldsLatestSubquery duplicates holdsLatestSubquery from
// internal/handlers/holds.go verbatim (column list and arg_max(...,
// updated_at) GROUP BY hold_id shape) — this package cannot import
// internal/handlers (handlers will import internal/sentry/skills in a later
// task, which would create a cycle), so the query text is copied rather than
// shared.
const curatedHoldsLatestSubquery = `(
  SELECT hold_id,
         arg_max(deployment_id,  updated_at) AS deployment_id,
         arg_max(agent_id,       updated_at) AS agent_id,
         arg_max(session_id,     updated_at) AS session_id,
         arg_max(run_id,         updated_at) AS run_id,
         arg_max(tool_name,      updated_at) AS tool_name,
         arg_max(rule_id,        updated_at) AS rule_id,
         arg_max(severity,       updated_at) AS severity,
         arg_max(args_summary,   updated_at) AS args_summary,
         arg_max(message,        updated_at) AS message,
         arg_max(created_at,     updated_at) AS created_at,
         arg_max(status,         updated_at) AS status,
         arg_max(decided_by,     updated_at) AS decided_by,
         arg_max(decided_at,     updated_at) AS decided_at,
         arg_max(decided_reason, updated_at) AS decided_reason
  FROM table(agentguard_holds)
  GROUP BY hold_id
)`

type getHolds struct{}

// NewGetHolds returns the read-only "get_holds" skill, mirroring ListHolds
// in internal/handlers/holds.go: same latest-per-hold_id subquery, same
// status filter (default "pending"), same ORDER BY created_at DESC.
func NewGetHolds() Skill { return getHolds{} }

func (getHolds) Name() string { return "get_holds" }
func (getHolds) Description() string {
	return "List AgentGuard human-in-the-loop holds (approve/deny records), latest state per hold_id — the same rows the Holds list shows. " +
		"Defaults to status='pending'."
}
func (getHolds) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{"type": "string", "description": "Filter by status (default: pending). One of pending/approved/denied/timeout/abandoned."},
			"limit":  map[string]any{"type": "integer", "description": "Max rows to return (default 100, max 500)."},
		},
	}
}
func (getHolds) Access() AccessClass { return ReadOnly }

func (getHolds) Execute(_ context.Context, rc *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		Status string `json:"status"`
		Limit  int    `json:"limit"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &a)
	}
	status := strings.TrimSpace(a.Status)
	if status == "" {
		status = "pending"
	}
	limit := clampInt(a.Limit, 100, 1, 500)

	sql := fmt.Sprintf(`
SELECT hold_id, deployment_id, agent_id, session_id, run_id, tool_name,
       rule_id, severity, args_summary, message, created_at, status,
       decided_by, decided_at, decided_reason
FROM `+curatedHoldsLatestSubquery+`
WHERE status = '%s'
ORDER BY created_at DESC
LIMIT %d`, dlpquery.EscapeSQLStr(status), limit)

	rows, err := tp.Query(&rc.Cfg.Timeplus, sql)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	out, _ := Truncate(string(b), rc.ResultMaxBytes)
	return out, nil
}

// ── get_rule_status ──────────────────────────────────────────────────────

// ruleStatusRow is the compact projection this skill returns — just the
// fields the brief calls out (rule_id, name, enabled, block_policy,
// updated_at) — rather than the full RuleInstance (which also carries
// rendered_sql, deployed_udfs, overrides, ... — far more than a status
// question needs and much more expensive to truncate/read).
type ruleStatusRow struct {
	RuleID      string `json:"rule_id"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	BlockPolicy string `json:"block_policy"`
	UpdatedAt   string `json:"updated_at"`
}

type getRuleStatus struct{}

// NewGetRuleStatus returns the read-only "get_rule_status" skill. It calls
// rules.NewStore(cfg).ListCurrent() — the exact exported helper
// internal/rules/store.go uses to reconstruct current rule state via
// arg_max(col, revision) GROUP BY rule_id over table(agentguard_rules),
// excluding soft-deleted rules — instead of duplicating that SQL, per the
// brief's "prefer an exported helper" guidance. internal/rules does not
// import internal/sentry, so this creates no import cycle.
func NewGetRuleStatus() Skill { return getRuleStatus{} }

func (getRuleStatus) Name() string { return "get_rule_status" }
func (getRuleStatus) Description() string {
	return "List installed AgentGuard rules with current enabled/block_policy state — the same current-state reconstruction " +
		"(arg_max(col, revision) over agentguard_rules, soft-deleted rules excluded) the Rule Packs page uses. No parameters."
}
func (getRuleStatus) Params() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (getRuleStatus) Access() AccessClass { return ReadOnly }

func (getRuleStatus) Execute(_ context.Context, rc *RunContext, _ json.RawMessage) (string, error) {
	store := rules.NewStore(&rc.Cfg.Timeplus)
	instances, err := store.ListCurrent()
	if err != nil {
		return "error: " + err.Error(), nil
	}

	out := make([]ruleStatusRow, 0, len(instances))
	for _, inst := range instances {
		out = append(out, ruleStatusRow{
			RuleID:      inst.RuleID,
			Name:        inst.Name,
			Enabled:     inst.Enabled,
			BlockPolicy: inst.BlockPolicy,
			UpdatedAt:   inst.UpdatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		})
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	res, _ := Truncate(string(b), rc.ResultMaxBytes)
	return res, nil
}

// ── get_cost_summary ─────────────────────────────────────────────────────

// modelCostRow is one per-model aggregate row returned by get_cost_summary.
type modelCostRow struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	RequestCount int64   `json:"request_count"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheTokens  int64   `json:"cache_tokens"`
	TotalCost    float64 `json:"total_cost"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

type getCostSummary struct{}

// NewGetCostSummary returns the read-only "get_cost_summary" skill. Its cost
// formula — sum_if(...llm.input_tokens...) * any(p.input_cost_per_million) +
// output + cache_read, all /1e6, LEFT JOIN agentguard_pricing on
// (provider, model) — is copied verbatim from queryCostByAgent in
// internal/handlers/cost.go, just grouped by (provider, model) summed across
// agents instead of by agent. avg_latency_ms is computed the same way
// GetLLMMetrics computes its latency stats (metric_name = 'llm.latency_ms',
// metric_value > 0), but as a single grouped average rather than the
// handler's quantile set — a simplification, called out in the task report.
//
// Simplification vs. GetCost: the /api/cost handler's headline numbers are
// UTC *calendar-month* windows (month-to-date + prior-period delta); this
// skill instead takes a simple trailing N-day window (?days=, default 30)
// per the brief's params, since a chat question like "cost this month" and
// "cost over N days" are different asks and GetCost's calendar-month/budget
// machinery (budgets, deltas, daily trend, token breakdown) is presentation
// logic this read-only adapter does not attempt to reproduce.
func NewGetCostSummary() Skill { return getCostSummary{} }

func (getCostSummary) Name() string { return "get_cost_summary" }
func (getCostSummary) Description() string {
	return "Per-model token/cost/latency totals over a trailing window (default 30 days), from agentguard_cim_metrics joined with " +
		"agentguard_pricing — same cost formula as the Cost Governance page's per-agent breakdown, aggregated by model. " +
		"Note: uses a trailing N-day window, not the UI's calendar-month window."
}
func (getCostSummary) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"days": map[string]any{"type": "integer", "description": "Trailing window size in days (default 30, max 365)."},
		},
	}
}
func (getCostSummary) Access() AccessClass { return ReadOnly }

func (getCostSummary) Execute(_ context.Context, rc *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		Days int `json:"days"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &a)
	}
	days := clampInt(a.Days, 30, 1, 365)

	sql := fmt.Sprintf(`
SELECT
  m.tags['provider'] AS provider,
  m.tags['model']    AS model,
  to_int64(sum_if(m.metric_value, m.metric_name = 'llm.call'))             AS request_count,
  to_int64(sum_if(m.metric_value, m.metric_name = 'llm.input_tokens'))     AS input_tokens,
  to_int64(sum_if(m.metric_value, m.metric_name = 'llm.output_tokens'))    AS output_tokens,
  to_int64(sum_if(m.metric_value, m.metric_name = 'llm.cache_read_tokens')) AS cache_tokens,
  (
    sum_if(m.metric_value, m.metric_name = 'llm.input_tokens')       * any(p.input_cost_per_million)
    + sum_if(m.metric_value, m.metric_name = 'llm.output_tokens')    * any(p.output_cost_per_million)
    + sum_if(m.metric_value, m.metric_name = 'llm.cache_read_tokens') * any(p.cache_read_cost_per_million)
  ) / 1000000.0                                                             AS total_cost,
  sum_if(m.metric_value, m.metric_name = 'llm.latency_ms' AND m.metric_value > 0) AS latency_sum,
  count_if(m.metric_name = 'llm.latency_ms' AND m.metric_value > 0)        AS latency_count
FROM table(agentguard_cim_metrics) AS m
LEFT JOIN table(agentguard_pricing) AS p
  ON m.tags['provider'] = p.provider AND m.tags['model'] = p.model
WHERE m.metric_name IN ('llm.call','llm.input_tokens','llm.output_tokens','llm.cache_read_tokens','llm.latency_ms')
  AND m.event_time >= now() - interval %d day
GROUP BY provider, model
ORDER BY total_cost DESC`, days)

	rows, err := tp.Query(&rc.Cfg.Timeplus, sql)
	if err != nil {
		return "error: " + err.Error(), nil
	}

	out := make([]modelCostRow, 0, len(rows))
	for _, r := range rows {
		latSum := dlpSearchFloat(r["latency_sum"])
		latCount := dlpSearchFloat(r["latency_count"])
		var avgLat float64
		if latCount > 0 {
			avgLat = latSum / latCount
		}
		out = append(out, modelCostRow{
			Provider:     dlpSearchStr(r["provider"]),
			Model:        dlpSearchStr(r["model"]),
			RequestCount: int64(dlpSearchFloat(r["request_count"])),
			InputTokens:  int64(dlpSearchFloat(r["input_tokens"])),
			OutputTokens: int64(dlpSearchFloat(r["output_tokens"])),
			CacheTokens:  int64(dlpSearchFloat(r["cache_tokens"])),
			TotalCost:    dlpSearchFloat(r["total_cost"]),
			AvgLatencyMs: avgLat,
		})
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	res, _ := Truncate(string(b), rc.ResultMaxBytes)
	return res, nil
}

// ── get_session_timeline ─────────────────────────────────────────────────

// timelineEventTypesCurated duplicates timelineEventTypes from
// internal/handlers/session_timeline.go — the canonical CIM event types the
// timeline renders.
const timelineEventTypesCurated = "'user_input','llm_response','tool_invoke','tool_complete','session_start','session_end'"

type getSessionTimeline struct{}

// NewGetSessionTimeline returns the read-only "get_session_timeline" skill.
// It mirrors GetSessionTimeline in internal/handlers/session_timeline.go:
// same event_type allowlist, same column set, same ORDER BY event_time ASC,
// against table(agentguard_cim_event). Content columns (tool_input,
// tool_result, user_message, assistant_message) are truncated to 200 chars
// with left(...) per the brief, and the query goes through tp.QueryNative
// (not tp.Query) for the same reason the handler does: arbitrary
// conversation content can break the HTTP+JSON serializer.
//
// Like the handler, this skill scopes by BOTH deployment_id AND agent_id —
// agent_id alone is not globally unique across deployments. Callers can
// supply either a compound "deploymentId__agentId" agent_id (the shape
// get_threats/get_holds already return) or a bare agent_id plus an explicit
// deployment_id param; see buildTimelineWhere.
func NewGetSessionTimeline() Skill { return getSessionTimeline{} }

func (getSessionTimeline) Name() string { return "get_session_timeline" }
func (getSessionTimeline) Description() string {
	return "Ordered timeline of CIM events (user_input/llm_response/tool_invoke/tool_complete/session_start/session_end) for one " +
		"agent+session, from agentguard_cim_event — the same rows the session timeline page renders. Message/tool content is " +
		"truncated to 200 chars. Both agent_id and session_id are required. agent_id may be a compound " +
		"\"deployment_id__agent_id\" id, as returned by other tools (e.g. get_threats/get_holds); alternatively pass a bare " +
		"agent_id along with the optional deployment_id param. A bare agent_id with no deployment_id falls back to " +
		"agent_id-only filtering, which can commingle rows across deployments that happen to share an agent_id."
}
func (getSessionTimeline) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_id": map[string]any{"type": "string", "description": "Agent id to scope the timeline to. May be a compound " +
				"\"deployment_id__agent_id\" id (as returned by other tools); the deployment half is then used to scope the query."},
			"deployment_id": map[string]any{"type": "string", "description": "Optional deployment id to scope the timeline to. " +
				"Use this alongside a bare agent_id. Takes precedence over any deployment half embedded in a compound agent_id."},
			"session_id": map[string]any{"type": "string", "description": "Session id to scope the timeline to."},
			"limit":      map[string]any{"type": "integer", "description": "Max events to return (default 200, max 1000)."},
		},
		"required": []string{"agent_id", "session_id"},
	}
}
func (getSessionTimeline) Access() AccessClass { return ReadOnly }

// timelineIDPartRe replicates handlers.idPartRe
// (internal/handlers/agents.go), which constrains each half of a compound
// "deploymentId__agentId" id. Duplicated here — like curatedHoldsLatestSubquery
// above — because internal/sentry/skills cannot import internal/handlers.
var timelineIDPartRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// splitCompoundAgentID replicates splitAgentID from
// internal/handlers/agent_sessions.go verbatim (SplitN on "__", both halves
// must match timelineIDPartRe) — it's unexported there, and this package
// cannot import internal/handlers, so the logic is duplicated rather than
// shared.
func splitCompoundAgentID(compoundID string) (deploymentID, agentID string, ok bool) {
	parts := strings.SplitN(compoundID, "__", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	if !timelineIDPartRe.MatchString(parts[0]) || !timelineIDPartRe.MatchString(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// buildTimelineWhere builds the get_session_timeline WHERE clause (without
// the leading "WHERE " keyword and without the event_type/LIMIT terms),
// mirroring GetSessionTimeline's deployment_id+agent_id scoping in
// internal/handlers/session_timeline.go — this codebase treats agent
// identity as the compound (deployment_id, agent_id) since agent_id is not
// globally unique across deployments.
//
// rawAgentID is always parsed via splitCompoundAgentID first: when it splits,
// agentID becomes the agent half regardless of explicitDeploymentID (parsing
// the compound shape is independent of which deployment_id ends up used).
//
// Deployment resolution order:
//  1. explicitDeploymentID, if non-empty, wins outright — it is never
//     silently overridden by a compound rawAgentID's deployment half.
//  2. else, the compound split's deployment half (if rawAgentID split).
//  3. else, no deployment_id predicate is added: bare agent_id filtering
//     only, a documented fallback for callers that don't have a
//     deployment_id (this can commingle rows across deployments that share
//     an agent_id — see the skill Description).
//
// All string components are escaped via dlpquery.EscapeSQLStr.
func buildTimelineWhere(rawAgentID, explicitDeploymentID, sessionID string) string {
	agentID := rawAgentID
	splitDeploymentID, splitAgentID, split := splitCompoundAgentID(rawAgentID)
	if split {
		agentID = splitAgentID
	}

	deploymentID := strings.TrimSpace(explicitDeploymentID)
	if deploymentID == "" && split {
		deploymentID = splitDeploymentID
	}

	where := make([]string, 0, 3)
	if deploymentID != "" {
		where = append(where, fmt.Sprintf("deployment_id = '%s'", dlpquery.EscapeSQLStr(deploymentID)))
	}
	where = append(where, fmt.Sprintf("agent_id = '%s'", dlpquery.EscapeSQLStr(agentID)))
	where = append(where, fmt.Sprintf("session_id = '%s'", dlpquery.EscapeSQLStr(sessionID)))

	return strings.Join(where, " AND ")
}

func (getSessionTimeline) Execute(_ context.Context, rc *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		AgentID      string `json:"agent_id"`
		DeploymentID string `json:"deployment_id"`
		SessionID    string `json:"session_id"`
		Limit        int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "error: get_session_timeline needs {\"agent_id\": \"...\", \"session_id\": \"...\"} arguments", nil
	}
	agentID := strings.TrimSpace(a.AgentID)
	sessionID := strings.TrimSpace(a.SessionID)
	if agentID == "" || sessionID == "" {
		return "error: get_session_timeline requires both agent_id and session_id", nil
	}
	limit := clampInt(a.Limit, 200, 1, 1000)

	sql := fmt.Sprintf(`
SELECT event_time, event_type, run_id, tool_name,
       left(tool_input, 200) AS tool_input,
       left(tool_result, 200) AS tool_result,
       tool_success,
       left(user_message, 200) AS user_message,
       left(assistant_message, 200) AS assistant_message,
       model, provider, hook_decision, block_reason
FROM table(agentguard_cim_event)
WHERE %s
  AND event_type IN (%s)
ORDER BY event_time ASC
LIMIT %d`,
		buildTimelineWhere(agentID, strings.TrimSpace(a.DeploymentID), sessionID),
		timelineEventTypesCurated, limit)

	rows, err := tp.QueryNative(&rc.Cfg.Timeplus, sql)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	b, err := json.Marshal(rows)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	out, _ := Truncate(string(b), rc.ResultMaxBytes)
	return out, nil
}
