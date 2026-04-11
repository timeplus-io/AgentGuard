package handlers

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// ── Row-value helpers ─────────────────────────────────────────────────────────
// All helpers accept the raw `any` value from a tp.Row map entry so callers
// can write parseStr(row["key"]) without a helper that takes a map+key pair.

// parseStr safely casts any to string.
func parseStr(v any) string {
	s, _ := v.(string)
	return s
}

// parseInt handles json.Number / float64 / int from Proton rows.
func parseInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	case int:
		return n
	}
	return 0
}

// parseInt64 handles json.Number / float64 / int64 / uint64 from Proton rows.
func parseInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
		// Fallback for uint64 values that exceed int64 max (e.g., very large counters).
		if u, err := strconv.ParseUint(string(n), 10, 64); err == nil {
			return int64(u)
		}
	case int64:
		return n
	case uint64:
		return int64(n)
	}
	return 0
}

// parseFloat64 handles float64 / json.Number from Proton rows.
func parseFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case interface{ Float64() (float64, error) }:
		if f, err := n.Float64(); err == nil {
			return f
		}
	}
	return 0
}

// parseBool handles Proton's count_if(...) result (returned as 0/1) or native bool.
func parseBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return parseInt(v) != 0
}

// parseTime handles the multiple time formats Proton returns.
func parseTime(v any) time.Time {
	switch s := v.(type) {
	case string:
		if s == "" {
			return time.Time{}
		}
		// Proton returns timestamps without timezone: "2026-04-08 03:25:17.544"
		if t, err := time.ParseInLocation("2006-01-02 15:04:05.999999999", s, time.UTC); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", s); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	case float64:
		return time.Unix(int64(s), 0)
	case json.Number:
		if n, err := s.Float64(); err == nil {
			return time.Unix(int64(n), 0)
		}
	}
	return time.Time{}
}

// escapeSQL replaces single quotes to prevent SQL injection in string literals.
func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
