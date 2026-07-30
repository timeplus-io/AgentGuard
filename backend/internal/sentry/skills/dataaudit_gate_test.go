package skills

import (
	"context"
	"strings"
	"testing"
)

// TestDataAuditGuideRespectsHiddenList asserts data-audit-guide is loadable
// when not hidden and neither listed nor loadable when hidden — the same
// contract agentguard-guide relies on. sentry.go appends "data-audit-guide"
// to the hidden set on the same branch as "agentguard-guide" whenever
// AgentGuard tools are disabled.
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
