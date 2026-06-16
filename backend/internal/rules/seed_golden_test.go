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
func assertDefaultsRenderInPlace(t *testing.T, d catalog.Definition, literals map[string]string) {
	t.Helper()
	expected := d.SQLTemplate
	for id, lit := range literals {
		ph := "{{params." + id + "}}"
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
// substitution: the string_list renders to the original tool list and the
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
		t.Errorf("string_list override not rendered: %q", compiled.SQL)
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
