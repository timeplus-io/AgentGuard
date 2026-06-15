package config

import "testing"

func TestCoerceAndValidate(t *testing.T) {
	cases := []struct {
		key     string
		raw     any
		wantErr bool
		want    any
	}{
		{"server.port", float64(9090), false, 9090},      // JSON numbers arrive as float64
		{"server.port", float64(0), true, nil},           // out of range
		{"server.port", float64(70000), true, nil},       // out of range
		{"server.port", "abc", true, nil},                // wrong type
		{"dlp.enabled", true, false, true},
		{"dlp.enabled", "yes", true, nil},                // wrong type
		{"holds.fail_policy", "allow", false, "allow"},
		{"holds.fail_policy", "maybe", true, nil},        // not in enum
		{"dlp.embedding_dim", float64(768), false, 768},
		{"dlp.embedding_dim", float64(0), true, nil},     // must be >= 1
		{"dlp.base_url", "http://x:4000/v1", false, "http://x:4000/v1"},
		{"dlp.base_url", "not a url", true, nil},
		{"agentguard.url", "https://h:8080", false, "https://h:8080"},
		{"dlp.api_key", "sk-123", false, "sk-123"},
		{"holds.timeout_seconds", float64(-1), true, nil},
		{"nope.key", "x", true, nil},                     // unknown key
	}
	for _, c := range cases {
		got, err := CoerceAndValidate(c.key, c.raw)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s=%v: expected error, got nil", c.key, c.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s=%v: unexpected error %v", c.key, c.raw, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s=%v: got %v (%T), want %v (%T)", c.key, c.raw, got, got, c.want, c.want)
		}
	}
}
