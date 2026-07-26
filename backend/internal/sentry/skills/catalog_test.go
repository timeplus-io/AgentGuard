package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/rules"
)

// seedDef is a minimal valid catalog definition YAML for tests.
const seedDef = `id: rp900
name: Test rule
version: "1.0.0"
description: test detection
severity: high
category: test
sql_template: SELECT 1
`

func catalogRunContext() *RunContext {
	// Unreachable Timeplus: installed-status lookup fails, and the tool must
	// degrade to catalog-only output rather than erroring.
	return &RunContext{Cfg: &config.Config{Timeplus: config.TimeplusConfig{Host: "127.0.0.1", Port: 1}}, Loaded: map[string]bool{}}
}

func newTestCatalogSkill(t *testing.T) Skill {
	t.Helper()
	fsys := fstest.MapFS{"rp900.yaml": &fstest.MapFile{Data: []byte(seedDef)}}
	mgr, err := catalog.New(fsys, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewRuleCatalog(mgr, rules.NewLifecycle(mgr))
}

func TestRuleCatalogListDegradesWithoutTimeplus(t *testing.T) {
	sk := newTestCatalogSkill(t)
	out, err := sk.Execute(context.Background(), catalogRunContext(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Catalog []map[string]any `json:"catalog"`
		Count   int              `json:"count"`
		Note    string           `json:"note"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("non-JSON output: %v\n%s", err, out)
	}
	if resp.Count != 1 || len(resp.Catalog) != 1 || resp.Catalog[0]["id"] != "rp900" {
		t.Fatalf("expected rp900 in catalog, got %s", out)
	}
	if resp.Note == "" {
		t.Fatal("expected installed-status-unavailable note when Timeplus is unreachable")
	}
}

func TestRuleCatalogDetailAndUnknown(t *testing.T) {
	sk := newTestCatalogSkill(t)
	args, _ := json.Marshal(map[string]string{"id": "rp900"})
	out, err := sk.Execute(context.Background(), catalogRunContext(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "sql_template") || !strings.Contains(out, "Test rule") {
		t.Fatalf("detail must include the full definition, got %.200q", out)
	}

	args, _ = json.Marshal(map[string]string{"id": "nope"})
	out, _ = sk.Execute(context.Background(), catalogRunContext(), args)
	if !strings.Contains(out, "unknown catalog id") {
		t.Fatalf("unknown id must say so, got %.200q", out)
	}
}
