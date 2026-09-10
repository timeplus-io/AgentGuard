//go:build integration

package tpapp_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/timeplus"
	"github.com/timeplus/agentguard/internal/tpapp"
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// TestIntegration_TpappInstall does what the Timeplus apps installer does,
// minus the zip: render every DDL file with a scratch database name and a
// free OTLP port, execute them in manifest order over HTTP, and verify the
// objects exist and the rule MVs are paused. Run with:
//
//	CGO_ENABLED=0 go test -tags integration ./internal/tpapp/ -run TestIntegration_TpappInstall -v
func TestIntegration_TpappInstall(t *testing.T) {
	port, _ := strconv.Atoi(envOr("TIMEPLUS_PORT", "3218"))
	cfg := &config.TimeplusConfig{
		Host: envOr("TIMEPLUS_HOST", "localhost"), Port: port,
		User: envOr("TIMEPLUS_USER", "proton"), Password: envOr("TIMEPLUS_PASSWORD", "timeplus@t+"),
	}
	db := fmt.Sprintf("ag_test_%d", time.Now().Unix())
	otelPort := envOr("TPAPP_TEST_OTEL_PORT", "14318")

	app, err := tpapp.Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := timeplus.ExecHTTP(cfg, "CREATE DATABASE "+db); err != nil {
		t.Fatalf("create scratch db: %v", err)
	}
	t.Cleanup(func() {
		// Drop the input first: it holds the OTLP port and is not dropped by
		// the database CASCADE.
		if err := timeplus.ExecHTTP(cfg, "DROP INPUT IF EXISTS "+db+".agentguard_otel_input"); err != nil {
			t.Logf("cleanup: drop input: %v", err)
		}
		if err := timeplus.ExecHTTP(cfg, "DROP DATABASE IF EXISTS "+db+" CASCADE"); err != nil {
			t.Errorf("cleanup: drop database %s: %v", db, err)
		}
	})

	render := func(sql string) string {
		sql = strings.ReplaceAll(sql, "{{ .DB }}", db)
		sql = strings.ReplaceAll(sql, "{{ .Config.otel_port }}", otelPort)
		sql = strings.ReplaceAll(sql, "{{ .Config.otel_listen_host }}", "0.0.0.0")
		return sql
	}
	for _, r := range app.Resources {
		if err := timeplus.ExecHTTP(cfg, render(r.SQL)); err != nil {
			t.Fatalf("provision %s (%s): %v", r.Name, r.File, err)
		}
	}

	// Every stream/mv exists in the scratch database.
	rows, err := timeplus.Query(cfg, fmt.Sprintf("SELECT name FROM system.tables WHERE database = '%s'", db))
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, row := range rows {
		have[fmt.Sprint(row["name"])] = true
	}
	for _, r := range app.Resources {
		switch r.Kind {
		case "stream", "mutable_stream", "materialized_view":
			if !have[r.Name] {
				t.Errorf("%s (%s) missing from system.tables", r.Name, r.Kind)
			}
		}
	}
	// The input exists.
	if rows, err := timeplus.Query(cfg, fmt.Sprintf("SELECT name FROM system.inputs WHERE database = '%s' AND name = 'agentguard_otel_input'", db)); err != nil || len(rows) == 0 {
		t.Errorf("agentguard_otel_input missing from system.inputs (err=%v)", err)
	}
	// Rule MVs are paused. system.local_system_states exposes one row per MV
	// with state_name='status' and state_string_value 'Paused'/'Executing';
	// the pause takes a moment to be reflected, so poll briefly.
	for _, id := range tpapp.CoreRuleIDs {
		mv := timeplus.MVName(id)
		if got := waitForMVStatus(t, cfg, db, mv, "Paused", 15*time.Second); got != "Paused" {
			t.Errorf("%s status = %q, want Paused", mv, got)
		}
	}
	// Re-running every file must be a no-op (upgrade path).
	for _, r := range app.Resources {
		if err := timeplus.ExecHTTP(cfg, render(r.SQL)); err != nil {
			t.Errorf("re-run %s: %v", r.Name, err)
		}
	}
}

// waitForMVStatus polls system.local_system_states until the MV reports want
// or the deadline passes, returning the last observed status.
func waitForMVStatus(t *testing.T, cfg *config.TimeplusConfig, db, mv, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for {
		rows, err := timeplus.Query(cfg, fmt.Sprintf(
			"SELECT state_string_value FROM system.local_system_states WHERE database = '%s' AND name = '%s' AND state_name = 'status'", db, mv))
		if err == nil && len(rows) > 0 {
			last = fmt.Sprint(rows[0]["state_string_value"])
			if last == want {
				return last
			}
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(500 * time.Millisecond)
	}
}
