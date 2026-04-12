package handlers

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	tp "github.com/timeplus/agentshield/internal/timeplus"
)

// threatKey uniquely identifies a deduplicated threat.
type threatKey struct{ agentID, deploymentID, sessionID, ruleID string }

// computeThreatID returns the MD5 hex string of the four key fields.
// Uses length-prefix encoding to prevent hash collisions when field values contain hyphens.
// Used as the API path parameter (:id) — never stored in Proton.
func computeThreatID(agentID, deploymentID, sessionID, ruleID string) string {
	encoded := fmt.Sprintf("%d:%s\x00%d:%s\x00%d:%s\x00%d:%s",
		len(agentID), agentID, len(deploymentID), deploymentID,
		len(sessionID), sessionID, len(ruleID), ruleID)
	h := md5.Sum([]byte(encoded))
	return hex.EncodeToString(h[:])
}

// parseProtonTime parses a datetime64(3) value as returned by the Proton HTTP interface.
// Proton may return "2006-01-02 15:04:05.000 UTC" or "2006-01-02 15:04:05.000 +0000".
func parseProtonTime(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02 15:04:05.000 UTC",
		"2006-01-02 15:04:05.000 MST",
		"2006-01-02 15:04:05.000 -0700",
		"2006-01-02 15:04:05.000",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

const sqlListThreats = `
SELECT agent_id, deployment_id, session_id, rule_id,
       rule_name, severity, last_seen, status, status_updated_at
FROM table(agentshield_threats)`

const sqlThreatStats = `
SELECT agent_id, deployment_id, session_id, rule_id,
       count()          AS event_count,
       min(detected_at) AS first_seen
FROM table(agentshield_security_events)
GROUP BY agent_id, deployment_id, session_id, rule_id`

const sqlThreatEvents = `
SELECT agent_id, deployment_id, session_id, rule_id,
       detected_at, run_id, hook_name, tool_name, message, event_data
FROM table(agentshield_security_events)
ORDER BY detected_at DESC`

// buildThreatsList runs three Proton snapshot queries concurrently, merges the results,
// fires re-open upserts for threats whose last_seen is newer than their cleared_at, and
// returns the full sorted slice. Filters and pagination are left to the caller.
func buildThreatsList(cfg *config.Config) ([]models.Threat, error) {
	type queryResult struct {
		rows []tp.Row
		err  error
	}

	ch1 := make(chan queryResult, 1)
	ch2 := make(chan queryResult, 1)
	ch3 := make(chan queryResult, 1)

	go func() {
		rows, err := tp.Query(&cfg.Timeplus, sqlListThreats)
		ch1 <- queryResult{rows, err}
	}()
	go func() {
		rows, err := tp.Query(&cfg.Timeplus, sqlThreatStats)
		ch2 <- queryResult{rows, err}
	}()
	go func() {
		rows, err := tp.Query(&cfg.Timeplus, sqlThreatEvents)
		ch3 <- queryResult{rows, err}
	}()

	r1, r2, r3 := <-ch1, <-ch2, <-ch3

	if r1.err != nil {
		return nil, r1.err
	}
	if r2.err != nil {
		return nil, r2.err
	}
	if r3.err != nil {
		return nil, r3.err
	}

	// Build stats map from Query 2 (event_count + first_seen per key).
	type statsEntry struct {
		eventCount int64
		firstSeen  string
	}
	statsMap := make(map[threatKey]statsEntry, len(r2.rows))
	for _, row := range r2.rows {
		k := threatKey{
			parseStr(row["agent_id"]),
			parseStr(row["deployment_id"]),
			parseStr(row["session_id"]),
			parseStr(row["rule_id"]),
		}
		statsMap[k] = statsEntry{
			eventCount: parseInt64(row["event_count"]),
			firstSeen:  parseStr(row["first_seen"]),
		}
	}

	// Build events map from Query 3 ([]ThreatEvent per key, newest first from ORDER BY).
	// Cap at 50 events per threat to bound memory usage.
	const maxEventsPerThreat = 50
	eventsMap := make(map[threatKey][]models.ThreatEvent, len(r1.rows))
	for _, row := range r3.rows {
		k := threatKey{
			parseStr(row["agent_id"]),
			parseStr(row["deployment_id"]),
			parseStr(row["session_id"]),
			parseStr(row["rule_id"]),
		}
		if len(eventsMap[k]) >= maxEventsPerThreat {
			continue
		}
		eventsMap[k] = append(eventsMap[k], models.ThreatEvent{
			DetectedAt: parseStr(row["detected_at"]),
			RunID:      parseStr(row["run_id"]),
			HookName:   parseStr(row["hook_name"]),
			ToolName:   parseStr(row["tool_name"]),
			Message:    parseStr(row["message"]),
			EventData:  parseStr(row["event_data"]),
		})
	}

	// Collect threats that need re-opening.
	var toReopen []threatKey

	threats := make([]models.Threat, 0, len(r1.rows))
	for _, row := range r1.rows {
		k := threatKey{
			parseStr(row["agent_id"]),
			parseStr(row["deployment_id"]),
			parseStr(row["session_id"]),
			parseStr(row["rule_id"]),
		}

		// Skip threats with no session_id: the mutable stream primary key includes
		// session_id, so an empty value would collapse all sessions into one key,
		// making status management (acknowledge/clear) affect all sessions at once.
		// Root cause: hook contexts such as tool_result_persist do not carry sessionId.
		if k.sessionID == "" {
			continue
		}

		status := parseStr(row["status"])
		if status == "" {
			status = "open"
		}
		statusUpdatedAt := parseStr(row["status_updated_at"])
		lastSeen := parseStr(row["last_seen"])

		// Re-open: if cleared but new events arrived after the clear.
		if status == "cleared" && statusUpdatedAt != "" && lastSeen != "" {
			ls, errLS := parseProtonTime(lastSeen)
			su, errSU := parseProtonTime(statusUpdatedAt)
			if errLS == nil && errSU == nil && ls.After(su) {
				status = "open"
				toReopen = append(toReopen, k)
			}
		}

		stats := statsMap[k]
		events := eventsMap[k]
		if events == nil {
			events = []models.ThreatEvent{}
		}

		threats = append(threats, models.Threat{
			ThreatID:        computeThreatID(k.agentID, k.deploymentID, k.sessionID, k.ruleID),
			AgentID:         k.agentID,
			DeploymentID:    k.deploymentID,
			SessionID:       k.sessionID,
			RuleID:          k.ruleID,
			RuleName:        parseStr(row["rule_name"]),
			Severity:        parseStr(row["severity"]),
			EventCount:      stats.eventCount,
			FirstSeen:       stats.firstSeen,
			LastSeen:        lastSeen,
			Status:          status,
			StatusUpdatedAt: statusUpdatedAt,
			Events:          events,
		})
	}

	// Fire-and-forget: upsert re-opened threats back to agentshield_threats.
	if len(toReopen) > 0 {
		go func(keys []threatKey) {
			for _, k := range keys {
				sql := fmt.Sprintf(
					`INSERT INTO agentshield_threats (agent_id, deployment_id, session_id, rule_id, status, status_updated_at) VALUES ('%s', '%s', '%s', '%s', 'open', now64(3))`,
					escapeSQL(k.agentID), escapeSQL(k.deploymentID),
					escapeSQL(k.sessionID), escapeSQL(k.ruleID),
				)
				_ = tp.ExecHTTP(&cfg.Timeplus, sql)
			}
		}(toReopen)
	}

	// Sort by last_seen descending (string comparison works for ISO timestamps).
	for i := 0; i < len(threats)-1; i++ {
		for j := i + 1; j < len(threats); j++ {
			if threats[j].LastSeen > threats[i].LastSeen {
				threats[i], threats[j] = threats[j], threats[i]
			}
		}
	}

	return threats, nil
}

// ListThreats handles GET /api/threats.
// Query params: ?severity=, ?status=, ?agent_id=, ?page=, ?limit=
func ListThreats(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		severityFilter := c.Query("severity")
		statusFilter := c.Query("status")
		agentIDFilter := c.Query("agent_id")
		pageStr := c.DefaultQuery("page", "1")
		limitStr := c.DefaultQuery("limit", "20")

		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			page = 1
		}
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			limit = 20
		}

		threats, err := buildThreatsList(cfg)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Apply filters.
		filtered := make([]models.Threat, 0, len(threats))
		for _, t := range threats {
			if severityFilter != "" && t.Severity != severityFilter {
				continue
			}
			if statusFilter != "" && t.Status != statusFilter {
				continue
			}
			if agentIDFilter != "" && t.AgentID != agentIDFilter {
				continue
			}
			filtered = append(filtered, t)
		}

		// Paginate.
		total := len(filtered)
		start := (page - 1) * limit
		if start >= total {
			c.JSON(http.StatusOK, models.ThreatListResponse{
				Threats: []models.Threat{},
				Total:   total, Page: page, Limit: limit,
			})
			return
		}
		end := start + limit
		if end > total {
			end = total
		}

		c.JSON(http.StatusOK, models.ThreatListResponse{
			Threats: filtered[start:end],
			Total:   total, Page: page, Limit: limit,
		})
	}
}

// GetThreat handles GET /api/threats/:id.
// Returns the single threat whose ThreatID matches the :id path param.
func GetThreat(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		threats, err := buildThreatsList(cfg)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		for _, t := range threats {
			if t.ThreatID == id {
				c.JSON(http.StatusOK, t)
				return
			}
		}

		c.JSON(http.StatusNotFound, gin.H{"error": "threat not found"})
	}
}

// ThreatStatusBody is the request body for acknowledge/clear endpoints.
// The client sends the full threat identity to avoid a read-before-write.
type ThreatStatusBody struct {
	AgentID      string `json:"agent_id"       binding:"required"`
	DeploymentID string `json:"deployment_id"   binding:"required"`
	SessionID    string `json:"session_id"      binding:"required"`
	RuleID       string `json:"rule_id"         binding:"required"`
}

func upsertThreatStatus(cfg *config.Config, body ThreatStatusBody, status string) error {
	if status != "acknowledged" && status != "cleared" {
		return fmt.Errorf("invalid status %q", status)
	}
	sql := fmt.Sprintf(
		`INSERT INTO agentshield_threats (agent_id, deployment_id, session_id, rule_id, status, status_updated_at) VALUES ('%s', '%s', '%s', '%s', '%s', now64(3))`,
		escapeSQL(body.AgentID),
		escapeSQL(body.DeploymentID),
		escapeSQL(body.SessionID),
		escapeSQL(body.RuleID),
		escapeSQL(status),
	)
	return tp.ExecHTTP(&cfg.Timeplus, sql)
}

// AcknowledgeThreat handles POST /api/threats/:id/acknowledge.
// Upserts cf_status with status='acknowledged'. :id is the computed threat_id and must match the body.
func AcknowledgeThreat(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		routeID := c.Param("id")
		var body ThreatStatusBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id, deployment_id, session_id, rule_id are required"})
			return
		}
		if computeThreatID(body.AgentID, body.DeploymentID, body.SessionID, body.RuleID) != routeID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "body identity does not match threat id"})
			return
		}
		if err := upsertThreatStatus(cfg, body, "acknowledged"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upsert failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": "acknowledged"})
	}
}

// ClearThreat handles POST /api/threats/:id/clear.
// Upserts cf_status with status='cleared'. :id is the computed threat_id and must match the body.
func ClearThreat(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		routeID := c.Param("id")
		var body ThreatStatusBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id, deployment_id, session_id, rule_id are required"})
			return
		}
		if computeThreatID(body.AgentID, body.DeploymentID, body.SessionID, body.RuleID) != routeID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "body identity does not match threat id"})
			return
		}
		if err := upsertThreatStatus(cfg, body, "cleared"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upsert failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "status": "cleared"})
	}
}
