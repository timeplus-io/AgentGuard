package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/rules"
)

// ruleCatalog exposes the rule catalog — builtin + custom Definitions that
// can be installed. The catalog lives in the Go binary / on disk
// (catalog.Manager), NOT in a Timeplus stream, so it is unreachable via
// run_sql and needs a real tool (#219: tools only where SQL can't).
type ruleCatalog struct {
	mgr *catalog.Manager
	lc  *rules.Lifecycle
}

// NewRuleCatalog returns the read-only "get_rule_catalog" skill.
func NewRuleCatalog(mgr *catalog.Manager, lc *rules.Lifecycle) Skill {
	return ruleCatalog{mgr: mgr, lc: lc}
}

func (ruleCatalog) Name() string { return "get_rule_catalog" }
func (ruleCatalog) Description() string {
	return "List the rule catalog: builtin + custom rule definitions available to install, with installed/enabled/upgradeable status. Pass {id} for one definition's full detail including its parameter schema and SQL template. The catalog is not stored in Timeplus — this tool is the only way to see installable rules."
}
func (ruleCatalog) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "description": "optional definition id for full detail"},
		},
	}
}
func (ruleCatalog) Access() AccessClass { return ReadOnly }

// catalogItem is the list-view summary, mirroring the /api/rules/catalog
// handler's shape so the agent sees the same fields as the Rule Packs page.
type catalogItem struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	Description       string   `json:"description"`
	Severity          string   `json:"severity"`
	Category          string   `json:"category"`
	FPRate            string   `json:"fp_rate,omitempty"`
	RequiredTelemetry string   `json:"required_telemetry,omitempty"`
	Origin            string   `json:"origin,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	Installed         bool     `json:"installed"`
	Enabled           bool     `json:"enabled,omitempty"`
	InstalledVersion  string   `json:"installed_version,omitempty"`
	Upgradeable       bool     `json:"upgradeable,omitempty"`
}

func (rc ruleCatalog) Execute(_ context.Context, run *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(args, &a)
	a.ID = strings.TrimSpace(a.ID)

	// Installed status comes from the rules snapshot store. Degrade
	// gracefully if Timeplus is unreachable: the catalog itself is local,
	// and a missing status must not hide the list of installable rules.
	installed := map[string]rules.RuleInstance{}
	statusNote := ""
	if insts, err := rc.lc.For(&run.Cfg.Timeplus).List(); err != nil {
		statusNote = "installed status unavailable: " + err.Error()
	} else {
		for _, inst := range insts {
			installed[inst.RuleID] = inst
		}
	}

	if a.ID != "" {
		def, ok := rc.mgr.Lookup(a.ID)
		if !ok {
			return fmt.Sprintf("unknown catalog id %q — call get_rule_catalog without arguments for the full list", a.ID), nil
		}
		out := map[string]any{"definition": def}
		if inst, ok := installed[def.ID]; ok {
			out["installed"] = true
			out["enabled"] = inst.Enabled
			out["installed_version"] = inst.DefinitionVersion
			out["overrides"] = inst.Overrides
			out["upgradeable"] = rules.CanonicalDefinitionHash(def) != inst.DefinitionHash
		} else {
			out["installed"] = false
		}
		if statusNote != "" {
			out["note"] = statusNote
		}
		b, err := json.Marshal(out)
		if err != nil {
			return "error: " + err.Error(), nil
		}
		return string(b), nil
	}

	entries := rc.mgr.All()
	items := make([]catalogItem, 0, len(entries))
	for _, e := range entries {
		item := catalogItem{
			ID: e.ID, Name: e.Name, Version: e.Version, Description: e.Description,
			Severity: e.Severity, Category: e.Category, FPRate: e.FPRate,
			RequiredTelemetry: e.RequiredTelemetry, Origin: e.Origin, Tags: e.Tags,
		}
		if inst, ok := installed[e.ID]; ok {
			item.Installed = true
			item.Enabled = inst.Enabled
			item.InstalledVersion = inst.DefinitionVersion
			item.Upgradeable = rules.CanonicalDefinitionHash(e) != inst.DefinitionHash
		}
		items = append(items, item)
	}
	out := map[string]any{"catalog": items, "count": len(items)}
	if statusNote != "" {
		out["note"] = statusNote
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	return string(b), nil
}
