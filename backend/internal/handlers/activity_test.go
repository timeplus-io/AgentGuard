package handlers

import (
	"fmt"
	"testing"
	"time"

	"github.com/timeplus/agentguard/internal/models"
)

func TestHoldRowToActivityEvent_BasicFields(t *testing.T) {
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	row := map[string]interface{}{
		"hold_id":        "test-hold-1",
		"deployment_id":  "dep-1",
		"agent_id":       "agent-1",
		"session_id":     "sess-1",
		"run_id":         "run-1",
		"tool_name":      "bash",
		"rule_id":        "rp_001",
		"rule_name":      "Dangerous Shell Commands",
		"severity":       "critical",
		"args_summary":   `{"command":"rm -rf /"}`,
		"message":        "dangerous command detected",
		"created_at":     now,
		"status":         "pending",
		"decided_by":     "",
		"decided_at":     time.Time{},
		"decided_reason": "",
	}

	ev := holdRowToActivityEvent(row)

	if ev.ID != "test-hold-1" {
		t.Fatalf("ID: got %q, want %q", ev.ID, "test-hold-1")
	}
	if ev.ToolName != "bash" {
		t.Fatalf("ToolName: got %q, want %q", ev.ToolName, "bash")
	}
	if ev.Status != "pending" {
		t.Fatalf("Status: got %q, want %q", ev.Status, "pending")
	}
	if ev.RuleName != "Dangerous Shell Commands" {
		t.Fatalf("RuleName: got %q, want %q", ev.RuleName, "Dangerous Shell Commands")
	}
	if !ev.IsHoldEvent {
		t.Fatal("IsHoldEvent: want true for hold row")
	}
	if ev.DecidedAt != nil {
		t.Fatal("DecidedAt: want nil for zero time")
	}
}

func TestHoldRowToActivityEvent_DecidedAt_Populated(t *testing.T) {
	decided := time.Date(2026, 4, 29, 11, 0, 0, 0, time.UTC)
	row := map[string]interface{}{
		"hold_id":        "h2",
		"deployment_id":  "", "agent_id": "", "session_id": "", "run_id": "",
		"tool_name":      "read_file",
		"rule_id":        "", "rule_name": "", "severity": "",
		"args_summary":   "", "message":   "",
		"created_at":     time.Time{},
		"status":         "approved",
		"decided_by":     "web-ui",
		"decided_at":     decided,
		"decided_reason": "looks safe",
	}

	ev := holdRowToActivityEvent(row)

	if ev.DecidedAt == nil {
		t.Fatal("DecidedAt: want non-nil for non-zero time")
	}
	if !ev.DecidedAt.Equal(decided) {
		t.Fatalf("DecidedAt: got %v, want %v", *ev.DecidedAt, decided)
	}
	if ev.DecidedReason != "looks safe" {
		t.Fatalf("DecidedReason: got %q, want %q", ev.DecidedReason, "looks safe")
	}
}

func TestSyntheticActivityID(t *testing.T) {
	id1 := syntheticActivityID("sess-1", "2026-04-29T10:00:00Z", "bash")
	id2 := syntheticActivityID("sess-1", "2026-04-29T10:00:00Z", "bash")
	id3 := syntheticActivityID("sess-1", "2026-04-29T10:00:01Z", "bash")

	if id1 != id2 {
		t.Fatal("same inputs must produce same id")
	}
	if id1 == id3 {
		t.Fatal("different event_time must produce different id")
	}
	if len(id1) != 16 {
		t.Fatalf("expected 16 hex chars, got %d", len(id1))
	}
}

func TestMergeAndSortActivityEvents_OrderedByEventTime(t *testing.T) {
	t1 := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 29, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	holds := []models.ActivityEvent{{ID: "h1", EventTime: t2}}
	allowed := []models.ActivityEvent{{ID: "a1", EventTime: t3}, {ID: "a2", EventTime: t1}}

	merged := mergeAndSortActivityEvents(holds, allowed, 10)

	if len(merged) != 3 {
		t.Fatalf("got %d events, want 3", len(merged))
	}
	if merged[0].ID != "a1" {
		t.Fatalf("first (newest): got %q, want a1", merged[0].ID)
	}
	if merged[1].ID != "h1" {
		t.Fatalf("second: got %q, want h1", merged[1].ID)
	}
	if merged[2].ID != "a2" {
		t.Fatalf("third (oldest): got %q, want a2", merged[2].ID)
	}
}

func TestMergeAndSortActivityEvents_RespectsLimit(t *testing.T) {
	evs := make([]models.ActivityEvent, 5)
	for i := range evs {
		evs[i] = models.ActivityEvent{ID: fmt.Sprintf("e%d", i), EventTime: time.Now()}
	}
	merged := mergeAndSortActivityEvents(evs, nil, 3)
	if len(merged) != 3 {
		t.Fatalf("got %d, want 3", len(merged))
	}
}
