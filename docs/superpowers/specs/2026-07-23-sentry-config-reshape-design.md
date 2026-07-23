# Sentry Config Reshape — Independent Chat + Memory(Embedding) Endpoints

> **Status:** Approved design
> **Issue:** [#207](https://github.com/timeplus-io/AgentGuard/issues/207) (parent), [#209](https://github.com/timeplus-io/AgentGuard/issues/209); on branch `feature/207-sentry-agent-design` / PR [#208](https://github.com/timeplus-io/AgentGuard/pull/208)
> **Date:** 2026-07-23
> **Supersedes:** the `shared`/`chat`/`embed` three-row model from `2026-07-22-sentry-dedicated-model-config-design.md`. That work is unmerged (same branch), so this is a clean reshape, not a migration.

## 1. Goal

Reshape Sentry's dedicated model config so that:

1. **Chat and embedding are fully independent endpoints** — each with its own `base_url`, `api_key`, and `model` — so an operator can run, e.g., local Ollama for embeddings and OpenAI for chat.
2. **The chat system prompt is not operator config** — the loop always uses the built-in `DefaultSentrySystemPrompt`; drop the `system_prompt` field from the config row and its validation.
3. **Embedding config lives under a "memory" config** gated by a `memory.enabled` toggle — because embeddings belong to Sentry memory (a Phase-2 feature). When memory is disabled, embedding need not be configured, and the config UI hides it.

Decisions settled with the user (brainstorming):
- **Fully self-contained rows** — remove the `shared` row and the model-name catalog entirely; each row carries a concrete endpoint. Sentry stops calling `dlp_config_resolve`.
- **Config scaffolding now, feature in Phase 2** — the `memory` row (enable toggle + embedding endpoint) is provisioned and editable now; actual memory storage/recall remains Phase 2.

## 2. Config rows — `agentguard_sentry_config`

The stream DDL is unchanged (`key`, `value` JSON, `updated_at`, `updated_by`, `PRIMARY KEY (key)`). Two rows replace the previous three (`shared` and `embed`/`chat`-with-catalog-name are gone):

### 2.1 `chat` (`SentryCfgKeyChat = "chat"`)
Self-contained chat endpoint + loop budgets. **No `system_prompt`.** `model` is a literal model id (not a catalog name).

```json
{
  "base_url": "https://api.openai.com/v1",
  "api_key": "sk-...",
  "model": "gpt-4o",
  "timeout": 60,
  "max_tokens": 1024,
  "max_iterations": 12,
  "turn_token_budget": 32000,
  "sql_timeout_ms": 30000,
  "sql_max_rows": 1000,
  "tool_result_max_bytes": 16384
}
```

### 2.2 `memory` (`SentryCfgKeyMemory = "memory"`)
The memory enable toggle + its own self-contained embedding endpoint. Provisioned now; consumed by Sentry memory in Phase 2.

```json
{
  "enabled": false,
  "base_url": "http://localhost:11434/v1",
  "api_key": "",
  "model": "nomic-embed-text",
  "embedding_dim": 768,
  "timeout": 60
}
```

`dlp_config_resolve` is **no longer used for Sentry** — it existed only to fold `shared ⊕ own` and resolve catalog names, both now removed. The pure UDF remains in the codebase (DLP still uses it); `InitSentry` still ensures it exists for DLP but Sentry no longer calls it. `LoadChatCfg` reads and unmarshals the `chat` row directly.

## 3. System prompt

Removed from config entirely. `ChatCfg.SystemPrompt` is dropped as a JSON-sourced field; the loop uses the exported constant `tp.DefaultSentrySystemPrompt` directly (still `{schema_card}`-substituted at turn time — that substitution already happens in the loop, not the config). `ValidateSentryConfigValue` loses its `system_prompt` string check. `DefaultSentryChatRow` no longer emits `system_prompt`.

## 4. Boot defaults — `config.yaml` `sentry.*`

Split the single endpoint into chat vs. embedding. Final `SentryConfig` fields (all hot-apply, seed-if-absent, `api_key` fields never written to `config.yaml` by `Save()`):

| Key | Default | Seeds |
|---|---|---|
| `sentry.enabled` | false | (existing) |
| `sentry.skill_dir` | "" | (existing) |
| `sentry.chat_base_url` | "" | `chat.base_url` |
| `sentry.chat_api_key` | "" (secret) | `chat.api_key` |
| `sentry.chat_model` | "" | `chat.model` |
| `sentry.request_timeout_seconds` | 60 | `chat.timeout` (+ `memory.timeout`) |
| `sentry.max_output_tokens` | 1024 | `chat.max_tokens` |
| `sentry.memory_enabled` | false | `memory.enabled` |
| `sentry.embedding_base_url` | "" | `memory.base_url` |
| `sentry.embedding_api_key` | "" (secret) | `memory.api_key` |
| `sentry.embedding_model` | "" | `memory.model` |
| `sentry.embedding_dim` | 1536 | `memory.embedding_dim` |

**Removed** from the previous design: `sentry.base_url`, `sentry.api_key` (the old shared endpoint). `sentry.chat_model` is retained but now seeds a literal `chat.model` (not a catalog name). Each new key needs the full five-point wiring (struct field; `SetDefault` + `v.GetX` read + `BindEnv` + `Save` `v.Set` **except the two `*_api_key` secrets, which are NOT written to Save()**; registry entry with `EnvVar`, `TypeSecret` for the api_key keys; `sources.go` `EffectiveValue` + `ApplyChange`; save-roundtrip test). `SentryConfigRows(sCfg)` renders the two rows from these.

## 5. Backend changes

- **`internal/timeplus/sentry_config_store.go`**: `SentryConfigKeys()` → `["chat","memory"]`; add `SentryCfgKeyMemory = "memory"`; drop `SentryCfgKeyShared`, `SentryCfgKeyEmbed`. `SentryConfigRows` emits `chat` + `memory`. `DefaultSentryChatRow` drops `system_prompt` and gains `base_url`/`api_key`/`model`/`timeout`. `ValidateSentryConfigValue`: `chat` (no system_prompt check; optional numeric-field sanity is fine), `memory` (`enabled` must be bool if present, `embedding_dim` must be a number if present).
- **`internal/timeplus/sentry.go`**: `seedSentryConfig` seeds `chat` + `memory` seed-if-absent. `InitSentry` still creates `agentguard_sentry_config` + the `dlp_config_resolve` UDF (for DLP) + conversation streams; no other change.
- **`internal/sentry/llm.go`**: `chatCfgResolveSQL()` → a direct read `SELECT value AS cfg FROM table(agentguard_sentry_config) WHERE key='chat'` (no `dlp_config_resolve` wrap). `LoadChatCfg` unmarshals `chat` directly, applies `applyDefaults`/clamps, errors on empty `base_url`. Remove `SystemPrompt` from the JSON-decoded `ChatCfg` (loop reads the constant). `applyDefaults`/clamp ceilings unchanged for the budget fields.
- **`internal/handlers/sentry_config.go`**: `TestSentryConfig` always probes the **chat** endpoint (reachable + tool-calling). It probes the **embedding** endpoint (from the `memory` row / `sentry.embedding_*` boot, its own base_url/api_key) **only when memory is enabled** (from body or resolved config); when disabled, return `checks.embedding = {"skipped": true}` and exclude it from the `ok` roll-up. `resolveSentryTestCreds` (the body→boot→stored fallback) is extended to resolve chat vs. embedding creds separately from their respective rows/boot keys. `GetSentryConfig`/`UpsertSentryConfig` are key-agnostic (already `IsSentryConfigKey`-gated) — only the key set changes.

## 6. Frontend — `SentryConfigEditor`

Two panels (was three), design-tokens only:

- **Chat model** (`chat` row): `base_url`, `api_key` (blank-preserves, shows "set" when `api_key_set`), `model`, and the 6 budget knobs with clamp-ceiling hints. **No system-prompt field.** A **Test connection** button → `POST /api/sentry/config/test` rendering reachable + chat(tool-calling) pass/fail.
- **Memory (embedding)** (`memory` row): an **enable toggle** at top. When off, the embedding fields (`base_url`, `api_key`, `model`, `embedding_dim`) are hidden and Save writes `{enabled:false}`. When on, the fields show, and Test connection additionally renders the embedding check. Labeled "Used by Sentry memory (Phase 2)".

The `SENTRY_SERVER_KEYS` filter in the Settings server-config list is updated to hide the new `sentry.chat_*`/`sentry.embedding_*`/`sentry.memory_enabled` keys (edited via the runtime editor), keeping `sentry.enabled`/`skill_dir` visible. The `/sentry` "not configured" hint (keyed on `base_url_set`) is unchanged.

## 7. Migration / compatibility

The three-row model is unmerged (same branch). This reshape changes what `InitSentry` seeds; a fresh provision creates the two-row config. No deployed Sentry config exists. An environment that already ran the three-row provisioner keeps orphaned `shared`/`embed` rows (harmless, unread) until a clean reprovision; optionally `InitSentry` could delete them, but per YAGNI we leave them (unread rows cost nothing). No `ALTER`, no dual-read.

## 8. Testing

- **Unit (Go)**: `SentryConfigRows` emits exactly `chat`+`memory` with the expected shapes (chat has no `system_prompt`, has base_url/model; memory has enabled/embedding_dim); `ValidateSentryConfigValue` accept/reject matrix for the two keys (memory `enabled` non-bool → reject, `embedding_dim` non-number → reject; chat bad JSON → reject); `chatCfgResolveSQL()` targets `agentguard_sentry_config` key='chat' and does NOT wrap in `dlp_config_resolve`; `LoadChatCfg` decodes a self-contained chat row; `resolveSentryTestCreds` resolves chat vs embedding creds independently (body→boot→stored) per endpoint; the config round-trip test covers the new boot keys with `*_api_key` NOT persisted.
- **Handler**: `TestSentryConfig` skips the embedding check (`skipped:true`, excluded from `ok`) when memory disabled; probes it when enabled. Unknown-key/bad-JSON 400s still hold.
- **Integration** (`sentry_integration.sh`): `PUT /api/sentry/config/chat` points a self-contained chat endpoint at the mock (base_url→mock, model→mock-chat); assert `POST /api/sentry/config/test` returns chat reachable+tool_calling ok with embedding `skipped` (memory off). Optionally a second case enabling memory + pointing embedding at the mock and asserting the embedding check passes. Keep the chat round-trip + replay assertions. Must pass live.
- **Frontend**: `npm run lint` + `npm run build` clean.

## 9. Docs

- `docs/sentry.md`: rewrite the config section to the two-row model — Chat model (own endpoint, no system prompt) and Memory (enable toggle + own embedding endpoint, Phase-2). Update the API examples (`PUT /api/sentry/config/chat`, `.../memory`), the defaults table (drop `system_prompt`; split chat/embedding boot keys), and the test-connection description (embedding skipped when memory off).
- `CLAUDE.md`: update the Sentry subsection + config-key references to the two-row model; note the system prompt is a built-in constant, not config, and embedding lives under memory.

## 10. Out of scope

- Sentry memory itself (storage/recall/retrieval) — Phase 2; this only provisions + exposes its embedding endpoint config and enable toggle.
- Auto-detecting `embedding_dim` by probing — operator sets it; revisit with Phase-2 memory provisioning.
- Any change to DLP config or `dlp_search`'s embedding (unchanged; still DLP-owned).
