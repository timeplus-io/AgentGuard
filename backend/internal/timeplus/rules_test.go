package timeplus

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeRuleExec is a stand-in for the Timeplus surface used by the candidate-
// validation and replacement primitives. It records every Exec/Query SQL and
// lets a test decide errors per statement, so SQL construction and failure-
// cleanup ordering can be verified without a live Proton.
type fakeRuleExec struct {
	execs    []string
	queries  []string
	rows     []Row
	queryErr error
	execErr  func(sql string) error // optional: return an error for matching SQL
}

func (f *fakeRuleExec) Exec(sql string) error {
	f.execs = append(f.execs, sql)
	if f.execErr != nil {
		return f.execErr(sql)
	}
	return nil
}

func (f *fakeRuleExec) Query(sql string) ([]Row, error) {
	f.queries = append(f.queries, sql)
	return f.rows, f.queryErr
}

func wantContains(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("expected %q to contain %q", s, sub)
	}
}

func wantNotContains(t *testing.T, s, sub string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Errorf("expected %q NOT to contain %q", s, sub)
	}
}

func TestCandidateNames(t *testing.T) {
	sink, mv := candidateNames("rp-001", "deadbeef")
	if sink == mv {
		t.Fatal("sink and mv names must differ")
	}
	// dashes stripped (matching MVName), token included.
	for _, name := range []string{sink, mv} {
		if strings.Contains(name, "-") {
			t.Errorf("name %q should have dashes stripped", name)
		}
		wantContains(t, name, "rp001")
		wantContains(t, name, "deadbeef")
	}
}

func TestCandidateSinkDDL(t *testing.T) {
	ddl := candidateSinkDDL("ag_preflight_sink_x")
	wantContains(t, ddl, "CREATE STREAM ag_preflight_sink_x")
	// must NOT create the real stream.
	wantNotContains(t, ddl, "CREATE STREAM agentguard_security_events")
	// schema is derived from ddlSecurityEvents, so its columns are present.
	for _, col := range []string{"detected_at", "rule_id", "rule_name", "severity", "message", "event_data"} {
		wantContains(t, ddl, col)
	}
}

func TestCandidateAndReplaceMVDDL(t *testing.T) {
	cand := candidateMVDDL("ag_preflight_mv_x", "ag_preflight_sink_x", "SELECT 1")
	wantContains(t, cand, "CREATE MATERIALIZED VIEW ag_preflight_mv_x INTO ag_preflight_sink_x AS SELECT 1")
	wantNotContains(t, cand, "IF NOT EXISTS")

	repl := replaceMVDDL("rp-001", "SELECT 2")
	// real target, MVName mapping, no IF NOT EXISTS.
	wantContains(t, repl, "CREATE MATERIALIZED VIEW "+MVName("rp-001")+" INTO agentguard_security_events AS SELECT 2")
	wantNotContains(t, repl, "IF NOT EXISTS")
}

func TestDropNoDelaySQL(t *testing.T) {
	v := dropViewNoDelaySQL("mv_rule_rp001")
	wantContains(t, v, "DROP VIEW IF EXISTS mv_rule_rp001 NO DELAY")
	s := dropStreamNoDelaySQL("ag_preflight_sink_x")
	wantContains(t, s, "DROP STREAM IF EXISTS ag_preflight_sink_x NO DELAY")
}

func TestPreflightCandidate_SuccessOrdering(t *testing.T) {
	fake := &fakeRuleExec{}
	if err := preflightCandidate(fake, "rp-001", "SELECT 1", "tok"); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(fake.execs) != 4 {
		t.Fatalf("expected 4 statements, got %d: %v", len(fake.execs), fake.execs)
	}
	// create sink → create candidate MV → drop MV → drop sink.
	wantContains(t, fake.execs[0], "CREATE STREAM ag_preflight_sink_rp001_tok")
	wantContains(t, fake.execs[1], "CREATE MATERIALIZED VIEW ag_preflight_mv_rp001_tok INTO ag_preflight_sink_rp001_tok")
	wantContains(t, fake.execs[2], "DROP VIEW IF EXISTS ag_preflight_mv_rp001_tok NO DELAY")
	wantContains(t, fake.execs[3], "DROP STREAM IF EXISTS ag_preflight_sink_rp001_tok NO DELAY")
}

func TestPreflightCandidate_SinkCreateFailsStillCleansUp(t *testing.T) {
	fake := &fakeRuleExec{execErr: func(sql string) error {
		// The sink DDL is derived from ddlSecurityEvents (which has a leading
		// newline), so match by substring rather than prefix.
		if strings.Contains(sql, "CREATE STREAM") {
			return fmt.Errorf("boom sink")
		}
		return nil
	}}
	err := preflightCandidate(fake, "rp-001", "SELECT 1", "tok")
	if err == nil || !strings.Contains(err.Error(), "create sink") {
		t.Fatalf("want create-sink error, got %v", err)
	}
	// MV create skipped; cleanup of both still attempted (IF EXISTS makes them safe).
	if len(fake.execs) != 3 {
		t.Fatalf("expected sink-create + 2 cleanup drops, got %v", fake.execs)
	}
	wantContains(t, fake.execs[1], "DROP VIEW IF EXISTS")
	wantContains(t, fake.execs[2], "DROP STREAM IF EXISTS")
}

func TestPreflightCandidate_MVCreateFailsReturnsValidationError(t *testing.T) {
	fake := &fakeRuleExec{execErr: func(sql string) error {
		if strings.HasPrefix(sql, "CREATE MATERIALIZED VIEW") {
			return fmt.Errorf("syntax error")
		}
		return nil
	}}
	err := preflightCandidate(fake, "rp-001", "SELECT bad", "tok")
	if err == nil || !strings.Contains(err.Error(), "validate rule SQL") {
		t.Fatalf("want validation error, got %v", err)
	}
	// sink-create, mv-create(fail), drop-mv, drop-sink.
	if len(fake.execs) != 4 {
		t.Fatalf("expected 4 statements, got %v", fake.execs)
	}
}

func TestPreflightCandidate_CleanupFailureSurfacedOnlyOnSuccess(t *testing.T) {
	// Creates succeed, drops fail: with no validation error to preserve, the
	// cleanup failure must surface (a leaked sink/MV is worth reporting).
	fake := &fakeRuleExec{execErr: func(sql string) error {
		if strings.HasPrefix(sql, "DROP") {
			return fmt.Errorf("drop failed")
		}
		return nil
	}}
	err := preflightCandidate(fake, "rp-001", "SELECT 1", "tok")
	if err == nil || !strings.Contains(err.Error(), "cleanup") {
		t.Fatalf("want cleanup error surfaced, got %v", err)
	}
}

func TestPreflightCandidate_ValidationErrorNotMaskedByCleanupFailure(t *testing.T) {
	// MV create fails AND drops fail: the caller's real signal (validation) wins.
	fake := &fakeRuleExec{execErr: func(sql string) error {
		if strings.HasPrefix(sql, "CREATE MATERIALIZED VIEW") {
			return fmt.Errorf("syntax error")
		}
		if strings.HasPrefix(sql, "DROP") {
			return fmt.Errorf("drop failed")
		}
		return nil
	}}
	err := preflightCandidate(fake, "rp-001", "SELECT bad", "tok")
	if err == nil || !strings.Contains(err.Error(), "validate rule SQL") {
		t.Fatalf("validation error must not be masked by cleanup failure, got %v", err)
	}
}

func TestApplyUDFs_OrderAndStopOnError(t *testing.T) {
	fake := &fakeRuleExec{}
	if err := applyUDFs(fake, []string{"udf0", "udf1", "udf2"}); err != nil {
		t.Fatalf("applyUDFs: %v", err)
	}
	if len(fake.execs) != 3 || fake.execs[0] != "udf0" || fake.execs[2] != "udf2" {
		t.Fatalf("UDFs applied out of order or incomplete: %v", fake.execs)
	}

	fail := &fakeRuleExec{execErr: func(sql string) error {
		if sql == "udf1" {
			return fmt.Errorf("bad udf")
		}
		return nil
	}}
	err := applyUDFs(fail, []string{"udf0", "udf1", "udf2"})
	if err == nil || !strings.Contains(err.Error(), "apply UDF[1]") {
		t.Fatalf("want apply UDF[1] error, got %v", err)
	}
	// must stop after the failing statement — udf2 never runs.
	if len(fail.execs) != 2 {
		t.Fatalf("expected to stop at the failing UDF, ran: %v", fail.execs)
	}
}

func TestApplyUDFs_Empty(t *testing.T) {
	fake := &fakeRuleExec{}
	if err := applyUDFs(fake, nil); err != nil {
		t.Fatalf("applyUDFs(nil): %v", err)
	}
	if len(fake.execs) != 0 {
		t.Fatalf("expected no statements for empty UDFs, got %v", fake.execs)
	}
}

func TestFailedMVs_ScanAndQuery(t *testing.T) {
	fake := &fakeRuleExec{rows: []Row{
		{"name": "mv_rule_rp001", "state": "Crashed", "_tp_time": "2026-06-15 12:00:00.123456789"},
		{"name": "mv_rule_rp002", "state": "Aborted", "_tp_time": ""},
		{"name": "mv_rule_rp003", "state": "Crashed", "_tp_time": "garbage"}, // bad time
		{"name": "", "state": "Crashed", "_tp_time": ""},                     // skipped: no name
	}}
	got, err := failedMVs(fake)
	if err != nil {
		t.Fatalf("failedMVs: %v", err)
	}
	wantContains(t, fake.queries[0], "system.v_failed_mat_views")
	// scoped to rule MVs so other apps' failures are excluded.
	wantContains(t, fake.queries[0], "starts_with(name, 'mv_rule_')")

	if len(got) != 3 {
		t.Fatalf("expected 3 failed MVs (empty-name skipped), got %d: %#v", len(got), got)
	}
	one := got["mv_rule_rp001"]
	if one.State != "Crashed" {
		t.Errorf("state = %q, want Crashed", one.State)
	}
	wantTime := time.Date(2026, 6, 15, 12, 0, 0, 123456789, time.UTC)
	if !one.SeenAt.Equal(wantTime) {
		t.Errorf("SeenAt = %v, want %v", one.SeenAt, wantTime)
	}
	if two := got["mv_rule_rp002"]; !two.SeenAt.IsZero() {
		t.Errorf("blank _tp_time should yield zero time, got %v", two.SeenAt)
	}
	// a malformed (non-empty) timestamp degrades to zero time without dropping
	// the row or losing its state.
	three := got["mv_rule_rp003"]
	if three.State != "Crashed" || !three.SeenAt.IsZero() {
		t.Errorf("garbage _tp_time: got state=%q seenAt=%v, want Crashed + zero time", three.State, three.SeenAt)
	}
}

func TestFailedMVs_QueryError(t *testing.T) {
	fake := &fakeRuleExec{queryErr: fmt.Errorf("boom")}
	if _, err := failedMVs(fake); err == nil {
		t.Fatal("failedMVs should propagate query errors")
	}
}

func TestMVExists_ParsesCountShapes(t *testing.T) {
	cases := []struct {
		name string
		rows []Row
		want bool
	}{
		// count() is uint64; Proton FORMAT JSON renders it as a quoted string,
		// while Query (UseNumber) may surface it as json.Number — accept both.
		{"string one", []Row{{"cnt": "1"}}, true},
		{"string zero", []Row{{"cnt": "0"}}, false},
		{"json.Number five", []Row{{"cnt": json.Number("5")}}, true},
		{"json.Number zero", []Row{{"cnt": json.Number("0")}}, false},
		{"empty rows", nil, false},
		{"unexpected type", []Row{{"cnt": 1}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeRuleExec{rows: tc.rows}
			got, err := mvExists(fake, "rp-001")
			if err != nil {
				t.Fatalf("mvExists: %v", err)
			}
			if got != tc.want {
				t.Errorf("mvExists = %v, want %v", got, tc.want)
			}
			// The query is scoped to the dash-stripped MV name in the active db.
			wantContains(t, fake.queries[0], "mv_rule_rp001")
			wantContains(t, fake.queries[0], "current_database()")
		})
	}
}

func TestMVExists_QueryError(t *testing.T) {
	fake := &fakeRuleExec{queryErr: fmt.Errorf("boom")}
	if _, err := mvExists(fake, "rp-001"); err == nil {
		t.Fatal("mvExists should propagate query errors")
	}
}
