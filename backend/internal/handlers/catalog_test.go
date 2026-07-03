package handlers

import (
	"bytes"
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

// newCatalogManager builds a test Manager seeded with one definition that has a
// UDF, and writes it to a temp directory so Clone can persist.
func newCatalogManager(t *testing.T) *catalog.Manager {
	t.Helper()
	seed := fstest.MapFS{
		"rp-003.yaml": {Data: []byte(
			"id: rp-003\n" +
				"name: Score Rule\n" +
				"version: 1.0.0\n" +
				"sql_template: \"SELECT ag_rp003_score(tool_input) AS s\"\n" +
				"udfs:\n" +
				"  - \"CREATE OR REPLACE FUNCTION ag_rp003_score(x string) RETURNS float LANGUAGE JAVASCRIPT AS $$ return 1.0; $$;\"\n",
		)},
	}
	m, err := catalog.New(seed, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New: %v", err)
	}
	return m
}

func TestCloneCatalogRule_Created(t *testing.T) {
	mgr := newCatalogManager(t)
	r := gin.New()
	r.POST("/rules/catalog/:id/clone", CloneCatalogRule(mgr))

	body, _ := json.Marshal(map[string]string{"id": "rp-003-c", "name": "Score Clone"})
	req := httptest.NewRequest(http.MethodPost, "/rules/catalog/rp-003/clone", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var def catalog.Definition
	if err := json.Unmarshal(w.Body.Bytes(), &def); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if def.Origin != catalog.OriginCustom {
		t.Errorf("origin = %q, want custom", def.Origin)
	}
	if def.ID != "rp-003-c" {
		t.Errorf("id = %q, want rp-003-c", def.ID)
	}
	if def.Name != "Score Clone" {
		t.Errorf("name = %q, want Score Clone", def.Name)
	}
	// UDF must be rescoped — old name must be gone.
	for _, udf := range def.UDFs {
		if strings.Contains(udf, "ag_rp003_score") {
			t.Error("old UDF name ag_rp003_score still present after clone")
		}
	}
}

func TestCloneCatalogRule_SourceNotFound(t *testing.T) {
	mgr := newCatalogManager(t)
	r := gin.New()
	r.POST("/rules/catalog/:id/clone", CloneCatalogRule(mgr))

	body, _ := json.Marshal(map[string]string{"id": "rp-999-c", "name": "Missing Clone"})
	req := httptest.NewRequest(http.MethodPost, "/rules/catalog/rp-does-not-exist/clone", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCloneCatalogRule_DuplicateID(t *testing.T) {
	mgr := newCatalogManager(t)
	r := gin.New()
	r.POST("/rules/catalog/:id/clone", CloneCatalogRule(mgr))

	// First clone succeeds.
	body, _ := json.Marshal(map[string]string{"id": "rp-003-dup", "name": "First"})
	req := httptest.NewRequest(http.MethodPost, "/rules/catalog/rp-003/clone", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first clone: want 201, got %d: %s", w.Code, w.Body.String())
	}

	// Second clone with same new ID must fail with 409.
	body2, _ := json.Marshal(map[string]string{"id": "rp-003-dup", "name": "Second"})
	req2 := httptest.NewRequest(http.MethodPost, "/rules/catalog/rp-003/clone", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate clone: want 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestCloneCatalogRule_InvalidBody(t *testing.T) {
	mgr := newCatalogManager(t)
	r := gin.New()
	r.POST("/rules/catalog/:id/clone", CloneCatalogRule(mgr))

	// Missing id field.
	body, _ := json.Marshal(map[string]string{"name": "No ID"})
	req := httptest.NewRequest(http.MethodPost, "/rules/catalog/rp-003/clone", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing id, got %d: %s", w.Code, w.Body.String())
	}
}

// newParamCatalogManager seeds a manager with a parameterized custom rule so the
// GET/compile tests exercise config[].key and {{ .Config.key }} rendering.
func newParamCatalogManager(t *testing.T) *catalog.Manager {
	t.Helper()
	seed := fstest.MapFS{
		"rp-003.yaml": {Data: []byte(
			"id: rp-003\n" +
				"name: Score Rule\n" +
				"version: 1.0.0\n" +
				"sql_template: \"SELECT 1 WHERE score > {{ .Config.threshold }}\"\n" +
				"config:\n" +
				"  - key: threshold\n" +
				"    label: Threshold\n" +
				"    type: integer\n" +
				"    default: 15\n" +
				"    min: 1\n" +
				"    max: 100\n",
		)},
	}
	m, err := catalog.New(seed, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New: %v", err)
	}
	return m
}

func TestGetCatalogRule_Found(t *testing.T) {
	mgr := newParamCatalogManager(t)
	r := gin.New()
	r.GET("/rules/catalog/:id", GetCatalogRule(mgr))

	req := httptest.NewRequest(http.MethodGet, "/rules/catalog/rp-003", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var def catalog.Definition
	if err := json.Unmarshal(w.Body.Bytes(), &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if def.ID != "rp-003" || len(def.Parameters) != 1 || def.Parameters[0].Key != "threshold" {
		t.Fatalf("definition not returned with config[].key: %+v", def)
	}
}

func TestGetCatalogRule_NotFound(t *testing.T) {
	mgr := newParamCatalogManager(t)
	r := gin.New()
	r.GET("/rules/catalog/:id", GetCatalogRule(mgr))

	req := httptest.NewRequest(http.MethodGet, "/rules/catalog/nope", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func postCompile(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	r := gin.New()
	r.POST("/rules/catalog/compile", CompileCatalogRule())
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/rules/catalog/compile", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCompileCatalogRule_Valid(t *testing.T) {
	w := postCompile(t, map[string]any{
		"definition": map[string]any{
			"id": "demo", "name": "Demo", "version": "1.0.0",
			"sql_template": "SELECT 1 WHERE x = {{ .Config.t }}",
			"config": []map[string]any{
				{"key": "t", "label": "T", "type": "integer", "default": 15, "min": 1, "max": 100},
			},
		},
		"overrides": map[string]any{"t": 30},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		RenderedSQL    string         `json:"rendered_sql"`
		DefinitionHash string         `json:"definition_hash"`
		Effective      map[string]any `json:"effective"`
		Overrides      map[string]any `json:"overrides"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(out.RenderedSQL, "= 30") {
		t.Errorf("rendered SQL missing override value: %q", out.RenderedSQL)
	}
	if out.DefinitionHash == "" {
		t.Errorf("definition_hash missing")
	}
}

// TestCompileCatalogRule_OmittedType: the compile preview accepts a parameter
// with no type (Apps semantics: omitted type means string) and renders it as a
// quoted string literal, rather than rejecting it as an unknown type.
func TestCompileCatalogRule_OmittedType(t *testing.T) {
	w := postCompile(t, map[string]any{
		"definition": map[string]any{
			"id": "demo", "name": "Demo", "version": "1.0.0",
			"sql_template": "SELECT 1 WHERE tool = {{ .Config.tool }}",
			"config": []map[string]any{
				{"key": "tool", "label": "Tool", "default": "bash"}, // type omitted
			},
		},
		"overrides": map[string]any{"tool": "git"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 for omitted type, got %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		RenderedSQL string `json:"rendered_sql"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(out.RenderedSQL, "'git'") {
		t.Errorf("omitted-type param not rendered as string literal: %q", out.RenderedSQL)
	}
}

// TestCompileCatalogRule_BakesIdentitySentinels: the compile preview substitutes
// the authoring template's '__rule_id__'/'__rule_name__' identity sentinels from
// the definition's own id/name, so the previewed SQL matches what a create/install
// would deploy (rather than leaving literal sentinels that break detection
// attribution).
func TestCompileCatalogRule_BakesIdentitySentinels(t *testing.T) {
	w := postCompile(t, map[string]any{
		"definition": map[string]any{
			"id": "my-rule", "name": "My Rule", "version": "1.0.0",
			"sql_template": "SELECT '__rule_id__' AS rule_id, '__rule_name__' AS rule_name FROM agentguard_hook_events WHERE 1 = 0",
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		RenderedSQL string `json:"rendered_sql"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if strings.Contains(out.RenderedSQL, "__rule_id__") || strings.Contains(out.RenderedSQL, "__rule_name__") {
		t.Errorf("compile preview left identity sentinels unsubstituted: %q", out.RenderedSQL)
	}
	if !strings.Contains(out.RenderedSQL, "'my-rule' AS rule_id") ||
		!strings.Contains(out.RenderedSQL, "'My Rule' AS rule_name") {
		t.Errorf("compile preview did not bake identity from id/name: %q", out.RenderedSQL)
	}
}

func TestCompileCatalogRule_BadTemplate(t *testing.T) {
	w := postCompile(t, map[string]any{
		"definition": map[string]any{
			"id": "demo", "name": "Demo", "version": "1.0.0",
			"sql_template": "SELECT 1 WHERE x = {{ .Config.undeclared }}",
		},
	})
	// Authoring context: a bad submitted template is a 400, not a 500.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad template, got %d: %s", w.Code, w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["kind"] != "bad_template" {
		t.Errorf("expected DefinitionError kind bad_template, got %v", out["kind"])
	}
}

func TestCompileCatalogRule_BadOverride(t *testing.T) {
	w := postCompile(t, map[string]any{
		"definition": map[string]any{
			"id": "demo", "name": "Demo", "version": "1.0.0",
			"sql_template": "SELECT 1 WHERE x = {{ .Config.t }}",
			"config": []map[string]any{
				{"key": "t", "label": "T", "type": "integer", "default": 15, "min": 1, "max": 100},
			},
		},
		"overrides": map[string]any{"t": 999},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad override, got %d: %s", w.Code, w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if _, ok := out["fields"]; !ok {
		t.Errorf("expected ValidationError fields in body, got %v", out)
	}
}

func TestCreateCatalogRule_Created(t *testing.T) {
	mgr := newCatalogManager(t)
	r := gin.New()
	r.POST("/rules/catalog", CreateCatalogRule(mgr))

	def := map[string]any{
		"id": "custom-demo", "name": "Demo", "version": "1.0.0",
		"sql_template": "SELECT event_time AS detected_at, 'custom-demo' AS rule_id FROM agentguard_hook_events",
	}
	raw, _ := json.Marshal(def)
	req := httptest.NewRequest(http.MethodPost, "/rules/catalog", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", w.Code, w.Body.String())
	}
	var got catalog.Definition
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Origin != catalog.OriginCustom {
		t.Errorf("origin = %q, want custom", got.Origin)
	}
	if !mgr.IsCustom("custom-demo") {
		t.Errorf("definition not saved to catalog")
	}
}

func TestUpdateCatalogRule_RejectsBuiltin(t *testing.T) {
	mgr := newParamCatalogManager(t) // rp-003 is builtin (seed)
	lc := rules.NewLifecycle(mgr)
	cfg := &config.Config{}
	r := gin.New()
	r.PUT("/rules/catalog/:id", UpdateCatalogRule(cfg, mgr, lc))

	def, _ := mgr.Lookup("rp-003")
	def.Description = "edited"
	raw, _ := json.Marshal(def)
	req := httptest.NewRequest(http.MethodPut, "/rules/catalog/rp-003", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Editing a builtin in place is a conflict — clone first.
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409 for builtin edit, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateCatalogRule_NotFound(t *testing.T) {
	mgr := newParamCatalogManager(t)
	lc := rules.NewLifecycle(mgr)
	cfg := &config.Config{}
	r := gin.New()
	r.PUT("/rules/catalog/:id", UpdateCatalogRule(cfg, mgr, lc))

	raw, _ := json.Marshal(map[string]any{"name": "X", "version": "1.0.0", "sql_template": "SELECT 1"})
	req := httptest.NewRequest(http.MethodPut, "/rules/catalog/does-not-exist", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCatalogRoutes_RegisterWithoutPanic verifies the new static (compile, create)
// and :id (get, put) catalog routes coexist with the existing static
// (import/export) and :id/clone routes without a gin tree panic, and that the
// static /compile path still resolves to the compile handler (not :id).
func TestCatalogRoutes_RegisterWithoutPanic(t *testing.T) {
	mgr := newParamCatalogManager(t)
	lc := rules.NewLifecycle(mgr)
	cfg := &config.Config{}
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("catalog route registration panicked: %v", rec)
		}
	}()
	r := gin.New()
	g := r.Group("/api")
	g.GET("/rules/catalog", func(c *gin.Context) { c.Status(http.StatusOK) })
	g.POST("/rules/catalog", CreateCatalogRule(mgr))
	g.POST("/rules/catalog/import", ImportCatalog(mgr))
	g.GET("/rules/catalog/export", ExportCatalog(mgr))
	g.POST("/rules/catalog/compile", CompileCatalogRule())
	g.GET("/rules/catalog/:id", GetCatalogRule(mgr))
	g.PUT("/rules/catalog/:id", UpdateCatalogRule(cfg, mgr, lc))
	g.POST("/rules/catalog/:id/clone", CloneCatalogRule(mgr))

	// The static /compile must resolve to the compile handler, not GET :id.
	req := httptest.NewRequest(http.MethodPost, "/api/rules/catalog/compile",
		strings.NewReader(`{"definition":{"id":"x","name":"X","version":"1.0.0","sql_template":"SELECT 1"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("compile route did not resolve to compile handler: got %d (%s)", w.Code, w.Body.String())
	}
}

// TestCompileCatalogRule_RejectsDeprecatedLengthConstraint verifies the deprecated
// min_length/max_length constraints are rejected loudly when submitted via a JSON
// authoring request (finding 5). The JSON decode path must PARSE the fields (not
// silently ignore them the way json:"-" would), so validation can reject them —
// otherwise a JSON author could smuggle a length constraint that is quietly
// dropped, the exact silent loosening the fix exists to prevent.
func TestCompileCatalogRule_RejectsDeprecatedLengthConstraint(t *testing.T) {
	r := gin.New()
	r.POST("/rules/catalog/compile", CompileCatalogRule())

	body := `{"definition":{"id":"rp-len","name":"Len","version":"1.0.0",` +
		`"sql_template":"SELECT 1 WHERE t = {{ .Config.tool }}",` +
		`"config":[{"key":"tool","type":"string","default":"bash","min_length":1}]}}`
	req := httptest.NewRequest(http.MethodPost, "/rules/catalog/compile", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("deprecated min_length via JSON: want 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "min_length") {
		t.Errorf("error should name min_length: %s", w.Body.String())
	}
}

// TestCreateCatalogRule_DuplicateCustomID verifies that POST /rules/catalog returns
// 409 when the submitted ID already exists as a custom definition, so the handler
// never silently overwrites an existing entry.
func TestCreateCatalogRule_DuplicateCustomID(t *testing.T) {
	mgr := newCatalogManager(t)
	r := gin.New()
	r.POST("/rules/catalog", CreateCatalogRule(mgr))

	def := map[string]any{
		"id": "custom-dup", "name": "Dup Test", "version": "1.0.0",
		"sql_template": "SELECT event_time AS detected_at, 'custom-dup' AS rule_id FROM agentguard_hook_events",
	}

	// First creation must succeed.
	raw, _ := json.Marshal(def)
	req := httptest.NewRequest(http.MethodPost, "/rules/catalog", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: want 201, got %d: %s", w.Code, w.Body.String())
	}

	// Second create with the same ID must fail with 409.
	raw2, _ := json.Marshal(def)
	req2 := httptest.NewRequest(http.MethodPost, "/rules/catalog", bytes.NewReader(raw2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("duplicate custom ID: want 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestCreateCatalogRule_BuiltinID verifies that POST /rules/catalog returns 409
// when the submitted ID matches a builtin (seed) definition, preventing the custom
// creation from shadowing the builtin — consistent with PUT which also returns 409
// for in-place builtin edits.
func TestCreateCatalogRule_BuiltinID(t *testing.T) {
	mgr := newCatalogManager(t) // seeded with rp-003 as builtin
	r := gin.New()
	r.POST("/rules/catalog", CreateCatalogRule(mgr))

	def := map[string]any{
		"id": "rp-003", "name": "Shadow Attempt", "version": "1.0.0",
		"sql_template": "SELECT event_time AS detected_at, 'rp-003' AS rule_id FROM agentguard_hook_events",
	}
	raw, _ := json.Marshal(def)
	req := httptest.NewRequest(http.MethodPost, "/rules/catalog", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("builtin ID shadow: want 409, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateCatalogRule_UDFConflict verifies that POST /rules/catalog returns 409
// when the submitted definition declares a UDF function name that is already owned
// by an existing catalog rule. A UDF name collision is operator-correctable
// (rename the function or clone first), so it must never map to 500.
func TestCreateCatalogRule_UDFConflict(t *testing.T) {
	// Seed manager has rp-003 with UDF ag_rp003_score.
	mgr := newCatalogManager(t)
	r := gin.New()
	r.POST("/rules/catalog", CreateCatalogRule(mgr))

	// Submit a new custom rule that reuses the same UDF function name.
	def := map[string]any{
		"id": "custom-conflict", "name": "Conflict Demo", "version": "1.0.0",
		"sql_template": "SELECT ag_rp003_score(x) AS s FROM agentguard_hook_events",
		"udfs": []string{
			"CREATE OR REPLACE FUNCTION ag_rp003_score(x string) RETURNS float LANGUAGE JAVASCRIPT AS $$ return 0.5; $$;",
		},
	}
	raw, _ := json.Marshal(def)
	req := httptest.NewRequest(http.MethodPost, "/rules/catalog", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("want 409 Conflict for UDF name collision, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "ag_rp003_score") {
		t.Errorf("error message should contain the conflicting UDF name, got: %q", msg)
	}
}

func TestRespondCatalogError_StatusMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", catalog.ErrNotFound, http.StatusNotFound},
		{"exists", catalog.ErrExists, http.StatusConflict},
		{"udf conflict", catalog.ErrUDFConflict, http.StatusConflict},
		{"invalid", catalog.ErrInvalid, http.StatusBadRequest},
		{"wrapped invalid", errors.New("outer: " + catalog.ErrInvalid.Error()), http.StatusInternalServerError}, // plain string, not wrapped
		{"wrapped via fmt", func() error {
			// simulate fmt.Errorf("%w: detail", ErrInvalid)
			type wrapErr struct{ e error }
			_ = wrapErr{} // suppress unused warning
			// Use errors.Join to demonstrate IS-chain traversal
			return errors.Join(catalog.ErrInvalid, errors.New("bad field"))
		}(), http.StatusBadRequest},
		{"generic", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			respondCatalogError(c, tc.err)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
