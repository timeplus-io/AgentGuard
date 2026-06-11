package handlers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

const (
	dlpStructuredLimit = 200
	dlpVectorLimit     = 100
	dlpCandidateCap    = 300
	dlpEvidenceChars   = 500
	dlpDefaultTopN     = 20
	dlpMaxTopN         = 50
)

var dlpWordRe = regexp.MustCompile(`[A-Za-z0-9]{4,}`)

// parseDLPIntent decodes the dlp_understand_query JSON; on failure it derives
// keywords from the question itself. The window is clamped to [1, 365].
func parseDLPIntent(raw, question string) models.DLPIntent {
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
       agent_type, agent_id, content, risk_tags, risk_score`

// buildDLPRecallSQL produces the two recall channels. The structured channel
// catches rows by entity/tag/keyword predicates regardless of embedding
// presence; the vector channel ranks embedded rows by cosine distance to the
// question embedding (dlp_embed runs inside Timeplus). Rows with empty
// embeddings would make cosine_distance error, hence the length() guard.
func buildDLPRecallSQL(intent models.DLPIntent, question string) (structured, vector string) {
	timeFilter := fmt.Sprintf("event_time > now() - INTERVAL %d DAY", intent.TimeRangeDays)

	var preds []string
	for _, et := range intent.EntityTypes {
		preds = append(preds, fmt.Sprintf("not empty(entities['%s'])", escapeSQLStr(et)))
	}
	for _, tag := range intent.RiskTags {
		preds = append(preds, fmt.Sprintf("has(risk_tags, '%s')", escapeSQLStr(tag)))
	}
	for _, kw := range intent.Keywords {
		preds = append(preds, fmt.Sprintf("position(lower(content), '%s') > 0", escapeSQLStr(strings.ToLower(kw))))
	}
	where := timeFilter
	if len(preds) > 0 {
		where += " AND (" + strings.Join(preds, " OR ") + ")"
	}
	structured = fmt.Sprintf(`SELECT %s
FROM table(agentguard_dlp_enriched)
WHERE %s
ORDER BY risk_score DESC, event_time DESC
LIMIT %d`, dlpRecallCols, where, dlpStructuredLimit)

	vector = fmt.Sprintf(`SELECT %s,
       cosine_distance(embedding, dlp_embed('%s', 'user')) AS dist
FROM table(agentguard_dlp_enriched)
WHERE %s AND length(embedding) > 0
ORDER BY dist ASC
LIMIT %d`, dlpRecallCols, escapeSQLStr(question), timeFilter, dlpVectorLimit)
	return structured, vector
}

// mergeDLPCandidates unions the channels in order, deduping by message_id.
func mergeDLPCandidates(a, b []tp.Row, cap int) []tp.Row {
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

// buildDLPRerankSQL scores candidates inside Timeplus via the dlp_rerank UDF.
func buildDLPRerankSQL(question string, ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = "'" + escapeSQLStr(id) + "'"
	}
	return fmt.Sprintf(`SELECT message_id, dlp_rerank('%s', content) AS score
FROM table(agentguard_dlp_enriched)
WHERE message_id IN (%s)`, escapeSQLStr(question), strings.Join(quoted, ", "))
}

func rowStr(row tp.Row, key string) string {
	s, _ := row[key].(string)
	return s
}

func rowStrSlice(row tp.Row, key string) []string {
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

// rowToDLPEvidence maps a recall row to a cited-evidence record, truncating
// content for the summarizer/UI.
func rowToDLPEvidence(row tp.Row, rerankScore float64) models.DLPEvidence {
	content := rowStr(row, "content")
	if len(content) > dlpEvidenceChars {
		content = content[:dlpEvidenceChars]
	}
	return models.DLPEvidence{
		MessageID:   rowStr(row, "message_id"),
		SessionID:   rowStr(row, "session_id"),
		RunID:       rowStr(row, "run_id"),
		EventTime:   rowStr(row, "event_time"),
		AgentID:     rowStr(row, "agent_id"),
		AgentType:   rowStr(row, "agent_type"),
		Role:        rowStr(row, "role"),
		Content:     content,
		RiskTags:    rowStrSlice(row, "risk_tags"),
		RiskScore:   parseFloat64(row["risk_score"]),
		RerankScore: rerankScore,
	}
}
