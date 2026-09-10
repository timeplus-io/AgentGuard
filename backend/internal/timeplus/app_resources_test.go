package timeplus

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"testing"
)

var createHeadRe = regexp.MustCompile(`(?is)^\s*CREATE\s+(MUTABLE\s+STREAM|STREAM|MATERIALIZED\s+VIEW|INPUT)\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)

func TestAppResources_NamesUniqueAndMatchDDL(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range AppResources() {
		if seen[r.Name] {
			t.Errorf("duplicate resource name %q", r.Name)
		}
		seen[r.Name] = true
		m := createHeadRe.FindStringSubmatch(r.DDL)
		if m == nil {
			t.Errorf("%s: DDL head not recognised: %.60q", r.Name, r.DDL)
			continue
		}
		if m[2] != r.Name {
			t.Errorf("%s: DDL creates %q", r.Name, m[2])
		}
		want := map[string]AppResourceKind{
			"STREAM": AppKindStream, "MUTABLE STREAM": AppKindMutableStream,
			"MATERIALIZED VIEW": AppKindMV, "INPUT": AppKindInput,
		}[strings.ToUpper(strings.Join(strings.Fields(m[1]), " "))]
		if want != r.Kind {
			t.Errorf("%s: kind %q but DDL is %q", r.Name, r.Kind, m[1])
		}
	}
}

func TestAppResources_Order(t *testing.T) {
	// Sinks must be declared before the MVs that write INTO them, and sources
	// before MVs that read FROM them — the installer executes in list order.
	declared := map[string]bool{}
	intoRe := regexp.MustCompile(`(?i)\bINTO\s+([a-z_][a-z0-9_]*)`)
	fromRe := regexp.MustCompile(`(?i)\bFROM\s+([a-z_][a-z0-9_]*)`)
	for _, r := range AppResources() {
		if r.Kind == AppKindMV {
			for _, m := range intoRe.FindAllStringSubmatch(r.DDL, -1) {
				if !declared[m[1]] {
					t.Errorf("%s writes INTO %s before it is declared", r.Name, m[1])
				}
			}
			for _, m := range fromRe.FindAllStringSubmatch(r.DDL, -1) {
				if strings.HasPrefix(m[1], "agentguard_") || strings.HasPrefix(m[1], "otel_") {
					if !declared[m[1]] {
						t.Errorf("%s reads FROM %s before it is declared", r.Name, m[1])
					}
				}
			}
		}
		declared[r.Name] = true
	}
}

// Every top-level ddl* const/var in init.go must be classified: either it is
// part of the app (AppResources) or it is deliberately excluded
// (AppExcludedDDL). Adding a stream to Init() without deciding fails here.
func TestAppResources_CoverEveryDDLConstant(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "init.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, s := range gd.Specs {
			vs, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range vs.Names {
				if strings.HasPrefix(n.Name, "ddl") {
					names = append(names, n.Name)
				}
			}
		}
	}
	if len(names) < 40 {
		t.Fatalf("expected to find the ddl* constants in init.go, got %d", len(names))
	}
	included := map[string]bool{}
	for _, r := range AppResources() {
		included[r.ddlConst] = true
	}
	excluded := map[string]bool{}
	for _, n := range AppExcludedDDL() {
		excluded[n] = true
	}
	for _, n := range names {
		switch {
		case included[n] && excluded[n]:
			t.Errorf("%s is both included and excluded", n)
		case !included[n] && !excluded[n]:
			t.Errorf("%s is neither in AppResources() nor AppExcludedDDL() — decide whether the app ships it", n)
		}
	}
	for n := range excluded {
		found := false
		for _, have := range names {
			if have == n {
				found = true
			}
		}
		if !found {
			t.Errorf("AppExcludedDDL lists %s but init.go has no such constant", n)
		}
	}
}
