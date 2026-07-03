# DLP UDF single-config signature

**Date:** 2026-07-02
**Issue:** [#195](https://github.com/timeplus-io/AgentGuard/issues/195) (follow-up to #189)

## Problem

Every DLP network UDF currently takes **two** config inputs — a `shared` string and its
own `config` string — and merges them internally:

```sql
dlp_embed(content string, role string, shared string, config string)
dlp_risk(content string, entity_types array(string), shared string, config string)
dlp_understand_query(question string, shared string, config string)
dlp_rerank(question string, content string, shared string, config string)
dlp_summarize(question string, evidence string, shared string, config string)
```

Passing `shared` as a distinct argument is a leaky abstraction: the UDF is handed the
entire shared endpoint/model catalog even though it only needs its own resolved
configuration. Two helpers — `_merged(shareds, configs)` (deep-merge `shared ⊕ own`) and
`_endpoint(cfg)` (resolve the `model` name against `shared.models`) — are **copy-pasted
into all five UDFs**, coupling each callee to config-composition concerns that belong to
the caller.

## Constraint that shapes the solution

Go cannot pre-merge and pass a literal config for the ingest path: the enrichment MV
(`mv_dlp_enrich`) is created **once** with static SQL and must pick up runtime config
changes live — that is why `shared` and each UDF's own row arrive as **streaming LEFT
JOIN** columns. A scalar sub-query is *frozen at MV creation*, so the merge cannot be a
sub-query either. Therefore the merge must happen either inside the UDF (today) or in a
**function over the JOINed columns**.

## Design: a resolver UDF

### New UDF — `dlp_config_resolve(shared string, config string) → string`

Pure Python, **no network**. It:

1. Deep-merges `shared ⊕ own` (own wins) — the current `_merged` logic.
2. Resolves the `model` name against the merged `models` catalog — the current
   `_endpoint` logic — overwriting `base_url` / `api_key` / `model` with the concrete
   endpoint (per-entry override → shared top-level fallback → literal passthrough for an
   unknown name).
3. **Strips the `models` catalog** from the output, so the emitted JSON carries only what
   that one UDF runs with. No other models leak.

This is the single home for merge + endpoint resolution, replacing five copies.

### Network UDFs become flat-config readers

Signatures drop `shared`:

```sql
dlp_embed(content string, role string, config string)
dlp_risk(content string, entity_types array(string), config string)
dlp_understand_query(question string, config string)
dlp_rerank(question string, content string, config string)
dlp_summarize(question string, evidence string, config string)
```

Each body removes `_merged` and `_endpoint` and reads flat keys off the single parsed
config: `base_url`, `api_key`, `model`, `timeout`, plus per-UDF `max_tokens` /
`max_chars` / `keywords` / `tag_for`. Fail-open behavior (empty/zero on outage) is
unchanged.

`dlp_extract_entities(content, config)` is **unchanged** — it is non-network and never
took `shared`.

### Ingest MV (`ddlDLPEnrichMV`)

Keep the LEFT JOINs producing `cfg_shared`, `cfg_entities`, `cfg_risk`, `cfg_embed`. Wrap
each network call with the resolver over the JOINed columns:

```sql
dlp_risk(content, map_keys(entities), dlp_config_resolve(cfg_shared, cfg_risk))
...
dlp_embed(content, role, dlp_config_resolve(cfg_shared, cfg_embed))
```

Resolution is a per-row function over JOINed columns → evaluated live per event, never a
frozen sub-query.

### Funnel (`dlp_query.go`)

Replace each `dlpCfgSubquery(shared), dlpCfgSubquery(key)` pair with one resolved arg via
a new helper:

```go
func dlpResolvedCfg(key string) string {
    return fmt.Sprintf("dlp_config_resolve(%s, %s)",
        dlpCfgSubquery(tp.DLPCfgKeyShared), dlpCfgSubquery(key))
}
```

Four call sites: understand, recall-embed, rerank, summarize. (The funnel re-plans each
run, so scalar sub-queries as resolver args are fine here.)

### Probe SQL (`dlpProbeDimSQL`)

Wrap the `dlp_embed` probe call the same way.

### Provisioning

Register `dlp_config_resolve` in the UDF create list (order-independent; it is pure). No
config-stream schema change — the `shared` and per-UDF rows are unchanged; only who reads
them moves.

## Testing

- Update `TestDLPQueryUDFSignatures` and `TestDLPQueryUDFsParseConfig` (`dlp_query_test.go`)
  and `TestDLPEmbedUDFSignature` / `TestDLPUDFsAreStaticAndKeyless` (`dlp_test.go`) to the
  new signatures (no `shared`; no `_merged`/`_endpoint` in the network bodies).
- Add a `dlp_config_resolve` DDL test: asserts the signature, that it carries the merge +
  catalog-resolution logic, and that it removes `models` from its output.
- MV / funnel SQL string assertions updated to expect `dlp_config_resolve(...)` wrapping.
- Verify: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...`. Optional
  end-to-end: `make dlp-integration` against a live stack.

## Docs

Update the Semantic DLP sections of `CLAUDE.md` and `docs/dlp.md` to describe the resolver
UDF and the single-config signatures.

## Out of scope

- No change to the `agentguard_dlp_config` stream schema or the `shared`/per-UDF row
  layout.
- No change to `dlp_extract_entities`.
- No change to fail-open semantics or the config seeding flow.
