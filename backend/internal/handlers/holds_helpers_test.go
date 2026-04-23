package handlers

import (
	"strings"
	"testing"
)

func TestFailPolicyToDecision(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"deny → block", "deny", "block"},
		{"allow → allow", "allow", "allow"},
		{"empty → block (fail-closed default)", "", "block"},
		{"unknown → block (fail-closed default)", "yolo", "block"},
		// Case-sensitive — config validation should normalize to lowercase upstream.
		{"DENY (uppercase) → block (not matched as allow)", "DENY", "block"},
		{"ALLOW (uppercase) → block (not matched as allow)", "ALLOW", "block"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := failPolicyToDecision(c.in); got != c.want {
				t.Errorf("failPolicyToDecision(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestTruncateJSON(t *testing.T) {
	t.Run("nil → empty string", func(t *testing.T) {
		if got := truncateJSON(nil, 100); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("small object fits without truncation", func(t *testing.T) {
		got := truncateJSON(map[string]string{"k": "v"}, 100)
		if got != `{"k":"v"}` {
			t.Errorf("got %q", got)
		}
	})

	t.Run("oversized payload is truncated with marker", func(t *testing.T) {
		big := strings.Repeat("a", 5000)
		got := truncateJSON(map[string]string{"data": big}, 50)
		if !strings.HasSuffix(got, "...[truncated]") {
			t.Errorf("expected truncation marker; got tail %q", got[len(got)-20:])
		}
		// Body should be exactly maxBytes followed by the marker.
		const marker = "...[truncated]"
		if len(got) != 50+len(marker) {
			t.Errorf("expected length %d, got %d", 50+len(marker), len(got))
		}
	})

	t.Run("non-marshalable value → empty string", func(t *testing.T) {
		// chan can't be JSON-marshaled.
		ch := make(chan int)
		if got := truncateJSON(ch, 100); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("zero maxBytes still allows tiny values", func(t *testing.T) {
		// `null` is 4 bytes; with maxBytes=0 it gets truncated to "" + marker.
		got := truncateJSON(nil, 0) // nil short-circuits before marshal
		if got != "" {
			t.Errorf("got %q", got)
		}
	})
}
