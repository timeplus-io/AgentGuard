package timeplus

import (
	"testing"
	"time"
)

// derefPtr is the helper that unwraps native-driver pointer-typed columns
// (which appear for Nullable(T) types) so handler-side parse* helpers see the
// underlying value, not a pointer. nil pointer → zero value of the element type.

func TestDerefPtr_NonPointerPassThrough(t *testing.T) {
	cases := []struct {
		name string
		in   any
	}{
		{"string", "hello"},
		{"int64", int64(42)},
		{"uint64", uint64(42)},
		{"float64", 3.14},
		{"bool", true},
		{"time.Time", time.Now()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := derefPtr(c.in)
			// Compare via interface equality — works for all comparable types.
			if got != c.in {
				t.Errorf("non-pointer should pass through unchanged; got %v, want %v", got, c.in)
			}
		})
	}
}

func TestDerefPtr_StringPointer(t *testing.T) {
	s := "hello"
	if got := derefPtr(&s); got != "hello" {
		t.Errorf("got %v, want \"hello\"", got)
	}
}

func TestDerefPtr_NilStringPointerReturnsZero(t *testing.T) {
	var p *string
	if got := derefPtr(p); got != "" {
		t.Errorf("nil *string should yield \"\"; got %v", got)
	}
}

func TestDerefPtr_Int64Pointer(t *testing.T) {
	v := int64(42)
	if got := derefPtr(&v); got != int64(42) {
		t.Errorf("got %v, want int64(42)", got)
	}
}

func TestDerefPtr_NilInt64PointerReturnsZero(t *testing.T) {
	var p *int64
	if got := derefPtr(p); got != int64(0) {
		t.Errorf("nil *int64 should yield int64(0); got %v", got)
	}
}

func TestDerefPtr_Uint64Pointer(t *testing.T) {
	v := uint64(7)
	if got := derefPtr(&v); got != uint64(7) {
		t.Errorf("got %v, want uint64(7)", got)
	}
}

func TestDerefPtr_NilUint64PointerReturnsZero(t *testing.T) {
	var p *uint64
	if got := derefPtr(p); got != uint64(0) {
		t.Errorf("nil *uint64 should yield uint64(0); got %v", got)
	}
}

func TestDerefPtr_Float64Pointer(t *testing.T) {
	v := 1.5
	if got := derefPtr(&v); got != 1.5 {
		t.Errorf("got %v, want 1.5", got)
	}
}

func TestDerefPtr_NilFloat64PointerReturnsZero(t *testing.T) {
	var p *float64
	if got := derefPtr(p); got != float64(0) {
		t.Errorf("nil *float64 should yield 0; got %v", got)
	}
}

func TestDerefPtr_BoolPointer(t *testing.T) {
	v := true
	if got := derefPtr(&v); got != true {
		t.Errorf("got %v, want true", got)
	}
}

func TestDerefPtr_NilBoolPointerReturnsFalse(t *testing.T) {
	var p *bool
	if got := derefPtr(p); got != false {
		t.Errorf("nil *bool should yield false; got %v", got)
	}
}

func TestDerefPtr_TimePointer(t *testing.T) {
	v := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	got, ok := derefPtr(&v).(time.Time)
	if !ok {
		t.Fatalf("expected time.Time, got %T", derefPtr(&v))
	}
	if !got.Equal(v) {
		t.Errorf("got %v, want %v", got, v)
	}
}

func TestDerefPtr_NilTimePointerReturnsZero(t *testing.T) {
	var p *time.Time
	got, ok := derefPtr(p).(time.Time)
	if !ok {
		t.Fatalf("expected time.Time, got %T", derefPtr(p))
	}
	if !got.IsZero() {
		t.Errorf("nil *time.Time should yield zero time; got %v", got)
	}
}

func TestDerefPtr_UnknownPointerTypePassesThrough(t *testing.T) {
	// derefPtr falls through reflect.Ptr handling: a *T it doesn't know how to
	// produce a typed-zero for still gets dereferenced via reflect.
	type custom struct{ X int }
	v := custom{X: 5}
	got, ok := derefPtr(&v).(custom)
	if !ok {
		t.Fatalf("expected custom, got %T", derefPtr(&v))
	}
	if got.X != 5 {
		t.Errorf("got %+v, want {X:5}", got)
	}
}

func TestDerefPtr_NilUnknownPointerReturnsZeroValue(t *testing.T) {
	type custom struct{ X int }
	var p *custom
	got, ok := derefPtr(p).(custom)
	if !ok {
		t.Fatalf("expected custom, got %T", derefPtr(p))
	}
	if got.X != 0 {
		t.Errorf("nil *custom should yield zero custom; got %+v", got)
	}
}
