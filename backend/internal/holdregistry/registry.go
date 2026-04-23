// Package holdregistry is an in-memory map of pending hold IDs to a
// single-buffered channel that delivers the human Approve/Deny decision
// to a long-poll goroutine waiting in the /api/holds handler.
//
// Lost on backend restart; in-flight long-polls then expire on their own
// timeout and the plugin's fail-policy applies.
package holdregistry

import "sync"

type Decision struct {
	Allow  bool
	Reason string
}

type Registry struct {
	m sync.Map // hold_id (string) → chan Decision
}

func New() *Registry {
	return &Registry{}
}

// Register reserves a slot for the given hold_id and returns the receive end
// of the channel. The channel is buffered (size 1) so Signal never blocks.
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
// block forever if no Signal arrives (e.g., backend restart, signal lost).
func (r *Registry) Register(holdID string) <-chan Decision {
	ch := make(chan Decision, 1)
	r.m.Store(holdID, ch)
	return ch
}

// Signal delivers a decision to the waiting goroutine. No-op if no goroutine
// is waiting (caller may have timed out and called Cancel). A second Signal
// for the same id is dropped — the channel buffer is already full.
func (r *Registry) Signal(holdID string, d Decision) {
	v, ok := r.m.Load(holdID)
	if !ok {
		return
	}
	ch, ok := v.(chan Decision)
	if !ok {
		return
	}
	select {
	case ch <- d:
	default:
	}
}

// Cancel removes the entry. Safe to call multiple times.
func (r *Registry) Cancel(holdID string) {
	r.m.Delete(holdID)
}
