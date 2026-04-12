# Claude Code AgentShield Plugin Design

## Goal

Build a Claude Code hook plugin that streams lifecycle events to the AgentShield Timeplus backend for real-time security monitoring, using the same `agentshield_hook_events` stream and `HookEvent` schema as the existing OpenClaw plugin.

## Architecture

The plugin is a TypeScript package at `agents/claudecode/agentshield-plugin/`. It mirrors the OpenClaw plugin structure exactly — same `config.ts`, same `timeplus-client.ts` shape, same `HookEvent` type — with one key difference: Claude Code hooks are **external processes** spawned per event, not a long-lived plugin loaded into the agent. Each hook invocation runs `node dist/index.js`, reads the Claude Code event JSON from stdin, normalizes it to the `HookEvent` schema, posts one row directly to Timeplus, and exits.

Because it's per-invocation rather than long-lived, the `TimeplusClient` is simplified: no timer, no queue, no batch. It exposes a single `send(event)` function that does one HTTP POST and resolves. All hooks are configured with `"async": true` in the Claude Code settings so they run in the background without adding latency to Claude's operation.

## Tech Stack

TypeScript, Node.js (ESM), Timeplus Proton REST ingest API (same as OpenClaw plugin).

## File Structure

```
agents/claudecode/agentshield-plugin/
├── src/
│   ├── index.ts           # Entry point: read stdin → normalize → send → exit
│   ├── config.ts          # Env-var config resolution (no pluginConfig — no SDK)
│   ├── normalize.ts       # Maps Claude Code hook input JSON to HookEvent
│   └── timeplus-client.ts # Single-event HTTP POST to Proton ingest API
├── hooks.json             # Ready-to-paste hooks config for ~/.claude/settings.json
├── package.json
└── tsconfig.json
```

## Events Captured

Only events relevant to security monitoring are subscribed. All are observe-only.

| Claude Code event     | `hook_name` written       | Purpose                        |
|-----------------------|---------------------------|--------------------------------|
| `SessionStart`        | `session_start`           | Session lifecycle              |
| `SessionEnd`          | `session_end`             | Session lifecycle              |
| `UserPromptSubmit`    | `user_prompt_submit`      | DLP — inspect user input       |
| `PreToolUse`          | `before_tool_call`        | Tool call monitoring           |
| `PostToolUse`         | `after_tool_call`         | Tool result monitoring         |
| `PostToolUseFailure`  | `tool_call_failure`       | Error visibility               |
| `SubagentStart`       | `subagent_spawning`       | Subagent tracking              |
| `SubagentStop`        | `subagent_ended`          | Subagent tracking              |
| `Stop`                | `llm_output`              | End of turn                    |
| `PermissionDenied`    | `permission_denied`       | Security / blocked action      |

## Field Mapping (Claude Code → HookEvent)

| HookEvent field    | Source                                                              |
|--------------------|---------------------------------------------------------------------|
| `hook_name`        | Normalized from `hook_event_name` (see table above)                |
| `event_time`       | `new Date().toISOString()`                                          |
| `session_id`       | `input.session_id`                                                  |
| `run_id`           | `input.tool_use_id` for tool events; `""` otherwise                 |
| `agent_id`         | `AGENTSHIELD_AGENT_ID` env var                                      |
| `deployment_id`    | `AGENTSHIELD_DEPLOYMENT_ID` env var (default `"default"`)           |
| `deployment_name`  | `AGENTSHIELD_DEPLOYMENT_NAME` env var (default `"Default"`)         |
| `session_key`      | `""` (not available in Claude Code)                                 |
| `tool_name`        | `input.tool_name` for tool events; `""` otherwise                   |
| `provider`         | `""` (not available per-event)                                      |
| `model`            | `input.model` from `SessionStart`; `""` for other events            |
| `hook_decision`    | `"observe"` always                                                  |
| `block_reason`     | `""` always                                                         |
| `event_data`       | Full stdin JSON stringified                                         |
| `agent_type`       | `"claudecode"`                                                      |

## Configuration

All via environment variables (set in shell profile before running `claude`):

| Variable                     | Default                        | Notes                               |
|------------------------------|--------------------------------|-------------------------------------|
| `AGENTSHIELD_AGENT_ID`       | `""` (required)                | Identifies this Claude Code instance |
| `AGENTSHIELD_DEPLOYMENT_ID`  | `"default"`                    |                                     |
| `AGENTSHIELD_DEPLOYMENT_NAME`| `"Default"`                    |                                     |
| `AGENTSHIELD_TIMEPLUS_URL`   | `http://localhost:3218`        |                                     |
| `AGENTSHIELD_STREAM`         | `agentshield_hook_events`      |                                     |
| `AGENTSHIELD_USERNAME`       | `proton`                       |                                     |
| `AGENTSHIELD_PASSWORD`       | `""`                           |                                     |

No `flushMs` or `batchSize` — not applicable to per-invocation model.

## Installation

1. `npm run build` in the plugin directory
2. Paste the contents of `hooks.json` into `~/.claude/settings.json` (or `.claude/settings.json` for per-project scope)
3. Set env vars in shell profile

## Data Flow

```
Claude Code event fires
  → spawns `node dist/index.js` (async, non-blocking)
    → reads stdin (Claude Code hook JSON)
    → normalize.ts maps to HookEvent
    → timeplus-client.ts POSTs one row to Proton ingest API
    → process exits 0
```

Existing AgentShield security rules (MVs over `agentshield_hook_events`) pick up `claudecode` events automatically since they filter by `hook_name`, not `agent_type`.

## Out of Scope

- Blocking tool calls (PreToolUse `permissionDecision: "deny"`) — future work
- Trace/timeline visualization for Claude Code runs — future work (requires a `timeline_claudecode.go` analyzer)
- `run_id` grouping across a full turn — not feasible without IPC; `tool_use_id` is used for tool events only
