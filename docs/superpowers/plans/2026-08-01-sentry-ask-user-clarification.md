# Sentry `ask_user` Clarification Tool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Sentry an `ask_user` tool: when the LLM judges the user's request ambiguous, it asks 1–4 structured questions; the loop blocks, the `/sentry` UI collects answers (option chips + "Other" free text + Skip), and the answers come back as the tool result.

**Architecture:** Mirrors the proven GatedWrite approval flow (#221): a new `ClarificationRegistry` (register-before-emit, single-winner, owner-only), a new `Interactive` skills access class intercepted by the loop, `clarification_request`/`clarification_result` NDJSON events, `POST /api/sentry/clarifications/:clarification_id/answer`, and Q&A persisted in the tool row's `toolCallMeta` for replay. Timeout reuses `cc.ApprovalTimeoutMs` and resolves as "proceed with your recommended defaults".

**Tech Stack:** Go (Gin), React (Vite, Tailwind, react-i18next), no new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-01-sentry-ask-user-clarification-design.md`

## Global Constraints

- All Go builds/tests: `CGO_ENABLED=0` (macOS 26 dyld workaround).
- Branch: work on `feat/issue-264-sentry-ask-user` (already created).
- `ask_user` is registered **unconditionally** (available regardless of `agentguard_tools_enabled`), but only **offered** to the model in interactive chat (both registries non-nil).
- No new config knob: clarification timeout = `cc.ApprovalTimeoutMs`.
- Timeout/skip semantics: the tool result tells the model to proceed with its recommended defaults and state assumptions. There is no auto-answer path.
- Frontend design system: no borders for sectioning, no drop shadows, radius max 4px except chips (`rounded-full`), gradient CTA `bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023]`.
- LLM-generated question/option text is rendered verbatim, never translated; UI chrome strings go in `en` + `zh-CN` sentry locale files.
- Every commit message ends with:
  ```
  Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01MuJKFZSUHHbbC2WCMGoVrB
  ```

---

### Task 1: `Interactive` access class + `ask_user` skill (skills package)

**Files:**
- Modify: `backend/internal/sentry/skills/skill.go` (AccessClass const block only)
- Create: `backend/internal/sentry/skills/askuser.go`
- Create: `backend/internal/sentry/skills/askuser_test.go`

**Interfaces:**
- Consumes: existing `Skill` interface, `AccessClass`, `Registry` from `skill.go`.
- Produces (used by Tasks 3–4):
  - `const Interactive AccessClass` (value 3, above `GatedWrite`)
  - `func NewAskUser() Skill` — Name() `"ask_user"`, Access() `Interactive`
  - `type AskUserOption struct { Label, Description string }` (JSON tags `label`, `description`)
  - `type AskUserQuestion struct { Header, Question string; MultiSelect bool; Options []AskUserOption }` (JSON tags `header`, `question`, `multiSelect`, `options`)
  - `func ParseAskUserQuestions(args json.RawMessage) ([]AskUserQuestion, error)` — validates 1–4 questions, non-empty question text, 2–4 options each

- [ ] **Step 1: Write the failing test**

Create `backend/internal/sentry/skills/askuser_test.go`:

```go
package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const validAskArgs = `{"questions":[{"header":"Tools","question":"Which tools are unauthorized?","multiSelect":true,"options":[{"label":"Bash (Recommended)","description":"shell execution"},{"label":"Write","description":"filesystem writes"}]}]}`

// TestAskUserDefGating: ask_user is Interactive-only — never offered at
// GatedWrite or below (headless / no clarification registry), offered at
// Interactive.
func TestAskUserDefGating(t *testing.T) {
	r := NewRegistry(NewAskUser())
	if got := len(r.Defs(GatedWrite)); got != 0 {
		t.Fatalf("Defs(GatedWrite) = %d defs, want 0 (ask_user must not leak below Interactive)", got)
	}
	defs := r.Defs(Interactive)
	if len(defs) != 1 || defs[0].Name != "ask_user" {
		t.Fatalf("Defs(Interactive) = %+v, want exactly [ask_user]", defs)
	}
}

// TestAskUserExecuteIsStub: the loop intercepts Interactive calls; a direct
// Execute (defensive path) must fail loudly, never pretend to have asked.
func TestAskUserExecuteIsStub(t *testing.T) {
	_, err := NewAskUser().Execute(context.Background(), &RunContext{}, json.RawMessage(validAskArgs))
	if err == nil {
		t.Fatal("Execute must return an error — ask_user is handled by the chat loop")
	}
}

func TestParseAskUserQuestions(t *testing.T) {
	qs, err := ParseAskUserQuestions(json.RawMessage(validAskArgs))
	if err != nil {
		t.Fatalf("valid args: %v", err)
	}
	if len(qs) != 1 || qs[0].Header != "Tools" || !qs[0].MultiSelect || len(qs[0].Options) != 2 {
		t.Fatalf("parsed = %+v", qs)
	}
	if qs[0].Options[0].Label != "Bash (Recommended)" {
		t.Fatalf("option label = %q", qs[0].Options[0].Label)
	}

	bad := map[string]string{
		"not json":        `{`,
		"no questions":    `{"questions":[]}`,
		"five questions":  `{"questions":[{"question":"a","options":[{"label":"x"},{"label":"y"}]},{"question":"b","options":[{"label":"x"},{"label":"y"}]},{"question":"c","options":[{"label":"x"},{"label":"y"}]},{"question":"d","options":[{"label":"x"},{"label":"y"}]},{"question":"e","options":[{"label":"x"},{"label":"y"}]}]}`,
		"empty question":  `{"questions":[{"question":"  ","options":[{"label":"x"},{"label":"y"}]}]}`,
		"one option":      `{"questions":[{"question":"a","options":[{"label":"x"}]}]}`,
		"five options":    `{"questions":[{"question":"a","options":[{"label":"1"},{"label":"2"},{"label":"3"},{"label":"4"},{"label":"5"}]}]}`,
	}
	for name, args := range bad {
		if _, err := ParseAskUserQuestions(json.RawMessage(args)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

// TestAskUserDescriptionMentionsWhen: the description is the model's only
// standing guidance in generic-analyst mode (no agentguard rules block), so
// it must carry the "only when the answer changes the investigation" rule.
func TestAskUserDescriptionMentionsWhen(t *testing.T) {
	d := NewAskUser().Description()
	for _, want := range []string{"ambiguous", "Recommended"} {
		if !strings.Contains(d, want) {
			t.Errorf("description missing %q: %q", want, d)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/sentry/skills/ -run 'TestAskUser|TestParseAskUser' -v
```
Expected: FAIL to compile — `undefined: NewAskUser`, `undefined: Interactive`, `undefined: ParseAskUserQuestions`.

- [ ] **Step 3: Add `Interactive` to skill.go and write askuser.go**

In `backend/internal/sentry/skills/skill.go`, extend the const block:

```go
const (
	ReadOnly    AccessClass = iota
	MemoryWrite             // chat-only, no approval (Phase 2)
	GatedWrite              // chat-only, approval required (Phase 4)
	Interactive             // chat-only, blocks on a human answer mid-turn (#264)
)
```

Create `backend/internal/sentry/skills/askuser.go`:

```go
package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// askUser is the Interactive clarification tool (#264): the model calls it
// when the user's request is ambiguous or hinges on a deployment-specific
// definition, and the chat loop — not Execute — blocks the turn on a human
// answer collected by the /sentry UI. Registered unconditionally (it is
// useful to the generic SQL-analyst mode too); only offered to the model in
// interactive chat (see Loop maxAccess).
type askUser struct{}

// NewAskUser returns the ask_user Interactive skill.
func NewAskUser() Skill { return askUser{} }

func (askUser) Name() string { return "ask_user" }
func (askUser) Description() string {
	return "Ask the operator up to 4 clarifying questions when their request is ambiguous or hinges on a deployment-specific definition (which tools count as unauthorized, which data is sensitive, scope, time window, target agents/sessions). The UI renders each question's options as choice chips plus a free-text \"Other\" field and returns the operator's answers as the tool result. Put your recommended option first with \" (Recommended)\" appended to its label. Ask only when the answer changes what you do next — proceed directly on specific requests, and never ask when a conventional default exists."
}
func (askUser) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"questions": map[string]any{
				"type": "array", "minItems": 1, "maxItems": 4,
				"description": "1-4 questions to ask the operator",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"header":      map[string]any{"type": "string", "description": "short chip label, at most 12 characters (e.g. \"Tools\", \"Time window\")"},
						"question":    map[string]any{"type": "string", "description": "the full question text, in the conversation language"},
						"multiSelect": map[string]any{"type": "boolean", "description": "true when several options may apply"},
						"options": map[string]any{
							"type": "array", "minItems": 2, "maxItems": 4,
							"description": "2-4 options; recommended option first with \" (Recommended)\" appended",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"label":       map[string]any{"type": "string", "description": "short option label the user clicks"},
									"description": map[string]any{"type": "string", "description": "one line on what choosing this means"},
								},
								"required": []string{"label"},
							},
						},
					},
					"required": []string{"header", "question", "options"},
				},
			},
		},
		"required": []string{"questions"},
	}
}
func (askUser) Access() AccessClass { return Interactive }

// Execute is a defensive stub: Interactive calls are intercepted by the chat
// loop (runClarification). Reaching Execute means no clarification registry
// was wired — fail loudly rather than fabricate an answer.
func (askUser) Execute(context.Context, *RunContext, json.RawMessage) (string, error) {
	return "", errors.New("ask_user requires the interactive chat loop (no clarification registry wired)")
}

// AskUserOption is one selectable option of an AskUserQuestion.
type AskUserOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AskUserQuestion is one structured question in an ask_user call.
type AskUserQuestion struct {
	Header      string          `json:"header"`
	Question    string          `json:"question"`
	MultiSelect bool            `json:"multiSelect"`
	Options     []AskUserOption `json:"options"`
}

// ParseAskUserQuestions validates and decodes an ask_user call's args:
// 1-4 questions, each with non-empty question text and 2-4 options. The
// loop feeds a validation error straight back to the model (no UI event),
// so the message should tell the model how to fix the call.
func ParseAskUserQuestions(args json.RawMessage) ([]AskUserQuestion, error) {
	var body struct {
		Questions []AskUserQuestion `json:"questions"`
	}
	if err := json.Unmarshal(args, &body); err != nil {
		return nil, errors.New("invalid ask_user args: " + err.Error())
	}
	if n := len(body.Questions); n < 1 || n > 4 {
		return nil, fmt.Errorf("ask_user requires 1-4 questions, got %d", n)
	}
	for i, q := range body.Questions {
		if strings.TrimSpace(q.Question) == "" {
			return nil, fmt.Errorf("ask_user question %d: empty question text", i+1)
		}
		if n := len(q.Options); n < 2 || n > 4 {
			return nil, fmt.Errorf("ask_user question %d: 2-4 options required, got %d", i+1, n)
		}
	}
	return body.Questions, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/sentry/skills/ -v -run 'TestAskUser|TestParseAskUser'
```
Expected: PASS (4 tests). Then the whole package (existing Defs tests must still pass):

```bash
cd backend && CGO_ENABLED=0 go test ./internal/sentry/skills/
```
Expected: ok.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/sentry/skills/skill.go backend/internal/sentry/skills/askuser.go backend/internal/sentry/skills/askuser_test.go
git commit -m "feat(sentry): add Interactive access class and ask_user skill (#264)"
```

---

### Task 2: `ClarificationRegistry`

**Files:**
- Create: `backend/internal/sentry/clarifications.go`
- Create: `backend/internal/sentry/clarifications_test.go` (registry test only; loop tests are Task 3)

**Interfaces:**
- Consumes: nothing new.
- Produces (used by Tasks 3–4):
  - `var ErrClarificationNotFound, ErrClarificationForbidden error`
  - `type ClarificationAnswer struct { Answers json.RawMessage; Skipped bool; AnsweredBy string }`
  - `type ClarificationRegistry` with `NewClarificationRegistry() *ClarificationRegistry`, unexported `register(id, owner string) chan ClarificationAnswer`, unexported `remove(id string)`, exported `Answer(id, user string, answers json.RawMessage, skipped bool) error`, exported `PendingCount() int`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/sentry/clarifications_test.go`:

```go
package sentry

import (
	"encoding/json"
	"errors"
	"testing"
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/sentry/ -run TestClarificationRegistry -v
```
Expected: FAIL to compile — `undefined: NewClarificationRegistry`.

- [ ] **Step 3: Write clarifications.go**

Create `backend/internal/sentry/clarifications.go`:

```go
package sentry

import (
	"encoding/json"
	"errors"
	"sync"
)

// Clarification-plumbing errors, mapped to HTTP statuses by the answer
// handler.
var (
	// ErrClarificationNotFound: unknown clarification id, or one already
	// answered / timed out / abandoned. Deliberately indistinguishable — an
	// answer that lost the race gets the same reply as a bogus id.
	ErrClarificationNotFound = errors.New("clarification not found or already resolved")
	// ErrClarificationForbidden: the answering user is not the
	// conversation's owner.
	ErrClarificationForbidden = errors.New("clarification belongs to another user's conversation")
)

// ClarificationAnswer is a human's answer to one ask_user call: the raw
// answers array from the UI (index-aligned with the questions), or Skipped
// when the operator explicitly told Sentry to proceed with defaults.
type ClarificationAnswer struct {
	Answers    json.RawMessage
	Skipped    bool
	AnsweredBy string
}

type pendingClarification struct {
	owner string
	ch    chan ClarificationAnswer
}

// ClarificationRegistry is the ask_user twin of ApprovalRegistry (#264
// mirroring #221): an in-memory map of pending clarification IDs to the
// channel the blocked chat loop waits on. Same discipline: the loop
// registers BEFORE emitting clarification_request so an answer can never
// race registration; Answer removes the entry under the lock, so exactly
// one answer wins.
type ClarificationRegistry struct {
	mu sync.Mutex
	m  map[string]pendingClarification
}

// NewClarificationRegistry returns an empty registry. One instance per
// server process, shared by the chat handler (via Loop) and the answer
// handler.
func NewClarificationRegistry() *ClarificationRegistry {
	return &ClarificationRegistry{m: map[string]pendingClarification{}}
}

// register creates the pending entry and returns the buffered channel the
// loop should block on. owner is the conversation's user — the only user
// allowed to answer.
func (r *ClarificationRegistry) register(id, owner string) chan ClarificationAnswer {
	ch := make(chan ClarificationAnswer, 1)
	r.mu.Lock()
	r.m[id] = pendingClarification{owner: owner, ch: ch}
	r.mu.Unlock()
	return ch
}

// remove drops the entry (idempotent). The loop calls it on every exit path
// (answered, timeout, context cancelled); Answer has usually already
// removed it.
func (r *ClarificationRegistry) remove(id string) {
	r.mu.Lock()
	delete(r.m, id)
	r.mu.Unlock()
}

// Answer resolves a pending clarification: owner-checked, single-winner
// (the entry is removed under the lock before delivery on the buffered
// channel, so delivery never blocks and never races a second answer).
// skipped=true means "proceed with your defaults" — answers are ignored.
func (r *ClarificationRegistry) Answer(id, user string, answers json.RawMessage, skipped bool) error {
	r.mu.Lock()
	p, ok := r.m[id]
	if !ok {
		r.mu.Unlock()
		return ErrClarificationNotFound
	}
	if p.owner != user {
		r.mu.Unlock()
		return ErrClarificationForbidden
	}
	delete(r.m, id)
	r.mu.Unlock()
	if skipped {
		answers = nil
	}
	p.ch <- ClarificationAnswer{Answers: answers, Skipped: skipped, AnsweredBy: user}
	return nil
}

// PendingCount reports how many clarifications are awaiting an answer
// (diagnostics/tests).
func (r *ClarificationRegistry) PendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.m)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/sentry/ -run TestClarificationRegistry -v
```
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/sentry/clarifications.go backend/internal/sentry/clarifications_test.go
git commit -m "feat(sentry): add ClarificationRegistry for ask_user answers (#264)"
```

---

### Task 3: Loop interception — `runClarification`, events, persistence

**Files:**
- Modify: `backend/internal/sentry/loop.go`
- Modify: `backend/internal/sentry/clarifications_test.go` (append loop tests)

**Interfaces:**
- Consumes: `skills.Interactive`, `skills.NewAskUser`, `skills.ParseAskUserQuestions`, `skills.AskUserQuestion` (Task 1); `ClarificationRegistry`, `ClarificationAnswer` (Task 2); existing `newConversationID`, `runTool`, `toolCallMeta`, `Event`.
- Produces (used by Tasks 4, 6):
  - `Loop.Clarifications *ClarificationRegistry` field
  - NDJSON events: `clarification_request` (`clarification_id`, `id`, `name`, `args` verbatim, `timeout_ms`) and `clarification_result` (`clarification_id`, `id`, `name`, `outcome` = `answered|skipped|timeout|abandoned`, `answers` raw JSON string, `answered_by`)
  - persisted `toolCallMeta.Clarification *clarificationMeta` (`{clarification_id, outcome, answers, answered_by}`)

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/sentry/clarifications_test.go` (add `"context"`, `"reflect"`, `"strings"`, and `"github.com/timeplus/agentguard/internal/sentry/skills"` to its imports):

```go
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
```

Note: `testChatCfg()` (in `loop_test.go`) has `ApprovalTimeoutMs: 0` today — `TestLoopClarificationAnswered` asserts `req.TimeoutMs == testChatCfg().ApprovalTimeoutMs`, which holds for any value, but the answered/skipped tests resolve on the request event before any timer matters. For safety add `ApprovalTimeoutMs: 60000` to `testChatCfg()` in `loop_test.go` so a slow CI machine can never race a 0ms timer:

```go
func testChatCfg() ChatCfg {
	return ChatCfg{
		MaxIterations:      5,
		TurnTokenBudget:    32000,
		ToolResultMaxBytes: 16384,
		ApprovalTimeoutMs:  60000,
		SystemPrompt:       "You are Sentry. {schema_card}",
	}
}
```

(Check `TestLoopGatedTimeout` still overrides it to 20 — it does.)

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/sentry/ -run TestLoopClarification -v
```
Expected: FAIL to compile — `unknown field Clarifications`, `e.ClarificationID undefined`.

- [ ] **Step 3: Implement the loop changes**

In `backend/internal/sentry/loop.go`:

**(a)** Update the `Event` doc comment's type list and add fields:

```go
type Event struct {
	Type             string `json:"event"`                        // meta|token|thinking|tool_call|tool_result|approval_request|approval_result|clarification_request|clarification_result|done|error
	Text             string `json:"text,omitempty"`               // token
	ID               string `json:"id,omitempty"`                 // tool_call/tool_result/approval_*/clarification_*
	Name             string `json:"name,omitempty"`               // tool_call/tool_result/approval_*/clarification_*
	Args             string `json:"args,omitempty"`               // tool_call/approval_request/clarification_request (verbatim)
	Result           string `json:"result,omitempty"`             // tool_result
	Truncated        bool   `json:"truncated,omitempty"`          // tool_result
	ConvID           string `json:"conversation_id,omitempty"`    // meta
	Seq              uint32 `json:"seq,omitempty"`                // meta
	TokensIn         int    `json:"tokens_in,omitempty"`          // done
	TokensOut        int    `json:"tokens_out,omitempty"`         // done
	CacheReadTokens  int    `json:"cache_read_tokens,omitempty"`  // done (anthropic prompt caching)
	CacheWriteTokens int    `json:"cache_write_tokens,omitempty"` // done (anthropic prompt caching)
	Message          string `json:"message,omitempty"`            // error / approval_result reason
	ProposalID       string `json:"proposal_id,omitempty"`        // approval_request/approval_result
	TimeoutMs        int    `json:"timeout_ms,omitempty"`         // approval_request/clarification_request
	Decision         string `json:"decision,omitempty"`           // approval_result: approved|denied|timeout|abandoned
	DecidedBy        string `json:"decided_by,omitempty"`         // approval_result
	ClarificationID  string `json:"clarification_id,omitempty"`   // clarification_request/clarification_result
	Outcome          string `json:"outcome,omitempty"`            // clarification_result: answered|skipped|timeout|abandoned
	Answers          string `json:"answers,omitempty"`            // clarification_result (raw answers JSON)
	AnsweredBy       string `json:"answered_by,omitempty"`        // clarification_result
}
```

**(b)** Add the registry field to `Loop` (after `Approvals`):

```go
	Approvals      *ApprovalRegistry
	Clarifications *ClarificationRegistry // ask_user answers (#264); nil keeps ask_user un-offered
```

**(c)** Extend `toolCallMeta` and add `clarificationMeta` next to `approvalMeta`:

```go
type toolCallMeta struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Args          string             `json:"args"`
	Approval      *approvalMeta      `json:"approval,omitempty"`
	Clarification *clarificationMeta `json:"clarification,omitempty"`
}

// clarificationMeta is the persisted audit record of one ask_user exchange.
type clarificationMeta struct {
	ClarificationID string          `json:"clarification_id"`
	Outcome         string          `json:"outcome"` // answered|skipped|timeout|abandoned
	Answers         json.RawMessage `json:"answers,omitempty"`
	AnsweredBy      string          `json:"answered_by,omitempty"`
}
```

**(d)** In `Run`, replace the `maxAccess` block:

```go
	// Interactive chat with an approval registry offers the gated write
	// surface too; adding a clarification registry offers ask_user on top.
	// Without registries (future headless runs) the model only ever sees
	// read-only tools.
	maxAccess := skills.ReadOnly
	if l.Approvals != nil {
		maxAccess = skills.GatedWrite
	}
	if l.Approvals != nil && l.Clarifications != nil {
		maxAccess = skills.Interactive
	}
```

**(e)** In `Run`'s tool dispatch, replace the `var out string ... if/else` with:

```go
		for _, tc := range res.ToolCalls {
			var out string
			var appr *approvalMeta
			var clar *clarificationMeta
			sk, known := l.Reg.Get(tc.Name)
			switch {
			case known && sk.Access() == skills.Interactive && l.Clarifications != nil:
				out, clar = l.runClarification(ctx, cc, conv, tc, emit)
			case known && sk.Access() == skills.GatedWrite && l.Approvals != nil:
				out, appr = l.runGated(ctx, cc, conv, rc, tc, emit)
			default:
				emit(Event{Type: "tool_call", ID: tc.ID, Name: tc.Name, Args: tc.Args})
				out = runTool(ctx, l.Reg, rc, tc)
			}
```

and pass both metas into the persisted row:

```go
			metaJSON, err := json.Marshal(toolCallMeta{ID: tc.ID, Name: tc.Name, Args: tc.Args, Approval: appr, Clarification: clar})
```

**(f)** Add `runClarification` and its helpers after `runGated`:

```go
// runClarification runs one ask_user call through the in-chat clarification
// flow (#264, mirroring runGated/#221): validate the questions, register
// the clarification (BEFORE emitting, so an answer can never race
// registration), emit clarification_request with the verbatim args, block
// until a human answer / skip / timeout / client disconnect, emit
// clarification_result, and feed the outcome back to the model as the tool
// result. Unanswered (skip/timeout) resolves as "proceed with your
// recommended defaults" — the turn always continues; there is no
// auto-answer path.
func (l *Loop) runClarification(ctx context.Context, cc ChatCfg, conv models.SentryConversation, tc ToolCall, emit func(Event)) (string, *clarificationMeta) {
	questions, err := skills.ParseAskUserQuestions(json.RawMessage(tc.Args))
	if err != nil {
		// Malformed call: correct the model without involving the UI.
		return "error: " + err.Error(), nil
	}
	cid, err := newConversationID() // same 32-hex crypto/rand generator
	if err != nil {
		return "error: could not create clarification: " + err.Error(), nil
	}
	ch := l.Clarifications.register(cid, conv.User)
	defer l.Clarifications.remove(cid)

	emit(Event{Type: "clarification_request", ClarificationID: cid, ID: tc.ID, Name: tc.Name,
		Args: tc.Args, TimeoutMs: cc.ApprovalTimeoutMs})

	timer := time.NewTimer(time.Duration(cc.ApprovalTimeoutMs) * time.Millisecond)
	defer timer.Stop()

	outcomeOf := func(a ClarificationAnswer) string {
		if a.Skipped {
			return "skipped"
		}
		return "answered"
	}
	var a ClarificationAnswer
	var outcome string
	select {
	case a = <-ch:
		outcome = outcomeOf(a)
	case <-timer.C:
		// An answer can win the registry race in the same instant the timer
		// fires. Drain once so it is honored rather than dropped.
		select {
		case a = <-ch:
			outcome = outcomeOf(a)
		default:
			outcome = "timeout"
		}
	case <-ctx.Done():
		outcome = "abandoned"
	}

	meta := &clarificationMeta{ClarificationID: cid, Outcome: outcome, Answers: a.Answers, AnsweredBy: a.AnsweredBy}
	emit(Event{Type: "clarification_result", ClarificationID: cid, ID: tc.ID, Name: tc.Name,
		Outcome: outcome, Answers: string(a.Answers), AnsweredBy: a.AnsweredBy})

	switch outcome {
	case "answered":
		return formatClarificationResult(questions, a.Answers), meta
	case "skipped":
		return clarificationNoAnswer("the user skipped the questions"), meta
	case "timeout":
		return clarificationNoAnswer(fmt.Sprintf("no answer after %ds", cc.ApprovalTimeoutMs/1000)), meta
	default: // abandoned — client disconnected while waiting
		return "error: clarification abandoned (connection closed) — no answer collected", meta
	}
}

// formatClarificationResult pairs each question with the UI's index-aligned
// answers ({selected, other}) into the tool result the model reads. The
// answers payload is operator-controlled; unparseable answers degrade to
// question-only entries rather than failing the turn.
func formatClarificationResult(questions []skills.AskUserQuestion, answers json.RawMessage) string {
	var parsed []struct {
		Selected []string `json:"selected"`
		Other    string   `json:"other"`
	}
	_ = json.Unmarshal(answers, &parsed)
	type qa struct {
		Question string   `json:"question"`
		Selected []string `json:"selected,omitempty"`
		Other    string   `json:"other,omitempty"`
	}
	out := make([]qa, len(questions))
	for i, q := range questions {
		out[i] = qa{Question: q.Question}
		if i < len(parsed) {
			out[i].Selected = parsed[i].Selected
			out[i].Other = parsed[i].Other
		}
	}
	b, err := json.Marshal(map[string]any{"answers": out})
	if err != nil {
		return "error: could not format answers: " + err.Error()
	}
	return "The user answered your questions:\n" + string(b)
}

// clarificationNoAnswer is the skip/timeout tool result: the turn continues
// on the model's own judgment, with the assumptions made explicit.
func clarificationNoAnswer(why string) string {
	return "No answer received (" + why + "). Proceed with your recommended defaults and clearly state the assumptions you made in your final answer."
}
```

**(g)** Update the `Loop` doc comment to mention ask_user:

```go
// Loop is AgentGuard Sentry's ReAct agent harness: it wires an LLM to a
// skills.Registry and a Store, running model -> tools -> model until the
// model stops requesting tools or cc.MaxIterations is hit. ReadOnly skills
// execute directly; GatedWrite skills go through the in-chat approval flow
// (#221) and require Approvals to be set; the Interactive ask_user skill
// blocks on a human answer via Clarifications (#264). Nil registries keep
// the corresponding surface un-offered to the model.
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/sentry/ -v -run 'TestLoopClarification|TestLoopAskUser'
```
Expected: PASS (6 tests). Then the whole package:

```bash
cd backend && CGO_ENABLED=0 go test ./internal/sentry/...
```
Expected: ok (existing approval/loop/context tests untouched).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/sentry/loop.go backend/internal/sentry/loop_test.go backend/internal/sentry/clarifications_test.go
git commit -m "feat(sentry): intercept ask_user in the chat loop with clarification events (#264)"
```

---

### Task 4: HTTP wiring — registry in registry list, answer endpoint, main.go

**Files:**
- Modify: `backend/internal/handlers/sentry.go` (`sentryRegistry`, `SentryChat` signature, new handler)
- Modify: `backend/internal/handlers/sentry_test.go` (skill lists, `SentryChat` call sites, new handler tests)
- Modify: `backend/cmd/server/main.go` (registry + route)

**Interfaces:**
- Consumes: `sentry.NewClarificationRegistry`, `sentry.ClarificationRegistry.Answer`, `sentry.ErrClarificationNotFound/Forbidden` (Task 2); `skills.NewAskUser` (Task 1); `Loop.Clarifications` (Task 3).
- Produces: `POST /api/sentry/clarifications/:clarification_id/answer` (body `{answers?: [...], skipped?: bool}` → 200 `{status:"answered", skipped}` / 400 / 403 / 404); `SentryChat(cfg, mgr, lc, approvals, clarifications, holdReg)`.

- [ ] **Step 1: Write the failing tests**

In `backend/internal/handlers/sentry_test.go`:

**(a)** Add `"ask_user"` after `"run_sql"` in BOTH `wantSentrySkills` and `baseSentrySkills` (registration order — `NewAskUser` goes right after `NewRunSQL`):

```go
var wantSentrySkills = []string{
	"load_skill", "load_skill_reference", "list_streams", "describe_stream",
	"run_sql", "ask_user", "dlp_search", "get_threats", "get_holds", "get_rule_status",
	"get_cost_summary", "get_session_timeline", "get_rule_catalog",
	"acknowledge_threat", "clear_threat", "decide_hold",
	"install_rule", "configure_rule", "set_rule_enabled", "set_rule_policy",
	"upgrade_rule", "uninstall_rule", "sync_rules",
	"get_sentry_config",
}

var baseSentrySkills = []string{
	"load_skill", "load_skill_reference", "list_streams", "describe_stream", "run_sql",
	"ask_user",
	"get_sentry_config",
}
```

**(b)** Update the three existing `SentryChat(cfg, nil, nil, nil, nil)` call sites to `SentryChat(cfg, nil, nil, nil, nil, nil)`.

**(c)** Append handler tests:

```go
func clarificationCtx(t *testing.T, id, user, body string) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/sentry/clarifications/"+id+"/answer", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "clarification_id", Value: id}}
	c.Set("timeplus_user", user)
	return w, c
}

func TestAnswerSentryClarification_UnknownID404(t *testing.T) {
	reg := sentry.NewClarificationRegistry()
	w, c := clarificationCtx(t, "nope", "alice", `{"skipped":true}`)
	AnswerSentryClarification(reg)(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body = %s", w.Code, w.Body.String())
	}
}

func TestAnswerSentryClarification_BadBody400(t *testing.T) {
	reg := sentry.NewClarificationRegistry()
	w, c := clarificationCtx(t, "x", "alice", `{`)
	AnswerSentryClarification(reg)(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
}

// Neither answers nor skipped is a client error: an accidental empty submit
// must not consume the single-answer slot.
func TestAnswerSentryClarification_EmptyAnswer400(t *testing.T) {
	reg := sentry.NewClarificationRegistry()
	w, c := clarificationCtx(t, "x", "alice", `{}`)
	AnswerSentryClarification(reg)(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", w.Code, w.Body.String())
	}
}
```

(`strings` is already imported in `sentry_test.go`; the owner-forbidden and answered-OK paths are covered by the registry tests in `internal/sentry` — `register` is unexported, so handler tests cover the reachable mappings: 400/404.)

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run 'TestAnswerSentryClarification|TestSentryRegistry' -v
```
Expected: FAIL to compile — `undefined: AnswerSentryClarification`, wrong `SentryChat` arg count.

- [ ] **Step 3: Implement**

In `backend/internal/handlers/sentry.go`:

**(a)** In `sentryRegistry`, add `skills.NewAskUser()` right after `skills.NewRunSQL()` in the base list, and note it in the doc comment:

```go
	sks := []skills.Skill{
		skills.NewLoadSkill(cfg.Sentry.SkillDir, hiddenSkills...),
		skills.NewLoadSkillReference(cfg.Sentry.SkillDir, hiddenSkills...),
		skills.NewListStreams(),
		skills.NewDescribeStream(),
		skills.NewRunSQL(),
		skills.NewAskUser(), // Interactive clarification (#264): always registered, offered only in interactive chat
		skills.NewGetSentryConfig(sentrySelfConfigFn(cfg, cc)),
	}
```

**(b)** Change `SentryChat`'s signature and Loop construction:

```go
func SentryChat(cfg *config.Config, mgr *catalog.Manager, lc *rules.Lifecycle, approvals *sentry.ApprovalRegistry, clarifications *sentry.ClarificationRegistry, holdReg *holdregistry.Registry) gin.HandlerFunc {
```

```go
		loop := &sentry.Loop{LLM: llm, Reg: sentryRegistry(rcfg, mgr, lc, holdReg, cc), Store: store, Cfg: rcfg,
			Approvals: approvals, Clarifications: clarifications,
			Memory: sentry.LoadMemoryCfg(&rcfg.Timeplus)}
```

**(c)** Add the handler after `DecideSentryApproval`:

```go
// AnswerSentryClarification handles POST
// /sentry/clarifications/:clarification_id/answer: resolve one pending
// ask_user clarification ({answers?: [...], skipped?: bool}). Owner-only —
// the registry enforces that only the conversation's user may answer (403);
// an unknown or already-resolved clarification is 404. skipped:true tells
// Sentry to proceed with its own defaults; otherwise a non-empty answers
// array is required so an accidental empty submit never consumes the
// single-answer slot (#264).
func AnswerSentryClarification(clarifications *sentry.ClarificationRegistry) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Answers are operator-authored free-form JSON relayed into the
		// model's context; cap the body so the tool result stays sane.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		var body struct {
			Answers json.RawMessage `json:"answers"`
			Skipped bool            `json:"skipped"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "body must be {answers?: [...], skipped?: bool}"})
			return
		}
		if !body.Skipped && len(body.Answers) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "answers are required unless skipped is true"})
			return
		}
		err := clarifications.Answer(c.Param("clarification_id"), sessionUser(c), body.Answers, body.Skipped)
		switch {
		case errors.Is(err, sentry.ErrClarificationNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, sentry.ErrClarificationForbidden):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		case err != nil:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusOK, gin.H{"status": "answered", "skipped": body.Skipped})
		}
	}
}
```

In `backend/cmd/server/main.go`, next to `sentryApprovals := sentry.NewApprovalRegistry()`:

```go
	sentryClarifications := sentry.NewClarificationRegistry()
```

and update/add the routes:

```go
			protected.POST("/sentry/chat", handlers.SentryChat(cfg, mgr, lifecycle, sentryApprovals, sentryClarifications, holdReg))
			protected.POST("/sentry/clarifications/:clarification_id/answer", handlers.AnswerSentryClarification(sentryClarifications))
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/handlers/ -run 'TestAnswerSentryClarification|TestSentryRegistry|TestSentryChat' -v
```
Expected: build ok; PASS (all listed tests, including updated skill lists).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/sentry.go backend/internal/handlers/sentry_test.go backend/cmd/server/main.go
git commit -m "feat(sentry): ask_user registration and clarification answer endpoint (#264)"
```

---

### Task 5: System prompt guidance + data-audit-guide "Clarify first" hints

**Files:**
- Modify: `backend/internal/timeplus/sentry.go` (`DefaultSentrySystemPrompt`)
- Modify: `backend/internal/sentry/skills/knowledge/data-audit-guide/SKILL.md`
- Modify: `backend/internal/sentry/skills/dataaudit_skill_test.go` (one assertion)

**Interfaces:**
- Consumes: the `ask_user` tool name (Task 1).
- Produces: prompt/skill text only.

- [ ] **Step 1: Write the failing test**

In `backend/internal/sentry/skills/dataaudit_skill_test.go`, inside `TestDataAuditGuideLoads`'s wanted-strings list (the loop asserting `strings.Contains(out, want)`), add two entries:

```go
		"Clarify first",
		"ask_user",
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/sentry/skills/ -run TestDataAuditGuideLoads -v
```
Expected: FAIL — `SKILL.md missing "Clarify first"`.

- [ ] **Step 3: Edit the prompt and the skill**

In `backend/internal/timeplus/sentry.go`, add one bullet to `DefaultSentrySystemPrompt`'s ground rules, directly after the `load_skill` bullet (`- Call load_skill once per needed knowledge skill ...`):

```
- If the request is ambiguous or hinges on a deployment-specific definition (which tools count as unauthorized, which data is sensitive, scope, time window, target agents/sessions), call ask_user with 1-3 concise questions (2-4 options each, your recommended option first) before investigating. Specific requests proceed directly; never ask when a conventional default exists.
```

In `backend/internal/sentry/skills/knowledge/data-audit-guide/SKILL.md`, insert a new section between "## Query skeleton" and "## How to investigate":

```markdown
## Clarify first (deployment-specific items)

Some items hinge on definitions only the operator knows. When the request
targets one of these and the user has not supplied the definition, call
`ask_user` (1–3 questions, your recommended option first) before querying:

- **A09 unauthorized tool call** — ask which tools count as unauthorized in
  this deployment. Suggested options: `Bash (Recommended)` (shell execution —
  highest-risk default), `Write` (filesystem writes), `WebFetch` / other
  network or exec tools; the operator's own allowlist arrives via "Other".
- **A13/A14 external addresses & domains** — ask which domains/addresses are
  registered or allowlisted; everything else counts as external/unregistered.
- **A17 bulk extraction** — ask what row/volume threshold and which data sets
  count as sensitive for this deployment.

A specific request ("flag any Bash calls in the last day") already contains
the definition — proceed directly without asking.
```

And in "## How to investigate", extend step 1's last sentence:

```markdown
1. **Start from the user's intent.** What do they actually want — a specific
   data type, agent, session, time window, or one of the A-codes? Pick the
   matching goal(s) below; the A-code is a lens, not a rigid script. If the
   item is listed under **Clarify first** and the user gave no definition,
   ask via `ask_user` before querying.
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && CGO_ENABLED=0 go test ./internal/sentry/skills/ ./internal/timeplus/ -run 'TestDataAudit|TestSentry' -v
```
Expected: PASS (SKILL.md assertions + the prompt placeholder tests in `internal/timeplus`, which the new bullet does not disturb).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/timeplus/sentry.go backend/internal/sentry/skills/knowledge/data-audit-guide/SKILL.md backend/internal/sentry/skills/dataaudit_skill_test.go
git commit -m "feat(sentry): ask_user guidance in system prompt and data-audit-guide (#264)"
```

---

### Task 6: Frontend — API client, stream handling, ClarificationCard, replay, i18n

**Files:**
- Modify: `frontend/src/api/client.js`
- Modify: `frontend/src/pages/Sentry.jsx`
- Modify: `frontend/src/i18n/locales/en/sentry.json`
- Modify: `frontend/src/i18n/locales/zh-CN/sentry.json`

**Interfaces:**
- Consumes: `clarification_request`/`clarification_result` events and the answer endpoint (Tasks 3–4); `meta.clarification` in replayed tool rows (Task 3).
- Produces: `api.answerSentryClarification(clarificationId, payload)`; `ClarificationCard` rendered for `role === 'clarification'` items.

- [ ] **Step 1: API client**

In `frontend/src/api/client.js`, after `sentryDecideApproval`:

```js
  answerSentryClarification: (clarificationId, payload) =>
    post(`/sentry/clarifications/${encodeURIComponent(clarificationId)}/answer`, payload),
```

- [ ] **Step 2: i18n strings**

In `frontend/src/i18n/locales/en/sentry.json`, after the `"approval"` object add:

```json
  "clarification": {
    "defaultName": "clarifying questions",
    "awaiting": "awaiting your answers…",
    "otherPlaceholder": "Other — type your own answer…",
    "submit": "Submit answers",
    "skip": "Skip — let Sentry decide",
    "noAnswer": "—",
    "status": {
      "answeredBy": "answered by {{by}}",
      "operator": "operator",
      "skipped": "skipped — Sentry proceeds with defaults",
      "timeout": "timed out — Sentry proceeds with defaults",
      "abandoned": "abandoned — no answer collected"
    }
  },
```

and inside its `"errors"` object add:

```json
    "clarificationFailed": "Failed to submit answers"
```

In `frontend/src/i18n/locales/zh-CN/sentry.json`, same shape:

```json
  "clarification": {
    "defaultName": "澄清问题",
    "awaiting": "等待你的回答…",
    "otherPlaceholder": "其他——输入你自己的回答…",
    "submit": "提交回答",
    "skip": "跳过——由 Sentry 决定",
    "noAnswer": "—",
    "status": {
      "answeredBy": "已由 {{by}} 回答",
      "operator": "操作员",
      "skipped": "已跳过——Sentry 将按默认假设继续",
      "timeout": "已超时——Sentry 将按默认假设继续",
      "abandoned": "已放弃——未收集到回答"
    }
  },
```

and in its `"errors"` object:

```json
    "clarificationFailed": "提交回答失败"
```

- [ ] **Step 3: Sentry.jsx — helpers**

Next to `approvalFromRow` add:

```js
// clarificationFromRow pulls the persisted ask_user audit record
// ({clarification_id, outcome, answers, answered_by}) out of a replayed
// tool row's meta, if any.
function clarificationFromRow(raw) {
  if (!raw) return null
  try {
    const meta = JSON.parse(raw)
    if (meta && typeof meta === 'object' && meta.clarification) return meta.clarification
  } catch { /* not toolCallMeta JSON */ }
  return null
}

// questionsFromArgs parses an ask_user call's verbatim args into its
// questions array ([] on any malformed input — render nothing, never throw).
function questionsFromArgs(args) {
  try {
    const q = JSON.parse(args)?.questions
    return Array.isArray(q) ? q : []
  } catch {
    return []
  }
}

// parseAnswers normalizes the answers payload — a raw JSON string on the
// live clarification_result event, an already-parsed array in replayed meta.
function parseAnswers(raw) {
  if (!raw) return null
  if (Array.isArray(raw)) return raw
  try {
    const v = JSON.parse(raw)
    return Array.isArray(v) ? v : null
  } catch {
    return null
  }
}

// clarificationStatusLine renders one compact line describing a resolved
// ask_user exchange (live card and replayed rows).
function clarificationStatusLine(item, t) {
  if (!item.outcome) return null
  const label = {
    answered: t('clarification.status.answeredBy', { by: item.answeredBy || item.answered_by || t('clarification.status.operator') }),
    skipped: t('clarification.status.skipped'),
    timeout: t('clarification.status.timeout'),
    abandoned: t('clarification.status.abandoned'),
  }[item.outcome] || item.outcome
  const cls = item.outcome === 'answered' ? 'text-primary' : 'text-gray-400'
  return <span className={`text-[10px] ${cls}`}>{label}</span>
}
```

- [ ] **Step 4: Sentry.jsx — ClarificationCard component**

Add after `ApprovalCard`:

```jsx
// ClarificationCard renders an ask_user exchange: the model's structured
// questions as selectable chips (radio for single-select, toggle for
// multiSelect) plus a free-text "Other" per question, with Submit / Skip
// while pending, then a compact Q&A summary. Question and option text is
// LLM-generated and rendered verbatim (it follows the conversation
// language); only the chrome strings are translated.
function ClarificationCard({ item, onAnswer }) {
  const { t } = useTranslation('sentry')
  const pending = !item.outcome
  const [selections, setSelections] = useState(() => (item.questions || []).map(() => ({ selected: [], other: '' })))

  function toggleOption(qi, label, multi) {
    setSelections(prev => prev.map((s, i) => {
      if (i !== qi) return s
      if (multi) {
        const selected = s.selected.includes(label) ? s.selected.filter(l => l !== label) : [...s.selected, label]
        return { ...s, selected }
      }
      return { ...s, selected: s.selected.includes(label) ? [] : [label] }
    }))
  }

  function setOther(qi, text) {
    setSelections(prev => prev.map((s, i) => (i === qi ? { ...s, other: text } : s)))
  }

  const canSubmit = selections.some(s => s.selected.length > 0 || s.other.trim())
  const answers = item.answers || []

  return (
    <div className="bg-surface-container rounded-sm text-xs max-w-2xl">
      <div className="px-4 py-2.5 flex items-center gap-2 text-gray-100">
        <span className="material-symbols-outlined text-sm shrink-0 text-primary">help</span>
        <span className="font-mono text-[11px]">{item.name || t('clarification.defaultName')}</span>
        {pending
          ? <span className="text-[10px] text-primary animate-pulse ml-1">{t('clarification.awaiting')}</span>
          : <span className="ml-1">{clarificationStatusLine(item, t)}</span>}
      </div>
      <div className="px-4 pb-3 flex flex-col gap-3">
        {(item.questions || []).map((q, qi) => (
          <div key={qi}>
            <div className="flex items-center gap-2 mb-1.5">
              {q.header && (
                <span className="text-[9px] font-semibold uppercase tracking-widest rounded-full px-2 py-0.5 bg-surface-container-high text-gray-400 whitespace-nowrap">{q.header}</span>
              )}
              <p className="text-[11px] text-gray-100">{q.question}</p>
            </div>
            {pending ? (
              <>
                <div className="flex flex-wrap gap-1.5 mb-1.5">
                  {(q.options || []).map(o => {
                    const on = selections[qi]?.selected.includes(o.label)
                    return (
                      <button
                        key={o.label}
                        type="button"
                        onClick={() => toggleOption(qi, o.label, !!q.multiSelect)}
                        title={o.description || ''}
                        className={`px-2.5 py-1 rounded-full text-[11px] ${on
                          ? 'bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-semibold'
                          : 'bg-surface-container-high text-gray-100 hover:text-primary'}`}
                      >
                        {o.label}
                      </button>
                    )
                  })}
                </div>
                <input
                  type="text"
                  value={selections[qi]?.other || ''}
                  onChange={e => setOther(qi, e.target.value)}
                  placeholder={t('clarification.otherPlaceholder')}
                  className="w-full bg-surface-container-high text-gray-100 text-[11px] rounded-sm px-2.5 py-1.5 placeholder-gray-500 focus:outline-none"
                />
              </>
            ) : (
              <p className="text-[11px] text-gray-400">
                {[...(answers[qi]?.selected || []), ...(answers[qi]?.other ? [answers[qi].other] : [])].join(', ') || t('clarification.noAnswer')}
              </p>
            )}
          </div>
        ))}
        {pending && (
          <div className="flex items-center gap-2">
            <button
              onClick={() => onAnswer(item, selections, false)}
              disabled={item.submitting || !canSubmit}
              className={`px-4 py-1.5 text-xs font-semibold rounded-sm ${primaryButtonClass}`}
            >
              {t('clarification.submit')}
            </button>
            <button
              onClick={() => onAnswer(item, null, true)}
              disabled={item.submitting}
              className="px-4 py-1.5 text-xs font-semibold rounded-sm bg-surface-container-high text-gray-100 hover:text-primary disabled:opacity-50"
            >
              {t('clarification.skip')}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
```

(`useState` is already imported in Sentry.jsx; verify and add to the import if not.)

- [ ] **Step 5: Sentry.jsx — page wiring (answer fn, stream cases, replay, render)**

**(a)** Next to `decideApproval` add:

```js
  // answerClarification resolves one pending ask_user exchange. The card's
  // state flips when the loop's clarification_result event echoes back —
  // the local `submitting` flag only debounces the buttons.
  async function answerClarification(item, selections, skipped) {
    setMessages(prev => prev.map(m => m.id === item.id ? { ...m, submitting: true } : m))
    try {
      await api.answerSentryClarification(item.clarificationId,
        skipped ? { skipped: true } : { answers: selections })
    } catch (err) {
      setMessages(prev => prev.map(m => m.id === item.id ? { ...m, submitting: false } : m))
      setConvError(err?.message || t('errors.clarificationFailed'))
    }
  }
```

**(b)** In `send()`'s event switch, after the `approval_result` case:

```js
          case 'clarification_request':
            finalizeOpenBubble()
            finalizeOpenThinking() // stop the reasoning ellipsis during the (possibly long) wait
            setMessages(prev => prev.some(m => m.id === ev.id)
              ? prev
              : [...prev, {
                  id: ev.id, role: 'clarification', clarificationId: ev.clarification_id,
                  name: ev.name, questions: questionsFromArgs(ev.args),
                  outcome: null, answers: null, submitting: false,
                }])
            break

          case 'clarification_result':
            setMessages(prev => prev.map(m => m.id === ev.id
              ? { ...m, outcome: ev.outcome, answers: parseAnswers(ev.answers), answeredBy: ev.answered_by, submitting: false }
              : m))
            break
```

**(c)** In `openConversation`'s `flatMap`, extend the `m.role === 'tool'` branch so a clarification tool row replays as a resolved card:

```js
            if (m.role === 'tool') {
              const clar = clarificationFromRow(m.tool_args)
              if (clar) {
                return [{
                  id: `s${m.seq}`, role: 'clarification', clarificationId: clar.clarification_id,
                  name: m.tool_name, questions: questionsFromArgs(toolArgsFromRow(m.tool_args)),
                  outcome: clar.outcome, answers: parseAnswers(clar.answers),
                  answeredBy: clar.answered_by, submitting: false,
                }]
              }
              return [{
                id: `s${m.seq}`, role: 'tool', name: m.tool_name,
                args: toolArgsFromRow(m.tool_args), result: m.tool_result,
                approval: approvalFromRow(m.tool_args),
                truncated: false, pending: false,
              }]
            }
```

**(d)** In the messages render chain, add the clarification branch beside approval:

```jsx
                    messages.map(item => item.role === 'tool'
                      ? <ToolCard key={item.id} item={item} />
                      : item.role === 'approval'
                        ? <ApprovalCard key={item.id} item={item} onDecide={decideApproval} />
                        : item.role === 'clarification'
                          ? <ClarificationCard key={item.id} item={item} onAnswer={answerClarification} />
                          : item.role === 'thinking'
                            ? <ThinkingCard key={item.id} item={item} />
                            : <MessageBubble key={item.id} item={item} />
                    )
```

- [ ] **Step 6: Lint and build**

```bash
cd frontend && npm run lint && npm run build
```
Expected: no ESLint errors, build succeeds.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/api/client.js frontend/src/pages/Sentry.jsx frontend/src/i18n/locales/en/sentry.json frontend/src/i18n/locales/zh-CN/sentry.json
git commit -m "feat(sentry-ui): render ask_user clarification cards with answers, skip and replay (#264)"
```

---

### Task 7: Documentation + full verification

**Files:**
- Modify: `CLAUDE.md` (endpoint table + Sentry agent section)
- Modify: `docs/sentry.md`

**Interfaces:** none (docs only).

- [ ] **Step 1: CLAUDE.md**

In the API endpoint table, after the `POST /api/sentry/approvals/:proposal_id/decide` row, add:

```markdown
| POST | `/api/sentry/clarifications/:clarification_id/answer` | `AnswerSentryClarification` | Answer or skip one pending `ask_user` clarification (`{answers?, skipped?}`); owner-only (403), unknown/already-resolved → 404. Skip/timeout → Sentry proceeds with stated defaults |
```

In the **Sentry agent** section, extend the sentence describing the NDJSON events to include `clarification_request`/`clarification_result`, and add one sentence (place it after the GatedWrite description):

```markdown
The `ask_user` Interactive tool (#264) mirrors that plumbing for clarifying questions: when the request is ambiguous or deployment-specific, the model asks 1–4 structured questions; the loop blocks on an in-memory `sentry.ClarificationRegistry` (owner-only answer via `POST /api/sentry/clarifications/:clarification_id/answer`, reuses the approval timeout), the `/sentry` UI renders option chips + free-text "Other" + Skip, and skip/timeout resolves as "proceed with your recommended defaults". It is registered unconditionally — available even with `agentguard_tools_enabled` off — but only offered in interactive chat; Q&A is persisted in the tool row's meta for replay.
```

Also update the `POST /api/sentry/chat` row's event list to include `clarification_request`/`clarification_result`.

- [ ] **Step 2: docs/sentry.md**

Insert a new section between "## Gated writes & approvals" and "## Anthropic provider extras (#212)":

```markdown
## Clarifying questions (`ask_user`, #264)

When a request is ambiguous or hinges on a deployment-specific definition
(which tools count as unauthorized, which data is sensitive, scope, time
window, target agents/sessions), Sentry can ask before investigating: the
model calls the `ask_user` tool with 1–4 structured questions (each a short
`header` chip, the question text, and 2–4 options, recommended first).
`ask_user` is always registered — it works even with
`agentguard_tools_enabled` off — but is only offered in interactive chat.

1. The loop registers the exchange in an in-memory clarification registry
   and emits a `clarification_request` NDJSON event carrying
   `clarification_id`, the verbatim questions, and the timeout, then blocks.
2. The `/sentry` page renders the questions as option chips (multi-select
   when the model says so) plus a free-text "Other" per question. The
   conversation owner answers via
   `POST /api/sentry/clarifications/:clarification_id/answer`
   (`{answers: [{selected, other}], skipped?}`) — owner-only, single-answer,
   404 once resolved. A **Skip — let Sentry decide** button posts
   `{skipped: true}`.
3. The loop emits `clarification_result`
   (`answered`/`skipped`/`timeout`/`abandoned`) and feeds the outcome back
   to the model as the tool result. Skip and timeout (reuses
   `approval_timeout_ms`) both resolve as "proceed with your recommended
   defaults and state your assumptions" — an unanswered question never kills
   the investigation. The Q&A is persisted in the tool row's meta
   (`toolCallMeta.clarification`), so replay shows the full exchange.

Unlike gated writes there is nothing to authorize here — the tool is
read-only and interactive; the registry only guarantees the answer comes
from the conversation's owner.
```

- [ ] **Step 3: Full verification**

```bash
cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...
cd ../frontend && npm run lint && npm run build
```
Expected: all green.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/sentry.md
git commit -m "docs: document Sentry ask_user clarification flow (#264)"
```
