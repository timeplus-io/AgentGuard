package rules

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/timeplus/agentguard/internal/timeplus"
)

// fakeQuerier is an in-package stand-in for the Timeplus HTTP surface. It
// records every SQL string it is handed and returns canned rows / errors, so
// the store's SQL construction and row scanning can be verified without a live
// Proton.
type fakeQuerier struct {
	queries  []string
	execs    []string
	rows     []timeplus.Row
	queryErr error
	execErr  error
}

func (f *fakeQuerier) Query(sql string) ([]timeplus.Row, error) {
	f.queries = append(f.queries, sql)
	return f.rows, f.queryErr
}

func (f *fakeQuerier) Exec(sql string) error {
	f.execs = append(f.execs, sql)
	return f.execErr
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("expected SQL to contain %q\n--- SQL ---\n%s", sub, s)
	}
}

func mustNotContain(t *testing.T, s, sub string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Errorf("expected SQL NOT to contain %q\n--- SQL ---\n%s", sub, s)
	}
}

// sampleRow builds a complete result row in the shape timeplus.Query yields:
// revision is a quoted string (Proton renders uint64 as a JSON string),
// booleans are Go bools, updated_at is a timezone-less timestamp string.
func sampleRow(id, revision string, deleted bool) timeplus.Row {
	return timeplus.Row{
		"rule_id":                  id,
		"snapshot_id":              "snap-" + id,
		"max_revision":             revision, // projection aliases max(revision) AS max_revision
		"name":                     "Name " + id,
		"definition_version":       "1.0.0",
		"definition_hash":          "hash-" + id,
		"description":              "desc " + id,
		"rendered_sql":             "SELECT 1",
		"deployed_udfs_json":       "[]",
		"parameter_overrides_json": "{}",
		"enabled":                  true,
		"deleted":                  deleted,
		"severity":                 "high",
		"category":                 "exfiltration",
		"required_telemetry":       "hook",
		"fp_rate":                  "low",
		"hook_sources":             "before_tool_call",
		"block_policy":             "log_only",
		"update_reason":            "install",
		"updated_by":               "alice",
		"updated_at":               "2026-06-15 12:00:00.123456789",
	}
}

func TestListCurrent_SQLAndScan(t *testing.T) {
	fake := &fakeQuerier{rows: []timeplus.Row{
		sampleRow("rp_001", "7", false),
		sampleRow("rp_002", "3", false),
	}}
	s := &Store{q: fake}

	got, err := s.ListCurrent()
	if err != nil {
		t.Fatalf("ListCurrent: %v", err)
	}

	sql := fake.queries[0]
	mustContain(t, sql, "FROM table(agentguard_rules)")
	mustContain(t, sql, "max(revision) AS max_revision")
	mustContain(t, sql, "arg_max(rendered_sql, revision)")
	mustContain(t, sql, "arg_max(deployed_udfs_json, revision)")
	mustContain(t, sql, "arg_max(parameter_overrides_json, revision)")
	mustContain(t, sql, "GROUP BY rule_id")
	// Deleted rows are filtered in an OUTER scope, never HAVING: a HAVING
	// arg_max(deleted, revision) would nest aggregates (the `deleted` arg resolves
	// to the same-named SELECT alias) → ILLEGAL_AGGREGATION on stricter timeplusd
	// (3.2.11). The grouped projection is wrapped and its plain `deleted` column
	// filtered instead.
	mustContain(t, sql, ") WHERE deleted = false")
	mustNotContain(t, sql, "HAVING")
	// Ordering must be by revision only — nothing keyed on updated_at. Every
	// arg_max uses ", revision)", so ", updated_at)" must never appear.
	mustNotContain(t, sql, ", updated_at)")

	if len(got) != 2 {
		t.Fatalf("ListCurrent returned %d rows, want 2", len(got))
	}
	if got[0].RuleID != "rp_001" || got[0].Revision != 7 {
		t.Errorf("row 0 = %q rev %d, want rp_001 rev 7", got[0].RuleID, got[0].Revision)
	}
	if got[1].RuleID != "rp_002" || got[1].Revision != 3 {
		t.Errorf("row 1 = %q rev %d, want rp_002 rev 3", got[1].RuleID, got[1].Revision)
	}
	if got[0].Severity != "high" || got[0].BlockPolicy != "log_only" || got[0].UpdatedBy != "alice" {
		t.Errorf("row 0 scalar fields not scanned: %+v", got[0])
	}
}

func TestListCurrent_QueryError(t *testing.T) {
	fake := &fakeQuerier{queryErr: errFake}
	s := &Store{q: fake}
	if _, err := s.ListCurrent(); err == nil {
		t.Fatal("ListCurrent should propagate query errors")
	}
}

func TestLatestSnapshot_NotFound(t *testing.T) {
	fake := &fakeQuerier{rows: nil}
	s := &Store{q: fake}
	_, found, err := s.LatestSnapshot("rp_404")
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if found {
		t.Fatal("found should be false when the rule has no snapshots")
	}
}

// A reinstall must continue the revision sequence rather than reset to 1, so the
// single-rule read must surface the latest snapshot even when it is the soft-
// delete (deleted=true) row. This locks in revision monotonicity across an
// uninstall→reinstall cycle.
func TestLatestSnapshot_ReturnsDeletedSnapshotForMonotonicity(t *testing.T) {
	fake := &fakeQuerier{rows: []timeplus.Row{sampleRow("rp_001", "4", true)}}
	s := &Store{q: fake}

	inst, found, err := s.LatestSnapshot("rp_001")
	if err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	if !found {
		t.Fatal("found should be true even when the latest snapshot is deleted")
	}
	if !inst.Deleted {
		t.Error("latest snapshot Deleted flag not surfaced")
	}
	if inst.Revision != 4 {
		t.Errorf("Revision = %d, want 4 (so the next revision is 5, not 1)", inst.Revision)
	}

	sql := fake.queries[0]
	mustContain(t, sql, "WHERE rule_id = 'rp_001'")
	mustContain(t, sql, "GROUP BY rule_id")
	// Must NOT filter deleted out — that is the whole point of this read.
	mustNotContain(t, sql, "HAVING")
	mustNotContain(t, sql, "deleted =")
}

func TestLatestSnapshot_EscapesRuleID(t *testing.T) {
	fake := &fakeQuerier{rows: nil}
	s := &Store{q: fake}
	if _, _, err := s.LatestSnapshot("rp'001"); err != nil {
		t.Fatalf("LatestSnapshot: %v", err)
	}
	mustContain(t, fake.queries[0], "WHERE rule_id = 'rp''001'")
}

func TestAppend_AllColumnsEscapedAndTyped(t *testing.T) {
	fake := &fakeQuerier{}
	s := &Store{q: fake}

	inst := RuleInstance{
		RuleID:            "rp_x",
		SnapshotID:        "snap1",
		Revision:          12,
		Name:              `O'Brien \ path`,
		DefinitionVersion: "2.1.0",
		DefinitionHash:    "abc123",
		Description:       "desc",
		RenderedSQL:       "SELECT 1",
		DeployedUDFs:      []string{"CREATE OR REPLACE FUNCTION f RETURNS bool"},
		Overrides:         map[string]any{"threshold": json.Number("15"), "note": "it's fine"},
		Enabled:           true,
		Deleted:           false,
		Severity:          "critical",
		Category:          "exfiltration",
		RequiredTelemetry: "hook",
		FPRate:            "low",
		HookSources:       "before_tool_call",
		BlockPolicy:       "auto_block",
		UpdateReason:      "configure",
		UpdatedBy:         "alice",
	}
	if err := s.Append(inst); err != nil {
		t.Fatalf("Append: %v", err)
	}

	sql := fake.execs[0]
	mustContain(t, sql, "INSERT INTO agentguard_rules (")
	for _, col := range []string{
		"rule_id", "snapshot_id", "revision", "name", "definition_version",
		"definition_hash", "description", "rendered_sql", "deployed_udfs_json",
		"parameter_overrides_json", "enabled", "deleted", "severity", "category",
		"required_telemetry", "fp_rate", "hook_sources", "block_policy",
		"update_reason", "updated_by", "updated_at",
	} {
		mustContain(t, sql, col)
	}
	// revision is a bare decimal, not a quoted literal.
	mustContain(t, sql, ",12,")
	mustNotContain(t, sql, "'12'")
	// booleans rendered as true/false (enabled=true, deleted=false are adjacent).
	mustContain(t, sql, "true,false,")
	// updated_at is DB-stamped.
	mustContain(t, sql, "now64(9))")
	// string escaping: backslash doubled, single quote doubled.
	mustContain(t, sql, `O''Brien \\ path`)
	// JSON columns: integer override is unquoted; single quote inside the JSON
	// value is SQL-escaped; UDF list is a JSON array.
	mustContain(t, sql, `"threshold":15`)
	mustContain(t, sql, `it''s fine`)
	mustContain(t, sql, `["CREATE OR REPLACE FUNCTION f RETURNS bool"]`)
}

func TestAppend_EmptyOverridesAndUDFsBecomeValidJSON(t *testing.T) {
	fake := &fakeQuerier{}
	s := &Store{q: fake}
	if err := s.Append(RuleInstance{RuleID: "rp_y", Revision: 1}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	sql := fake.execs[0]
	mustContain(t, sql, "'{}'") // empty overrides
	mustContain(t, sql, "'[]'") // empty udfs
}

func TestAppend_ExecError(t *testing.T) {
	fake := &fakeQuerier{execErr: errFake}
	s := &Store{q: fake}
	if err := s.Append(RuleInstance{RuleID: "rp_z", Revision: 1}); err == nil {
		t.Fatal("Append should propagate exec errors")
	}
}

func TestScanInstance_RoundTrip(t *testing.T) {
	row := sampleRow("rp_001", "7", false)
	row["deployed_udfs_json"] = `["CREATE OR REPLACE FUNCTION f"]`
	row["parameter_overrides_json"] = `{"k":"v","n":15}`

	inst, err := scanInstance(row)
	if err != nil {
		t.Fatalf("scanInstance: %v", err)
	}
	if inst.RuleID != "rp_001" || inst.Revision != 7 {
		t.Errorf("identity wrong: %q rev %d", inst.RuleID, inst.Revision)
	}
	if !inst.Enabled || inst.Deleted {
		t.Errorf("flags wrong: enabled=%v deleted=%v", inst.Enabled, inst.Deleted)
	}
	if len(inst.DeployedUDFs) != 1 || inst.DeployedUDFs[0] != "CREATE OR REPLACE FUNCTION f" {
		t.Errorf("DeployedUDFs wrong: %#v", inst.DeployedUDFs)
	}
	if inst.Overrides["k"] != "v" {
		t.Errorf("Overrides[k] = %#v, want \"v\"", inst.Overrides["k"])
	}
	// Integer override values must survive as json.Number (not float64), per the
	// package's UseNumber JSON-consumer contract.
	if n, ok := inst.Overrides["n"].(json.Number); !ok || n.String() != "15" {
		t.Errorf("Overrides[n] = %#v (%T), want json.Number 15", inst.Overrides["n"], inst.Overrides["n"])
	}
	want := time.Date(2026, 6, 15, 12, 0, 0, 123456789, time.UTC)
	if !inst.UpdatedAt.Equal(want) {
		t.Errorf("UpdatedAt = %v, want %v", inst.UpdatedAt, want)
	}
}

func TestScanInstance_BadJSONErrors(t *testing.T) {
	row := sampleRow("rp_bad", "1", false)
	row["parameter_overrides_json"] = "{not json"
	if _, err := scanInstance(row); err == nil {
		t.Fatal("scanInstance should error on malformed parameter_overrides_json")
	}
}

func TestScanInstance_BadRevisionErrors(t *testing.T) {
	row := sampleRow("rp_bad", "not-a-number", false)
	if _, err := scanInstance(row); err == nil {
		t.Fatal("scanInstance should error on an unparseable revision")
	}
}

func TestScanInstance_MissingRevisionErrors(t *testing.T) {
	row := sampleRow("rp_bad", "1", false)
	delete(row, "max_revision")
	if _, err := scanInstance(row); err == nil {
		t.Fatal("scanInstance should error on a missing revision column")
	}
}

func TestRowUint64(t *testing.T) {
	valid := []struct {
		in   any
		want uint64
	}{
		{"7", 7},
		{" 42 ", 42},
		{json.Number("99"), 99},
		{float64(5), 5},
	}
	for _, c := range valid {
		got, err := rowUint64(c.in)
		if err != nil || got != c.want {
			t.Errorf("rowUint64(%#v) = %d, %v; want %d, nil", c.in, got, err, c.want)
		}
	}
	// revision is load-bearing: malformed/missing values must error, not zero.
	invalid := []any{"", nil, "-3", "abc", float64(-1), float64(1.5), true}
	for _, c := range invalid {
		if got, err := rowUint64(c); err == nil {
			t.Errorf("rowUint64(%#v) = %d, nil; want error", c, got)
		}
	}
}

func TestRowBool(t *testing.T) {
	cases := []struct {
		in   any
		want bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"1", true},
		{"0", false},
		{json.Number("1"), true},
		{json.Number("0"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := rowBool(c.in); got != c.want {
			t.Errorf("rowBool(%#v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestRowTime(t *testing.T) {
	want := time.Date(2026, 6, 15, 12, 0, 0, 123456789, time.UTC)
	if got := rowTime("2026-06-15 12:00:00.123456789"); !got.Equal(want) {
		t.Errorf("rowTime = %v, want %v", got, want)
	}
	if got := rowTime(""); !got.IsZero() {
		t.Errorf("rowTime(\"\") = %v, want zero", got)
	}
	if got := rowTime(nil); !got.IsZero() {
		t.Errorf("rowTime(nil) = %v, want zero", got)
	}
}

func TestMarshalJSONHelpers(t *testing.T) {
	if got, _ := marshalOverridesJSON(nil); got != "{}" {
		t.Errorf("marshalOverridesJSON(nil) = %q, want {}", got)
	}
	if got, _ := marshalOverridesJSON(map[string]any{}); got != "{}" {
		t.Errorf("marshalOverridesJSON(empty) = %q, want {}", got)
	}
	if got, _ := marshalUDFsJSON(nil); got != "[]" {
		t.Errorf("marshalUDFsJSON(nil) = %q, want []", got)
	}
	if got, _ := marshalUDFsJSON([]string{}); got != "[]" {
		t.Errorf("marshalUDFsJSON(empty) = %q, want []", got)
	}
}

var errFake = fakeError("boom")

type fakeError string

func (e fakeError) Error() string { return string(e) }
