package handlers

import (
	"testing"

	"github.com/timeplus/agentguard/internal/models"
)

func TestDecide_KillSwitchAllowsAlways(t *testing.T) {
	out := Decide(DecisionInput{KillSwitch: true})
	if out.Decision != "allow" {
		t.Fatalf("got %s, want allow", out.Decision)
	}
}

func TestDecide_NoMatchesAllow(t *testing.T) {
	out := Decide(DecisionInput{})
	if out.Decision != "allow" {
		t.Fatalf("got %s, want allow", out.Decision)
	}
}

func TestDecide_AutoBlockReturnsBlock(t *testing.T) {
	out := Decide(DecisionInput{
		Matches: []RuleMatch{
			{RuleID: "rp_001", BlockPolicy: "auto_block", Severity: "critical", Message: "blocked!"},
		},
	})
	if out.Decision != "block" {
		t.Fatalf("got %s, want block", out.Decision)
	}
	if out.RuleID != "rp_001" {
		t.Fatalf("got rule %s, want rp_001", out.RuleID)
	}
	if out.Reason != "blocked!" {
		t.Fatalf("got reason %q, want %q", out.Reason, "blocked!")
	}
}

func TestDecide_HoldReturnsHold(t *testing.T) {
	out := Decide(DecisionInput{
		Matches: []RuleMatch{
			{RuleID: "rp_002", BlockPolicy: "hold", Severity: "warning", Message: "needs review"},
		},
	})
	if out.Decision != "hold" {
		t.Fatalf("got %s, want hold", out.Decision)
	}
}

func TestDecide_HoldBeatsAutoBlock(t *testing.T) {
	out := Decide(DecisionInput{
		Matches: []RuleMatch{
			{RuleID: "rp_001", BlockPolicy: "auto_block"},
			{RuleID: "rp_002", BlockPolicy: "hold"},
		},
	})
	if out.Decision != "hold" {
		t.Fatalf("got %s, want hold (hold > auto_block)", out.Decision)
	}
	if out.RuleID != "rp_002" {
		t.Fatalf("got rule %s, want rp_002", out.RuleID)
	}
}

func TestDecide_LogOnlyDoesNotCount(t *testing.T) {
	out := Decide(DecisionInput{
		Matches: []RuleMatch{
			{RuleID: "rp_003", BlockPolicy: "log_only"},
		},
	})
	if out.Decision != "allow" {
		t.Fatalf("got %s, want allow (log_only does not block)", out.Decision)
	}
}

func TestDecide_PreExistingThreatTriggers(t *testing.T) {
	out := Decide(DecisionInput{
		PreExistingThreats: []models.Threat{
			{RuleID: "rp_001", Status: "open"},
		},
		ThreatRulePolicies: map[string]string{"rp_001": "auto_block"},
	})
	if out.Decision != "block" {
		t.Fatalf("got %s, want block", out.Decision)
	}
}

func TestDecide_PreExistingClearedThreatIgnored(t *testing.T) {
	// status='cleared' (not 'open') must NOT auto-trigger
	out := Decide(DecisionInput{
		PreExistingThreats: []models.Threat{
			{RuleID: "rp_001", Status: "cleared"},
		},
		ThreatRulePolicies: map[string]string{"rp_001": "auto_block"},
	})
	if out.Decision != "allow" {
		t.Fatalf("got %s, want allow (cleared threats don't trigger)", out.Decision)
	}
}
