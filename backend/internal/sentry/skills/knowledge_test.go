package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func loadArgs(t *testing.T, m map[string]string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSkillNamesEmbedded(t *testing.T) {
	names := skillNames("", nil)
	want := map[string]bool{"timeplus-sql-guide": false, "agentguard-guide": false}
	for _, n := range names {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("embedded skill %q missing from %v", n, names)
		}
	}
}

func TestLoadSkillEmbedded(t *testing.T) {
	rc := &RunContext{Loaded: map[string]bool{}}
	out, err := NewLoadSkill("").Execute(context.Background(), rc, loadArgs(t, map[string]string{"name": "timeplus-sql-guide"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Timeplus") {
		t.Fatalf("SKILL.md content missing, got %.80q", out)
	}
	again, _ := NewLoadSkill("").Execute(context.Background(), rc, loadArgs(t, map[string]string{"name": "timeplus-sql-guide"}))
	if !strings.Contains(again, "already loaded") {
		t.Fatal("second load must be a cheap no-op")
	}

	// Caching is per skill: loading the OTHER skill still returns content.
	other, _ := NewLoadSkill("").Execute(context.Background(), rc, loadArgs(t, map[string]string{"name": "agentguard-guide"}))
	if strings.Contains(other, "already loaded") || !strings.Contains(other, "AgentGuard") {
		t.Fatalf("agentguard-guide must load independently, got %.80q", other)
	}
}

func TestLoadSkillUnknownListsSkills(t *testing.T) {
	rc := &RunContext{Loaded: map[string]bool{}}
	for _, name := range []string{"", "no-such-skill", "../evil"} {
		out, err := NewLoadSkill("").Execute(context.Background(), rc, loadArgs(t, map[string]string{"name": name}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "available skills:") || !strings.Contains(out, "timeplus-sql-guide") {
			t.Fatalf("name=%q: miss must list skills, got %.120q", name, out)
		}
	}
}

func TestLoadSkillReferenceUnknown(t *testing.T) {
	rc := &RunContext{Loaded: map[string]bool{}}
	out, err := NewLoadSkillReference("").Execute(context.Background(), rc,
		loadArgs(t, map[string]string{"skill": "timeplus-sql-guide", "name": "no-such-file"}))
	if err != nil {
		t.Fatal(err) // skill errors are returned as results, not Go errors
	}
	if !strings.Contains(out, "available references") {
		t.Fatalf("miss must list valid names, got %.120q", out)
	}

	out, _ = NewLoadSkillReference("").Execute(context.Background(), rc,
		loadArgs(t, map[string]string{"skill": "no-such-skill", "name": "SQL_REFERENCE.md"}))
	if !strings.Contains(out, "available skills:") {
		t.Fatalf("unknown skill must list skills, got %.120q", out)
	}
}

func TestLoadSkillReferenceKnown(t *testing.T) {
	rc := &RunContext{Loaded: map[string]bool{}}
	args := loadArgs(t, map[string]string{"skill": "timeplus-sql-guide", "name": "SQL_REFERENCE.md"})
	out, err := NewLoadSkillReference("").Execute(context.Background(), rc, args)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "available references") {
		t.Fatalf("expected reference content, got miss message: %.120q", out)
	}
	again, _ := NewLoadSkillReference("").Execute(context.Background(), rc, args)
	if !strings.Contains(again, "already loaded") {
		t.Fatal("second load of same reference must be a cheap no-op")
	}
}

func TestAgentGuardGuideReferencesLoad(t *testing.T) {
	rc := &RunContext{Loaded: map[string]bool{}}
	for _, ref := range referenceNames("", "agentguard-guide") {
		out, err := NewLoadSkillReference("").Execute(context.Background(), rc,
			loadArgs(t, map[string]string{"skill": "agentguard-guide", "name": ref}))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "available references") || len(out) < 200 {
			t.Fatalf("reference %s did not load properly: %.120q", ref, out)
		}
	}
}
