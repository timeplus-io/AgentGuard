package config

import "testing"

func TestRegistryCoversAllKeys(t *testing.T) {
	reg := Registry()
	if len(reg) == 0 {
		t.Fatal("registry is empty")
	}
	// Spot-check a representative key from each section.
	want := []string{
		"server.port", "timeplus.host", "timeplus.native-port",
		"timeplus.user", "timeplus.password", "agentguard.url",
		"catalog.dir", "holds.fail_policy", "dlp.enabled",
		"dlp.api_key", "dlp.embedding_dim", "sentry.enabled",
		"sentry.skill_dir", "sentry.chat_base_url", "sentry.chat_api_key",
		"sentry.chat_model", "sentry.chat_api_type", "sentry.memory_enabled", "sentry.embedding_base_url",
		"sentry.embedding_api_key", "sentry.embedding_model", "sentry.embedding_dim",
		"sentry.request_timeout_seconds", "sentry.max_output_tokens",
		"setup.complete",
	}
	for _, k := range want {
		if _, ok := MetaFor(k); !ok {
			t.Errorf("registry missing key %q", k)
		}
	}
}

func TestRegistryMetaInvariants(t *testing.T) {
	for _, m := range Registry() {
		if m.Key == "" || m.Section == "" || m.Label == "" {
			t.Errorf("key %q: empty Key/Section/Label", m.Key)
		}
		if m.Type == TypeEnum && len(m.EnumValues) == 0 {
			t.Errorf("key %q: enum with no EnumValues", m.Key)
		}
	}
	// Read-only & env-only invariants.
	if m, _ := MetaFor("setup.complete"); !m.ReadOnly {
		t.Error("setup.complete must be ReadOnly")
	}
	if m, _ := MetaFor("timeplus.password"); !m.ReadOnly || m.Type != TypeSecret {
		t.Error("timeplus.password must be ReadOnly secret")
	}
	if m, _ := MetaFor("dlp.api_key"); m.Type != TypeSecret || m.ReadOnly {
		t.Error("dlp.api_key must be an editable secret")
	}
	if m, _ := MetaFor("server.port"); !m.Restart {
		t.Error("server.port must be Restart")
	}
	// embedding_dim is auto-detected from the model at provision (probed via
	// dlp_embed), so it's read-only and no longer a re-provision-drift trigger.
	if m, _ := MetaFor("dlp.embedding_dim"); !m.ReadOnly || m.Reprovision {
		t.Error("dlp.embedding_dim must be ReadOnly and not Reprovision")
	}
}
