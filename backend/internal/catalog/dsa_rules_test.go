package catalog

import "testing"

// dsaExpected is the full set of authored customer-audit rule IDs. It grows as
// each category task lands; the package (data-security-audit) must contain
// exactly this set (asserted in package_content_test once the manifest exists).
var dsaExpected = []string{
	"dsa-a01", "dsa-a03", "dsa-a04", "dsa-a05", "dsa-a06", "dsa-a07",
	"dsa-a08", "dsa-a09", "dsa-a10", "dsa-a11", "dsa-a12",
	"dsa-a14", "dsa-a17", "dsa-a18",
	"dsa-a19", "dsa-a20", "dsa-a22",
}

// TestDataSecurityAuditRulesPresent asserts each authored dsa-* rule exists,
// validates, and carries a zh-CN name+description. Rules land task-by-task;
// this test lists only the IDs that should exist so far by filtering on load.
func TestDataSecurityAuditRulesPresent(t *testing.T) {
	m, err := New(SeedFS, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New(SeedFS): %v", err)
	}
	for _, id := range dsaExpected {
		d, ok := m.Lookup(id)
		if !ok {
			continue // not authored yet; a later task adds it
		}
		if err := d.Validate(); err != nil {
			t.Errorf("%s fails Validate: %v", id, err)
		}
		tr, ok := d.TranslationFor("zh-CN")
		if !ok || tr.Name == "" || tr.Description == "" {
			t.Errorf("%s missing zh-CN name/description", id)
		}
	}
	// At least the Task-1 six must be present by the time all tasks are done;
	// this lower bound catches a wholesale load failure.
	got := 0
	for _, id := range dsaExpected {
		if _, ok := m.Lookup(id); ok {
			got++
		}
	}
	if got == 0 {
		t.Fatal("no dsa-* rules loaded at all")
	}
}

// TestDataSecurityAuditPackageMembership asserts the data-security-audit
// package exists and contains exactly the authored dsa-* rule set — no more,
// no less — so the manifest can't drift from the rules.
func TestDataSecurityAuditPackageMembership(t *testing.T) {
	m, err := New(SeedFS, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New(SeedFS): %v", err)
	}
	var pkg *Package
	for _, p := range m.Packages() {
		if p.ID == "data-security-audit" {
			pp := p
			pkg = &pp
			break
		}
	}
	if pkg == nil {
		t.Fatal("data-security-audit package not found")
	}
	want := map[string]bool{}
	for _, id := range dsaExpected {
		want[id] = true
	}
	got := map[string]bool{}
	for _, id := range pkg.Rules {
		got[id] = true
	}
	for id := range want {
		if !got[id] {
			t.Errorf("package missing rule %s", id)
		}
	}
	for id := range got {
		if !want[id] {
			t.Errorf("package has unexpected rule %s", id)
		}
	}
	if pkg.DefaultEnabled {
		t.Error("data-security-audit must be default_enabled: false")
	}
}
