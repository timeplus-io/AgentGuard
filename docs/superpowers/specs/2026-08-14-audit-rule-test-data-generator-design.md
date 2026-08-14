# Audit-Rule Test-Data Generator — Design

- **Issue:** [#280](https://github.com/timeplus-io/AgentGuard/issues/280) — *[Customer] Need to generate test data for rule A01 ~ A15*
- **Source of truth:** `docs/research/智能体对话与工具调用数据安全审计规则清单.pdf` (customer checklist, A01–A27). This design covers **A01–A15** per the issue.
- **Status:** Design — pending review.
- **Date:** 2026-08-14.

## 1. Purpose

Produce a **labeled benchmark corpus** of simulated agent sessions that also serves as **customer demo data**. Each case is a realistic multi-turn agent conversation tied to one audit code, labeled `positive` (genuinely violates the rule) or `negative` (a benign look-alike). A Python tool ingests cases directly into `agentguard_cim_event`; a scoring mode reads back `agentguard_security_events` and reports, per code, whether the installed `dsa-*` rule caught the positives and left the negatives alone.

## 2. Guiding principle — description-driven, not regex-driven

**The ground truth is the PDF rule *description* (the semantic intent), never the `dsa-*` SQL regex.** For A01 the criterion is *"用户输入了身份证号、银行卡号、账户信息、密码、验证码、密钥或客户资料，且非业务必需 / 超出智能体用途 / 进入非授权模型"* — not the regex in `dsa-a01.yaml`.

Consequences:

- A **positive** is a conversation that a human reading the description would call a violation.
- A **negative** is a conversation that *looks* similar but the description does **not** apply: the sensitive data is business-necessary, already masked, properly authorized, or simply absent.
- Because the data is authored independently of the SQL, the benchmark can expose **false negatives** (a real violation the regex misses) and **false positives** (the regex firing on a benign case) — including the three codes with no detector at all (§4), whose positives are documented detector gaps.

To enforce this decoupling *structurally*, the corpus is authored by subagents that are shown the PDF description, the CIM schema, and real-world research — **but never the `dsa-*` SQL** (§6).

## 3. Background — CIM event schema

Cases ingest directly into `agentguard_cim_event` (per the issue). This also feeds the DLP-enrich and metrics MVs downstream, so the whole pipeline lights up. Relevant columns (full schema: `docs/cim.md` §2.1):

| Column | Use in a case |
|--------|---------------|
| `event_time` | stamped by loader (now − descending offset) |
| `event_type` | `session_start` / `user_input` / `llm_response` / `tool_invoke` / `tool_complete` / `session_end` |
| `agent_type` | `claudecode` \| `openclaw` \| `hermes` (per case) |
| `agent_id` | demo identity; **empty for A11 "missing identity" positives** |
| `deployment_id` | `audit-demo` (tag for isolation + scoring) |
| `session_id` | `audit-<case_id>` — unique per case, the scoring join key |
| `run_id` | one per user→assistant round |
| `model` / `provider` | realistic demo values |
| `tool_name` / `tool_input` | authored for `tool_invoke` |
| `tool_result` / `tool_success` | authored for `tool_complete` |
| `user_message` | authored for `user_input` |
| `assistant_message` | authored for `llm_response` |
| `raw_hook_name` | canonical hook name for the agent_type |
| `raw_event_data` | JSON blob the loader synthesizes from the row's fields |

**Ingestion precedent:** `backend/scripts/dlp_load_events.py` inserts via the ClickHouse-compatible HTTP interface (`INSERT … FORMAT JSONEachRow`) with no third-party deps. The new tool follows the same shape.

## 4. Coverage

All of **A01–A15**. Twelve have an installed detector; three do not:

| Has `dsa-*` rule | A01, A03, A04, A05, A06, A07, A08, A09, A10, A11, A12, A14 |
|------------------|-----------------------------------------------------------|
| **No rule** (deferred #261) | **A02** (输入模型前未脱敏), **A13** (未登记工具或外部地址), **A15** (外传保护等级下降) |

The three without a rule are still authored: their positives become documented detector gaps in the scoring report, which is exactly the kind of signal a benchmark should surface.

## 5. Corpus format

One JSON file per code: `scenarios/A01.json … A15.json`. Each file is a list of **cases**; each case is one complete multi-turn session.

Default **5 positive + 5 negative** per code (configurable at authoring time). ~15 × 10 = ~150 cases, plus ~10 fully-benign background sessions (no code) for realism. Each case is a realistic conversation of **multiple turns** — several `(user_input → llm_response)` rounds with interleaved tool calls, bracketed by `session_start`/`session_end` — with the rule-relevant content sitting naturally inside the longer flow.

```json
{
  "case_id": "A01-pos-001",
  "audit_code": "A01",
  "label": "positive",
  "violates": ["A01"],
  "rule_description_zh": "检测用户输入的身份证号、银行卡号、账户信息、密码、验证码、密钥和客户资料。非业务必需、超出智能体用途或进入非授权模型时告警。",
  "rationale": "In an unrelated debugging conversation the user pastes 身份证 + 银行卡 + 密码; none is needed for the coding question — 非业务必需.",
  "agent_type": "openclaw",
  "session": { "model": "claude-sonnet-4-6", "provider": "anthropic" },
  "events": [
    { "event_type": "session_start" },
    { "event_type": "user_input",   "user_message": "帮我看下这个 Python 报错 …" },
    { "event_type": "llm_response", "assistant_message": "这个 KeyError 是因为 …" },
    { "event_type": "user_input",   "user_message": "顺便记一下我的信息：身份证 11010119900307461X，卡号 6222021234567890123，登录密码 Summer2024!" },
    { "event_type": "llm_response", "assistant_message": "我不需要你的身份证或密码来解决这个报错，建议不要在对话中粘贴这些信息。" },
    { "event_type": "session_end" }
  ]
}
```

Field rules:

- **`session_id` is never authored or overridden** — the loader always sets it to `audit-<case_id>` so scoring can join fired events back to the label.
- Any other CIM field may be set explicitly on an event (`tool_name`, `agent_id`, `tool_input`, …). This is how structural cases are expressed — e.g. an A09 positive authors `tool_invoke` with `tool_name: "admin_delete_user"`; an A11 positive sets `agent_id: ""` at the session level.
- `session.*` holds per-case defaults (agent_type, model, provider, agent_id) applied to every event unless the event overrides them.
- `offset_ms` is optional; if omitted the loader spaces events by a default step so ordering is deterministic.

## 6. Authoring methodology (the LLM step, done once, committed)

The corpus is generated **once** by an LLM and committed to the repo. Authoring is done with **one subagent per A-code**, each given:

1. the code's **PDF description** (verbatim, the labeling criterion),
2. the **CIM event schema** and the corpus format above,
3. the **real-world research** (Samsung/ChatGPT source-code paste, ChatGPT Redis cross-user leak, EchoLeak, McHire IDOR, InjecAgent/AgentDojo, etc.),
4. a **fake-but-format-valid value library** (`fake_values.py`): 身份证 with MOD-11 checksum, UnionPay Luhn card, `1[3-9]` mobile, AWS `AKIA…`, GitHub `ghp_…`, JWT, PEM — all clearly fake but format-valid,

and explicitly **not** the `dsa-*` SQL. The agent returns 5 positive + 5 negative multi-turn cases with rationales. Output is human-reviewed for realism and correct labeling, then committed. Re-running authoring with a different count is supported but not required.

## 7. The tool — `backend/scripts/gen_audit_data.py`

Python, stdlib-only, ClickHouse HTTP interface (mirrors `dlp_load_events.py`). Subcommands:

### `load`
```
python gen_audit_data.py load [--codes A01,A03,…] [--count N] [--repeat K]
                               [--deployment-id audit-demo] [--host … --port 8123 --user … --password …]
```
- Reads scenario files (filtered by `--codes`; `--count` caps cases per code; `--repeat` re-loads with distinct session suffixes for volume).
- For each case: assign `session_id = audit-<case_id>[-rK]`, `deployment_id`, `run_id` per turn, `event_time = now − descending offset`; fill CIM columns from session defaults + event overrides; synthesize `raw_event_data` JSON; pick the `raw_hook_name` canonical for the agent_type.
- `INSERT INTO agentguard_cim_event FORMAT JSONEachRow`.
- Prints a summary: cases loaded, rows inserted, codes covered.

### `score`
```
python gen_audit_data.py score [--codes …] [--deployment-id audit-demo] [--settle 5]
```
- Waits `--settle` seconds for rule MVs to process, then:
  `SELECT rule_id, session_id FROM table(agentguard_security_events) WHERE deployment_id = '<dep>'`.
- Maps each fired `(rule_id, session_id)` back to its case via the `session_id` → `case_id` → `label` mapping.
- Per A-code, computes a confusion matrix against the ground-truth labels:
  - positive case, expected rule fired → **TP**; didn't fire → **FN**
  - negative case, rule fired → **FP**; didn't fire → **TN**
  - Reports precision / recall per code and a list of mislabeled cases (the interesting ones).
- Cross-fires (an A06 positive also tripping A01) count as correct when the extra code is genuinely in `violates`; scoring keys on the case's primary `audit_code`.
- Codes with no detector (A02/A13/A15) report "no rule installed — N positives undetected (known gap)".

### `clear`
Best-effort only. `agentguard_cim_event` is an **append-only** stream, so seeded rows cannot be surgically deleted. Isolation is by `deployment_id = audit-demo` and the recent `event_time` window; true teardown is drop/recreate of the stream. Documented, not pretended-away. `clear` prints the manual teardown SQL rather than silently no-op'ing.

## 8. Layout

```
backend/scripts/audit_dataset/
  scenarios/
    A01.json … A15.json      # committed labeled corpus (LLM-authored, reviewed)
  gen_audit_data.py          # load / score / clear
  fake_values.py             # format-valid fake PII/secret helpers
  README.md                  # what it is, how to run, cleanup caveat, label semantics
```
Makefile targets: `audit-data-seed` (→ `load`), `audit-data-score` (→ `score`).

## 9. Field-mapping edge cases

| Code | How the positive is expressed in CIM (from the description) |
|------|------------------------------------------------------------|
| A08 (cross-tenant) | tool_input / tool_result / assistant_message references a `tenant_id`/`org_id`/customer other than the caller |
| A09 (unauthorized tool) | `tool_invoke` with an admin/system tool name the session's role shouldn't call |
| A10 (privilege params) | `tool_input` with id-override or path-traversal parameters |
| A11 (missing identity) | high-risk tool call with `agent_id = ""` (session_id retained for scoring) |
| A12 (bypass after denial) | a `tool_invoke` blocked (`hook_decision=block`) then a follow-up turn achieving the same goal by another tool / encoded param |

These are authored as conversation + explicit CIM fields; the authoring agent decides them from the description, not the regex.

## 10. Out of scope (YAGNI)

- No live LLM generation at ingest time (corpus is committed; authoring is offline).
- No new UI, no new streams, no new rules — A02/A13/A15 stay ruleless (that's the point).
- No hook_events path — direct-to-`cim_event` only, per the issue.
- No automated deletion of seeded rows (append-only stream; documented caveat).

## 11. Risks / open questions

- **Regex-authored contamination:** the value of the benchmark depends on authors not peeking at the SQL. Mitigated structurally (subagents don't get the SQL) but relies on review discipline.
- **Negatives that legitimately trip dumb regexes** (e.g. an A01 negative with a business-necessary, unmasked ID) will show as false positives. This is *intended* signal, not a corpus bug — the README states so, so a demo viewer isn't confused.
- **Scoring settle time:** streaming MVs process asynchronously; `--settle` is a heuristic. Scoring may need a retry/poll loop if the default is too short.
```
