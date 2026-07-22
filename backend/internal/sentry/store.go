package sentry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/dlpquery"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// storeWriteTimeout bounds a single native-batch write (connect + prepare +
// send) against the Sentry conversation/message streams.
const storeWriteTimeout = 30 * time.Second

// sentryTimeLayout matches the "YYYY-MM-DD HH:MM:SS.mmm" shape Proton emits
// for datetime64(3) columns over the HTTP/JSON query path.
const sentryTimeLayout = "2006-01-02 15:04:05.999999999"

// Store provides CRUD-ish access to the Sentry conversation/message streams
// (agentguard_sentry_conversations, agentguard_sentry_messages), provisioned
// by internal/timeplus.InitSentry (Task 2). Reads go over tp.Query against
// table(...); writes use the native conn.PrepareBatch path (via tp.Connect)
// so arbitrary conversation/message content never has to survive SQL-literal
// escaping, mirroring WriteDLPConfig.
type Store struct {
	TP *config.TimeplusConfig
}

// newConversationID generates a 32-hex-char id from 16 crypto/rand bytes.
func newConversationID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sentry store: generate conversation id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// truncateTitle truncates title to at most 80 runes. Rune-safe: it slices the
// rune array, not the byte string, so multibyte titles aren't corrupted
// mid-codepoint.
func truncateTitle(title string) string {
	r := []rune(title)
	if len(r) <= 80 {
		return title
	}
	return string(r[:80])
}

// parseSentryTime parses a Proton-formatted datetime64(3) string back into a
// time.Time (UTC) for re-insertion on a mutable-stream upsert. Falls back to
// time.Now().UTC() for an empty or unparseable input rather than erroring —
// callers only use this to preserve created_at across an upsert, so the
// worst case of a fallback is a slightly-off created_at, not a failed write.
func parseSentryTime(s string) time.Time {
	if s == "" {
		return time.Now().UTC()
	}
	if t, err := time.ParseInLocation(sentryTimeLayout, s, time.UTC); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	return time.Now().UTC()
}

// formatSentryTime renders t as the "YYYY-MM-DD HH:MM:SS.mmm" shape Proton
// uses for datetime64(3), so freshly-created structs display identically to
// ones read back from a query.
func formatSentryTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.000")
}

// rowStr reads a string field out of a query row, tolerating a missing key,
// a nil value, or a non-string value (stringified via fmt).
func rowStr(r tp.Row, key string) string {
	v, ok := r[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// numFromAny best-effort converts a query-row numeric value (json.Number,
// float64, the native driver's int/uint variants) to int64. Returns 0, false
// for nil/missing/unrecognized values.
func numFromAny(v any) (int64, bool) {
	switch n := v.(type) {
	case nil:
		return 0, false
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return i, true
	case float64:
		return int64(n), true
	case float32:
		return int64(n), true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		return int64(n), true
	default:
		return 0, false
	}
}

// rowUint32 reads a numeric field out of a query row as uint32, defaulting to
// 0 for a missing/nil/unrecognized value.
func rowUint32(r tp.Row, key string) uint32 {
	v, ok := r[key]
	if !ok {
		return 0
	}
	n, ok := numFromAny(v)
	if !ok {
		return 0
	}
	return uint32(n)
}

// nextSeqFromRows derives the next message sequence number from the result of
// `SELECT max(seq) AS max_seq FROM table(agentguard_sentry_messages) WHERE
// conversation_id = ...`. That aggregate always returns exactly one row, with
// max_seq = NULL when the conversation has no messages yet — but this helper
// also tolerates an empty row slice defensively. Returns 1 for "no messages
// yet", else max(seq)+1.
func nextSeqFromRows(rows []tp.Row) uint32 {
	if len(rows) == 0 {
		return 1
	}
	n, ok := numFromAny(rows[0]["max_seq"])
	if !ok {
		return 1
	}
	return uint32(n) + 1
}

// conversationFromRow maps a table(agentguard_sentry_conversations) query row
// to a models.SentryConversation. Pure — no I/O.
func conversationFromRow(r tp.Row) models.SentryConversation {
	return models.SentryConversation{
		ConversationID: rowStr(r, "conversation_id"),
		User:           rowStr(r, "user"),
		Title:          rowStr(r, "title"),
		Status:         rowStr(r, "status"),
		CreatedAt:      rowStr(r, "created_at"),
		UpdatedAt:      rowStr(r, "updated_at"),
	}
}

// messageFromRow maps a table(agentguard_sentry_messages) query row to a
// models.SentryMessage. Pure — no I/O.
func messageFromRow(r tp.Row) models.SentryMessage {
	return models.SentryMessage{
		EventTime:      rowStr(r, "event_time"),
		ConversationID: rowStr(r, "conversation_id"),
		Seq:            rowUint32(r, "seq"),
		Role:           rowStr(r, "role"),
		Content:        rowStr(r, "content"),
		ToolName:       rowStr(r, "tool_name"),
		ToolArgs:       rowStr(r, "tool_args"),
		ToolResult:     rowStr(r, "tool_result"),
		Model:          rowStr(r, "model"),
		TokensIn:       rowUint32(r, "tokens_in"),
		TokensOut:      rowUint32(r, "tokens_out"),
	}
}

// writeConversationRow re-inserts the FULL conversation row via the native
// batch path. agentguard_sentry_conversations is a mutable stream keyed on
// conversation_id, so re-inserting the same id replaces the row — this is
// the only way to "update" a mutable stream. createdAt/updatedAt are passed
// as time.Time (not the struct's string fields) because the native driver
// needs a concrete typed value for the datetime64(3) columns.
func (s *Store) writeConversationRow(c models.SentryConversation, createdAt, updatedAt time.Time) error {
	conn, err := tp.Connect(s.TP)
	if err != nil {
		return fmt.Errorf("sentry store: connect: %w", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), storeWriteTimeout)
	defer cancel()

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO agentguard_sentry_conversations (conversation_id, user, title, status, created_at, updated_at)")
	if err != nil {
		return fmt.Errorf("sentry store: prepare conversation batch: %w", err)
	}
	defer func() { _ = batch.Abort() }()

	if err := batch.Append(c.ConversationID, c.User, c.Title, c.Status, createdAt, updatedAt); err != nil {
		return fmt.Errorf("sentry store: append conversation: %w", err)
	}
	return batch.Send()
}

// CreateConversation creates a new conversation for user with the given
// title (truncated to 80 runes), status "active".
func (s *Store) CreateConversation(user, title string) (models.SentryConversation, error) {
	id, err := newConversationID()
	if err != nil {
		return models.SentryConversation{}, err
	}
	now := time.Now().UTC()
	nowStr := formatSentryTime(now)
	c := models.SentryConversation{
		ConversationID: id,
		User:           user,
		Title:          truncateTitle(title),
		Status:         "active",
		CreatedAt:      nowStr,
		UpdatedAt:      nowStr,
	}
	if err := s.writeConversationRow(c, now, now); err != nil {
		return models.SentryConversation{}, err
	}
	return c, nil
}

// TouchConversation bumps updated_at to now, re-inserting the full row
// (mutable-stream upsert) so the conversation surfaces at the top of List.
func (s *Store) TouchConversation(id string) error {
	c, ok, err := s.Conversation(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("sentry store: conversation %s not found", id)
	}
	createdAt := parseSentryTime(c.CreatedAt)
	return s.writeConversationRow(c, createdAt, time.Now().UTC())
}

// Archive marks a conversation "archived" (re-inserting the full row).
func (s *Store) Archive(id string) error {
	c, ok, err := s.Conversation(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("sentry store: conversation %s not found", id)
	}
	c.Status = "archived"
	createdAt := parseSentryTime(c.CreatedAt)
	return s.writeConversationRow(c, createdAt, time.Now().UTC())
}

// Conversation returns the current row for id, or ok=false if it doesn't
// exist.
func (s *Store) Conversation(id string) (models.SentryConversation, bool, error) {
	sql := fmt.Sprintf(`SELECT conversation_id, user, title, status, created_at, updated_at
FROM table(agentguard_sentry_conversations)
WHERE conversation_id = '%s'
LIMIT 1`, dlpquery.EscapeSQLStr(id))
	rows, err := tp.Query(s.TP, sql)
	if err != nil {
		return models.SentryConversation{}, false, fmt.Errorf("sentry store: query conversation: %w", err)
	}
	if len(rows) == 0 {
		return models.SentryConversation{}, false, nil
	}
	return conversationFromRow(rows[0]), true, nil
}

// List returns user's conversations, most-recently-updated first, capped at
// limit.
func (s *Store) List(user string, limit int) ([]models.SentryConversation, error) {
	if limit <= 0 {
		limit = 50
	}
	sql := fmt.Sprintf(`SELECT conversation_id, user, title, status, created_at, updated_at
FROM table(agentguard_sentry_conversations)
WHERE user = '%s'
ORDER BY updated_at DESC
LIMIT %d`, dlpquery.EscapeSQLStr(user), limit)
	rows, err := tp.Query(s.TP, sql)
	if err != nil {
		return nil, fmt.Errorf("sentry store: list conversations: %w", err)
	}
	out := make([]models.SentryConversation, 0, len(rows))
	for _, r := range rows {
		out = append(out, conversationFromRow(r))
	}
	return out, nil
}

// Messages returns all messages for a conversation, ordered by seq.
func (s *Store) Messages(id string) ([]models.SentryMessage, error) {
	sql := fmt.Sprintf(`SELECT event_time, conversation_id, seq, role, content, tool_name, tool_args, tool_result, model, tokens_in, tokens_out
FROM table(agentguard_sentry_messages)
WHERE conversation_id = '%s'
ORDER BY seq`, dlpquery.EscapeSQLStr(id))
	rows, err := tp.Query(s.TP, sql)
	if err != nil {
		return nil, fmt.Errorf("sentry store: query messages: %w", err)
	}
	out := make([]models.SentryMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, messageFromRow(r))
	}
	return out, nil
}

// NextSeq returns the next message sequence number for a conversation: 1 if
// it has no messages yet, else max(seq)+1.
func (s *Store) NextSeq(id string) (uint32, error) {
	sql := fmt.Sprintf(`SELECT max(seq) AS max_seq
FROM table(agentguard_sentry_messages)
WHERE conversation_id = '%s'`, dlpquery.EscapeSQLStr(id))
	rows, err := tp.Query(s.TP, sql)
	if err != nil {
		return 0, fmt.Errorf("sentry store: query next seq: %w", err)
	}
	return nextSeqFromRows(rows), nil
}

// Append inserts one message via the native batch path (content/tool_args/
// tool_result may contain arbitrary JSON/quotes). If m.EventTime is empty or
// unparseable, event_time is written as now (UTC).
func (s *Store) Append(m models.SentryMessage) error {
	conn, err := tp.Connect(s.TP)
	if err != nil {
		return fmt.Errorf("sentry store: connect: %w", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), storeWriteTimeout)
	defer cancel()

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO agentguard_sentry_messages (event_time, conversation_id, seq, role, content, tool_name, tool_args, tool_result, model, tokens_in, tokens_out)")
	if err != nil {
		return fmt.Errorf("sentry store: prepare message batch: %w", err)
	}
	defer func() { _ = batch.Abort() }()

	evtTime := parseSentryTime(m.EventTime)
	if err := batch.Append(evtTime, m.ConversationID, m.Seq, m.Role, m.Content, m.ToolName, m.ToolArgs, m.ToolResult, m.Model, m.TokensIn, m.TokensOut); err != nil {
		return fmt.Errorf("sentry store: append message: %w", err)
	}
	return batch.Send()
}
