package sentry

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/sentry/skills"
)

func TestClarificationRegistryAnswer(t *testing.T) {
	r := NewClarificationRegistry()
	ch := r.register("c1", "alice")

	if err := r.Answer("c1", "bob", nil, true); !errors.Is(err, ErrClarificationForbidden) {
		t.Fatalf("wrong-user answer: err = %v, want ErrClarificationForbidden", err)
	}
	if err := r.Answer("nope", "alice", nil, true); !errors.Is(err, ErrClarificationNotFound) {
		t.Fatalf("unknown id: err = %v, want ErrClarificationNotFound", err)
	}
	if err := r.Answer("c1", "alice", json.RawMessage(`[{"selected":["Bash"]}]`), false); err != nil {
		t.Fatalf("owner answer: %v", err)
	}
	a := <-ch
	if a.Skipped || a.AnsweredBy != "alice" || string(a.Answers) != `[{"selected":["Bash"]}]` {
		t.Fatalf("answer = %+v", a)
	}
	// Single-answer: the entry is gone.
	if err := r.Answer("c1", "alice", nil, true); !errors.Is(err, ErrClarificationNotFound) {
		t.Fatalf("second answer: err = %v, want ErrClarificationNotFound", err)
	}
	if r.PendingCount() != 0 {
		t.Fatalf("pending = %d, want 0", r.PendingCount())
	}
}

func TestClarificationRegistrySkip(t *testing.T) {
	r := NewClarificationRegistry()
	ch := r.register("c2", "alice")
	if err := r.Answer("c2", "alice", nil, true); err != nil {
		t.Fatalf("skip: %v", err)
	}
	a := <-ch
	if !a.Skipped || a.AnsweredBy != "alice" || a.Answers != nil {
		t.Fatalf("skip answer = %+v", a)
	}
}

const askArgs = `{"questions":[{"header":"Tools","question":"Which tools are unauthorized?","multiSelect":true,"options":[{"label":"Bash (Recommended)","description":"shell execution"},{"label":"Write","description":"filesystem writes"}]}]}`

// askLoop builds a Loop with the real ask_user skill and a scripted LLM
// that calls it once then answers.
func askLoop() (*Loop, *fakeLLM) {
	llm := &fakeLLM{script: []ChatResult{
		{ToolCalls: []ToolCall{{ID: "1", Name: "ask_user", Args: askArgs}}},
		{Content: "final"},
	}}
	reg := skills.NewRegistry(fakeEchoSkill{}, skills.NewAskUser())
	return &Loop{LLM: llm, Reg: reg, Store: &Store{Disabled: true}, Cfg: testCfg(),
		Approvals: NewApprovalRegistry(), Clarifications: NewClarificationRegistry()}, llm
}

// answerOnRequest answers the clarification as soon as the
// clarification_request event is seen — the same ordering the UI produces.
func answerOnRequest(l *Loop, user, answers string, skipped bool, events *[]Event) func(Event) {
	return func(e Event) {
		*events = append(*events, e)
		if e.Type == "clarification_request" {
			var raw json.RawMessage
			if answers != "" {
				raw = json.RawMessage(answers)
			}
			if err := l.Clarifications.Answer(e.ClarificationID, user, raw, skipped); err != nil {
				panic(err)
			}
		}
	}
}

func TestLoopClarificationAnswered(t *testing.T) {
	l, llm := askLoop()
	var events []Event
	err := l.Run(context.Background(), testChatCfg(), testConv(), "check unauthorized tools",
		answerOnRequest(l, "alice", `[{"selected":["Bash (Recommended)","Write"],"other":"also Curl"}]`, false, &events))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"meta", "clarification_request", "clarification_result", "tool_result", "token", "done"}
	if got := eventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	var req, res, tr Event
	for _, e := range events {
		switch e.Type {
		case "clarification_request":
			req = e
		case "clarification_result":
			res = e
		case "tool_result":
			tr = e
		}
	}
	if req.Args != askArgs || req.ClarificationID == "" || req.TimeoutMs != testChatCfg().ApprovalTimeoutMs {
		t.Fatalf("clarification_request = %+v", req)
	}
	if res.Outcome != "answered" || res.AnsweredBy != "alice" || !strings.Contains(res.Answers, "Bash (Recommended)") {
		t.Fatalf("clarification_result = %+v", res)
	}
	// The model saw the paired question+answers, including the free text.
	for _, want := range []string{"Which tools are unauthorized?", "Bash (Recommended)", "also Curl"} {
		if !strings.Contains(tr.Result, want) {
			t.Fatalf("tool_result %q missing %q", tr.Result, want)
		}
	}
	second := llm.gotMsgs[1]
	if last := second[len(second)-1]; !strings.Contains(last.Content, "Bash (Recommended)") {
		t.Fatalf("model saw %q, want the answers", last.Content)
	}
	// Audit trail: the persisted tool row carries the clarification meta.
	msgs, _ := l.Store.Messages("conv1")
	found := false
	for _, m := range msgs {
		if m.Role == "tool" {
			var meta toolCallMeta
			_ = json.Unmarshal([]byte(m.ToolArgs), &meta)
			if meta.Clarification == nil || meta.Clarification.Outcome != "answered" ||
				meta.Clarification.AnsweredBy != "alice" || meta.Clarification.ClarificationID == "" {
				t.Fatalf("persisted clarification meta = %+v", meta.Clarification)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no tool row persisted")
	}
}

func TestLoopClarificationSkipped(t *testing.T) {
	l, llm := askLoop()
	var events []Event
	err := l.Run(context.Background(), testChatCfg(), testConv(), "check",
		answerOnRequest(l, "alice", "", true, &events))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var res Event
	for _, e := range events {
		if e.Type == "clarification_result" {
			res = e
		}
	}
	if res.Outcome != "skipped" || res.AnsweredBy != "alice" {
		t.Fatalf("clarification_result = %+v", res)
	}
	second := llm.gotMsgs[1]
	last := second[len(second)-1]
	if !strings.Contains(last.Content, "recommended defaults") {
		t.Fatalf("model saw %q, want proceed-with-defaults instruction", last.Content)
	}
}

func TestLoopClarificationTimeout(t *testing.T) {
	l, _ := askLoop()
	cc := testChatCfg()
	cc.ApprovalTimeoutMs = 20 // fast timeout
	var events []Event
	err := l.Run(context.Background(), cc, testConv(), "check", func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var res, tr Event
	for _, e := range events {
		if e.Type == "clarification_result" {
			res = e
		}
		if e.Type == "tool_result" {
			tr = e
		}
	}
	if res.Outcome != "timeout" {
		t.Fatalf("outcome = %q, want timeout", res.Outcome)
	}
	if !strings.Contains(tr.Result, "recommended defaults") {
		t.Fatalf("tool_result %q, want proceed-with-defaults instruction", tr.Result)
	}
	if l.Clarifications.PendingCount() != 0 {
		t.Fatal("timed-out clarification must be removed from the registry")
	}
}

// TestLoopClarificationInvalidArgs: malformed questions go straight back to
// the model as an error tool result — no clarification_request event, no UI
// involvement.
func TestLoopClarificationInvalidArgs(t *testing.T) {
	l, llm := askLoop()
	llm.script[0] = ChatResult{ToolCalls: []ToolCall{{ID: "1", Name: "ask_user", Args: `{"questions":[]}`}}}
	var events []Event
	err := l.Run(context.Background(), testChatCfg(), testConv(), "check", func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"meta", "tool_result", "token", "done"}
	if got := eventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v (no clarification_request for invalid args)", got, want)
	}
	if !strings.HasPrefix(events[1].Result, "error:") {
		t.Fatalf("tool_result = %q, want error:", events[1].Result)
	}
}

// TestLoopAskUserHiddenWithoutClarifications: with no ClarificationRegistry
// the Interactive skill must not be offered to the model at all.
func TestLoopAskUserHiddenWithoutClarifications(t *testing.T) {
	llm := &fakeLLM{script: []ChatResult{{Content: "final"}}}
	reg := skills.NewRegistry(fakeEchoSkill{}, skills.NewAskUser())
	l := &Loop{LLM: llm, Reg: reg, Store: &Store{Disabled: true}, Cfg: testCfg(), Approvals: NewApprovalRegistry()}
	if err := l.Run(context.Background(), testChatCfg(), testConv(), "hi", func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, td := range llm.gotTools[0] {
		if td.Name == "ask_user" {
			t.Fatal("ask_user offered to the model without a clarification registry")
		}
	}
}

// TestLoopAskUserOfferedWithRegistries is the positive twin.
func TestLoopAskUserOfferedWithRegistries(t *testing.T) {
	l, llm := askLoop()
	llm.script = []ChatResult{{Content: "final"}}
	if err := l.Run(context.Background(), testChatCfg(), testConv(), "hi", func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, td := range llm.gotTools[0] {
		if td.Name == "ask_user" {
			found = true
		}
	}
	if !found {
		t.Fatal("ask_user not offered despite both registries present")
	}
}
