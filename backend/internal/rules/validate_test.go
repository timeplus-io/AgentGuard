package rules

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/catalog"
)

func fptr(f float64) *float64 { return &f }
func iptr(i int) *int         { return &i }

// defWith builds a minimal valid definition around the given parameters. The
// template references every parameter exactly once so ValidateTemplate passes.
func defWith(params ...catalog.Parameter) catalog.Definition {
	var b strings.Builder
	b.WriteString("SELECT 1")
	for _, p := range params {
		b.WriteString(" /* {{ .Config." + p.Key + " }} */")
	}
	return catalog.Definition{
		ID:          "rp_test",
		Name:        "Test Rule",
		Version:     "1.0.0",
		SQLTemplate: b.String(),
		Parameters:  params,
	}
}

// fieldErr extracts the single FieldError for a parameter from err, failing
// the test if err is not a *ValidationError containing exactly one entry for
// that parameter.
func fieldErr(t *testing.T, err error, param string) FieldError {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	var found []FieldError
	for _, f := range ve.Fields {
		if f.Parameter == param {
			found = append(found, f)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one field error for %q, got %d in %v", param, len(found), ve.Fields)
	}
	return found[0]
}

// ValidateDefinition is the compiler-semantic gate wired into the catalog at
// Save/Import: it must pass a clean definition and reject both a template that
// ignores a declared parameter and a default that violates its own constraint.
func TestValidateDefinition(t *testing.T) {
	okParam := catalog.Parameter{Key: "t", Label: "t", Type: "integer", Default: int64(15), Min: fptr(1), Max: fptr(100)}

	if err := ValidateDefinition(defWith(okParam)); err != nil {
		t.Fatalf("clean definition should validate, got %v", err)
	}

	// Declares "t" but the template never references it → ValidateTemplate fails.
	unused := defWith(okParam)
	unused.SQLTemplate = "SELECT 1"
	if err := ValidateDefinition(unused); err == nil {
		t.Errorf("template not using a declared parameter must fail")
	}

	// Default 999 is outside the declared [1,100] range → defaults compile fails.
	badDefault := catalog.Parameter{Key: "t", Label: "t", Type: "integer", Default: int64(999), Min: fptr(1), Max: fptr(100)}
	if err := ValidateDefinition(defWith(badDefault)); err == nil {
		t.Errorf("default outside [min,max] must fail")
	}
}

func TestNormalizeOverridesInteger(t *testing.T) {
	p := catalog.Parameter{Key: "threshold", Type: "integer", Default: 10, Min: fptr(1), Max: fptr(100)}
	def := defWith(p)

	cases := []struct {
		name    string
		value   any
		want    int64
		wantErr string
	}{
		{name: "json float integral", value: float64(5), want: 5},
		{name: "go int", value: 7, want: 7},
		{name: "json.Number", value: json.Number("42"), want: 42},
		{name: "int64", value: int64(99), want: 99},
		{name: "fractional", value: 5.5, wantErr: "whole number"},
		{name: "nan", value: math.NaN(), wantErr: "finite"},
		{name: "pos inf", value: math.Inf(1), wantErr: "finite"},
		{name: "neg inf", value: math.Inf(-1), wantErr: "finite"},
		{name: "below min", value: float64(0), wantErr: ">= 1"},
		{name: "above max", value: float64(101), wantErr: "<= 100"},
		{name: "string", value: "5", wantErr: "expected integer, got string"},
		{name: "bool", value: true, wantErr: "expected integer, got boolean"},
		{name: "null", value: nil, wantErr: "null"},
		{name: "huge float out of int64 range", value: 1e300, wantErr: "out of integer range"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeOverrides(def, map[string]any{"threshold": tc.value})
			if tc.wantErr != "" {
				fe := fieldErr(t, err, "threshold")
				if !strings.Contains(fe.Message, tc.wantErr) {
					t.Fatalf("message %q does not contain %q", fe.Message, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v, ok := got["threshold"].(int64); !ok || v != tc.want {
				t.Fatalf("got %#v, want int64(%d)", got["threshold"], tc.want)
			}
		})
	}
}

func TestNormalizeOverridesIntegerRangeOverflow(t *testing.T) {
	// Without Min/Max, a float64 beyond int64 must be rejected as out of
	// range rather than silently wrapped.
	p := catalog.Parameter{Key: "n", Type: "integer", Default: 0}
	def := defWith(p)
	for _, v := range []any{1e300, float64(math.MaxInt64)} { // 2^63 not representable as int64
		_, err := NormalizeOverrides(def, map[string]any{"n": v})
		fe := fieldErr(t, err, "n")
		if !strings.Contains(fe.Message, "range") {
			t.Fatalf("value %v: message %q should mention range", v, fe.Message)
		}
	}
	// Exact MinInt64 is representable and must pass.
	got, err := NormalizeOverrides(def, map[string]any{"n": float64(math.MinInt64)})
	if err != nil {
		t.Fatalf("MinInt64 should normalize: %v", err)
	}
	if got["n"].(int64) != math.MinInt64 {
		t.Fatalf("got %v, want MinInt64", got["n"])
	}
}

func TestNormalizeOverridesIntegerBoundCheckableRange(t *testing.T) {
	huge := json.Number("9007199254740993") // 2^53 + 1: int64-exact, float64-inexact
	negHuge := json.Number("-9007199254740993")

	// With Min/Max declared, bound checks happen in float64 — values beyond
	// ±2^53 would silently round, so they are rejected instead.
	bounded := defWith(catalog.Parameter{Key: "n", Type: "integer", Default: 1, Max: fptr(1e18)})
	for _, v := range []any{huge, negHuge, int64(1) << 60} {
		_, err := NormalizeOverrides(bounded, map[string]any{"n": v})
		fe := fieldErr(t, err, "n")
		if !strings.Contains(fe.Message, "exceeds bound-checkable range") {
			t.Fatalf("value %v: message %q should mention bound-checkable range", v, fe.Message)
		}
	}

	// Exactly ±2^53 is still float64-exact and must pass the bound check.
	got, err := NormalizeOverrides(bounded, map[string]any{"n": int64(1) << 53})
	if err != nil {
		t.Fatalf("2^53 should normalize: %v", err)
	}
	if got["n"].(int64) != 1<<53 {
		t.Fatalf("got %v, want 2^53", got["n"])
	}

	// Without bounds there is nothing to compare against; large exact
	// integers are accepted unchanged.
	unbounded := defWith(catalog.Parameter{Key: "n", Type: "integer", Default: 1})
	got, err = NormalizeOverrides(unbounded, map[string]any{"n": huge})
	if err != nil {
		t.Fatalf("unbounded large integer should normalize: %v", err)
	}
	if got["n"].(int64) != 9007199254740993 {
		t.Fatalf("got %v, want 9007199254740993", got["n"])
	}
}

func TestNormalizeOverridesRejectsNUL(t *testing.T) {
	strDef := defWith(catalog.Parameter{Key: "pattern", Type: "string", Default: "abc"})
	_, err := NormalizeOverrides(strDef, map[string]any{"pattern": "a\x00b"})
	fe := fieldErr(t, err, "pattern")
	if !strings.Contains(fe.Message, "NUL") {
		t.Fatalf("string message %q should mention NUL", fe.Message)
	}

	listDef := defWith(catalog.Parameter{Key: "cmds", Type: "string_list", Default: []any{"rm"}})
	_, err = NormalizeOverrides(listDef, map[string]any{"cmds": []any{"ok", "bad\x00"}})
	fe = fieldErr(t, err, "cmds")
	if !strings.Contains(fe.Message, "item 1") || !strings.Contains(fe.Message, "NUL") {
		t.Fatalf("list message %q should mention item 1 and NUL", fe.Message)
	}

	// Other control characters remain allowed.
	got, err := NormalizeOverrides(strDef, map[string]any{"pattern": "a\tb\nc"})
	if err != nil {
		t.Fatalf("tab/newline should be allowed: %v", err)
	}
	if got["pattern"] != "a\tb\nc" {
		t.Fatalf("got %#v", got["pattern"])
	}
}

func TestNormalizeOverridesNumber(t *testing.T) {
	p := catalog.Parameter{Key: "ratio", Type: "number", Default: 0.5, Min: fptr(0), Max: fptr(1)}
	def := defWith(p)

	cases := []struct {
		name    string
		value   any
		want    float64
		wantErr string
	}{
		{name: "float", value: 0.25, want: 0.25},
		{name: "int accepted as number", value: 1, want: 1},
		{name: "json.Number", value: json.Number("0.75"), want: 0.75},
		{name: "nan", value: math.NaN(), wantErr: "finite"},
		{name: "pos inf", value: math.Inf(1), wantErr: "finite"},
		{name: "neg inf", value: math.Inf(-1), wantErr: "finite"},
		{name: "below min", value: -0.1, wantErr: ">= 0"},
		{name: "above max", value: 1.1, wantErr: "<= 1"},
		{name: "string", value: "0.5", wantErr: "expected number, got string"},
		{name: "list", value: []any{1.0}, wantErr: "expected number, got list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeOverrides(def, map[string]any{"ratio": tc.value})
			if tc.wantErr != "" {
				fe := fieldErr(t, err, "ratio")
				if !strings.Contains(fe.Message, tc.wantErr) {
					t.Fatalf("message %q does not contain %q", fe.Message, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v, ok := got["ratio"].(float64); !ok || v != tc.want {
				t.Fatalf("got %#v, want float64(%v)", got["ratio"], tc.want)
			}
		})
	}
}

func TestNormalizeOverridesBoolean(t *testing.T) {
	p := catalog.Parameter{Key: "strict", Type: "boolean", Default: false}
	def := defWith(p)

	got, err := NormalizeOverrides(def, map[string]any{"strict": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := got["strict"].(bool); !ok || !v {
		t.Fatalf("got %#v, want true", got["strict"])
	}

	for name, bad := range map[string]any{
		"string": "true",
		"number": float64(1),
		"null":   nil,
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := NormalizeOverrides(def, map[string]any{"strict": bad})
			fieldErr(t, err, "strict")
		})
	}
}

func TestNormalizeOverridesString(t *testing.T) {
	p := catalog.Parameter{Key: "pattern", Type: "string", Default: "abc", MinLength: iptr(2), MaxLength: iptr(5)}
	def := defWith(p)

	cases := []struct {
		name    string
		value   any
		wantErr string
	}{
		{name: "ok", value: "hello"},
		{name: "unicode counts runes not bytes", value: "héllo"}, // 5 runes, 6 bytes
		{name: "too short", value: "x", wantErr: "at least 2"},
		{name: "too long", value: "abcdef", wantErr: "at most 5"},
		{name: "wrong type", value: 5.0, wantErr: "expected string, got number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeOverrides(def, map[string]any{"pattern": tc.value})
			if tc.wantErr != "" {
				fe := fieldErr(t, err, "pattern")
				if !strings.Contains(fe.Message, tc.wantErr) {
					t.Fatalf("message %q does not contain %q", fe.Message, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["pattern"] != tc.value {
				t.Fatalf("got %#v, want %#v", got["pattern"], tc.value)
			}
		})
	}
}

func TestNormalizeOverridesEnum(t *testing.T) {
	p := catalog.Parameter{Key: "mode", Type: "enum", Default: "warn", Options: []string{"warn", "block"}}
	def := defWith(p)

	got, err := NormalizeOverrides(def, map[string]any{"mode": "block"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["mode"] != "block" {
		t.Fatalf("got %#v, want block", got["mode"])
	}

	_, err = NormalizeOverrides(def, map[string]any{"mode": "explode"})
	fe := fieldErr(t, err, "mode")
	if !strings.Contains(fe.Message, "one of: warn, block") {
		t.Fatalf("message %q should list options", fe.Message)
	}

	_, err = NormalizeOverrides(def, map[string]any{"mode": 1.0})
	fieldErr(t, err, "mode")

	// Enum with no options is a definition bug, surfaced as a field error.
	bad := defWith(catalog.Parameter{Key: "mode", Type: "enum", Default: "x"})
	_, err = NormalizeOverrides(bad, map[string]any{"mode": "x"})
	fe = fieldErr(t, err, "mode")
	if !strings.Contains(fe.Message, "no options") {
		t.Fatalf("message %q should mention missing options", fe.Message)
	}
}

func TestNormalizeOverridesStringList(t *testing.T) {
	p := catalog.Parameter{
		Key: "cmds", Type: "string_list", Default: []any{"rm"},
		MinItems: iptr(1), MaxItems: iptr(3),
		MinLength: iptr(2), MaxLength: iptr(10),
	}
	def := defWith(p)

	cases := []struct {
		name    string
		value   any
		want    []string
		wantErr string
	}{
		{name: "json list", value: []any{"rm", "dd"}, want: []string{"rm", "dd"}},
		{name: "go string slice", value: []string{"rm", "dd"}, want: []string{"rm", "dd"}},
		{name: "order preserved", value: []any{"zz", "aa", "mm"}, want: []string{"zz", "aa", "mm"}},
		{name: "empty list", value: []any{}, wantErr: "at least one item"},
		{name: "duplicate", value: []any{"rm", "rm"}, wantErr: `duplicate item "rm"`},
		{name: "non-string item", value: []any{"rm", 5.0}, wantErr: "item 1: expected string, got number"},
		{name: "too many items", value: []any{"aa", "bb", "cc", "dd"}, wantErr: "at most 3 items"},
		{name: "item too short", value: []any{"x"}, wantErr: "at least 2 characters"},
		{name: "item too long", value: []any{"abcdefghijk"}, wantErr: "at most 10 characters"},
		{name: "not a list", value: "rm", wantErr: "expected list of strings, got string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeOverrides(def, map[string]any{"cmds": tc.value})
			if tc.wantErr != "" {
				fe := fieldErr(t, err, "cmds")
				if !strings.Contains(fe.Message, tc.wantErr) {
					t.Fatalf("message %q does not contain %q", fe.Message, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got["cmds"], tc.want) {
				t.Fatalf("got %#v, want %#v", got["cmds"], tc.want)
			}
		})
	}
}

func TestNormalizeOverridesEmptyListRejectedEvenWithoutMinItems(t *testing.T) {
	p := catalog.Parameter{Key: "cmds", Type: "string_list", Default: []any{"rm"}}
	def := defWith(p)
	_, err := NormalizeOverrides(def, map[string]any{"cmds": []any{}})
	fe := fieldErr(t, err, "cmds")
	if !strings.Contains(fe.Message, "at least one item") {
		t.Fatalf("message %q should reject empty list", fe.Message)
	}
}

func TestNormalizeOverridesUnknownKey(t *testing.T) {
	def := defWith(catalog.Parameter{Key: "a", Type: "integer", Default: 1})
	_, err := NormalizeOverrides(def, map[string]any{"nope": 1.0})
	fe := fieldErr(t, err, "nope")
	if fe.Message != "unknown parameter" {
		t.Fatalf("got message %q", fe.Message)
	}
}

func TestNormalizeOverridesUnsupportedType(t *testing.T) {
	// regex/duration are Phase B; until then they must fail loudly.
	for _, typ := range []string{"regex", "duration", "frobnicate"} {
		def := defWith(catalog.Parameter{Key: "p", Type: typ, Default: "x"})
		_, err := NormalizeOverrides(def, map[string]any{"p": "y"})
		fe := fieldErr(t, err, "p")
		if !strings.Contains(fe.Message, "unsupported parameter type") {
			t.Fatalf("type %q: message %q should say unsupported", typ, fe.Message)
		}
	}
}

func TestNormalizeOverridesCollectsAllErrorsDeterministically(t *testing.T) {
	def := defWith(
		catalog.Parameter{Key: "a", Type: "integer", Default: 1},
		catalog.Parameter{Key: "b", Type: "string", Default: "x"},
	)
	_, err := NormalizeOverrides(def, map[string]any{
		"b": 1.0,
		"a": "no",
		"z": true,
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if len(ve.Fields) != 3 {
		t.Fatalf("expected 3 field errors, got %v", ve.Fields)
	}
	// sortedKeys guarantees deterministic ordering: a, b, z.
	for i, want := range []string{"a", "b", "z"} {
		if ve.Fields[i].Parameter != want {
			t.Fatalf("field %d = %q, want %q (fields %v)", i, ve.Fields[i].Parameter, want, ve.Fields)
		}
	}
	if !strings.Contains(ve.Error(), "invalid parameter values") {
		t.Fatalf("Error() = %q", ve.Error())
	}
}

func TestNormalizeOverridesEmptyAndNil(t *testing.T) {
	def := defWith(catalog.Parameter{Key: "a", Type: "integer", Default: 1})
	for _, in := range []map[string]any{nil, {}} {
		got, err := NormalizeOverrides(def, in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("got %#v, want empty", got)
		}
	}
}

func TestValidateDefaults(t *testing.T) {
	ok := defWith(
		catalog.Parameter{Key: "a", Type: "integer", Default: 5, Min: fptr(1)},
		catalog.Parameter{Key: "b", Type: "string_list", Default: []any{"x", "y"}},
		catalog.Parameter{Key: "c", Type: "enum", Default: "warn", Options: []string{"warn", "block"}},
	)
	if err := ValidateDefaults(ok); err != nil {
		t.Fatalf("valid defaults rejected: %v", err)
	}

	bad := defWith(
		catalog.Parameter{Key: "a", Type: "integer", Default: 0, Min: fptr(1)},          // below min
		catalog.Parameter{Key: "b", Type: "enum", Default: "x", Options: []string{"y"}}, // not an option
		catalog.Parameter{Key: "c", Type: "string_list", Default: []any{}},              // empty list
	)
	err := ValidateDefaults(bad)
	var de *DefinitionError
	if !errors.As(err, &de) {
		t.Fatalf("expected *DefinitionError, got %T: %v", err, err)
	}
	if de.Kind != DefinitionBadDefault {
		t.Fatalf("Kind = %q, want %q", de.Kind, DefinitionBadDefault)
	}
	if len(de.Fields) != 3 {
		t.Fatalf("expected 3 default failures, got %v", de.Fields)
	}
	for _, f := range de.Fields {
		if !strings.HasPrefix(f.Message, "default: ") {
			t.Fatalf("field %v should be prefixed with 'default: '", f)
		}
	}
	// Definition-side failures must not be mistakable for user-input errors.
	var ve *ValidationError
	if errors.As(err, &ve) {
		t.Fatalf("bad defaults must not satisfy errors.As(*ValidationError): %v", err)
	}
}

// TestErrorTaxonomy pins the errors.As split: override failures are
// *ValidationError, definition defects (bad default, bad template) are
// *DefinitionError, and neither matches the other.
func TestErrorTaxonomy(t *testing.T) {
	var ve *ValidationError
	var de *DefinitionError

	t.Run("bad override", func(t *testing.T) {
		def := defWith(catalog.Parameter{Key: "n", Type: "integer", Default: 5, Min: fptr(1)})
		_, err := Compile(def, map[string]any{"n": float64(0)})
		if !errors.As(err, &ve) {
			t.Fatalf("expected *ValidationError, got %T: %v", err, err)
		}
		if errors.As(err, &de) {
			t.Fatalf("override failure must not match *DefinitionError: %v", err)
		}
	})

	t.Run("bad default", func(t *testing.T) {
		def := defWith(catalog.Parameter{Key: "n", Type: "integer", Default: "ten"})
		_, err := Compile(def, nil)
		if !errors.As(err, &de) {
			t.Fatalf("expected *DefinitionError, got %T: %v", err, err)
		}
		if de.Kind != DefinitionBadDefault {
			t.Fatalf("Kind = %q, want %q", de.Kind, DefinitionBadDefault)
		}
		if errors.As(err, &ve) {
			t.Fatalf("default failure must not match *ValidationError: %v", err)
		}
	})

	t.Run("bad template", func(t *testing.T) {
		def := catalog.Definition{
			ID: "rp_t", Name: "T", Version: "1.0.0",
			SQLTemplate: "SELECT {{ .Config.unknown }}",
			Parameters:  []catalog.Parameter{{Key: "n", Type: "integer", Default: 5}},
		}
		for name, err := range map[string]error{
			"ValidateTemplate": ValidateTemplate(def),
			"Compile":          func() error { _, err := Compile(def, nil); return err }(),
		} {
			if !errors.As(err, &de) {
				t.Fatalf("%s: expected *DefinitionError, got %T: %v", name, err, err)
			}
			if de.Kind != DefinitionBadTemplate {
				t.Fatalf("%s: Kind = %q, want %q", name, de.Kind, DefinitionBadTemplate)
			}
			if de.Unwrap() == nil {
				t.Fatalf("%s: template DefinitionError must wrap its cause", name)
			}
			if errors.As(err, &ve) {
				t.Fatalf("%s: template failure must not match *ValidationError: %v", name, err)
			}
		}
	})
}
