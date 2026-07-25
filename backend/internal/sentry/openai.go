package sentry

import (
	"context"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// openaiLLM is an LLM backed by any OpenAI-compatible chat completions
// endpoint (via openai-go), resolved per-call from ChatCfg.BaseURL/APIKey.
type openaiLLM struct{}

// NewOpenAILLM returns an LLM implementation that talks to an
// OpenAI-compatible /chat/completions endpoint.
func NewOpenAILLM() LLM { return openaiLLM{} }

func (openaiLLM) ChatStream(ctx context.Context, cc ChatCfg, msgs []Msg, tools []ToolDef, onToken func(string)) (ChatResult, error) {
	client := openai.NewClient(option.WithBaseURL(cc.BaseURL), option.WithAPIKey(cc.APIKey))

	oaMsgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "system":
			oaMsgs = append(oaMsgs, openai.SystemMessage(m.Content))
		case "user":
			oaMsgs = append(oaMsgs, openai.UserMessage(m.Content))
		case "tool":
			oaMsgs = append(oaMsgs, openai.ToolMessage(m.Content, m.ToolCallID))
		case "assistant":
			am := openai.ChatCompletionAssistantMessageParam{}
			if m.Content != "" {
				am.Content.OfString = openai.String(m.Content)
			}
			for _, tc := range m.ToolCalls {
				am.ToolCalls = append(am.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID: tc.ID,
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      tc.Name,
							Arguments: tc.Args,
						},
					},
				})
			}
			oaMsgs = append(oaMsgs, openai.ChatCompletionMessageParamUnion{OfAssistant: &am})
		}
	}

	oaTools := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		oaTools = append(oaTools, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
			Name:        t.Name,
			Description: openai.String(t.Description),
			Parameters:  openai.FunctionParameters(t.Params),
		}))
	}

	stream := client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{
		Model:               cc.Model,
		Messages:            oaMsgs,
		Tools:               oaTools,
		MaxCompletionTokens: openai.Int(int64(cc.MaxTokens)),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
	})
	defer stream.Close()

	acc := openai.ChatCompletionAccumulator{}
	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" && onToken != nil {
			onToken(chunk.Choices[0].Delta.Content)
		}
	}
	if err := stream.Err(); err != nil {
		return ChatResult{}, err
	}

	res := ChatResult{
		TokensIn:  int(acc.Usage.PromptTokens),
		TokensOut: int(acc.Usage.CompletionTokens),
	}
	if len(acc.Choices) > 0 {
		res.Content = acc.Choices[0].Message.Content
		for _, tc := range acc.Choices[0].Message.ToolCalls {
			res.ToolCalls = append(res.ToolCalls, ToolCall{
				ID: tc.ID, Name: tc.Function.Name, Args: tc.Function.Arguments,
			})
		}
	}
	return res, nil
}
