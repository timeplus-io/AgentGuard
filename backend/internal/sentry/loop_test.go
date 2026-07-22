package sentry

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/sentry/skills"
)

// fakeLLM scripts a sequence of ChatResults, one per call, and records the
// msgs/tools it was called with so tests can assert context assembly and
// the tools-omitted-on-final-iteration behavior.
type fakeLLM struct {
	script   []ChatResult
	calls    int
	gotMsgs  [][]Msg
	gotTools [][]ToolDef
}

func (f *fakeLLM) ChatStream(_ context.Context, _ ChatCfg, msgs []Msg, tools []ToolDef, onToken func(string)) (ChatResult, error) {
	f.gotMsgs = append(f.gotMsgs, msgs)
	f.gotTools = append(f.gotTools, tools)
	r := f.script[f.calls]
	f.calls++
	if r.Content != "" && onToken != nil {
		onToken(r.Content)
	}
	return r, nil
}

// fakeEchoSkill is a minimal read-only skill used by loop tests: it always
// returns "pong" regardless of args.
type fakeEchoSkill struct{}

func (fakeEchoSkill) Name() string               { return "echo" }
func (fakeEchoSkill) Description() string        { return "echo" }
func (fakeEchoSkill) Params() map[string]any     { return map[string]any{"type": "object"} }
func (fakeEchoSkill) Access() skills.AccessClass { return skills.ReadOnly }
func (fakeEchoSkill) Execute(_ context.Context, _ *skills.RunContext, _ json.RawMessage) (string, error) {
	return "pong", nil
}

func testConv() models.SentryConversation {
	return models.SentryConversation{ConversationID: "conv1", User: "alice", Status: "active"}
}

// testCfg returns a zero-value (non-nil) *config.Config: its Timeplus
// sub-config points at host="" port=0, so any live query
// (skills.SchemaCard's system.tables lookup) fails fast and SchemaCard falls
// back to its static card — which is exactly what TestLoopSystemPromptCard
// needs (a real stream name with no live Timeplus in the test process).
func testCfg() *config.Config {
	return &config.Config{}
}

// testChatCfg returns a ChatCfg with enough headroom (iterations, token
// budget, result size) that none of the loop tests hit an unrelated limit;
// individual tests override MaxIterations/SystemPrompt as needed.
func testChatCfg() ChatCfg {
	return ChatCfg{
		MaxIterations:      5,
		TurnTokenBudget:    32000,
		ToolResultMaxBytes: 16384,
		SystemPrompt:       "You are Sentry. {schema_card}",
	}
}

func eventTypes(events []Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Type
	}
	return out
}

func TestLoopToolRoundTrip(t *testing.T) {
	llm := &fakeLLM{script: []ChatResult{
		{ToolCalls: []ToolCall{{ID: "1", Name: "echo", Args: "{}"}}},
		{Content: "final"},
	}}
	reg := skills.NewRegistry(fakeEchoSkill{})
	store := &Store{Disabled: true}
	l := &Loop{LLM: llm, Reg: reg, Store: store, Cfg: testCfg()}

	var events []Event
	err := l.Run(context.Background(), testChatCfg(), testConv(), "hi", func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got, want := eventTypes(events), []string{"meta", "tool_call", "tool_result", "token", "done"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}

	if len(llm.gotMsgs) != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(llm.gotMsgs))
	}
	second := llm.gotMsgs[1]
	last := second[len(second)-1]
	if last.Role != "tool" || last.Content != "pong" || last.ToolCallID != "1" {
		t.Fatalf("last msg of 2nd call = %+v, want Role=tool Content=pong ToolCallID=1", last)
	}
}

func TestLoopIterationBudget(t *testing.T) {
	llm := &fakeLLM{script: []ChatResult{
		{ToolCalls: []ToolCall{{ID: "1", Name: "echo", Args: "{}"}}},
		{ToolCalls: []ToolCall{{ID: "2", Name: "echo", Args: "{}"}}},
		{Content: "forced final"},
	}}
	reg := skills.NewRegistry(fakeEchoSkill{})
	store := &Store{Disabled: true}
	l := &Loop{LLM: llm, Reg: reg, Store: store, Cfg: testCfg()}

	cc := testChatCfg()
	cc.MaxIterations = 3

	var events []Event
	err := l.Run(context.Background(), cc, testConv(), "hi", func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(llm.gotTools) != 3 {
		t.Fatalf("expected 3 LLM calls, got %d", len(llm.gotTools))
	}
	if len(llm.gotTools[0]) == 0 || len(llm.gotTools[1]) == 0 {
		t.Fatalf("expected tools present on first two calls, got %v %v", llm.gotTools[0], llm.gotTools[1])
	}
	if len(llm.gotTools[2]) != 0 {
		t.Fatalf("expected tools omitted on final (budget-exhausted) call, got %d tools", len(llm.gotTools[2]))
	}

	last := events[len(events)-1]
	if last.Type != "done" {
		t.Fatalf("last event = %s, want done", last.Type)
	}
}

func TestLoopUnknownTool(t *testing.T) {
	llm := &fakeLLM{script: []ChatResult{
		{ToolCalls: []ToolCall{{ID: "1", Name: "nonexistent", Args: "{}"}}},
		{Content: "final answer"},
	}}
	reg := skills.NewRegistry(fakeEchoSkill{})
	store := &Store{Disabled: true}
	l := &Loop{LLM: llm, Reg: reg, Store: store, Cfg: testCfg()}

	var events []Event
	err := l.Run(context.Background(), testChatCfg(), testConv(), "hi", func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var toolResult *Event
	for i := range events {
		if events[i].Type == "tool_result" {
			toolResult = &events[i]
			break
		}
	}
	if toolResult == nil {
		t.Fatal("no tool_result event emitted")
	}
	if got := toolResult.Result; got != "error: unknown tool nonexistent" {
		t.Fatalf("tool_result = %q, want to contain %q", got, "unknown tool")
	}

	last := events[len(events)-1]
	if last.Type != "done" {
		t.Fatalf("loop did not continue to scripted final answer, last event = %s", last.Type)
	}
}

func TestLoopSystemPromptCard(t *testing.T) {
	llm := &fakeLLM{script: []ChatResult{{Content: "ok"}}}
	reg := skills.NewRegistry(fakeEchoSkill{})
	store := &Store{Disabled: true}
	l := &Loop{LLM: llm, Reg: reg, Store: store, Cfg: testCfg()}

	cc := testChatCfg()
	cc.SystemPrompt = "X {schema_card} Y"

	err := l.Run(context.Background(), cc, testConv(), "hi", func(Event) {})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(llm.gotMsgs) == 0 || len(llm.gotMsgs[0]) == 0 {
		t.Fatal("no messages recorded on first call")
	}
	first := llm.gotMsgs[0][0]
	if first.Role != "system" {
		t.Fatalf("msgs[0].Role = %s, want system", first.Role)
	}
	if !strings.Contains(first.Content, "agentguard_cim_event") {
		t.Fatalf("system prompt = %q, want it to contain a real stream name (agentguard_cim_event)", first.Content)
	}
}

// assertToolPairingInvariant enforces the OpenAI chat-completions
// requirement that assembleContext's output must never violate: (a) every
// assistant Msg with non-empty ToolCalls has EACH of its ToolCall IDs
// answered by a later role=tool Msg with a matching ToolCallID, and (b)
// every role=tool Msg's ToolCallID matches a ToolCall on an assistant Msg
// that appears earlier in the slice. Violating either direction is exactly
// the bug this fix closes: an orphaned tool_call (no answer) or an orphaned
// tool answer (no parent) both make OpenAI 400 the whole conversation.
func assertToolPairingInvariant(t *testing.T, msgs []Msg) {
	t.Helper()

	// answered[i] tracks, for assistant msgs[i], which of its ToolCall IDs
	// have been matched by a later tool msg.
	answered := make([]map[string]bool, len(msgs))
	for i, m := range msgs {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			answered[i] = map[string]bool{}
		}
	}

	for i, m := range msgs {
		if m.Role != "tool" {
			continue
		}
		found := false
		for j := 0; j < i; j++ {
			if msgs[j].Role != "assistant" {
				continue
			}
			for _, tc := range msgs[j].ToolCalls {
				if tc.ID == m.ToolCallID {
					answered[j][tc.ID] = true
					found = true
				}
			}
		}
		if !found {
			t.Errorf("msgs[%d] role=tool ToolCallID=%q has no preceding assistant ToolCall with that id", i, m.ToolCallID)
		}
	}

	for i, m := range msgs {
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			if !answered[i][tc.ID] {
				t.Errorf("msgs[%d] role=assistant ToolCall id=%q has no following role=tool answer", i, tc.ID)
			}
		}
	}
}

// TestAssembleContextTrimsToolRowsFirst builds a history containing an
// assistant-with-tool_call row immediately followed by its (bulky) tool
// answer, bracketed by an older and a newer plain turn. The budget is sized
// so the tool_call/answer pair alone blows it. The pre-fix, per-row
// algorithm evaluated the assistant row (tiny — no Content) and the tool row
// (huge — the ToolResult) independently: the assistant row fit and was kept,
// the tool row didn't and was skipped, producing an assistant ToolCall with
// no answering tool message (an OpenAI-400-inducing sequence). The fix must
// treat the assistant+tool-answer(s) as one atomic unit: keep the whole
// pair, or drop the whole pair.
func TestAssembleContextTrimsToolRowsFirst(t *testing.T) {
	history := []models.SentryMessage{
		{Seq: 1, Role: "user", Content: "old-user-msg"}, // ~3 tokens
		{Seq: 2, Role: "assistant", Content: "",
			ToolArgs: marshalCalls([]ToolCall{{ID: "tc-old", Name: "run_sql", Args: "{}"}})}, // ~0 tokens alone
		{Seq: 3, Role: "tool", ToolName: "run_sql",
			ToolArgs:   `{"id":"tc-old","name":"run_sql","args":"{}"}`,
			ToolResult: strings.Repeat("x", 400)}, // ~100 tokens: too big for budget
		{Seq: 4, Role: "assistant", Content: "recent-final-msg"}, // ~4 tokens
	}
	const budget = 10 // fits both small plain rows (~7 tokens) but not the tool_call/answer pair (~100)

	got := assembleContext("SYS", history, "new-user-msg", budget)

	var roles []string
	var contents []string
	for _, m := range got {
		roles = append(roles, m.Role)
		contents = append(contents, m.Content)
	}
	t.Logf("assembled: roles=%v contents=%v", roles, contents)

	assertToolPairingInvariant(t, got)

	for _, m := range got {
		if strings.Contains(m.Content, "xxxxxxxx") {
			t.Fatalf("expected the oversized tool answer dropped, but found its content: %v", contents)
		}
		if len(m.ToolCalls) > 0 {
			t.Fatalf("expected the orphaned assistant tool_call (tc-old) dropped along with its answer, but it was kept: %+v", m)
		}
	}
	if !containsMsg(got, "user", "old-user-msg") {
		t.Errorf("expected old-user-msg kept (walk continues past the skipped tool_call/answer pair), got %v", contents)
	}
	if !containsMsg(got, "assistant", "recent-final-msg") {
		t.Errorf("expected recent-final-msg kept, got %v", contents)
	}
	if got[0].Role != "system" || got[0].Content != "SYS" {
		t.Errorf("got[0] = %+v, want system/SYS prepended", got[0])
	}
	last := got[len(got)-1]
	if last.Role != "user" || last.Content != "new-user-msg" {
		t.Errorf("last msg = %+v, want the new user msg appended", last)
	}
	// Chronological order preserved among kept rows: old-user-msg before
	// recent-final-msg.
	oldIdx, newIdx := -1, -1
	for i, m := range got {
		if m.Content == "old-user-msg" {
			oldIdx = i
		}
		if m.Content == "recent-final-msg" {
			newIdx = i
		}
	}
	if oldIdx == -1 || newIdx == -1 || oldIdx >= newIdx {
		t.Errorf("expected old-user-msg (idx %d) before recent-final-msg (idx %d)", oldIdx, newIdx)
	}
}

// TestAssembleContextRetainsNewestTurn verifies that when trimming must
// happen, the newest complete turn — including an assistant tool_call and
// its answer, kept as one atomic pair — always survives, and that
// assembleContext always prepends the system prompt and appends the fresh
// user message.
func TestAssembleContextRetainsNewestTurn(t *testing.T) {
	history := []models.SentryMessage{
		{Seq: 1, Role: "user", Content: strings.Repeat("a", 4000)}, // ~1000 tokens: must not fit
		{Seq: 2, Role: "user", Content: "recent question"},         // ~4 tokens
		{Seq: 3, Role: "assistant", Content: "",
			ToolArgs: marshalCalls([]ToolCall{{ID: "tc-new", Name: "run_sql", Args: "{}"}})}, // ~0 tokens
		{Seq: 4, Role: "tool", ToolName: "run_sql",
			ToolArgs:   `{"id":"tc-new","name":"run_sql","args":"{}"}`,
			ToolResult: "small result"}, // ~3 tokens
	}
	const budget = 20 // fits the newest turn (question + tool_call/answer pair) but not the old huge user row

	got := assembleContext("SYS", history, "new-user-msg", budget)

	assertToolPairingInvariant(t, got)

	if got[0].Role != "system" || got[0].Content != "SYS" {
		t.Fatalf("got[0] = %+v, want system/SYS", got[0])
	}
	last := got[len(got)-1]
	if last.Role != "user" || last.Content != "new-user-msg" {
		t.Fatalf("last msg = %+v, want the fresh user msg appended", last)
	}
	if !containsMsg(got, "user", "recent question") {
		t.Errorf("expected the newest turn's user msg retained, got %+v", got)
	}
	foundAnswer := false
	for _, m := range got {
		if m.Role == "tool" && m.ToolCallID == "tc-new" {
			foundAnswer = true
		}
		if strings.Contains(m.Content, "aaaa") {
			t.Errorf("expected the old huge user row dropped, but found it: %+v", got)
		}
	}
	if !foundAnswer {
		t.Errorf("expected the newest tool_call/answer pair retained, got %+v", got)
	}
}

// TestAssembleContextReconstructsToolCallID verifies the tool-pair replay
// scheme: a persisted role=tool row's ToolArgs carries
// {"id":...,"name":...,"args":...} so a later turn's assembleContext can
// rebuild Msg.ToolCallID from history alone (SentryMessage has no
// tool_call_id column of its own). The history includes the preceding
// assistant tool_call row too, since a lone tool row with no parent is an
// invalid (and now deliberately dropped) sequence.
func TestAssembleContextReconstructsToolCallID(t *testing.T) {
	history := []models.SentryMessage{
		{Seq: 1, Role: "assistant", Content: "",
			ToolArgs: marshalCalls([]ToolCall{{ID: "call-42", Name: "echo", Args: "{}"}})},
		{Seq: 2, Role: "tool", ToolName: "echo",
			ToolArgs:   `{"id":"call-42","name":"echo","args":"{}"}`,
			ToolResult: "pong"},
	}
	got := assembleContext("SYS", history, "hi", 100000)

	assertToolPairingInvariant(t, got)

	var toolMsg *Msg
	for i := range got {
		if got[i].Role == "tool" {
			toolMsg = &got[i]
		}
	}
	if toolMsg == nil {
		t.Fatal("expected a tool message in assembled context")
	}
	if toolMsg.ToolCallID != "call-42" || toolMsg.Content != "pong" {
		t.Fatalf("tool msg = %+v, want ToolCallID=call-42 Content=pong", toolMsg)
	}
}

// TestAssembleContextDropsOrphanedToolRow defends the new orphan-safety
// behavior: a role=tool row with no preceding assistant tool_call in history
// (e.g. a truncated/corrupt store) can never satisfy the pairing invariant on
// its own, so it must be dropped rather than ever surfacing as a lone tool
// message.
func TestAssembleContextDropsOrphanedToolRow(t *testing.T) {
	history := []models.SentryMessage{
		{Seq: 1, Role: "tool", ToolName: "echo",
			ToolArgs:   `{"id":"call-orphan","name":"echo","args":"{}"}`,
			ToolResult: "pong"},
	}
	got := assembleContext("SYS", history, "hi", 100000)

	assertToolPairingInvariant(t, got)
	for _, m := range got {
		if m.Role == "tool" {
			t.Errorf("expected the orphaned tool row dropped, got %+v", got)
		}
	}
}

func containsMsg(msgs []Msg, role, content string) bool {
	for _, m := range msgs {
		if m.Role == role && m.Content == content {
			return true
		}
	}
	return false
}
