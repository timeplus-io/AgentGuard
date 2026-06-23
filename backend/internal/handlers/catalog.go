package handlers

import (
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/catalog"
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
