package sentry

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/sentry/skills"
)

func selfCtxFixture() (ChatCfg, *config.Config) {
	cc := ChatCfg{
		Model: "anthropic.claude-opus-4-8", APIType: "anthropic",
		APIKey: "sk-SECRET-canary", BaseURL: "https://secret-endpoint.example/anthropic",
		Thinking: "adaptive", Effort: "xhigh",
	}
	cc.applyDefaults()
	cfg := &config.Config{}
	cfg.Timeplus.Host = "timeplus.internal"
	return cc, cfg
}

func TestSelfContextContents(t *testing.T) {
	cc, cfg := selfCtxFixture()
	out := SelfContext(cc, MemoryCfg{Configured: true, Enabled: true, Model: "nomic-embed-text", EmbeddingDim: 768}, cfg, time.Date(2026, 7, 27, 23, 59, 0, 0, time.UTC))

	for _, want := range []string{
		`"anthropic.claude-opus-4-8"`,
		"Anthropic Messages API",
		"Adaptive thinking is enabled",
		`Reasoning effort is set to "xhigh"`,
		"12 tool iterations",       // default MaxIterations
		"32000-token",              // default TurnTokenBudget
		`"timeplus.internal"`,
		"2026-07-27",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("self-context missing %q:\n%s", want, out)
		}
	}

	// OpenAI dialect variant: no anthropic-only lines.
	cc2 := ChatCfg{Model: "gpt-x", APIType: "openai", Thinking: "adaptive", Effort: "high"}
	cc2.applyDefaults()
	out2 := SelfContext(cc2, MemoryCfg{}, cfg, time.Now())
	if !strings.Contains(out2, "OpenAI-compatible") {
		t.Errorf("openai dialect missing:\n%s", out2)
	}
	if strings.Contains(out2, "Adaptive thinking") || strings.Contains(out2, "Reasoning effort") {
		t.Errorf("anthropic-only lines must not render for openai:\n%s", out2)
	}
}

// TestSelfContextNeverLeaksSecrets: the endpoint and credentials must never
// be exposable through prompt extraction.
func TestSelfContextNeverLeaksSecrets(t *testing.T) {
	cc, cfg := selfCtxFixture()
	out := SelfContext(cc, MemoryCfg{Configured: true, Model: "nomic-embed-text"}, cfg, time.Now())
	if strings.Contains(out, "sk-SECRET-canary") || strings.Contains(out, "secret-endpoint.example") {
		t.Fatalf("self-context leaked credentials:\n%s", out)
	}
}

// TestLoopSubstitutesSelfContext: the {self_context} placeholder in the
// system prompt reaches the model expanded.
func TestLoopSubstitutesSelfContext(t *testing.T) {
	llm := &fakeLLM{script: []ChatResult{{Content: "final"}}}
	l := &Loop{LLM: llm, Reg: skills.NewRegistry(fakeEchoSkill{}), Store: &Store{Disabled: true}, Cfg: testCfg()}

	cc := testChatCfg()
	cc.Model = "test-model-id"
	cc.SystemPrompt = "You are Sentry.\nAbout you:\n{self_context}\n{schema_card}"
	if err := l.Run(context.Background(), cc, testConv(), "hi", func(Event) {}); err != nil {
		t.Fatal(err)
	}
	system := llm.gotMsgs[0][0]
	if system.Role != "system" {
		t.Fatalf("first message role = %s", system.Role)
	}
	if strings.Contains(system.Content, "{self_context}") {
		t.Fatal("placeholder not substituted")
	}
	if !strings.Contains(system.Content, `"test-model-id"`) {
		t.Fatalf("substituted self-context missing model id:\n%.300s", system.Content)
	}
}

func TestSelfContextMemoryLine(t *testing.T) {
	cc, cfg := selfCtxFixture()
	cases := []struct {
		mem  MemoryCfg
		want string
	}{
		{MemoryCfg{Configured: true, Enabled: true, Model: "nomic-embed-text", EmbeddingDim: 768}, `enabled and configured (embedding model "nomic-embed-text", 768 dims)`},
		{MemoryCfg{Configured: true, Model: "nomic-embed-text"}, "disabled and not yet active"},
		{MemoryCfg{}, "not configured"},
	}
	for _, c := range cases {
		out := SelfContext(cc, c.mem, cfg, time.Now())
		if !strings.Contains(out, c.want) {
			t.Errorf("mem=%+v: missing %q in:\n%s", c.mem, c.want, out)
		}
		if !strings.Contains(out, "NOT yet active") && c.mem.Enabled {
			t.Errorf("enabled memory must state it is not yet active")
		}
	}
}
