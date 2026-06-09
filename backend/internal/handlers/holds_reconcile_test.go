package handlers

import (
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

// The `AND status = 'pending'` guard skips the write when the hold has already
// committed a terminal state: every caller transitions pending→terminal, so a
// late 'abandoned' write (startup reconcile, session-end drain, rule-disable)
// finds no pending row and no-ops instead of clobbering a decision an operator
// just made. It is NOT a concurrency-atomic compare-and-set — streams are
// append-only and these reads are flush-lagged, so single-writer-per-hold is
// enforced by the holdregistry claim; this guard is the cross-restart /
// cross-process backstop on top of it.
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
	origInterval, origAttempts := reconcileRetryInterval, reconcileMaxAttempts
	reconcileRetryInterval = time.Millisecond
	reconcileMaxAttempts = 3
	defer func() { reconcileRetryInterval, reconcileMaxAttempts = origInterval, origAttempts }()

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
