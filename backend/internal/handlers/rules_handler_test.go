package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/rules"
)

func init() { gin.SetMode(gin.TestMode) }

// TestCreateRule_DuplicateIDIsAtomicConflict verifies the legacy POST /api/rules
// path (finding 3) claims the rule ID through the atomic catalog SaveIfAbsent:
// a create whose ID already exists is rejected with 409 and never overwrites the
// existing definition. The old path used a non-atomic ValidateID()+Save() with an
// unconditional rollback Delete, so two concurrent same-ID creates could both
// pass the guard and one's failed install could delete the definition backing the
// other's installed rule. The concurrency guarantee behind the atomic claim is
// proven deterministically at the catalog layer by
// catalog.TestSaveIfAbsent_ConcurrentSameIDExactlyOneWins; this test pins the
// handler's contract (409 + no overwrite) at the API boundary.
func TestCreateRule_DuplicateIDIsAtomicConflict(t *testing.T) {
	mgr := newParamCatalogManager(t)
	lc := rules.NewLifecycle(mgr)
	cfg := &config.Config{}
	r := gin.New()
	r.POST("/rules", CreateRule(cfg, mgr, lc))

	// A pre-existing custom definition the create must not clobber.
	existing := catalog.Definition{
		ID: "rp-dup", Name: "Existing", Version: "1.0.0",
		SQLTemplate: "SELECT 'keep' AS marker FROM agentguard_hook_events WHERE 1 = 0",
		Origin:      catalog.OriginCustom,
	}
	if err := mgr.Save(existing); err != nil {
		t.Fatalf("seed existing definition: %v", err)
	}

	body := `{"id":"rp-dup","name":"Overwriter","description":"d",` +
		`"query_sql":"SELECT 'CLOBBER' AS marker FROM agentguard_hook_events WHERE 1 = 0"}`
	req := httptest.NewRequest(http.MethodPost, "/rules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate id: want 409, got %d: %s", w.Code, w.Body.String())
	}
	got, ok := mgr.Lookup("rp-dup")
	if !ok {
		t.Fatal("pre-existing definition was deleted by a duplicate create")
	}
	if got.Name != "Existing" || !strings.Contains(got.SQLTemplate, "'keep'") {
		t.Errorf("pre-existing definition was overwritten: name=%q sql=%q", got.Name, got.SQLTemplate)
	}
}

func TestRespondRuleError_StatusMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", rules.ErrNotFound, http.StatusNotFound},
		{"already installed", rules.ErrAlreadyInstalled, http.StatusConflict},
		{"validation", &rules.ValidationError{Fields: []rules.FieldError{{Parameter: "t", Message: "bad"}}}, http.StatusBadRequest},
		{"conflict", &rules.ConflictError{Message: "drift"}, http.StatusConflict},
		{"definition", &rules.DefinitionError{Kind: rules.DefinitionBadTemplate, Err: errors.New("x")}, http.StatusInternalServerError},
		{"critical", &rules.CriticalError{Op: "configure", RuleID: "rp-1", Err: errors.New("a"), RollbackErr: errors.New("b")}, http.StatusInternalServerError},
		{"wrapped not found", errors.New("rules store: " + rules.ErrNotFound.Error()), http.StatusInternalServerError}, // plain string is NOT ErrNotFound
		{"generic", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			respondRuleError(c, tc.err)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestRespondRuleError_ConflictBodyCarriesDetail(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	respondRuleError(c, &rules.ConflictError{
		Message:   "upgrade blocked",
		Conflicts: []rules.CompatibilityIssue{{Parameter: "t", Message: "type changed"}},
		Warnings:  []rules.CompatibilityIssue{{Parameter: "old", Message: "dropped"}},
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	var body struct {
		Error     string `json:"error"`
		Conflicts []struct {
			Parameter string `json:"parameter"`
		} `json:"conflicts"`
		Warnings []struct {
			Parameter string `json:"parameter"`
		} `json:"warnings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Error != "upgrade blocked" || len(body.Conflicts) != 1 || len(body.Warnings) != 1 {
		t.Errorf("conflict detail not serialized: %+v", body)
	}
}

func TestRespondRuleError_ValidationBodyCarriesFields(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	respondRuleError(c, &rules.ValidationError{Fields: []rules.FieldError{
		{Parameter: "command_chain_threshold", Message: "must be <= 100"},
	}})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var body struct {
		Fields []struct {
			Parameter string `json:"parameter"`
			Message   string `json:"message"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Fields) != 1 || body.Fields[0].Parameter != "command_chain_threshold" {
		t.Errorf("field errors not serialized: %+v", body)
	}
}

func TestDecodeMutationBody(t *testing.T) {
	t.Run("preserves large integers as json.Number", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPut, "/",
			strings.NewReader(`{"overrides":{"big":9007199254740993},"reason":"tune"}`))
		body, err := decodeMutationBody(c)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Reason != "tune" {
			t.Errorf("reason = %q", body.Reason)
		}
		n, ok := body.Overrides["big"].(json.Number)
		if !ok {
			t.Fatalf("override not json.Number: %T", body.Overrides["big"])
		}
		// 2^53+1 must survive without float64 rounding.
		if n.String() != "9007199254740993" {
			t.Errorf("large int rounded: %s", n.String())
		}
	})

	t.Run("empty body yields nil overrides", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(""))
		body, err := decodeMutationBody(c)
		if err != nil {
			t.Fatalf("decode empty: %v", err)
		}
		if body.Overrides != nil {
			t.Errorf("expected nil overrides for empty body, got %#v", body.Overrides)
		}
	})

	t.Run("malformed JSON errors", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"overrides":`))
		if _, err := decodeMutationBody(c); err == nil {
			t.Fatal("expected error for malformed JSON")
		}
	})

	t.Run("unknown fields are rejected (typo must not become reset-all)", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPut, "/",
			strings.NewReader(`{"overides":{"threshold":5}}`))
		if _, err := decodeMutationBody(c); err == nil {
			t.Fatal("expected error for unknown field; nil Overrides would silently reset every parameter to defaults")
		}
	})

	t.Run("empty JSON object stays valid with nil overrides", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
		body, err := decodeMutationBody(c)
		if err != nil {
			t.Fatalf("decode {}: %v", err)
		}
		if body.Overrides != nil {
			t.Errorf("expected nil overrides for {}, got %#v", body.Overrides)
		}
	})
}

func TestInstanceToPack(t *testing.T) {
	inst := rules.RuleInstance{
		RuleID:            "rp-003",
		Name:              "Privilege Guard",
		DefinitionVersion: "2.0.0",
		RenderedSQL:       "SELECT 1",
		Enabled:           true,
		BlockPolicy:       "hold",
		Revision:          7,
		SnapshotID:        "snap-7",
	}
	p := instanceToPack(inst)
	if p.ID != "rp-003" || p.Version != "2.0.0" || p.RenderedSQL != "SELECT 1" ||
		!p.Enabled || p.BlockPolicy != "hold" || p.Revision != 7 || p.SnapshotID != "snap-7" {
		t.Fatalf("projection mismatch: %+v", p)
	}
}

// TestOrphanedInstalledRuleIDs verifies SyncMVs's orphan detection: a sync result
// whose rule_id has no catalog definition is reported (installed but orphaned),
// while an installed rule still backed by the catalog is not. The result is always
// a non-nil slice so the API field is [] not null.
func TestOrphanedInstalledRuleIDs(t *testing.T) {
	mgr, err := catalog.New(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New: %v", err)
	}
	// rp-live has a catalog definition; rp-gone does not.
	if err := mgr.Save(catalog.Definition{ID: "rp-live", Name: "Live", Version: "1.0.0",
		SQLTemplate: "SELECT 1", Severity: "warning"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	t.Run("reports only orphaned, preserving order", func(t *testing.T) {
		results := []rules.SyncResult{
			{RuleID: "rp-live", Action: "replaced"},
			{RuleID: "rp-gone", Action: "created"},
			{RuleID: "rp-also-gone", Action: "ok"},
		}
		got := orphanedInstalledRuleIDs(results, mgr)
		want := []string{"rp-gone", "rp-also-gone"}
		if len(got) != len(want) {
			t.Fatalf("orphaned = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("orphaned = %v, want %v", got, want)
			}
		}
	})

	t.Run("none orphaned returns non-nil empty slice", func(t *testing.T) {
		got := orphanedInstalledRuleIDs([]rules.SyncResult{{RuleID: "rp-live"}}, mgr)
		if got == nil {
			t.Fatal("orphaned = nil, want non-nil empty slice (JSON [] not null)")
		}
		if len(got) != 0 {
			t.Fatalf("orphaned = %v, want empty", got)
		}
	})
}
