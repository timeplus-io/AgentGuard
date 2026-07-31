package notify

import (
	"strings"
	"testing"
)

// The watcher's identifiers come from the event stream (ultimately from agent
// telemetry), so the seen-state SQL must use the backslash-aware escaper —
// same reasoning as the retired threatOpenCheckSQL webhook test.
func TestNotifyStoreSQLEscapesBackslashQuoteBypass(t *testing.T) {
	e := Event{
		AgentID:      `\' OR 1=1 --`,
		DeploymentID: "d",
		SessionID:    "s",
		RuleID:       "r",
		RuleName:     "n",
		Severity:     "critical",
		Message:      `it's a \' test`,
		DetectedAt:   "2026-07-30 10:00:00.000",
	}
	for name, sql := range map[string]string{
		"get":  getStateSQL(e),
		"mark": markNotifiedSQL(e),
	} {
		if strings.Contains(sql, `\' OR`) {
			t.Errorf("%s: raw \\' escape sequence survived into SQL: %s", name, sql)
		}
		if !strings.Contains(sql, `\\''`) {
			t.Errorf("%s: payload not backslash-escaped: %s", name, sql)
		}
	}
}

func TestMarkNotifiedSQLWritesUnconfirmed(t *testing.T) {
	e := Event{AgentID: "a", DeploymentID: "d", SessionID: "s", RuleID: "r", DetectedAt: "2026-07-30 10:00:00.000"}
	sql := markNotifiedSQL(e)
	for _, want := range []string{"INSERT INTO agentguard_notifications", "now64(3)", "false", "1970-01-01 00:00:00.000"} {
		if !strings.Contains(sql, want) {
			t.Errorf("markNotifiedSQL missing %q: %s", want, sql)
		}
	}
}
