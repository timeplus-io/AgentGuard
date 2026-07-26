package skills

import (
	"strings"
	"testing"
)

// TestValidateReadOnlySQLDeniedStreams: config streams carrying api_key
// secrets must not be readable via run_sql, in any casing/qualification,
// while ordinary agentguard streams stay readable.
func TestValidateReadOnlySQLDeniedStreams(t *testing.T) {
	rejected := []string{
		"SELECT value FROM table(agentguard_dlp_config)",
		"SELECT value FROM table(agentguard_sentry_config) WHERE key = 'chat'",
		"SELECT value FROM table(AGENTGUARD_SENTRY_CONFIG)",
		"SELECT value FROM table(default.agentguard_dlp_config)",
		"SELECT * FROM (SELECT value FROM table(agentguard_sentry_config))",
		"SELECT a.x FROM table(agentguard_cim_event) AS a JOIN table(agentguard_dlp_config) AS b ON a.x = b.key",
	}
	for _, sql := range rejected {
		if _, err := ValidateReadOnlySQL(sql, 100); err == nil {
			t.Errorf("expected credential-stream rejection, got accept: %s", sql)
		} else if !strings.Contains(err.Error(), "credentials") {
			t.Errorf("wrong error for %s: %v", sql, err)
		}
	}

	allowed := []string{
		"SELECT * FROM table(agentguard_cim_event) LIMIT 10",
		// A name merely containing a denied name as a prefix/suffix is fine.
		"SELECT * FROM table(agentguard_sentry_config_audit) LIMIT 10",
	}
	for _, sql := range allowed {
		if _, err := ValidateReadOnlySQL(sql, 100); err != nil {
			t.Errorf("expected accept, got %v: %s", err, sql)
		}
	}
}
