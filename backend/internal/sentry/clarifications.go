package sentry

import (
	"encoding/json"
	"errors"
	"sync"
)

// Clarification-plumbing errors, mapped to HTTP statuses by the answer
// handler.
var (
	// ErrClarificationNotFound: unknown clarification id, or one already
	// answered / timed out / abandoned. Deliberately indistinguishable — an
	// answer that lost the race gets the same reply as a bogus id.
	ErrClarificationNotFound = errors.New("clarification not found or already resolved")
	// ErrClarificationForbidden: the answering user is not the
	// conversation's owner.
	ErrClarificationForbidden = errors.New("clarification belongs to another user's conversation")
)

// ClarificationAnswer is a human's answer to one ask_user call: the raw
// answers array from the UI (index-aligned with the questions), or Skipped
// when the operator explicitly told Sentry to proceed with defaults.
type ClarificationAnswer struct {
	Answers    json.RawMessage
	Skipped    bool
	AnsweredBy string
}

type pendingClarification struct {
	owner string
	ch    chan ClarificationAnswer
}

// ClarificationRegistry is the ask_user twin of ApprovalRegistry (#264
// mirroring #221): an in-memory map of pending clarification IDs to the
// channel the blocked chat loop waits on. Same discipline: the loop
// registers BEFORE emitting clarification_request so an answer can never
// race registration; Answer removes the entry under the lock, so exactly
// one answer wins.
type ClarificationRegistry struct {
	mu sync.Mutex
	m  map[string]pendingClarification
}

// NewClarificationRegistry returns an empty registry. One instance per
// server process, shared by the chat handler (via Loop) and the answer
// handler.
func NewClarificationRegistry() *ClarificationRegistry {
	return &ClarificationRegistry{m: map[string]pendingClarification{}}
}

// register creates the pending entry and returns the buffered channel the
// loop should block on. owner is the conversation's user — the only user
// allowed to answer.
func (r *ClarificationRegistry) register(id, owner string) chan ClarificationAnswer {
	ch := make(chan ClarificationAnswer, 1)
	r.mu.Lock()
	r.m[id] = pendingClarification{owner: owner, ch: ch}
	r.mu.Unlock()
	return ch
}

// remove drops the entry (idempotent). The loop calls it on every exit path
// (answered, timeout, context cancelled); Answer has usually already
// removed it.
func (r *ClarificationRegistry) remove(id string) {
	r.mu.Lock()
	delete(r.m, id)
	r.mu.Unlock()
}

// Answer resolves a pending clarification: owner-checked, single-winner
// (the entry is removed under the lock before delivery on the buffered
// channel, so delivery never blocks and never races a second answer).
// skipped=true means "proceed with your defaults" — answers are ignored.
func (r *ClarificationRegistry) Answer(id, user string, answers json.RawMessage, skipped bool) error {
	r.mu.Lock()
	p, ok := r.m[id]
	if !ok {
		r.mu.Unlock()
		return ErrClarificationNotFound
	}
	if p.owner != user {
		r.mu.Unlock()
		return ErrClarificationForbidden
	}
	delete(r.m, id)
	r.mu.Unlock()
	if skipped {
		answers = nil
	}
	p.ch <- ClarificationAnswer{Answers: answers, Skipped: skipped, AnsweredBy: user}
	return nil
}

// PendingCount reports how many clarifications are awaiting an answer
// (diagnostics/tests).
func (r *ClarificationRegistry) PendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.m)
}
