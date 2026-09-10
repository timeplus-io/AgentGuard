package config

import "testing"

func TestTimeplusDatabaseDefaultAndValidation(t *testing.T) {
	cfg := &Config{}
	if err := ApplyChange(cfg, "timeplus.database", "ag"); err != nil {
		t.Fatal(err)
	}
	if cfg.Timeplus.Database != "ag" {
		t.Errorf("got %q", cfg.Timeplus.Database)
	}
	if v := EffectiveValue(cfg, "timeplus.database"); v != "ag" {
		t.Errorf("EffectiveValue = %v", v)
	}
	for _, bad := range []string{"Ag", "1ag", "a-g", "a b", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if _, err := CoerceAndValidate("timeplus.database", bad); err == nil {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
	if v, err := CoerceAndValidate("timeplus.database", ""); err != nil || v != "default" {
		t.Errorf("empty should coerce to default, got %v %v", v, err)
	}
	if v, err := CoerceAndValidate("timeplus.database", "  ag  "); err != nil || v != "ag" {
		t.Errorf("whitespace should be trimmed, got %v %v", v, err)
	}
	found := false
	for _, k := range Registry() {
		if k.Key == "timeplus.database" {
			found = true
			if k.EnvVar != "TIMEPLUS_DATABASE" || k.FlagName != "timeplus-database" || k.Section != "timeplus" || k.Type != TypeString {
				t.Errorf("registry entry: %+v", k)
			}
		}
	}
	if !found {
		t.Error("timeplus.database missing from Registry()")
	}
}
