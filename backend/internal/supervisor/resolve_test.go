package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolver_PrefersFlagThenEnvThenPath(t *testing.T) {
	// Real executable files for the flag and env targets (both are validated).
	dir := t.TempDir()
	flagBin := filepath.Join(dir, "tp-flag")
	envBin := filepath.Join(dir, "tp-env")
	for _, p := range []string{flagBin, envBin} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	r := Resolver{
		Flag:     flagBin,
		Getenv:   func(string) string { return envBin },
		LookPath: func(string) (string, error) { return "/path/timeplusd", nil },
	}
	got, err := r.Resolve()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != flagBin {
		t.Errorf("flag should win: got %q", got)
	}

	// No flag -> env wins.
	r.Flag = ""
	if got, _ := r.Resolve(); got != envBin {
		t.Errorf("env should win: got %q", got)
	}

	// No flag, no env -> PATH wins.
	r.Getenv = func(string) string { return "" }
	if got, _ := r.Resolve(); got != "/path/timeplusd" {
		t.Errorf("PATH should win: got %q", got)
	}
}

func TestResolver_ErrorsWhenEnvNotExecutable(t *testing.T) {
	r := Resolver{
		Getenv:   func(string) string { return "/does/not/exist/timeplusd" },
		LookPath: func(string) (string, error) { return "/path/timeplusd", nil },
	}
	// An explicit-but-invalid TIMEPLUSD_BIN must error rather than silently
	// falling through to $PATH.
	if _, err := r.Resolve(); err == nil {
		t.Fatal("expected error when TIMEPLUSD_BIN points at a non-executable")
	}
}

func TestResolver_ErrorsWhenUnresolvableAndNoEmbed(t *testing.T) {
	r := Resolver{
		Getenv:   func(string) string { return "" },
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
		DataDir:  t.TempDir(),
	}
	// In the default (non-embed) build, embeddedTimeplusd() returns (nil,false),
	// so resolution must fail with actionable guidance.
	if _, err := r.Resolve(); err == nil {
		t.Fatal("expected error when timeplusd cannot be resolved")
	}
}
