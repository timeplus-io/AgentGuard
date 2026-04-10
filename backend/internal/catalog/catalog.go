package catalog

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Entry is the canonical representation of a catalog rule.
type Entry struct {
	ID                string   `yaml:"id"                 json:"id"`
	Name              string   `yaml:"name"               json:"name"`
	Version           string   `yaml:"version"            json:"version"`
	Description       string   `yaml:"description"        json:"description"`
	Severity          string   `yaml:"severity"           json:"severity"`
	Category          string   `yaml:"category"           json:"category"`
	FPRate            string   `yaml:"fp_rate"            json:"fp_rate"`
	HookSources       string   `yaml:"hook_sources"       json:"hook_sources"`
	RequiredTelemetry string   `yaml:"required_telemetry" json:"required_telemetry"`
	Author            string   `yaml:"author"             json:"author"`
	Tags              []string `yaml:"tags"               json:"tags"`
	QuerySQL          string   `yaml:"query_sql"          json:"query_sql"`
}

// Manager holds the merged view of the embedded initial catalog and the
// local catalog directory. Local entries override initial ones by ID.
type Manager struct {
	mu       sync.RWMutex
	initial  map[string]Entry // from go:embed, read-only
	local    map[string]Entry // from catalogDir, read-write
	dir      string
}

// New creates a Manager. initialFS is the embedded FS rooted at the YAML files.
// catalogDir is the writable directory for user overrides and additions.
func New(initialFS fs.FS, catalogDir string) (*Manager, error) {
	m := &Manager{
		initial: make(map[string]Entry),
		local:   make(map[string]Entry),
		dir:     catalogDir,
	}
	if err := m.loadFS(initialFS, m.initial); err != nil {
		return nil, fmt.Errorf("load initial catalog: %w", err)
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

func (m *Manager) loadFS(fsys fs.FS, target map[string]Entry) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		var e Entry
		if err := yaml.Unmarshal(data, &e); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if e.ID != "" {
			target[e.ID] = e
		}
		return nil
	})
}

func (m *Manager) loadDir(dir string, target map[string]Entry) error {
	des, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, de := range des {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			continue
		}
		var e Entry
		if err := yaml.Unmarshal(data, &e); err != nil {
			continue
		}
		if e.ID != "" {
			target[e.ID] = e
		}
	}
	return nil
}

// All returns all merged entries sorted by ID (local overrides initial).
func (m *Manager) All() []Entry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	merged := m.mergedLocked()
	result := make([]Entry, 0, len(merged))
	for _, e := range merged {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// mergedLocked returns the combined map (local wins over initial). Caller must hold mu.
func (m *Manager) mergedLocked() map[string]Entry {
	out := make(map[string]Entry, len(m.initial)+len(m.local))
	for id, e := range m.initial {
		out[id] = e
	}
	for id, e := range m.local {
		out[id] = e
	}
	return out
}

// Lookup finds an entry by ID (local overrides initial).
func (m *Manager) Lookup(id string) (Entry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e, ok := m.local[id]; ok {
		return e, true
	}
	if e, ok := m.initial[id]; ok {
		return e, true
	}
	return Entry{}, false
}

// Exists reports whether id is present in the catalog.
func (m *Manager) Exists(id string) bool {
	_, ok := m.Lookup(id)
	return ok
}

// Save writes an entry to the local catalog directory and updates the in-memory map.
// This is the path for user-created rules and overrides of initial entries.
func (m *Manager) Save(e Entry) error {
	if m.dir == "" {
		return fmt.Errorf("no catalog directory configured")
	}
	data, err := yaml.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal entry %s: %w", e.ID, err)
	}
	path := filepath.Join(m.dir, e.ID+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	m.mu.Lock()
	m.local[e.ID] = e
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
