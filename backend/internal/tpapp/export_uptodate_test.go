package tpapp

import (
	"path/filepath"
	"runtime"
	"testing"
)

// The committed apps/agentguard/ tree must equal the generator's output.
// Regenerate with `make tpapp` from the repo root.
func TestAppExportUpToDate(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "apps", "agentguard")
	app, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	diff, err := Diff(app, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) != 0 {
		t.Fatalf("apps/agentguard is out of date (run `make tpapp`): %v", diff)
	}
}
