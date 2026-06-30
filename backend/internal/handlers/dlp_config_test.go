package handlers

import (
	"encoding/json"
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
}
