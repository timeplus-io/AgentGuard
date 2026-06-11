# Semantic DLP — Phase 1 Implementation Spec

> **Status:** Approved design (supersedes nothing; implements Phase 1 of
> `docs/research/semantic-dlp-design.md`)
> **Issue:** [#159](https://github.com/timeplus-io/AgentGuard/issues/159)
> **Date:** 2026-06-11

## 1. Goal

Let a security operator ask natural-language forensic questions about agent
conversation logs ("Who sent customer contact data to an external model?") and
get a sourced answer with cited messages. The large model only ever reasons
over the final ~20 candidates; all expensive enrichment is pre-computed at
ingest.

This spec narrows the research design to concrete Phase 1 decisions. Three
architectural choices were settled with the user:

1. **All ML inference runs as Timeplus Python UDFs** — no sidecar service, no
   backend-side model client. The Go backend only composes SQL.
2. **UDFs call a configurable OpenAI-compatible endpoint** via `requests`
   (LiteLLM proxy, OpenAI, or Ollama). No model weights inside Timeplus.
3. **Single enrichment MV with per-layer UDFs**, each failing open so ingest
   never stalls on a model-endpoint outage.

## 2. Data model

### 2.1 Stream `agentguard_dlp_enriched`

One row per conversation message, written by `mv_dlp_enrich`:

```sql
CREATE STREAM agentguard_dlp_enriched
(
  event_time      datetime64(3),
  message_id      string,                        -- hash(session_id, run_id, event_time, role)
  session_id      string,
  run_id          string,
  role            low_cardinality(string),       -- 'user' | 'assistant' | 'tool'
  agent_type      low_cardinality(string),
  agent_id        string,
  deployment_id   low_cardinality(string),
  model           low_cardinality(string),
  tool_name       string,                        -- '' except role='tool'
  content         string,                        -- message text (PII-bearing; behind auth)
  entities        map(string, array(string)),    -- entity type -> values; secrets masked
  risk_tags       array(low_cardinality(string)),
  risk_score      float64,
  embedding       array(float32)                 -- [] for tool rows and on enrichment failure
)
```

Differences from the research doc (§5.2): no `user_uid`/`user_name`/
`user_department` — `agentguard_cim_event` carries no human-user identity, so
"who" in Phase 1 means agent + deployment + session. No `attachments` column —
CIM has no attachment field. Both can be added in Phase 2 without migration
pain (new columns).

**Entities as a map, not fixed columns.** The research doc used one array
column per entity type (`entity_phone`, `entity_email`, …). This spec uses a
single `entities map(string, array(string))` instead: the entity taxonomy is
the part most likely to evolve (org-specific dictionaries, new secret
formats), and with a map a new entity type is a **UDF-only change** — no
`ALTER STREAM`, no MV rebuild. Canonical keys in Phase 1: `phone`, `email`,
`ip`, `domain`, `person`, `company`, `secret` (values under `secret` are
masked: type + first/last 4 chars). Query patterns:
`not empty(entities['phone'])`, `has(entities['person'], '张三')`,
`map_keys(entities)` for type filters. The per-type filter cost of a map is
marginally higher than dedicated columns, which is irrelevant at MVP scale.

**Vector index:** after creating the stream, provisioning attempts an HNSW
vector-similarity index on `embedding` (cosine). If the deployed Timeplus
Enterprise version rejects the DDL, provisioning logs and continues — queries
fall back to brute-force `cosine_distance`, which is acceptable at MVP scale.
The exact index DDL is validated against the live server during
implementation (research doc open question §14.1).

### 2.2 Row mapping from CIM

`mv_dlp_enrich` selects from `agentguard_cim_event`:

| CIM `event_type` | role | content | embedded? |
|------------------|------|---------|-----------|
| `user_input` | `user` | `user_message` | yes |
| `llm_response` | `assistant` | `assistant_message` | yes |
| `tool_complete` | `tool` | `tool_input` | **no** (entities + risk only) |

Rows with empty content are skipped. Tool rows get entity extraction and risk
tagging (file exfil signals) but no embedding, per the research doc's pitfall
#3 ("embed only user prompt + agent response").

## 3. Ingest enrichment

### 3.1 Pipeline

```
agentguard_cim_event ──► mv_dlp_enrich ──► agentguard_dlp_enriched
                              │
                              ├─ dlp_extract_entities(content)        L0/L1, pure Python
                              ├─ dlp_embed(content)                   L2, API callout
                              └─ dlp_risk(content, entities)          L3, gated API callout
```

### 3.2 UDFs (Python, `CREATE OR REPLACE FUNCTION ... LANGUAGE PYTHON`)

All three are provisioned by Go code that interpolates config values into the
UDF source (same pattern as the existing `notify_agentguard_udf` in
`backend/internal/timeplus/init.go`). Python UDFs receive batched lists, so
`dlp_embed` and `dlp_risk` make **one API call per batch**, not per row.

| UDF | Signature | Behavior |
|-----|-----------|----------|
| `dlp_extract_entities` | `(content string) -> map(string, array(string))` | Regex + small trie dictionary for phone, email, IP, domain, secrets (AK/SK, tokens, private keys), person/company names. Secret values masked before return (`type:abcd…wxyz`). No network. Returns the entity map directly (Python dict → SQL map); the MV stores it as-is in `entities` — adding an entity type later touches only this UDF. |
| `dlp_embed` | `(content string, role string) -> array(float32)` | Batched `POST {base_url}/embeddings` with `{embedding_model}`. The `role` param gates embedding inside Python (tool rows get `[]`) because SQL `if()` evaluates both branches. Truncates input to a safe length. On any error returns `[]` for the batch. |
| `dlp_risk` | `(content string, entity_types array(string)) -> string` (JSON) | Receives `map_keys(entities)` rather than the map (simpler UDF param typing). Internally gated: if no entity types and no risk keywords match, returns `{"score":0,"tags":[]}` without network. Otherwise a chat-completion to `{risk_model}` asking for strict-JSON multi-label classification. On error, falls back to a heuristic score derived from entity types. Returns `{"score":0.91,"tags":["PII","PHONE"]}`. |

**Verified against Timeplus Enterprise 3.2.1 (sub-issue 1, 2026-06-11):**
Python UDFs return `map(string, array(string))` natively — no JSON-cast
fallback needed (note: UDF *names* ending in `_map` are reserved). `SHOW
FUNCTIONS` returns a `name` column over HTTP JSON. The `vector_similarity`
HNSW index type does **not** exist on this build, so brute-force
`cosine_distance` (which works) is the retrieval path until the instance is
upgraded; the index DDL remains best-effort for newer builds.

**Fail-open invariant:** no UDF may raise; enrichment degradation produces
empty/zero columns, never blocks the MV. This matches the repo's recent
"do not block agent on network failure" direction.

**Config interpolation & secrets:** the API key is embedded in the UDF source
at provision time and is therefore visible via `SHOW FUNCTIONS` to anyone with
Timeplus DDL access. Documented as a known limitation; acceptable because
Timeplus credentials already gate raw conversation content.

## 4. Query pipeline

`POST /api/dlp/query {question, time_range?}` — Go orchestrates the funnel;
every model touch is a UDF invoked via SQL through the existing
`timeplus.Query()` client.

1. **Understand** — `SELECT dlp_understand_query(:question)` → intent JSON:
   `{intent, entities:{person, data_type…}, risk_tags, time_range, keywords}`.
2. **Multi-recall** — Go builds hybrid SQL over
   `table(agentguard_dlp_enriched)`:
   - Channel A (hybrid structured+vector): entity/tag/keyword/time predicates
     from the intent, `ORDER BY cosine_distance(embedding, dlp_embed(:question)) LIMIT 200`.
   - Channel B (pure vector, no predicates): catches semantic matches the
     structured filters would exclude. `LIMIT 100`.
   Results merged + deduped by `message_id` in Go → candidate set (≤300).
3. **Rerank** — `SELECT dlp_rerank(:question, candidate)` over the candidate
   batch; the UDF scores relevance 0–1 with `{risk_model}` in chunked batched
   calls. Top 20 kept. (Cross-encoder deferred; LLM-scoring is the Phase 1
   reranker.)
4. **Summarize** — `SELECT dlp_summarize(:question, :evidence_json)` with the
   top-20 messages → audit report: findings, each citing `message_id`s.
5. **Stream** — the endpoint streams NDJSON stage events as they complete:
   `{stage:"intent"…}`, `{stage:"candidates", count…}`, `{stage:"answer",
   report, citations:[…]}`. Frontend renders progressively.

Query-time UDFs `dlp_understand_query`, `dlp_rerank`, `dlp_summarize` follow
the same provisioning/interpolation rules as §3.2; `dlp_summarize` uses
`{summary_model}` (frontier), the other two use `{risk_model}` (small).

Failure handling: if DLP is not provisioned (stream or UDFs missing) the
endpoint returns 503 with a "provision required" message; per-stage errors
stream an `{stage:"error"}` event and end the response.

## 5. Configuration

New `dlp` section in `config.yaml`, loaded with the `config.LoadFrom()`
pattern (the global viper + AutomaticEnv path silently breaks nested
file-only keys):

```yaml
dlp:
  enabled: true
  base_url: http://localhost:4000/v1   # OpenAI-compatible (LiteLLM/OpenAI/Ollama)
  api_key: ""                          # overridden by env DLP_API_KEY
  embedding_model: text-embedding-3-small
  embedding_dim: 1536
  risk_model: gpt-4o-mini
  summary_model: gpt-4o
```

`embedding_dim` must match the model and is fixed at provision time (it is
baked into the stream/index); changing it requires re-provisioning the DLP
stream.

## 6. Provisioning

New file `backend/internal/timeplus/dlp.go`:

- `InitDLP(cfg)` — creates `agentguard_dlp_enriched`, attempts the HNSW index,
  creates/replaces the six UDFs with interpolated config, creates
  `mv_dlp_enrich`. Idempotent (`IF NOT EXISTS` / `CREATE OR REPLACE`).
- Wired into `timeplus.Init()` so the setup wizard (`POST /api/setup/provision`)
  and `POST /api/rules/sync` pick it up. Skipped when `dlp.enabled: false`.
- `DLPStatus(cfg)` — reports stream existence, MV state, UDF presence,
  row counts, and embedding coverage % (rows with non-empty embedding).

## 7. API surface

| Method | Path | Handler | Notes |
|--------|------|---------|-------|
| POST | `/api/dlp/query` | `DLPQuery` | NL question → funnel; streams NDJSON stage events |
| GET | `/api/dlp/messages` | `ListEnrichedMessages` | Filters: `risk_tag`, `entity_type`, `agent_id`, `session_id`, `role`, `since`/`until`, `min_score`; paginated (`page`/`limit`, max 500) |
| GET | `/api/dlp/messages/:id` | `GetEnrichedMessage` | Full enriched record by `message_id` |
| GET | `/api/dlp/profiles` | `DLPProfiles` | Top risk agents / sessions / tags over a window |
| GET | `/api/dlp/status` | `DLPStatusHandler` | Provisioning + enrichment health |

Handlers follow the existing convention: one file
`backend/internal/handlers/dlp.go` (+ `dlp_query.go` for the funnel), closure
over `cfg`, structs in `models.go`, routes registered in `cmd/server/main.go`
behind the auth middleware.

## 8. Frontend

New sidebar route **`/dlp` — "Semantic DLP"** (`pages/SemanticDLP.jsx`),
standard Sidebar/TopNav/Footer layout, Digital Sentry theme:

- **Ask box** with example prompts; submitting opens an NDJSON stream via a
  new `api.dlpQuery()` (async generator, same `ndjsonStreamParser` used by
  `protonQuery`).
- **Progressive answer pane** — renders intent → candidate count → final
  report as stages arrive; citations render as evidence cards linking to the
  message detail view and the session.
- **Risk profile widgets** — top risk agents / sessions / tags from
  `/api/dlp/profiles`.
- **Enriched message browser** — filterable table over `/api/dlp/messages`
  with severity-style badges for `risk_tags` / `risk_score`; row click shows
  the full record (entities, score, content).
- If `/api/dlp/status` reports unprovisioned, the page shows a setup call-to-
  action instead of the ask box.

## 9. Testing

- **Go unit tests** — handlers and funnel orchestration against a mocked
  Timeplus HTTP server (existing pattern in `internal/handlers/*_test.go`);
  intent-JSON parsing, candidate merge/dedup, NDJSON stage framing, fail
  states (unprovisioned, stage error).
- **UDF/MV integration** — a script (`backend/scripts/dlp_integration.sh` or
  Make target) that provisions against a live Timeplus with `base_url`
  pointed at a bundled mock OpenAI-compatible server (tiny Go binary serving
  deterministic embeddings/completions), inserts sample hook events, and
  asserts enriched rows + a full query round-trip.
- **Frontend** — ESLint + manual verification against the running stack.

## 10. Sub-issue split (sequential PRs, each merged before the next)

1. **DLP ingest foundation** — §2, §3, §5, §6 + `/api/dlp/status`.
   Done when: hook events inserted → enriched rows appear with entities,
   risk, embeddings (against mock endpoint); status endpoint reports healthy.
2. **DLP query pipeline + API** — §4, §7 (query/messages/profiles).
   Done when: NL question returns a cited report over seeded data via curl.
3. **Semantic DLP frontend** — §8.
   Done when: `/dlp` page runs the full ask → progressive answer → evidence
   drill-down flow against the live backend.
4. **Hardening & docs** — §9 integration script + mock model server, demo
   seed data, `docs/cim.md`/`CLAUDE.md` updates, close out #159 checklist.

## 11. Explicitly deferred (Phase 2/3 — do not build now)

Conversation graph / lineage, risk timeline, prompt-attack classifier,
query/result caching, hot/cold tiering automation, human-user identity
mapping, PII masking of `content` at rest, operator query auditing,
cross-encoder reranker, auto-emitting high-risk rows into
`agentguard_security_events`.
