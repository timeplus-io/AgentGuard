package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLoadSkillEmbedded(t *testing.T) {
	rc := &RunContext{Loaded: map[string]bool{}}
	out, err := NewLoadSkill("").Execute(context.Background(), rc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Timeplus") {
		t.Fatalf("SKILL.md content missing, got %.80q", out)
	}
	again, _ := NewLoadSkill("").Execute(context.Background(), rc, nil)
	if !strings.Contains(again, "already loaded") {
		t.Fatal("second load must be a cheap no-op")
	}
}

func TestLoadSkillReferenceUnknown(t *testing.T) {
	rc := &RunContext{Loaded: map[string]bool{}}
	args, _ := json.Marshal(map[string]string{"name": "no-such-file"})
	out, err := NewLoadSkillReference("").Execute(context.Background(), rc, args)
	if err != nil {
		t.Fatal(err) // skill errors are returned as results, not Go errors
	}
	if !strings.Contains(out, "available references:") {
		t.Fatalf("miss must list valid names, got %.120q", out)
	}
}

func TestLoadSkillReferenceKnown(t *testing.T) {
	rc := &RunContext{Loaded: map[string]bool{}}
	args, _ := json.Marshal(map[string]string{"name": "SQL_REFERENCE.md"})
	out, err := NewLoadSkillReference("").Execute(context.Background(), rc, args)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "available references:") {
		t.Fatalf("expected reference content, got miss message: %.120q", out)
	}
	again, _ := NewLoadSkillReference("").Execute(context.Background(), rc, args)
	if !strings.Contains(again, "already loaded") {
		t.Fatal("second load of same reference must be a cheap no-op")
	}
}
