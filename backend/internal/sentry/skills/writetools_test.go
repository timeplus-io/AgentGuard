package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func execSkill(t *testing.T, sk Skill, rc *RunContext, args map[string]string) string {
	t.Helper()
	b, _ := json.Marshal(args)
	out, err := sk.Execute(context.Background(), rc, b)
	if err != nil {
		t.Fatalf("%s: unexpected Go error: %v", sk.Name(), err)
	}
	return out
}

func TestThreatStatusSkills(t *testing.T) {
	var got []string
	fn := func(agentID, deploymentID, sessionID, ruleID, status string) error {
		got = []string{agentID, deploymentID, sessionID, ruleID, status}
		return nil
	}
	full := map[string]string{
		"agent_id": "a1", "deployment_id": "d1", "session_id": "s1", "rule_id": "rp001",
	}

	for _, tc := range []struct {
		sk         Skill
		name, want string
	}{
		{NewAcknowledgeThreat(fn), "acknowledge_threat", "acknowledged"},
		{NewClearThreat(fn), "clear_threat", "cleared"},
	} {
		if tc.sk.Name() != tc.name {
			t.Fatalf("name = %q, want %q", tc.sk.Name(), tc.name)
		}
		if tc.sk.Access() != GatedWrite {
			t.Fatalf("%s must be GatedWrite", tc.name)
		}
		got = nil
		out := execSkill(t, tc.sk, nil, full)
		if !strings.Contains(out, tc.want) || got == nil || got[4] != tc.want {
			t.Fatalf("%s: out=%q fn-called=%v", tc.name, out, got)
		}
		// Missing identity fields are rejected before the fn runs.
		got = nil
		out = execSkill(t, tc.sk, nil, map[string]string{"agent_id": "a1"})
		if !strings.Contains(out, "error:") || got != nil {
			t.Fatalf("%s: missing fields must error without calling fn, out=%q", tc.name, out)
		}
	}
}

func TestDecideHoldSkill(t *testing.T) {
	var got []string
	sk := NewDecideHold(func(holdID, decision, decidedBy, reason string) (string, error) {
		got = []string{holdID, decision, decidedBy, reason}
		return "hold " + holdID + " " + decision, nil
	})
	if sk.Access() != GatedWrite {
		t.Fatal("decide_hold must be GatedWrite")
	}
	rc := &RunContext{User: "alice"}

	out := execSkill(t, sk, rc, map[string]string{"hold_id": "h1", "decision": "Approved", "reason": "ok"})
	if !strings.Contains(out, "h1 approved") {
		t.Fatalf("out = %q", out)
	}
	if got[1] != "approved" || got[2] != "alice" || got[3] != "ok" {
		t.Fatalf("fn args = %v (decision must normalize, decided_by from RunContext.User)", got)
	}

	got = nil
	if out := execSkill(t, sk, rc, map[string]string{"hold_id": "h1", "decision": "maybe"}); !strings.Contains(out, "error:") || got != nil {
		t.Fatalf("invalid decision must error without calling fn, out=%q", out)
	}
	if out := execSkill(t, sk, rc, map[string]string{"decision": "approved"}); !strings.Contains(out, "error:") {
		t.Fatalf("missing hold_id must error, out=%q", out)
	}

	// Empty User falls back to a non-empty attribution.
	got = nil
	_ = execSkill(t, sk, &RunContext{}, map[string]string{"hold_id": "h2", "decision": "denied"})
	if got[2] == "" {
		t.Fatal("decided_by must never be empty")
	}
}
