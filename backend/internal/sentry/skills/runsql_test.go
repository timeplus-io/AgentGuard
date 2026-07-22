package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

// TestValidateReadOnlySQL is the binding accept/reject matrix from the task-6
// brief (spec §6.2). Do not add cases into this slice — extra hardening
// cases live in TestValidateReadOnlySQLHardening below so this matrix stays
// verifiable against the brief verbatim.
func TestValidateReadOnlySQL(t *testing.T) {
	cases := []struct {
		name, sql string
		wantErr   string // substring, "" = accept
		wantIn    string // substring expected in rewritten SQL
	}{
		{"plain select", "SELECT 1", "", "LIMIT 100"},
		{"table select", "SELECT * FROM table(agentguard_cim_event)", "", "LIMIT 100"},
		{"with cte", "WITH t AS (SELECT 1) SELECT * FROM t", "", "LIMIT 100"},
		{"keeps small limit", "SELECT 1 LIMIT 5", "", "LIMIT 5"},
		{"clamps big limit", "SELECT 1 LIMIT 999999", "", "LIMIT 1000"},
		{"trailing semicolon ok", "SELECT 1;", "", "LIMIT 100"},
		{"multi statement", "SELECT 1; SELECT 2", "single statement", ""},
		{"insert", "INSERT INTO x VALUES (1)", "must start with SELECT", ""},
		{"embedded drop", "SELECT 1 FROM t; DROP STREAM x", "single statement", ""},
		{"create keyword", "SELECT * FROM (SELECT 1) CREATE", "forbidden keyword", ""},
		{"settings", "SELECT 1 SETTINGS max_threads=1", "forbidden keyword", ""},
		{"bare stream read", "SELECT * FROM agentguard_cim_event", "table(", ""},
		{"join bare stream", "SELECT * FROM table(a) JOIN agentguard_rules USING (x)", "table(", ""},
		{"keyword in string ok", "SELECT 'please INSERT nothing' FROM table(a)", "", "LIMIT 100"},
		{"comment stripped", "SELECT 1 -- DROP STREAM x", "", "LIMIT 100"},
		{"block comment stripped", "SELECT /* INSERT */ 1", "", "LIMIT 100"},
		{"system tables allowed", "SELECT name FROM system.tables", "", "LIMIT 100"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := ValidateReadOnlySQL(c.sql, 1000)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected reject: %v", err)
				}
				if !strings.Contains(out, c.wantIn) {
					t.Fatalf("rewritten %q missing %q", out, c.wantIn)
				}
			} else if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

// TestValidateReadOnlySQLHardening covers adversarial bypasses beyond the
// binding matrix: comment-prefixed keywords, lowercase forms, semicolons
// inside comments, multi-CTE lists, nested bare streams, schema-qualified
// bare streams, and the SYSTEM keyword vs. the "system." table prefix.
func TestValidateReadOnlySQLHardening(t *testing.T) {
	cases := []struct {
		name, sql string
		wantErr   string
		wantIn    string
	}{
		{"comment-prefixed insert rejected", "/*x*/INSERT INTO y VALUES (1)", "must start with SELECT", ""},
		{"lowercase insert rejected", "insert into x values (1)", "must start with SELECT", ""},
		{"lowercase create rejected", "select * from (select 1) create", "forbidden keyword", ""},
		{"lowercase drop rejected", "select * from table(a); drop stream x", "single statement", ""},
		{"semicolon inside line comment ok", "SELECT 1 -- ; DROP STREAM x", "", "LIMIT 100"},
		{"semicolon inside block comment ok", "SELECT 1 /* ; DROP STREAM x */", "", "LIMIT 100"},
		{"multiple ctes", "WITH a AS (SELECT 1), b AS (SELECT 2) SELECT * FROM a JOIN b ON 1=1", "", "LIMIT 100"},
		{"bare stream inside subquery", "SELECT * FROM (SELECT * FROM agentguard_cim_event)", "table(", ""},
		{"bare stream with schema prefix", "SELECT * FROM mydb.agentguard_cim_event", "table(", ""},
		{"stream name prefixed with system but not system schema", "SELECT * FROM systemx_tables", "table(", ""},
		{"bare system keyword forbidden", "SELECT 1 SYSTEM STOP MERGES", "forbidden keyword", ""},
		{"system dot table not forbidden", "SELECT * FROM system.v_failed_mat_views", "", "LIMIT 100"},
		{"whitespace before limit clamped", "SELECT 1   LIMIT    999999", "", "LIMIT 1000"},
		{"case-insensitive limit clamp", "select 1 limit 999999", "", "LIMIT 1000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := ValidateReadOnlySQL(c.sql, 1000)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected reject: %v", err)
				}
				if !strings.Contains(out, c.wantIn) {
					t.Fatalf("rewritten %q missing %q", out, c.wantIn)
				}
			} else if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

// TestValidateReadOnlySQLTask7Fixes covers the adversarial-review findings
// fixed after task 6 shipped: comma-joined FROM sources that bypass the
// immediate-source-only bare-stream check, trusted table(...) interiors,
// LIMIT clamping for the OFFSET/comma forms, and OUTFILE/INFILE.
func TestValidateReadOnlySQLTask7Fixes(t *testing.T) {
	cases := []struct {
		name, sql string
		wantErr   string
		wantIn    string
	}{
		// Finding 1: comma-joined bare streams must still be caught.
		{"comma joined bare stream after table()", "SELECT * FROM table(a), agentguard_cim_event", "table(", ""},
		{"comma joined bare stream after subquery+alias", "SELECT * FROM (SELECT 1) x, agentguard_rules", "table(", ""},
		{"comma joined bare stream after system table", "SELECT * FROM system.tables, agentguard_cim_event", "table(", ""},
		// Finding 2: table(...) interior must be a single plain identifier.
		{"table() wrapping url() rejected", "SELECT * FROM table(url('http://evil'))", "table() must wrap a plain stream name", ""},
		{"table() wrapping file() rejected", "SELECT * FROM table(file('/etc/passwd'))", "table() must wrap a plain stream name", ""},
		// Finding 3: LIMIT clamp must cover OFFSET and comma forms, and
		// never append a duplicate LIMIT when one already exists.
		{"limit offset clamped", "SELECT 1 LIMIT 999999 OFFSET 5", "", "LIMIT 1000 OFFSET 5"},
		{"limit comma clamped", "SELECT 1 LIMIT 10, 999999", "", "LIMIT 10, 1000"},
		{"limit offset within bound stays intact, no duplicate limit", "SELECT 1 LIMIT 5 OFFSET 2", "", "LIMIT 5 OFFSET 2"},
		// Finding 4: OUTFILE/INFILE are forbidden client directives.
		{"into outfile rejected", "SELECT * FROM table(a) INTO OUTFILE '/tmp/x'", "forbidden keyword", ""},
		{"infile rejected", "SELECT * FROM table(a) INTO INFILE '/tmp/x'", "forbidden keyword", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := ValidateReadOnlySQL(c.sql, 1000)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected reject: %v", err)
				}
				if !strings.Contains(out, c.wantIn) {
					t.Fatalf("rewritten %q missing %q", out, c.wantIn)
				}
			} else if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want error containing %q, got %v", c.wantErr, err)
			}
		})
	}
}

// TestValidateReadOnlySQLTask7NoDuplicateLimitAppend guards the "leave
// unchanged, don't append a second LIMIT" branch for a LIMIT form the
// clamp regexes don't recognize (LIMIT present but not end-anchored in a
// clampable shape) — the presence check must still suppress the append.
func TestValidateReadOnlySQLTask7NoDuplicateLimitAppend(t *testing.T) {
	out, err := ValidateReadOnlySQL("SELECT 1 LIMIT 5 OFFSET 2", 1000)
	if err != nil {
		t.Fatalf("unexpected reject: %v", err)
	}
	if strings.Count(strings.ToUpper(out), "LIMIT") != 1 {
		t.Fatalf("expected exactly one LIMIT clause, got %q", out)
	}
}

func TestRunSQLSkillMetadata(t *testing.T) {
	s := NewRunSQL()
	if s.Name() != "run_sql" {
		t.Fatalf("Name() = %q, want run_sql", s.Name())
	}
	if s.Access() != ReadOnly {
		t.Fatalf("Access() = %v, want ReadOnly", s.Access())
	}
	params := s.Params()
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Params() properties missing or wrong type: %#v", params)
	}
	if _, ok := props["sql"]; !ok {
		t.Fatalf("Params() missing sql property: %#v", params)
	}
}

func TestRunSQLExecuteMissingArg(t *testing.T) {
	s := NewRunSQL()
	rc := &RunContext{Cfg: &config.Config{}, SQLTimeoutMs: 1000, SQLMaxRows: 1000, ResultMaxBytes: 4096}
	out, err := s.Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("want error: prefix, got %q", out)
	}
}

func TestRunSQLExecuteBadJSON(t *testing.T) {
	s := NewRunSQL()
	rc := &RunContext{Cfg: &config.Config{}, SQLTimeoutMs: 1000, SQLMaxRows: 1000, ResultMaxBytes: 4096}
	out, err := s.Execute(context.Background(), rc, json.RawMessage(`not json`))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("want error: prefix, got %q", out)
	}
}

func TestRunSQLExecuteValidationFailure(t *testing.T) {
	s := NewRunSQL()
	rc := &RunContext{Cfg: &config.Config{}, SQLTimeoutMs: 1000, SQLMaxRows: 1000, ResultMaxBytes: 4096}
	args, _ := json.Marshal(map[string]string{"sql": "DROP STREAM x"})
	out, err := s.Execute(context.Background(), rc, args)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") || !strings.Contains(out, "must start with SELECT") {
		t.Fatalf("want validation error result, got %q", out)
	}
}
