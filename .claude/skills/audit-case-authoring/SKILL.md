---
name: audit-case-authoring
description: Use when authoring, generating, or reviewing test cases for the audit test-data corpus (backend/scripts/audit_dataset/scenarios/, audit codes A01-A15) — adding positive/negative cases, growing coverage for a dsa-* rule, staging LLM-generated cases, or merging candidates into the corpus.
---

# Authoring Audit Corpus Test Cases

Cases are labeled ground truth for scoring `dsa-*` detectors. The label
derives from the audit code's **rule description** (`rule_description_zh`,
present in every existing case) — never from what a detector would match.

## Iron rules

1. **NEVER read the detector SQL.** Do not open
   `backend/internal/catalog/seed/dsa-*.yaml` or anything under
   `backend/internal/rules/` while authoring. A case written to match the
   SQL can only confirm the SQL agrees with itself; it can never expose a
   detector bug. If you have already read a rule's SQL this session, do not
   author cases for that code.
2. **Mint sensitive values with `fake_values.py` — never invent digits.**
   Hand-typed IDs/cards usually fail checksum validation, silently making a
   positive untriggerable for checksum-aware detectors:
   ```bash
   cd backend/scripts/audit_dataset
   python3 -c "import fake_values as f; print(f.china_id(), f.unionpay_card(), f.china_mobile(), f.aws_access_key())"
   ```
3. **Stage, don't append.** New cases go to `scenarios_generated/AXX.json`
   for human label review, then `python3 gen_audit_data.py merge` folds them
   into `scenarios/`. Never write directly into `scenarios/AXX.json`.

## Case schema

One JSON object per case, appended to a per-code array:

```json
{
  "case_id": "A05-pos-007",
  "audit_code": "A05",
  "label": "positive",
  "violates": ["A05"],
  "rule_description_zh": "<copy VERBATIM from any existing case of this code>",
  "rationale": "<one Chinese sentence: why this label is correct per the description>",
  "agent_type": "claudecode",
  "session": {"model": "claude-sonnet-4-6", "provider": "anthropic"},
  "events": [ {"event_type": "session_start"}, "...", {"event_type": "session_end"} ]
}
```

- `case_id`: `AXX-(pos|neg)-NNN`, continuing past the max number used in
  `scenarios/` **and** `scenarios_generated/` for that code+label.
- `violates`: `["AXX"]` for positives, `[]` for negatives.
- `agent_type`: `claudecode`, `openclaw`, or `hermes`.
- `events`: ordered; types are `session_start`, `session_end`, `user_input`
  (+`user_message`), `llm_request`, `llm_response` (+`assistant_message`),
  `tool_invoke` / `tool_complete` (+`tool_name`/`tool_input`/`tool_result`).
- **Never** author a `session_id` anywhere — it is derived at load time.
- **Multi-turn is mandatory**: ≥2 `user_input` turns OR at least one tool
  round. Single Q&A cases are rejected.

## Label semantics

- **positive** — a genuine violation of the rule description. Make it
  realistic; embed minted fake values so it looks real.
- **negative** — a benign look-alike that is **deliberately adversarial**:
  reuse the surface features a naive keyword/regex detector keys on
  (same keywords, tool names, data shapes — but masked values, clearly-fake
  values, authorized context, or in-scope business purpose). The rationale
  must say why it is benign. A soft, obviously-unrelated negative is a
  wasted case.

## Verify before handing off

```bash
cd backend/scripts/audit_dataset
python3 gen_audit_data.py load --dry-run --scenarios scenarios_generated  # schema pass
python3 -m unittest test_corpus                                          # after merge
```

For LLM batch generation use the built-in pipeline instead of hand-calling
a model: `python3 gen_audit_data.py generate --codes AXX --model ...`
(see the README in `backend/scripts/audit_dataset/`).

## Red flags — stop and fix

- You are looking at a `dsa-*.yaml` or wondering "will the rule catch this?"
- A sensitive value was typed by hand instead of minted by `fake_values.py`.
- A negative case has no surface feature in common with the positives.
- You are about to edit `scenarios/AXX.json` directly.
