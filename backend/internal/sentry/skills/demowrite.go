package skills

import (
	"context"
	"encoding/json"
)

// demoWrite is a verification-only GatedWrite skill with NO side effects:
// it just echoes its args after surviving the approval flow. Registered
// only when the server runs with SENTRY_DEMO_WRITE=1 (see sentryRegistry) —
// it exists so operators can exercise the #221 approval plumbing end-to-end
// (card → approve/deny/timeout → audit replay) before the first real write
// tool ships (#222). Remove once #222 lands if desired.
type demoWrite struct{}

// NewDemoWrite returns the demo GatedWrite skill.
func NewDemoWrite() Skill { return demoWrite{} }

func (demoWrite) Name() string { return "demo_write" }
func (demoWrite) Description() string {
	return "Demo write action for testing the approval flow. Does nothing — it only echoes its arguments after human approval. Call it when the operator asks to test/verify write approvals."
}
func (demoWrite) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"note": map[string]any{"type": "string", "description": "any text to echo back"},
		},
	}
}
func (demoWrite) Access() AccessClass { return GatedWrite }
func (demoWrite) Execute(_ context.Context, _ *RunContext, args json.RawMessage) (string, error) {
	return "demo write executed (no side effects). args: " + string(args), nil
}
