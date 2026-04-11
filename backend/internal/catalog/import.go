package catalog

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ImportResult summarizes the outcome of a catalog import operation.
type ImportResult struct {
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
	Skipped []string `json:"skipped"`
	Errors  []string `json:"errors"`
}

// ImportZip reads a zip archive and merges YAML rule files into the local catalog.
// If force is true, existing entries are overwritten even when the version is not newer.
func (m *Manager) ImportZip(r io.ReaderAt, size int64, force bool) (ImportResult, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return ImportResult{}, fmt.Errorf("open zip: %w", err)
	}

	var result ImportResult
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, ".yaml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: open: %v", f.Name, err))
			continue
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: read: %v", f.Name, err))
			continue
		}

		var entry Entry
		if err := yaml.Unmarshal(data, &entry); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: parse yaml: %v", f.Name, err))
			continue
		}
		if entry.ID == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: missing 'id' field", f.Name))
			continue
		}
		if entry.QuerySQL == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: missing 'query_sql' field", f.Name))
			continue
		}

		action := m.mergeEntry(entry, force)
		switch action {
		case "added":
			result.Added = append(result.Added, entry.ID)
		case "updated":
			result.Updated = append(result.Updated, entry.ID)
		case "skipped":
			result.Skipped = append(result.Skipped, entry.ID)
		}
	}
	return result, nil
}

// ImportBytes is a convenience wrapper for in-memory zip data.
func (m *Manager) ImportBytes(data []byte, force bool) (ImportResult, error) {
	return m.ImportZip(bytes.NewReader(data), int64(len(data)), force)
}

// mergeEntry applies one entry to the local catalog with the configured strategy.
// Returns "added", "updated", or "skipped".
func (m *Manager) mergeEntry(e Entry, force bool) string {
	existing, exists := m.Lookup(e.ID)
	if !exists {
		if err := m.Save(e); err == nil {
			return "added"
		}
		return "skipped"
	}
	if force || versionGreater(e.Version, existing.Version) {
		if err := m.Save(e); err == nil {
			return "updated"
		}
	}
	return "skipped"
}

// versionGreater returns true if version string a is strictly greater than b.
// Versions are expected to follow X.Y.Z semver convention.
func versionGreater(a, b string) bool {
	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		var av, bv int
		if i < len(aParts) {
			av, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bv, _ = strconv.Atoi(bParts[i])
		}
		if av > bv {
			return true
		}
		if av < bv {
			return false
		}
	}
	return false
}
