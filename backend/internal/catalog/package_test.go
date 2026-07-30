package catalog

import "testing"

func TestPackageLocalized(t *testing.T) {
	p := Package{
		ID:          "core",
		Name:        "Core Protection",
		Description: "Essential detections.",
		Translations: map[string]Translation{
			"zh-CN": {Name: "核心防护", Description: "基础检测。"},
		},
	}

	t.Run("known locale overlays name and description", func(t *testing.T) {
		got := p.Localized("zh-CN")
		if got.Name != "核心防护" {
			t.Errorf("Name = %q, want 核心防护", got.Name)
		}
		if got.Description != "基础检测。" {
			t.Errorf("Description = %q, want 基础检测。", got.Description)
		}
		if got.Translations != nil {
			t.Errorf("Translations not cleared: %v", got.Translations)
		}
	})

	t.Run("unknown locale returns base copy", func(t *testing.T) {
		got := p.Localized("fr")
		if got.Name != "Core Protection" || got.Description != "Essential detections." {
			t.Errorf("unknown locale changed base: %+v", got)
		}
	})

	t.Run("empty translated field falls back per-field", func(t *testing.T) {
		q := Package{
			ID:           "x",
			Name:         "Base Name",
			Description:  "Base Desc",
			Translations: map[string]Translation{"zh-CN": {Name: "只有名字"}},
		}
		got := q.Localized("zh-CN")
		if got.Name != "只有名字" {
			t.Errorf("Name = %q, want 只有名字", got.Name)
		}
		if got.Description != "Base Desc" {
			t.Errorf("Description = %q, want Base Desc (fallback)", got.Description)
		}
	})
}

func TestPackageValidate(t *testing.T) {
	valid := Package{ID: "core", Name: "Core Protection", Rules: []string{"rp-001"}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid package rejected: %v", err)
	}

	cases := []struct {
		name string
		pkg  Package
	}{
		{"empty id", Package{Name: "X", Rules: []string{"rp-001"}}},
		{"bad id chars", Package{ID: "core pack!", Name: "X", Rules: []string{"rp-001"}}},
		{"empty name", Package{ID: "core", Rules: []string{"rp-001"}}},
		{"no rules", Package{ID: "core", Name: "X"}},
		{"duplicate rule id", Package{ID: "core", Name: "X", Rules: []string{"rp-001", "rp-001"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.pkg.Validate(); err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}
