# agentguard-codex

AgentGuard monitoring plugin for [OpenAI Codex](https://github.com/openai/codex).

Codex runs a command hook for each lifecycle event and writes one JSON object
to stdin. This plugin reads that event, translates it with the
[AgentGuard Python SDK](../../../sdk/python), and exports OpenTelemetry spans to
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

Nothing changes for the five existing events: they emit the same `session`,
`llm` and `tool` spans as before.

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
[Codex hooks reference](https://developers.openai.com/codex/hooks). Every span
carries `session.id`, and `run.id` (the Codex `turn_id`) when Codex supplies one.

| Codex hook | Emitted span | What it carries |
|---|---|---|
| `SessionStart` | none (start time recorded) | |
| `SessionEnd` | `session` | Session duration |
| `UserPromptSubmit` | `llm` | The prompt (`input.value`), model |
| `PreToolUse` | none (start time recorded) | |
| `PostToolUse` | `tool` | `tool.name`, `tool.parameters`, `output.value`, tool duration |
| `PreModelRequest` | `pre_model_request` | Request id, attempt number, kind |
| `PostModelRequest` | `post_model_request` | Request id, attempt number, status, error, token usage, attempt duration |
| `Stop` | `stop` | The final assistant message (`output.value`) |
| `Interrupt` | `interrupt` | The interrupted turn |
| `SubagentStart` | `subagent_start` | Subagent id and type |
| `SubagentStop` | `subagent_stop` | Subagent id and type, its last message, subagent duration |
| `PermissionRequest` | `permission_request` | `tool.name`, `tool.parameters`, the request description |
| `PreCompact` | `pre_compact` | Trigger (`manual` or `auto`) |
| `PostCompact` | `post_compact` | Trigger, compaction duration |

The first five events go through the SDK's lifecycle hooks (`on_session_start`,
`on_session_end`, `on_llm_end`, `on_pre_tool`, `on_post_tool`). The other nine
each emit one span named after the hook, with `agentguard.hook_name` set to the
same name.

### Span attributes for the nine newer events

| Attribute | Set on | Source |
|---|---|---|
| `agentguard.hook_name` | all nine | the span name |
| `llm.model_name` | all nine | `model` |
| `codex.permission_mode` | when Codex sends it | `permission_mode` |
| `agentguard.subagent.id`, `agentguard.subagent.type` | subagent spans, and any other hook fired inside a subagent | `agent_id`, `agent_type` |
| `task.id` | model request spans: the request id; subagent spans: the subagent id | `request_id`, `agent_id` |
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

### Prompts, model attempts and final responses

These are three separate records, so counting one does not count another:

- **Prompt** — one `llm` span per `UserPromptSubmit`. It is the only span with
  `openinference.span.kind=LLM`, and it carries no token usage.
- **Model attempt** — one `pre_model_request` / `post_model_request` pair per
  attempt. A retry is a new pair with the same `codex.request.id` and the next
  `codex.request.attempt`. Token usage appears only here, so sum usage over
  `post_model_request` spans.
- **Final response** — one `stop` span per completed turn, with the assistant's
  last message and no usage. `Stop` ends a turn, not the session: the `session`
  span is still emitted only on `SessionEnd`. An interrupted turn produces an
  `interrupt` span instead.

Subagent hooks carry the parent's `session.id`; `agentguard.subagent.id`
identifies the child.

### Pairing and timing

`PreToolUse` and `PostToolUse` are paired through the SDK's on-disk store,
keyed by session, turn (`turn_id`), and tool call (`tool_use_id`), so
overlapping tool calls in one session do not overwrite each other's start
time. The plugin pairs model requests (by request id and attempt number),
subagents (by subagent id) and compactions (by turn) the same way, in
`~/.agentguard/<agent-id>/codex-starts/`, so each `post_*` and `subagent_stop`
span starts when its opening event fired. Without a matching opening event the
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
