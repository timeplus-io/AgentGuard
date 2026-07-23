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
   `agentguard_sentry_conversations` / `agentguard_sentry_messages` streams
   and seeds the `sentry_chat` config row (seed-if-absent — a re-provision
   never clobbers operator edits).
3. Point the shared config catalog (`agentguard_dlp_config`'s `shared` row)
   at a tool-calling-capable chat model — the same catalog the Semantic DLP
   UDFs use. Sentry works even with `dlp.enabled: false`, as long as
   `sentry.enabled: true` and the shared row has a `base_url`.
4. Optionally set `sentry.skill_dir` to an on-disk override of the vendored
   `timeplus-sql-guide` skill (`SENTRY_SKILL_DIR` env var); refresh the
   vendored copy with `make sentry-sync-skill`.

## `sentry_chat` config row

Like the DLP UDFs, Sentry's settings are a JSON row in `agentguard_dlp_config`
(key `sentry_chat`), resolved at call time via `dlp_config_resolve(shared,
sentry_chat)` — `shared ⊕ own`, own wins, model name resolved against the
shared catalog. Edit it with `PUT /api/dlp/config/sentry_chat`. Defaults
(`backend/internal/timeplus/sentry.go`, `SentryChatDefaults`):

| Field | Default | Notes |
|-------|---------|-------|
| `model` | `"chat"` | A shared-catalog model **name**, not a literal model id — point it at a tool-calling-capable model |
| `system_prompt` | built-in `DefaultSentrySystemPrompt` | `{schema_card}` is substituted at turn time with a live stream/column summary |
| `max_tokens` | `dlp.max_output_tokens` | Per-completion cap |
| `max_iterations` | `12` | Model↔tool round-trips per turn before a forced final answer (clamped ≤ 32) |
| `turn_token_budget` | `32000` | Approximate token budget for history included in a turn (clamped ≤ 256000) |
| `sql_timeout_ms` | `30000` | `run_sql` query timeout (clamped ≤ 55000, under the 60s HTTP ceiling) |
| `sql_max_rows` | `1000` | `run_sql` row cap (clamped ≤ 5000) |
| `tool_result_max_bytes` | `16384` | Tool output truncation before it's fed back to the model (clamped ≤ 131072) |

## Configuring the chat endpoint (the `shared` row)

Sentry has **no endpoint setting of its own.** The actual `base_url`, API key,
and model list all come from the `shared` row of `agentguard_dlp_config` — the
same row the Semantic DLP UDFs use. `sentry_chat.model` is only a **name** that
`dlp_config_resolve` looks up in `shared.models`; whichever endpoint that name
resolves to is where Sentry sends chat requests. So to point Sentry at a real,
tool-calling-capable model you edit `shared`, not `sentry_chat`:

```
PUT /api/dlp/config/shared
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

Resolution rules (`dlp_config_resolve`):
- `sentry_chat.model` (default `"chat"`) is looked up in `shared.models`. A
  match uses that entry's `model` and its own `base_url`/`api_key`, falling
  back to the shared top-level `base_url`/`api_key` when the entry omits them.
- **A name that is NOT in the catalog is used as a literal model id on the
  shared top-level `base_url`.** So if `shared.base_url` still points at the
  mock server (`http://localhost:4000/v1`, seeded from `dlp.base_url` in
  `config.yaml`) and there is no `chat` catalog entry, every turn dials
  `localhost:4000` and fails with `connection refused` unless the mock is
  running. Point `shared.base_url` at a real endpoint and add a `chat` entry.

The **chat model must support tool/function calling** — the ReAct loop is
non-functional without it. Embeddings (`dlp_embed` row) are only used by
`dlp_search`/DLP enrichment in Phase 1; Sentry chat itself never embeds
(memory arrives in Phase 2).

**Editing in the UI:** the `shared` endpoint and its model catalog are editable
today under **Settings → Semantic DLP → Runtime configuration** (the DLP config
editor — add/remove catalog models there). Two caveats: that editor is nested
under the *DLP* section (not branded for Sentry), and it reads via
`GET /api/dlp/config`, which returns `503` until DLP is provisioned — so on a
`sentry.enabled: true`, `dlp.enabled: false` deployment you currently configure
the endpoint via the `PUT /api/dlp/config/shared` API above rather than the UI.
A dedicated Sentry model-configuration UI is tracked as follow-up work.

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

`GET /api/sentry/status` reports `enabled`, `provisioned` (both streams
present), the resolved `model`, and `base_url_set`.

A `503` from `POST /api/sentry/chat` means one of:
- `sentry.enabled=false` — enable and restart/hot-apply.
- Sentry not provisioned — run `/api/setup/provision` or `/api/rules/sync`.
- No `base_url` in the resolved config — the `shared` row in
  `agentguard_dlp_config` is empty/unset; upsert it via
  `PUT /api/dlp/config/shared`.

A **mid-stream `error` event** (the chat opens, then fails) such as
`dial tcp ... :4000: connect: connection refused` means the resolved endpoint
is unreachable — the `shared` row's `base_url` (or the matched `models` entry's)
points somewhere nothing is listening, classically the mock port `4000`. See
**Configuring the chat endpoint** above: set `shared.base_url` to a real
OpenAI-compatible endpoint and add a `chat` model to the catalog (or start the
mock with `make dlp-mock` if you only want to smoke-test).

To test locally without a real model endpoint, use the deterministic mock
server (`backend/scripts/dlp_mock_model.py`, shared with the DLP tests — it
supports streaming tool-calls): send a user message containing
`MOCK_TOOL_CALL {"name":"...","arguments":{...}}` and the mock replies with a
streamed tool call, then `MOCK_ANSWER ...` once a tool result comes back.
`backend/scripts/sentry_integration.sh` (`make sentry-integration`) drives
this end-to-end against a live Timeplus: mock model → provision → point the
shared config at the mock → forced tool-call chat turn → assert the NDJSON
stream → replay the conversation and assert a `role="tool"` row.
