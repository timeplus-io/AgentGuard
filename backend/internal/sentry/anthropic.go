package sentry

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// anthropicLLM is an LLM backed by the Anthropic Messages API, authenticated
// with an x-api-key header (AWS Bedrock Mantle's Anthropic surface and native
// api.anthropic.com both use this scheme). normalizeAnthropicBaseURL strips a
// trailing /v1 so the SDK's appended /v1/messages lands correctly; note the
// base URL differs per provider on Bedrock (Anthropic at .../anthropic, the
// OpenAI-compatible surface at .../openai/v1) — see docs/sentry.md.
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

// anthropicMessagesFromMsgs translates the loop's OpenAI-shaped messages into
// the Anthropic system string + message list. Consecutive role:"tool" messages
// are coalesced into ONE user message carrying all their tool_result blocks, as
// the Anthropic Messages API requires for (parallel) tool use.
func anthropicMessagesFromMsgs(msgs []Msg) (system string, amsgs []anthropic.MessageParam) {
	var toolResults []anthropic.ContentBlockParamUnion
	flush := func() {
		if len(toolResults) > 0 {
			amsgs = append(amsgs, anthropic.NewUserMessage(toolResults...))
			toolResults = nil
		}
	}
	for _, m := range msgs {
		switch m.Role {
		case "system":
			system = m.Content
		case "user":
			flush()
			amsgs = append(amsgs, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			flush()
			var blocks []anthropic.ContentBlockParamUnion
			// Thinking blocks must be echoed back verbatim, BEFORE text and
			// tool_use — the API requires an assistant tool_use turn to
			// retain its reasoning blocks when the conversation continues
			// within the same tool loop.
			for _, tb := range m.Thinking {
				blocks = append(blocks, anthropic.NewThinkingBlock(tb.Signature, tb.Text))
			}
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, argsToAnthropicInput(tc.Args), tc.Name))
			}
			amsgs = append(amsgs, anthropic.NewAssistantMessage(blocks...))
		case "tool":
			toolResults = append(toolResults, anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false))
		}
	}
	flush()
	return system, amsgs
}

func (anthropicLLM) ChatStream(ctx context.Context, cc ChatCfg, msgs []Msg, tools []ToolDef, onDelta func(kind, text string)) (ChatResult, error) {
	client := anthropic.NewClient(
		option.WithBaseURL(normalizeAnthropicBaseURL(cc.BaseURL)),
		// x-api-key header — the auth style used by both AWS Bedrock Mantle's
		// Anthropic Messages surface (base_url .../anthropic, key = a Bedrock
		// API key) and native api.anthropic.com. The SDK also sets the required
		// anthropic-version header automatically.
		option.WithAPIKey(cc.APIKey),
	)

	// Build system + messages. System is a top-level param, not a message.
	system, amsgs := anthropicMessagesFromMsgs(msgs)

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
		sys := anthropic.TextBlockParam{Text: system}
		// Prompt caching (on unless disabled): a breakpoint on the last
		// system block caches tools + system together — the stable prefix
		// (schema card included) that the loop resends every iteration.
		if !cc.PromptCacheDisabled {
			sys.CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
		params.System = []anthropic.TextBlockParam{sys}
	}
	if cc.Thinking == "adaptive" {
		// Summarized display so reasoning can stream to the chat UI; the
		// default ("omitted") returns empty thinking text.
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{
				Display: anthropic.ThinkingConfigAdaptiveDisplaySummarized,
			},
		}
	}
	if cc.Effort != "" {
		params.OutputConfig = anthropic.OutputConfigParam{Effort: anthropic.OutputConfigEffort(cc.Effort)}
	}

	stream := client.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	msg := anthropic.Message{}
	for stream.Next() {
		event := stream.Current()
		_ = msg.Accumulate(event)
		if d, ok := event.AsAny().(anthropic.ContentBlockDeltaEvent); ok && onDelta != nil {
			switch delta := d.Delta.AsAny().(type) {
			case anthropic.TextDelta:
				if delta.Text != "" {
					onDelta("token", delta.Text)
				}
			case anthropic.ThinkingDelta:
				if delta.Thinking != "" {
					onDelta("thinking", delta.Thinking)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return ChatResult{}, err
	}

	res := ChatResult{
		TokensIn:         int(msg.Usage.InputTokens),
		TokensOut:        int(msg.Usage.OutputTokens),
		CacheReadTokens:  int(msg.Usage.CacheReadInputTokens),
		CacheWriteTokens: int(msg.Usage.CacheCreationInputTokens),
	}
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.ThinkingBlock:
			res.Thinking = append(res.Thinking, ThinkingBlock{Text: b.Thinking, Signature: b.Signature})
		case anthropic.TextBlock:
			res.Content += b.Text
		case anthropic.ToolUseBlock:
			// b.Input is json.RawMessage (already a JSON object); it
			// implements json.Marshaler so anthropicInputToArgs re-emits
			// it verbatim without a decode/re-encode round trip. An
			// empty/absent/null input must still come back as "{}", not "null".
			args := "{}"
			if len(b.Input) > 0 && string(b.Input) != "null" {
				args = anthropicInputToArgs(b.Input)
			}
			res.ToolCalls = append(res.ToolCalls, ToolCall{
				ID: b.ID, Name: b.Name, Args: args,
			})
		}
	}
	return res, nil
}
