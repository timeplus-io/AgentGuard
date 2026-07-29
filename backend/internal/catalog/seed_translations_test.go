package catalog

import "testing"

// TestSeedRulesHaveChineseTranslations asserts every builtin (embedded seed)
// rule carries a non-empty zh-CN name and description. It is the definition of
// done for the translation content pass and guards against regressions where a
// new seed rule is added without a translation.
func TestSeedRulesHaveChineseTranslations(t *testing.T) {
	m, err := New(SeedFS, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New(SeedFS): %v", err)
	}
	var missing []string
	for _, d := range m.All() {
		if d.Origin != OriginBuiltin {
			continue
		}
		tr, ok := d.TranslationFor("zh-CN")
		switch {
		case !ok:
			missing = append(missing, d.ID+" (no zh-CN block)")
		case tr.Name == "":
			missing = append(missing, d.ID+" (empty zh-CN name)")
		case tr.Description == "":
			missing = append(missing, d.ID+" (empty zh-CN description)")
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d builtin rules missing zh-CN translation:\n%v", len(missing), missing)
	}
}
