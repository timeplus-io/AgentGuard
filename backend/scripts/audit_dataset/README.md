# Audit-rule test-data generator (#280)

A labeled benchmark + demo dataset for AgentGuard's customer-audit rules
(`dsa-a01` .. `dsa-a15`, see `project_customer_audit_rules_selfcontained` /
issue #250). `gen_audit_data.py` loads a corpus of simulated multi-turn agent
sessions straight into `agentguard_cim_event` and scores what the installed
`dsa-*` detectors actually caught against the corpus's ground-truth labels.

For the bigger picture — how this tool, the `dsa-*` rule package, and the
`data-audit-guide` Sentry skill fit together — see
[`docs/audit-data.md`](../../../docs/audit-data.md).

## Purpose

The dataset exists to answer one question: **do the `dsa-*` rules correctly
detect what the audit's PDF rule descriptions say they should?**

The ground truth for every case is the PDF rule **description**, not the
`dsa-*` SQL. That is deliberate — scoring a rule's SQL against its own SQL
would only ever tell you the SQL is internally consistent, never whether it
implements the described control correctly. Because the labels are
independent of the detector implementation, `score` can surface both:

- **False negatives** — a case that is a genuine violation of the
  description, which the installed rule fails to catch.
- **False positives** — a case that is a benign look-alike (not a
  violation), which the installed rule fires on anyway.

This also makes the corpus a decent demo/seed dataset for the `/threats`,
`/agents`, and dashboard pages, independent of its scoring use.

## Label semantics

Each case in `scenarios/A*.json` is one multi-turn simulated session, labeled:

- `positive` — a genuine violation of the audit code's description. A
  correct detector should fire on it.
- `negative` — a benign look-alike: superficially similar surface features
  (keywords, tool names, data shapes) but not actually a violation. A
  correct detector should **not** fire on it.

**Negatives are intentionally adversarial.** Several negative cases are
constructed specifically to trip a naive regex/keyword-only detector (e.g.
a masked or clearly-fake value that only differs from a genuine violation in
a way that a naive pattern match wouldn't distinguish). If `score` reports a
false positive on one of these negatives, that is the corpus **doing its
job** — it is exposing a real precision gap in the detector's SQL, not a bug
in the test data. Do not "fix" the corpus by softening these cases; fix the
detector, or accept the known gap.

## The A02 / A13 / A15 no-detector gap

Three audit codes — **A02, A13, A15** — have no installed `dsa-*` rule at
all; they are documented detector gaps, not omissions from this tool. Their
`positive` cases exist in the corpus (so the gap is visible and quantified)
but there is no `dsa-a02` / `dsa-a13` / `dsa-a15` materialized view to catch
them. `score` reports these codes as `NO RULE (gap)` rather than as
false negatives — they're already known-missing, not newly discovered.

## Corpus shape

- 15 audit codes, `A01` .. `A15`.
- One JSON file per code under `scenarios/` (`scenarios/A01.json` ..
  `scenarios/A15.json`).
- Each file holds 6 positive + 6 negative cases (12 cases per code), for
  180 cases total.
- Each case is a multi-turn simulated agent session (`session` metadata +
  an ordered list of CIM-shaped `events`: `user_input`, `llm_request`,
  `llm_response`, `tool_invoke`, `tool_complete`, `session_start`,
  `session_end`). `session_id` is never authored in the corpus — it is
  derived deterministically from `case_id` at load time
  (`case_session_id`).

## Usage

Run from `backend/scripts/audit_dataset/`:

```bash
# Ingest the corpus into agentguard_cim_event
python3 gen_audit_data.py load [--codes A01,A03] [--cases A01-pos-001] \
    [--count N] [--repeat K] [--deployment-id audit-demo] [--dry-run]

# Score installed dsa-* detectors against the corpus labels
python3 gen_audit_data.py score [--codes A01,A03] [--cases A01-pos-001] [--settle 5]

# Trigger a single specific case end-to-end
python3 gen_audit_data.py load --cases A01-pos-001
python3 gen_audit_data.py score --cases A01-pos-001

# Print teardown guidance for seeded data
python3 gen_audit_data.py clear

# LLM-generate NEW candidate cases into scenarios_generated/ (staged, not corpus)
OPENAI_API_KEY=... python3 gen_audit_data.py generate --codes A03,A10 \
    --base-url https://api.openai.com/v1 --model gpt-5.2 [--per-code 6] \
    [--label positive|negative|both] [--api-type openai|anthropic]

# After human review of the staged labels, fold them into scenarios/
python3 gen_audit_data.py merge [--codes A03]
```

Flags:

- `--codes` — comma-separated audit codes to restrict to (e.g. `A01,A03`).
  Applies to both `load` and `score`. Default: all 15.
- `--cases` — comma-separated exact case ids (e.g. `A01-pos-001`,
  case-insensitive) to trigger specific cases. Applies to both `load` and
  `score`; errors on unknown ids. Note `--count N` alone selects by
  `case_id` sort order, where `neg` sorts before `pos` — use `--cases` when
  you need a specific (e.g. positive) case.
- `--count` — cap the number of cases loaded per code (0 = all). `load`
  only.
- `--repeat K` — replay the (possibly `--count`-capped) case set `K` times,
  each repeat getting distinct synthetic `session_id`s so repeats don't
  collide. `load` only.
- `--deployment-id` — tags every inserted row (default `audit-demo`) and is
  used by `score` to scope its `agentguard_security_events` query. Also
  used by `clear` to print the exact filter to use for manual cleanup.
- `--dry-run` — `load` only; prints the case/row counts without inserting
  anything into Timeplus.
- `--settle` — `score` only; seconds to sleep before querying
  `agentguard_security_events`, to give materialized views time to run
  against the just-ingested rows. Default `5`.

### Environment variables

Connection settings read from the environment (overridable with
`--host`/`--user`/`--password`; there is no `TIMEPLUS_PORT` environment
variable, but the port can be overridden with the `--port` flag, default
`8123`):

| Variable | Default |
|---|---|
| `TIMEPLUS_HOST` | `localhost` |
| `TIMEPLUS_USER` | `proton` |
| `TIMEPLUS_PASSWORD` | `` (empty) |

## Generating more cases with an LLM

`generate` asks an LLM to author NEW candidate cases from an audit code's
**rule description** plus two few-shot format examples — never from the
`dsa-*` detector SQL, preserving the corpus's label-independence contract.
Fresh format-valid fake PII from `fake_values.py` is minted into the prompt
so positives look real.

Candidates land in **`scenarios_generated/`** (staging), never directly in
`scenarios/`. Every case is schema-validated (`validate_case` + the
multi-turn rule + id uniqueness); invalid ones are dropped and re-requested
(`--retries`, default 2). Ids continue the corpus numbering
(`A03-pos-007`, …) and each staged case carries a `generated_by: <model>`
marker.

The workflow is generate → **human-review the staged labels** → merge:

```bash
python3 gen_audit_data.py generate --codes A03 --base-url ... --model ...
# review scenarios_generated/A03.json — check each label + rationale
# optional trial run without touching the corpus:
python3 gen_audit_data.py load  --scenarios scenarios_generated --codes A03
python3 gen_audit_data.py score --scenarios scenarios_generated --codes A03
python3 gen_audit_data.py merge --codes A03
python3 -m unittest test_corpus       # re-check the corpus contract
```

`merge` is all-or-nothing: it validates every staged case against the corpus
before writing, refuses duplicates/invalid cases, and deletes the staged
file on success. Endpoint config: `--api-type openai` (default) uses
`--base-url`/`--api-key` or `OPENAI_BASE_URL`/`OPENAI_API_KEY`;
`--api-type anthropic` uses `ANTHROPIC_API_KEY` (base URL defaults to
`https://api.anthropic.com`).

### Volume control

Total generated = codes × labels × per-code:

- `--codes A03,A10` — which audit codes (required; no default).
- `--per-code N` — cases per code **per label** (default 6, matching the
  corpus's 6+6 convention).
- `--label positive|negative|both` — which label batches (default `both`).

```bash
# 2 codes × 2 labels × 6 = 24 cases
... generate --codes A03,A10 --label both --per-code 6

# exactly one positive for A12
... generate --codes A12 --label positive --per-code 1
```

You may get slightly fewer than asked: invalid LLM output is dropped and
re-requested up to `--retries` times (default 2); any shortfall is printed
as a warning, never silent. Note this is a different knob from
`load --count/--repeat`, which controls how many *existing* cases are
replayed into Timeplus — the two don't interact.

### Write semantics: append-only, never overwrite

- **`generate` appends to staging** (`scenarios_generated/AXX.json`) and
  never writes to `scenarios/` at all. Successive runs accumulate: ids are
  allocated past the max used in both the corpus and staging
  (`A03-pos-007`, then `-008`, …), so nothing collides or gets lost.
- **`merge` appends to the corpus** (`scenarios/AXX.json`), all-or-nothing:
  every staged case is validated (schema, multi-turn, duplicate ids against
  both the corpus and the staged set) *before* anything is written. On any
  error nothing changes anywhere and the staging file is kept for
  inspection; on success the staged file is deleted — its cases now live in
  the corpus.

The only failure mode for a stale staged id (e.g. someone merged other
cases with the same numbers first) is an explicit merge refusal — never a
silent overwrite. These semantics are pinned by unit tests
(`test_staged_ids_not_reused`, `test_merge_appends_and_removes_staging`,
`test_merge_rejects_duplicate_id`).

**Review is not optional.** An LLM mislabel that gets merged becomes ground
truth and silently corrupts every future `score`. Check that each positive
genuinely violates the description and each negative is genuinely benign —
the rationale field is there to make that check fast.

For agents authoring cases directly (hand-writing rather than the
`generate` pipeline), the repo ships a **self-contained** skill encoding
these rules: `.claude/skills/audit-case-authoring/SKILL.md` — schema, the
full A01–A15 description catalog, label semantics, the
never-read-the-detector-SQL rule, and inline checksum-valid fake-value
generators. It references no repo files, so it can be shared verbatim with
agents that have no access to this repository.

## Prerequisites for a meaningful `score`

`load` only needs a reachable Timeplus with `agentguard_cim_event`
provisioned. For `score` to report anything other than `NO RULE (gap)` /
all-`TN`, the corresponding `dsa-*` rules must already be **installed and
enabled** (see `POST /api/rules/:id/install` or the Rules page) so their
materialized views are live and writing to `agentguard_security_events`
before you run `score`.

Because `load` inserts directly into `agentguard_cim_event`, the same CIM
row also drives every other materialized view that reads that stream —
notably DLP enrichment (`mv_dlp_enrich` → `agentguard_dlp_enriched`, if
`dlp.enabled`) and the LLM/tool metrics MVs. Loading this corpus is a
reasonable way to seed demo data for those surfaces too, not just for
`dsa-*` scoring.

## Cleanup caveat (append-only)

`agentguard_cim_event` (and `agentguard_security_events`) are append-only
Timeplus streams — there is no `DELETE` that removes seeded rows in place.
Isolation from other data instead relies on `deployment_id` (default
`audit-demo`, override with `--deployment-id`): filter it out of any query
or view that shouldn't see the seeded corpus.

`gen_audit_data.py clear` does not delete anything; it prints the
`deployment_id` filter and a reminder that removing the seeded rows for
real requires dropping and recreating the stream (destructive to
**everything** in it, not just the seeded rows) — a decision to make
deliberately, not something this tool automates.

## Running the tests

From `backend/scripts/audit_dataset/` (stdlib `unittest`, no dependencies):

```bash
python3 -m unittest -v
```

Or target individual modules:

```bash
python3 -m unittest -v test_fake_values     # checksum/generator unit tests
python3 -m unittest -v test_corpus          # corpus shape/label invariants
python3 -m unittest -v test_gen_audit_data  # loader/mapping/payload/scoring
```
