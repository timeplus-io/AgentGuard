package handlers

import (
	"testing"
	"testing/fstest"

	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/models"
)

func TestLocalizeThreatRuleNames(t *testing.T) {
	seed := fstest.MapFS{
		"rp-900.yaml": {Data: []byte(
			"id: rp-900\n" +
				"name: Base Rule\n" +
				"version: 1.0.0\n" +
				"description: d\n" +
				"sql_template: \"SELECT 1\"\n" +
				"translations:\n" +
				"  zh-CN:\n" +
				"    name: 中文规则\n")},
		"rp-901.yaml": {Data: []byte(
			"id: rp-901\n" +
				"name: Untranslated Rule\n" +
				"version: 1.0.0\n" +
				"description: d\n" +
				"sql_template: \"SELECT 1\"\n")},
	}
	mgr, err := catalog.New(seed, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New: %v", err)
	}

	base := []models.Threat{
		{RuleID: "rp-900", RuleName: "Base Rule"},         // builtin with a zh-CN translation
		{RuleID: "rp-901", RuleName: "Untranslated Rule"}, // builtin, no translation
		{RuleID: "rp-nope", RuleName: "Ghost Rule"},       // not in the catalog
	}

	// zh-CN: only the translated rule's name changes; others keep the baked name.
	zh := append([]models.Threat(nil), base...)
	localizeThreatRuleNames(mgr, "zh-CN", zh)
	if zh[0].RuleName != "中文规则" {
		t.Errorf("rp-900 name = %q, want 中文规则", zh[0].RuleName)
	}
	if zh[1].RuleName != "Untranslated Rule" {
		t.Errorf("rp-901 name = %q, want baked English preserved", zh[1].RuleName)
	}
	if zh[2].RuleName != "Ghost Rule" {
		t.Errorf("rp-nope name = %q, want baked name preserved (not in catalog)", zh[2].RuleName)
	}

	// en / unknown locale: nothing changes.
	en := append([]models.Threat(nil), base...)
	localizeThreatRuleNames(mgr, "en", en)
	if en[0].RuleName != "Base Rule" {
		t.Errorf("en rp-900 name = %q, want baked English unchanged", en[0].RuleName)
	}
}

func TestShouldAutoReopenThreat(t *testing.T) {
	tests := []struct {
		name            string
		status          string
		statusUpdatedAt string
		lastSeen        string
		want            bool
	}{
		{
			name:            "cleared threat reopens on later detection",
			status:          "cleared",
			statusUpdatedAt: "2026-05-13 09:49:36.146",
			lastSeen:        "2026-05-13 09:49:37.679",
			want:            true,
		},
		{
			name:            "acknowledged threat reopens on later detection",
			status:          "acknowledged",
			statusUpdatedAt: "2026-05-13 09:49:36.146",
			lastSeen:        "2026-05-13 09:49:37.679",
			want:            true,
		},
		{
			name:            "cleared threat stays cleared without later detection",
			status:          "cleared",
			statusUpdatedAt: "2026-05-13 09:49:37.679",
			lastSeen:        "2026-05-13 09:49:36.146",
			want:            false,
		},
		{
			name:            "acknowledged threat stays acknowledged without later detection",
			status:          "acknowledged",
			statusUpdatedAt: "2026-05-13 09:49:37.679",
			lastSeen:        "2026-05-13 09:49:36.146",
			want:            false,
		},
		{
			name:            "open threat is already open",
			status:          "open",
			statusUpdatedAt: "2026-05-13 09:49:36.146",
			lastSeen:        "2026-05-13 09:49:37.679",
			want:            false,
		},
		{
			name:            "brand-new threat is handled by empty-status path",
			status:          "",
			statusUpdatedAt: "1970-01-01 00:00:00.000",
			lastSeen:        "2026-05-13 09:49:37.679",
			want:            false,
		},
		{
			name:            "invalid timestamp does not reopen",
			status:          "cleared",
			statusUpdatedAt: "not-a-time",
			lastSeen:        "2026-05-13 09:49:37.679",
			want:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAutoReopenThreat(tt.status, tt.statusUpdatedAt, tt.lastSeen)
			if got != tt.want {
				t.Fatalf("shouldAutoReopenThreat(%q, %q, %q) = %v, want %v",
					tt.status, tt.statusUpdatedAt, tt.lastSeen, got, tt.want)
			}
		})
	}
}
