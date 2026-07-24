package sentry

import (
	"encoding/json"
	"testing"
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
