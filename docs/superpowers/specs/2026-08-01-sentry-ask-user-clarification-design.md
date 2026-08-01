# Sentry `ask_user` Clarification Tool — Design

**Date:** 2026-08-01
**Issue:** [#264](https://github.com/timeplus-io/AgentGuard/issues/264)
**Related:** #221/#222 (approval plumbing this mirrors), #250 (data-audit guide), #219 (tool surface)

## Problem

The Sentry ReAct loop always dives straight into tool calls, even when the
user's request is under-specified. For a vague or deployment-specific request
("check for unauthorized tool calls" — unauthorized *by whose definition?*),
Sentry has to guess the scope, and a wrong guess produces a plausible-looking
but inaccurate investigation.

## Solution overview

A new `ask_user` tool the LLM calls when the answer genuinely changes the
investigation. The loop blocks mid-turn, the `/sentry` UI renders 1–4
structured questions (option chips + free-text "Other"), and the collected
answers come back as the tool result. The plumbing mirrors the proven
GatedWrite approval flow (#221): in-memory registry, register-before-emit,
NDJSON request/result events, owner-only HTTP answer endpoint, timeout.

**Decisions made during brainstorming:**

- **Timeout → proceed with defaults.** An unanswered clarification does not
  end the turn; the tool result tells the model to proceed with its
  recommended defaults and state its assumptions.
- **Explicit Skip.** The UI offers "Skip — let Sentry decide", which resolves
  the clarification immediately with the same proceed-with-defaults semantics
  as a timeout.
- **Approach: parallel path** — a small dedicated `ClarificationRegistry`
  copying the `ApprovalRegistry` shape, not a generics refactor of tested
  approval code, and not a reuse of approval events (whose approve/deny and
  timeout→deny semantics are wrong for questions).

## Backend

### Tool definition (`internal/sentry/skills/askuser.go`)

- New access class: `Interactive AccessClass = 3` (above `GatedWrite`).
- Skill `ask_user`, params modeled on Claude Code's `AskUserQuestion`:

```json
{
  "questions": [            // 1–4
    {
      "header": "Tools",    // short chip label, ≤12 chars
      "question": "Which tools do you consider unauthorized in your environment?",
      "multiSelect": true,
      "options": [          // 2–4
        {"label": "Bash (Recommended)", "description": "Shell execution — highest-risk default"},
        {"label": "Write", "description": "Filesystem writes"}
      ]
    }
  ]
}
```

- Registered **unconditionally** in `sentryRegistry` (before the
  `agentguard_tools_enabled` block): the generic SQL-analyst mode gets it too.
- The loop intercepts Interactive calls, so the skill's `Execute` is a
  defensive stub returning an error string (mirrors how GatedWrite skills are
  never executed without approval).
- The UI always appends its own "Other" free-text option; the model puts its
  recommended option first with "(Recommended)" appended.

### `ClarificationRegistry` (`internal/sentry/clarifications.go`)

Structural copy of `ApprovalRegistry` with a different payload:

```go
type ClarificationAnswer struct {
    Answers    json.RawMessage // [{selected: [labels], other: "free text"}], index-aligned with questions
    Skipped    bool
    AnsweredBy string
}
```

Same discipline: register **before** emit; single-winner (entry removed under
the lock before delivery on a buffered channel); owner-only
(`ErrClarificationForbidden`); unknown/already-resolved indistinguishable
(`ErrClarificationNotFound`); `remove` idempotent on every loop exit path.
Wired in `main.go` next to `sentryApprovals`; new `Loop.Clarifications` field.

### Loop interception (`internal/sentry/loop.go`)

Tool dispatch gains a branch: skill with `Access() == Interactive` and
non-nil `l.Clarifications` → `runClarification` (mirroring `runGated`):

1. **Validate args**: 1–4 questions, each with non-empty `question`, 2–4
   options. Invalid → error string straight back to the model; no UI event.
2. Register id (same 32-hex `newConversationID` generator), defer remove.
3. Emit `clarification_request`: new `Event` field `clarification_id`, plus
   existing `ID`/`Name`, verbatim `Args`, `TimeoutMs`.
4. Block on answer channel / timer / `ctx.Done()`. Timeout reuses
   `cc.ApprovalTimeoutMs` (no new config knob). Drain-once after timer fires,
   like `runGated`.
5. Emit `clarification_result` with outcome
   `answered|skipped|timeout|abandoned`, the answers JSON, and `answered_by`.
6. Tool result fed to the model:
   - **answered**: JSON pairing each question with its
     `{selected, other}` so the model continues with clarified scope.
   - **skipped / timeout**: fixed string — no answer arrived; proceed with
     your recommended defaults and state your assumptions in the final
     answer.
   - **abandoned**: error string (client disconnected; turn is dying).

`maxAccess` becomes `Interactive` when both `Approvals` and `Clarifications`
are non-nil (always true in interactive chat; headless stays `ReadOnly`). The
existing per-call `Approvals != nil` guard on GatedWrite dispatch stays as
defense in depth.

### Persistence & replay

`toolCallMeta` gains `Clarification *clarificationMeta`:

```go
type clarificationMeta struct {
    ClarificationID string          `json:"clarification_id"`
    Outcome         string          `json:"outcome"` // answered|skipped|timeout|abandoned
    Answers         json.RawMessage `json:"answers,omitempty"`
    AnsweredBy      string          `json:"answered_by,omitempty"`
}
```

Persisted in the normal `role="tool"` row, so `seq`-ordered replay
reconstructs the exchange. `buildHistoryUnits`/`assembleContext` need **no
changes**: the Q&A row is a regular tool unit (kept-or-dropped atomically),
and unknown JSON fields are ignored by older readers.

### Answer endpoint

`POST /api/sentry/clarifications/:id/answer` (protected group), body:

```json
{"answers": [{"selected": ["Bash", "Write"], "other": ""}], "skipped": false}
```

- `skipped: true` resolves with skip semantics (answers ignored).
- Owner-only via the registry (403); unknown/already-resolved → 404;
  mirrors `DecideSentryApproval`'s error mapping.
- Body size capped (reject oversized payloads with 400); answers passed
  through as raw JSON — the model is tolerant of shape, the server validates
  only JSON well-formedness and size.
- Documented in the CLAUDE.md endpoint table and `docs/sentry.md`
  (`handlers/docs.go` documents no Sentry endpoints today, so none is added
  there — matching the existing approvals endpoint's convention).

## Frontend (`/sentry` page)

### Stream handling (`Sentry.jsx`)

New cases beside the approval ones:

- `clarification_request` → finalize open thinking, append
  `{role: 'clarification', clarificationId, questions (parsed from args), timeoutMs}`.
- `clarification_result` → flip the item to its outcome state. UI state is
  driven by the echoed result event, not by the POST response (same
  discipline as approvals).

### `ClarificationCard`

Modeled on `ApprovalCard` (`help` icon instead of `gavel`):

- Per question: header chip, question text, options as selectable chips
  (radio for single-select, checkbox for `multiSelect`), each option's
  `description` shown; a per-question "Other" free-text input.
- Footer: **Submit answers** (primary gradient button) + **Skip — let Sentry
  decide** (secondary). Submit posts index-aligned
  `{answers: [{selected, other}]}`; Skip posts `{skipped: true}`.
- Resolved state: compact Q&A summary (selected labels / skipped / timed
  out), also used when replaying a stored conversation (questions from meta
  args, answers from `meta.clarification`).

### API client & i18n

- `api.answerSentryClarification(id, body)` in `api/client.js`.
- UI chrome strings (awaiting, submit, skip, other-placeholder, status
  lines) in `en` and `zh` sentry locale files. Question/option text is
  LLM-generated, rendered verbatim, follows the conversation language, never
  translated.

## System prompt + data-audit-guide hints

- `DefaultSentrySystemPrompt` gains one ground-rule bullet: call `ask_user`
  (1–3 questions, 2–4 options each, recommended option first) only when the
  answer genuinely changes the investigation — scope, time window, target
  agents/sessions, deployment-specific definitions. Specific requests proceed
  directly; never ask when a conventional default exists.
- `data-audit-guide/SKILL.md` gains a **"Clarify first"** section listing
  deployment-specific items where `ask_user` should precede the query, each
  with the question and a default option set:
  - **A09** unauthorized tools — defaults: `Bash`, `Write`, `WebFetch`,
    network/exec tools, or a user-provided allowlist.
  - **A13/A14** external addresses & allowlisted domains.
  - **A17** bulk-extraction thresholds & sensitive-table lists.
  - "How to investigate" step 1 points at the section.

## Testing

- `clarifications_test.go`: registry semantics (single-winner, owner-only,
  idempotent remove) mirroring `approvals_test.go`.
- Loop tests (scripted-LLM pattern from `loop_test.go`): answered / skipped /
  timeout / abandoned paths; args-validation rejection (no event emitted);
  event ordering (`clarification_request` → `clarification_result` →
  `tool_result`); persisted `clarificationMeta` round-trip through replay.
- Handler test: answer endpoint 404/403/OK/oversized-body.
- Skills tests: `ask_user` offered regardless of `agentguard_tools_enabled`;
  absent from `Defs(ReadOnly)` (headless).
- Frontend: ESLint (no JS test infra in repo).
- `CGO_ENABLED=0 go test ./...` green.

## Out of scope

- No new timeout config knob (reuses `ApprovalTimeoutMs`).
- No changes to the approval flow or a shared generic registry.
- No headless/non-interactive clarification path.
- No translation of LLM-generated question text.
