package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/rules"
)

// respondCatalogError is the unified error responder for all catalog-authoring
// endpoints (clone, create-custom, compile, and future B3 update). It covers
// both pure-catalog errors (ErrNotFound/ErrExists/ErrInvalid) and the rules
// service errors that will flow through the authoring path in B3+.
//
// Mapping rationale vs respondRuleError (rules.go):
//   - DefinitionError → 400 here (operator-submitted definition defect, caller
//     fault) vs 500 in respondRuleError (builtin-default defect, server fault).
//   - CriticalError is checked before ConflictError/ValidationError so the
//     critical path is never misclassified by Unwrap traversal.
//   - isTimeplusUnavailable is deliberately omitted: respondRuleError (rules.go:82)
//     does NOT implement a 503 predicate — it defaults non-typed errors to 500.
//     We follow the same convention rather than inventing a parallel predicate.
func respondCatalogError(c *gin.Context, err error) {
	var verr *rules.ValidationError
	var derr *rules.DefinitionError
	var cerr *rules.ConflictError
	var crit *rules.CriticalError
	switch {
	// CriticalError first: it Unwraps to its cause, so other As/Is checks would
	// traverse into it and could misclassify a critical failure as 404/400.
	case errors.As(err, &crit):
		log.Printf("catalog: CRITICAL: %v", crit)
		c.JSON(http.StatusInternalServerError, gin.H{"error": crit.Error()})
	case errors.Is(err, catalog.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, catalog.ErrExists):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, catalog.ErrUDFConflict): // UDF name collision — operator must rename or clone
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.As(err, &cerr): // upgrade/edit compatibility conflict
		c.JSON(http.StatusConflict, gin.H{"error": cerr.Error()})
	case errors.As(err, &verr):
		c.JSON(http.StatusBadRequest, gin.H{"error": verr.Error(), "fields": verr.Fields})
	case errors.As(err, &derr): // operator-submitted definition defect → caller fault (400)
		c.JSON(http.StatusBadRequest, gin.H{"error": derr.Error(), "kind": derr.Kind, "fields": derr.Fields})
	case errors.Is(err, catalog.ErrInvalid): // wrapped structural Validate() failure
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// cloneRequest is the request body for CloneCatalogRule.
type cloneRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CloneCatalogRule handles POST /api/rules/catalog/:id/clone.
// Creates a copy of the source catalog definition with a new ID and name,
// rescopes its UDF function names, and saves it as a custom definition.
// Returns 201 with the cloned Definition on success.
func CloneCatalogRule(mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		srcID := c.Param("id")
		var req cloneRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
			return
		}
		if req.ID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
			return
		}
		if req.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
			return
		}
		cloned, err := mgr.Clone(srcID, req.ID, req.Name)
		if err != nil {
			respondCatalogError(c, err)
			return
		}
		c.JSON(http.StatusCreated, cloned)
	}
}

// decodeDefinitionBody decodes a catalog.Definition request body with
// json.Decoder.UseNumber() so large integer parameter defaults survive without
// float64 rounding before they reach the compiler (mirrors decodeMutationBody).
func decodeDefinitionBody(c *gin.Context) (catalog.Definition, error) {
	var def catalog.Definition
	dec := json.NewDecoder(c.Request.Body)
	dec.UseNumber()
	if err := dec.Decode(&def); err != nil && !errors.Is(err, io.EOF) {
		return catalog.Definition{}, err
	}
	return def, nil
}

// GetCatalogRule handles GET /api/rules/catalog/:id.
// Returns the full catalog Definition (including config[] parameter schema) or
// 404 when the ID is not in the merged catalog.
func GetCatalogRule(mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := mgr.Lookup(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "rule definition not found"})
			return
		}
		c.JSON(http.StatusOK, d)
	}
}

// compileRequest is the body for POST /api/rules/catalog/compile.
type compileRequest struct {
	Definition catalog.Definition `json:"definition"`
	Overrides  map[string]any     `json:"overrides"`
}

// CompileCatalogRule handles POST /api/rules/catalog/compile.
// It is a PURE validate-and-compile preview: it structurally validates the
// submitted definition, compiles it against the supplied overrides, and returns
// the rendered SQL, definition hash, effective values, and canonical overrides.
// It NEVER touches the catalog, the snapshot store, or Timeplus. Authoring
// errors (bad template / bad override) map to 400 via respondCatalogError.
func CompileCatalogRule() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body compileRequest
		dec := json.NewDecoder(c.Request.Body)
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
			return
		}
		if err := body.Definition.Validate(); err != nil {
			respondCatalogError(c, err)
			return
		}
		compiled, err := rules.Compile(body.Definition, body.Overrides)
		if err != nil {
			respondCatalogError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"rendered_sql":    compiled.SQL,
			"definition_hash": compiled.DefinitionHash,
			"effective":       compiled.Effective,
			"overrides":       compiled.Overrides,
		})
	}
}

// CreateCatalogRule handles POST /api/rules/catalog.
// It creates a custom Definition (forcing Origin=custom) and publishes it to the
// local catalog via Manager.Save. It does NOT install the rule — installation is
// a separate lifecycle action. Returns 201 with the saved Definition.
//
// Returns 409 when the submitted ID already exists in the catalog (whether as a
// custom or builtin entry), mirroring the Clone guard so PUT (which rejects
// in-place builtin edits with 409) and POST stay consistent.
//
// UDF ownership note: Save validates UDF names only against other CATALOG
// definitions. Ownership against UDFs already DEPLOYED by installed instances
// (the snapshot ledger) is enforced later, at install time, by the lifecycle
// service's checkUDFOwnership. So a definition can be authored/saved here with a
// UDF name that collides with a deployed one and will then be rejected (409) on
// install rather than at write — no live MV is ever corrupted. The same applies
// to CloneCatalogRule and ImportCatalog, which also write through Manager.
func CreateCatalogRule(mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		def, err := decodeDefinitionBody(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
			return
		}
		def.Origin = catalog.OriginCustom
		// Guard: reject duplicate IDs (custom or builtin) before Save would
		// silently overwrite the existing catalog entry. Mirrors Clone's Exists
		// check so POST/PUT/Clone are consistent: existing ID → 409 ErrExists.
		if mgr.Exists(def.ID) {
			respondCatalogError(c, catalog.ErrExists)
			return
		}
		if err := mgr.Save(def); err != nil {
			respondCatalogError(c, err)
			return
		}
		c.JSON(http.StatusCreated, def)
	}
}

// UpdateCatalogRule handles PUT /api/rules/catalog/:id.
// It edits an existing custom Definition and, when an instance is installed,
// atomically republishes the definition and redeploys its MV through the
// lifecycle service (rolling both back on failure). The path :id wins over any
// body id; Origin is forced to custom. Returns 200 with the published definition
// (and the new instance when one is installed). Errors — including the
// reject-builtin conflict, definition defects, and a critical rollback failure —
// map via respondCatalogError.
func UpdateCatalogRule(cfg *config.Config, mgr *catalog.Manager, lc *rules.Lifecycle) gin.HandlerFunc {
	return func(c *gin.Context) {
		reqCfg := auth.RequestCfg(c, cfg)
		id := c.Param("id")
		def, err := decodeDefinitionBody(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
			return
		}
		def.ID = id
		def.Origin = catalog.OriginCustom

		inst, err := lc.For(&reqCfg.Timeplus).UpdateDefinition(def, sessionUser(c), "edit definition")
		if err != nil {
			log.Printf("rules: UpdateCatalogRule %s: %v", id, err)
			respondCatalogError(c, err)
			return
		}
		// Re-read the published definition so the response reflects exactly what is
		// persisted; include the instance only when one is installed (UpdateDefinition
		// returns a zero instance for a definition-only edit).
		out := gin.H{"definition": def}
		if published, ok := mgr.Lookup(id); ok {
			out["definition"] = published
		}
		if inst.RuleID != "" {
			out["instance"] = inst
		}
		c.JSON(http.StatusOK, out)
	}
}

// ImportCatalog handles POST /api/rules/catalog/import.
// Accepts a multipart form with a "file" field containing a zip archive of YAML rule files.
// Query param ?force=true overwrites existing entries even when the imported version is not newer.
func ImportCatalog(mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		force := c.Query("force") == "true"

		fh, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "multipart field 'file' is required"})
			return
		}

		f, err := fh.Open()
		if err != nil {
			log.Printf("catalog: ImportCatalog open uploaded file: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot open uploaded file"})
			return
		}
		defer f.Close()

		data, err := io.ReadAll(f)
		if err != nil {
			log.Printf("catalog: ImportCatalog read uploaded file: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot read uploaded file"})
			return
		}

		result, err := mgr.ImportBytes(data, force)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, result)
	}
}

// ExportCatalog handles GET /api/rules/catalog/export.
// Streams a zip archive containing all merged catalog entries as YAML files.
func ExportCatalog(mgr *catalog.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", `attachment; filename="agentguard-catalog.zip"`)
		c.Status(http.StatusOK)

		if err := mgr.ExportZip(c.Writer); err != nil {
			// Headers already sent — log only, cannot send a JSON error.
			log.Printf("[catalog] ExportCatalog zip write failed: %v", err)
		}
	}
}
