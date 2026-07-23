package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/viper"
)

// Mu protects live config mutations that happen after startup.
// Provision acquires a write lock when mutating cfg fields;
// AuthStatus acquires a read lock when reading cfg.SetupComplete.
var Mu sync.RWMutex

type TimeplusConfig struct {
	Host       string
	Port       int
	NativePort int
	// User and Password come from TIMEPLUS_USER / TIMEPLUS_PASSWORD env vars on
	// the AgentGuard process — service credentials used for server-to-server
	// endpoints (plugin → /api/holds, Timeplus → /api/alerts/webhook). UI-facing
	// handlers use credentials from the user's session cookie (auth.RequestCfg)
	// instead.
	User     string
	Password string
}

type ServerConfig struct {
	Port int
}

type HoldsConfig struct {
	MVWaitMs       int    `mapstructure:"mv_wait_ms"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
	FailPolicy     string `mapstructure:"fail_policy"`
	Disabled       bool   `mapstructure:"disabled"`
}

type DLPConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	BaseURL        string `mapstructure:"base_url"`
	APIKey         string `mapstructure:"api_key"`
	EmbeddingModel string `mapstructure:"embedding_model"`
	EmbeddingDim   int    `mapstructure:"embedding_dim"`
	RiskModel      string `mapstructure:"risk_model"`
	SummaryModel   string `mapstructure:"summary_model"`
	// RequestTimeoutSeconds is the per-call timeout (seconds) on every DLP UDF
	// model request. Clamped to [5,55] so query-funnel UDFs return before the
	// 60s protonHTTPClient ceiling.
	RequestTimeoutSeconds int `mapstructure:"request_timeout_seconds"`
	// MaxOutputTokens caps chat-UDF completions (risk/understand/rerank);
	// dlp_summarize uses max(MaxOutputTokens, 800).
	MaxOutputTokens int `mapstructure:"max_output_tokens"`
}

// SentryConfig configures the Sentry conversational agent and its two
// self-contained model endpoints — chat and memory(embedding) — as boot
// defaults seeded seed-if-absent into agentguard_sentry_config (`chat` +
// `memory` rows; the stream is authoritative after first provision). Spec:
// 2026-07-23-sentry-config-reshape.
type SentryConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	SkillDir string `mapstructure:"skill_dir"` // optional on-disk override for the vendored timeplus-sql-guide skill
	// chat endpoint boot defaults → agentguard_sentry_config `chat` row
	ChatBaseURL           string `mapstructure:"chat_base_url"`
	ChatAPIKey            string `mapstructure:"chat_api_key"`
	ChatModel             string `mapstructure:"chat_model"`
	RequestTimeoutSeconds int    `mapstructure:"request_timeout_seconds"`
	MaxOutputTokens       int    `mapstructure:"max_output_tokens"`
	// memory(embedding) endpoint boot defaults → `memory` row (Phase-2 feature)
	MemoryEnabled    bool   `mapstructure:"memory_enabled"`
	EmbeddingBaseURL string `mapstructure:"embedding_base_url"`
	EmbeddingAPIKey  string `mapstructure:"embedding_api_key"`
	EmbeddingModel   string `mapstructure:"embedding_model"`
	EmbeddingDim     int    `mapstructure:"embedding_dim"`
}

// ProvisionedDLP records the DLP model settings that were baked into UDFs and
// the enriched stream at the last successful provision. Used to detect drift
// ("re-provision required") when the live DLP config changes afterward.
type ProvisionedDLP struct {
	Recorded       bool   `mapstructure:"recorded"`
	EmbeddingModel string `mapstructure:"embedding_model"`
	EmbeddingDim   int    `mapstructure:"embedding_dim"`
	RiskModel      string `mapstructure:"risk_model"`
	SummaryModel   string `mapstructure:"summary_model"`
	// RequestTimeoutSeconds and MaxOutputTokens are also baked into the UDF DDL,
	// so drift in them likewise requires a re-provision.
	RequestTimeoutSeconds int `mapstructure:"request_timeout_seconds"`
	MaxOutputTokens       int `mapstructure:"max_output_tokens"`
}

type Config struct {
	Server         ServerConfig
	Timeplus       TimeplusConfig
	CatalogDir     string
	AgentGuardURL  string
	SetupComplete  bool
	Holds          HoldsConfig    `mapstructure:"holds"`
	DLP            DLPConfig      `mapstructure:"dlp"`
	ProvisionedDLP ProvisionedDLP // loaded from provisioned.dlp.* in config.yaml
	Sentry         SentryConfig   `mapstructure:"sentry"`
	// ConfigPath is the file path this config was loaded from (or will be written to).
	// Populated by the caller after Load(); not read from the config file itself.
	ConfigPath string
}

// Load reads configuration from (in order of increasing precedence):
//  1. Built-in defaults
//  2. Config file (config.yaml in working dir or $HOME/.agentguard)
//  3. Environment variables prefixed with TIMEPLUS_ or SERVER_
//  4. CLI flags (bound by the caller via viper.BindPFlag)
//
// User-facing UI handlers always require an authenticated session cookie.
// Server-to-server handlers (plugin → /api/holds, Timeplus → /api/alerts/webhook)
// require TIMEPLUS_USER + TIMEPLUS_PASSWORD env vars; if those are unset the
// handlers return 503 with an actionable message — there is no fallback.
func Load() (*Config, error) {
	return load(viper.GetViper(), true)
}

// LoadFrom reads configuration from a specific file path without applying
// process environment overrides.
// Returns a config with defaults if the file does not exist.
func LoadFrom(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return load(v, false)
		}
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}
	return load(v, false)
}

func load(v *viper.Viper, searchPaths bool) (*Config, error) {
	v.SetDefault("server.port", 8080)
	v.SetDefault("catalog.dir", "./catalog")
	v.SetDefault("agentguard.url", "http://localhost:8080")
	v.SetDefault("timeplus.host", "localhost")
	v.SetDefault("timeplus.port", 3218)
	v.SetDefault("timeplus.native-port", 8463)
	v.SetDefault("timeplus.user", "")
	v.SetDefault("timeplus.password", "")
	v.SetDefault("setup.complete", false)
	v.SetDefault("holds.mv_wait_ms", 500)
	v.SetDefault("holds.timeout_seconds", 300)
	v.SetDefault("holds.fail_policy", "deny")
	v.SetDefault("holds.disabled", false)
	v.SetDefault("dlp.enabled", false)
	v.SetDefault("dlp.base_url", "http://localhost:4000/v1")
	v.SetDefault("dlp.api_key", "")
	v.SetDefault("dlp.embedding_model", "text-embedding-3-small")
	v.SetDefault("dlp.embedding_dim", 1536)
	v.SetDefault("dlp.risk_model", "gpt-4o-mini")
	v.SetDefault("dlp.summary_model", "gpt-4o")
	v.SetDefault("dlp.request_timeout_seconds", 50)
	v.SetDefault("dlp.max_output_tokens", 512)
	v.SetDefault("provisioned.dlp.recorded", false)
	v.SetDefault("provisioned.dlp.embedding_model", "")
	v.SetDefault("provisioned.dlp.embedding_dim", 0)
	v.SetDefault("provisioned.dlp.risk_model", "")
	v.SetDefault("provisioned.dlp.summary_model", "")
	v.SetDefault("provisioned.dlp.request_timeout_seconds", 0)
	v.SetDefault("provisioned.dlp.max_output_tokens", 0)
	v.SetDefault("sentry.enabled", false)
	v.SetDefault("sentry.skill_dir", "")
	v.SetDefault("sentry.chat_base_url", "")
	v.SetDefault("sentry.chat_api_key", "")
	v.SetDefault("sentry.chat_model", "")
	v.SetDefault("sentry.request_timeout_seconds", 60)
	v.SetDefault("sentry.max_output_tokens", 1024)
	v.SetDefault("sentry.memory_enabled", false)
	v.SetDefault("sentry.embedding_base_url", "")
	v.SetDefault("sentry.embedding_api_key", "")
	v.SetDefault("sentry.embedding_model", "")
	v.SetDefault("sentry.embedding_dim", 1536)

	if searchPaths {
		explicitConfigFile := v.ConfigFileUsed()
		v.SetConfigType("yaml")
		// Only configure the default search paths when no explicit config file
		// has been set (e.g. via --config / viper.SetConfigFile). viper's
		// SetConfigName resets the internal configFile, so calling it here would
		// discard an explicit --config path and make ReadInConfig search "."
		// instead — silently ignoring every field in the user's config file.
		if explicitConfigFile == "" {
			v.SetConfigName("config")
			v.AddConfigPath(".")
			if home, err := os.UserHomeDir(); err == nil {
				v.AddConfigPath(filepath.Join(home, ".agentguard"))
			}
		}

		if err := v.ReadInConfig(); err != nil {
			if explicitConfigFile != "" {
				if errors.Is(err, os.ErrNotExist) {
					log.Printf("config: %s not found, using defaults and environment variables", explicitConfigFile)
				} else {
					return nil, fmt.Errorf("reading explicit config file %s: %w", explicitConfigFile, err)
				}
			} else {
				var notFound viper.ConfigFileNotFoundError
				if !errors.As(err, &notFound) && !errors.Is(err, os.ErrNotExist) {
					return nil, fmt.Errorf("reading config file: %w", err)
				}
			}
		}

		bindEnv(v)
	}

	cfg := &Config{}
	cfg.Server.Port = v.GetInt("server.port")
	cfg.CatalogDir = v.GetString("catalog.dir")
	cfg.AgentGuardURL = v.GetString("agentguard.url")
	cfg.Timeplus.Host = v.GetString("timeplus.host")
	cfg.Timeplus.Port = v.GetInt("timeplus.port")
	cfg.Timeplus.NativePort = v.GetInt("timeplus.native-port")
	cfg.SetupComplete = v.GetBool("setup.complete")
	cfg.Holds.MVWaitMs = v.GetInt("holds.mv_wait_ms")
	cfg.Holds.TimeoutSeconds = v.GetInt("holds.timeout_seconds")
	cfg.Holds.FailPolicy = v.GetString("holds.fail_policy")
	cfg.Holds.Disabled = v.GetBool("holds.disabled")
	cfg.DLP.Enabled = v.GetBool("dlp.enabled")
	cfg.DLP.BaseURL = v.GetString("dlp.base_url")
	cfg.DLP.APIKey = v.GetString("dlp.api_key")
	cfg.DLP.EmbeddingModel = v.GetString("dlp.embedding_model")
	cfg.DLP.EmbeddingDim = v.GetInt("dlp.embedding_dim")
	cfg.DLP.RiskModel = v.GetString("dlp.risk_model")
	cfg.DLP.SummaryModel = v.GetString("dlp.summary_model")
	cfg.DLP.RequestTimeoutSeconds = v.GetInt("dlp.request_timeout_seconds")
	cfg.DLP.MaxOutputTokens = v.GetInt("dlp.max_output_tokens")
	cfg.ProvisionedDLP.Recorded = v.GetBool("provisioned.dlp.recorded")
	cfg.ProvisionedDLP.EmbeddingModel = v.GetString("provisioned.dlp.embedding_model")
	cfg.ProvisionedDLP.EmbeddingDim = v.GetInt("provisioned.dlp.embedding_dim")
	cfg.ProvisionedDLP.RiskModel = v.GetString("provisioned.dlp.risk_model")
	cfg.ProvisionedDLP.SummaryModel = v.GetString("provisioned.dlp.summary_model")
	cfg.ProvisionedDLP.RequestTimeoutSeconds = v.GetInt("provisioned.dlp.request_timeout_seconds")
	cfg.ProvisionedDLP.MaxOutputTokens = v.GetInt("provisioned.dlp.max_output_tokens")
	cfg.Sentry.Enabled = v.GetBool("sentry.enabled")
	cfg.Sentry.SkillDir = v.GetString("sentry.skill_dir")
	cfg.Sentry.ChatBaseURL = v.GetString("sentry.chat_base_url")
	cfg.Sentry.ChatAPIKey = v.GetString("sentry.chat_api_key")
	cfg.Sentry.ChatModel = v.GetString("sentry.chat_model")
	cfg.Sentry.RequestTimeoutSeconds = v.GetInt("sentry.request_timeout_seconds")
	cfg.Sentry.MaxOutputTokens = v.GetInt("sentry.max_output_tokens")
	cfg.Sentry.MemoryEnabled = v.GetBool("sentry.memory_enabled")
	cfg.Sentry.EmbeddingBaseURL = v.GetString("sentry.embedding_base_url")
	cfg.Sentry.EmbeddingAPIKey = v.GetString("sentry.embedding_api_key")
	cfg.Sentry.EmbeddingModel = v.GetString("sentry.embedding_model")
	cfg.Sentry.EmbeddingDim = v.GetInt("sentry.embedding_dim")
	// TIMEPLUS_USER / TIMEPLUS_PASSWORD: optional canonical service account
	// for server-to-server endpoints. Never written to the config file.
	cfg.Timeplus.User = v.GetString("timeplus.user")
	cfg.Timeplus.Password = v.GetString("timeplus.password")

	const holdTimeoutCeilingSec = 540
	if cfg.Holds.TimeoutSeconds > holdTimeoutCeilingSec {
		log.Printf("config: holds.timeout_seconds=%d exceeds ceiling %d; clamping",
			cfg.Holds.TimeoutSeconds, holdTimeoutCeilingSec)
		cfg.Holds.TimeoutSeconds = holdTimeoutCeilingSec
	}
	if cfg.DLP.RequestTimeoutSeconds < 5 {
		cfg.DLP.RequestTimeoutSeconds = 5
	} else if cfg.DLP.RequestTimeoutSeconds > 55 {
		log.Printf("config: dlp.request_timeout_seconds=%d exceeds 55; clamping", cfg.DLP.RequestTimeoutSeconds)
		cfg.DLP.RequestTimeoutSeconds = 55
	}
	if cfg.DLP.MaxOutputTokens < 16 {
		cfg.DLP.MaxOutputTokens = 16
	} else if cfg.DLP.MaxOutputTokens > 4096 {
		cfg.DLP.MaxOutputTokens = 4096
	}

	return cfg, nil
}

func bindEnv(v *viper.Viper) {
	_ = v.BindEnv("server.port", "SERVER_PORT")
	_ = v.BindEnv("catalog.dir", "CATALOG_DIR")
	_ = v.BindEnv("agentguard.url", "AGENTGUARD_URL")
	_ = v.BindEnv("setup.complete", "SETUP_COMPLETE")
	_ = v.BindEnv("timeplus.host", "TIMEPLUS_HOST")
	_ = v.BindEnv("timeplus.port", "TIMEPLUS_PORT")
	_ = v.BindEnv("timeplus.native-port", "TIMEPLUS_NATIVE_PORT")
	_ = v.BindEnv("holds.mv_wait_ms", "AGENTGUARD_HOLD_MV_WAIT_MS")
	_ = v.BindEnv("holds.timeout_seconds", "AGENTGUARD_HOLD_TIMEOUT_SECONDS")
	_ = v.BindEnv("holds.fail_policy", "AGENTGUARD_HOLDS_FAIL_POLICY")
	_ = v.BindEnv("holds.disabled", "AGENTGUARD_HOLDS_DISABLED")
	_ = v.BindEnv("timeplus.user", "TIMEPLUS_USER")
	_ = v.BindEnv("timeplus.password", "TIMEPLUS_PASSWORD")
	_ = v.BindEnv("dlp.enabled", "DLP_ENABLED")
	_ = v.BindEnv("dlp.base_url", "DLP_BASE_URL")
	_ = v.BindEnv("dlp.api_key", "DLP_API_KEY")
	_ = v.BindEnv("dlp.embedding_model", "DLP_EMBEDDING_MODEL")
	_ = v.BindEnv("dlp.embedding_dim", "DLP_EMBEDDING_DIM")
	_ = v.BindEnv("dlp.risk_model", "DLP_RISK_MODEL")
	_ = v.BindEnv("dlp.summary_model", "DLP_SUMMARY_MODEL")
	_ = v.BindEnv("dlp.request_timeout_seconds", "DLP_REQUEST_TIMEOUT_SECONDS")
	_ = v.BindEnv("dlp.max_output_tokens", "DLP_MAX_OUTPUT_TOKENS")
	_ = v.BindEnv("sentry.enabled", "SENTRY_ENABLED")
	_ = v.BindEnv("sentry.skill_dir", "SENTRY_SKILL_DIR")
	_ = v.BindEnv("sentry.chat_base_url", "SENTRY_CHAT_BASE_URL")
	_ = v.BindEnv("sentry.chat_api_key", "SENTRY_CHAT_API_KEY")
	_ = v.BindEnv("sentry.chat_model", "SENTRY_CHAT_MODEL")
	_ = v.BindEnv("sentry.request_timeout_seconds", "SENTRY_REQUEST_TIMEOUT_SECONDS")
	_ = v.BindEnv("sentry.max_output_tokens", "SENTRY_MAX_OUTPUT_TOKENS")
	_ = v.BindEnv("sentry.memory_enabled", "SENTRY_MEMORY_ENABLED")
	_ = v.BindEnv("sentry.embedding_base_url", "SENTRY_EMBEDDING_BASE_URL")
	_ = v.BindEnv("sentry.embedding_api_key", "SENTRY_EMBEDDING_API_KEY")
	_ = v.BindEnv("sentry.embedding_model", "SENTRY_EMBEDDING_MODEL")
	_ = v.BindEnv("sentry.embedding_dim", "SENTRY_EMBEDDING_DIM")
}

// Save writes non-sensitive config fields to the specified path (typically "config.yaml").
// Credentials are never written.
func Save(cfg *Config, path string) error {
	v := viper.New()
	v.SetConfigType("yaml")
	v.Set("server.port", cfg.Server.Port)
	v.Set("catalog.dir", cfg.CatalogDir)
	v.Set("agentguard.url", cfg.AgentGuardURL)
	v.Set("timeplus.host", cfg.Timeplus.Host)
	v.Set("timeplus.port", cfg.Timeplus.Port)
	v.Set("timeplus.native-port", cfg.Timeplus.NativePort)
	v.Set("setup.complete", cfg.SetupComplete)
	v.Set("holds.mv_wait_ms", cfg.Holds.MVWaitMs)
	v.Set("holds.timeout_seconds", cfg.Holds.TimeoutSeconds)
	v.Set("holds.fail_policy", cfg.Holds.FailPolicy)
	v.Set("holds.disabled", cfg.Holds.Disabled)
	v.Set("dlp.enabled", cfg.DLP.Enabled)
	v.Set("dlp.base_url", cfg.DLP.BaseURL)
	v.Set("dlp.embedding_model", cfg.DLP.EmbeddingModel)
	v.Set("dlp.embedding_dim", cfg.DLP.EmbeddingDim)
	v.Set("dlp.risk_model", cfg.DLP.RiskModel)
	v.Set("dlp.summary_model", cfg.DLP.SummaryModel)
	v.Set("dlp.request_timeout_seconds", cfg.DLP.RequestTimeoutSeconds)
	v.Set("dlp.max_output_tokens", cfg.DLP.MaxOutputTokens)
	v.Set("provisioned.dlp.recorded", cfg.ProvisionedDLP.Recorded)
	v.Set("provisioned.dlp.embedding_model", cfg.ProvisionedDLP.EmbeddingModel)
	v.Set("provisioned.dlp.embedding_dim", cfg.ProvisionedDLP.EmbeddingDim)
	v.Set("provisioned.dlp.risk_model", cfg.ProvisionedDLP.RiskModel)
	v.Set("provisioned.dlp.summary_model", cfg.ProvisionedDLP.SummaryModel)
	v.Set("provisioned.dlp.request_timeout_seconds", cfg.ProvisionedDLP.RequestTimeoutSeconds)
	v.Set("provisioned.dlp.max_output_tokens", cfg.ProvisionedDLP.MaxOutputTokens)
	v.Set("sentry.enabled", cfg.Sentry.Enabled)
	v.Set("sentry.skill_dir", cfg.Sentry.SkillDir)
	v.Set("sentry.chat_base_url", cfg.Sentry.ChatBaseURL)
	v.Set("sentry.chat_model", cfg.Sentry.ChatModel)
	v.Set("sentry.request_timeout_seconds", cfg.Sentry.RequestTimeoutSeconds)
	v.Set("sentry.max_output_tokens", cfg.Sentry.MaxOutputTokens)
	v.Set("sentry.memory_enabled", cfg.Sentry.MemoryEnabled)
	v.Set("sentry.embedding_base_url", cfg.Sentry.EmbeddingBaseURL)
	v.Set("sentry.embedding_model", cfg.Sentry.EmbeddingModel)
	v.Set("sentry.embedding_dim", cfg.Sentry.EmbeddingDim)
	// sentry.chat_api_key / sentry.embedding_api_key are secrets — never written.
	return v.WriteConfigAs(path)
}
