package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	tp "github.com/timeplus/agentguard/internal/timeplus"
)

var (
	lineComment  = regexp.MustCompile(`--[^\n]*`)
	blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	limitRe      = regexp.MustCompile(`(?i)\bLIMIT\s+(\d+)\s*$`)

	// fromJoinRe finds the start of each source clause; the token following
	// it is inspected procedurally (Go's RE2 has no lookahead, so the
	// "not table(/not (/not system." exclusion from the brief's reference
	// regex can't be expressed as a single pattern).
	fromJoinRe = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+`)
	identRe    = regexp.MustCompile(`^[a-zA-Z_][\w.]*`)
	tableFnRe  = regexp.MustCompile(`(?i)^table\s*\(`)

	// CTE name collection: the first "WITH <name> AS" and any subsequent
	// ", <name> AS (" in a comma-separated WITH list.
	cteFirstRe = regexp.MustCompile(`(?i)\bWITH\s+(\w+)\s+AS\b`)
	cteRestRe  = regexp.MustCompile(`(?i),\s*(\w+)\s+AS\s*\(`)

	// forbiddenRe covers write/DDL/admin keywords. SYSTEM is intentionally
	// excluded here and checked separately (systemKeywordRe) because
	// "system.tables" / "system.v_failed_mat_views" are legitimate
	// read-only identifiers and \bSYSTEM\b would otherwise match the
	// "system" in "system.tables" (a "." is a word-boundary too).
	forbiddenRe     = regexp.MustCompile(`(?i)\b(INSERT|ALTER|CREATE|DROP|TRUNCATE|RENAME|OPTIMIZE|GRANT|REVOKE|KILL|SETTINGS)\b`)
	systemKeywordRe = regexp.MustCompile(`(?i)\bSYSTEM\b`)
)

// stripStrings blanks single-quoted literals so keyword scanning ignores
// their contents.
func stripStrings(s string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case in && ch == '\\' && i+1 < len(s):
			b.WriteByte(' ')
			i++ // skip escaped char
			b.WriteByte(' ')
		case ch == '\'':
			in = !in
			b.WriteByte(ch)
		case in:
			b.WriteByte(' ')
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}

// findForbiddenKeyword returns the first forbidden write/DDL/admin keyword
// found in scan, or "" if none. SYSTEM is only forbidden when it is not
// immediately followed by "." (i.e. not a "system.<table>" reference).
func findForbiddenKeyword(scan string) string {
	if m := forbiddenRe.FindString(scan); m != "" {
		return m
	}
	for _, loc := range systemKeywordRe.FindAllStringIndex(scan, -1) {
		end := loc[1]
		if end < len(scan) && scan[end] == '.' {
			continue // "system.tables" — legitimate read-only reference
		}
		return scan[loc[0]:loc[1]]
	}
	return ""
}

// collectCTENames returns the lowercase names defined in a leading WITH
// clause, e.g. "WITH a AS (...), b AS (...)" -> {"a", "b"}.
func collectCTENames(scan string) map[string]bool {
	names := map[string]bool{}
	if m := cteFirstRe.FindStringSubmatch(scan); m != nil {
		names[strings.ToLower(m[1])] = true
	}
	for _, m := range cteRestRe.FindAllStringSubmatch(scan, -1) {
		names[strings.ToLower(m[1])] = true
	}
	return names
}

// checkBareStreams rejects any FROM/JOIN source that is a bare stream
// identifier — i.e. not a subquery "(", not "table(...)", not a
// "system.<table>" reference, and not a name defined by a preceding WITH
// clause (CTE). Bare stream reads are unbounded streaming reads in
// Timeplus; historical reads must go through table(...).
func checkBareStreams(scan string) error {
	ctes := collectCTENames(scan)
	for _, loc := range fromJoinRe.FindAllStringIndex(scan, -1) {
		rest := strings.TrimLeft(scan[loc[1]:], " \t\n\r")
		if rest == "" {
			continue
		}
		if rest[0] == '(' {
			continue // subquery
		}
		if tableFnRe.MatchString(rest) {
			continue // table(...)
		}
		if len(rest) >= 7 && strings.EqualFold(rest[:7], "system.") {
			continue // system.<table>
		}
		name := identRe.FindString(rest)
		if name == "" {
			// Couldn't identify a source token — conservative: reject
			// rather than silently accept unknown/malformed input.
			return fmt.Errorf("could not parse source after FROM/JOIN — rejected for safety")
		}
		base := name
		if idx := strings.IndexByte(name, '.'); idx >= 0 {
			base = name[:idx]
		}
		if ctes[strings.ToLower(base)] {
			continue // CTE reference
		}
		return fmt.Errorf("stream %q must be read via table(%s) — unbounded streaming reads are not allowed", name, name)
	}
	return nil
}

// ValidateReadOnlySQL enforces the spec §6.2 rules and returns the SQL with
// a LIMIT injected (default 100) or clamped to maxRows. Conservative by
// design: false rejects are acceptable, false accepts are not.
func ValidateReadOnlySQL(sql string, maxRows int) (string, error) {
	s := blockComment.ReplaceAllString(sql, " ")
	s = lineComment.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ";")
	s = strings.TrimSpace(s)

	scan := stripStrings(s)
	if strings.Contains(scan, ";") {
		return "", fmt.Errorf("only a single statement is allowed")
	}

	upper := strings.ToUpper(strings.TrimSpace(scan))
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return "", fmt.Errorf("query must start with SELECT or WITH")
	}

	if kw := findForbiddenKeyword(scan); kw != "" {
		return "", fmt.Errorf("forbidden keyword %q — run_sql is read-only", strings.ToUpper(kw))
	}

	if err := checkBareStreams(scan); err != nil {
		return "", err
	}

	defaultLimit := 100
	if maxRows > 0 && maxRows < defaultLimit {
		defaultLimit = maxRows
	}

	if lm := limitRe.FindStringSubmatch(scan); lm != nil {
		n, err := strconv.Atoi(lm[1])
		if err == nil && maxRows > 0 && n > maxRows {
			s = limitRe.ReplaceAllString(s, fmt.Sprintf("LIMIT %d", maxRows))
		}
	} else {
		s = s + fmt.Sprintf(" LIMIT %d", defaultLimit)
	}
	return s, nil
}

type runSQL struct{}

// NewRunSQL returns the read-only "run_sql" skill.
func NewRunSQL() Skill { return runSQL{} }

func (runSQL) Name() string { return "run_sql" }
func (runSQL) Description() string {
	return "Run one read-only SQL query against Timeplus (historical: wrap streams in table()). Returns rows as JSON. LIMIT is enforced."
}
func (runSQL) Params() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"sql": map[string]any{"type": "string"}},
		"required":   []string{"sql"},
	}
}
func (runSQL) Access() AccessClass { return ReadOnly }

func (runSQL) Execute(ctx context.Context, rc *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		SQL string `json:"sql"`
	}
	if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.SQL) == "" {
		return "error: run_sql needs a {\"sql\": \"...\"} argument", nil
	}

	rewritten, err := ValidateReadOnlySQL(a.SQL, rc.SQLMaxRows)
	if err != nil {
		return "error: " + err.Error(), nil
	}

	qctx, cancel := context.WithTimeout(ctx, time.Duration(rc.SQLTimeoutMs)*time.Millisecond)
	defer cancel()

	type qres struct {
		rows []tp.Row
		err  error
	}
	ch := make(chan qres, 1)
	go func() {
		rows, err := tp.QueryNative(&rc.Cfg.Timeplus, rewritten)
		ch <- qres{rows, err}
	}()

	select {
	case <-qctx.Done():
		return "error: query timed out", nil
	case r := <-ch:
		if r.err != nil {
			return "error: " + r.err.Error(), nil
		}
		b, err := json.Marshal(r.rows)
		if err != nil {
			return "error: " + err.Error(), nil
		}
		out, _ := Truncate(string(b), rc.ResultMaxBytes)
		return out, nil
	}
}
