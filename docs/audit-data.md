# AgentGuard — Data-Security Audit: Test-Data Generation & the `data-audit-guide` Skill

AgentGuard implements a customer 27-point conversation / tool-call
data-security audit checklist (codes **A01–A27**) with three cooperating
pieces:

| Piece | What it is | Where it lives |
|-------|-----------|----------------|
| **`dsa-*` detection rules** | The machine-detectable subset of the checklist as installable catalog rules (materialized views over `agentguard_cim_event`), bundled as the **Data-Security Audit** rule package | `backend/internal/catalog/seed/dsa-a*.yaml`, package manifest `backend/internal/catalog/packages/data-security-audit.yaml` |
| **`data-audit-guide` skill** | A Sentry knowledge skill that turns the *full* A01–A27 checklist into interactive, adaptable investigation guidance | `backend/internal/sentry/skills/knowledge/data-audit-guide/` |
| **Test-data generator** | A labeled corpus of simulated agent sessions + a loader/scorer that measures whether the installed `dsa-*` detectors actually catch what the checklist descriptions say they should | `backend/scripts/audit_dataset/` |

The rules cover what a streaming SQL detector can decide on its own; the skill
covers everything — including the items that need human judgment, operator
context, or fields the event model doesn't carry; the generator provides the
ground truth to validate the rules (and doubles as a demo/seed dataset).

> **Implements:** rules + skill in issue #250, test-data generator in issue
> #280. Ten checklist items that are investigate-only (no runnable detector)
> were deferred to #261.

---

## 1. The `data-audit-guide` skill

`data-audit-guide` is a **knowledge skill** for the Sentry agent (see
[`docs/sentry.md`](sentry.md)): a Markdown instruction package Sentry loads
before running a data-security audit, so its exploration of
`agentguard_cim_event` follows the checklist instead of improvising.

### What it contains

- **`SKILL.md`** — the entry point. It defines the audit boundary (runtime
  data only — user input, model output, gateway traffic, tool calls;
  infrastructure/network configuration is explicitly out of scope), the full
  A01–A27 catalog (bilingual zh/en names), a shared query skeleton, and the
  investigation workflow.
- **`references/`** — one reference file per checklist theme, each entry
  stating the item's *goal* (what to detect and why), a *sample case*, and an
  *example query* to adapt:

  | A-codes | Theme | Reference |
  |---------|-------|-----------|
  | A01–A07 | Sensitive data in the conversation chain | `SENSITIVE_DATA.md` |
  | A08–A12 | Permissions & isolation | `PERMISSIONS.md` |
  | A13–A18 | External transport / exfiltration | `EXFIL.md` |
  | A19–A23 | Attack & anomaly | `ATTACK.md` |
  | A24–A27 | Audit trail | `AUDIT_TRAIL.md` |

### Design principles

- **Example queries are starting points, not scripts.** Sentry is instructed
  to read the operator's actual intent and adapt columns, patterns,
  thresholds, event types, and time windows — the A-code is a lens, not a
  rigid procedure.
- **Field reality is stated explicitly.** `agentguard_cim_event` has no
  `tenant_id`, `policy_decision`, version, or protocol columns. Items that
  truly need such fields (A15, A16, A26, A27) are marked as **proxies** over
  the closest observable signal, and Sentry must report "not captured" rather
  than claim to audit an absent field.
- **Clarify-first for deployment-specific items.** Items whose definition
  only the operator knows (A09 "unauthorized" tools, A13/A14 allowlisted
  domains, A17 bulk thresholds) trigger Sentry's `ask_user` clarification
  flow before any query runs.
- **Evidence discipline.** Every finding records the A-code, session/call
  chain, masked hit fragment, and the reason for the judgment — with absent
  fields marked "not captured", never guessed.

The skill sits behind the same opt-in switch as the rest of Sentry's
AgentGuard tool surface (the chat config row's `agentguard_tools_enabled`
flag, off by default — see `docs/sentry.md`).

### Relationship to the `dsa-*` rules

The **Data-Security Audit** rule package installs streaming detectors for the
machine-detectable codes (17 rules: `dsa-a01`, `dsa-a03`–`dsa-a12`,
`dsa-a14`, `dsa-a17`–`dsa-a20`, `dsa-a22`). The remaining codes have no
runnable detector — they are covered by the skill's guided investigation
instead. A02, A13, and A15 in particular are documented detector gaps (see
§2.5).

---

## 2. Test-data simulation & generation

`backend/scripts/audit_dataset/` holds a **labeled benchmark corpus** of
simulated multi-turn agent sessions plus `gen_audit_data.py`, which ingests
the corpus into `agentguard_cim_event` and scores the installed `dsa-*`
detectors against the corpus's ground-truth labels. Full CLI/flag detail:
[`backend/scripts/audit_dataset/README.md`](../backend/scripts/audit_dataset/README.md).

### 2.1 What the data looks like

- **15 audit codes** (`A01`–`A15`), one JSON file per code under
  `scenarios/` — 6 positive + 6 negative cases each, **180 cases total**.
- Each case is a complete multi-turn simulated agent session: session
  metadata (model, provider, agent type) plus an ordered list of CIM-shaped
  events (`session_start`, `user_input`, `llm_response`, `tool_invoke`,
  `tool_complete`, …), with a `rationale` explaining the label.
- `session_id` is never authored in the corpus; it is derived
  deterministically from `case_id` at load time, so repeated loads with
  `--repeat` get distinct synthetic sessions.

**Fake but format-valid values.** `fake_values.py` generates PII and secrets
that are clearly fake yet pass the checksum a naive detector would apply —
China resident IDs with a correct GB 11643 MOD-11 check digit, bank card
numbers that are Luhn-valid, plausibly-shaped keys and tokens. This matters
for both sides of the label: positives must *look* real enough to trigger a
correct detector, and adversarial negatives can differ from a violation only
in ways a naive pattern match can't see.

### 2.2 Label semantics: ground truth is the description, not the SQL

Every case is labeled against the checklist item's **description** — never
against the `dsa-*` SQL. Scoring a rule against its own SQL would only prove
the SQL is self-consistent; independent labels let `score` surface both:

- **False negatives** — genuine violations of the description that the
  installed rule fails to catch.
- **False positives** — benign look-alikes the rule fires on anyway.

**Negatives are intentionally adversarial.** Several are constructed
specifically to trip a keyword/regex-only detector (masked or clearly-fake
values, benign discussion of sensitive topics). A false positive on one of
these is the corpus *doing its job* — it exposes a precision gap in the
detector. Fix the detector or accept the known gap; do not soften the corpus.

### 2.3 Loading (simulation into a live stack)

```bash
make audit-data-seed        # = python3 gen_audit_data.py load (all 15 codes)

# Or directly, from backend/scripts/audit_dataset/:
python3 gen_audit_data.py load [--codes A01,A03] [--cases A01-pos-001] \
    [--count N] [--repeat K] [--deployment-id audit-demo] [--dry-run]

# Trigger one specific case (e.g. a single A01 positive) for testing:
python3 gen_audit_data.py load --cases A01-pos-001
python3 gen_audit_data.py score --cases A01-pos-001
```

`load` inserts rows straight into `agentguard_cim_event`, so the corpus also
drives every other MV reading that stream — DLP enrichment (if
`dlp.enabled`), the LLM/tool metrics MVs, and the timeline. That makes it a
reasonable **demo/seed dataset** for the `/threats`, `/agents`, `/dlp`, and
dashboard pages, independent of scoring.

Connection comes from `TIMEPLUS_HOST` / `TIMEPLUS_USER` /
`TIMEPLUS_PASSWORD` (port via `--port`, default `8123`).

### 2.4 Scoring

```bash
make audit-data-score       # = python3 gen_audit_data.py score
```

`score` never runs any detection itself — it compares what the live `dsa-*`
materialized views already wrote against the corpus labels:

1. It re-reads the corpus (same `--codes`/`--cases` filters as `load`) for
   the ground truth: each case's label and its **deterministic session ID**
   (`audit-<case_id>`). That determinism is the only load↔score link —
   nothing is stored to correlate runs.
2. It waits `--settle` seconds (default 5) so the streaming MVs have
   processed the just-ingested rows.
3. One SQL query pulls all `(rule_id, session_id)` pairs from
   `agentguard_security_events` scoped by `deployment_id` — which is why
   that tag matters: it keeps real agent traffic out of the score.
4. Per code it checks whether the **expected** rule (`A01` → `dsa-a01`)
   fired in each case's session and builds the confusion matrix:
   positive+fired = TP, positive+quiet = FN, negative+fired = FP,
   negative+quiet = TN. A different rule firing on the same session is
   ignored.

Two consequences: scoring is **cumulative** (the events stream is
append-only and unwindowed, so detections from earlier loads still count),
and `score` **cannot distinguish "rule not installed" from "rule missed"** —
both show as FN. For a meaningful score, the corresponding `dsa-*` rules
must already be **installed and enabled** (enable the *Data-Security Audit*
package on the Rules page, or
`POST /api/rules/packages/data-security-audit/enable`).

### 2.5 Known detector gaps: A02, A13, A15

Three corpus codes have **no installed `dsa-*` rule at all** — documented
gaps, not tool omissions. Their positive cases exist so the gap stays visible
and quantified; `score` reports them as `NO RULE (gap)` rather than as false
negatives. Investigating those codes is the `data-audit-guide` skill's job
(§1).

### 2.6 Cleanup caveat

`agentguard_cim_event` and `agentguard_security_events` are **append-only**
streams — there is no in-place delete. Isolation relies on the
`deployment_id` tag (default `audit-demo`): filter it out of any query or
view that shouldn't see seeded data. `gen_audit_data.py clear` prints that
filter and a reminder that truly removing the rows means dropping and
recreating the stream — destructive to *everything* in it, deliberately not
automated.

### 2.7 Growing the corpus with an LLM

`gen_audit_data.py generate` uses an LLM (OpenAI-compatible endpoint or the
Anthropic API) to author **new candidate cases** for chosen codes. The
prompt is built from the code's rule description, two few-shot format
examples, and freshly minted format-valid fake PII — never the detector
SQL, so labels stay description-derived. Candidates are schema-validated
and staged in `scenarios_generated/` for **mandatory human label review**;
`gen_audit_data.py merge` then folds reviewed cases into `scenarios/`
(all-or-nothing, duplicate/invalid-safe). Staged cases can be trial-run
against a live stack first via `load/score --scenarios scenarios_generated`.

Volume is `--codes × --label × --per-code` (default 6 per code per label);
both `generate` and `merge` are strictly **append-only** — `generate`
accumulates into staging with ids continuing the corpus numbering, `merge`
appends validated cases to `scenarios/` or refuses without writing anything
(never a silent overwrite). Full workflow and semantics: the tool README.

### 2.8 Tests

The corpus and loader are guarded by stdlib-`unittest` suites (no
dependencies), run from `backend/scripts/audit_dataset/`:

```bash
python3 -m unittest -v
```

- `test_fake_values` — checksum/generator unit tests.
- `test_corpus` — structural contract: every code has its file, 6+6 labels,
  event shapes, no authored `session_id`.
- `test_gen_audit_data` — loader mapping, payload construction, scoring.

---

## 3. Recommended test procedure

The expected end-to-end verification flow has three steps: bulk-seed a
volume of data, trigger and score one specific case, then verify the result
independently through Sentry. All commands run from
`backend/scripts/audit_dataset/` against a running stack (with docker
compose, Timeplus is on `localhost:8123`, user `proton`).

**Prerequisite:** the *Data-Security Audit* rule package is installed and
enabled (Rules page, or
`POST /api/rules/packages/data-security-audit/enable`), so the `dsa-*` MVs
are live before any data lands.

### Step 1 — Bulk-generate test data (volume is yours to pick)

```bash
# Everything: 180 cases
python3 gen_audit_data.py load

# Control the volume:
python3 gen_audit_data.py load --codes A01,A03,A06      # only these codes
python3 gen_audit_data.py load --count 3                # ≤3 cases per code
python3 gen_audit_data.py load --repeat 10              # replay ×10 (distinct sessions)
python3 gen_audit_data.py load --repeat 10 --dry-run    # preview counts, insert nothing
```

Volume = (selected cases, capped by `--count`) × `--repeat`; each repeat
gets distinct synthetic session IDs. Use `--dry-run` first to see the
case/row counts. Note `--count` picks cases in `case_id` sort order, where
`neg` sorts before `pos` — it controls volume, not which labels you get.
This seeded volume also populates `/threats`, `/agents`, the dashboard, and
(if enabled) DLP.

### Step 2 — Trigger one specific case and score it

```bash
python3 gen_audit_data.py load  --cases A01-pos-001
python3 gen_audit_data.py score --cases A01-pos-001
```

Expected: `A01  1  0  0  0  100.0%  100.0%` — one true positive. An FN here
means either `dsa-a01` missed it (a real detector gap) or the rule isn't
installed/enabled — `score` cannot tell the two apart (§2.4). Scoring is
scoped to the case's deterministic session (`audit-A01-pos-001`), so the
bulk data from Step 1 doesn't interfere. You can also score the whole seeded
corpus with a bare `python3 gen_audit_data.py score` for the full per-code
matrix.

### Step 3 — Verify through the Sentry agent

Use Sentry (`/sentry` page) as an independent check that the data and the
detection are really there — it queries the same streams the score read,
but through its own guided investigation (§1).

Prerequisites: Sentry enabled with a working `chat` endpoint, and the
**AgentGuard tools** switch on (`agentguard_tools_enabled` in Settings →
Sentry Agent) so the `data-audit-guide` skill and the read tools are
available.

Then ask, for example:

> *Audit user inputs for sensitive PII (A01) in deployment `audit-demo`
> over the last hour. Did any detection rule fire on session
> `audit-A01-pos-001`?*

Sentry loads `data-audit-guide`, adapts the A01 reference query over
`table(agentguard_cim_event)` (it should surface the seeded ID/bank-card
paste in `user_message`), and can cross-check
`agentguard_security_events` via `run_sql` to confirm `dsa-a01` fired on
that session. This closes the loop three ways: the raw event is in the CIM
stream, the detector wrote a security event, and an independent
investigation finds both. Sentry is also the *only* verification path for
the no-detector gap codes A02/A13/A15 (§2.5) — there is no rule to score,
so a guided investigation is the check.
