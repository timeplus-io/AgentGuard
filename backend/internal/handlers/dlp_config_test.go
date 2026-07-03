package handlers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactAPIKey(t *testing.T) {
	red, had := redactAPIKey(`{"base_url":"http://x/v1","api_key":"sk-secret","model":"m"}`)
	if !had {
		t.Error("expected api_key_set true when a non-empty key is present")
	}
	var m map[string]any
	if err := json.Unmarshal(red, &m); err != nil {
		t.Fatalf("redacted value not JSON: %v", err)
	}
	if m["api_key"] != "" {
		t.Errorf("api_key not redacted: %v", m["api_key"])
	}
	if m["base_url"] != "http://x/v1" {
		t.Error("non-secret fields must be preserved")
	}

	// No api_key field (e.g. entities) → api_key_set false, value intact.
	_, had2 := redactAPIKey(`{"max_per_type":20,"entities":[]}`)
	if had2 {
		t.Error("entities config has no api_key; api_key_set must be false")
	}

	// Empty api_key → present but not "set".
	_, had3 := redactAPIKey(`{"api_key":""}`)
	if had3 {
		t.Error("empty api_key must report api_key_set false")
	}

	// Per-model api_key nested under models must also be redacted + counted.
	red4, had4 := redactAPIKey(`{"api_key":"","models":{"chat":{"model":"m","api_key":"sk-per-model"}}}`)
	if !had4 {
		t.Error("nested per-model api_key should set api_key_set true")
	}
	var m4 map[string]any
	if err := json.Unmarshal(red4, &m4); err != nil {
		t.Fatalf("redacted value not JSON: %v", err)
	}
	models := m4["models"].(map[string]any)
	chat := models["chat"].(map[string]any)
	if chat["api_key"] != "" {
		t.Errorf("nested api_key not redacted: %v", chat["api_key"])
	}
	if chat["model"] != "m" {
		t.Error("nested non-secret fields must be preserved")
	}
}

// GET must report the EFFECTIVE prompt: rows without one (pre-#197 seeds or a
// cleared field) get the built-in default filled in; a tuned prompt and non-LLM
// rows pass through untouched.
func TestFillDefaultPrompt(t *testing.T) {
	// Row without a prompt → default filled, other fields preserved.
	out := fillDefaultPrompt("dlp_summarize", json.RawMessage(`{"model":"summary","max_tokens":800}`))
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("filled value not JSON: %v", err)
	}
	p, _ := m["prompt"].(string)
	if p == "" || !containsAll(p, "TASK: AUDIT_SUMMARY", "{question}", "{evidence}") {
		t.Errorf("default summarize prompt not filled: %q", p)
	}
	if m["max_tokens"].(float64) != 800 {
		t.Error("existing fields must be preserved")
	}

	// Empty prompt → treated as absent (the UDF falls back too).
	out = fillDefaultPrompt("dlp_risk", json.RawMessage(`{"prompt":""}`))
	_ = json.Unmarshal(out, &m)
	if p, _ := m["prompt"].(string); p == "" {
		t.Error("empty prompt should be replaced by the default")
	}

	// Tuned prompt passes through untouched.
	out = fillDefaultPrompt("dlp_rerank", json.RawMessage(`{"prompt":"custom {items}"}`))
	_ = json.Unmarshal(out, &m)
	if m["prompt"] != "custom {items}" {
		t.Errorf("tuned prompt must not be overwritten: %v", m["prompt"])
	}

	// Non-LLM keys untouched.
	in := json.RawMessage(`{"model":"embedding","max_chars":8000}`)
	if string(fillDefaultPrompt("dlp_embed", in)) != string(in) {
		t.Error("non-LLM rows must pass through unchanged")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
