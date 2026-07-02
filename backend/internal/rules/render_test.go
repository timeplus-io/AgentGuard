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
		SQLTemplate: "SELECT * WHERE n > {{ .Config.threshold }} AND r < {{ .Config.ratio }} " +
			"AND strict = {{ .Config.strict }} AND name = {{ .Config.name }} " +
			"AND mode = {{ .Config.mode }} AND cmd IN {{ .Config.cmds }} " +
			"AND service IN {{ .Config.services }}",
		Parameters: []catalog.Parameter{
			{Key: "threshold", Type: "integer", Default: 10},
			{Key: "ratio", Type: "float", Default: 0.5},
			{Key: "strict", Type: "bool", Default: false},
			{Key: "name", Type: "string", Default: "bash"},
			{Key: "mode", Type: "choice", Default: "warn", Options: []string{"warn", "block"}},
			{Key: "cmds", Type: "list", Default: []any{"rm", "dd"}},
			{Key: "services", Type: "multi_choice", Default: []any{"github"}, Options: []string{"github", "slack", "jira"}},
		},
	}
	c, err := Compile(def, map[string]any{
		"threshold": float64(42),
		"strict":    true,
		"cmds":      []any{"curl", "wget"},
		"services":  []any{"github", "slack"},
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := "SELECT * WHERE n > 42 AND r < 0.5 AND strict = true AND name = 'bash' " +
		"AND mode = 'warn' AND cmd IN ('curl', 'wget') AND service IN ('github', 'slack')"
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
		"services":  []string{"github", "slack"},
	}
	if !reflect.DeepEqual(c.Effective, wantEffective) {
		t.Fatalf("Effective = %#v, want %#v", c.Effective, wantEffective)
	}
	wantOverrides := map[string]any{
		"threshold": int64(42),
		"strict":    true,
		"cmds":      []string{"curl", "wget"},
		"services":  []string{"github", "slack"},
	}
	if !reflect.DeepEqual(c.Overrides, wantOverrides) {
		t.Fatalf("Overrides = %#v, want %#v", c.Overrides, wantOverrides)
	}
}

func TestCompileNumberRenderingIsCanonicalDecimal(t *testing.T) {
	def := catalog.Definition{
		ID: "rp_num", Name: "Num", Version: "1.0.0",
		SQLTemplate: "SELECT {{ .Config.v }}",
		Parameters:  []catalog.Parameter{{Key: "v", Type: "float", Default: 0.0}},
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
	p := catalog.Parameter{Key: "ratio", Type: "float", Default: 0.5}
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
		SQLTemplate: "SELECT {{ .Config.s }}",
		Parameters:  []catalog.Parameter{{Key: "s", Type: "string", Default: "x"}},
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
		SQLTemplate: "SELECT cmd IN {{ .Config.cmds }}",
		Parameters:  []catalog.Parameter{{Key: "cmds", Type: "list", Default: []any{"x"}}},
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

func TestMultiChoiceEscaping(t *testing.T) {
	def := catalog.Definition{
		ID: "rp_multi", Name: "Multi", Version: "1.0.0",
		SQLTemplate: "SELECT service IN {{ .Config.services }}",
		Parameters: []catalog.Parameter{{
			Key: "services", Type: "multi_choice",
			Default: []any{"github"},
			Options: []string{"github", `it'\\s`},
		}},
	}
	c, err := Compile(def, map[string]any{"services": []any{"github", `it'\\s`}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := `SELECT service IN ('github', 'it''\\\\s')`
	if c.SQL != want {
		t.Fatalf("got %q, want %q", c.SQL, want)
	}
}

func TestPlaceholderRepeatedOK(t *testing.T) {
	def := catalog.Definition{
		ID: "rp_rep", Name: "Rep", Version: "1.0.0",
		SQLTemplate: "SELECT {{ .Config.n }} + {{ .Config.n }} + {{ .Config.n }}",
		Parameters:  []catalog.Parameter{{Key: "n", Type: "integer", Default: 2}},
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
	param := catalog.Parameter{Key: "x", Type: "integer", Default: 1}
	cases := []struct {
		name    string
		tmpl    string
		params  []catalog.Parameter
		wantErr string
	}{
		{"unknown placeholder", "SELECT {{ .Config.x }} {{ .Config.unknown }}", []catalog.Parameter{param}, "does not match any declared parameter"},
		{"template action", "SELECT {{if x}}1{{end}}", nil, "invalid placeholder"},
		{"legacy params namespace", "SELECT {{params.x}}", []catalog.Parameter{param}, "invalid placeholder"},
		{"trailing space in key", "SELECT {{params.x }} {{params.x}}", []catalog.Parameter{param}, "invalid placeholder"},
		{"unterminated", "SELECT {{ .Config.x", []catalog.Parameter{param}, "unclosed '{{'"},
		{"unterminated after valid", "SELECT {{ .Config.x }} AND {{ .Config.x", []catalog.Parameter{param}, "unclosed '{{'"},
		{"declared param never used", "SELECT 1", []catalog.Parameter{param}, `parameter "x" is never used`},
		{"empty key", "SELECT {{.Config.}}", []catalog.Parameter{param}, "invalid placeholder"},
		{"wrong namespace", "SELECT {{values.x}}", []catalog.Parameter{param}, "invalid placeholder"},
		{"uppercase key", "SELECT {{.Config.X}} {{.Config.x}}", []catalog.Parameter{param}, "invalid placeholder"},
		{"nested open", "SELECT {{.Config.a{{.Config.x}}", []catalog.Parameter{param}, "invalid placeholder"},
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
		SQLTemplate: "SELECT {{ .Config.n }}, {{ .Config.cmds }}",
		Parameters: []catalog.Parameter{
			{Key: "n", Type: "integer", Default: 10},
			{Key: "cmds", Type: "list", Default: []any{"rm", "dd"}},
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
		SQLTemplate: "SELECT {{ .Config.n }}",
		Parameters:  []catalog.Parameter{{Key: "n", Type: "integer", Default: "ten"}},
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
		"SELECT {{ .Config.alpha }}",
		"SELECT {{ .Config.alpha }} AND {{ .Config.beta }}",
		"SELECT {{ .Config.alpha }} AND {{ .Config.alpha }}",
		"SELECT {{ .Config.unknown }}",
		"SELECT {{.Config.alpha}}",
		"SELECT {{ .Config.alpha",
		"{{",
		"}}",
		"{{}}",
		"{{.Config.}}",
		"{{if x}}{{end}}",
		"{{ .Config.alpha }}{{ .Config.beta }}",
		"{{{ .Config.alpha }}}",
		"{{ .Config.alpha {{ .Config.beta }}",
		"héllo 世界 {{ .Config.alpha }} 🚀",
		"SELECT '{{ .Config.alpha }}' || '\\'",
		strings.Repeat("{", 7) + ".Config.alpha" + strings.Repeat("}", 7),
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
			// The token bytes are {{ + (optional whitespace) + .Config.<key> + (optional whitespace) + }}.
			got := tmpl[ph.Start:ph.End]
			inner := strings.TrimSpace(got[2 : len(got)-2])
			if inner != ".Config."+ph.ID {
				t.Fatalf("offsets do not reconstruct token: inner=%q vs key %q", inner, ph.ID)
			}
		}
		def := catalog.Definition{
			ID: "rp_fuzz", Name: "Fuzz", Version: "1.0.0",
			SQLTemplate: tmpl,
			Parameters: []catalog.Parameter{
				{Key: "alpha", Type: "integer", Default: 1},
				{Key: "beta", Type: "string", Default: "x"},
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

func TestRender_Regex(t *testing.T) {
	p := catalog.Parameter{Key: "pat", Type: "regex", Default: "a'b"}
	out, err := renderValue(p, "a'b")
	if err != nil {
		t.Fatal(err)
	}
	if out != "'a''b'" {
		t.Fatalf("got %q, want %q", out, "'a''b'")
	}
}

func TestRender_Duration(t *testing.T) {
	p := catalog.Parameter{Key: "w", Type: "duration", Unit: "seconds", Default: 60}
	out, err := renderValue(p, int64(90))
	if err != nil || out != "90" {
		t.Fatalf("got %q %v", out, err)
	}
}

func TestCompileRendersRegexAndDuration(t *testing.T) {
	def := catalog.Definition{
		ID: "rp_phase_b", Name: "Phase B Types", Version: "1.0.0",
		SQLTemplate: "SELECT * WHERE match(tool_name, {{ .Config.pat }}) AND event_time > now() - INTERVAL {{ .Config.window_seconds }} SECOND",
		Parameters: []catalog.Parameter{
			{Key: "pat", Type: "regex", Default: "rm|dd"},
			{Key: "window_seconds", Type: "duration", Unit: "seconds", Default: 300, Min: fptr(60), Max: fptr(3600)},
		},
	}
	c, err := Compile(def, map[string]any{
		"pat":            "curl|wget",
		"window_seconds": 600,
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := "SELECT * WHERE match(tool_name, 'curl|wget') AND event_time > now() - INTERVAL 600 SECOND"
	if c.SQL != want {
		t.Fatalf("SQL:\n got %q\nwant %q", c.SQL, want)
	}
}

// The authoring surface ships '__rule_id__'/'__rule_name__' identity sentinels
// in its default template (a template is written before its rule id is known).
// Compile must bake them from the definition's own id/name so an installed MV
// writes correctly-attributed detection rows — see the following tests.

func TestCompileBakesIdentitySentinels(t *testing.T) {
	def := catalog.Definition{
		ID: "my-rule", Name: "My Rule", Version: "1.0.0",
		SQLTemplate: "SELECT '__rule_id__' AS rule_id, '__rule_name__' AS rule_name FROM agentguard_hook_events WHERE 1 = 0",
	}
	c, err := Compile(def, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if strings.Contains(c.SQL, "__rule_id__") || strings.Contains(c.SQL, "__rule_name__") {
		t.Fatalf("identity sentinels left unsubstituted: %q", c.SQL)
	}
	if !strings.Contains(c.SQL, "'my-rule' AS rule_id") {
		t.Errorf("rule_id sentinel not baked from def.ID: %q", c.SQL)
	}
	if !strings.Contains(c.SQL, "'My Rule' AS rule_name") {
		t.Errorf("rule_name sentinel not baked from def.Name: %q", c.SQL)
	}
}

func TestCompileHashIsOriginalDefinitionNotBaked(t *testing.T) {
	def := catalog.Definition{
		ID: "my-rule", Name: "My Rule", Version: "1.0.0",
		SQLTemplate: "SELECT '__rule_id__' AS rule_id, '__rule_name__' AS rule_name FROM agentguard_hook_events WHERE 1 = 0",
	}
	c, err := Compile(def, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// The compiled hash must equal the ORIGINAL (sentinel-carrying) definition's
	// hash, not the baked copy's — otherwise an installed snapshot's stored hash
	// would never match the catalog definition's hash and the rule would look
	// permanently "upgradeable".
	if c.DefinitionHash != def.Hash() {
		t.Errorf("DefinitionHash = %q, want original def.Hash() %q", c.DefinitionHash, def.Hash())
	}
	// Guard the guard: baking genuinely changes the hashed content, so this
	// assertion is only meaningful if the baked copy's hash actually differs.
	baked := def
	baked.SQLTemplate = "SELECT 'my-rule' AS rule_id, 'My Rule' AS rule_name FROM agentguard_hook_events WHERE 1 = 0"
	if baked.Hash() == def.Hash() {
		t.Fatal("test precondition failed: baked template hashes identically to original")
	}
}

func TestCompileDoesNotRewriteParamValueEqualToSentinel(t *testing.T) {
	// Baking must happen BEFORE parameter rendering: a string parameter whose
	// value is literally "__rule_id__" must survive into the SQL unchanged, not
	// be rewritten to the rule id by a bake step that ran after rendering.
	def := catalog.Definition{
		ID: "my-rule", Name: "My Rule", Version: "1.0.0",
		SQLTemplate: "SELECT '__rule_id__' AS rule_id FROM agentguard_hook_events WHERE tool_name = {{ .Config.needle }}",
		Parameters:  []catalog.Parameter{{Key: "needle", Label: "Needle", Type: "string", Default: "__rule_id__"}},
	}
	c, err := Compile(def, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(c.SQL, "'my-rule' AS rule_id") {
		t.Errorf("identity column not baked: %q", c.SQL)
	}
	if !strings.Contains(c.SQL, "tool_name = '__rule_id__'") {
		t.Errorf("parameter value equal to the sentinel was rewritten: %q", c.SQL)
	}
}
