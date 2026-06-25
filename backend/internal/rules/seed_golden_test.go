package rules

import (
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/catalog"
)

// loadSeed loads the embedded seed catalog for compiler-level assertions.
func loadSeed(t *testing.T) *catalog.Manager {
	t.Helper()
	m, err := catalog.New(catalog.SeedFS, "")
	if err != nil {
		t.Fatalf("load seed catalog: %v", err)
	}
	return m
}

func seedDef(t *testing.T, m *catalog.Manager, id string) catalog.Definition {
	t.Helper()
	d, ok := m.Lookup(id)
	if !ok {
		t.Fatalf("seed %s not found", id)
	}
	return d
}

// TestSeedDefinitionsCompileWithDefaults asserts that EVERY embedded seed
// definition compiles end-to-end with its declared defaults: the sql_template
// validates against the declared parameters, the defaults satisfy their own
// constraints, and rendering substitutes every placeholder (no residual
// {{...}}). This is the catch-all guard that any parameterized seed rule —
// present or future — stays installable from defaults.
func TestSeedDefinitionsCompileWithDefaults(t *testing.T) {
	defs := loadSeed(t).All()
	if len(defs) == 0 {
		t.Fatal("seed catalog loaded zero definitions")
	}
	for _, d := range defs {
		compiled, err := Compile(d, nil)
		if err != nil {
			t.Errorf("seed %s: Compile with defaults: %v", d.ID, err)
			continue
		}
		if strings.Contains(compiled.SQL, "{{") || strings.Contains(compiled.SQL, "}}") {
			t.Errorf("seed %s: rendered SQL still contains a placeholder delimiter", d.ID)
		}
	}
}

// assertDefaultsRenderInPlace is the golden check for a parameterized rule: the
// defaults-rendered SQL must equal the sql_template with each placeholder
// textually replaced by its default's rendered literal — proving the renderer
// performs a pure in-place substitution and the parameterization introduced no
// other change to the SQL (behavior preservation), independent of brittle
// whole-file golden strings.
//
// ph is the exact placeholder string as written in the seed YAML, e.g.
// "{{ .Config.dangerous_tools }}" (with surrounding spaces).
func assertDefaultsRenderInPlace(t *testing.T, d catalog.Definition, literals map[string]string) {
	t.Helper()
	expected := d.SQLTemplate
	for id, lit := range literals {
		ph := "{{ .Config." + id + " }}"
		if !strings.Contains(expected, ph) {
			t.Fatalf("%s: template is missing placeholder %s", d.ID, ph)
		}
		expected = strings.ReplaceAll(expected, ph, lit)
	}
	compiled, err := Compile(d, nil)
	if err != nil {
		t.Fatalf("%s: Compile defaults: %v", d.ID, err)
	}
	if compiled.SQL != expected {
		t.Errorf("%s: default-rendered SQL is not the template with defaults substituted in place.\n got:\n%s\nwant:\n%s",
			d.ID, compiled.SQL, expected)
	}
}

// TestRP003_DefaultsGolden pins rp-003's defaults to the exact in-place
// substitution: the list renders to the original tool list and the
// integer renders to 15. The /2 threshold form is behavior-equivalent to the
// pre-parameterization `>= 30` (each removed '&&' is 2 chars, so the net length
// is always even and `/2 >= 15` ⟺ `>= 30`).
func TestRP003_DefaultsGolden(t *testing.T) {
	d := seedDef(t, loadSeed(t), "rp-003")
	assertDefaultsRenderInPlace(t, d, map[string]string{
		"dangerous_tools":         "('bash', 'shell', 'exec', 'run_terminal_cmd', 'computer')",
		"command_chain_threshold": "15",
	})
}

// TestRP003_OverridesChangeSQL proves the parameters actually drive the SQL:
// distinct typed overrides produce the corresponding rendered literals, and the
// canonical overrides are persisted (defaults dropped).
func TestRP003_OverridesChangeSQL(t *testing.T) {
	d := seedDef(t, loadSeed(t), "rp-003")
	compiled, err := Compile(d, map[string]any{
		"dangerous_tools":         []any{"bash", "sh"},
		"command_chain_threshold": 50,
	})
	if err != nil {
		t.Fatalf("Compile overrides: %v", err)
	}
	if !strings.Contains(compiled.SQL, "lower(tool_name) IN ('bash', 'sh')") {
		t.Errorf("list override not rendered: %q", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, ") / 2) >= 50") {
		t.Errorf("integer override not rendered: %q", compiled.SQL)
	}
	if got, _ := compiled.Overrides["command_chain_threshold"].(int64); got != 50 {
		t.Errorf("canonical override = %#v, want int64(50)", compiled.Overrides["command_chain_threshold"])
	}
}

// TestRP064_DefaultsGolden pins rp-064's single metrics threshold: defaults
// render the original `count() > 25` in place (no other change).
func TestRP064_DefaultsGolden(t *testing.T) {
	d := seedDef(t, loadSeed(t), "rp-064")
	assertDefaultsRenderInPlace(t, d, map[string]string{
		"tool_call_rate_threshold": "25",
	})
	compiled, _ := Compile(d, nil)
	if !strings.Contains(compiled.SQL, "HAVING count() > 25") {
		t.Errorf("rp-064 default threshold not rendered in place: %q", compiled.SQL)
	}
}

// TestRP128_DefaultsGolden pins rp-128 — the seed that exercises both AG type
// extensions: the duration parameter renders as a bare integer (10000) and the
// regex parameter renders as a single-quoted SQL literal, both substituted in
// place. This proves a shipped seed (not just a fixture) uses regex/duration.
func TestRP128_DefaultsGolden(t *testing.T) {
	d := seedDef(t, loadSeed(t), "rp-128")
	assertDefaultsRenderInPlace(t, d, map[string]string{
		"min_duration_ms":  "10000",
		"output_signature": "'(AKIA|ASIA)[0-9A-Z]{16}|gh[po]_[A-Za-z0-9]{36}|xox[baprs]-[0-9A-Za-z-]{10,}|sk-[A-Za-z0-9]{20,}|-----BEGIN'",
	})
	compiled, _ := Compile(d, nil)
	if !strings.Contains(compiled.SQL, "tool_duration_ms > 10000") {
		t.Errorf("rp-128 duration default not rendered bare in place: %q", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "match(coalesce(tool_result, ''), '(AKIA|ASIA)[0-9A-Z]{16}|gh[po]_[A-Za-z0-9]{36}|xox[baprs]-[0-9A-Za-z-]{10,}|sk-[A-Za-z0-9]{20,}|-----BEGIN')") {
		t.Errorf("rp-128 regex default not rendered as a quoted literal in place: %q", compiled.SQL)
	}
}

// TestRP128_OverridesChangeSQL proves the rp-128 regex/duration parameters drive
// the SQL: distinct typed overrides produce the corresponding rendered literals.
func TestRP128_OverridesChangeSQL(t *testing.T) {
	d := seedDef(t, loadSeed(t), "rp-128")
	compiled, err := Compile(d, map[string]any{
		"min_duration_ms":  30000,
		"output_signature": "BEGIN .* PRIVATE KEY",
	})
	if err != nil {
		t.Fatalf("Compile overrides: %v", err)
	}
	if !strings.Contains(compiled.SQL, "tool_duration_ms > 30000") {
		t.Errorf("duration override not rendered: %q", compiled.SQL)
	}
	if !strings.Contains(compiled.SQL, "'BEGIN .* PRIVATE KEY'") {
		t.Errorf("regex override not rendered as a quoted literal: %q", compiled.SQL)
	}
	if got, _ := compiled.Overrides["min_duration_ms"].(int64); got != 30000 {
		t.Errorf("canonical duration override = %#v, want int64(30000)", compiled.Overrides["min_duration_ms"])
	}
}

// TestNoParameterRuleRendersByteIdentical proves the plan's "a definition with
// no parameters renders unchanged" case against a real seed rule.
func TestNoParameterRuleRendersByteIdentical(t *testing.T) {
	d := seedDef(t, loadSeed(t), "rp-001")
	if len(d.Parameters) != 0 {
		t.Fatalf("rp-001 is expected to have no parameters; got %d", len(d.Parameters))
	}
	compiled, err := Compile(d, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled.SQL != d.SQLTemplate {
		t.Errorf("zero-parameter rule must render byte-identical to its template")
	}
	if len(compiled.Overrides) != 0 {
		t.Errorf("zero-parameter rule must have no overrides, got %#v", compiled.Overrides)
	}
}

// ── Phase B: duration and regex fixture golden tests ─────────────────────────
//
// Approach: dedicated test-fixture definitions (not seed rules) are used for
// the golden assertions. The duration fixture uses a plain INTERVAL N SECOND in
// a WHERE clause (safe — not a streaming MAXSPAN/TIMEOUT argument). The regex
// fixture uses match() with an operator-supplied pattern. No existing seed rules
// are modified for these tests, avoiding cascading golden-test updates.

// TestDurationFixture_DefaultsGolden proves duration defaults render as bare
// decimal integers and substitute in-place (no other change to the SQL).
func TestDurationFixture_DefaultsGolden(t *testing.T) {
	d := catalog.Definition{
		ID:          "rp-test-dur",
		Name:        "Duration Window Test",
		Version:     "1.0.0",
		SQLTemplate: "SELECT * FROM agentguard_cim_metrics WHERE event_time > now() - INTERVAL {{ .Config.window_seconds }} SECOND",
		Parameters: []catalog.Parameter{{
			Key:  "window_seconds",
			Type: "duration",
			Unit: "seconds",
			// 300 s = 5 minutes; matches a common pre-parameterisation hard-coded value.
			Default: 300,
			Min:     fptr(60),
			Max:     fptr(3600),
		}},
	}
	assertDefaultsRenderInPlace(t, d, map[string]string{
		"window_seconds": "300",
	})
	compiled, err := Compile(d, nil)
	if err != nil {
		t.Fatalf("Compile defaults: %v", err)
	}
	if !strings.Contains(compiled.SQL, "INTERVAL 300 SECOND") {
		t.Errorf("duration default not rendered in place: %q", compiled.SQL)
	}
}

// TestDurationFixture_OverrideChangesSQL proves the duration parameter drives
// the rendered SQL and that canonical overrides are persisted.
func TestDurationFixture_OverrideChangesSQL(t *testing.T) {
	d := catalog.Definition{
		ID:          "rp-test-dur",
		Name:        "Duration Window Test",
		Version:     "1.0.0",
		SQLTemplate: "SELECT * FROM agentguard_cim_metrics WHERE event_time > now() - INTERVAL {{ .Config.window_seconds }} SECOND",
		Parameters: []catalog.Parameter{{
			Key: "window_seconds", Type: "duration", Unit: "seconds",
			Default: 300, Min: fptr(60), Max: fptr(3600),
		}},
	}
	compiled, err := Compile(d, map[string]any{"window_seconds": 600})
	if err != nil {
		t.Fatalf("Compile overrides: %v", err)
	}
	if !strings.Contains(compiled.SQL, "INTERVAL 600 SECOND") {
		t.Errorf("duration override not rendered: %q", compiled.SQL)
	}
	if got, _ := compiled.Overrides["window_seconds"].(int64); got != 600 {
		t.Errorf("canonical override = %#v, want int64(600)", compiled.Overrides["window_seconds"])
	}
}

// TestRegexFixture_DefaultsGolden proves regex defaults render as single-quoted
// SQL literals (escaping single quotes) and substitute in-place.
func TestRegexFixture_DefaultsGolden(t *testing.T) {
	d := catalog.Definition{
		ID:          "rp-test-re",
		Name:        "Regex Pattern Test",
		Version:     "1.0.0",
		SQLTemplate: "SELECT * FROM agentguard_cim_event WHERE match(lower(tool_name), {{ .Config.pattern }})",
		Parameters: []catalog.Parameter{{
			Key:     "pattern",
			Type:    "regex",
			Default: "rm|dd|mkfs",
		}},
	}
	assertDefaultsRenderInPlace(t, d, map[string]string{
		"pattern": "'rm|dd|mkfs'",
	})
	compiled, err := Compile(d, nil)
	if err != nil {
		t.Fatalf("Compile defaults: %v", err)
	}
	if !strings.Contains(compiled.SQL, "'rm|dd|mkfs'") {
		t.Errorf("regex default not rendered in place: %q", compiled.SQL)
	}
}

// TestRegexFixture_QuoteEscapedInSQL proves that a regex containing a single
// quote is correctly escaped so the rendered SQL is a valid single-quoted literal.
func TestRegexFixture_QuoteEscapedInSQL(t *testing.T) {
	d := catalog.Definition{
		ID:          "rp-test-re2",
		Name:        "Regex Escape Test",
		Version:     "1.0.0",
		SQLTemplate: "SELECT match(name, {{ .Config.pat }})",
		Parameters: []catalog.Parameter{{
			Key:     "pat",
			Type:    "regex",
			Default: "it's",
		}},
	}
	compiled, err := Compile(d, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(compiled.SQL, "''s'") {
		t.Errorf("single quote not escaped in regex literal: %q", compiled.SQL)
	}
}
