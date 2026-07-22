package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// streamNameRe validates a stream/view name before it is interpolated into
// SQL (describe_stream): letters/digits/underscore only, starting with a
// letter or underscore. No quotes, dots, or whitespace can ever reach the
// query string this way — defense-in-depth on top of the literal-escaping
// fallback below.
var streamNameRe = regexp.MustCompile(`^[a-zA-Z_]\w*$`)

// streamDoc is one line of the static schema card: a stream name plus a
// short purpose/key-column hint. Column hints are copied from the real DDL
// (backend/internal/timeplus/*.go) rather than paraphrased, since the LLM
// grounds the SQL it writes on this text.
type streamDoc struct {
	name string
	desc string
}

// knownStreamDocs mirrors CLAUDE.md's "Raw -> CIM Pipeline" table plus the
// operational streams (threats/holds) and the Sentry-agent streams from
// docs/superpowers/specs/2026-07-22-sentry-agent-design.md §2. Order is the
// display order in schemaCardStatic.
var knownStreamDocs = []streamDoc{
	{"agentguard_hook_events", "raw hook payloads from agent plugins; key cols: event_time, hook_name, agent_id, deployment_id, session_id, run_id, tool_name, provider, model, hook_decision, event_data"},
	{"agentguard_otel_logs", "raw OTel logs emitted by Claude Code telemetry"},
	{"agentguard_otel_metrics", "raw OTel metrics emitted by Claude Code telemetry"},
	{"agentguard_cim_event", "normalized lifecycle events (session/llm_call/tool_call); key cols: event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, user_message, assistant_message"},
	{"agentguard_cim_metrics", "pre-computed scalar metrics per call (tokens, cost, latency); key cols: event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags"},
	{"agentguard_rules", "append-only Rule Instance snapshot log; current state via arg_max(col, revision) GROUP BY rule_id; key cols: rule_id, snapshot_id, revision, name, enabled, deleted, severity, category"},
	{"agentguard_security_events", "rule-triggered detections written by MVs; key cols: detected_at, rule_id, rule_name, severity, agent_id, deployment_id, session_id, run_id, tool_name, message"},
	{"agentguard_cost_budgets", "per-deployment monthly budget mutation log; current state via arg_max(...updated_at) GROUP BY deployment_id HAVING NOT deleted; key cols: deployment_id, monthly_budget, currency, updated_by, deleted, updated_at"},
	{"agentguard_dlp_enriched", "one enriched row per conversation message (entities, risk tags/score, inline embedding); written by mv_dlp_enrich; key cols: event_time, message_id, session_id, run_id, role, agent_id, deployment_id, model, tool_name, content, entities, risk_tags, risk_score, embedding"},
	{"agentguard_dlp_config", "mutable key -> JSON value config stream (a shared row + one row per DLP UDF: model/max_tokens/prompt); key cols: key, value, updated_at, updated_by"},
	{"agentguard_threats", "acknowledged/cleared threat status upserts; key cols: agent_id, deployment_id, session_id, rule_id, rule_name, severity, last_seen, status, status_updated_at"},
	{"agentguard_holds", "human-in-the-loop hold-and-decide records; key cols: hold_id, deployment_id, agent_id, session_id, run_id, tool_name, rule_id, severity, status, created_at, decided_by, decided_at, decided_reason"},
	{"agentguard_sentry_conversations", "Sentry chat conversation metadata, scoped per user; key cols: conversation_id, user, title, status, created_at, updated_at"},
	{"agentguard_sentry_messages", "Sentry chat message log — one row per user/assistant turn or tool call; key cols: event_time, conversation_id, seq, role, content, tool_name, tool_args, tool_result, model, tokens_in, tokens_out"},
}

// schemaCardStatic renders the fixed, always-available part of the schema
// card: one line per knownStreamDocs entry. It is pure (no Timeplus query)
// so it is directly unit-testable and is also the fail-open fallback for
// SchemaCard when the live system.tables query errors.
func schemaCardStatic() string {
	var b strings.Builder
	b.WriteString("Known AgentGuard streams:\n")
	for _, d := range knownStreamDocs {
		b.WriteString("- " + d.name)
		if d.desc != "" {
			b.WriteString(": " + d.desc)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// SchemaCard renders schemaCardStatic plus any live agentguard_* stream
// names not already documented there, under an "Other streams:" section —
// e.g. a custom rule's sink or a stream created outside this catalog. On
// any query error it fails open and returns the static card alone, since a
// schema card unusable due to a transient Timeplus hiccup is worse than an
// incomplete one.
func SchemaCard(cfg *config.TimeplusConfig) string {
	card := schemaCardStatic()

	rows, err := tp.Query(cfg, "SELECT name FROM system.tables WHERE database = current_database() AND name LIKE 'agentguard_%' ORDER BY name")
	if err != nil {
		return card
	}

	known := make(map[string]bool, len(knownStreamDocs))
	for _, d := range knownStreamDocs {
		known[d.name] = true
	}

	var other []string
	for _, r := range rows {
		name, _ := r["name"].(string)
		if name == "" || known[name] {
			continue
		}
		other = append(other, name)
	}
	if len(other) == 0 {
		return card
	}
	sort.Strings(other)

	var b strings.Builder
	b.WriteString(card)
	b.WriteString("Other streams:\n")
	for _, n := range other {
		b.WriteString("- " + n + "\n")
	}
	return b.String()
}

// escapeStrLiteral escapes a single-quoted SQL string literal. Called only
// on names already validated by streamNameRe (so it can never actually
// contain a quote) — kept as defense-in-depth for the system.columns
// fallback query below.
func escapeStrLiteral(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

type listStreams struct{}

// NewListStreams returns the read-only "list_streams" skill.
func NewListStreams() Skill { return listStreams{} }

func (listStreams) Name() string { return "list_streams" }
func (listStreams) Description() string {
	return "List AgentGuard streams and materialized views (agentguard_* and mv_*) currently present in Timeplus. Returns a JSON array of names."
}
func (listStreams) Params() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (listStreams) Access() AccessClass { return ReadOnly }

func (listStreams) Execute(_ context.Context, rc *RunContext, _ json.RawMessage) (string, error) {
	rows, err := tp.Query(&rc.Cfg.Timeplus,
		"SELECT name FROM system.tables WHERE database = current_database() AND (name LIKE 'agentguard_%' OR name LIKE 'mv_%') ORDER BY name")
	if err != nil {
		return "error: " + err.Error(), nil
	}

	names := make([]string, 0, len(rows))
	for _, r := range rows {
		if n, ok := r["name"].(string); ok {
			names = append(names, n)
		}
	}

	b, err := json.Marshal(names)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	out, _ := Truncate(string(b), rc.ResultMaxBytes)
	return out, nil
}

type describeStream struct{}

// NewDescribeStream returns the read-only "describe_stream" skill.
func NewDescribeStream() Skill { return describeStream{} }

func (describeStream) Name() string { return "describe_stream" }
func (describeStream) Description() string {
	return "Describe a stream's columns and types (name, type). Use list_streams first to find valid names."
}
func (describeStream) Params() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"required":   []string{"name"},
	}
}
func (describeStream) Access() AccessClass { return ReadOnly }

func (describeStream) Execute(_ context.Context, rc *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Name) == "" {
		return "error: describe_stream needs a {\"name\": \"...\"} argument", nil
	}
	name := strings.TrimSpace(a.Name)
	if !streamNameRe.MatchString(name) {
		return "error: invalid stream name — expected letters, digits and underscores only", nil
	}

	rows, err := tp.Query(&rc.Cfg.Timeplus, "DESCRIBE "+name)
	if err != nil {
		rows, err = tp.Query(&rc.Cfg.Timeplus, fmt.Sprintf(
			"SELECT name, type FROM system.columns WHERE table = '%s' AND database = current_database()",
			escapeStrLiteral(name)))
		if err != nil {
			return "error: " + err.Error(), nil
		}
	}

	b, err := json.Marshal(rows)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	out, _ := Truncate(string(b), rc.ResultMaxBytes)
	return out, nil
}
