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
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/notify"
	tp "github.com/timeplus/agentguard/internal/timeplus"
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

// ── Persisted notification state (#258) ─────────────────────────────────────

// notificationIdentity is the threat identity a browser confirms. deployment
// and session may legitimately be empty on older/partial events.
type notificationIdentity struct {
	AgentID      string `json:"agent_id"      binding:"required"`
	DeploymentID string `json:"deployment_id"`
	SessionID    string `json:"session_id"`
	RuleID       string `json:"rule_id"       binding:"required"`
}

// confirmNotificationSQL upserts only the cf_confirm family — the event
// columns keep their last-notified values. Identifiers are browser-supplied,
// hence the backslash-aware escaper (pinned by a unit test).
func confirmNotificationSQL(id notificationIdentity) string {
	return fmt.Sprintf(
		`INSERT INTO agentguard_notifications (agent_id, deployment_id, session_id, rule_id, confirmed, confirmed_at) VALUES ('%s', '%s', '%s', '%s', true, now64(3))`,
		escapeSQLStr(id.AgentID), escapeSQLStr(id.DeploymentID), escapeSQLStr(id.SessionID), escapeSQLStr(id.RuleID))
}

// ConfirmNotification handles POST /api/notifications/confirm — the user
// explicitly dismissed or clicked a toast. Idempotent.
func ConfirmNotification(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		var id notificationIdentity
		if err := c.ShouldBindJSON(&id); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id and rule_id are required"})
			return
		}
		if err := tp.ExecHTTP(&cfg.Timeplus, confirmNotificationSQL(id)); err != nil {
			log.Printf("notifications: confirm failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "confirm failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// unconfirmedNotificationsSQL backs browser backfill on SSE connect. The
// LIMIT bounds re-toasting after long disconnects; the visible toast stack
// caps at 5 client-side anyway.
const unconfirmedNotificationsSQL = `SELECT agent_id, deployment_id, session_id, rule_id, rule_name, severity, message, to_string(detected_at) AS detected_at, to_string(notified_at) AS notified_at
FROM table(agentguard_notifications)
WHERE confirmed = false
ORDER BY notified_at DESC
LIMIT 20`

// ListUnconfirmedNotifications handles GET /api/notifications/unconfirmed.
// Best-effort: on query failure (e.g. stream not provisioned yet) it returns
// an empty list rather than an error, so the toast UI stays quiet.
func ListUnconfirmedNotifications(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		rows, err := tp.Query(&cfg.Timeplus, unconfirmedNotificationsSQL)
		if err != nil {
			log.Printf("notifications: unconfirmed query failed: %v", err)
			c.JSON(http.StatusOK, gin.H{"notifications": []any{}})
			return
		}
		out := make([]gin.H, 0, len(rows))
		for _, r := range rows {
			get := func(k string) string { s, _ := r[k].(string); return s }
			out = append(out, gin.H{
				"id":            notify.NotificationID(get("deployment_id"), get("agent_id"), get("session_id"), get("rule_id")),
				"rule_id":       get("rule_id"),
				"rule_name":     get("rule_name"),
				"severity":      get("severity"),
				"agent_id":      get("agent_id"),
				"deployment_id": get("deployment_id"),
				"session_id":    get("session_id"),
				"message":       get("message"),
				"detected_at":   get("detected_at"),
			})
		}
		c.JSON(http.StatusOK, gin.H{"notifications": out})
	}
}
