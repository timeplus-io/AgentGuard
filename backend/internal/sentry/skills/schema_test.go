package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

// TestSchemaCardStaticContent is the brief's binding test: the static card
// must mention the streams an LLM most needs to ground SQL on.
func TestSchemaCardStaticContent(t *testing.T) {
	card := schemaCardStatic()
	for _, must := range []string{"agentguard_cim_event", "agentguard_security_events",
		"agentguard_dlp_enriched", "agentguard_hook_events", "agentguard_cim_metrics"} {
		if !strings.Contains(card, must) {
			t.Fatalf("schema card missing %s", must)
		}
	}
}

// TestSchemaCardStaticContentAllKnownStreams broadens the check to every
// stream this task's brief calls out by name, so a future edit to
// knownStreamDocs can't silently drop one of the operational/Sentry streams
// while still passing the narrower brief test above.
func TestSchemaCardStaticContentAllKnownStreams(t *testing.T) {
	card := schemaCardStatic()
	for _, must := range []string{
		"agentguard_cim_event", "agentguard_cim_metrics", "agentguard_security_events",
		"agentguard_dlp_enriched", "agentguard_hook_events", "agentguard_threats",
		"agentguard_holds", "agentguard_rules", "agentguard_cost_budgets",
		"agentguard_sentry_conversations", "agentguard_sentry_messages",
	} {
		if !strings.Contains(card, must) {
			t.Fatalf("schema card missing %s", must)
		}
	}
}

func TestListStreamsSkillMetadata(t *testing.T) {
	s := NewListStreams()
	if s.Name() != "list_streams" {
		t.Fatalf("Name() = %q, want list_streams", s.Name())
	}
	if s.Access() != ReadOnly {
		t.Fatalf("Access() = %v, want ReadOnly", s.Access())
	}
}

func TestDescribeStreamSkillMetadata(t *testing.T) {
	s := NewDescribeStream()
	if s.Name() != "describe_stream" {
		t.Fatalf("Name() = %q, want describe_stream", s.Name())
	}
	if s.Access() != ReadOnly {
		t.Fatalf("Access() = %v, want ReadOnly", s.Access())
	}
	params := s.Params()
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Params() properties missing or wrong type: %#v", params)
	}
	if _, ok := props["name"]; !ok {
		t.Fatalf("Params() missing name property: %#v", params)
	}
}

func TestDescribeStreamMissingArg(t *testing.T) {
	s := NewDescribeStream()
	rc := &RunContext{Cfg: &config.Config{}, ResultMaxBytes: 4096}
	out, err := s.Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("want error: prefix, got %q", out)
	}
}

// TestDescribeStreamRejectsInvalidName is the brief's name-validation reject
// test: describe_stream must never let a name containing anything outside
// [a-zA-Z_]\w* reach an interpolated SQL string, so this must fail before
// any query is attempted (no Timeplus needed to observe the rejection).
func TestDescribeStreamRejectsInvalidName(t *testing.T) {
	cases := []string{
		"agentguard_cim_event; DROP STREAM x",
		"agentguard_cim_event'--",
		"agentguard cim event",
		"1agentguard",
		"agentguard.cim_event",
		"",
		"   ",
	}
	s := NewDescribeStream()
	rc := &RunContext{Cfg: &config.Config{}, ResultMaxBytes: 4096}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"name": name})
			out, err := s.Execute(context.Background(), rc, args)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			if !strings.HasPrefix(out, "error:") {
				t.Fatalf("want error: prefix for invalid name %q, got %q", name, out)
			}
		})
	}
}

func TestDescribeStreamAcceptsValidNameShape(t *testing.T) {
	// A syntactically valid name still errors out at Execute time (no live
	// Timeplus in this test), but that error must come from the query
	// itself, not from the name-validation branch — i.e. it must not be
	// the "invalid stream name" message.
	s := NewDescribeStream()
	rc := &RunContext{Cfg: &config.Config{Timeplus: config.TimeplusConfig{Host: "127.0.0.1", Port: 1}}, ResultMaxBytes: 4096}
	args, _ := json.Marshal(map[string]string{"name": "agentguard_cim_event"})
	out, err := s.Execute(context.Background(), rc, args)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.HasPrefix(out, "error:") {
		t.Fatalf("expected a query error against an unreachable host, got %q", out)
	}
	if strings.Contains(out, "invalid stream name") {
		t.Fatalf("valid name shape must not be rejected by name validation, got %q", out)
	}
}
