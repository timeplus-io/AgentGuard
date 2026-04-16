package auth_test

import (
	"testing"

	"github.com/timeplus/agentguard/internal/auth"
)

func TestSessionStore_CreateAndGet(t *testing.T) {
	store := auth.NewSessionStore()
	sess := store.Create("alice", "s3cret")

	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if sess.User != "alice" {
		t.Errorf("user: want alice, got %s", sess.User)
	}

	got, ok := store.Get(sess.ID)
	if !ok {
		t.Fatal("expected session to exist after Create")
	}
	if got.User != "alice" {
		t.Errorf("got user: want alice, got %s", got.User)
	}
	if got.Password != "s3cret" {
		t.Errorf("got password: want s3cret, got %s", got.Password)
	}
}

func TestSessionStore_Delete(t *testing.T) {
	store := auth.NewSessionStore()
	sess := store.Create("bob", "pw")
	store.Delete(sess.ID)

	_, ok := store.Get(sess.ID)
	if ok {
		t.Error("expected session to be gone after Delete")
	}
}

func TestSessionStore_GetMissing(t *testing.T) {
	store := auth.NewSessionStore()
	_, ok := store.Get("nonexistent-id")
	if ok {
		t.Error("expected false for missing session")
	}
}
