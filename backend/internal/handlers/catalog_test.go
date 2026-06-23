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

func TestRespondCatalogError_StatusMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", catalog.ErrNotFound, http.StatusNotFound},
		{"exists", catalog.ErrExists, http.StatusConflict},
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
