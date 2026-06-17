package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/viper"
)

// CoerceAndValidate converts a raw JSON value (float64/bool/string) to the Go
// type the key expects and validates it. Returns the coerced value or an error
// suitable for surfacing to the UI. Unknown keys are an error.
func CoerceAndValidate(key string, raw any) (any, error) {
	meta, ok := MetaFor(key)
	if !ok {
		return nil, fmt.Errorf("unknown config key %q", key)
	}
	switch meta.Type {
	case TypeBool:
		b, ok := raw.(bool)
		if !ok {
			return nil, errors.New("expected a boolean")
		}
		return b, nil

	case TypeInt:
		i, err := toInt(raw)
		if err != nil {
			return nil, err
		}
		if err := validateInt(key, i); err != nil {
			return nil, err
		}
		return i, nil

	case TypeEnum:
		s, ok := raw.(string)
		if !ok {
			return nil, errors.New("expected a string")
		}
		for _, v := range meta.EnumValues {
			if v == s {
				return s, nil
			}
		}
		return nil, fmt.Errorf("must be one of %v", meta.EnumValues)

	case TypeString, TypeSecret:
		s, ok := raw.(string)
		if !ok {
			return nil, errors.New("expected a string")
		}
		if err := validateString(key, s); err != nil {
			return nil, err
		}
		return s, nil
	}
	return nil, fmt.Errorf("unsupported type for key %q", key)
}

func toInt(raw any) (int, error) {
	switch n := raw.(type) {
	case float64: // JSON numbers
		if n != float64(int(n)) {
			return 0, errors.New("expected a whole number")
		}
		return int(n), nil
	case int:
		return n, nil
	default:
		return 0, errors.New("expected a number")
	}
}

func validateInt(key string, v int) error {
	switch key {
	case "server.port", "timeplus.port", "timeplus.native-port":
		if v < 1 || v > 65535 {
			return errors.New("must be between 1 and 65535")
		}
	case "dlp.embedding_dim":
		if v < 1 {
			return errors.New("must be at least 1")
		}
	case "holds.timeout_seconds":
		// Mirror the ceiling enforced by config.load() so a saved value is not
		// silently clamped only on the next restart.
		if v < 0 || v > 540 {
			return errors.New("must be between 0 and 540")
		}
	case "holds.mv_wait_ms":
		if v < 0 {
			return errors.New("must be zero or greater")
		}
	case "dlp.request_timeout_seconds":
		if v < 5 || v > 55 {
			return errors.New("must be between 5 and 55")
		}
	case "dlp.max_output_tokens":
		if v < 16 || v > 4096 {
			return errors.New("must be between 16 and 4096")
		}
	}
	return nil
}

func validateString(key, v string) error {
	switch key {
	case "agentguard.url", "dlp.base_url":
		if v == "" {
			return errors.New("must not be empty")
		}
		u, err := url.ParseRequestURI(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("must be a valid http(s) URL")
		}
	}
	return nil
}

// Source indicates where a key's effective value comes from.
type Source string

const (
	SourceDefault Source = "default"
	SourceFile    Source = "file"
	SourceEnv     Source = "env"
	SourceFlag    Source = "flag"
)

// ResolveSource returns the highest-precedence source supplying a key:
// flag > env > file > default.
func ResolveSource(meta KeyMeta, fileKeys, changedFlags map[string]bool) Source {
	if meta.FlagName != "" && changedFlags[meta.FlagName] {
		return SourceFlag
	}
	if meta.EnvVar != "" && os.Getenv(meta.EnvVar) != "" {
		return SourceEnv
	}
	if fileKeys[meta.Key] {
		return SourceFile
	}
	return SourceDefault
}

// EffectiveValue returns the live value of a key from cfg as a comparable
// scalar (int, bool, or string). Secrets are returned verbatim; callers that
// expose values to clients must mask secrets themselves.
func EffectiveValue(cfg *Config, key string) any {
	switch key {
	case "server.port":
		return cfg.Server.Port
	case "timeplus.host":
		return cfg.Timeplus.Host
	case "timeplus.port":
		return cfg.Timeplus.Port
	case "timeplus.native-port":
		return cfg.Timeplus.NativePort
	case "timeplus.user":
		return cfg.Timeplus.User
	case "timeplus.password":
		return cfg.Timeplus.Password
	case "agentguard.url":
		return cfg.AgentGuardURL
	case "catalog.dir":
		return cfg.CatalogDir
	case "holds.mv_wait_ms":
		return cfg.Holds.MVWaitMs
	case "holds.timeout_seconds":
		return cfg.Holds.TimeoutSeconds
	case "holds.fail_policy":
		return cfg.Holds.FailPolicy
	case "holds.disabled":
		return cfg.Holds.Disabled
	case "dlp.enabled":
		return cfg.DLP.Enabled
	case "dlp.base_url":
		return cfg.DLP.BaseURL
	case "dlp.api_key":
		return cfg.DLP.APIKey
	case "dlp.embedding_model":
		return cfg.DLP.EmbeddingModel
	case "dlp.embedding_dim":
		return cfg.DLP.EmbeddingDim
	case "dlp.risk_model":
		return cfg.DLP.RiskModel
	case "dlp.summary_model":
		return cfg.DLP.SummaryModel
	case "dlp.request_timeout_seconds":
		return cfg.DLP.RequestTimeoutSeconds
	case "dlp.max_output_tokens":
		return cfg.DLP.MaxOutputTokens
	case "setup.complete":
		return cfg.SetupComplete
	}
	return nil
}

// ApplyChange writes a single coerced value into cfg. The value must already
// be coerced via CoerceAndValidate (int/bool/string). Callers must hold Mu.
func ApplyChange(cfg *Config, key string, value any) error {
	switch key {
	case "server.port":
		return setInt(&cfg.Server.Port, value)
	case "timeplus.host":
		return setString(&cfg.Timeplus.Host, value)
	case "timeplus.port":
		return setInt(&cfg.Timeplus.Port, value)
	case "timeplus.native-port":
		return setInt(&cfg.Timeplus.NativePort, value)
	case "agentguard.url":
		return setString(&cfg.AgentGuardURL, value)
	case "catalog.dir":
		return setString(&cfg.CatalogDir, value)
	case "holds.mv_wait_ms":
		return setInt(&cfg.Holds.MVWaitMs, value)
	case "holds.timeout_seconds":
		return setInt(&cfg.Holds.TimeoutSeconds, value)
	case "holds.fail_policy":
		return setString(&cfg.Holds.FailPolicy, value)
	case "holds.disabled":
		return setBool(&cfg.Holds.Disabled, value)
	case "dlp.enabled":
		return setBool(&cfg.DLP.Enabled, value)
	case "dlp.base_url":
		return setString(&cfg.DLP.BaseURL, value)
	case "dlp.api_key":
		return setString(&cfg.DLP.APIKey, value)
	case "dlp.embedding_model":
		return setString(&cfg.DLP.EmbeddingModel, value)
	case "dlp.embedding_dim":
		return setInt(&cfg.DLP.EmbeddingDim, value)
	case "dlp.risk_model":
		return setString(&cfg.DLP.RiskModel, value)
	case "dlp.summary_model":
		return setString(&cfg.DLP.SummaryModel, value)
	case "dlp.request_timeout_seconds":
		return setInt(&cfg.DLP.RequestTimeoutSeconds, value)
	case "dlp.max_output_tokens":
		return setInt(&cfg.DLP.MaxOutputTokens, value)
	}
	return fmt.Errorf("key %q cannot be applied", key)
}

func setInt(dst *int, v any) error {
	i, ok := v.(int)
	if !ok {
		return fmt.Errorf("expected int, got %T", v)
	}
	*dst = i
	return nil
}

func setString(dst *string, v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("expected string, got %T", v)
	}
	*dst = s
	return nil
}

func setBool(dst *bool, v any) error {
	b, ok := v.(bool)
	if !ok {
		return fmt.Errorf("expected bool, got %T", v)
	}
	*dst = b
	return nil
}

// FileKeys returns the set of dotted keys present in the YAML file at path.
// A missing file yields an empty set (not an error).
func FileKeys(path string) (map[string]bool, error) {
	out := map[string]bool{}
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	for _, k := range v.AllKeys() {
		out[k] = true
	}
	return out, nil
}

// SaveChanges merge-writes only the given keys into the YAML file at path,
// preserving every other existing key, then enforces 0600 permissions.
// A missing file is created. Values must be pre-coerced.
func SaveChanges(path string, changes map[string]any) error {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	for k, val := range changes {
		v.Set(k, val)
	}
	if err := v.WriteConfigAs(path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}
