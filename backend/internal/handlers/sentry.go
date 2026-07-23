package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/sentry"
	"github.com/timeplus/agentguard/internal/sentry/skills"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// errConversationForbidden and errConversationNotFound are sentinel errors
// resolveConversation uses to tell the caller which HTTP status a lookup
// failure maps to, distinct from a plain Timeplus/store error (mapped to 500).
var (
	errConversationForbidden = errors.New("sentry: conversation belongs to another user")
	errConversationNotFound  = errors.New("sentry: conversation not found")
)

// sentryRegistry builds the full Phase-1 (read-only) tool surface Sentry's
// chat loop exposes to the model: knowledge lookup, schema introspection,
// ad-hoc SQL, DLP semantic search, and curated cross-resource read tools.
func sentryRegistry(cfg *config.Config) *skills.Registry {
	return skills.NewRegistry(
		skills.NewLoadSkill(cfg.Sentry.SkillDir),
		skills.NewLoadSkillReference(cfg.Sentry.SkillDir),
		skills.NewListStreams(),
		skills.NewDescribeStream(),
		skills.NewRunSQL(),
		skills.NewDLPSearch(),
		skills.NewGetThreats(),
		skills.NewGetHolds(),
		skills.NewGetRuleStatus(),
		skills.NewGetCostSummary(),
		skills.NewGetSessionTimeline(),
	)
}

// resolveConversation implements the create-or-continue semantics shared by
// SentryChat: an empty id starts a new conversation (titled from the first
// message); a non-empty id must exist and be owned by user, else an error
// mapping to 404/403 (checked BEFORE any NDJSON header is written).
func resolveConversation(store *sentry.Store, user, id, message string) (models.SentryConversation, error) {
	if strings.TrimSpace(id) == "" {
		return store.CreateConversation(user, message)
	}
	conv, ok, err := store.Conversation(id)
	if err != nil {
		return models.SentryConversation{}, err
	}
	if !ok {
		return models.SentryConversation{}, errConversationNotFound
	}
	if conv.User != user {
		return models.SentryConversation{}, errConversationForbidden
	}
	return conv, nil
}

// SentryChat handles POST /sentry/chat: {conversation_id?, message} -> an
// NDJSON stream of sentry.Event lines. Every failure that can be reported as
// plain JSON (disabled, bad input, chat-config load, conversation
// resolution/ownership) happens BEFORE any header is written; once streaming
// begins, only NDJSON error events can report a failure.
func SentryChat(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rcfg := auth.RequestCfg(c, cfg)
		if !rcfg.Sentry.Enabled {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Sentry is disabled (sentry.enabled=false)"})
			return
		}
		var body struct {
			ConversationID string `json:"conversation_id"`
			Message        string `json:"message"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Message) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
			return
		}
		user := sessionUser(c)
		store := &sentry.Store{TP: &rcfg.Timeplus}

		// Everything that can fail with a plain JSON status happens BEFORE headers.
		cc, err := sentry.LoadChatCfg(&rcfg.Timeplus)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		conv, err := resolveConversation(store, user, body.ConversationID, body.Message)
		if err != nil {
			switch {
			case errors.Is(err, errConversationForbidden):
				c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			case errors.Is(err, errConversationNotFound):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		c.Header("Content-Type", "application/x-ndjson")
		c.Header("X-Accel-Buffering", "no")
		c.Header("Cache-Control", "no-cache")
		c.Status(http.StatusOK)
		flusher, _ := c.Writer.(http.Flusher)
		emitted := false
		emit := func(ev sentry.Event) {
			emitted = true
			b, err := json.Marshal(ev)
			if err != nil {
				log.Printf("sentry: chat: marshal event: %v", err)
				return
			}
			_, _ = c.Writer.Write(append(b, '\n'))
			if flusher != nil {
				flusher.Flush()
			}
		}

		loop := &sentry.Loop{LLM: sentry.NewOpenAILLM(), Reg: sentryRegistry(rcfg), Store: store, Cfg: rcfg}
		if err := loop.Run(c.Request.Context(), cc, conv, strings.TrimSpace(body.Message), emit); err != nil {
			log.Printf("sentry: chat: %v", err)
			// The loop emits its own "error" event for in-stream failures
			// (e.g. a mid-turn LLM call). The pre-stream Messages/NextSeq
			// path in Loop.Run returns before calling emit at all, so if
			// nothing was written yet, this handler must surface the error
			// itself -- as an NDJSON line, since headers are already sent.
			if !emitted {
				emit(sentry.Event{Type: "error", Message: err.Error()})
			}
		}
	}
}

// ListSentryConversations handles GET /sentry/conversations: the session
// user's own conversations, most-recently-updated first.
func ListSentryConversations(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rcfg := auth.RequestCfg(c, cfg)
		store := &sentry.Store{TP: &rcfg.Timeplus}
		convs, err := store.List(sessionUser(c), 0)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"conversations": convs})
	}
}

// GetSentryConversation handles GET /sentry/conversations/:id: the
// conversation plus its full message replay, owner-only (403/404).
func GetSentryConversation(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rcfg := auth.RequestCfg(c, cfg)
		store := &sentry.Store{TP: &rcfg.Timeplus}
		conv, ok, err := store.Conversation(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
			return
		}
		if conv.User != sessionUser(c) {
			c.JSON(http.StatusForbidden, gin.H{"error": "conversation belongs to another user"})
			return
		}
		msgs, err := store.Messages(conv.ConversationID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"conversation": conv, "messages": msgs})
	}
}

// ArchiveSentryConversation handles DELETE /sentry/conversations/:id:
// owner-only soft-archive (status -> "archived").
func ArchiveSentryConversation(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rcfg := auth.RequestCfg(c, cfg)
		store := &sentry.Store{TP: &rcfg.Timeplus}
		conv, ok, err := store.Conversation(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
			return
		}
		if conv.User != sessionUser(c) {
			c.JSON(http.StatusForbidden, gin.H{"error": "conversation belongs to another user"})
			return
		}
		if err := store.Archive(conv.ConversationID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "archived"})
	}
}

// SentryStatus handles GET /sentry/status: whether Sentry is enabled,
// whether both its streams are provisioned, the resolved chat model, and
// whether a base_url is configured (or a config-load error string).
func SentryStatus(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		rcfg := auth.RequestCfg(c, cfg)
		resp := gin.H{"enabled": rcfg.Sentry.Enabled, "provisioned": false}
		if !rcfg.Sentry.Enabled {
			c.JSON(http.StatusOK, resp)
			return
		}

		rows, err := tp.Query(&rcfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name IN ('agentguard_sentry_conversations', 'agentguard_sentry_messages', 'agentguard_sentry_config')
			  AND database = current_database()`)
		if err != nil {
			log.Printf("sentry: status system.tables: %v", err)
			resp["tables_check_error"] = err.Error()
		} else {
			found := map[string]bool{}
			for _, row := range rows {
				if name, ok := row["name"].(string); ok {
					found[name] = true
				}
			}
			resp["provisioned"] = found["agentguard_sentry_conversations"] &&
				found["agentguard_sentry_messages"] && found["agentguard_sentry_config"]
		}

		cc, err := sentry.LoadChatCfg(&rcfg.Timeplus)
		if err != nil {
			resp["config_error"] = err.Error()
			c.JSON(http.StatusOK, resp)
			return
		}
		resp["model"] = cc.Model
		resp["base_url_set"] = strings.TrimSpace(cc.BaseURL) != ""
		c.JSON(http.StatusOK, resp)
	}
}
