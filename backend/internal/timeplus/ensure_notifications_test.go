package timeplus

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestEnsureNotificationsSetupOrder pins the migration path for deployments
// that finished setup before #258 shipped (Finding 1): the stream DDL must
// run first (so the watcher/confirm endpoint have somewhere to write), then
// the legacy alert drop, then the legacy UDF drop (the alert references the
// UDF, so it must go first).
func TestEnsureNotificationsSetupOrder(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := streamTestCfg(t, srv.URL)

	if err := EnsureNotificationsSetup(&cfg); err != nil {
		t.Fatalf("EnsureNotificationsSetup: %v", err)
	}

	if len(bodies) != 3 {
		t.Fatalf("got %d requests, want 3 (stream DDL + 2 legacy drops): %v", len(bodies), bodies)
	}
	if bodies[0] != ddlNotificationsStream {
		t.Errorf("request[0] = %q, want the notifications stream DDL", bodies[0])
	}
	if bodies[1] != legacyNotifierDrops[0] {
		t.Errorf("request[1] = %q, want the alert drop %q", bodies[1], legacyNotifierDrops[0])
	}
	if bodies[2] != legacyNotifierDrops[1] {
		t.Errorf("request[2] = %q, want the UDF drop %q", bodies[2], legacyNotifierDrops[1])
	}
}

// TestEnsureNotificationsSetupReturnsStreamError asserts the stream-creation
// failure is surfaced to the caller (so the watcher/sync handler can log and
// retry) while the legacy drops are best-effort and never attempted once the
// stream call itself fails.
func TestEnsureNotificationsSetupReturnsStreamError(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := streamTestCfg(t, srv.URL)

	err := EnsureNotificationsSetup(&cfg)
	if err == nil {
		t.Fatal("EnsureNotificationsSetup returned nil error, want the stream-creation failure surfaced")
	}
	if calls != 1 {
		t.Errorf("server got %d requests, want 1 (legacy drops must not run after the stream call fails)", calls)
	}
}

// TestEnsureNotificationsSetupDropsAreBestEffort asserts a legacy-drop
// failure (e.g. the objects don't exist, or a transient error) never fails
// the overall call once the stream DDL succeeded.
func TestEnsureNotificationsSetupDropsAreBestEffort(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusOK) // stream DDL succeeds
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError) // drops fail
	}))
	defer srv.Close()

	cfg := streamTestCfg(t, srv.URL)

	if err := EnsureNotificationsSetup(&cfg); err != nil {
		t.Fatalf("EnsureNotificationsSetup returned %v, want nil (legacy drops are best-effort)", err)
	}
	if calls != 3 {
		t.Errorf("server got %d requests, want 3 (both drops attempted despite failures)", calls)
	}
}
