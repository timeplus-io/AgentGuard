package timeplus

import "testing"

func TestValidateDLPConfigValue(t *testing.T) {
	// Non-entities keys: any valid JSON object passes.
	if err := ValidateDLPConfigValue(DLPCfgKeyEmbed, `{"base_url":"http://x","model":"m"}`); err != nil {
		t.Errorf("valid embed config rejected: %v", err)
	}
	if err := ValidateDLPConfigValue(DLPCfgKeyEmbed, `not json`); err == nil {
		t.Error("invalid JSON should be rejected")
	}
	// Entities: structural checks.
	good := `{"max_per_type":20,"entities":[{"type":"credit_card","kind":"regex","store":"masked","pattern":"\\d{16}"},{"type":"cn","kind":"dictionary","terms":["orion"],"store":"raw"}]}`
	if err := ValidateDLPConfigValue(DLPCfgKeyEntities, good); err != nil {
		t.Errorf("valid catalog rejected: %v", err)
	}
	bad := []string{
		`{"entities":[{"kind":"regex","pattern":"x"}]}`,                            // missing type
		`{"entities":[{"type":"t","kind":"regex"}]}`,                               // regex without pattern
		`{"entities":[{"type":"t","kind":"dictionary"}]}`,                          // dictionary without terms
		`{"entities":[{"type":"t","kind":"weird","pattern":"x"}]}`,                 // bad kind
		`{"entities":[{"type":"t","kind":"regex","pattern":"x","store":"plain"}]}`, // bad store
	}
	for _, b := range bad {
		if err := ValidateDLPConfigValue(DLPCfgKeyEntities, b); err == nil {
			t.Errorf("expected rejection for %s", b)
		}
	}
}

// LLM UDF rows: a present prompt must be a string; absent/string prompts pass.
func TestValidateDLPConfigPrompt(t *testing.T) {
	for _, key := range []string{DLPCfgKeyRisk, DLPCfgKeyUnderstand, DLPCfgKeyRerank, DLPCfgKeySummarize} {
		if err := ValidateDLPConfigValue(key, `{"model":"chat","prompt":"Classify: {message}"}`); err != nil {
			t.Errorf("%s: string prompt rejected: %v", key, err)
		}
		if err := ValidateDLPConfigValue(key, `{"model":"chat"}`); err != nil {
			t.Errorf("%s: absent prompt rejected: %v", key, err)
		}
		if err := ValidateDLPConfigValue(key, `{"prompt":42}`); err == nil {
			t.Errorf("%s: non-string prompt should be rejected", key)
		}
		if err := ValidateDLPConfigValue(key, `{"prompt":["a"]}`); err == nil {
			t.Errorf("%s: array prompt should be rejected", key)
		}
	}
	// Non-LLM keys are unaffected by the prompt check.
	if err := ValidateDLPConfigValue(DLPCfgKeyEmbed, `{"prompt":42}`); err != nil {
		t.Errorf("embed row should not run the prompt check: %v", err)
	}
}

func TestIsDLPConfigKey(t *testing.T) {
	if !IsDLPConfigKey(DLPCfgKeyRisk) {
		t.Error("dlp_risk should be a valid key")
	}
	if IsDLPConfigKey("nope") {
		t.Error("unknown key should be rejected")
	}
}
