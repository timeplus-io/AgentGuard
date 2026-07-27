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
   `agentguard_sentry_config` streams and seeds the two
   `agentguard_sentry_config` rows (`chat`/`memory`, seed-if-absent —
   a re-provision never clobbers operator edits).
   Enabling `sentry.enabled` later via **Settings** also works: saving the
   toggle provisions the Sentry streams automatically (#226); if Timeplus
   was unreachable at that moment, the Sentry page shows a **Provision
   now** button that runs `/api/rules/sync`.
3. Point Sentry at a tool-calling-capable chat model, either via the
   `sentry.chat_*` boot defaults in `config.yaml` before first provision, or
   live afterward under **Settings → Sentry Agent** (see below). Sentry works
   even with `dlp.enabled: false` — its config is entirely independent of
   DLP's.
4. Optionally set `sentry.skill_dir` to an on-disk override of the embedded
   knowledge skills (`SENTRY_SKILL_DIR` env var): a **parent** directory
   containing one subdirectory per skill (`timeplus-sql-guide/`,
   `agentguard-guide/`), each with a `SKILL.md` + `references/`. Two skills
   ship embedded: `timeplus-sql-guide` (generic Timeplus SQL; its
   `references/` are vendored verbatim from
   [timeplus-io/AgentSkills](https://github.com/timeplus-io/AgentSkills) and
   refreshed with `make sentry-sync-skill`, while `SKILL.md` is locally
   adapted for Sentry's internal read-only `run_sql` execution channel and
   is not overwritten by the sync — #215) and `agentguard-guide`
   (AgentGuard's own streams and the canonical queries behind every API
   resource — #219).

## `agentguard_sentry_config`: Sentry's dedicated model config

Sentry has its **own** config stream, `agentguard_sentry_config`
(`backend/internal/timeplus/sentry_config_store.go`) — decoupled from
Semantic DLP's `agentguard_dlp_config`. It holds **two self-contained
key→JSON rows**. Each row carries its own literal endpoint (`base_url` +
`api_key` + `model`) — there is no `shared` row, no model catalog, and no
`dlp_config_resolve` indirection. The chat and memory endpoints are fully
independent, so the embedding model can live on a different endpoint from the
chat model (e.g. chat on OpenAI, embeddings on a local Ollama).

| Row | Purpose |
|-----|---------|
| `chat` | The ReAct loop's LLM — its own endpoint (`base_url`, `api_key`, `model` as a literal model id) + per-turn budgets. The system prompt is **not** here; it's a built-in constant (`DefaultSentrySystemPrompt`). |
| `memory` | An `enabled` toggle + the embedding endpoint (own `base_url`, `api_key`, `model`, `embedding_dim`). Reserved for Sentry memory (**Phase 2** — chat itself never embeds today). |

`chat` row defaults (`DefaultSentryChatRow`, `backend/internal/timeplus/sentry_config_store.go`):

| Field | Default | Notes |
|-------|---------|-------|
| `api_type` | `sentry.chat_api_type` (fallback `openai`) | `"openai"` (OpenAI-compatible chat/completions) or `"anthropic"` (Anthropic Messages API, e.g. Claude on Bedrock Mantle). Selects the LLM provider (`sentry.NewLLM`, `backend/internal/sentry/llm.go`). Not a secret — persisted to `config.yaml` like any other boot key. |
| `base_url` | `sentry.chat_base_url` | The chat endpoint. For `api_type:"openai"` this is the OpenAI-compatible `/v1` base (`/chat/completions` is appended); for `api_type:"anthropic"` the SDK appends `/v1/messages` (so on Bedrock Mantle the base is `.../anthropic`). See **Claude on AWS Bedrock** below — the base URL differs per provider. |
| `api_key` | `sentry.chat_api_key` | Secret; redacted on GET, preserved on a blank PUT |
| `model` | `sentry.chat_model` | A **literal** model id — point it at a tool-calling-capable model (for `anthropic`, a Claude inference-profile id) |
| `timeout` | `sentry.request_timeout_seconds` (fallback `60`) | Per-request timeout (seconds) |
| `max_tokens` | `sentry.max_output_tokens` (fallback `1024`) | Per-completion cap |
| `max_iterations` | `12` | Model↔tool round-trips per turn before a forced final answer (clamped ≤ 32) |
| `turn_token_budget` | `32000` | Approximate token budget for history included in a turn (clamped ≤ 256000) |
| `sql_timeout_ms` | `30000` | `run_sql` query timeout (clamped ≤ 55000, under the 60s HTTP ceiling) |
| `sql_max_rows` | `1000` | `run_sql` row cap (clamped ≤ 5000) |
| `tool_result_max_bytes` | `16384` | Tool output truncation before it's fed back to the model (clamped ≤ 131072) |

There is **no `system_prompt` field** — the ReAct loop always uses the
built-in `DefaultSentrySystemPrompt` constant (`{schema_card}` is substituted
at turn time with a live stream/column summary). Config cannot override it.

`memory` row defaults (`DefaultSentryMemoryRow`): `enabled`
(`sentry.memory_enabled`, default `false`), `base_url`
(`sentry.embedding_base_url`), `api_key` (`sentry.embedding_api_key`),
`model` (`sentry.embedding_model`), `embedding_dim`
(`sentry.embedding_dim`, fallback `1536`), `timeout`.

Both rows are seeded from the `sentry.*` section of `config.yaml` — those are
**boot defaults only**; the stream is authoritative after first provision.

Boot keys (`config.yaml` `sentry:` section → seeded row):

| Key | Seeds |
|-----|-------|
| `sentry.chat_api_type` | `chat` row `api_type` (default `openai`; set `anthropic` for the Anthropic Messages API) |
| `sentry.chat_base_url` / `sentry.chat_api_key` / `sentry.chat_model` | `chat` row endpoint |
| `sentry.request_timeout_seconds` / `sentry.max_output_tokens` | `chat` row `timeout` / `max_tokens` (also `memory` `timeout`) |
| `sentry.memory_enabled` | `memory` row `enabled` |
| `sentry.embedding_base_url` / `sentry.embedding_api_key` / `sentry.embedding_model` / `sentry.embedding_dim` | `memory` row endpoint + dim |

## Editing the config: Settings → Sentry Agent, or the API directly

**In the UI:** open **Settings → Sentry Agent**. This section is reachable
regardless of `dlp.enabled` (unlike the DLP config editor, which 503s until
DLP is provisioned) — it reads/writes `agentguard_sentry_config` directly via
`GET /api/sentry/config`. Edit the `chat` and `memory` rows there, then use
the built-in **Test connection** action (`POST /api/sentry/config/test`)
before saving.

**Via the API:**

```
GET  /api/sentry/config              → both rows (api_key redacted)
PUT  /api/sentry/config/:key         → upsert one row ("chat"/"memory")
POST /api/sentry/config/test         → validate a candidate endpoint
```

```
PUT /api/sentry/config/chat
{"value": {
  "api_type": "openai",
  "base_url": "https://api.openai.com/v1",
  "api_key":  "sk-...",
  "model":    "gpt-4o",
  "timeout":  60
}}
```

`api_type` defaults to `"openai"` when omitted. See **Claude on AWS Bedrock
(Anthropic API type)** below for the `"anthropic"` variant.

```
PUT /api/sentry/config/memory
{"value": {
  "enabled":       true,
  "base_url":      "http://localhost:11434/v1",
  "api_key":       "",
  "model":         "nomic-embed-text",
  "embedding_dim": 768,
  "timeout":       60
}}
```

`POST /api/sentry/config/test` always checks the **chat** endpoint —
reachability (`GET /models`) and a chat completion **with a tool forced**
(`tool_choice: "required"`) asserting the response actually carries
`tool_calls` (the ReAct loop is non-functional without a tool-calling-capable
model). The **embedding** endpoint is probed only when memory is enabled;
when memory is off, `checks.embedding` reports `{"skipped": true}` and is
excluded from the overall `ok` roll-up. Whether memory is on is taken from
the request body's `memory_enabled` (if present), else the stored `memory`
row, else the `sentry.memory_enabled` boot default.

Some OpenAI-compatible endpoints/gateways reject `tool_choice: "required"`
outright and will false-fail the chat check even when the model supports tool
calling under `"auto"` — in that case, verify tool calling with an actual
Sentry chat turn instead. The test accepts an optional body
(`base_url`/`api_key`/`chat_model`/`memory_enabled`/`embedding_base_url`/
`embedding_api_key`/`embedding_model`/`embedding_dim`) to test a candidate
before saving; omitted fields fall back first to the live `sentry.*` boot
config, then to the runtime-stored `chat`/`memory` rows of
`agentguard_sentry_config` — the Settings editor never echoes a saved
`api_key` back, so this is what makes "Test connection" work for a deployment
configured entirely at runtime. `chat_api_type` (`"openai"`/`"anthropic"`) is
one of those optional body fields — omit it to test against the stored/boot
`api_type`.

The **chat model must support tool/function calling.** The embedding model
(`memory` row) is reserved for Sentry's own memory in **Phase 2** — Sentry
chat itself never embeds today (Semantic DLP's `dlp_embed`/`dlp_search` are
unrelated, driven by `agentguard_dlp_config`).

## Claude on AWS Bedrock (Anthropic API type)

Sentry's chat LLM has two providers, selected by the `chat` row's `api_type`
(boot default `sentry.chat_api_type`, default `openai`):

- `"openai"` — the default; any OpenAI-compatible `/v1/chat/completions`
  endpoint (OpenAI itself, most gateways/proxies, local model servers).
- `"anthropic"` — the Anthropic Messages API, authenticated with an
  `x-api-key` header, implemented with `anthropic-sdk-go`
  (`backend/internal/sentry/anthropic.go`). This is how Sentry talks to
  **Claude on AWS Bedrock** via Bedrock Mantle (and it also works against
  native `api.anthropic.com`, which uses the same `x-api-key` auth).

To point Sentry at Claude on Bedrock, under **Settings → Sentry Agent** (or
`PUT /api/sentry/config/chat`) set the `chat` row to:

| Field | Value |
|-------|-------|
| API type | `anthropic` |
| Base URL | `https://bedrock-mantle.<region>.api.aws/anthropic` — note the `/anthropic` path prefix, **not** `/v1`. Pick a region where your model is **in-region** available (see caveat below); `us-east-1` is the safe default. |
| API key | A Bedrock API key (sent as the `x-api-key` header) |
| Model | A Bedrock Claude model id, **bare** (no `us.`/`global.` prefix, which are for `bedrock-runtime` only), e.g. `anthropic.claude-opus-4-8` or `anthropic.claude-sonnet-5` |

```
PUT /api/sentry/config/chat
{"value": {
  "api_type": "anthropic",
  "base_url": "https://bedrock-mantle.us-east-1.api.aws/anthropic",
  "api_key":  "<bedrock-api-key>",
  "model":    "anthropic.claude-opus-4-8",
  "timeout":  60
}}
```

> **Region + model access caveat.** Mantle's Anthropic Messages endpoint
> serves the **in-region** model only — it does **not** offer the
> cross-region Geo/Global inference profiles (`us.`/`global.`) that
> `bedrock-runtime` does. So the model must be *in-region available* in the
> region in your base URL **and** access-enabled for your account (Bedrock
> console → **Model access**). A model that works in the Bedrock playground
> (which uses `bedrock-runtime` cross-region routing) can still 404 on Mantle
> in a region where it has no in-region presence — e.g. Claude Opus 4.8 and
> Sonnet 5 are in-region in `us-east-1` but **not** in `us-west-1`/`us-west-2`.
> A `404 "model … does not exist"` means wrong region/not in-region; a
> `403 "Provide an Agreement token"` means the id is right but you haven't
> subscribed to that model in that region yet.

Run **Test connection** (`POST /api/sentry/config/test` with
`chat_api_type: "anthropic"`) afterward and confirm `checks.chat.tool_calling`
is `true` — Sentry's ReAct loop requires tool calling, and this is the same
requirement as the `openai` path.

**The base URL differs per provider on Bedrock Mantle.** The SDK appends
`/v1/messages` to the configured base URL, and Mantle serves the Anthropic
Messages API at `/anthropic/v1/messages` — so the `anthropic` base URL is
`https://bedrock-mantle.<region>.api.aws/anthropic`. The OpenAI-compatible
Chat Completions surface lives at a different prefix
(`https://bedrock-mantle.<region>.api.aws/openai/v1`, used with
`api_type: "openai"`). `normalizeAnthropicBaseURL` strips a trailing `/v1` if
you include one (so `.../anthropic/v1` also works), but you cannot point both
api types at the same URL on Bedrock.

## Skills (tool surface)

All Phase-1 tools are read-only (`internal/sentry/skills/`, registered in
`sentryRegistry`, `backend/internal/handlers/sentry.go`):

| Tool | Access |
|------|--------|
| `load_skill` | Load a knowledge skill by `name`: `timeplus-sql-guide` (SQL syntax) or `agentguard-guide` (AgentGuard streams + canonical queries) — call each once per conversation as needed |
| `load_skill_reference` | Load one deep-dive reference file from a skill (`skill`, `name`) |
| `list_streams` | List `agentguard_*`/`mv_*` streams and materialized views |
| `describe_stream` | Columns + types for one stream |
| `run_sql` | Ad-hoc read-only SQL (`table(...)` for historical reads); LIMIT enforced; credential-bearing config streams (`agentguard_dlp_config`, `agentguard_sentry_config`) are not readable |
| `dlp_search` | Semantic DLP recall+rerank over `agentguard_dlp_enriched` |
| `get_threats` | Current AgentGuard threats (same rows as the Threats page) |
| `get_holds` | Human-in-the-loop hold records (same rows as the Holds list) |
| `get_rule_status` | Installed rules with current enabled/block_policy state |
| `get_cost_summary` | Per-model token/cost/latency totals over a trailing window |
| `get_session_timeline` | Ordered CIM event timeline for one session |
| `get_rule_catalog` | Installable rule definitions (builtin + custom) with installed/upgradeable status; `{id}` for full detail incl. parameter schema. The catalog is filesystem-backed, not a Timeplus stream — SQL can't reach it |
| `acknowledge_threat` | **GatedWrite** — mark one threat acknowledged (identity from `get_threats`); runs the same status upsert as the Threats page |
| `clear_threat` | **GatedWrite** — mark one threat cleared; auto-reopens if the rule fires again |
| `decide_hold` | **GatedWrite** — approve/deny one pending hold; runs the same terminalization path as the Holds UI (holdregistry claim + SSE broadcast), so the waiting agent action unblocks |

Every tool call and result is streamed to the client as `tool_call`/
`tool_result` NDJSON events and persisted to `agentguard_sentry_messages`
(`role="tool"`) so a conversation replays exactly what happened.

## Conversation management

Conversations are owner-scoped (the session user) and managed from the
`/sentry` page's conversation rail:

- **Rename** — `PATCH /api/sentry/conversations/:id` with `{title}`
  (inline edit in the rail). Titles are truncated to 80 runes. A rename
  preserves the conversation's position in the list (`updated_at` is kept).
- **Delete** — `DELETE /api/sentry/conversations/:id` is a soft-delete
  (`status` → `deleted`): the conversation disappears from the list and
  reads 404 afterwards. Message rows stay in the append-only
  `agentguard_sentry_messages` log but become unreachable through the API.
  Legacy `archived` rows are treated the same as deleted.
- **LLM auto-naming** — a new conversation is initially titled with its
  (truncated) first user message. After each successful turn, while the
  title still equals that auto-derived value, Sentry asks the configured
  chat model for a short summary name (≤ 8 words) and renames the
  conversation. This runs asynchronously off the turn's critical path and
  fails open (any error keeps the current title). A title set by the
  operator — or a previous auto-naming pass — is never overwritten; no
  extra schema column is needed because "still auto-titled" is detected by
  comparing the title against the truncated first user message.

## Gated writes & approvals

Write actions run through the approval plumbing (#221). Batch 1 of the
write tools (#222) ships `acknowledge_threat`, `clear_threat`, and
`decide_hold` — each calls the same internal logic as its HTTP endpoint
(injected as a closure, so agent-driven and UI-driven writes can't drift),
and `decided_by` attribution comes from the conversation's owner.

The whole **AgentGuard interaction surface is opt-in**: the chat config
row's `agentguard_tools_enabled` bool (toggle under **Settings → Sentry
Agent**), **disabled by default**. Off, Sentry is a generic Timeplus SQL
analyst — knowledge skills (minus `agentguard-guide`), `list_streams`/
`describe_stream`, and read-only `run_sql`. On, it additionally gets the
AgentGuard read tools (`get_threats`, `get_holds`, `get_rule_status`,
`get_cost_summary`, `get_session_timeline`, `get_rule_catalog`,
`dlp_search`), the `agentguard-guide` knowledge skill, and the gated write
tools — each write still individually approval-gated. The system prompt
adapts to the switch via the `{agentguard_tools}` placeholder. Any skill
whose `Access()` is `GatedWrite` runs through the human-approval flow
instead of executing directly:

1. The loop registers a proposal in an in-memory registry, then emits an
   `approval_request` NDJSON event carrying `proposal_id`, the tool name,
   the **verbatim args**, and `timeout_ms`, and blocks.
2. The chat UI renders a confirmation card (args shown verbatim) with
   Approve / Deny. The decision goes to
   `POST /api/sentry/approvals/:proposal_id/decide` (`{approve, reason?}`),
   which only the conversation's owner may call (403 otherwise; an unknown
   or already-resolved proposal is 404).
3. The loop emits `approval_result` (`approved`/`denied`/`timeout`/
   `abandoned` + decided_by + reason) and only executes the tool on an
   explicit approve. Denial, timeout (default 5 minutes,
   `approval_timeout_ms` in the chat config row, max 30 minutes), and
   client disconnect all return an "not executed" result to the model.
4. The decision and outcome are persisted in the tool row's metadata
   (`toolCallMeta.approval`), so conversation replay shows the full audit
   trail.

There is **no auto-approve setting, by design**. A Loop constructed without
an approval registry (e.g. future headless runs) never offers gated tools to
the model at all.

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
- No `base_url` in the resolved config — the `chat` row in
  `agentguard_sentry_config` is empty/unset; upsert it under
  **Settings → Sentry Agent**, or via `PUT /api/sentry/config/chat`.

A **mid-stream `error` event** (the chat opens, then fails) such as
`dial tcp ... :4000: connect: connection refused` means the chat endpoint is
unreachable — the `chat` row's `base_url` points somewhere nothing is
listening, classically the mock port `4000`. See **Editing the config**
above: go to **Settings → Sentry Agent**, set the `chat` row's `base_url` +
`model` to a real OpenAI-compatible endpoint, run **Test connection**, then
save (or start the mock with `make dlp-mock` if you only want to smoke-test).

To test locally without a real model endpoint, use the deterministic mock
server (`backend/scripts/dlp_mock_model.py`, shared with the DLP tests — it
supports streaming tool-calls, and a non-stream tool-call response for the
`POST /api/sentry/config/test` probe): send a user message containing
`MOCK_TOOL_CALL {"name":"...","arguments":{...}}` and the mock replies with a
streamed tool call, then `MOCK_ANSWER ...` once a tool result comes back. The
mock also serves `POST /v1/messages` as an Anthropic Messages SSE stream (a
`tool_use` block when the request carries `tools`, else a `text` block) so
`api_type: "anthropic"` can be exercised the same way, with no real Bedrock
endpoint.
`backend/scripts/sentry_integration.sh` (`make sentry-integration`) drives
this end-to-end against a live Timeplus: mock model → provision → point
`agentguard_sentry_config`'s `chat` row at the mock → assert
`POST /api/sentry/config/test` (memory off → embedding skipped, then memory
on → embedding probed) → switch the `chat` row to `api_type: "anthropic"`
against the mock's `/v1/messages` and re-assert `checks.chat.tool_calling`,
then revert to `openai` → forced tool-call chat turn → assert the NDJSON
stream → replay the conversation and assert a `role="tool"` row.
