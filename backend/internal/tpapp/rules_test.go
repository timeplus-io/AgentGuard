package tpapp

import (
	"strings"
	"testing"
)

func TestRuleArtifacts_ShapeAndOrder(t *testing.T) {
	names := []string{"agentguard_cim_event", "agentguard_security_events"}
	got, err := RuleArtifacts(names)
	if err != nil {
		t.Fatal(err)
	}
	var mvs, pauses, udfs int
	for i, r := range got {
		switch r.Kind {
		case "materialized_view":
			mvs++
			if !strings.HasPrefix(r.Name, "mv_rule_rp00") {
				t.Errorf("unexpected MV name %q", r.Name)
			}
			wantHead := "CREATE MATERIALIZED VIEW IF NOT EXISTS {{ .DB }}." + r.Name + "\nINTO {{ .DB }}.agentguard_security_events\nAS "
			if !strings.HasPrefix(r.SQL, wantHead) {
				t.Errorf("%s: head is %.120q", r.Name, r.SQL)
			}
			if strings.Contains(r.SQL, "FROM agentguard_cim_event") {
				t.Errorf("%s: unqualified source stream", r.Name)
			}
			if !strings.Contains(r.SQL, "FROM {{ .DB }}.agentguard_cim_event") {
				t.Errorf("%s: expected qualified FROM", r.Name)
			}
			// the very next artifact must pause this MV
			if i+1 >= len(got) || got[i+1].Kind != "system" || got[i+1].Name != "pause_"+r.Name ||
				got[i+1].SQL != "SYSTEM PAUSE MATERIALIZED VIEW {{ .DB }}."+r.Name {
				t.Errorf("%s is not followed by its pause resource", r.Name)
			}
		case "system":
			pauses++
		case "udf":
			udfs++
			if strings.Contains(r.SQL, "{{ .DB }}") {
				t.Errorf("udf %s must not be database-qualified", r.Name)
			}
			if !strings.HasPrefix(r.SQL, "CREATE OR REPLACE FUNCTION "+r.Name) {
				t.Errorf("udf %s: head %.80q", r.Name, r.SQL)
			}
			// its rule's MV comes later and calls it
			found := false
			for _, later := range got[i+1:] {
				if later.Kind == "materialized_view" && strings.Contains(later.SQL, r.Name+"(") {
					found = true
				}
			}
			if !found {
				t.Errorf("udf %s is not used by a later MV", r.Name)
			}
		default:
			t.Errorf("unexpected kind %q", r.Kind)
		}
		if strings.HasSuffix(strings.TrimSpace(r.SQL), ";") {
			t.Errorf("%s ends with ';'", r.Name)
		}
	}
	if mvs != 4 || pauses != 4 {
		t.Errorf("want 4 MVs + 4 pauses, got %d + %d", mvs, pauses)
	}
	if udfs < 1 {
		t.Error("rp-002 ships ag_dlp_jwt_check; expected at least one udf artifact")
	}
	if got[0].Name != "mv_rule_rp001" {
		t.Errorf("first artifact should be mv_rule_rp001, got %s", got[0].Name)
	}
}
