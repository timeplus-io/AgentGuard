package catalog

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// versionGreater here lives in catalog/import.go and uses strconv.Atoi.
// The handlers package has a sibling implementation (rules.go) that uses
// fmt.Sscanf — both are tested independently to guard against drift.
func TestVersionGreater(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"equal", "1.0.0", "1.0.0", false},
		{"patch greater", "1.0.1", "1.0.0", true},
		{"patch lesser", "1.0.0", "1.0.1", false},
		{"minor greater", "1.1.0", "1.0.99", true},
		{"major greater", "2.0.0", "1.99.99", true},
		{"missing parts treated as 0", "1.2", "1.2.0", false},
		{"missing parts: 1.3 vs 1.2.99 → true", "1.3", "1.2.99", true},
		{"non-numeric parts treated as 0", "abc", "0.0.0", false},
		{"empty strings", "", "", false},
		// SplitN(_, _, 3) means a 4th component goes inside the third part:
		// "1.0.0.5" → ["1", "0", "0.5"]. Atoi("0.5") fails → 0. Equal to "1.0.0".
		{"4-part input — extra ignored", "1.0.0.5", "1.0.0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := versionGreater(c.a, c.b); got != c.want {
				t.Errorf("versionGreater(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

// buildZip assembles an in-memory zip from name → content pairs.
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// ImportZip must skip invalid definitions with a reported error while still
// importing the valid ones, and must mark everything it merges as origin
// "imported" regardless of what the zip entry claims.
func TestImportZip_ValidationAndOrigin(t *testing.T) {
	m, err := New(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	data := buildZip(t, map[string]string{
		"good.yaml":    "id: rp-good\nname: Good\nversion: 1.0.0\nsql_template: SELECT 1\norigin: custom\n",
		"no-sql.yaml":  "id: rp-nosql\nname: No SQL\nversion: 1.0.0\n",
		"bad-udf.yaml": "id: rp-badudf\nname: Bad UDF\nversion: 1.0.0\nsql_template: SELECT 1\nudfs:\n  - DROP FUNCTION nope\n",
	})
	res, err := m.ImportBytes(data, false)
	if err != nil {
		t.Fatalf("ImportBytes: %v", err)
	}
	if len(res.Added) != 1 || res.Added[0] != "rp-good" {
		t.Errorf("Added = %v, want [rp-good]", res.Added)
	}
	if len(res.Errors) != 2 {
		t.Errorf("Errors = %v, want 2 entries (missing sql_template, unparseable UDF)", res.Errors)
	}
	if m.Exists("rp-nosql") || m.Exists("rp-badudf") {
		t.Error("invalid definitions must not enter the catalog")
	}
	got, ok := m.Lookup("rp-good")
	if !ok {
		t.Fatal("rp-good not imported")
	}
	if got.Origin != OriginImported {
		t.Errorf("imported origin = %q, want %q (zip claimed custom)", got.Origin, OriginImported)
	}
}

// A zip definition whose UDF name is owned by a different catalog rule is
// skipped with a reported error; the rest of the zip still imports.
func TestImportZip_UDFOwnershipConflictSkipped(t *testing.T) {
	seed := fstest.MapFS{
		"a.yaml": {Data: []byte("id: rp-001\nname: A\nversion: 1.0.0\nsql_template: SELECT 1\nudfs:\n  - CREATE FUNCTION ag_owned(v string) AS $$ x $$\n")},
	}
	m, err := New(seed, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	data := buildZip(t, map[string]string{
		"conflict.yaml": "id: rp-002\nname: Conflict\nversion: 1.0.0\nsql_template: SELECT 1\nudfs:\n  - CREATE FUNCTION ag_owned(v string) AS $$ y $$\n",
		"ok.yaml":       "id: rp-003\nname: OK\nversion: 1.0.0\nsql_template: SELECT 1\n",
	})
	res, err := m.ImportBytes(data, false)
	if err != nil {
		t.Fatalf("ImportBytes: %v", err)
	}
	if len(res.Added) != 1 || res.Added[0] != "rp-003" {
		t.Errorf("Added = %v, want [rp-003]", res.Added)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "rp-001") {
		t.Errorf("Errors = %v, want one ownership error naming rp-001", res.Errors)
	}
	if m.Exists("rp-002") {
		t.Error("conflicting definition must not enter the catalog")
	}
}

// TestImportZip_SemanticValidatorSkips proves the injected compiler-semantic
// check runs per entry during import: a structurally-valid entry that fails the
// validator is skipped with a reported error while valid entries still import.
func TestImportZip_SemanticValidatorSkips(t *testing.T) {
	m, err := New(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.SemanticValidator = func(d Definition) error {
		if d.ID == "rp-bad" {
			return errors.New("template does not compile")
		}
		return nil
	}
	data := buildZip(t, map[string]string{
		"good.yaml": "id: rp-good\nname: Good\nversion: 1.0.0\nsql_template: SELECT 1\n",
		"bad.yaml":  "id: rp-bad\nname: Bad\nversion: 1.0.0\nsql_template: SELECT 1\n",
	})
	res, err := m.ImportBytes(data, true)
	if err != nil {
		t.Fatalf("ImportBytes: %v", err)
	}
	if len(res.Added) != 1 || res.Added[0] != "rp-good" {
		t.Errorf("Added = %v, want [rp-good]", res.Added)
	}
	if m.Exists("rp-bad") {
		t.Error("semantically-invalid entry must not enter the catalog")
	}
	foundErr := false
	for _, e := range res.Errors {
		if strings.Contains(e, "bad.yaml") && strings.Contains(e, "does not compile") {
			foundErr = true
		}
	}
	if !foundErr {
		t.Errorf("expected a reported semantic error for bad.yaml, got %v", res.Errors)
	}
}

// Import must never overwrite a builtin definition — not even with force. A
// local shadow replaces builtin detection SQL while keeping the builtin's id
// and name (the exact edit PUT /rules/catalog/:id rejects with 409), and the
// installed builtin then reports "upgradeable" with the imported SQL one
// upgrade click away from deployment.
func TestImportZip_NeverOverwritesBuiltin(t *testing.T) {
	seed := fstest.MapFS{
		"rp-b.yaml": &fstest.MapFile{Data: []byte("id: rp-b\nname: Builtin\nversion: 1.0.0\nsql_template: SELECT 1\n")},
	}
	m, err := New(seed, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	zipData := buildZip(t, map[string]string{
		"rp-b.yaml": "id: rp-b\nname: Shadow\nversion: 99.0.0\nsql_template: SELECT 'evil'\n",
	})
	for _, force := range []bool{false, true} {
		res, err := m.ImportBytes(zipData, force)
		if err != nil {
			t.Fatalf("ImportBytes(force=%v): %v", force, err)
		}
		if len(res.SkippedBuiltin) != 1 || res.SkippedBuiltin[0] != "rp-b" {
			t.Errorf("force=%v: SkippedBuiltin = %v, want [rp-b]", force, res.SkippedBuiltin)
		}
		if len(res.Updated) != 0 || len(res.Added) != 0 {
			t.Errorf("force=%v: builtin reported as added/updated: %+v", force, res)
		}
		got, _ := m.Lookup("rp-b")
		if got.Origin != OriginBuiltin || got.Name != "Builtin" || got.SQLTemplate != "SELECT 1" {
			t.Errorf("force=%v: builtin was shadowed: %+v", force, got)
		}
	}
}

// Import must not silently clobber a user's custom rule on a bare version
// comparison (which also flips its origin to "imported", locking it out of the
// editor); force explicitly authorizes the overwrite.
func TestImportZip_CustomRuleRequiresForce(t *testing.T) {
	m, err := New(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	custom := Definition{ID: "rp-c", Name: "Mine", Version: "1.0.0",
		SQLTemplate: "SELECT 1", Origin: OriginCustom}
	if err := m.Save(custom); err != nil {
		t.Fatalf("Save: %v", err)
	}
	zipData := buildZip(t, map[string]string{
		"rp-c.yaml": "id: rp-c\nname: Theirs\nversion: 2.0.0\nsql_template: SELECT 2\n",
	})

	res, err := m.ImportBytes(zipData, false)
	if err != nil {
		t.Fatalf("ImportBytes: %v", err)
	}
	if len(res.SkippedCustom) != 1 || res.SkippedCustom[0] != "rp-c" {
		t.Errorf("SkippedCustom = %v, want [rp-c]", res.SkippedCustom)
	}
	if got, _ := m.Lookup("rp-c"); got.Name != "Mine" || got.Origin != OriginCustom {
		t.Errorf("custom rule clobbered without force: %+v", got)
	}

	res, err = m.ImportBytes(zipData, true)
	if err != nil {
		t.Fatalf("ImportBytes(force): %v", err)
	}
	if len(res.Updated) != 1 || res.Updated[0] != "rp-c" {
		t.Errorf("force: Updated = %v, want [rp-c]", res.Updated)
	}
	if got, _ := m.Lookup("rp-c"); got.Name != "Theirs" || got.Origin != OriginImported {
		t.Errorf("force overwrite did not apply: %+v", got)
	}
}

// The imported→imported merge policy is unchanged: newer version updates,
// equal-or-older skips, force overrides.
func TestImportZip_ImportedVersionPolicyUnchanged(t *testing.T) {
	m, err := New(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	v1 := buildZip(t, map[string]string{"r.yaml": "id: rp-i\nname: V1\nversion: 1.0.0\nsql_template: SELECT 1\n"})
	v2 := buildZip(t, map[string]string{"r.yaml": "id: rp-i\nname: V2\nversion: 2.0.0\nsql_template: SELECT 2\n"})

	if res, _ := m.ImportBytes(v1, false); len(res.Added) != 1 {
		t.Fatalf("initial import: %+v", res)
	}
	if res, _ := m.ImportBytes(v1, false); len(res.Skipped) != 1 {
		t.Errorf("same-version re-import should skip: %+v", res)
	}
	if res, _ := m.ImportBytes(v2, false); len(res.Updated) != 1 {
		t.Errorf("newer version should update: %+v", res)
	}
	if res, _ := m.ImportBytes(v1, true); len(res.Updated) != 1 {
		t.Errorf("force should overwrite an imported entry with an older version: %+v", res)
	}
	if got, _ := m.Lookup("rp-i"); got.Name != "V1" {
		t.Errorf("force downgrade did not apply: %+v", got)
	}
}
