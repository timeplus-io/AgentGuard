# Sentry Anthropic Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Sentry drive Claude/Anthropic models on AWS Bedrock (Mantle) by adding an `api_type` toggle on the chat config row + an `anthropic-sdk-go` client behind the existing `sentry.LLM` interface, selected by a factory.

**Architecture:** The `chat` config row gains `api_type` (`openai`|`anthropic`). A `NewLLM(cc ChatCfg) LLM` factory returns the existing OpenAI client or a new `anthropicLLM` (Anthropic Messages API over a Bearer endpoint). The Anthropic client translates the loop's OpenAI-shaped `Msg`/`ToolDef`/`ChatResult` to/from Anthropic Messages, keeping `ChatResult` byte-identical (tool args as JSON strings) — so `loop.go`, the store, and replay are unchanged. Spec: `docs/superpowers/specs/2026-07-24-sentry-anthropic-provider-design.md`. Branch: `feature/207-sentry-agent-design`. Issue #207.

**Tech Stack:** Go + Gin, `openai-go/v3` (existing), `anthropic-sdk-go` (new), React 18 + Vite, Python stdlib mock model.

## Global Constraints

- Go builds/tests from `backend/` with `CGO_ENABLED=0`; gofmt-clean; "Timeplus Enterprise" never "Proton".
- The `sentry.LLM` interface, `Msg`/`ToolCall`/`ToolDef`/`ChatResult` types are UNCHANGED — the loop/store/replay must not be touched.
- `ChatResult.ToolCalls[i].Args` is a JSON **string** in both providers (the store + replay depend on it).
- `api_type` unknown/empty ⇒ treated as `"openai"`. Default everywhere is `"openai"`.
- Both providers share the same `chat` row `base_url`/`api_key`/`model`; the Anthropic client normalizes the base URL (strips a trailing `/v1`) so the SAME configured URL works for both.
- `ChatStream` must tolerate a nil `onToken` (the test-connection probe passes nil) — guard before calling it, like the OpenAI client does.
- Anthropic auth is Bearer (`option.WithAuthToken`), never x-api-key (out of scope).
- New config key needs full five-point wiring (struct; SetDefault+read+BindEnv+Save; registry+EnvVar; sources EffectiveValue+ApplyChange; save-roundtrip test). `api_key`-style secrets are never written to Save() — `api_type` is NOT a secret, so it IS written to Save().
- Frontend: design tokens; no borders-for-sectioning; no shadows; radius ≤4px.
- Commit after each task on the current branch.

---

### Task 1: Config — `api_type` on the chat row + `sentry.chat_api_type` boot key

**Files:**
- Modify: `backend/internal/config/config.go` (SentryConfig struct + SetDefault/read/BindEnv/Save)
- Modify: `backend/internal/config/registry.go` (one entry)
- Modify: `backend/internal/config/sources.go` (EffectiveValue + ApplyChange)
- Modify: `backend/internal/config/config_test.go` (roundtrip), `registry_test.go` (key list)
- Modify: `backend/internal/timeplus/sentry_config_store.go` (`DefaultSentryChatRow` emits `api_type`)
- Modify: `backend/internal/timeplus/sentry_config_store_test.go` (assert `api_type`)
- Modify: `backend/internal/sentry/llm.go` (`ChatCfg.APIType` + `applyDefaults`)
- Modify: `backend/internal/sentry/llm_test.go` (applyDefaults default)

**Interfaces:**
- Produces: `config.SentryConfig.ChatAPIType string \`mapstructure:"chat_api_type"\``; viper key `sentry.chat_api_type` (default `"openai"`); `DefaultSentryChatRow` JSON includes `"api_type"`; `ChatCfg.APIType string \`json:"api_type"\`` defaulted to `"openai"`.

- [ ] **Step 1: Config wiring.** In `config.go`, add to `SentryConfig` (after `ChatModel`):
```go
	ChatAPIType string `mapstructure:"chat_api_type"` // "openai" | "anthropic"
```
Add `v.SetDefault("sentry.chat_api_type", "openai")` (next to the other sentry.chat_* defaults); `cfg.Sentry.ChatAPIType = v.GetString("sentry.chat_api_type")` (in the read block); `_ = v.BindEnv("sentry.chat_api_type", "SENTRY_CHAT_API_TYPE")`; and in `Save()` `v.Set("sentry.chat_api_type", cfg.Sentry.ChatAPIType)` (it is NOT a secret, so it IS saved).

- [ ] **Step 2: Registry + sources.** In `registry.go`, next to the `sentry.chat_model` entry:
```go
		{Key: "sentry.chat_api_type", Section: "sentry", Label: "Sentry chat API type",
			Description: "LLM API dialect for the Sentry chat model: 'openai' (Chat Completions) or 'anthropic' (Anthropic Messages, e.g. Claude on Bedrock).", Type: TypeString, EnvVar: "SENTRY_CHAT_API_TYPE"},
```
In `sources.go`, add to BOTH switches (next to `sentry.chat_model`):
```go
	// EffectiveValue:
	case "sentry.chat_api_type":
		return cfg.Sentry.ChatAPIType
	// ApplyChange:
	case "sentry.chat_api_type":
		return setString(&cfg.Sentry.ChatAPIType, value)
```

- [ ] **Step 3: Store default row.** In `sentry_config_store.go` `DefaultSentryChatRow`, add `api_type` to the marshaled map (default `"openai"` when empty):
```go
	apiType := sCfg.ChatAPIType
	if apiType == "" {
		apiType = "openai"
	}
	b, _ := json.Marshal(map[string]any{
		"api_type":              apiType,
		"base_url":              sCfg.ChatBaseURL,
		"api_key":               sCfg.ChatAPIKey,
		"model":                 sCfg.ChatModel,
		"timeout":               timeout,
		"max_tokens":            maxTokens,
		"max_iterations":        12,
		"turn_token_budget":     32000,
		"sql_timeout_ms":        30000,
		"sql_max_rows":          1000,
		"tool_result_max_bytes": 16384,
	})
```

- [ ] **Step 4: ChatCfg field.** In `llm.go`, add to `ChatCfg` (after `Model`):
```go
	APIType string `json:"api_type"`
```
In `applyDefaults`, add (near the SystemPrompt default):
```go
	if c.APIType == "" {
		c.APIType = "openai"
	}
```

- [ ] **Step 5: Tests (TDD — write first, watch fail, then the impl above makes them pass).**
  - `sentry_config_store_test.go` — extend `TestSentryConfigRowsSeedsChatAndMemory` (or the chat-row test): assert the `chat` row JSON has `api_type == "openai"` by default, and `== "anthropic"` when `config.SentryConfig{ChatAPIType:"anthropic"}`.
  - `llm_test.go` — add:
    ```go
    func TestChatCfgAPITypeDefaultsOpenAI(t *testing.T) {
    	cc := ChatCfg{}
    	cc.applyDefaults()
    	if cc.APIType != "openai" {
    		t.Fatalf("api_type default = %q, want openai", cc.APIType)
    	}
    	cc2 := ChatCfg{APIType: "anthropic"}
    	cc2.applyDefaults()
    	if cc2.APIType != "anthropic" {
    		t.Fatal("api_type must be preserved")
    	}
    }
    ```
  - `config_test.go` `TestSave_RoundTrip` — set `ChatAPIType:"anthropic"`, assert it survives save→load.
  - `registry_test.go` — add `sentry.chat_api_type` to the `want` key list.

- [ ] **Step 6: Verify + commit**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/config/... ./internal/timeplus/... ./internal/sentry/...`
Expected: PASS. Commit: `feat(sentry): add chat api_type config (openai | anthropic)`.

---

### Task 2: `anthropicLLM` client + `NewLLM` factory

**Files:**
- Modify: `backend/go.mod` (new dep)
- Create: `backend/internal/sentry/anthropic.go`
- Create: `backend/internal/sentry/anthropic_test.go`
- Modify: `backend/internal/sentry/llm.go` (add `NewLLM` factory)
- Modify: `backend/internal/sentry/llm_test.go` (factory-selection test)

**Interfaces:**
- Consumes: `ChatCfg.APIType` (Task 1); the `LLM` interface, `Msg`/`ToolCall`/`ToolDef`/`ChatResult` (unchanged).
- Produces: `NewLLM(cc ChatCfg) LLM`; `anthropicLLM` (implements `ChatStream`); `normalizeAnthropicBaseURL(string) string`; pure translation helpers `anthropicMessagesFromMsgs([]Msg) (system string, msgs []<sdk msg param>)` and `toolCallsFromAnthropic(<sdk content blocks>) []ToolCall` (exact SDK types resolved in Step 1).

- [ ] **Step 1: Add the dependency + verify its surface.**

Run: `cd backend && go get github.com/anthropics/anthropic-sdk-go@latest && go mod tidy`
Then confirm the EXACT type/method names before coding (the SDK surface below is the intended shape; adapt to the module's real API):
```
go doc github.com/anthropics/anthropic-sdk-go | head -60
go doc github.com/anthropics/anthropic-sdk-go Client.Messages
go doc github.com/anthropics/anthropic-sdk-go MessageNewParams
go doc github.com/anthropics/anthropic-sdk-go.option WithAuthToken
go doc github.com/anthropics/anthropic-sdk-go MessageParam
go doc github.com/anthropics/anthropic-sdk-go ToolParam
go doc github.com/anthropics/anthropic-sdk-go ToolUseBlock
go doc github.com/anthropics/anthropic-sdk-go Client.Messages.NewStreaming
```
Record the real names in the report. If the module path or streaming/tool types differ from the code below, adapt the implementation to the real API — the SEMANTICS in Step 3 are binding, the exact SDK identifiers are not. If streaming (`NewStreaming`) is absent or hard to reconcile, a non-streaming `Messages.New` call is acceptable for v1 (call `onToken` once with the full text) — note the choice in the report.

- [ ] **Step 2: Failing tests** (`anthropic_test.go` — pure helpers, no network):
```go
package sentry

import (
	"encoding/json"
	"testing"
)

func TestNormalizeAnthropicBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://bedrock-mantle.us-west-2.api.aws/v1":  "https://bedrock-mantle.us-west-2.api.aws",
		"https://bedrock-mantle.us-west-2.api.aws/v1/": "https://bedrock-mantle.us-west-2.api.aws",
		"https://api.anthropic.com":                    "https://api.anthropic.com",
		"https://x/v1/foo":                             "https://x/v1/foo", // only a TRAILING /v1 is stripped
	}
	for in, want := range cases {
		if got := normalizeAnthropicBaseURL(in); got != want {
			t.Fatalf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnthropicToolArgsRoundTripToJSONString(t *testing.T) {
	// A tool_use input object must come back as a compact JSON string on ChatResult.
	in := map[string]any{"a": 1.0, "b": "x"}
	got := anthropicInputToArgs(in) // helper: object -> JSON string
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatalf("Args not valid JSON: %v (%q)", err, got)
	}
	if back["a"] != 1.0 || back["b"] != "x" {
		t.Fatalf("round-trip lost data: %v", back)
	}
}

func TestArgsToAnthropicInputParsesJSONString(t *testing.T) {
	// An assistant ToolCall.Args (JSON string) must become an object for tool_use.
	obj := argsToAnthropicInput(`{"k":"v"}`) // helper: JSON string -> map
	if obj["k"] != "v" {
		t.Fatalf("parse failed: %v", obj)
	}
	// Invalid/empty JSON must yield an empty object, not panic.
	if m := argsToAnthropicInput(""); len(m) != 0 {
		t.Fatalf("empty args should be empty object, got %v", m)
	}
}

func TestNewLLMFactory(t *testing.T) {
	if _, ok := NewLLM(ChatCfg{APIType: "anthropic"}).(anthropicLLM); !ok {
		t.Fatal("api_type anthropic must select anthropicLLM")
	}
	if _, ok := NewLLM(ChatCfg{APIType: "openai"}).(openaiLLM); !ok {
		t.Fatal("api_type openai must select openaiLLM")
	}
	if _, ok := NewLLM(ChatCfg{APIType: ""}).(openaiLLM); !ok {
		t.Fatal("empty api_type must default to openaiLLM")
	}
}
```

- [ ] **Step 3: Implement `anthropic.go`.** Structure (adapt SDK identifiers to Step 1's `go doc` output):
```go
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
			InputSchema: anthropic.ToolInputSchemaParam{Properties: t.Params["properties"]},
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
			res.ToolCalls = append(res.ToolCalls, ToolCall{
				ID: b.ID, Name: b.Name, Args: anthropicInputToArgs(b.Input),
			})
		}
	}
	return res, nil
}
```
> The `anthropic.New*`/`AsAny`/`Accumulate` identifiers above are the SDK's documented streaming-accumulator pattern; if the installed version differs, adapt using Step 1's `go doc`. The InputSchema construction (`t.Params["properties"]`) assumes `ToolDef.Params` is a JSON-schema object with a `properties` key (it is — the skills build `{"type":"object","properties":{…}}`); pass the whole schema if the SDK's `InputSchema` accepts a raw object.

- [ ] **Step 4: Add the factory** to `llm.go`:
```go
// NewLLM returns the LLM implementation for the configured api_type.
// Unknown/empty api_type defaults to the OpenAI-compatible client.
func NewLLM(cc ChatCfg) LLM {
	if cc.APIType == "anthropic" {
		return anthropicLLM{}
	}
	return openaiLLM{}
}
```
Add the `TestNewLLMFactory` to `llm_test.go` (or keep it in anthropic_test.go per Step 2).

- [ ] **Step 5: Verify + commit**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/sentry/...`
Expected: PASS (pure-helper + factory tests; the network `ChatStream` path is integration-covered in Task 5). Commit: `feat(sentry): Anthropic Messages API client + LLM factory`.

---

### Task 3: Wire the factory into the chat handler + unified test-connection probe

**Files:**
- Modify: `backend/internal/handlers/sentry.go:128` (chat handler uses the factory)
- Modify: `backend/internal/handlers/sentry_config.go` (`sentryTestBody` + `resolveSentryChatCreds` gain api_type; `TestSentryConfig` chat probe goes through the factory)
- Modify: `backend/internal/handlers/sentry_config_test.go` (resolver api_type precedence)

**Interfaces:**
- Consumes: `sentry.NewLLM`, `sentry.ChatCfg`, `resolveSentryChatCreds`.
- Produces: chat handler builds `sentry.Loop{LLM: sentry.NewLLM(cc), ...}`; test-connection probes via `NewLLM`.

- [ ] **Step 1: Chat handler.** In `sentry.go`, the handler already has `cc` from `LoadChatCfg` before constructing the loop. Change:
```go
	loop := &sentry.Loop{LLM: sentry.NewLLM(cc), Reg: sentryRegistry(rcfg), Store: store, Cfg: rcfg}
```
(Confirm the local var name for the loaded ChatCfg — it's the value passed to `loop.Run`; reuse it. If the loop is built before `LoadChatCfg`, move the `NewLLM(cc)` construction after the cfg load.)

- [ ] **Step 2: Resolver + body api_type.** In `sentry_config.go`:
  - `sentryTestBody` gains `ChatAPIType string \`json:"chat_api_type"\``.
  - `resolvedChatCreds` gains `APIType string`.
  - `resolveSentryChatCreds` resolves it (body → boot `ChatAPIType` → stored `chat` row `api_type`), and the stored-struct gains `APIType string \`json:"api_type"\``:
    ```go
    Model:   orStr(body.ChatModel, orStr(boot.ChatModel, stored.Model)),
    APIType: orStr(body.ChatAPIType, orStr(boot.ChatAPIType, stored.APIType)),
    ```

- [ ] **Step 3: Unified probe.** Replace the OpenAI-only `sentryCheckChatToolCalling` raw POST with a factory-based probe so it validates whichever provider is configured. Add a helper in `sentry_config.go`:
```go
// sentryProbeChat runs a minimal tool-forcing turn through the configured
// provider and reports whether a tool_call came back. Never returns an error to
// the caller — failures become {ok:false, error:...} for the checks payload.
func sentryProbeChat(ctx context.Context, chat resolvedChatCreds, maxTokens int) gin.H {
	cc := sentry.ChatCfg{
		APIType: chat.APIType, BaseURL: chat.BaseURL, APIKey: chat.APIKey,
		Model: chat.Model, MaxTokens: maxTokens,
	}
	llm := sentry.NewLLM(cc)
	res, err := llm.ChatStream(ctx, cc,
		[]sentry.Msg{{Role: "user", Content: "What is the current time? You must call the get_current_time tool to answer."}},
		[]sentry.ToolDef{{Name: "get_current_time", Description: "Returns the current UTC time.",
			Params: map[string]any{"type": "object", "properties": map[string]any{}}}},
		nil)
	if err != nil {
		return gin.H{"ok": false, "tool_calling": false, "error": err.Error()}
	}
	if len(res.ToolCalls) == 0 {
		return gin.H{"ok": false, "tool_calling": false, "error": "model did not return tool_calls"}
	}
	return gin.H{"ok": true, "tool_calling": true, "tool_call_name": res.ToolCalls[0].Name}
}
```
In `TestSentryConfig`, after the `reachable` check passes, call `checks["chat"] = sentryProbeChat(c.Request.Context(), chat, maxTokens)` (where `chat` is the `resolveSentryChatCreds(...)` result and `maxTokens` a small cap like 256). Keep the `GET /models` reachability pre-check unchanged. Remove `sentryCheckChatToolCalling` if now unused. `maxTokens` for the probe: use a small fixed value (e.g. 256) or `s.MaxOutputTokens`.
> Note: the probe's `ChatStream(..., nil)` passes a nil onToken — both clients must guard it (constraint). The Anthropic client's `MaxTokens` is required, so `maxTokens` must be > 0 (256 is fine).

- [ ] **Step 4: Resolver test.** In `sentry_config_test.go`, extend the chat-creds precedence test to cover `APIType` (body wins; else boot; else stored `chat` row `api_type`):
```go
func TestResolveSentryChatCredsAPIType(t *testing.T) {
	got := resolveSentryChatCreds(sentryTestBody{}, config.SentryConfig{ChatAPIType: ""},
		`{"api_type":"anthropic","base_url":"http://x/v1","model":"m"}`)
	if got.APIType != "anthropic" {
		t.Fatalf("stored api_type fallback failed: %+v", got)
	}
	got2 := resolveSentryChatCreds(sentryTestBody{ChatAPIType: "openai"},
		config.SentryConfig{ChatAPIType: "anthropic"}, `{"api_type":"anthropic"}`)
	if got2.APIType != "openai" {
		t.Fatal("body api_type should win")
	}
}
```

- [ ] **Step 5: Verify + commit**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/handlers/ -run Sentry ./internal/sentry/...`
Expected: PASS. Commit: `feat(sentry): select provider via factory in chat handler + test-connection`.

---

### Task 4: Frontend — Chat panel `api_type` selector

**Files:**
- Modify: `frontend/src/components/SentryConfigEditor.jsx`

**Interfaces:**
- Consumes: `GET /api/sentry/config` `chat` row now carries `api_type`; `PUT /api/sentry/config/chat` accepts it; `POST /api/sentry/config/test` accepts `chat_api_type`.

- [ ] **Step 1: Add api_type to draft + save.** Read the file. In the chat-panel draft init, add `api_type: chat.api_type || 'openai'`. In the chat panel's `buildValue`/save payload, include `api_type: draft.chat.api_type`. In the test-connection payload, add `chat_api_type: draft.chat.api_type`.

- [ ] **Step 2: Add the selector UI** at the top of the Chat panel (before base_url), design-tokens only:
```jsx
<label className="block text-xs text-gray-400 mb-1">API type</label>
<select
  value={draft.chat.api_type}
  onChange={(e) => setChat({ api_type: e.target.value })}
  className="w-full rounded-sm bg-gray-900 text-gray-100 text-sm px-2 py-1.5 mb-3"
>
  <option value="openai">OpenAI-compatible (Chat Completions)</option>
  <option value="anthropic">Anthropic (Claude, incl. Bedrock)</option>
</select>
{draft.chat.api_type === 'anthropic' && (
  <p className="text-[11px] text-gray-500 mb-3">
    For AWS Bedrock: base URL <code>https://bedrock-mantle.&lt;region&gt;.api.aws/v1</code>,
    API key = your Bedrock API key, model = a Claude inference-profile id
    (e.g. <code>us.anthropic.claude-…</code>). Tool calling is required.
  </p>
)}
```
(Match the existing panel's exact class tokens / `setChat` helper name — read the file first; adapt if the helper is named differently.)

- [ ] **Step 3: Verify + commit**

Run: `cd frontend && npm run lint && npm run build`
Expected: clean. Commit: `feat(sentry): chat API-type selector in the config editor`.

---

### Task 5: Mock model `/v1/messages`, integration test, docs

**Files:**
- Modify: `backend/scripts/dlp_mock_model.py` (add `POST /v1/messages`)
- Modify: `backend/scripts/sentry_integration.sh` (anthropic case)
- Modify: `docs/sentry.md`, `CLAUDE.md`

- [ ] **Step 1: Mock `/v1/messages`.** Read `dlp_mock_model.py`. Add a handler branch for `POST` paths ending `/messages`: when the request body has a non-empty `tools`, return an Anthropic-shaped response with a `tool_use` content block; else a plain text response. Minimal non-stream JSON is enough IF the Anthropic client can be pointed at a non-stream call — but the client uses streaming (`NewStreaming`), so the mock must return an SSE stream of Messages events. Implement a minimal SSE: emit `message_start`, a `content_block_start` with a `tool_use` block, a `content_block_stop`, `message_delta` (stop_reason `tool_use`, usage), `message_stop` — `data: {json}\n\n` per event, `Content-Type: text/event-stream`. Model the exact event JSON on the Anthropic Messages streaming format (the implementer should confirm the minimal event set the SDK's stream accumulator requires from Task 2's `go doc`, and shape the mock to satisfy it). Keep the existing `/v1/chat/completions` + `/v1/embeddings` + `/v1/models` handlers byte-identical.
  - If, in Task 2, the implementer chose a NON-streaming Anthropic call, then the mock only needs a non-stream JSON `/v1/messages` returning `{content:[{type:"tool_use",id,name,input}], usage:{...}, stop_reason:"tool_use"}` — simpler. Match whichever Task 2 chose (read task-2 report / the anthropic.go implementation).
- [ ] **Step 2: Smoke the mock** locally:
```bash
python3 backend/scripts/dlp_mock_model.py 4055 8 &
curl -sN localhost:4055/v1/messages -d '{"model":"mock-claude","max_tokens":256,"tools":[{"name":"get_current_time"}],"messages":[{"role":"user","content":"time?"}]}'
kill %1
```
Expected: an SSE stream (or JSON) containing a `tool_use` with `name":"get_current_time"`. Confirm `/v1/chat/completions` non-stream still returns the old shape.
- [ ] **Step 3: Integration case.** In `sentry_integration.sh`, after the existing openai chat test, add an anthropic case: `PUT /api/sentry/config/chat` with `{"value":{"api_type":"anthropic","base_url":"http://localhost:$MOCK/v1","api_key":"x","model":"mock-claude","timeout":60}}`, then `POST /api/sentry/config/test` with `{"base_url":"http://localhost:$MOCK/v1","api_key":"x","chat_model":"mock-claude","chat_api_type":"anthropic","memory_enabled":false}` and assert the response has `"tool_calling":true`. Keep the existing openai + round-trip + replay assertions. Run `make sentry-integration` live → PASS. If no live Timeplus here, make it correct-by-construction and report where it stopped — do NOT fake a pass.
- [ ] **Step 4: Docs.** `docs/sentry.md`: add a **"Claude on AWS Bedrock (Anthropic API type)"** subsection — set the Chat panel API type to Anthropic, base URL `https://bedrock-mantle.<region>.api.aws/v1` (region-gated: `us-east-1`/`us-west-2`, **not** `us-west-1`), API key = a long-term Bedrock API key, model = a Claude inference-profile id; tool calling required; same base URL works for both api types. Document the `api_type` chat field + `sentry.chat_api_type` boot key. `CLAUDE.md`: note the Sentry LLM layer now has OpenAI + Anthropic providers selected by the chat row's `api_type` (Anthropic via `anthropic-sdk-go` over a Bearer endpoint).
- [ ] **Step 5: Full verification**

```bash
cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./... \
  && cd ../frontend && npm run lint && npm run build
```
Expected: all green. Commit: `feat(sentry): anthropic mock + integration case + docs`.

---

## Self-Review Notes (resolved inline)

- Spec coverage: §2 api_type config → Task 1; §3 factory → Task 2; §4 anthropicLLM → Task 2; §5 test-connection → Task 3; §6 frontend → Task 4; §7 dep/mock/tests → Tasks 2+5; §8 docs → Task 5. All covered.
- SDK-surface risk: Task 2 Step 1 verifies `anthropic-sdk-go`'s real identifiers via `go doc` before coding (mirrors how the openai-go client was built); the streaming-vs-nonstreaming choice is explicitly allowed to flex, with the mock (Task 5) told to match whichever was chosen.
- Loop/store/replay untouched: `ChatResult.ToolCalls.Args` stays a JSON string (Task 2 `anthropicInputToArgs`); the `LLM` interface + `Msg`/`ToolDef`/`ChatResult` are unchanged; only the handler swaps `NewOpenAILLM()`→`NewLLM(cc)`.
- nil onToken: constraint + Task 2 code guards it; Task 3's probe passes nil.
- Type consistency: `ChatCfg.APIType` (Task 1) ↔ `NewLLM` (Task 2) ↔ `resolvedChatCreds.APIType`/`sentryTestBody.ChatAPIType` (Task 3) ↔ `chat_api_type` frontend (Task 4) ↔ `api_type` mock/integration (Task 5). Consistent.
- Secret handling: `api_type` is NOT a secret → written to Save() (unlike the api_key keys). Stated in Global Constraints + Task 1 Step 1.
