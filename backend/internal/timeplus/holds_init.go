package timeplus

import (
	"context"

	proton "github.com/timeplus-io/proton-go-driver/v2"
)

const ddlHoldsStream = `
CREATE STREAM IF NOT EXISTS agentguard_holds (
  hold_id          string,
  deployment_id    string,
  agent_id         string,
  session_id       string,
  run_id           string,
  tool_name        string,
  rule_id          string,
  severity         string,
  args_summary     string,
  message          string,
  created_at       datetime64(3),
  status           string,
  decided_by       string,
  decided_at       datetime64(3),
  decided_reason   string,
  updated_at       datetime64(3)
)`

// SetupHoldsStorage creates the holds stream. Latest-state-per-hold is computed
// inline at query time via `arg_max(field, updated_at) GROUP BY hold_id` against
// the raw stream — matching how agentguard_rules and other mutation logs are
// queried. We deliberately do NOT create a stateful MV here: a streaming MV with
// GROUP BY emits one snapshot per source row (not one row per key), so a simple
// `WHERE status='pending'` against the MV would pick up stale snapshots from
// before a decision was written.
func SetupHoldsStorage(ctx context.Context, conn proton.Conn, emit func(status, msg string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_holds", ddlHoldsStream, emit)
}
