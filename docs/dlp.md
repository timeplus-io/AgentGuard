# AgentGuard — Semantic DLP

**Semantic DLP** (Data Leakage Prevention) is a natural-language forensic layer
over agent conversation logs. It lets a security operator ask questions like
*"Who sent customer contact data to an external model?"* or *"Which sessions
leaked source code?"* and get a **sourced answer with cited messages**, instead
of only the rule-based detections that `agentguard_security_events` provides.

The design principle is cost control through a funnel: all expensive enrichment
(entity extraction, embeddings, risk classification) is **pre-computed at
ingest**, and the large summarization model only ever reasons over the final
~20 candidate messages — never the full corpus.

> **Implements:** parent issue [#159](https://github.com/timeplus-io/AgentGuard/issues/159), Phase 1.
> **Design spec:** [`docs/superpowers/specs/2026-06-11-semantic-dlp-design.md`](superpowers/specs/2026-06-11-semantic-dlp-design.md).
> **Built on:** the CIM event layer — see [`docs/cim.md`](cim.md#6-downstream-semantic-dlp).

---

## Table of Contents

1. [Architecture](#1-architecture)
2. [Data Model](#2-data-model)
3. [Ingest Enrichment](#3-ingest-enrichment)
4. [Query Pipeline](#4-query-pipeline)
5. [Configuration](#5-configuration)
6. [Provisioning](#6-provisioning)
7. [API Surface](#7-api-surface)
8. [Frontend](#8-frontend)
9. [Verification & Demo](#9-verification--demo)
10. [Limitations & Deferred Work](#10-limitations--deferred-work)

---

## 1. Architecture

Two architectural decisions shape the whole feature:

1. **All ML inference runs as Timeplus Python UDFs.** There is no sidecar
   service and no model client in the Go backend — the backend only composes
   SQL. UDFs call a configurable **OpenAI-compatible endpoint** (`dlp.base_url`:
   LiteLLM proxy, OpenAI, Ollama, vLLM, …) via `requests`.
2. **Every UDF fails open.** A model-endpoint outage degrades a row to
   empty/zero columns; it never raises, never stalls the enrichment MV, and
   never blocks ingest.

```
                    INGEST (real-time, per message)                    QUERY (per question)

agentguard_cim_event                                         POST /api/dlp/query
        │                                                            │
        ▼  mv_dlp_enrich                                             ▼  dlp_understand_query   (intent JSON)
  ┌──────────────────────────┐                                      │
  │ dlp_extract_entities (L1)│  regex/dictionary, no network        ▼  two-channel recall
  │ dlp_embed            (L2)│  batched embeddings API                 ├─ structured predicates
  │ dlp_risk            (L3)│  gated risk classification               └─ vector cosine_distance
  └──────────────────────────┘                                      │   (merged + deduped in Go)
        │                                                            ▼  dlp_rerank             (top ~20)
        ▼                                                            │
agentguard_dlp_enriched  ───────────────────────────────────────►   ▼  dlp_summarize          (cited report)
 (entities, risk_tags, risk_score, embedding)                        │
                                                                     ▼  NDJSON stage stream → UI
```

The layered model (from the research design) keeps cost roughly two orders of
magnitude below full-LLM analysis:

| Layer | Work | Engine | Coverage |
|-------|------|--------|----------|
| L0 Rules | regex (phone/token/secret) | existing rules MVs | 100% |
| L1 Entities | phone/email/ip/domain/secret | `dlp_extract_entities` (no network) | 100% of messages |
| L2 Embedding | user prompt + agent response | `dlp_embed` (embedding model) | user/assistant messages |
| L3 Risk | "is this leakage?" multi-label | `dlp_risk` (small model, gated) | flagged messages |
| L4 Report | natural-language audit | `dlp_summarize` (frontier model) | **only top ~20 / query** |

---

## 2. Data Model

### 2.1 Stream `agentguard_dlp_enriched`

One row per conversation message, written by `mv_dlp_enrich`
(`backend/internal/timeplus/dlp.go`):

```sql
CREATE STREAM agentguard_dlp_enriched
(
  event_time     datetime64(3),
  message_id     string,                       -- city_hash64(session_id, run_id, event_time, role)
  session_id     string,
  run_id         string,
  role           low_cardinality(string),      -- 'user' | 'assistant' | 'tool'
  agent_type     low_cardinality(string),
  agent_id       string,
  deployment_id  low_cardinality(string),
  model          low_cardinality(string),
  tool_name      string,                        -- '' except role='tool'
  content        string,                        -- message text (PII-bearing; behind auth)
  entities       map(string, array(string)),    -- entity type -> values; secrets masked
  risk_tags      array(low_cardinality(string)),
  risk_score     float64,
  embedding      array(float32)                 -- [] for tool rows and on enrichment failure
)
```

After the stream is created, provisioning attempts an HNSW
`vector_similarity` index on `embedding` (cosine). On Timeplus Enterprise builds
that lack that index type, provisioning logs and continues — retrieval falls
back to brute-force `cosine_distance`, which is correct (just slower) at MVP
scale.

**Why `entities` is a map, not fixed columns:** the entity taxonomy is the part
most likely to evolve. With a `map(string, array(string))`, adding a new entity
type is a **UDF-only change** — no `ALTER STREAM`, no MV rebuild. Canonical keys
in Phase 1: `phone`, `email`, `ip`, `domain`, `secret` (secret values are
masked — `prefix***suffix`). Query patterns: `not empty(entities['phone'])`,
`has(entities['person'], '…')`, `map_keys(entities)`.

### 2.2 Row mapping from CIM

`mv_dlp_enrich` selects three event types from `agentguard_cim_event`:

| CIM `event_type` | `role` | `content` source | embedded? |
|------------------|--------|------------------|-----------|
| `user_input` | `user` | `user_message` | yes |
| `llm_response` | `assistant` | `assistant_message` | yes |
| `tool_complete` | `tool` | `tool_input` | **no** (entities + risk only) |

Rows with empty content are skipped. Tool rows get entity extraction and risk
tagging (file-exfil signals) but no embedding, per the design's "embed only the
prompt and the response" rule. Because the MV reads normalized CIM columns
rather than agent-specific hook payloads, **no per-agent DLP code is needed** —
Claude Code, OpenClaw, and Hermes all enrich identically.

---

## 3. Ingest Enrichment

`mv_dlp_enrich` calls three UDFs per message. The UDF bodies are **static** — every
config value (endpoint, model names, timeouts, token budgets, the entity catalog,
risk keywords, tag map) arrives through `config` string parameters, read live from
the `agentguard_dlp_config` mutable stream via streaming `LEFT JOIN`s on literal
keys (see §5). The network UDFs take **two** config args — the **`shared`** row
(`base_url`, `api_key`, `timeout`, common to all) and their own per-UDF row
(`model`, `max_tokens`, …) — and merge them at call time (`shared ⊕ own`, own
wins). An operator upsert therefore changes ingest behavior on the **next event**,
with no UDF redeploy and no re-provision. Python UDFs receive batched lists, so
the API-calling UDFs make **one request per batch**, not per row, and parse
config once per batch.

| UDF | Signature | Behavior |
|-----|-----------|----------|
| `dlp_extract_entities` | `(content, config) -> map(string, array(string))` | Network-free, so no `shared` arg. Regex + dictionary extraction driven by `config.entities[]` (default catalog: phone, email, IP, domain, and secrets — AWS keys, `sk-`/`ghp_` tokens, JWTs, private keys). Each entry has `kind` (`regex`\|`dictionary`), `store` (`raw`\|`masked`), `tag`, optional `flags`. Patterns are compiled once per distinct config blob. Secret/`masked` values are masked before return. |
| `dlp_embed` | `(content, role, shared, config) -> array(float32)` | Batched `POST {base_url}/embeddings` with `{config.model}` (endpoint from `shared`). `role` gates embedding (tool rows → `[]`). Truncates to `config.max_chars`. On any error or absent `base_url` returns `[]`. |
| `dlp_risk` | `(content, entity_types, shared, config) -> string` (JSON) | Gated by `config.keywords`: with no entity types and no keyword, returns `{"score":0,"tags":[]}` without a network call. Otherwise a chat-completion to `{config.model}` (endpoint from `shared`); on error (or absent `base_url`) falls back to a heuristic using `config.tag_for`. Returns e.g. `{"score":0.91,"tags":["PII","PHONE"]}`. |

**Fail-open invariant:** no UDF may raise. Enrichment degradation produces
empty/zero columns, never a blocked MV. The MV uses `LEFT JOIN` + `coalesce`, so
a missing config row never drops an event — the UDF just sees `''` and fails
open.

**Secrets note:** the model API key now lives in the `agentguard_dlp_config`
stream (a `value` column) rather than in the UDF source, so it is **no longer
exposed via `SHOW FUNCTIONS`/`system.functions`**. It is still readable by anyone
with access to the config stream; Timeplus credentials already gate the raw
conversation content, so use a scoped key for the model endpoint.

---

## 4. Query Pipeline

`POST /api/dlp/query {question, top_n?}` orchestrates the funnel in Go
(`backend/internal/handlers/dlp_query.go`); every model touch is a UDF invoked
through SQL. The response is an **NDJSON stream** — one JSON object per stage,
flushed as it completes, so the UI can render progressively.

Every funnel UDF also takes `shared` + `config` arguments, each supplied as a
coalesced scalar sub-query against `agentguard_dlp_config`
(`coalesce((SELECT value FROM table(agentguard_dlp_config) WHERE key='…'), '')`).
Unlike the ingest MV, a historical query is re-planned on each execution, so the
sub-query always reads the latest upsert — `coalesce(…, '')` keeps the argument
non-nullable (a nullable arg makes map/array-returning UDFs fail to type).

1. **Understand** — `SELECT dlp_understand_query(:question, :shared, :config)` → intent JSON
   (`keywords`, `entity_types`, `risk_tags`, `time_range_days`). On parse
   failure, keywords are derived from the question itself.
   → `{"stage":"intent","intent":{…}}`
2. **Multi-recall** — two channels over `table(agentguard_dlp_enriched)`:
   - **Structured** — entity/tag/keyword predicates from the intent, time
     filter, `ORDER BY risk_score DESC, event_time DESC LIMIT 200`. Recalls
     rows regardless of embedding presence.
   - **Vector** — time filter + `length(embedding) > 0`,
     `ORDER BY cosine_distance(embedding, dlp_embed(:question,'user',:shared,:config)) LIMIT 100`.
     Best-effort; failure (e.g. dim drift) degrades to structured-only.

   Merged + deduped by `message_id` in Go → candidate set (≤300).
   → `{"stage":"candidates","count":N,"structured":N1,"vector":N2}`

   > This deviates from the spec's original single hybrid statement:
   > `cosine_distance` errors on empty embeddings, and fail-open rows have
   > `embedding = []`, so the channels are split and merged in Go.
3. **Rerank** — `SELECT dlp_rerank(:question, content, :shared, :config)` over the candidate
   batch scores relevance 0–1 with `{risk_model}`; top `top_n` (default 20,
   max 50) kept. On failure, falls back to recall order.
   → `{"stage":"reranked","kept":K}`
4. **Summarize** — `SELECT dlp_summarize(:question, :evidence_json, :shared, :config)` over the
   top-N → an audit report citing `message_id`s.
   → `{"stage":"answer","report":"…","evidence":[…]}`

Any stage error is terminal: `{"stage":"error","error":"…"}`. If DLP is
disabled or unprovisioned, the endpoint returns **503** (plain JSON, before
streaming starts) so the client handles it uniformly.

Query-time UDFs (`dlp_understand_query(question, shared, config)`,
`dlp_rerank(question, content, shared, config)`, `dlp_summarize(question, evidence, shared, config)`)
are static and config-driven like the ingest UDFs; their default config rows set
`dlp_summarize` to `{summary_model}` (frontier) and the other two to
`{risk_model}` (small).

---

## 5. Configuration

A `dlp` section in `config.yaml` (loaded with the `config.LoadFrom()` pattern,
since global viper + `AutomaticEnv` silently breaks nested file-only keys):

```yaml
dlp:
  enabled: true
  base_url: http://localhost:4000/v1   # OpenAI-compatible (LiteLLM / OpenAI / Ollama / vLLM)
  api_key: ""                          # overridden by env DLP_API_KEY
  embedding_model: text-embedding-3-small
  embedding_dim: 1536
  risk_model: gpt-4o-mini
  summary_model: gpt-4o
```

| Key | Notes |
|-----|-------|
| `enabled` | When `false`, provisioning skips DLP and the API endpoints return 503. |
| `base_url` | The URL the **UDFs** call. When Timeplus runs in Docker and the endpoint is on the host, this is `http://host.docker.internal:<port>/v1`. |
| `embedding_dim` | Must match the embedding model. **Baked into the stream/index at provision time** — changing it requires re-provisioning the DLP stream. The **only** setting that still needs a re-provision. |
| `risk_model` | Small model for `dlp_risk`, `dlp_rerank`, `dlp_understand_query`. |
| `summary_model` | Frontier model for `dlp_summarize`. |

### 5.1 The `agentguard_dlp_config` mutable stream (runtime config)

`config.yaml` provides **boot defaults**. At provision time those defaults are
rendered (`DLPConfigRows`) into a **shared** endpoint row plus one row per UDF in
a **mutable** stream:

```sql
CREATE MUTABLE STREAM agentguard_dlp_config (
  key string, value string,            -- value is a JSON blob
  updated_at datetime64(3) DEFAULT now64(3), updated_by string
) PRIMARY KEY (key)
```

Keys and what each holds (a network UDF's effective config is `shared ⊕ its own
row`, its own row winning):

| Key | Holds |
|-----|-------|
| `shared` | `base_url`, `api_key`, `timeout`, and **`models`** — common to every network UDF; set the endpoint, rotate the key, or define models here **once** |
| `dlp_extract_entities` | `entities` catalog + `max_per_type` (network-free; no `shared`) |
| `dlp_embed` | `model` (catalog name), `max_chars` |
| `dlp_risk` | `model` (name), `max_tokens`, `keywords`, `tag_for` |
| `dlp_understand_query` / `dlp_rerank` | `model` (name), `max_tokens` |
| `dlp_summarize` | `model` (name), `max_tokens` |

**Named model catalog.** `shared.models` maps a name → an actual model id; each
UDF's `model` is a **name** that resolves against it at call time. So a model is
defined once and pointed at by several UDFs — e.g. `risk`, `understand`, and
`rerank` all reference `"chat"`, so changing `shared.models.chat` updates all
three. Defaults:

```json
shared:     {"base_url":"…","api_key":"…","timeout":50,
             "models":{"chat":"gpt-4o-mini","embedding":"text-embedding-3-small","summary":"gpt-4o"}}
dlp_embed:  {"model":"embedding","max_chars":8000}
dlp_risk:   {"model":"chat","max_tokens":512,"keywords":[…],"tag_for":{…}}
dlp_summarize: {"model":"summary","max_tokens":800}
```

Names are arbitrary — define `model1`/`model2`/… and point each UDF wherever you
like. A `model` that isn't a catalog name is used as a **literal** model id
(backward compatible). A per-UDF row may also override any shared field (e.g.
point one UDF at a different `base_url` or its own `models`) since it wins the
merge.

Because the stream is read/write, **the config stream is authoritative at
runtime**. The preferred way to change it is the API
(`GET /api/dlp/config`, `PUT /api/dlp/config/:key` — which validates the JSON,
structurally checks the entity catalog, and preserves the stored `api_key` when
omitted). You can also upsert directly via SQL — re-`INSERT` the same `key` — and
the ingest MV (next event) and funnel (next query) use it immediately, no rebuild
and no re-provision:

```sql
-- add a credit-card pattern to the entity catalog, live:
INSERT INTO agentguard_dlp_config (key, value, updated_by)
VALUES ('dlp_extract_entities',
        '{"max_per_type":20,"entities":[ … ,{"type":"credit_card","kind":"regex","store":"masked","tag":"FINANCIAL","pattern":"(?<!\\d)(?:\\d[ -]?){13,16}(?!\\d)"}]}',
        'analyst');
```

Validate regex patterns before upserting (a bad pattern is silently skipped by
the fail-open extractor, so a typo means "type never matches", not an error).
Loose patterns also raise per-message LLM cost — any detected entity un-gates
`dlp_risk` — so pattern *precision* is a cost lever.

**Seeding is seed-if-absent:** `InitDLP`/`SyncDLP` insert a default row only for a
key that does not already exist, so re-provisioning or `POST /api/rules/sync`
never clobbers runtime edits. To **reset** a row to the `config.yaml` default,
delete it (`DELETE FROM agentguard_dlp_config WHERE key='…'`) and re-run sync.

**Re-provision vs. hot config:** only `embedding_dim` is baked at provision time
(it sizes the embedding column and HNSW index), so it is the only change
`/settings` flags as `reprovision_required`. Model/endpoint/timeout/token/catalog
changes are hot via the stream. Note that editing those in `config.yaml` only
takes effect on the **next fresh seed** (a key that's already present is
preserved); change them at runtime by upserting the stream.

---

## 6. Provisioning

DLP provisioning lives in `backend/internal/timeplus/dlp.go`:

- **`InitDLP(cfg)`** — creates `agentguard_dlp_enriched`, drops the enrichment MV
  (so it is rebuilt with the current definition and never blocks a UDF replace),
  attempts the HNSW index (best-effort), creates the `agentguard_dlp_config`
  mutable stream and **seeds it (seed-if-absent)** from `DLPConfigRows(cfg)`,
  creates/replaces the six static UDFs, and recreates `mv_dlp_enrich` (which
  `LEFT JOIN`s the config stream). Idempotent. Skipped when `dlp.enabled: false`.
- Wired into **`timeplus.Init()`**, so the setup wizard
  (`POST /api/setup/provision`) and **`POST /api/rules/sync`** both pick it up.
  Note re-provision rebuilds the UDFs/MV and seeds *missing* config rows, but
  does **not** overwrite existing config rows — runtime model/endpoint changes
  are made by upserting `agentguard_dlp_config`, not by re-provisioning (§5.1).
- **`/api/dlp/status`** reports stream existence, MV state, UDF presence, row
  counts, and embedding coverage (rows with a non-empty embedding).

The server starts independently of Timeplus; if the instance is unreachable,
provisioning fails but the binary itself starts cleanly.

**Enabling from the UI.** When DLP is off or unprovisioned, the `/dlp` page shows
a call-to-action. If `dlp.enabled` is `false`, the button reads **Enable Semantic
DLP** — it flips `dlp.enabled` (a hot config key, no restart) via
`PUT /api/config` and then runs provisioning via `POST /api/rules/sync`. If DLP is
already enabled but the streams/UDFs/MV are missing, the button reads **Provision
now** and only runs the sync. (If `DLP_ENABLED` is set as an environment variable
it overrides the file and the UI can't change it — unset it to control from the
UI.)

---

## 7. API Surface

All endpoints are behind the auth middleware (`/api/dlp/*`).

| Method | Path | Handler | Notes |
|--------|------|---------|-------|
| POST | `/api/dlp/query` | `DLPQuery` | NL question → funnel; streams NDJSON stage events. 503 if disabled/unprovisioned. |
| GET | `/api/dlp/messages` | `ListEnrichedMessages` | Browse enriched rows. Filters: `risk_tag`, `entity_type`, `agent_id`, `session_id`, `role`, `since`/`until` (RFC3339), `min_score`; paginated (`page`/`limit`, max 500). |
| GET | `/api/dlp/messages/:id` | `GetEnrichedMessage` | Full enriched record by `message_id`. |
| GET | `/api/dlp/profiles` | `DLPProfilesHandler` | Top risk agents / sessions / tags over a `?days=` window (default 30). |
| GET | `/api/dlp/config` | `GetDLPConfig` | Current `agentguard_dlp_config` rows (one per UDF). `api_key` redacted; `api_key_set` reports whether one is stored. |
| PUT | `/api/dlp/config/:key` | `UpsertDLPConfig` | Upsert one config row (`{"value": <json>}`). Validates JSON (+ entity-catalog structure); preserves the stored `api_key` when the body omits/blanks it. Takes effect live, no re-provision. |
| GET | `/api/dlp/status` | `DLPStatusHandler` | Provisioning + enrichment health. |
| POST | `/api/config/test-dlp` | `TestDLPConfig` | Tests `base_url` reachability (`GET /models`); never 5xx on an unreachable endpoint. |

Handlers live in `backend/internal/handlers/dlp.go` (+ `dlp_query.go` for the
funnel); structs are in `models.go`; routes are registered in
`cmd/server/main.go`.

---

## 8. Frontend

Sidebar route **`/dlp` — "Semantic DLP"** (`frontend/src/pages/SemanticDLP.jsx`),
standard Sidebar/TopNav/Footer layout, Digital Sentry theme:

- **Ask box** with example prompts; submitting opens an NDJSON stream via
  `api.dlpQuery()` (async generator, same parser as `protonQuery`).
- **Progressive answer pane** — renders intent → candidate count → final report
  as stages arrive; citations render as evidence cards linking to the message
  detail and the session.
- **Risk profile widgets** — top risk agents / sessions / tags from
  `/api/dlp/profiles`.
- **Enriched-message browser** — filterable table over `/api/dlp/messages` with
  severity-style badges for `risk_tags` / `risk_score`; row click shows the full
  record (entities, score, content).
- If `/api/dlp/status` reports unprovisioned, the page shows a setup
  call-to-action instead of the ask box.

---

## 9. Verification & Demo

A deterministic mock model server and an end-to-end script let you exercise the
whole feature with **no real model endpoint and no model weights** — useful for
CI and local development. Everything lives under `backend/scripts/`.

| File | Purpose |
|------|---------|
| `dlp_mock_model.py` | Mock OpenAI-compatible server — deterministic embeddings (sha256-derived) and canned completions for query-understanding / rerank / summary / risk; also serves `GET /models` and `/health`. |
| `dlp_sample_events.json` | Demo hook events: a customer-PII leak, a credential paste (masked secret), a source-code exfiltration tool call, and benign control messages, across Claude Code and OpenClaw. |
| `dlp_load_events.py` | Dependency-free loader: stamps recent `event_time`s and inserts the sample events into `agentguard_hook_events` over the `:8123` ClickHouse HTTP interface. |
| `dlp_seed.sh` | Seed a **provisioned, running** stack for the `/dlp` page. |
| `dlp_integration.sh` | Full end-to-end test against a **live** Timeplus. |

Make targets:

```bash
make dlp-mock          # run the mock model server on :4000
make dlp-seed          # load demo data into a provisioned, running stack (then open /dlp)
make dlp-integration   # full end-to-end test
```

`make dlp-integration` runs: start mock → build + run the backend pointed at the
mock → `POST /api/setup/provision` → insert sample hook events → wait for the
`hook_events → cim_event → dlp_enriched` pipeline → assert enriched rows
(entities, risk, embeddings, all six UDFs present) → `POST /api/dlp/query` and
assert the `intent → candidates → reranked → answer` NDJSON stages with cited
evidence → check `/api/dlp/messages` and `/api/dlp/profiles`. It prints a
PASS/FAIL summary and exits non-zero on any failure.

**Configuration** is via environment variables with docker-compose defaults
(`TIMEPLUS_HOST`, `TIMEPLUS_USER`, `TIMEPLUS_PASSWORD`, `TIMEPLUS_HTTP_PORT`,
`TIMEPLUS_PORT`, `TIMEPLUS_NATIVE_PORT`, `MOCK_PORT`, `EMBED_DIM`,
`BACKEND_PORT`, `DLP_BASE_URL`). The most important knob is **`DLP_BASE_URL`** —
the URL the Timeplus UDFs use to reach the mock. The default
(`http://host.docker.internal:4000/v1`) works when Timeplus runs in Docker; for
a native Timeplus on the same host set
`DLP_BASE_URL=http://localhost:4000/v1`.

Typical run against the docker-compose stack:

```bash
make compose-up         # Timeplus + AgentGuard
make dlp-integration    # → "PASS — Semantic DLP end-to-end integration succeeded."
```

The integration test is intentionally a **local/manual** target — it depends on
a live Timeplus, which CI treats as an external dependency (no GitHub Actions
job spins one up).

---

## 10. Limitations & Deferred Work

Explicitly **not** built in Phase 1 (Phase 2/3):

- Conversation graph / lineage and risk timeline.
- Prompt-attack classifier.
- Query/result caching; hot/cold tiering automation.
- Human-user identity mapping (Phase 1 "who" = agent + deployment + session;
  CIM carries no human-user identity).
- PII masking of `content` at rest; operator query auditing.
- Cross-encoder reranker (Phase 1 uses LLM-scoring via `dlp_rerank`).
- Auto-emitting high-`risk_score` rows into `agentguard_security_events`
  (bridge back into threats/holds).

Known limitations of the shipped surface:

- The model API key is visible to anyone with Timeplus DDL access (§3).
- `embedding_dim` is fixed at provision time; changing it requires
  re-provisioning the DLP stream (§5).
- The HNSW vector index is best-effort; on builds without `vector_similarity`,
  retrieval uses brute-force `cosine_distance` (§2.1).

---

## References

- Design spec: [`docs/superpowers/specs/2026-06-11-semantic-dlp-design.md`](superpowers/specs/2026-06-11-semantic-dlp-design.md)
- Research design: [`docs/research/semantic-dlp-design.md`](research/semantic-dlp-design.md)
- CIM layer (this feature's upstream): [`docs/cim.md`](cim.md#6-downstream-semantic-dlp)
- Code: `backend/internal/timeplus/dlp.go`, `dlp_query.go`;
  `backend/internal/handlers/dlp.go`, `dlp_query.go`;
  `frontend/src/pages/SemanticDLP.jsx`; `backend/scripts/dlp_*`

---

## Local models (Ollama)

Semantic DLP works with any OpenAI-compatible endpoint, including a local
[Ollama](https://ollama.com) server. Because the model UDFs run **inside
Timeplus**, `dlp.base_url` must be reachable from the Timeplus process.

```yaml
dlp:
  enabled: true
  base_url: http://host.docker.internal:11434/v1   # Timeplus in Docker, Ollama on host
  # base_url: http://localhost:11434/v1            # Timeplus running natively
  api_key: ""                                       # Ollama ignores auth
  embedding_model: mxbai-embed-large                # 1024 dims
  embedding_dim: 1024
  risk_model: qwen2.5-coder:7b
  summary_model: qwen2.5-coder:7b
  request_timeout_seconds: 50                        # clamped to [5,55]
  max_output_tokens: 512                             # raise for verbose/reasoning models
```

**Use direct-response models, not reasoning models.** Reasoning models
(`gemma4`, `qwen3`, …) emit chain-of-thought into a separate `reasoning`
field and only fill `content` afterward, so under a small token budget the
UDF gets empty `content`. AgentGuard mitigates this by falling back to the
`reasoning` field and by the configurable `max_output_tokens`, but a
direct-response model (`qwen2.5-coder:7b`, `llama3.2:1b`) is recommended.
The Settings → Semantic DLP **Test endpoint** button now verifies the
embedding dimension and detects empty-content reasoning models.

**Embedding dimension must match the model** and is baked into the
`agentguard_dlp_enriched` stream. Changing `embedding_dim` requires
dropping and recreating the stream + MV before re-provisioning:

```sql
DROP VIEW IF EXISTS mv_dlp_enrich;
DROP STREAM IF EXISTS agentguard_dlp_enriched;
```
then `POST /api/rules/sync` (or re-run the setup wizard).

**Timeouts.** `request_timeout_seconds` is the per-call model timeout in the
UDFs. It is clamped to ≤ 55s so the query funnel returns before the 60s
internal query-client ceiling. Local 7B summaries over ~20 evidence
messages take ~15–20s warm.

**Cold start.** Ollama unloads idle models after ~5 min; the first query
after a gap pays a model-load penalty. Set `OLLAMA_KEEP_ALIVE=-1` (or a long
duration) to keep models resident.

**Vector index.** Timeplus builds where the `vector_similarity` (HNSW) index
type is unavailable fall back to brute-force `cosine_distance`. This is
correct, just slower at large row counts.
