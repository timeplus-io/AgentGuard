package handlers

import (
	"encoding/json"
	"testing"

	"github.com/timeplus/agentguard/internal/models"
)

// The notify watcher publishes with a stable identity id so repeated
// deliveries of the same threat dedup client-side (multi-tab, backfill).
func TestPublishWithIDUsesCallerID(t *testing.T) {
	b := &alertBroadcaster{clients: make(map[chan []byte]struct{})}
	ch, cancel := b.subscribe()
	defer cancel()

	b.PublishWithID("dep|agent|sess|rp-001", models.AlertWebhookPayload{
		RuleID:    "rp-001",
		SessionID: "sess",
	})

	var env map[string]any
	if err := json.Unmarshal(<-ch, &env); err != nil {
		t.Fatal(err)
	}
	if env["id"] != "dep|agent|sess|rp-001" {
		t.Errorf("id = %v, want the caller-supplied identity key", env["id"])
	}
	if env["session_id"] != "sess" {
		t.Errorf("session_id = %v, want sess (payload must carry the session)", env["session_id"])
	}
}

// Publish (used by holds) must keep generating a fresh unique id per call.
func TestPublishGeneratesUniqueID(t *testing.T) {
	b := &alertBroadcaster{clients: make(map[chan []byte]struct{})}
	ch, cancel := b.subscribe()
	defer cancel()

	b.Publish(models.AlertWebhookPayload{RuleName: "hold_pending"})
	b.Publish(models.AlertWebhookPayload{RuleName: "hold_pending"})

	var a, bb map[string]any
	_ = json.Unmarshal(<-ch, &a)
	_ = json.Unmarshal(<-ch, &bb)
	if a["id"] == "" || a["id"] == bb["id"] {
		t.Errorf("Publish ids must be unique and non-empty, got %v / %v", a["id"], bb["id"])
	}
}
