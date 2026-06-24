package catalog

import (
	"errors"
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
	// Local-dir definitions must pass Validate to load (unlike seed test
	// fixtures), so they carry the required fields.
	if err := os.WriteFile(filepath.Join(tmp, "rp-002.yaml"),
		[]byte("id: rp-002\nname: Local Override\nversion: 1.0.0\nsql_template: SELECT 1\n"), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "rp-003.yaml"),
		[]byte("id: rp-003\nname: Local New\nversion: 1.0.0\nsql_template: SELECT 1\n"), 0o644); err != nil {
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

	entry := Definition{ID: "rp-new", Name: "New Rule", Version: "1.0.0",
		SQLTemplate: "SELECT 1", Severity: "warning"}
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

// TestNew_SemanticValidatorSkipsInvalidLocalDef proves the validator injected via
// WithSemanticValidator is applied while local-directory files load (not only at
// Save/Import): a structurally-valid YAML that fails the validator is skipped, so
// a hand-placed non-compiling local file never enters the served catalog.
func TestNew_SemanticValidatorSkipsInvalidLocalDef(t *testing.T) {
	tmp := t.TempDir()
	good := "id: rp-good\nname: Good\nversion: 1.0.0\nsql_template: SELECT 1\n"
	bad := "id: rp-bad\nname: Bad\nversion: 1.0.0\nsql_template: SELECT 1\n"
	if err := os.WriteFile(filepath.Join(tmp, "good.yaml"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "bad.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(fstest.MapFS{}, tmp, WithSemanticValidator(func(d Definition) error {
		if d.ID == "rp-bad" {
			return errors.New("does not compile")
		}
		return nil
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := m.Lookup("rp-good"); !ok {
		t.Error("compiling local definition should load")
	}
	if _, ok := m.Lookup("rp-bad"); ok {
		t.Error("non-compiling local definition must be skipped at load")
	}
}

// TestSave_SemanticValidatorRejects proves the injected compiler-semantic check
// runs at Save time: a definition that passes structural Validate() but fails the
// validator is rejected before it is written to disk or added to memory (so an
// authoring defect surfaces as a client error at create time, not a 500 at first
// install).
func TestSave_SemanticValidatorRejects(t *testing.T) {
	tmp := t.TempDir()
	m, err := New(fstest.MapFS{}, tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.SemanticValidator = func(d Definition) error {
		return errors.New("compiler said no")
	}
	entry := Definition{ID: "rp-bad", Name: "Bad", Version: "1.0.0",
		SQLTemplate: "SELECT 1", Severity: "warning"}
	if err := m.Save(entry); err == nil || !strings.Contains(err.Error(), "compiler said no") {
		t.Fatalf("Save should surface the semantic validator error, got %v", err)
	}
	if _, ok := m.Lookup("rp-bad"); ok {
		t.Errorf("rejected definition must not be added to the catalog")
	}
	if _, statErr := os.Stat(filepath.Join(tmp, "rp-bad.yaml")); statErr == nil {
		t.Errorf("rejected definition must not be written to disk")
	}
}

func TestDelete_RemovesLocalDefinitionAndFile(t *testing.T) {
	tmp := t.TempDir()
	seed := fstest.MapFS{"a.yaml": {Data: []byte("id: rp-001\nname: Builtin\n")}}
	m, err := New(seed, tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	entry := Definition{ID: "custom-x", Name: "Custom", Version: "1.0.0",
		SQLTemplate: "SELECT 1", Severity: "warning"}
	if err := m.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, ok := m.Lookup("custom-x"); !ok {
		t.Fatal("custom-x should exist after Save")
	}

	if err := m.Delete("custom-x"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := m.Lookup("custom-x"); ok {
		t.Error("custom-x should be gone after Delete")
	}
	if _, err := os.Stat(filepath.Join(tmp, "custom-x.yaml")); !os.IsNotExist(err) {
		t.Errorf("file should be removed, stat err = %v", err)
	}

	// Deleting an unknown local ID is a no-op (no error).
	if err := m.Delete("nope"); err != nil {
		t.Errorf("Delete unknown should be a no-op, got %v", err)
	}

	// Deleting a builtin's ID removes no file and leaves the seed entry intact.
	if err := m.Delete("rp-001"); err != nil {
		t.Errorf("Delete builtin: %v", err)
	}
	if !m.Exists("rp-001") {
		t.Error("builtin rp-001 must survive Delete (it is not file-backed)")
	}
}

func TestDelete_NoCatalogDirErrors(t *testing.T) {
	m, err := New(fstest.MapFS{}, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Delete("x"); err == nil {
		t.Error("expected error when no catalog dir is configured")
	}
}

func TestSave_NoCatalogDirErrors(t *testing.T) {
	m, err := New(fstest.MapFS{}, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Save(Definition{ID: "x"}); err == nil {
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

// loadDir must validate each local definition and skip (not fail startup on)
// invalid or unparseable files, so anything reachable via Lookup/All has
// passed Validate.
func TestLoadDir_SkipsInvalidDefinitions(t *testing.T) {
	tmp := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("good.yaml", "id: rp-good\nname: Good\nversion: 1.0.0\nsql_template: SELECT 1\n")
	// Fails Validate: missing sql_template.
	write("invalid.yaml", "id: rp-invalid\nname: Invalid\nversion: 1.0.0\n")
	// Unparseable YAML.
	write("broken.yaml", "id: [this-is: not-valid-yaml-at-all")

	m, err := New(fstest.MapFS{}, tmp)
	if err != nil {
		t.Fatalf("New must not fail on invalid local files: %v", err)
	}
	if !m.Exists("rp-good") {
		t.Error("valid local definition should be loaded")
	}
	if m.Exists("rp-invalid") {
		t.Error("Validate-failing local definition must be skipped")
	}
	if got := len(m.All()); got != 1 {
		t.Errorf("expected 1 entry, got %d", got)
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

// Compile-time check that Definition is the type returned from Lookup/All —
// guards against accidental field renames downstream consumers depend on.
var _ = func() Definition {
	return Definition{ID: "_", Name: "_", Version: "_", Description: "_", Severity: "_",
		Category: "_", FPRate: "_", HookSources: "_", RequiredTelemetry: "_",
		Author: "_", Tags: nil, UDFs: nil, SQLTemplate: "_", Parameters: nil, Origin: "_"}
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
		for _, line := range strings.Split(e.SQLTemplate, "\n") {
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
	if e.Name == "" || e.SQLTemplate == "" {
		t.Errorf("rp-013 has empty Name or SQLTemplate: %+v", e)
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

	query := e.SQLTemplate
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
			t.Fatalf("rp-013 SQLTemplate missing %q", want)
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
	query := e.SQLTemplate

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
	if e.Version != "0.3.1" {
		t.Fatalf("rp-018 version = %q, want 0.3.1", e.Version)
	}
	if strings.Contains(e.RequiredTelemetry, "llm.total_tokens") {
		t.Fatalf("rp-018 RequiredTelemetry still includes llm.total_tokens: %q", e.RequiredTelemetry)
	}
	udfs := strings.Join(e.UDFs, "\n")
	if len(e.UDFs) == 0 || !strings.Contains(udfs, "ag_rp018_max_tool_calls") {
		t.Fatal("rp-018 missing ag_rp018_max_tool_calls aggregate UDF")
	}
	if !strings.Contains(udfs, "Object.create(null)") {
		t.Fatal("rp-018 UDAF must use Object.create(null) for tool-count map to prevent prototype-key evasion")
	}

	q := e.SQLTemplate
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
			t.Errorf("rp-018 SQLTemplate missing %q", want)
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
			t.Errorf("rp-018 SQLTemplate still contains forbidden pre-v0.3.0 pattern %q", bad)
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
	q := e.SQLTemplate
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
			t.Errorf("rp-047 SQLTemplate missing %q", want)
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
	q := e.SQLTemplate
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
			t.Errorf("rp-050 SQLTemplate missing %q", want)
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
			if !strings.Contains(e.SQLTemplate, c.queryUse) {
				t.Errorf("%s SQLTemplate missing %q", c.id, c.queryUse)
			}
			sqlWithoutComments := stripSQLComments(e.SQLTemplate)
			for _, forbidden := range c.antiSnippets {
				if strings.Contains(sqlWithoutComments, forbidden) {
					t.Errorf("%s SQLTemplate still contains pre-UDA snippet %q", c.id, forbidden)
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
	q := e.SQLTemplate
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
			t.Errorf("rp-102 SQLTemplate missing %q", want)
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
	q := e.SQLTemplate
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
			t.Errorf("rp-123 SQLTemplate missing %q", want)
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
	for _, id := range []string{"rp-013", "rp-047", "rp-102", "rp-123", "rp-042", "rp-048", "rp-082"} {
		e, ok := m.Lookup(id)
		if !ok {
			t.Errorf("%s missing from seed catalog", id)
			continue
		}
		// Strip line comments first so changelog prose mentioning the
		// historical bare-`\b` bug does not false-positive.
		stripped := stripSQLComments(e.SQLTemplate)
		// Remove escaped occurrences; anything left is bare.
		bareSearch := strings.ReplaceAll(stripped, `\\b`, "")
		if strings.Contains(bareSearch, `\b`) {
			t.Errorf("rule %s contains bare \\b in SQL — escape as \\\\b (Proton parses \\b inside string literals as backspace 0x08)", id)
		}
	}
}

// TestSeedFS_RP048Branch2Lowercased locks in the issue #155 fix: Branch 2's
// redirect_uri positive check and localhost exclusion must be lower()-wrapped,
// and the URL-encoded hex in the exclusion must be lowercased (%3a%2f%2f) so it
// still matches after lower() lowercases the input's hex digits.
func TestSeedFS_RP048Branch2Lowercased(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-048")
	if !ok {
		t.Fatal("rp-048 missing from seed catalog")
	}
	if e.Version != "0.1.2" {
		t.Fatalf("rp-048 version = %q, want 0.1.2", e.Version)
	}
	q := e.SQLTemplate
	mustContain := func(needle string) {
		t.Helper()
		if !strings.Contains(q, needle) {
			t.Errorf("rp-048 SQLTemplate missing %q", needle)
		}
	}
	mustContain("match(lower(tool_result), 'redirect_uri=')")
	mustContain("NOT match(lower(tool_result), 'redirect_uri=(https?%3a%2f%2f|https?://)(localhost")
	// Old case-sensitive forms must be gone (strip comments so changelog prose
	// describing the bug does not false-positive).
	stripped := stripSQLComments(q)
	if strings.Contains(stripped, "match(tool_result, 'redirect_uri=')") {
		t.Error("rp-048 Branch 2 still has bare-case match(tool_result, 'redirect_uri=')")
	}
	if strings.Contains(stripped, "%3A%2F%2F") {
		t.Error("rp-048 still has uppercase URL-encoded hex in the Branch 2 exclusion; after lower() the pattern must use lowercase hex")
	}
}

// TestSeedFS_RP048HostDelimiter locks in the PR #156 review fix: the
// localhost/loopback exclusion must be followed by a host delimiter so an
// attacker host that merely starts with the literal (e.g. localhost.evil.com)
// is not treated as local and silently suppressed.
func TestSeedFS_RP048HostDelimiter(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-048")
	if !ok {
		t.Fatal("rp-048 missing from seed catalog")
	}
	q := e.SQLTemplate
	if !strings.Contains(q, `(localhost|127\.0\.0\.1|0\.0\.0\.0)["/:?#]`) {
		t.Error("rp-048 Branch 1 localhost exclusion missing the trailing host delimiter")
	}
	if !strings.Contains(q, `(localhost|127\.0\.0\.1|0\.0\.0\.0)([:/?#&" ]|%2f|%3a|%3f|%23|%26|$)`) {
		t.Error("rp-048 Branch 2 localhost exclusion missing the trailing host delimiter")
	}
	// The old delimiter-less form (loopback literal immediately closing the
	// regex) must be gone from both branches.
	if strings.Contains(q, `0\.0\.0\.0)'`) {
		t.Error("rp-048 still has a delimiter-less localhost exclusion (host literal immediately closing the pattern)")
	}
}

// TestSeedFS_RP082APCDCSAllVariants locks in the issue #155 fix: the APC/DCS
// (ESC_ / ESC P) Branch 7 must use all four ESC encodings — raw byte (\x1b,
// \033) plus literal text (\\x1b, \\033) — in BOTH the tool_result and the
// assistant_message sections (it previously had only the two literal-text forms
// in tool_result, and was missing entirely from assistant_message).
func TestSeedFS_RP082APCDCSAllVariants(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-082")
	if !ok {
		t.Fatal("rp-082 missing from seed catalog")
	}
	if e.Version != "0.2.1" {
		t.Fatalf("rp-082 version = %q, want 0.2.1", e.Version)
	}
	q := e.SQLTemplate
	apcdcs := `(\\x1b|\\033|\\\\x1b|\\\\033)[_p]`
	if c := strings.Count(q, apcdcs); c < 2 {
		t.Errorf("rp-082 APC/DCS 4-variant pattern appears %d time(s), want >=2 (tool_result + assistant_message)", c)
	}
	// The old 2-variant-only tool_result form must be gone.
	if strings.Contains(q, `(\\\\x1b|\\\\033)[_p]`) {
		t.Error("rp-082 still has the 2-variant-only APC/DCS form")
	}
}

// TestSeedFS_RP042WindowedBranches locks in the issue #155 rewrite: the eager
// per-event 5a/5b matches are removed from the non-windowed leg and replaced
// with two windowed legs in a UNION ALL ... EMIT STREAM MV — tumble(60s) absence
// detection (5a) and tumble(10m) count>=4 (5b).
func TestSeedFS_RP042WindowedBranches(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	e, ok := m.Lookup("rp-042")
	if !ok {
		t.Fatal("rp-042 missing from seed catalog")
	}
	if e.Version != "0.5.0" {
		t.Fatalf("rp-042 version = %q, want 0.5.0", e.Version)
	}
	q := e.SQLTemplate
	must := func(needle, why string) {
		t.Helper()
		if !strings.Contains(q, needle) {
			t.Errorf("%s: rp-042 missing %q", why, needle)
		}
	}
	// 5a — tumble(60s) absence detection (cherry-picked Branch 6).
	must("FROM tumble(agentguard_cim_event, event_time, interval 60 second)", "5a needs 60s tumble window")
	must("verify (my|your|the) (answer|solution|response|work|output)", "5a needs the RLVR verify-trigger")
	must("against (my|our|the) (policy|guidelines|terms)", "5a needs the safety-refusal lexicon")
	// 5b — tumble(10m) count>=4.
	must("FROM tumble(agentguard_cim_event, event_time, interval 10 minute)", "5b needs 10m tumble window")
	must("(verify this|confirm this|validate this|check this|test this)", "5b needs the borderline phrase")
	must(") >= 4", "5b needs the count>=4 threshold")
	// Mixed UNION ALL MV must be terminated with EMIT STREAM (rp-047/rp-013 shape).
	must("EMIT STREAM", "mixed UNION ALL MV needs EMIT STREAM terminator")
	// Count/split on comment-stripped SQL — the changelog and pointer comments
	// also mention "UNION ALL" and would otherwise inflate the count and mis-split.
	stripped := stripSQLComments(q)
	if c := strings.Count(stripped, "UNION ALL"); c != 2 {
		t.Errorf("rp-042 has %d UNION ALL, want 2 (per-event + 5a + 5b)", c)
	}
	// The eager 5a/5b must be GONE from the per-event leg (everything before the
	// first UNION ALL). The verify-trigger / borderline phrase may now appear only
	// inside the windowed count_if legs.
	perEvent := strings.SplitN(stripped, "UNION ALL", 2)[0]
	if strings.Contains(perEvent, "verify (my|your|the)") {
		t.Error("eager 5a still present in rp-042 per-event leg; must be windowed-only")
	}
	if strings.Contains(perEvent, "(verify this|confirm this") {
		t.Error("eager 5b still present in rp-042 per-event leg; must be windowed-only")
	}
	// 5a must group by session, not run_id (PR #156 review): agent plugins enqueue
	// user_input and llm_response under different run_ids, so a run_id-keyed group
	// never sees the refusal and false-positives on refused prompts.
	if strings.Contains(q, "session_id, run_id, agent_id") {
		t.Error("rp-042 5a still groups by run_id; must group by session only so same-session refusals correlate")
	}
}

// newTestManagerWithUDF creates a Manager seeded with a single definition that
// has one UDF, for use in Clone and RescopeUDFs integration tests.
func newTestManagerWithUDF(t *testing.T) *Manager {
	t.Helper()
	seed := fstest.MapFS{
		"rp-003.yaml": {Data: []byte(
			"id: rp-003\n" +
				"name: Score Rule\n" +
				"version: 1.0.0\n" +
				"sql_template: \"SELECT ag_rp003_score(tool_input) AS s\"\n" +
				"udfs:\n" +
				"  - \"CREATE OR REPLACE FUNCTION ag_rp003_score(x string) RETURNS float LANGUAGE JAVASCRIPT AS $$ return 1.0; $$;\"\n",
		)},
	}
	m, err := New(seed, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func TestManager_Clone_Basic(t *testing.T) {
	m := newTestManagerWithUDF(t)

	cloned, err := m.Clone("rp-003", "rp-003-custom", "RP-003 (custom)")
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if cloned.ID != "rp-003-custom" {
		t.Errorf("cloned.ID = %q, want rp-003-custom", cloned.ID)
	}
	if cloned.Name != "RP-003 (custom)" {
		t.Errorf("cloned.Name = %q, want RP-003 (custom)", cloned.Name)
	}
	if cloned.Origin != OriginCustom {
		t.Errorf("cloned.Origin = %q, want custom", cloned.Origin)
	}

	// Clone must be persisted in the catalog.
	if !m.Exists("rp-003-custom") {
		t.Error("cloned definition not found in catalog")
	}
	if !m.IsCustom("rp-003-custom") {
		t.Error("cloned definition must have custom origin")
	}

	// UDF names must be rescoped (no longer reference the source rule's ID).
	names, err := cloned.UDFNames()
	if err != nil {
		t.Fatalf("UDFNames: %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("expected 1 UDF, got %v", names)
	}
	if names[0] == "ag_rp003_score" {
		t.Error("UDF name was not rescoped from source rule's name")
	}
	if strings.Contains(cloned.SQLTemplate, "ag_rp003_score") {
		t.Error("SQLTemplate still references old UDF name")
	}
	if !strings.Contains(cloned.SQLTemplate, names[0]) {
		t.Errorf("SQLTemplate does not reference new UDF name %q", names[0])
	}
}

func TestManager_Clone_DuplicateIDFails(t *testing.T) {
	m := newTestManagerWithUDF(t)

	if _, err := m.Clone("rp-003", "rp-003-c", "First clone"); err != nil {
		t.Fatalf("first Clone: %v", err)
	}

	_, err := m.Clone("rp-003", "rp-003-c", "Duplicate")
	if !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists for duplicate id, got %v", err)
	}
}

func TestManager_Clone_SourceMissingFails(t *testing.T) {
	m := newTestManagerWithUDF(t)

	_, err := m.Clone("does-not-exist", "new-id", "New")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for missing source, got %v", err)
	}
}

func TestManager_Clone_InvalidNewIDFails(t *testing.T) {
	m := newTestManagerWithUDF(t)

	_, err := m.Clone("rp-003", "id with spaces!", "Clone")
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for bad id, got %v", err)
	}
}

func TestManager_IsCustom(t *testing.T) {
	seed := fstest.MapFS{
		"rp-001.yaml": {Data: []byte("id: rp-001\nname: Builtin\nversion: 1.0.0\nsql_template: SELECT 1\n")},
	}
	m, err := New(seed, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Builtin definition is not custom.
	if m.IsCustom("rp-001") {
		t.Error("builtin definition must not be reported as custom")
	}

	// Unknown id is not custom.
	if m.IsCustom("rp-nope") {
		t.Error("unknown definition must not be reported as custom")
	}

	// Saved definition defaults to custom.
	d := validDef()
	d.ID = "custom-x"
	if err := m.Save(d); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !m.IsCustom("custom-x") {
		t.Error("saved definition must be reported as custom")
	}
}

// TestMVResourceKey_StripsHyphens pins the MV-namespace normalization that Save
// uses to reject colliding IDs. It MUST mirror internal/timeplus.MVName, which
// names each rule's view mv_rule_<id-with-hyphens-stripped> (see
// testdata/README.md and definition.go). If MVName's normalization ever changes,
// MVResourceKey — and this test — must change with it.
func TestMVResourceKey_StripsHyphens(t *testing.T) {
	cases := map[string]string{
		"rp-042": "rp042",
		"rp042":  "rp042",
		"a-b-c":  "abc",
		"abc":    "abc",
		"x_1":    "x_1", // underscores are significant; only hyphens collapse
	}
	for in, want := range cases {
		if got := MVResourceKey(in); got != want {
			t.Errorf("MVResourceKey(%q) = %q, want %q", in, got, want)
		}
	}
	if MVResourceKey("rp-042") != MVResourceKey("rp042") {
		t.Error("rp-042 and rp042 must share an MV resource key (hyphens stripped)")
	}
	if MVResourceKey("x-1") == MVResourceKey("x_1") {
		t.Error("x-1 and x_1 must NOT share a key (only hyphens are stripped)")
	}
}

// TestNew_SkipsMVNamespaceCollidingLocalDef proves that a hand-added local YAML
// whose ID collapses onto a seed rule's MV name (here local "rp001" beside seed
// "rp-001") is skipped at load — the same MV-namespace uniqueness the API Save
// path enforces, applied to the on-disk catalog directory too.
func TestNew_SkipsMVNamespaceCollidingLocalDef(t *testing.T) {
	tmp := t.TempDir()
	seed := fstest.MapFS{"a.yaml": {Data: []byte("id: rp-001\nname: Builtin\n")}}
	// Local file collides with seed rp-001 in the MV namespace (rp001 -> rp001).
	if err := os.WriteFile(filepath.Join(tmp, "rp001.yaml"),
		[]byte("id: rp001\nname: Collider\nversion: 1.0.0\nsql_template: SELECT 1\n"), 0o644); err != nil {
		t.Fatalf("write collider: %v", err)
	}
	m, err := New(seed, tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := m.Lookup("rp001"); ok {
		t.Error("MV-namespace-colliding local definition must be skipped at load")
	}
	if _, ok := m.Lookup("rp-001"); !ok {
		t.Error("the seed definition must remain")
	}
}

// TestSave_RejectsMVNamespaceCollision verifies that Save rejects a NEW id that
// collapses onto a different existing rule's MV name — "team-a" and "teama" both
// deploy to mv_rule_teama and would silently clobber each other on
// install/toggle/sync. Re-saving the SAME id (override/edit/rollback republish)
// must NOT be blocked.
func TestSave_RejectsMVNamespaceCollision(t *testing.T) {
	tmp := t.TempDir()
	seed := fstest.MapFS{"a.yaml": {Data: []byte("id: rp-001\nname: Builtin\n")}}
	m, err := New(seed, tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	a := validDef()
	a.ID = "team-a"
	if err := m.Save(a); err != nil {
		t.Fatalf("save first rule: %v", err)
	}

	// A different id with the same MV key must be rejected as an existence
	// conflict (→ 409), naming the rule it collides with.
	b := validDef()
	b.ID = "teama"
	err = m.Save(b)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("colliding id: want ErrExists, got %v", err)
	}
	if !strings.Contains(err.Error(), "team-a") {
		t.Errorf("collision error should name the existing rule, got %v", err)
	}
	if _, ok := m.Lookup("teama"); ok {
		t.Error("the colliding id must not have been persisted")
	}

	// Re-saving the SAME id is an override/edit, not a collision: it must succeed.
	a.Description = "edited"
	if err := m.Save(a); err != nil {
		t.Fatalf("re-saving the same id must not trip the collision guard: %v", err)
	}

	// A seed id collision is caught too: "rp001" collapses onto seed "rp-001".
	c := validDef()
	c.ID = "rp001"
	if err := m.Save(c); !errors.Is(err, ErrExists) {
		t.Fatalf("collision with a seed id: want ErrExists, got %v", err)
	}
}
