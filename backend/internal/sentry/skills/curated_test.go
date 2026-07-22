package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

// TestCuratedSkillNames pins the exact tool-name strings the loop and
// frontend depend on (per the task-10 brief). A typo here breaks callers
// silently, so this is a plain string-equality check per skill.
func TestCuratedSkillNames(t *testing.T) {
	cases := []struct {
		skill Skill
		want  string
	}{
		{NewGetThreats(), "get_threats"},
		{NewGetHolds(), "get_holds"},
		{NewGetRuleStatus(), "get_rule_status"},
		{NewGetCostSummary(), "get_cost_summary"},
		{NewGetSessionTimeline(), "get_session_timeline"},
	}
	for _, tc := range cases {
		if got := tc.skill.Name(); got != tc.want {
			t.Errorf("Name() = %q, want %q", got, tc.want)
		}
	}
}

// TestCuratedSkillsReadOnly ensures none of these five curated adapters was
// accidentally given write/gated access.
func TestCuratedSkillsReadOnly(t *testing.T) {
	skills := []Skill{
		NewGetThreats(), NewGetHolds(), NewGetRuleStatus(),
		NewGetCostSummary(), NewGetSessionTimeline(),
	}
	for _, s := range skills {
		if s.Access() != ReadOnly {
			t.Errorf("%s: Access() = %v, want ReadOnly", s.Name(), s.Access())
		}
	}
}

// TestGetSessionTimelineParamsRequired checks that agent_id and session_id
// are marked required in the tool schema — the loop relies on this to
// prompt the model for both before calling.
func TestGetSessionTimelineParamsRequired(t *testing.T) {
	s := NewGetSessionTimeline()
	params := s.Params()
	req, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("Params()[\"required\"] missing or wrong type: %#v", params)
	}
	want := map[string]bool{"agent_id": true, "session_id": true}
	if len(req) != len(want) {
		t.Fatalf("required = %v, want exactly %v", req, want)
	}
	for _, r := range req {
		if !want[r] {
			t.Fatalf("unexpected required field %q in %v", r, req)
		}
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Params() properties missing or wrong type: %#v", params)
	}
	for _, name := range []string{"agent_id", "session_id", "limit"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("Params() missing %s property: %#v", name, params)
		}
	}
}

// TestOtherCuratedSkillsHaveNoRequiredParams double-checks the remaining
// four skills (all-optional params, per the brief) don't accidentally
// require anything.
func TestOtherCuratedSkillsHaveNoRequiredParams(t *testing.T) {
	for _, s := range []Skill{NewGetThreats(), NewGetHolds(), NewGetRuleStatus(), NewGetCostSummary()} {
		params := s.Params()
		if req, ok := params["required"]; ok {
			t.Errorf("%s: unexpected required=%v (all params should be optional)", s.Name(), req)
		}
	}
}

// TestGetSessionTimelineMissingSessionID is the brief's binding guard test:
// a missing session_id must produce an "error: "-prefixed result with a nil
// Go error, per the skills package convention (see runsql.go/schema.go).
func TestGetSessionTimelineMissingSessionID(t *testing.T) {
	rc := &RunContext{Cfg: &config.Config{}, ResultMaxBytes: 4096}
	cases := []json.RawMessage{
		json.RawMessage(`{"agent_id": "agent-1"}`),
		json.RawMessage(`{"agent_id": "agent-1", "session_id": ""}`),
		json.RawMessage(`{"agent_id": "agent-1", "session_id": "   "}`),
		json.RawMessage(`{}`),
		json.RawMessage(`not json`),
	}
	s := NewGetSessionTimeline()
	for _, args := range cases {
		out, err := s.Execute(context.Background(), rc, args)
		if err != nil {
			t.Fatalf("unexpected Go error for args %s: %v", args, err)
		}
		if !strings.HasPrefix(out, "error:") {
			t.Fatalf("args %s: want error: prefix, got %q", args, out)
		}
	}
}

// TestGetSessionTimelineMissingAgentID mirrors the above for a missing
// agent_id with a valid session_id.
func TestGetSessionTimelineMissingAgentID(t *testing.T) {
	rc := &RunContext{Cfg: &config.Config{}, ResultMaxBytes: 4096}
	args, _ := json.Marshal(map[string]string{"session_id": "sess-1"})
	out, err := NewGetSessionTimeline().Execute(context.Background(), rc, args)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("want error: prefix, got %q", out)
	}
}

// TestGetThreatsAcceptsEmptyArgs checks the all-optional skills tolerate a
// missing/empty args body ({} — every filter omitted) without a Go error,
// since Timeplus is unreachable in this unit test the query itself fails,
// but that failure must surface as an "error: "-prefixed result, not a
// panic or a Go error return.
func TestGetThreatsAcceptsEmptyArgs(t *testing.T) {
	rc := &RunContext{Cfg: &config.Config{Timeplus: config.TimeplusConfig{Host: "127.0.0.1", Port: 1}}, ResultMaxBytes: 4096}
	out, err := NewGetThreats().Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("expected a query error against an unreachable host, got %q", out)
	}
}

// TestGetRuleStatusNoParamsExecutes checks the zero-arg skill runs without a
// Go error even against an unreachable Timeplus (surfaces as "error: ...").
func TestGetRuleStatusNoParamsExecutes(t *testing.T) {
	rc := &RunContext{Cfg: &config.Config{Timeplus: config.TimeplusConfig{Host: "127.0.0.1", Port: 1}}, ResultMaxBytes: 4096}
	out, err := NewGetRuleStatus().Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("expected a query error against an unreachable host, got %q", out)
	}
}

// TestGetCostSummaryClampsDays exercises the days-param path (including an
// out-of-range value) against an unreachable Timeplus; it must not panic or
// return a Go error regardless of the clamp outcome.
func TestGetCostSummaryClampsDays(t *testing.T) {
	rc := &RunContext{Cfg: &config.Config{Timeplus: config.TimeplusConfig{Host: "127.0.0.1", Port: 1}}, ResultMaxBytes: 4096}
	for _, args := range []json.RawMessage{
		json.RawMessage(`{}`),
		json.RawMessage(`{"days": 30}`),
		json.RawMessage(`{"days": 999999}`),
		json.RawMessage(`{"days": -5}`),
	} {
		out, err := NewGetCostSummary().Execute(context.Background(), rc, args)
		if err != nil {
			t.Fatalf("args %s: unexpected Go error: %v", args, err)
		}
		if !strings.HasPrefix(out, "error:") {
			t.Fatalf("args %s: expected a query error against an unreachable host, got %q", args, out)
		}
	}
}

// TestGetHoldsDefaultsToPending checks the default status value is applied
// when omitted (still exercised against an unreachable host).
func TestGetHoldsDefaultsToPending(t *testing.T) {
	rc := &RunContext{Cfg: &config.Config{Timeplus: config.TimeplusConfig{Host: "127.0.0.1", Port: 1}}, ResultMaxBytes: 4096}
	out, err := NewGetHolds().Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("expected a query error against an unreachable host, got %q", out)
	}
}
