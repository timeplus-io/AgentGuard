# Sentry Agent

Sentry is AgentGuard's conversational security-analyst agent: a chat UI
(`/sentry`) backed by an LLM-driven ReAct loop that answers operator
questions about monitored AI coding agents — security detections, DLP/
data-leak forensics, cost, sessions, tool usage — by calling a fixed,
read-only tool surface against Timeplus. Full design:
`docs/superpowers/specs/2026-07-22-sentry-agent-design.md`.

**This document covers Phase 1 (chat core) only.** Sentry currently has no
memory, no background tasks, and no write access to AgentGuard — every tool
is read-only. Those are later phases.

## Enabling

1. Set `sentry.enabled: true` in `config.yaml` (or `SENTRY_ENABLED=true`).
2. Provision (or re-provision): `POST /api/setup/provision`, or
   `POST /api/rules/sync` on an already-set-up deployment. This creates the
   `agentguard_sentry_conversations` / `agentguard_sentry_messages` /
   `agentguard_sentry_config` streams and seeds the three
   `agentguard_sentry_config` rows (`shared`/`chat`/`embed`, seed-if-absent —
   a re-provision never clobbers operator edits).
3. Point Sentry at a tool-calling-capable chat model, either via the
   `sentry.*` boot defaults in `config.yaml` before first provision, or live
   afterward under **Settings → Sentry Agent** (see below). Sentry works even
   with `dlp.enabled: false` — its config is entirely independent of DLP's.
4. Optionally set `sentry.skill_dir` to an on-disk override of the vendored
   `timeplus-sql-guide` skill (`SENTRY_SKILL_DIR` env var); refresh the
   vendored copy with `make sentry-sync-skill`.

## `agentguard_sentry_config`: Sentry's dedicated model config

Sentry has its **own** config stream, `agentguard_sentry_config`
(`backend/internal/timeplus/sentry_config_store.go`) — decoupled from
Semantic DLP's `agentguard_dlp_config`. It holds three key→JSON rows,
resolved with the same pure `dlp_config_resolve(shared, own)` UDF DLP uses
(`shared ⊕ own`, own wins, model name resolved against the shared catalog),
but over Sentry's own data:

| Row | Purpose |
|-----|---------|
| `shared` | Endpoint defaults (`base_url`, `api_key`, `timeout`) + a named model catalog (`models: {name → {model[, base_url, api_key]}}`) |
| `chat` | The ReAct loop's LLM: model name (default `"chat"`, looked up in `shared.models`), `system_prompt`, `max_tokens`, `max_iterations`, `turn_token_budget`, `sql_timeout_ms`, `sql_max_rows`, `tool_result_max_bytes` |
| `embed` | Embedding model (default name `"embedding"`) + `embedding_dim`, reserved for Sentry memory (**Phase 2** — chat itself never embeds today) |

`chat` row defaults (`DefaultSentryChatRow`, `backend/internal/timeplus/sentry_config_store.go`):

| Field | Default | Notes |
|-------|---------|-------|
| `model` | `"chat"` | A `shared.models` **name**, not a literal model id — point it at a tool-calling-capable model |
| `system_prompt` | built-in `DefaultSentrySystemPrompt` | `{schema_card}` is substituted at turn time with a live stream/column summary |
| `max_tokens` | `sentry.max_output_tokens` (fallback `1024`) | Per-completion cap |
| `max_iterations` | `12` | Model↔tool round-trips per turn before a forced final answer (clamped ≤ 32) |
| `turn_token_budget` | `32000` | Approximate token budget for history included in a turn (clamped ≤ 256000) |
| `sql_timeout_ms` | `30000` | `run_sql` query timeout (clamped ≤ 55000, under the 60s HTTP ceiling) |
| `sql_max_rows` | `1000` | `run_sql` row cap (clamped ≤ 5000) |
| `tool_result_max_bytes` | `16384` | Tool output truncation before it's fed back to the model (clamped ≤ 131072) |

All three rows are seeded from the `sentry.*` section of `config.yaml`
(`base_url`, `api_key`, `chat_model`, `embedding_model`, `embedding_dim`,
`request_timeout_seconds`, `max_output_tokens`) — those are **boot defaults
only**; the stream is authoritative after first provision.

## Editing the config: Settings → Sentry Agent, or the API directly

**In the UI:** open **Settings → Sentry Agent**. This section is reachable
regardless of `dlp.enabled` (unlike the DLP config editor, which 503s until
DLP is provisioned) — it reads/writes `agentguard_sentry_config` directly via
`GET /api/sentry/config`. Edit the `shared` endpoint/model catalog and the
`chat`/`embed` rows there, then use the built-in **Test connection** action
(`POST /api/sentry/config/test`) before saving.

**Via the API:**

```
GET  /api/sentry/config              → all three rows (api_key redacted)
PUT  /api/sentry/config/:key         → upsert one row ("shared"/"chat"/"embed")
POST /api/sentry/config/test         → validate a candidate endpoint
```

```
PUT /api/sentry/config/shared
{"value": {
  "base_url": "https://api.openai.com/v1",
  "api_key":  "sk-...",
  "timeout":  60,
  "models": {
    "chat":      {"model": "gpt-4o"},
    "embedding": {"model": "text-embedding-3-small"}
  }
}}
```

`POST /api/sentry/config/test` checks reachability (`GET /models`), a chat
completion **with a tool forced** (`tool_choice: "required"`) asserting the
response actually carries `tool_calls` — the ReAct loop is non-functional
without a tool-calling-capable model — and a real embedding call. Some
OpenAI-compatible endpoints/gateways reject `tool_choice: "required"` outright
and will false-fail this check even when the model supports tool calling
under `"auto"` — in that case, verify tool calling with an actual Sentry chat
turn instead. It accepts an optional body (`base_url`/`api_key`/`chat_model`/
`embedding_model`/`embedding_dim`) to test a candidate before saving; omitted
fields fall back first to the live `sentry.*` boot config, then to the
runtime-stored `shared` row of `agentguard_sentry_config` — the Settings
editor never echoes a saved `api_key` back, so this is what makes "Test
connection" work for a deployment configured entirely at runtime.

Resolution rules (`dlp_config_resolve`, same as DLP):
- `chat.model` (default `"chat"`) is looked up in `shared.models`. A match
  uses that entry's `model` and its own `base_url`/`api_key`, falling back to
  the shared top-level `base_url`/`api_key` when the entry omits them.
- **A name that is NOT in the catalog is used as a literal model id on the
  shared top-level `base_url`.** So if `shared.base_url` still points at the
  mock server (`http://localhost:4000/v1`) and there is no `chat` catalog
  entry, every turn dials `localhost:4000` and fails with
  `connection refused` unless the mock is running.

The **chat model must support tool/function calling.** The embedding model
(`embed` row) is reserved for Sentry's own memory in **Phase 2** — Sentry
chat itself never embeds today (Semantic DLP's `dlp_embed`/`dlp_search` are
unrelated, driven by `agentguard_dlp_config`).

## Skills (tool surface)

All Phase-1 tools are read-only (`internal/sentry/skills/`, registered in
`sentryRegistry`, `backend/internal/handlers/sentry.go`):

| Tool | Access |
|------|--------|
| `load_skill` | Load the Timeplus SQL guide — call once before non-trivial SQL |
| `load_skill_reference` | Load one deep-dive reference file from the guide |
| `list_streams` | List `agentguard_*`/`mv_*` streams and materialized views |
| `describe_stream` | Columns + types for one stream |
| `run_sql` | Ad-hoc read-only SQL (`table(...)` for historical reads); LIMIT enforced |
| `dlp_search` | Semantic DLP recall+rerank over `agentguard_dlp_enriched` |
| `get_threats` | Current AgentGuard threats (same rows as the Threats page) |
| `get_holds` | Human-in-the-loop hold records (same rows as the Holds list) |
| `get_rule_status` | Installed rules with current enabled/block_policy state |
| `get_cost_summary` | Per-model token/cost/latency totals over a trailing window |
| `get_session_timeline` | Ordered CIM event timeline for one session |

Every tool call and result is streamed to the client as `tool_call`/
`tool_result` NDJSON events and persisted to `agentguard_sentry_messages`
(`role="tool"`) so a conversation replays exactly what happened.

## Budgets

A turn stops requesting tools once `max_iterations` is hit (the final
iteration drops tool availability, forcing a plain answer), once
`turn_token_budget` worth of history+system prompt has been assembled, or
once a tool result exceeds `tool_result_max_bytes` (truncated, not dropped —
the model sees a `truncated: true` marker). These are per-turn ceilings, not
account-wide budgets; there's no cost-governance integration yet.

## Troubleshooting

`GET /api/sentry/status` reports `enabled`, `provisioned` (all three
streams — conversations/messages/config — present), the resolved `model`,
and `base_url_set`.

A `503` from `POST /api/sentry/chat` means one of:
- `sentry.enabled=false` — enable and restart/hot-apply.
- Sentry not provisioned — run `/api/setup/provision` or `/api/rules/sync`.
- No `base_url` in the resolved config — the `shared` row in
  `agentguard_sentry_config` is empty/unset; upsert it under
  **Settings → Sentry Agent**, or via `PUT /api/sentry/config/shared`.

A **mid-stream `error` event** (the chat opens, then fails) such as
`dial tcp ... :4000: connect: connection refused` means the resolved endpoint
is unreachable — the `shared` row's `base_url` (or the matched `models`
entry's) points somewhere nothing is listening, classically the mock port
`4000`. See **Editing the config** above: go to **Settings → Sentry Agent**,
set `shared.base_url` to a real OpenAI-compatible endpoint and add a `chat`
model to the catalog, run **Test connection**, then save (or start the mock
with `make dlp-mock` if you only want to smoke-test).

To test locally without a real model endpoint, use the deterministic mock
server (`backend/scripts/dlp_mock_model.py`, shared with the DLP tests — it
supports streaming tool-calls, and a non-stream tool-call response for the
`POST /api/sentry/config/test` probe): send a user message containing
`MOCK_TOOL_CALL {"name":"...","arguments":{...}}` and the mock replies with a
streamed tool call, then `MOCK_ANSWER ...` once a tool result comes back.
`backend/scripts/sentry_integration.sh` (`make sentry-integration`) drives
this end-to-end against a live Timeplus: mock model → provision → point
`agentguard_sentry_config`'s `shared` row at the mock → assert
`POST /api/sentry/config/test` → forced tool-call chat turn → assert the
NDJSON stream → replay the conversation and assert a `role="tool"` row.
