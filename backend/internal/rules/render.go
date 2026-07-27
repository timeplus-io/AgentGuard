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

// Identity sentinels. The catalog authoring surface ships these literals in its
// default sql_template because a template is written before its rule id/name are
// known; Compile substitutes them (from the definition's own id/name) so a
// created/installed MV writes correctly-attributed detection rows. They are
// quoted so they read as ordinary SQL string literals in the author-facing
// template and never appear inside a {{ }} placeholder.
const (
	ruleIDSentinel   = "'__rule_id__'"
	ruleNameSentinel = "'__rule_name__'"
)

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

// EscapeSQLString escapes a string for embedding in a Proton/ClickHouse
// single-quoted literal. Backslashes are escaped first because Proton
// interprets \' as an escaped single quote, which would terminate the string
// early if only the quote were doubled.
//
// This is the canonical full escaper (quotes + backslashes); the handlers
// package's escapeSQLStr — and, through it, handlers.escapeSQL — delegate here
// so all three stay backslash-safe and never drift.
func EscapeSQLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "'", "''")
	return s
}

// renderStringLiteral renders a single-quoted SQL string literal.
func renderStringLiteral(s string) string {
	return "'" + EscapeSQLString(s) + "'"
}

// renderValue renders a canonical (normalized) value as a SQL fragment for
// the parameter's type.
func renderValue(p catalog.Parameter, v any) (string, error) {
	switch p.Type {
	// duration renders identically to integer (the unit lives in the template).
	case "integer", "duration":
		i, ok := v.(int64)
		if !ok {
			return "", fmt.Errorf("parameter %q: internal: expected int64, got %T", p.Key, v)
		}
		return strconv.FormatInt(i, 10), nil

	case "float":
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

	case "bool":
		b, ok := v.(bool)
		if !ok {
			return "", fmt.Errorf("parameter %q: internal: expected bool, got %T", p.Key, v)
		}
		if b {
			return "true", nil
		}
		return "false", nil

	case "string", "choice":
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("parameter %q: internal: expected string, got %T", p.Key, v)
		}
		return renderStringLiteral(s), nil

	case "list", "multi_choice":
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

	case "regex":
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("parameter %q: internal: expected string, got %T", p.Key, v)
		}
		return renderStringLiteral(s), nil

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

// bakeIdentity returns a copy of def whose sql_template has the identity
// sentinels ('__rule_id__' / '__rule_name__') replaced by the definition's own
// id and (SQL-escaped) name. Templates without the sentinels — every builtin
// seed, and any author who wrote real identity columns — are returned unchanged.
// The input def is passed by value and never mutated, so the caller's copy (and
// therefore its Hash) is unaffected.
//
// Baking is applied to the template before parameter rendering (see Compile) so
// a string parameter whose value happens to equal a sentinel is not rewritten.
// A rule name carrying {{ or }} is rejected here because, once baked into the
// SQL, it would be re-scanned as a placeholder — the same injection guard the
// legacy create path enforces.
func bakeIdentity(def catalog.Definition) (catalog.Definition, error) {
	t := def.SQLTemplate
	if !strings.Contains(t, ruleIDSentinel) && !strings.Contains(t, ruleNameSentinel) {
		return def, nil
	}
	if strings.Contains(t, ruleNameSentinel) &&
		(strings.Contains(def.Name, "{{") || strings.Contains(def.Name, "}}")) {
		return catalog.Definition{}, &DefinitionError{
			Kind: DefinitionBadTemplate,
			Err:  fmt.Errorf("rule name must not contain '{{' or '}}' when the template bakes it in via __rule_name__"),
		}
	}
	t = strings.ReplaceAll(t, ruleIDSentinel, "'"+EscapeSQLString(def.ID)+"'")
	t = strings.ReplaceAll(t, ruleNameSentinel, "'"+EscapeSQLString(def.Name)+"'")
	def.SQLTemplate = t
	return def, nil
}

// CanonicalDefinitionHash is the single hash used for install-snapshot equality
// and upgrade detection. It hashes the definition's CANONICAL form:
//   - Apps normalization (omitted types → string, numeric defaults canonicalized),
//     via catalog.NormalizeDefinition, and
//   - identity sentinels baked ('__rule_id__'/'__rule_name__' replaced with the
//     rule's own id/name), via bakeIdentity.
//
// Baking is what makes finding-1 safe: a builtin whose identity columns were
// rewritten from literals ('rp-001' AS rule_id) to sentinels renders to
// byte-identical SQL, so it must hash identically — otherwise every install made
// before the rewrite would show a phantom "upgradeable" and lock its
// Configuration tab behind a needless, MV-resetting upgrade. Normalization is
// what makes finding-2 safe: a numeric default submitted as json.Number hashes
// the same as its persisted native-scalar form.
//
// EVERY hash comparison against a stored RuleInstance.DefinitionHash MUST go
// through this function (Compile records it; the read model and the
// configure/upgrade guards compare against it), or the two sides can disagree.
// If bakeIdentity rejects the definition (a name carrying '{{'/'}}' under the
// name sentinel) the un-baked normalized form is hashed — such a definition
// cannot compile and will be rejected on its own path; the hash only needs to be
// deterministic.
func CanonicalDefinitionHash(def catalog.Definition) string {
	def = catalog.NormalizeDefinition(def)
	if baked, err := bakeIdentity(def); err == nil {
		def = baked
	}
	return def.Hash()
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
	// Record the CANONICAL hash (normalized + identity-baked). Every install
	// snapshot stores this and every upgrade/configure guard compares against it
	// via CanonicalDefinitionHash, so a sentinel rewrite that renders identical
	// SQL — or a numeric default submitted in a different scalar form — does not
	// look like an upgrade. See CanonicalDefinitionHash.
	hash := CanonicalDefinitionHash(def)
	// Bake identity sentinels into a render copy before rendering parameters, so
	// a parameter value equal to a sentinel is not rewritten.
	renderDef, err := bakeIdentity(def)
	if err != nil {
		return Compiled{}, err
	}
	if err := ValidateTemplate(renderDef); err != nil {
		return Compiled{}, err
	}
	defaults, err := normalizedDefaults(renderDef)
	if err != nil {
		return Compiled{}, err
	}
	norm, err := NormalizeOverrides(renderDef, overrides)
	if err != nil {
		return Compiled{}, err
	}
	effective := effectiveValues(defaults, norm)
	sql, err := renderTemplate(renderDef, effective)
	if err != nil {
		return Compiled{}, err
	}
	return Compiled{
		Overrides:      canonicalOverrides(defaults, norm),
		Effective:      effective,
		SQL:            sql,
		DefinitionHash: hash,
	}, nil
}
