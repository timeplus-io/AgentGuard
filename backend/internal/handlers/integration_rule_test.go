//go:build integration

package handlers_test

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestIntegration_RuleRP018Detection installs rp-018 as a materialized view,
// inserts fixture data into the CIM streams, and verifies that the MV emits
// the correct detections into agentguard_security_events.
//
// Scenarios exercised:
//   - s4-true:       token burn + same-tool concentration + content signal → 1 detection
//   - s1-token-only: high tokens without content or same-tool → 0 detections
//
// Requires: timeplusd running on TIMEPLUS_HOST:TIMEPLUS_PORT,
// AgentGuard server running on AGENTGUARD_URL, already provisioned.
func TestIntegration_RuleRP018Detection(t *testing.T) {
	client := newClient(t)
	waitForServer(t, client, 30*time.Second)

	tpHost := envOr("TIMEPLUS_HOST", "localhost")
	tpPort := envOr("TIMEPLUS_PORT", "3218")
	tpUser := envOr("TIMEPLUS_USER", "default")
	tpPass := envOr("TIMEPLUS_PASSWORD", "")
	tpBase := fmt.Sprintf("http://%s:%s", tpHost, tpPort)

	tpExec := func(sql string) {
		t.Helper()
		req, err := http.NewRequest("POST", tpBase, strings.NewReader(sql))
		if err != nil {
			t.Fatalf("tpExec: %v", err)
		}
		req.SetBasicAuth(tpUser, tpPass)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("tpExec: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("tpExec %d: %s\nSQL: %s", resp.StatusCode, body, sql)
		}
	}

	tpQuery := func(sql string) string {
		t.Helper()
		req, err := http.NewRequest("POST", tpBase, strings.NewReader(sql))
		if err != nil {
			t.Fatalf("tpQuery: %v", err)
		}
		req.SetBasicAuth(tpUser, tpPass)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("tpQuery: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("tpQuery %d: %s\nSQL: %s", resp.StatusCode, body, sql)
		}
		return strings.TrimSpace(string(body))
	}

	// ── Provision (idempotent) ────────────────────────────────────────────
	provisionBody := fmt.Sprintf(`{
		"host": "%s", "port": %s, "native_port": %s,
		"username": "%s", "password": "%s"
	}`, tpHost, tpPort,
		envOr("TIMEPLUS_NATIVE_PORT", "8463"),
		tpUser, tpPass,
	)
	resp := doPost(t, client, "/api/setup/provision", provisionBody)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		t.Fatalf("provision: %d %s", resp.StatusCode, body)
	}

	// Login
	loginBody := fmt.Sprintf(`{"username":"%s","password":"%s"}`, tpUser, tpPass)
	resp = doPost(t, client, "/api/auth/login", loginBody)
	assertStatus(t, "login", resp, http.StatusOK)

	// ── Install rp-018 ───────────────────────────────────────────────────
	resp = doPost(t, client, "/api/rules/rp-018/install", "")
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		if strings.Contains(body, "already installed") {
			t.Log("rp-018 already installed, continuing")
		} else {
			t.Fatalf("install rp-018: %d %s", resp.StatusCode, body)
		}
	}
	t.Cleanup(func() {
		doReq(t, client, "DELETE", "/api/rules/rp-018", "")
	})

	// Verify the MV actually exists and is not in a failed state.
	time.Sleep(2 * time.Second)
	mvCheck := tpQuery(`SELECT count() FROM system.tables WHERE name = 'mv_rule_rp018' FORMAT TSV`)
	if strings.TrimSpace(mvCheck) == "0" {
		t.Fatalf("mv_rule_rp018 not found in system.tables after install — UDAF or MV creation failed")
	}

	failedMVs := tpQuery(`SELECT name FROM system.v_failed_mat_views WHERE name = 'mv_rule_rp018' FORMAT TSV`)
	if strings.TrimSpace(failedMVs) != "" {
		t.Fatalf("mv_rule_rp018 is in failed state")
	}
	t.Log("mv_rule_rp018 installed and healthy")

	// ── Insert fixture data ──────────────────────────────────────────────
	// Use a unique run prefix so reruns don't see stale detections.
	var rnd [4]byte
	rand.Read(rnd[:])
	pfx := hex.EncodeToString(rnd[:])

	s4 := fmt.Sprintf("s4-%s", pfx)
	s1 := fmt.Sprintf("s1-%s", pfx)

	// Use timestamps anchored to now() so they stay within the watermark.
	now := time.Now().UTC()
	ts := func(offset time.Duration) string {
		return now.Add(offset).Format("2006-01-02 15:04:05.000")
	}

	cimEventCols := `(event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)`

	// ── s4: token burn + same-tool concentration + content signal ────────
	// Expected: 1 detection
	tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s', 'tool_complete', 'openclaw', 'agent-e2e', 'dep-e2e', '%s', 'r4', '', '', 'search_docs', '', 'PROGRESS:47%% - retry with broader search terms to retrieve all remaining records', true, 0, '', '', '', '', 'after_tool_call', '{}')`,
		cimEventCols, ts(0), s4))

	tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags) VALUES ('%s', 'llm.input_tokens', 30000.0, 'openclaw', 'agent-e2e', 'dep-e2e', '%s', {'run_id': 'r4', 'tool_name': ''})`,
		ts(20*time.Second), s4))

	tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags) VALUES ('%s', 'llm.output_tokens', 30000.0, 'openclaw', 'agent-e2e', 'dep-e2e', '%s', {'run_id': 'r4', 'tool_name': ''})`,
		ts(20*time.Second), s4))

	// 18 tool.call metrics for same-tool concentration (> 15 threshold)
	tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags) SELECT to_datetime64('%s', 3, 'UTC'), 'tool.call', to_float64(1), 'openclaw', 'agent-e2e', 'dep-e2e', '%s', map_cast(['run_id', 'tool_name'], ['r4', 'search_docs']) FROM numbers(18)`,
		ts(30*time.Second), s4))

	// Wait until s4's fixtures are durably ingested before inserting the
	// session_end that closes the EMIT AFTER SESSION CLOSE window. On a cold
	// engine the bulk tool.call metric insert lags; if those 18 metrics are not
	// yet in the stream when the session closes, the same-tool concentration
	// stays below threshold, the (empty) emission yields 0 detections, and the
	// late metrics cannot reopen the closed session — the source of this test's
	// CI flake.
	waitForRows := func(label, query string, want int) {
		t.Helper()
		deadline := time.Now().Add(45 * time.Second)
		var got int
		for time.Now().Before(deadline) {
			got = 0
			fmt.Sscanf(tpQuery(query), "%d", &got)
			if got >= want {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatalf("%s: only %d/%d rows durably ingested before deadline", label, got, want)
	}
	waitForRows("s4 metrics",
		fmt.Sprintf(`SELECT count() FROM table(agentguard_cim_metrics) WHERE session_id = '%s' FORMAT TSV`, s4), 20)
	waitForRows("s4 events",
		fmt.Sprintf(`SELECT count() FROM table(agentguard_cim_event) WHERE session_id = '%s' FORMAT TSV`, s4), 1)

	// session_end closes the window immediately
	tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s', 'session_end', 'openclaw', 'agent-e2e', 'dep-e2e', '%s', 'r4', '', '', '', '', '', true, 0, '', '', '', '', 'session_end', '{}')`,
		cimEventCols, ts(60*time.Second), s4))

	// ── s1: high tokens but no content signal or same-tool ───────────────
	// Expected: 0 detections
	tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s', 'tool_complete', 'openclaw', 'agent-e2e', 'dep-e2e', '%s', 'r1', '', '', 'search_docs', '', 'normal result', true, 0, '', '', '', '', 'after_tool_call', '{}')`,
		cimEventCols, ts(5*time.Minute), s1))

	tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags) VALUES ('%s', 'llm.input_tokens', 25000.0, 'openclaw', 'agent-e2e', 'dep-e2e', '%s', {'run_id': 'r1', 'tool_name': ''})`,
		ts(5*time.Minute+20*time.Second), s1))

	tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_metrics (event_time, metric_name, metric_value, agent_type, agent_id, deployment_id, session_id, tags) VALUES ('%s', 'llm.output_tokens', 25000.0, 'openclaw', 'agent-e2e', 'dep-e2e', '%s', {'run_id': 'r1', 'tool_name': ''})`,
		ts(5*time.Minute+20*time.Second), s1))

	tpExec(fmt.Sprintf(`INSERT INTO agentguard_cim_event %s VALUES ('%s', 'session_end', 'openclaw', 'agent-e2e', 'dep-e2e', '%s', 'r1', '', '', '', '', '', true, 0, '', '', '', '', 'session_end', '{}')`,
		cimEventCols, ts(6*time.Minute), s1))

	// Wait until the full fixture is durably ingested before polling. s1's
	// later-timestamped rows are what advance the MV watermark past s4's
	// session boundary (closing it), so they must land before we expect a
	// detection. Totals: 4 events (s4/s1 tool_complete + session_end) and 22
	// metrics (s4: 2 token + 18 tool.call; s1: 2 token).
	waitForRows("all events",
		fmt.Sprintf(`SELECT count() FROM table(agentguard_cim_event) WHERE session_id IN ('%s', '%s') FORMAT TSV`, s4, s1), 4)
	waitForRows("all metrics",
		fmt.Sprintf(`SELECT count() FROM table(agentguard_cim_metrics) WHERE session_id IN ('%s', '%s') FORMAT TSV`, s4, s1), 22)
	t.Logf("fixture durably ingested: 4 cim_events, 22 cim_metrics")

	// ── Poll for detections ──────────────────────────────────────────────
	// The MV should emit after the session_end event closes the window.
	// Poll with a generous timeout to account for MV startup latency.
	var s4Count, s1Count int
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		result := tpQuery(fmt.Sprintf(
			`SELECT session_id, count() AS cnt `+
				`FROM table(agentguard_security_events) `+
				`WHERE rule_id = 'rp-018' AND session_id IN ('%s', '%s') `+
				`GROUP BY session_id FORMAT TSV`, s4, s1))

		s4Count = 0
		s1Count = 0
		for _, line := range strings.Split(result, "\n") {
			parts := strings.Split(line, "\t")
			if len(parts) != 2 {
				continue
			}
			var cnt int
			fmt.Sscanf(parts[1], "%d", &cnt)
			switch parts[0] {
			case s4:
				s4Count = cnt
			case s1:
				s1Count = cnt
			}
		}

		if s4Count >= 1 {
			break
		}
	}

	// If still 0 after timeout, dump MV diagnostics.
	if s4Count == 0 {
		lag := tpQuery(`SELECT name, lag, processed_sn, ckpt_sn FROM system.v_mat_view_lags WHERE name = 'mv_rule_rp018' FORMAT TSV`)
		failed := tpQuery(`SELECT name FROM system.v_failed_mat_views WHERE name = 'mv_rule_rp018' FORMAT TSV`)
		t.Logf("MV diagnostics — lag: [%s], failed: [%s]", lag, failed)
	}

	if s4Count != 1 {
		t.Errorf("%s: got %d detections, want 1", s4, s4Count)
	}
	if s1Count != 0 {
		t.Errorf("%s: got %d detections, want 0", s1, s1Count)
	}

	// Log detection message for debugging.
	if s4Count > 0 {
		msg := tpQuery(fmt.Sprintf(
			`SELECT message `+
				`FROM table(agentguard_security_events) `+
				`WHERE rule_id = 'rp-018' AND session_id = '%s' `+
				`LIMIT 1 FORMAT TSV`, s4))
		t.Logf("%s detection message: %s", s4, msg)
	}
}
