package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
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
	// User and Password are NOT loaded from config file or env vars.
	// They are populated at runtime: by auth.RequestCfg (from the session)
	// or by handlers that receive them directly (setup/provision).
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

type Config struct {
	Server        ServerConfig
	Timeplus      TimeplusConfig
	CatalogDir    string
	AgentGuardURL string
	SetupComplete bool
	Holds         HoldsConfig `mapstructure:"holds"`
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
// Credentials (TIMEPLUS_USER, TIMEPLUS_PASSWORD) are intentionally not loaded.
func Load() (*Config, error) {
	return load(viper.GetViper(), true)
}

// LoadFrom reads configuration from a specific file path.
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
	v.SetDefault("setup.complete", false)
	v.SetDefault("holds.mv_wait_ms", 500)
	v.SetDefault("holds.timeout_seconds", 300)
	v.SetDefault("holds.fail_policy", "deny")
	v.SetDefault("holds.disabled", false)

	if searchPaths {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		if home, err := os.UserHomeDir(); err == nil {
			v.AddConfigPath(filepath.Join(home, ".agentguard"))
		}

		if err := v.ReadInConfig(); err != nil {
			var notFound viper.ConfigFileNotFoundError
			if !errors.As(err, &notFound) && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("reading config file: %w", err)
			}
		}

		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		v.AutomaticEnv()
	}

	_ = v.BindEnv("holds.mv_wait_ms", "AGENTGUARD_HOLD_MV_WAIT_MS")
	_ = v.BindEnv("holds.timeout_seconds", "AGENTGUARD_HOLD_TIMEOUT_SECONDS")
	_ = v.BindEnv("holds.fail_policy", "AGENTGUARD_HOLD_FAIL_POLICY")
	_ = v.BindEnv("holds.disabled", "AGENTGUARD_HOLDS_DISABLED")

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
	// Credentials are NOT loaded — always empty in the file-based config.

	const holdTimeoutCeilingSec = 540
	if cfg.Holds.TimeoutSeconds > holdTimeoutCeilingSec {
		log.Printf("config: holds.timeout_seconds=%d exceeds ceiling %d; clamping",
			cfg.Holds.TimeoutSeconds, holdTimeoutCeilingSec)
		cfg.Holds.TimeoutSeconds = holdTimeoutCeilingSec
	}

	return cfg, nil
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
	return v.WriteConfigAs(path)
}
