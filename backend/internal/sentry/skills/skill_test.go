package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

type fake struct {
	name   string
	access AccessClass
}

func (f fake) Name() string           { return f.name }
func (f fake) Description() string    { return "fake" }
func (f fake) Params() map[string]any { return map[string]any{"type": "object"} }
func (f fake) Access() AccessClass    { return f.access }
func (f fake) Execute(context.Context, *RunContext, json.RawMessage) (string, error) {
	return "ok", nil
}

func TestRegistryFiltersByAccess(t *testing.T) {
	r := NewRegistry(fake{"a", ReadOnly}, fake{"b", GatedWrite}, fake{"c", MemoryWrite})
	if got := len(r.Defs(ReadOnly)); got != 1 {
		t.Fatalf("ReadOnly defs = %d, want 1 (headless surface)", got)
	}
	if got := len(r.Defs(GatedWrite)); got != 3 {
		t.Fatalf("GatedWrite defs = %d, want 3", got)
	}
	if _, ok := r.Get("b"); !ok {
		t.Fatal("Get(b) failed")
	}
}

func TestTruncate(t *testing.T) {
	s, cut := Truncate("héllo wörld", 7)
	if !cut || !strings.HasSuffix(s, "... [truncated]") {
		t.Fatalf("got %q cut=%v", s, cut)
	}
	if s2, cut2 := Truncate("short", 100); cut2 || s2 != "short" {
		t.Fatalf("short string must pass through, got %q", s2)
	}
}

func TestTruncateRuneBoundary(t *testing.T) {
	// Test case (a): max lands mid-rune (é is 2 bytes)
	s, cut := Truncate("héllo", 2)
	if !cut {
		t.Fatalf("expected cut=true for mid-rune truncation, got cut=%v", cut)
	}
	if !strings.HasSuffix(s, "... [truncated]") {
		t.Fatalf("expected truncation marker, got %q", s)
	}
	if !isValidUTF8(s) {
		t.Fatalf("result is not valid UTF-8: %q", s)
	}
}

func TestTruncateNegativeMax(t *testing.T) {
	// Test case (b): negative max should not panic
	s, cut := Truncate("hello", -5)
	if !cut {
		t.Fatalf("expected cut=true for negative max, got cut=%v", cut)
	}
	if !strings.HasSuffix(s, "... [truncated]") {
		t.Fatalf("expected truncation marker, got %q", s)
	}
	if !isValidUTF8(s) {
		t.Fatalf("result is not valid UTF-8: %q", s)
	}
}

func isValidUTF8(s string) bool {
	return len(s) == 0 || utf8.ValidString(s)
}
