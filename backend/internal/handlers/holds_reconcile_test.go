package handlers

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/holdregistry"
)

// Behavior contract for buildOrphanedPendingHoldsSQL.
//
// On startup the in-memory hold registry is empty, so a hold left 'pending' in
// Timeplus by a previous process (crash, or a graceful-shutdown drain that
// signalled the plugin but did not persist a terminal status) is orphaned and
// should be marked 'abandoned'.
//
// The critical safety property: it must NOT touch holds created by THIS process.
// A hold created after startup has a live goroutine waiting on its in-memory
// channel; clobbering its status to 'abandoned' would both lie in the UI and
// make DecideHold refuse the real decision (it rejects non-'pending' holds).
// We exclude them with a strict created_at < startup-time threshold.
func TestBuildOrphanedPendingHoldsSQL(t *testing.T) {
	before := time.Date(2026, 6, 8, 3, 21, 41, 123_000_000, time.UTC)
	sql := buildOrphanedPendingHoldsSQL(before)

	must := []struct{ fragment, why string }{
		{"SELECT hold_id", "selects the id to abandon"},
		{"FROM table(agentguard_holds)", "must read the holds stream"},
		{"GROUP BY hold_id", "must fold to latest-state per hold via the subquery, not raw append-only rows"},
		{"status = 'pending'", "only pending holds are candidates"},
		{"created_at < '2026-06-08 03:21:41.123'", "only holds created before this process started — excludes live holds owned by this process"},
	}
	for _, m := range must {
		if !strings.Contains(sql, m.fragment) {
			t.Errorf("missing %q (%s)\nSQL:\n%s", m.fragment, m.why, sql)
		}
	}

	// Must be a strict less-than: a hold created in the same millisecond as
	// startup is treated as ours (not reconciled), never the reverse — a false
	// negative is harmless (caught next restart); a false positive clobbers a
	// live hold.
	if strings.Contains(sql, "created_at <= ") {
		t.Errorf("threshold must be strict '<', not '<='\nSQL:\n%s", sql)
	}
}

// The `AND status = 'pending'` guard skips the write when a terminal state is
// already VISIBLE to this read: every caller transitions pending→terminal, so a
// late 'abandoned' write (startup reconcile, session-end drain, rule-disable)
// that finds no pending row no-ops instead of clobbering an already-flushed
// decision. It is NOT a cross-process guarantee — table() reads are flush-lagged,
// so a terminal row committed just before a fast restart can be invisible here
// and get clobbered; the startup reconcile only narrows that window with a settle
// delay. Single-writer-per-hold WITHIN a process is enforced by the holdregistry
// claim.
func TestBuildHoldStatusSQLGuardsOnPending(t *testing.T) {
	sql := buildHoldStatusSQL("h-1", "abandoned", "system", "reconciled on restart", "2026-06-08 03:21:41.123")
	for _, frag := range []string{
		"INSERT INTO agentguard_holds",
		"'abandoned' AS status",
		"GROUP BY hold_id", // reads latest folded state, not raw rows
		"WHERE hold_id = 'h-1' AND status = 'pending'",
	} {
		if !strings.Contains(sql, frag) {
			t.Errorf("missing %q\nSQL:\n%s", frag, sql)
		}
	}

	// The guard must live in a nested subquery, BELOW the `'abandoned' AS status`
	// re-alias. Proton/ClickHouse expose SELECT-list aliases in WHERE, so if the
	// guard shares a scope with the re-alias it compares the literal new status to
	// 'pending' (always false) and the INSERT no-ops. The `SELECT * FROM (` wrapper
	// is the structural marker that the filter sits one level down on the real
	// column. (Only the live Timeplus E2E proves the semantics; this catches a
	// naive flattening revert.)
	alias := strings.Index(sql, "AS status")
	wrap := strings.Index(sql, "SELECT * FROM")
	guard := strings.Index(sql, "status = 'pending'")
	if wrap == -1 || alias >= wrap || wrap >= guard {
		t.Errorf("guard must be nested (alias < `SELECT * FROM` < guard); got alias=%d wrap=%d guard=%d\nSQL:\n%s",
			alias, wrap, guard, sql)
	}
}

// ReconcileOrphanedHolds must tolerate Timeplus being unreachable at boot
// (timeplusd still warming up under the CLI): it retries a bounded number of
// times and then returns — it must never block startup forever or panic.
func TestReconcileOrphanedHoldsGivesUpWhenTimeplusUnreachable(t *testing.T) {
	origDelay, origInterval, origAttempts := reconcileSettleDelay, reconcileRetryInterval, reconcileMaxAttempts
	reconcileSettleDelay = 0 // skip the startup settle sleep
	reconcileRetryInterval = time.Millisecond
	reconcileMaxAttempts = 3
	defer func() {
		reconcileSettleDelay, reconcileRetryInterval, reconcileMaxAttempts = origDelay, origInterval, origAttempts
	}()

	cfg := &config.Config{}
	cfg.Timeplus.Host = "127.0.0.1"
	cfg.Timeplus.Port = 1 // nothing listening → connection refused, fast

	done := make(chan struct{})
	go func() {
		ReconcileOrphanedHolds(cfg, holdregistry.New(), time.Now().UTC())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ReconcileOrphanedHolds blocked when Timeplus is unreachable")
	}
}

// The settle delay must precede the FIRST Timeplus query, not just sit between
// failed retries — a query that succeeds on the first attempt (timeplusd already
// up) is exactly when the flush-lag window is open. We stand up a server that
// timestamps its first request and assert it arrives no sooner than the delay.
func TestReconcileOrphanedHoldsSettlesBeforeFirstQuery(t *testing.T) {
	origDelay, origInterval, origAttempts := reconcileSettleDelay, reconcileRetryInterval, reconcileMaxAttempts
	reconcileSettleDelay = 150 * time.Millisecond
	reconcileRetryInterval = time.Millisecond
	reconcileMaxAttempts = 1
	defer func() {
		reconcileSettleDelay, reconcileRetryInterval, reconcileMaxAttempts = origDelay, origInterval, origAttempts
	}()

	start := time.Now()
	firstQuery := make(chan time.Duration, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case firstQuery <- time.Since(start):
		default: // only record the first hit
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer srv.Close()

	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("parse test server url %q: %v", srv.URL, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse test server port %q: %v", portStr, err)
	}

	cfg := &config.Config{}
	cfg.Timeplus.Host = host
	cfg.Timeplus.Port = port

	done := make(chan struct{})
	go func() {
		ReconcileOrphanedHolds(cfg, holdregistry.New(), time.Now().UTC())
		close(done)
	}()

	select {
	case elapsed := <-firstQuery:
		if elapsed < reconcileSettleDelay {
			t.Errorf("first Timeplus query at %v, before settle delay %v — the sleep must precede the query", elapsed, reconcileSettleDelay)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReconcileOrphanedHolds never queried Timeplus")
	}
	<-done
}
