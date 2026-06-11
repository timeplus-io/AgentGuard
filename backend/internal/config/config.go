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
}

type Config struct {
	Server        ServerConfig
	Timeplus      TimeplusConfig
	CatalogDir    string
	AgentGuardURL string
	SetupComplete bool
	Holds         HoldsConfig `mapstructure:"holds"`
	DLP           DLPConfig   `mapstructure:"dlp"`
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
				return nil, fmt.Errorf("reading explicit config file %s: %w", explicitConfigFile, err)
			}
			var notFound viper.ConfigFileNotFoundError
			if !errors.As(err, &notFound) && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("reading config file: %w", err)
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
	return v.WriteConfigAs(path)
}
