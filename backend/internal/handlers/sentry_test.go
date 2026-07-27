package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/holdregistry"
	"github.com/timeplus/agentguard/internal/rules"
	"github.com/timeplus/agentguard/internal/sentry"
	"github.com/timeplus/agentguard/internal/sentry/skills"
)

func TestSentryChat_DisabledReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{} // Sentry.Enabled = false

	body, _ := json.Marshal(map[string]string{"message": "hello"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/sentry/chat", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	SentryChat(cfg, nil, nil, nil, nil)(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body = %s", w.Code, w.Body.String())
	}
}

func TestSentryChat_EmptyMessageReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Sentry.Enabled = true

	body, _ := json.Marshal(map[string]string{"message": "   "})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/sentry/chat", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	SentryChat(cfg, nil, nil, nil, nil)(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
}

func TestSentryChat_MissingBodyReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Sentry.Enabled = true

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/sentry/chat", bytes.NewReader(nil))
	c.Request.Header.Set("Content-Type", "application/json")

	SentryChat(cfg, nil, nil, nil, nil)(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
}

// wantSentrySkills is the full tool surface with agentguard_tools_enabled on.
var wantSentrySkills = []string{
	"load_skill", "load_skill_reference", "list_streams", "describe_stream",
	"run_sql", "dlp_search", "get_threats", "get_holds", "get_rule_status",
	"get_cost_summary", "get_session_timeline", "get_rule_catalog",
	"acknowledge_threat", "clear_threat", "decide_hold",
	"install_rule", "configure_rule", "set_rule_enabled", "set_rule_policy",
	"upgrade_rule", "uninstall_rule", "sync_rules",
}

// baseSentrySkills is the default (AgentGuard tools disabled) surface: a
// generic Timeplus SQL analyst.
var baseSentrySkills = []string{
	"load_skill", "load_skill_reference", "list_streams", "describe_stream", "run_sql",
}

// testRegistry builds sentryRegistry with an empty in-memory catalog manager
// and default (all-enabled) write-tool groups.
func testRegistry(t *testing.T, cfg *config.Config) *skills.Registry {
	t.Helper()
	return sentryRegistryWithCC(t, cfg, sentry.ChatCfg{})
}

// sentryRegistryWithCC is testRegistry with an explicit chat config (for
// write_tool_groups filtering tests).
func sentryRegistryWithCC(t *testing.T, cfg *config.Config, cc sentry.ChatCfg) *skills.Registry {
	t.Helper()
	mgr, err := catalog.New(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return sentryRegistry(cfg, mgr, rules.NewLifecycle(mgr), holdregistry.New(), cc)
}

// TestSentryRegistry_AgentGuardToolsDefaultOff: a zero ChatCfg (absent key)
// yields ONLY the generic Timeplus analyst surface — no AgentGuard read or
// write tools, and load_skill neither lists nor serves agentguard-guide.
func TestSentryRegistry_AgentGuardToolsDefaultOff(t *testing.T) {
	t.Setenv("SENTRY_DEMO_WRITE", "")
	reg := testRegistry(t, &config.Config{}) // zero ChatCfg => disabled

	defs := reg.Defs(skills.GatedWrite)
	if len(defs) != len(baseSentrySkills) {
		t.Fatalf("got %d skills, want %d: %+v", len(defs), len(baseSentrySkills), defs)
	}
	for _, name := range baseSentrySkills {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("base tool %s missing", name)
		}
	}
	for _, name := range []string{"get_threats", "dlp_search", "get_rule_catalog", "acknowledge_threat", "decide_hold"} {
		if _, ok := reg.Get(name); ok {
			t.Errorf("%s must be absent when AgentGuard tools are disabled", name)
		}
	}

	// The agentguard-guide knowledge skill is hidden too.
	sk, _ := reg.Get("load_skill")
	if strings.Contains(sk.Description(), "agentguard-guide") {
		t.Error("load_skill description must not advertise agentguard-guide when disabled")
	}
	out, err := sk.Execute(context.Background(), &skills.RunContext{Loaded: map[string]bool{}},
		json.RawMessage(`{"name":"agentguard-guide"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "unknown skill") || strings.Contains(out, "agentguard-guide") {
		t.Fatalf("agentguard-guide must be unloadable and unlisted, got %.150q", out)
	}
}

func TestSentryRegistry_HasAllSkills(t *testing.T) {
	t.Setenv("SENTRY_DEMO_WRITE", "") // count assumes the demo tool is off
	cfg := &config.Config{}
	reg := sentryRegistryWithCC(t, cfg, sentry.ChatCfg{AgentGuardToolsEnabled: true})

	defs := reg.Defs(skills.GatedWrite) // widest access class: every registered skill must show up
	if len(defs) != len(wantSentrySkills) {
		t.Fatalf("got %d skills, want %d: %+v", len(defs), len(wantSentrySkills), defs)
	}
	got := map[string]bool{}
	for _, d := range defs {
		got[d.Name] = true
	}
	for _, name := range wantSentrySkills {
		if !got[name] {
			t.Errorf("missing skill %q in registry", name)
		}
	}
}

// TestSentryRegistry_DemoWriteEnvGate: demo_write appears ONLY when
// SENTRY_DEMO_WRITE=1 is set in the server's environment.
func TestSentryRegistry_DemoWriteEnvGate(t *testing.T) {
	t.Setenv("SENTRY_DEMO_WRITE", "")
	reg := testRegistry(t, &config.Config{})
	if _, ok := reg.Get("demo_write"); ok {
		t.Fatal("demo_write must be absent without SENTRY_DEMO_WRITE=1")
	}

	t.Setenv("SENTRY_DEMO_WRITE", "1")
	reg = testRegistry(t, &config.Config{})
	sk, ok := reg.Get("demo_write")
	if !ok {
		t.Fatal("demo_write missing with SENTRY_DEMO_WRITE=1")
	}
	if sk.Access() != skills.GatedWrite {
		t.Fatalf("demo_write access = %v, want GatedWrite", sk.Access())
	}
	// And it must be offered in the gated def list (what the loop sends to
	// the model when an approval registry is present).
	found := false
	for _, d := range reg.Defs(skills.GatedWrite) {
		if d.Name == "demo_write" {
			found = true
		}
	}
	if !found {
		t.Fatal("demo_write not in GatedWrite defs")
	}
}

func TestSentryRegistry_DefsReadOnlyExcludesWriteSkills(t *testing.T) {
	cfg := &config.Config{}
	reg := testRegistry(t, cfg)

	defs := reg.Defs(skills.ReadOnly)
	if len(defs) == 0 {
		t.Fatal("expected at least one read-only skill")
	}
	for _, d := range defs {
		sk, ok := reg.Get(d.Name)
		if !ok {
			t.Fatalf("Defs returned %q but Get can't find it", d.Name)
		}
		if sk.Access() != skills.ReadOnly {
			t.Errorf("skill %q leaked into ReadOnly Defs() with access class %v", d.Name, sk.Access())
		}
	}
}
