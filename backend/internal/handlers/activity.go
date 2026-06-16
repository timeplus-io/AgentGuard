package handlers

import (
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// ListActivity handles GET /api/activity.
//
// Query params:
//
//	session_id      — filter to a single session (optional)
//	status          — filter by hold status (optional)
//	include_allowed — "true" to also show hook events that were auto-allowed (default false)
//	limit           — max rows (default 100; capped at 500 when include_allowed=true)
func ListActivity(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)

		sessionID := c.Query("session_id")
		statusFilter := c.Query("status")
		includeAllowed := c.Query("include_allowed") == "true"

		limit := 100
		maxLimit := 100
		if includeAllowed {
			maxLimit = 500
		}
		if v := c.Query("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxLimit {
				limit = n
			}
		}

		holdEvents, err := queryHoldEvents(cfg, sessionID, statusFilter, limit)
		if err != nil {
			log.Printf("activity: queryHoldEvents: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}

		var allowedEvents []models.ActivityEvent
		if includeAllowed {
			holdKeys := buildHoldKeySet(holdEvents)
			allowedEvents, err = queryAllowedHookEvents(cfg, sessionID, holdKeys, limit)
			if err != nil {
				log.Printf("activity: queryAllowedHookEvents: %v", err)
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
				return
			}
		}

		events := mergeAndSortActivityEvents(holdEvents, allowedEvents, limit)
		c.JSON(http.StatusOK, models.ActivityResponse{Events: events, Total: len(events)})
	}
}

// queryHoldEvents fetches holds from agentguard_holds with optional session and status filters.
// rule_name is resolved via a LEFT JOIN on agentguard_rules at query time.
func queryHoldEvents(cfg *config.Config, sessionID, statusFilter string, limit int) ([]models.ActivityEvent, error) {
	whereClauses := "1=1"
	if sessionID != "" {
		whereClauses += fmt.Sprintf(" AND h.session_id = '%s'", escapeSQLStr(sessionID))
	}
	if statusFilter != "" {
		whereClauses += fmt.Sprintf(" AND h.status = '%s'", escapeSQLStr(statusFilter))
	}

	sql := fmt.Sprintf(`
SELECT h.hold_id, h.deployment_id, h.agent_id, h.session_id, h.run_id,
       h.tool_name, h.rule_id, COALESCE(r.rule_name, '') AS rule_name,
       h.severity, h.args_summary, h.message,
       h.created_at, h.status, h.decided_by, h.decided_at, h.decided_reason
FROM `+holdsLatestSubquery+` AS h
LEFT JOIN (
  -- Latest name per rule by monotonic revision (the snapshot invariant), not
  -- updated_at. No deleted filter: a since-deleted rule's name is still the
  -- correct label for a historical hold row.
  SELECT rule_id, arg_max(name, revision) AS rule_name
  FROM table(agentguard_rules)
  GROUP BY rule_id
) AS r ON h.rule_id = r.rule_id
WHERE %s
ORDER BY h.created_at DESC
LIMIT %d`, whereClauses, limit)

	rows, err := tp.Query(&cfg.Timeplus, sql)
	if err != nil {
		return nil, err
	}
	events := make([]models.ActivityEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, holdRowToActivityEvent(row))
	}
	return events, nil
}

// queryAllowedHookEvents fetches PreToolUse/before_tool_call hook events that are
// NOT already represented in the holdKeys set (i.e., events that were auto-allowed
// without triggering any rule or hold).
func queryAllowedHookEvents(cfg *config.Config, sessionID string, holdKeys map[holdKey]struct{}, limit int) ([]models.ActivityEvent, error) {
	whereClause := "hook_name IN ('before_tool_call', 'PreToolUse')"
	if sessionID != "" {
		whereClause += fmt.Sprintf(" AND session_id = '%s'", escapeSQLStr(sessionID))
	}

	sql := fmt.Sprintf(`
SELECT session_id, run_id, tool_name, event_time, event_data, agent_id
FROM table(agentguard_hook_events)
WHERE %s
ORDER BY event_time DESC
LIMIT %d`, whereClause, limit)

	rows, err := tp.Query(&cfg.Timeplus, sql)
	if err != nil {
		return nil, err
	}

	events := make([]models.ActivityEvent, 0)
	for _, row := range rows {
		sess := parseStr(row["session_id"])
		run := parseStr(row["run_id"])
		tool := parseStr(row["tool_name"])
		// Deduplicate by (session, run, tool): any hook event for a (session, run, toolName)
		// that has a hold is omitted here — the hold row represents it with richer data.
		// Tradeoff: if the same tool is called multiple times in one run and one triggered a
		// hold, later calls in that run will also be omitted when include_allowed=true.
		key := holdKey{sessionID: sess, runID: run, toolName: tool}
		if _, inHolds := holdKeys[key]; inHolds {
			continue // already represented as a hold event
		}
		evTime := parseTime(row["event_time"])
		events = append(events, models.ActivityEvent{
			ID:          syntheticActivityID(sess, evTime.UTC().Format("2006-01-02T15:04:05.000Z"), tool),
			EventTime:   evTime,
			ToolName:    tool,
			Status:      "allowed",
			AgentID:     parseStr(row["agent_id"]),
			SessionID:   sess,
			RunID:       run,
			HookPayload: parseStr(row["event_data"]),
			IsHoldEvent: false,
		})
	}
	return events, nil
}

// holdKey is the deduplication key between holds and hook events.
type holdKey struct {
	sessionID string
	runID     string
	toolName  string
}

// buildHoldKeySet returns a set of holdKey values from hold events for O(1) lookup.
func buildHoldKeySet(holdEvents []models.ActivityEvent) map[holdKey]struct{} {
	keys := make(map[holdKey]struct{}, len(holdEvents))
	for _, e := range holdEvents {
		keys[holdKey{sessionID: e.SessionID, runID: e.RunID, toolName: e.ToolName}] = struct{}{}
	}
	return keys
}

// mergeAndSortActivityEvents combines hold and allowed events, sorts newest-first, and caps at limit.
func mergeAndSortActivityEvents(holds, allowed []models.ActivityEvent, limit int) []models.ActivityEvent {
	all := make([]models.ActivityEvent, 0, len(holds)+len(allowed))
	all = append(all, holds...)
	all = append(all, allowed...)
	sort.Slice(all, func(i, j int) bool {
		return all[i].EventTime.After(all[j].EventTime)
	})
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

// holdRowToActivityEvent converts a Timeplus query row (from the holds+rules JOIN) into an ActivityEvent.
func holdRowToActivityEvent(row map[string]interface{}) models.ActivityEvent {
	ev := models.ActivityEvent{
		ID:            parseStr(row["hold_id"]),
		EventTime:     parseTime(row["created_at"]),
		ToolName:      parseStr(row["tool_name"]),
		Status:        parseStr(row["status"]),
		RuleID:        parseStr(row["rule_id"]),
		RuleName:      parseStr(row["rule_name"]),
		Severity:      parseStr(row["severity"]),
		AgentID:       parseStr(row["agent_id"]),
		SessionID:     parseStr(row["session_id"]),
		RunID:         parseStr(row["run_id"]),
		ArgsSummary:   parseStr(row["args_summary"]),
		Message:       parseStr(row["message"]),
		DecidedBy:     parseStr(row["decided_by"]),
		DecidedReason: parseStr(row["decided_reason"]),
		IsHoldEvent:   true,
	}
	if t := parseTime(row["decided_at"]); !t.IsZero() {
		ev.DecidedAt = &t
	}
	return ev
}

// syntheticActivityID returns a 16-char hex string derived from the inputs.
// Used as a stable client-side ID for hook-only (allowed) events that have no hold_id.
func syntheticActivityID(sessionID, eventTimeISO, toolName string) string {
	h := sha256.Sum256([]byte(sessionID + "|" + eventTimeISO + "|" + toolName))
	return fmt.Sprintf("%x", h[:8])
}
