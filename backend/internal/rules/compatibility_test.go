package rules

import (
	"reflect"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/catalog"
)

func defV(version string, params ...catalog.Parameter) catalog.Definition {
	d := defWith(params...)
	d.Version = version
	return d
}

// checkAccounting asserts the "no silent reset" property: every input
// override produced exactly one outcome (preserved, warned, or conflicted).
func checkAccounting(t *testing.T, in map[string]any, res CompatibilityResult) {
	t.Helper()
	got := len(res.Overrides) + len(res.Warnings) + len(res.Conflicts)
	if got != len(in) {
		t.Fatalf("accounting violated: %d inputs but %d outcomes (overrides=%v warnings=%v conflicts=%v)",
			len(in), got, res.Overrides, res.Warnings, res.Conflicts)
	}
}

func TestPlanUpgradePreservesCompatibleOverride(t *testing.T) {
	cur := defV("1.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10, Max: fptr(100)})
	next := defV("2.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 20, Max: fptr(100)})
	in := map[string]any{"n": int64(42)}

	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if !res.OK() {
		t.Fatalf("expected OK, got conflicts %v", res.Conflicts)
	}
	if !reflect.DeepEqual(res.Overrides, map[string]any{"n": int64(42)}) {
		t.Fatalf("Overrides = %#v", res.Overrides)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("Warnings = %v", res.Warnings)
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeAcceptsRawJSONOverrideValues(t *testing.T) {
	// Persisted overrides may round-trip through JSON (int64 → float64);
	// PlanUpgrade must normalize them, not require canonical Go types.
	cur := defV("1.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10})
	next := defV("2.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10})
	res, err := PlanUpgrade(cur, next, map[string]any{"n": float64(42)})
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if !reflect.DeepEqual(res.Overrides, map[string]any{"n": int64(42)}) {
		t.Fatalf("Overrides = %#v, want normalized int64", res.Overrides)
	}
}

func TestPlanUpgradeNewParameterTakesDefault(t *testing.T) {
	cur := defV("1.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10})
	next := defV("2.0.0",
		catalog.Parameter{ID: "n", Type: "integer", Default: 10},
		catalog.Parameter{ID: "extra", Type: "string", Default: "x"},
	)
	in := map[string]any{"n": int64(5)}
	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if _, ok := res.Overrides["extra"]; ok {
		t.Fatal("new parameter must take its default, not appear as an override")
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeRemovedWithoutAliasWarns(t *testing.T) {
	cur := defV("1.0.0",
		catalog.Parameter{ID: "n", Type: "integer", Default: 10},
		catalog.Parameter{ID: "gone", Type: "string", Default: "x"},
	)
	next := defV("2.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10})
	in := map[string]any{"gone": "custom", "n": int64(5)}

	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if !res.OK() {
		t.Fatalf("removal without alias must not block: %v", res.Conflicts)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Parameter != "gone" {
		t.Fatalf("Warnings = %v", res.Warnings)
	}
	if !strings.Contains(res.Warnings[0].Message, "removed in version 2.0.0") {
		t.Fatalf("warning message = %q", res.Warnings[0].Message)
	}
	if !reflect.DeepEqual(res.Overrides, map[string]any{"n": int64(5)}) {
		t.Fatalf("Overrides = %#v", res.Overrides)
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeAliasMigratesOverride(t *testing.T) {
	cur := defV("1.0.0", catalog.Parameter{ID: "old_name", Type: "string", Default: "x"})
	next := defV("2.0.0", catalog.Parameter{ID: "new_name", Type: "string", Default: "x", Aliases: []string{"old_name"}})
	in := map[string]any{"old_name": "custom"}

	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if !res.OK() {
		t.Fatalf("conflicts: %v", res.Conflicts)
	}
	if !reflect.DeepEqual(res.Overrides, map[string]any{"new_name": "custom"}) {
		t.Fatalf("Overrides = %#v, want migrated under new_name", res.Overrides)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("Warnings = %v", res.Warnings)
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeAliasTypeMismatchConflicts(t *testing.T) {
	cur := defV("1.0.0", catalog.Parameter{ID: "old_name", Type: "string", Default: "5"})
	next := defV("2.0.0", catalog.Parameter{ID: "new_name", Type: "integer", Default: 5, Aliases: []string{"old_name"}})
	in := map[string]any{"old_name": "7"}

	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if res.OK() {
		t.Fatal("type mismatch through alias must conflict")
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Parameter != "old_name" {
		t.Fatalf("Conflicts = %v", res.Conflicts)
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeAliasConstraintViolationConflicts(t *testing.T) {
	cur := defV("1.0.0", catalog.Parameter{ID: "old_name", Type: "integer", Default: 10})
	next := defV("2.0.0", catalog.Parameter{ID: "new_name", Type: "integer", Default: 10, Max: fptr(50), Aliases: []string{"old_name"}})
	in := map[string]any{"old_name": int64(500)}

	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if res.OK() {
		t.Fatal("constraint violation through alias must conflict")
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeAliasAmbiguityConflicts(t *testing.T) {
	cur := defV("1.0.0", catalog.Parameter{ID: "old_name", Type: "string", Default: "x"})
	// Two next parameters both claim the alias — PlanUpgrade must not guess,
	// even though catalog validation would normally reject this definition.
	next := defV("2.0.0",
		catalog.Parameter{ID: "a", Type: "string", Default: "x", Aliases: []string{"old_name"}},
		catalog.Parameter{ID: "b", Type: "string", Default: "x", Aliases: []string{"old_name"}},
	)
	in := map[string]any{"old_name": "custom"}

	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if res.OK() {
		t.Fatal("ambiguous alias must conflict")
	}
	if len(res.Conflicts) != 1 || !strings.Contains(res.Conflicts[0].Message, "ambiguous") {
		t.Fatalf("Conflicts = %v", res.Conflicts)
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeTypeChangeConflicts(t *testing.T) {
	cur := defV("1.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10})
	next := defV("2.0.0", catalog.Parameter{ID: "n", Type: "string", Default: "10"})
	in := map[string]any{"n": int64(42)}

	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if res.OK() {
		t.Fatal("type change must conflict")
	}
	if !strings.Contains(res.Conflicts[0].Message, `type changed from "integer" to "string"`) {
		t.Fatalf("Conflicts = %v", res.Conflicts)
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeConstraintTighteningConflicts(t *testing.T) {
	cur := defV("1.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10, Max: fptr(1000)})
	next := defV("2.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10, Max: fptr(50)})
	in := map[string]any{"n": int64(500)}

	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if res.OK() {
		t.Fatal("override violating tightened constraint must conflict")
	}
	if !strings.Contains(res.Conflicts[0].Message, "no longer valid") {
		t.Fatalf("Conflicts = %v", res.Conflicts)
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeEnumOptionNarrowingConflicts(t *testing.T) {
	cur := defV("1.0.0", catalog.Parameter{ID: "mode", Type: "enum", Default: "warn", Options: []string{"warn", "block", "hold"}})
	next := defV("2.0.0", catalog.Parameter{ID: "mode", Type: "enum", Default: "warn", Options: []string{"warn", "block"}})
	in := map[string]any{"mode": "hold"}

	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if res.OK() {
		t.Fatal("removed enum option must conflict")
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeStaleOverrideWarns(t *testing.T) {
	cur := defV("1.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10})
	next := defV("2.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10})
	in := map[string]any{"phantom": "x"}

	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if !res.OK() {
		t.Fatalf("stale override must warn, not block: %v", res.Conflicts)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Parameter != "phantom" {
		t.Fatalf("Warnings = %v", res.Warnings)
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeDoubleMappingConflicts(t *testing.T) {
	// Both a direct override for the surviving "n" and an aliased override
	// for the removed "old_n" target the same next parameter; only one can
	// win and the other must be a conflict, never a silent overwrite.
	cur := defV("1.0.0",
		catalog.Parameter{ID: "n", Type: "integer", Default: 10},
		catalog.Parameter{ID: "old_n", Type: "integer", Default: 10},
	)
	next := defV("2.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10, Aliases: []string{"old_n"}})
	in := map[string]any{"n": int64(1), "old_n": int64(2)}

	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	if res.OK() {
		t.Fatal("two overrides mapping to one parameter must conflict")
	}
	if len(res.Overrides) != 1 || len(res.Conflicts) != 1 {
		t.Fatalf("Overrides = %v, Conflicts = %v", res.Overrides, res.Conflicts)
	}
	checkAccounting(t, in, res)
}

func TestPlanUpgradeIDMismatchIsError(t *testing.T) {
	cur := defV("1.0.0")
	next := defV("2.0.0")
	next.ID = "rp_other"
	if _, err := PlanUpgrade(cur, next, nil); err == nil {
		t.Fatal("definition ID mismatch must be an error")
	}
}

func TestPlanUpgradeEmptyOverrides(t *testing.T) {
	cur := defV("1.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 10})
	next := defV("2.0.0", catalog.Parameter{ID: "n", Type: "integer", Default: 20})
	for _, in := range []map[string]any{nil, {}} {
		res, err := PlanUpgrade(cur, next, in)
		if err != nil {
			t.Fatalf("PlanUpgrade: %v", err)
		}
		if !res.OK() || len(res.Overrides) != 0 || len(res.Warnings) != 0 {
			t.Fatalf("empty input must yield empty plan: %+v", res)
		}
	}
}

func TestPlanUpgradeAccountingAcrossMixedOutcomes(t *testing.T) {
	// One of each outcome in a single plan: preserved, alias-migrated,
	// warned (removed), warned (stale), conflicted (type change).
	cur := defV("1.0.0",
		catalog.Parameter{ID: "keep", Type: "integer", Default: 1},
		catalog.Parameter{ID: "old", Type: "string", Default: "x"},
		catalog.Parameter{ID: "dead", Type: "boolean", Default: false},
		catalog.Parameter{ID: "morph", Type: "integer", Default: 1},
	)
	next := defV("2.0.0",
		catalog.Parameter{ID: "keep", Type: "integer", Default: 1},
		catalog.Parameter{ID: "renamed", Type: "string", Default: "x", Aliases: []string{"old"}},
		catalog.Parameter{ID: "morph", Type: "string", Default: "1"},
	)
	in := map[string]any{
		"keep":    int64(9),
		"old":     "custom",
		"dead":    true,
		"morph":   int64(3),
		"phantom": "stale",
	}
	res, err := PlanUpgrade(cur, next, in)
	if err != nil {
		t.Fatalf("PlanUpgrade: %v", err)
	}
	wantOverrides := map[string]any{"keep": int64(9), "renamed": "custom"}
	if !reflect.DeepEqual(res.Overrides, wantOverrides) {
		t.Fatalf("Overrides = %#v, want %#v", res.Overrides, wantOverrides)
	}
	if len(res.Warnings) != 2 {
		t.Fatalf("Warnings = %v", res.Warnings)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Parameter != "morph" {
		t.Fatalf("Conflicts = %v", res.Conflicts)
	}
	checkAccounting(t, in, res)
}
