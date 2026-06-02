package supervisor

import (
	"strings"
	"testing"
)

func TestLogBuffer_KeepsLastNLines(t *testing.T) {
	lb := NewLogBuffer(3)
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		_, _ = lb.Write([]byte(s + "\n"))
	}
	got := lb.Lines()
	want := []string{"c", "d", "e"}
	if len(got) != len(want) {
		t.Fatalf("want %d lines, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: want %q got %q", i, want[i], got[i])
		}
	}
}

func TestLogBuffer_RedactsSecrets(t *testing.T) {
	lb := NewLogBuffer(10)
	_, _ = lb.Write([]byte("starting with password=hunter2 ok\n"))
	_, _ = lb.Write([]byte("license_key: ABCD-1234-EFGH-5678\n"))
	out := strings.Join(lb.Lines(), "\n")
	if strings.Contains(out, "hunter2") {
		t.Error("password value not redacted")
	}
	if strings.Contains(out, "ABCD-1234-EFGH-5678") {
		t.Error("license value not redacted")
	}
	if !strings.Contains(out, "password=") {
		t.Error("redaction should keep the key, mask the value")
	}
}

func TestLogBuffer_TailIsSizeCapped(t *testing.T) {
	lb := NewLogBuffer(100)
	for i := 0; i < 100; i++ {
		_, _ = lb.Write([]byte(strings.Repeat("x", 50) + "\n"))
	}
	if got := len(lb.Tail(200)); got > 200 {
		t.Errorf("Tail(200) returned %d bytes, want <= 200", got)
	}
}
