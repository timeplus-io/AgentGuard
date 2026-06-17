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

`mv_dlp_enrich` calls three UDFs per message. All are provisioned by Go code
that interpolates config values (`base_url`, `api_key`, model names) into the
UDF source — the same pattern as `notify_agentguard_udf`. Python UDFs receive
batched lists, so the API-calling UDFs make **one request per batch**, not per
row.

| UDF | Signature | Behavior |
|-----|-----------|----------|
| `dlp_extract_entities` | `(content string) -> map(string, array(string))` | Regex + dictionary for phone, email, IP, domain, and secrets (AWS keys, `sk-`/`ghp_` tokens, JWTs, private keys). Secret values masked before return. **No network.** |
| `dlp_embed` | `(content string, role string) -> array(float32)` | Batched `POST {base_url}/embeddings` with `{embedding_model}`. The `role` param gates embedding inside Python (tool rows → `[]`). Truncates input to a safe length. On any error returns `[]`. |
| `dlp_risk` | `(content string, entity_types array(string)) -> string` (JSON) | Gated: with no entity types and no risk keywords, returns `{"score":0,"tags":[]}` without a network call. Otherwise a chat-completion to `{risk_model}` for strict-JSON multi-label classification; on error falls back to a heuristic derived from entity types. Returns e.g. `{"score":0.91,"tags":["PII","PHONE"]}`. |

**Fail-open invariant:** no UDF may raise. Enrichment degradation produces
empty/zero columns, never a blocked MV.

**Secrets caveat:** the model API key is embedded verbatim in the UDF source at
provision time, so it is visible via `SHOW FUNCTIONS`/`system.functions` to
anyone with Timeplus DDL access. This is a documented, accepted limitation —
Timeplus credentials already gate the raw conversation content; use a scoped
key for the model endpoint.

---

## 4. Query Pipeline

`POST /api/dlp/query {question, top_n?}` orchestrates the funnel in Go
(`backend/internal/handlers/dlp_query.go`); every model touch is a UDF invoked
through SQL. The response is an **NDJSON stream** — one JSON object per stage,
flushed as it completes, so the UI can render progressively.

1. **Understand** — `SELECT dlp_understand_query(:question)` → intent JSON
   (`keywords`, `entity_types`, `risk_tags`, `time_range_days`). On parse
   failure, keywords are derived from the question itself.
   → `{"stage":"intent","intent":{…}}`
2. **Multi-recall** — two channels over `table(agentguard_dlp_enriched)`:
   - **Structured** — entity/tag/keyword predicates from the intent, time
     filter, `ORDER BY risk_score DESC, event_time DESC LIMIT 200`. Recalls
     rows regardless of embedding presence.
   - **Vector** — time filter + `length(embedding) > 0`,
     `ORDER BY cosine_distance(embedding, dlp_embed(:question,'user')) LIMIT 100`.
     Best-effort; failure (e.g. dim drift) degrades to structured-only.

   Merged + deduped by `message_id` in Go → candidate set (≤300).
   → `{"stage":"candidates","count":N,"structured":N1,"vector":N2}`

   > This deviates from the spec's original single hybrid statement:
   > `cosine_distance` errors on empty embeddings, and fail-open rows have
   > `embedding = []`, so the channels are split and merged in Go.
3. **Rerank** — `SELECT dlp_rerank(:question, content)` over the candidate
   batch scores relevance 0–1 with `{risk_model}`; top `top_n` (default 20,
   max 50) kept. On failure, falls back to recall order.
   → `{"stage":"reranked","kept":K}`
4. **Summarize** — `SELECT dlp_summarize(:question, :evidence_json)` over the
   top-N → an audit report citing `message_id`s.
   → `{"stage":"answer","report":"…","evidence":[…]}`

Any stage error is terminal: `{"stage":"error","error":"…"}`. If DLP is
disabled or unprovisioned, the endpoint returns **503** (plain JSON, before
streaming starts) so the client handles it uniformly.

Query-time UDFs (`dlp_understand_query(question)`,
`dlp_rerank(question, content)`, `dlp_summarize(question, evidence)`) follow the
same provisioning/interpolation rules as the ingest UDFs;
`dlp_summarize` uses `{summary_model}` (frontier), the other two use
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
| `embedding_dim` | Must match the embedding model. **Baked into the stream/index at provision time** — changing it requires re-provisioning the DLP stream. |
| `risk_model` | Small model for `dlp_risk` and `dlp_rerank`. |
| `summary_model` | Frontier model for `dlp_summarize`. |

These settings are editable from the **Settings** page (`/settings`); changing a
model or `base_url` flags a re-provision (the values are baked into UDFs). The
last-provisioned snapshot is recorded under `provisioned.dlp.*` for drift
detection.

---

## 6. Provisioning

DLP provisioning lives in `backend/internal/timeplus/dlp.go`:

- **`InitDLP(cfg)`** — creates `agentguard_dlp_enriched`, attempts the HNSW
  index (best-effort), creates/replaces the six UDFs with interpolated config,
  and creates `mv_dlp_enrich`. Idempotent (`IF NOT EXISTS` / `CREATE OR
  REPLACE`). Skipped when `dlp.enabled: false`.
- Wired into **`timeplus.Init()`**, so the setup wizard
  (`POST /api/setup/provision`) and **`POST /api/rules/sync`** both pick it up —
  config changes (endpoint, models) apply without re-running the wizard.
- **`/api/dlp/status`** reports stream existence, MV state, UDF presence, row
  counts, and embedding coverage (rows with a non-empty embedding).

The server starts independently of Timeplus; if the instance is unreachable,
provisioning fails but the binary itself starts cleanly.

---

## 7. API Surface

All endpoints are behind the auth middleware (`/api/dlp/*`).

| Method | Path | Handler | Notes |
|--------|------|---------|-------|
| POST | `/api/dlp/query` | `DLPQuery` | NL question → funnel; streams NDJSON stage events. 503 if disabled/unprovisioned. |
| GET | `/api/dlp/messages` | `ListEnrichedMessages` | Browse enriched rows. Filters: `risk_tag`, `entity_type`, `agent_id`, `session_id`, `role`, `since`/`until` (RFC3339), `min_score`; paginated (`page`/`limit`, max 500). |
| GET | `/api/dlp/messages/:id` | `GetEnrichedMessage` | Full enriched record by `message_id`. |
| GET | `/api/dlp/profiles` | `DLPProfilesHandler` | Top risk agents / sessions / tags over a `?days=` window (default 30). |
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
