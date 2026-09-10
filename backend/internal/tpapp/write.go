package tpapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Write renders every file under dir and deletes ddl/*.sql files the app no
// longer produces. Everything is rendered in memory first (Build), so a
// failure never leaves a half-written tree except for I/O errors.
func Write(app *App, dir string) error {
	want := map[string]bool{}
	for _, f := range app.Files {
		want[f.Path] = true
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, f.Content, 0o644); err != nil {
			return err
		}
	}
	stale, err := filepath.Glob(filepath.Join(dir, "ddl", "*.sql"))
	if err != nil {
		return err
	}
	for _, p := range stale {
		rel := "ddl/" + filepath.Base(p)
		if !want[rel] {
			if err := os.Remove(p); err != nil {
				return err
			}
		}
	}
	return nil
}

// Diff returns the relative paths whose on-disk content differs from app
// (missing files included) plus stale ddl/*.sql files. nil means up to date.
func Diff(app *App, dir string) ([]string, error) {
	var out []string
	want := map[string]bool{}
	for _, f := range app.Files {
		want[f.Path] = true
		cur, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil || !bytes.Equal(cur, f.Content) {
			out = append(out, f.Path)
		}
	}
	stale, err := filepath.Glob(filepath.Join(dir, "ddl", "*.sql"))
	if err != nil {
		return nil, err
	}
	for _, p := range stale {
		rel := "ddl/" + filepath.Base(p)
		if !want[rel] {
			out = append(out, rel+" (stale)")
		}
	}
	sort.Strings(out)
	return out, nil
}

// FormatDiff renders Diff output for CLI messages.
func FormatDiff(paths []string) string {
	if len(paths) == 0 {
		return "up to date"
	}
	return fmt.Sprintf("%d file(s) differ:\n  %s", len(paths), strings.Join(paths, "\n  "))
}
