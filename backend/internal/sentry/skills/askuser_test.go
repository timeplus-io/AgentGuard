package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const validAskArgs = `{"questions":[{"header":"Tools","question":"Which tools are unauthorized?","multiSelect":true,"options":[{"label":"Bash (Recommended)","description":"shell execution"},{"label":"Write","description":"filesystem writes"}]}]}`

// TestAskUserDefGating: ask_user is Interactive-only — never offered at
// GatedWrite or below (headless / no clarification registry), offered at
// Interactive.
func TestAskUserDefGating(t *testing.T) {
	r := NewRegistry(NewAskUser())
	if got := len(r.Defs(GatedWrite)); got != 0 {
		t.Fatalf("Defs(GatedWrite) = %d defs, want 0 (ask_user must not leak below Interactive)", got)
	}
	defs := r.Defs(Interactive)
	if len(defs) != 1 || defs[0].Name != "ask_user" {
		t.Fatalf("Defs(Interactive) = %+v, want exactly [ask_user]", defs)
	}
}

// TestAskUserExecuteIsStub: the loop intercepts Interactive calls; a direct
// Execute (defensive path) must fail loudly, never pretend to have asked.
func TestAskUserExecuteIsStub(t *testing.T) {
	_, err := NewAskUser().Execute(context.Background(), &RunContext{}, json.RawMessage(validAskArgs))
	if err == nil {
		t.Fatal("Execute must return an error — ask_user is handled by the chat loop")
	}
}

func TestParseAskUserQuestions(t *testing.T) {
	qs, err := ParseAskUserQuestions(json.RawMessage(validAskArgs))
	if err != nil {
		t.Fatalf("valid args: %v", err)
	}
	if len(qs) != 1 || qs[0].Header != "Tools" || !qs[0].MultiSelect || len(qs[0].Options) != 2 {
		t.Fatalf("parsed = %+v", qs)
	}
	if qs[0].Options[0].Label != "Bash (Recommended)" {
		t.Fatalf("option label = %q", qs[0].Options[0].Label)
	}

	bad := map[string]string{
		"not json":        `{`,
		"no questions":    `{"questions":[]}`,
		"five questions":  `{"questions":[{"question":"a","options":[{"label":"x"},{"label":"y"}]},{"question":"b","options":[{"label":"x"},{"label":"y"}]},{"question":"c","options":[{"label":"x"},{"label":"y"}]},{"question":"d","options":[{"label":"x"},{"label":"y"}]},{"question":"e","options":[{"label":"x"},{"label":"y"}]}]}`,
		"empty question":  `{"questions":[{"question":"  ","options":[{"label":"x"},{"label":"y"}]}]}`,
		"one option":      `{"questions":[{"question":"a","options":[{"label":"x"}]}]}`,
		"five options":    `{"questions":[{"question":"a","options":[{"label":"1"},{"label":"2"},{"label":"3"},{"label":"4"},{"label":"5"}]}]}`,
	}
	for name, args := range bad {
		if _, err := ParseAskUserQuestions(json.RawMessage(args)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

// TestAskUserDescriptionMentionsWhen: the description is the model's only
// standing guidance in generic-analyst mode (no agentguard rules block), so
// it must carry the "only when the answer changes the investigation" rule.
func TestAskUserDescriptionMentionsWhen(t *testing.T) {
	d := NewAskUser().Description()
	for _, want := range []string{"ambiguous", "Recommended"} {
		if !strings.Contains(d, want) {
			t.Errorf("description missing %q: %q", want, d)
		}
	}
}
