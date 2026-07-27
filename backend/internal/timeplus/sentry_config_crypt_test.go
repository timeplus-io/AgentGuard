package timeplus

import (
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/secrets"
)

func withTestCodec(t *testing.T) {
	t.Helper()
	c, err := secrets.NewCodec(sha256.Sum256([]byte("store-test-key")))
	if err != nil {
		t.Fatal(err)
	}
	secrets.SetDefault(c)
	t.Cleanup(func() { secrets.SetDefault(nil) })
}

func TestSentryValueEncryptDecryptRoundTrip(t *testing.T) {
	withTestCodec(t)
	in := `{"api_type":"anthropic","model":"claude-x","api_key":"sk-STORE-canary","base_url":"https://b","timeout":60}`

	enc := encryptSentryValue(in)
	if strings.Contains(enc, "sk-STORE-canary") {
		t.Fatalf("plaintext key survived encryption: %s", enc)
	}
	if !strings.Contains(enc, `"api_key":"`+secrets.Prefix) {
		t.Fatalf("api_key not sealed: %s", enc)
	}
	for _, keep := range []string{`"model":"claude-x"`, `"base_url":"https://b"`, `"timeout":60`} {
		if !strings.Contains(enc, keep) {
			t.Errorf("non-secret field mangled, missing %s: %s", keep, enc)
		}
	}

	dec, err := decryptSentryValue(enc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dec, `"api_key":"sk-STORE-canary"`) {
		t.Fatalf("decrypt did not restore the key: %s", dec)
	}

	// Idempotence: re-encrypting an encrypted value changes nothing secret.
	enc2 := encryptSentryValue(enc)
	if strings.Contains(enc2, "sk-STORE-canary") {
		t.Fatal("double-encrypt leaked plaintext")
	}
	if dec2, err := decryptSentryValue(enc2); err != nil || !strings.Contains(dec2, "sk-STORE-canary") {
		t.Fatalf("double-encrypt round trip failed: %v", err)
	}
}

func TestSentryValueLegacyAndEdgeCases(t *testing.T) {
	withTestCodec(t)

	// Legacy plaintext value: passes through decrypt unchanged.
	legacy := `{"api_key":"sk-plain","model":"m"}`
	out, err := decryptSentryValue(legacy)
	if err != nil || !strings.Contains(out, "sk-plain") {
		t.Fatalf("legacy passthrough failed: %q, %v", out, err)
	}

	// Empty key and absent key: untouched by encryption.
	for _, v := range []string{`{"api_key":"","model":"m"}`, `{"model":"m"}`, "not-json"} {
		if got := encryptSentryValue(v); got != v {
			t.Errorf("value %q must pass through, got %q", v, got)
		}
	}

	// No codec at all: writes stay plaintext (legacy), ciphertext read errors.
	secrets.SetDefault(nil)
	if got := encryptSentryValue(legacy); got != legacy {
		t.Fatalf("no-codec encrypt must pass through, got %q", got)
	}
	if _, err := decryptSentryValue(`{"api_key":"` + secrets.Prefix + `AAAA"}`); err == nil {
		t.Fatal("ciphertext without a key must error loudly")
	}
}
