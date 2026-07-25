# Sentry — Native Anthropic Messages API Provider (Claude on AWS Bedrock)

> **Status:** Approved design
> **Issue:** [#207](https://github.com/timeplus-io/AgentGuard/issues/207); on branch `feature/207-sentry-agent-design` / PR [#208](https://github.com/timeplus-io/AgentGuard/pull/208)
> **Date:** 2026-07-24
> **Builds on:** `2026-07-23-sentry-config-reshape-design.md` (the two-row `chat`/`memory` config).

## 1. Goal

Let Sentry drive **Claude / Anthropic models hosted on AWS Bedrock** (and any Anthropic-compatible endpoint) directly. Today Sentry's LLM client speaks only the OpenAI Chat Completions API (`/v1/chat/completions`), but Claude on Bedrock is served through the **Anthropic Messages API** (`/v1/messages`) — a different wire format. This adds a native Anthropic provider selectable per-config, so a Bedrock user points the chat endpoint at Bedrock Mantle with a Bearer (Bedrock API key) and runs Claude with tool calling — no gateway, no extra process.

Decisions settled with the user (brainstorming):
- **Official `anthropic-sdk-go`** for the client (handles Messages streaming, `tool_use`/`tool_result`, retries), wrapped behind the existing `sentry.LLM` interface.
- **Bearer-token endpoints** (Bedrock Mantle + compatible): the chat row's `api_key` is sent as a Bearer token. Direct `api.anthropic.com` (x-api-key) and native Bedrock SigV4 are out of scope.

## 2. Config — `api_type` on the `chat` row

The `chat` row gains one field:

```json
{"api_type": "openai", "base_url": "...", "api_key": "...", "model": "...", "timeout": 60,
 "max_tokens": 1024, "max_iterations": 12, "turn_token_budget": 32000,
 "sql_timeout_ms": 30000, "sql_max_rows": 1000, "tool_result_max_bytes": 16384}
```

- `api_type`: `"openai"` (default) | `"anthropic"`. Any other/empty value is treated as `"openai"`.
- Boot default `sentry.chat_api_type` (default `"openai"`), full five-point wiring (struct field `config.SentryConfig.ChatAPIType`; `SetDefault`+read+`BindEnv SENTRY_CHAT_API_TYPE`+`Save` `v.Set`; registry entry `TypeString`, EnvVar; `sources.go` `EffectiveValue`/`ApplyChange`; save-roundtrip test). `SentryConfigRows`/`DefaultSentryChatRow` emit `api_type` from `sCfg.ChatAPIType` (falling back to `"openai"` when empty).
- `ChatCfg` (`internal/sentry/llm.go`) gains `APIType string \`json:"api_type"\``. `applyDefaults` sets it to `"openai"` when empty.

No new stream/DDL; `memory` row unchanged. base_url/api_key/model/budgets are shared by both providers.

## 3. LLM factory

`internal/sentry/llm.go` gains:
```go
func NewLLM(cc ChatCfg) LLM {
	if cc.APIType == "anthropic" {
		return anthropicLLM{}
	}
	return openaiLLM{}
}
```
Callers switch from `NewOpenAILLM()` to `NewLLM(cc)`:
- `internal/handlers/sentry.go:128` (the chat handler) — it already loads `cc` via `LoadChatCfg` before building the loop, so `sentry.Loop{LLM: sentry.NewLLM(cc), ...}`.
- `TestSentryConfig` (see §5).

The `LLM` interface — `ChatStream(ctx, cc ChatCfg, msgs []Msg, tools []ToolDef, onToken func(string)) (ChatResult, error)` — and the `Msg`/`ToolCall`/`ToolDef`/`ChatResult` types are **unchanged**. Therefore `loop.go`, the conversation store, and replay need no changes. `NewOpenAILLM()` stays (used by existing tests) or is kept as a thin alias; the factory is the new entry point.

## 4. `anthropicLLM` (new `internal/sentry/anthropic.go`)

Implements `ChatStream` using `github.com/anthropics/anthropic-sdk-go`:
```go
client := anthropic.NewClient(
	option.WithBaseURL(normalizeAnthropicBaseURL(cc.BaseURL)),
	option.WithAuthToken(cc.APIKey), // Authorization: Bearer <key> — Bedrock Mantle
)
```

**Base-URL normalization** (`normalizeAnthropicBaseURL`): the SDK appends `/v1/messages`, so strip exactly one trailing `/v1` (and trailing `/`) from `cc.BaseURL`. This means the **same** configured URL (`https://bedrock-mantle.us-west-2.api.aws/v1`) works for both api types — the OpenAI client keeps `/v1` (openai-go appends `/chat/completions`), the Anthropic client strips it. Unit-tested.

**Translation** (the loop's OpenAI-shaped types ↔ Anthropic Messages):
- **System**: the `role:"system"` Msg's `Content` → the request's top-level `System` param (not a message).
- **User/assistant messages**: `role:"user"`/`"assistant"` → `MessageParam` with a text block for non-empty `Content`.
- **Assistant tool calls**: each `Msg.ToolCalls[i]` (assistant) → a `tool_use` content block `{id, name, input}` where `input` is `Args` (a JSON string) parsed into an object.
- **Tool results**: each `role:"tool"` Msg → a `tool_result` content block `{tool_use_id: ToolCallID, content: Content}` carried in a `user` message (Anthropic requires tool_results in a user turn). Consecutive tool results are grouped into one user message where the transcript has them adjacent.
- **Tools**: `ToolDef{Name, Description, Params}` → `ToolParam{Name, Description, InputSchema: Params}`.
- **max_tokens**: required by Anthropic — use `cc.MaxTokens` (already defaulted/clamped).
- **temperature**: 0 for determinism (matches the OpenAI test-probe intent; the loop doesn't set temperature today, so leave the loop path at the SDK default — only the probe forces 0. Keep parity: do not introduce a temperature field on ChatCfg).

**Streaming**: use the SDK's streaming API; accumulate text deltas → `onToken(text)`; collect `tool_use` blocks → `ChatResult.ToolCalls` where `Args` is the tool_use `input` **re-serialized to a JSON string** (so `ChatResult` is byte-for-byte the same shape the OpenAI path and the store/replay already use). Populate `ChatResult.Content` (final assistant text), `TokensIn`/`TokensOut` from the message `usage`.

**Errors**: return the SDK error as-is (the loop already emits an `error` event and the handler surfaces it). `anthropic-version` header is set by the SDK; confirm at build time it is accepted by Bedrock Mantle's Anthropic surface (Mantle is Anthropic-compatible; if a Bedrock-specific version string is required, set it via `option.WithHeader("anthropic-version", …)` — resolve during implementation and note in the report).

## 5. Test-connection (`TestSentryConfig`)

Rewrite the chat tool-calling probe to go through the factory rather than a raw OpenAI POST, so it validates **whichever provider is configured**:
```go
cc := chatCfgFromResolved(resolveSentryChatCreds(...)) // base_url, api_key, model, api_type, small max_tokens
llm := sentry.NewLLM(cc)
res, err := llm.ChatStream(ctx, cc, []Msg{{Role:"user", Content:"What is the current time? You must call get_current_time."}},
	[]ToolDef{{Name:"get_current_time", Description:"Returns current UTC time.", Params: emptyObjectSchema}}, nil)
// ok = err==nil && len(res.ToolCalls) > 0
```
- `sentryTestBody` gains `chat_api_type` (string). `resolveSentryChatCreds`/`resolvedChatCreds` gain `APIType` (body → boot `ChatAPIType` → stored `chat` row `api_type`).
- Reachability: keep the existing `GET <base_url>/models` check (Mantle serves `/v1/models` for both surfaces); it stays a fast pre-check. The chat check then uses the factory as above.
- Response shape unchanged: `{ok, checks:{reachable, chat:{ok, tool_calling, tool_call_name?, error?}, embedding:{…}|{skipped:true}}}`. Never 5xx on unreachable (the factory call's error becomes `chat.error`).
- This removes the OpenAI-only `sentryCheckChatToolCalling` raw POST in favor of the unified factory probe (or keeps it only for the openai path if simpler — but a single factory path is preferred for DRY and correctness).

## 6. Frontend — Chat panel `api_type`

`SentryConfigEditor.jsx` Chat panel gains an **API type** `<select>`: `OpenAI-compatible` (value `openai`, default) / `Anthropic (Claude, incl. Bedrock)` (value `anthropic`), bound to `draft.chat.api_type`, saved in the chat-row PUT. When `anthropic` is selected, show a one-line hint: "For Bedrock: base URL `https://bedrock-mantle.<region>.api.aws/v1`, API key = your Bedrock API key, model = a Claude inference-profile id." The Test-connection payload already sends the chat fields; add `chat_api_type: draft.chat.api_type`. No other panel changes.

## 7. Dependency, mock, tests

- **Dep**: `github.com/anthropics/anthropic-sdk-go` (+ `.../option`). Pure Go; fits `CGO_ENABLED=0`. `go mod tidy`.
- **Mock model** (`backend/scripts/dlp_mock_model.py`): add a minimal `POST /v1/messages` handler that, when the request carries `tools`, returns an Anthropic-shaped response containing a `tool_use` block (non-stream is enough for the test-connection probe; add a streaming `?`/SSE variant only if the SDK's ChatStream requires it — resolve in implementation, mirror the existing chat-completions mock's tool branch). This lets `sentry_integration.sh` exercise the anthropic path with no real Bedrock.
- **Unit tests** (`internal/sentry/anthropic_test.go` + `llm_test.go`):
  - `normalizeAnthropicBaseURL` strips one trailing `/v1`/`/` (`https://x/v1` → `https://x`; `https://x` → `https://x`).
  - Translation helpers: a `[]Msg` with system + user + assistant(tool_calls) + tool(result) maps to the expected Anthropic params (system extracted; tool_use input is the parsed object; tool_result carries the tool_use_id); a `tool_use` response maps back to `ChatResult.ToolCalls` with `Args` as a JSON string.
  - `NewLLM` returns `anthropicLLM` for `api_type:"anthropic"`, `openaiLLM` otherwise/empty.
  - `applyDefaults` sets `APIType="openai"` when empty; `chat` row / `DefaultSentryChatRow` includes `api_type`.
- **Integration** (`sentry_integration.sh`): add a case that `PUT /api/sentry/config/chat {api_type:"anthropic", base_url→mock, model:"mock-claude"}` then `POST /api/sentry/config/test {chat_api_type:"anthropic", …}` asserting `checks.chat.tool_calling:true`. Keep the existing openai case + round-trip + replay. Run live.
- **Frontend**: `npm run lint` + `npm run build` clean.

## 8. Docs

- `docs/sentry.md`: add an **"Anthropic / Claude on AWS Bedrock"** subsection — set the Chat panel's API type to Anthropic, base URL `https://bedrock-mantle.<region>.api.aws/v1`, API key = a long-term Bedrock API key, model = a Claude inference-profile id (e.g. `us.anthropic.claude-...`); note tool calling is required and Mantle is region-gated (`us-east-1`/`us-west-2`, not `us-west-1`). Document the `api_type` chat field + `sentry.chat_api_type` boot key. Note the same base URL works for both api types.
- `CLAUDE.md`: Sentry subsection — the LLM layer now has an OpenAI and an Anthropic provider selected by the chat row's `api_type`; Anthropic uses `anthropic-sdk-go` over a Bearer endpoint (Bedrock Mantle).

## 9. Out of scope

- Direct `api.anthropic.com` (x-api-key auth) — a later small `auth_style` flag on the anthropic provider.
- Native Bedrock SigV4 / Converse via `aws-sdk-go` — not needed; Mantle's Bearer endpoint is covered.
- Anthropic-specific features beyond the shared `ChatCfg` (extended thinking, prompt caching, etc.).
- Per-provider embedding — the `memory` embedding endpoint stays OpenAI-embeddings-shaped (Phase-2; unaffected).
