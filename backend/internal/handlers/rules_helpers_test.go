package handlers

import "testing"

func TestEscapeSQLStr(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no special chars", "hello", "hello"},
		{"single quote", "O'Reilly", "O''Reilly"},
		// escapeSQLStr (unlike escapeSQL) also escapes backslashes — required for
		// values that flow into SQL string literals containing backslash sequences
		// (e.g. JSON-encoded payloads stored in event_data).
		{"backslash", `path\to\file`, `path\\to\\file`},
		{"both backslash and quote", `it's a \path`, `it''s a \\path`},
		// Backslash-then-quote: backslash gets doubled FIRST (because the
		// implementation applies the backslash replace before the quote replace),
		// then the original quote becomes ''. So `\'` → `\\` then `\\''` (4 chars).
		{"backslash before quote", `\'`, `\\''`},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := escapeSQLStr(c.in); got != c.want {
				t.Errorf("escapeSQLStr(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

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
		{"minor greater", "1.1.0", "1.0.5", true},
		{"major greater", "2.0.0", "1.99.99", true},
		{"empty parts treated as 0", "1.0", "1.0.0", false},
		{"missing parts: 1.2 vs 1.2.1 → false", "1.2", "1.2.1", false},
		{"missing parts: 1.3 vs 1.2.99 → true", "1.3", "1.2.99", true},
		{"non-numeric parts treated as 0", "abc", "0.0.0", false},
		{"empty strings", "", "", false},
		// Both sides truncated to 3 parts; trailing components ignored.
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
