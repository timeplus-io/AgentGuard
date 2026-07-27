// Package secrets provides at-rest encryption for credential fields stored
// in Timeplus config streams (#234). Values are sealed with AES-256-GCM and
// stored as "enc:v1:<base64(nonce||ciphertext)>", so any SQL reader (the
// Timeplus console, /investigate, backups) sees only ciphertext — only the
// AgentGuard runtime, which holds the master key, can decrypt.
//
// Master-key resolution (Init):
//  1. AGENTGUARD_ENCRYPTION_KEY env var — the operator-provided passphrase
//     is stretched to a 32-byte key via SHA-256 (any non-empty string works).
//  2. Otherwise a key file (encryption.key, 64 hex chars, mode 0600) in the
//     data directory next to config.yaml — auto-generated on first boot so
//     zero-setup deployments still get encryption.
//
// Losing the key means stored credentials cannot be decrypted — operators
// must re-enter them in Settings (documented in docs/sentry.md).
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// Prefix marks an encrypted value; anything without it is treated as
	// legacy plaintext (lazy migration: re-encrypted on next write).
	Prefix = "enc:v1:"

	envKey      = "AGENTGUARD_ENCRYPTION_KEY"
	keyFileName = "encryption.key"
)

// Codec seals and opens credential strings with a fixed 32-byte key.
type Codec struct{ aead cipher.AEAD }

// NewCodec builds a Codec from a 32-byte key.
func NewCodec(key [32]byte) (*Codec, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Codec{aead: aead}, nil
}

// Encrypt seals plain into the enc:v1 wire format. Empty input stays empty
// (an unset credential must remain visibly unset). Already-encrypted input
// is returned unchanged (idempotent re-writes).
func (c *Codec) Encrypt(plain string) string {
	if c == nil || plain == "" || strings.HasPrefix(plain, Prefix) {
		return plain
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		// Practically unreachable; refuse to write plaintext silently.
		log.Printf("secrets: nonce generation failed: %v — storing value unencrypted", err)
		return plain
	}
	sealed := c.aead.Seal(nil, nonce, []byte(plain), nil)
	return Prefix + base64.StdEncoding.EncodeToString(append(nonce, sealed...))
}

// Decrypt opens an enc:v1 value. Non-prefixed input is legacy plaintext and
// passes through unchanged. A prefixed value that cannot be opened (wrong
// key, corruption) returns an error — never a silently-empty credential.
func (c *Codec) Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, Prefix) {
		return value, nil
	}
	if c == nil {
		return "", fmt.Errorf("secrets: encrypted value but no encryption key is configured")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
	if err != nil {
		return "", fmt.Errorf("secrets: malformed ciphertext: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) <= ns {
		return "", fmt.Errorf("secrets: ciphertext too short")
	}
	plain, err := c.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("secrets: decrypt failed (wrong key or corrupted value): %w", err)
	}
	return string(plain), nil
}

var (
	mu       sync.Mutex
	deflt    *Codec
	initOnce bool
)

// Init resolves the master key (env var, else key file in dataDir —
// auto-generated when missing) and installs the process-wide default codec.
// Called once from main; safe to call again (no-op).
func Init(dataDir string) error {
	mu.Lock()
	defer mu.Unlock()
	if initOnce {
		return nil
	}

	var key [32]byte
	if pass := os.Getenv(envKey); pass != "" {
		key = sha256.Sum256([]byte(pass))
	} else {
		path := filepath.Join(dataDir, keyFileName)
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			raw, derr := hex.DecodeString(strings.TrimSpace(string(b)))
			if derr != nil || len(raw) != 32 {
				return fmt.Errorf("secrets: %s is not 64 hex chars", path)
			}
			copy(key[:], raw)
		case os.IsNotExist(err):
			if _, rerr := rand.Read(key[:]); rerr != nil {
				return fmt.Errorf("secrets: generate key: %w", rerr)
			}
			if werr := os.WriteFile(path, []byte(hex.EncodeToString(key[:])+"\n"), 0o600); werr != nil {
				return fmt.Errorf("secrets: write %s: %w", path, werr)
			}
			log.Printf("secrets: generated new encryption key at %s", path)
		default:
			return fmt.Errorf("secrets: read %s: %w", path, err)
		}
	}

	c, err := NewCodec(key)
	if err != nil {
		return err
	}
	deflt = c
	initOnce = true
	return nil
}

// Default returns the process-wide codec, or nil when Init has not run
// (unit tests, callers before main wiring). A nil codec encrypts to
// plaintext (legacy behavior) and decrypts plaintext passthrough — but
// refuses to open ciphertext, so a missing key surfaces loudly instead of
// yielding empty credentials.
func Default() *Codec {
	mu.Lock()
	defer mu.Unlock()
	return deflt
}

// SetDefault overrides the process codec — tests only.
func SetDefault(c *Codec) {
	mu.Lock()
	defer mu.Unlock()
	deflt = c
	initOnce = c != nil
}
