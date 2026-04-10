package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type TimeplusConfig struct {
	Host       string
	Port       int
	NativePort int
	User       string
	Password   string
}

type ServerConfig struct {
	Port int
}

type Config struct {
	Server     ServerConfig
	Timeplus   TimeplusConfig
	CatalogDir string
}

// Load reads configuration from (in order of increasing precedence):
//  1. Built-in defaults
//  2. Config file (config.yaml / config.json / config.toml in working dir or $HOME)
//  3. Environment variables prefixed with TIMEPLUS_ or SERVER_
//  4. CLI flags (bound by the caller via viper.BindPFlag)
func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("catalog.dir", "./catalog")
	v.SetDefault("timeplus.host", "localhost")
	v.SetDefault("timeplus.port", 3218)
	v.SetDefault("timeplus.native-port", 8463)
	v.SetDefault("timeplus.user", "default")
	v.SetDefault("timeplus.password", "")

	// Config file
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("$HOME/.agentshield")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
		// No config file is fine — use defaults + env
	}

	// Environment variables: TIMEPLUS_HOST, TIMEPLUS_PORT, TIMEPLUS_USER,
	// TIMEPLUS_PASSWORD, SERVER_PORT. The AutomaticEnv + replacer maps
	// "timeplus.host" → TIMEPLUS_HOST automatically.
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	cfg := &Config{}
	cfg.Server.Port = v.GetInt("server.port")
	cfg.CatalogDir = v.GetString("catalog.dir")
	cfg.Timeplus.Host = v.GetString("timeplus.host")
	cfg.Timeplus.Port = v.GetInt("timeplus.port")
	cfg.Timeplus.NativePort = v.GetInt("timeplus.native-port")
	cfg.Timeplus.User = v.GetString("timeplus.user")
	cfg.Timeplus.Password = v.GetString("timeplus.password")

	return cfg, nil
}
