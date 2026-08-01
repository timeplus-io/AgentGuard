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
