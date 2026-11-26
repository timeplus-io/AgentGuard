# Codex plugin — implementation TODO

Tracking issue: [timeplus-io/AgentGuard#308](https://github.com/timeplus-io/AgentGuard/issues/308)

## Direction

This implementation uses the **Python SDK + OTel traces**, not the hook-event ingest
(`agentguard_hook_events`) or npm package described in issue #308:

```
Codex hook (one process per event)
        │  JSON on stdin
        ▼
agentguard-codex  (agentguard_codex, Python)
        │  AgentGuard Python SDK
        ▼
OTLP/HTTP traces  ──►  Timeplus OTLP input :4318  ──►  otel_traces
                                                        │
                                                        ▼
                                          server-side MVs (Timeplus app)
                                                        │
                                                        ▼
                                 agentguard_cim_event / agentguard_cim_metrics
```

Consequences of this choice:

- Codex never writes `agentguard_hook_events`; `agent_type`, provider, model and
  hook identity must be encoded in the OTel resource/span attributes.
- Hook processes are short-lived and independent: there is **no cross-process
  parent/child span relationship**. Session/turn/tool correlation must come from
  attributes (`session.id`, `run.id`, `tool_use_id`) and from matching
  before/after spans server-side.
- The server converter (MVs in `timeplus-io/apps`, `apps/agentguard`) is a hard
  dependency; the plugin TODO and the app TODO must agree on one span contract.

## Current state on this branch

- `agents/codex/agentguard-plugin` Python package, `agentguard-codex` CLI.
- All 14 hooks handled (issues #317, #318), one span per event, emitted by the
  plugin through the SDK client: `session_start`, `session`,
  `user_prompt_submit`, `tool`, `pre_model_request`, `post_model_request`,
  `stop`, `interrupt`, `subagent_start`, `subagent_stop`, `permission_request`,
  `pre_compact`, `post_compact` (`PreToolUse` only records a start time). The
  span and attribute reference is in the plugin README.
- Decided in #318: Codex stays on the OTLP path and reuses the app's
  OpenInference views. `service.name` is `codex` (the views report it as
  `agent_type`); `post_model_request` is the LLM-kind span, `tool` the
  TOOL-kind span, `session` the SESSION-kind span. Prompt, reply and the live
  session start are separate non-kind spans that the app still has to map
  (timeplus-io/apps#69).
- The nine newer handlers were written from the hooks reference and the
  generated schemas in `openai/codex` (`codex-rs/hooks/schema/generated`), not
  from captured payloads, so Phase 0 item 0.1 is still open.
- Dispatch, sink and CLI unit tests, including an end-to-end run against an
  in-process OTLP receiver; CI (`.github/workflows/ci.yml`) and the root
  `Makefile` targets exist.
- Unchecked items below are still open.

---

## Phase 0 — Verify assumptions (do these first)

Each item is a question the current code/issue does not answer. Each has a check
the user can run before implementation depends on it.

| # | Question / uncertainty | How to check | Pass criteria |
|---|---|---|---|
| 0.1 | What are the real payloads for hooks that were only documented, not captured: `Stop`, `PreModelRequest`, `PostModelRequest`, `SubagentStart`, `SubagentStop`, `PermissionRequest`, `PreCompact`, `PostCompact`, `Interrupt`? | Temporarily register a dump hook: `"command": "tee -a /tmp/codex-hooks.jsonl"` (writes stdin, prints nothing, exits 0). Run one scripted `codex exec` session and inspect the JSONL. | One fixture per hook under `tests/fixtures/`, with `hook_event_name`, `session_id`, `turn_id`, and the payload fields. |
| 0.2 | Does `PostToolUse` fire for **failed** shell commands, and what does `tool_response` look like (exit code, stderr, interrupted)? | Dump hook + `codex exec "run false"` and a command that exits non-zero. | Documented `tool_success` rule (e.g. `exit_code == 0`) confirmed against the fixture. |
| 0.3 | What are the exact `tool_input` shapes for `apply_patch`, `update_plan`, `spawn_agent`, and MCP tools? | Dump hook + a session that edits a file, uses a plan, and spawns a sub-agent. | Fixture for each; `apply_patch` file path extractable from the patch header. |
| 0.4 | Does `PostModelRequest.token_usage.input_tokens` already include `cached_input_tokens`? | Compare a `PostModelRequest` dump with the matching `token_usage_record` in the transcript. | A documented rule for emitting input/cache token attributes without double counting. |
| 0.5 | Where does Codex load hooks from: `~/.codex/hooks.json`, `[hooks]` in `~/.codex/config.toml`, or both? What is the exact schema? | Install the dump hook in each location; run `codex exec`; see which fires. | Installer target path and schema confirmed. |
| 0.6 | Does Codex block / alter a turn when a hook exits non-zero? | Point a hook at `sh -c 'exit 1'` and run a turn. | Confirmed whether the plugin may exit non-zero; if it may not, keep exit 0. |
| 0.7 | Does `async = true` work for each hook, and is the `SessionEnd` budget really 1 s (3 s max)? | Time `agentguard-codex` against a slow/unreachable endpoint; register async on all but `SessionEnd`. | Exporter timeout chosen to fit the budget; measured. |
| 0.8 | Do hooks fire in the Codex desktop app / IDE extension, not just `codex exec`? | Same dump hook in the desktop/IDE session. | Supported or explicitly documented as unsupported. |
| 0.9 | Does `/hooks` trust survive plugin upgrades, and what is the managed-hooks path? | Change the hook command, rerun, observe whether trust is required; check `/etc/codex/hooks.json`. | Install/upgrade docs verified against real behavior. |
| 0.10 | What is the transcript rollout shape (`session_meta`, `turn_context`, `response_item`, `token_usage_record`), and how large can it get? | Capture `transcript_path` for a real session; inspect line types and sizes. | Defensive parser design + truncation limits confirmed. |
| 0.11 | Does the AgentGuard app's OTLP input actually route **traces** to `otel_traces`, and how are `span_attributes` values stringified (bools, arrays, nested JSON)? | Send one test span to `:4318`; query `SELECT * FROM table(otel_traces)`. | Server converter input confirmed; attribute encoding documented. |
| 0.12 | Can `agentguard_cim_event` be produced for Codex without changing canonical `event_type` values? (Compaction, permission, interrupt, subagent end are not canonical today.) | Read `docs/cim.md` §2.2 and the app's `mv_cim_*` DDL. | Either new `event_type` values are approved or these map to `other` with `raw_hook_name`. |
| 0.13 | Does the SDK's exporter flush reliably from a short-lived hook process? | Run the CLI with a span and an endpoint, check `otel_traces` without relying on `__del__`. | Explicit `flush()`/`shutdown()` in `main()` is proven necessary/sufficient. |
| 0.14 | Is `OTEL_EXPORTER_OTLP_ENDPOINT` respected, and what is the exporter timeout? | Run with/without the env var and with an unreachable host. | Configuration and timeout verified; documented in README. |

---

## Phase 1 — Agree the span contract with the server converter

Nothing else should be implemented until this contract is written down. Proposed
baseline (needs confirmation against Phase 0):

**Resource attributes** (from the SDK resource + new codex additions):

| Attribute | Value |
|---|---|
| `service.name` | `codex` (decided in #318; the app's views report it as `agent_type`) |
| `agent.id` | `AGENTGUARD_AGENT_ID` or hostname |
| `deployment.id` | `AGENTGUARD_DEPLOYMENT_ID` |
| `deployment.name` | `AGENTGUARD_DEPLOYMENT_NAME` |
| `agentguard.agent_type` | not sent; `service.name` carries the agent type |

**Common span attributes**:

| Attribute | Source |
|---|---|
| `agentguard.hook_name` | normalized Codex hook name (`session_start`, `user_prompt_submit`, …) |
| `session.id` | `session_id` |
| `run.id` | `turn_id` when present, else `session_id` |
| `llm.provider` | transcript `session_meta.model_provider` or config (TBD) |
| `llm.model` | `model` / `turn_context.model` |
| `agentguard.agent_id`, `agentguard.agent_type` | sub-agent hooks only |
| `agentguard.tool_use_id` | `tool_use_id` (Pre/PostToolUse) |
| `tool.name`, `tool.parameters`, `tool.result`, `tool.success` | tool hooks |
| `llm.input_tokens`, `llm.output_tokens`, `llm.cached_input_tokens`, `llm.cache_write_tokens`, `llm.reasoning_output_tokens`, `llm.total_tokens` | `PostModelRequest.token_usage` |
| `agentguard.messages` | transcript context events (truncated JSON) |

**Hook → span → CIM mapping**:

| Codex hook | Proposed `span_name` | CIM `event_type` | Notes |
|---|---|---|---|
| `SessionStart` | `session_start` | `session_start` | Emit immediately — do **not** wait for `SessionEnd` |
| `SessionEnd` | `session_end` | `session_end` | Duration from stored start time; must fit 1 s budget |
| `UserPromptSubmit` | `user_prompt_submit` | `user_input` | Prompt in `user_message`/attribute |
| `Stop` | `stop` | `llm_response` | `last_assistant_message` |
| `PreModelRequest` | `pre_model_request` | `llm_request` | Only real pre-send event Codex offers |
| `PostModelRequest` | `post_model_request` | metrics only (TBD) | Token usage; avoid a duplicate `llm_response` |
| `PreToolUse` | `pre_tool_use` | `tool_invoke` | Pair with Post via `tool_use_id` |
| `PostToolUse` | `post_tool_use` | `tool_complete` | `tool_success` from `tool_response` |
| `SubagentStart` | `subagent_start` | `session_start` | Sub-agent `agent_id`/`agent_type` |
| `SubagentStop` | `subagent_stop` | `other` (new type TBD) | `last_assistant_message` |
| `PermissionRequest` | `permission_request` | `other` (new type TBD) | Tool name + input |
| `PreCompact` | `pre_compact` | `other` (new type TBD) | `trigger` |
| `PostCompact` | `post_compact` | `other` (new type TBD) | `trigger` |
| `Interrupt` | `interrupt` | `other` (new type TBD) | `turn_id` |

Tasks:

- [ ] Write the contract into `docs/cim.md` (repo) and the app package README.
- [ ] Decide: reuse the SDK's `session`/`llm`/`tool` span names + `openinference.span.kind`, or use per-hook span names. Do not mix both.
- [ ] Decide how the converter derives `agent_id` and `agent_type` (resource vs span attribute).
- [ ] Decide whether context messages go in a span attribute (size limits!) or a separate span.
- [ ] Record the contract in a test fixture + snapshot test so drift is caught.

## Phase 2 — Event normalization and hook coverage

- [x] Handle all 14 hook names above (one mapping table in code, not scattered `if`s).
- [ ] `run_id` = `turn_id` when present, else `session_id`.
- [x] `provider`: transcript `session_meta.model_provider`, fallback `openai`.
- [x] `SessionEnd` has no `model`; do not fail when absent.
- [ ] `PostToolUse`: derive success from `tool_response` (exit code / error field — see 0.2).
- [x] `PostModelRequest`: carry `token_usage` and `status`/`error`.
- [x] `SubagentStart/Stop`: carry `agent_id`, `agent_type`, `agent_transcript_path`, `last_assistant_message`.
- [x] `PermissionRequest`: carry `tool_name`, `tool_input`.
- [x] `PreCompact`/`PostCompact`: carry `trigger`.
- [x] Unknown/new hooks: ignore silently (forward compatibility).
- [x] Keep every hook observe-only — no stdout, no blocking, holds stay off.

## Phase 3 — Span emission and process behavior

- [ ] Emit one span per observed hook event; set start/end timestamps to the event time where the payload allows.
- [x] `SessionStart` emits its own span (`session_start`), so a running session is visible before `SessionEnd`.
- [ ] Emit `SessionEnd` with the stored start time, but flush deterministically.
- [x] Replace `__del__` reliance with explicit `force_flush()` / `shutdown()` in a `finally` block.
- [ ] Set the OTLP exporter timeout to fit `SessionEnd` (see 0.7); consider `SimpleSpanProcessor` for that hook.
- [x] Attach `tool_use_id` to the tool span.
- [x] Stop `agentguard.span.kind=SESSION` leaking onto `llm`/`tool` spans: the plugin emits with the client's `start_span`, and only the `session` span sets it.
- [ ] Truncate `tool_input`, `tool_result` and transcript messages to a documented limit.
- [ ] Swallow all errors, print only to stderr, **exit 0** (pending 0.6); never write stdout.
- [ ] If the endpoint is unreachable, exit immediately rather than hanging the turn.
- [ ] Make the tracer/hooks injectable so tests can use an in-memory exporter.

## Phase 4 — Transcript reader

- [ ] Read `transcript_path` defensively (JSONL, unknown record types ignored).
- [ ] On `UserPromptSubmit`: emit `prompt_context` with the conversation messages.
- [ ] On `Stop`: emit `llm_context` with conversation + tool results.
- [ ] Extract `session_meta.model_provider`, `cli_version`, `originator` for resource/span attributes.
- [ ] Do not let a missing/changed transcript fail the hook (transcript is explicitly unstable).
- [ ] Fixtures: normal rollout, truncated line, unknown record type, missing file.

## Phase 5 — Installation and configuration

- [ ] Add `agentguard-codex install` (and `uninstall`) that merges hooks into the
  location verified in 0.5. Idempotent; back up the file; support `--hooks-file`.
- [ ] `async = true` for all hooks except `SessionEnd` (pending 0.7).
- [ ] Document the `/hooks` trust step and the managed-hooks path (0.9).
- [ ] Document env vars: `AGENTGUARD_AGENT_ID`, `AGENTGUARD_DEPLOYMENT_ID`,
  `AGENTGUARD_DEPLOYMENT_NAME`, `OTEL_EXPORTER_OTLP_ENDPOINT`.
- [ ] Consider shipping as a Codex plugin (`.codex-plugin/plugin.json` + bundled hooks).
- [ ] Keep `hooks.json` in sync with the installer output.

## Phase 6 — Tests

- [ ] Fixtures from Phase 0 under `tests/fixtures/` (one per hook).
- [ ] Unit tests for every hook → span attributes.
- [ ] In-memory-span-exporter tests (no network) for the emission layer.
- [ ] Subprocess tests: valid event exits 0 with empty stdout; malformed JSON with
  the agreed exit code; unreachable endpoint does not hang.
- [x] Fake OTLP/HTTP receiver test: decode the protobuf and assert the spans (`tests/test_cli.py`).
- [ ] SessionEnd timeout test (slow endpoint) within the Codex budget.
- [ ] Optional e2e: run `codex exec` with the plugin, then query `otel_traces` and
  the CIM views (requires a Timeplus instance).

## Phase 7 — Server conversion (timeplus-io/apps, `apps/agentguard`)

Blocked on the Phase 1 contract. Tracked here so the plugin is not declared done early.

- [ ] Confirm the app's OTLP input stores traces in `otel_traces` (0.11).
- [ ] `mv_cim_codex`: `otel_traces WHERE agent_type = 'codex'` → `agentguard_cim_event`,
  with all fields from the Phase 1 table.
- [ ] `mv_cim_metrics_codex_llm` from `PostModelRequest` token attributes
  (`llm.call`, input/output/total/cache tokens; avoid double counting cached input).
- [ ] `mv_cim_metrics_codex_tools` from Pre/Post spans.
- [ ] Approve or reject new `event_type` values for compaction/permission/interrupt/subagent end.
- [ ] Pricing rows for the OpenAI/Codex models in use.
- [ ] Agent Rooms: `codex: ['CX', 'Codex']`, `spawn_agent`, `update_plan`,
  `apply_patch` path label, read-only shell classification, Thinking from `pre_model_request`,
  query the new hook names.
- [ ] Update `docs/cim.md` (this repo) and the app README with the Codex rows.

## Phase 8 — Docs and release

- [ ] Plugin README: full hook table, config matrix, install/uninstall, limitations.
- [ ] `docs/plugin-install.md`: Codex section.
- [ ] `skills/agentguard/SKILL.md`: Codex install path.
- [ ] `docs/cim.md`: Codex trace → CIM mapping (Phase 1 contract).
- [x] PyPI publishing: `.github/workflows/publish-codex-plugin.yml` publishes on a
  `codex-plugin-v<version>` GitHub Release. The SDK is on PyPI as
  `timeplus-agentguard` (import name `agentguard`), so the dependency
  `timeplus-agentguard>=0.3.0` resolves without a local editable install.

## Acceptance (adapted from issue #308)

- [ ] A Codex session shows as a room with a `CX` badge and model, with prompt,
  shell call, file edit, MCP call, sub-agent, final answer and compaction each
  driven by trace data.
- [ ] The context gauge moves per `PostModelRequest` with no extra OTel setup.
- [ ] Plugin failures (Timeplus unreachable, bad config, bad transcript) never
  block or change a Codex turn.
- [ ] All Phase 0 checks are recorded with their results (fixtures or notes in this folder).