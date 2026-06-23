package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Origin values for a Definition. The catalog loader enforces them:
// embedded seed definitions are always OriginBuiltin (overriding anything
// stored in the YAML), local-dir definitions keep their stored origin
// (defaulting to OriginCustom when absent), and anything arriving via
// ImportZip becomes OriginImported.
const (
	OriginBuiltin  = "builtin"
	OriginImported = "imported"
	OriginCustom   = "custom"
)

// Definition is the canonical representation of a catalog rule definition.
// The catalog is the authoritative store for rule definitions; installed
// rule state (enabled, block policy, ...) lives in Timeplus.
type Definition struct {
	ID                string      `yaml:"id"                 json:"id"`
	Name              string      `yaml:"name"               json:"name"`
	Version           string      `yaml:"version"            json:"version"`
	Description       string      `yaml:"description"        json:"description"`
	Severity          string      `yaml:"severity"           json:"severity"`
	Category          string      `yaml:"category"           json:"category"`
	FPRate            string      `yaml:"fp_rate"            json:"fp_rate"`
	HookSources       string      `yaml:"hook_sources"       json:"hook_sources"`
	RequiredTelemetry string      `yaml:"required_telemetry" json:"required_telemetry"`
	Author            string      `yaml:"author"             json:"author"`
	Tags              []string    `yaml:"tags"               json:"tags"`
	UDFs              []string    `yaml:"udfs"               json:"udfs"`
	SQLTemplate       string      `yaml:"sql_template"       json:"sql_template"`
	Parameters        []Parameter `yaml:"config"             json:"config,omitempty"`
	Origin            string      `yaml:"origin"             json:"origin,omitempty"`
}

// Parameter describes one user-editable knob of a rule definition.
// Supported Type values are listed in parameterTypes.
//
// Key is the canonical identifier used in placeholders: {{ .Config.<key> }}.
// Helper names ValidParameterID / ParameterIDPattern / paramIDRe validate the
// same identifier syntax for both keys and aliases (id == key historically).
type Parameter struct {
	Key         string   `yaml:"key"                   json:"key"`
	Label       string   `yaml:"label"                 json:"label"`
	Description string   `yaml:"description"           json:"description"`
	Type        string   `yaml:"type"                  json:"type"`
	Default     any      `yaml:"default"               json:"default"`
	Min         *float64 `yaml:"min,omitempty"         json:"min,omitempty"`
	Max         *float64 `yaml:"max,omitempty"         json:"max,omitempty"`
	MinLength   *int     `yaml:"min_length,omitempty"  json:"min_length,omitempty"`
	MaxLength   *int     `yaml:"max_length,omitempty"  json:"max_length,omitempty"`
	Options     []string `yaml:"options,omitempty"     json:"options,omitempty"`
	MinItems    *int     `yaml:"min_items,omitempty"   json:"min_items,omitempty"`
	MaxItems    *int     `yaml:"max_items,omitempty"   json:"max_items,omitempty"`
	Aliases     []string `yaml:"aliases,omitempty"     json:"aliases,omitempty"`
	Unit        string   `yaml:"unit,omitempty"        json:"unit,omitempty"`
	Required    bool     `yaml:"required,omitempty"    json:"required,omitempty"`
	Secret      bool     `yaml:"secret,omitempty"      json:"secret,omitempty"`
}

// parameterTypes is the set of parameter types supported in Phase A.
var parameterTypes = map[string]bool{
	"integer":     true,
	"number":      true,
	"boolean":     true,
	"string":      true,
	"enum":        true,
	"string_list": true,
}

// ParameterIDPattern is the exact syntax for parameter keys (and aliases,
// which are former keys). internal/rules validates {{ .Config.<key> }} template
// placeholders against the same rule via ValidParameterID, so the two
// definitions cannot drift.
const ParameterIDPattern = `^[a-z][a-z0-9_]{0,63}$`

// paramIDRe constrains parameter IDs (and aliases, which are former IDs).
var paramIDRe = regexp.MustCompile(ParameterIDPattern)

// ValidParameterID reports whether s is a syntactically valid parameter ID.
func ValidParameterID(s string) bool { return paramIDRe.MatchString(s) }

// udfNameRe extracts the function name from a UDF DDL statement. Seed
// definitions use both scalar (CREATE [OR REPLACE] FUNCTION) and aggregate
// (CREATE [OR REPLACE] AGGREGATE FUNCTION) forms; CREATE FUNCTION IF NOT
// EXISTS is also accepted. The name may be backtick-quoted — quoting must be
// symmetric, and the captured name excludes the backticks (group 1 for the
// quoted alternative, group 2 for the bare one).
var udfNameRe = regexp.MustCompile(`(?i)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:AGGREGATE\s+)?FUNCTION\s+(?:IF\s+NOT\s+EXISTS\s+)?` +
	"(?:`([A-Za-z_][A-Za-z0-9_]*)`" + `|([A-Za-z_][A-Za-z0-9_]*))`)

// udfReservedNames are SQL keywords that can never be a real function name.
// Extracting one of them means the regex latched onto DDL syntax (e.g. a
// malformed `CREATE FUNCTION IF ...`), so it is reported as a parse error
// rather than silently registering a bogus UDF name.
var udfReservedNames = map[string]bool{
	"create":    true,
	"or":        true,
	"replace":   true,
	"aggregate": true,
	"function":  true,
	"if":        true,
	"not":       true,
	"exists":    true,
}

// stripSQLComments removes `--` line comments from a SQL string. Changelog
// comments inside DDL/SQL reasonably mention statement keywords (e.g. an old
// CREATE FUNCTION name); stripping keeps them from being parsed as live SQL.
func stripSQLComments(q string) string {
	var b strings.Builder
	for _, line := range strings.Split(q, "\n") {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// UDFNames extracts the declared function name from each UDF DDL string,
// in declaration order. Line comments are stripped before matching so a
// changelog comment mentioning CREATE FUNCTION cannot shadow the real
// declaration. It returns an error if any DDL does not parse well enough
// to yield a plausible function name.
func (d Definition) UDFNames() ([]string, error) {
	names := make([]string, 0, len(d.UDFs))
	for i, ddl := range d.UDFs {
		m := udfNameRe.FindStringSubmatch(stripSQLComments(ddl))
		if m == nil {
			return nil, fmt.Errorf("udfs[%d]: cannot extract function name (expected CREATE [OR REPLACE] [AGGREGATE] FUNCTION [IF NOT EXISTS] <name>)", i)
		}
		name := m[1]
		if name == "" {
			name = m[2]
		}
		if udfReservedNames[strings.ToLower(name)] {
			return nil, fmt.Errorf("udfs[%d]: extracted function name %q is a reserved word — DDL is malformed", i, name)
		}
		names = append(names, name)
	}
	return names, nil
}

// hashPayload fixes the canonical field order for Hash. json.Marshal of a
// struct emits fields in declaration order, which makes the encoding
// deterministic.
type hashPayload struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	Version           string      `json:"version"`
	Description       string      `json:"description"`
	Severity          string      `json:"severity"`
	Category          string      `json:"category"`
	FPRate            string      `json:"fp_rate"`
	HookSources       string      `json:"hook_sources"`
	RequiredTelemetry string      `json:"required_telemetry"`
	Tags              []string    `json:"tags"`
	UDFs              []string    `json:"udfs"`
	SQLTemplate       string      `json:"sql_template"`
	Parameters        []Parameter `json:"parameters"`
}

// normSlice maps a nil slice to an empty one so nil and [] serialize (and
// therefore hash) identically. YAML/JSON round-trips flip freely between the
// two, and the persisted definition_hash must not depend on that.
func normSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// Hash returns the SHA-256 hex digest of a canonical serialization of the
// definition content: id, name, version, description, severity, category,
// fp_rate, hook_sources, required_telemetry, tags, udfs, sql_template, and
// parameters (in declared order). It covers definition content only —
// provenance fields (Origin, Author) are deliberately excluded, so the same
// content hashes identically whether builtin, imported, or custom.
// Nil slices are normalized to empty before serialization (see normSlice).
func (d Definition) Hash() string {
	params := make([]Parameter, len(d.Parameters))
	for i, p := range d.Parameters {
		p.Options = normSlice(p.Options)
		p.Aliases = normSlice(p.Aliases)
		params[i] = p
	}
	data, err := json.Marshal(hashPayload{
		ID:                d.ID,
		Name:              d.Name,
		Version:           d.Version,
		Description:       d.Description,
		Severity:          d.Severity,
		Category:          d.Category,
		FPRate:            d.FPRate,
		HookSources:       d.HookSources,
		RequiredTelemetry: d.RequiredTelemetry,
		Tags:              normSlice(d.Tags),
		UDFs:              normSlice(d.UDFs),
		SQLTemplate:       d.SQLTemplate,
		Parameters:        params,
	})
	if err != nil {
		// json.Marshal only fails on unsupported Default values (chan, func,
		// cycles) which Validate-passing definitions never contain. Fall back
		// to hashing the error so the result is still deterministic non-empty.
		data = []byte("marshal-error:" + err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Validate performs structural validation of the definition. It owns schema
// shape only — deep parameter value semantics (e.g. default-vs-min/max
// consistency) are enforced by the rule compiler, not here.
func (d Definition) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("id is required")
	}
	// The rule ID is interpolated (dash-stripped, unescaped) into MV names and
	// system.tables lookups in internal/timeplus, so it must be restricted to a
	// safe charset at the single point every definition passes through. This
	// mirrors Manager.ValidateID; enforcing it here covers seed, local, and
	// imported definitions, not only API-created ones.
	if len(d.ID) > 64 {
		return fmt.Errorf("id %q must be 64 characters or fewer", d.ID)
	}
	for _, c := range d.ID {
		if !isIDChar(c) {
			return fmt.Errorf("id %q may only contain letters, digits, hyphens, and underscores", d.ID)
		}
	}
	if d.Name == "" {
		return fmt.Errorf("name is required")
	}
	if d.Version == "" {
		return fmt.Errorf("version is required")
	}
	if d.SQLTemplate == "" {
		return fmt.Errorf("sql_template is required")
	}

	seen := make(map[string]string, len(d.Parameters)) // name → "key"/"alias" owner param
	claim := func(name, kind, owner string) error {
		if prev, dup := seen[name]; dup {
			return fmt.Errorf("parameter %s %q duplicates %s", kind, name, prev)
		}
		seen[name] = fmt.Sprintf("%s of parameter %q", kind, owner)
		return nil
	}
	for i, p := range d.Parameters {
		if !paramIDRe.MatchString(p.Key) {
			return fmt.Errorf("config[%d]: key %q must match %s", i, p.Key, paramIDRe.String())
		}
		if err := claim(p.Key, "key", p.Key); err != nil {
			return fmt.Errorf("config[%d]: %w", i, err)
		}
		for _, a := range p.Aliases {
			if !paramIDRe.MatchString(a) {
				return fmt.Errorf("config[%d] (%s): alias %q must match %s", i, p.Key, a, paramIDRe.String())
			}
			if err := claim(a, "alias", p.Key); err != nil {
				return fmt.Errorf("config[%d] (%s): %w", i, p.Key, err)
			}
		}
		if !parameterTypes[p.Type] {
			return fmt.Errorf("config[%d] (%s): unknown type %q", i, p.Key, p.Type)
		}
		if p.Secret {
			return fmt.Errorf("config[%d] (%s): secret parameters are not supported — parameters are rendered into SQL and must never store secrets", i, p.Key)
		}
		if p.Default == nil {
			return fmt.Errorf("config[%d] (%s): default is required", i, p.Key)
		}
		if p.Type == "enum" && len(p.Options) == 0 {
			return fmt.Errorf("config[%d] (%s): enum requires non-empty options", i, p.Key)
		}
	}

	names, err := d.UDFNames()
	if err != nil {
		return err
	}
	udfSeen := make(map[string]bool, len(names))
	for _, n := range names {
		if udfSeen[n] {
			return fmt.Errorf("duplicate UDF function name %q within definition", n)
		}
		udfSeen[n] = true
	}
	return nil
}

// ValidateUDFOwnership rejects a definition that declares a UDF function name
// already declared by a different definition currently in the catalog.
// Re-saving the same definition (same ID) is always allowed.
//
// LIMITATION (Phase B): ownership is checked against catalog definitions only
// (m.All()), not against the UDFs actually deployed by installed rules
// (recorded per snapshot in agentguard_rules.deployed_udfs_json). If an
// installed rule's definition is absent from or changed in the catalog, a new
// or imported rule can claim/replace a UDF that a live MV still depends on,
// since global CREATE OR REPLACE FUNCTION would overwrite it. The robust fix is
// to consult the installed snapshots' deployed UDFs (the real ownership ledger),
// which requires a store read this catalog-only method does not have.
func (m *Manager) ValidateUDFOwnership(d Definition) error {
	names, err := d.UDFNames()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	mine := make(map[string]bool, len(names))
	for _, n := range names {
		mine[n] = true
	}
	for _, other := range m.All() {
		if other.ID == d.ID {
			continue
		}
		otherNames, err := other.UDFNames()
		if err != nil {
			continue // unparseable foreign UDFs cannot claim ownership
		}
		for _, n := range otherNames {
			if mine[n] {
				return fmt.Errorf("UDF function %q is already declared by rule %q", n, other.ID)
			}
		}
	}
	return nil
}
