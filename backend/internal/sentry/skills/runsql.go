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
	reservedAfterSourceRe = regexp.MustCompile(`(?i)^(JOIN|INNER|LEFT|RIGHT|FULL|CROSS|OUTER|SEMI|ANTI|ANY|ALL|ASOF|WHERE|GROUP|ORDER|HAVING|LIMIT|UNION|EXCEPT|INTERSECT|ON|USING|SETTINGS|ARRAY|WITH|PREWHERE|QUALIFY|FORMAT|INTO)$`)

	// joinLeadKeywordRe/joinModifierRe identify the subset of
	// reservedAfterSourceRe that introduces a joined source (a bare "JOIN",
	// or a modifier that precedes one, e.g. "LEFT"/"ASOF"). joinModifierRe
	// excludes JOIN itself — it is used by skipJoinKeyword to walk past zero
	// or more modifiers before the mandatory literal JOIN token.
	joinLeadKeywordRe = regexp.MustCompile(`(?i)^(JOIN|INNER|LEFT|RIGHT|FULL|CROSS|OUTER|SEMI|ANTI|ANY|ALL|ASOF)$`)
	joinModifierRe    = regexp.MustCompile(`(?i)^(INNER|LEFT|RIGHT|FULL|CROSS|OUTER|SEMI|ANTI|ANY|ALL|ASOF)$`)

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

// skipJoinKeyword skips a join-lead sequence at the start of s — zero or
// more modifiers (LEFT/RIGHT/INNER/FULL/CROSS/OUTER/SEMI/ANTI/ANY/ALL/ASOF)
// followed by the mandatory literal "JOIN" — and returns what follows the
// "JOIN" token (the joined source, still to be validated by the caller). s
// must start with a token matching joinLeadKeywordRe (the caller checks
// this before calling). Every iteration consumes a token, so this always
// terminates; an unrecognized token before a "JOIN" is reached is rejected
// rather than silently skipped.
func skipJoinKeyword(s string) (string, error) {
	for {
		s = strings.TrimLeft(s, " \t\n\r")
		tok := identRe.FindString(s)
		if tok == "" {
			return "", fmt.Errorf("expected JOIN keyword near %q — rejected for safety", errSnippet(s))
		}
		if strings.EqualFold(tok, "JOIN") {
			return s[len(tok):], nil
		}
		if !joinModifierRe.MatchString(tok) {
			return "", fmt.Errorf("expected JOIN keyword near %q — rejected for safety", errSnippet(s))
		}
		s = s[len(tok):]
	}
}

// consumeUntilTopLevelStop scans s — the text immediately after an ON/USING
// keyword — for the end of that clause's predicate expression, tracking
// parenthesis depth so a comma or keyword nested inside the predicate's own
// parens (a function call, a sub-expression) doesn't end it prematurely. It
// stops at, and does NOT consume:
//   - a depth-0 comma (the predicate is done; a comma-joined source follows);
//   - a depth-0 occurrence of any reservedAfterSourceRe keyword (this covers
//     both a chained JOIN — "... ON a=b JOIN c ON ..." — and every
//     FROM-terminating clause keyword: WHERE/GROUP/ORDER/HAVING/LIMIT/
//     SETTINGS/UNION/EXCEPT/INTERSECT/WITH/PREWHERE/QUALIFY/ARRAY/FORMAT/
//     INTO, plus a bare ON/USING which would indicate malformed SQL);
//   - a depth-0 ")" that has no matching "(" within s itself — this closes a
//     paren opened before the clause started (an enclosing subquery/CTE/
//     function-arg scope), so it isn't part of the predicate.
//
// It returns len(s) (consuming everything) at end of string. The returned
// value is always the number of bytes to consume, so the caller resumes
// scanning exactly at the stop token.
func consumeUntilTopLevelStop(s string) int {
	depth := 0
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '(':
			depth++
			i++
		case c == ')':
			if depth == 0 {
				return i
			}
			depth--
			i++
		case depth == 0 && c == ',':
			return i
		case depth == 0 && ((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'):
			tok := identRe.FindString(s[i:])
			if reservedAfterSourceRe.MatchString(tok) {
				return i
			}
			i += len(tok)
		default:
			i++
		}
	}
	return len(s)
}

// consumeOnClause consumes an "ON <predicate>" clause at the start of
// trimmed (trimmed must start with the "ON" token) and returns what follows
// the predicate — a depth-0 comma, a depth-0 clause/JOIN keyword, a
// depth-0 ")" belonging to an enclosing scope, or "" at end of string — so
// the caller can resume its FROM-list continuation check right there.
func consumeOnClause(trimmed string) string {
	tok := identRe.FindString(trimmed) // "ON"
	rest := strings.TrimLeft(trimmed[len(tok):], " \t\n\r")
	n := consumeUntilTopLevelStop(rest)
	return rest[n:]
}

// consumeUsingClause consumes a "USING (<cols>)" or "USING <col>" clause at
// the start of trimmed (trimmed must start with the "USING" token) and
// returns what follows it, on the same terms as consumeOnClause. The
// parenthesized column-list form is the common one and is skipped via the
// existing matching-close helper; a non-paren column-ref form falls back to
// the generic predicate scanner.
func consumeUsingClause(trimmed string) (string, error) {
	tok := identRe.FindString(trimmed) // "USING"
	rest := strings.TrimLeft(trimmed[len(tok):], " \t\n\r")
	if strings.HasPrefix(rest, "(") {
		idx := findMatchingClose(rest[1:])
		if idx == -1 {
			return "", fmt.Errorf("unbalanced parentheses in USING clause — rejected for safety")
		}
		return rest[idx+2:], nil
	}
	n := consumeUntilTopLevelStop(rest)
	return rest[n:], nil
}

// maxErrSnippetLen bounds how much of the offending FROM-clause tail is
// echoed back in the "unrecognized token" error below.
const maxErrSnippetLen = 40

func errSnippet(s string) string {
	if len(s) <= maxErrSnippetLen {
		return s
	}
	return s[:maxErrSnippetLen] + "..."
}

// parenDepthBefore returns the net paren-nesting depth of scan[:idx] (count
// of unmatched "(" before idx). checkBareStreams uses this to tell whether
// a ")" encountered while walking a FROM-list closes a scope (subquery, CTE
// body, function argument) that was opened before this FROM/JOIN keyword —
// in which case it's the legitimate end of this nested FROM-list — versus
// an unbalanced/injected ")" with no corresponding open paren at this
// FROM's own level, which must be rejected instead.
func parenDepthBefore(scan string, idx int) int {
	depth := 0
	for i := 0; i < idx; i++ {
		switch scan[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	return depth
}

// checkBareStreams rejects any FROM/JOIN source that is a bare stream
// identifier — i.e. not a subquery "(", not "table(...)", not a
// "system.<table>" reference, and not a name defined by a preceding WITH
// clause (CTE). Bare stream reads are unbounded streaming reads in
// Timeplus; historical reads must go through table(...).
//
// Each FROM/JOIN source can itself be followed by further comma-separated
// relations (e.g. "FROM table(a), agentguard_cim_event"), or by a JOIN
// whose ON/USING predicate is in turn followed by further comma-separated
// relations (e.g. "FROM table(a) JOIN table(b) ON a.id=b.id, bare_stream").
// After validating one source and skipping at most one optional
// "[AS] alias" token, checkBareStreams inspects the next non-whitespace
// token and continues or stops ONLY when that token is recognized:
//   - a comma -> validate the next source;
//   - a JOIN keyword (joinLeadKeywordRe: JOIN itself, or a modifier that
//     precedes it — LEFT/RIGHT/INNER/FULL/CROSS/OUTER/SEMI/ANTI/ANY/ALL/
//     ASOF) -> skip the modifier(s) and the "JOIN" token, then validate the
//     next source (the joined table) exactly as above — chained joins
//     ("A JOIN B ON .. JOIN C ON ..") recurse through this same step;
//   - ON/USING -> consume that clause's predicate (paren-depth-aware, via
//     consumeOnClause/consumeUsingClause) and resume this same check on
//     whatever follows the predicate, rather than stopping — this is what
//     lets a comma-joined source (or a further chained JOIN) after the
//     predicate still be reached and validated;
//   - any other recognized clause keyword (the remainder of
//     reservedAfterSourceRe: WHERE, GROUP, ORDER, HAVING, LIMIT, UNION,
//     EXCEPT, INTERSECT, SETTINGS, ARRAY, WITH, PREWHERE, QUALIFY, FORMAT,
//     INTO) -> the FROM-list ends here, cleanly;
//   - end of string -> the FROM-list ends here, cleanly;
//   - a ")" that closes a paren opened before this FROM/JOIN's own source
//     list started (parenDepthBefore > 0, i.e. this FROM lives inside an
//     enclosing subquery/CTE-body/function-arg) -> that enclosing scope is
//     closing, so this FROM-list ends here, cleanly;
//   - anything else -> REJECT.
//
// This is deliberately fail-closed: an unrecognized token (SAMPLE, FINAL, a
// second alias-shaped word, ...) between a validated source and whatever
// follows must never be silently treated as "end of FROM-list", because
// that would let a comma-joined bare stream hide behind it unvalidated.
// Some legitimate modifiers we don't parse (SAMPLE/FINAL) are rejected as
// an acceptable Phase 1 false-reject — the caller can rephrase.
func checkBareStreams(scan string) error {
	ctes := collectCTENames(scan)
	for _, loc := range fromJoinRe.FindAllStringIndex(scan, -1) {
		startDepth := parenDepthBefore(scan, loc[1])
		cursor := scan[loc[1]:]

	sourceLoop:
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

			// Resolve whatever follows the validated source: a comma-joined
			// sibling, a JOIN (possibly chained through more ON/USING
			// clauses), or a clean stop. ON/USING clauses are consumed and
			// re-checked in place rather than ending the walk, so a source
			// after the predicate is never skipped over unvalidated.
			for {
				trimmed := strings.TrimLeft(cursor, " \t\n\r")
				if trimmed == "" {
					break sourceLoop // end of string — FROM-list ends here
				}
				if strings.HasPrefix(trimmed, ",") {
					cursor = trimmed[1:] // consume the comma
					continue sourceLoop  // validate the next source
				}
				if strings.HasPrefix(trimmed, ")") && startDepth > 0 {
					break sourceLoop // closes an enclosing subquery/CTE/function-arg scope
				}
				next := identRe.FindString(trimmed)
				if next == "" {
					return fmt.Errorf("unrecognized token in FROM clause near %q — only table(), sub-selects, system.* and CTE names are allowed", errSnippet(trimmed))
				}
				switch {
				case joinLeadKeywordRe.MatchString(next):
					rest, err := skipJoinKeyword(trimmed)
					if err != nil {
						return err
					}
					cursor = rest
					continue sourceLoop // validate the joined source
				case strings.EqualFold(next, "ON"):
					cursor = consumeOnClause(trimmed)
					continue // re-check what follows the predicate
				case strings.EqualFold(next, "USING"):
					rest, err := consumeUsingClause(trimmed)
					if err != nil {
						return err
					}
					cursor = rest
					continue // re-check what follows the predicate
				case reservedAfterSourceRe.MatchString(next):
					break sourceLoop // WHERE/GROUP/ORDER/... clause keyword ends the FROM-list cleanly
				default:
					return fmt.Errorf("unrecognized token in FROM clause near %q — only table(), sub-selects, system.* and CTE names are allowed", errSnippet(trimmed))
				}
			}
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
