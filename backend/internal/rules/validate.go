package rules

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/timeplus/agentguard/internal/catalog"
)

// paramIndex builds a Key → Parameter lookup for a definition.
func paramIndex(def catalog.Definition) map[string]catalog.Parameter {
	idx := make(map[string]catalog.Parameter, len(def.Parameters))
	for _, p := range def.Parameters {
		idx[p.Key] = p
	}
	return idx
}

// sortedKeys returns the map keys in sorted order so error lists and
// iteration-dependent results are deterministic.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// formatBound renders a numeric constraint bound without exponent notation.
func formatBound(b float64) string {
	return strconv.FormatFloat(b, 'f', -1, 64)
}

// coerceInt64 converts a decoded JSON/YAML numeric value to int64. Fractional
// values are rejected, not truncated.
func coerceInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int8:
		return int64(n), nil
	case int16:
		return int64(n), nil
	case int32:
		return int64(n), nil
	case int64:
		return n, nil
	case uint:
		if uint64(n) > math.MaxInt64 {
			return 0, fmt.Errorf("value %d overflows integer range", n)
		}
		return int64(n), nil
	case uint8:
		return int64(n), nil
	case uint16:
		return int64(n), nil
	case uint32:
		return int64(n), nil
	case uint64:
		if n > math.MaxInt64 {
			return 0, fmt.Errorf("value %d overflows integer range", n)
		}
		return int64(n), nil
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, fmt.Errorf("must be a finite integer")
		}
		if n != math.Trunc(n) {
			return 0, fmt.Errorf("must be a whole number, got %s", strconv.FormatFloat(n, 'f', -1, 64))
		}
		if n < math.MinInt64 || n >= math.MaxInt64 {
			return 0, fmt.Errorf("value out of integer range")
		}
		return int64(n), nil
	case float32:
		return coerceInt64(float64(n))
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i, nil
		}
		f, err := n.Float64()
		if err != nil {
			return 0, fmt.Errorf("invalid number %q", n.String())
		}
		return coerceInt64(f)
	default:
		return 0, fmt.Errorf("expected integer, got %s", typeName(v))
	}
}

// coerceFloat64 converts a decoded JSON/YAML numeric value to float64.
func coerceFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int8:
		return float64(n), nil
	case int16:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint8:
		return float64(n), nil
	case uint16:
		return float64(n), nil
	case uint32:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, fmt.Errorf("invalid number %q", n.String())
		}
		return f, nil
	default:
		return 0, fmt.Errorf("expected number, got %s", typeName(v))
	}
}

// coerceStringSlice converts a decoded JSON/YAML list value to []string.
func coerceStringSlice(v any) ([]string, error) {
	switch list := v.(type) {
	case []string:
		out := make([]string, len(list))
		copy(out, list)
		return out, nil
	case []any:
		out := make([]string, len(list))
		for i, item := range list {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("item %d: expected string, got %s", i, typeName(item))
			}
			out[i] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected list of strings, got %s", typeName(v))
	}
}

// checkNumericBounds enforces Min/Max on a numeric value.
func checkNumericBounds(p catalog.Parameter, f float64) error {
	if p.Min != nil && f < *p.Min {
		return fmt.Errorf("must be >= %s", formatBound(*p.Min))
	}
	if p.Max != nil && f > *p.Max {
		return fmt.Errorf("must be <= %s", formatBound(*p.Max))
	}
	return nil
}

// checkNoNUL rejects strings containing a NUL (U+0000) character. NUL has no
// legitimate use in rule parameter values and is a classic string-truncation
// vector once the value crosses into C-string territory. Other control
// characters remain allowed.
func checkNoNUL(s string) error {
	if strings.ContainsRune(s, 0) {
		return fmt.Errorf("must not contain NUL (U+0000) characters")
	}
	return nil
}

func checkOption(p catalog.Parameter, s string) error {
	if len(p.Options) == 0 {
		return fmt.Errorf("%s parameter declares no options", p.Type)
	}
	for _, opt := range p.Options {
		if s == opt {
			return nil
		}
	}
	return fmt.Errorf("must be one of: %s", strings.Join(p.Options, ", "))
}

func normalizeStringList(p catalog.Parameter, v any) ([]string, error) {
	items, err := coerceStringSlice(v)
	if err != nil {
		return nil, err
	}
	// Empty-list SQL semantics (e.g. IN ()) are rejected, not guessed.
	if len(items) == 0 {
		return nil, fmt.Errorf("must contain at least one item")
	}
	if p.MinItems != nil && len(items) < *p.MinItems {
		return nil, fmt.Errorf("must contain at least %d items, got %d", *p.MinItems, len(items))
	}
	if p.MaxItems != nil && len(items) > *p.MaxItems {
		return nil, fmt.Errorf("must contain at most %d items, got %d", *p.MaxItems, len(items))
	}
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		if seen[item] {
			return nil, fmt.Errorf("duplicate item %q at index %d", item, i)
		}
		seen[item] = true
		if err := checkNoNUL(item); err != nil {
			return nil, fmt.Errorf("item %d (%q): %s", i, item, err.Error())
		}
	}
	return items, nil
}

// normalizeValue coerces a decoded JSON/YAML value into the canonical Go
// representation for the parameter's type (see types.go) and enforces the
// parameter's constraints. The returned error message is per-parameter and
// does not repeat the parameter ID.
func normalizeValue(p catalog.Parameter, v any) (any, error) {
	if v == nil {
		return nil, fmt.Errorf("value is required, got null")
	}
	switch p.Type {
	// duration is integer + a unit annotation; its value normalizes identically.
	case "integer", "duration":
		i, err := coerceInt64(v)
		if err != nil {
			return nil, err
		}
		if p.Min != nil || p.Max != nil {
			// Bounds are float64; integers beyond ±2^53 are not exactly
			// representable as float64, so the comparison would silently
			// round. Strictness over silent rounding: reject instead.
			const maxBoundCheckable = int64(1) << 53
			if i > maxBoundCheckable || i < -maxBoundCheckable {
				return nil, fmt.Errorf("value %d exceeds bound-checkable range (|value| > 2^53) and cannot be compared precisely against min/max", i)
			}
			if err := checkNumericBounds(p, float64(i)); err != nil {
				return nil, err
			}
		}
		return i, nil

	case "float":
		f, err := coerceFloat64(v)
		if err != nil {
			return nil, err
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("must be a finite number")
		}
		if err := checkNumericBounds(p, f); err != nil {
			return nil, err
		}
		return f, nil

	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool, got %s", typeName(v))
		}
		return b, nil

	case "string":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %s", typeName(v))
		}
		if err := checkNoNUL(s); err != nil {
			return nil, err
		}
		return s, nil

	case "choice":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %s", typeName(v))
		}
		if err := checkOption(p, s); err != nil {
			return nil, err
		}
		return s, nil

	case "list":
		return normalizeStringList(p, v)

	case "multi_choice":
		items, err := normalizeStringList(p, v)
		if err != nil {
			return nil, err
		}
		for i, item := range items {
			if err := checkOption(p, item); err != nil {
				return nil, fmt.Errorf("item %d: %s", i, err.Error())
			}
		}
		return items, nil

	case "regex":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %s", typeName(v))
		}
		if err := checkNoNUL(s); err != nil {
			return nil, err
		}
		if _, err := regexp.Compile(s); err != nil {
			return nil, fmt.Errorf("must be a valid RE2 regular expression: %v", err)
		}
		return s, nil

	default:
		return nil, fmt.Errorf("unsupported parameter type %q (supported: integer, float, bool, string, choice, list, multi_choice, regex, duration)", p.Type)
	}
}

// normalizedDefaults normalizes every parameter's default value. Failures are
// defects of the definition, not of any user input, so they are collected
// into a single *DefinitionError (kind DefinitionBadDefault) — never a
// *ValidationError.
func normalizedDefaults(def catalog.Definition) (map[string]any, error) {
	out := make(map[string]any, len(def.Parameters))
	var fields []FieldError
	for _, p := range def.Parameters {
		nv, err := normalizeValue(p, p.Default)
		if err != nil {
			fields = append(fields, FieldError{Parameter: p.Key, Message: "default: " + err.Error()})
			continue
		}
		out[p.Key] = nv
	}
	if len(fields) > 0 {
		return nil, &DefinitionError{Kind: DefinitionBadDefault, Fields: fields}
	}
	return out, nil
}

// ValidateDefaults verifies that every parameter's declared default value
// normalizes and satisfies its own constraints, so installing a rule with
// defaults always compiles. This is value-level validation owned by the
// compiler; catalog.Definition.Validate covers structure only. Failures are
// reported as a *DefinitionError.
func ValidateDefaults(def catalog.Definition) error {
	_, err := normalizedDefaults(def)
	return err
}

// ValidateDefinition checks a catalog definition's parameter-compiler semantics:
// that every {{ .Config.<key> }} placeholder references a declared parameter, that
// every declared parameter is used by the template, and that the definition
// compiles from its own defaults. It complements catalog.Definition.Validate()
// (which checks only structural shape), so authoring defects in a saved or
// imported definition are caught at write time — mapped to a client error —
// instead of first surfacing as a 500 *DefinitionError at install/configure.
// It is wired into the catalog Manager via Manager.SemanticValidator so the
// catalog package need not depend on internal/rules.
func ValidateDefinition(def catalog.Definition) error {
	if err := ValidateTemplate(def); err != nil {
		return err
	}
	if err := ValidateDefaults(def); err != nil {
		return err
	}
	if _, err := Compile(def, nil); err != nil {
		return err
	}
	return nil
}

// NormalizeOverrides validates a set of user-supplied overrides (decoded
// JSON values keyed by parameter ID) against the definition and returns the
// canonical normalized values. Unknown parameter IDs and per-value failures
// are collected into a single ValidationError.
func NormalizeOverrides(def catalog.Definition, overrides map[string]any) (map[string]any, error) {
	params := paramIndex(def)
	out := make(map[string]any, len(overrides))
	var fields []FieldError
	for _, k := range sortedKeys(overrides) {
		p, ok := params[k]
		if !ok {
			fields = append(fields, FieldError{Parameter: k, Message: "unknown parameter"})
			continue
		}
		nv, err := normalizeValue(p, overrides[k])
		if err != nil {
			fields = append(fields, FieldError{Parameter: k, Message: err.Error()})
			continue
		}
		out[k] = nv
	}
	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	return out, nil
}

// effectiveValues overlays normalized overrides onto normalized defaults.
func effectiveValues(defaults, overrides map[string]any) map[string]any {
	out := make(map[string]any, len(defaults))
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

// canonicalOverrides drops override entries whose normalized value deep-equals
// the parameter's current normalized default, so only real deviations persist.
func canonicalOverrides(defaults, overrides map[string]any) map[string]any {
	out := make(map[string]any, len(overrides))
	for k, v := range overrides {
		if d, ok := defaults[k]; ok && reflect.DeepEqual(v, d) {
			continue
		}
		out[k] = v
	}
	return out
}
