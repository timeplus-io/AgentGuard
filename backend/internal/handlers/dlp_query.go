package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/dlpquery"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

const (
	dlpDefaultTopN = 20
	dlpMaxTopN     = 50
)

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
			fmt.Sprintf(`SELECT dlp_understand_query('%s', %s) AS intent`, escapeSQLStr(question), dlpquery.ResolvedCfg(tp.DLPCfgKeyUnderstand)))
		if err != nil {
			fail("understand", err)
			return
		}
		rawIntent := ""
		if len(intentRows) == 1 {
			rawIntent = parseStr(intentRows[0]["intent"])
		}
		intent := dlpquery.ParseIntent(rawIntent, question)
		dlpStage(c, flusher, gin.H{"stage": "intent", "intent": intent})

		// 2. Multi-recall (content columns → native protocol).
		structuredSQL, vectorSQL := dlpquery.BuildRecallSQL(intent, question)
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
		candidates := dlpquery.MergeCandidates(structuredRows, vectorRows, dlpquery.CandidateCap)
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
			if rerankN > dlpquery.RerankCap {
				rerankN = dlpquery.RerankCap
			}
			ids := make([]string, rerankN)
			for i := 0; i < rerankN; i++ {
				ids[i] = parseStr(candidates[i]["message_id"])
			}
			scoreRows, err := tp.QueryNative(&cfg.Timeplus, dlpquery.BuildRerankSQL(question, ids))
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
			evidence[i] = dlpquery.RowToEvidence(row, scores[parseStr(row["message_id"])])
		}
		evidenceJSON, _ := json.Marshal(evidence)
		report := ""
		reportRows, err := tp.Query(&cfg.Timeplus, fmt.Sprintf(
			`SELECT dlp_summarize('%s', '%s', %s) AS report`,
			escapeSQLStr(question), escapeSQLStr(string(evidenceJSON)), dlpquery.ResolvedCfg(tp.DLPCfgKeySummarize)))
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
