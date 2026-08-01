package notify

import (
	"encoding/json"
	"fmt"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/rules"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// TimeplusStore persists seen-state in the agentguard_notifications mutable
// stream. It re-reads cfg.Timeplus under config.Mu per call so hot-applied
// endpoint changes take effect without a restart. Point queries on the PK
// are cheap at threat-notification volume — no in-memory cache to keep
// coherent across replicas.
type TimeplusStore struct {
	Cfg *config.Config
}

func esc(s string) string { return rules.EscapeSQLString(s) }

func getStateSQL(e Event) string {
	return fmt.Sprintf(
		`SELECT confirmed, to_string(confirmed_at) AS confirmed_at FROM table(agentguard_notifications) WHERE agent_id = '%s' AND deployment_id = '%s' AND session_id = '%s' AND rule_id = '%s' LIMIT 1`,
		esc(e.AgentID), esc(e.DeploymentID), esc(e.SessionID), esc(e.RuleID))
}

// markNotifiedSQL writes the full row: cf_event from the event, plus
// confirmed=false so a re-notify after confirm returns the identity to the
// "pending" state (which absorbs burst duplicates). confirmed_at resets to
// the epoch — shouldNotify only reads it when confirmed is true.
func markNotifiedSQL(e Event) string {
	return fmt.Sprintf(
		`INSERT INTO agentguard_notifications (agent_id, deployment_id, session_id, rule_id, rule_name, severity, message, detected_at, notified_at, confirmed, confirmed_at) VALUES ('%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', now64(3), false, '1970-01-01 00:00:00.000')`,
		esc(e.AgentID), esc(e.DeploymentID), esc(e.SessionID), esc(e.RuleID),
		esc(e.RuleName), esc(e.Severity), esc(e.Message), esc(e.DetectedAt))
}

func (s *TimeplusStore) tpCfg() config.TimeplusConfig {
	config.Mu.RLock()
	defer config.Mu.RUnlock()
	return s.Cfg.Timeplus
}

func (s *TimeplusStore) Get(e Event) (*State, error) {
	tpCfg := s.tpCfg()
	rows, err := timeplus.Query(&tpCfg, getStateSQL(e))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	st := &State{}
	// Inline decode (bool or 0/1 json.Number): handlers' parseBool covers the
	// same shapes but sits behind an import cycle (handlers → notify), and its
	// semantics are tied to the native-driver type family. Extract a shared
	// helper only if more Proton-row decoding accumulates here (#267).
	switch v := rows[0]["confirmed"].(type) {
	case bool:
		st.Confirmed = v
	case json.Number: // Proton may serialize bool as 0/1
		st.Confirmed = v.String() != "0"
	}
	if v, ok := rows[0]["confirmed_at"].(string); ok {
		st.ConfirmedAt = v
	}
	return st, nil
}

func (s *TimeplusStore) MarkNotified(e Event) error {
	tpCfg := s.tpCfg()
	return timeplus.ExecHTTP(&tpCfg, markNotifiedSQL(e))
}
