# AgentShield Plugin for Claude Code

Streams Claude Code hook events to the AgentShield [Timeplus Proton](https://www.timeplus.com/) backend for real-time security monitoring. Works alongside the OpenClaw plugin — both write to the same `agentshield_hook_events` stream, tagged with `agent_type = "claudecode"` or `"openclaw"` respectively.

## How it works

Claude Code supports [hooks](https://code.claude.com/docs/en/hooks) — shell commands that fire at lifecycle points (tool calls, session start/end, etc.). This plugin registers as an async hook command: for each event, Claude Code spawns `node dist/index.js`, which reads the event JSON from stdin and sends up to two rows to Timeplus:

1. **The real hook event** — normalized from the Claude Code payload to the `HookEvent` schema.
2. **A synthetic context event** (only on `UserPromptSubmit` and `Stop`) — the plugin reads `transcript_path` from the hook payload, parses the conversation JSONL, and posts an additional `prompt_context` or `llm_context` event carrying the full conversation history. This gives security rules access to the complete assembled prompt, not just the user's latest message.

The process always exits 0 — it never blocks Claude Code's operation.

## Events sent

### Real hook events

| Claude Code event     | Stored as `hook_name`  | Purpose                        |
|-----------------------|------------------------|--------------------------------|
| `SessionStart`        | `session_start`        | Session lifecycle              |
| `SessionEnd`          | `session_end`          | Session lifecycle              |
| `UserPromptSubmit`    | `user_prompt_submit`   | Inspect user input             |
| `PreToolUse`          | `before_tool_call`     | Tool call monitoring           |
| `PostToolUse`         | `after_tool_call`      | Tool result monitoring         |
| `PostToolUseFailure`  | `tool_call_failure`    | Error visibility               |
| `SubagentStart`       | `subagent_spawning`    | Subagent tracking              |
| `SubagentStop`        | `subagent_ended`       | Subagent tracking              |
| `Stop`                | `llm_output`           | End of turn                    |
| `PermissionDenied`    | `permission_denied`    | Security / blocked action      |

### Synthetic context events (virtual — not fired by Claude Code itself)

| Trigger event      | Stored as `hook_name` | `event_data` payload                              |
|--------------------|-----------------------|---------------------------------------------------|
| `UserPromptSubmit` | `prompt_context`      | `{ message_count, messages[] }` — conversation state before the LLM turn |
| `Stop`             | `llm_context`         | `{ message_count, messages[] }` — complete turn including all tool call/result pairs |

`messages[]` contains the parsed transcript in role/content format, truncated to the last `AGENTSHIELD_TRANSCRIPT_MESSAGES` messages (default: 50). Text content is truncated to `AGENTSHIELD_TRANSCRIPT_CONTENT_CHARS` characters per block (default: 10,000). If `transcript_path` is missing or unreadable the context event is skipped silently.

> **`run_id` note:** Claude Code has no "run" concept. `run_id` is always equal to `session_id` for all events.

## Prerequisites

- Node.js ≥ 18
- Claude Code CLI
- AgentShield backend running with Timeplus Proton accessible

## Build

The compiled `dist/` is committed to the repository, so a build step is only needed after modifying source files.

```bash
cd agents/claudecode/agentshield-plugin
npm install
npm run build
```

## Configuration

Set these environment variables in your shell profile (e.g. `~/.zshrc` or `~/.bashrc`) **before** launching Claude Code:

| Variable                              | Default                     | Description                               |
|---------------------------------------|-----------------------------|-------------------------------------------|
| `AGENTSHIELD_AGENT_ID`                | `""` (required)             | Identifies this Claude Code instance      |
| `AGENTSHIELD_DEPLOYMENT_ID`           | `"default"`                 | Deployment group identifier               |
| `AGENTSHIELD_DEPLOYMENT_NAME`         | `"Default"`                 | Human-readable deployment name            |
| `AGENTSHIELD_TIMEPLUS_URL`            | `http://localhost:3218`     | Timeplus Proton HTTP API base URL         |
| `AGENTSHIELD_STREAM`                  | `agentshield_hook_events`   | Target stream name                        |
| `AGENTSHIELD_USERNAME`                | `proton`                    | Timeplus username                         |
| `AGENTSHIELD_PASSWORD`                | `""`                        | Timeplus password                         |
| `AGENTSHIELD_TRANSCRIPT_MESSAGES`     | `50`                        | Max messages included in context events   |
| `AGENTSHIELD_TRANSCRIPT_CONTENT_CHARS`| `10000`                     | Max chars per content block in transcript |

Example:

```bash
export AGENTSHIELD_AGENT_ID="gangtao-macbook"
export AGENTSHIELD_DEPLOYMENT_ID="local-dev"
export AGENTSHIELD_DEPLOYMENT_NAME="Local Dev"
# AGENTSHIELD_TIMEPLUS_URL defaults to http://localhost:3218
```

## Installation

Merge the contents of `hooks.json` into your Claude Code user settings at `~/.claude/settings.json`.

If you don't have a `settings.json` yet, copy it directly:

```bash
cp hooks.json ~/.claude/settings.json
```

If you already have a `settings.json` with existing hooks, merge the `hooks` key manually. The `$CLAUDE_PLUGIN_ROOT` environment variable is **automatically set by Claude Code** to the directory containing this `hooks.json`, so no manual path substitution is needed when hooks are loaded from the correct location.

If you are referencing `dist/index.js` with an absolute path instead, replace `$CLAUDE_PLUGIN_ROOT` with the absolute path to this directory:

```json
"command": "node \"/Users/you/path/to/agentshield-plugin/dist/index.js\""
```

## Verification

After installing and starting a Claude Code session, check that events are arriving:

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"SELECT hook_name, agent_id, tool_name, event_time FROM table(agentshield_hook_events) WHERE agent_type='"'"'claudecode'"'"' ORDER BY event_time DESC LIMIT 10"}'
```

You should see rows for `session_start`, `user_prompt_submit`, `prompt_context`, `before_tool_call`, `after_tool_call`, `llm_output`, `llm_context`, etc.

To verify context events carry the full transcript:

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"SELECT hook_name, json_value(event_data, '"'"'$.message_count'"'"') AS msg_count, event_time FROM table(agentshield_hook_events) WHERE agent_type='"'"'claudecode'"'"' AND hook_name IN ('"'"'prompt_context'"'"', '"'"'llm_context'"'"') ORDER BY event_time DESC LIMIT 5"}'
```

## Development

```bash
npm test          # Run unit tests (48 tests across 3 suites)
npm run build     # Compile TypeScript → dist/
npm run dev       # Watch mode
```

Source files:

| File                     | Responsibility                                                |
|--------------------------|---------------------------------------------------------------|
| `src/config.ts`          | Resolve `AgentShieldConfig` from env vars                     |
| `src/timeplus-client.ts` | `HookEvent` type + `send()` HTTP POST to Timeplus             |
| `src/normalize.ts`       | Map Claude Code hook JSON → `HookEvent` (`normalize`, `normalizeContextEvent`) |
| `src/transcript.ts`      | Read and parse `transcript_path` JSONL; truncate content      |
| `src/index.ts`           | Entry point: stdin → normalize → send hook event → read transcript → send context event → exit 0 |
| `hooks.json`             | Claude Code settings config snippet                           |
