package catalog

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// loadPackagesFS parses every *.yaml manifest in fsys into a Package, keyed by
// ID. Non-YAML files are ignored. A parse error or a structurally invalid
// manifest (Validate) fails the load — package manifests are few and embedded
// (golden), so drift is a build/CI problem to surface loudly, not something to
// silently skip. Cross-referencing rule IDs against the definition catalog is
// intentionally NOT done here (a Manager-level concern, guarded by a catalog
// completeness test); a package may reference a rule that was later removed
// without breaking startup — the enable handler skips/reports the missing one.
func loadPackagesFS(fsys fs.FS) (map[string]Package, error) {
	out := make(map[string]Package)
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		var p Package
		if err := yaml.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if err := p.Validate(); err != nil {
			return fmt.Errorf("invalid package manifest %s: %w", path, err)
		}
		if _, dup := out[p.ID]; dup {
			return fmt.Errorf("duplicate package id %q (%s)", p.ID, path)
		}
		out[p.ID] = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Packages returns all loaded packages sorted by ID. Returned copies keep their
// Translations map intact; callers localize per request via Package.Localized.
func (m *Manager) Packages() []Package {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Package, 0, len(m.packages))
	for _, p := range m.packages {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// PackageByID returns the package with the given ID and whether it exists.
func (m *Manager) PackageByID(id string) (Package, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.packages[id]
	return p, ok
}

// Package is a named bundle of rule definitions. Packages let operators enable
// coverage in meaningful groups (a curated "core" set, or per-threat-class
// functional packs) instead of installing all 100+ rules one at a time.
//
// A rule may belong to multiple packages; membership lives here (the manifest),
// not on the rule Definition. Rules in no package remain individually
// installable. Packages are presentation/curation metadata only — they never
// change how a rule is compiled or deployed; enabling a package just installs
// its member rules through the normal lifecycle service.
//
// DefaultEnabled marks a package as "Recommended" in the UI. It does NOT
// auto-install: provisioning still creates zero materialized views. The flag
// only pre-selects the package and drives the one-click "Enable package"
// affordance.
type Package struct {
	ID             string   `yaml:"id"                     json:"id"`
	Name           string   `yaml:"name"                   json:"name"`
	Description    string   `yaml:"description"            json:"description"`
	DefaultEnabled bool     `yaml:"default_enabled"        json:"default_enabled"`
	Rules          []string `yaml:"rules"                  json:"rules"`

	// Translations holds locale-specific display text keyed by locale
	// (e.g. "zh-CN"), mirroring Definition.Translations: excluded from any
	// hash, resolved server-side at read time, and cleared before a response.
	Translations map[string]Translation `yaml:"translations,omitempty" json:"translations,omitempty"`
}

// TranslationFor returns the translation entry for locale and whether one
// exists. Indexing a nil map is safe (zero value, ok=false).
func (p Package) TranslationFor(locale string) (Translation, bool) {
	tr, ok := p.Translations[locale]
	return tr, ok
}

// Localized returns a copy of p with Name/Description replaced by the locale's
// translated values (per-field fallback to the base when a translated field is
// empty), and with Translations cleared so the map never leaks onto a read
// response. An unknown/empty locale or a missing entry returns the base copy.
func (p Package) Localized(locale string) Package {
	out := p
	out.Translations = nil
	if tr, ok := p.TranslationFor(locale); ok {
		if tr.Name != "" {
			out.Name = tr.Name
		}
		if tr.Description != "" {
			out.Description = tr.Description
		}
	}
	return out
}

// Validate checks the package is structurally sound. It does NOT verify that
// referenced rule IDs exist in the catalog — that cross-reference check is the
// Manager's job (it needs the loaded definitions), so a bare Package can be
// validated in isolation.
func (p Package) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("%w: package id is required", ErrInvalid)
	}
	if len(p.ID) > 64 {
		return fmt.Errorf("%w: package id must be 64 characters or fewer", ErrInvalid)
	}
	for _, c := range p.ID {
		if !isIDChar(c) {
			return fmt.Errorf("%w: package id %q may only contain letters, digits, hyphens, and underscores", ErrInvalid, p.ID)
		}
	}
	if p.Name == "" {
		return fmt.Errorf("%w: package %q name is required", ErrInvalid, p.ID)
	}
	if len(p.Rules) == 0 {
		return fmt.Errorf("%w: package %q must list at least one rule", ErrInvalid, p.ID)
	}
	seen := make(map[string]struct{}, len(p.Rules))
	for _, id := range p.Rules {
		if id == "" {
			return fmt.Errorf("%w: package %q contains an empty rule id", ErrInvalid, p.ID)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("%w: package %q lists rule %q more than once", ErrInvalid, p.ID, id)
		}
		seen[id] = struct{}{}
	}
	return nil
}
