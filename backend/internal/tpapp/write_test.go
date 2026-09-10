package tpapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteThenDiffIsClean(t *testing.T) {
	app, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// a stale file that Write must remove
	if err := os.MkdirAll(filepath.Join(dir, "ddl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ddl", "999_stale.sql"), []byte("SELECT 1"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(app, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ddl", "999_stale.sql")); !os.IsNotExist(err) {
		t.Error("stale ddl file was not removed")
	}
	diff, err := Diff(app, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) != 0 {
		t.Errorf("expected clean diff, got %v", diff)
	}
	// mutate one file → Diff reports it
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, _ = Diff(app, dir)
	if len(diff) != 1 || diff[0] != "README.md" {
		t.Errorf("expected [README.md], got %v", diff)
	}
}
