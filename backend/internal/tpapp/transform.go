// Package tpapp renders the AgentGuard core pipeline as a Timeplus app
// (.tpapp, package format v1) from the same DDL constants timeplus.Init()
// executes. See docs/superpowers/specs/2026-09-10-tpapp-export-design.md.
package tpapp

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// DBPlaceholder is the installer's Go-template expression for the app database.
const DBPlaceholder = "{{ .DB }}"

var createHeadRe = regexp.MustCompile(`(?is)^(\s*CREATE\s+(?:MUTABLE\s+STREAM|STREAM|MATERIALIZED\s+VIEW|INPUT)\s+)(IF\s+NOT\s+EXISTS\s+)?`)

// EnsureIfNotExists inserts IF NOT EXISTS into a bare CREATE STREAM /
// CREATE MUTABLE STREAM / CREATE MATERIALIZED VIEW / CREATE INPUT head. App
// upgrades re-run every DDL file, so every CREATE must be re-runnable.
func EnsureIfNotExists(ddl string) (string, error) {
	loc := createHeadRe.FindStringSubmatchIndex(ddl)
	if loc == nil {
		return "", fmt.Errorf("unsupported CREATE head: %.60q", strings.TrimSpace(ddl))
	}
	if loc[4] != -1 { // IF NOT EXISTS already present
		return ddl, nil
	}
	return ddl[:loc[3]] + "IF NOT EXISTS " + ddl[loc[3]:], nil
}

// Qualify rewrites every whole-word occurrence of a known resource name as
// {{ .DB }}.name. Occurrences already preceded by "{{ .DB }}." are left
// alone, so the function is idempotent. It intentionally rewrites inside
// string literals and comments too: CREATE INPUT target streams are quoted
// ('otel_traces'), and a stream name never legitimately appears as data in
// this DDL.
func Qualify(sql string, names []string) string {
	// Longest first: \b already prevents partial matches, ordering is
	// belt-and-braces for names that are prefixes of other names.
	sorted := append([]string(nil), names...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, n := range sorted {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(n) + `\b`)
		sql = re.ReplaceAllStringFunc(sql, func(m string) string { return DBPlaceholder + "." + m })
	}
	// Collapse the double qualification produced by a name that was already
	// qualified in the input.
	return strings.ReplaceAll(sql, DBPlaceholder+"."+DBPlaceholder+".", DBPlaceholder+".")
}

var (
	otelPortRe = regexp.MustCompile(`(?m)^(\s*tcp_port\s*=\s*)\d+(\s*,?)`)
	otelHostRe = regexp.MustCompile(`(?m)^(\s*listen_host\s*=\s*)'[^']*'(\s*,?)`)
)

// TemplateOTelInput replaces the literal OTLP port and bind address in the
// agentguard_otel_input DDL with manifest config references.
func TemplateOTelInput(ddl string) (string, error) {
	if !otelPortRe.MatchString(ddl) || !otelHostRe.MatchString(ddl) {
		return "", fmt.Errorf("otel input DDL: tcp_port/listen_host settings not found")
	}
	ddl = otelPortRe.ReplaceAllString(ddl, "${1}{{ .Config.otel_port }}${2}")
	ddl = otelHostRe.ReplaceAllString(ddl, "${1}'{{ .Config.otel_listen_host }}'${2}")
	return ddl, nil
}

// StripTrailingSemicolon trims whitespace and one trailing ';' — the installer
// wants exactly one bare statement per file.
func StripTrailingSemicolon(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ";")
	return strings.TrimSpace(s)
}

// Transform applies EnsureIfNotExists, Qualify and StripTrailingSemicolon —
// the standard pipeline for a stream/MV/input DDL constant.
func Transform(ddl string, names []string) (string, error) {
	out, err := EnsureIfNotExists(ddl)
	if err != nil {
		return "", err
	}
	return StripTrailingSemicolon(Qualify(out, names)), nil
}
