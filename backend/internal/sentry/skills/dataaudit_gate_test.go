package skills

import (
	"context"
	"strings"
	"testing"
)

// TestDataAuditGuideRespectsHiddenList exercises the generic hidden-list
// mechanism: a skill is loadable when not hidden and neither listed nor loadable
// when explicitly hidden. Note sentry.go does NOT gate data-audit-guide behind
// agentguard_tools_enabled (only agentguard-guide is): data-audit-guide is a
// self-contained run_sql playbook, so it stays available regardless of that
// switch. This test covers the mechanism, not that wiring.
func TestDataAuditGuideRespectsHiddenList(t *testing.T) {
	// Visible when not hidden.
	rc := &RunContext{Loaded: map[string]bool{}}
	out, err := NewLoadSkill("").Execute(context.Background(), rc,
		loadArgs(t, map[string]string{"name": "data-audit-guide"}))
	if err != nil || strings.Contains(out, "unknown skill") {
		t.Fatalf("data-audit-guide should load when not hidden: %v / %.80q", err, out)
	}

	// Hidden when listed: not loadable...
	hidden := NewLoadSkill("", "agentguard-guide", "data-audit-guide")
	rc2 := &RunContext{Loaded: map[string]bool{}}
	hiddenOut, _ := hidden.Execute(context.Background(), rc2,
		loadArgs(t, map[string]string{"name": "data-audit-guide"}))
	if !strings.Contains(hiddenOut, "unknown skill") {
		t.Fatalf("data-audit-guide must be unloadable when hidden, got %.80q", hiddenOut)
	}
	// ...and not advertised in the tool description.
	if strings.Contains(hidden.Description(), "data-audit-guide") {
		t.Error("hidden skill must not appear in load_skill description")
	}
}
