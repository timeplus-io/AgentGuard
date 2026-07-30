package catalog

import "testing"

// TestEmbeddedPackagesHaveChineseTranslations asserts every embedded package
// carries a non-empty zh-CN name and description — the localization definition
// of done for packages, mirroring TestSeedRulesHaveChineseTranslations for
// rules. Guards against a new package manifest landing without a translation.
func TestEmbeddedPackagesHaveChineseTranslations(t *testing.T) {
	m, err := New(SeedFS, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New(SeedFS): %v", err)
	}
	var missing []string
	for _, p := range m.Packages() {
		tr, ok := p.TranslationFor("zh-CN")
		switch {
		case !ok:
			missing = append(missing, p.ID+" (no zh-CN block)")
		case tr.Name == "":
			missing = append(missing, p.ID+" (empty zh-CN name)")
		case tr.Description == "":
			missing = append(missing, p.ID+" (empty zh-CN description)")
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d packages missing zh-CN translation:\n%v", len(missing), missing)
	}
}

// TestEmbeddedPackageRulesExist asserts every rule ID referenced by an embedded
// package resolves to a real catalog definition. This is the cross-reference
// check the loader deliberately skips (it can't see definitions), so drift —
// e.g. a rule renamed or removed without updating its package — fails CI here
// rather than silently degrading a package at enable time.
func TestEmbeddedPackageRulesExist(t *testing.T) {
	m, err := New(SeedFS, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New(SeedFS): %v", err)
	}
	var dangling []string
	for _, p := range m.Packages() {
		for _, id := range p.Rules {
			if !m.Exists(id) {
				dangling = append(dangling, p.ID+" -> "+id)
			}
		}
	}
	if len(dangling) > 0 {
		t.Fatalf("%d package rule references do not exist in catalog:\n%v", len(dangling), dangling)
	}
}

// TestCorePackageIsDefaultEnabled locks in the product invariant that exactly
// the core package ships recommended-on; all functional packs are opt-in.
func TestCorePackageIsDefaultEnabled(t *testing.T) {
	m, err := New(SeedFS, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New(SeedFS): %v", err)
	}
	var defaultOn []string
	for _, p := range m.Packages() {
		if p.DefaultEnabled {
			defaultOn = append(defaultOn, p.ID)
		}
	}
	if len(defaultOn) != 1 || defaultOn[0] != "core" {
		t.Fatalf("default-enabled packages = %v, want [core]", defaultOn)
	}
}
