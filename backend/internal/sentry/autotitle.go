package sentry

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/timeplus/agentguard/internal/models"
)

// autoTitleTimeout bounds the whole auto-title pass (store reads + one LLM
// call + rename write). MaybeAutoTitle runs detached from the request in a
// goroutine, so it carries its own deadline.
const autoTitleTimeout = 30 * time.Second

// autoTitleSnippetRunes caps how much of the first user/assistant message is
// quoted into the title prompt — the summary needs the gist, not the full
// transcript.
const autoTitleSnippetRunes = 1200

// IsAutoTitle reports whether title is still the auto-derived title for a
// conversation whose first user message was firstUserMsg — i.e. neither the
// operator nor a previous LLM pass has renamed it. This is what makes a
// user rename permanent without a schema column: any rename changes the
// title away from truncate(first message), and the LLM never overwrites a
// non-auto title.
func IsAutoTitle(title, firstUserMsg string) bool {
	return title != "" && title == truncateTitle(firstUserMsg)
}

// truncRunes truncates s to at most n runes (rune-safe, like truncateTitle
// but with a caller-chosen bound).
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// sanitizeTitle normalizes a model-produced title: trims whitespace and
// wrapping quotes, collapses internal whitespace/newlines to single spaces,
// and truncates to the store's 80-rune title bound. Returns "" for anything
// that normalizes to nothing (caller skips the rename).
func sanitizeTitle(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "\"'`“”‘’")
	s = strings.Join(strings.Fields(s), " ")
	return truncateTitle(s)
}

// firstUserAssistant returns the first user message content and the first
// non-empty assistant answer content from a seq-ordered message replay.
// Either may be "" when absent. Pure — no I/O.
func firstUserAssistant(msgs []models.SentryMessage) (user, assistant string) {
	for _, m := range msgs {
		switch {
		case user == "" && m.Role == "user":
			user = m.Content
		case assistant == "" && m.Role == "assistant" && m.Content != "":
			assistant = m.Content
		}
		if user != "" && assistant != "" {
			break
		}
	}
	return user, assistant
}

// buildTitlePrompt returns the minimal message list asking the chat model to
// name the conversation. Bounded input (see autoTitleSnippetRunes), plain
// system+user shape, no tools.
func buildTitlePrompt(userMsg, assistantMsg string) []Msg {
	var b strings.Builder
	b.WriteString("Name this conversation between a security operator and Sentry, AgentGuard's security analyst agent.\n\n")
	b.WriteString("Operator asked:\n")
	b.WriteString(truncRunes(userMsg, autoTitleSnippetRunes))
	if assistantMsg != "" {
		b.WriteString("\n\nSentry answered:\n")
		b.WriteString(truncRunes(assistantMsg, autoTitleSnippetRunes))
	}
	b.WriteString("\n\nReply with ONLY the name: at most 8 words, plain text, no quotes, no trailing punctuation.")
	return []Msg{
		{Role: "system", Content: "You produce short, specific conversation titles. Reply with the title only."},
		{Role: "user", Content: b.String()},
	}
}

// MaybeAutoTitle renames conversation convID with an LLM-generated summary
// title, but only while the conversation still carries its auto-derived
// title (the truncated first user message) — a title set by the operator
// (or by a previous pass of this function) is never overwritten. Every
// failure path is fail-open: the existing title stays and the error is at
// most logged. Intended to be called in a goroutine after a successful chat
// turn; it deliberately uses context.Background (+ own timeout) so it
// survives the request ending.
func MaybeAutoTitle(llm LLM, cc ChatCfg, store *Store, convID string) {
	ctx, cancel := context.WithTimeout(context.Background(), autoTitleTimeout)
	defer cancel()

	conv, ok, err := store.Conversation(convID)
	if err != nil || !ok || conv.Status != "active" {
		return
	}
	msgs, err := store.Messages(convID)
	if err != nil {
		return
	}
	userMsg, assistantMsg := firstUserAssistant(msgs)
	if userMsg == "" || !IsAutoTitle(conv.Title, userMsg) {
		return
	}

	tcc := cc
	tcc.MaxTokens = 64
	res, err := llm.ChatStream(ctx, tcc, buildTitlePrompt(userMsg, assistantMsg), nil, func(string) {})
	if err != nil {
		log.Printf("sentry: auto-title %s: llm: %v", convID, err)
		return
	}
	title := sanitizeTitle(res.Content)
	if title == "" || title == conv.Title {
		return
	}
	if err := store.Rename(convID, title); err != nil {
		log.Printf("sentry: auto-title %s: rename: %v", convID, err)
	}
}

// autoTitleDone, when non-nil, is closed by MaybeAutoTitleAsync's goroutine
// on completion — test hook only.
var autoTitleDone chan struct{}

// MaybeAutoTitleAsync runs MaybeAutoTitle in a goroutine (fire-and-forget
// off the chat turn's critical path).
func MaybeAutoTitleAsync(llm LLM, cc ChatCfg, store *Store, convID string) {
	done := autoTitleDone
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("sentry: auto-title %s: panic: %v", convID, r)
			}
			if done != nil {
				close(done)
			}
		}()
		MaybeAutoTitle(llm, cc, store, convID)
	}()
}
