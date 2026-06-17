# Semantic DLP — Local-Model Robustness (issue #184)

Status: approved design (2026-06-17)
Branch: `feature/184-semantic-dlp-manual-test`
Parent feature: Semantic DLP (`docs/dlp.md`, `docs/superpowers/specs/2026-06-11-semantic-dlp-design.md`, issue #159)

## Background

Issue #184 ("Semantic DLP : Manual Test") tracks manually exercising the Semantic
DLP funnel against a **real, local** OpenAI-compatible model backend (Ollama)
rather than the deterministic mock server (`backend/scripts/dlp_mock_model.py`).

That manual test surfaced four gaps that this enhancement closes:

1. **Timeouts too tight for local LLMs.** The shared Proton HTTP query client was
   capped at 15s, but a warm local 7B summary over ~20 evidence messages takes
   ~17s, so the query funnel's `dlp_summarize` call hit `context deadline
   exceeded` and the report degraded to "Model summary unavailable". (The client
   cap was already raised to 60s during manual testing; see "Already done".)
2. **Token budgets too small + thinking models silently fail.** Reasoning models
   (e.g. `gemma4:e2b`, `qwen3:8b`) emit chain-of-thought into a separate
   `reasoning` field and only fill `content` afterward. Under the UDFs' small
   `max_tokens` (risk = 100), the budget is spent on reasoning, `content` comes
   back empty (`finish_reason: "length"`), and the UDF — which reads only
   `content` — returns nothing. Model choice fails with no signal.
3. **Preflight is shallow.** `POST /api/config/test-dlp` only does `GET /models`,
   so an unusable configuration (wrong embedding dimension, a thinking model that
   returns empty content) passes the test and only fails later at query time.
4. **No local-model setup docs.** `docs/dlp.md` documents the feature but not how
   to point it at a local Ollama backend or the gotchas above.

These are cohesive ("make DLP usable with real local models") and ship as one spec
/ one implementation plan.

## Goals

- Make DLP model-call timeouts and output-token budgets **tunable via config**,
  so a slower or more verbose local model does not require a code change.
- Make the chat UDFs **tolerate thinking models** without breaking the
  endpoint-agnostic OpenAI-compatible contract.
- Make `test-dlp` **actually verify** a configuration is usable before it is saved.
- **Document** the local-Ollama setup end-to-end.

## Non-goals (YAGNI)

- Per-stage timeout/token configuration. A single global pair of knobs is enough;
  the stages do not need independent tuning in practice.
- Auto-injecting provider-specific "disable thinking" parameters
  (`chat_template_kwargs`, `reasoning_effort`). These are Ollama-specific and a
  strict OpenAI/cloud endpoint would reject them. We use the endpoint-agnostic
  content-fallback + larger budget instead.
- Adding an HNSW vector index. Timeplus 3.2.1 does not support the
  `vector_similarity` index type; the funnel already falls back to brute-force
  `cosine_distance`, which is correct (just slower at scale). Documented, not fixed.

## Already done (during manual testing, in the working tree)

`backend/internal/timeplus/client.go`: `protonHTTPClient` timeout raised
15s → 60s, with the `queryNativeTimeout` comment updated to match. This spec
keeps that change and treats 60s as the fixed query-client ceiling that the new
per-UDF timeout knob must stay under.

## Design

### Component 1 — Config knobs

Add two keys to the `dlp:` section. Both are `Reprovision: true` because their
values are baked into the UDF DDL at provision time.

| Key | Type | Default | Validation | Applies to |
|---|---|---|---|---|
| `dlp.request_timeout_seconds` | int | `60` | clamp to `[5, 55]` | the `timeout=` arg on every UDF `requests.post` (embed + 4 chat UDFs) |
| `dlp.max_output_tokens` | int | `512` | clamp to `[16, 4096]` | `max_tokens` for `dlp_risk`, `dlp_understand_query`, `dlp_rerank` |

`dlp_summarize` computes its budget as `max(max_output_tokens, 800)` in the Go DDL
builder, so audit reports always have room regardless of the global value.

**Timeout relationship (intentional, documented).** The query funnel
(`dlp_understand_query` → recall → `dlp_rerank` → `dlp_summarize`) runs through
`protonHTTPClient` (60s). A UDF-internal `requests.post` timeout that meets or
exceeds the client timeout would let the Go client abandon the SQL before the UDF
returns. So `request_timeout_seconds` is **clamped to ≤ 55s**, leaving headroom
under the 60s client ceiling. The ingest UDFs (`dlp_embed`, `dlp_risk`) run inside
`mv_dlp_enrich`, not via `protonHTTPClient`, so for them 55s is merely an upper
bound, not a hard requirement. (Making `protonHTTPClient` itself configurable to
allow longer query timeouts is a possible future change but is out of scope: it is
a shared client used by every query, not just DLP.)

Touch points:

- `internal/config/config.go` — `DLPConfig` gains `RequestTimeoutSeconds int`
  (`mapstructure:"request_timeout_seconds"`) and `MaxOutputTokens int`
  (`mapstructure:"max_output_tokens"`); add `SetDefault`, `GetInt` reads in
  `load()`, `BindEnv` (`DLP_REQUEST_TIMEOUT_SECONDS`, `DLP_MAX_OUTPUT_TOKENS`),
  and `Set` calls in `Save()`.
- `internal/config/registry.go` — two `KeyMeta` entries in `Registry()`, section
  `dlp`, `Type: TypeInt`, `Reprovision: true`, with labels/descriptions.
- `internal/config/sources.go` — add the two keys to the coerce/validate switch
  (`case "dlp.request_timeout_seconds"`, `case "dlp.max_output_tokens"`),
  enforcing the clamps above and rejecting non-integers / non-positive values.

### Component 2 — Thinking-model content fallback

A small helper is added to the Python body of each of the four chat UDFs
(`dlp_understand_query`, `dlp_rerank`, `dlp_summarize`, `dlp_risk._classify`):

```python
def _msg_content(resp):
    m = resp.json()['choices'][0]['message']
    return (m.get('content') or '').strip() or \
           (m.get('reasoning') or m.get('reasoning_content') or '').strip()
```

Each `raw = resp.json()['choices'][0]['message']['content']` line becomes
`raw = _msg_content(resp)`. The larger `max_output_tokens` is the primary fix
(content fills in after reasoning); the reasoning-field fallback is a best-effort
safety net for the case where the budget still truncates. For the JSON-producing
UDFs the existing brace-extraction (`raw[raw.find('{'):raw.rfind('}')+1]`) then
operates on whatever text was returned — so even a reasoning blob that happens to
contain the JSON object is recoverable; if no JSON is found the UDF falls open as
today (`_fallback`, `0.5` rerank default, `''` summary).

The `dlp_embed` UDF is unchanged apart from receiving the configurable timeout.

#### Template parameterization

`dlp.go` and `dlp_query.go` hold the UDF templates; `dlpReplacer(cfg, model)`
fills `{{BASE_URL}}`, `{{API_KEY}}`, `{{MODEL}}`. Extend it (or add a sibling
builder) to also fill `{{TIMEOUT}}` and `{{MAX_TOKENS}}`:

- Replace literal `timeout=30` / `timeout=60` in the templates with
  `timeout={{TIMEOUT}}`.
- Replace literal `'max_tokens': 100|200|400` with `'max_tokens': {{MAX_TOKENS}}`.
- `dlp_summarize`'s builder substitutes `max({{MAX_TOKENS}}, 800)` as a literal
  integer computed in Go (not a Python expression), keeping the template simple.

Because all six UDFs are `CREATE OR REPLACE`, a re-provision / `POST
/api/rules/sync` picks up new values with no stream rebuild (only an
`embedding_dim` change requires the drop-and-recreate documented in `docs/dlp.md`).

### Component 3 — Preflight validation (`POST /api/config/test-dlp`)

Extend the handler (`internal/handlers/settings.go`) to accept optional
`embedding_model`, `embedding_dim`, and `risk_model` in the request body (falling
back to live `cfg.DLP` values when omitted), and run up to three checks:

1. **reachable** — `GET {base_url}/models` (existing behavior).
2. **embedding** — `POST {base_url}/embeddings` with `embedding_model` and a tiny
   input; verify `data[0].embedding` exists; compare `len(embedding)` to
   `embedding_dim`. Report `{ok, dim, expected}`.
3. **chat** — `POST {base_url}/chat/completions` with `risk_model`, a tiny prompt,
   and `max_output_tokens`; verify non-empty `content`. If `content` is empty but
   `reasoning`/`reasoning_content` is present, set `thinking_detected: true` and a
   note recommending a larger `max_output_tokens` or a direct-response model.

Response shape (HTTP 200 always; never 5xx, consistent with today's contract):

```json
{
  "ok": true,
  "checks": {
    "reachable": {"ok": true},
    "embedding": {"ok": true, "dim": 1024, "expected": 1024},
    "chat": {"ok": true, "thinking_detected": false, "note": ""}
  }
}
```

`ok` is the AND of the individual checks. Each network call uses its own short
client timeout (≈10s) so the endpoint stays responsive; an unreachable base URL
yields `reachable.ok=false` and short-circuits the rest. Checks 2 and 3 are
best-effort: a check that errors reports `ok:false` with an `error` string rather
than failing the whole request.

### Component 4 — Settings UI

- The two new knobs render automatically in the DLP section via the registry
  (no per-field UI code).
- `frontend/src/pages/Settings.jsx` — extend the existing "Test DLP" result
  rendering to display the structured `checks` (a short pass/fail line per check,
  surfacing the embedding dim mismatch and the thinking-model note). When the
  response has no `checks` key (older shape), fall back to the current ok/error
  rendering.
- `api/client.js` — `testDLP()` passes the additional fields
  (`embedding_model`, `embedding_dim`, `risk_model`) from the current drafts.

### Component 5 — Documentation

Add a "Local models (Ollama)" section to `docs/dlp.md` covering:

- Example `dlp:` config block pointing at `http://host.docker.internal:11434/v1`
  (Docker) or `http://localhost:11434/v1` (native), with `api_key` empty.
- Recommended **direct-response** models (embedding: `mxbai-embed-large` 1024d or
  `all-minilm` 384d; chat: `qwen2.5-coder:7b` or `llama3.2:1b`) and the
  **thinking-model caveat** (why `gemma4`/`qwen3` return empty and how the
  fallback + `max_output_tokens` mitigate it).
- `embedding_dim` must match the model exactly, and changing it requires the
  drop-and-recreate of `agentguard_dlp_enriched` + `mv_dlp_enrich` then a
  re-provision (already documented; cross-reference).
- The new `request_timeout_seconds` / `max_output_tokens` knobs and the
  ≤55s-under-60s-client-ceiling relationship.
- Cold-start note (`OLLAMA_KEEP_ALIVE`) and the TP 3.2.1 brute-force-vs-HNSW note.

## Error handling

- All UDFs remain **fail-open** — unchanged. The fallback never raises; on any
  exception the existing `_fallback` / default-score / empty-summary paths apply.
- `test-dlp` never returns 5xx; every check failure is reported in-band.
- `PUT /api/config` rejects out-of-range knob values with a clear message
  (existing validation path in `sources.go`).

## Testing

Follow existing patterns (`config_test.go`, `internal/timeplus/dlp_test.go`,
`dlp_query_test.go`, `internal/handlers` settings tests):

- **Config**: load/Save round-trip for the two new keys; registry contains them
  with `Reprovision: true`; `sources.go` validation clamps/rejects out-of-range
  values.
- **DDL builders**: generated UDF DDL substitutes the configured timeout and
  `max_tokens`, `dlp_summarize` uses `max(n, 800)`, and every chat UDF body
  contains the `_msg_content` helper.
- **test-dlp**: drive the handler against an `httptest` stub OpenAI server
  covering reachable, embedding dim match, embedding dim mismatch, non-empty
  chat content, and empty-content-with-reasoning (thinking detection).
- Existing DLP unit tests continue to pass; UDF fail-open behavior preserved.

## Build order

1. Config: struct + defaults + registry + sources validation (+ tests).
2. UDF templates: `{{TIMEOUT}}`/`{{MAX_TOKENS}}` parameterization + `_msg_content`
   fallback + `dlpReplacer` change (+ DDL-builder tests).
3. `test-dlp` handler: structured checks (+ httptest tests).
4. Frontend: `testDLP()` payload + structured result rendering.
5. Docs: `docs/dlp.md` local-models section.
