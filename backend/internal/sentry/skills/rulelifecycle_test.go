package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// recordingFns returns a RuleLifecycleFuncs whose calls are appended to log.
func recordingFns(log *[]string) RuleLifecycleFuncs {
	rec := func(parts ...string) (string, error) {
		*log = append(*log, strings.Join(parts, "|"))
		return "ok", nil
	}
	return RuleLifecycleFuncs{
		Install: func(id string, ov map[string]any, user, reason string) (string, error) {
			b, _ := json.Marshal(ov)
			return rec("install", id, string(b), user, reason)
		},
		Configure: func(id string, ov map[string]any, user, reason string) (string, error) {
			b, _ := json.Marshal(ov)
			return rec("configure", id, string(b), user, reason)
		},
		SetEnabled: func(id string, enabled bool, user string) (string, error) {
			state := "off"
			if enabled {
				state = "on"
			}
			return rec("set_enabled", id, state, user)
		},
		SetPolicy: func(id, policy, user, reason string) (string, error) {
			return rec("set_policy", id, policy, user, reason)
		},
		Upgrade:   func(id, user string) (string, error) { return rec("upgrade", id, user) },
		Uninstall: func(id, user string) (string, error) { return rec("uninstall", id, user) },
		Sync:      func(user string) (string, error) { return rec("sync", user) },
	}
}

func execByName(t *testing.T, sks []Skill, name string, rc *RunContext, args string) string {
	t.Helper()
	for _, sk := range sks {
		if sk.Name() == name {
			if sk.Access() != GatedWrite {
				t.Fatalf("%s must be GatedWrite", name)
			}
			out, err := sk.Execute(context.Background(), rc, json.RawMessage(args))
			if err != nil {
				t.Fatalf("%s: unexpected Go error: %v", name, err)
			}
			return out
		}
	}
	t.Fatalf("skill %s not found", name)
	return ""
}

func TestRuleLifecycleSkillsDispatch(t *testing.T) {
	var log []string
	sks := NewRuleLifecycleSkills(recordingFns(&log))
	if len(sks) != 7 {
		t.Fatalf("got %d skills, want 7", len(sks))
	}
	rc := &RunContext{User: "alice"}

	execByName(t, sks, "install_rule", rc, `{"rule_id":"rp001","overrides":{"threshold":5},"reason":"tighten"}`)
	execByName(t, sks, "configure_rule", rc, `{"rule_id":"rp001","overrides":{"threshold":9}}`)
	execByName(t, sks, "set_rule_enabled", rc, `{"rule_id":"rp001","enabled":false}`)
	execByName(t, sks, "set_rule_policy", rc, `{"rule_id":"rp001","block_policy":"hold"}`)
	execByName(t, sks, "upgrade_rule", rc, `{"rule_id":"rp001"}`)
	execByName(t, sks, "uninstall_rule", rc, `{"rule_id":"rp001"}`)
	execByName(t, sks, "sync_rules", rc, `{}`)

	want := []string{
		`install|rp001|{"threshold":5}|alice|tighten`,
		`configure|rp001|{"threshold":9}|alice|configure via sentry`,
		"set_enabled|rp001|off|alice",
		"set_policy|rp001|hold|alice|set policy via sentry",
		"upgrade|rp001|alice",
		"uninstall|rp001|alice",
		"sync|alice",
	}
	if len(log) != len(want) {
		t.Fatalf("calls = %v", log)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, log[i], want[i])
		}
	}
}

func TestRuleLifecycleSkillsValidation(t *testing.T) {
	var log []string
	sks := NewRuleLifecycleSkills(recordingFns(&log))
	rc := &RunContext{User: "alice"}

	cases := []struct{ name, args string }{
		{"install_rule", `{}`},                                              // missing rule_id
		{"set_rule_enabled", `{"rule_id":"rp001"}`},                         // missing enabled
		{"set_rule_policy", `{"rule_id":"rp001","block_policy":"nuke"}`},    // invalid policy
		{"set_rule_policy", `{"rule_id":"rp001"}`},                         // missing policy
		{"uninstall_rule", `{"rule_id":"  "}`},                             // blank rule_id
	}
	for _, c := range cases {
		out := execByName(t, sks, c.name, rc, c.args)
		if !strings.HasPrefix(out, "error:") {
			t.Errorf("%s %s: expected validation error, got %q", c.name, c.args, out)
		}
	}
	if len(log) != 0 {
		t.Fatalf("invalid args must never reach the write functions: %v", log)
	}

	// Attribution never empty.
	execByName(t, sks, "sync_rules", &RunContext{}, `{}`)
	if len(log) != 1 || log[0] != "sync|sentry" {
		t.Fatalf("empty user must fall back to 'sentry': %v", log)
	}
}
