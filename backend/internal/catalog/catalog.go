package catalog

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// The Definition model lives in definition.go; the catalog is the
// authoritative store for rule definitions.

// Manager holds the merged view of the embedded seed catalog and the
// local catalog directory. Local definitions override seed ones by ID.
type Manager struct {
	mu    sync.RWMutex
	seed  map[string]Definition // from go:embed, read-only
	local map[string]Definition // from catalogDir, read-write
	dir   string

	// SemanticValidator, if set, runs parameter-compiler validation on a
	// definition during Save/ImportZip (in addition to structural Validate()),
	// so authoring defects in user-supplied definitions are rejected at write
	// time rather than at first install. It is injected by the server wiring
	// (rules.ValidateDefinition) to avoid a catalog→rules import cycle; nil
	// (e.g. in tests and seed-only managers) means the check is skipped.
	SemanticValidator func(Definition) error
}

// New creates a Manager. seedFS is the embedded FS rooted at the YAML files.
// catalogDir is the writable directory for user overrides and additions.
// Option configures a Manager at construction, before any catalog files load.
type Option func(*Manager)

// WithSemanticValidator sets the compiler-semantic validator (typically
// rules.ValidateDefinition) so it applies to local-directory definitions as they
// load — not only to later Save/Import — closing the gap where a hand-placed or
// previously-persisted local YAML with a bad template/default would enter the
// catalog and fail only at install. Seed (builtin) definitions are not subject to
// it: they are validated at build time by the seed test suite.
func WithSemanticValidator(fn func(Definition) error) Option {
	return func(m *Manager) { m.SemanticValidator = fn }
}

func New(seedFS fs.FS, catalogDir string, opts ...Option) (*Manager, error) {
	m := &Manager{
		seed:  make(map[string]Definition),
		local: make(map[string]Definition),
		dir:   catalogDir,
	}
	// Apply options before loading so loadDir sees the semantic validator.
	for _, opt := range opts {
		opt(m)
	}
	if err := m.loadFS(seedFS, m.seed); err != nil {
		return nil, fmt.Errorf("load seed catalog: %w", err)
	}
	if catalogDir != "" {
		if err := os.MkdirAll(catalogDir, 0o755); err != nil {
			return nil, fmt.Errorf("create catalog dir %s: %w", catalogDir, err)
		}
		if err := m.loadDir(catalogDir, m.local); err != nil {
			return nil, fmt.Errorf("load local catalog: %w", err)
		}
	}
	return m, nil
}

func (m *Manager) loadFS(fsys fs.FS, target map[string]Definition) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		var def Definition
		if err := yaml.Unmarshal(data, &def); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if def.ID != "" {
			// Embedded seed definitions are always builtin, regardless of
			// any origin stored in the YAML.
			def.Origin = OriginBuiltin
			target[def.ID] = def
		}
		return nil
	})
}

// loadDir loads local-dir definitions. Invalid, unreadable, or unparseable
// files are skipped with a log line rather than failing startup — one bad
// file in the user's catalog directory must not take the server down. This
// establishes the invariant that anything returned by Lookup/All has passed
// Validate (loadFS enforces the same for seed definitions via the seed test).
func (m *Manager) loadDir(dir string, target map[string]Definition) error {
	des, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, de := range des {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, de.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("catalog: skipping unreadable local definition %s: %v", path, err)
			continue
		}
		var def Definition
		if err := yaml.Unmarshal(data, &def); err != nil {
			log.Printf("catalog: skipping unparseable local definition %s: %v", path, err)
			continue
		}
		if def.ID == "" {
			log.Printf("catalog: skipping local definition %s: missing id", path)
			continue
		}
		if err := def.Validate(); err != nil {
			log.Printf("catalog: skipping invalid local definition %s: %v", path, err)
			continue
		}
		// Compiler-semantic validation (template/defaults compile): a local file
		// can be structurally valid yet not compile, which would otherwise only
		// surface at install. Skip it the same way as a structurally-invalid one.
		if m.SemanticValidator != nil {
			if err := m.SemanticValidator(def); err != nil {
				log.Printf("catalog: skipping non-compiling local definition %s: %v", path, err)
				continue
			}
		}
		// Local definitions keep their stored origin; absent means custom.
		if def.Origin == "" {
			def.Origin = OriginCustom
		}
		target[def.ID] = def
	}
	return nil
}

// All returns all merged definitions sorted by ID (local overrides seed).
func (m *Manager) All() []Definition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	merged := m.mergedLocked()
	result := make([]Definition, 0, len(merged))
	for _, d := range merged {
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// mergedLocked returns the combined map (local wins over seed). Caller must hold mu.
func (m *Manager) mergedLocked() map[string]Definition {
	out := make(map[string]Definition, len(m.seed)+len(m.local))
	for id, d := range m.seed {
		out[id] = d
	}
	for id, d := range m.local {
		out[id] = d
	}
	return out
}

// Lookup finds a definition by ID (local overrides seed).
func (m *Manager) Lookup(id string) (Definition, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if d, ok := m.local[id]; ok {
		return d, true
	}
	if d, ok := m.seed[id]; ok {
		return d, true
	}
	return Definition{}, false
}

// Exists reports whether id is present in the catalog.
func (m *Manager) Exists(id string) bool {
	_, ok := m.Lookup(id)
	return ok
}

// mvResourceKey returns the key an ID collapses to in the Timeplus
// materialized-view namespace. It MUST mirror internal/timeplus.MVName, which
// names each rule's view mv_rule_<key> after stripping hyphens. Two distinct
// catalog IDs that share a key would deploy to the same view and silently
// clobber each other; Save rejects that. (Kept here rather than importing
// internal/timeplus to avoid pulling the Proton client into the catalog layer;
// TestMVResourceKey_StripsHyphens guards the mirror — see testdata/README.md.)
func mvResourceKey(id string) string {
	return strings.ReplaceAll(id, "-", "")
}

// mvKeyConflict reports an existing catalog ID, different from id, that maps to
// the same MV resource key (e.g. "rp-042" vs "rp042"). The same id is never a
// conflict with itself, so override/edit/rollback re-saves are unaffected.
func (m *Manager) mvKeyConflict(id string) (string, bool) {
	key := mvResourceKey(id)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for other := range m.mergedLocked() {
		if other != id && mvResourceKey(other) == key {
			return other, true
		}
	}
	return "", false
}

// Save validates a definition, writes it to the local catalog directory, and
// updates the in-memory map. This is the path for user-created rules and
// overrides of seed definitions. Definitions saved without an origin default
// to custom.
func (m *Manager) Save(d Definition) error {
	if m.dir == "" {
		return fmt.Errorf("no catalog directory configured")
	}
	if d.Origin == "" {
		d.Origin = OriginCustom
	}
	if err := d.Validate(); err != nil {
		return fmt.Errorf("invalid definition %s: %w", d.ID, err)
	}
	if m.SemanticValidator != nil {
		if err := m.SemanticValidator(d); err != nil {
			return fmt.Errorf("invalid definition %s: %w", d.ID, err)
		}
	}
	// NOTE: this ownership check followed by the insert below is a
	// check-then-act race across different rule IDs — two concurrent Saves
	// claiming the same UDF name can both pass. Save is NOT serialized by the
	// rules lifecycle service (CreateRule calls Save outside the per-rule lock,
	// and the lock is keyed by rule_id while this race is cross-rule); a
	// catalog-wide mutation lock is the real fix. Tolerated for now: custom-rule
	// creation is a low-frequency operator action.
	if err := m.ValidateUDFOwnership(d); err != nil {
		return fmt.Errorf("definition %s: %w", d.ID, err)
	}
	// Reject an ID that collapses onto a DIFFERENT existing rule's MV name. The
	// Timeplus MV namespace strips hyphens (internal/timeplus.MVName), so "rp-042"
	// and "rp042" would both deploy to mv_rule_rp042 and silently clobber each
	// other on install/toggle/sync. Exact same-ID re-saves (override, edit
	// republish, rollback) are unaffected — only a distinct colliding ID is blocked.
	if other, ok := m.mvKeyConflict(d.ID); ok {
		return fmt.Errorf("%w: id %q collides with rule %q in the materialized-view namespace (hyphens are not significant there); choose a distinct id", ErrExists, d.ID, other)
	}
	data, err := yaml.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshal definition %s: %w", d.ID, err)
	}
	path := filepath.Join(m.dir, d.ID+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	m.mu.Lock()
	m.local[d.ID] = d
	m.mu.Unlock()
	return nil
}

// Delete removes a local (custom/imported) definition: its catalog-directory
// file and its in-memory entry. Seed (builtin) definitions are not file-backed
// and are unaffected; deleting an unknown local ID is a no-op. The create-custom
// flow uses this to roll back a Save when the subsequent install fails, so an
// aborted creation does not leave an orphaned definition behind.
func (m *Manager) Delete(id string) error {
	if m.dir == "" {
		return fmt.Errorf("no catalog directory configured")
	}
	path := filepath.Join(m.dir, id+".yaml")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete %s: %w", path, err)
	}
	m.mu.Lock()
	delete(m.local, id)
	m.mu.Unlock()
	return nil
}

// ValidateID checks that an ID is syntactically valid and not already in use.
func (m *Manager) ValidateID(id string) error {
	if id == "" {
		return fmt.Errorf("id is required")
	}
	if len(id) > 64 {
		return fmt.Errorf("id must be 64 characters or fewer")
	}
	for _, c := range id {
		if !isIDChar(c) {
			return fmt.Errorf("id may only contain letters, digits, hyphens, and underscores")
		}
	}
	if m.Exists(id) {
		return fmt.Errorf("id %q already exists in catalog", id)
	}
	return nil
}

func isIDChar(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '-' || c == '_'
}

// IsCustom reports whether id is in the local (writable) catalog and has
// OriginCustom. Builtin (seed) definitions and unknown IDs both return false.
func (m *Manager) IsCustom(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.local[id]
	return ok && d.Origin == OriginCustom
}

// Clone creates a copy of the definition with srcID, assigns it the new ID and
// name, rescopes its UDF function names so they do not collide with the source
// rule in the Timeplus function registry, and saves it as a custom definition.
//
// Returns:
//   - ErrNotFound if srcID is not in the catalog.
//   - ErrExists  if newID is already in the catalog.
//   - ErrInvalid if newID is syntactically invalid.
func (m *Manager) Clone(srcID, newID, newName string) (Definition, error) {
	src, ok := m.Lookup(srcID)
	if !ok {
		return Definition{}, ErrNotFound
	}
	if m.Exists(newID) {
		return Definition{}, ErrExists
	}
	// ValidateID checks both syntax and existence; since we've just confirmed the
	// ID does not exist, only the syntax branch can fail here.
	if err := m.ValidateID(newID); err != nil {
		return Definition{}, fmt.Errorf("%w: %s", ErrInvalid, err)
	}

	// Rescope UDF function names to avoid collision with the source rule.
	cloned, err := RescopeUDFs(src, newID)
	if err != nil {
		return Definition{}, fmt.Errorf("%w: rescope UDFs: %s", ErrInvalid, err)
	}
	cloned.ID = newID
	cloned.Name = newName
	cloned.Origin = OriginCustom

	if err := m.Save(cloned); err != nil {
		return Definition{}, err
	}
	return cloned, nil
}
