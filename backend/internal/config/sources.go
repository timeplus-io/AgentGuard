package config

import (
	"errors"
	"fmt"
	"net/url"
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
	case "holds.mv_wait_ms", "holds.timeout_seconds":
		if v < 0 {
			return errors.New("must be zero or greater")
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
