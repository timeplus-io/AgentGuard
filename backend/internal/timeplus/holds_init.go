package timeplus

import (
	"context"
	"fmt"

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

const ddlHoldsCurrentMV = `
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_holds_current AS
SELECT
  hold_id,
  arg_max(deployment_id, updated_at) AS deployment_id,
  arg_max(agent_id,      updated_at) AS agent_id,
  arg_max(session_id,    updated_at) AS session_id,
  arg_max(run_id,        updated_at) AS run_id,
  arg_max(tool_name,     updated_at) AS tool_name,
  arg_max(rule_id,       updated_at) AS rule_id,
  arg_max(severity,      updated_at) AS severity,
  arg_max(args_summary,  updated_at) AS args_summary,
  arg_max(message,       updated_at) AS message,
  arg_max(created_at,    updated_at) AS created_at,
  arg_max(status,        updated_at) AS status,
  arg_max(decided_by,    updated_at) AS decided_by,
  arg_max(decided_at,    updated_at) AS decided_at,
  arg_max(decided_reason, updated_at) AS decided_reason
FROM agentguard_holds
GROUP BY hold_id`

const ddlAlterRulesAddBlockPolicy = `
ALTER STREAM agentguard_rules ADD COLUMN IF NOT EXISTS block_policy string DEFAULT 'log_only'`

// SetupHoldsStorage creates the holds stream, the current-state MV, and
// adds the block_policy column to agentguard_rules. All operations are
// idempotent. Errors are logged and returned but the caller may choose to
// continue (matching the existing init.go pattern).
func SetupHoldsStorage(ctx context.Context, conn proton.Conn, emit func(status, msg string)) error {
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_holds", ddlHoldsStream, emit); err != nil {
		return err
	}
	if err := execIfNotExists(ctx, conn, "view", "mv_holds_current", ddlHoldsCurrentMV, emit); err != nil {
		return err
	}
	if err := conn.Exec(ctx, ddlAlterRulesAddBlockPolicy); err != nil {
		return fmt.Errorf("alter agentguard_rules add block_policy: %w", err)
	}
	emit("ok", "  ✓ agentguard_rules.block_policy column")
	return nil
}

// AbandonStalePendingHolds writes status='abandoned' for any holds rows that
// are still 'pending' at server startup. These are leftovers from a prior
// process that crashed mid-hold; the in-memory channels are gone, so the UI
// would otherwise show stale entries forever.
func AbandonStalePendingHolds(ctx context.Context, conn proton.Conn, emit func(status, msg string)) error {
	sql := `
INSERT INTO agentguard_holds
  (hold_id, deployment_id, agent_id, session_id, run_id, tool_name, rule_id,
   severity, args_summary, message, created_at, status,
   decided_by, decided_at, decided_reason, updated_at)
SELECT
  hold_id, deployment_id, agent_id, session_id, run_id, tool_name, rule_id,
  severity, args_summary, message, created_at,
  'abandoned' AS status,
  'startup'   AS decided_by,
  now64(3)    AS decided_at,
  'orphaned by backend restart' AS decided_reason,
  now64(3)    AS updated_at
FROM table(mv_holds_current)
WHERE status = 'pending'`
	if err := conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("abandon stale pending holds: %w", err)
	}
	emit("ok", "  ✓ abandoned stale pending holds")
	return nil
}
