// Package rules implements the strict parameter compiler for catalog rule
// definitions: typed override normalization and validation, deterministic SQL
// rendering from strict {{ .Config.<key> }} placeholders, and upgrade
// compatibility planning for installed overrides.
//
// The compiler core (validate.go, render.go, compatibility.go) is pure — no
// Timeplus I/O, no Gin, no config — and depends only on internal/catalog
// (which owns Definition and Parameter; catalog must never import this
// package). The package additionally owns the rule-instance persistence layer
// (store.go): the single reader/writer of revision-ordered snapshots in the
// agentguard_rules stream, and the one place in the package that performs
// Timeplus I/O.
//
// Failures split into two errors.As-able types: *ValidationError for
// user-supplied override values (handlers map it to 400) and
// *DefinitionError for defects in the definition itself — a default that
// violates its own constraints, a malformed sql_template (handlers map it to
// a server-side status, never blaming the caller).
//
// JSON consumers should decode override request bodies with
// json.Decoder.UseNumber() so large integers reach the compiler without
// float64 rounding — the value coercers accept json.Number directly.
package rules

import (
	"fmt"
	"strings"
	"time"
)

// Canonical Go representations per parameter type (Timeplus Apps vocabulary +
// AgentGuard extensions):
//
//	integer  → int64
//	float    → float64 (always finite)
//	bool     → bool
//	string   → string
//	choice   → string (member of Options)
//	list     → []string (non-empty, unique, order-preserving)
//	multi_choice → []string (non-empty, unique, order-preserving, all in Options)
//	regex    → string (compiles as an RE2 pattern; rendered as a SQL string literal)
//	duration → int64 (count of the parameter's fixed unit; rendered bare)

// FieldError is a single per-parameter validation failure.
type FieldError struct {
	Parameter string `json:"parameter"`
	Message   string `json:"message"`
}

// ValidationError aggregates per-parameter failures from normalizing a set of
// values. Handlers serialize Fields into a 400 response body.
type ValidationError struct {
	Fields []FieldError `json:"fields"`
}

// Error renders all field errors into one readable message.
func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Parameter + ": " + f.Message
	}
	return "invalid parameter values: " + strings.Join(parts, "; ")
}

// DefinitionErrorKind identifies which part of a rule definition is
// defective.
type DefinitionErrorKind string

const (
	// DefinitionBadDefault: a parameter's declared default value fails its
	// own type coercion or constraints.
	DefinitionBadDefault DefinitionErrorKind = "bad_default"
	// DefinitionBadTemplate: the sql_template is malformed or inconsistent
	// with the declared parameters.
	DefinitionBadTemplate DefinitionErrorKind = "bad_template"
)

// DefinitionError reports a defect in the rule definition itself, as opposed
// to the user-supplied override values (*ValidationError). It is deliberately
// never built from (and never unwraps to) a *ValidationError, so handlers can
// distinguish the two with errors.As in either order.
type DefinitionError struct {
	// Kind identifies what is wrong with the definition.
	Kind DefinitionErrorKind `json:"kind"`
	// Fields carries per-parameter detail; populated for DefinitionBadDefault.
	Fields []FieldError `json:"fields,omitempty"`
	// Err is the underlying cause; populated for DefinitionBadTemplate.
	Err error `json:"-"`
}

// Error renders the defect kind plus its detail into one readable message.
func (e *DefinitionError) Error() string {
	msg := "invalid rule definition (" + string(e.Kind) + ")"
	if len(e.Fields) > 0 {
		parts := make([]string, len(e.Fields))
		for i, f := range e.Fields {
			parts[i] = f.Parameter + ": " + f.Message
		}
		return msg + ": " + strings.Join(parts, "; ")
	}
	if e.Err != nil {
		return msg + ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is/errors.As chains.
func (e *DefinitionError) Unwrap() error { return e.Err }

// Compiled is the result of Compile: everything a caller needs to persist and
// deploy a configured rule instance.
type Compiled struct {
	// Overrides is the canonical persisted form: normalized values with
	// entries equal to the current default dropped.
	Overrides map[string]any `json:"overrides"`
	// Effective is defaults overlaid with overrides, all normalized.
	Effective map[string]any `json:"effective"`
	// SQL is the rendered sql_template with every placeholder substituted.
	SQL string `json:"sql"`
	// DefinitionHash is catalog.Definition.Hash() of the compiled definition.
	DefinitionHash string `json:"definition_hash"`
}

// CompatibilityIssue describes one parameter-level outcome of an upgrade plan
// that is not a plain preservation.
type CompatibilityIssue struct {
	Parameter string `json:"parameter"`
	Message   string `json:"message"`
}

// CompatibilityResult is the outcome of PlanUpgrade. Every input override is
// accounted for exactly once: preserved in Overrides (possibly under a renamed
// ID), dropped with an entry in Warnings, or blocked with an entry in
// Conflicts. A non-empty Conflicts list blocks the upgrade (callers map it to
// HTTP 409).
type CompatibilityResult struct {
	// Overrides are the preserved overrides keyed by the parameter ID in the
	// next definition (renamed when migrated via an alias). Values are
	// normalized against the next definition's parameters.
	Overrides map[string]any `json:"overrides"`
	// Warnings are non-blocking drops (e.g. parameter removed without alias).
	Warnings []CompatibilityIssue `json:"warnings,omitempty"`
	// Conflicts are blocking incompatibilities (type changes, constraint
	// violations, alias ambiguity).
	Conflicts []CompatibilityIssue `json:"conflicts,omitempty"`
}

// OK reports whether the plan has no blocking conflicts.
func (r CompatibilityResult) OK() bool { return len(r.Conflicts) == 0 }

// RuleInstance is the in-memory form of one complete agentguard_rules snapshot
// row — a single immutable revision of a rule instance. The snapshot store
// (store.go) reads instances via arg_max(..., revision) and appends complete
// snapshots; nothing else reads or writes that stream.
//
// Ordering is ALWAYS by Revision, a per-rule monotonically increasing counter —
// never by UpdatedAt. The DeployedUDFs and Overrides fields are the decoded
// forms of the deployed_udfs_json and parameter_overrides_json columns.
type RuleInstance struct {
	RuleID            string `json:"rule_id"`
	SnapshotID        string `json:"snapshot_id"`
	Revision          uint64 `json:"revision"`
	Name              string `json:"name"`
	DefinitionVersion string `json:"definition_version"`
	DefinitionHash    string `json:"definition_hash"`
	Description       string `json:"description"`
	RenderedSQL       string `json:"rendered_sql"`
	// DeployedUDFs is the decoded form of the deployed_udfs_json column: a JSON
	// array of UDF DDL statements deployed alongside this rule's materialized
	// view. The store persists an empty list as "[]", never as a blank string.
	DeployedUDFs []string `json:"deployed_udfs"`
	// Overrides is the decoded form of the parameter_overrides_json column: the
	// canonical normalized override map produced by the compiler. The store
	// persists an empty map as "{}", never as a blank string.
	//
	// NOTE on numeric round-trip: the compiler's canonical integer form is
	// int64, but the store decodes JSON with UseNumber(), so numbers read back
	// from a snapshot are json.Number, not int64/float64. A caller comparing a
	// freshly-compiled override set against a store-read set (e.g. reflect.
	// DeepEqual to detect a no-op change) must normalize both sides first.
	Overrides         map[string]any `json:"overrides"`
	Enabled           bool           `json:"enabled"`
	Deleted           bool           `json:"deleted"`
	Severity          string         `json:"severity"`
	Category          string         `json:"category"`
	RequiredTelemetry string         `json:"required_telemetry"`
	FPRate            string         `json:"fp_rate"`
	HookSources       string         `json:"hook_sources"`
	BlockPolicy       string         `json:"block_policy"`
	UpdateReason      string         `json:"update_reason"`
	UpdatedBy         string         `json:"updated_by"`
	// UpdatedAt is read-populated audit metadata: the database stamps it with
	// now64(9) on Append (the caller's value is ignored on write). It MUST NOT
	// be used as a current-state ordering key — Revision is the only ordering
	// key, because replays and clock skew make wall-clock ordering unsafe and
	// Proton's arg_max tie-breaks a tied max to the first-seen value.
	UpdatedAt time.Time `json:"updated_at"`
}

// typeName returns a human-friendly name for a decoded JSON/YAML value's
// type, for use in validation error messages.
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "number"
	case []any, []string:
		return "list"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}
