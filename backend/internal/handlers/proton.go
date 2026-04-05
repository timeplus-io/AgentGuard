package handlers

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentshield/internal/config"
)

type protonQueryRequest struct {
	SQL string `json:"sql" binding:"required"`
}

// ProtonQueryHandler returns a Gin handler that proxies SQL queries to the
// configured Proton/Timeplus server and streams NDJSON rows back to the client.
func ProtonQueryHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req protonQueryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if strings.TrimSpace(req.SQL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sql is required"})
			return
		}

		protonURL := fmt.Sprintf("http://%s:%d/?default_format=JSONEachRow",
			cfg.Timeplus.Host, cfg.Timeplus.Port)

		httpReq, err := http.NewRequestWithContext(
			c.Request.Context(),
			http.MethodPost,
			protonURL,
			strings.NewReader(req.SQL),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		httpReq.Header.Set("Content-Type", "text/plain")
		if cfg.Timeplus.User != "" {
			httpReq.SetBasicAuth(cfg.Timeplus.User, cfg.Timeplus.Password)
		}

		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf(
				"cannot reach Proton at %s:%d — %s", cfg.Timeplus.Host, cfg.Timeplus.Port, err.Error(),
			)})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			c.JSON(resp.StatusCode, gin.H{"error": string(body)})
			return
		}

		// Disable proxy buffering and stream NDJSON directly to the client.
		c.Header("Content-Type", "application/x-ndjson")
		c.Header("X-Accel-Buffering", "no")
		c.Header("Cache-Control", "no-cache")
		c.Status(http.StatusOK)

		io.Copy(c.Writer, resp.Body) //nolint:errcheck
	}
}
