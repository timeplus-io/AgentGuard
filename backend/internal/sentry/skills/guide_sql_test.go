package skills

import (
	"regexp"
	"strings"
	"testing"
)

var sqlFenceRe = regexp.MustCompile("(?s)```sql\n(.*?)```")

// TestAgentGuardGuideSQLPassesValidator extracts every ```sql fence from the
// embedded agentguard-guide skill and runs the complete queries through
// ValidateReadOnlySQL — the guide must never publish a canonical query that
// run_sql would reject. Fences that are deliberate fragments (a WHERE
// predicate, a bare subquery template) are skipped: only fences starting
// with SELECT/WITH and free of <placeholder-in-FROM> templating are
// validated.
func TestAgentGuardGuideSQLPassesValidator(t *testing.T) {
	files := []string{"SKILL.md"}
	for _, r := range referenceNames("", "agentguard-guide") {
		files = append(files, "references/"+r)
	}
	checked := 0
	for _, f := range files {
		b, err := readSkillFile("", "agentguard-guide", f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, m := range sqlFenceRe.FindAllStringSubmatch(string(b), -1) {
			sql := strings.TrimSpace(m[1])
			upper := strings.ToUpper(sql)
			if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
				continue // fragment (WHERE snippet, etc.)
			}
			// Skip fences with a templating placeholder in a FROM position
			// (e.g. "FROM ( <current-holds subquery> )") — not literal SQL.
			// String-literal placeholders like '<session>' are fine.
			if regexp.MustCompile(`(?i)FROM\s*\(\s*<`).MatchString(sql) {
				continue
			}
			if _, err := ValidateReadOnlySQL(sql, 1000); err != nil {
				t.Errorf("%s: guide SQL rejected by run_sql validator: %v\n---\n%s", f, err, sql)
			}
			checked++
		}
	}
	if checked < 15 {
		t.Fatalf("only %d SQL fences validated — extraction is probably broken", checked)
	}
}
