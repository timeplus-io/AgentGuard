package handlers

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseStr(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"string", "hello", "hello"},
		{"empty string", "", ""},
		{"nil", nil, ""},
		{"int (not a string)", 42, ""},
		{"float (not a string)", 3.14, ""},
		{"bool (not a string)", true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseStr(c.in); got != c.want {
				t.Errorf("parseStr(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestParseInt(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		// HTTP+JSON path
		{"float64", float64(42), 42},
		{"json.Number int", json.Number("42"), 42},
		{"json.Number negative", json.Number("-7"), -7},
		{"json.Number invalid", json.Number("not-a-number"), 0},
		{"int", 42, 42},
		// Native binary driver path
		{"int64", int64(42), 42},
		{"uint64", uint64(42), 42},
		{"int32", int32(42), 42},
		{"uint32", uint32(42), 42},
		{"int16", int16(42), 42},
		{"uint16", uint16(42), 42},
		{"int8", int8(42), 42},
		{"uint8", uint8(42), 42},
		// Fallback
		{"nil", nil, 0},
		{"string", "42", 0},
		{"bool", true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseInt(c.in); got != c.want {
				t.Errorf("parseInt(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestParseInt64(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int64
	}{
		{"float64", float64(42), 42},
		{"json.Number int", json.Number("42"), 42},
		// Largest uint64 value: parsed via the ParseUint fallback path, then
		// reinterpreted as int64 (wraps to -1). Documents the current behavior
		// — counter columns this large should be read with parseInt64 only when
		// you know they fit in int64; otherwise add a uint64 helper.
		{"json.Number max uint64 wraps", json.Number("18446744073709551615"), int64(-1)},
		{"int64", int64(42), 42},
		{"uint64", uint64(42), 42},
		// String case: Timeplus FORMAT JSON quotes 64-bit integers as strings.
		{"string decimal", "42", 42},
		{"string negative", "-7", -7},
		{"string bogus", "bogus", 0},
		{"nil", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseInt64(c.in); got != c.want {
				t.Errorf("parseInt64(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestParseFloat64(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want float64
	}{
		{"float64", 3.14, 3.14},
		{"float32", float32(2.5), 2.5},
		{"json.Number", json.Number("1.5"), 1.5},
		{"int", 7, 7.0},
		{"int64", int64(8), 8.0},
		{"uint64", uint64(9), 9.0},
		{"int32", int32(10), 10.0},
		{"uint32", uint32(11), 11.0},
		{"nil", nil, 0},
		{"string", "3.14", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseFloat64(c.in)
			// Use small tolerance for float32 → float64 conversion.
			if diff := got - c.want; diff > 1e-6 || diff < -1e-6 {
				t.Errorf("parseFloat64(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestParseBool(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want bool
	}{
		{"bool true", true, true},
		{"bool false", false, false},
		{"int 1 (count_if result)", 1, true},
		{"int 0 (count_if result)", 0, false},
		{"int64 1", int64(1), true},
		{"uint64 0", uint64(0), false},
		{"json.Number 1", json.Number("1"), true},
		{"json.Number 0", json.Number("0"), false},
		{"float64 1", float64(1), true},
		{"nil", nil, false},
		{"string", "true", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseBool(c.in); got != c.want {
				t.Errorf("parseBool(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestParseTime(t *testing.T) {
	expected := time.Date(2026, 4, 8, 3, 25, 17, 544_000_000, time.UTC)

	t.Run("native time.Time pass-through", func(t *testing.T) {
		// Native binary driver delivers time.Time directly.
		got := parseTime(expected)
		if !got.Equal(expected) {
			t.Errorf("parseTime(time.Time) = %v, want %v", got, expected)
		}
	})

	t.Run("native time.Time with non-UTC timezone is normalized to UTC", func(t *testing.T) {
		// The native driver on a UTC+8 server returns time.Time values in the
		// server's local timezone. parseTime must normalize to UTC so Go's JSON
		// encoder emits "Z" (not "+08:00"), which the frontend parses correctly.
		loc := time.FixedZone("UTC+8", 8*60*60)
		inUTCPlus8 := time.Date(2026, 5, 6, 10, 42, 11, 0, loc) // 10:42 local = 02:42 UTC
		got := parseTime(inUTCPlus8)
		if got.Location() != time.UTC {
			t.Errorf("parseTime(time.Time in UTC+8) location = %v, want UTC", got.Location())
		}
		wantUTC := time.Date(2026, 5, 6, 2, 42, 11, 0, time.UTC)
		if !got.Equal(wantUTC) {
			t.Errorf("parseTime(time.Time in UTC+8) = %v, want %v", got, wantUTC)
		}
	})

	t.Run("Proton string format (no timezone)", func(t *testing.T) {
		got := parseTime("2026-04-08 03:25:17.544")
		if !got.Equal(expected) {
			t.Errorf("parseTime(string) = %v, want %v", got, expected)
		}
	})

	t.Run("RFC3339Nano string", func(t *testing.T) {
		got := parseTime("2026-04-08T03:25:17.544Z")
		if !got.Equal(expected) {
			t.Errorf("parseTime(RFC3339) = %v, want %v", got, expected)
		}
	})

	t.Run("empty string returns zero", func(t *testing.T) {
		if !parseTime("").IsZero() {
			t.Error("parseTime(\"\") should return zero time")
		}
	})

	t.Run("nil returns zero", func(t *testing.T) {
		if !parseTime(nil).IsZero() {
			t.Error("parseTime(nil) should return zero time")
		}
	})

	t.Run("invalid string returns zero", func(t *testing.T) {
		if !parseTime("not a date").IsZero() {
			t.Error("parseTime(invalid) should return zero time")
		}
	})

	t.Run("unix timestamp from float64", func(t *testing.T) {
		got := parseTime(float64(1_700_000_000))
		want := time.Unix(1_700_000_000, 0)
		if !got.Equal(want) {
			t.Errorf("parseTime(float64) = %v, want %v", got, want)
		}
	})

	t.Run("unix timestamp from json.Number", func(t *testing.T) {
		got := parseTime(json.Number("1700000000"))
		want := time.Unix(1_700_000_000, 0)
		if !got.Equal(want) {
			t.Errorf("parseTime(json.Number) = %v, want %v", got, want)
		}
	})
}

func TestEscapeSQL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"no quotes", "hello world", "hello world"},
		{"single quote", "O'Reilly", "O''Reilly"},
		{"multiple quotes", "'a'b'c'", "''a''b''c''"},
		{"empty", "", ""},
		// escapeSQL is now backslash-safe (delegates to the canonical escaper):
		// backslashes are doubled so a trailing "\" can't swallow the closing
		// quote and desync the statement (#237).
		{"backslash doubled", `path\to\file`, `path\\to\\file`},
		{"trailing backslash + quote", `a\`, `a\\`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := escapeSQL(c.in); got != c.want {
				t.Errorf("escapeSQL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
