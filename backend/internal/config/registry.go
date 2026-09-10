package config

// KeyType classifies a config key for UI rendering and value coercion.
type KeyType string

const (
	TypeString KeyType = "string"
	TypeInt    KeyType = "int"
	TypeBool   KeyType = "bool"
	TypeSecret KeyType = "secret"
	TypeEnum   KeyType = "enum"
	// TypeList is a multi-value string list (e.g. an allowlist). The set of
	// selectable values is not fixed in the registry; the API attaches dynamic
	// options to the key view where applicable (see settings.go).
	TypeList KeyType = "list"
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
		{"catalog", "Catalog"},
		{"holds", "Approval Holds"},
		{"dlp", "Semantic DLP"},
		{"sentry", "Sentry Agent"},
		{"rules", "Rules"},
		{"ui", "User Interface"},
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
		{Key: "timeplus.database", Section: "timeplus", Label: "Database",
			Description: "Timeplus database holding the AgentGuard streams. Use \"default\" for a wizard-provisioned deployment, or the app's db_name (e.g. \"ag\") when the pipeline was installed as a Timeplus app.",
			Type:        TypeString, EnvVar: "TIMEPLUS_DATABASE", FlagName: "timeplus-database"},
		{Key: "timeplus.user", Section: "timeplus", Label: "Service user",
			Description: "Service account username. Set via the TIMEPLUS_USER environment variable only.",
			Type:        TypeString, EnvVar: "TIMEPLUS_USER", ReadOnly: true},
		{Key: "timeplus.password", Section: "timeplus", Label: "Service password",
			Description: "Service account password. Set via the TIMEPLUS_PASSWORD environment variable only.",
			Type:        TypeSecret, EnvVar: "TIMEPLUS_PASSWORD", ReadOnly: true},

		// ── ui ──
		{Key: "ui.default_language", Section: "ui", Label: "Default UI language",
			Description: "Language shown to users who haven't picked one in their browser.",
			Type:        TypeEnum, EnumValues: []string{"en", "zh-CN"},
			EnvVar: "AGENTGUARD_UI_DEFAULT_LANGUAGE"},

		// ── rules ──
		{Key: "rules.visible_packages", Section: "rules", Label: "Visible rule packages",
			Description: "Rule packages surfaced on the Rules page. Packages left unchecked (and their not-yet-installed rules) are hidden. File-only; edited here, applied live.",
			Type:        TypeList},

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

		// ── sentry ──
		{Key: "sentry.enabled", Section: "sentry", Label: "Enable Sentry agent",
			Description: "Conversational security analyst agent (chat, SQL skills). Model settings live in agentguard_sentry_config, editable under Settings → Sentry Agent.",
			Type:        TypeBool, EnvVar: "SENTRY_ENABLED"},
		{Key: "sentry.skill_dir", Section: "sentry", Label: "SQL skill directory override",
			Description: "Optional path to an updated timeplus-sql-guide skill directory; empty uses the embedded copy.",
			Type:        TypeString, EnvVar: "SENTRY_SKILL_DIR"},
		{Key: "sentry.chat_base_url", Section: "sentry", Label: "Sentry chat base URL",
			Description: "OpenAI-compatible endpoint for the Sentry chat model (boot default; edit live under Settings → Sentry Agent).",
			Type:        TypeString, EnvVar: "SENTRY_CHAT_BASE_URL"},
		{Key: "sentry.chat_api_key", Section: "sentry", Label: "Sentry chat API key",
			Description: "API key for the Sentry chat endpoint.",
			Type:        TypeSecret, EnvVar: "SENTRY_CHAT_API_KEY"},
		{Key: "sentry.chat_model", Section: "sentry", Label: "Sentry chat model",
			Description: "Tool-calling-capable chat model id for the Sentry agent loop.",
			Type:        TypeString, EnvVar: "SENTRY_CHAT_MODEL"},
		{Key: "sentry.chat_api_type", Section: "sentry", Label: "Sentry chat API type",
			Description: "LLM API dialect for the Sentry chat model: 'openai' (Chat Completions) or 'anthropic' (Anthropic Messages, e.g. Claude on Bedrock).",
			Type:        TypeString, EnvVar: "SENTRY_CHAT_API_TYPE"},
		{Key: "sentry.memory_enabled", Section: "sentry", Label: "Enable Sentry memory (Phase 2)",
			Description: "Enables Sentry's embedding-backed memory (Phase 2). When off, no embedding endpoint is needed.",
			Type:        TypeBool, EnvVar: "SENTRY_MEMORY_ENABLED"},
		{Key: "sentry.embedding_base_url", Section: "sentry", Label: "Sentry embedding base URL",
			Description: "OpenAI-compatible endpoint for the Sentry memory embedding model (may differ from chat, e.g. local Ollama).",
			Type:        TypeString, EnvVar: "SENTRY_EMBEDDING_BASE_URL"},
		{Key: "sentry.embedding_api_key", Section: "sentry", Label: "Sentry embedding API key",
			Description: "API key for the Sentry embedding endpoint.",
			Type:        TypeSecret, EnvVar: "SENTRY_EMBEDDING_API_KEY"},
		{Key: "sentry.embedding_model", Section: "sentry", Label: "Sentry embedding model",
			Description: "Embedding model id for Sentry memory (Phase 2).",
			Type:        TypeString, EnvVar: "SENTRY_EMBEDDING_MODEL"},
		{Key: "sentry.embedding_dim", Section: "sentry", Label: "Sentry embedding dimension",
			Description: "Vector dimension of the Sentry embedding model (Phase 2 memory).",
			Type:        TypeInt, EnvVar: "SENTRY_EMBEDDING_DIM"},
		{Key: "sentry.request_timeout_seconds", Section: "sentry", Label: "Sentry model request timeout (s)",
			Description: "Per-call timeout for Sentry model requests.",
			Type:        TypeInt, EnvVar: "SENTRY_REQUEST_TIMEOUT_SECONDS"},
		{Key: "sentry.max_output_tokens", Section: "sentry", Label: "Sentry max output tokens",
			Description: "Per-completion token cap for the Sentry chat model.",
			Type:        TypeInt, EnvVar: "SENTRY_MAX_OUTPUT_TOKENS"},

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
