package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Sentinel errors for catalog operations. Callers can use errors.Is to
// distinguish error classes without importing implementation details.
var (
	// ErrNotFound is returned when a requested definition is not in the catalog.
	ErrNotFound = errors.New("rule definition not found")
	// ErrExists is returned when a new definition ID conflicts with an existing one.
	ErrExists = errors.New("rule definition already exists")
	// ErrInvalid is returned when a definition fails structural or semantic validation.
	ErrInvalid = errors.New("invalid rule definition")
	// ErrUDFConflict is returned when a definition declares a UDF function name
	// that is already owned by another rule in the catalog or the deployed ledger.
	// This is an operator-correctable conflict (rename the function or clone first).
	ErrUDFConflict = errors.New("UDF function name conflict")
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

// parameterTypes is the set of supported parameter types. The vocabulary mirrors
// the Timeplus Apps manifest (integer, float, bool, string, choice, list) plus two
// AgentGuard-specific extensions (regex, duration) that have no Apps equivalent.
// The Phase A internal names (number/boolean/enum/string_list) were renamed to the
// Apps tokens before release and are no longer accepted.
var parameterTypes = map[string]bool{
	"integer":  true,
	"float":    true, // numeric (Phase A: "number")
	"bool":     true, // boolean (Phase A: "boolean")
	"string":   true,
	"choice":   true, // single-select from options (Phase A: "enum")
	"list":     true, // free-form string list (Phase A: "string_list")
	"regex":    true, // AG extension: RE2-validated string rendered as a single-quoted SQL literal
	"duration": true, // AG extension: int64 count of a fixed unit rendered as a bare decimal integer
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

// ExtractUDFNames extracts the declared function name from each UDF DDL string
// in declaration order. Line comments are stripped before matching so a
// changelog comment mentioning CREATE FUNCTION cannot shadow the real
// declaration. It returns an error if any DDL does not parse well enough
// to yield a plausible function name.
//
// This is a package-level function so that internal/rules can call it to
// inspect deployed UDFs stored in installed snapshots (the real ownership
// ledger) without needing access to a Definition.
func ExtractUDFNames(ddls []string) ([]string, error) {
	names := make([]string, 0, len(ddls))
	for i, ddl := range ddls {
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

// UDFNames extracts the declared function name from each UDF DDL string
// declared in this definition. It delegates to ExtractUDFNames.
func (d Definition) UDFNames() ([]string, error) {
	return ExtractUDFNames(d.UDFs)
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

// validate is the internal structural-validation implementation. It is
// separated from the public Validate so the sentinel ErrInvalid can be wrapped
// exactly once at the public boundary without double-wrapping.
func (d Definition) validate() error {
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
		if p.Type == "choice" && len(p.Options) == 0 {
			return fmt.Errorf("config[%d] (%s): choice requires non-empty options", i, p.Key)
		}
		if p.Type == "duration" && p.Unit == "" {
			return fmt.Errorf("config[%d] (%s): duration requires a unit", i, p.Key)
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

// Validate performs structural validation of the definition and wraps any
// error with ErrInvalid so callers can use errors.Is(err, ErrInvalid) to
// detect schema defects without string matching. It owns schema shape only —
// deep parameter value semantics (e.g. default-vs-min/max consistency) are
// enforced by the rule compiler, not here.
func (d Definition) Validate() error {
	if err := d.validate(); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	return nil
}

// RescopeUDFs returns a copy of def with all UDF function names (and all
// references to those names in UDF DDL bodies and SQLTemplate) renamed so they
// are unique to newID. The naming convention is ag_<normalizedNewID>_<index>
// where the index is the 0-based position of the UDF in the UDFs slice and
// dashes in newID are stripped.
//
// This is called by Manager.Clone to ensure the clone's UDFs do not collide
// with the source rule's UDFs in the Timeplus function registry.
func RescopeUDFs(def Definition, newID string) (Definition, error) {
	if len(def.UDFs) == 0 {
		return def, nil
	}

	norm := strings.ReplaceAll(newID, "-", "")

	oldNames, err := def.UDFNames()
	if err != nil {
		return Definition{}, fmt.Errorf("extract source UDF names: %w", err)
	}

	// Guard: no duplicate source names (Validate already bars this, but
	// RescopeUDFs can be called independently so we check again).
	seenOld := make(map[string]bool, len(oldNames))
	for _, n := range oldNames {
		if seenOld[n] {
			return Definition{}, fmt.Errorf("duplicate UDF function name %q in source definition", n)
		}
		seenOld[n] = true
	}

	// Build old→new name map and compile per-name word-boundary regexps.
	type replacement struct {
		re      *regexp.Regexp
		newName string
	}
	replacements := make([]replacement, len(oldNames))
	for i, old := range oldNames {
		newName := fmt.Sprintf("ag_%s_%d", norm, i)
		replacements[i] = replacement{
			re:      regexp.MustCompile(`\b` + regexp.QuoteMeta(old) + `\b`),
			newName: newName,
		}
	}

	rewrite := func(s string) string {
		for _, r := range replacements {
			s = r.re.ReplaceAllString(s, r.newName)
		}
		return s
	}

	newUDFs := make([]string, len(def.UDFs))
	for i, udf := range def.UDFs {
		newUDFs[i] = rewrite(udf)
	}

	out := def
	out.UDFs = newUDFs
	out.SQLTemplate = rewrite(def.SQLTemplate)
	return out, nil
}

// ValidateUDFOwnership rejects a definition that declares a UDF function name
// already declared by a different definition currently in the catalog.
// Re-saving the same definition (same ID) is always allowed.
//
// NOTE: this catalog-only check is not the complete ownership gate. The
// lifecycle Service (internal/rules) additionally consults installed snapshots'
// deployed_udfs_json (the real ownership ledger) via
// rules.Service.checkUDFOwnership, which wraps this call. A definition present
// in the catalog cannot alone assert ownership of a UDF deployed by a live MV
// whose definition has since been removed or changed.
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
				return fmt.Errorf("%w: UDF function %q is already declared by rule %q", ErrUDFConflict, n, other.ID)
			}
		}
	}
	return nil
}
