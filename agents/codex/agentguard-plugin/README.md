# agentguard-codex

AgentGuard monitoring plugin for [OpenAI Codex](https://github.com/openai/codex).

Codex runs a command hook for each lifecycle event and writes one JSON object
to stdin. This plugin reads that event, translates it with the
[AgentGuard Python SDK](../../../sdk/python), and exports OpenTelemetry spans to
the [AgentGuard Timeplus app](https://github.com/timeplus-io/apps/tree/main/apps/agentguard)'s
OTLP/HTTP input (default `http://localhost:4318`).

> **Status: initial scaffold.** This package currently covers the hook events
> from the original SDK example. The implementation plan — full hook coverage,
> transcript context, the span contract the Timeplus app converts to CIM, and
> the open verification items — is in [`../docs/todo.md`](../docs/todo.md) and
> [issue #308](https://github.com/timeplus-io/AgentGuard/issues/308).

## Requirements

- Python 3.10+
- A Codex build with hooks enabled
- The AgentGuard Python SDK (not published on PyPI yet — install it from this repo)

## Installation

From the repository root:

```bash
pip install -e sdk/python
pip install -e agents/codex/agentguard-plugin
```

Or, from `agents/codex/agentguard-plugin`:

```bash
make install
```

Either installs the `agentguard-codex` console script.

## Configure Codex hooks

Merge [`hooks.json`](hooks.json) into your Codex hook configuration —
`~/.codex/hooks.json`, or the equivalent `[hooks]` table in
`~/.codex/config.toml`:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command", "command": "agentguard-codex" } ] }
    ]
  }
}
```

Codex skips a new or changed user-level hook until you review it. Run `/hooks`
in Codex once and approve the AgentGuard entries. Managed (system/MDM) hooks are
trusted by policy and skip this step.

If the plugin is not installed as a console script, use
`python -m agentguard_codex` as the command instead.

## Configuration

Hooks run in a fresh process, so environment variables are the usual way to
configure the plugin.

| Flag | Environment variable | Default |
|---|---|---|
| `--agent-id` | `AGENTGUARD_AGENT_ID` | hostname |
| `--deployment-id` | `AGENTGUARD_DEPLOYMENT_ID` | `local` |
| `--deployment-name` | `AGENTGUARD_DEPLOYMENT_NAME` | `Local Dev` |
| `--endpoint` | `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` |

Flags take precedence over environment variables. The plugin resolves the
agent id (flag, then `AGENTGUARD_AGENT_ID`, then hostname) before constructing
the SDK hooks, so every hook process keys its on-disk Pre/Post-tool timing
store by the same id the spans carry. For example:

```bash
export AGENTGUARD_DEPLOYMENT_ID=production
export AGENTGUARD_DEPLOYMENT_NAME="Production Codex"
export OTEL_EXPORTER_OTLP_ENDPOINT=http://timeplus.example.com:4318
```

## What gets captured

| Codex hook | SDK call | Emitted span |
|---|---|---|
| `SessionStart` | `on_session_start` | `session` (start time recorded) |
| `SessionEnd` | `on_session_end` | `session` (duration) |
| `UserPromptSubmit` | `on_llm_end` | `llm` |
| `PreToolUse` | `on_pre_tool` | start time recorded |
| `PostToolUse` | `on_post_tool` | `tool` |

`PreToolUse` and `PostToolUse` are paired through the SDK's on-disk store,
keyed by session, turn (`turn_id`), and tool call (`tool_use_id`), so
overlapping tool calls in one session do not overwrite each other's start
time. Other hook events are ignored for now, so a newer Codex can emit hooks
this plugin does not know about without breaking.

## Development

```bash
make test    # unit tests (stdlib only — no SDK or collector needed)
make build   # sdist + wheel into dist/
```

The plugin writes nothing to stdout and returns 0 even when it fails — any
error goes to stderr — so hook output cannot alter a Codex turn.

### Local end-to-end test

```bash
cd /path/to/AgentGuard

# 1. venv with the SDK + plugin (SDK is not on PyPI yet)
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

Expected with the current scaffold: an `llm` span for `UserPromptSubmit`, one
`tool` span per `PostToolUse`, and a `session` span for `SessionEnd` if Codex
fires it. `SessionStart` and `PreToolUse` only record timestamps today, so they
emit no span. See [`../docs/todo.md`](../docs/todo.md) for the remaining work.

## Publishing

```bash
pip install hatch
hatch build     # dist/agentguard_codex_plugin-*.whl
hatch publish
```
