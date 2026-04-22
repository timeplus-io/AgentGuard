package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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

// failPolicyToDecision maps the config FailPolicy ("deny"|"allow") to the
// HoldResponse Decision contract ("block"|"allow"). Returns "block" by default
// for any unknown value (fail-closed).
func failPolicyToDecision(policy string) string {
	if policy == "allow" {
		return "allow"
	}
	return "block"
}

// CreateHold handles POST /api/holds.
// Synchronous endpoint — does not return until backend has decided
// (allow / block / hold + human decision / hold timeout).
func CreateHold(cfg *config.Config, registry *holdregistry.Registry, broadcaster *alertBroadcaster) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
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
				_ = writeHoldStatus(cfg, holdID, "timeout", "system", "hold timeout")
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
	sql := fmt.Sprintf(`
INSERT INTO agentguard_hook_events
  (hook_name, event_time, session_id, run_id, agent_id, deployment_id,
   deployment_name, session_key, tool_name, provider, model,
   hook_decision, block_reason, event_data, agent_type)
VALUES
  ('before_tool_call', '%s', '%s', '%s', '%s', '%s',
   '', '', '%s', '', '',
   'observe', '', '%s', 'claudecode')`,
		req.EventTime.UTC().Format("2006-01-02 15:04:05.999"),
		escapeSQLStr(req.SessionID),
		escapeSQLStr(req.RunID),
		escapeSQLStr(req.AgentID),
		escapeSQLStr(req.DeploymentID),
		escapeSQLStr(req.ToolName),
		escapeSQLStr(string(payloadJSON)),
	)
	return tp.ExecHTTP(&cfg.Timeplus, sql)
}

// queryPreExistingOpenThreats returns open threats for this session and
// a rule_id → block_policy map for those threats' rules.
func queryPreExistingOpenThreats(cfg *config.Config, req *models.HoldRequest) ([]models.Threat, map[string]string, error) {
	sql := fmt.Sprintf(`
SELECT t.rule_id, t.rule_name, t.severity, 'open' AS status, r.block_policy
FROM (
  SELECT rule_id,
         any(rule_name) AS rule_name,
         any(severity)  AS severity
  FROM table(agentguard_security_events)
  WHERE deployment_id = '%s' AND agent_id = '%s' AND session_id = '%s'
  GROUP BY rule_id
) AS t
LEFT JOIN (
  SELECT rule_id, arg_max(block_policy, updated_at) AS block_policy
  FROM table(agentguard_rules)
  GROUP BY rule_id
) AS r ON t.rule_id = r.rule_id`,
		escapeSQLStr(req.DeploymentID), escapeSQLStr(req.AgentID), escapeSQLStr(req.SessionID))

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
			Status:   parseStr(row["status"]),
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
LEFT JOIN (
  SELECT rule_id, arg_max(block_policy, updated_at) AS block_policy
  FROM table(agentguard_rules)
  GROUP BY rule_id
) AS r ON s.rule_id = r.rule_id
WHERE r.block_policy IN ('hold', 'auto_block')`,
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
FROM mv_holds_current
WHERE hold_id = '%s'
LIMIT 1`,
		status, escapeSQLStr(decidedBy), now, escapeSQLStr(decidedReason), now,
		escapeSQLStr(holdID))
	return tp.ExecHTTP(&cfg.Timeplus, sql)
}
