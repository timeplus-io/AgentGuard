package sentry

import (
	"errors"
	"sync"
)

// Approval-plumbing errors, mapped to HTTP statuses by the decide handler.
var (
	// ErrApprovalNotFound: unknown proposal id, or one already decided /
	// timed out / abandoned. Deliberately indistinguishable — a decide that
	// lost the race gets the same answer as a bogus id.
	ErrApprovalNotFound = errors.New("approval proposal not found or already resolved")
	// ErrApprovalForbidden: the deciding user is not the conversation's owner.
	ErrApprovalForbidden = errors.New("approval belongs to another user's conversation")
)

// ApprovalDecision is a human's answer to one GatedWrite proposal.
type ApprovalDecision struct {
	Approved  bool
	DecidedBy string
	Reason    string
}

type pendingApproval struct {
	owner string
	ch    chan ApprovalDecision
}

// ApprovalRegistry is an in-memory map of pending GatedWrite proposal IDs to
// the channel the blocked chat loop is waiting on — the same
// register-before-emit discipline as holdregistry: the loop registers the
// proposal BEFORE emitting the approval_request event, so a decide can never
// race ahead of registration. Single-decide semantics: Decide removes the
// entry under the lock, so exactly one decision wins; later attempts (and
// decides after the loop's timeout removed the entry) get
// ErrApprovalNotFound.
type ApprovalRegistry struct {
	mu sync.Mutex
	m  map[string]pendingApproval
}

// NewApprovalRegistry returns an empty registry. One instance per server
// process, shared by the chat handler (via Loop) and the decide handler.
func NewApprovalRegistry() *ApprovalRegistry {
	return &ApprovalRegistry{m: map[string]pendingApproval{}}
}

// register creates the pending entry and returns the buffered channel the
// loop should block on. owner is the conversation's user — the only user
// allowed to decide.
func (r *ApprovalRegistry) register(id, owner string) chan ApprovalDecision {
	ch := make(chan ApprovalDecision, 1)
	r.mu.Lock()
	r.m[id] = pendingApproval{owner: owner, ch: ch}
	r.mu.Unlock()
	return ch
}

// remove drops the entry (idempotent). The loop calls it on every exit path
// (decided, timeout, context cancelled); Decide has usually already removed
// it.
func (r *ApprovalRegistry) remove(id string) {
	r.mu.Lock()
	delete(r.m, id)
	r.mu.Unlock()
}

// Decide resolves a pending proposal: owner-checked, single-winner (the
// entry is removed under the lock before the decision is delivered on the
// buffered channel, so delivery never blocks and never races a second
// decide).
func (r *ApprovalRegistry) Decide(id, user string, approved bool, reason string) error {
	r.mu.Lock()
	p, ok := r.m[id]
	if !ok {
		r.mu.Unlock()
		return ErrApprovalNotFound
	}
	if p.owner != user {
		r.mu.Unlock()
		return ErrApprovalForbidden
	}
	delete(r.m, id)
	r.mu.Unlock()
	p.ch <- ApprovalDecision{Approved: approved, DecidedBy: user, Reason: reason}
	return nil
}

// PendingCount reports how many proposals are currently awaiting a decision
// (diagnostics/tests).
func (r *ApprovalRegistry) PendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.m)
}
