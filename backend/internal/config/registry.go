package config

// KeyType classifies a config key for UI rendering and value coercion.
type KeyType string

const (
	TypeString KeyType = "string"
	TypeInt    KeyType = "int"
	TypeBool   KeyType = "bool"
	TypeSecret KeyType = "secret"
	TypeEnum   KeyType = "enum"
)

// KeyMeta declares everything the API and UI need to know about one config key.
type KeyMeta struct {
	Key         string // viper dotted key, e.g. "dlp.api_key"
	Section     string // section id, e.g. "dlp"
	Label       string // human label
	Description string // help text
	Type        KeyType
	EnumValues  []string // for TypeEnum
	EnvVar      string   // overriding env var name, "" if none
	FlagName    string   // overriding CLI flag name, "" if none
	Restart     bool     // change takes effect only after server restart
	Reprovision bool     // change needs DLP re-provision to take effect
	ReadOnly    bool     // never editable from the UI
}

// SectionMeta gives sections a stable display order and title.
type SectionMeta struct {
	ID    string
	Title string
}

// Sections returns config sections in display order.
func Sections() []SectionMeta {
	return []SectionMeta{
		{"server", "Server"},
		{"timeplus", "Timeplus Connection"},
		{"agentguard", "AgentGuard"},
		{"catalog", "Catalog"},
		{"holds", "Approval Holds"},
		{"dlp", "Semantic DLP"},
		{"setup", "Setup"},
	}
}

// Registry returns every config key in display order.
func Registry() []KeyMeta {
	return []KeyMeta{
		// ── server ──
		{Key: "server.port", Section: "server", Label: "HTTP port",
			Description: "Port the AgentGuard server listens on.",
			Type:        TypeInt, EnvVar: "SERVER_PORT", FlagName: "port", Restart: true},

		// ── timeplus ──
		{Key: "timeplus.host", Section: "timeplus", Label: "Host",
			Description: "Timeplus Enterprise hostname.",
			Type:        TypeString, EnvVar: "TIMEPLUS_HOST", FlagName: "timeplus-host"},
		{Key: "timeplus.port", Section: "timeplus", Label: "HTTP port",
			Description: "Timeplus HTTP (Proton) port.",
			Type:        TypeInt, EnvVar: "TIMEPLUS_PORT", FlagName: "timeplus-port"},
		{Key: "timeplus.native-port", Section: "timeplus", Label: "Native port",
			Description: "Timeplus native TCP port.",
			Type:        TypeInt, EnvVar: "TIMEPLUS_NATIVE_PORT", FlagName: "timeplus-native-port"},
		{Key: "timeplus.user", Section: "timeplus", Label: "Service user",
			Description: "Service account username. Set via the TIMEPLUS_USER environment variable only.",
			Type:        TypeString, EnvVar: "TIMEPLUS_USER", ReadOnly: true},
		{Key: "timeplus.password", Section: "timeplus", Label: "Service password",
			Description: "Service account password. Set via the TIMEPLUS_PASSWORD environment variable only.",
			Type:        TypeSecret, EnvVar: "TIMEPLUS_PASSWORD", ReadOnly: true},

		// ── agentguard ──
		{Key: "agentguard.url", Section: "agentguard", Label: "Webhook base URL",
			Description: "URL Timeplus uses to call back into AgentGuard (alerts, holds).",
			Type:        TypeString, EnvVar: "AGENTGUARD_URL", FlagName: "agentguard-url"},

		// ── catalog ──
		{Key: "catalog.dir", Section: "catalog", Label: "Catalog directory",
			Description: "Filesystem directory the rule catalog is loaded from.",
			Type:        TypeString, EnvVar: "CATALOG_DIR", FlagName: "catalog-dir", Restart: true},

		// ── holds ──
		{Key: "holds.mv_wait_ms", Section: "holds", Label: "MV wait (ms)",
			Description: "Milliseconds to wait for a rule MV to evaluate before deciding a hold.",
			Type:        TypeInt, EnvVar: "AGENTGUARD_HOLD_MV_WAIT_MS"},
		{Key: "holds.timeout_seconds", Section: "holds", Label: "Hold timeout (s)",
			Description: "Seconds a hold waits for a human decision before failing (max 540).",
			Type:        TypeInt, EnvVar: "AGENTGUARD_HOLD_TIMEOUT_SECONDS"},
		{Key: "holds.fail_policy", Section: "holds", Label: "Fail policy",
			Description: "What to do when a hold times out or the server shuts down.",
			Type:        TypeEnum, EnumValues: []string{"deny", "allow"}, EnvVar: "AGENTGUARD_HOLDS_FAIL_POLICY"},
		{Key: "holds.disabled", Section: "holds", Label: "Disable holds",
			Description: "When true, no PreToolUse holds are created.",
			Type:        TypeBool, EnvVar: "AGENTGUARD_HOLDS_DISABLED"},

		// ── dlp ──
		{Key: "dlp.enabled", Section: "dlp", Label: "Enable Semantic DLP",
			Description: "Turn on the Semantic DLP enrichment + query pipeline.",
			Type:        TypeBool, EnvVar: "DLP_ENABLED"},
		{Key: "dlp.base_url", Section: "dlp", Label: "LLM base URL",
			Description: "OpenAI-compatible base URL for DLP embedding/risk/summary models.",
			Type:        TypeString, EnvVar: "DLP_BASE_URL"},
		{Key: "dlp.api_key", Section: "dlp", Label: "API key",
			Description: "Bearer key for the DLP LLM endpoint. Write-only; never displayed.",
			Type:        TypeSecret, EnvVar: "DLP_API_KEY"},
		{Key: "dlp.embedding_model", Section: "dlp", Label: "Embedding model",
			Description: "Model used to embed content. Baked into UDFs — change needs re-provision.",
			Type:        TypeString, EnvVar: "DLP_EMBEDDING_MODEL", Reprovision: true},
		{Key: "dlp.embedding_dim", Section: "dlp", Label: "Embedding dimension",
			Description: "Auto-detected from the embedding model at provision (probed via dlp_embed). Read-only; the configured value is only a fallback used when the endpoint is unreachable.",
			Type:        TypeInt, EnvVar: "DLP_EMBEDDING_DIM", ReadOnly: true},
		{Key: "dlp.risk_model", Section: "dlp", Label: "Risk model",
			Description: "Model used for risk classification. Baked into UDFs — change needs re-provision.",
			Type:        TypeString, EnvVar: "DLP_RISK_MODEL", Reprovision: true},
		{Key: "dlp.summary_model", Section: "dlp", Label: "Summary model",
			Description: "Model used for query summarization. Baked into UDFs — change needs re-provision.",
			Type:        TypeString, EnvVar: "DLP_SUMMARY_MODEL", Reprovision: true},
		{Key: "dlp.request_timeout_seconds", Section: "dlp", Label: "Model request timeout (s)",
			Description: "Per-call timeout for DLP model requests. 5–55 (must stay under the 60s query client). Baked into UDFs — change needs re-provision.",
			Type:        TypeInt, EnvVar: "DLP_REQUEST_TIMEOUT_SECONDS", Reprovision: true},
		{Key: "dlp.max_output_tokens", Section: "dlp", Label: "Max output tokens",
			Description: "Token budget for risk/understand/rerank model calls (summary uses at least 800). Raise for reasoning models. Baked into UDFs — change needs re-provision.",
			Type:        TypeInt, EnvVar: "DLP_MAX_OUTPUT_TOKENS", Reprovision: true},

		// ── setup ──
		{Key: "setup.complete", Section: "setup", Label: "Setup complete",
			Description: "Whether the setup wizard has been run.",
			Type:        TypeBool, EnvVar: "SETUP_COMPLETE", ReadOnly: true},
	}
}

// MetaFor returns the KeyMeta for a dotted key.
func MetaFor(key string) (KeyMeta, bool) {
	for _, m := range Registry() {
		if m.Key == key {
			return m, true
		}
	}
	return KeyMeta{}, false
}
