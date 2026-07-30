package skills

import (
	"context"
	"strings"
	"testing"
)

// TestDataAuditGuideLoads asserts the data-audit-guide SKILL.md is embedded and
// mentions the customer audit-rule frame (A01 / A27) so the model gets the
// catalog when it loads the skill.
func TestDataAuditGuideLoads(t *testing.T) {
	rc := &RunContext{Loaded: map[string]bool{}}
	out, err := NewLoadSkill("").Execute(context.Background(), rc, loadArgs(t, map[string]string{"name": "data-audit-guide"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"A01", "A27", "investigate"} {
		if !strings.Contains(out, want) {
			t.Errorf("SKILL.md missing %q; got %.120q", want, out)
		}
	}
}

// TestDataAuditGuideInSkillList asserts the skill is discoverable by name.
func TestDataAuditGuideInSkillList(t *testing.T) {
	found := false
	for _, n := range skillNames("", nil) {
		if n == "data-audit-guide" {
			found = true
		}
	}
	if !found {
		t.Fatal("data-audit-guide not in embedded skill list")
	}
}

// TestDataAuditGuideReferencesLoad asserts each category reference resolves and
// mentions its lead A-code, so the SKILL.md links are never dangling.
func TestDataAuditGuideReferencesLoad(t *testing.T) {
	cases := map[string]string{
		"SENSITIVE_DATA.md": "A01",
		"PERMISSIONS.md":    "A08",
		"EXFIL.md":          "A13",
		"ATTACK.md":         "A19",
		"AUDIT_TRAIL.md":    "A24",
	}
	for file, want := range cases {
		rc := &RunContext{Loaded: map[string]bool{}}
		out, err := NewLoadSkillReference("").Execute(context.Background(), rc,
			loadArgs(t, map[string]string{"skill": "data-audit-guide", "name": file}))
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if !strings.Contains(out, want) {
			t.Errorf("%s missing %q; got %.100q", file, want, out)
		}
	}
}
