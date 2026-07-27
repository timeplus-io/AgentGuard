package secrets

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testCodec(t *testing.T) *Codec {
	t.Helper()
	c, err := NewCodec(sha256.Sum256([]byte("test-master-key")))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCodecRoundTrip(t *testing.T) {
	c := testCodec(t)
	sealed := c.Encrypt("sk-super-secret")
	if !strings.HasPrefix(sealed, Prefix) {
		t.Fatalf("sealed = %q, want %s prefix", sealed, Prefix)
	}
	if strings.Contains(sealed, "super-secret") {
		t.Fatal("plaintext visible in ciphertext")
	}
	plain, err := c.Decrypt(sealed)
	if err != nil || plain != "sk-super-secret" {
		t.Fatalf("round trip: %q, %v", plain, err)
	}

	// Distinct nonces: same plaintext seals differently.
	if c.Encrypt("sk-super-secret") == sealed {
		t.Fatal("nonce reuse: identical ciphertexts")
	}
	// Idempotent: sealing ciphertext is a no-op.
	if c.Encrypt(sealed) != sealed {
		t.Fatal("re-encrypting ciphertext must be a no-op")
	}
	// Empty stays empty.
	if c.Encrypt("") != "" {
		t.Fatal("empty credential must stay empty")
	}
}

func TestCodecLegacyPlaintextPassthrough(t *testing.T) {
	c := testCodec(t)
	plain, err := c.Decrypt("sk-legacy-plaintext")
	if err != nil || plain != "sk-legacy-plaintext" {
		t.Fatalf("legacy passthrough: %q, %v", plain, err)
	}
}

func TestCodecTamperAndWrongKey(t *testing.T) {
	c := testCodec(t)
	sealed := c.Encrypt("sk-secret")

	// Tamper: flip a tail character.
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, err := c.Decrypt(tampered); err == nil {
		t.Fatal("tampered ciphertext must fail")
	}

	// Wrong key.
	other, _ := NewCodec(sha256.Sum256([]byte("different-key")))
	if _, err := other.Decrypt(sealed); err == nil {
		t.Fatal("wrong key must fail, not return garbage")
	}
}

func TestNilCodecSemantics(t *testing.T) {
	var c *Codec
	// Encrypt on nil = legacy passthrough (no key configured yet).
	if c.Encrypt("sk-x") != "sk-x" {
		t.Fatal("nil codec must pass plaintext through on encrypt")
	}
	// Decrypt of plaintext passes; of ciphertext must ERROR (a missing key
	// must be loud, never a silently-empty credential).
	if v, err := c.Decrypt("sk-x"); err != nil || v != "sk-x" {
		t.Fatal("nil codec plaintext decrypt must pass through")
	}
	if _, err := c.Decrypt(Prefix + "AAAA"); err == nil {
		t.Fatal("nil codec must refuse to open ciphertext")
	}
}

func TestInitKeyFileLifecycle(t *testing.T) {
	t.Setenv("AGENTGUARD_ENCRYPTION_KEY", "")
	dir := t.TempDir()
	t.Cleanup(func() { SetDefault(nil) })

	SetDefault(nil)
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, keyFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %o, want 600", info.Mode().Perm())
	}
	sealed := Default().Encrypt("sk-abc")

	// Re-init from the same file decrypts what the first key sealed.
	SetDefault(nil)
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if plain, err := Default().Decrypt(sealed); err != nil || plain != "sk-abc" {
		t.Fatalf("reloaded key failed round trip: %q, %v", plain, err)
	}
}

func TestInitEnvKeyDeterministic(t *testing.T) {
	t.Setenv("AGENTGUARD_ENCRYPTION_KEY", "operator-passphrase")
	t.Cleanup(func() { SetDefault(nil) })

	SetDefault(nil)
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	sealed := Default().Encrypt("sk-env")

	SetDefault(nil)
	if err := Init(t.TempDir()); err != nil { // different dir — key comes from env
		t.Fatal(err)
	}
	if plain, err := Default().Decrypt(sealed); err != nil || plain != "sk-env" {
		t.Fatalf("env-derived key must be deterministic: %q, %v", plain, err)
	}
}
