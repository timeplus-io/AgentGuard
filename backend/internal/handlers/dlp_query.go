package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
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
	// dlpRerankCap bounds how many recall candidates are sent to the LLM
	// reranker. Recall is already ordered (risk_score / vector distance), so we
	// rerank the most promising prefix and leave the rest at the 0.0 default.
	// A local model scores ~CHUNK items per call; this keeps the rerank query
	// well under the 60s native timeout.
	dlpRerankCap = 40
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
       agent_type, agent_id, deployment_id, content, risk_tags, risk_score`

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

// rowToDLPEvidence maps a recall row to a cited-evidence record, truncating
// content for the summarizer/UI.
func rowToDLPEvidence(row tp.Row, rerankScore float64) models.DLPEvidence {
	content := parseStr(row["content"])
	if len(content) > dlpEvidenceChars {
		i := dlpEvidenceChars
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

// dlpStage writes one NDJSON stage event and flushes it to the client.
func dlpStage(c *gin.Context, flusher http.Flusher, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = c.Writer.Write(data)
	_, _ = c.Writer.Write([]byte{'\n'})
	if flusher != nil {
		flusher.Flush()
	}
}

// DLPQuery runs the NL forensic funnel: understand → multi-recall → rerank →
// summarize, streaming NDJSON stage events:
//
//	{"stage":"intent","intent":{...}}
//	{"stage":"candidates","count":N,"structured":N1,"vector":N2}
//	{"stage":"reranked","kept":K}
//	{"stage":"answer","report":"...","evidence":[...]}
//	{"stage":"error","error":"..."}   (terminal, any point after headers)
func DLPQuery(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		if !cfg.DLP.Enabled {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Semantic DLP is disabled (dlp.enabled=false)"})
			return
		}

		var body struct {
			Question string `json:"question"`
			TopN     int    `json:"top_n"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Question) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "question is required"})
			return
		}
		question := strings.TrimSpace(body.Question)
		topN := body.TopN
		if topN <= 0 {
			topN = dlpDefaultTopN
		}
		if topN > dlpMaxTopN {
			topN = dlpMaxTopN
		}

		// Provision check happens BEFORE streaming headers so failures are
		// plain JSON the client can handle uniformly.
		provRows, err := tp.Query(&cfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name IN ('agentguard_dlp_enriched', 'mv_dlp_enrich')
			  AND database = current_database()`)
		if err != nil || len(provRows) < 2 {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "Semantic DLP is not provisioned; run setup or POST /api/rules/sync"})
			return
		}

		c.Header("Content-Type", "application/x-ndjson")
		c.Header("X-Accel-Buffering", "no")
		c.Header("Cache-Control", "no-cache")
		c.Status(http.StatusOK)
		flusher, _ := c.Writer.(http.Flusher)

		fail := func(stage string, err error) {
			log.Printf("dlp: query %s: %v", stage, err)
			dlpStage(c, flusher, gin.H{"stage": "error", "error": stage + ": " + err.Error()})
		}

		// 1. Understand.
		intentRows, err := tp.Query(&cfg.Timeplus,
			fmt.Sprintf(`SELECT dlp_understand_query('%s') AS intent`, escapeSQLStr(question)))
		if err != nil {
			fail("understand", err)
			return
		}
		rawIntent := ""
		if len(intentRows) == 1 {
			rawIntent = parseStr(intentRows[0]["intent"])
		}
		intent := parseDLPIntent(rawIntent, question)
		dlpStage(c, flusher, gin.H{"stage": "intent", "intent": intent})

		// 2. Multi-recall (content columns → native protocol).
		structuredSQL, vectorSQL := buildDLPRecallSQL(intent, question)
		structuredRows, err := tp.QueryNative(&cfg.Timeplus, structuredSQL)
		if err != nil {
			fail("recall", err)
			return
		}
		vectorRows, err := tp.QueryNative(&cfg.Timeplus, vectorSQL)
		if err != nil {
			// Vector channel is best-effort (e.g. embedding dim drift); the
			// structured channel alone still answers.
			log.Printf("dlp: query vector recall (continuing structured-only): %v", err)
			vectorRows = nil
		}
		candidates := mergeDLPCandidates(structuredRows, vectorRows, dlpCandidateCap)
		dlpStage(c, flusher, gin.H{"stage": "candidates", "count": len(candidates),
			"structured": len(structuredRows), "vector": len(vectorRows)})

		if len(candidates) == 0 {
			dlpStage(c, flusher, gin.H{"stage": "answer",
				"report":   "No matching messages found in the selected time window.",
				"evidence": []models.DLPEvidence{}})
			return
		}

		// 3. Rerank inside Timeplus; on failure fall back to recall order.
		scores := make(map[string]float64, len(candidates))
		if len(candidates) > topN {
			// Rerank only the most promising prefix of recall candidates — a
			// local LLM cannot score 150+ items within the query timeout.
			// Un-reranked candidates keep the 0.0 default and stay in recall
			// order behind the scored ones.
			rerankN := len(candidates)
			if rerankN > dlpRerankCap {
				rerankN = dlpRerankCap
			}
			ids := make([]string, rerankN)
			for i := 0; i < rerankN; i++ {
				ids[i] = parseStr(candidates[i]["message_id"])
			}
			scoreRows, err := tp.QueryNative(&cfg.Timeplus, buildDLPRerankSQL(question, ids))
			if err != nil {
				log.Printf("dlp: query rerank (falling back to recall order): %v", err)
			} else {
				for _, row := range scoreRows {
					scores[parseStr(row["message_id"])] = parseFloat64(row["score"])
				}
				sort.SliceStable(candidates, func(i, j int) bool {
					return scores[parseStr(candidates[i]["message_id"])] > scores[parseStr(candidates[j]["message_id"])]
				})
			}
			candidates = candidates[:topN]
			dlpStage(c, flusher, gin.H{"stage": "reranked", "kept": len(candidates)})
		} else {
			dlpStage(c, flusher, gin.H{"stage": "reranked", "kept": len(candidates), "skipped": true})
		}

		// 4. Summarize over the top-N evidence.
		evidence := make([]models.DLPEvidence, len(candidates))
		for i, row := range candidates {
			evidence[i] = rowToDLPEvidence(row, scores[parseStr(row["message_id"])])
		}
		evidenceJSON, _ := json.Marshal(evidence)
		report := ""
		reportRows, err := tp.Query(&cfg.Timeplus, fmt.Sprintf(
			`SELECT dlp_summarize('%s', '%s') AS report`,
			escapeSQLStr(question), escapeSQLStr(string(evidenceJSON))))
		if err != nil {
			log.Printf("dlp: query summarize (returning evidence without report): %v", err)
		} else if len(reportRows) == 1 {
			report = parseStr(reportRows[0]["report"])
		}
		if report == "" {
			report = fmt.Sprintf("Model summary unavailable. Top %d candidate messages are attached as evidence.", len(evidence))
		}
		dlpStage(c, flusher, gin.H{"stage": "answer", "report": report, "evidence": evidence})
	}
}
