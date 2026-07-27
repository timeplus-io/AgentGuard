package skills

import (
	"context"
	"encoding/json"
	"strings"
)

// Rule-lifecycle GatedWrite skills (#222 batch 2). Like writetools.go, each
// skill is a thin validated arg-parser around logic INJECTED from the
// handlers package — the same lifecycle-service calls (parameter compiler,
// candidate preflight, per-rule-serialized drop/create MV) and hold-
// abandonment side effects the HTTP endpoints run. Everything here is
// Access() == GatedWrite: the loop executes only after explicit in-chat
// human approval (#221), and the whole group sits behind the
// agentguard_tools_enabled switch.

// RuleLifecycleFuncs bundles the injected rule-lifecycle operations. Each
// returns a model-readable result summary (typically the updated instance
// as JSON) or an error.
type RuleLifecycleFuncs struct {
	Install    func(ruleID string, overrides map[string]any, user, reason string) (string, error)
	Configure  func(ruleID string, overrides map[string]any, user, reason string) (string, error)
	SetEnabled func(ruleID string, enabled bool, user string) (string, error)
	SetPolicy  func(ruleID, policy, user, reason string) (string, error)
	Upgrade    func(ruleID, user string) (string, error)
	Uninstall  func(ruleID, user string) (string, error)
	Sync       func(user string) (string, error)
}

// ruleLifecycleSkill is the shared shape: name/description/params plus an
// exec closure that pulls validated args and dispatches into fns.
type ruleLifecycleSkill struct {
	name, desc string
	params     map[string]any
	exec       func(rc *RunContext, args map[string]json.RawMessage) (string, error)
}

func (s ruleLifecycleSkill) Name() string           { return s.name }
func (s ruleLifecycleSkill) Description() string    { return s.desc }
func (s ruleLifecycleSkill) Params() map[string]any { return s.params }
func (s ruleLifecycleSkill) Access() AccessClass    { return GatedWrite }

func (s ruleLifecycleSkill) Execute(_ context.Context, rc *RunContext, raw json.RawMessage) (string, error) {
	args := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &args)
	out, err := s.exec(rc, args)
	if err != nil {
		return "error: " + err.Error(), nil
	}
	return out, nil
}

// argStr decodes a string argument, "" when absent/invalid.
func argStr(args map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(args[key], &s)
	return strings.TrimSpace(s)
}

// argMap decodes an object argument, nil when absent/invalid.
func argMap(args map[string]json.RawMessage, key string) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(args[key], &m)
	return m
}

// actor resolves decided-by attribution: the conversation owner, never "".
func actor(rc *RunContext) string {
	if rc != nil && rc.User != "" {
		return rc.User
	}
	return "sentry"
}

func ruleIDParam(extra map[string]any) map[string]any {
	props := map[string]any{
		"rule_id": map[string]any{"type": "string", "description": "installed or catalog rule id (e.g. rp001)"},
	}
	for k, v := range extra {
		props[k] = v
	}
	return map[string]any{"type": "object", "properties": props, "required": []string{"rule_id"}}
}

const gatedNote = " Requires in-chat human approval before executing."

// NewRuleLifecycleSkills returns the batch-2 gated write tools, wired to
// fns. The slice order is the registration order.
func NewRuleLifecycleSkills(fns RuleLifecycleFuncs) []Skill {
	reasonProp := map[string]any{"type": "string", "description": "short justification recorded on the rule's revision history"}
	overridesProp := map[string]any{"type": "object", "description": "typed parameter overrides keyed by parameter key (see get_rule_catalog {id} for the schema)"}

	return []Skill{
		ruleLifecycleSkill{
			name: "install_rule",
			desc: "Install a rule from the catalog (get_rule_catalog lists what's installable), optionally with typed parameter overrides; deploys its detection materialized view." + gatedNote,
			params: ruleIDParam(map[string]any{
				"overrides": overridesProp, "reason": reasonProp,
			}),
			exec: func(rc *RunContext, args map[string]json.RawMessage) (string, error) {
				id := argStr(args, "rule_id")
				if id == "" {
					return "error: rule_id is required", nil
				}
				return fns.Install(id, argMap(args, "overrides"), actor(rc), orDefault(argStr(args, "reason"), "install via sentry"))
			},
		},
		ruleLifecycleSkill{
			name: "configure_rule",
			desc: "Replace an installed rule's FULL parameter override map (omitted keys reset to definition defaults) and redeploy its materialized view." + gatedNote,
			params: ruleIDParam(map[string]any{
				"overrides": overridesProp, "reason": reasonProp,
			}),
			exec: func(rc *RunContext, args map[string]json.RawMessage) (string, error) {
				id := argStr(args, "rule_id")
				if id == "" {
					return "error: rule_id is required", nil
				}
				return fns.Configure(id, argMap(args, "overrides"), actor(rc), orDefault(argStr(args, "reason"), "configure via sentry"))
			},
		},
		ruleLifecycleSkill{
			name: "set_rule_enabled",
			desc: "Enable or disable an installed rule (idempotent — states the outcome if already in the desired state). Disabling drops the rule's materialized view and abandons its pending holds." + gatedNote,
			params: ruleIDParam(map[string]any{
				"enabled": map[string]any{"type": "boolean", "description": "desired state"},
			}),
			exec: func(rc *RunContext, args map[string]json.RawMessage) (string, error) {
				id := argStr(args, "rule_id")
				if id == "" {
					return "error: rule_id is required", nil
				}
				var enabled bool
				if err := json.Unmarshal(args["enabled"], &enabled); err != nil {
					return "error: enabled (boolean) is required", nil
				}
				return fns.SetEnabled(id, enabled, actor(rc))
			},
		},
		ruleLifecycleSkill{
			name: "set_rule_policy",
			desc: "Set an installed rule's block policy: log_only, auto_block, or hold. Switching to log_only abandons the rule's pending holds." + gatedNote,
			params: ruleIDParam(map[string]any{
				"block_policy": map[string]any{"type": "string", "enum": []string{"log_only", "auto_block", "hold"}},
				"reason":       reasonProp,
			}),
			exec: func(rc *RunContext, args map[string]json.RawMessage) (string, error) {
				id := argStr(args, "rule_id")
				policy := argStr(args, "block_policy")
				if id == "" || policy == "" {
					return "error: rule_id and block_policy are required", nil
				}
				switch policy {
				case "log_only", "auto_block", "hold":
				default:
					return "error: block_policy must be log_only, auto_block, or hold", nil
				}
				return fns.SetPolicy(id, policy, actor(rc), orDefault(argStr(args, "reason"), "set policy via sentry"))
			},
		},
		ruleLifecycleSkill{
			name: "upgrade_rule",
			desc: "Upgrade an installed rule to the current catalog definition, preserving compatible parameter overrides (fails cleanly on conflicts)." + gatedNote,
			params: ruleIDParam(nil),
			exec: func(rc *RunContext, args map[string]json.RawMessage) (string, error) {
				id := argStr(args, "rule_id")
				if id == "" {
					return "error: rule_id is required", nil
				}
				return fns.Upgrade(id, actor(rc))
			},
		},
		ruleLifecycleSkill{
			name: "uninstall_rule",
			desc: "Uninstall a rule: soft-deletes the instance, drops its materialized view, and abandons its pending holds. The catalog definition remains installable." + gatedNote,
			params: ruleIDParam(nil),
			exec: func(rc *RunContext, args map[string]json.RawMessage) (string, error) {
				id := argStr(args, "rule_id")
				if id == "" {
					return "error: rule_id is required", nil
				}
				return fns.Uninstall(id, actor(rc))
			},
		},
		ruleLifecycleSkill{
			name: "sync_rules",
			desc: "Reconcile detection materialized views with the installed rule snapshots (recreate missing MVs, drop disabled ones). Use after MV health issues (get_rule_status / system.v_failed_mat_views)." + gatedNote,
			params: map[string]any{"type": "object", "properties": map[string]any{}},
			exec: func(rc *RunContext, _ map[string]json.RawMessage) (string, error) {
				return fns.Sync(actor(rc))
			},
		},
	}
}

// orDefault returns s, or def when s is empty.
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
