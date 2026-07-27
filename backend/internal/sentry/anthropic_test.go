package sentry

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestNormalizeAnthropicBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://bedrock-mantle.us-west-2.api.aws/v1":  "https://bedrock-mantle.us-west-2.api.aws",
		"https://bedrock-mantle.us-west-2.api.aws/v1/": "https://bedrock-mantle.us-west-2.api.aws",
		"https://api.anthropic.com":                    "https://api.anthropic.com",
		"https://x/v1/foo":                             "https://x/v1/foo", // only a TRAILING /v1 is stripped
	}
	for in, want := range cases {
		if got := normalizeAnthropicBaseURL(in); got != want {
			t.Fatalf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnthropicToolArgsRoundTripToJSONString(t *testing.T) {
	// A tool_use input object must come back as a compact JSON string on ChatResult.
	in := map[string]any{"a": 1.0, "b": "x"}
	got := anthropicInputToArgs(in) // helper: object -> JSON string
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("Args not valid JSON: %v (%q)", err, got)
	}
	if back["a"] != 1.0 || back["b"] != "x" {
		t.Fatalf("round-trip lost data: %v", back)
	}
}

func TestArgsToAnthropicInputParsesJSONString(t *testing.T) {
	// An assistant ToolCall.Args (JSON string) must become an object for tool_use.
	obj := argsToAnthropicInput(`{"k":"v"}`) // helper: JSON string -> map
	if obj["k"] != "v" {
		t.Fatalf("parse failed: %v", obj)
	}
	// Invalid/empty JSON must yield an empty object, not panic.
	if m := argsToAnthropicInput(""); len(m) != 0 {
		t.Fatalf("empty args should be empty object, got %v", m)
	}
	// Malformed JSON must also yield an empty object, not panic.
	if m := argsToAnthropicInput("{invalid"); len(m) != 0 {
		t.Fatalf("malformed JSON should be empty object, got %v", m)
	}
}

func TestAnthropicMessagesGroupsConsecutiveToolResults(t *testing.T) {
	msgs := []Msg{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "a", Name: "t1", Args: `{}`}, {ID: "b", Name: "t2", Args: `{}`}}},
		{Role: "tool", ToolCallID: "a", Content: "r1"},
		{Role: "tool", ToolCallID: "b", Content: "r2"},
		{Role: "user", Content: "next"},
	}
	system, amsgs := anthropicMessagesFromMsgs(msgs)
	if system != "sys" {
		t.Fatalf("system = %q, want sys", system)
	}
	// Expect: user(hi), assistant(2 tool_use), user(2 tool_result), user(next) = 4 messages.
	if len(amsgs) != 4 {
		t.Fatalf("got %d messages, want 4: %+v", len(amsgs), amsgs)
	}
	// The 3rd message must be a single user turn carrying BOTH tool_result blocks.
	grouped := amsgs[2]
	if grouped.Role != anthropic.MessageParamRoleUser {
		t.Fatalf("grouped tool-results message role = %v, want user", grouped.Role)
	}
	if len(grouped.Content) != 2 {
		t.Fatalf("grouped tool-results block count = %d, want 2 (both results in ONE user message)", len(grouped.Content))
	}
}

func TestNewLLMFactory(t *testing.T) {
	if _, ok := NewLLM(ChatCfg{APIType: "anthropic"}).(anthropicLLM); !ok {
		t.Fatal("api_type anthropic must select anthropicLLM")
	}
	if _, ok := NewLLM(ChatCfg{APIType: "openai"}).(openaiLLM); !ok {
		t.Fatal("api_type openai must select openaiLLM")
	}
	if _, ok := NewLLM(ChatCfg{APIType: ""}).(openaiLLM); !ok {
		t.Fatal("empty api_type must default to openaiLLM")
	}
}

// TestAnthropicMessagesReplayThinkingBlocksFirst: assistant thinking blocks
// must be echoed back verbatim and BEFORE text/tool_use blocks — the API
// requires a tool_use turn to retain its reasoning when the loop continues.
func TestAnthropicMessagesReplayThinkingBlocksFirst(t *testing.T) {
	msgs := []Msg{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "using a tool",
			Thinking:  []ThinkingBlock{{Text: "let me check", Signature: "sig1"}},
			ToolCalls: []ToolCall{{ID: "t1", Name: "run_sql", Args: "{}"}}},
		{Role: "tool", ToolCallID: "t1", Content: "rows"},
	}
	_, amsgs := anthropicMessagesFromMsgs(msgs)
	if len(amsgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(amsgs))
	}
	assistant := amsgs[1]
	if len(assistant.Content) != 3 {
		t.Fatalf("assistant blocks = %d, want 3 (thinking, text, tool_use)", len(assistant.Content))
	}
	tb := assistant.Content[0].OfThinking
	if tb == nil || tb.Thinking != "let me check" || tb.Signature != "sig1" {
		t.Fatalf("first block must be the verbatim thinking block, got %+v", assistant.Content[0])
	}
	if assistant.Content[1].OfText == nil || assistant.Content[2].OfToolUse == nil {
		t.Fatal("thinking must precede text and tool_use")
	}
}
