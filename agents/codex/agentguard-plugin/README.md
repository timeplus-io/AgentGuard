# agentguard-codex

AgentGuard monitoring plugin for [OpenAI Codex](https://github.com/openai/codex).

Codex runs a command hook for each lifecycle event and writes one JSON object
to stdin. This plugin reads that event, turns it into an OpenTelemetry span
with the [AgentGuard Python SDK](../../../sdk/python), and exports it to
the [AgentGuard Timeplus app](https://github.com/timeplus-io/apps/tree/main/apps/agentguard)'s
OTLP/HTTP input (default `http://localhost:4318`).

## Requirements

- Python 3.10+
- A Codex build with hooks enabled
- The AgentGuard Python SDK ([`timeplus-agentguard`](https://pypi.org/project/timeplus-agentguard/) on PyPI, installed automatically as a dependency)

## Installation

```bash
pip install timeplus-agentguard-codex-plugin
```

To work on the plugin itself, install it from a checkout instead:
`pip install -e agents/codex/agentguard-plugin`.

## Configure Codex hooks

Merge [`hooks.json`](hooks.json) into your Codex hook configuration —
`~/.codex/hooks.json`, or the equivalent `[hooks]` table in
`~/.codex/config.toml`:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "agentguard-codex", "timeout": 10 } ] }
    ],
    "Stop": [
      { "hooks": [ { "type": "command", "command": "agentguard-codex", "timeout": 10 } ] }
    ]
  }
}
```

The sample registers all 14 Codex lifecycle events with the same command.
Codex limits `SessionEnd` and `Interrupt` hooks to 3 seconds, so the sample
gives those two a `timeout` of 3 and the rest 10.

Codex skips a new or changed user-level hook until you review it. Run `/hooks`
in Codex once and approve the AgentGuard entries. Managed (system/MDM) hooks are
trusted by policy and skip this step.

If the plugin is not installed as a console script, use
`python -m agentguard_codex` as the command instead.

### Upgrading from the five-hook configuration

Versions before 0.2.0 handled five events: `SessionStart`, `SessionEnd`,
`UserPromptSubmit`, `PreToolUse` and `PostToolUse`. To capture the other nine:

1. Upgrade the package: `pip install -U timeplus-agentguard-codex-plugin`
   (or `pipx upgrade timeplus-agentguard-codex-plugin`). Check with
   `pip show timeplus-agentguard-codex-plugin` that the version is 0.2.0 or later.
   Registering the new events against an older version does nothing: it
   ignores events it does not know.
2. Add the nine new events to your hook configuration — `PreModelRequest`,
   `PostModelRequest`, `Stop`, `Interrupt`, `SubagentStart`, `SubagentStop`,
   `PermissionRequest`, `PreCompact`, `PostCompact` — copying the entries from
   [`hooks.json`](hooks.json). Use the same `command` as your existing five
   entries (including any absolute path, flags or `env` prefix you added), and
   leave the existing entries in place.
3. Set `"timeout": 3` on `Interrupt`, and lower `SessionEnd` to 3 if yours
   still says 10.
4. Start Codex, run `/hooks`, and approve the new and changed entries. Codex
   skips them until you do; the five existing hooks keep running meanwhile.

The spans of the five existing events change in 0.2.0 too (see
[What gets captured](#what-gets-captured)): the agent type is now `codex`
instead of `agentguard-sdk`, the prompt is a `user_prompt_submit` span instead
of an `llm` span, and a `session_start` span is sent when the session starts.
Queries that filter on `service_name = 'agentguard-sdk'` or `span_name = 'llm'`
need updating.

## Configuration

Hooks run in a fresh process, so environment variables are the usual way to
configure the plugin.

| Flag | Environment variable | Default |
|---|---|---|
| `--agent-id` | `AGENTGUARD_AGENT_ID` | hostname |
| `--deployment-id` | `AGENTGUARD_DEPLOYMENT_ID` | `local` |
| `--deployment-name` | `AGENTGUARD_DEPLOYMENT_NAME` | `Local Dev` |
| `--endpoint` | `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` |

Flags take precedence over environment variables. The plugin resolves every
setting (flag, then environment variable, then the default above) before
constructing the SDK hooks, so each short-lived hook process sees the same
values — including the agent id, which keys the on-disk Pre/Post-tool timing
store to match the spans. For example:

```bash
export AGENTGUARD_DEPLOYMENT_ID=production
export AGENTGUARD_DEPLOYMENT_NAME="Production Codex"
export OTEL_EXPORTER_OTLP_ENDPOINT=http://timeplus.example.com:4318
```

## What gets captured

The plugin handles all 14 events in the
[Codex hooks reference](https://developers.openai.com/codex/hooks). Each event
becomes one span, exported with the OTel service name `codex`, which the
AgentGuard app reports as the agent type.

| Codex hook | Emitted span | What it carries |
|---|---|---|
| `SessionStart` | `session_start` | Start source (`startup`, `resume`, ...); sent immediately, so a running session is visible |
| `SessionEnd` | `session` | The whole session, from `SessionStart` to `SessionEnd` |
| `UserPromptSubmit` | `user_prompt_submit` | The prompt (`input.value`) |
| `PreToolUse` | none (start time recorded) | |
| `PostToolUse` | `tool` | `tool.name`, `tool.parameters`, `output.value`, tool duration |
| `PreModelRequest` | `pre_model_request` | Request id, attempt number, kind |
| `PostModelRequest` | `post_model_request` | The LLM call: status, error, token usage, attempt duration |
| `Stop` | `stop` | The final assistant message (`output.value`) |
| `Interrupt` | `interrupt` | The interrupted turn |
| `SubagentStart` | `subagent_start` | Subagent id and type |
| `SubagentStop` | `subagent_stop` | Subagent id and type, its last message, subagent duration |
| `PermissionRequest` | `permission_request` | `tool.name`, `tool.parameters`, the request description |
| `PreCompact` | `pre_compact` | Trigger (`manual` or `auto`) |
| `PostCompact` | `post_compact` | Trigger, compaction duration |

### Span attributes

On every span, when Codex sends the field:

| Attribute | Source |
|---|---|
| `session.id` | `session_id` |
| `run.id` | `turn_id` (turn-scoped hooks) |
| `agentguard.hook_name` | the hook, in snake case (`session_start`, `post_tool_use`, ...) |
| `llm.model_name` | `model` |
| `llm.provider` | `model_provider` from the first record of the session transcript; `openai` when the transcript cannot be read |
| `cwd` | `cwd`, the session's working directory |
| `codex.permission_mode` | `permission_mode` |
| `agentguard.subagent.id`, `agentguard.subagent.type` | `agent_id`, `agent_type` (subagent spans, and any other hook fired inside a subagent) |

Three spans carry the kind attribute the app's OpenInference views key on:

| Span | Kind attribute | The app derives |
|---|---|---|
| `post_model_request` | `openinference.span.kind=LLM` | `llm_request` / `llm_response` events, and the `llm.*` metrics |
| `tool` | `openinference.span.kind=TOOL` | `tool_invoke` / `tool_complete` events |
| `session` | `agentguard.span.kind=SESSION` | `session_start` / `session_end` events |

Per span:

| Attribute | Set on | Source |
|---|---|---|
| `source` | `session_start` | `source` |
| `codex.session.end_reason` | `session` | `reason` |
| `input.value` | `user_prompt_submit` | `prompt`, as JSON: `{"messages": [{"role": "user", "content": "..."}]}` |
| `tool.name`, `tool.parameters`, `output.value`, `tool_use_id` | `tool` | `tool_name`, `tool_input`, `tool_response`, `tool_use_id` |
| `task.id` | `tool`: the tool call id; model request spans: the request id; subagent spans: the subagent id | `tool_use_id`, `request_id`, `agent_id` |
| `codex.request.id`, `codex.request.attempt`, `codex.request.kind`, `codex.thread_id` | model request spans | `request_id`, `attempt_number`, `kind`, `thread_id` |
| `codex.request.status`, `codex.request.error` | `post_model_request` | `status` (`completed`, `failed`, `cancelled`), `error` |
| `llm.token_count.prompt`, `.completion`, `.total`, `.prompt_details.cache_read`, `.prompt_details.cache_write`, `.completion_details.reasoning` | `post_model_request`, when Codex reports usage | `token_usage` |
| `output.value` | `stop`, `subagent_stop` | `last_assistant_message` |
| `codex.stop_hook_active` | `stop`, `subagent_stop` | `stop_hook_active` |
| `codex.agent_transcript_path` | `subagent_stop` | `agent_transcript_path` |
| `tool.name`, `tool.parameters`, `codex.permission.description` | `permission_request` | `tool_name`, `tool_input`, `tool_input.description` |
| `codex.compact.trigger` | `pre_compact`, `post_compact` | `trigger` |

An attribute is left off the span when Codex does not send the field. A
`post_model_request` span with status `failed` also has the OTel span status
`ERROR`. Token counts are passed through as Codex reports them; whether
`input_tokens` already includes the cached tokens is not yet confirmed (see
[`docs/todo.md`](../docs/todo.md), item 0.4).

### Prompts, model requests and final responses

These are three separate records, so counting one does not count another:

- **Prompt** — one `user_prompt_submit` span per prompt. It is not an LLM
  call: it has no duration and no token usage.
- **Model request** — one `pre_model_request` / `post_model_request` pair per
  attempt. Only `post_model_request` is an LLM-kind span, so there is one LLM
  call per attempt, with its real latency and token usage. A retry is a new
  pair with the same `codex.request.id` and the next `codex.request.attempt`.
- **Final response** — one `stop` span per completed turn, with the assistant's
  last message and no usage. `Stop` ends a turn, not the session. An
  interrupted turn produces an `interrupt` span instead.

Subagent hooks carry the parent's `session.id`; `agentguard.subagent.id`
identifies the child.

### What the AgentGuard app shows

With the app's current OpenInference views (app 0.4.0), a Codex session gives:

- `agentguard_cim_event`: `session_start` / `session_end`, `llm_request` /
  `llm_response` per model request, and `tool_invoke` / `tool_complete`, each
  with agent type `codex`, the model, the provider, and `cwd` in
  `raw_event_data`.
- `agentguard_cim_metrics`: `llm.call`, `llm.latency_ms` and the token metrics,
  one set per model request.

The prompt text, the reply text and the live `session_start` are in
`otel_traces` (the `user_prompt_submit`, `stop` and `session_start` spans) but
the app does not turn them into CIM events yet; that is
[timeplus-io/apps#69](https://github.com/timeplus-io/apps/issues/69).

### Pairing and timing

Each hook is its own process, so opening events record their start time on
disk, in `~/.agentguard/<agent-id>/codex-starts/`, and the closing event's
span starts there. Tool calls are paired by session, turn and `tool_use_id`
(so overlapping tool calls do not overwrite each other's start time), model
requests by request id and attempt number, subagents by subagent id,
compactions by turn, and the session by session id. Without a matching opening event the
span is still emitted, with no duration. Entries nobody collected are removed
after 24 hours.

### Observe only

The plugin never answers a hook. It writes nothing to stdout and exits 0, which
Codex treats as "no opinion": `PermissionRequest` is recorded and the approval
decision stays with Codex's normal flow, `Stop` does not continue the turn, and
`PreCompact` does not stop a compaction. Hook events this plugin does not know
are ignored, so a newer Codex can add hooks without breaking it.

The model request hooks run once per attempt, and each hook is a new process
(about 0.15 s on a laptop with a reachable endpoint). Remove `PreModelRequest`
and `PostModelRequest` from your configuration if you do not need per-attempt
status and token usage.

## Development

```bash
make test    # unit tests; the end-to-end telemetry tests are skipped unless the SDK is installed
make build   # sdist + wheel into dist/ (needs hatch)
```

The plugin writes nothing to stdout and returns 0 even when it fails — any
error goes to stderr — so hook output cannot alter a Codex turn.

### Local end-to-end test

```bash
cd /path/to/AgentGuard

# 1. venv with the plugin and the in-repo SDK (drop `-e sdk/python` to use the PyPI release)
uv venv ~/venvs/agentguard-codex --python 3.12
uv pip install --python ~/venvs/agentguard-codex/bin/python \
  -e sdk/python -e agents/codex/agentguard-plugin

# 2. merge this package's hooks.json into ~/.codex/hooks.json, replacing
#    "command" with the venv's absolute path plus the plugin's env vars:
#      "command": "env AGENTGUARD_AGENT_ID=codex-local \
#         OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318 \
#         /Users/you/venvs/agentguard-codex/bin/agentguard-codex"

# 3. start Codex, run /hooks, review and trust the AgentGuard entries, quit
codex

# 4. run a turn
codex exec --skip-git-repo-check -s workspace-write \
  "Run pwd, then create done.txt containing ok."

# 5. inspect the exported spans at the configured OTLP endpoint (the app's
#    otel_traces, or any local OTLP collector), then remove the AgentGuard
#    entries from ~/.codex/hooks.json
```

## Publishing

Releases are published to PyPI by CI:

1. Bump `version` in `pyproject.toml` and `__version__` in `agentguard_codex/__init__.py`, and merge.
2. Publish a GitHub Release tagged `codex-plugin-v<version>` (for example `codex-plugin-v0.1.1`).

The `Publish Codex plugin` workflow tests, builds and uploads `timeplus-agentguard-codex-plugin`. It fails if the tag does not match the package version. Running it manually builds and validates without uploading; `make check` does the same locally (needs `hatch` and `twine`).
