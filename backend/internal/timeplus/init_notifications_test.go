package timeplus

import (
	"strings"
	"testing"
)

// Pins the seen-state schema (#258): threat-identity PK, coalesced upserts,
// and a separate cf_confirm family so the confirm endpoint can write
// (confirmed, confirmed_at) without knowing the event columns.
func TestNotificationsStreamDDL(t *testing.T) {
	for _, want := range []string{
		"CREATE MUTABLE STREAM IF NOT EXISTS agentguard_notifications",
		"PRIMARY KEY (agent_id, deployment_id, session_id, rule_id)",
		"FAMILY cf_event(rule_name, severity, message, detected_at, notified_at)",
		"FAMILY cf_confirm(confirmed, confirmed_at)",
		"coalesced = true",
	} {
		if !strings.Contains(ddlNotificationsStream, want) {
			t.Errorf("ddlNotificationsStream missing %q", want)
		}
	}
}

// The legacy Timeplus→AgentGuard callback objects must be dropped on
// provision — the alert first (it references the UDF).
func TestLegacyNotifierTeardownDDL(t *testing.T) {
	if len(legacyNotifierDrops) != 2 {
		t.Fatalf("legacyNotifierDrops = %v, want exactly the alert + UDF drops", legacyNotifierDrops)
	}
	if !strings.Contains(legacyNotifierDrops[0], "DROP ALERT IF EXISTS agentguard_notifier") {
		t.Errorf("first drop must be the alert, got %q", legacyNotifierDrops[0])
	}
	if !strings.Contains(legacyNotifierDrops[1], "DROP FUNCTION IF EXISTS notify_agentguard_udf") {
		t.Errorf("second drop must be the UDF, got %q", legacyNotifierDrops[1])
	}
}
