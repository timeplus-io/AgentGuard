package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

var dlpUDFNames = []string{"dlp_extract_entities", "dlp_embed", "dlp_risk"}

// DLPStatusHandler reports Semantic DLP provisioning + enrichment health.
func DLPStatusHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		status := models.DLPStatus{
			Enabled: cfg.DLP.Enabled,
			UDFs:    map[string]bool{},
		}
		for _, fn := range dlpUDFNames {
			status.UDFs[fn] = false
		}
		if !cfg.DLP.Enabled {
			c.JSON(http.StatusOK, status)
			return
		}

		rows, err := tp.Query(&cfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name IN ('agentguard_dlp_enriched', 'mv_dlp_enrich')
			  AND database = current_database()`)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		for _, row := range rows {
			switch row["name"] {
			case "agentguard_dlp_enriched":
				status.StreamExists = true
			case "mv_dlp_enrich":
				status.MVExists = true
			}
		}

		if fnRows, err := tp.Query(&cfg.Timeplus, `SHOW FUNCTIONS`); err == nil {
			for _, row := range fnRows {
				if name, ok := row["name"].(string); ok {
					if _, tracked := status.UDFs[name]; tracked {
						status.UDFs[name] = true
					}
				}
			}
		} else {
			log.Printf("dlp: status SHOW FUNCTIONS: %v", err)
		}

		if status.StreamExists {
			counts, err := tp.Query(&cfg.Timeplus, `
				SELECT count() AS total,
				       count_if(length(embedding) > 0) AS embedded,
				       count_if(risk_score > 0) AS flagged
				FROM table(agentguard_dlp_enriched)`)
			if err == nil && len(counts) == 1 {
				status.TotalRows = parseInt64(counts[0]["total"])
				status.EmbeddedRows = parseInt64(counts[0]["embedded"])
				status.FlaggedRows = parseInt64(counts[0]["flagged"])
				if status.TotalRows > 0 {
					status.EmbeddingCoverage = float64(status.EmbeddedRows) / float64(status.TotalRows)
				}
			} else if err != nil {
				log.Printf("dlp: status counts: %v", err)
			}
		}

		c.JSON(http.StatusOK, status)
	}
}
