# AgentGuard `.tpapp` Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generate an installable Timeplus app (`apps/agentguard/`, `db_name: ag`) from the backend's own provisioning DDL, and let the AgentGuard UI attach to it via a new `timeplus.database` config key.

**Architecture:** An exported, ordered resource list in the `timeplus` package references the existing `ddl*` constants; a new `internal/tpapp` package transforms them (IF NOT EXISTS, `{{ .DB }}.` qualification, OTel config templating), compiles rules rp-001..rp-004 with defaults, and renders `manifest.yaml` + `ddl/*.sql`; `cmd/tpapp-export` writes the tree and a drift test keeps `apps/agentguard/` identical to the generator's output. The backend gains `timeplus.database`, applied as the connection default database so handler SQL stays unqualified.

**Tech Stack:** Go 1.2x (`text/template`, `go/parser`, `regexp`), existing `internal/catalog` + `internal/rules` packages, Timeplus Enterprise HTTP (8123/3218) + native driver, `zip`.

**Spec:** `docs/superpowers/specs/2026-09-10-tpapp-export-design.md`

## Global Constraints

- All Go commands run from `backend/` with `CGO_ENABLED=0` (macOS 26 dyld issue): `CGO_ENABLED=0 go test ./...`, `CGO_ENABLED=0 go build ./...`.
- App identity: `id: io.timeplus.agentguard`, `db_name: ag`, `package_format_version: 1`, initial `version: 0.1.0`, `author: Timeplus`, `categories: [security, observability]`.
- Every generated DDL file holds exactly ONE statement, no trailing `;`. Every `CREATE` (except UDFs) is `IF NOT EXISTS`; UDFs are `CREATE OR REPLACE FUNCTION` with NO `{{ .DB }}.` prefix.
- Every reference to an app resource is `{{ .DB }}.<name>` (DDL) / `[[ .DB ]].<name>` (dashboard JSON).
- Excluded forever from the app: `agentguard_notify_events`, `mv_notify_filter`, `agentguard_notifications`, `agentguard_roles`, `agentguard_user_roles`, `agentguard_holds`, all `agentguard_sentry_*`, all `agentguard_dlp_*`, `dlp_*` UDFs, rules beyond rp-004.
- Commit after every task; commit messages end with the session trailer:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01H9tkRxosPfxiiUCRwxmxy8
  ```
- Branch: `feat/tpapp-agentguard` (already created from `origin/main`).

---

## File map

| Path | Responsibility |
|---|---|
| `backend/internal/timeplus/app_resources.go` | `AppResource`, `AppResources()`, `AppExcludedDDL()` — the ordered pipeline list |
| `backend/internal/timeplus/app_resources_test.go` | uniqueness, DDL-head/name agreement, `go/parser` coverage of every `ddl*` constant |
| `backend/internal/tpapp/transform.go` | `Transform`, `Qualify`, `EnsureIfNotExists`, `TemplateOTelInput`, `StripTrailingSemicolon` |
| `backend/internal/tpapp/transform_test.go` | golden cases per transformation |
| `backend/internal/tpapp/rules.go` | `RuleArtifacts` — compile rp-001..004 into udf/mv/pause files |
| `backend/internal/tpapp/rules_test.go` | ordering, pause pairing, UDF-before-MV, SQL qualification |
| `backend/internal/tpapp/build.go` | `File`, `Resource`, `App`, `Build()`, manifest rendering, icon data URI |
| `backend/internal/tpapp/manifest.yaml.tmpl` | `text/template` for manifest |
| `backend/internal/tpapp/static/{dashboards/main.json,README.md,Makefile,icon.svg}` | verbatim files |
| `backend/internal/tpapp/build_test.go` | one statement per file, order, manifest parse, no unqualified names |
| `backend/internal/tpapp/write.go` | `Write(app, dir)` + `Diff(app, dir)` |
| `backend/internal/tpapp/export_uptodate_test.go` | drift test vs `apps/agentguard/` |
| `backend/cmd/tpapp-export/main.go` | CLI: `--out`, `--check` |
| `apps/agentguard/**` | generated output (committed) |
| `Makefile` | `tpapp`, `tpapp-check` targets |
| `backend/internal/config/{config.go,registry.go,sources.go}` | `timeplus.database` key |
| `backend/cmd/server/main.go` | `--timeplus-database` flag |
| `backend/internal/timeplus/{client.go,stream.go,init.go}` | apply database on connections |
| `backend/internal/timeplus/tpapp_integration_test.go` | `-tags integration` scratch-db install |
| `docs/tpapp.md`, `CLAUDE.md` | docs |

---

### Task 1: `timeplus.AppResources()` — the ordered resource list

**Files:**
- Create: `backend/internal/timeplus/app_resources.go`
- Test: `backend/internal/timeplus/app_resources_test.go`

**Interfaces:**
- Produces:
  ```go
  type AppResourceKind string
  const (
      AppKindStream        AppResourceKind = "stream"
      AppKindMutableStream AppResourceKind = "mutable_stream"
      AppKindMV            AppResourceKind = "materialized_view"
      AppKindInput         AppResourceKind = "input"
  )
  type AppResource struct { Kind AppResourceKind; Name string; DDL string }
  func AppResources() []AppResource
  func AppExcludedDDL() []string // names of ddl* constants deliberately excluded
  ```

- [ ] **Step 1: Write the failing tests**

```go
// backend/internal/timeplus/app_resources_test.go
package timeplus

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
	"testing"
)

var createHeadRe = regexp.MustCompile(`(?is)^\s*CREATE\s+(MUTABLE\s+STREAM|STREAM|MATERIALIZED\s+VIEW|INPUT)\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z_][a-z0-9_]*)`)

func TestAppResources_NamesUniqueAndMatchDDL(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range AppResources() {
		if seen[r.Name] {
			t.Errorf("duplicate resource name %q", r.Name)
		}
		seen[r.Name] = true
		m := createHeadRe.FindStringSubmatch(r.DDL)
		if m == nil {
			t.Errorf("%s: DDL head not recognised: %.60q", r.Name, r.DDL)
			continue
		}
		if m[2] != r.Name {
			t.Errorf("%s: DDL creates %q", r.Name, m[2])
		}
		want := map[string]AppResourceKind{
			"STREAM": AppKindStream, "MUTABLE STREAM": AppKindMutableStream,
			"MATERIALIZED VIEW": AppKindMV, "INPUT": AppKindInput,
		}[strings.ToUpper(strings.Join(strings.Fields(m[1]), " "))]
		if want != r.Kind {
			t.Errorf("%s: kind %q but DDL is %q", r.Name, r.Kind, m[1])
		}
	}
}

func TestAppResources_Order(t *testing.T) {
	// Sinks must be declared before the MVs that write INTO them, and sources
	// before MVs that read FROM them — the installer executes in list order.
	declared := map[string]bool{}
	intoRe := regexp.MustCompile(`(?i)\bINTO\s+([a-z_][a-z0-9_]*)`)
	fromRe := regexp.MustCompile(`(?i)\bFROM\s+([a-z_][a-z0-9_]*)`)
	for _, r := range AppResources() {
		if r.Kind == AppKindMV {
			for _, m := range intoRe.FindAllStringSubmatch(r.DDL, -1) {
				if !declared[m[1]] {
					t.Errorf("%s writes INTO %s before it is declared", r.Name, m[1])
				}
			}
			for _, m := range fromRe.FindAllStringSubmatch(r.DDL, -1) {
				if strings.HasPrefix(m[1], "agentguard_") || strings.HasPrefix(m[1], "otel_") {
					if !declared[m[1]] {
						t.Errorf("%s reads FROM %s before it is declared", r.Name, m[1])
					}
				}
			}
		}
		declared[r.Name] = true
	}
}

// Every top-level ddl* const/var in init.go must be classified: either it is
// part of the app (AppResources) or it is deliberately excluded
// (AppExcludedDDL). Adding a stream to Init() without deciding fails here.
func TestAppResources_CoverEveryDDLConstant(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "init.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			for _, n := range vs.Names {
				if strings.HasPrefix(n.Name, "ddl") {
					names = append(names, n.Name)
				}
			}
		}
	}
	if len(names) < 40 {
		t.Fatalf("expected to find the ddl* constants in init.go, got %d", len(names))
	}
	included := map[string]bool{}
	for _, r := range AppResources() {
		included[r.ddlConst] = true
	}
	excluded := map[string]bool{}
	for _, n := range AppExcludedDDL() {
		excluded[n] = true
	}
	for _, n := range names {
		switch {
		case included[n] && excluded[n]:
			t.Errorf("%s is both included and excluded", n)
		case !included[n] && !excluded[n]:
			t.Errorf("%s is neither in AppResources() nor AppExcludedDDL() — decide whether the app ships it", n)
		}
	}
}
```

Note the test reads the unexported `ddlConst` field — add it to the struct (it is the Go identifier of the constant, used only by this test).

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/ -run 'TestAppResources' -v`
Expected: compile error — `AppResources`, `AppResourceKind` undefined.

- [ ] **Step 3: Implement `app_resources.go`**

```go
// backend/internal/timeplus/app_resources.go
package timeplus

// AppResourceKind is a Timeplus app manifest resource type
// (https://github.com/timeplus-io/apps package format v1).
type AppResourceKind string

const (
	AppKindStream        AppResourceKind = "stream"
	AppKindMutableStream AppResourceKind = "mutable_stream"
	AppKindMV            AppResourceKind = "materialized_view"
	AppKindInput         AppResourceKind = "input"
)

// AppResource is one provisioned object, referenced by the same DDL constant
// Init() executes. Nothing is copied — the .tpapp exporter (internal/tpapp)
// transforms these in place, so the app and the setup wizard cannot drift.
type AppResource struct {
	Kind AppResourceKind
	Name string // object name without database, e.g. "agentguard_cim_event"
	DDL  string // the ddl* constant, verbatim (bare CREATE, unqualified names)

	ddlConst string // Go identifier of DDL; used by the coverage test only
}

func res(kind AppResourceKind, name, ddlConst, ddl string) AppResource {
	return AppResource{Kind: kind, Name: name, DDL: ddl, ddlConst: ddlConst}
}

// AppResources returns the data-pipeline subset of what Init() provisions, in
// Init()'s dependency order (sinks before the MVs that write into them).
// Control-plane objects (notifications, holds, RBAC, Sentry, DLP) are NOT
// here — see AppExcludedDDL and docs/superpowers/specs/2026-09-10-tpapp-export-design.md.
func AppResources() []AppResource {
	return []AppResource{
		// raw OTel
		res(AppKindStream, "otel_traces", "ddlOTelTraces", ddlOTelTraces),
		res(AppKindStream, "otel_logs", "ddlOTelLogs", ddlOTelLogs),
		res(AppKindStream, "otel_metrics_gauge", "ddlOTelMetricsGauge", ddlOTelMetricsGauge),
		res(AppKindStream, "otel_metrics_sum", "ddlOTelMetricsSum", ddlOTelMetricsSum),
		res(AppKindStream, "otel_metrics_histogram", "ddlOTelMetricsHistogram", ddlOTelMetricsHistogram),
		res(AppKindStream, "otel_metrics_exponential_histogram", "ddlOTelMetricsExponentialHistogram", ddlOTelMetricsExponentialHistogram),
		res(AppKindStream, "otel_metrics_summary", "ddlOTelMetricsSummary", ddlOTelMetricsSummary),
		res(AppKindInput, "agentguard_otel_input", "ddlOTelInput", ddlOTelInput),
		// raw hooks
		res(AppKindStream, "agentguard_hook_events", "ddlAgentGuardHookEvents", ddlAgentGuardHookEvents),
		// CIM
		res(AppKindStream, "agentguard_cim_event", "ddlCIMEvent", ddlCIMEvent),
		res(AppKindMV, "mv_cim_claudecode", "ddlCIMClaudeCodeMV", ddlCIMClaudeCodeMV),
		res(AppKindMV, "mv_cim_openclaw", "ddlCIMOpenClawMV", ddlCIMOpenClawMV),
		res(AppKindMV, "mv_cim_hermes", "ddlCIMHermesMV", ddlCIMHermesMV),
		res(AppKindMV, "mv_cim_openinference", "ddlCIMOpenInferenceMV", ddlCIMOpenInferenceMV),
		res(AppKindMV, "mv_hook_bridge_openinference", "ddlHookBridgeOpenInferenceMV", ddlHookBridgeOpenInferenceMV),
		// metrics (lookup feeders first — the metrics MVs JOIN against them)
		res(AppKindMutableStream, "agentguard_session_agents", "ddlSessionAgentsStream", ddlSessionAgentsStream),
		res(AppKindMutableStream, "agentguard_run_latency", "ddlRunLatencyStream", ddlRunLatencyStream),
		res(AppKindStream, "agentguard_cim_metrics", "ddlCIMMetrics", ddlCIMMetrics),
		res(AppKindMV, "mv_session_agents", "ddlSessionAgentsMV", ddlSessionAgentsMV),
		res(AppKindMV, "mv_run_latency", "ddlRunLatencyMV", ddlRunLatencyMV),
		res(AppKindMV, "mv_cim_metrics_openclaw_llm", "ddlCIMMetricsOpenClawLlmMV", ddlCIMMetricsOpenClawLlmMV),
		res(AppKindMV, "mv_cim_metrics_openclaw_tools", "ddlCIMMetricsOpenClawToolsMV", ddlCIMMetricsOpenClawToolsMV),
		res(AppKindMV, "mv_cim_metrics_claudecode_otel", "ddlCIMMetricsClaudeCodeOtelMV", ddlCIMMetricsClaudeCodeOtelMV),
		res(AppKindMV, "mv_cim_metrics_claudecode_tools", "ddlCIMMetricsClaudeCodeToolsMV", ddlCIMMetricsClaudeCodeToolsMV),
		res(AppKindMV, "mv_cim_metrics_hermes_llm", "ddlCIMMetricsHermesLlmMV", ddlCIMMetricsHermesLlmMV),
		res(AppKindMV, "mv_cim_metrics_hermes_tools", "ddlCIMMetricsHermesToolsMV", ddlCIMMetricsHermesToolsMV),
		res(AppKindMV, "mv_cim_metrics_openinference_llm", "ddlCIMMetricsOpenInferenceLlmMV", ddlCIMMetricsOpenInferenceLlmMV),
		// security
		res(AppKindStream, "agentguard_security_events", "ddlSecurityEvents", ddlSecurityEvents),
		res(AppKindMutableStream, "agentguard_threats", "ddlThreatsStream", ddlThreatsStream),
		res(AppKindMV, "mv_threats", "ddlThreatsMV", ddlThreatsMV),
		// cost (schema only; no seed rows)
		res(AppKindMutableStream, "agentguard_pricing", "ddlPricingStream", ddlPricingStream),
		res(AppKindStream, "agentguard_cost_budgets", "ddlCostBudgetsStream", ddlCostBudgetsStream),
		// skills
		res(AppKindStream, "agentguard_skills", "ddlSkillsStream", ddlSkillsStream),
		res(AppKindMV, "mv_skills_invoked_claudecode", "ddlMVSkillsInvokedClaudeCode", ddlMVSkillsInvokedClaudeCode),
		res(AppKindMV, "mv_skills_invoked_openclaw", "ddlMVSkillsInvokedOpenClaw", ddlMVSkillsInvokedOpenClaw),
		res(AppKindMV, "mv_skills_invoked_hermes", "ddlMVSkillsInvokedHermes", ddlMVSkillsInvokedHermes),
		res(AppKindMV, "mv_skills_loaded_claudecode", "ddlMVSkillsLoadedClaudeCode", ddlMVSkillsLoadedClaudeCode),
		res(AppKindMV, "mv_skills_loaded_openclaw", "ddlMVSkillsLoadedOpenClaw", ddlMVSkillsLoadedOpenClaw),
		// memory ops
		res(AppKindStream, "agentguard_memory_ops", "ddlMemoryOpsStream", ddlMemoryOpsStream),
		res(AppKindMV, "mv_memory_claudecode_automem", "ddlMVMemoryClaudeCodeAutoMem", ddlMVMemoryClaudeCodeAutoMem),
		res(AppKindMV, "mv_memory_claudecode_mcp", "ddlMVMemoryClaudeCodeMCP", ddlMVMemoryClaudeCodeMCP),
		res(AppKindMV, "mv_memory_openclaw_file", "ddlMVMemoryOpenClawFile", ddlMVMemoryOpenClawFile),
		res(AppKindMV, "mv_memory_hermes", "ddlMVMemoryHermes", ddlMVMemoryHermes),
		// rules snapshot log (instances are NOT seeded; rule MVs come from internal/tpapp)
		res(AppKindStream, "agentguard_rules", "ddlRulesStream", ddlRulesStream),
	}
}

// AppExcludedDDL lists the ddl* constants in init.go that the app deliberately
// does not ship (backend-only control plane). Kept explicit so the coverage
// test forces a decision for every new constant.
func AppExcludedDDL() []string {
	return []string{
		"ddlNotifyEventsStream",  // notifications: feeds the backend toast watcher only
		"ddlNotifyFilterMV",      // notifications
		"ddlNotificationsStream", // notifications seen-state
	}
}
```

Check the actual constant identifiers with `grep -n "^const ddl\|^var ddl" internal/timeplus/init.go` and adjust any name that differs. If the coverage test reports a constant you did not expect (e.g. one under a different prefix), classify it and add it to the right list — do not widen the prefix filter.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/ -run 'TestAppResources' -v`
Expected: PASS ×3.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/timeplus/app_resources.go backend/internal/timeplus/app_resources_test.go
git commit -m "feat(timeplus): exported ordered AppResources list for the .tpapp exporter (#291)"
```

---

### Task 2: `tpapp.Transform` — DDL text transformations

**Files:**
- Create: `backend/internal/tpapp/transform.go`
- Test: `backend/internal/tpapp/transform_test.go`

**Interfaces:**
- Produces:
  ```go
  const DBPlaceholder = "{{ .DB }}"
  func EnsureIfNotExists(ddl string) (string, error)   // bare CREATE STREAM/MUTABLE STREAM/MATERIALIZED VIEW/INPUT → IF NOT EXISTS
  func Qualify(sql string, names []string) string       // every \bname\b → {{ .DB }}.name, skipping already-qualified
  func TemplateOTelInput(ddl string) (string, error)    // tcp_port/listen_host → {{ .Config.* }}
  func StripTrailingSemicolon(s string) string
  func Transform(ddl string, names []string) (string, error) // EnsureIfNotExists + Qualify + StripTrailingSemicolon
  ```

- [ ] **Step 1: Write the failing tests**

```go
// backend/internal/tpapp/transform_test.go
package tpapp

import (
	"strings"
	"testing"
)

func TestEnsureIfNotExists(t *testing.T) {
	cases := map[string]string{
		"\nCREATE STREAM otel_traces\n(\n a string\n)":               "\nCREATE STREAM IF NOT EXISTS otel_traces\n(\n a string\n)",
		"CREATE MUTABLE STREAM IF NOT EXISTS agentguard_threats (x string)": "CREATE MUTABLE STREAM IF NOT EXISTS agentguard_threats (x string)",
		"CREATE MUTABLE STREAM agentguard_pricing (x string)":              "CREATE MUTABLE STREAM IF NOT EXISTS agentguard_pricing (x string)",
		"CREATE MATERIALIZED VIEW mv_threats\nINTO agentguard_threats AS SELECT 1": "CREATE MATERIALIZED VIEW IF NOT EXISTS mv_threats\nINTO agentguard_threats AS SELECT 1",
		"CREATE INPUT agentguard_otel_input\nSETTINGS type='otel'":          "CREATE INPUT IF NOT EXISTS agentguard_otel_input\nSETTINGS type='otel'",
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
	if strings.Contains(Qualify(got, names), "{{ .DB }}.{{ .DB }}") {
		t.Error("Qualify must be idempotent")
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/tpapp/ -v`
Expected: compile error — package has no non-test files / undefined symbols.

- [ ] **Step 3: Implement `transform.go`**

```go
// backend/internal/tpapp/transform.go
// Package tpapp renders the AgentGuard core pipeline as a Timeplus app
// (.tpapp, package format v1) from the same DDL constants timeplus.Init()
// executes. See docs/superpowers/specs/2026-09-10-tpapp-export-design.md.
package tpapp

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// DBPlaceholder is the installer's Go-template expression for the app database.
const DBPlaceholder = "{{ .DB }}"

var createHeadRe = regexp.MustCompile(`(?is)^(\s*CREATE\s+(?:MUTABLE\s+STREAM|STREAM|MATERIALIZED\s+VIEW|INPUT)\s+)(IF\s+NOT\s+EXISTS\s+)?`)

// EnsureIfNotExists inserts IF NOT EXISTS into a bare CREATE STREAM /
// CREATE MUTABLE STREAM / CREATE MATERIALIZED VIEW / CREATE INPUT head. App
// upgrades re-run every DDL file, so every CREATE must be re-runnable.
func EnsureIfNotExists(ddl string) (string, error) {
	loc := createHeadRe.FindStringSubmatchIndex(ddl)
	if loc == nil {
		return "", fmt.Errorf("unsupported CREATE head: %.60q", strings.TrimSpace(ddl))
	}
	if loc[4] != -1 { // IF NOT EXISTS already present
		return ddl, nil
	}
	return ddl[:loc[3]] + "IF NOT EXISTS " + ddl[loc[3]:], nil
}

// Qualify rewrites every whole-word occurrence of a known resource name as
// {{ .DB }}.name. Occurrences already preceded by "{{ .DB }}." are left
// alone, so the function is idempotent. It intentionally rewrites inside
// string literals and comments too: CREATE INPUT target streams are quoted
// ('otel_traces'), and a stream name never legitimately appears as data in
// this DDL.
func Qualify(sql string, names []string) string {
	// Longest first so "agentguard_cim_event" does not pre-empt
	// "agentguard_cim_events" if both were ever names (\b already prevents
	// partial matches; ordering is belt-and-braces).
	sorted := append([]string(nil), names...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, n := range sorted {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(n) + `\b`)
		sql = re.ReplaceAllStringFunc(sql, func(m string) string { return DBPlaceholder + "." + m })
	}
	// Collapse any double qualification produced by a name that was already
	// qualified in the input.
	return strings.ReplaceAll(sql, DBPlaceholder+"."+DBPlaceholder+".", DBPlaceholder+".")
}

var (
	otelPortRe = regexp.MustCompile(`(?m)^(\s*tcp_port\s*=\s*)\d+(\s*,?)`)
	otelHostRe = regexp.MustCompile(`(?m)^(\s*listen_host\s*=\s*)'[^']*'(\s*,?)`)
)

// TemplateOTelInput replaces the literal OTLP port and bind address in the
// agentguard_otel_input DDL with manifest config references.
func TemplateOTelInput(ddl string) (string, error) {
	if !otelPortRe.MatchString(ddl) || !otelHostRe.MatchString(ddl) {
		return "", fmt.Errorf("otel input DDL: tcp_port/listen_host settings not found")
	}
	ddl = otelPortRe.ReplaceAllString(ddl, "${1}{{ .Config.otel_port }}${2}")
	ddl = otelHostRe.ReplaceAllString(ddl, "${1}'{{ .Config.otel_listen_host }}'${2}")
	return ddl, nil
}

// StripTrailingSemicolon trims whitespace and one trailing ';' — the installer
// wants exactly one bare statement per file.
func StripTrailingSemicolon(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ";")
	return strings.TrimSpace(s)
}

// Transform applies EnsureIfNotExists, Qualify and StripTrailingSemicolon —
// the standard pipeline for a stream/MV/input DDL constant.
func Transform(ddl string, names []string) (string, error) {
	out, err := EnsureIfNotExists(ddl)
	if err != nil {
		return "", err
	}
	return StripTrailingSemicolon(Qualify(out, names)), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/tpapp/ -v`
Expected: PASS ×5.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/tpapp/transform.go backend/internal/tpapp/transform_test.go
git commit -m "feat(tpapp): DDL transformations for the app exporter (IF NOT EXISTS, {{ .DB }} qualification, OTel config)"
```

---

### Task 3: `tpapp.RuleArtifacts` — the four core rules, paused

**Files:**
- Create: `backend/internal/tpapp/rules.go`
- Test: `backend/internal/tpapp/rules_test.go`

**Interfaces:**
- Consumes: `catalog.New(catalog.SeedFS, "")`, `(*catalog.Manager).Lookup(id)`, `rules.Compile(def, nil)` → `.SQL`, `catalog.ExtractUDFNames([]string)`, `timeplus.MVName(ruleID)` (returns `mv_rule_rp001` for `rp-001`), Task 2's `Qualify`, `StripTrailingSemicolon`.
- Produces:
  ```go
  var CoreRuleIDs = []string{"rp-001", "rp-002", "rp-003", "rp-004"}
  type Resource struct { Kind string; Name string; File string; SQL string } // Kind ∈ stream|mutable_stream|materialized_view|input|udf|system
  func RuleArtifacts(names []string) ([]Resource, error) // udf* → mv → pause, per rule, in CoreRuleIDs order
  ```
  `File` is left empty here; `Build` (Task 4) assigns `ddl/NNN_<name>.sql`.

- [ ] **Step 1: Write the failing tests**

```go
// backend/internal/tpapp/rules_test.go
package tpapp

import (
	"strings"
	"testing"
)

func TestRuleArtifacts_ShapeAndOrder(t *testing.T) {
	names := []string{"agentguard_cim_event", "agentguard_security_events"}
	got, err := RuleArtifacts(names)
	if err != nil {
		t.Fatal(err)
	}
	var mvs, pauses, udfs int
	for i, r := range got {
		switch r.Kind {
		case "materialized_view":
			mvs++
			if !strings.HasPrefix(r.Name, "mv_rule_rp00") {
				t.Errorf("unexpected MV name %q", r.Name)
			}
			wantHead := "CREATE MATERIALIZED VIEW IF NOT EXISTS {{ .DB }}." + r.Name + "\nINTO {{ .DB }}.agentguard_security_events\nAS "
			if !strings.HasPrefix(r.SQL, wantHead) {
				t.Errorf("%s: head is %.120q", r.Name, r.SQL)
			}
			if strings.Contains(r.SQL, "FROM agentguard_cim_event") {
				t.Errorf("%s: unqualified source stream", r.Name)
			}
			if !strings.Contains(r.SQL, "FROM {{ .DB }}.agentguard_cim_event") {
				t.Errorf("%s: expected qualified FROM", r.Name)
			}
			// the very next artifact must pause this MV
			if i+1 >= len(got) || got[i+1].Kind != "system" || got[i+1].Name != "pause_"+r.Name ||
				got[i+1].SQL != "SYSTEM PAUSE MATERIALIZED VIEW {{ .DB }}."+r.Name {
				t.Errorf("%s is not followed by its pause resource", r.Name)
			}
		case "system":
			pauses++
		case "udf":
			udfs++
			if strings.Contains(r.SQL, "{{ .DB }}") {
				t.Errorf("udf %s must not be database-qualified", r.Name)
			}
			if !strings.HasPrefix(r.SQL, "CREATE OR REPLACE FUNCTION "+r.Name) {
				t.Errorf("udf %s: head %.80q", r.Name, r.SQL)
			}
			// its rule's MV comes later
			found := false
			for _, later := range got[i+1:] {
				if later.Kind == "materialized_view" && strings.Contains(later.SQL, r.Name+"(") {
					found = true
				}
			}
			if !found {
				t.Errorf("udf %s is not used by a later MV", r.Name)
			}
		default:
			t.Errorf("unexpected kind %q", r.Kind)
		}
		if strings.HasSuffix(strings.TrimSpace(r.SQL), ";") {
			t.Errorf("%s ends with ';'", r.Name)
		}
	}
	if mvs != 4 || pauses != 4 {
		t.Errorf("want 4 MVs + 4 pauses, got %d + %d", mvs, pauses)
	}
	if udfs < 1 {
		t.Error("rp-002 ships ag_dlp_jwt_check; expected at least one udf artifact")
	}
	if got[0].Name != "mv_rule_rp001" {
		t.Errorf("first artifact should be mv_rule_rp001, got %s", got[0].Name)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/tpapp/ -run TestRuleArtifacts -v`
Expected: compile error — `RuleArtifacts`, `Resource` undefined.

- [ ] **Step 3: Implement `rules.go`**

```go
// backend/internal/tpapp/rules.go
package tpapp

import (
	"fmt"

	"github.com/timeplus-io/agentguard/backend/internal/catalog"
	"github.com/timeplus-io/agentguard/backend/internal/rules"
	"github.com/timeplus-io/agentguard/backend/internal/timeplus"
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
```

Check the module path with `head -1 backend/go.mod` and `timeplus.MVName`'s exact behaviour with `grep -n "func MVName" -A4 backend/internal/timeplus/rules.go`; adjust imports/names if they differ.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/tpapp/ -run TestRuleArtifacts -v`
Expected: PASS. If the UDF-used-by-MV check fails because the seed SQL calls the UDF with a different spelling, inspect `rp-002.yaml` and adjust the assertion to what the template actually contains (the UDF must be referenced; the assertion is on the reference, not the shape).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/tpapp/rules.go backend/internal/tpapp/rules_test.go
git commit -m "feat(tpapp): compile core rules rp-001..004 into paused MV artifacts"
```

---

### Task 4: `tpapp.Build` — manifest, file naming, static files

**Files:**
- Create: `backend/internal/tpapp/build.go`, `backend/internal/tpapp/manifest.yaml.tmpl`, `backend/internal/tpapp/static/README.md`, `backend/internal/tpapp/static/Makefile`, `backend/internal/tpapp/static/icon.svg`, `backend/internal/tpapp/static/dashboards/main.json`
- Test: `backend/internal/tpapp/build_test.go`

**Interfaces:**
- Consumes: `timeplus.AppResources()`, Task 2 transforms, Task 3 `RuleArtifacts`, `Resource`.
- Produces:
  ```go
  const (
      AppID = "io.timeplus.agentguard"; AppName = "AgentGuard"; AppVersion = "0.1.0"; AppDBName = "ag"
  )
  type File struct { Path string; Content []byte }   // Path relative to the app dir, "/"-separated
  type App struct { Resources []Resource; Files []File } // Files: manifest.yaml, ddl/*.sql, dashboards/main.json, README.md, Makefile
  func Build() (*App, error)
  ```

- [ ] **Step 1: Add the static files**

`backend/internal/tpapp/static/icon.svg` (catalog style — gradient rounded square, white shield with `</>`):

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 48 48"><defs><linearGradient id="bg" x1="0" y1="0" x2="48" y2="48" gradientUnits="userSpaceOnUse"><stop offset="0%" stop-color="#D53F8C"/><stop offset="100%" stop-color="#9F2BC0"/></linearGradient></defs><rect width="48" height="48" rx="11" fill="url(#bg)"/><path d="M24 11 L35 15.5 V24 C35 30.5 30 36 24 38 C18 36 13 30.5 13 24 V15.5 Z" stroke="white" stroke-width="1.5" stroke-linejoin="round" fill="none"/><path d="M21 20 L17.5 24.5 L21 29" stroke="white" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/><path d="M27 20 L30.5 24.5 L27 29" stroke="white" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/></svg>
```

`backend/internal/tpapp/static/Makefile` (apps repo template):

```make
APP_NAME    ?= agentguard
OUT         ?= $(APP_NAME).tpapp

NEUTRON_URL ?= http://localhost:8000
TENANT      ?= default

.PHONY: build install

build:
	zip -r $(OUT) manifest.yaml ddl/ dashboards/

install: build
	curl -X POST $(NEUTRON_URL)/$(TENANT)/api/v1beta2/apps/install -F "file=@$(OUT)"
```

`backend/internal/tpapp/static/README.md`:

````markdown
# AgentGuard — core telemetry pipeline for AI coding agents

Installs the [AgentGuard](https://github.com/timeplus-io/AgentGuard) streaming pipeline that turns raw telemetry from AI coding agents (Claude Code, OpenClaw, Hermes, any OpenInference/OTLP emitter) into a normalized event model, per-call metrics, and security detections — all as Timeplus streams and materialized views in database `ag`.

> This package is **generated** from the AgentGuard backend (`make tpapp` in the AgentGuard repo). Do not edit the SQL here by hand; change the backend DDL and regenerate.

## What gets installed

| Layer | Resources |
|---|---|
| Raw OTel | `otel_traces`, `otel_logs`, `otel_metrics_*` streams + `agentguard_otel_input` (OTLP/HTTP receiver on `otel_port`) |
| Raw hooks | `agentguard_hook_events` — agent plugins insert here |
| CIM | `agentguard_cim_event` + `mv_cim_*` normalizers (one per agent type) |
| Metrics | `agentguard_cim_metrics` + `mv_cim_metrics_*` (tokens, cost, latency per call), lookup streams `agentguard_session_agents`, `agentguard_run_latency` |
| Security | `agentguard_security_events`, `agentguard_threats` + `mv_threats` |
| Derived | `agentguard_skills` + `mv_skills_*`, `agentguard_memory_ops` + `mv_memory_*` |
| Cost | `agentguard_pricing`, `agentguard_cost_budgets` (schema only) |
| Rules | `agentguard_rules` + four Core Protection detections `mv_rule_rp001..rp004`, **paused** on install |

Not included (they need the AgentGuard server): user management, notifications, approval holds, Sentry, Semantic DLP, and the rest of the rule catalog.

## Build & install

```bash
make build
make install            # NEUTRON_URL=http://localhost:8000 TENANT=default
```

Or from the apps repo root: `make build APP=agentguard`.

## Config

| key | default | notes |
|---|---|---|
| `otel_port` | `4318` | TCP port the OTLP/HTTP input listens on. Pick another port if 4318 is already taken on the Timeplus host. |
| `otel_listen_host` | `0.0.0.0` | Bind address for the OTLP/HTTP input |

## Sending telemetry

- **OTLP/HTTP** (Claude Code with `CLAUDE_CODE_ENABLE_TELEMETRY=1`, OpenInference exporters): point `OTEL_EXPORTER_OTLP_ENDPOINT` at `http://<timeplus-host>:<otel_port>`.
- **Hook plugins** (AgentGuard Claude Code / OpenClaw / Hermes plugins): configure them to write to stream `ag.agentguard_hook_events`.

## Enabling a detection rule

Rules are installed paused so a fresh install never emits threats unexpectedly:

```sql
SYSTEM RESUME MATERIALIZED VIEW ag.mv_rule_rp001;  -- Prompt Injection Shield
SYSTEM RESUME MATERIALIZED VIEW ag.mv_rule_rp002;  -- DLP Sentinel
SYSTEM RESUME MATERIALIZED VIEW ag.mv_rule_rp003;  -- Privilege Guard
SYSTEM RESUME MATERIALIZED VIEW ag.mv_rule_rp004;  -- Supply Chain Watch
```

Matches land in `ag.agentguard_security_events`; `mv_threats` folds them into `ag.agentguard_threats` (one row per agent/session/rule).

## Attaching the AgentGuard UI

Set `timeplus.database: ag` in AgentGuard's `config.yaml` (or `TIMEPLUS_DATABASE=ag`). The setup wizard then reports every resource as already present and the UI reads the app's streams.

## Dashboard

`dashboards/main.json` — AgentGuard Overview: events per minute by agent type, tokens and cost by model, open threats by severity, recent threats.

## Uninstall

Uninstalling drops database `ag` and **all telemetry stored in it**.
````

`backend/internal/tpapp/static/dashboards/main.json`:

```json
[
  {
    "id": "ag-control-time",
    "title": "Time Range",
    "description": "",
    "position": { "h": 1, "w": 4, "x": 0, "y": 0, "nextX": 4, "nextY": 1 },
    "viz_type": "control",
    "viz_content": "",
    "viz_config": {
      "chartType": "selector",
      "defaultValue": "1h",
      "inlineValues": "5m,15m,1h,6h,24h",
      "label": "Time Range",
      "labelWidth": "80",
      "target": "filter_time_range"
    }
  },
  {
    "id": "ag-events-per-minute",
    "title": "Events per minute by agent type",
    "description": "Normalized CIM events (sessions, LLM calls, tool calls) per minute.",
    "position": { "h": 4, "w": 8, "x": 0, "y": 1, "nextX": 8, "nextY": 5 },
    "viz_type": "chart",
    "viz_content": "SELECT window_start AS time, agent_type, count() AS events FROM tumble(table([[ .DB ]].agentguard_cim_event), 1m) WHERE _tp_time > now() - {{filter_time_range}} GROUP BY window_start, agent_type ORDER BY time",
    "viz_config": {
      "chartType": "line",
      "config": {
        "xAxis": "time",
        "yAxis": ["events"],
        "color": "agent_type",
        "xRange": "Infinity",
        "xFormat": "",
        "xTitle": "",
        "yTitle": "",
        "yRange": { "min": null, "max": null },
        "lineStyle": "curve",
        "dataLabel": false,
        "showAll": false,
        "legend": true,
        "points": false,
        "gridlines": true,
        "unit": { "position": "left", "value": "" },
        "fractionDigits": 0,
        "colors": ["#ED64A6", "#F0BE3E", "#DA4B36", "#9A1563", "#FF4A71", "#D12D50", "#8934D9", "#D53F8C", "#F7775A", "#8934D9"],
        "yTickLabel": { "maxChar": 25 }
      }
    }
  },
  {
    "id": "ag-open-threats",
    "title": "Open threats by severity",
    "description": "Rows in agentguard_threats not yet acknowledged or cleared.",
    "position": { "h": 4, "w": 4, "x": 8, "y": 1, "nextX": 12, "nextY": 5 },
    "viz_type": "chart",
    "viz_content": "SELECT severity, count() AS threats FROM table([[ .DB ]].agentguard_threats) WHERE status = 'open' GROUP BY severity ORDER BY severity",
    "viz_config": {
      "chartType": "column",
      "config": {
        "xAxis": "severity",
        "yAxis": ["threats"],
        "color": "",
        "xTitle": "",
        "yTitle": "",
        "dataLabel": true,
        "legend": false,
        "gridlines": true,
        "unit": { "position": "left", "value": "" },
        "fractionDigits": 0,
        "colors": ["#DA4B36", "#F0BE3E", "#ED64A6", "#8934D9"]
      }
    }
  },
  {
    "id": "ag-tokens-cost",
    "title": "Tokens and cost by model",
    "description": "Sum of input/output tokens and computed cost per LLM model in the selected window.",
    "position": { "h": 4, "w": 6, "x": 0, "y": 5, "nextX": 6, "nextY": 9 },
    "viz_type": "chart",
    "viz_content": "SELECT model, sum(input_tokens) AS input_tokens, sum(output_tokens) AS output_tokens, round(sum(cost_usd), 4) AS cost_usd, count() AS calls FROM table([[ .DB ]].agentguard_cim_metrics) WHERE metric_type = 'llm' AND _tp_time > now() - {{filter_time_range}} GROUP BY model ORDER BY cost_usd DESC",
    "viz_config": {
      "chartType": "table",
      "config": { "columns": [], "pageSize": 20 }
    }
  },
  {
    "id": "ag-recent-threats",
    "title": "Recent threats",
    "description": "Latest security detections across all agents.",
    "position": { "h": 4, "w": 6, "x": 6, "y": 5, "nextX": 12, "nextY": 9 },
    "viz_type": "chart",
    "viz_content": "SELECT last_seen, severity, rule_name, agent_id, session_id, status FROM table([[ .DB ]].agentguard_threats) WHERE last_seen > now() - {{filter_time_range}} ORDER BY last_seen DESC LIMIT 50",
    "viz_config": {
      "chartType": "table",
      "config": { "columns": [], "pageSize": 20 }
    }
  }
]
```

Before finalising the dashboard SQL, confirm the column names against the stream DDL: `grep -n "agent_type\|metric_type\|input_tokens\|cost_usd\|model" backend/internal/timeplus/init.go | head` (CIM event/metrics schemas at `ddlCIMEvent` / `ddlCIMMetrics`) and `ddlThreatsStream` for `status`/`last_seen`. Use the real column names; the dashboard is static text and is only validated by eye and by the integration task.

- [ ] **Step 2: Write the manifest template**

`backend/internal/tpapp/manifest.yaml.tmpl`:

```yaml
package_format_version: 1
id: {{ .ID }}
name: {{ .Name }}
version: {{ .Version }}
author: Timeplus
icon: "{{ .Icon }}"
description: >
  AgentGuard core telemetry pipeline for AI coding agents (Claude Code, OpenClaw,
  Hermes, OpenInference/OTLP). Ingests OTLP traces/logs/metrics and agent hook
  events, normalizes them into a Common Information Model (agentguard_cim_event),
  computes per-call token/cost/latency metrics, and ships four Core Protection
  detection rules (prompt injection, DLP, privilege escalation, supply chain)
  installed PAUSED. Resume a rule with:
    SYSTEM RESUME MATERIALIZED VIEW <db>.mv_rule_rp001;
  Generated from the AgentGuard backend — do not hand-edit the SQL.
db_name: {{ .DBName }}
categories:
  - security
  - observability

config:
  - key: otel_port
    type: integer
    required: false
    default: "4318"
    description: TCP port for the OTLP/HTTP input (agentguard_otel_input). Choose a free port on the Timeplus host.
  - key: otel_listen_host
    type: string
    required: false
    default: "0.0.0.0"
    description: Bind address for the OTLP/HTTP input.

resources:
{{- range .Resources }}
  - file: {{ .File }}
    type: {{ .Kind }}
    name: {{ .Name }}
{{- end }}

dashboards:
  - file: dashboards/main.json
    name: AgentGuard Overview
    description: Events per minute by agent type, tokens and cost by model, open and recent threats.
```

- [ ] **Step 3: Write the failing tests**

```go
// backend/internal/tpapp/build_test.go
package tpapp

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func fileByPath(t *testing.T, app *App, path string) []byte {
	t.Helper()
	for _, f := range app.Files {
		if f.Path == path {
			return f.Content
		}
	}
	t.Fatalf("file %s not produced", path)
	return nil
}

type manifest struct {
	PackageFormatVersion int    `yaml:"package_format_version"`
	ID                   string `yaml:"id"`
	DBName               string `yaml:"db_name"`
	Icon                 string `yaml:"icon"`
	Config               []struct {
		Key     string `yaml:"key"`
		Default string `yaml:"default"`
	} `yaml:"config"`
	Resources []struct {
		File string `yaml:"file"`
		Type string `yaml:"type"`
		Name string `yaml:"name"`
	} `yaml:"resources"`
	Dashboards []struct{ File string `yaml:"file"` } `yaml:"dashboards"`
}

func TestBuild_ManifestMatchesFiles(t *testing.T) {
	app, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := yaml.Unmarshal(fileByPath(t, app, "manifest.yaml"), &m); err != nil {
		t.Fatalf("manifest.yaml does not parse: %v", err)
	}
	if m.PackageFormatVersion != 1 || m.ID != AppID || m.DBName != AppDBName {
		t.Errorf("manifest identity: %+v", m)
	}
	if !strings.HasPrefix(m.Icon, "data:image/svg+xml;base64,") {
		t.Errorf("icon must be an svg data URI, got %.40q", m.Icon)
	}
	if _, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(m.Icon, "data:image/svg+xml;base64,")); err != nil {
		t.Errorf("icon payload is not base64: %v", err)
	}
	if len(m.Config) != 2 || m.Config[0].Key != "otel_port" || m.Config[0].Default != "4318" {
		t.Errorf("config keys: %+v", m.Config)
	}
	if len(m.Resources) != len(app.Resources) {
		t.Fatalf("manifest lists %d resources, Build produced %d", len(m.Resources), len(app.Resources))
	}
	for i, r := range m.Resources {
		want := fmt.Sprintf("ddl/%03d_%s.sql", i+1, app.Resources[i].Name)
		if r.File != want || r.Name != app.Resources[i].Name || r.Type != app.Resources[i].Kind {
			t.Errorf("resource %d: manifest %+v vs built %+v", i, r, app.Resources[i])
		}
		fileByPath(t, app, r.File) // must exist
	}
	if len(m.Dashboards) != 1 || m.Dashboards[0].File != "dashboards/main.json" {
		t.Errorf("dashboards: %+v", m.Dashboards)
	}
	for _, p := range []string{"dashboards/main.json", "README.md", "Makefile"} {
		fileByPath(t, app, p)
	}
}

func TestBuild_EveryDDLFileIsOneQualifiedStatement(t *testing.T) {
	app, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, r := range app.Resources {
		if r.Kind != "system" {
			names[r.Name] = true
		}
	}
	for _, r := range app.Resources {
		sql := string(fileByPath(t, app, r.File))
		if strings.TrimSpace(sql) != strings.TrimSpace(r.SQL) {
			t.Errorf("%s: file content differs from resource SQL", r.File)
		}
		if strings.HasSuffix(strings.TrimSpace(sql), ";") {
			t.Errorf("%s: trailing semicolon", r.File)
		}
		if n := countTopLevelSemicolons(sql); n != 0 {
			t.Errorf("%s: %d statement separators found — one statement per file", r.File, n)
		}
		switch r.Kind {
		case "udf":
			if strings.Contains(sql, DBPlaceholder) {
				t.Errorf("%s: udf must not be db-qualified", r.File)
			}
			if !strings.HasPrefix(sql, "CREATE OR REPLACE FUNCTION ") {
				t.Errorf("%s: udf must be CREATE OR REPLACE FUNCTION", r.File)
			}
		case "system":
			if !strings.HasPrefix(sql, "SYSTEM PAUSE MATERIALIZED VIEW "+DBPlaceholder+".") {
				t.Errorf("%s: %q", r.File, sql)
			}
		default:
			if !strings.Contains(sql, "IF NOT EXISTS "+DBPlaceholder+"."+r.Name) {
				t.Errorf("%s: expected 'IF NOT EXISTS {{ .DB }}.%s' in head: %.100q", r.File, r.Name, sql)
			}
			// no unqualified reference to any app resource
			for n := range names {
				re := "\\b" + n + "\\b"
				for _, m := range regexpFindAll(re, sql) {
					if !strings.HasSuffix(m.before, DBPlaceholder+".") {
						t.Errorf("%s: unqualified reference to %s", r.File, n)
					}
				}
			}
		}
	}
}

func TestBuild_RuleArtifactsComeLastAndPaired(t *testing.T) {
	app, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	first := -1
	for i, r := range app.Resources {
		if strings.HasPrefix(r.Name, "mv_rule_") || r.Kind == "udf" || r.Kind == "system" {
			if first == -1 {
				first = i
			}
		} else if first != -1 {
			t.Errorf("non-rule resource %s appears after rule artifacts started", r.Name)
		}
	}
	if first == -1 {
		t.Fatal("no rule artifacts")
	}
	// agentguard_rules and agentguard_security_events must precede the rule MVs
	for _, need := range []string{"agentguard_rules", "agentguard_security_events", "agentguard_cim_event"} {
		found := false
		for _, r := range app.Resources[:first] {
			if r.Name == need {
				found = true
			}
		}
		if !found {
			t.Errorf("%s must be declared before rule artifacts", need)
		}
	}
}
```

Add the two helpers used above to the test file:

```go
// countTopLevelSemicolons counts ';' outside single-quoted strings, $$ bodies,
// and -- line comments.
func countTopLevelSemicolons(sql string) int {
	n := 0
	inStr, inDollar, inComment := false, false, false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case inComment:
			if c == '\n' {
				inComment = false
			}
		case inDollar:
			if c == '$' && i+1 < len(sql) && sql[i+1] == '$' {
				inDollar = false
				i++
			}
		case inStr:
			if c == '\'' {
				inStr = false
			}
		default:
			switch {
			case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
				inComment = true
			case c == '$' && i+1 < len(sql) && sql[i+1] == '$':
				inDollar = true
				i++
			case c == '\'':
				inStr = true
			case c == ';':
				n++
			}
		}
	}
	return n
}

type match struct{ before string }

func regexpFindAll(pattern, s string) []match {
	re := regexp.MustCompile(pattern)
	var out []match
	for _, loc := range re.FindAllStringIndex(s, -1) {
		start := loc[0] - len(DBPlaceholder) - 1
		if start < 0 {
			start = 0
		}
		out = append(out, match{before: s[start:loc[0]]})
	}
	return out
}
```

(add `"regexp"` to the imports.) If `gopkg.in/yaml.v3` is not already in `go.mod`, check `grep yaml backend/go.mod` and use whichever YAML library the catalog package imports.

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/tpapp/ -run TestBuild -v`
Expected: compile error — `Build`, `App`, `AppID` undefined.

- [ ] **Step 5: Implement `build.go`**

```go
// backend/internal/tpapp/build.go
package tpapp

import (
	"bytes"
	"embed"
	"encoding/base64"
	"fmt"
	"io/fs"
	"path"
	"text/template"

	"github.com/timeplus-io/agentguard/backend/internal/timeplus"
)

const (
	AppID      = "io.timeplus.agentguard"
	AppName    = "AgentGuard"
	AppVersion = "0.1.0" // bump with releases; the drift test pins the output
	AppDBName  = "ag"
)

//go:embed manifest.yaml.tmpl static
var assets embed.FS

// File is one output file, Path relative to the app directory.
type File struct {
	Path    string
	Content []byte
}

// App is the fully rendered package: ordered resources and every file.
type App struct {
	Resources []Resource
	Files     []File
}

// Build renders the app entirely in memory. It needs no Timeplus connection.
func Build() (*App, error) {
	base := timeplus.AppResources()
	names := make([]string, 0, len(base)+len(CoreRuleIDs))
	for _, r := range base {
		names = append(names, r.Name)
	}
	for _, id := range CoreRuleIDs {
		names = append(names, timeplus.MVName(id))
	}

	var resources []Resource
	for _, r := range base {
		ddl := r.DDL
		var err error
		if r.Name == "agentguard_otel_input" {
			if ddl, err = TemplateOTelInput(ddl); err != nil {
				return nil, err
			}
		}
		sql, err := Transform(ddl, names)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", r.Name, err)
		}
		resources = append(resources, Resource{Kind: string(r.Kind), Name: r.Name, SQL: sql})
	}
	ruleRes, err := RuleArtifacts(names)
	if err != nil {
		return nil, err
	}
	resources = append(resources, ruleRes...)

	var files []File
	for i := range resources {
		resources[i].File = fmt.Sprintf("ddl/%03d_%s.sql", i+1, resources[i].Name)
		files = append(files, File{Path: resources[i].File, Content: []byte(resources[i].SQL + "\n")})
	}

	icon, err := assets.ReadFile("static/icon.svg")
	if err != nil {
		return nil, err
	}
	manifest, err := renderManifest(resources, "data:image/svg+xml;base64,"+base64.StdEncoding.EncodeToString(bytes.TrimSpace(icon)))
	if err != nil {
		return nil, err
	}
	files = append([]File{{Path: "manifest.yaml", Content: manifest}}, files...)

	for _, p := range []string{"dashboards/main.json", "README.md", "Makefile"} {
		b, err := assets.ReadFile(path.Join("static", p))
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: p, Content: b})
	}
	return &App{Resources: resources, Files: files}, nil
}

func renderManifest(resources []Resource, icon string) ([]byte, error) {
	src, err := fs.ReadFile(assets, "manifest.yaml.tmpl")
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("manifest").Parse(string(src))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, map[string]any{
		"ID": AppID, "Name": AppName, "Version": AppVersion, "DBName": AppDBName,
		"Icon": icon, "Resources": resources,
	})
	if err != nil {
		return nil, fmt.Errorf("render manifest: %w", err)
	}
	return buf.Bytes(), nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/tpapp/ -v`
Expected: all PASS. Typical first-run failures and fixes:
- *unqualified reference* in a metrics MV: a stream referenced through an alias or sub-select the `\b` regex missed — inspect the file and fix `Qualify`, never the test.
- *statement separators found*: a DDL constant contains a `;` inside a `COMMENT '...'` or similar — the counter already skips quotes; if it is a real second statement, split the constant in `init.go` is NOT allowed; report it.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/tpapp/
git commit -m "feat(tpapp): Build renders manifest, ordered DDL files, dashboard, README, Makefile"
```

---

### Task 5: `Write`/`Diff`, the CLI, Makefile targets, generated `apps/agentguard/`, drift test

**Files:**
- Create: `backend/internal/tpapp/write.go`, `backend/internal/tpapp/write_test.go`, `backend/cmd/tpapp-export/main.go`, `backend/internal/tpapp/export_uptodate_test.go`, `apps/agentguard/**` (generated)
- Modify: `Makefile` (root)

**Interfaces:**
- Produces:
  ```go
  func Write(app *App, dir string) error            // writes all files; removes stale ddl/*.sql
  func Diff(app *App, dir string) ([]string, error) // paths that differ / are missing / are stale; nil = up to date
  ```

- [ ] **Step 1: Write the failing tests**

```go
// backend/internal/tpapp/write_test.go
package tpapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteThenDiffIsClean(t *testing.T) {
	app, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// a stale file that Write must remove
	os.MkdirAll(filepath.Join(dir, "ddl"), 0o755)
	os.WriteFile(filepath.Join(dir, "ddl", "999_stale.sql"), []byte("SELECT 1"), 0o644)

	if err := Write(app, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ddl", "999_stale.sql")); !os.IsNotExist(err) {
		t.Error("stale ddl file was not removed")
	}
	diff, err := Diff(app, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) != 0 {
		t.Errorf("expected clean diff, got %v", diff)
	}
	// mutate one file → Diff reports it
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed"), 0o644)
	diff, _ = Diff(app, dir)
	if len(diff) != 1 || diff[0] != "README.md" {
		t.Errorf("expected [README.md], got %v", diff)
	}
}
```

```go
// backend/internal/tpapp/export_uptodate_test.go
package tpapp

import (
	"path/filepath"
	"runtime"
	"testing"
)

// The committed apps/agentguard/ tree must equal the generator's output.
// Regenerate with `make tpapp` from the repo root.
func TestAppExportUpToDate(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "apps", "agentguard")
	app, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	diff, err := Diff(app, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) != 0 {
		t.Fatalf("apps/agentguard is out of date (run `make tpapp`): %v", diff)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/tpapp/ -run 'TestWrite|TestAppExport' -v`
Expected: compile error — `Write`, `Diff` undefined.

- [ ] **Step 3: Implement `write.go`**

```go
// backend/internal/tpapp/write.go
package tpapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Write renders every file under dir and deletes ddl/*.sql files the app no
// longer produces. Everything is rendered in memory first (Build), so a
// failure never leaves a half-written tree except for I/O errors.
func Write(app *App, dir string) error {
	want := map[string]bool{}
	for _, f := range app.Files {
		want[f.Path] = true
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, f.Content, 0o644); err != nil {
			return err
		}
	}
	stale, err := filepath.Glob(filepath.Join(dir, "ddl", "*.sql"))
	if err != nil {
		return err
	}
	for _, p := range stale {
		rel := "ddl/" + filepath.Base(p)
		if !want[rel] {
			if err := os.Remove(p); err != nil {
				return err
			}
		}
	}
	return nil
}

// Diff returns the relative paths whose on-disk content differs from app
// (missing files included) plus stale ddl/*.sql files. nil means up to date.
func Diff(app *App, dir string) ([]string, error) {
	var out []string
	want := map[string]bool{}
	for _, f := range app.Files {
		want[f.Path] = true
		cur, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil || !bytes.Equal(cur, f.Content) {
			out = append(out, f.Path)
		}
	}
	stale, err := filepath.Glob(filepath.Join(dir, "ddl", "*.sql"))
	if err != nil {
		return nil, err
	}
	for _, p := range stale {
		rel := "ddl/" + filepath.Base(p)
		if !want[rel] {
			out = append(out, rel+" (stale)")
		}
	}
	sort.Strings(out)
	return out, nil
}

// FormatDiff renders Diff output for CLI messages.
func FormatDiff(paths []string) string {
	if len(paths) == 0 {
		return "up to date"
	}
	return fmt.Sprintf("%d file(s) differ:\n  %s", len(paths), strings.Join(paths, "\n  "))
}
```

- [ ] **Step 4: Implement the CLI**

```go
// backend/cmd/tpapp-export/main.go
// tpapp-export renders the AgentGuard core pipeline as a Timeplus app
// (apps/agentguard). It needs no Timeplus connection.
//
//	go run ./cmd/tpapp-export --out ../apps/agentguard
//	go run ./cmd/tpapp-export --out ../apps/agentguard --check   # exit 1 if out of date
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/timeplus-io/agentguard/backend/internal/tpapp"
)

func main() {
	out := flag.String("out", "../apps/agentguard", "output directory (the app root)")
	check := flag.Bool("check", false, "do not write; exit 1 if the output directory is out of date")
	flag.Parse()

	app, err := tpapp.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tpapp-export:", err)
		os.Exit(2)
	}
	if *check {
		diff, err := tpapp.Diff(app, *out)
		if err != nil {
			fmt.Fprintln(os.Stderr, "tpapp-export:", err)
			os.Exit(2)
		}
		fmt.Println(tpapp.FormatDiff(diff))
		if len(diff) > 0 {
			os.Exit(1)
		}
		return
	}
	if err := tpapp.Write(app, *out); err != nil {
		fmt.Fprintln(os.Stderr, "tpapp-export:", err)
		os.Exit(2)
	}
	fmt.Printf("wrote %d files to %s (%d resources)\n", len(app.Files), *out, len(app.Resources))
}
```

- [ ] **Step 5: Add Makefile targets**

In the root `Makefile`, add `tpapp tpapp-check` to the `.PHONY` list and, after the `package-plugins` target:

```make
# ── Timeplus app package ─────────────────────────────────────────────────────

## tpapp: regenerate apps/agentguard from the backend DDL and zip it into apps/agentguard.tpapp
tpapp:
	cd backend && CGO_ENABLED=0 go run ./cmd/tpapp-export --out ../apps/agentguard
	cd apps/agentguard && rm -f ../agentguard.tpapp && zip -qr ../agentguard.tpapp manifest.yaml ddl/ dashboards/
	@echo "→ apps/agentguard.tpapp"

## tpapp-check: fail if apps/agentguard is out of date with the backend DDL
tpapp-check:
	cd backend && CGO_ENABLED=0 go run ./cmd/tpapp-export --out ../apps/agentguard --check
```

Add `apps/*.tpapp` to `.gitignore`.

- [ ] **Step 6: Generate the app and run all tests**

```bash
make tpapp
ls apps/agentguard apps/agentguard/ddl | head -60
cd backend && CGO_ENABLED=0 go test ./internal/tpapp/ ./internal/timeplus/ -v 2>&1 | tail -20
```
Expected: `apps/agentguard/{manifest.yaml,README.md,Makefile,dashboards/main.json,ddl/001_otel_traces.sql … ddl/0NN_pause_mv_rule_rp004.sql}` exists; all tests PASS including `TestAppExportUpToDate`. Then `make tpapp-check` prints `up to date`.

- [ ] **Step 7: Eyeball three generated files**

```bash
cat apps/agentguard/ddl/008_agentguard_otel_input.sql
sed -n 1,12p apps/agentguard/ddl/0*_mv_threats.sql
grep -c "{{ .DB }}" apps/agentguard/ddl/*mv_rule_rp002.sql
```
Expected: input has `{{ .Config.otel_port }}` and `'{{ .DB }}.otel_traces'`; mv_threats is `CREATE MATERIALIZED VIEW IF NOT EXISTS {{ .DB }}.mv_threats\nINTO {{ .DB }}.agentguard_threats …`; rp-002 references are qualified.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/tpapp/ backend/cmd/tpapp-export/ apps/agentguard/ Makefile .gitignore
git commit -m "feat(tpapp): tpapp-export CLI, make tpapp/tpapp-check, generated apps/agentguard with drift test (#291)"
```

---

### Task 6: Backend `timeplus.database` config key

**Files:**
- Modify: `backend/internal/config/config.go` (defaults ~line 163, struct ~line 19, load ~line 240, bindEnv ~line 309, save ~line 350), `backend/internal/config/registry.go` (~line 66), `backend/internal/config/sources.go` (`CoerceAndValidate` ~line 15, `EffectiveValue` ~line 199, `ApplyChange` ~line 279), `backend/cmd/server/main.go` (~line 65/72), `backend/internal/timeplus/client.go` (`Query` line 34, `ExecHTTP` line 161), `backend/internal/timeplus/stream.go` (line 41), `backend/internal/timeplus/init.go` (`connectOnce` line 30)
- Test: `backend/internal/config/database_test.go`, `backend/internal/timeplus/database_url_test.go`

**Interfaces:**
- Produces: `config.TimeplusConfig.Database string` (default `"default"`), `timeplus.HTTPURL(cfg *config.TimeplusConfig, extra string) string`.

- [ ] **Step 1: Write the failing tests**

```go
// backend/internal/config/database_test.go
package config

import "testing"

func TestTimeplusDatabaseDefaultAndValidation(t *testing.T) {
	cfg := &Config{}
	if err := ApplyChange(cfg, "timeplus.database", "ag"); err != nil {
		t.Fatal(err)
	}
	if cfg.Timeplus.Database != "ag" {
		t.Errorf("got %q", cfg.Timeplus.Database)
	}
	if v := EffectiveValue(cfg, "timeplus.database"); v != "ag" {
		t.Errorf("EffectiveValue = %v", v)
	}
	for _, bad := range []string{"Ag", "1ag", "a-g", "a b", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if _, err := CoerceAndValidate("timeplus.database", bad); err == nil {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
	if v, err := CoerceAndValidate("timeplus.database", ""); err != nil || v != "default" {
		t.Errorf("empty should coerce to default, got %v %v", v, err)
	}
	found := false
	for _, k := range Registry() {
		if k.Key == "timeplus.database" {
			found = true
			if k.EnvVar != "TIMEPLUS_DATABASE" || k.FlagName != "timeplus-database" || k.Section != "timeplus" {
				t.Errorf("registry entry: %+v", k)
			}
		}
	}
	if !found {
		t.Error("timeplus.database missing from Registry()")
	}
}
```

```go
// backend/internal/timeplus/database_url_test.go
package timeplus

import (
	"testing"

	"github.com/timeplus-io/agentguard/backend/internal/config"
)

func TestHTTPURL(t *testing.T) {
	cfg := &config.TimeplusConfig{Host: "h", Port: 3218}
	if got := HTTPURL(cfg, ""); got != "http://h:3218/" {
		t.Errorf("got %q", got)
	}
	cfg.Database = "default"
	if got := HTTPURL(cfg, ""); got != "http://h:3218/" {
		t.Errorf("default db must not be appended, got %q", got)
	}
	cfg.Database = "ag"
	if got := HTTPURL(cfg, ""); got != "http://h:3218/?database=ag" {
		t.Errorf("got %q", got)
	}
	if got := HTTPURL(cfg, "default_format=JSONEachRow"); got != "http://h:3218/?default_format=JSONEachRow&database=ag" {
		t.Errorf("got %q", got)
	}
	cfg.Database = ""
	if got := HTTPURL(cfg, "default_format=JSONEachRow"); got != "http://h:3218/?default_format=JSONEachRow" {
		t.Errorf("got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ ./internal/timeplus/ -run 'TestTimeplusDatabase|TestHTTPURL' -v`
Expected: compile errors (`Database` field, `HTTPURL` undefined) / validation not rejecting.

- [ ] **Step 3: Implement in `config`**

`config.go`: add `Database string // default database for every connection; "default" unless attached to an app-installed pipeline (e.g. "ag")` to `TimeplusConfig` after `NativePort`; `v.SetDefault("timeplus.database", "default")`; `cfg.Timeplus.Database = v.GetString("timeplus.database")`; `_ = v.BindEnv("timeplus.database", "TIMEPLUS_DATABASE")`; `v.Set("timeplus.database", cfg.Timeplus.Database)` in the save function.

`registry.go`, after the `timeplus.native-port` entry:

```go
		{Key: "timeplus.database", Section: "timeplus", Label: "Database",
			Description: "Timeplus database holding the AgentGuard streams. Use \"default\" for a wizard-provisioned deployment, or the app's db_name (e.g. \"ag\") when the pipeline was installed as a Timeplus app.",
			Type:        TypeString, EnvVar: "TIMEPLUS_DATABASE", FlagName: "timeplus-database"},
```

`sources.go`: in `CoerceAndValidate` add a case (place it next to `timeplus.host`, following the file's existing pattern for string coercion):

```go
	case "timeplus.database":
		s, err := coerceString(raw) // use the file's existing string coercion helper name
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return "default", nil
		}
		if !dbNameRe.MatchString(s) {
			return nil, fmt.Errorf("timeplus.database must match ^[a-z][a-z0-9_]{0,31}$")
		}
		return s, nil
```
with `var dbNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)` at file scope. In `EffectiveValue`: `case "timeplus.database": return cfg.Timeplus.Database`. In `ApplyChange`: `case "timeplus.database": return setString(&cfg.Timeplus.Database, value)`.

Look at how `CoerceAndValidate` coerces `timeplus.host` first (`sed -n 15,60p backend/internal/config/sources.go`) and reuse that exact helper; if strings are validated via a generic `TypeString` branch rather than per-key cases, add the regex check inside the `timeplus.database` case only.

`cmd/server/main.go`: `flags.String("timeplus-database", "default", "Timeplus database holding the AgentGuard streams")` next to `timeplus-host`, and `_ = viper.BindPFlag("timeplus.database", flags.Lookup("timeplus-database"))`.

- [ ] **Step 4: Implement in `timeplus`**

`client.go`, add:

```go
// HTTPURL builds the Proton HTTP endpoint for cfg. extra is an optional
// pre-encoded query string (e.g. "default_format=JSONEachRow"). When
// cfg.Database is set and not "default", database=<name> is appended so every
// unqualified stream name in handler SQL resolves inside that database — how
// the UI attaches to an app-installed pipeline (db_name "ag").
func HTTPURL(cfg *config.TimeplusConfig, extra string) string {
	u := fmt.Sprintf("http://%s:%d/", cfg.Host, cfg.Port)
	var params []string
	if extra != "" {
		params = append(params, extra)
	}
	if cfg.Database != "" && cfg.Database != "default" {
		params = append(params, "database="+url.QueryEscape(cfg.Database))
	}
	if len(params) == 0 {
		return u
	}
	return u + "?" + strings.Join(params, "&")
}
```
(import `net/url`). Replace `url := fmt.Sprintf("http://%s:%d/", cfg.Host, cfg.Port)` in `Query` and `ExecHTTP` with `url := HTTPURL(cfg, "")` — rename the local variable to `endpoint` to avoid shadowing the `net/url` import. In `stream.go` line 41: `url := HTTPURL(cfg, "default_format=JSONEachRow")` (same rename). In `init.go` `connectOnce`: `Auth: proton.Auth{Database: cfg.Database, Username: cfg.User, Password: cfg.Password}` — the driver treats an empty `Database` as `default`.

Grep for any other place that builds a Timeplus HTTP URL: `grep -rn '"http://%s:%d' backend/internal backend/cmd --include='*.go' | grep -v _test` — every hit must go through `HTTPURL` (exclude the DLP/Sentry model-endpoint URLs, which are not Timeplus).

- [ ] **Step 5: Run tests and build**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/config/ ./internal/timeplus/ ./internal/handlers/ 2>&1 | tail -5`
Expected: build OK, all PASS (existing handler tests compare URLs/SQL only; if one asserts the exact request URL, update it to the new `HTTPURL` output).

- [ ] **Step 6: Frontend settings view**

No frontend change is required: `Settings` renders every `Registry()` key generically by section and type. Verify by starting the app (`make dev`) and confirming **Settings → Timeplus Connection** shows the new **Database** field; if the settings page hard-codes the timeplus keys instead, add the field next to Host following that file's pattern (`grep -n "timeplus.native-port" frontend/src/pages/Settings.jsx`).

- [ ] **Step 7: Commit**

```bash
git add backend/internal/config backend/internal/timeplus backend/cmd/server/main.go frontend/src
git commit -m "feat(config): timeplus.database — attach the UI to an app-installed pipeline (#291)"
```

---

### Task 7: Integration test — install the rendered app into a scratch database

**Files:**
- Create: `backend/internal/timeplus/tpapp_integration_test.go` (build tag `integration`)

**Interfaces:**
- Consumes: `tpapp.Build()`, `timeplus.ExecHTTP`, `timeplus.Query`, `config.TimeplusConfig`. Import cycle check: `tpapp` imports `timeplus`, so this test must live in package `tpapp` or in a separate `_test` package under `handlers`. Put it in `backend/internal/tpapp/integration_test.go`, package `tpapp_test`, to avoid the cycle.

- [ ] **Step 1: Write the test**

```go
//go:build integration

// backend/internal/tpapp/integration_test.go
package tpapp_test

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/timeplus-io/agentguard/backend/internal/config"
	"github.com/timeplus-io/agentguard/backend/internal/timeplus"
	"github.com/timeplus-io/agentguard/backend/internal/tpapp"
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
//   CGO_ENABLED=0 go test -tags integration ./internal/tpapp/ -run TestIntegration_TpappInstall -v
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
		// Drop the input first: it holds the OTLP port and is not database-scoped.
		_ = timeplus.ExecHTTP(cfg, "DROP INPUT IF EXISTS "+db+".agentguard_otel_input")
		_ = timeplus.ExecHTTP(cfg, "DROP DATABASE IF EXISTS "+db+" NO DELAY")
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
	if rows, err := timeplus.Query(cfg, "SELECT name FROM system.inputs WHERE name = 'agentguard_otel_input'"); err != nil || len(rows) == 0 {
		t.Errorf("agentguard_otel_input missing from system.inputs (err=%v)", err)
	}
	// Rule MVs are paused.
	for _, id := range tpapp.CoreRuleIDs {
		mv := timeplus.MVName(id)
		rows, err := timeplus.Query(cfg, fmt.Sprintf("SELECT status FROM system.tables WHERE database = '%s' AND name = '%s'", db, mv))
		if err != nil || len(rows) == 0 {
			t.Errorf("%s: %v", mv, err)
			continue
		}
		if s := strings.ToLower(fmt.Sprint(rows[0]["status"])); !strings.Contains(s, "pause") {
			t.Errorf("%s status = %q, want paused", mv, s)
		}
	}
	// Re-running every file must be a no-op (upgrade path).
	for _, r := range app.Resources {
		if err := timeplus.ExecHTTP(cfg, render(r.SQL)); err != nil {
			t.Errorf("re-run %s: %v", r.Name, err)
		}
	}
}
```

- [ ] **Step 2: Run it against the local Timeplus**

Run: `cd backend && CGO_ENABLED=0 go test -tags integration ./internal/tpapp/ -run TestIntegration_TpappInstall -v`
Expected: PASS. Likely first-run findings and how to handle them:
- `system.tables` has no `status` column for MV pause state on this version — find the right source with `SELECT * FROM system.tables WHERE name='mv_rule_rp001' FORMAT Vertical` (or `system.materialized_views`) and adjust the assertion.
- `CREATE INPUT IF NOT EXISTS` rejected by the parser — the test fails on file 008; report it and, only if confirmed, change `EnsureIfNotExists` to leave `CREATE INPUT` bare and document that inputs are not re-runnable.
- `DROP INPUT` syntax differs — check `SHOW CREATE INPUT` docs via the `timeplus-sql-guide` skill.
- A rule MV fails to create because its SQL references a stream via `table()` the regex did not qualify — fix `Qualify`, regenerate (`make tpapp`), rerun.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/tpapp/integration_test.go apps/agentguard
git commit -m "test(tpapp): integration install of the rendered app into a scratch database"
```

---

### Task 8: Docs

**Files:**
- Create: `docs/tpapp.md`
- Modify: `CLAUDE.md` (Development Commands + Timeplus integration sections), `apps/agentguard/README.md` is generated — do not edit; edit `backend/internal/tpapp/static/README.md` and `make tpapp` if wording changes.

- [ ] **Step 1: Write `docs/tpapp.md`**

```markdown
# Installing the AgentGuard pipeline as a Timeplus app

AgentGuard's core streaming pipeline can be installed two ways:

| | Setup wizard (`POST /api/setup/provision`) | Timeplus app (`apps/agentguard.tpapp`) |
|---|---|---|
| Runs | `timeplus.Init()` from the AgentGuard server | Timeplus apps installer (UI or `POST /<tenant>/api/v1beta2/apps/install`) |
| Database | `default` (unqualified names) | `ag` (`db_name` in the manifest) |
| Contents | everything, incl. control plane (holds, notifications, RBAC, Sentry, DLP) | data pipeline only + 4 Core Protection rules, paused |
| Rules | installed via `/api/rules` (lifecycle service) | `mv_rule_rp001..rp004` pre-created and paused; no `agentguard_rules` rows |

## Generating the package

```bash
make tpapp          # regenerates apps/agentguard/ from backend DDL and zips apps/agentguard.tpapp
make tpapp-check    # CI: fail if apps/agentguard/ is stale
```

The package is generated from the same DDL constants `timeplus.Init()` executes (`backend/internal/timeplus/app_resources.go` lists them in order; `backend/internal/tpapp` transforms them). `TestAppExportUpToDate` fails if the committed `apps/agentguard/` differs from the generator, and `TestAppResources_CoverEveryDDLConstant` fails if a new `ddl*` constant in `init.go` is neither shipped nor explicitly excluded.

## Attaching the AgentGuard UI to an app install

1. Install the app (database `ag`).
2. Set `timeplus.database: ag` in `config.yaml` (or `TIMEPLUS_DATABASE=ag`, `--timeplus-database ag`, or Settings → Timeplus Connection → Database).
3. Run the setup wizard. It reports the app's resources as already existing and creates the control-plane streams (holds, notifications, RBAC, …) inside `ag`.
4. Rules: the four app-installed MVs are invisible to `/rules` until installed there; installing rp-001 from the catalog replaces the app's paused MV with a live one owned by the lifecycle service. Uninstalling the app afterwards drops it again.

## Verifying

`CGO_ENABLED=0 go test -tags integration ./internal/tpapp/ -run TestIntegration_TpappInstall -v` renders the app into a scratch database on a live Timeplus (env `TIMEPLUS_HOST/PORT/USER/PASSWORD`, OTLP port `TPAPP_TEST_OTEL_PORT`, default 14318), asserts every object exists and the rule MVs are paused, re-runs all files (upgrade path), and drops the database.

## Publishing

The apps catalog PR (timeplus-io/apps#50) copies `apps/agentguard/` verbatim. Bump `AppVersion` in `backend/internal/tpapp/build.go` with each published change.
```

- [ ] **Step 2: Update `CLAUDE.md`**

Under **Production build** add:
```
make tpapp        # regenerate apps/agentguard (Timeplus app package) + zip; make tpapp-check verifies
```
Under **Timeplus Proton Integration**, add a short paragraph after the Rules and MVs section:

> **Timeplus app export.** `apps/agentguard/` is a generated Timeplus app (`db_name: ag`) built from `timeplus.AppResources()` by `internal/tpapp` (`make tpapp`). It ships the data pipeline plus rules rp-001..004 paused, and nothing from the control plane. `timeplus.database` (default `default`) points the backend at an app-installed database. Never hand-edit `apps/agentguard/`; a drift test enforces it. See `docs/tpapp.md`.

Also add `timeplus.database` to the config env list in the Architecture section (`TIMEPLUS_DATABASE`).

- [ ] **Step 3: Full verification**

```bash
cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./... 2>&1 | tail -15
cd .. && make tpapp-check && make lint 2>&1 | tail -5
```
Expected: build OK, all tests PASS, `up to date`, lint clean.

- [ ] **Step 4: Commit**

```bash
git add docs/tpapp.md CLAUDE.md
git commit -m "docs: Timeplus app export (make tpapp, timeplus.database, verification)"
```

---

## Self-review

- **Spec coverage:** §3.1 → Task 1; §3.2 Transform/Rules/Build/Write → Tasks 2–5; §3.3 CLI + Makefile → Task 5; §3.4 database key → Task 6; §3.5 static files → Task 4; §6 unit tests → Tasks 1–6, integration → Task 7, docs → Task 8. Manual `.tpapp` install on an apps-API-capable Timeplus is deferred (noted in docs and PR).
- **Type consistency:** `Resource{Kind string, Name, File, SQL}` (Task 3) is used unchanged by Tasks 4, 5, 7; `AppResource.Kind` is `AppResourceKind` and converted with `string(r.Kind)` in `Build`; `HTTPURL(cfg *config.TimeplusConfig, extra string)` used in client.go/stream.go; `tpapp.CoreRuleIDs` used by Task 7.
- **Import cycle:** `tpapp` → `timeplus`, `catalog`, `rules`; the integration test lives in `tpapp_test` (Task 7) so `timeplus` never imports `tpapp`.
