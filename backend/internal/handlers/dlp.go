package handlers

import (
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

var dlpUDFNames = []string{
	"dlp_extract_entities", "dlp_embed", "dlp_risk",
	"dlp_understand_query", "dlp_rerank", "dlp_summarize",
}

// DLPStatusHandler reports Semantic DLP provisioning + enrichment health.
func DLPStatusHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		status := models.DLPStatus{
			Enabled: cfg.DLP.Enabled,
			UDFs:    map[string]bool{},
		}
		for _, fn := range dlpUDFNames {
			status.UDFs[fn] = false
		}
		if !cfg.DLP.Enabled {
			c.JSON(http.StatusOK, status)
			return
		}

		rows, err := tp.Query(&cfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name IN ('agentguard_dlp_enriched', 'mv_dlp_enrich')
			  AND database = current_database()`)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		for _, row := range rows {
			switch row["name"] {
			case "agentguard_dlp_enriched":
				status.StreamExists = true
			case "mv_dlp_enrich":
				status.MVExists = true
			}
		}

		if fnRows, err := tp.Query(&cfg.Timeplus, `SHOW FUNCTIONS`); err == nil {
			for _, row := range fnRows {
				if name, ok := row["name"].(string); ok {
					if _, tracked := status.UDFs[name]; tracked {
						status.UDFs[name] = true
					}
				}
			}
		} else {
			log.Printf("dlp: status SHOW FUNCTIONS: %v", err)
		}

		if status.StreamExists {
			counts, err := tp.Query(&cfg.Timeplus, `
				SELECT count() AS total,
				       count_if(length(embedding) > 0) AS embedded,
				       count_if(risk_score > 0) AS flagged
				FROM table(agentguard_dlp_enriched)`)
			if err == nil && len(counts) == 1 {
				status.TotalRows = parseInt64(counts[0]["total"])
				status.EmbeddedRows = parseInt64(counts[0]["embedded"])
				status.FlaggedRows = parseInt64(counts[0]["flagged"])
				if status.TotalRows > 0 {
					status.EmbeddingCoverage = float64(status.EmbeddedRows) / float64(status.TotalRows)
				}
			} else if err != nil {
				log.Printf("dlp: status counts: %v", err)
			}
		}

		c.JSON(http.StatusOK, status)
	}
}

type dlpMessagesFilters struct {
	RiskTag    string
	EntityType string
	AgentID    string
	SessionID  string
	Role       string
	Since      string
	Until      string
	MinScore   float64
}

// buildDLPMessagesWhere returns the WHERE clause for the enriched-message
// browser. Returns "1 = 1" when no filters are set. (Second return value is
// reserved for future parameterized queries; currently always nil.)
func buildDLPMessagesWhere(f dlpMessagesFilters) (string, []any) {
	var preds []string
	if f.RiskTag != "" {
		preds = append(preds, fmt.Sprintf("has(risk_tags, '%s')", escapeSQLStr(f.RiskTag)))
	}
	if f.EntityType != "" {
		preds = append(preds, fmt.Sprintf("not empty(entities['%s'])", escapeSQLStr(f.EntityType)))
	}
	if f.AgentID != "" {
		preds = append(preds, fmt.Sprintf("agent_id = '%s'", escapeSQLStr(f.AgentID)))
	}
	if f.SessionID != "" {
		preds = append(preds, fmt.Sprintf("session_id = '%s'", escapeSQLStr(f.SessionID)))
	}
	if f.Role != "" {
		preds = append(preds, fmt.Sprintf("role = '%s'", escapeSQLStr(f.Role)))
	}
	if f.Since != "" {
		preds = append(preds, fmt.Sprintf("event_time >= '%s'", escapeSQLStr(f.Since)))
	}
	if f.Until != "" {
		preds = append(preds, fmt.Sprintf("event_time <= '%s'", escapeSQLStr(f.Until)))
	}
	if f.MinScore > 0 {
		preds = append(preds, fmt.Sprintf("risk_score >= %f", f.MinScore))
	}
	if len(preds) == 0 {
		return "1 = 1", nil
	}
	return strings.Join(preds, " AND "), nil
}

// buildDLPProfilesSQLs returns the three ranking queries (agents, sessions,
// tags) over flagged rows in the window. Tags are expanded with array_join.
func buildDLPProfilesSQLs(days int) []string {
	base := func(keyExpr string) string {
		return fmt.Sprintf(`SELECT %s AS key,
       count() AS flagged_count,
       max(risk_score) AS max_risk_score,
       avg(risk_score) AS avg_risk_score,
       to_string(max(event_time)) AS last_seen
FROM table(agentguard_dlp_enriched)
WHERE risk_score > 0 AND event_time > now() - INTERVAL %d DAY
GROUP BY key
ORDER BY flagged_count DESC, max_risk_score DESC
LIMIT 10`, keyExpr, days)
	}
	return []string{
		base("agent_id"),
		base("session_id"),
		base("array_join(risk_tags)"),
	}
}

// ListEnrichedMessages browses agentguard_dlp_enriched with filters.
// GET /api/dlp/messages?risk_tag=&entity_type=&agent_id=&session_id=&role=&since=&until=&min_score=&page=&limit=
func ListEnrichedMessages(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		minScore, _ := strconv.ParseFloat(c.Query("min_score"), 64)
		if math.IsInf(minScore, 0) || math.IsNaN(minScore) || minScore < 0 {
			minScore = 0
		}

		var since, until string
		if s := c.Query("since"); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid since: use RFC3339"})
				return
			}
			since = t.UTC().Format("2006-01-02 15:04:05.999")
		}
		if u := c.Query("until"); u != "" {
			t, err := time.Parse(time.RFC3339, u)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid until: use RFC3339"})
				return
			}
			until = t.UTC().Format("2006-01-02 15:04:05.999")
		}

		filters := dlpMessagesFilters{
			RiskTag:    c.Query("risk_tag"),
			EntityType: c.Query("entity_type"),
			AgentID:    c.Query("agent_id"),
			SessionID:  c.Query("session_id"),
			Role:       c.Query("role"),
			Since:      since,
			Until:      until,
			MinScore:   minScore,
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		if page < 1 {
			page = 1
		}
		if limit < 1 || limit > 500 {
			limit = 50
		}
		where, _ := buildDLPMessagesWhere(filters)

		countRows, err := tp.Query(&cfg.Timeplus, fmt.Sprintf(
			`SELECT count() AS total FROM table(agentguard_dlp_enriched) WHERE %s`, where))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		total := int64(0)
		if len(countRows) == 1 {
			total = parseInt64(countRows[0]["total"])
		}

		if total == 0 || int64((page-1)*limit) >= total {
			c.JSON(http.StatusOK, gin.H{"messages": []models.DLPEnrichedMessage{}, "total": total, "page": page, "limit": limit})
			return
		}

		rows, err := tp.QueryNative(&cfg.Timeplus, fmt.Sprintf(`
			SELECT to_string(event_time) AS event_time, message_id, session_id, run_id, role,
			       agent_type, agent_id, deployment_id, model, tool_name, content,
			       entities, risk_tags, risk_score, length(embedding) AS embedding_dim
			FROM table(agentguard_dlp_enriched)
			WHERE %s
			ORDER BY event_time DESC
			LIMIT %d OFFSET %d`, where, limit, (page-1)*limit))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		msgs := make([]models.DLPEnrichedMessage, 0, len(rows))
		for _, row := range rows {
			msgs = append(msgs, rowToDLPMessage(row))
		}
		c.JSON(http.StatusOK, gin.H{"messages": msgs, "total": total, "page": page, "limit": limit})
	}
}

// GetEnrichedMessage returns one enriched record by message_id.
func GetEnrichedMessage(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		rows, err := tp.QueryNative(&cfg.Timeplus, fmt.Sprintf(`
			SELECT to_string(event_time) AS event_time, message_id, session_id, run_id, role,
			       agent_type, agent_id, deployment_id, model, tool_name, content,
			       entities, risk_tags, risk_score, length(embedding) AS embedding_dim
			FROM table(agentguard_dlp_enriched)
			WHERE message_id = '%s'
			LIMIT 1`, escapeSQLStr(c.Param("id"))))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		if len(rows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "message not found"})
			return
		}
		c.JSON(http.StatusOK, rowToDLPMessage(rows[0]))
	}
}

// DLPProfilesHandler returns top risk agents / sessions / tags over a window.
// GET /api/dlp/profiles?days=30
func DLPProfilesHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
		if days < 1 || days > 365 {
			days = 30
		}
		sqls := buildDLPProfilesSQLs(days)
		out := models.DLPProfiles{
			WindowDays:  days,
			TopAgents:   []models.DLPProfileEntry{},
			TopSessions: []models.DLPProfileEntry{},
			TopTags:     []models.DLPProfileEntry{},
		}
		dests := []*[]models.DLPProfileEntry{&out.TopAgents, &out.TopSessions, &out.TopTags}
		for i, sql := range sqls {
			rows, err := tp.Query(&cfg.Timeplus, sql)
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
				return
			}
			for _, row := range rows {
				*dests[i] = append(*dests[i], models.DLPProfileEntry{
					Key:          parseStr(row["key"]),
					FlaggedCount: parseInt64(row["flagged_count"]),
					MaxRiskScore: parseFloat64(row["max_risk_score"]),
					AvgRiskScore: parseFloat64(row["avg_risk_score"]),
					LastSeen:     parseStr(row["last_seen"]),
				})
			}
		}
		c.JSON(http.StatusOK, out)
	}
}

// rowToDLPMessage maps an enriched row (native or HTTP) to the API model.
func rowToDLPMessage(row tp.Row) models.DLPEnrichedMessage {
	entities := map[string][]string{}
	switch v := row["entities"].(type) {
	case map[string][]string:
		entities = v
	case map[string]any:
		for k, raw := range v {
			if arr, ok := raw.([]any); ok {
				var vals []string
				for _, x := range arr {
					if s, ok := x.(string); ok {
						vals = append(vals, s)
					}
				}
				entities[k] = vals
			}
		}
	}
	return models.DLPEnrichedMessage{
		EventTime:    parseStr(row["event_time"]),
		MessageID:    parseStr(row["message_id"]),
		SessionID:    parseStr(row["session_id"]),
		RunID:        parseStr(row["run_id"]),
		Role:         parseStr(row["role"]),
		AgentType:    parseStr(row["agent_type"]),
		AgentID:      parseStr(row["agent_id"]),
		DeploymentID: parseStr(row["deployment_id"]),
		Model:        parseStr(row["model"]),
		ToolName:     parseStr(row["tool_name"]),
		Content:      parseStr(row["content"]),
		Entities:     entities,
		RiskTags:     rowStrSlice(row, "risk_tags"),
		RiskScore:    parseFloat64(row["risk_score"]),
		EmbeddingDim: int(parseInt64(row["embedding_dim"])),
	}
}
