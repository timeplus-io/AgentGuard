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

From the repository root:

```bash
pip install -e agents/codex/agentguard-plugin
```

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
