package notify

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
)

type fakeStore struct {
	state  map[string]*State
	marked []Event
	getErr error
}

func stateKey(e Event) string {
	return e.AgentID + "|" + e.DeploymentID + "|" + e.SessionID + "|" + e.RuleID
}

func (f *fakeStore) Get(e Event) (*State, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.state[stateKey(e)], nil
}

func (f *fakeStore) MarkNotified(e Event) error {
	f.marked = append(f.marked, e)
	return nil
}

type fakePub struct {
	ids      []string
	payloads []models.AlertWebhookPayload
}

func (f *fakePub) PublishWithID(id string, p models.AlertWebhookPayload) {
	f.ids = append(f.ids, id)
	f.payloads = append(f.payloads, p)
}

func TestShouldNotify(t *testing.T) {
	cases := []struct {
		name       string
		st         *State
		detectedAt string
		want       bool
	}{
		{"never notified", nil, "2026-07-30 10:00:00.000", true},
		{"pending unconfirmed (burst dup)", &State{Confirmed: false}, "2026-07-30 10:00:01.000", false},
		{"re-emerged after confirm", &State{Confirmed: true, ConfirmedAt: "2026-07-30 10:00:00.000"}, "2026-07-30 10:05:00.000", true},
		{"replayed old event after confirm", &State{Confirmed: true, ConfirmedAt: "2026-07-30 10:00:00.000"}, "2026-07-30 09:59:59.999", false},
		{"event at exactly confirm time", &State{Confirmed: true, ConfirmedAt: "2026-07-30 10:00:00.000"}, "2026-07-30 10:00:00.000", false},
	}
	for _, tc := range cases {
		if got := shouldNotify(tc.st, tc.detectedAt); got != tc.want {
			t.Errorf("%s: shouldNotify = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHandleLineNewThreatPublishesWithStableID(t *testing.T) {
	st := &fakeStore{state: map[string]*State{}}
	pub := &fakePub{}
	w := &Watcher{store: st, pub: pub}

	w.handleLine([]byte(`{"detected_at":"2026-07-30 10:00:00.000","rule_id":"rp-001","rule_name":"Secrets in prompt","severity":"critical","agent_id":"a1","deployment_id":"d1","session_id":"s1","message":"m"}`))

	if len(pub.ids) != 1 {
		t.Fatalf("published %d times, want 1", len(pub.ids))
	}
	if pub.ids[0] != "d1|a1|s1|rp-001" {
		t.Errorf("id = %q, want deployment|agent|session|rule", pub.ids[0])
	}
	if pub.payloads[0].SessionID != "s1" || pub.payloads[0].RuleName != "Secrets in prompt" {
		t.Errorf("payload not filled from the event: %+v", pub.payloads[0])
	}
	if len(st.marked) != 1 {
		t.Errorf("MarkNotified called %d times, want 1", len(st.marked))
	}
}

func TestHandleLinePendingUnconfirmedSkips(t *testing.T) {
	e := Event{AgentID: "a1", DeploymentID: "d1", SessionID: "s1", RuleID: "rp-001"}
	st := &fakeStore{state: map[string]*State{stateKey(e): {Confirmed: false}}}
	pub := &fakePub{}
	w := &Watcher{store: st, pub: pub}

	w.handleLine([]byte(`{"detected_at":"2026-07-30 10:00:00.000","rule_id":"rp-001","agent_id":"a1","deployment_id":"d1","session_id":"s1"}`))

	if len(pub.ids) != 0 {
		t.Errorf("published %d times, want 0 (already pending)", len(pub.ids))
	}
}

func TestHandleLineStoreErrorFailsOpen(t *testing.T) {
	st := &fakeStore{getErr: errors.New("timeplus down")}
	pub := &fakePub{}
	w := &Watcher{store: st, pub: pub}

	w.handleLine([]byte(`{"detected_at":"2026-07-30 10:00:00.000","rule_id":"rp-001","agent_id":"a1"}`))

	if len(pub.ids) != 1 {
		t.Errorf("published %d times, want 1 (fail open toward notifying)", len(pub.ids))
	}
}

func TestHandleLineMalformedAndIncompleteRowsIgnored(t *testing.T) {
	pub := &fakePub{}
	w := &Watcher{store: &fakeStore{}, pub: pub}

	w.handleLine([]byte(`not json`))
	w.handleLine([]byte(`{"detected_at":"2026-07-30 10:00:00.000"}`)) // no agent_id/rule_id

	if len(pub.ids) != 0 {
		t.Errorf("published %d times, want 0", len(pub.ids))
	}
}

// TestRunCallsEnsureBeforeFirstStream pins Finding 1(b): Run must call the
// notifications-migration hook once the watcher is ready, before its first
// StreamLines attempt, so an upgraded deployment gets the seen-state stream
// and legacy teardown without any operator action.
func TestRunCallsEnsureBeforeFirstStream(t *testing.T) {
	cfg := &config.Config{SetupComplete: true}
	w := NewWatcher(cfg, &fakeStore{state: map[string]*State{}}, &fakePub{})

	var calls []string
	ensureDone := make(chan struct{})
	w.ensure = func(*config.TimeplusConfig) error {
		calls = append(calls, "ensure")
		close(ensureDone)
		return nil
	}
	streamed := make(chan struct{})
	w.streamFn = func(ctx context.Context, cfg *config.TimeplusConfig, sql string, onConnected func(), onLine func([]byte) error) error {
		calls = append(calls, "stream")
		select {
		case <-streamed:
		default:
			close(streamed)
		}
		return errors.New("stop")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	select {
	case <-streamed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first stream attempt")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to return after cancel")
	}

	if len(calls) < 2 || calls[0] != "ensure" || calls[1] != "stream" {
		t.Fatalf("calls = %v, want ensure before the first stream attempt", calls)
	}
}

// TestRunEnsureNotRetriedAfterSuccess pins the "guard bool" contract: once
// ensure succeeds it must not be called again on subsequent reconnects.
func TestRunEnsureNotRetriedAfterSuccess(t *testing.T) {
	cfg := &config.Config{SetupComplete: true}
	w := NewWatcher(cfg, &fakeStore{state: map[string]*State{}}, &fakePub{})

	var ensureCalls int
	w.ensure = func(*config.TimeplusConfig) error {
		ensureCalls++
		return nil
	}
	var streamCalls int
	secondStream := make(chan struct{})
	w.streamFn = func(ctx context.Context, cfg *config.TimeplusConfig, sql string, onConnected func(), onLine func([]byte) error) error {
		streamCalls++
		if streamCalls == 2 {
			close(secondStream)
		}
		return errors.New("stop")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	select {
	case <-secondStream:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a second stream attempt")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Run to return after cancel")
	}

	if ensureCalls != 1 {
		t.Errorf("ensure called %d times, want exactly 1 (guarded after success)", ensureCalls)
	}
}

// TestRunSkipsEnsureWhileNotReady asserts the migration hook never fires
// before the watcher's SetupComplete gate opens.
func TestRunSkipsEnsureWhileNotReady(t *testing.T) {
	cfg := &config.Config{SetupComplete: false}
	w := NewWatcher(cfg, &fakeStore{state: map[string]*State{}}, &fakePub{})

	var ensureCalls int
	w.ensure = func(*config.TimeplusConfig) error {
		ensureCalls++
		return nil
	}
	w.streamFn = func(ctx context.Context, cfg *config.TimeplusConfig, sql string, onConnected func(), onLine func([]byte) error) error {
		t.Error("streamFn must not be called while not ready")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	w.Run(ctx)

	if ensureCalls != 0 {
		t.Errorf("ensure called %d times, want 0 while SetupComplete is false", ensureCalls)
	}
}
