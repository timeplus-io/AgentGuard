package sentry

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// anthropicLLM is an LLM backed by the Anthropic Messages API over a Bearer
// endpoint (Bedrock Mantle or compatible). base_url is normalized so the same
// configured URL works for both providers.
type anthropicLLM struct{}

// NewAnthropicLLM returns an LLM implementation that talks to an Anthropic
// Messages API compatible endpoint (Claude on Bedrock Mantle et al).
func NewAnthropicLLM() LLM { return anthropicLLM{} }

// normalizeAnthropicBaseURL strips a single trailing "/v1" (and trailing "/")
// because the SDK appends "/v1/messages"; the OpenAI client keeps "/v1".
func normalizeAnthropicBaseURL(u string) string {
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, "/v1")
	return strings.TrimRight(u, "/")
}

// argsToAnthropicInput parses an OpenAI-style tool-call arguments JSON string
// into an object for an Anthropic tool_use block. Empty/invalid -> {}.
func argsToAnthropicInput(args string) map[string]any {
	m := map[string]any{}
	if strings.TrimSpace(args) == "" {
		return m
	}
	_ = json.Unmarshal([]byte(args), &m)
	return m
}

// anthropicInputToArgs serializes an Anthropic tool_use input object back to a
// compact JSON string, matching ChatResult.ToolCalls.Args in the OpenAI path.
func anthropicInputToArgs(input any) string {
	b, err := json.Marshal(input)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// requiredStrings normalizes a JSON-schema "required" value (typically
// []string when built in Go, or []any when round-tripped through JSON) into
// a []string for anthropic.ToolInputSchemaParam.Required.
func requiredStrings(v any) []string {
	switch r := v.(type) {
	case []string:
		return r
	case []any:
		out := make([]string, 0, len(r))
		for _, e := range r {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func (anthropicLLM) ChatStream(ctx context.Context, cc ChatCfg, msgs []Msg, tools []ToolDef, onToken func(string)) (ChatResult, error) {
	client := anthropic.NewClient(
		option.WithBaseURL(normalizeAnthropicBaseURL(cc.BaseURL)),
		option.WithAuthToken(cc.APIKey), // Authorization: Bearer <key>
	)

	// Build system + messages. System is a top-level param, not a message.
	var system string
	var amsgs []anthropic.MessageParam
	for _, m := range msgs {
		switch m.Role {
		case "system":
			system = m.Content
		case "user":
			amsgs = append(amsgs, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			var blocks []anthropic.ContentBlockParamUnion
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, argsToAnthropicInput(tc.Args), tc.Name))
			}
			amsgs = append(amsgs, anthropic.NewAssistantMessage(blocks...))
		case "tool":
			// tool_result blocks ride in a USER message.
			amsgs = append(amsgs, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false)))
		}
	}

	var atools []anthropic.ToolUnionParam
	for _, t := range tools {
		atools = append(atools, anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: t.Params["properties"],
				Required:   requiredStrings(t.Params["required"]),
			},
		}})
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(cc.Model),
		MaxTokens: int64(cc.MaxTokens),
		Messages:  amsgs,
		Tools:     atools,
	}
	if system != "" {
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}

	stream := client.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	msg := anthropic.Message{}
	for stream.Next() {
		event := stream.Current()
		_ = msg.Accumulate(event)
		if d, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok {
			if td, ok := d.Delta.AsAny().(anthropic.TextDelta); ok && td.Text != "" && onToken != nil {
				onToken(td.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return ChatResult{}, err
	}

	res := ChatResult{
		TokensIn:  int(msg.Usage.InputTokens),
		TokensOut: int(msg.Usage.OutputTokens),
	}
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			res.Content += b.Text
		case anthropic.ToolUseBlock:
			// b.Input is json.RawMessage (already a JSON object); it
			// implements json.Marshaler so anthropicInputToArgs re-emits
			// it verbatim without a decode/re-encode round trip. An
			// empty/absent input must still come back as "{}", not "null".
			args := "{}"
			if len(b.Input) > 0 {
				args = anthropicInputToArgs(b.Input)
			}
			res.ToolCalls = append(res.ToolCalls, ToolCall{
				ID: b.ID, Name: b.Name, Args: args,
			})
		}
	}
	return res, nil
}
