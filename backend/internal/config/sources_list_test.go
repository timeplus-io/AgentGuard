package config

import (
	"reflect"
	"testing"
)

// TestCoerceListVisiblePackages covers the TypeList coercion path: a JSON array
// arrives as []any, must coerce to []string, dedupe while preserving order, and
// reject non-string elements. nil clears the list.
func TestCoerceListVisiblePackages(t *testing.T) {
	got, err := CoerceAndValidate("rules.visible_packages", []any{"core", "supply-chain", "core"})
	if err != nil {
		t.Fatalf("CoerceAndValidate: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"core", "supply-chain"}) {
		t.Errorf("coerced = %v, want [core supply-chain] (deduped, ordered)", got)
	}

	if got, err := CoerceAndValidate("rules.visible_packages", nil); err != nil || !reflect.DeepEqual(got, []string{}) {
		t.Errorf("nil should clear to empty slice, got %v err %v", got, err)
	}

	if _, err := CoerceAndValidate("rules.visible_packages", []any{"core", 3}); err == nil {
		t.Error("expected error for non-string list entry, got nil")
	}

	if _, err := CoerceAndValidate("rules.visible_packages", "core"); err == nil {
		t.Error("expected error for scalar passed to a list key, got nil")
	}
}

// TestEffectiveAndApplyVisiblePackages verifies the value round-trips through
// ApplyChange (hot-apply) and EffectiveValue for the list key.
func TestEffectiveAndApplyVisiblePackages(t *testing.T) {
	cfg := &Config{}
	cfg.Rules.VisiblePackages = []string{"core"}

	if got := EffectiveValue(cfg, "rules.visible_packages"); !reflect.DeepEqual(got, []string{"core"}) {
		t.Errorf("EffectiveValue = %v, want [core]", got)
	}

	coerced, err := CoerceAndValidate("rules.visible_packages", []any{"core", "prompt-injection"})
	if err != nil {
		t.Fatalf("coerce: %v", err)
	}
	if err := ApplyChange(cfg, "rules.visible_packages", coerced); err != nil {
		t.Fatalf("ApplyChange: %v", err)
	}
	if !reflect.DeepEqual(cfg.Rules.VisiblePackages, []string{"core", "prompt-injection"}) {
		t.Errorf("after apply = %v, want [core prompt-injection]", cfg.Rules.VisiblePackages)
	}
}
