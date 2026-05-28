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
		"good.yaml":    {Data: []byte("id: rp-001\nname: Good\n")},
		"readme.md":    {Data: []byte("# not yaml")},                 // skipped (extension)
		"empty.yaml":   {Data: []byte("name: missing-id\n")},         // valid YAML, no id → skipped
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

// TestSeedFS_NoUnsupportedStreamingSQL guards against re-introducing query patterns
// that Timeplus 3.2.1 streaming materialized views do not support. Rules using
// these patterns fail at install time with Code 48 NOT_IMPLEMENTED (see issue #89,
// #88, #95). When such a pattern is needed for detection semantics, the rule
// belongs in docs/research/failed/ until rewritten — not in seed/.
func TestSeedFS_NoUnsupportedStreamingSQL(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	entries := m.All()
	if len(entries) == 0 {
		t.Fatal("seed catalog loaded zero entries")
	}

	// Patterns the streaming MV engine rejects (Code 48 NOT_IMPLEMENTED).
	// `ROWS BETWEEN` / `RANGE BETWEEN` window frames are the canonical case.
	bannedPatterns := []string{
		"ROWS BETWEEN",
		"RANGE BETWEEN",
	}
	for _, e := range entries {
		// Strip SQL line comments — changelogs often mention the banned
		// patterns when explaining why they were removed.
		var stripped strings.Builder
		for _, line := range strings.Split(e.QuerySQL, "\n") {
			if idx := strings.Index(line, "--"); idx >= 0 {
				line = line[:idx]
			}
			stripped.WriteString(line)
			stripped.WriteByte('\n')
		}
		upper := strings.ToUpper(stripped.String())
		for _, pat := range bannedPatterns {
			if strings.Contains(upper, pat) {
				t.Errorf("rule %s (%s) uses %q which is not supported in streaming MVs (Code 48 NOT_IMPLEMENTED); move to docs/research/failed/ or rewrite", e.ID, e.Name, pat)
			}
		}
	}
}

// TestSeedFS_HasRP013 guards against rp-013 silently disappearing from the
// catalog. The v0.4.0 rewrite resolves issue #89 by moving the rule from
// docs/research/failed/ back into seed/ with a streaming-MV-compatible query.
func TestSeedFS_HasRP013(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-013")
	if !ok {
		t.Fatal("rp-013 missing from seed catalog")
	}
	if e.Name == "" || e.QuerySQL == "" {
		t.Errorf("rp-013 has empty Name or QuerySQL: %+v", e)
	}
}

// TestSeedFS_RP013HasWindowFallback keeps rp-013 from regressing back to the
// temporary absolute-threshold-only shape. The rule should preserve relative and
// sustained spike coverage through streaming-compatible time-window aggregation.
func TestSeedFS_RP013HasWindowFallback(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-013")
	if !ok {
		t.Fatal("rp-013 missing from seed catalog")
	}
	if e.Version != "0.4.0" {
		t.Fatalf("rp-013 version = %q, want 0.4.0", e.Version)
	}
	if !strings.Contains(e.RequiredTelemetry, "llm.input_tokens") ||
		!strings.Contains(e.RequiredTelemetry, "llm.output_tokens") {
		t.Fatalf("rp-013 RequiredTelemetry = %q, want input and output token metrics", e.RequiredTelemetry)
	}

	query := e.QuerySQL
	required := []string{
		// Branch A is tumble-bounded (load-bearing — see SQL changelog).
		"FROM tumble(agentguard_cim_metrics, event_time, interval 5 minute)",
		// Branch B uses hop() so spikes near a 5-min boundary are still covered.
		"FROM hop(agentguard_cim_metrics, event_time, interval 1 minute, interval 5 minute)",
		"max_if(metric_value, metric_name = 'llm.output_tokens')",
		"count_if(metric_name = 'llm.output_tokens' AND metric_value > 3000)",
		"sum_if(metric_value, metric_name = 'llm.input_tokens') < 4000",
	}
	for _, want := range required {
		if !strings.Contains(query, want) {
			t.Fatalf("rp-013 QuerySQL missing %q", want)
		}
	}
}

// TestSeedFS_RP013SemanticScenarios documents the detection scenarios the
// v0.4.0 streaming rewrite must cover and locks in the SQL invariants for
// each. SQL invariants are not the same as behavior — the live MV behavior is
// verified separately against Proton (see the PR description's run log).
// These subtests guard against silent edits that would narrow or widen the
// detection envelope without anyone noticing.
func TestSeedFS_RP013SemanticScenarios(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-013")
	if !ok {
		t.Fatal("rp-013 missing from seed catalog")
	}
	query := e.QuerySQL

	mustContain := func(t *testing.T, needle, why string) {
		t.Helper()
		if !strings.Contains(query, needle) {
			t.Errorf("%s\n  expected SQL to contain: %s", why, needle)
		}
	}

	t.Run("absolute_spike_branch_A", func(t *testing.T) {
		// An LLM response with >8000 output tokens and <4000 input tokens
		// must fire. This is the strongest, lowest-FP signal — preserved
		// from v0.3.0 modulo the run_id grouping change.
		mustContain(t, "sum_if(metric_value, metric_name = 'llm.output_tokens') > 8000",
			"Branch A must enforce the absolute >8000 output-token threshold")
		mustContain(t, "sum_if(metric_value, metric_name = 'llm.input_tokens') < 4000",
			"Branch A must enforce the input-guard <4000 to suppress codegen sessions")
		mustContain(t, "tags['run_id']",
			"Branch A must group by run_id so aggregation is bounded to a single LLM call")
	})

	t.Run("branch_A_bounded_emission_via_tumble", func(t *testing.T) {
		// Branch A must be wrapped in a 5-minute tumble window so the
		// streaming GROUP BY does not re-emit qualifying run_ids on every
		// checkpoint (the O(N^2) emission bug surfaced by the live MV test
		// against Timeplus Enterprise 3.2.1 — see the rule's SQL changelog).
		mustContain(t, "FROM tumble(agentguard_cim_metrics, event_time, interval 5 minute)",
			"Branch A must use a 5-minute tumble window to bound emission")
		// run_id must remain in the Branch A GROUP BY for per-run scoping.
		mustContain(t, "GROUP BY agent_id, deployment_id, session_id, tags['run_id'], tags['tool_name'], window_start, window_end",
			"Branch A GROUP BY must include run_id AND window bounds")
	})

	t.Run("branch_B_uses_hop_for_boundary_coverage", func(t *testing.T) {
		// Branch B uses hop(... slide=1m, window=5m) so a qualifying spike or
		// sustained stream that straddles a 5-minute boundary is still covered
		// by overlapping windows. Argument order is hop(stream, ts, slide,
		// window) per src/TableFunctions/TableFunctionHop.cpp. Each event lives
		// in up to window/slide = 5 overlapping windows, so Branch B can emit
		// up to 5 detection rows per qualifying spike — bounded duplicate
		// emission that mv_notify_filter collapses for end-user alerts.
		mustContain(t, "FROM hop(agentguard_cim_metrics, event_time, interval 1 minute, interval 5 minute)",
			"Branch B must use hop(slide=1m, window=5m)")
		// hop() must NOT replace Branch A — Branch A is still tumble-bounded.
		// (Branch A and Branch B share the agentguard_cim_metrics source so
		// the count check is on the full FROM phrase.)
		if c := strings.Count(query, "FROM tumble(agentguard_cim_metrics"); c != 1 {
			t.Errorf("expected exactly one tumble() (Branch A); got %d", c)
		}
		if c := strings.Count(query, "FROM hop(agentguard_cim_metrics"); c != 1 {
			t.Errorf("expected exactly one hop() (Branch B); got %d", c)
		}
	})

	t.Run("codegen_suppression_both_branches", func(t *testing.T) {
		// A large output paired with a large input (legitimate codegen) must
		// NOT fire. Both branches need an input-token guard:
		//   Branch A: sum_if(input) < 4000 (per run, so input is the run's own input).
		//   Branch B: max_if(input) < 4000 (per window — using max, not avg,
		//     so a single high-input codegen call is not diluted to passing
		//     by surrounding low-input baseline calls in the window).
		// avg_if would let one 10000/5000 codegen call inside 5 baseline
		// 500/500 calls slip through (avg = 1625) — that case was
		// live-reproduced on 2026-05-11 and is now prevented by max_if.
		mustContain(t, "sum_if(metric_value, metric_name = 'llm.input_tokens') < 4000",
			"Branch A missing sum-based input guard — codegen suppression broken")
		mustContain(t, "max_if(metric_value, metric_name = 'llm.input_tokens') < 4000",
			"Branch B must use max_if (not avg_if) so codegen calls cannot be diluted by baseline traffic")
		// Explicit anti-regression: avg_if input guard must not come back.
		if strings.Contains(query, "avg_if(metric_value, metric_name = 'llm.input_tokens') < 4000") {
			t.Error("Branch B input guard regressed to avg_if — dilutes high-input codegen calls against baseline traffic; use max_if")
		}
	})

	t.Run("relative_spike_5x_branch_B", func(t *testing.T) {
		// Within a 5-minute window, max output > 5x mean of the OTHER outputs
		// in that same window. v0.3.0 used a rolling 10-call baseline; this
		// version uses a bucket-relative baseline (sum - max) / (count - 1)
		// computed inside each hop window.
		mustContain(t, "FROM hop(agentguard_cim_metrics, event_time, interval 1 minute, interval 5 minute)",
			"Branch B must use hop(slide=1m, window=5m)")
		mustContain(t, "* 5",
			"Branch B must enforce a 5x ratio between max and baseline")
		mustContain(t,
			"(sum_if(metric_value, metric_name = 'llm.output_tokens') - max_if(metric_value, metric_name = 'llm.output_tokens')) / (count_if(metric_name = 'llm.output_tokens') - 1)",
			"Branch B baseline must be the mean of non-max outputs in the window")
	})

	t.Run("sustained_3_high_outputs_branch_B", func(t *testing.T) {
		// >=3 output values exceed 3000 tokens in the same 5-minute window.
		// v0.3.0 required 3 *consecutive* high-output calls; v0.4.0 fires on
		// 3 high-output calls in the same hop window, not necessarily
		// consecutive. Boundary-split coverage comes from the 1-min hop slide.
		mustContain(t, "count_if(metric_name = 'llm.output_tokens' AND metric_value > 3000) >= 3",
			"Branch B sustained must fire on >=3 outputs >3000 tokens in the same hop window")
	})

}

func TestSeedFS_RP018EventWindowRewrite(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-018")
	if !ok {
		t.Fatal("rp-018 missing from seed catalog")
	}
	if e.Version != "0.3.0" {
		t.Fatalf("rp-018 version = %q, want 0.3.0", e.Version)
	}
	if strings.Contains(e.RequiredTelemetry, "llm.total_tokens") {
		t.Fatalf("rp-018 RequiredTelemetry still includes llm.total_tokens: %q", e.RequiredTelemetry)
	}
	if len(e.UDFs) == 0 || !strings.Contains(strings.Join(e.UDFs, "\n"), "ag_rp018_max_tool_calls") {
		t.Fatal("rp-018 missing ag_rp018_max_tool_calls aggregate UDF")
	}

	q := e.QuerySQL
	required := []string{
		"WITH unified_run_events AS",
		"UNION ALL",
		"FROM agentguard_cim_event",
		"FROM agentguard_cim_metrics",
		"metric_name IN ('llm.input_tokens', 'llm.output_tokens', 'tool.call')",
		"sum_if(metric_value, metric_name = 'llm.input_tokens')",
		"sum_if(metric_value, metric_name = 'llm.output_tokens')",
		"SHUFFLE BY agent_id, deployment_id, session_id, run_id",
		"GROUP BY agent_id, deployment_id, session_id, run_id",
		"AND ag_rp018_max_tool_calls(metric_name, metric_tool_name) > 15",
		"AND max(content_signal_hit) = 1",
		"EMIT AFTER SESSION CLOSE",
		"IDENTIFIED BY (event_time, true, is_session_end)",
		"WITH MAXSPAN 15m AND TIMEOUT 15m",
		"SETTINGS default_hash_table = 'hybrid'",
	}
	for _, want := range required {
		if !strings.Contains(q, want) {
			t.Errorf("rp-018 QuerySQL missing %q", want)
		}
	}

	stripped := stripSQLComments(q)
	forbidden := []string{
		"now() - INTERVAL",
		"llm.total_tokens",
		"LEFT JOIN agentguard_cim_metrics",
		"HAVING total_tokens > 40000",
	}
	for _, bad := range forbidden {
		if strings.Contains(stripped, bad) {
			t.Errorf("rp-018 QuerySQL still contains forbidden pre-v0.3.0 pattern %q", bad)
		}
	}
}

// TestSeedFS_RP047HasHopWindow pins rp-047 (LLM Model Substitution) to its
// streaming-MV rewrite. The pre-#88 v0.1.1 used `OVER (... ROWS BETWEEN N
// PRECEDING AND M PRECEDING)` window frames that streaming MVs reject with
// Code 48 NOT_IMPLEMENTED. Branch 1 must now use a 5-minute hop window with
// a same-window max/min latency comparison. Branch 2 is a single-event match.
func TestSeedFS_RP047HasHopWindow(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-047")
	if !ok {
		t.Fatal("rp-047 missing from seed catalog")
	}
	if e.Version != "0.2.1" {
		t.Fatalf("rp-047 version = %q, want 0.2.1", e.Version)
	}
	q := e.QuerySQL
	required := []string{
		// Branch 1: 5-min hop over cim_metrics
		"FROM hop(agentguard_cim_metrics, event_time, interval 1 minute, interval 5 minute)",
		// Same-window collapse-detection: baseline + collapsed + 5x ratio
		"HAVING max(metric_value) > 2000",
		// Zero-guard: `min < 300` admits min=0; the SELECT divides
		// max/min, and to_int64(inf) throws CANNOT_CONVERT_TYPE
		// (proton-enterprise FunctionsConversion.h:291-303). Block
		// failure, not garbled output.
		"min(metric_value) > 0",
		"min(metric_value) < 300",
		"max(metric_value) > min(metric_value) * 5",
		// Branch 2: single-event over cim_event (preserved from v0.1.1)
		"FROM agentguard_cim_event",
		"event_type IN ('llm_request', 'llm_response')",
		"EMIT STREAM",
	}
	for _, want := range required {
		if !strings.Contains(q, want) {
			t.Errorf("rp-047 QuerySQL missing %q", want)
		}
	}
	// Regex word-boundaries in Branch 2 must be `\\b` so the SQL string-
	// literal parser does not turn them into backspace (0x08). See the
	// rp-102/rp-123 rewrite for the same fix in larger volume.
	if strings.Contains(q, `'\b(proxy|gateway`) {
		t.Error("rp-047 has unescaped single-backslash \\b regex word boundary — replace with \\\\b")
	}
}

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
		// Branch A's event_data must carry window_start. With a 1m/5m hop, each
		// duplicate pair lands in up to 5 overlapping windows; without
		// window_start the resulting rows are byte-identical and operators see
		// 5 indistinguishable threats per attack.
		"' window_start=', to_string(window_start)",
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
	if strings.Contains(stripSQLComments(q), "AS inner_e") {
		t.Error("rp-050 has correlated subquery (`AS inner_e`) — Proton raises Code 47 UNKNOWN_COLUMN in streaming MV")
	}
}

func TestSeedFS_Issue43RulesUseJSAggregateUDFs(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}

	cases := []struct {
		id           string
		version      string
		udfName      string
		queryUse     string
		antiSnippets []string
	}{
		{"rp-023", "0.4.0", "ag_rp023_privilege_fsm", "HAVING ag_rp023_privilege_fsm(", []string{"lag(event_type)", "prev_event_type"}},
		{"rp-044", "0.4.0", "ag_rp044_clawworm_fsm", "HAVING ag_rp044_clawworm_fsm(", []string{"lags(event_type", "prev2_events"}},
		{"rp-066", "0.2.0", "ag_rp066_skillinject_fsm", "HAVING ag_rp066_skillinject_fsm(", []string{".env\\b"}},
		{"rp-076", "0.2.0", "ag_rp076_cfh_fsm", "HAVING ag_rp076_cfh_fsm(", []string{"Branch D: Code-execution tool invoked immediately"}},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			e, ok := m.Lookup(c.id)
			if !ok {
				t.Fatalf("%s missing from seed catalog", c.id)
			}
			if e.Version != c.version {
				t.Fatalf("%s version = %q, want %q", c.id, e.Version, c.version)
			}
			if len(e.UDFs) == 0 {
				t.Fatalf("%s has no UDF definitions", c.id)
			}
			joinedUDFs := strings.Join(e.UDFs, "\n")
			requiredUDFSnippets := []string{
				"CREATE OR REPLACE AGGREGATE FUNCTION " + c.udfName,
				"LANGUAGE JAVASCRIPT",
				"serialize: function()",
				"deserialize: function(state_str)",
				"merge: function(state_str)",
				"this.emitted",
			}
			for _, want := range requiredUDFSnippets {
				if !strings.Contains(joinedUDFs, want) {
					t.Errorf("%s UDF missing %q", c.id, want)
				}
			}
			if !strings.Contains(e.QuerySQL, c.queryUse) {
				t.Errorf("%s QuerySQL missing %q", c.id, c.queryUse)
			}
			sqlWithoutComments := stripSQLComments(e.QuerySQL)
			for _, forbidden := range c.antiSnippets {
				if strings.Contains(sqlWithoutComments, forbidden) {
					t.Errorf("%s QuerySQL still contains pre-UDA snippet %q", c.id, forbidden)
				}
			}
		})
	}
}

func TestSeedFS_FSMRulesMergeCorrectness(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}

	cases := []struct {
		id             string
		requiredFields []string
		forbiddenFlags []string
	}{
		{"rp-076", []string{"false_status_ts", "code_exec_ts"}, []string{"seen_false_status", "seen_code_exec"}},
		{"rp-066", []string{"injected_skill_ts", "sensitive_access_ts"}, []string{"seen_injected_skill", "seen_sensitive_access"}},
		{"rp-044", []string{"this.phase"}, nil},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			e, ok := m.Lookup(c.id)
			if !ok {
				t.Fatalf("%s missing from seed catalog", c.id)
			}
			joinedUDFs := strings.Join(e.UDFs, "\n")
			for _, f := range c.requiredFields {
				if !strings.Contains(joinedUDFs, f) {
					t.Errorf("%s UDAF missing timestamp field %q for compositional merge", c.id, f)
				}
			}
			for _, f := range c.forbiddenFlags {
				if strings.Contains(joinedUDFs, f) {
					t.Errorf("%s UDAF still uses non-compositional field %q", c.id, f)
				}
			}
		})
	}

	// rp-044: verify phase FSM for 3-stage ordering; merge is best-effort.
	t.Run("rp-044-phase-fsm", func(t *testing.T) {
		e, _ := m.Lookup("rp-044")
		joinedUDFs := strings.Join(e.UDFs, "\n")
		for _, want := range []string{
			"this.phase === 0",
			"this.phase >= 1",
			"this.phase >= 2",
			"this.phase = 3",
		} {
			if !strings.Contains(joinedUDFs, want) {
				t.Errorf("rp-044 UDAF missing phase FSM snippet %q", want)
			}
		}
	})
}

// TestSeedFS_RP102HasHopWindow pins rp-102 (Agentic Pressure Normative Drift,
// closes #88) to its streaming-MV rewrite. Branch A must use a 5-minute hop
// window in place of `OVER (... ROWS BETWEEN 4 PRECEDING AND CURRENT ROW)`,
// firing when 3+ pressure-pattern tool failures land in the same window.
// Branch B (rationalization-language match in assistant_message) is single-
// event and must use double-backslash word boundaries.
func TestSeedFS_RP102HasHopWindow(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-102")
	if !ok {
		t.Fatal("rp-102 missing from seed catalog")
	}
	if e.Version != "0.3.0" {
		t.Fatalf("rp-102 version = %q, want 0.3.0", e.Version)
	}
	q := e.QuerySQL
	required := []string{
		// Branch A: 5-min hop over cim_event
		"FROM hop(agentguard_cim_event, event_time, interval 1 minute, interval 5 minute)",
		// Threshold preserved from v0.2.0
		"AND match(lower(tool_result), '\\\\b(rate.?limit|quota.?exceed",
		") >= 3",
		// Branch B: single-event llm_response rationalization match
		"event_type = 'llm_response'",
		"\\\\bgiven (the )?(unavailability",
		"EMIT STREAM",
	}
	for _, want := range required {
		if !strings.Contains(q, want) {
			t.Errorf("rp-102 QuerySQL missing %q", want)
		}
	}
	// Anti-regression: ROWS BETWEEN must not appear in the live SQL.
	// Strip `--` line comments first — the changelog reasonably mentions
	// the historical ROWS BETWEEN frame.
	if strings.Contains(strings.ToUpper(stripSQLComments(q)), "ROWS BETWEEN") {
		t.Error("rp-102 has ROWS BETWEEN — must use hop() time window in streaming MV")
	}
}

// TestSeedFS_RP123HasHopWindow pins rp-123 (FAB Finetuning-Activated Dormant
// Adversarial Behavior). Branch B (over-refusal chain) must use a 5-minute
// hop window. Branch A (advertising) and Branch C (jailbreakability) are
// preserved from v0.1.1; Branch C's `lag() OVER (PARTITION BY session_id)`
// is streaming-MV-compatible (rp-016 Branch 3 precedent).
func TestSeedFS_RP123HasHopWindow(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-123")
	if !ok {
		t.Fatal("rp-123 missing from seed catalog")
	}
	if e.Version != "0.2.0" {
		t.Fatalf("rp-123 version = %q, want 0.2.0", e.Version)
	}
	q := e.QuerySQL
	required := []string{
		// Branch A: single-event advertising match (presence of the regex prefix)
		"'(for (best|optimal) (results|performance|outcomes)",
		// Branch B: 5-min hop refusal aggregation with escaped word boundary
		"FROM hop(agentguard_cim_event, event_time, interval 1 minute, interval 5 minute)",
		"\\\\b(sorry|i am unable|i cannot",
		") >= 3",
		// Branch C: lag-based prev-event jailbreakability check (CTE 'lagged')
		"WITH lagged AS",
		"lag(event_type)",
		"OVER (PARTITION BY session_id) AS prev_event_type",
		"lag(assistant_message)",
		"OVER (PARTITION BY session_id) AS prev_assistant_msg",
		"prev_event_type = 'llm_response'",
		"EMIT STREAM",
	}
	for _, want := range required {
		if !strings.Contains(q, want) {
			t.Errorf("rp-123 QuerySQL missing %q", want)
		}
	}
	// Anti-regression: the w1/w2 CTE chain with table()-wrapped CTE refs
	// must NOT come back — both raised streaming-MV errors pre-#88.
	if strings.Contains(q, "FROM table(w1)") || strings.Contains(q, "FROM table(w2)") {
		t.Error("rp-123 has `FROM table(cte)` — Proton raises Code 80 in streaming MV; reference CTEs without table()")
	}
	if strings.Contains(strings.ToUpper(stripSQLComments(q)), "ROWS BETWEEN") {
		t.Error("rp-123 has ROWS BETWEEN — must use hop() time window in streaming MV")
	}
}

// TestSeedFS_TouchedRulesNoBareBackslashB guards the four rules restored in
// the #88/#89 streaming-MV rewrite against re-introducing bare `\b` word
// boundaries inside SQL string literals. Proton parses `\b` inside a
// single-quoted SQL string as backspace 0x08 (IO/ReadHelpers.h), making the
// regex word-boundary clause dead code — the rule still installs but
// silently never matches. The required spelling is `\\b`. A catalog-wide
// migration of the other ~47 seed rules with the same bug is tracked
// separately; this test only locks in the four rules we've already fixed.
func TestSeedFS_TouchedRulesNoBareBackslashB(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	for _, id := range []string{"rp-013", "rp-047", "rp-102", "rp-123"} {
		e, ok := m.Lookup(id)
		if !ok {
			t.Errorf("%s missing from seed catalog", id)
			continue
		}
		// Strip line comments first so changelog prose mentioning the
		// historical bare-`\b` bug does not false-positive.
		stripped := stripSQLComments(e.QuerySQL)
		// Remove escaped occurrences; anything left is bare.
		bareSearch := strings.ReplaceAll(stripped, `\\b`, "")
		if strings.Contains(bareSearch, `\b`) {
			t.Errorf("rule %s contains bare \\b in SQL — escape as \\\\b (Proton parses \\b inside string literals as backspace 0x08)", id)
		}
	}
}

// stripSQLComments removes `--` line comments from a SQL string. The changelog
// inside a rule's query_sql block reasonably mentions banned-pattern names
// when explaining why they were rewritten; the comment-strip avoids those
// references being treated as live SQL by anti-regression assertions.
func stripSQLComments(q string) string {
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
