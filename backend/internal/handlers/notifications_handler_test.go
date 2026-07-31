package handlers

import (
	"strings"
	"testing"
)

// The confirm endpoint takes browser-supplied identifiers into a SQL literal,
// so the escaping must be backslash-aware (Proton reads \' as an escaped
// quote) — same invariant the retired webhook's threatOpenCheckSQL pinned.
func TestConfirmNotificationSQL_EscapesBackslashQuoteBypass(t *testing.T) {
	payload := `\' OR 1=1 --`
	sql := confirmNotificationSQL(notificationIdentity{
		AgentID:      payload,
		DeploymentID: payload,
		SessionID:    payload,
		RuleID:       payload,
	})
	if !strings.Contains(sql, `\\''`) {
		t.Fatalf("payload not backslash-escaped: %s", sql)
	}
	if strings.Contains(sql, `\' OR`) {
		t.Fatalf("raw \\' escape sequence survived into SQL: %s", sql)
	}
	for _, want := range []string{"INSERT INTO agentguard_notifications", "true, now64(3)"} {
		if !strings.Contains(sql, want) {
			t.Errorf("confirm SQL missing %q: %s", want, sql)
		}
	}
}

func TestUnconfirmedNotificationsSQLShape(t *testing.T) {
	for _, want := range []string{
		"FROM table(agentguard_notifications)",
		"WHERE confirmed = false",
		"ORDER BY notified_at DESC",
		"LIMIT 20",
	} {
		if !strings.Contains(unconfirmedNotificationsSQL, want) {
			t.Errorf("unconfirmed SQL missing %q", want)
		}
	}
}
