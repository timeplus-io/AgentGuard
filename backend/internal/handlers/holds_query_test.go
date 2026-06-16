package handlers

import (
	"strings"
	"testing"
)

// Behavior contract for buildPreExistingOpenThreatsSQL.
//
// The blocking decision must read from agentguard_threats (the mutable dedup state),
// NOT agentguard_security_events (the append-only audit log). Reading from
// security_events ignores cleared/acknowledged status updates and produces
// "Pre-existing open threat" blocks for threats the operator has cleared —
// see https://github.com/timeplus-io/AgentGuard/issues/118.
//
// The predicate must accept a row as effectively open when:
//   - status is empty (never operator-touched), OR
//   - status is 'open' (auto-promoted via the threats list reopen path —
//     threats.go writes status='open' with status_updated_at=now() when it
//     sees last_seen > status_updated_at), OR
//   - status is 'acknowledged' or 'cleared' AND a new detection arrived
//     after the operator action (cf_events.last_seen > cf_status.status_updated_at).
//     This second branch handles the case where mv_threats has advanced
//     last_seen but the threats-list endpoint has not run the auto-promote yet.
func TestBuildPreExistingOpenThreatsSQL(t *testing.T) {
	sql := buildPreExistingOpenThreatsSQL("dep-A", "agent-B", "sess-C")

	must := []struct {
		fragment string
		why      string
	}{
		{"table(agentguard_threats)", "must read from threats stream, not security_events"},
		{"'open' AS reported_status", "synthetic open marker must use unique alias to avoid colliding with t.status in WHERE"},
		{"table(agentguard_rules)", "must still honor enabled/deleted via rules join"},
		{"arg_max(block_policy, revision)", "current rule state must order by monotonic revision, not updated_at"},
		{"revision) AS enabled", "enabled state must be reduced by revision"},
		{"r.enabled", "must filter out disabled rules"},
		{"NOT r.deleted", "must filter out deleted rules"},
		{"t.status = ''", "first branch: never-touched threats are open"},
		{"t.status = 'open'", "second branch: explicitly auto-reopened threats stay blocking"},
		{"t.last_seen > t.status_updated_at", "third branch: cleared/acked with new detection re-opens"},
		{"'acknowledged'", "third branch covers acknowledged"},
		{"'cleared'", "third branch covers cleared"},
		{"'dep-A'", "deployment_id parameter"},
		{"'agent-B'", "agent_id parameter"},
		{"'sess-C'", "session_id parameter"},
	}
	for _, m := range must {
		if !strings.Contains(sql, m.fragment) {
			t.Errorf("missing %q (%s)\nSQL:\n%s", m.fragment, m.why, sql)
		}
	}

	mustNot := []struct {
		fragment string
		why      string
	}{
		{"FROM table(agentguard_security_events)", "the bug: reading detections directly bypasses cleared status"},
		{"updated_at) AS enabled", "rule state must not be ordered by wall-clock updated_at (revision is the invariant)"},
	}
	for _, m := range mustNot {
		if strings.Contains(sql, m.fragment) {
			t.Errorf("found forbidden %q (%s)\nSQL:\n%s", m.fragment, m.why, sql)
		}
	}
}

// SQL identity inputs must pass through escapeSQLStr to neutralize injection.
func TestBuildPreExistingOpenThreatsSQL_EscapesQuotes(t *testing.T) {
	sql := buildPreExistingOpenThreatsSQL("dep'A", "agent'B", "sess'C")
	for _, want := range []string{"'dep''A'", "'agent''B'", "'sess''C'"} {
		if !strings.Contains(sql, want) {
			t.Errorf("expected escaped %q in SQL\nSQL:\n%s", want, sql)
		}
	}
}
