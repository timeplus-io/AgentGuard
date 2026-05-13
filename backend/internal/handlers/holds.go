package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/holdregistry"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

const argsSummaryMaxBytes = 2048

// holdsLatestSubquery folds agentguard_holds down to one row per hold_id with
// the latest field values. We compute this inline at query time (matching the
// agentguard_rules pattern) instead of via a stateful MV — Proton's streaming
// MVs with GROUP BY emit one snapshot per source row, so a `WHERE status='X'`
// against an MV can match stale snapshots from before a status change.
const holdsLatestSubquery = `(
  SELECT hold_id,
         arg_max(deployment_id,  updated_at) AS deployment_id,
         arg_max(agent_id,       updated_at) AS agent_id,
         arg_max(session_id,     updated_at) AS session_id,
         arg_max(run_id,         updated_at) AS run_id,
         arg_max(tool_name,      updated_at) AS tool_name,
         arg_max(rule_id,        updated_at) AS rule_id,
         arg_max(severity,       updated_at) AS severity,
         arg_max(args_summary,   updated_at) AS args_summary,
         arg_max(message,        updated_at) AS message,
         arg_max(created_at,     updated_at) AS created_at,
         arg_max(status,         updated_at) AS status,
         arg_max(decided_by,     updated_at) AS decided_by,
         arg_max(decided_at,     updated_at) AS decided_at,
         arg_max(decided_reason, updated_at) AS decided_reason
  FROM table(agentguard_holds)
  GROUP BY hold_id
)`

// failPolicyToDecision maps the config FailPolicy ("deny"|"allow") to the
// HoldResponse Decision contract ("block"|"allow"). Returns "block" by default
// for any unknown value (fail-closed).
func failPolicyToDecision(policy string) string {
	if policy == "allow" {
		return "allow"
	}
	return "block"
}

// pluginCfg validates that service credentials are configured for handlers
// called server-to-server (no session cookie). Plugin/Timeplus → AgentGuard
// endpoints must have TIMEPLUS_USER + TIMEPLUS_PASSWORD set as env vars on
// the AgentGuard process — there is no fallback.
//
// Returns (cfg, "") on success or (nil, "<actionable msg>") if creds missing.
// Callers should respond with 503 + the message and a log line.
func pluginCfg(cfg *config.Config) (*config.Config, string) {
	if cfg.Timeplus.User == "" {
		return nil, "service credentials not configured: set TIMEPLUS_USER and TIMEPLUS_PASSWORD env vars on the AgentGuard server"
	}
	return cfg, ""
}

// CreateHold handles POST /api/holds.
// Synchronous endpoint — does not return until backend has decided
// (allow / block / hold + human decision / hold timeout).
func CreateHold(cfg *config.Config, registry *holdregistry.Registry, broadcaster *alertBroadcaster) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg, errMsg := pluginCfg(cfg)
		if errMsg != "" {
			log.Printf("holds: CreateHold: %s", errMsg)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": errMsg})
			return
		}
		var req models.HoldRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		// Master kill-switch: skip all decision logic, just allow.
		if cfg.Holds.Disabled {
			c.JSON(http.StatusOK, models.HoldResponse{Decision: "allow"})
			return
		}

		// Step 1: Ingest the hook event into agentguard_hook_events so MVs see it.
		if err := ingestHookEvent(cfg, &req); err != nil {
			log.Printf("holds: ingest event failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("ingest failed: %v", err)})
			return
		}

		// Step 2: pre-existing open threats for this session.
		preExisting, threatPolicies, err := queryPreExistingOpenThreats(cfg, &req)
		if err != nil {
			log.Printf("holds: pre-existing threats query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("pre-existing threats query: %v", err)})
			return
		}

		// Step 3: wait up to MVWaitMs for new MV detections matching this event.
		newMatches, err := waitForNewMVDetections(cfg, &req)
		if err != nil {
			log.Printf("holds: MV-wait query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("MV-wait query: %v", err)})
			return
		}

		// Step 4: pure decision logic.
		out := Decide(DecisionInput{
			KillSwitch:         false,
			Matches:            newMatches,
			PreExistingThreats: preExisting,
			ThreatRulePolicies: threatPolicies,
		})

		// Diagnostic: log non-allow decisions so operators can trace WHICH rule
		// is causing a hold/block. Useful when the operator believes a rule is
		// disabled but holds keep appearing — the rule_id printed here is the
		// authoritative source.
		if out.Decision != "allow" {
			log.Printf("holds: decision=%s tool=%s session=%s rule=%s policy_matches=%d pre_existing=%d reason=%q",
				out.Decision, req.ToolName, req.SessionID, out.RuleID,
				len(newMatches), len(preExisting), out.Reason)
		}

		switch out.Decision {
		case "allow":
			c.JSON(http.StatusOK, models.HoldResponse{Decision: "allow"})
			return
		case "block":
			c.JSON(http.StatusOK, models.HoldResponse{
				Decision: "block",
				Reason:   out.Reason,
			})
			return
		case "hold":
			// Step 5: open hold record, register channel, long-poll.
			holdID := uuid.NewString()
			argsSummary := truncateJSON(req.ToolInput, argsSummaryMaxBytes)
			now := time.Now().UTC()
			if err := writeHoldRow(cfg, models.Hold{
				HoldID:       holdID,
				DeploymentID: req.DeploymentID,
				AgentID:      req.AgentID,
				SessionID:    req.SessionID,
				RunID:        req.RunID,
				ToolName:     req.ToolName,
				RuleID:       out.RuleID,
				Severity:     out.Severity,
				ArgsSummary:  argsSummary,
				Message:      out.Reason,
				CreatedAt:    now,
				Status:       "pending",
			}, now); err != nil {
				log.Printf("holds: write pending hold row: %v", err)
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("write hold row: %v", err)})
				return
			}

			ch := registry.Register(holdID)
			defer registry.Cancel(holdID)

			// Notify the UI via the existing SSE notification stream.
			broadcaster.Publish(models.AlertWebhookPayload{
				RuleID:     out.RuleID,
				RuleName:   "hold_pending",
				Severity:   out.Severity,
				AgentID:    req.AgentID,
				Message:    fmt.Sprintf("hold:%s|tool:%s|reason:%s", holdID, req.ToolName, out.Reason),
				DetectedAt: now.Format(time.RFC3339),
			})

			timeout := time.Duration(cfg.Holds.TimeoutSeconds) * time.Second
			select {
			case d := <-ch:
				// Human decided.
				decision := "block"
				if d.Allow {
					decision = "allow"
				}
				c.JSON(http.StatusOK, models.HoldResponse{
					Decision: decision,
					Reason:   d.Reason,
					HoldID:   holdID,
				})
			case <-time.After(timeout):
				// Timeout — apply fail-policy and write timeout row.
				if err := writeHoldStatus(cfg, holdID, "timeout", "system", "hold timeout"); err != nil {
					log.Printf("holds: writeHoldStatus timeout %s: %v", holdID, err)
				}
				c.JSON(http.StatusOK, models.HoldResponse{
					Decision: failPolicyToDecision(cfg.Holds.FailPolicy),
					Reason:   "AgentGuard hold timed out",
					HoldID:   holdID,
				})
			}
			return
		}
	}
}

func truncateJSON(v interface{}, maxBytes int) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	if len(b) > maxBytes {
		return string(b[:maxBytes]) + "...[truncated]"
	}
	return string(b)
}

// ingestHookEvent inserts the just-emitted PreToolUse event into
// agentguard_hook_events so MVs can detect it. The plugin does NOT also
// call its async send() for PreToolUse, so this is the only ingest path.
func ingestHookEvent(cfg *config.Config, req *models.HoldRequest) error {
	payloadJSON, err := json.Marshal(req.HookPayload)
	if err != nil {
		return fmt.Errorf("marshal hook payload: %w", err)
	}
	agentType := req.AgentType
	if agentType == "" {
		agentType = "claudecode" // backward compat
	}
	sql := fmt.Sprintf(`
INSERT INTO agentguard_hook_events
  (hook_name, event_time, session_id, run_id, agent_id, deployment_id,
   deployment_name, session_key, tool_name, provider, model,
   hook_decision, block_reason, event_data, agent_type)
VALUES
  ('before_tool_call', '%s', '%s', '%s', '%s', '%s',
   '%s', '', '%s', '', '',
   'observe', '', '%s', '%s')`,
		req.EventTime.UTC().Format("2006-01-02 15:04:05.999"),
		escapeSQLStr(req.SessionID),
		escapeSQLStr(req.RunID),
		escapeSQLStr(req.AgentID),
		escapeSQLStr(req.DeploymentID),
		escapeSQLStr(req.DeploymentName),
		escapeSQLStr(req.ToolName),
		escapeSQLStr(string(payloadJSON)),
		escapeSQLStr(agentType),
	)
	return tp.ExecHTTP(&cfg.Timeplus, sql)
}

// buildPreExistingOpenThreatsSQL composes the SQL that decides whether the
// current session has any prior threats that should block new tool calls.
//
// Source of truth is agentguard_threats (the mutable dedup state), NOT
// agentguard_security_events (the append-only audit log). The schema splits
// detection cadence and operator action into two column families that evolve
// independently:
//
//	cf_events(rule_name, severity, last_seen)        ← mv_threats writes this
//	cf_status(status, status_updated_at)             ← acknowledge/clear writes this
//
// A row is "effectively open" — i.e. should block — when any of:
//
//	status = ''                                       (never operator-touched)
//	status = 'open'                                   (threats.go auto-promoted on reopen)
//	status IN ('acknowledged','cleared')
//	  AND last_seen > status_updated_at               (re-fired after operator action)
//
// The rules join still applies: disabling a rule retroactively removes its
// block, which is the operator's escape hatch.
func buildPreExistingOpenThreatsSQL(deploymentID, agentID, sessionID string) string {
	// Note: the synthetic 'open' column is aliased as reported_status (not status)
	// to avoid colliding with the t.status reference in the WHERE clause — Proton's
	// planner can't disambiguate a base-column reference from a same-named output
	// alias and drops the t.status column from the join projection.
	return fmt.Sprintf(`
SELECT t.rule_id, t.rule_name, t.severity, 'open' AS reported_status, r.block_policy
FROM table(agentguard_threats) AS t
INNER JOIN (
  SELECT rule_id,
         arg_max(block_policy, updated_at) AS block_policy,
         arg_max(enabled,      updated_at) AS enabled,
         arg_max(deleted,      updated_at) AS deleted
  FROM table(agentguard_rules)
  GROUP BY rule_id
) AS r ON t.rule_id = r.rule_id
WHERE t.deployment_id = '%s'
  AND t.agent_id      = '%s'
  AND t.session_id    = '%s'
  AND r.enabled
  AND NOT r.deleted
  AND (
       t.status = ''
    OR t.status = 'open'
    OR (t.status IN ('acknowledged', 'cleared')
        AND t.last_seen > t.status_updated_at)
  )`,
		escapeSQLStr(deploymentID), escapeSQLStr(agentID), escapeSQLStr(sessionID))
}

// queryPreExistingOpenThreats returns open threats for this session and
// a rule_id → block_policy map for those threats' rules.
//
// Rules where the latest state is enabled=false or deleted=true are excluded:
// the user has turned the rule off, and stale threats from before the disable
// must not block new tool calls.
func queryPreExistingOpenThreats(cfg *config.Config, req *models.HoldRequest) ([]models.Threat, map[string]string, error) {
	sql := buildPreExistingOpenThreatsSQL(req.DeploymentID, req.AgentID, req.SessionID)

	rows, err := tp.Query(&cfg.Timeplus, sql)
	if err != nil {
		return nil, nil, err
	}
	threats := make([]models.Threat, 0, len(rows))
	policies := make(map[string]string, len(rows))
	for _, row := range rows {
		ruleID := parseStr(row["rule_id"])
		threats = append(threats, models.Threat{
			RuleID:   ruleID,
			RuleName: parseStr(row["rule_name"]),
			Severity: parseStr(row["severity"]),
			Status:   parseStr(row["reported_status"]),
		})
		policies[ruleID] = parseStr(row["block_policy"])
	}
	return threats, policies, nil
}

// waitForNewMVDetections polls agentguard_security_events every 50ms for up to
// MVWaitMs, looking for rows tagged to this session with detected_at >= event_time.
// Returns the matching rules (joined with their current block_policy).
func waitForNewMVDetections(cfg *config.Config, req *models.HoldRequest) ([]RuleMatch, error) {
	const pollIntervalMs = 50
	totalMs := cfg.Holds.MVWaitMs
	if totalMs < pollIntervalMs {
		totalMs = pollIntervalMs
	}
	deadline := time.Now().Add(time.Duration(totalMs) * time.Millisecond)

	sql := fmt.Sprintf(`
SELECT s.rule_id, s.rule_name, s.severity, s.message, r.block_policy
FROM (
  SELECT rule_id, rule_name, severity, message
  FROM table(agentguard_security_events)
  WHERE deployment_id = '%s' AND agent_id = '%s' AND session_id = '%s'
    AND detected_at >= '%s'
) AS s
INNER JOIN (
  SELECT rule_id,
         arg_max(block_policy, updated_at) AS block_policy,
         arg_max(enabled,      updated_at) AS enabled,
         arg_max(deleted,      updated_at) AS deleted
  FROM table(agentguard_rules)
  GROUP BY rule_id
) AS r ON s.rule_id = r.rule_id
WHERE r.enabled AND NOT r.deleted
  AND r.block_policy IN ('hold', 'auto_block')`,
		escapeSQLStr(req.DeploymentID), escapeSQLStr(req.AgentID), escapeSQLStr(req.SessionID),
		req.EventTime.UTC().Format("2006-01-02 15:04:05.999"))

	for time.Now().Before(deadline) {
		rows, err := tp.Query(&cfg.Timeplus, sql)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			matches := make([]RuleMatch, 0, len(rows))
			for _, row := range rows {
				matches = append(matches, RuleMatch{
					RuleID:      parseStr(row["rule_id"]),
					BlockPolicy: parseStr(row["block_policy"]),
					Severity:    parseStr(row["severity"]),
					Message:     parseStr(row["message"]),
				})
			}
			return matches, nil
		}
		time.Sleep(pollIntervalMs * time.Millisecond)
	}
	return nil, nil
}

// writeHoldRow inserts a new row into agentguard_holds (initial 'pending' state).
func writeHoldRow(cfg *config.Config, h models.Hold, now time.Time) error {
	sql := fmt.Sprintf(`
INSERT INTO agentguard_holds
  (hold_id, deployment_id, agent_id, session_id, run_id, tool_name, rule_id,
   severity, args_summary, message, created_at, status,
   decided_by, decided_at, decided_reason, updated_at)
VALUES
  ('%s','%s','%s','%s','%s','%s','%s',
   '%s','%s','%s','%s','%s',
   '','1970-01-01 00:00:00.000','','%s')`,
		escapeSQLStr(h.HoldID), escapeSQLStr(h.DeploymentID), escapeSQLStr(h.AgentID),
		escapeSQLStr(h.SessionID), escapeSQLStr(h.RunID), escapeSQLStr(h.ToolName), escapeSQLStr(h.RuleID),
		escapeSQLStr(h.Severity), escapeSQLStr(h.ArgsSummary), escapeSQLStr(h.Message),
		now.Format("2006-01-02 15:04:05.999"), h.Status,
		now.Format("2006-01-02 15:04:05.999"))
	return tp.ExecHTTP(&cfg.Timeplus, sql)
}

// writeHoldStatus appends a status-update row for an existing hold (e.g.,
// approved / denied / timeout / abandoned).
func writeHoldStatus(cfg *config.Config, holdID, status, decidedBy, decidedReason string) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05.999")
	sql := fmt.Sprintf(`
INSERT INTO agentguard_holds
  (hold_id, deployment_id, agent_id, session_id, run_id, tool_name, rule_id,
   severity, args_summary, message, created_at, status,
   decided_by, decided_at, decided_reason, updated_at)
SELECT
  hold_id, deployment_id, agent_id, session_id, run_id, tool_name, rule_id,
  severity, args_summary, message, created_at,
  '%s' AS status,
  '%s' AS decided_by,
  '%s' AS decided_at,
  '%s' AS decided_reason,
  '%s' AS updated_at
FROM ` + holdsLatestSubquery + `
WHERE hold_id = '%s'
LIMIT 1`,
		status, escapeSQLStr(decidedBy), now, escapeSQLStr(decidedReason), now,
		escapeSQLStr(holdID))
	return tp.ExecHTTP(&cfg.Timeplus, sql)
}

// DecideHold handles POST /api/holds/:hold_id/decide.
// Body: HoldDecideRequest. Idempotent: if already decided, returns existing decision.
func DecideHold(cfg *config.Config, registry *holdregistry.Registry, broadcaster *alertBroadcaster) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		holdID := c.Param("hold_id")
		var body models.HoldDecideRequest
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}
		if body.Decision != "approved" && body.Decision != "denied" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "decision must be approved|denied"})
			return
		}

		existing, err := getHoldByID(cfg, holdID)
		if err != nil {
			log.Printf("holds: DecideHold getHoldByID: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("query hold: %v", err)})
			return
		}
		if existing == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "hold not found"})
			return
		}
		if existing.Status != "pending" {
			// Already terminal — idempotent: return existing decision without writing a new row.
			c.JSON(http.StatusOK, gin.H{"ok": true, "already_decided_as": existing.Status})
			return
		}

		newStatus := body.Decision // "approved" | "denied"
		if err := writeHoldStatus(cfg, holdID, newStatus, body.DecidedBy, body.DecidedReason); err != nil {
			log.Printf("holds: DecideHold writeHoldStatus: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": fmt.Sprintf("write decision: %v", err)})
			return
		}

		// Wake the long-poll goroutine.
		registry.Signal(holdID, holdregistry.Decision{
			Allow:  newStatus == "approved",
			Reason: body.DecidedReason,
		})

		// Push SSE so toasts/list refresh.
		broadcaster.Publish(models.AlertWebhookPayload{
			RuleID:     existing.RuleID,
			RuleName:   "hold_decided",
			Severity:   existing.Severity,
			AgentID:    existing.AgentID,
			Message:    fmt.Sprintf("hold:%s|decision:%s", holdID, newStatus),
			DetectedAt: time.Now().UTC().Format(time.RFC3339),
		})

		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// ListHolds handles GET /api/holds. Query params: ?status=pending (default), ?limit=N (default 100, max 500).
func ListHolds(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		status := c.DefaultQuery("status", "pending")
		limit := 100
		if v := c.Query("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		sql := fmt.Sprintf(`
SELECT hold_id, deployment_id, agent_id, session_id, run_id, tool_name,
       rule_id, severity, args_summary, message, created_at, status,
       decided_by, decided_at, decided_reason
FROM ` + holdsLatestSubquery + `
WHERE status = '%s'
ORDER BY created_at DESC
LIMIT %d`, escapeSQLStr(status), limit)
		rows, err := tp.Query(&cfg.Timeplus, sql)
		if err != nil {
			log.Printf("holds: ListHolds query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		holds := make([]models.Hold, 0, len(rows))
		for _, row := range rows {
			holds = append(holds, rowToHold(row))
		}
		c.JSON(http.StatusOK, gin.H{"holds": holds, "total": len(holds)})
	}
}

// GetHold handles GET /api/holds/:hold_id. Returns current state + full timeline.
func GetHold(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		holdID := c.Param("hold_id")

		current, err := getHoldByID(cfg, holdID)
		if err != nil {
			log.Printf("holds: GetHold getHoldByID: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		if current == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "hold not found"})
			return
		}

		timelineSQL := fmt.Sprintf(`
SELECT status, decided_by, decided_at, decided_reason, updated_at
FROM table(agentguard_holds)
WHERE hold_id = '%s'
ORDER BY updated_at ASC`, escapeSQLStr(holdID))
		rows, err := tp.Query(&cfg.Timeplus, timelineSQL)
		if err != nil {
			log.Printf("holds: GetHold timeline query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		timeline := make([]gin.H, 0, len(rows))
		for _, row := range rows {
			timeline = append(timeline, gin.H{
				"status":         parseStr(row["status"]),
				"decided_by":     parseStr(row["decided_by"]),
				"decided_at":     parseTime(row["decided_at"]),
				"decided_reason": parseStr(row["decided_reason"]),
				"updated_at":     parseTime(row["updated_at"]),
			})
		}

		c.JSON(http.StatusOK, gin.H{"hold": current, "timeline": timeline})
	}
}

// AbandonSession handles POST /api/holds/_abandon-session/:session_id.
// Plugin's SessionEnd hook fires this fire-and-forget to clean up holds for
// the ending session.
func AbandonSession(cfg *config.Config, registry *holdregistry.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg, errMsg := pluginCfg(cfg)
		if errMsg != "" {
			log.Printf("holds: AbandonSession: %s", errMsg)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": errMsg})
			return
		}
		sessionID := c.Param("session_id")

		sql := fmt.Sprintf(`
SELECT hold_id FROM ` + holdsLatestSubquery + `
WHERE session_id = '%s' AND status = 'pending'`, escapeSQLStr(sessionID))
		rows, err := tp.Query(&cfg.Timeplus, sql)
		if err != nil {
			log.Printf("holds: AbandonSession query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		holdIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			if id := parseStr(row["hold_id"]); id != "" {
				holdIDs = append(holdIDs, id)
			}
		}

		failAllow := cfg.Holds.FailPolicy == "allow"
		for _, id := range holdIDs {
			if err := writeHoldStatus(cfg, id, "abandoned", "system", "session ended"); err != nil {
				log.Printf("holds: writeHoldStatus abandon %s: %v", id, err)
			}
			registry.Signal(id, holdregistry.Decision{
				Allow:  failAllow,
				Reason: "session ended",
			})
		}
		c.JSON(http.StatusOK, gin.H{"abandoned": len(holdIDs)})
	}
}

// DrainHolds handles POST /api/holds/_drain. Operator-only: marks all pending holds as 'abandoned'.
func DrainHolds(cfg *config.Config, registry *holdregistry.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		sql := `SELECT hold_id FROM ` + holdsLatestSubquery + ` WHERE status = 'pending'`
		rows, err := tp.Query(&cfg.Timeplus, sql)
		if err != nil {
			log.Printf("holds: DrainHolds query: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		holdIDs := make([]string, 0, len(rows))
		for _, row := range rows {
			if id := parseStr(row["hold_id"]); id != "" {
				holdIDs = append(holdIDs, id)
			}
		}
		failAllow := cfg.Holds.FailPolicy == "allow"
		for _, id := range holdIDs {
			if err := writeHoldStatus(cfg, id, "abandoned", "operator", "drained"); err != nil {
				log.Printf("holds: writeHoldStatus drain %s: %v", id, err)
			}
			registry.Signal(id, holdregistry.Decision{Allow: failAllow, Reason: "drained"})
		}
		c.JSON(http.StatusOK, gin.H{"drained": len(holdIDs)})
	}
}

// AbandonHoldsForRule marks every pending hold for the given rule as 'abandoned'
// and signals the in-memory channel so any waiting plugin receives a fast
// fail-policy decision. Also publishes hold_decided SSE events so the UI
// removes the toasts immediately.
//
// Used when a rule is disabled or its block_policy drops to log_only — the
// operator's intent is "stop blocking on this rule", which must apply to
// in-flight holds, not just future tool calls. Errors are logged but not
// returned: the caller's main operation (toggle / set policy) has already
// succeeded; cleanup is best-effort.
func AbandonHoldsForRule(cfg *config.Config, registry *holdregistry.Registry, broadcaster *alertBroadcaster, ruleID, reason string) int {
	sql := fmt.Sprintf(`
SELECT hold_id, deployment_id, agent_id, session_id, severity
FROM `+holdsLatestSubquery+`
WHERE rule_id = '%s' AND status = 'pending'`, escapeSQLStr(ruleID))
	rows, err := tp.Query(&cfg.Timeplus, sql)
	if err != nil {
		log.Printf("holds: AbandonHoldsForRule query rule=%s: %v", ruleID, err)
		return 0
	}
	failAllow := cfg.Holds.FailPolicy == "allow"
	count := 0
	for _, row := range rows {
		holdID := parseStr(row["hold_id"])
		if holdID == "" {
			continue
		}
		if err := writeHoldStatus(cfg, holdID, "abandoned", "system", reason); err != nil {
			log.Printf("holds: AbandonHoldsForRule writeHoldStatus(%s): %v", holdID, err)
			continue
		}
		registry.Signal(holdID, holdregistry.Decision{Allow: failAllow, Reason: reason})
		// Push hold_decided so NotificationsProvider drops the toast right away.
		if broadcaster != nil {
			broadcaster.Publish(models.AlertWebhookPayload{
				RuleID:     ruleID,
				RuleName:   "hold_decided",
				Severity:   parseStr(row["severity"]),
				AgentID:    parseStr(row["agent_id"]),
				Message:    fmt.Sprintf("hold:%s|decision:abandoned", holdID),
				DetectedAt: time.Now().UTC().Format(time.RFC3339),
			})
		}
		count++
	}
	if count > 0 {
		log.Printf("holds: abandoned %d pending hold(s) for rule=%s reason=%q", count, ruleID, reason)
	}
	return count
}

// ── Helpers ────────────────────────────────────────────────────────────────

func getHoldByID(cfg *config.Config, holdID string) (*models.Hold, error) {
	sql := fmt.Sprintf(`
SELECT hold_id, deployment_id, agent_id, session_id, run_id, tool_name,
       rule_id, severity, args_summary, message, created_at, status,
       decided_by, decided_at, decided_reason
FROM ` + holdsLatestSubquery + `
WHERE hold_id = '%s'
LIMIT 1`, escapeSQLStr(holdID))
	rows, err := tp.Query(&cfg.Timeplus, sql)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	h := rowToHold(rows[0])
	return &h, nil
}

func rowToHold(row map[string]interface{}) models.Hold {
	return models.Hold{
		HoldID:        parseStr(row["hold_id"]),
		DeploymentID:  parseStr(row["deployment_id"]),
		AgentID:       parseStr(row["agent_id"]),
		SessionID:     parseStr(row["session_id"]),
		RunID:         parseStr(row["run_id"]),
		ToolName:      parseStr(row["tool_name"]),
		RuleID:        parseStr(row["rule_id"]),
		Severity:      parseStr(row["severity"]),
		ArgsSummary:   parseStr(row["args_summary"]),
		Message:       parseStr(row["message"]),
		CreatedAt:     parseTime(row["created_at"]),
		Status:        parseStr(row["status"]),
		DecidedBy:     parseStr(row["decided_by"]),
		DecidedAt:     parseTime(row["decided_at"]),
		DecidedReason: parseStr(row["decided_reason"]),
	}
}
