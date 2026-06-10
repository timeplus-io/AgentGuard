package holdregistry

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A registered waiter receives the decision a terminalizer commits, even when
// the commit arrives asynchronously while the long-poll is blocked on the
// channel. This is the canonical resolve path: claim → Commit → deliver.
func TestRegisterReceivesCommittedDecision(t *testing.T) {
	r := New()
	ch := r.Register("h1")
	defer r.Cancel("h1")
	committed := make(chan struct{})
	go func() {
		defer close(committed)
		time.Sleep(10 * time.Millisecond)
		if term, ok := r.TryBeginTerminalization("h1"); ok {
			term.Commit(Decision{Allow: true})
		}
	}()
	select {
	case d := <-ch:
		if !d.Allow {
			t.Fatalf("got %+v, want Allow=true", d)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for committed decision")
	}
	<-committed // Commit reads the tombstoneTTL global; let it return before the test ends
}

func TestDrainSignalsExistingWaiters(t *testing.T) {
	r := New()
	ch1 := r.Register("h1")
	ch2 := r.Register("h2")
	defer r.Cancel("h1")
	defer r.Cancel("h2")

	want := Decision{Allow: false, Reason: "shutting down"}
	r.Drain(want)

	for name, ch := range map[string]<-chan Decision{"h1": ch1, "h2": ch2} {
		select {
		case got := <-ch:
			if got != want {
				t.Fatalf("%s got %+v, want %+v", name, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for drain decision on %s", name)
		}
	}
}

func TestRegisterAfterDrainReceivesDrainDecision(t *testing.T) {
	r := New()
	want := Decision{Allow: true, Reason: "shutting down"}
	r.Drain(want)

	ch := r.Register("h1")
	defer r.Cancel("h1")

	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for immediate drain decision")
	}
}

func TestTryBeginTerminalizationSingleWinner(t *testing.T) {
	r := New()
	r.Register("h1")
	defer r.Cancel("h1")

	if _, ok := r.TryBeginTerminalization("h1"); !ok {
		t.Fatal("first terminalizer lost")
	}
	if _, ok := r.TryBeginTerminalization("h1"); ok {
		t.Fatal("second terminalizer won")
	}
}

func TestAbortTerminalizationAllowsRetry(t *testing.T) {
	r := New()
	r.Register("h1")
	defer r.Cancel("h1")

	terminalization, ok := r.TryBeginTerminalization("h1")
	if !ok {
		t.Fatal("first terminalizer lost")
	}
	terminalization.Abort()
	if _, ok := r.TryBeginTerminalization("h1"); !ok {
		t.Fatal("terminalizer could not retry after abort")
	}
}

func TestTerminalizationSignalsOwnedWaiter(t *testing.T) {
	r := New()
	ch := r.Register("h1")
	defer r.Cancel("h1")

	terminalization, ok := r.TryBeginTerminalization("h1")
	if !ok {
		t.Fatal("terminalizer lost")
	}
	want := Decision{Allow: true, Reason: "approved"}
	terminalization.Commit(want)

	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for committed decision")
	}
}

func TestDrainSkipsClaimedWaiter(t *testing.T) {
	r := New()
	ch := r.Register("h1")
	defer r.Cancel("h1")

	terminalization, ok := r.TryBeginTerminalization("h1")
	if !ok {
		t.Fatal("terminalizer lost")
	}
	r.Drain(Decision{Allow: false, Reason: "shutting down"})

	select {
	case got := <-ch:
		t.Fatalf("drain signaled claimed waiter: %+v", got)
	default:
	}

	want := Decision{Allow: true, Reason: "approved"}
	terminalization.Commit(want)
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for committed decision")
	}
}

func TestAbortDuringDrainSignalsDrainDecision(t *testing.T) {
	r := New()
	ch := r.Register("h1")
	defer r.Cancel("h1")

	terminalization, ok := r.TryBeginTerminalization("h1")
	if !ok {
		t.Fatal("terminalizer lost")
	}
	want := Decision{Allow: false, Reason: "shutting down"}
	r.Drain(want)
	terminalization.Abort()

	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for drain decision after abort")
	}
}

// An orphaned hold has no registered waiter — e.g. a hold left 'pending' by a
// previous process and swept by startup reconcile, or a timed-out hold whose
// waiter was already cancelled. Concurrent in-process terminalizers (reconcile
// vs. an operator decide, or two operators) must still serialize to a single
// winner. The storage guard is NOT a substitute: an append-only INSERT...SELECT
// ...WHERE status='pending' is not atomic under concurrency (flush-lagged reads
// let two writers both see 'pending' and both append a terminal row), so the
// in-process claim is what prevents competing terminal rows.
func TestTryBeginTerminalizationSerializesOrphans(t *testing.T) {
	r := New() // no Register — "orphan" has no waiter

	if _, ok := r.TryBeginTerminalization("orphan"); !ok {
		t.Fatal("first orphan terminalizer lost")
	}
	if _, ok := r.TryBeginTerminalization("orphan"); ok {
		t.Fatal("second orphan terminalizer won — orphans are not serialized in-process")
	}
}

// A failed orphan write (Abort) must release the claim so a later sweep or
// operator decision can retry — nothing was persisted, so the hold is still
// genuinely pending.
func TestOrphanAbortAllowsRetry(t *testing.T) {
	r := New()

	term, ok := r.TryBeginTerminalization("orphan")
	if !ok {
		t.Fatal("first orphan terminalizer lost")
	}
	term.Abort() // simulate a failed storage write

	if _, ok := r.TryBeginTerminalization("orphan"); !ok {
		t.Fatal("could not retry orphan terminalization after abort")
	}
}

// A successful orphan terminalization (Commit) must keep the claim so a later
// in-process terminalizer cannot write a competing terminal row for the same
// hold (the storage guard alone would not stop it under flush lag).
func TestOrphanWinBlocksLaterTerminalizer(t *testing.T) {
	r := New()

	term, ok := r.TryBeginTerminalization("orphan")
	if !ok {
		t.Fatal("first orphan terminalizer lost")
	}
	term.Commit(Decision{Allow: false}) // success: no waiter, but claim is kept

	if _, ok := r.TryBeginTerminalization("orphan"); ok {
		t.Fatal("a second terminalizer won after a successful orphan terminalization")
	}
}

// A successful terminalization of a LIVE waiter must leave a tombstone that
// survives the long-poll's defer Cancel. Otherwise, during Timeplus flush lag
// (the terminal row not yet visible to historical reads), a second terminalizer
// — a session-end abandon, drain, or rule cleanup firing seconds later — reads
// the hold as still 'pending', wins a fresh claim, and appends a competing
// terminal row that clobbers the real decision in the record (last updated_at
// wins). The in-process tombstone is what closes that window until the terminal
// row is durably visible.
func TestLiveTerminalizationLeavesTombstoneAfterCancel(t *testing.T) {
	r := New()
	r.Register("h1")

	term, ok := r.TryBeginTerminalization("h1")
	if !ok {
		t.Fatal("first terminalizer lost")
	}
	term.Commit(Decision{Allow: true}) // operator decision wins + signals waiter
	r.Cancel("h1")                      // long-poll receives, returns, defers Cancel

	if _, ok := r.TryBeginTerminalization("h1"); ok {
		t.Fatal("second terminalizer won after live terminalization + cancel — flush-lag double-write window is open")
	}
}

// The tombstone is bounded: once the TTL elapses (terminal row now durably
// visible, so the SQL guard suffices), the entry is collected and a later
// terminalization can re-claim — no unbounded growth.
func TestTombstoneExpiresAllowingReclaim(t *testing.T) {
	old := tombstoneTTL
	tombstoneTTL = 5 * time.Millisecond
	defer func() { tombstoneTTL = old }()

	r := New()
	term, ok := r.TryBeginTerminalization("orphan")
	if !ok {
		t.Fatal("first terminalizer lost")
	}
	term.Commit(Decision{Allow: false}) // success → short-lived tombstone

	deadline := time.Now().Add(time.Second)
	for {
		if _, ok := r.TryBeginTerminalization("orphan"); ok {
			return // re-claimed after expiry — tombstone was collected
		}
		if time.Now().After(deadline) {
			t.Fatal("tombstone never expired")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestConcurrentOrphanTerminalizationSingleWinner(t *testing.T) {
	r := New()
	const N = 50
	var wins int32
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			if _, ok := r.TryBeginTerminalization("orphan"); ok {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("expected exactly 1 winner for an orphaned hold, got %d", wins)
	}
}

func TestConcurrentRegisterTerminalize(t *testing.T) {
	r := New()
	const N = 100
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		id := "h" + strconv.Itoa(i)
		ch := r.Register(id)
		go func() {
			defer wg.Done()
			defer r.Cancel(id)
			d := <-ch
			if !d.Allow {
				t.Errorf("expected allow")
			}
		}()
		go func() {
			if term, ok := r.TryBeginTerminalization(id); ok {
				term.Commit(Decision{Allow: true})
			}
		}()
	}
	wg.Wait()
}
