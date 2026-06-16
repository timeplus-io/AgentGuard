package rules

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/catalog"
)

func TestCompileRendersEveryType(t *testing.T) {
	def := catalog.Definition{
		ID: "rp_all", Name: "All Types", Version: "1.0.0",
		SQLTemplate: "SELECT * WHERE n > {{params.threshold}} AND r < {{params.ratio}} " +
			"AND strict = {{params.strict}} AND name = {{params.name}} " +
			"AND mode = {{params.mode}} AND cmd IN {{params.cmds}}",
		Parameters: []catalog.Parameter{
			{ID: "threshold", Type: "integer", Default: 10},
			{ID: "ratio", Type: "number", Default: 0.5},
			{ID: "strict", Type: "boolean", Default: false},
			{ID: "name", Type: "string", Default: "bash"},
			{ID: "mode", Type: "enum", Default: "warn", Options: []string{"warn", "block"}},
			{ID: "cmds", Type: "string_list", Default: []any{"rm", "dd"}},
		},
	}
	c, err := Compile(def, map[string]any{
		"threshold": float64(42),
		"strict":    true,
		"cmds":      []any{"curl", "wget"},
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := "SELECT * WHERE n > 42 AND r < 0.5 AND strict = true AND name = 'bash' " +
		"AND mode = 'warn' AND cmd IN ('curl', 'wget')"
	if c.SQL != want {
		t.Fatalf("SQL:\n got %q\nwant %q", c.SQL, want)
	}
	if c.DefinitionHash != def.Hash() {
		t.Fatalf("DefinitionHash = %q, want def.Hash() = %q", c.DefinitionHash, def.Hash())
	}
	wantEffective := map[string]any{
		"threshold": int64(42),
		"ratio":     0.5,
		"strict":    true,
		"name":      "bash",
		"mode":      "warn",
		"cmds":      []string{"curl", "wget"},
	}
	if !reflect.DeepEqual(c.Effective, wantEffective) {
		t.Fatalf("Effective = %#v, want %#v", c.Effective, wantEffective)
	}
	wantOverrides := map[string]any{
		"threshold": int64(42),
		"strict":    true,
		"cmds":      []string{"curl", "wget"},
	}
	if !reflect.DeepEqual(c.Overrides, wantOverrides) {
		t.Fatalf("Overrides = %#v, want %#v", c.Overrides, wantOverrides)
	}
}

func TestCompileNumberRenderingIsCanonicalDecimal(t *testing.T) {
	def := catalog.Definition{
		ID: "rp_num", Name: "Num", Version: "1.0.0",
		SQLTemplate: "SELECT {{params.v}}",
		Parameters:  []catalog.Parameter{{ID: "v", Type: "number", Default: 0.0}},
	}
	cases := []struct {
		value any
		want  string
	}{
		{0.1, "SELECT 0.1"},
		{float64(3), "SELECT 3"},
		{1e21, "SELECT 1000000000000000000000"}, // never exponent notation
		{1e-7, "SELECT 0.0000001"},
		{-2.5, "SELECT -2.5"},
	}
	for _, tc := range cases {
		c, err := Compile(def, map[string]any{"v": tc.value})
		if err != nil {
			t.Fatalf("Compile(%v): %v", tc.value, err)
		}
		if c.SQL != tc.want {
			t.Fatalf("Compile(%v) SQL = %q, want %q", tc.value, c.SQL, tc.want)
		}
	}
}

// TestRenderValueRejectsNonFiniteNumber exercises the render-side guard
// directly: normalizeValue already rejects NaN/Inf, but renderValue must
// re-reject them for symmetry with its other type assertions.
func TestRenderValueRejectsNonFiniteNumber(t *testing.T) {
	p := catalog.Parameter{ID: "ratio", Type: "number", Default: 0.5}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := renderValue(p, v); err == nil || !strings.Contains(err.Error(), "non-finite") {
			t.Fatalf("renderValue(%v) error = %v, want non-finite rejection", v, err)
		}
	}
	if got, err := renderValue(p, 0.5); err != nil || got != "0.5" {
		t.Fatalf("renderValue(0.5) = %q, %v", got, err)
	}
}

func TestStringEscaping(t *testing.T) {
	def := catalog.Definition{
		ID: "rp_esc", Name: "Esc", Version: "1.0.0",
		SQLTemplate: "SELECT {{params.s}}",
		Parameters:  []catalog.Parameter{{ID: "s", Type: "string", Default: "x"}},
	}
	cases := []struct {
		name  string
		value string
		want  string
	}{
		{"single quote", "it's", `SELECT 'it''s'`},
		{"backslash", `a\b`, `SELECT 'a\\b'`},
		{"backslash-quote", `\'`, `SELECT '\\'''`},
		{"sql injection attempt", `'; DROP STREAM x; --`, `SELECT '''; DROP STREAM x; --'`},
		{"unicode passthrough", "héllo 世界 🚀", "SELECT 'héllo 世界 🚀'"},
		{"trailing backslash", `end\`, `SELECT 'end\\'`},
		{"only quotes", `'''`, `SELECT ''''''''`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(def, map[string]any{"s": tc.value})
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			if c.SQL != tc.want {
				t.Fatalf("got %q, want %q", c.SQL, tc.want)
			}
		})
	}
}

func TestStringListEscaping(t *testing.T) {
	def := catalog.Definition{
		ID: "rp_lst", Name: "List", Version: "1.0.0",
		SQLTemplate: "SELECT cmd IN {{params.cmds}}",
		Parameters:  []catalog.Parameter{{ID: "cmds", Type: "string_list", Default: []any{"x"}}},
	}
	c, err := Compile(def, map[string]any{"cmds": []any{`rm -rf '/'`, `pa\th`, "ok"}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := `SELECT cmd IN ('rm -rf ''/''', 'pa\\th', 'ok')`
	if c.SQL != want {
		t.Fatalf("got %q, want %q", c.SQL, want)
	}
}

func TestPlaceholderRepeatedOK(t *testing.T) {
	def := catalog.Definition{
		ID: "rp_rep", Name: "Rep", Version: "1.0.0",
		SQLTemplate: "SELECT {{params.n}} + {{params.n}} + {{params.n}}",
		Parameters:  []catalog.Parameter{{ID: "n", Type: "integer", Default: 2}},
	}
	c, err := Compile(def, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if c.SQL != "SELECT 2 + 2 + 2" {
		t.Fatalf("got %q", c.SQL)
	}
}

func TestZeroParameterTemplateIsByteIdentical(t *testing.T) {
	// Includes tricky-but-legal bytes: lone braces, a }} with no opener.
	tmpl := "SELECT map{'k': 1}, '}}', arr[1] FROM s WHERE x = '{plain}'"
	def := catalog.Definition{ID: "rp_zero", Name: "Zero", Version: "1.0.0", SQLTemplate: tmpl}
	c, err := Compile(def, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if c.SQL != tmpl {
		t.Fatalf("SQL changed:\n got %q\nwant %q", c.SQL, tmpl)
	}
	if len(c.Overrides) != 0 || len(c.Effective) != 0 {
		t.Fatalf("expected empty maps, got %#v / %#v", c.Overrides, c.Effective)
	}
	if c.DefinitionHash == "" {
		t.Fatal("DefinitionHash must be populated")
	}
}

func TestTemplateErrors(t *testing.T) {
	param := catalog.Parameter{ID: "x", Type: "integer", Default: 1}
	cases := []struct {
		name    string
		tmpl    string
		params  []catalog.Parameter
		wantErr string
	}{
		{"unknown placeholder", "SELECT {{params.x}} {{params.unknown}}", []catalog.Parameter{param}, "does not match any declared parameter"},
		{"template action", "SELECT {{if x}}1{{end}}", nil, "invalid placeholder"},
		{"spaces inside", "SELECT {{ params.x }}", []catalog.Parameter{param}, "invalid placeholder"},
		{"trailing space in id", "SELECT {{params.x }} {{params.x}}", []catalog.Parameter{param}, "invalid placeholder"},
		{"unterminated", "SELECT {{params.x", []catalog.Parameter{param}, "unclosed '{{'"},
		{"unterminated after valid", "SELECT {{params.x}} AND {{params.x", []catalog.Parameter{param}, "unclosed '{{'"},
		{"declared param never used", "SELECT 1", []catalog.Parameter{param}, `parameter "x" is never used`},
		{"empty id", "SELECT {{params.}}", []catalog.Parameter{param}, "invalid placeholder"},
		{"wrong namespace", "SELECT {{values.x}}", []catalog.Parameter{param}, "invalid placeholder"},
		{"uppercase id", "SELECT {{params.X}} {{params.x}}", []catalog.Parameter{param}, "invalid placeholder"},
		{"nested open", "SELECT {{params.a{{params.x}}", []catalog.Parameter{param}, "invalid placeholder"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := catalog.Definition{ID: "rp_t", Name: "T", Version: "1.0.0", SQLTemplate: tc.tmpl, Parameters: tc.params}
			err := ValidateTemplate(def)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateTemplate error = %v, want containing %q", err, tc.wantErr)
			}
			// Compile must surface the same failure.
			if _, cerr := Compile(def, nil); cerr == nil {
				t.Fatal("Compile should fail when ValidateTemplate fails")
			}
		})
	}
}

func TestCompileDropsDefaultEqualOverrides(t *testing.T) {
	def := catalog.Definition{
		ID: "rp_canon", Name: "Canon", Version: "1.0.0",
		SQLTemplate: "SELECT {{params.n}}, {{params.cmds}}",
		Parameters: []catalog.Parameter{
			{ID: "n", Type: "integer", Default: 10},
			{ID: "cmds", Type: "string_list", Default: []any{"rm", "dd"}},
		},
	}
	// Both overrides equal the defaults after normalization → dropped.
	c, err := Compile(def, map[string]any{"n": float64(10), "cmds": []any{"rm", "dd"}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(c.Overrides) != 0 {
		t.Fatalf("default-equal overrides must be dropped, got %#v", c.Overrides)
	}
	// Different order is a different value → kept.
	c, err = Compile(def, map[string]any{"cmds": []any{"dd", "rm"}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !reflect.DeepEqual(c.Overrides, map[string]any{"cmds": []string{"dd", "rm"}}) {
		t.Fatalf("reordered list must persist as override, got %#v", c.Overrides)
	}
}

func TestCompileRejectsInvalidDefaults(t *testing.T) {
	def := catalog.Definition{
		ID: "rp_baddef", Name: "Bad", Version: "1.0.0",
		SQLTemplate: "SELECT {{params.n}}",
		Parameters:  []catalog.Parameter{{ID: "n", Type: "integer", Default: "ten"}},
	}
	if _, err := Compile(def, nil); err == nil {
		t.Fatal("Compile must reject a definition whose default does not normalize")
	}
}

// FuzzScanPlaceholders asserts the scanner never panics and, when the
// template validates cleanly against a fixed two-parameter definition, every
// placeholder it finds resolves to a declared parameter and reconstructs
// exactly from its byte offsets.
func FuzzScanPlaceholders(f *testing.F) {
	seeds := []string{
		"",
		"SELECT 1",
		"SELECT {{params.alpha}}",
		"SELECT {{params.alpha}} AND {{params.beta}}",
		"SELECT {{params.alpha}} AND {{params.alpha}}",
		"SELECT {{params.unknown}}",
		"SELECT {{ params.alpha }}",
		"SELECT {{params.alpha",
		"{{",
		"}}",
		"{{}}",
		"{{params.}}",
		"{{if x}}{{end}}",
		"{{params.alpha}}{{params.beta}}",
		"{{{params.alpha}}}",
		"{{params.alpha{{params.beta}}",
		"héllo 世界 {{params.alpha}} 🚀",
		"SELECT '{{params.alpha}}' || '\\'",
		strings.Repeat("{", 7) + "params.alpha" + strings.Repeat("}", 7),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	declared := map[string]bool{"alpha": true, "beta": true}
	f.Fuzz(func(t *testing.T, tmpl string) {
		phs, err := scanPlaceholders(tmpl) // must never panic
		if err != nil {
			return
		}
		for _, ph := range phs {
			if ph.Start < 0 || ph.End > len(tmpl) || ph.Start >= ph.End {
				t.Fatalf("placeholder offsets out of range: %+v in %q", ph, tmpl)
			}
			if got := tmpl[ph.Start:ph.End]; got != "{{params."+ph.ID+"}}" {
				t.Fatalf("offsets do not reconstruct token: %q vs id %q", got, ph.ID)
			}
		}
		def := catalog.Definition{
			ID: "rp_fuzz", Name: "Fuzz", Version: "1.0.0",
			SQLTemplate: tmpl,
			Parameters: []catalog.Parameter{
				{ID: "alpha", Type: "integer", Default: 1},
				{ID: "beta", Type: "string", Default: "x"},
			},
		}
		if verr := ValidateTemplate(def); verr == nil {
			for _, ph := range phs {
				if !declared[ph.ID] {
					t.Fatalf("ValidateTemplate accepted undeclared placeholder %q in %q", ph.ID, tmpl)
				}
			}
		}
	})
}

// FuzzRenderStringLiteral asserts every rendered literal is a balanced
// single-quoted SQL string: outer quotes present, and the inner body contains
// no unescaped single quote (every ' is either doubled or preceded by an
// escaping backslash — our escaper only produces doubling, but the checker
// parses both, mirroring how Proton lexes the literal).
func FuzzRenderStringLiteral(f *testing.F) {
	for _, s := range []string{
		"", "plain", "it's", `back\slash`, `\'`, `'`, `''`, `\`, `\\`,
		`'; DROP STREAM x; --`, "héllo 世界 🚀", "end\\", "\x00\n\t",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		lit := renderStringLiteral(s) // must never panic
		if len(lit) < 2 || lit[0] != '\'' || lit[len(lit)-1] != '\'' {
			t.Fatalf("literal %q is not wrapped in single quotes (input %q)", lit, s)
		}
		inner := lit[1 : len(lit)-1]
		for i := 0; i < len(inner); {
			switch inner[i] {
			case '\\':
				if i+1 >= len(inner) {
					t.Fatalf("dangling backslash escapes closing quote: %q (input %q)", lit, s)
				}
				i += 2
			case '\'':
				if i+1 >= len(inner) || inner[i+1] != '\'' {
					t.Fatalf("unescaped single quote at %d in %q (input %q)", i, lit, s)
				}
				i += 2
			default:
				i++
			}
		}
	})
}
