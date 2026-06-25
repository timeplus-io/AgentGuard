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

// TestBakeRuleIdentity covers the create-custom-rule sentinel substitution. The
// rule name is baked into the SQL via the '__rule_name__' sentinel; because the
// template placeholder scanner treats every '{{' as a placeholder (with no
// literal-brace escape), a name carrying template delimiters would otherwise be
// rejected later with a cryptic "unclosed '{{'" error. bakeRuleIdentity rejects
// it up front with a clear message — but only when the name is actually baked.
func TestBakeRuleIdentity(t *testing.T) {
	t.Run("rejects template delimiters in a baked name", func(t *testing.T) {
		if _, err := bakeRuleIdentity("SELECT '__rule_name__'", "custom-1", "Block {{ .Config.x }} traffic"); err == nil {
			t.Fatal("expected an error for a name with '{{' baked via __rule_name__")
		}
	})

	t.Run("rejects a closing brace too", func(t *testing.T) {
		if _, err := bakeRuleIdentity("WHERE label = '__rule_name__'", "custom-1", "weird }} name"); err == nil {
			t.Fatal("expected an error for a name with '}}'")
		}
	})

	t.Run("allows braces when the name is not baked", func(t *testing.T) {
		got, err := bakeRuleIdentity("SELECT '__rule_id__'", "custom-1", "Has {{ braces")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "SELECT 'custom-1'" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("substitutes id and SQL-escapes the name", func(t *testing.T) {
		got, err := bakeRuleIdentity("SELECT '__rule_id__', '__rule_name__'", "custom-1", "O'Reilly rule")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "SELECT 'custom-1', 'O''Reilly rule'"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
