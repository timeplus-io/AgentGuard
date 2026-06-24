package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"
)

func f64(v float64) *float64 { return &v }

// validDef returns a minimal Definition that passes Validate.
func validDef() Definition {
	return Definition{
		ID:          "rp-test",
		Name:        "Test Rule",
		Version:     "1.0.0",
		SQLTemplate: "SELECT 1",
	}
}

func TestDefinitionValidate_RequiredFields(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Definition)
		wantErr string
	}{
		{"valid", func(d *Definition) {}, ""},
		{"missing id", func(d *Definition) { d.ID = "" }, "id is required"},
		{"missing name", func(d *Definition) { d.Name = "" }, "name is required"},
		{"missing version", func(d *Definition) { d.Version = "" }, "version is required"},
		{"missing sql_template", func(d *Definition) { d.SQLTemplate = "" }, "sql_template is required"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := validDef()
			c.mutate(&d)
			err := d.Validate()
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("expected error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

func TestDefinitionValidate_Parameters(t *testing.T) {
	param := func(mutate func(*Parameter)) Parameter {
		p := Parameter{Key: "threshold", Label: "Threshold", Type: "integer", Default: 5}
		mutate(&p)
		return p
	}
	cases := []struct {
		name    string
		params  []Parameter
		wantErr string
	}{
		{"valid integer", []Parameter{param(func(p *Parameter) {})}, ""},
		{"valid all types", []Parameter{
			{Key: "a_int", Type: "integer", Default: 1},
			{Key: "a_num", Type: "float", Default: 1.5, Min: f64(0), Max: f64(10)},
			{Key: "a_bool", Type: "bool", Default: true},
			{Key: "a_str", Type: "string", Default: "x"},
			{Key: "a_enum", Type: "choice", Default: "low", Options: []string{"low", "high"}},
			{Key: "a_list", Type: "list", Default: []any{"x"}},
		}, ""},
		{"empty key", []Parameter{param(func(p *Parameter) { p.Key = "" })}, "must match"},
		{"uppercase key", []Parameter{param(func(p *Parameter) { p.Key = "Threshold" })}, "must match"},
		{"leading digit", []Parameter{param(func(p *Parameter) { p.Key = "1x" })}, "must match"},
		{"hyphen key", []Parameter{param(func(p *Parameter) { p.Key = "max-tools" })}, "must match"},
		{"key too long (65)", []Parameter{param(func(p *Parameter) { p.Key = "a" + strings.Repeat("b", 64) })}, "must match"},
		{"key max length (64) ok", []Parameter{param(func(p *Parameter) { p.Key = "a" + strings.Repeat("b", 63) })}, ""},
		{"duplicate keys", []Parameter{
			param(func(p *Parameter) {}),
			param(func(p *Parameter) { p.Key = "threshold" }),
		}, "duplicates"},
		{"key collides with alias", []Parameter{
			param(func(p *Parameter) { p.Aliases = []string{"limit"} }),
			param(func(p *Parameter) { p.Key = "limit" }),
		}, "duplicates"},
		{"alias collides with alias", []Parameter{
			param(func(p *Parameter) { p.Aliases = []string{"limit"} }),
			param(func(p *Parameter) { p.Key = "other"; p.Aliases = []string{"limit"} }),
		}, "duplicates"},
		{"bad alias", []Parameter{param(func(p *Parameter) { p.Aliases = []string{"Bad-Alias"} })}, "must match"},
		{"unknown type", []Parameter{param(func(p *Parameter) { p.Type = "frobnicate" })}, "unknown type"},
		// The Phase A internal type names were renamed to the Apps vocabulary and
		// are no longer accepted (rejected, not aliased).
		{"legacy number rejected", []Parameter{param(func(p *Parameter) { p.Type = "number" })}, "unknown type"},
		{"legacy boolean rejected", []Parameter{param(func(p *Parameter) { p.Type = "boolean" })}, "unknown type"},
		{"legacy enum rejected", []Parameter{param(func(p *Parameter) { p.Type = "enum" })}, "unknown type"},
		{"legacy string_list rejected", []Parameter{param(func(p *Parameter) { p.Type = "string_list" })}, "unknown type"},
		{"nil default", []Parameter{param(func(p *Parameter) { p.Default = nil })}, "default is required"},
		{"choice without options", []Parameter{{Key: "mode", Type: "choice", Default: "x"}}, "non-empty options"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := validDef()
			d.Parameters = c.params
			err := d.Validate()
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("expected error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

func TestDefinitionValidate_UDFs(t *testing.T) {
	cases := []struct {
		name    string
		udfs    []string
		wantErr string
	}{
		{"scalar", []string{"CREATE OR REPLACE FUNCTION ag_check(v string) RETURNS bool LANGUAGE JAVASCRIPT AS $$ ... $$"}, ""},
		{"aggregate", []string{"CREATE OR REPLACE AGGREGATE FUNCTION ag_fsm(e string) RETURNS uint8 LANGUAGE JAVASCRIPT AS $$ ... $$"}, ""},
		{"plain create", []string{"CREATE FUNCTION plain_fn(v string) RETURNS bool AS $$ x $$"}, ""},
		{"case insensitive", []string{"create or replace function lower_fn(v string) returns bool as $$ x $$"}, ""},
		{"unparseable", []string{"DROP FUNCTION nope"}, "cannot extract function name"},
		{"duplicate names", []string{
			"CREATE OR REPLACE FUNCTION ag_dup(v string) AS $$ a $$",
			"CREATE FUNCTION ag_dup(v string) AS $$ b $$",
		}, "duplicate UDF function name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := validDef()
			d.UDFs = c.udfs
			err := d.Validate()
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("expected error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

func TestExtractUDFNames(t *testing.T) {
	names, err := ExtractUDFNames([]string{
		"CREATE OR REPLACE FUNCTION ag_r_score(x string) RETURNS float LANGUAGE PYTHON AS $$...$$;",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "ag_r_score" {
		t.Fatalf("got %v", names)
	}
}

func TestUDFNames(t *testing.T) {
	d := validDef()
	d.UDFs = []string{
		"-- leading comment\nCREATE OR REPLACE AGGREGATE FUNCTION ag_one(e string) AS $$ x $$",
		"CREATE FUNCTION ag_two(v string) AS $$ y $$",
	}
	names, err := d.UDFNames()
	if err != nil {
		t.Fatalf("UDFNames: %v", err)
	}
	if len(names) != 2 || names[0] != "ag_one" || names[1] != "ag_two" {
		t.Errorf("UDFNames = %v", names)
	}
}

func TestUDFNames_Extraction(t *testing.T) {
	cases := []struct {
		name    string
		ddl     string
		want    string
		wantErr string
	}{
		{
			// A changelog comment mentioning an old CREATE FUNCTION must not
			// shadow the real declaration that follows.
			"comment mentioning old CREATE FUNCTION",
			"-- v0.2.0: renamed from CREATE FUNCTION ag_old to the new name\nCREATE OR REPLACE FUNCTION ag_new(v string) AS $$ x $$",
			"ag_new", "",
		},
		{
			"IF NOT EXISTS form",
			"CREATE FUNCTION IF NOT EXISTS ag_ine(v string) AS $$ x $$",
			"ag_ine", "",
		},
		{
			"backtick-quoted name",
			"CREATE FUNCTION `ag_quoted`(v string) AS $$ x $$",
			"ag_quoted", "",
		},
		{
			"backtick-quoted aggregate with IF NOT EXISTS",
			"CREATE AGGREGATE FUNCTION IF NOT EXISTS `ag_qagg`(v string) AS $$ x $$",
			"ag_qagg", "",
		},
		{
			"CREATE AGGREGATE FUNCTION without OR REPLACE",
			"CREATE AGGREGATE FUNCTION ag_agg(e string) RETURNS uint8 AS $$ x $$",
			"ag_agg", "",
		},
		{
			// Asymmetric backtick: opening quote without a closing one must
			// not silently yield a name.
			"unterminated backtick",
			"CREATE FUNCTION `ag_broken(v string) AS $$ x $$",
			"", "cannot extract function name",
		},
		{
			// `IF` followed by something other than NOT EXISTS would
			// previously be extracted as the function name "IF".
			"malformed IF clause rejected",
			"CREATE FUNCTION IF (v string) AS $$ x $$",
			"", "reserved word",
		},
		{
			"DDL that is only a comment",
			"-- CREATE FUNCTION ag_ghost(v string) AS $$ x $$",
			"", "cannot extract function name",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := validDef()
			d.UDFs = []string{c.ddl}
			names, err := d.UDFNames()
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("expected error containing %q, got (%v, %v)", c.wantErr, names, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("UDFNames: %v", err)
			}
			if len(names) != 1 || names[0] != c.want {
				t.Errorf("UDFNames = %v, want [%s]", names, c.want)
			}
		})
	}
}

func TestDefinitionHash(t *testing.T) {
	d := validDef()
	h1 := d.Hash()
	if len(h1) != 64 {
		t.Fatalf("expected 64-char sha256 hex, got %q", h1)
	}
	if d.Hash() != h1 {
		t.Error("Hash is not deterministic")
	}

	// Content fields change the hash.
	mutations := []func(*Definition){
		func(d *Definition) { d.Name = "Other" },
		func(d *Definition) { d.Version = "2.0.0" },
		func(d *Definition) { d.SQLTemplate = "SELECT 2" },
		func(d *Definition) { d.Tags = []string{"x"} },
		func(d *Definition) { d.UDFs = []string{"CREATE FUNCTION f(v string) AS $$ $$"} },
		func(d *Definition) {
			d.Parameters = []Parameter{{Key: "p", Type: "integer", Default: 1}}
		},
	}
	for i, mutate := range mutations {
		d2 := validDef()
		mutate(&d2)
		if d2.Hash() == h1 {
			t.Errorf("mutation %d did not change hash", i)
		}
	}

	// Parameter order is part of the canonical content.
	a := validDef()
	a.Parameters = []Parameter{
		{Key: "p1", Type: "integer", Default: 1},
		{Key: "p2", Type: "integer", Default: 2},
	}
	b := validDef()
	b.Parameters = []Parameter{a.Parameters[1], a.Parameters[0]}
	if a.Hash() == b.Hash() {
		t.Error("parameter order should affect the hash")
	}

	// Provenance fields do NOT change the hash — it covers definition content only.
	d3 := validDef()
	d3.Origin = OriginImported
	d3.Author = "someone-else"
	if d3.Hash() != h1 {
		t.Error("Origin/Author must not affect the content hash")
	}
}

// Nil and empty slices must hash identically — YAML/JSON round-trips flip
// freely between the two, and the persisted definition_hash must be stable
// across that.
func TestDefinitionHash_NilAndEmptySlicesEqual(t *testing.T) {
	a := validDef() // all slices nil
	b := validDef()
	b.Tags = []string{}
	b.UDFs = []string{}
	b.Parameters = []Parameter{}
	if a.Hash() != b.Hash() {
		t.Error("nil and empty Tags/UDFs/Parameters must hash identically")
	}

	c := validDef()
	c.Parameters = []Parameter{{Key: "p", Type: "choice", Default: "x", Options: []string{"x"}, Aliases: nil}}
	d := validDef()
	d.Parameters = []Parameter{{Key: "p", Type: "choice", Default: "x", Options: []string{"x"}, Aliases: []string{}}}
	if c.Hash() != d.Hash() {
		t.Error("nil and empty parameter Aliases must hash identically")
	}
}

// TestDefinitionHash_Golden pins the exact digest of a fixed definition.
// The hash is persisted as definition_hash in snapshots, so any change to
// the canonical serialization (field set, ordering, nil-normalization)
// silently invalidates stored hashes. If this test fails, you have changed
// the hash function — that requires a deliberate migration story, not just
// updating the constant.
//
// Hash rotation log:
//   - Phase A baseline: "edaa7b3808890082a8cd0176cce2414d6f7f935552b3aa7831d0a27e3581b72b"
//     (Parameter serialized with json:"id")
//   - Phase B /apps rename (#33): deliberately rotated by Parameter.ID→Key rename
//     (json tag "id"→"key" changes canonical serialization; clean reprovision required)
func TestDefinitionHash_Golden(t *testing.T) {
	d := Definition{
		ID:                "rp-golden",
		Name:              "Golden Rule",
		Version:           "1.2.3",
		Description:       "Pinned content hash",
		Severity:          "high",
		Category:          "exfiltration",
		FPRate:            "low",
		HookSources:       "PreToolUse",
		RequiredTelemetry: "tool.call",
		Author:            "agentguard", // excluded from the hash
		Tags:              []string{"golden", "pin"},
		UDFs:              []string{"CREATE FUNCTION ag_golden(v string) RETURNS bool AS $$ x $$"},
		SQLTemplate:       "SELECT 1 WHERE x > {{threshold}}",
		Parameters: []Parameter{{
			Key: "threshold", Label: "Threshold", Description: "limit",
			Type: "integer", Default: 5, Min: f64(1), Max: f64(10), Unit: "calls",
		}},
		Origin: OriginBuiltin, // excluded from the hash
	}
	const want = "f195216132275a6dd02caf840bb87c687d35964e414015624de949c8dd1550f9"
	if got := d.Hash(); got != want {
		t.Errorf("golden hash mismatch:\n got  %s\n want %s", got, want)
	}
}

// TestParameter_KeyAndSecret verifies the /apps authoring surface:
// - the YAML "config:" block deserializes into d.Parameters with Key populated;
// - secret:true is rejected by Validate (parameters render into SQL).
func TestParameter_KeyAndSecret(t *testing.T) {
	y := []byte("id: r\nname: n\nversion: \"1\"\nsql_template: \"x {{ .Config.k }}\"\nconfig:\n  - key: k\n    type: integer\n    default: 1\n")
	var d Definition
	if err := yaml.Unmarshal(y, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.Parameters) != 1 || d.Parameters[0].Key != "k" {
		t.Fatalf("expected config[0].key=k, got %+v", d.Parameters)
	}
	// secret:true must be rejected by Validate
	d.Parameters[0].Secret = true
	if err := d.Validate(); err == nil {
		t.Fatal("expected secret:true to be rejected")
	}
}

func TestValidateUDFOwnership(t *testing.T) {
	seed := fstest.MapFS{
		"a.yaml": {Data: []byte("id: rp-001\nname: A\nversion: 1.0.0\nsql_template: SELECT 1\nudfs:\n  - CREATE OR REPLACE FUNCTION ag_owned(v string) AS $$ x $$\n")},
	}
	m, err := New(seed, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Run("conflict with other rule", func(t *testing.T) {
		d := validDef()
		d.ID = "rp-002"
		d.UDFs = []string{"CREATE FUNCTION ag_owned(v string) AS $$ y $$"}
		err := m.ValidateUDFOwnership(d)
		if err == nil || !strings.Contains(err.Error(), "rp-001") {
			t.Fatalf("expected ownership conflict naming rp-001, got %v", err)
		}
	})

	t.Run("same rule re-save allowed", func(t *testing.T) {
		d := validDef()
		d.ID = "rp-001"
		d.UDFs = []string{"CREATE OR REPLACE FUNCTION ag_owned(v string) AS $$ z $$"}
		if err := m.ValidateUDFOwnership(d); err != nil {
			t.Fatalf("same-ID re-save should be allowed: %v", err)
		}
	})

	t.Run("fresh name allowed", func(t *testing.T) {
		d := validDef()
		d.ID = "rp-003"
		d.UDFs = []string{"CREATE FUNCTION ag_fresh(v string) AS $$ z $$"}
		if err := m.ValidateUDFOwnership(d); err != nil {
			t.Fatalf("fresh UDF name should be allowed: %v", err)
		}
	})

	t.Run("no udfs allowed", func(t *testing.T) {
		if err := m.ValidateUDFOwnership(validDef()); err != nil {
			t.Fatalf("definition without UDFs should pass: %v", err)
		}
	})
}

func TestSave_RejectsInvalidDefinition(t *testing.T) {
	m, err := New(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	d := validDef()
	d.SQLTemplate = ""
	if err := m.Save(d); err == nil || !strings.Contains(err.Error(), "sql_template") {
		t.Fatalf("Save should reject invalid definition, got %v", err)
	}
	if m.Exists(d.ID) {
		t.Error("invalid definition must not enter the catalog")
	}
}

func TestSave_RejectsUDFOwnershipConflict(t *testing.T) {
	seed := fstest.MapFS{
		"a.yaml": {Data: []byte("id: rp-001\nname: A\nversion: 1.0.0\nsql_template: SELECT 1\nudfs:\n  - CREATE FUNCTION ag_owned(v string) AS $$ x $$\n")},
	}
	m, err := New(seed, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	d := validDef()
	d.ID = "rp-002"
	d.UDFs = []string{"CREATE FUNCTION ag_owned(v string) AS $$ y $$"}
	if err := m.Save(d); err == nil || !strings.Contains(err.Error(), "already declared") {
		t.Fatalf("Save should reject UDF ownership conflict, got %v", err)
	}
}

func TestSave_DefaultsOriginToCustom(t *testing.T) {
	m, err := New(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := m.Save(validDef()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, _ := m.Lookup("rp-test")
	if got.Origin != OriginCustom {
		t.Errorf("Origin = %q, want %q", got.Origin, OriginCustom)
	}
}

// TestDefinitionValidate_PhaseB pins Phase B type acceptance.
func TestDefinitionValidate_PhaseB(t *testing.T) {
	// regex with no extra constraints — must be accepted.
	d := validDef()
	d.SQLTemplate = "SELECT {{ .Config.pat }}"
	d.Parameters = []Parameter{{Key: "pat", Type: "regex", Default: "a.*b"}}
	if err := d.Validate(); err != nil {
		t.Fatalf("regex parameter must be accepted by Validate: %v", err)
	}

	// duration with unit — must be accepted.
	d2 := validDef()
	d2.SQLTemplate = "SELECT {{ .Config.w }}"
	d2.Parameters = []Parameter{{Key: "w", Type: "duration", Unit: "seconds", Default: 60}}
	if err := d2.Validate(); err != nil {
		t.Fatalf("duration parameter with unit must be accepted by Validate: %v", err)
	}
}

// TestValidate_DurationRequiresUnit rejects a duration parameter with an empty unit.
func TestValidate_DurationRequiresUnit(t *testing.T) {
	d := Definition{
		ID: "r", Name: "n", Version: "1",
		SQLTemplate: "x {{ .Config.w }}",
		Parameters:  []Parameter{{Key: "w", Type: "duration", Default: 60}},
	}
	if err := d.Validate(); err == nil {
		t.Fatal("duration without unit must be rejected")
	}
}

// Origin loader rules: seed definitions are always builtin (overriding any
// stored origin); local-dir definitions keep their stored origin, defaulting
// to custom when absent.
func TestOrigin_LoaderRules(t *testing.T) {
	seed := fstest.MapFS{
		"a.yaml": {Data: []byte("id: rp-001\nname: A\nversion: 1.0.0\nsql_template: SELECT 1\norigin: custom\n")},
	}
	tmp := t.TempDir()
	writeLocal := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	writeLocal("rp-002.yaml", "id: rp-002\nname: B\nversion: 1.0.0\nsql_template: SELECT 1\norigin: imported\n")
	writeLocal("rp-003.yaml", "id: rp-003\nname: C\nversion: 1.0.0\nsql_template: SELECT 1\n")

	m, err := New(seed, tmp)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d, _ := m.Lookup("rp-001"); d.Origin != OriginBuiltin {
		t.Errorf("seed origin = %q, want builtin (stored origin must be overridden)", d.Origin)
	}
	if d, _ := m.Lookup("rp-002"); d.Origin != OriginImported {
		t.Errorf("local stored origin = %q, want imported (kept)", d.Origin)
	}
	if d, _ := m.Lookup("rp-003"); d.Origin != OriginCustom {
		t.Errorf("local absent origin = %q, want custom (default)", d.Origin)
	}
}

// TestSeedFS_AllDefinitionsValid asserts every embedded seed definition passes
// structural validation and that no two seed definitions collide on UDF
// function names or on their hyphen-insensitive MV-namespace key (MVResourceKey)
// — the latter is a real invariant the Save/load/install guards enforce, so the
// shipped seeds must satisfy it too.
func TestSeedFS_AllDefinitionsValid(t *testing.T) {
	m, err := New(SeedFS, "")
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	defs := m.All()
	if len(defs) == 0 {
		t.Fatal("seed catalog loaded zero definitions")
	}
	udfOwner := map[string]string{}   // function name → rule ID
	mvKeyOwner := map[string]string{} // MV resource key → rule ID
	for _, d := range defs {
		if err := d.Validate(); err != nil {
			t.Errorf("seed definition %s fails Validate: %v", d.ID, err)
			continue
		}
		if d.Origin != OriginBuiltin {
			t.Errorf("seed definition %s has origin %q, want builtin", d.ID, d.Origin)
		}
		if owner, dup := mvKeyOwner[MVResourceKey(d.ID)]; dup {
			t.Errorf("MV resource key %q claimed by both %s and %s (hyphen-insensitive collision)", MVResourceKey(d.ID), owner, d.ID)
		}
		mvKeyOwner[MVResourceKey(d.ID)] = d.ID
		names, err := d.UDFNames()
		if err != nil {
			t.Errorf("seed definition %s: UDFNames: %v", d.ID, err)
			continue
		}
		for _, n := range names {
			if owner, dup := udfOwner[n]; dup {
				t.Errorf("UDF %q declared by both %s and %s", n, owner, d.ID)
			}
			udfOwner[n] = d.ID
		}
	}
}

// TestErrInvalid_IsWrapped verifies that Validate() wraps errors with ErrInvalid
// so callers can use errors.Is(err, ErrInvalid) to distinguish schema defects.
func TestErrInvalid_IsWrapped(t *testing.T) {
	d := Definition{} // missing required fields
	err := d.Validate()
	if err == nil {
		t.Fatal("expected error from empty Definition")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid to be wrapped, got %v", err)
	}
	if !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("expected 'id is required' in error, got %q", err.Error())
	}
}

// TestRescopeUDFs_RenamesAndRewritesSQLTemplate verifies that RescopeUDFs
// renames UDF function names to avoid collisions with the source rule after
// cloning, and that the SQLTemplate references are updated accordingly.
func TestRescopeUDFs_RenamesAndRewritesSQLTemplate(t *testing.T) {
	src := Definition{
		ID:          "rp-003",
		SQLTemplate: "SELECT ag_rp003_score(tool_input) AS s, ag_rp003_count(session_id) AS c",
		UDFs: []string{
			"CREATE OR REPLACE FUNCTION ag_rp003_score(x string) RETURNS float LANGUAGE JAVASCRIPT AS $$ return 1.0; $$;",
			"CREATE OR REPLACE FUNCTION ag_rp003_count(x string) RETURNS uint64 LANGUAGE JAVASCRIPT AS $$ return 0; $$;",
		},
	}

	out, err := RescopeUDFs(src, "my-clone")
	if err != nil {
		t.Fatalf("RescopeUDFs: %v", err)
	}

	names, err := out.UDFNames()
	if err != nil {
		t.Fatalf("UDFNames after rescopeing: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 UDF names, got %v", names)
	}

	// Old names must be gone, new names must be present.
	for _, old := range []string{"ag_rp003_score", "ag_rp003_count"} {
		if strings.Contains(out.SQLTemplate, old) {
			t.Errorf("old name %q still present in SQLTemplate", old)
		}
	}
	for _, newName := range names {
		if !strings.Contains(out.SQLTemplate, newName) {
			t.Errorf("new name %q not found in SQLTemplate", newName)
		}
	}

	// New names should include the normalized new ID.
	norm := "myclone" // "my-clone" with "-" stripped
	for _, n := range names {
		if !strings.Contains(n, norm) {
			t.Errorf("new name %q does not contain normalized id %q", n, norm)
		}
	}
}

// TestRescopeUDFs_NoUDFs verifies that a definition without UDFs passes through
// unchanged.
func TestRescopeUDFs_NoUDFs(t *testing.T) {
	src := validDef()
	out, err := RescopeUDFs(src, "clone-id")
	if err != nil {
		t.Fatalf("RescopeUDFs with no UDFs: %v", err)
	}
	if out.SQLTemplate != src.SQLTemplate {
		t.Errorf("SQLTemplate changed unexpectedly")
	}
}

// TestRescopeUDFs_UDFBodyCrossReference verifies that when a UDF body references
// another UDF by name (rare but valid), the cross-reference is also rewritten.
func TestRescopeUDFs_UDFBodyCrossReference(t *testing.T) {
	src := Definition{
		ID:          "rp-010",
		SQLTemplate: "SELECT ag_rp010_outer(tool_input) AS r",
		UDFs: []string{
			"CREATE OR REPLACE FUNCTION ag_rp010_inner(x string) RETURNS float LANGUAGE JAVASCRIPT AS $$ return 1.0; $$;",
			// Second UDF references the first by name in its body.
			"CREATE OR REPLACE FUNCTION ag_rp010_outer(x string) RETURNS float LANGUAGE JAVASCRIPT AS $$ return ag_rp010_inner(x) * 2.0; $$;",
		},
	}

	out, err := RescopeUDFs(src, "rp-010-custom")
	if err != nil {
		t.Fatalf("RescopeUDFs: %v", err)
	}

	// The outer UDF's body must not contain the old inner name.
	for _, udf := range out.UDFs {
		if strings.Contains(udf, "ag_rp010_inner") || strings.Contains(udf, "ag_rp010_outer") {
			t.Errorf("old UDF name still present after rescope: %s", udf[:min(len(udf), 80)])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
