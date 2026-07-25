package sentry

import (
	"strings"
	"testing"
	"time"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
)

func TestIsAutoTitle(t *testing.T) {
	long := strings.Repeat("x", 200)
	cases := []struct {
		name, title, first string
		want               bool
	}{
		{"fresh conversation", "show me threats", "show me threats", true},
		{"user renamed", "My triage session", "show me threats", false},
		{"llm renamed", "Threat triage for dev agents", "show me threats", false},
		{"long first message still auto", truncateTitle(long), long, true},
		{"empty title never auto", "", "show me threats", false},
	}
	for _, c := range cases {
		if got := IsAutoTitle(c.title, c.first); got != c.want {
			t.Errorf("%s: IsAutoTitle(%q, %q) = %v, want %v", c.name, c.title, c.first, got, c.want)
		}
	}
}

func TestSanitizeTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"Threat triage"`, "Threat triage"},
		{"  Cost   spike\ninvestigation  ", "Cost spike investigation"},
		{"'quoted'", "quoted"},
		{"“smart quotes”", "smart quotes"},
		{"   ", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := sanitizeTitle(c.in); got != c.want {
			t.Errorf("sanitizeTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := sanitizeTitle(strings.Repeat("a", 100)); len([]rune(got)) != 80 {
		t.Errorf("sanitizeTitle long input: got %d runes, want 80", len([]rune(got)))
	}
}

func TestFirstUserAssistant(t *testing.T) {
	msgs := []models.SentryMessage{
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "", ToolArgs: `[{"ID":"1"}]`}, // tool-only turn
		{Role: "tool", ToolResult: "rows"},
		{Role: "assistant", Content: "the answer"},
		{Role: "user", Content: "second question"},
		{Role: "assistant", Content: "second answer"},
	}
	u, a := firstUserAssistant(msgs)
	if u != "first question" || a != "the answer" {
		t.Fatalf("got (%q, %q), want (first question, the answer)", u, a)
	}

	u, a = firstUserAssistant(nil)
	if u != "" || a != "" {
		t.Fatalf("empty replay: got (%q, %q), want empty", u, a)
	}
}

func TestMaybeAutoTitleAsyncFailsOpen(t *testing.T) {
	// Unreachable Timeplus: the store lookup fails, and the pass must
	// complete silently (fail-open) without panicking or calling the LLM.
	store := &Store{TP: &config.TimeplusConfig{Host: "127.0.0.1", Port: 1}}
	autoTitleDone = make(chan struct{})
	defer func() { autoTitleDone = nil }()
	MaybeAutoTitleAsync(nil, ChatCfg{}, store, "conv-1")
	select {
	case <-autoTitleDone:
	case <-time.After(35 * time.Second):
		t.Fatal("auto-title goroutine did not finish")
	}
}

func TestBuildTitlePrompt(t *testing.T) {
	msgs := buildTitlePrompt("why did cost spike", "Because of model X")
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Fatalf("unexpected shape: %+v", msgs)
	}
	if !strings.Contains(msgs[1].Content, "why did cost spike") ||
		!strings.Contains(msgs[1].Content, "Because of model X") {
		t.Fatalf("prompt missing conversation content: %q", msgs[1].Content)
	}

	// Oversized inputs are bounded, not embedded whole.
	huge := strings.Repeat("z", 10000)
	msgs = buildTitlePrompt(huge, huge)
	if len(msgs[1].Content) > 2*autoTitleSnippetRunes+500 {
		t.Fatalf("prompt not bounded: %d bytes", len(msgs[1].Content))
	}

	// Assistant answer is optional (LLM failed mid-turn, tool-only turn).
	msgs = buildTitlePrompt("only a question", "")
	if strings.Contains(msgs[1].Content, "Sentry answered") {
		t.Fatalf("empty assistant answer must be omitted: %q", msgs[1].Content)
	}
}
