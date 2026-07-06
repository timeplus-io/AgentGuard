package rules

import (
	"fmt"
	"strings"

	"github.com/timeplus/agentguard/internal/catalog"
)

// PlanUpgrade computes what happens to an installed rule's overrides when its
// definition is upgraded from current to next. Every override in the input is
// accounted for exactly once — preserved (possibly under a renamed ID via the
// next parameter's Aliases), dropped with a warning, or blocked with a
// conflict. No path silently resets an override.
//
// Semantics per override key k:
//   - k declared in next with the EXACT same type as in current, and the
//     value satisfies next's constraints → preserved under k.
//   - k removed in next but exactly one next parameter lists k in Aliases →
//     migrated under the new ID (type must match exactly and the value must
//     satisfy the new constraints, otherwise conflict).
//   - k removed with no alias → dropped with a warning.
//   - Type change, alias ambiguity, constraint violation, or two overrides
//     mapping to the same next parameter → conflict (blocks the upgrade).
//   - k not declared by current at all (stale override) → dropped with a
//     warning.
//
// Parameters that exist only in next take their defaults and never appear in
// the result. The returned error is reserved for misuse (mismatched
// definition IDs); compatibility outcomes are reported via the result.
func PlanUpgrade(current catalog.Definition, next catalog.Definition, overrides map[string]any) (CompatibilityResult, error) {
	if current.ID != next.ID {
		return CompatibilityResult{}, fmt.Errorf("definition ID mismatch: current %q vs next %q", current.ID, next.ID)
	}

	curParams := paramIndex(current)
	nextParams := paramIndex(next)
	// aliasTargets maps an old parameter ID to every next parameter that
	// claims it as an alias. catalog.Definition.Validate forbids duplicate
	// alias claims within a definition, but PlanUpgrade does not assume next
	// was validated, so ambiguity is still handled (as a conflict).
	aliasTargets := make(map[string][]catalog.Parameter)
	for _, p := range next.Parameters {
		for _, a := range p.Aliases {
			aliasTargets[a] = append(aliasTargets[a], p)
		}
	}

	res := CompatibilityResult{Overrides: make(map[string]any)}
	preservedFrom := make(map[string]string) // next ID → override key it came from

	preserve := func(key string, np catalog.Parameter, v any) {
		if from, taken := preservedFrom[np.Key]; taken {
			res.Conflicts = append(res.Conflicts, CompatibilityIssue{
				Parameter: key,
				Message:   fmt.Sprintf("override maps to parameter %q, which is already set by the override for %q", np.Key, from),
			})
			return
		}
		nv, err := normalizeValue(np, v)
		if err != nil {
			res.Conflicts = append(res.Conflicts, CompatibilityIssue{
				Parameter: key,
				Message:   fmt.Sprintf("override value no longer valid for parameter %q: %s", np.Key, err.Error()),
			})
			return
		}
		res.Overrides[np.Key] = nv
		preservedFrom[np.Key] = key
	}

	for _, k := range sortedKeys(overrides) {
		v := overrides[k]
		cur, inCurrent := curParams[k]
		if !inCurrent {
			res.Warnings = append(res.Warnings, CompatibilityIssue{
				Parameter: k,
				Message:   "override references a parameter not declared by the current definition; dropped",
			})
			continue
		}

		if np, ok := nextParams[k]; ok {
			if np.Type != cur.Type {
				res.Conflicts = append(res.Conflicts, CompatibilityIssue{
					Parameter: k,
					Message:   fmt.Sprintf("type changed from %q to %q", cur.Type, np.Type),
				})
				continue
			}
			preserve(k, np, v)
			continue
		}

		targets := aliasTargets[k]
		switch len(targets) {
		case 0:
			res.Warnings = append(res.Warnings, CompatibilityIssue{
				Parameter: k,
				Message:   fmt.Sprintf("parameter removed in version %s; override dropped", next.Version),
			})
		case 1:
			np := targets[0]
			if np.Type != cur.Type {
				res.Conflicts = append(res.Conflicts, CompatibilityIssue{
					Parameter: k,
					Message:   fmt.Sprintf("aliased to %q but type changed from %q to %q", np.Key, cur.Type, np.Type),
				})
				continue
			}
			preserve(k, np, v)
		default:
			ids := make([]string, len(targets))
			for i, t := range targets {
				ids[i] = t.Key
			}
			res.Conflicts = append(res.Conflicts, CompatibilityIssue{
				Parameter: k,
				Message:   fmt.Sprintf("alias is ambiguous: parameters %s all claim %q", strings.Join(ids, ", "), k),
			})
		}
	}
	return res, nil
}

// PlanUpgradeOverrides computes how an installed instance's stored, already-
// normalized overrides map onto a (possibly newer) definition WITHOUT access to
// the previous definition.
//
// Phase A persists a rule instance's rendered_sql, overrides, and
// definition_hash, but NOT the prior parameter schema, so the previous
// parameter types are unavailable at upgrade time (a builtin's old version is
// gone once a newer binary ships; a custom definition's old YAML is overwritten
// on save). The two-definition PlanUpgrade is therefore unusable from persisted
// state alone; this is the entry point the lifecycle service uses for upgrades.
//
// The accounting is the same as PlanUpgrade — every override is preserved
// (possibly under an aliased ID), dropped with a warning, or blocked with a
// conflict, and no path silently resets an override — with one fidelity loss:
// a parameter whose TYPE changed across versions cannot be reported as an
// explicit "type changed" conflict, because the old type is unknown. Instead the
// stored value (in its old canonical form) fails normalization against the new
// parameter and surfaces as a value conflict, which still blocks the upgrade.
// The exceptions are same-shape families whose stored canonical values can
// normalize against either type: integer↔float (json.Number) and list↔multi_choice
// ([]string, only when the values satisfy the next options). Those changes are
// silently PRESERVED rather than blocked. This is value preservation, not a silent
// reset, so it still honors the "never silently reset an override" rule; every
// other type change fails normalization and conflicts as expected. Alias migration
// and ambiguity detection are unaffected, since both derive only from the next
// definition's declared aliases.
func PlanUpgradeOverrides(next catalog.Definition, overrides map[string]any) CompatibilityResult {
	nextParams := paramIndex(next)
	// aliasTargets maps an old parameter ID to every next parameter that claims
	// it as an alias. Duplicate claims (forbidden by Definition.Validate) are
	// still handled here as a conflict, since next may not have been validated.
	aliasTargets := make(map[string][]catalog.Parameter)
	for _, p := range next.Parameters {
		for _, a := range p.Aliases {
			aliasTargets[a] = append(aliasTargets[a], p)
		}
	}

	res := CompatibilityResult{Overrides: make(map[string]any)}
	preservedFrom := make(map[string]string) // next ID → override key it came from

	preserve := func(key string, np catalog.Parameter, v any) {
		if from, taken := preservedFrom[np.Key]; taken {
			res.Conflicts = append(res.Conflicts, CompatibilityIssue{
				Parameter: key,
				Message:   fmt.Sprintf("override maps to parameter %q, which is already set by the override for %q", np.Key, from),
			})
			return
		}
		nv, err := normalizeValue(np, v)
		if err != nil {
			res.Conflicts = append(res.Conflicts, CompatibilityIssue{
				Parameter: key,
				Message:   fmt.Sprintf("override value no longer valid for parameter %q: %s", np.Key, err.Error()),
			})
			return
		}
		res.Overrides[np.Key] = nv
		preservedFrom[np.Key] = key
	}

	for _, k := range sortedKeys(overrides) {
		v := overrides[k]
		if np, ok := nextParams[k]; ok {
			preserve(k, np, v)
			continue
		}
		targets := aliasTargets[k]
		switch len(targets) {
		case 0:
			res.Warnings = append(res.Warnings, CompatibilityIssue{
				Parameter: k,
				Message:   fmt.Sprintf("parameter not present in version %s; override dropped", next.Version),
			})
		case 1:
			preserve(k, targets[0], v)
		default:
			ids := make([]string, len(targets))
			for i, t := range targets {
				ids[i] = t.Key
			}
			res.Conflicts = append(res.Conflicts, CompatibilityIssue{
				Parameter: k,
				Message:   fmt.Sprintf("alias is ambiguous: parameters %s all claim %q", strings.Join(ids, ", "), k),
			})
		}
	}
	return res
}
