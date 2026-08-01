package handlers

import (
	"encoding/json"
	"testing"
)

// TestOpenAPISpecIsValidJSON guards against the embedded OpenAPI spec (served
// by APIDocs / GET /docs/openapi.json) drifting into invalid JSON as it's
// hand-edited — a broken backtick-delimited Go string const would otherwise
// only surface at request time.
func TestOpenAPISpecIsValidJSON(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(openAPISpec), &doc); err != nil {
		t.Fatalf("openAPISpec is not valid JSON: %v", err)
	}
}
