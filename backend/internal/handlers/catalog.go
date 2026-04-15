package handlers

import (
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/catalog"
)

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
			c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot open uploaded file"})
			return
		}
		defer f.Close()

		data, err := io.ReadAll(f)
		if err != nil {
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
