package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Resolver locates the timeplusd binary: flag → env (TIMEPLUSD_BIN) → $PATH →
// embedded copy (only in the embed_timeplusd build). Fields are injectable for
// tests; NewResolver wires the real os/exec functions.
type Resolver struct {
	Flag     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
	DataDir  string
}

func NewResolver(flag, dataDir string) Resolver {
	return Resolver{Flag: flag, Getenv: os.Getenv, LookPath: exec.LookPath, DataDir: dataDir}
}

func (r Resolver) Resolve() (string, error) {
	if r.Flag != "" {
		if isExecutable(r.Flag) {
			return r.Flag, nil
		}
		return "", fmt.Errorf("timeplusd: --timeplusd-bin %q is not an executable file", r.Flag)
	}
	if env := r.Getenv("TIMEPLUSD_BIN"); env != "" {
		if isExecutable(env) {
			return env, nil
		}
		return "", fmt.Errorf("timeplusd: TIMEPLUSD_BIN %q is not an executable file", env)
	}
	if p, err := r.LookPath("timeplusd"); err == nil && p != "" {
		return p, nil
	}
	if data, ok := embeddedTimeplusd(); ok {
		return r.extractEmbedded(data)
	}
	return "", fmt.Errorf("timeplusd binary not found: pass --timeplusd-bin, set TIMEPLUSD_BIN, " +
		"put timeplusd on $PATH, or build with -tags embed_timeplusd")
}

func (r Resolver) extractEmbedded(data []byte) (string, error) {
	dst := filepath.Join(r.DataDir, "bin", "timeplusd")
	hashPath := dst + ".sha256"
	expectedHash := sha256Hex(data)
	if isExecutable(dst) {
		if raw, err := os.ReadFile(hashPath); err == nil && strings.TrimSpace(string(raw)) == expectedHash {
			return dst, nil
		}
		if gotHash, err := fileSHA256Hex(dst); err == nil && gotHash == expectedHash {
			if err := writeHashSidecar(hashPath, expectedHash); err != nil {
				return "", err
			}
			return dst, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("timeplusd: extract dir: %w", err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		return "", fmt.Errorf("timeplusd: extract: %w", err)
	}
	if err := writeHashSidecar(hashPath, expectedHash); err != nil {
		return "", err
	}
	return dst, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileSHA256Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeHashSidecar(path, hash string) error {
	if err := os.WriteFile(path, []byte(hash+"\n"), 0o644); err != nil {
		return fmt.Errorf("timeplusd: write hash sidecar: %w", err)
	}
	return nil
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}
