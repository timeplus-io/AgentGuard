package timeplus

import (
	"fmt"
	"log"
	"strings"

	"github.com/timeplus/agentshield/internal/config"
)

// MVName returns the materialized view name for a rule ID.
// e.g. "rp-001" → "mv_rule_rp001"
func MVName(ruleID string) string {
	return "mv_rule_" + strings.ReplaceAll(ruleID, "-", "")
}

// CreateRuleMV creates a new materialized view for the rule.
// querySQLBody is the full SELECT ... FROM agentshield_hook_events WHERE ... body.
func CreateRuleMV(cfg *config.TimeplusConfig, ruleID, querySQLBody string) error {
	mvName := MVName(ruleID)
	ddl := fmt.Sprintf(
		"CREATE MATERIALIZED VIEW IF NOT EXISTS %s INTO agentshield_security_events AS %s",
		mvName, querySQLBody,
	)
	if err := ExecHTTP(cfg, ddl); err != nil {
		return fmt.Errorf("create MV %s: %w", mvName, err)
	}
	log.Printf("rules: created MV %s", mvName)
	return nil
}

// PauseRuleMV permanently pauses the MV (survives Proton restarts).
func PauseRuleMV(cfg *config.TimeplusConfig, ruleID string) error {
	mvName := MVName(ruleID)
	sql := fmt.Sprintf("SYSTEM PAUSE MATERIALIZED VIEW %s PERMANENT", mvName)
	if err := ExecHTTP(cfg, sql); err != nil {
		return fmt.Errorf("pause MV %s: %w", mvName, err)
	}
	log.Printf("rules: paused MV %s", mvName)
	return nil
}

// ResumeRuleMV permanently resumes the MV (survives Proton restarts).
func ResumeRuleMV(cfg *config.TimeplusConfig, ruleID string) error {
	mvName := MVName(ruleID)
	sql := fmt.Sprintf("SYSTEM RESUME MATERIALIZED VIEW %s PERMANENT", mvName)
	if err := ExecHTTP(cfg, sql); err != nil {
		return fmt.Errorf("resume MV %s: %w", mvName, err)
	}
	log.Printf("rules: resumed MV %s", mvName)
	return nil
}

// DropRuleMV drops the MV entirely (used on upgrade and uninstall).
func DropRuleMV(cfg *config.TimeplusConfig, ruleID string) error {
	mvName := MVName(ruleID)
	sql := fmt.Sprintf("DROP VIEW IF EXISTS %s", mvName)
	if err := ExecHTTP(cfg, sql); err != nil {
		return fmt.Errorf("drop MV %s: %w", mvName, err)
	}
	log.Printf("rules: dropped MV %s", mvName)
	return nil
}
