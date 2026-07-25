// Package dlpquery holds the recall/rerank SQL builders and intent parsing
// used by the Semantic DLP NL query funnel. It was extracted from
// internal/handlers/dlp_query.go so both the /api/dlp/query HTTP handler and
// the Sentry agent's dlp_search skill can share the exact same recall logic.
package dlpquery

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/rules"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

const (
	StructuredLimit = 200
	VectorLimit     = 100
	CandidateCap    = 300
	EvidenceChars   = 500
	// RerankCap bounds how many recall candidates are sent to the LLM
	// reranker. Recall is already ordered (risk_score / vector distance), so we
	// rerank the most promising prefix and leave the rest at the 0.0 default.
	// A local model scores ~CHUNK items per call; this keeps the rerank query
	// well under the 60s native timeout.
	RerankCap = 40
)

var dlpWordRe = regexp.MustCompile(`[A-Za-z0-9]{4,}`)

// EscapeSQLStr escapes a string for embedding in a Proton/ClickHouse
// single-quoted string literal. This is a twin of the package-wide
// handlers.escapeSQLStr helper (backend/internal/handlers/rules.go) — both
// delegate to rules.EscapeSQLString, the canonical full escaper, so the two
// copies can never drift. Kept separate (rather than importing handlers) to
// avoid an import cycle: handlers imports dlpquery, not the other way round.
func EscapeSQLStr(s string) string {
	return rules.EscapeSQLString(s)
}

// CfgSubquery returns a scalar sub-query that reads the live config value for
// a UDF from agentguard_dlp_config (re-planned each execution, so it always sees
// the latest upsert). coalesce(...,”) keeps the result non-nullable — a
// nullable arg makes map/array-returning UDFs fail to type.
func CfgSubquery(key string) string {
	return fmt.Sprintf("coalesce((SELECT value FROM table(agentguard_dlp_config) WHERE key='%s'), '')", key)
}

// ResolvedCfg returns a single, fully-resolved config expression for one UDF:
// dlp_config_resolve folds the shared endpoint row and the UDF's own row into a
// flat config (base_url/api_key/model/…) so the UDF never receives the shared
// model catalog. Both rows are read live via CfgSubquery (re-planned per run).
func ResolvedCfg(key string) string {
	return fmt.Sprintf("dlp_config_resolve(%s, %s)", CfgSubquery(tp.DLPCfgKeyShared), CfgSubquery(key))
}

// ParseIntent decodes the dlp_understand_query JSON; on failure it derives
// keywords from the question itself. The window is clamped to [1, 365].
func ParseIntent(raw, question string) models.DLPIntent {
	var intent models.DLPIntent
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		for _, w := range dlpWordRe.FindAllString(question, 8) {
			intent.Keywords = append(intent.Keywords, strings.ToLower(w))
		}
	}
	if intent.TimeRangeDays <= 0 {
		intent.TimeRangeDays = 30
	}
	if intent.TimeRangeDays > 365 {
		intent.TimeRangeDays = 365
	}
	return intent
}

const dlpRecallCols = `event_time, message_id, session_id, run_id, role,
       agent_type, agent_id, deployment_id, content, risk_tags, risk_score`

// BuildRecallSQL produces the two recall channels. The structured channel
// catches rows by entity/tag/keyword predicates regardless of embedding
// presence; the vector channel ranks embedded rows by cosine distance to the
// question embedding (dlp_embed runs inside Timeplus). Rows with empty
// embeddings would make cosine_distance error, hence the length() guard.
func BuildRecallSQL(intent models.DLPIntent, question string) (structured, vector string) {
	timeFilter := fmt.Sprintf("event_time > now() - INTERVAL %d DAY", intent.TimeRangeDays)

	var preds []string
	for _, et := range intent.EntityTypes {
		preds = append(preds, fmt.Sprintf("not empty(entities['%s'])", EscapeSQLStr(et)))
	}
	for _, tag := range intent.RiskTags {
		preds = append(preds, fmt.Sprintf("has(risk_tags, '%s')", EscapeSQLStr(tag)))
	}
	for _, kw := range intent.Keywords {
		preds = append(preds, fmt.Sprintf("position(lower(content), '%s') > 0", EscapeSQLStr(strings.ToLower(kw))))
	}
	where := timeFilter
	if len(preds) > 0 {
		where += " AND (" + strings.Join(preds, " OR ") + ")"
	}
	structured = fmt.Sprintf(`SELECT %s
FROM table(agentguard_dlp_enriched)
WHERE %s
ORDER BY risk_score DESC, event_time DESC
LIMIT %d`, dlpRecallCols, where, StructuredLimit)

	vector = fmt.Sprintf(`SELECT %s,
       cosine_distance(embedding, dlp_embed('%s', 'user', %s)) AS dist
FROM table(agentguard_dlp_enriched)
WHERE %s AND length(embedding) > 0
ORDER BY dist ASC
LIMIT %d`, dlpRecallCols, EscapeSQLStr(question), ResolvedCfg(tp.DLPCfgKeyEmbed), timeFilter, VectorLimit)
	return structured, vector
}

// MergeCandidates unions the channels in order, deduping by message_id.
func MergeCandidates(a, b []tp.Row, cap int) []tp.Row {
	seen := make(map[string]bool, len(a)+len(b))
	var out []tp.Row
	for _, row := range append(append([]tp.Row{}, a...), b...) {
		id, _ := row["message_id"].(string)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, row)
		if len(out) >= cap {
			break
		}
	}
	return out
}

// BuildRerankSQL scores candidates inside Timeplus via the dlp_rerank UDF.
func BuildRerankSQL(question string, ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = "'" + EscapeSQLStr(id) + "'"
	}
	return fmt.Sprintf(`SELECT message_id, dlp_rerank('%s', content, %s) AS score
FROM table(agentguard_dlp_enriched)
WHERE message_id IN (%s)`, EscapeSQLStr(question), ResolvedCfg(tp.DLPCfgKeyRerank), strings.Join(quoted, ", "))
}

// RowToEvidence maps a recall row to a cited-evidence record, truncating
// content for the summarizer/UI.
func RowToEvidence(row tp.Row, rerankScore float64) models.DLPEvidence {
	content := parseStr(row["content"])
	if len(content) > EvidenceChars {
		i := EvidenceChars
		for i > 0 && !utf8.RuneStart(content[i]) {
			i--
		}
		content = content[:i]
	}
	return models.DLPEvidence{
		MessageID:    parseStr(row["message_id"]),
		SessionID:    parseStr(row["session_id"]),
		RunID:        parseStr(row["run_id"]),
		EventTime:    parseStr(row["event_time"]),
		AgentID:      parseStr(row["agent_id"]),
		AgentType:    parseStr(row["agent_type"]),
		DeploymentID: parseStr(row["deployment_id"]),
		Role:         parseStr(row["role"]),
		Content:      content,
		RiskTags:     rowStrSlice(row, "risk_tags"),
		RiskScore:    parseFloat64(row["risk_score"]),
		RerankScore:  rerankScore,
	}
}

// ── Row-value helpers ─────────────────────────────────────────────────────────
// Twins of the unexported helpers in internal/handlers/helpers.go (parseStr,
// parseFloat64, rowStrSlice). Duplicated here — rather than imported — to
// avoid an import cycle: handlers imports dlpquery, not the other way round.
// Keep both copies in sync if the row-decoding behavior ever changes.

// parseStr safely casts any to string. DateTime columns arrive as time.Time
// through the native driver; format them like Timeplus to_string(datetime64).
func parseStr(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case time.Time:
		return s.UTC().Format("2006-01-02 15:04:05.000")
	}
	return ""
}

// parseFloat64 handles float64 / json.Number from Proton rows.
// Also handles float32 and integer types from the native driver.
func parseFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case interface{ Float64() (float64, error) }:
		if f, err := n.Float64(); err == nil {
			return f
		}
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	case int32:
		return float64(n)
	case uint32:
		return float64(n)
	}
	return 0
}

// rowStrSlice extracts a string slice from a row value, handling both
// []any (HTTP+JSON path) and []string (native driver path).
func rowStrSlice(row map[string]any, key string) []string {
	var out []string
	switch v := row[key].(type) {
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = v
	}
	return out
}
