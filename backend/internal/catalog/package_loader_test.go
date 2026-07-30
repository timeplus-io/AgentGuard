package catalog

import (
	"testing"
	"testing/fstest"
)

func TestLoadPackagesFS(t *testing.T) {
	fsys := fstest.MapFS{
		"core.yaml": {Data: []byte("id: core\nname: Core Protection\ndefault_enabled: true\nrules: [rp-001, rp-002]\n")},
		"extra.yaml": {Data: []byte("id: extra\nname: Extra Pack\nrules: [rp-003]\n")},
		"notes.txt":  {Data: []byte("ignore me")},
	}
	pkgs, err := loadPackagesFS(fsys)
	if err != nil {
		t.Fatalf("loadPackagesFS: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("loaded %d packages, want 2", len(pkgs))
	}
	core, ok := pkgs["core"]
	if !ok {
		t.Fatal("core package not loaded")
	}
	if !core.DefaultEnabled {
		t.Error("core.DefaultEnabled = false, want true")
	}
	if len(core.Rules) != 2 || core.Rules[0] != "rp-001" {
		t.Errorf("core.Rules = %v, want [rp-001 rp-002]", core.Rules)
	}
	if extra := pkgs["extra"]; extra.DefaultEnabled {
		t.Error("extra.DefaultEnabled = true, want false (default)")
	}
}

func TestLoadPackagesFS_InvalidFails(t *testing.T) {
	fsys := fstest.MapFS{
		"bad.yaml": {Data: []byte("name: No ID\nrules: [rp-001]\n")},
	}
	if _, err := loadPackagesFS(fsys); err == nil {
		t.Fatal("expected error for manifest missing id, got nil")
	}
}

func TestManagerPackagesSortedAndLookup(t *testing.T) {
	m := &Manager{packages: map[string]Package{
		"beta":  {ID: "beta", Name: "Beta", Rules: []string{"rp-002"}},
		"alpha": {ID: "alpha", Name: "Alpha", Rules: []string{"rp-001"}},
	}}
	got := m.Packages()
	if len(got) != 2 || got[0].ID != "alpha" || got[1].ID != "beta" {
		t.Fatalf("Packages() not sorted by id: %+v", got)
	}
	if _, ok := m.PackageByID("alpha"); !ok {
		t.Error("PackageByID(alpha) not found")
	}
	if _, ok := m.PackageByID("nope"); ok {
		t.Error("PackageByID(nope) unexpectedly found")
	}
}

// The embedded manifests load cleanly and include the default-on core package.
func TestEmbeddedPackagesLoad(t *testing.T) {
	pkgs, err := loadPackagesFS(PackagesFS)
	if err != nil {
		t.Fatalf("embedded packages failed to load: %v", err)
	}
	core, ok := pkgs["core"]
	if !ok {
		t.Fatal("embedded catalog missing 'core' package")
	}
	if !core.DefaultEnabled {
		t.Error("core package must be default_enabled")
	}
}
