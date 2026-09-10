package tpapp

import (
	"strings"
	"testing"
)

func TestEnsureIfNotExists(t *testing.T) {
	cases := map[string]string{
		"\nCREATE STREAM otel_traces\n(\n a string\n)":                             "\nCREATE STREAM IF NOT EXISTS otel_traces\n(\n a string\n)",
		"CREATE MUTABLE STREAM IF NOT EXISTS agentguard_threats (x string)":        "CREATE MUTABLE STREAM IF NOT EXISTS agentguard_threats (x string)",
		"CREATE MUTABLE STREAM agentguard_pricing (x string)":                      "CREATE MUTABLE STREAM IF NOT EXISTS agentguard_pricing (x string)",
		"CREATE MATERIALIZED VIEW mv_threats\nINTO agentguard_threats AS SELECT 1": "CREATE MATERIALIZED VIEW IF NOT EXISTS mv_threats\nINTO agentguard_threats AS SELECT 1",
		"CREATE INPUT agentguard_otel_input\nSETTINGS type='otel'":                 "CREATE INPUT IF NOT EXISTS agentguard_otel_input\nSETTINGS type='otel'",
	}
	for in, want := range cases {
		got, err := EnsureIfNotExists(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Errorf("EnsureIfNotExists(%q)\n got %q\nwant %q", in, got, want)
		}
	}
	if _, err := EnsureIfNotExists("CREATE FUNCTION f() RETURNS bool"); err == nil {
		t.Error("expected error for unsupported CREATE head")
	}
}

func TestQualify(t *testing.T) {
	names := []string{"agentguard_cim_event", "agentguard_cim_metrics", "otel_traces"}
	in := "INSERT INTO agentguard_cim_metrics SELECT * FROM agentguard_cim_event AS e JOIN otel_traces t\n" +
		"WHERE t.x = 'otel_traces' AND e.agentguard_cim_events = 1 -- agentguard_cim_event\n" +
		"FROM {{ .DB }}.agentguard_cim_event"
	got := Qualify(in, names)
	want := "INSERT INTO {{ .DB }}.agentguard_cim_metrics SELECT * FROM {{ .DB }}.agentguard_cim_event AS e JOIN {{ .DB }}.otel_traces t\n" +
		"WHERE t.x = '{{ .DB }}.otel_traces' AND e.agentguard_cim_events = 1 -- {{ .DB }}.agentguard_cim_event\n" +
		"FROM {{ .DB }}.agentguard_cim_event"
	if got != want {
		t.Errorf("Qualify\n got %q\nwant %q", got, want)
	}
	if again := Qualify(got, names); again != got {
		t.Errorf("Qualify must be idempotent\n got %q\nwant %q", again, got)
	}
}

func TestTemplateOTelInput(t *testing.T) {
	in := "CREATE INPUT agentguard_otel_input\nSETTINGS\n  type = 'otel',\n  tcp_port                        = 4318,\n  listen_host                     = '0.0.0.0',\n  traces_target_stream = 'otel_traces'"
	got, err := TemplateOTelInput(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tcp_port                        = {{ .Config.otel_port }},", "listen_host                     = '{{ .Config.otel_listen_host }}',"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "4318") || strings.Contains(got, "0.0.0.0") {
		t.Errorf("literals not replaced:\n%s", got)
	}
	if _, err := TemplateOTelInput("CREATE INPUT x SETTINGS type='otel'"); err == nil {
		t.Error("expected error when tcp_port/listen_host are absent")
	}
}

func TestStripTrailingSemicolon(t *testing.T) {
	if got := StripTrailingSemicolon("SELECT 1;\n  \n"); got != "SELECT 1" {
		t.Errorf("got %q", got)
	}
	if got := StripTrailingSemicolon("  SELECT 1 "); got != "SELECT 1" {
		t.Errorf("got %q", got)
	}
}

func TestTransform(t *testing.T) {
	got, err := Transform("\nCREATE MATERIALIZED VIEW mv_threats\nINTO agentguard_threats\nAS SELECT 1 FROM agentguard_security_events;\n",
		[]string{"mv_threats", "agentguard_threats", "agentguard_security_events"})
	if err != nil {
		t.Fatal(err)
	}
	want := "CREATE MATERIALIZED VIEW IF NOT EXISTS {{ .DB }}.mv_threats\nINTO {{ .DB }}.agentguard_threats\nAS SELECT 1 FROM {{ .DB }}.agentguard_security_events"
	if got != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
}
