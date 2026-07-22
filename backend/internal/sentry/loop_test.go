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

// TestAssembleContextTrimsToolRowsFirst builds a small history where the
// newest row is a large tool row that alone blows the budget, and an older
// row is small user/assistant content that fits. assembleContext walks
// newest-first: it must SKIP (not stop on) the oversized tool row and keep
// walking to older rows, while still dropping oldest-first among
// user/assistant rows once the budget is genuinely exhausted. This proves
// tool rows are trimmed before user/assistant rows, not just "whatever
// doesn't fit".
func TestAssembleContextTrimsToolRowsFirst(t *testing.T) {
	history := []models.SentryMessage{
		{Seq: 1, Role: "user", Content: "old-user-msg"}, // ~3 tokens
		{Seq: 2, Role: "tool", ToolName: "run_sql",
			ToolArgs:   `{"id":"tc-old","name":"run_sql","args":"{}"}`,
			ToolResult: strings.Repeat("x", 400)}, // ~100 tokens: too big for budget
		{Seq: 3, Role: "assistant", Content: "recent-final-msg"}, // ~4 tokens
	}
	const budget = 10 // fits both small rows (~7 tokens) but not the huge tool row alone

	got := assembleContext("SYS", history, "new-user-msg", budget)

	var roles []string
	var contents []string
	for _, m := range got {
		roles = append(roles, m.Role)
		contents = append(contents, m.Content)
	}
	t.Logf("assembled: roles=%v contents=%v", roles, contents)

	for _, c := range contents {
		if strings.Contains(c, "xxxxxxxx") {
			t.Fatalf("expected the oversized tool row dropped, but found its content: %v", contents)
		}
	}
	if !containsMsg(got, "user", "old-user-msg") {
		t.Errorf("expected old-user-msg kept (walk continues past the skipped tool row), got %v", contents)
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

// TestAssembleContextReconstructsToolCallID verifies the tool-pair replay
// scheme: a persisted role=tool row's ToolArgs carries
// {"id":...,"name":...,"args":...} so a later turn's assembleContext can
// rebuild Msg.ToolCallID from history alone (SentryMessage has no
// tool_call_id column of its own).
func TestAssembleContextReconstructsToolCallID(t *testing.T) {
	history := []models.SentryMessage{
		{Seq: 1, Role: "tool", ToolName: "echo",
			ToolArgs:   `{"id":"call-42","name":"echo","args":"{}"}`,
			ToolResult: "pong"},
	}
	got := assembleContext("SYS", history, "hi", 100000)

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

func containsMsg(msgs []Msg, role, content string) bool {
	for _, m := range msgs {
		if m.Role == role && m.Content == content {
			return true
		}
	}
	return false
}
