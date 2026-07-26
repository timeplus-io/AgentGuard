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

// fakeWriteSkill is a GatedWrite skill that records whether it executed.
type fakeWriteSkill struct{ executed *bool }

func (fakeWriteSkill) Name() string               { return "test_write" }
func (fakeWriteSkill) Description() string        { return "test write" }
func (fakeWriteSkill) Params() map[string]any     { return map[string]any{"type": "object"} }
func (fakeWriteSkill) Access() skills.AccessClass { return skills.GatedWrite }
func (f fakeWriteSkill) Execute(_ context.Context, _ *skills.RunContext, _ json.RawMessage) (string, error) {
	*f.executed = true
	return "write done", nil
}

func TestApprovalRegistryDecide(t *testing.T) {
	r := NewApprovalRegistry()
	ch := r.register("p1", "alice")

	if err := r.Decide("p1", "bob", true, ""); !errors.Is(err, ErrApprovalForbidden) {
		t.Fatalf("wrong-user decide: err = %v, want ErrApprovalForbidden", err)
	}
	if err := r.Decide("nope", "alice", true, ""); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("unknown id: err = %v, want ErrApprovalNotFound", err)
	}
	if err := r.Decide("p1", "alice", true, "lgtm"); err != nil {
		t.Fatalf("owner decide: %v", err)
	}
	d := <-ch
	if !d.Approved || d.DecidedBy != "alice" || d.Reason != "lgtm" {
		t.Fatalf("decision = %+v", d)
	}
	// Single-decide: the entry is gone.
	if err := r.Decide("p1", "alice", false, ""); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("second decide: err = %v, want ErrApprovalNotFound", err)
	}
	if r.PendingCount() != 0 {
		t.Fatalf("pending = %d, want 0", r.PendingCount())
	}
}

// gatedLoop builds a Loop with one gated write skill and a scripted LLM that
// calls it once then answers.
func gatedLoop(executed *bool) (*Loop, *fakeLLM) {
	llm := &fakeLLM{script: []ChatResult{
		{ToolCalls: []ToolCall{{ID: "1", Name: "test_write", Args: `{"x":1}`}}},
		{Content: "final"},
	}}
	reg := skills.NewRegistry(fakeEchoSkill{}, fakeWriteSkill{executed: executed})
	return &Loop{LLM: llm, Reg: reg, Store: &Store{Disabled: true}, Cfg: testCfg(), Approvals: NewApprovalRegistry()}, llm
}

// decideOnRequest returns an emit wrapper that decides the proposal as soon
// as the approval_request event is seen — the same ordering a human hitting
// the button produces, minus the latency.
func decideOnRequest(l *Loop, user string, approve bool, reason string, events *[]Event) func(Event) {
	return func(e Event) {
		*events = append(*events, e)
		if e.Type == "approval_request" {
			if err := l.Approvals.Decide(e.ProposalID, user, approve, reason); err != nil {
				panic(err)
			}
		}
	}
}

func TestLoopGatedApprove(t *testing.T) {
	executed := false
	l, llm := gatedLoop(&executed)
	var events []Event
	err := l.Run(context.Background(), testChatCfg(), testConv(), "do it",
		decideOnRequest(l, "alice", true, "looks safe", &events))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"meta", "approval_request", "approval_result", "tool_result", "token", "done"}
	if got := eventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if !executed {
		t.Fatal("approved write must execute")
	}
	var res, ar Event
	for _, e := range events {
		if e.Type == "tool_result" {
			res = e
		}
		if e.Type == "approval_result" {
			ar = e
		}
	}
	if res.Result != "write done" {
		t.Fatalf("tool_result = %q, want write done", res.Result)
	}
	if ar.Decision != "approved" || ar.DecidedBy != "alice" || ar.Message != "looks safe" {
		t.Fatalf("approval_result = %+v", ar)
	}
	// Audit trail: the persisted tool row carries the approval meta.
	msgs, _ := l.Store.Messages("conv1")
	var meta toolCallMeta
	found := false
	for _, m := range msgs {
		if m.Role == "tool" {
			_ = json.Unmarshal([]byte(m.ToolArgs), &meta)
			found = true
		}
	}
	if !found || meta.Approval == nil || meta.Approval.Decision != "approved" || meta.Approval.DecidedBy != "alice" {
		t.Fatalf("persisted approval meta = %+v", meta.Approval)
	}
	// The model saw the execution result.
	second := llm.gotMsgs[1]
	if last := second[len(second)-1]; last.Content != "write done" {
		t.Fatalf("model saw %q, want write done", last.Content)
	}
}

func TestLoopGatedDeny(t *testing.T) {
	executed := false
	l, llm := gatedLoop(&executed)
	var events []Event
	err := l.Run(context.Background(), testChatCfg(), testConv(), "do it",
		decideOnRequest(l, "alice", false, "too risky", &events))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executed {
		t.Fatal("denied write must NOT execute")
	}
	second := llm.gotMsgs[1]
	last := second[len(second)-1]
	if !strings.Contains(last.Content, "denied by alice") || !strings.Contains(last.Content, "too risky") {
		t.Fatalf("model saw %q, want denial with reason", last.Content)
	}
	msgs, _ := l.Store.Messages("conv1")
	for _, m := range msgs {
		if m.Role == "tool" {
			var meta toolCallMeta
			_ = json.Unmarshal([]byte(m.ToolArgs), &meta)
			if meta.Approval == nil || meta.Approval.Decision != "denied" || meta.Approval.Reason != "too risky" {
				t.Fatalf("persisted approval meta = %+v", meta.Approval)
			}
		}
	}
}

func TestLoopGatedTimeout(t *testing.T) {
	executed := false
	l, _ := gatedLoop(&executed)
	cc := testChatCfg()
	cc.ApprovalTimeoutMs = 20 // fast auto-deny
	var events []Event
	err := l.Run(context.Background(), cc, testConv(), "do it", func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executed {
		t.Fatal("timed-out write must NOT execute")
	}
	var ar Event
	for _, e := range events {
		if e.Type == "approval_result" {
			ar = e
		}
	}
	if ar.Decision != "timeout" {
		t.Fatalf("decision = %q, want timeout", ar.Decision)
	}
	if l.Approvals.PendingCount() != 0 {
		t.Fatal("timed-out proposal must be removed from the registry")
	}
}

// TestLoopGatedHiddenWithoutRegistry: with no ApprovalRegistry the gated
// skill must not be offered to the model at all.
func TestLoopGatedHiddenWithoutRegistry(t *testing.T) {
	executed := false
	llm := &fakeLLM{script: []ChatResult{{Content: "final"}}}
	reg := skills.NewRegistry(fakeEchoSkill{}, fakeWriteSkill{executed: &executed})
	l := &Loop{LLM: llm, Reg: reg, Store: &Store{Disabled: true}, Cfg: testCfg()}
	if err := l.Run(context.Background(), testChatCfg(), testConv(), "hi", func(Event) {}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, td := range llm.gotTools[0] {
		if td.Name == "test_write" {
			t.Fatal("gated skill offered to the model without an approval registry")
		}
	}
}
