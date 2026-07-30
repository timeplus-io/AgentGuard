package handlers

import (
	"reflect"
	"testing"

	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/rules"
)

func TestPlanPackageEnable(t *testing.T) {
	members := []string{"rp-001", "rp-002", "rp-003", "rp-gone"}
	installed := map[string]bool{"rp-001": true}
	exists := func(id string) bool { return id != "rp-gone" }

	plan := planPackageEnable(members, installed, exists)

	if !reflect.DeepEqual(plan.ToInstall, []string{"rp-002", "rp-003"}) {
		t.Errorf("ToInstall = %v, want [rp-002 rp-003]", plan.ToInstall)
	}
	if !reflect.DeepEqual(plan.Skipped, []string{"rp-001"}) {
		t.Errorf("Skipped = %v, want [rp-001]", plan.Skipped)
	}
	if !reflect.DeepEqual(plan.Missing, []string{"rp-gone"}) {
		t.Errorf("Missing = %v, want [rp-gone]", plan.Missing)
	}
}

func TestPackageStatusCounts(t *testing.T) {
	pkg := catalog.Package{ID: "core", Rules: []string{"rp-001", "rp-002", "rp-003"}}
	installed := map[string]rules.RuleInstance{
		"rp-001": {RuleID: "rp-001", Enabled: true},
		"rp-002": {RuleID: "rp-002", Enabled: false},
		// rp-003 not installed
		"rp-999": {RuleID: "rp-999", Enabled: true}, // unrelated, must not count
	}

	total, installedCount, enabledCount := packageStatusCounts(pkg, installed)

	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if installedCount != 2 {
		t.Errorf("installedCount = %d, want 2", installedCount)
	}
	if enabledCount != 1 {
		t.Errorf("enabledCount = %d, want 1", enabledCount)
	}
}
