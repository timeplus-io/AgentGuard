package auth_test

import (
	"sync"
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
		t.Error("got wrong password")
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

func TestSessionStore_Concurrent(t *testing.T) {
	store := auth.NewSessionStore()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess := store.Create("user", "pw")
			store.Get(sess.ID)
			store.Delete(sess.ID)
		}()
	}
	wg.Wait()
}
