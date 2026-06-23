package rules

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/timeplus/agentguard/internal/catalog"
)

// placeholderPrefix is the only namespace allowed inside {{...}}.
// The /apps authoring surface uses {{ .Config.<key> }} — whitespace around
// the inner expression is tolerated by the scanner (trimmed before prefix check).
const placeholderPrefix = ".Config."

// The strict syntax for the <key> part of a placeholder is
// catalog.ValidParameterID — the same rule catalog enforces on declared
// parameter keys — so the scanner rejects anything (whitespace, uppercase,
// punctuation) that could never be a declared key, even against a hand-built
// definition that skipped catalog validation, and the two rules cannot drift.

// placeholder is one {{ .Config.<key> }} occurrence in a template.
// Start/End are byte offsets of the full token including braces.
// ID holds the captured <key> value (field name kept as ID for internal use).
type placeholder struct {
	Start int
	End   int
	ID    string
}

// scanPlaceholders finds every `{{` occurrence in the template and parses it
// as a strict placeholder. The syntax is `{{ .Config.<key> }}` — surrounding
// whitespace inside the braces is tolerated (trimmed), but no other namespaces
// or template actions are accepted. Any `{{` that is not followed by a
// well-formed placeholder is an error, including a `{{` with no closing `}}`.
// Whether <key> is declared by the definition is the caller's concern.
func scanPlaceholders(template string) ([]placeholder, error) {
	var out []placeholder
	for i := 0; ; {
		j := strings.Index(template[i:], "{{")
		if j < 0 {
			return out, nil
		}
		start := i + j
		rest := template[start+2:]
		k := strings.Index(rest, "}}")
		if k < 0 {
			return nil, fmt.Errorf("sql_template: unclosed '{{' at byte offset %d", start)
		}
		inner := strings.TrimSpace(rest[:k])
		id, ok := strings.CutPrefix(inner, placeholderPrefix)
		if !ok || !catalog.ValidParameterID(id) {
			return nil, fmt.Errorf("sql_template: invalid placeholder %q at byte offset %d (expected {{ .Config.<key> }})", "{{"+rest[:k]+"}}", start)
		}
		out = append(out, placeholder{Start: start, End: start + 2 + k + 2, ID: id})
		i = start + 2 + k + 2
	}
}

// ValidateTemplate checks the definition's sql_template against its declared
// parameters: every placeholder must be well-formed and resolve to a declared
// parameter, and every declared parameter must appear at least once. Intended
// to run at definition-validation time as well as before rendering. Failures
// are defects of the definition and are reported as a *DefinitionError (kind
// DefinitionBadTemplate).
func ValidateTemplate(def catalog.Definition) error {
	if err := validateTemplate(def); err != nil {
		return &DefinitionError{Kind: DefinitionBadTemplate, Err: err}
	}
	return nil
}

// validateTemplate is ValidateTemplate without the *DefinitionError wrapping.
func validateTemplate(def catalog.Definition) error {
	phs, err := scanPlaceholders(def.SQLTemplate)
	if err != nil {
		return err
	}
	params := paramIndex(def)
	used := make(map[string]bool, len(params))
	for _, ph := range phs {
		if _, ok := params[ph.ID]; !ok {
			return fmt.Errorf("sql_template: placeholder {{ .Config.%s }} does not match any declared parameter", ph.ID)
		}
		used[ph.ID] = true
	}
	for _, p := range def.Parameters {
		if !used[p.Key] {
			return fmt.Errorf("sql_template: declared parameter %q is never used", p.Key)
		}
	}
	return nil
}

// escapeSQLString escapes a string for embedding in a Proton/ClickHouse
// single-quoted literal. Backslashes are escaped first because Proton
// interprets \' as an escaped single quote, which would terminate the string
// early if only the quote were doubled.
func escapeSQLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", "''")
	return s
}

// renderStringLiteral renders a single-quoted SQL string literal.
func renderStringLiteral(s string) string {
	return "'" + escapeSQLString(s) + "'"
}

// renderValue renders a canonical (normalized) value as a SQL fragment for
// the parameter's type.
func renderValue(p catalog.Parameter, v any) (string, error) {
	switch p.Type {
	case "integer":
		i, ok := v.(int64)
		if !ok {
			return "", fmt.Errorf("parameter %q: internal: expected int64, got %T", p.Key, v)
		}
		return strconv.FormatInt(i, 10), nil

	case "number":
		f, ok := v.(float64)
		if !ok {
			return "", fmt.Errorf("parameter %q: internal: expected float64, got %T", p.Key, v)
		}
		// normalizeValue already rejects non-finite numbers; re-check here for
		// symmetry with the other type assertions, since NaN/Inf have no SQL
		// literal form.
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", fmt.Errorf("parameter %q: internal: non-finite number cannot be rendered", p.Key)
		}
		// 'f' format: canonical decimal, never exponent notation in SQL.
		return strconv.FormatFloat(f, 'f', -1, 64), nil

	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return "", fmt.Errorf("parameter %q: internal: expected bool, got %T", p.Key, v)
		}
		if b {
			return "true", nil
		}
		return "false", nil

	case "string", "enum":
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("parameter %q: internal: expected string, got %T", p.Key, v)
		}
		return renderStringLiteral(s), nil

	case "string_list":
		items, ok := v.([]string)
		if !ok {
			return "", fmt.Errorf("parameter %q: internal: expected []string, got %T", p.Key, v)
		}
		if len(items) == 0 {
			return "", fmt.Errorf("parameter %q: internal: empty list cannot be rendered", p.Key)
		}
		parts := make([]string, len(items))
		for i, item := range items {
			parts[i] = renderStringLiteral(item)
		}
		return "(" + strings.Join(parts, ", ") + ")", nil

	// Phase B types land here:
	// case "regex":
	// case "duration":

	default:
		return "", fmt.Errorf("parameter %q: unsupported parameter type %q", p.Key, p.Type)
	}
}

// renderTemplate substitutes every placeholder with the rendered value from
// the (normalized, complete) value map. A template with no placeholders is
// returned byte-identical.
func renderTemplate(def catalog.Definition, values map[string]any) (string, error) {
	phs, err := scanPlaceholders(def.SQLTemplate)
	if err != nil {
		return "", err
	}
	if len(phs) == 0 {
		return def.SQLTemplate, nil
	}
	params := paramIndex(def)
	var b strings.Builder
	prev := 0
	for _, ph := range phs {
		p, ok := params[ph.ID]
		if !ok {
			return "", fmt.Errorf("sql_template: placeholder {{ .Config.%s }} does not match any declared parameter", ph.ID)
		}
		v, ok := values[ph.ID]
		if !ok {
			return "", fmt.Errorf("parameter %q: no value available for rendering", ph.ID)
		}
		frag, err := renderValue(p, v)
		if err != nil {
			return "", err
		}
		b.WriteString(def.SQLTemplate[prev:ph.Start])
		b.WriteString(frag)
		prev = ph.End
	}
	b.WriteString(def.SQLTemplate[prev:])
	return b.String(), nil
}

// Compile is the top-level entry point of the parameter compiler. It
// validates the template against the declared parameters, normalizes the
// defaults and the supplied overrides (decoded JSON values keyed by
// parameter ID), resolves effective values, renders the SQL, and returns the
// canonical persisted overrides (entries equal to the current default are
// dropped) together with the definition hash.
//
// Validation failures of the override values are returned as a
// *ValidationError carrying per-parameter field errors. Defects of the
// definition itself — a malformed template or a default that fails its own
// constraints — are returned as a *DefinitionError instead, so handlers can
// map the two to different HTTP statuses with errors.As.
func Compile(def catalog.Definition, overrides map[string]any) (Compiled, error) {
	if err := ValidateTemplate(def); err != nil {
		return Compiled{}, err
	}
	defaults, err := normalizedDefaults(def)
	if err != nil {
		return Compiled{}, err
	}
	norm, err := NormalizeOverrides(def, overrides)
	if err != nil {
		return Compiled{}, err
	}
	effective := effectiveValues(defaults, norm)
	sql, err := renderTemplate(def, effective)
	if err != nil {
		return Compiled{}, err
	}
	return Compiled{
		Overrides:      canonicalOverrides(defaults, norm),
		Effective:      effective,
		SQL:            sql,
		DefinitionHash: def.Hash(),
	}, nil
}
