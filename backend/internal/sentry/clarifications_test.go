package sentry

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestClarificationRegistryAnswer(t *testing.T) {
	r := NewClarificationRegistry()
	ch := r.register("c1", "alice")

	if err := r.Answer("c1", "bob", nil, true); !errors.Is(err, ErrClarificationForbidden) {
		t.Fatalf("wrong-user answer: err = %v, want ErrClarificationForbidden", err)
	}
	if err := r.Answer("nope", "alice", nil, true); !errors.Is(err, ErrClarificationNotFound) {
		t.Fatalf("unknown id: err = %v, want ErrClarificationNotFound", err)
	}
	if err := r.Answer("c1", "alice", json.RawMessage(`[{"selected":["Bash"]}]`), false); err != nil {
		t.Fatalf("owner answer: %v", err)
	}
	a := <-ch
	if a.Skipped || a.AnsweredBy != "alice" || string(a.Answers) != `[{"selected":["Bash"]}]` {
		t.Fatalf("answer = %+v", a)
	}
	// Single-answer: the entry is gone.
	if err := r.Answer("c1", "alice", nil, true); !errors.Is(err, ErrClarificationNotFound) {
		t.Fatalf("second answer: err = %v, want ErrClarificationNotFound", err)
	}
	if r.PendingCount() != 0 {
		t.Fatalf("pending = %d, want 0", r.PendingCount())
	}
}

func TestClarificationRegistrySkip(t *testing.T) {
	r := NewClarificationRegistry()
	ch := r.register("c2", "alice")
	if err := r.Answer("c2", "alice", nil, true); err != nil {
		t.Fatalf("skip: %v", err)
	}
	a := <-ch
	if !a.Skipped || a.AnsweredBy != "alice" || a.Answers != nil {
		t.Fatalf("skip answer = %+v", a)
	}
}
