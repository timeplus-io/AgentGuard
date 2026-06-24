//go:build integration

package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// End-to-end coverage for the editable-rule-parameters authoring paths (#33)
// against a real timeplusd. The fake-querier unit tests prove the orchestration
// and the renderer output as strings; these tests prove the rendered SQL — for a
// custom rule, an EDITED custom rule, and the new regex/duration parameter types —
// actually compiles into a live materialized view and emits the right detections.
//
// Requires (same as the other integration tests): timeplusd on
// TIMEPLUS_HOST:TIMEPLUS_PORT and a provisioned AgentGuard server on
// AGENTGUARD_URL. Run with: go test -tags integration ./internal/handlers/...
//
// FILE NAMING — "integration_zzz_..." sorts AFTER integration_zz_rule155_test.go
// so these MV-installing, cim_event-writing tests never run before the rp-018
// session-close test (see the ordering note in integration_zz_rule155_test.go).

// detectionTemplate builds a single-SELECT detection rule body whose projection
// matches agentguard_security_events, parameterised only by its rule_id and a
// WHERE condition (which carries the {{ .Config.* }} placeholder under test).
// Mirrors the projection of shipped seed rules (e.g. rp-082), so MV creation
// exercises the same column contract.
func detectionTemplate(ruleID, whereCond string) string {
	return fmt.Sprintf(`SELECT
  event_time AS detected_at,
  '%s' AS rule_id,
  '%s' AS rule_name,
  'warning' AS severity,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  raw_hook_name AS hook_name,
  tool_name,
  'integration detection' AS message,
  raw_event_data AS event_data
FROM agentguard_cim_event
WHERE %s`, ruleID, ruleID, whereCond)
}

// createCustomRule POSTs a custom Definition to the catalog (expects 201). It does
// not install — installation is a separate lifecycle action.
func (r *ruleE2E) createCustomRule(def map[string]any) {
	r.t.Helper()
	raw, err := json.Marshal(def)
	if err != nil {
		r.t.Fatalf("marshal definition: %v", err)
	}
	resp := doPost(r.t, r.client, "/api/rules/catalog", string(raw))
	body := readBody(r.t, resp)
	if resp.StatusCode != http.StatusCreated {
		r.t.Fatalf("create custom rule %v: %d %s", def["id"], resp.StatusCode, body)
	}
}

// installCustomRule installs an already-created custom rule (optionally with an
// overrides body, e.g. {"overrides":{"marker":"x"}}; "" installs with defaults),
// registers cleanup, and verifies the MV exists and is healthy. Mirrors install().
func (r *ruleE2E) installCustomRule(ruleID, overridesBody string) {
	r.t.Helper()
	mv := "mv_rule_" + strings.ReplaceAll(ruleID, "-", "")
	r.tpExec("DROP VIEW IF EXISTS " + mv)

	resp := doPost(r.t, r.client, "/api/rules/"+ruleID+"/install", overridesBody)
	body := readBody(r.t, resp)
	if resp.StatusCode != http.StatusOK {
		r.t.Fatalf("install custom %s: %d %s", ruleID, resp.StatusCode, body)
	}
	r.t.Cleanup(func() { doReq(r.t, r.client, "DELETE", "/api/rules/"+ruleID, "") })

	time.Sleep(2 * time.Second)
	if r.tpQuery(fmt.Sprintf(`SELECT count() FROM system.tables WHERE name = '%s' FORMAT TSV`, mv)) == "0" {
		r.t.Fatalf("%s not found in system.tables after install — MV creation failed", mv)
	}
	if r.tpQuery(fmt.Sprintf(`SELECT name FROM system.v_failed_mat_views WHERE name = '%s' FORMAT TSV`, mv)) != "" {
		r.t.Fatalf("%s is in a failed state", mv)
	}
	r.t.Logf("%s installed and healthy", mv)
}

// editDefinition PUTs an updated custom Definition (the edit-rule path) and waits
// for the atomic MV redeploy to settle before returning.
func (r *ruleE2E) editDefinition(ruleID string, def map[string]any) {
	r.t.Helper()
	raw, err := json.Marshal(def)
	if err != nil {
		r.t.Fatalf("marshal definition: %v", err)
	}
	resp := doReq(r.t, r.client, "PUT", "/api/rules/catalog/"+ruleID, string(raw))
	body := readBody(r.t, resp)
	if resp.StatusCode != http.StatusOK {
		r.t.Fatalf("edit definition %s: %d %s", ruleID, resp.StatusCode, body)
	}
	time.Sleep(2 * time.Second)
}

// TestIntegration_EditCustomRuleRedeploysMV proves the edit-rule lifecycle end to
// end: a custom parameterised rule is created + installed, its definition is then
// edited (the stored override is preserved while the template switches which CIM
// field it scans), and the live MV is redeployed so detection follows the NEW
// rendered SQL — the old behaviour is gone, the new behaviour is live.
func TestIntegration_EditCustomRuleRedeploysMV(t *testing.T) {
	r := newRuleE2E(t)
	pfx := randPfx(t)
	id := "rp-itest-edit-" + pfx
	marker := "zzmark" + pfx // unique token so cross-run data cannot interfere

	// v1 scans tool_result for the marker.
	v1 := map[string]any{
		"id": id, "name": "Integration Edit Rule", "version": "1.0.0",
		"description": "edit-rule live redeploy test (v1 scans tool_result)",
		"severity":    "warning", "category": "test", "hook_sources": "tool_complete",
		"sql_template": detectionTemplate(id,
			"event_type = 'tool_complete' AND position(coalesce(tool_result, ''), {{ .Config.marker }}) > 0"),
		"config": []map[string]any{{
			"key": "marker", "label": "Marker", "type": "string", "default": "zzdefault",
		}},
	}
	r.createCustomRule(v1)
	r.installCustomRule(id, fmt.Sprintf(`{"overrides":{"marker":"%s"}}`, marker))

	cols := `(event_time, event_type, agent_id, deployment_id, session_id, run_id, tool_name, tool_result, user_message)`
	ts := time.Now().UTC().Format("2006-01-02 15:04:05.000")
	p1 := "edit-v1pos-" + pfx
	n1 := "edit-v1neg-" + pfx
	// v1 POS: marker in tool_result -> fires.
	r.tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s','tool_complete','agent-e2e','dep-e2e','%s','r1','cat','contains %s here','benign')`, cols, ts, p1, marker))
	// v1 NEG: marker only in user_message -> v1 scans tool_result -> does not fire.
	r.tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s','tool_complete','agent-e2e','dep-e2e','%s','r1','cat','benign','contains %s here')`, cols, ts, n1, marker))

	c1 := r.detectionCounts(id, []string{p1}, []string{p1, n1})
	if c1[p1] != 1 {
		t.Errorf("v1 %s: got %d, want 1 (marker in tool_result)", p1, c1[p1])
	}
	if c1[n1] != 0 {
		t.Errorf("v1 %s: got %d, want 0 (v1 must not scan user_message)", n1, c1[n1])
	}

	// EDIT to v2: same marker param (override 'zzmark<pfx>' must be preserved), but
	// the template now scans user_message instead of tool_result.
	v2 := map[string]any{
		"id": id, "name": "Integration Edit Rule", "version": "1.1.0",
		"description": "edit-rule live redeploy test (v2 scans user_message)",
		"severity":    "warning", "category": "test", "hook_sources": "tool_complete",
		"sql_template": detectionTemplate(id,
			"event_type = 'tool_complete' AND position(coalesce(user_message, ''), {{ .Config.marker }}) > 0"),
		"config": []map[string]any{{
			"key": "marker", "label": "Marker", "type": "string", "default": "zzdefault",
		}},
	}
	r.editDefinition(id, v2)

	ts2 := time.Now().UTC().Format("2006-01-02 15:04:05.000")
	p2 := "edit-v2pos-" + pfx
	n2 := "edit-v2neg-" + pfx
	// v2 POS: marker in user_message -> fires (proves the redeployed MV runs new SQL).
	r.tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s','tool_complete','agent-e2e','dep-e2e','%s','r1','cat','benign','contains %s here')`, cols, ts2, p2, marker))
	// v2 NEG: marker in tool_result -> v2 no longer scans it -> does not fire (old SQL gone).
	r.tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s','tool_complete','agent-e2e','dep-e2e','%s','r1','cat','contains %s here','benign')`, cols, ts2, n2, marker))

	c2 := r.detectionCounts(id, []string{p2}, []string{p2, n2})
	if c2[p2] != 1 {
		t.Errorf("v2 %s: got %d, want 1 (edit redeployed the MV to scan user_message)", p2, c2[p2])
	}
	if c2[n2] != 0 {
		t.Errorf("v2 %s: got %d, want 0 (old tool_result scan must be gone after edit)", n2, c2[n2])
	}
}

// TestIntegration_RegexParamRuleDetects proves a regex-typed parameter renders
// into a live MV: an operator-supplied RE2 pattern (an AWS access-key id shape)
// compiled via match() fires on a matching tool_result and is silent otherwise.
func TestIntegration_RegexParamRuleDetects(t *testing.T) {
	r := newRuleE2E(t)
	pfx := randPfx(t)
	id := "rp-itest-re-" + pfx

	def := map[string]any{
		"id": id, "name": "Integration Regex Rule", "version": "1.0.0",
		"description": "regex param live-detection test", "severity": "warning",
		"category": "test", "hook_sources": "tool_complete",
		"sql_template": detectionTemplate(id,
			"event_type = 'tool_complete' AND match(coalesce(tool_result, ''), {{ .Config.pattern }})"),
		"config": []map[string]any{{
			"key": "pattern", "label": "Pattern", "type": "regex",
			"default": "AKIA[0-9A-Z]{16}", // AWS access key id shape
		}},
	}
	r.createCustomRule(def)
	r.installCustomRule(id, "")

	cols := `(event_time, event_type, agent_id, deployment_id, session_id, run_id, tool_name, tool_result)`
	ts := time.Now().UTC().Format("2006-01-02 15:04:05.000")
	pos := "re-pos-" + pfx
	neg := "re-neg-" + pfx
	// POS: AKIA + 16 [0-9A-Z] -> matches the RE2 pattern.
	r.tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s','tool_complete','agent-e2e','dep-e2e','%s','r1','cat','leaked AKIAIOSFODNN7EXAMPLE in build log')`, cols, ts, pos))
	// NEG: no AWS-key-shaped token.
	r.tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s','tool_complete','agent-e2e','dep-e2e','%s','r1','cat','nothing sensitive in this output')`, cols, ts, neg))

	counts := r.detectionCounts(id, []string{pos}, []string{pos, neg})
	if counts[pos] != 1 {
		t.Errorf("%s: got %d, want 1 (regex match on AWS key id)", pos, counts[pos])
	}
	if counts[neg] != 0 {
		t.Errorf("%s: got %d, want 0 (no regex match)", neg, counts[neg])
	}
}

// TestIntegration_DurationParamRuleDetects proves a duration-typed parameter
// renders as a bare integer into a live MV: a per-row tool_duration_ms threshold
// (non-streaming, no MAXSPAN window) fires above the rendered bound and is silent
// below it.
func TestIntegration_DurationParamRuleDetects(t *testing.T) {
	r := newRuleE2E(t)
	pfx := randPfx(t)
	id := "rp-itest-dur-" + pfx

	def := map[string]any{
		"id": id, "name": "Integration Duration Rule", "version": "1.0.0",
		"description": "duration param live-detection test", "severity": "warning",
		"category": "test", "hook_sources": "tool_complete",
		"sql_template": detectionTemplate(id,
			"event_type = 'tool_complete' AND tool_duration_ms > {{ .Config.max_tool_ms }}"),
		"config": []map[string]any{{
			"key": "max_tool_ms", "label": "Max tool ms", "type": "duration",
			"unit": "millisecond", "default": 5000,
		}},
	}
	r.createCustomRule(def)
	r.installCustomRule(id, "")

	cols := `(event_time, event_type, agent_id, deployment_id, session_id, run_id, tool_name, tool_duration_ms)`
	ts := time.Now().UTC().Format("2006-01-02 15:04:05.000")
	pos := "dur-pos-" + pfx
	neg := "dur-neg-" + pfx
	// POS: 9000ms > 5000ms default -> fires.
	r.tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s','tool_complete','agent-e2e','dep-e2e','%s','r1','build',9000)`, cols, ts, pos))
	// NEG: 1000ms -> below the rendered threshold.
	r.tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s','tool_complete','agent-e2e','dep-e2e','%s','r1','build',1000)`, cols, ts, neg))

	counts := r.detectionCounts(id, []string{pos}, []string{pos, neg})
	if counts[pos] != 1 {
		t.Errorf("%s: got %d, want 1 (tool_duration_ms 9000 > 5000)", pos, counts[pos])
	}
	if counts[neg] != 0 {
		t.Errorf("%s: got %d, want 0 (tool_duration_ms 1000 <= 5000)", neg, counts[neg])
	}
}

// TestIntegration_RuleRP128Detection installs the shipped seed rule rp-128 (which
// uses BOTH a duration and a regex parameter) and verifies its conjunction
// semantics live: a tool_complete that is slow AND whose output matches the
// default signature fires; one that is slow-but-clean, or matching-but-fast, does
// not. This proves the regex/duration extensions work in a real shipped rule, not
// only in machinery/fixtures.
func TestIntegration_RuleRP128Detection(t *testing.T) {
	r := newRuleE2E(t)
	r.install("rp-128") // defaults: min_duration_ms=10000, output_signature matches AKIA…

	pfx := randPfx(t)
	hit := "rp128-hit-" + pfx        // slow + matching -> fires
	slowClean := "rp128-slow-" + pfx // slow + no match -> suppressed
	fastHit := "rp128-fast-" + pfx   // matching + fast -> suppressed
	ts := time.Now().UTC().Format("2006-01-02 15:04:05.000")
	cols := `(event_time, event_type, agent_id, deployment_id, session_id, run_id, tool_name, tool_result, tool_duration_ms)`

	// HIT: 15000ms (> 10000) AND output contains an AWS key id (matches default regex).
	r.tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s','tool_complete','agent-e2e','dep-e2e','%s','r1','cat','dumped AKIAIOSFODNN7EXAMPLE from env',15000)`, cols, ts, hit))
	// SLOW-CLEAN: slow but no signature match -> suppressed by the regex branch.
	r.tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s','tool_complete','agent-e2e','dep-e2e','%s','r1','find','large benign listing with no secrets',15000)`, cols, ts, slowClean))
	// FAST-HIT: matches the signature but ran only 1000ms -> suppressed by the duration floor.
	r.tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s','tool_complete','agent-e2e','dep-e2e','%s','r1','cat','dumped AKIAIOSFODNN7EXAMPLE from env',1000)`, cols, ts, fastHit))

	counts := r.detectionCounts("rp-128", []string{hit}, []string{hit, slowClean, fastHit})
	if counts[hit] != 1 {
		t.Errorf("%s: got %d, want 1 (slow AND matching signature)", hit, counts[hit])
	}
	if counts[slowClean] != 0 {
		t.Errorf("%s: got %d, want 0 (slow but no signature match)", slowClean, counts[slowClean])
	}
	if counts[fastHit] != 0 {
		t.Errorf("%s: got %d, want 0 (matches signature but below the duration floor)", fastHit, counts[fastHit])
	}
}
