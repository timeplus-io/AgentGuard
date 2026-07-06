package timeplus

import (
	"strings"
	"testing"
)

// cloneExpectedSchema returns a fresh copy of the complete expected
// agentguard_rules column→type map so tests can mutate it freely.
func cloneExpectedSchema() map[string]string {
	cols := make(map[string]string, len(expectedRulesSchema))
	for name, typ := range expectedRulesSchema {
		cols[name] = typ
	}
	return cols
}

func TestValidateRulesStreamSchema_ExactMatchPasses(t *testing.T) {
	if err := validateRulesStreamSchema(cloneExpectedSchema()); err != nil {
		t.Fatalf("exact-match schema rejected: %v", err)
	}
}

func TestValidateRulesStreamSchema_IgnoresInternalTpColumns(t *testing.T) {
	cols := cloneExpectedSchema()
	cols["_tp_time"] = "datetime64(3, 'UTC')"
	cols["_tp_sn"] = "int64"
	if err := validateRulesStreamSchema(cols); err != nil {
		t.Fatalf("internal _tp_* columns must be ignored, got: %v", err)
	}
}

func TestValidateRulesStreamSchema_LegacySchemaFailsMentioningReprovision(t *testing.T) {
	// The pre-snapshot schema: query_sql instead of rendered_sql, version
	// instead of definition_version, no snapshot/revision columns,
	// datetime64(3) timestamps.
	legacy := map[string]string{
		"rule_id":            "low_cardinality(string)",
		"name":               "string",
		"version":            "string",
		"description":        "string",
		"query_sql":          "string",
		"enabled":            "bool",
		"deleted":            "bool",
		"severity":           "low_cardinality(string)",
		"category":           "low_cardinality(string)",
		"required_telemetry": "string",
		"fp_rate":            "low_cardinality(string)",
		"hook_sources":       "string",
		"block_policy":       "low_cardinality(string)",
		"updated_at":         "datetime64(3)",
	}
	err := validateRulesStreamSchema(legacy)
	if err == nil {
		t.Fatal("legacy schema accepted; want incompatible-schema error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "reprovision") {
		t.Errorf("error must tell the operator to reprovision, got: %v", err)
	}
	if !strings.Contains(msg, "revision") {
		t.Errorf("error should mention the missing revision column, got: %v", err)
	}
	if !strings.Contains(msg, "query_sql") {
		t.Errorf("error should mention the unexpected query_sql column, got: %v", err)
	}
}

func TestValidateRulesStreamSchema_WrongRevisionTypeFails(t *testing.T) {
	cols := cloneExpectedSchema()
	cols["revision"] = "string"
	err := validateRulesStreamSchema(cols)
	if err == nil {
		t.Fatal("wrong revision type accepted; want error")
	}
	if !strings.Contains(err.Error(), "revision") {
		t.Errorf("error should name the revision column, got: %v", err)
	}
}

func TestValidateRulesStreamSchema_WrongUpdatedAtPrecisionFails(t *testing.T) {
	cols := cloneExpectedSchema()
	cols["updated_at"] = "datetime64(3)"
	if err := validateRulesStreamSchema(cols); err == nil {
		t.Fatal("datetime64(3) updated_at accepted; want datetime64(9) enforced")
	}
}

func TestValidateRulesStreamSchema_ExtraColumnFails(t *testing.T) {
	cols := cloneExpectedSchema()
	cols["bonus_column"] = "string"
	err := validateRulesStreamSchema(cols)
	if err == nil {
		t.Fatal("extra unexpected column accepted; want complete-set comparison failure")
	}
	if !strings.Contains(err.Error(), "bonus_column") {
		t.Errorf("error should name the unexpected column, got: %v", err)
	}
}

func TestValidateRulesStreamSchema_MissingColumnFails(t *testing.T) {
	cols := cloneExpectedSchema()
	delete(cols, "deployed_udfs_json")
	err := validateRulesStreamSchema(cols)
	if err == nil {
		t.Fatal("missing deployed_udfs_json accepted; want error")
	}
	if !strings.Contains(err.Error(), "deployed_udfs_json") {
		t.Errorf("error should name the missing column, got: %v", err)
	}
}

func TestDecodeDeployedUDFs(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{"empty string", "", 0, false},
		{"blank string", "   \n\t", 0, false},
		{"empty array", "[]", 0, false},
		{"two statements", `["CREATE OR REPLACE FUNCTION f1 ...","CREATE OR REPLACE FUNCTION f2 ..."]`, 2, false},
		{"invalid json", "{not json", 0, true},
		{"wrong shape", `{"a":1}`, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeDeployedUDFs(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("decodeDeployedUDFs(%q) = %v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeDeployedUDFs(%q) unexpected error: %v", tc.in, err)
			}
			if len(got) != tc.want {
				t.Fatalf("decodeDeployedUDFs(%q) = %d statements, want %d", tc.in, len(got), tc.want)
			}
		})
	}
}

// parseRulesStreamDDL extracts the column name→type map declared in the
// ddlRulesStream constant by reading the lines between its parentheses. Column
// types in this DDL contain no internal spaces, so "first field = name, rest =
// type" is unambiguous.
func parseRulesStreamDDL(t *testing.T) map[string]string {
	t.Helper()
	cols := make(map[string]string)
	inBody := false
	for _, raw := range strings.Split(ddlRulesStream, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !inBody {
			if line == "(" {
				inBody = true
			}
			continue
		}
		if strings.HasPrefix(line, ")") {
			break
		}
		line = strings.TrimSuffix(line, ",")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			t.Fatalf("cannot parse DDL column line: %q", raw)
		}
		cols[fields[0]] = strings.Join(fields[1:], " ")
	}
	return cols
}

// TestRulesStreamDDLMatchesExpectedSchema guards against the DDL constant and
// the validation map drifting apart. It asserts exact name+type set-equality so
// that a type drift (e.g. revision uint32 vs uint64) or an extra/renamed DDL
// column fails the build — not just a missing column name. Without this, such a
// drift would slip through and make ensureRulesStream reject the very stream it
// just created.
func TestRulesStreamDDLMatchesExpectedSchema(t *testing.T) {
	ddl := parseRulesStreamDDL(t)
	for name, want := range expectedRulesSchema {
		got, ok := ddl[name]
		if !ok {
			t.Errorf("ddlRulesStream missing expected column %q (%s)", name, want)
			continue
		}
		if got != want {
			t.Errorf("ddlRulesStream column %q has type %q, want %q", name, got, want)
		}
	}
	for name := range ddl {
		if _, ok := expectedRulesSchema[name]; !ok {
			t.Errorf("ddlRulesStream declares unexpected column %q", name)
		}
	}
	for _, banned := range []string{"query_sql", "DEFAULT"} {
		if strings.Contains(ddlRulesStream, banned) {
			t.Errorf("ddlRulesStream still contains legacy artifact %q", banned)
		}
	}
}

// TestValidateRulesStreamSchema_ToleratesDatetimeTimezone ensures a freshly
// created stream is not rejected just because system.columns reports the
// datetime64 column with a session timezone argument the DDL did not declare.
func TestValidateRulesStreamSchema_ToleratesDatetimeTimezone(t *testing.T) {
	cols := cloneExpectedSchema()
	cols["updated_at"] = "datetime64(9, 'UTC')"
	if err := validateRulesStreamSchema(cols); err != nil {
		t.Fatalf("datetime64(9, 'UTC') must be treated as datetime64(9): %v", err)
	}
}

func TestNormalizeColumnType(t *testing.T) {
	cases := map[string]string{
		"datetime64(9)":           "datetime64(9)",
		"datetime64(9, 'UTC')":    "datetime64(9)",
		"DateTime64(9, 'UTC')":    "datetime64(9)",
		"  datetime64(9)  ":       "datetime64(9)",
		"datetime64(3)":           "datetime64(3)", // precision preserved
		"uint64":                  "uint64",
		"UInt64":                  "uint64",
		"low_cardinality(string)": "low_cardinality(string)",
		"string":                  "string",
	}
	for in, want := range cases {
		if got := normalizeColumnType(in); got != want {
			t.Errorf("normalizeColumnType(%q) = %q, want %q", in, got, want)
		}
	}
}
