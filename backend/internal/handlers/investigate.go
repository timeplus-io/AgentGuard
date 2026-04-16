package handlers

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// priorityStreams are always shown first and in this order.
var priorityStreams = []string{
	"agentguard_hook_events",
	"agentguard_security_events",
	"agentguard_rules",
}

// ListTables queries Proton for the real schema of each stream.
// Priority AgentGuard streams come first, followed by any otel_* streams found in system.tables.
func ListTables(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		// Discover otel_* streams dynamically.
		otelRows, err := tp.Query(&cfg.Timeplus,
			`SELECT name FROM system.tables WHERE name LIKE 'otel_%' ORDER BY name`)
		if err != nil {
			log.Printf("[investigate] ListTables otel discovery failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		streams := make([]string, 0, len(priorityStreams)+len(otelRows))
		streams = append(streams, priorityStreams...)
		for _, r := range otelRows {
			streams = append(streams, parseStr(r["name"]))
		}

		tables := make([]models.TableInfo, 0, len(streams))
		for _, name := range streams {
			colRows, err := tp.Query(&cfg.Timeplus, fmt.Sprintf(
				`SELECT name, type FROM system.columns WHERE table = '%s' ORDER BY position`,
				escapeSQL(name),
			))
			if err != nil {
				log.Printf("[investigate] ListTables column query for %q failed: %v", name, err)
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
				return
			}
			cols := make([]models.ColumnInfo, 0, len(colRows))
			for _, r := range colRows {
				cols = append(cols, models.ColumnInfo{
					Name: parseStr(r["name"]),
					Type: parseStr(r["type"]),
				})
			}
			tables = append(tables, models.TableInfo{
				Name:    name,
				Type:    "stream",
				Columns: cols,
			})
		}

		c.JSON(http.StatusOK, gin.H{"tables": tables})
	}
}

