package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
	"github.com/timeplus/agentshield/internal/models"
	"github.com/timeplus/agentshield/internal/timeplus"
)

// uuidRe validates strict UUID-format params before SQL interpolation.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// runIDRe validates run_id values — OpenClaw uses multiple formats:
//   - chatcmpl_<uuid>   (HTTP API / OpenAI-compat)
//   - announce:v1:agent:...:run:<uuid>  (subagent runs)
//   - slug-gen-<timestamp>  (internal, filtered at session level)
// Allows alphanum, hyphens, underscores, dots, colons. Excludes quotes/backslash.
var runIDRe = regexp.MustCompile(`^[a-zA-Z0-9._:/-]+$`)

// sessionType derives a human-readable type label from the session_key pattern.
func sessionType(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) < 3 {
		return "Unknown"
	}
	switch parts[2] {
	case "main":
		return "Web UI"
	case "openai-user":
		return "HTTP API"
	case "cron":
		return "Cron"
	case "subagent":
		return "Sub-agent"
	default:
		return "Unknown"
	}
}

// parseTime handles the multiple time formats Proton returns.
func parseTime(v any) time.Time {
	switch s := v.(type) {
	case string:
		if s == "" {
			return time.Time{}
		}
		// Proton returns timestamps without timezone: "2026-04-08 03:25:17.544"
		if t, err := time.ParseInLocation("2006-01-02 15:04:05.999999999", s, time.UTC); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", s); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	case float64:
		return time.Unix(int64(s), 0)
	case json.Number:
		if n, err := s.Float64(); err == nil {
			return time.Unix(int64(n), 0)
		}
	}
	return time.Time{}
}

// parseInt handles json.Number / float64 / int from Proton rows.
func parseInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	case int:
		return n
	}
	return 0
}

// parseBool handles Proton's count_if(...) result (returned as 0/1).
func parseBool(v any) bool {
	return parseInt(v) != 0
}

// parseStr safely casts any to string.
func parseStr(v any) string {
	s, _ := v.(string)
	return s
}

// parseRFC3339 parses an RFC3339 timestamp string into Proton-compatible format.
// Returns ("", nil) when s is empty. Returns an error when s is non-empty but invalid.
func parseRFC3339(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "", err
	}
	return t.UTC().Format("2006-01-02 15:04:05.999999999"), nil
}

// buildTimeWhere returns SQL AND clauses for since/until Proton timestamps.
// Each condition starts with a newline so it can be appended to an existing WHERE clause.
func buildTimeWhere(since, until string) string {
	out := ""
	if since != "" {
		out += fmt.Sprintf("\n      AND event_time >= '%s'", since)
	}
	if until != "" {
		out += fmt.Sprintf("\n      AND event_time <= '%s'", until)
	}
	return out
}

// splitAgentID splits the compound "deploymentId__agentId" path param.
func splitAgentID(compoundID string) (deploymentID, agentID string, ok bool) {
	parts := strings.SplitN(compoundID, "__", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	if !idPartRe.MatchString(parts[0]) || !idPartRe.MatchString(parts[1]) {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// ListSessions handles GET /api/agents/:id/sessions
// Query params: since (RFC3339), until (RFC3339)
func ListSessions(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format, expected deploymentId__agentId"})
			return
		}

		sinceStr, err := parseRFC3339(c.Query("since"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid since: use RFC3339"})
			return
		}
		untilStr, err := parseRFC3339(c.Query("until"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid until: use RFC3339"})
			return
		}
		timeWhere := buildTimeWhere(sinceStr, untilStr)

		sql := fmt.Sprintf(`
SELECT
    session_id,
    any_if(session_key, session_key != '')              AS session_key,
    count_distinct(if(run_id != '', run_id, null))      AS run_count,
    count()                               AS event_count,
    min(event_time)                       AS first_seen,
    max(event_time)                       AS last_seen
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    != ''%s
GROUP BY session_id
HAVING session_key NOT LIKE 'temp:%%'
   AND session_id  NOT LIKE 'slug-generator-%%'
   AND count_if(run_id != '') > 0
ORDER BY last_seen DESC`,
			deploymentID, agentID, timeWhere)

		rows, err := timeplus.Query(&cfg.Timeplus, sql)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		sessions := make([]models.Session, 0, len(rows))
		for _, row := range rows {
			key := parseStr(row["session_key"])
			sessions = append(sessions, models.Session{
				SessionID:   parseStr(row["session_id"]),
				SessionKey:  key,
				SessionType: sessionType(key),
				RunCount:    parseInt(row["run_count"]),
				EventCount:  parseInt(row["event_count"]),
				FirstSeen:   parseTime(row["first_seen"]),
				LastSeen:    parseTime(row["last_seen"]),
			})
		}

		c.JSON(http.StatusOK, sessions)
	}
}

// GetSession handles GET /api/agents/:id/sessions/:session_id
func GetSession(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format"})
			return
		}

		sessionID := c.Param("session_id")
		if !idPartRe.MatchString(sessionID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session_id format"})
			return
		}

		// Single query: fetch session_key alongside per-run aggregates.
		// any(session_key) returns the same value for all rows in the group (session_key
		// is constant within a session), so this is safe and eliminates a round-trip.
		runsSQL := fmt.Sprintf(`
SELECT
    any(session_key)                        AS session_key,
    run_id,
    min(event_time)                         AS start_time,
    max(event_time)                         AS end_time,
    count()                                 AS event_count,
    any(model)                              AS model,
    count_if(hook_name = 'before_tool_call') AS tool_count,
    count_if(hook_decision = 'block')        AS block_count
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'
  AND run_id        != ''
GROUP BY run_id
ORDER BY start_time ASC`,
			deploymentID, agentID, sessionID)

		runsRows, err := timeplus.Query(&cfg.Timeplus, runsSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		if len(runsRows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}

		// Second query: session-level counters (no run_id filter, catches session_end).
		statsSQL := fmt.Sprintf(`
SELECT
    count_if(hook_name = 'session_end')      AS has_ended,
    count_if(hook_name = 'before_tool_call') AS total_tool_calls
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'`,
			deploymentID, agentID, sessionID)

		statsRows, err := timeplus.Query(&cfg.Timeplus, statsSQL)
		if err != nil {
			// Non-fatal: degrade gracefully if stats query fails.
			statsRows = nil
		}

		status := "active"
		totalToolCalls := 0
		if len(statsRows) > 0 {
			if parseBool(statsRows[0]["has_ended"]) {
				status = "ended"
			}
			totalToolCalls = parseInt(statsRows[0]["total_tool_calls"])
		}

		sessionKey := parseStr(runsRows[0]["session_key"])

		runs := make([]models.Run, 0, len(runsRows))
		for _, row := range runsRows {
			runs = append(runs, models.Run{
				RunID:      parseStr(row["run_id"]),
				StartTime:  parseTime(row["start_time"]),
				EndTime:    parseTime(row["end_time"]),
				EventCount: parseInt(row["event_count"]),
				Model:      parseStr(row["model"]),
				ToolCount:  parseInt(row["tool_count"]),
				HasBlock:   parseBool(row["block_count"]),
			})
		}

		c.JSON(http.StatusOK, models.SessionDetail{
			SessionID:      sessionID,
			SessionKey:     sessionKey,
			SessionType:    sessionType(sessionKey),
			Status:         status,
			TotalToolCalls: totalToolCalls,
			Runs:           runs,
		})
	}
}

// GetRunTrace handles GET /api/agents/:id/sessions/:session_id/runs/:run_id/trace
// It synthesizes a waterfall span tree from agentshield_hook_events (run bounds +
// tool spans) and otel_traces (LLM call spans). OTel query failure is non-fatal.
func GetRunTrace(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		deploymentID, agentID, ok := splitAgentID(c.Param("id"))
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent id format"})
			return
		}

		sessionID := c.Param("session_id")
		if !idPartRe.MatchString(sessionID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session_id format"})
			return
		}

		runID := c.Param("run_id")
		if !runIDRe.MatchString(runID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run_id format"})
			return
		}

		// ── Query 1: run time bounds ──────────────────────────────────────────
		boundsSQL := fmt.Sprintf(`
SELECT
    min(event_time) AS start_time,
    max(event_time) AS end_time
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'
  AND run_id        = '%s'`,
			deploymentID, agentID, sessionID, runID)

		boundsRows, err := timeplus.Query(&cfg.Timeplus, boundsSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}
		if len(boundsRows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}

		startTime := parseTime(boundsRows[0]["start_time"])
		endTime := parseTime(boundsRows[0]["end_time"])
		if startTime.IsZero() {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}

		startMs := startTime.UnixMilli()
		endMs := endTime.UnixMilli()
		durationMs := endMs - startMs
		if durationMs <= 0 {
			durationMs = 1
		}

		spans := []models.TraceSpan{
			{
				SpanID:     runID,
				ParentID:   "",
				Kind:       "run",
				Name:       "run",
				StartMs:    startMs,
				DurationMs: durationMs,
				Status:     "ok",
				Attributes: map[string]string{},
			},
		}

		// ── Query 2: OTel LLM spans (non-fatal) ──────────────────────────────
		startNs := startTime.UnixNano()
		endNs := endTime.UnixNano()

		otelSQL := fmt.Sprintf(`
SELECT
    span_id,
    start_time_ns,
    duration,
    span_attributes
FROM table(otel_traces)
WHERE span_attributes['openclaw.sessionId'] = '%s'
  AND span_name = 'openclaw.model.usage'
  AND start_time_ns >= %d
  AND start_time_ns <= %d
ORDER BY start_time_ns ASC`,
			sessionID, startNs, endNs)

		otelRows, otelErr := timeplus.Query(&cfg.Timeplus, otelSQL)
		if otelErr != nil {
			log.Printf("GetRunTrace: otel_traces query failed (non-fatal): %v", otelErr)
		} else {
			for i, row := range otelRows {
				spanID := parseStr(row["span_id"])
				if spanID == "" {
					spanID = fmt.Sprintf("llm-%d", i)
				}
				spanStartMs := parseInt64(row["start_time_ns"]) / 1_000_000
				spanDurationMs := parseInt64(row["duration"]) / 1_000_000

				attrs := map[string]string{}
				// span_attributes is a map column; Proton returns it as map[string]interface{}
				if rawMap, ok := row["span_attributes"].(map[string]interface{}); ok {
					for k, v := range rawMap {
						if s, ok2 := v.(string); ok2 {
							attrs[k] = s
						}
					}
				}

				spans = append(spans, models.TraceSpan{
					SpanID:     spanID,
					ParentID:   runID,
					Kind:       "llm",
					Name:       "LLM call",
					StartMs:    spanStartMs,
					DurationMs: spanDurationMs,
					Status:     "ok",
					Attributes: attrs,
				})
			}
		}

		// ── Query 3: tool call spans ──────────────────────────────────────────
		toolSQL := fmt.Sprintf(`
SELECT hook_name, event_time, tool_name, hook_decision, block_reason, event_data
FROM table(agentshield_hook_events)
WHERE deployment_id = '%s'
  AND agent_id      = '%s'
  AND session_id    = '%s'
  AND run_id        = '%s'
  AND hook_name     IN ('before_tool_call', 'after_tool_call')
ORDER BY event_time ASC`,
			deploymentID, agentID, sessionID, runID)

		toolRows, err := timeplus.Query(&cfg.Timeplus, toolSQL)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data store unavailable"})
			return
		}

		// Collect before/after events; key by toolCallId when present.
		type toolEvt struct {
			hookName     string
			eventTime    time.Time
			toolName     string
			hookDecision string
			blockReason  string
			eventData    string
			toolCallID   string
		}

		var befores []toolEvt
		afterByID := map[string]toolEvt{}
		var aftersNoID []toolEvt

		for _, row := range toolRows {
			evt := toolEvt{
				hookName:     parseStr(row["hook_name"]),
				eventTime:    parseTime(row["event_time"]),
				toolName:     parseStr(row["tool_name"]),
				hookDecision: parseStr(row["hook_decision"]),
				blockReason:  parseStr(row["block_reason"]),
				eventData:    parseStr(row["event_data"]),
			}
			var data map[string]interface{}
			if json.Unmarshal([]byte(evt.eventData), &data) == nil {
				if tcid, ok := data["toolCallId"].(string); ok {
					evt.toolCallID = tcid
				}
			}
			if evt.hookName == "before_tool_call" {
				befores = append(befores, evt)
			} else {
				if evt.toolCallID != "" {
					afterByID[evt.toolCallID] = evt
				} else {
					aftersNoID = append(aftersNoID, evt)
				}
			}
		}

		noIDIdx := 0
		for i, before := range befores {
			var after *toolEvt
			if before.toolCallID != "" {
				if a, ok := afterByID[before.toolCallID]; ok {
					after = &a
				}
			} else if noIDIdx < len(aftersNoID) {
				a := aftersNoID[noIDIdx]
				after = &a
				noIDIdx++
			}

			spanStartMs := before.eventTime.UnixMilli()
			var spanDurationMs int64
			status := "ok"
			attrs := map[string]string{"tool_name": before.toolName}

			if before.hookDecision == "block" {
				status = "blocked"
				if before.blockReason != "" {
					attrs["block_reason"] = before.blockReason
				}
			} else if after != nil {
				spanDurationMs = after.eventTime.UnixMilli() - spanStartMs
				var data map[string]interface{}
				if json.Unmarshal([]byte(after.eventData), &data) == nil {
					if d, ok := data["durationMs"].(float64); ok {
						spanDurationMs = int64(d)
					}
					if errMsg, ok := data["error"].(string); ok && errMsg != "" {
						status = "error"
						attrs["error"] = errMsg
					}
				}
			}

			spans = append(spans, models.TraceSpan{
				SpanID:     fmt.Sprintf("tool-%d", i),
				ParentID:   runID,
				Kind:       "tool",
				Name:       before.toolName,
				StartMs:    spanStartMs,
				DurationMs: spanDurationMs,
				Status:     status,
				Attributes: attrs,
			})
		}

		// Sort all spans by StartMs ascending (root first, then children).
		sort.Slice(spans, func(i, j int) bool {
			if spans[i].ParentID == "" {
				return true
			}
			if spans[j].ParentID == "" {
				return false
			}
			return spans[i].StartMs < spans[j].StartMs
		})

		c.JSON(http.StatusOK, models.TraceResponse{Spans: spans})
	}
}
