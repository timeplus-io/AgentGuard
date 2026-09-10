package tpapp

import (
	"fmt"

	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/rules"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// CoreRuleIDs are the Core Protection package members shipped with the app,
// installed PAUSED. Everything else in the catalog stays AgentGuard-only.
var CoreRuleIDs = []string{"rp-001", "rp-002", "rp-003", "rp-004"}

// Resource is one manifest resource + its single-statement DDL.
type Resource struct {
	Kind string // stream | mutable_stream | materialized_view | input | udf | system
	Name string // manifest name; also the object name for stream/mv/input/udf
	File string // ddl/NNN_<name>.sql, assigned by Build
	SQL  string
}

// RuleArtifacts compiles each core rule with its default parameters and
// returns, per rule in CoreRuleIDs order: its UDFs (global, CREATE OR
// REPLACE), the MV writing INTO agentguard_security_events, and a system
// resource that pauses the MV. names is the set of app resource names to
// qualify inside the rendered SQL.
func RuleArtifacts(names []string) ([]Resource, error) {
	m, err := catalog.New(catalog.SeedFS, "")
	if err != nil {
		return nil, fmt.Errorf("load seed catalog: %w", err)
	}
	var out []Resource
	for _, id := range CoreRuleIDs {
		def, ok := m.Lookup(id)
		if !ok {
			return nil, fmt.Errorf("rule %s not in seed catalog", id)
		}
		compiled, err := rules.Compile(def, nil)
		if err != nil {
			return nil, fmt.Errorf("compile %s with defaults: %w", id, err)
		}
		udfNames, err := catalog.ExtractUDFNames(def.UDFs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		for i, ddl := range def.UDFs {
			out = append(out, Resource{Kind: "udf", Name: udfNames[i], SQL: StripTrailingSemicolon(ddl)})
		}
		mv := timeplus.MVName(id)
		body := StripTrailingSemicolon(Qualify(compiled.SQL, names))
		out = append(out,
			Resource{
				Kind: "materialized_view",
				Name: mv,
				SQL: fmt.Sprintf("CREATE MATERIALIZED VIEW IF NOT EXISTS %s.%s\nINTO %s.agentguard_security_events\nAS %s",
					DBPlaceholder, mv, DBPlaceholder, body),
			},
			Resource{
				Kind: "system",
				Name: "pause_" + mv,
				SQL:  fmt.Sprintf("SYSTEM PAUSE MATERIALIZED VIEW %s.%s", DBPlaceholder, mv),
			},
		)
	}
	return out, nil
}
