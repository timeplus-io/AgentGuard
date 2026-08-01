package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

type protonQueryRequest struct {
	SQL string `json:"sql" binding:"required"`
}

// errClientGone marks a write failure to the browser (navigated away). It ends
// the stream without logging — this is the normal end of a live tail.
var errClientGone = errors.New("client gone")

// ProtonQueryHandler returns a Gin handler that proxies SQL queries to the
// configured Proton/Timeplus server and streams NDJSON rows back to the client.
// Rows are forwarded byte-for-byte (no re-marshalling) and flushed per line so
// live events reach the browser immediately.
func ProtonQueryHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		var req protonQueryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if strings.TrimSpace(req.SQL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sql is required"})
			return
		}

		flusher, canFlush := c.Writer.(http.Flusher)
		newline := []byte{'\n'}
		err := tp.StreamLines(c.Request.Context(), &cfg.Timeplus, req.SQL,
			func() {
				// Disable proxy buffering and stream NDJSON directly to the client.
				c.Header("Content-Type", "application/x-ndjson")
				c.Header("X-Accel-Buffering", "no")
				c.Header("Cache-Control", "no-cache")
				c.Status(http.StatusOK)
			},
			func(line []byte) error {
				if _, werr := c.Writer.Write(line); werr != nil {
					return errClientGone
				}
				if _, werr := c.Writer.Write(newline); werr != nil {
					return errClientGone
				}
				if canFlush {
					flusher.Flush()
				}
				return nil
			})
		if err == nil || errors.Is(err, errClientGone) {
			return
		}
		var httpErr *tp.StreamHTTPError
		if errors.As(err, &httpErr) {
			c.JSON(httpErr.Status, gin.H{"error": httpErr.Body})
			return
		}
		// context.Canceled is normal: the browser navigated away or the component
		// unmounted, closing the streaming connection. Not an error. This must be
		// checked before the !Written() branch below: Gin's Written() stays false
		// until bytes are actually written (c.Status(200) alone doesn't count), so
		// a client that cancels after Timeplus returned 200 but before the first
		// row would otherwise be misclassified as a 502 "cannot reach Proton".
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		if !c.Writer.Written() {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf(
				"cannot reach Proton at %s:%d — %s", cfg.Timeplus.Host, cfg.Timeplus.Port, err.Error(),
			)})
			return
		}
		// Headers already sent so we can't change status, but we can log.
		sql := req.SQL
		if len(sql) > 200 {
			sql = sql[:200] + "…"
		}
		log.Printf("[proton] stream error: %v | client=%s | sql=%s", err, c.ClientIP(), sql)
	}
}
