package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestSeedRenderedSQLGolden is the behavior-preservation guardrail for the
// identity-sentinel refactor: every embedded seed compiled with its DEFAULTS
// must render byte-for-byte to the captured golden. The golden was captured
// from the pre-refactor seeds (literal `'rp-xxx' AS rule_id` / `'<Name>' AS
// rule_name` columns); after the columns switch to the '__rule_id__' /
// '__rule_name__' sentinels, Compile's identity bake must restore exactly the
// same deployed SQL — so a passing test proves no builtin's MV SQL changed.
//
// Regenerate INTENTIONALLY (only when a seed's rendered SQL is meant to change)
// with:  UPDATE_SEED_GOLDEN=1 go test ./internal/rules/ -run TestSeedRenderedSQLGolden
func TestSeedRenderedSQLGolden(t *testing.T) {
	defs := loadSeed(t).All()
	if len(defs) == 0 {
		t.Fatal("seed catalog loaded zero definitions")
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })

	var b strings.Builder
	for _, d := range defs {
		c, err := Compile(d, nil)
		if err != nil {
			t.Fatalf("seed %s: compile with defaults: %v", d.ID, err)
		}
		b.WriteString("== " + d.ID + " ==\n")
		b.WriteString(c.SQL)
		b.WriteString("\n")
	}
	got := b.String()

	path := filepath.Join("testdata", "seed_rendered.golden")
	if os.Getenv("UPDATE_SEED_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s (%d seeds)", path, len(defs))
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (create with UPDATE_SEED_GOLDEN=1): %v", err)
	}
	if got != string(want) {
		t.Errorf("seed rendered SQL differs from golden — a builtin's deployed MV SQL changed.\n" +
			"If this change is intentional, regenerate with UPDATE_SEED_GOLDEN=1 and review the diff.")
	}
}
