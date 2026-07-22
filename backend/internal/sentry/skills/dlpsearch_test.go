package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

// TestDLPSearchMetadata checks the tool's static shape: name, access class,
// and the required "question" param.
func TestDLPSearchMetadata(t *testing.T) {
	s := NewDLPSearch()
	if s.Name() != "dlp_search" {
		t.Fatalf("Name() = %q, want dlp_search", s.Name())
	}
	if s.Access() != ReadOnly {
		t.Fatalf("Access() = %v, want ReadOnly", s.Access())
	}
	params := s.Params()
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Params() properties missing or wrong type: %#v", params)
	}
	if _, ok := props["question"]; !ok {
		t.Fatalf("Params() missing question property: %#v", params)
	}
	if _, ok := props["top_n"]; !ok {
		t.Fatalf("Params() missing top_n property: %#v", params)
	}
}

// TestDLPSearchDisabled is the brief's binding guard test: when DLP is
// disabled in config, dlp_search must never attempt a Timeplus query — it
// should short-circuit and tell the agent to use other streams instead.
func TestDLPSearchDisabled(t *testing.T) {
	rc := &RunContext{Cfg: &config.Config{}} // DLP.Enabled=false
	args, _ := json.Marshal(map[string]any{"question": "leaks?"})
	out, err := NewDLPSearch().Execute(context.Background(), rc, args)
	if err != nil || !strings.Contains(out, "DLP is not enabled") {
		t.Fatalf("got %q err=%v", out, err)
	}
}

// TestDLPSearchMissingQuestion covers args parsing: a missing/blank
// "question" must produce an "error: ..." result with a nil Go error, per
// the skills package convention (see runsql.go/schema.go).
func TestDLPSearchMissingQuestion(t *testing.T) {
	cfg := &config.Config{}
	cfg.DLP.Enabled = true
	rc := &RunContext{Cfg: cfg, ResultMaxBytes: 4096}

	cases := []json.RawMessage{
		json.RawMessage(`{}`),
		json.RawMessage(`{"question": ""}`),
		json.RawMessage(`{"question": "   "}`),
		json.RawMessage(`not json`),
	}
	for _, args := range cases {
		out, err := NewDLPSearch().Execute(context.Background(), rc, args)
		if err != nil {
			t.Fatalf("unexpected Go error for args %s: %v", args, err)
		}
		if !strings.HasPrefix(out, "error:") {
			t.Fatalf("args %s: want error: prefix, got %q", args, out)
		}
	}
}
