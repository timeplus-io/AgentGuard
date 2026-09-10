package tpapp

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func fileByPath(t *testing.T, app *App, p string) []byte {
	t.Helper()
	for _, f := range app.Files {
		if f.Path == p {
			return f.Content
		}
	}
	t.Fatalf("file %s not produced", p)
	return nil
}

type manifest struct {
	PackageFormatVersion int    `yaml:"package_format_version"`
	ID                   string `yaml:"id"`
	DBName               string `yaml:"db_name"`
	Icon                 string `yaml:"icon"`
	Config               []struct {
		Key     string `yaml:"key"`
		Default string `yaml:"default"`
	} `yaml:"config"`
	Resources []struct {
		File string `yaml:"file"`
		Type string `yaml:"type"`
		Name string `yaml:"name"`
	} `yaml:"resources"`
	Dashboards []struct {
		File string `yaml:"file"`
	} `yaml:"dashboards"`
}

func TestBuild_ManifestMatchesFiles(t *testing.T) {
	app, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := yaml.Unmarshal(fileByPath(t, app, "manifest.yaml"), &m); err != nil {
		t.Fatalf("manifest.yaml does not parse: %v", err)
	}
	if m.PackageFormatVersion != 1 || m.ID != AppID || m.DBName != AppDBName {
		t.Errorf("manifest identity: %+v", m)
	}
	if !strings.HasPrefix(m.Icon, "data:image/svg+xml;base64,") {
		t.Errorf("icon must be an svg data URI, got %.40q", m.Icon)
	}
	if _, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(m.Icon, "data:image/svg+xml;base64,")); err != nil {
		t.Errorf("icon payload is not base64: %v", err)
	}
	if len(m.Config) != 2 || m.Config[0].Key != "otel_port" || m.Config[0].Default != "4318" {
		t.Errorf("config keys: %+v", m.Config)
	}
	if len(m.Resources) != len(app.Resources) {
		t.Fatalf("manifest lists %d resources, Build produced %d", len(m.Resources), len(app.Resources))
	}
	for i, r := range m.Resources {
		want := fmt.Sprintf("ddl/%03d_%s.sql", i+1, app.Resources[i].Name)
		if r.File != want || r.Name != app.Resources[i].Name || r.Type != app.Resources[i].Kind {
			t.Errorf("resource %d: manifest %+v vs built %+v", i, r, app.Resources[i])
		}
		fileByPath(t, app, r.File) // must exist
	}
	if len(m.Dashboards) != 1 || m.Dashboards[0].File != "dashboards/main.json" {
		t.Errorf("dashboards: %+v", m.Dashboards)
	}
	for _, p := range []string{"dashboards/main.json", "README.md", "Makefile"} {
		fileByPath(t, app, p)
	}
}

func TestBuild_EveryDDLFileIsOneQualifiedStatement(t *testing.T) {
	app, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, r := range app.Resources {
		// UDFs are global (never db-qualified); system resources are not objects.
		if r.Kind != "system" && r.Kind != "udf" {
			names[r.Name] = true
		}
	}
	for _, r := range app.Resources {
		sql := string(fileByPath(t, app, r.File))
		if strings.TrimSpace(sql) != strings.TrimSpace(r.SQL) {
			t.Errorf("%s: file content differs from resource SQL", r.File)
		}
		if strings.HasSuffix(strings.TrimSpace(sql), ";") {
			t.Errorf("%s: trailing semicolon", r.File)
		}
		if n := countTopLevelSemicolons(sql); n != 0 {
			t.Errorf("%s: %d statement separators found — one statement per file", r.File, n)
		}
		switch r.Kind {
		case "udf":
			if strings.Contains(sql, DBPlaceholder) {
				t.Errorf("%s: udf must not be db-qualified", r.File)
			}
			if !strings.HasPrefix(sql, "CREATE OR REPLACE FUNCTION ") {
				t.Errorf("%s: udf must be CREATE OR REPLACE FUNCTION", r.File)
			}
		case "system":
			t.Errorf("%s: system resources are not allowed — they race distributed DDL on clusters", r.File)
		default:
			if !strings.Contains(sql, "IF NOT EXISTS "+DBPlaceholder+"."+r.Name) {
				t.Errorf("%s: expected 'IF NOT EXISTS {{ .DB }}.%s' in head: %.100q", r.File, r.Name, sql)
			}
			// no unqualified reference to any app resource
			for n := range names {
				for _, before := range precedingText(`\b`+regexp.QuoteMeta(n)+`\b`, sql) {
					if !strings.HasSuffix(before, DBPlaceholder+".") {
						t.Errorf("%s: unqualified reference to %s (preceded by %q)", r.File, n, before)
					}
				}
			}
		}
	}
}

func TestBuild_RuleArtifactsComeLast(t *testing.T) {
	app, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	first := -1
	for i, r := range app.Resources {
		if strings.HasPrefix(r.Name, "mv_rule_") || r.Kind == "udf" || r.Kind == "system" {
			if first == -1 {
				first = i
			}
		} else if first != -1 {
			t.Errorf("non-rule resource %s appears after rule artifacts started", r.Name)
		}
	}
	if first == -1 {
		t.Fatal("no rule artifacts")
	}
	for _, r := range app.Resources[first:] {
		if strings.Contains(strings.ToLower(r.SQL), "pause") {
			t.Errorf("%s: rule artifacts must not pause anything", r.Name)
		}
	}
	for _, need := range []string{"agentguard_rules", "agentguard_security_events", "agentguard_cim_event"} {
		found := false
		for _, r := range app.Resources[:first] {
			if r.Name == need {
				found = true
			}
		}
		if !found {
			t.Errorf("%s must be declared before rule artifacts", need)
		}
	}
}

// countTopLevelSemicolons counts ';' outside single-quoted strings, $$ bodies,
// and -- line comments.
func countTopLevelSemicolons(sql string) int {
	n := 0
	inStr, inDollar, inComment := false, false, false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case inComment:
			if c == '\n' {
				inComment = false
			}
		case inDollar:
			if c == '$' && i+1 < len(sql) && sql[i+1] == '$' {
				inDollar = false
				i++
			}
		case inStr:
			if c == '\'' {
				inStr = false
			}
		default:
			switch {
			case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
				inComment = true
			case c == '$' && i+1 < len(sql) && sql[i+1] == '$':
				inDollar = true
				i++
			case c == '\'':
				inStr = true
			case c == ';':
				n++
			}
		}
	}
	return n
}

// precedingText returns, for every match of pattern in s, the text immediately
// before the match (up to len(DBPlaceholder)+1 bytes).
func precedingText(pattern, s string) []string {
	re := regexp.MustCompile(pattern)
	var out []string
	for _, loc := range re.FindAllStringIndex(s, -1) {
		start := loc[0] - len(DBPlaceholder) - 1
		if start < 0 {
			start = 0
		}
		out = append(out, s[start:loc[0]])
	}
	return out
}
