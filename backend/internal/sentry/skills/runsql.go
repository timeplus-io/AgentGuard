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

	// limitOffsetRe/limitCommaRe extend clamping to "LIMIT n OFFSET m" and
	// "LIMIT o, n" (the row-count component is the one clamped: group 1 for
	// OFFSET form, group 2 for the comma form). limitPresenceRe is a
	// non-anchored "is there a LIMIT anywhere" check used only to decide
	// whether to skip appending a second LIMIT when none of the clampable
	// end-anchored forms above matched.
	limitOffsetRe   = regexp.MustCompile(`(?i)\bLIMIT\s+(\d+)\s+OFFSET\s+(\d+)\s*$`)
	limitCommaRe    = regexp.MustCompile(`(?i)\bLIMIT\s+(\d+)\s*,\s*(\d+)\s*$`)
	limitPresenceRe = regexp.MustCompile(`(?i)\bLIMIT\b`)

	// fromJoinRe finds the start of each source clause; the token following
	// it is inspected procedurally (Go's RE2 has no lookahead, so the
	// "not table(/not (/not system." exclusion from the brief's reference
	// regex can't be expressed as a single pattern).
	fromJoinRe = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+`)
	identRe    = regexp.MustCompile(`^[a-zA-Z_][\w.]*`)
	tableFnRe  = regexp.MustCompile(`(?i)^table\s*\(`)

	// plainTableArgRe is the only shape accepted inside table(...): a
	// single bare identifier, no parens, no quotes — so table(url('...'))
	// / table(file('...')) can't smuggle a network/file source in.
	plainTableArgRe = regexp.MustCompile(`^[a-zA-Z_][\w.]*$`)

	// asKeywordRe/reservedAfterSourceRe support walking comma-separated
	// FROM-list relations: after validating one source, skip an optional
	// "[AS] alias" token before checking whether a top-level comma follows.
	// reservedAfterSourceRe stops the alias-skip from swallowing the next
	// clause keyword (JOIN, WHERE, GROUP BY, ...) as if it were an alias.
	asKeywordRe           = regexp.MustCompile(`(?i)^AS\b`)
	reservedAfterSourceRe = regexp.MustCompile(`(?i)^(JOIN|INNER|LEFT|RIGHT|FULL|CROSS|OUTER|SEMI|ANTI|ASOF|WHERE|GROUP|ORDER|HAVING|LIMIT|UNION|EXCEPT|INTERSECT|ON|USING|SETTINGS|ARRAY|WITH|PREWHERE|QUALIFY)$`)

	// CTE name collection: the first "WITH <name> AS" and any subsequent
	// ", <name> AS (" in a comma-separated WITH list.
	cteFirstRe = regexp.MustCompile(`(?i)\bWITH\s+(\w+)\s+AS\b`)
	cteRestRe  = regexp.MustCompile(`(?i),\s*(\w+)\s+AS\s*\(`)

	// forbiddenRe covers write/DDL/admin keywords and client I/O directives
	// (OUTFILE/INFILE, e.g. "... INTO OUTFILE '/tmp/x'"). SYSTEM is
	// intentionally excluded here and checked separately (systemKeywordRe)
	// because "system.tables" / "system.v_failed_mat_views" are legitimate
	// read-only identifiers and \bSYSTEM\b would otherwise match the
	// "system" in "system.tables" (a "." is a word-boundary too).
	forbiddenRe     = regexp.MustCompile(`(?i)\b(INSERT|ALTER|CREATE|DROP|TRUNCATE|RENAME|OPTIMIZE|GRANT|REVOKE|KILL|SETTINGS|OUTFILE|INFILE)\b`)
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

// findMatchingClose returns the index within s (which must be the text
// immediately after an already-consumed opening '(', i.e. starting at
// depth 1) of the ')' that returns depth to 0, or -1 if s ends first.
// Nested parens (table(...) args, function calls, sub-selects) are
// depth-tracked so an inner ')' doesn't prematurely end the scan.
func findMatchingClose(s string) int {
	depth := 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// validateTableCall validates the argument of a "table(...)" source
// starting at rest (which matches tableFnRe) and returns the length of
// rest consumed by the whole "table(...)" construct. Only a single plain
// identifier is accepted inside the parens — table(url('...')) /
// table(file('...')) etc. are rejected, since anything else lets the
// argument reach a UDTF that can read arbitrary network/file sources.
func validateTableCall(rest string) (int, error) {
	m := tableFnRe.FindString(rest)
	openIdx := len(m)
	idx := findMatchingClose(rest[openIdx:])
	if idx == -1 {
		return 0, fmt.Errorf("unbalanced parentheses in table() — rejected for safety")
	}
	interior := strings.TrimSpace(rest[openIdx : openIdx+idx])
	if !plainTableArgRe.MatchString(interior) {
		return 0, fmt.Errorf("table() must wrap a plain stream name")
	}
	return openIdx + idx + 1, nil
}

// validateSource validates a single FROM/JOIN source at the start of rest
// (rest must be non-empty) — a subquery "(...)", a "table(...)" call, a
// "system.<table>" reference, or a name defined by a preceding WITH clause
// (CTE) — and returns the number of bytes of rest it consumes, so the
// caller can continue scanning past it (e.g. to check for a following
// comma-joined source in the same FROM list).
func validateSource(rest string, ctes map[string]bool) (int, error) {
	if rest[0] == '(' {
		idx := findMatchingClose(rest[1:])
		if idx == -1 {
			return 0, fmt.Errorf("unbalanced parentheses in FROM clause — rejected for safety")
		}
		return idx + 2, nil // subquery — allowed
	}
	if tableFnRe.MatchString(rest) {
		return validateTableCall(rest)
	}
	if len(rest) >= 7 && strings.EqualFold(rest[:7], "system.") {
		name := identRe.FindString(rest)
		if name == "" {
			return 0, fmt.Errorf("could not parse source after FROM/JOIN — rejected for safety")
		}
		return len(name), nil // system.<table>
	}
	name := identRe.FindString(rest)
	if name == "" {
		// Couldn't identify a source token — conservative: reject rather
		// than silently accept unknown/malformed input.
		return 0, fmt.Errorf("could not parse source after FROM/JOIN — rejected for safety")
	}
	base := name
	if idx := strings.IndexByte(name, '.'); idx >= 0 {
		base = name[:idx]
	}
	if ctes[strings.ToLower(base)] {
		return len(name), nil // CTE reference
	}
	return 0, fmt.Errorf("stream %q must be read via table(%s) — unbounded streaming reads are not allowed", name, name)
}

// skipAlias skips an optional "[AS] <alias>" token at the start of s and
// returns what follows it. It only ever consumes a single trailing
// identifier (optionally preceded by AS) so it can't run away past clause
// boundaries; it refuses to consume a reserved clause keyword (JOIN,
// WHERE, GROUP, ...) as if it were an alias, so it never masks a bare
// stream that follows one of those keywords instead of a comma.
func skipAlias(s string) string {
	t := strings.TrimLeft(s, " \t\n\r")
	if asKeywordRe.MatchString(t) {
		t = strings.TrimLeft(t[2:], " \t\n\r")
	}
	name := identRe.FindString(t)
	if name == "" || reservedAfterSourceRe.MatchString(name) {
		return s // nothing consumed
	}
	return t[len(name):]
}

// checkBareStreams rejects any FROM/JOIN source that is a bare stream
// identifier — i.e. not a subquery "(", not "table(...)", not a
// "system.<table>" reference, and not a name defined by a preceding WITH
// clause (CTE). Bare stream reads are unbounded streaming reads in
// Timeplus; historical reads must go through table(...).
//
// Each FROM/JOIN source can itself be followed by further comma-separated
// relations (e.g. "FROM table(a), agentguard_cim_event"); after validating
// one source, checkBareStreams skips its optional alias and, if a
// top-level comma follows, validates the next source the same way — so a
// bare stream can't hide behind an earlier, legitimate first source.
func checkBareStreams(scan string) error {
	ctes := collectCTENames(scan)
	for _, loc := range fromJoinRe.FindAllStringIndex(scan, -1) {
		cursor := scan[loc[1]:]
		for {
			cursor = strings.TrimLeft(cursor, " \t\n\r")
			if cursor == "" {
				break
			}
			consumed, err := validateSource(cursor, ctes)
			if err != nil {
				return err
			}
			cursor = cursor[consumed:]
			cursor = skipAlias(cursor)
			trimmed := strings.TrimLeft(cursor, " \t\n\r")
			if !strings.HasPrefix(trimmed, ",") {
				break
			}
			cursor = trimmed[1:] // consume the comma, loop to the next source
		}
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

	switch {
	case limitCommaRe.MatchString(scan):
		// "LIMIT offset, count" — the second number is the row count.
		lm := limitCommaRe.FindStringSubmatch(scan)
		n, err := strconv.Atoi(lm[2])
		if err == nil && maxRows > 0 && n > maxRows {
			s = limitCommaRe.ReplaceAllString(s, fmt.Sprintf("LIMIT $1, %d", maxRows))
		}
	case limitOffsetRe.MatchString(scan):
		// "LIMIT count OFFSET offset" — the first number is the row count.
		lm := limitOffsetRe.FindStringSubmatch(scan)
		n, err := strconv.Atoi(lm[1])
		if err == nil && maxRows > 0 && n > maxRows {
			s = limitOffsetRe.ReplaceAllString(s, fmt.Sprintf("LIMIT %d OFFSET $2", maxRows))
		}
	case limitRe.MatchString(scan):
		lm := limitRe.FindStringSubmatch(scan)
		n, err := strconv.Atoi(lm[1])
		if err == nil && maxRows > 0 && n > maxRows {
			s = limitRe.ReplaceAllString(s, fmt.Sprintf("LIMIT %d", maxRows))
		}
	case limitPresenceRe.MatchString(scan):
		// A LIMIT exists somewhere but not in a form we confidently clamp
		// end-anchored — leave it unchanged rather than risk generating
		// invalid SQL with two LIMIT clauses.
	default:
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
