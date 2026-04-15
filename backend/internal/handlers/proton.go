package handlers

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
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

		// Stream line-by-line and flush after each row so live events reach the
		// browser immediately. io.Copy + http.ResponseWriter buffers up to 4 KB
		// before writing, which means individual live rows stall until the buffer
		// fills — unacceptable for real-time streaming.
		flusher, canFlush := c.Writer.(http.Flusher)
		newline := []byte{'\n'}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 256*1024), 256*1024) // 256 KB max line (large event_data)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			if _, err := c.Writer.Write(line); err != nil {
				break
			}
			if _, err := c.Writer.Write(newline); err != nil {
				break
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if err := scanner.Err(); err != nil {
			// headers already sent so we can't change status, but we can log
			log.Printf("[proton] ProtonQueryHandler stream scan error: %v", err)
		}
	}
}
