package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/timeplus/agentguard/internal/models"
)

// alertBroadcaster fans webhook payloads out to all connected SSE clients.
// A single server-side instance is shared across goroutines.
type alertBroadcaster struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

// GlobalAlertBroadcaster is the process-wide broadcaster wired to both the
// webhook receiver and the SSE stream handler.
var GlobalAlertBroadcaster = &alertBroadcaster{
	clients: make(map[chan []byte]struct{}),
}

func (b *alertBroadcaster) subscribe() (ch chan []byte, cancel func()) {
	ch = make(chan []byte, 32)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	cancel = func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
		close(ch)
	}
	return
}

// Publish sends a payload to every currently-connected SSE client with a
// fresh unique envelope id (used by holds, where every event is distinct).
// Slow clients whose channel buffer is full are silently skipped.
func (b *alertBroadcaster) Publish(payload models.AlertWebhookPayload) {
	b.PublishWithID(uuid.New().String(), payload)
}

// PublishWithID publishes with a caller-supplied stable envelope id so
// repeated deliveries of the same notification (reconnect backfill, multiple
// tabs) dedup client-side. Used by the notify watcher (#258).
func (b *alertBroadcaster) PublishWithID(id string, payload models.AlertWebhookPayload) {
	envelope := struct {
		models.AlertWebhookPayload
		ID string `json:"id"`
	}{
		AlertWebhookPayload: payload,
		ID:                  id,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		log.Printf("notifications: Publish marshal failed: %v", err)
		return
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- data:
		default:
		}
	}
}

// NotificationsSSEHandler streams alert notifications to browser clients.
// The browser opens one long-lived GET /api/notifications/stream connection;
// each webhook call pushes an "alert" SSE event to every open connection.
func NotificationsSSEHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")
		c.Status(http.StatusOK)

		flusher, canFlush := c.Writer.(http.Flusher)

		ch, cancel := GlobalAlertBroadcaster.subscribe()
		defer cancel()

		// Send an initial comment so the browser confirms the connection is live.
		_, _ = fmt.Fprintf(c.Writer, ": connected\n\n")
		if canFlush {
			flusher.Flush()
		}

		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-c.Request.Context().Done():
				return
			case <-ticker.C:
				_, _ = fmt.Fprintf(c.Writer, ": keepalive\n\n")
				if canFlush {
					flusher.Flush()
				}
			case data, ok := <-ch:
				if !ok {
					return
				}
				_, _ = fmt.Fprintf(c.Writer, "event: alert\ndata: %s\n\n", data)
				if canFlush {
					flusher.Flush()
				}
			}
		}
	}
}
