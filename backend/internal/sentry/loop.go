package sentry

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/sentry/skills"
)

// Event is one line of the NDJSON stream sent to the Sentry chat UI over
// the course of a single turn: meta once, then any number of
// token/tool_call/tool_result, ending in exactly one done or error.
type Event struct {
	Type      string `json:"event"`                     // meta|token|tool_call|tool_result|done|error
	Text      string `json:"text,omitempty"`            // token
	ID        string `json:"id,omitempty"`              // tool_call/tool_result
	Name      string `json:"name,omitempty"`            // tool_call/tool_result
	Args      string `json:"args,omitempty"`            // tool_call
	Result    string `json:"result,omitempty"`          // tool_result
	Truncated bool   `json:"truncated,omitempty"`       // tool_result
	ConvID    string `json:"conversation_id,omitempty"` // meta
	Seq       uint32 `json:"seq,omitempty"`             // meta
	TokensIn  int    `json:"tokens_in,omitempty"`       // done
	TokensOut int    `json:"tokens_out,omitempty"`      // done
	Message   string `json:"message,omitempty"`         // error
}

// Loop is AgentGuard Sentry's ReAct agent harness: it wires an LLM to a
// read-only skills.Registry and a Store, running model -> tools -> model
// until the model stops requesting tools or cc.MaxIterations is hit.
type Loop struct {
	LLM   LLM
	Reg   *skills.Registry
	Store *Store
	Cfg   *config.Config // request-scoped (user creds)
}

// toolCallMeta is the JSON shape persisted in a role="tool" SentryMessage's
// ToolArgs column: the original call id/name alongside the raw args. A bare
// SentryMessage has no tool_call_id column of its own, so without this the
// next turn's history replay (assembleContext) couldn't reconstruct which
// assistant tool_call a stored tool result answers -- it would desync
// ToolCallID across a multi-turn conversation. Persisting it here is what
// lets assembleContext rebuild the pairing purely from history rows.
type toolCallMeta struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

// Run executes one user turn: persists the user message, iterates
// model->tools->model up to cc.MaxIterations, emits events, persists all
// rows. Persist failures are logged but never fail the turn (the chat
// response already succeeded from the operator's point of view).
func (l *Loop) Run(ctx context.Context, cc ChatCfg, conv models.SentryConversation, userMsg string, emit func(Event)) error {
	rc := &skills.RunContext{
		Cfg: l.Cfg, ConversationID: conv.ConversationID,
		SQLTimeoutMs: cc.SQLTimeoutMs, SQLMaxRows: cc.SQLMaxRows,
		ResultMaxBytes: cc.ToolResultMaxBytes, Loaded: map[string]bool{},
	}
	system := strings.ReplaceAll(cc.SystemPrompt, "{schema_card}", skills.SchemaCard(&l.Cfg.Timeplus))

	history, err := l.Store.Messages(conv.ConversationID)
	if err != nil {
		return err
	}
	msgs := assembleContext(system, history, userMsg, cc.TurnTokenBudget)

	seq, err := l.Store.NextSeq(conv.ConversationID)
	if err != nil {
		return err
	}
	emit(Event{Type: "meta", ConvID: conv.ConversationID, Seq: seq})
	l.persist(models.SentryMessage{ConversationID: conv.ConversationID, Seq: seq, Role: "user", Content: userMsg})
	seq++

	tools := defsToTools(l.Reg.Defs(skills.ReadOnly)) // Phase 1: read-only surface
	totalIn, totalOut := 0, 0
	for iter := 0; iter < cc.MaxIterations; iter++ {
		callTools := tools
		if iter == cc.MaxIterations-1 {
			callTools = nil // budget exhausted: force a final answer (spec §13)
		}
		res, err := l.LLM.ChatStream(ctx, cc, msgs, callTools, func(t string) { emit(Event{Type: "token", Text: t}) })
		if err != nil {
			emit(Event{Type: "error", Message: err.Error()})
			return err
		}
		totalIn += res.TokensIn
		totalOut += res.TokensOut
		l.persist(models.SentryMessage{ConversationID: conv.ConversationID, Seq: seq, Role: "assistant",
			Content: res.Content, Model: cc.Model, TokensIn: uint32(res.TokensIn), TokensOut: uint32(res.TokensOut),
			ToolArgs: marshalCalls(res.ToolCalls)})
		seq++
		msgs = append(msgs, Msg{Role: "assistant", Content: res.Content, ToolCalls: res.ToolCalls})

		if len(res.ToolCalls) == 0 {
			emit(Event{Type: "done", TokensIn: totalIn, TokensOut: totalOut})
			if err := l.Store.TouchConversation(conv.ConversationID); err != nil {
				log.Printf("sentry: touch conversation %s failed: %v", conv.ConversationID, err)
			}
			return nil
		}
		for _, tc := range res.ToolCalls {
			emit(Event{Type: "tool_call", ID: tc.ID, Name: tc.Name, Args: tc.Args})
			out := runTool(ctx, l.Reg, rc, tc)
			outStr, cut := skills.Truncate(out, cc.ToolResultMaxBytes)
			emit(Event{Type: "tool_result", ID: tc.ID, Name: tc.Name, Result: outStr, Truncated: cut})

			metaJSON, err := json.Marshal(toolCallMeta{ID: tc.ID, Name: tc.Name, Args: tc.Args})
			if err != nil {
				log.Printf("sentry: marshal tool_call metadata for %s failed: %v", tc.ID, err)
				metaJSON = nil
			}
			l.persist(models.SentryMessage{ConversationID: conv.ConversationID, Seq: seq, Role: "tool",
				ToolName: tc.Name, ToolArgs: string(metaJSON), ToolResult: outStr})
			seq++
			msgs = append(msgs, Msg{Role: "tool", Content: outStr, ToolCallID: tc.ID})
		}
	}
	emit(Event{Type: "done", TokensIn: totalIn, TokensOut: totalOut})
	if err := l.Store.TouchConversation(conv.ConversationID); err != nil {
		log.Printf("sentry: touch conversation %s failed: %v", conv.ConversationID, err)
	}
	return nil
}

// persist appends m to the Store, logging (never failing the turn) if the
// write fails -- the chat response has already succeeded from the
// operator's point of view by the time this is called.
func (l *Loop) persist(m models.SentryMessage) {
	if err := l.Store.Append(m); err != nil {
		log.Printf("sentry: persist message failed (conversation=%s seq=%d role=%s): %v",
			m.ConversationID, m.Seq, m.Role, err)
	}
}

// runTool dispatches a single model-requested tool call against the
// registry, returning a plain-text result (never an error) so the loop can
// always feed a tool message back to the model: an unrecognized tool name
// or a skill execution failure both surface as an "error: ..." string
// rather than aborting the turn.
func runTool(ctx context.Context, reg *skills.Registry, rc *skills.RunContext, tc ToolCall) string {
	sk, ok := reg.Get(tc.Name)
	if !ok {
		return "error: unknown tool " + tc.Name
	}
	out, err := sk.Execute(ctx, rc, json.RawMessage(tc.Args))
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}

// defsToTools converts a skills.Registry's tool definitions to the LLM
// package's ToolDef shape.
func defsToTools(defs []skills.Def) []ToolDef {
	if len(defs) == 0 {
		return nil
	}
	out := make([]ToolDef, len(defs))
	for i, d := range defs {
		out[i] = ToolDef{Name: d.Name, Description: d.Description, Params: d.Params}
	}
	return out
}

// marshalCalls serializes an assistant turn's requested tool calls for
// persistence in a SentryMessage's ToolArgs column. Empty/nil input yields
// "" (no column noise for a plain answer turn). Marshal of []ToolCall can't
// actually fail (no channels/funcs/cycles in the type), but the error is
// still handled defensively rather than ignored.
func marshalCalls(calls []ToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	b, err := json.Marshal(calls)
	if err != nil {
		log.Printf("sentry: marshal tool calls failed: %v", err)
		return ""
	}
	return string(b)
}

// unmarshalCalls is marshalCalls's inverse, used when assembleContext
// replays an assistant history row back into a Msg. "" or invalid JSON
// yields nil (a plain answer turn, or best-effort recovery from corrupt
// history) rather than an error -- history replay must never fail a new
// turn over an old row's shape.
func unmarshalCalls(s string) []ToolCall {
	if s == "" {
		return nil
	}
	var calls []ToolCall
	if err := json.Unmarshal([]byte(s), &calls); err != nil {
		return nil
	}
	return calls
}

// approxTokens is a cheap token estimate (~4 chars/token) used only for
// context-window budgeting during history trimming, not for billing (see
// ChatResult.TokensIn/TokensOut for the model-reported real counts).
func approxTokens(s string) int {
	return len(s) / 4
}

// assembleContext builds the message list sent to the LLM for one turn: a
// system prompt, a token-budget-trimmed slice of history, and the new user
// message. Pure function of its arguments -- no I/O, no clock -- so it is
// fully unit-testable (TestAssembleContextTrimsToolRowsFirst).
//
// History (oldest-first, as returned by Store.Messages) is walked
// newest-first so the most recent turns are prioritized. Once adding a row
// would exceed budget: a role=tool row is SKIPPED (dropped, but the walk
// continues to older rows), while a role=user/assistant row STOPS the walk
// entirely (dropping it and everything older). The net effect is that tool
// rows -- typically the bulkiest, least reusable content (raw SQL/tool
// results) -- are trimmed before user/assistant turns, and among
// user/assistant rows the oldest are dropped first.
//
// A role=tool row's ToolCallID is reconstructed from its ToolArgs column,
// which the loop persists as {"id":...,"name":...,"args":...} (see
// toolCallMeta) specifically so this replay can rebuild the pairing a bare
// SentryMessage row can't otherwise carry.
func assembleContext(system string, history []models.SentryMessage, userMsg string, budget int) []Msg {
	var kept []Msg
	used := 0

	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]

		if m.Role == "tool" {
			cost := approxTokens(m.ToolResult)
			if used+cost > budget {
				continue // drop this tool row only; keep walking older rows
			}
			used += cost
			var meta toolCallMeta
			_ = json.Unmarshal([]byte(m.ToolArgs), &meta)
			kept = append(kept, Msg{Role: "tool", Content: m.ToolResult, ToolCallID: meta.ID})
			continue
		}

		cost := approxTokens(m.Content)
		if used+cost > budget {
			break // budget exhausted on a user/assistant row: stop (drops it + everything older)
		}
		used += cost
		var calls []ToolCall
		if m.Role == "assistant" {
			calls = unmarshalCalls(m.ToolArgs)
		}
		kept = append(kept, Msg{Role: m.Role, Content: m.Content, ToolCalls: calls})
	}

	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}

	out := make([]Msg, 0, len(kept)+2)
	out = append(out, Msg{Role: "system", Content: system})
	out = append(out, kept...)
	out = append(out, Msg{Role: "user", Content: userMsg})
	return out
}
