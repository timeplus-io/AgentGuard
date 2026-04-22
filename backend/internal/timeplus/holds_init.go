package timeplus

import (
	"context"
	"fmt"
	"log"
	"sync"

	proton "github.com/timeplus-io/proton-go-driver/v2"

	"github.com/timeplus/agentguard/internal/config"
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

// migrateOnce ensures MigrateHoldsSchema runs at most once per server lifetime.
var migrateOnce sync.Once

// MigrateHoldsSchema runs the holds-related schema additions against an existing
// AgentGuard deployment that was provisioned before the holds feature existed.
//
// The full Init() flow only runs once via the /api/setup/provision wizard. To
// pick up new schema bits without forcing a re-provision, the server calls this
// after the FIRST successful login (when Timeplus credentials become available
// — they are deliberately not persisted to the config file). The sync.Once
// guarantees a single attempt per process, so subsequent logins are no-op.
//
// All operations are idempotent (CREATE ... IF NOT EXISTS, ADD COLUMN IF NOT
// EXISTS), so it is safe to call on fresh installs too.
//
// Failures are logged but do not abort startup — the holds endpoints will
// return 503 until the schema is completed, but the rest of the server stays up.
func MigrateHoldsSchema(timeplusCfg config.TimeplusConfig) {
	migrateOnce.Do(func() {
		conn, err := connectOnce(&timeplusCfg)
		if err != nil {
			log.Printf("timeplus migrate: connect failed: %v (skipping holds migration)", err)
			return
		}
		defer conn.Close()

		ctx := context.Background()
		emit := func(status, msg string) { log.Printf("[migrate:%s] %s", status, msg) }

		if err := SetupHoldsStorage(ctx, conn, emit); err != nil {
			log.Printf("timeplus migrate: holds storage setup failed: %v (continuing — /api/holds will return 503)", err)
			return
		}
		if err := AbandonStalePendingHolds(ctx, conn, emit); err != nil {
			log.Printf("timeplus migrate: abandon stale pending holds: %v (continuing)", err)
		}
	})
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
FROM mv_holds_current
WHERE status = 'pending'`
	if err := conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("abandon stale pending holds: %w", err)
	}
	emit("ok", "  ✓ abandoned stale pending holds")
	return nil
}
