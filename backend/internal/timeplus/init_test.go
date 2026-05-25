package timeplus

import (
	"strings"
	"testing"
)

// Regression test for https://github.com/timeplus-io/AgentGuard/issues/150
//
// Claude Code's SessionEnd hook is captured by the plugin and written to
// agentguard_hook_events with hook_name='session_end', but the CIM
// normalization MV silently drops it because:
//   1. The multi_if mapping has no case for hook_name = 'session_end'.
//   2. The WHERE hook_name IN (...) whitelist omits 'session_end'.
//
// Both issues must be fixed together; either alone leaves the event invisible
// in agentguard_cim_event.

func TestCIMClaudeCodeMV_MapsSessionEndInMultiIf(t *testing.T) {
	const want = `hook_name = 'session_end'`
	if !strings.Contains(ddlCIMClaudeCodeMV, want) {
		t.Errorf(
			"ddlCIMClaudeCodeMV is missing the multi_if mapping for %s.\n"+
				"Without it, Claude Code SessionEnd events become event_type='other'.\n"+
				"See issue #150.",
			want,
		)
	}
}

func TestCIMClaudeCodeMV_WhitelistsSessionEnd(t *testing.T) {
	const filterStart = "hook_name IN ("
	inIdx := strings.Index(ddlCIMClaudeCodeMV, filterStart)
	if inIdx == -1 {
		t.Fatalf("ddlCIMClaudeCodeMV does not contain a %q filter clause", filterStart)
	}
	rest := ddlCIMClaudeCodeMV[inIdx+len(filterStart):]
	closeIdx := strings.Index(rest, ")")
	if closeIdx == -1 {
		t.Fatalf("ddlCIMClaudeCodeMV has an unterminated %q filter", filterStart)
	}
	inClause := rest[:closeIdx]
	if !strings.Contains(inClause, `'session_end'`) {
		t.Errorf(
			"ddlCIMClaudeCodeMV's WHERE %s...) clause does not include 'session_end'.\n"+
				"Without it, the event is filtered out before multi_if even evaluates.\n"+
				"Current IN list:\n%s\nSee issue #150.",
			filterStart, inClause,
		)
	}
}
