package handlers

import (
	"strings"
	"testing"
)

func TestSplitAgentID(t *testing.T) {
	cases := []struct {
		name              string
		in                string
		wantDeploy        string
		wantAgent         string
		wantOk            bool
	}{
		{"valid", "local__alice", "local", "alice", true},
		{"valid with hyphens", "prod-east__agent-01", "prod-east", "agent-01", true},
		{"valid with underscores", "prod_env__agent_01", "prod_env", "agent_01", true},
		{"missing separator", "localalice", "", "", false},
		{"only separator", "__", "", "", false},
		{"empty deployment", "__alice", "", "", false},
		{"empty agent", "local__", "", "", false},
		{"deployment has invalid char", "local!__alice", "", "", false},
		{"agent has slash (path traversal attempt)", "local__../etc", "", "", false},
		{"agent has SQL quote (injection attempt)", "local__a'b", "", "", false},
		{"empty input", "", "", "", false},
		// SplitN(_, _, 2) splits on the first "__" only — anything after stays
		// inside the agent half. idPartRe permits underscores so this is accepted.
		// Documents the real behavior: agent_id may itself contain "__".
		{"three segments — only first __ splits", "a__b__c", "a", "b__c", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, a, ok := splitAgentID(c.in)
			if ok != c.wantOk {
				t.Errorf("ok = %v, want %v", ok, c.wantOk)
			}
			if d != c.wantDeploy {
				t.Errorf("deployment = %q, want %q", d, c.wantDeploy)
			}
			if a != c.wantAgent {
				t.Errorf("agent = %q, want %q", a, c.wantAgent)
			}
		})
	}
}

func TestParseRFC3339(t *testing.T) {
	t.Run("empty returns empty no-error", func(t *testing.T) {
		got, err := parseRFC3339("")
		if err != nil || got != "" {
			t.Errorf("parseRFC3339(\"\") = (%q, %v), want (\"\", nil)", got, err)
		}
	})

	t.Run("UTC RFC3339 normalizes to Proton format", func(t *testing.T) {
		got, err := parseRFC3339("2026-04-08T03:25:17Z")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "2026-04-08 03:25:17"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("offset RFC3339 converts to UTC", func(t *testing.T) {
		got, err := parseRFC3339("2026-04-08T05:25:17+02:00")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// 05:25:17 +02:00 → 03:25:17 UTC
		want := "2026-04-08 03:25:17"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("invalid string returns error", func(t *testing.T) {
		_, err := parseRFC3339("not a date")
		if err == nil {
			t.Error("expected error for invalid date")
		}
	})

	t.Run("missing timezone is invalid for RFC3339", func(t *testing.T) {
		_, err := parseRFC3339("2026-04-08 03:25:17")
		if err == nil {
			t.Error("expected error for missing TZ (RFC3339 requires it)")
		}
	})
}

func TestSessionType(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{"main → Web UI", "openclaw:v1:main:abc", "Web UI"},
		{"openai-user → HTTP API", "openclaw:v1:openai-user:abc", "HTTP API"},
		{"cron → Cron", "openclaw:v1:cron:abc", "Cron"},
		{"subagent → Sub-agent", "openclaw:v1:subagent:abc", "Sub-agent"},
		{"unknown type label", "openclaw:v1:unrecognized:abc", "Unknown"},
		{"empty key", "", "Unknown"},
		{"too few segments", "openclaw:v1", "Unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sessionType(c.key); got != c.want {
				t.Errorf("sessionType(%q) = %q, want %q", c.key, got, c.want)
			}
		})
	}
}

func TestBuildTimeWhere(t *testing.T) {
	t.Run("both empty", func(t *testing.T) {
		if got := buildTimeWhere("", ""); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("only since", func(t *testing.T) {
		got := buildTimeWhere("2026-04-08 00:00:00", "")
		if !strings.Contains(got, "AND event_time >= '2026-04-08 00:00:00'") {
			t.Errorf("missing since clause in: %s", got)
		}
		if strings.Contains(got, "<=") {
			t.Errorf("until clause should be absent: %s", got)
		}
	})

	t.Run("only until", func(t *testing.T) {
		got := buildTimeWhere("", "2026-04-08 23:59:59")
		if !strings.Contains(got, "AND event_time <= '2026-04-08 23:59:59'") {
			t.Errorf("missing until clause in: %s", got)
		}
		if strings.Contains(got, ">=") {
			t.Errorf("since clause should be absent: %s", got)
		}
	})

	t.Run("both", func(t *testing.T) {
		got := buildTimeWhere("2026-04-08 00:00:00", "2026-04-08 23:59:59")
		if !strings.Contains(got, ">=") || !strings.Contains(got, "<=") {
			t.Errorf("missing both clauses: %s", got)
		}
	})
}
