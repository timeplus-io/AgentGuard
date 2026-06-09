// Package holdregistry is an in-memory map of pending hold IDs to a
// single-buffered channel that delivers the human Approve/Deny decision
// to a long-poll goroutine waiting in the /api/holds handler.
//
// Lost on backend restart; in-flight long-polls then expire on their own
// timeout and the plugin's fail-policy applies.
package holdregistry

import (
	"sync"
	"time"
)

// tombstoneTTL is how long a resolved hold's claim is retained after the
// long-poll cancels, before the entry is collected. It must exceed the worst
// Timeplus flush/read lag so that by the time the tombstone is dropped, the
// terminal row is durably visible and the SQL guard alone prevents a competing
// write. Var (not const) so tests can shrink it.
var tombstoneTTL = 60 * time.Second

type Decision struct {
	Allow  bool
	Reason string
}

type Registry struct {
	drainMu       sync.RWMutex
	draining      bool
	drainDecision Decision
	m             sync.Map // hold_id (string) → *entry
}

func New() *Registry {
	return &Registry{}
}

// entry is the per-hold record in the registry map. One struct serves three
// roles across a hold's lifetime, distinguished by the two fields below:
//
//   - live waiter:  ch != nil, claimed == false — a blocked /api/holds long-poll
//     is waiting on ch for its decision.
//   - claim:        claimed == true — a terminalizer owns the exclusive right to
//     resolve this hold. ch is non-nil if it claimed a live waiter, nil if it
//     LoadOrStore'd a claim for an orphan (a hold with no live waiter).
//   - tombstone:    claimed == true, retained for tombstoneTTL past the
//     long-poll's Cancel after a successful Commit, so a flush-lagged
//     terminalizer can't win a fresh claim and clobber the recorded decision.
type entry struct {
	mu      sync.Mutex
	ch      chan Decision // nil for an orphan claim (no live waiter to deliver to)
	claimed bool          // a terminalizer owns this entry (live claim, orphan claim, or tombstone)
}

type Terminalization struct {
	r      *Registry
	e      *entry
	holdID string
	// orphan is true when this claim was created for a hold with no registered
	// waiter (LoadOrStore'd by TryBeginTerminalization). Such claims have no
	// channel and a different cleanup lifecycle than live-waiter claims.
	orphan bool
}

// Abort releases the terminalization claim after the caller failed before
// reaching a terminal decision. If process shutdown has already begun, Abort
// hands the waiter to the shutdown drain decision instead of leaving it blocked.
// It must not be called after Commit.
func (t Terminalization) Abort() {
	if t.e == nil {
		return
	}
	if t.orphan {
		// No live waiter and nothing was persisted — drop the claim entirely so
		// a later sweep or operator decision can re-claim and retry. (Draining
		// is irrelevant: there is no blocked /api/holds request to release.)
		t.r.m.Delete(t.holdID)
		return
	}
	if t.r != nil {
		t.r.drainMu.RLock()
		draining := t.r.draining
		drainDecision := t.r.drainDecision
		if draining {
			t.r.drainMu.RUnlock()
			t.Commit(drainDecision)
			return
		}
		t.e.mu.Lock()
		t.e.claimed = false
		t.e.mu.Unlock()
		t.r.drainMu.RUnlock()
		return
	}
	t.e.mu.Lock()
	t.e.claimed = false
	t.e.mu.Unlock()
}

// Commit records a SUCCESSFUL terminalization. For a live waiter it delivers the
// decision to the blocked /api/holds request; for an orphan there is no waiter.
// In both cases it then retains the claim as a tombstone for a bounded window
// (tombstoneTTL) — even after the long-poll's defer Cancel — so a concurrent
// terminalizer (a session-end abandon, drain, or rule cleanup) that reads this
// hold as still 'pending' during Timeplus flush lag cannot win a fresh claim and
// append a competing terminal row that clobbers the real decision. Once the TTL
// elapses the entry self-collects, by which point the terminal row is durably
// visible and the SQL guard alone suffices.
func (t Terminalization) Commit(d Decision) {
	if t.e == nil {
		return
	}
	if !t.orphan {
		select {
		case t.e.ch <- d:
		default:
		}
	}
	t.r.scheduleTombstone(t.holdID)
}

// scheduleTombstone collects the hold's claim after tombstoneTTL. Per-entry
// timer rather than a sweeper goroutine: no registry lifecycle to manage, and
// hold IDs are unique (uuids) so the delete can never hit a reused entry.
func (r *Registry) scheduleTombstone(holdID string) {
	time.AfterFunc(tombstoneTTL, func() { r.m.Delete(holdID) })
}

// Register reserves a slot for the given hold_id and returns the receive end
// of the channel. The channel is buffered (size 1) so Commit never blocks.
//
// CONTRACT: callers MUST receive with a timeout, e.g.:
//
//	ch := r.Register(id)
//	defer r.Cancel(id)
//	select {
//	case d := <-ch:
//	    // decision received
//	case <-time.After(timeout):
//	    // give up on this hold
//	}
//
// The channel is never closed by Cancel — receiving without a timeout would
// block forever if no decision arrives (e.g., backend restart, signal lost).
func (r *Registry) Register(holdID string) <-chan Decision {
	ch := make(chan Decision, 1)

	r.drainMu.RLock()
	defer r.drainMu.RUnlock()
	if r.draining {
		ch <- r.drainDecision
		return ch
	}

	r.m.Store(holdID, &entry{ch: ch})
	return ch
}

// TryBeginTerminalization claims the right to resolve a hold in this process.
// Only one caller can win; losers must not persist, signal, publish, or count
// the hold. The winner MUST finish with exactly one of Commit (success) or
// Abort (failure).
//
// This serializes ALL holds, not just those with a live waiter. An orphaned hold
// (left 'pending' by a previous process and swept by reconcile, or a timed-out
// hold whose waiter was cancelled) has no registry entry, so we LoadOrStore a
// claim for it. This matters because the storage guard is not a true CAS — an
// append-only INSERT...SELECT...WHERE status='pending' against flush-lagged reads
// lets two concurrent writers both observe 'pending' and both append a competing
// terminal row (last updated_at wins). The in-process claim is what guarantees a
// single terminal write per hold within this process.
func (r *Registry) TryBeginTerminalization(holdID string) (Terminalization, bool) {
	claim := &entry{claimed: true}
	v, loaded := r.m.LoadOrStore(holdID, claim)
	if !loaded {
		// We installed the claim → we won. It has no channel: an orphaned hold
		// has no blocked /api/holds request to signal.
		return Terminalization{r: r, e: claim, holdID: holdID, orphan: true}, true
	}

	e, ok := v.(*entry)
	if !ok {
		return Terminalization{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.claimed {
		return Terminalization{}, false
	}
	e.claimed = true
	return Terminalization{r: r, e: e, holdID: holdID}, true
}

// Drain releases all current and future waiters with the same decision.
// Used during process shutdown so synchronous hold calls receive an explicit
// fail-policy verdict instead of a dropped HTTP connection.
func (r *Registry) Drain(d Decision) {
	r.drainMu.Lock()
	defer r.drainMu.Unlock()

	r.draining = true
	r.drainDecision = d
	r.m.Range(func(_, v any) bool {
		e, ok := v.(*entry)
		if !ok {
			return true
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.claimed {
			return true
		}
		e.claimed = true
		select {
		case e.ch <- d:
		default:
		}
		return true
	})
}

// Cancel is the long-poll's deferred cleanup. It removes the entry UNLESS the
// hold was resolved (a terminalization won) — in that case a tombstone is in
// place and is left to self-collect, so a flush-lag double-write can't slip in.
// Safe to call multiple times.
func (r *Registry) Cancel(holdID string) {
	v, ok := r.m.Load(holdID)
	if !ok {
		return
	}
	e, ok := v.(*entry)
	if !ok {
		r.m.Delete(holdID)
		return
	}
	e.mu.Lock()
	resolved := e.claimed
	e.mu.Unlock()
	if resolved {
		// A terminalization won for this hold; Commit scheduled a tombstone to
		// keep the claim alive across the flush-lag window. Deleting here would
		// reopen the double-write window, so leave collection to the tombstone.
		return
	}
	r.m.Delete(holdID)
}
