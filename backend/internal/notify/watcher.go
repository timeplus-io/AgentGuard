// Package notify implements the AgentGuard-side replacement (#258) for the
// old Timeplus→AgentGuard alert callback: a long-lived streaming subscription
// on agentguard_notify_events that dedups against the persisted
// agentguard_notifications seen-state and publishes new-threat notifications
// to the in-process SSE broadcaster.
package notify

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// Event is one row of agentguard_notify_events (already filtered by
// mv_notify_filter to threats that are new or previously cleared).
// datetime64 columns arrive as fixed-width text, e.g. "2026-07-30 12:34:56.789".
type Event struct {
	DetectedAt   string `json:"detected_at"`
	RuleID       string `json:"rule_id"`
	RuleName     string `json:"rule_name"`
	Severity     string `json:"severity"`
	AgentID      string `json:"agent_id"`
	DeploymentID string `json:"deployment_id"`
	SessionID    string `json:"session_id"`
	HookName     string `json:"hook_name"`
	Message      string `json:"message"`
}

// State is the persisted seen-state for one threat identity.
type State struct {
	Confirmed   bool
	ConfirmedAt string // datetime64(3) text; zero-state is the 1970 epoch
}

// Store persists per-threat notification state.
type Store interface {
	// Get returns the current seen-state, or nil when never notified.
	Get(e Event) (*State, error)
	// MarkNotified upserts the row as notified-and-unconfirmed.
	MarkNotified(e Event) error
}

// Publisher is the subset of the SSE broadcaster the watcher needs.
type Publisher interface {
	PublishWithID(id string, payload models.AlertWebhookPayload)
}

// NotificationID is the stable SSE envelope id for a threat notification —
// the same key order useLiveThreatFeed dedups on.
func NotificationID(deploymentID, agentID, sessionID, ruleID string) string {
	return deploymentID + "|" + agentID + "|" + sessionID + "|" + ruleID
}

// shouldNotify decides whether an incoming event produces a notification.
// Lexicographic comparison of the datetime64 text is chronological: both
// values are rendered by the same Timeplus server in the same fixed-width
// format and timezone.
func shouldNotify(st *State, detectedAt string) bool {
	if st == nil {
		return true // never notified
	}
	if !st.Confirmed {
		return false // delivered, pending confirm — absorbs burst duplicates
	}
	return detectedAt > st.ConfirmedAt // re-emerged after the user confirmed
}

// watchSQL replays the last 24h on (re)connect so a server restart recovers
// missed threats; shouldNotify filters out rows already notified/confirmed.
const watchSQL = `SELECT detected_at, rule_id, rule_name, severity,
  agent_id, deployment_id, session_id, hook_name, message
FROM agentguard_notify_events
SETTINGS seek_to='-24h'`

const (
	initialBackoff = time.Second
	maxBackoff     = 30 * time.Second
	notReadyPoll   = 5 * time.Second
)

// Watcher tails agentguard_notify_events and publishes new-threat toasts.
type Watcher struct {
	cfg   *config.Config
	store Store
	pub   Publisher
}

func NewWatcher(cfg *config.Config, store Store, pub Publisher) *Watcher {
	return &Watcher{cfg: cfg, store: store, pub: pub}
}

// Run blocks until ctx is cancelled, reconnecting forever with capped
// exponential backoff. It self-gates on cfg.SetupComplete so it can be
// started unconditionally at boot (setup may complete later via the wizard).
func (w *Watcher) Run(ctx context.Context) {
	backoff := initialBackoff
	for ctx.Err() == nil {
		config.Mu.RLock()
		ready := w.cfg.SetupComplete
		tpCfg := w.cfg.Timeplus
		config.Mu.RUnlock()
		if !ready {
			sleepCtx(ctx, notReadyPoll)
			continue
		}

		err := timeplus.StreamLines(ctx, &tpCfg, watchSQL,
			func() {
				log.Printf("notify: watching agentguard_notify_events")
				backoff = initialBackoff // reset once a connection succeeds
			},
			func(line []byte) error {
				w.handleLine(line)
				return nil
			})
		if ctx.Err() != nil {
			return
		}
		log.Printf("notify: stream ended (%v), reconnecting in %s", err, backoff)
		sleepCtx(ctx, backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// handleLine processes one NDJSON row. Failures never stop the stream:
// malformed rows are skipped, seen-state errors fail open toward notifying.
func (w *Watcher) handleLine(line []byte) {
	var e Event
	if err := json.Unmarshal(line, &e); err != nil {
		log.Printf("notify: skipping malformed row: %v", err)
		return
	}
	if e.AgentID == "" || e.RuleID == "" {
		return
	}

	st, err := w.store.Get(e)
	if err != nil {
		log.Printf("notify: seen-state read failed (failing open): %v", err)
		st = nil
	}
	if !shouldNotify(st, e.DetectedAt) {
		return
	}
	if err := w.store.MarkNotified(e); err != nil {
		log.Printf("notify: seen-state write failed (still notifying): %v", err)
	}
	w.pub.PublishWithID(
		NotificationID(e.DeploymentID, e.AgentID, e.SessionID, e.RuleID),
		models.AlertWebhookPayload{
			RuleID:       e.RuleID,
			RuleName:     e.RuleName,
			Severity:     e.Severity,
			AgentID:      e.AgentID,
			DeploymentID: e.DeploymentID,
			SessionID:    e.SessionID,
			Message:      e.Message,
			DetectedAt:   e.DetectedAt,
		})
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
