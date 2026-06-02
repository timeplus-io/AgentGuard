package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	if fi, err := os.Stat(dst); err == nil && fi.Size() == int64(len(data)) && isExecutable(dst) {
		return dst, nil // already extracted, same size, still executable
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("timeplusd: extract dir: %w", err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		return "", fmt.Errorf("timeplusd: extract: %w", err)
	}
	return dst, nil
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}
