package sentry

import (
	"strings"
	"testing"

	tp "github.com/timeplus/agentguard/internal/timeplus"
)

func TestTruncateTitle(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{"empty", "", ""},
		{"short", "hello", "hello"},
		{"exactly 80", strings.Repeat("a", 80), strings.Repeat("a", 80)},
		{"81 runes truncates to 80", strings.Repeat("a", 81), strings.Repeat("a", 80)},
		{"multibyte runes counted not bytes", strings.Repeat("界", 81), strings.Repeat("界", 80)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateTitle(tc.title)
			if got != tc.want {
				t.Fatalf("truncateTitle(%d runes) = %d runes, want %d runes", len([]rune(tc.title)), len([]rune(got)), len([]rune(tc.want)))
			}
		})
	}
}

func TestNextSeqFromRows(t *testing.T) {
	cases := []struct {
		name string
		rows []tp.Row
		want uint32
	}{
		{"no rows", []tp.Row{}, 1},
		{"single row nil max_seq", []tp.Row{{"max_seq": nil}}, 1},
		{"single row missing key", []tp.Row{{}}, 1},
		{"single row float64 max_seq", []tp.Row{{"max_seq": float64(5)}}, 6},
		{"single row int max_seq", []tp.Row{{"max_seq": 41}}, 42},
		{"single row uint32 max_seq", []tp.Row{{"max_seq": uint32(0)}}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextSeqFromRows(tc.rows)
			if got != tc.want {
				t.Fatalf("nextSeqFromRows(%v) = %d, want %d", tc.rows, got, tc.want)
			}
		})
	}
}

func TestConversationFromRow(t *testing.T) {
	row := tp.Row{
		"conversation_id": "abc123",
		"user":            "alice",
		"title":           "Investigate threat",
		"status":          "active",
		"created_at":      "2026-07-21 10:00:00.000",
		"updated_at":      "2026-07-21 10:05:00.000",
	}
	got := conversationFromRow(row)
	if got.ConversationID != "abc123" || got.User != "alice" || got.Title != "Investigate threat" ||
		got.Status != "active" || got.CreatedAt != "2026-07-21 10:00:00.000" || got.UpdatedAt != "2026-07-21 10:05:00.000" {
		t.Fatalf("conversationFromRow mapped incorrectly: %+v", got)
	}
}

func TestConversationFromRowHandlesNilFields(t *testing.T) {
	row := tp.Row{"conversation_id": "abc123"}
	got := conversationFromRow(row)
	if got.ConversationID != "abc123" || got.User != "" || got.Title != "" {
		t.Fatalf("conversationFromRow with missing fields = %+v, want zero-value strings", got)
	}
}

func TestMessageFromRow(t *testing.T) {
	row := tp.Row{
		"event_time":      "2026-07-21 10:00:00.000",
		"conversation_id": "abc123",
		"seq":             uint32(3),
		"role":            "assistant",
		"content":         "hello there",
		"tool_name":       "get_threats",
		"tool_args":       `{"limit":5}`,
		"tool_result":     `{"count":0}`,
		"model":           "gpt-4o",
		"tokens_in":       float64(120),
		"tokens_out":      float64(48),
	}
	got := messageFromRow(row)
	want := struct {
		Seq       uint32
		TokensIn  uint32
		TokensOut uint32
	}{3, 120, 48}
	if got.ConversationID != "abc123" || got.Role != "assistant" || got.Content != "hello there" ||
		got.ToolName != "get_threats" || got.Model != "gpt-4o" ||
		got.Seq != want.Seq || got.TokensIn != want.TokensIn || got.TokensOut != want.TokensOut {
		t.Fatalf("messageFromRow mapped incorrectly: %+v", got)
	}
}

func TestMessageFromRowHandlesMissingOptionalFields(t *testing.T) {
	row := tp.Row{
		"event_time":      "2026-07-21 10:00:00.000",
		"conversation_id": "abc123",
		"seq":             1,
		"role":            "user",
		"content":         "hi",
	}
	got := messageFromRow(row)
	if got.ToolName != "" || got.ToolArgs != "" || got.ToolResult != "" || got.Model != "" || got.TokensIn != 0 || got.TokensOut != 0 {
		t.Fatalf("messageFromRow with missing optional fields = %+v, want zero values", got)
	}
}

func TestParseAndFormatSentryTimeRoundTrip(t *testing.T) {
	s := "2026-07-21 10:05:30.123"
	got := parseSentryTime(s)
	formatted := formatSentryTime(got)
	if formatted != s {
		t.Fatalf("parseSentryTime/formatSentryTime round trip: got %q, want %q", formatted, s)
	}
}

func TestParseSentryTimeFallsBackOnEmpty(t *testing.T) {
	got := parseSentryTime("")
	if got.IsZero() {
		t.Fatalf("parseSentryTime(\"\") should fall back to a non-zero time (now), got zero value")
	}
}
