package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/holdregistry"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/rules"
	"github.com/timeplus/agentguard/internal/sentry"
	"github.com/timeplus/agentguard/internal/sentry/skills"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// errConversationForbidden and errConversationNotFound are sentinel errors
// resolveConversation uses to tell the caller which HTTP status a lookup
// failure maps to, distinct from a plain Timeplus/store error (mapped to 500).
var (
	errConversationForbidden = errors.New("sentry: conversation belongs to another user")
	errConversationNotFound  = errors.New("sentry: conversation not found")
)

// sentryThreatStatusFn adapts the threat-status upsert the HTTP endpoints
// use (upsertThreatStatus) into the skills.ThreatStatusFunc shape.
func sentryThreatStatusFn(cfg *config.Config) skills.ThreatStatusFunc {
	return func(agentID, deploymentID, sessionID, ruleID, status string) error {
		return upsertThreatStatus(cfg, ThreatStatusBody{
			AgentID: agentID, DeploymentID: deploymentID,
			SessionID: sessionID, RuleID: ruleID,
		}, status)
	}
}

// sentryHoldDecideFn adapts the hold decision path — the SAME
// getHoldByID + terminalizeHold sequence DecideHold runs, including the
// in-memory holdregistry claim (unblocks the waiting plugin request) and
// the SSE broadcast — into the skills.HoldDecideFunc shape.
func sentryHoldDecideFn(cfg *config.Config, holdReg *holdregistry.Registry) skills.HoldDecideFunc {
	return func(holdID, decision, decidedBy, reason string) (string, error) {
		existing, err := getHoldByID(cfg, holdID)
		if err != nil {
			return "", fmt.Errorf("query hold: %w", err)
		}
		if existing == nil {
			return "", fmt.Errorf("hold %s not found", holdID)
		}
		if existing.Status != "pending" {
			return "hold already decided: " + existing.Status + " (no new write)", nil
		}
		won, err := terminalizeHold(cfg, holdReg, holdID, decision, decidedBy, reason, holdregistry.Decision{
			Allow:  decision == "approved",
			Reason: reason,
		})
		if err != nil {
			return "", fmt.Errorf("write decision: %w", err)
		}
		if !won {
			return "hold is already being resolved by another operator (no new write)", nil
		}
		GlobalAlertBroadcaster.Publish(models.AlertWebhookPayload{
			RuleID:     existing.RuleID,
			RuleName:   "hold_decided",
			Severity:   existing.Severity,
			AgentID:    existing.AgentID,
			Message:    fmt.Sprintf("hold:%s|decision:%s", holdID, decision),
			DetectedAt: time.Now().UTC().Format(time.RFC3339),
		})
		return fmt.Sprintf("hold %s %s (tool %s, agent %s)", holdID, decision, existing.ToolName, existing.AgentID), nil
	}
}

// sentryRuleLifecycleFuncs adapts the rule-lifecycle service — the SAME
// calls the HTTP endpoints run (parameter compiler, candidate preflight,
// per-rule-serialized drop/create MV) including the hold-abandonment side
// effects — into the skills.RuleLifecycleFuncs shape. Results are the
// updated instance as JSON, matching what the Rules UI receives.
func sentryRuleLifecycleFuncs(cfg *config.Config, lc *rules.Lifecycle, holdReg *holdregistry.Registry) skills.RuleLifecycleFuncs {
	instJSON := func(inst rules.RuleInstance) (string, error) {
		b, err := json.Marshal(inst)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return skills.RuleLifecycleFuncs{
		Install: func(ruleID string, overrides map[string]any, user, reason string) (string, error) {
			inst, err := lc.For(&cfg.Timeplus).Install(ruleID, overrides, user, reason)
			if err != nil {
				return "", err
			}
			return instJSON(inst)
		},
		Configure: func(ruleID string, overrides map[string]any, user, reason string) (string, error) {
			inst, err := lc.For(&cfg.Timeplus).Configure(ruleID, overrides, user, reason)
			if err != nil {
				return "", err
			}
			return instJSON(inst)
		},
		SetEnabled: func(ruleID string, enabled bool, user string) (string, error) {
			// Idempotent explicit-state semantics on top of the service's
			// flip-only Toggle: a model acting on stale state must never
			// accidentally flip the wrong way.
			svc := lc.For(&cfg.Timeplus)
			insts, err := svc.List()
			if err != nil {
				return "", err
			}
			for _, inst := range insts {
				if inst.RuleID == ruleID && inst.Enabled == enabled {
					return fmt.Sprintf(`{"rule_id":%q,"enabled":%t,"note":"already in the desired state; no change"}`, ruleID, enabled), nil
				}
			}
			inst, err := svc.Toggle(ruleID, user, "toggle via sentry")
			if err != nil {
				return "", err
			}
			if !inst.Enabled {
				AbandonHoldsForRule(cfg, holdReg, GlobalAlertBroadcaster, ruleID, "rule disabled")
			}
			return instJSON(inst)
		},
		SetPolicy: func(ruleID, policy, user, reason string) (string, error) {
			inst, err := lc.For(&cfg.Timeplus).SetPolicy(ruleID, policy, user, reason)
			if err != nil {
				return "", err
			}
			if inst.BlockPolicy == "log_only" {
				AbandonHoldsForRule(cfg, holdReg, GlobalAlertBroadcaster, ruleID, "rule policy changed to log_only")
			}
			return instJSON(inst)
		},
		Upgrade: func(ruleID, user string) (string, error) {
			inst, err := lc.For(&cfg.Timeplus).Upgrade(ruleID, user, "upgrade via sentry")
			if err != nil {
				return "", err
			}
			return instJSON(inst)
		},
		Uninstall: func(ruleID, user string) (string, error) {
			if _, err := lc.For(&cfg.Timeplus).Uninstall(ruleID, user, "uninstall via sentry"); err != nil {
				return "", err
			}
			AbandonHoldsForRule(cfg, holdReg, GlobalAlertBroadcaster, ruleID, "rule uninstalled")
			return fmt.Sprintf(`{"rule_id":%q,"deleted":true}`, ruleID), nil
		},
		Sync: func(user string) (string, error) {
			results, err := lc.For(&cfg.Timeplus).Sync()
			if err != nil {
				return "", err
			}
			b, err := json.Marshal(map[string]any{"results": results})
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	}
}

// sentryConfigToolView renders the get_sentry_config tool result from raw
// config rows: the chat/memory values redacted with the SAME redactAPIKey
// the HTTP API uses (api_key -> api_key_set flag), plus status/posture and
// the honest phase note that memory is configuration-only until #211.
// Pure — unit-tested with canary keys.
func sentryConfigToolView(rows []tp.SentryConfigRow, cc sentry.ChatCfg) (string, error) {
	cfgOut := map[string]any{}
	keySet := map[string]bool{}
	for _, r := range rows {
		redacted, had := redactAPIKey(r.Value)
		cfgOut[r.Key] = redacted
		keySet[r.Key] = had
	}
	out := map[string]any{
		"config":      cfgOut,
		"api_key_set": keySet,
		"status": map[string]any{
			"agentguard_tools_enabled": cc.AgentGuardToolsEnabled,
			"write_actions":            "require in-chat human approval",
			"memory_active":            false,
			"note":                     "memory is configuration-only until the memory feature (Phase 2) ships; the embedding endpoint is validated but not yet consumed",
		},
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// sentrySelfConfigFn injects get_sentry_config's read: live rows from the
// config stream, rendered through sentryConfigToolView.
func sentrySelfConfigFn(cfg *config.Config, cc sentry.ChatCfg) func() (string, error) {
	return func() (string, error) {
		rows, err := tp.ReadSentryConfig(&cfg.Timeplus)
		if err != nil {
			return "", err
		}
		return sentryConfigToolView(rows, cc)
	}
}

// sentryRegistry builds the tool surface Sentry's chat loop exposes to the
// model.
// The AgentGuard interaction surface — the agentguard-guide knowledge skill
// plus every AgentGuard-specific tool, read and write — sits behind ONE
// operator switch, the chat config row's agentguard_tools_enabled bool,
// DISABLED by default. Off, Sentry is a generic Timeplus SQL analyst
// (knowledge, schema introspection, run_sql). Write tools remain
// individually approval-gated (#221) even when the surface is enabled.
func sentryRegistry(cfg *config.Config, mgr *catalog.Manager, lc *rules.Lifecycle, holdReg *holdregistry.Registry, cc sentry.ChatCfg) *skills.Registry {
	var hiddenSkills []string
	if !cc.AgentGuardToolsEnabled {
		hiddenSkills = append(hiddenSkills, "agentguard-guide")
	}
	sks := []skills.Skill{
		skills.NewLoadSkill(cfg.Sentry.SkillDir, hiddenSkills...),
		skills.NewLoadSkillReference(cfg.Sentry.SkillDir, hiddenSkills...),
		skills.NewListStreams(),
		skills.NewDescribeStream(),
		skills.NewRunSQL(),
		skills.NewGetSentryConfig(sentrySelfConfigFn(cfg, cc)),
	}
	if cc.AgentGuardToolsEnabled {
		sks = append(sks,
			skills.NewDLPSearch(),
			skills.NewGetThreats(),
			skills.NewGetHolds(),
			skills.NewGetRuleStatus(),
			skills.NewGetCostSummary(),
			skills.NewGetSessionTimeline(),
			skills.NewRuleCatalog(mgr, lc),
			skills.NewAcknowledgeThreat(sentryThreatStatusFn(cfg)),
			skills.NewClearThreat(sentryThreatStatusFn(cfg)),
			skills.NewDecideHold(sentryHoldDecideFn(cfg, holdReg)))
		sks = append(sks, skills.NewRuleLifecycleSkills(sentryRuleLifecycleFuncs(cfg, lc, holdReg))...)
	}
	// Verification-only: a no-side-effect GatedWrite tool for exercising the
	// #221 approval flow end-to-end.
	if os.Getenv("SENTRY_DEMO_WRITE") == "1" {
		sks = append(sks, skills.NewDemoWrite())
	}
	return skills.NewRegistry(sks...)
}

// resolveConversation implements the create-or-continue semantics shared by
// SentryChat: an empty id starts a new conversation (titled from the first
// message); a non-empty id must exist, be owned by user, and still be
// "active" (a deleted/archived conversation is gone — not found), else an
// error mapping to 404/403 (checked BEFORE any NDJSON header is written).
func resolveConversation(store *sentry.Store, user, id, message string) (models.SentryConversation, error) {
	if strings.TrimSpace(id) == "" {
		return store.CreateConversation(user, message)
	}
	conv, ok, err := store.Conversation(id)
	if err != nil {
		return models.SentryConversation{}, err
	}
	if !ok || conv.Status != "active" {
		return models.SentryConversation{}, errConversationNotFound
	}
	if conv.User != user {
		return models.SentryConversation{}, errConversationForbidden
	}
	return conv, nil
}

// ownedActiveConversation loads :id, enforcing existence + "active" status
// (404) and session-user ownership (403), writing the error response itself.
// Shared by the Get/Rename/Delete conversation handlers.
func ownedActiveConversation(c *gin.Context, store *sentry.Store) (models.SentryConversation, bool) {
	conv, ok, err := store.Conversation(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return models.SentryConversation{}, false
	}
	if !ok || conv.Status != "active" {
		c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
		return models.SentryConversation{}, false
	}
	if conv.User != sessionUser(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "conversation belongs to another user"})
		return models.SentryConversation{}, false
	}
	return conv, true
}

// SentryChat handles POST /sentry/chat: {conversation_id?, message} -> an
// NDJSON stream of sentry.Event lines. Every failure that can be reported as
// plain JSON (disabled, bad input, chat-config load, conversation
// resolution/ownership) happens BEFORE any header is written; once streaming
// begins, only NDJSON error events can report a failure.
func SentryChat(cfg *config.Config, mgr *catalog.Manager, lc *rules.Lifecycle, approvals *sentry.ApprovalRegistry, holdReg *holdregistry.Registry) gin.HandlerFunc {
	return func(c *gin.Context) {
		rcfg := auth.RequestCfg(c, cfg)
		if !rcfg.Sentry.Enabled {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Sentry is disabled (sentry.enabled=false)"})
			return
		}
		var body struct {
			ConversationID string `json:"conversation_id"`
			Message        string `json:"message"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Message) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
			return
		}
		user := sessionUser(c)
		store := &sentry.Store{TP: &rcfg.Timeplus}

		// Everything that can fail with a plain JSON status happens BEFORE headers.
		cc, err := sentry.LoadChatCfg(&rcfg.Timeplus)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		llm := sentry.NewLLM(cc)
		conv, err := resolveConversation(store, user, body.ConversationID, body.Message)
		if err != nil {
			switch {
			case errors.Is(err, errConversationForbidden):
				c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			case errors.Is(err, errConversationNotFound):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		c.Header("Content-Type", "application/x-ndjson")
		c.Header("X-Accel-Buffering", "no")
		c.Header("Cache-Control", "no-cache")
		c.Status(http.StatusOK)
		flusher, _ := c.Writer.(http.Flusher)
		emitted := false
		emit := func(ev sentry.Event) {
			emitted = true
			b, err := json.Marshal(ev)
			if err != nil {
				log.Printf("sentry: chat: marshal event: %v", err)
				return
			}
			_, _ = c.Writer.Write(append(b, '\n'))
			if flusher != nil {
				flusher.Flush()
			}
		}

		loop := &sentry.Loop{LLM: llm, Reg: sentryRegistry(rcfg, mgr, lc, holdReg, cc), Store: store, Cfg: rcfg, Approvals: approvals,
			Memory: sentry.LoadMemoryCfg(&rcfg.Timeplus)}
		if err := loop.Run(c.Request.Context(), cc, conv, strings.TrimSpace(body.Message), emit); err != nil {
			log.Printf("sentry: chat: %v", err)
			// The loop emits its own "error" event for in-stream failures
			// (e.g. a mid-turn LLM call). The pre-stream Messages/NextSeq
			// path in Loop.Run returns before calling emit at all, so if
			// nothing was written yet, this handler must surface the error
			// itself -- as an NDJSON line, since headers are already sent.
			if !emitted {
				emit(sentry.Event{Type: "error", Message: err.Error()})
			}
			return
		}
		// Off the critical path: while the conversation still carries its
		// auto-derived title (first user message, never renamed by the
		// operator), ask the chat model for a short summary name. No-op once
		// any rename — human or LLM — has happened; fail-open on any error.
		sentry.MaybeAutoTitleAsync(llm, cc, store, conv.ConversationID)
	}
}

// ListSentryConversations handles GET /sentry/conversations: the session
// user's own conversations, most-recently-updated first.
func ListSentryConversations(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rcfg := auth.RequestCfg(c, cfg)
		store := &sentry.Store{TP: &rcfg.Timeplus}
		convs, err := store.List(sessionUser(c), 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"conversations": convs})
	}
}

// GetSentryConversation handles GET /sentry/conversations/:id: the
// conversation plus its full message replay, owner-only (403/404; a deleted
// conversation is 404).
func GetSentryConversation(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rcfg := auth.RequestCfg(c, cfg)
		store := &sentry.Store{TP: &rcfg.Timeplus}
		conv, ok := ownedActiveConversation(c, store)
		if !ok {
			return
		}
		msgs, err := store.Messages(conv.ConversationID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"conversation": conv, "messages": msgs})
	}
}

// RenameSentryConversation handles PATCH /sentry/conversations/:id:
// owner-only title replacement ({title}, non-empty, truncated to 80 runes).
// A user-set title is permanent: it no longer matches the auto-derived
// first-message title, so LLM auto-naming (MaybeAutoTitle) will never
// overwrite it.
func RenameSentryConversation(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rcfg := auth.RequestCfg(c, cfg)
		store := &sentry.Store{TP: &rcfg.Timeplus}
		var body struct {
			Title string `json:"title"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Title) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "title is required"})
			return
		}
		conv, ok := ownedActiveConversation(c, store)
		if !ok {
			return
		}
		if err := store.Rename(conv.ConversationID, strings.TrimSpace(body.Title)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		conv, _, err := store.Conversation(conv.ConversationID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"conversation": conv})
	}
}

// DeleteSentryConversation handles DELETE /sentry/conversations/:id:
// owner-only soft-delete (status -> "deleted"). The conversation disappears
// from List and reads 404 afterwards; message rows stay in the append-only
// log but become unreachable through the API.
func DeleteSentryConversation(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rcfg := auth.RequestCfg(c, cfg)
		store := &sentry.Store{TP: &rcfg.Timeplus}
		conv, ok := ownedActiveConversation(c, store)
		if !ok {
			return
		}
		if err := store.Delete(conv.ConversationID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "deleted"})
	}
}

// DecideSentryApproval handles POST /sentry/approvals/:proposal_id/decide:
// resolve one pending GatedWrite proposal ({approve, reason?}). Owner-only —
// the registry itself enforces that only the conversation's user may decide
// (403); an unknown or already-resolved proposal is 404. There is no
// auto-approve path anywhere, by design (#221).
func DecideSentryApproval(approvals *sentry.ApprovalRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Approve bool   `json:"approve"`
			Reason  string `json:"reason"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "body must be {approve: bool, reason?: string}"})
			return
		}
		err := approvals.Decide(c.Param("proposal_id"), sessionUser(c), body.Approve, strings.TrimSpace(body.Reason))
		switch {
		case errors.Is(err, sentry.ErrApprovalNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, sentry.ErrApprovalForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusOK, gin.H{"status": "decided", "approved": body.Approve})
		}
	}
}

// SentryStatus handles GET /sentry/status: whether Sentry is enabled,
// whether both its streams are provisioned, the resolved chat model, and
// whether a base_url is configured (or a config-load error string).
func SentryStatus(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rcfg := auth.RequestCfg(c, cfg)
		resp := gin.H{"enabled": rcfg.Sentry.Enabled, "provisioned": false}
		if !rcfg.Sentry.Enabled {
			c.JSON(http.StatusOK, resp)
			return
		}

		rows, err := tp.Query(&rcfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name IN ('agentguard_sentry_conversations', 'agentguard_sentry_messages', 'agentguard_sentry_config')
			  AND database = current_database()`)
		if err != nil {
			log.Printf("sentry: status system.tables: %v", err)
			resp["tables_check_error"] = err.Error()
		} else {
			found := map[string]bool{}
			for _, row := range rows {
				if name, ok := row["name"].(string); ok {
					found[name] = true
				}
			}
			resp["provisioned"] = found["agentguard_sentry_conversations"] &&
				found["agentguard_sentry_messages"] && found["agentguard_sentry_config"]
		}

		cc, err := sentry.LoadChatCfg(&rcfg.Timeplus)
		if err != nil {
			resp["config_error"] = err.Error()
			c.JSON(http.StatusOK, resp)
			return
		}
		resp["model"] = cc.Model
		resp["base_url_set"] = strings.TrimSpace(cc.BaseURL) != ""
		c.JSON(http.StatusOK, resp)
	}
}
