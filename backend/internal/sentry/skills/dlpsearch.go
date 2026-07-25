package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/timeplus/agentguard/internal/dlpquery"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

const (
	dlpSearchDefaultTopN = 20
	dlpSearchMaxTopN     = 50

	dlpNotEnabledMsg = "DLP is not enabled/provisioned on this deployment — answer from other streams instead"
)

// dlpSearchRow/dlpSearchFloat mirror the row-decoding helpers duplicated in
// internal/dlpquery/recall.go and internal/handlers/helpers.go — this package
// needs its own copy for the same import-cycle reason documented there
// (dlpquery/handlers must not import skills, and vice versa).
func dlpSearchStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

type dlpSearch struct{}

// NewDLPSearch returns the read-only "dlp_search" skill: it reuses the
// Semantic DLP funnel's recall+rerank stages (internal/dlpquery, extracted
// from internal/handlers/dlp_query.go) as a callable tool, skipping the
// summarize stage and returning cited evidence as JSON.
func NewDLPSearch() Skill { return dlpSearch{} }

func (dlpSearch) Name() string { return "dlp_search" }
func (dlpSearch) Description() string {
	return "Semantically search conversation messages for PII/secret leaks and other DLP-tagged content. " +
		"Runs the Semantic DLP recall+rerank funnel against agentguard_dlp_enriched and returns the top " +
		"matching messages as evidence JSON (message_id, session_id, content, risk_tags, risk_score, ...). " +
		"Returns a note instead of results when Semantic DLP isn't enabled/provisioned."
}
func (dlpSearch) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{"type": "string"},
			"top_n":    map[string]any{"type": "integer"},
		},
		"required": []string{"question"},
	}
}
func (dlpSearch) Access() AccessClass { return ReadOnly }

func (dlpSearch) Execute(_ context.Context, rc *RunContext, args json.RawMessage) (string, error) {
	if !rc.Cfg.DLP.Enabled {
		return dlpNotEnabledMsg, nil
	}

	var a struct {
		Question string `json:"question"`
		TopN     int    `json:"top_n"`
	}
	if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Question) == "" {
		return "error: dlp_search needs a {\"question\": \"...\"} argument", nil
	}
	question := strings.TrimSpace(a.Question)
	topN := a.TopN
	if topN <= 0 {
		topN = dlpSearchDefaultTopN
	}
	if topN > dlpSearchMaxTopN {
		topN = dlpSearchMaxTopN
	}

	// Provisioning guard — same check DLPQuery does before streaming, fail
	// closed on any query error (missing streams look the same as an
	// unreachable Timeplus from the agent's point of view).
	provRows, err := tp.Query(&rc.Cfg.Timeplus, `
		SELECT name FROM system.tables
		WHERE name IN ('agentguard_dlp_enriched', 'mv_dlp_enrich')
		  AND database = current_database()`)
	if err != nil || len(provRows) < 2 {
		return dlpNotEnabledMsg, nil
	}

	// 1. Understand.
	rawIntent := ""
	intentRows, err := tp.Query(&rc.Cfg.Timeplus, fmt.Sprintf(
		`SELECT dlp_understand_query('%s', %s) AS intent`,
		dlpquery.EscapeSQLStr(question), dlpquery.ResolvedCfg(tp.DLPCfgKeyUnderstand)))
	if err != nil {
		log.Printf("dlp_search: understand (falling back to keyword intent): %v", err)
	} else if len(intentRows) == 1 {
		rawIntent = dlpSearchStr(intentRows[0]["intent"])
	}
	intent := dlpquery.ParseIntent(rawIntent, question)

	// 2. Multi-recall (content columns → native protocol).
	structuredSQL, vectorSQL := dlpquery.BuildRecallSQL(intent, question)
	structuredRows, err := tp.QueryNative(&rc.Cfg.Timeplus, structuredSQL)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	vectorRows, err := tp.QueryNative(&rc.Cfg.Timeplus, vectorSQL)
	if err != nil {
		// Vector channel is best-effort (e.g. embedding dim drift); the
		// structured channel alone still answers. Never bubble this up.
		log.Printf("dlp_search: vector recall (continuing structured-only): %v", err)
		vectorRows = nil
	}
	candidates := dlpquery.MergeCandidates(structuredRows, vectorRows, dlpquery.CandidateCap)

	if len(candidates) == 0 {
		return "no matching messages found in the selected time window", nil
	}

	// 3. Rerank inside Timeplus; on failure fall back to recall order.
	scores := make(map[string]float64, len(candidates))
	if len(candidates) > topN {
		rerankN := len(candidates)
		if rerankN > dlpquery.RerankCap {
			rerankN = dlpquery.RerankCap
		}
		ids := make([]string, rerankN)
		for i := 0; i < rerankN; i++ {
			ids[i] = dlpSearchStr(candidates[i]["message_id"])
		}
		scoreRows, err := tp.QueryNative(&rc.Cfg.Timeplus, dlpquery.BuildRerankSQL(question, ids))
		if err != nil {
			log.Printf("dlp_search: rerank (falling back to recall order): %v", err)
		} else {
			for _, row := range scoreRows {
				scores[dlpSearchStr(row["message_id"])] = dlpSearchFloat(row["score"])
			}
			sort.SliceStable(candidates, func(i, j int) bool {
				return scores[dlpSearchStr(candidates[i]["message_id"])] > scores[dlpSearchStr(candidates[j]["message_id"])]
			})
		}
		candidates = candidates[:topN]
	}

	evidence := make([]models.DLPEvidence, len(candidates))
	for i, row := range candidates {
		evidence[i] = dlpquery.RowToEvidence(row, scores[dlpSearchStr(row["message_id"])])
	}

	b, err := json.Marshal(evidence)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	out, _ := Truncate(string(b), rc.ResultMaxBytes)
	return out, nil
}

// dlpSearchFloat handles float64 / json.Number from Proton rows (twin of
// internal/dlpquery's unexported parseFloat64 — see the import-cycle note
// on dlpSearchStr above).
func dlpSearchFloat(v any) float64 {
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
