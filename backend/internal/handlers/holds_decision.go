package handlers

import "github.com/timeplus/agentguard/internal/models"

// RuleMatch represents one rule that fired for the current hold request.
// Either from a pre-existing open threat or from a fresh MV detection
// observed during the wait window.
type RuleMatch struct {
	RuleID      string
	BlockPolicy string // hold | auto_block | log_only
	Severity    string
	Message     string
}

// DecisionInput is the data the Decide function works on.
// All Timeplus I/O happens in the handler before this struct is built.
type DecisionInput struct {
	KillSwitch         bool
	Matches            []RuleMatch
	PreExistingThreats []models.Threat
	// ThreatRulePolicies maps rule_id → block_policy for the pre-existing threats.
	// (We resolve this once in the handler so Decide stays pure.)
	ThreatRulePolicies map[string]string
}

// DecisionOutput is what the hold handler does next.
type DecisionOutput struct {
	Decision string // "allow" | "block" | "hold"
	RuleID   string
	Reason   string
	Severity string
}

// Decide picks the strictest applicable block_policy across matches and
// pre-existing open threats. Strictness: hold > auto_block > log_only > (no match).
// Map-output:
//
//	hold       → Decision="hold"
//	auto_block → Decision="block"
//	log_only   → Decision="allow"
//	no match   → Decision="allow"
func Decide(in DecisionInput) DecisionOutput {
	if in.KillSwitch {
		return DecisionOutput{Decision: "allow"}
	}

	var allMatches []RuleMatch
	allMatches = append(allMatches, in.Matches...)
	for _, t := range in.PreExistingThreats {
		if t.Status != "open" {
			continue
		}
		policy := in.ThreatRulePolicies[t.RuleID]
		if policy == "" || policy == "log_only" {
			continue
		}
		allMatches = append(allMatches, RuleMatch{
			RuleID:      t.RuleID,
			BlockPolicy: policy,
			Severity:    t.Severity,
			Message:     "Pre-existing open threat: " + t.RuleName,
		})
	}

	var hold, autoBlock *RuleMatch
	for i := range allMatches {
		switch allMatches[i].BlockPolicy {
		case "hold":
			if hold == nil {
				hold = &allMatches[i]
			}
		case "auto_block":
			if autoBlock == nil {
				autoBlock = &allMatches[i]
			}
		}
	}

	if hold != nil {
		return DecisionOutput{
			Decision: "hold",
			RuleID:   hold.RuleID,
			Reason:   hold.Message,
			Severity: hold.Severity,
		}
	}
	if autoBlock != nil {
		return DecisionOutput{
			Decision: "block",
			RuleID:   autoBlock.RuleID,
			Reason:   autoBlock.Message,
			Severity: autoBlock.Severity,
		}
	}
	return DecisionOutput{Decision: "allow"}
}
