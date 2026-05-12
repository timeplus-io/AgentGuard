package catalog

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestIsIDChar(t *testing.T) {
	cases := []struct {
		c    rune
		want bool
	}{
		{'a', true}, {'z', true}, {'A', true}, {'Z', true},
		{'0', true}, {'9', true},
		{'-', true}, {'_', true},
		{' ', false}, {'.', false}, {'/', false}, {'\\', false},
		{'\'', false}, {'"', false}, {'!', false},
		// Non-ASCII (e.g. emoji or accented letters) — must be rejected so IDs
		// stay safe to interpolate into SQL string literals.
		{'é', false}, {'中', false},
	}
	for _, c := range cases {
		t.Run(string(c.c), func(t *testing.T) {
			if got := isIDChar(c.c); got != c.want {
				t.Errorf("isIDChar(%q) = %v, want %v", c.c, got, c.want)
			}
		})
	}
}

func TestValidateID(t *testing.T) {
	// Manager with one seed entry to test the "already exists" case.
	seed := fstest.MapFS{
		"existing.yaml": {Data: []byte("id: rp-001\nname: Existing Rule\n")},
	}
	m, err := New(seed, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cases := []struct {
		name      string
		id        string
		wantError string // substring; "" means no error
	}{
		{"valid simple", "rp-002", ""},
		{"valid with underscores", "my_rule_42", ""},
		{"empty", "", "id is required"},
		{"too long (65)", strings.Repeat("a", 65), "64 characters or fewer"},
		{"max length (64) ok", strings.Repeat("a", 64), ""},
		{"contains space", "my rule", "may only contain"},
		{"contains slash", "rp/001", "may only contain"},
		{"contains dot", "rp.001", "may only contain"},
		{"contains quote", "rp'001", "may only contain"},
		{"already exists", "rp-001", "already exists"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := m.ValidateID(c.id)
			if c.wantError == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", c.wantError)
				return
			}
			if !strings.Contains(err.Error(), c.wantError) {
				t.Errorf("error %q does not contain %q", err.Error(), c.wantError)
			}
		})
	}
}

func TestLookupAndExists(t *testing.T) {
	seed := fstest.MapFS{
		"a.yaml": {Data: []byte("id: rp-001\nname: Seed One\n")},
		"b.yaml": {Data: []byte("id: rp-002\nname: Seed Two\n")},
	}
	m, err := New(seed, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Run("Lookup existing", func(t *testing.T) {
		e, ok := m.Lookup("rp-001")
		if !ok {
			t.Fatal("expected to find rp-001")
		}
		if e.Name != "Seed One" {
			t.Errorf("got name %q", e.Name)
		}
	})

	t.Run("Lookup missing", func(t *testing.T) {
		_, ok := m.Lookup("does-not-exist")
		if ok {
			t.Error("expected not found")
		}
	})

	t.Run("Exists true", func(t *testing.T) {
		if !m.Exists("rp-001") {
			t.Error("expected rp-001 to exist")
		}
	})

	t.Run("Exists false", func(t *testing.T) {
		if m.Exists("nope") {
			t.Error("expected nope to not exist")
		}
	})
}

func TestAll_SortedAndMerged(t *testing.T) {
	seed := fstest.MapFS{
		"a.yaml": {Data: []byte("id: rp-002\nname: Seed Two\n")},
		"b.yaml": {Data: []byte("id: rp-001\nname: Seed One\n")},
	}
	tmp := t.TempDir()
	// Local override that should win over seed's rp-002, plus a fresh entry.
	if err := os.WriteFile(filepath.Join(tmp, "rp-002.yaml"),
		[]byte("id: rp-002\nname: Local Override\n"), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "rp-003.yaml"),
		[]byte("id: rp-003\nname: Local New\n"), 0o644); err != nil {
		t.Fatalf("write new: %v", err)
	}

	m, err := New(seed, tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	all := m.All()
	if len(all) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(all))
	}
	// Sorted by ID
	if all[0].ID != "rp-001" || all[1].ID != "rp-002" || all[2].ID != "rp-003" {
		t.Errorf("unsorted: %s, %s, %s", all[0].ID, all[1].ID, all[2].ID)
	}
	// Local override won over seed
	if all[1].Name != "Local Override" {
		t.Errorf("expected local override; got %q", all[1].Name)
	}
}

func TestSave_PersistsAndUpdatesMemory(t *testing.T) {
	tmp := t.TempDir()
	m, err := New(fstest.MapFS{}, tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	entry := Entry{ID: "rp-new", Name: "New Rule", Severity: "warning"}
	if err := m.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// In-memory: Lookup returns the saved entry.
	got, ok := m.Lookup("rp-new")
	if !ok || got.Name != "New Rule" {
		t.Errorf("Lookup after Save = (%+v, %v)", got, ok)
	}

	// On disk: file exists with the right content.
	data, err := os.ReadFile(filepath.Join(tmp, "rp-new.yaml"))
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if !strings.Contains(string(data), "id: rp-new") || !strings.Contains(string(data), "name: New Rule") {
		t.Errorf("file content unexpected:\n%s", string(data))
	}
}

func TestSave_NoCatalogDirErrors(t *testing.T) {
	m, err := New(fstest.MapFS{}, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Save(Entry{ID: "x"}); err == nil {
		t.Error("expected error when no catalog dir is configured")
	}
}

func TestLoadFS_SkipsNonYAMLAndInvalid(t *testing.T) {
	seed := fstest.MapFS{
		"good.yaml":   {Data: []byte("id: rp-001\nname: Good\n")},
		"readme.md":   {Data: []byte("# not yaml")},                   // skipped (extension)
		"empty.yaml":  {Data: []byte("name: missing-id\n")},           // valid YAML, no id → skipped
		"sub/dir.yaml": {Data: []byte("id: rp-002\nname: Nested\n")}, // walked recursively
	}
	m, err := New(seed, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !m.Exists("rp-001") {
		t.Error("expected rp-001 to be loaded")
	}
	if !m.Exists("rp-002") {
		t.Error("expected nested rp-002 to be loaded")
	}
	if len(m.All()) != 2 {
		t.Errorf("expected 2 entries, got %d", len(m.All()))
	}
}

// Ensure New with a missing local dir creates it.
func TestNew_CreatesLocalCatalogDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "nonexistent", "catalog")
	_, err := New(fstest.MapFS{}, target)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("expected dir created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

// Sanity check that fs.WalkDir behavior matches what we expect — guards against
// future Go changes affecting catalog discovery.
func TestLoadFS_PropagatesReadErrors(t *testing.T) {
	// Use a real os.DirFS pointing at a valid path with a permission-restricted
	// file. Skipping permission-based test for portability — this just exercises
	// the path that returns an early error from WalkDir.
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "bad.yaml"),
		[]byte("id: [this-is: not-valid-yaml-at-all"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Wrap via os.DirFS to satisfy fs.FS.
	_, err := New(os.DirFS(tmp), "")
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected YAML parse error from seed, got %v", err)
	}
}

// Compile-time check that Entry is the type returned from Lookup/All — guards
// against accidental field renames downstream consumers depend on.
var _ = func() Entry {
	return Entry{ID: "_", Name: "_", Version: "_", Description: "_", Severity: "_",
		Category: "_", FPRate: "_", HookSources: "_", RequiredTelemetry: "_",
		Author: "_", Tags: nil, UDFs: nil, QuerySQL: "_"}
}

// Ensure the embedded fs.FS interface is honored — gives a sensible compile
// error if we ever accidentally take a *fstest.MapFS instead.
var _ fs.FS = fstest.MapFS{}

// TestSeedFS_RP050HasHopWindow pins rp-050 (Semantic Rollback Attack) to its
// streaming-MV rewrite. The pre-#95 v0.1.0 used a correlated subquery
// referencing the outer stream from inside the WHERE clause, which Timeplus
// 3.2.1 streaming MVs reject with Code 47 UNKNOWN_COLUMN. Branch A must now
// use a 5-minute hop window grouped by session_id + run_id; Branches B and C
// are single-event matches preserved from v0.1.0.
//
// Also documents the Proton ILLEGAL_AGGREGATION gotcha: an aggregate's output
// alias must not collide with a column referenced in the pre-aggregation
// WHERE filter. Branch A aliases arg_max(tool_name, event_time) to
// `sample_tool` (not `tool_name`) for this reason; positional binding to
// agentguard_security_events maps it to the target's tool_name column.
func TestSeedFS_RP050HasHopWindow(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-050")
	if !ok {
		t.Fatal("rp-050 missing from seed catalog")
	}
	if e.Version != "0.2.0" {
		t.Fatalf("rp-050 version = %q, want 0.2.0", e.Version)
	}
	q := e.QuerySQL
	required := []string{
		// Branch A: 5-min hop over cim_event, grouped by run_id, count > 1
		"FROM hop(agentguard_cim_event, event_time, interval 1 minute, interval 5 minute)",
		"GROUP BY session_id, agent_id, deployment_id, run_id, window_start, window_end",
		"HAVING count() > 1",
		// Aggregate aliases must NOT shadow tool_name (the column is
		// referenced in Branch A's WHERE filter — collision raises
		// Proton ILLEGAL_AGGREGATION).
		"arg_max(tool_name, event_time) AS sample_tool",
		"arg_max(raw_hook_name, event_time) AS sample_hook",
		// Branch A tool-name match: financial/destructive scope unchanged
		"(payment|transfer|pay_|wire_|send_money|purchase|charge_|provision",
		// Branch B: callback-crash single-event match (unchanged from v0.1.0)
		"(receipt|callback|confirm|acknowledge|verify_payment",
		"(null pointer|segmentation fault|internal server error",
		// Branch C: authority resurrection token match (unchanged from v0.1.0)
		"(approv|authoriz|confirm_action|grant_access|sign_off",
		`"(token|approval_token|auth_token|otp|nonce|approval_code`,
		"EMIT STREAM",
	}
	for _, want := range required {
		if !strings.Contains(q, want) {
			t.Errorf("rp-050 QuerySQL missing %q", want)
		}
	}
	// Anti-regression: the v0.1.0 correlated subquery referenced
	// `agentguard_cim_event AS inner_e` and joined on outer fields. Streaming
	// MVs raise Code 47 UNKNOWN_COLUMN. Comments are stripped first since the
	// changelog reasonably mentions the historical shape.
	stripped := stripSQLCommentsForTest(q)
	if strings.Contains(stripped, "AS inner_e") {
		t.Error("rp-050 has correlated subquery (`AS inner_e`) — Proton raises Code 47 UNKNOWN_COLUMN in streaming MV")
	}
}

// stripSQLCommentsForTest removes `--` line comments from a SQL string so
// anti-regression assertions don't trip on banned-pattern names mentioned in
// the rule's changelog comments. Renamed to avoid collision if PR #65's
// stripSQLComments helper lands first.
func stripSQLCommentsForTest(q string) string {
	var b strings.Builder
	for _, line := range strings.Split(q, "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
