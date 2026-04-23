package catalog

import "testing"

// versionGreater here lives in catalog/import.go and uses strconv.Atoi.
// The handlers package has a sibling implementation (rules.go) that uses
// fmt.Sscanf — both are tested independently to guard against drift.
func TestVersionGreater(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{"equal", "1.0.0", "1.0.0", false},
		{"patch greater", "1.0.1", "1.0.0", true},
		{"patch lesser", "1.0.0", "1.0.1", false},
		{"minor greater", "1.1.0", "1.0.99", true},
		{"major greater", "2.0.0", "1.99.99", true},
		{"missing parts treated as 0", "1.2", "1.2.0", false},
		{"missing parts: 1.3 vs 1.2.99 → true", "1.3", "1.2.99", true},
		{"non-numeric parts treated as 0", "abc", "0.0.0", false},
		{"empty strings", "", "", false},
		// SplitN(_, _, 3) means a 4th component goes inside the third part:
		// "1.0.0.5" → ["1", "0", "0.5"]. Atoi("0.5") fails → 0. Equal to "1.0.0".
		{"4-part input — extra ignored", "1.0.0.5", "1.0.0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := versionGreater(c.a, c.b); got != c.want {
				t.Errorf("versionGreater(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}
