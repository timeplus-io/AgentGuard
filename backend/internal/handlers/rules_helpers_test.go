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
