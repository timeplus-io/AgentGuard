package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Batch-1 GatedWrite skills (#222): thin, validated arg-parsers around
// write logic INJECTED from the handlers package as closures — the same
// functions the HTTP endpoints run (threat-status upsert, hold
// terminalization incl. the in-memory holdregistry claim and SSE
// broadcast), so agent-driven writes can never drift from UI-driven ones.
// Every skill here is Access() == GatedWrite: the loop only executes it
// after an explicit in-chat human approval (#221).

// ThreatStatusFunc applies a threat status upsert (status is
// "acknowledged" or "cleared"). Injected by the handlers package.
type ThreatStatusFunc func(agentID, deploymentID, sessionID, ruleID, status string) error

// HoldDecideFunc resolves one pending hold ("approved" | "denied") and
// returns a short outcome description. Injected by the handlers package.
type HoldDecideFunc func(holdID, decision, decidedBy, reason string) (string, error)

// threatStatus implements acknowledge_threat / clear_threat.
type threatStatus struct {
	fn     ThreatStatusFunc
	status string // "acknowledged" | "cleared"
}

// NewAcknowledgeThreat returns the gated "acknowledge_threat" skill.
func NewAcknowledgeThreat(fn ThreatStatusFunc) Skill {
	return threatStatus{fn: fn, status: "acknowledged"}
}

// NewClearThreat returns the gated "clear_threat" skill.
func NewClearThreat(fn ThreatStatusFunc) Skill {
	return threatStatus{fn: fn, status: "cleared"}
}

func (t threatStatus) Name() string {
	if t.status == "cleared" {
		return "clear_threat"
	}
	return "acknowledge_threat"
}

func (t threatStatus) Description() string {
	verb := "Acknowledge"
	if t.status == "cleared" {
		verb = "Clear"
	}
	return verb + " one threat (identified by agent_id + deployment_id + session_id + rule_id, as returned by get_threats). Requires in-chat human approval before executing. If the rule fires again afterwards, the threat auto-reopens."
}

func (threatStatus) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_id":      map[string]any{"type": "string"},
			"deployment_id": map[string]any{"type": "string"},
			"session_id":    map[string]any{"type": "string"},
			"rule_id":       map[string]any{"type": "string"},
		},
		"required": []string{"agent_id", "deployment_id", "session_id", "rule_id"},
	}
}

func (threatStatus) Access() AccessClass { return GatedWrite }

func (t threatStatus) Execute(_ context.Context, _ *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		AgentID      string `json:"agent_id"`
		DeploymentID string `json:"deployment_id"`
		SessionID    string `json:"session_id"`
		RuleID       string `json:"rule_id"`
	}
	_ = json.Unmarshal(args, &a)
	for name, v := range map[string]string{
		"agent_id": a.AgentID, "deployment_id": a.DeploymentID,
		"session_id": a.SessionID, "rule_id": a.RuleID,
	} {
		if strings.TrimSpace(v) == "" {
			return "error: " + name + " is required (use the exact identity from get_threats)", nil
		}
	}
	if t.fn == nil {
		return "error: threat status writes are not wired in this context", nil
	}
	if err := t.fn(a.AgentID, a.DeploymentID, a.SessionID, a.RuleID, t.status); err != nil {
		return "error: " + err.Error(), nil
	}
	return fmt.Sprintf("threat (rule %s, session %s) marked %s", a.RuleID, a.SessionID, t.status), nil
}

// decideHold implements the gated "decide_hold" skill.
type decideHold struct{ fn HoldDecideFunc }

// NewDecideHold returns the gated "decide_hold" skill.
func NewDecideHold(fn HoldDecideFunc) Skill { return decideHold{fn: fn} }

func (decideHold) Name() string { return "decide_hold" }
func (decideHold) Description() string {
	return "Approve or deny one pending human-in-the-loop hold (hold_id from get_holds). decision must be 'approved' or 'denied'. Requires in-chat human approval before executing; the waiting agent action then proceeds or is blocked accordingly."
}
func (decideHold) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"hold_id":  map[string]any{"type": "string"},
			"decision": map[string]any{"type": "string", "enum": []string{"approved", "denied"}},
			"reason":   map[string]any{"type": "string", "description": "short justification recorded on the hold"},
		},
		"required": []string{"hold_id", "decision"},
	}
}
func (decideHold) Access() AccessClass { return GatedWrite }

func (d decideHold) Execute(_ context.Context, rc *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		HoldID   string `json:"hold_id"`
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	_ = json.Unmarshal(args, &a)
	a.HoldID = strings.TrimSpace(a.HoldID)
	a.Decision = strings.ToLower(strings.TrimSpace(a.Decision))
	if a.HoldID == "" {
		return "error: hold_id is required (use get_holds to find pending holds)", nil
	}
	if a.Decision != "approved" && a.Decision != "denied" {
		return "error: decision must be 'approved' or 'denied'", nil
	}
	if d.fn == nil {
		return "error: hold decisions are not wired in this context", nil
	}
	decidedBy := rc.User
	if decidedBy == "" {
		decidedBy = "sentry"
	}
	out, err := d.fn(a.HoldID, a.Decision, decidedBy, a.Reason)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	return out, nil
}
