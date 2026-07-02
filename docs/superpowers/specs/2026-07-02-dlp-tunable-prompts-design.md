# DLP tunable LLM prompts

**Date:** 2026-07-02
**Issue:** [#197](https://github.com/timeplus-io/AgentGuard/issues/197) (builds on #195/#196)

## Problem

The four LLM-calling DLP UDFs hardcode their prompt in the Python UDF body.
Operators can tune the endpoint, model, timeout, and token budget at runtime via
`agentguard_dlp_config`, but changing a prompt means editing Go, rebuilding, and
re-provisioning.

## Design

### Config shape

Each LLM UDF's config row gains a `prompt` key holding the **full prompt template**
with named placeholders. The template is fully operator-editable (no fixed/locked
portion); the structured-output instruction lives in the default text and is
documented as load-bearing.

| UDF | Placeholders | Default template |
|---|---|---|
| `dlp_risk` | `{entity_types}`, `{message}` | today's prompt with `', '.join(ets)` → `{entity_types}`, `text` → `{message}` |
| `dlp_understand_query` | `{question}` | today's prompt with the question spliced at `{question}` |
| `dlp_rerank` | `{count}`, `{question}`, `{items}` | today's prompt: `... ITEMS: {count}\nQUESTION: {question}\n{items}` |
| `dlp_summarize` | `{question}`, `{evidence}` | today's prompt: `... QUESTION: {question}\nEVIDENCE: {evidence}` |

`dlp_embed` and `dlp_extract_entities` have no prompt and are unchanged.

### UDF rendering

- Each UDF body keeps a `DEFAULT_PROMPT` Python constant. A missing/empty
  `prompt` in config falls back to it — fail-open and behavior-preserving for
  un-tuned deployments.
- Substitution is a **single-pass `re.sub`** over that UDF's placeholder names:
  `re.sub(r'\{(question|evidence)\}', lookup, tpl)`. Single-pass means substituted
  content is never re-scanned, so a message containing the literal text
  `{evidence}` cannot inject into a later replacement (sequential `.replace`
  would).
- Today's truncation limits are unchanged and apply to the values **before**
  substitution (`question[:2000]` understand / `[:1000]` rerank+summarize,
  `message[:4000]`, `evidence[:12000]`, snippet `[:600]`).
- A placeholder the operator omits simply means that data is not sent.

### Default-prompt sync

The default template exists in two places by necessity: the Python
`DEFAULT_PROMPT` (in the static UDF DDL constant — bodies must stay
interpolation-free per #189) and the Go seed in `DLPConfigRows`. A unit test
keeps them identical: the Go seed string, with `\n` normalized to the Python
escape `\n`, must appear verbatim in the UDF DDL constant.

### Seeding

`DLPConfigRows` adds `"prompt": <default template>` to the `dlp_risk`,
`dlp_understand_query`, `dlp_rerank`, and `dlp_summarize` rows. Seeding remains
seed-if-absent, so a re-provision never clobbers a tuned prompt.

Adopting this change on a live stack requires one re-provision (or
`POST /api/rules/sync`) because the UDF **bodies** change; after that, prompt
edits are live per event/run with no re-provision (same as every other config
key).

### Validation

`ValidateDLPConfigValue` gains a light check: for the four LLM UDF keys, a
present `prompt` must be a JSON string. No placeholder-presence blocking —
a missing placeholder degrades (data not sent; JSON-parse failures fall back to
heuristic/recall order as today).

Load-bearing parts, documented rather than enforced:
- the strict-JSON schema instruction (risk/understand/rerank parse the reply);
- `TASK: <NAME>` markers and rerank's `ITEMS: {count}` — the mock model server
  (`dlp_mock_model.py`) dispatches on the markers and regexes `ITEMS: (\d+)`,
  so the integration test requires the defaults to keep them. Real models don't
  care.

### UI

`frontend/src/components/DLPConfigEditor.jsx` (used in Settings): each of the
four LLM UDF cards gains a multiline `prompt` textarea seeded from the row,
included in the save patch, with a hint line listing that UDF's placeholders.
The existing `{ ...prev, ... }` save merge already preserves unknown keys;
the change wires `prompt` as an edited field.

## Testing

- DDL tests: each LLM UDF declares `DEFAULT_PROMPT`, renders via `re.sub`, and
  no longer contains the old inline `prompt = (` construction.
- Seed/sync test: `DLPConfigRows` prompt values match the `DEFAULT_PROMPT` in
  each DDL constant (with `\n` normalization).
- Validation test: non-string `prompt` rejected; string accepted; other keys
  unaffected.
- Frontend: ESLint.
- End-to-end: `dlp_integration.sh` against a live stack — default prompts keep
  the mock's dispatch markers, so the funnel round-trip must still pass.

## Out of scope

- The hardcoded `ENTITY_TYPES` / `RISK_TAGS` post-parse filter lists inside
  `dlp_understand_query` (pre-existing; a custom entity catalog already can't
  extend them).
- Making truncation limits configurable.
- `dlp_embed` / `dlp_extract_entities`.
