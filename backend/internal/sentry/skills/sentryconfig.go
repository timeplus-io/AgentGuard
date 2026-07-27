package skills

import (
	"context"
	"encoding/json"
)

// sentryConfig exposes Sentry's OWN runtime configuration (#233) — the
// redacted view of the agentguard_sentry_config chat/memory rows plus
// status, injected from the handlers package so redaction is the exact
// logic the HTTP API uses (api_key → api_key_set, never the value). This is
// the sanctioned path; run_sql deliberately denies the raw stream because
// its rows carry credentials. Registered ALWAYS (not behind
// agentguard_tools_enabled): self-introspection works even in
// generic-analyst mode.
type sentryConfig struct {
	fn func() (string, error)
}

// NewGetSentryConfig returns the read-only "get_sentry_config" skill.
func NewGetSentryConfig(fn func() (string, error)) Skill { return sentryConfig{fn: fn} }

func (sentryConfig) Name() string { return "get_sentry_config" }
func (sentryConfig) Description() string {
	return "Read your OWN runtime configuration (redacted): chat model settings, memory/embedding settings, and feature posture. api keys are never included — only whether one is set. Use this to answer any question about your own configuration."
}
func (sentryConfig) Params() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (sentryConfig) Access() AccessClass { return ReadOnly }

func (s sentryConfig) Execute(_ context.Context, _ *RunContext, _ json.RawMessage) (string, error) {
	if s.fn == nil {
		return "error: sentry config introspection is not wired in this context", nil
	}
	out, err := s.fn()
	if err != nil {
		return "error: " + err.Error(), nil
	}
	return out, nil
}
