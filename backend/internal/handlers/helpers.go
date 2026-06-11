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
// Also handles int64/uint64/int32/uint32 emitted by the native driver.
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
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case int32:
		return int(n)
	case uint32:
		return int(n)
	case int16:
		return int(n)
	case uint16:
		return int(n)
	case int8:
		return int(n)
	case uint8:
		return int(n)
	}
	return 0
}

// parseInt64 handles json.Number / float64 / int64 / uint64 / string from Proton rows.
// The string case covers FORMAT JSON responses where Timeplus quotes 64-bit integers.
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
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	case int64:
		return n
	case uint64:
		return int64(n)
	}
	return 0
}

// parseFloat64 handles float64 / json.Number from Proton rows.
// Also handles float32 and integer types from the native driver.
func parseFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case interface{ Float64() (float64, error) }:
		if f, err := n.Float64(); err == nil {
			return f
		}
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	case int32:
		return float64(n)
	case uint32:
		return float64(n)
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
// HTTP+JSON path delivers strings; the native binary driver delivers time.Time.
func parseTime(v any) time.Time {
	switch s := v.(type) {
	case time.Time:
		return s.UTC()
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
