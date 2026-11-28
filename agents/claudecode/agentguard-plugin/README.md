# AgentGuard Plugin for Claude Code

Streams Claude Code hook events into [Timeplus](https://www.timeplus.com/), where the [AgentGuard Timeplus app](https://github.com/timeplus-io/apps/tree/main/apps/agentguard) turns them into normalized events, metrics, and security detections. Works alongside the OpenClaw plugin — both write to the same `agentguard_hook_events` stream, tagged with `agent_type = "claudecode"` or `"openclaw"` respectively.

> **Holds are legacy.** The synchronous hold-and-wait path described below called the standalone AgentGuard server's `/api/holds` endpoint. That server has been retired in favour of the [AgentGuard Timeplus app](https://github.com/timeplus-io/apps/tree/main/apps/agentguard), which has no hold capability. Leave `AGENTGUARD_HOLDS_ENABLED` at its default (`false`): the plugin then runs in observe-only mode and only needs Timeplus.

## How it works

Claude Code supports [hooks](https://code.claude.com/docs/en/hooks) — shell commands that fire at lifecycle points (tool calls, session start/end, etc.). This plugin registers `node dist/index.js` against every hook. Two paths run depending on the hook type:

### Async observation path (most hooks)

For every hook except `PreToolUse`, the plugin:
1. Reads the event JSON from stdin
2. Normalizes it to the `HookEvent` schema and POSTs to Timeplus async (fire-and-forget HTTP)
3. On `Stop` and `SubagentStop`, additionally reads the transcript and posts one `llm_call` event per model request with its token usage (see [Token usage](#token-usage-llm_call-events-and-_llm_usage))
4. On `UserPromptSubmit` and `Stop`, additionally reads `transcript_path`, parses the JSONL conversation log, and posts a synthetic `prompt_context` or `llm_context` event carrying the full conversation history (truncated to `AGENTGUARD_TRANSCRIPT_MESSAGES`/`AGENTGUARD_TRANSCRIPT_CONTENT_CHARS`)
5. Exits 0 — never blocks

`SessionEnd` additionally fires a fire-and-forget `POST /api/holds/_abandon-session/:session_id` to the AgentGuard server so any hold pending for the ending session gets cleaned up cleanly.

### Synchronous hold-and-wait path (`PreToolUse` only)

When `AGENTGUARD_HOLDS_ENABLED` is `true` AND `PreToolUse` fires, the plugin follows a different path:

1. Plugin POSTs the event to AgentGuard's `/api/holds` endpoint **synchronously** (does NOT post to Timeplus separately — `/api/holds` ingests the event itself)
2. Backend ingests, then waits up to ~500 ms for any rule's materialized view to detect a match (also checks pre-existing open threats from earlier in the session)
3. Backend resolves to one of three outcomes based on the matched rule's `block_policy`:
   - `allow` (no rule fired, or rule policy is `log_only`) → returned immediately
   - `block` (rule policy is `auto_block`) → returned immediately with reason
   - `hold` (rule policy is `hold`) → opens a hold record, long-polls for human Approve/Deny in the AgentGuard UI (up to 5 minutes), returns the human's decision
4. Plugin writes Claude Code's `permissionDecision` JSON shape to stdout: `{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "allow"|"deny", "permissionDecisionReason": "..."}}`. Claude Code reads this and either runs or rejects the tool call.

The agent's hot path is paused for the duration of the hold — no LLM tokens are burned waiting for human review.

If `AGENTGUARD_HOLDS_ENABLED` is `false`, `PreToolUse` writes a permissive `permissionDecision: "allow"` immediately, then falls through to the async observation path so the event still reaches Timeplus. Useful for toggling blocking on/off during testing without unsetting hooks in `~/.claude/settings.json`.

> **Hook timeout:** the synchronous path requires Claude Code's hook timeout to be set to `600000` ms (10 minutes). The provided `hooks.json` and the `make install` script set this automatically. The backend hold timeout is capped at 540 s to leave a safety margin.

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
| `InstructionsLoaded`  | `instructions_loaded`  | `CLAUDE.md` / rules file loaded into context (memory without a tool call) |
| `PreCompact`          | `pre_compact`          | Context compaction is starting |
| `PostCompact`         | `post_compact`         | Context compaction finished    |
| `PermissionRequest`   | `permission_request`   | A tool call is waiting for a permission decision |
| `Notification`        | `notification`         | Claude Code is waiting on the user (`permission_prompt`, `idle_prompt`, ...) |
| `StopFailure`         | `stop_failure`         | Turn ended by an API error (no `Stop` fires) |
| `TaskCreated`         | `task_created`         | Task list tracking |
| `TaskCompleted`       | `task_completed`       | Task list tracking |
| `PostModelSwitch`     | `model_switched`       | Session model changed |
| `ConfigChange`        | `config_change`        | A settings or skills file changed mid-session |

### Synthetic context events (virtual — not fired by Claude Code itself)

| Trigger event      | Stored as `hook_name` | `event_data` payload                              |
|--------------------|-----------------------|---------------------------------------------------|
| `UserPromptSubmit` | `prompt_context`      | `{ message_count, messages[] }` — conversation state before the LLM turn |
| `Stop`             | `llm_context`         | `{ message_count, messages[] }` — complete turn including all tool call/result pairs |

`messages[]` contains the parsed transcript in role/content format, truncated to the last `AGENTGUARD_TRANSCRIPT_MESSAGES` messages (default: 50). Text content is truncated to `AGENTGUARD_TRANSCRIPT_CONTENT_CHARS` characters per block (default: 10,000). If `transcript_path` is missing or unreadable the context event is skipped silently.

### Token usage (`llm_call` events and `_llm_usage`)

Claude Code has no hook per model request, so the plugin reads token usage from the transcript:

| Trigger event  | What is sent |
|----------------|--------------|
| `Stop`         | One `llm_call` row per model request made since the previous `Stop`, and `_llm_usage` in the `event_data` of the `llm_output` row: the sum of those requests |
| `SubagentStop` | One `llm_call` row per model request of the sub-agent, read from `agent_transcript_path` (a sub-agent has its own transcript; its requests are not in the main one) |
| `PostToolUse` of an `Agent` call that completed | The same, for a sub-agent that ended without a `SubagentStop` hook (see [Sub-agent start and end](#sub-agent-start-and-end)) |

`llm_call` rows carry the model in the `model` column, the time of the response as `event_time`, and this `event_data`:

```json
{
  "prompt_id": "5f422a24-…",
  "message_id": "msg_01…", "request_id": "req_01…",
  "model": "claude-sonnet-4-6", "stop_reason": "tool_use",
  "usage": { "input_tokens": 12, "output_tokens": 538, "cache_read_input_tokens": 91230,
             "cache_creation_input_tokens": 0, "total_tokens": 550 },
  "duration_ms": 6354, "started_at": "2026-01-01T00:00:00.500Z",
  "is_sidechain": false
}
```

- `usage` has the meaning of the Anthropic API: `input_tokens` does not include cached tokens, and `total_tokens` is `input_tokens + output_tokens`.
- `duration_ms` is the time from the request to the last part of the response. It needs the `api-request` entries of the transcript; when they are absent (older Claude Code versions) `duration_ms` and `started_at` are left out.
- Sub-agent rows have `is_sidechain: true`, `subagent_id` and `subagent_type`.
- A response that the transcript stores as several entries (one per content block) is counted once, by `message.id`.

`_llm_usage` is the usage of the **turn**, not of the session: `{ scope: "turn", input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens, total_tokens, duration_ms, llm_calls, model, context_tokens, models[] }`. `model` is the model of the last request, `context_tokens` the prompt size of the last main-thread request (input + cache read + cache write), and `models[]` the same counts per model. To get totals, sum the `llm_call` rows; do not add `_llm_usage` to them.

Claude Code writes the last response of a turn to the transcript shortly after `Stop` fires (about 100 ms), so on `Stop` the plugin re-reads the transcript until that response is there, for at most 1.5 s. A turn that made no model request waits the full 1.5 s.

Limits: requests that Claude Code does not write to the transcript (for example compaction and its small background requests) are not counted; a turn that ends without `Stop` is reported with the next `Stop`.

### Sub-agent start and end

Claude Code does not report every sub-agent the same way (checked on 2.1.332):

| Sub-agent | `SubagentStart` | `SubagentStop` | `PostToolUse` of its `Agent` call |
|---|---|---|---|
| Run in the background (for example `Explore`) | yes | yes | at launch: `tool_response.status = "async_launched"` |
| Run in the foreground as `web-fetch` | **no** | **no** | when it ends: `status = "completed"`, with `agentId`, `agentType`, `totalTokens` |
| Internal helper (for example the compaction summarizer) | no | yes, with an empty `agent_type` | no `Agent` call |

The plugin closes these gaps as follows:

- **End without a hook.** On the `PostToolUse` of an `Agent` call with `status = "completed"`, when no `SubagentStop` was seen for that sub-agent, the plugin sends a `subagent_ended` row built from the tool result, and the sub-agent's `llm_call` rows. The row has `agent_id`, `agent_type` and `agent_transcript_path` like a real one, plus `"synthetic": true`, `"source": "agent_tool_result"`, `tool_use_id`, `total_duration_ms`, `total_tokens` and `total_tool_use_count`.
- **Internal helpers.** Their `subagent_ended` row has `"internal_helper": true` in `event_data`, so a consumer can leave them out of the sub-agents of a session.
- **No request twice.** The plugin remembers how many requests it sent for each sub-agent (a small file per sub-agent under `AGENTGUARD_STATE_DIR`, removed on `SessionEnd`). A sub-agent that is resumed and stops again only sends its new requests, and a sub-agent reported by both hooks is sent once.

There is no synthetic start: the id of a sub-agent is not known before it runs. A consumer finds the start and end of a sub-agent like this:

| | Signal |
|---|---|
| Start | `subagent_spawning`, or else the first row of the session whose `event_data.agent_id` is that sub-agent (its tool calls carry `agent_id` and `agent_type`) |
| End | `subagent_ended` (real or synthetic). A sub-agent can be resumed, so the same `agent_id` can become active and end again |

> **`run_id` note:** `run_id` is the id of the turn. Claude Code sends it as `prompt_id` on the hooks that fire inside a turn, and it is the same from `UserPromptSubmit` to the `Stop` of that turn, so grouping a session by `run_id` gives its turns. The `prompt_context` / `llm_context` rows take the `prompt_id` of the hook that triggered them (also in `event_data.prompt_id`); `llm_call` rows take the turn the request was made in, read from the transcript. Hooks outside a turn (`SessionStart`, `PostModelSwitch`) and Claude Code versions that do not send `prompt_id` have `run_id` equal to `session_id`.

## Prerequisites

- Node.js ≥ 18
- Claude Code CLI
- Timeplus Enterprise with the [AgentGuard app](https://github.com/timeplus-io/apps/tree/main/apps/agentguard) installed (creates `ag.agentguard_hook_events`)

## Build

`dist/` is not committed; build before using the plugin from source (`npm publish` rebuilds it via `prepublishOnly`).

```bash
cd agents/claudecode/agentguard-plugin
npm install
npm run build
```

## Configuration

Set these environment variables in your shell profile (e.g. `~/.zshrc` or `~/.bashrc`) **before** launching Claude Code, or let `make install` (in `agents/claudecode/`) inject them into `~/.claude/settings.json` for you:

| Variable                              | Default                     | Description                                                                 |
|---------------------------------------|-----------------------------|-----------------------------------------------------------------------------|
| `AGENTGUARD_AGENT_ID`                 | `""` (required)             | Identifies this Claude Code instance                                        |
| `AGENTGUARD_DEPLOYMENT_ID`            | `"default"`                 | Deployment group identifier                                                 |
| `AGENTGUARD_DEPLOYMENT_NAME`          | `"Default"`                 | Human-readable deployment name                                              |
| `AGENTGUARD_TIMEPLUS_URL`             | `http://localhost:3218`     | Timeplus base URL for event ingest. timeplusd HTTP port (3218) in `timeplusd` mode; the web console / REST API base (port 8000, or its HTTPS URL behind an ingress) in `rest` mode |
| `AGENTGUARD_INGEST_API`               | `timeplusd`                 | `timeplusd` = POST straight to timeplusd (`/timeplusd/v2/ingest/…`); `rest` = the Timeplus REST API (`/{workspace}/api/v1beta2/streams/{stream}/ingest`) for deployments where port 3218 is not exposed. Same username/password (Basic auth) in both modes |
| `AGENTGUARD_WORKSPACE`                | `"default"`                 | REST API workspace id in the URL path. Rarely needed — `default` is correct for self-hosted deployments |
| `AGENTGUARD_DATABASE`                 | `"ag"`                      | Timeplus database owning the AgentGuard streams. Matches the AgentGuard Timeplus app's `db_name: ag`. |
| `AGENTGUARD_STREAM`                   | `agentguard_hook_events`    | Target stream name                                                          |
| `AGENTGUARD_USERNAME`                 | `proton`                    | Timeplus username                                                           |
| `AGENTGUARD_PASSWORD`                 | `""`                        | Timeplus password                                                           |
| `AGENTGUARD_URL`                      | `http://localhost:8080`     | Legacy: standalone AgentGuard server base URL (for `/api/holds`). Unused while holds are off |
| `AGENTGUARD_HOLDS_FAIL_POLICY`        | `deny`                      | What to return when `/api/holds` is unreachable: `deny` blocks, `allow` lets call through |
| `AGENTGUARD_HOLDS_ENABLED`            | `false`                     | When `true`, `PreToolUse` calls `/api/holds` synchronously and may block. **Opt-in only** — the AgentGuard app's data pipeline is observability-first. |
| `AGENTGUARD_TRANSCRIPT_MESSAGES`      | `50`                        | Max messages included in context events                                     |
| `AGENTGUARD_STATE_DIR`                | `<tmp>/agentguard-claudecode` | Where the plugin keeps what it already sent for each sub-agent              |
| `AGENTGUARD_TRANSCRIPT_CONTENT_CHARS` | `10000`                     | Max chars per content block in transcript                                   |

Example:

```bash
export AGENTGUARD_AGENT_ID="gangtao-macbook"
export AGENTGUARD_DEPLOYMENT_ID="local-dev"
export AGENTGUARD_DEPLOYMENT_NAME="Local Dev"
# AGENTGUARD_TIMEPLUS_URL defaults to http://localhost:3218
# AGENTGUARD_DATABASE defaults to "ag"
# AGENTGUARD_URL defaults to http://localhost:8080
# AGENTGUARD_HOLDS_ENABLED defaults to false and disables synchrounous tool blocking
```

When only the Timeplus web console is exposed (Kubernetes / on-prem behind an ingress, port 3218 not reachable), switch to the REST API — same credentials:

```bash
export AGENTGUARD_INGEST_API=rest
export AGENTGUARD_TIMEPLUS_URL=http://timeplus-host:8000   # or the console's HTTPS URL
export AGENTGUARD_USERNAME=proton
export AGENTGUARD_PASSWORD=<timeplus password>
# AGENTGUARD_WORKSPACE defaults to "default"; the payload is identical in both modes
```

## Installation

### Option A — `make install` (recommended)

From the parent directory, the Makefile builds the plugin and patches `~/.claude/settings.json` with the right hook shapes (`PreToolUse` synchronous with `timeout: 600000`, all others async) plus all the AGENTGUARD_* env vars in one shot:

```bash
cd agents/claudecode
AGENTGUARD_AGENT_ID="$(hostname)" \
AGENTGUARD_DEPLOYMENT_ID="local" \
AGENTGUARD_DEPLOYMENT_NAME="Local Dev" \
make install
```

This is the only path that is kept in sync with the latest hook contract. Override any AGENTGUARD_* variable on the command line — for example to disable blocking during testing:

```bash
AGENTGUARD_HOLDS_ENABLED=false make install
```

To remove the plugin's hooks and env vars from your settings:

```bash
make unconfigure unconfigure-otel
```

### Option B — Manual settings.json merge

The `hooks.json` file in this directory contains the canonical shape: every hook except `PreToolUse` runs `async: true`; `PreToolUse` is synchronous with `timeout: 600000` (10 min) so the hold-and-wait path can long-poll the AgentGuard server. Merge `hooks.json` into `~/.claude/settings.json` manually, then add the AGENTGUARD_* env vars to the `env` block (see Configuration above).

`$CLAUDE_PLUGIN_ROOT` is **automatically set by Claude Code** to the directory containing the loaded `hooks.json`, so no path substitution is needed when the file is loaded from the right location.

If you reference `dist/index.js` with an absolute path instead, replace `$CLAUDE_PLUGIN_ROOT`:

```json
"command": "node \"/Users/you/path/to/agentguard-plugin/dist/index.js\""
```

> **Important:** The synchronous `PreToolUse` hook entry MUST include `"timeout": 600000` and MUST NOT include `"async": true`. Without those, the hold-and-wait path can't long-poll long enough for human Approve/Deny — Claude Code will time out the hook before the backend resolves the hold. The Makefile install handles this automatically.

## Verification

After installing and starting a Claude Code session, check that events are arriving:

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -u proton:'<password>' -H 'Content-Type: application/json' \
  -d '{"query":"SELECT hook_name, agent_id, tool_name, event_time FROM table(ag.agentguard_hook_events) WHERE agent_type='"'"'claudecode'"'"' ORDER BY event_time DESC LIMIT 10"}'
```

You should see rows for `session_start`, `user_prompt_submit`, `prompt_context`, `before_tool_call`, `after_tool_call`, `llm_output`, `llm_call`, `llm_context`, etc.

To verify context events carry the full transcript:

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -u proton:'<password>' -H 'Content-Type: application/json' \
  -d '{"query":"SELECT hook_name, json_value(event_data, '"'"'$.message_count'"'"') AS msg_count, event_time FROM table(ag.agentguard_hook_events) WHERE agent_type='"'"'claudecode'"'"' AND hook_name IN ('"'"'prompt_context'"'"', '"'"'llm_context'"'"') ORDER BY event_time DESC LIMIT 5"}'
```

## Development

```bash
npm test          # Run unit tests
npm run build     # Compile TypeScript → dist/
npm run dev       # Watch mode
```

Source files:

| File                     | Responsibility                                                |
|--------------------------|---------------------------------------------------------------|
| `src/config.ts`          | Resolve `AgentGuardConfig` from env vars (incl. `agentguardUrl`, `holdFailPolicy`, `holdsEnabled`) |
| `src/timeplus-client.ts` | `HookEvent` type + `send()` HTTP POST to Timeplus (async ingest) |
| `src/normalize.ts`       | Map Claude Code hook JSON → `HookEvent` (`normalize`, `normalizeContextEvent`); `inferProvider` from model name |
| `src/transcript.ts`      | Read and parse `transcript_path` JSONL; truncate content; aggregate token usage |
| `src/holds.ts`           | `requestHold` synchronous client to AgentGuard `/api/holds` (abort-timeout + fail-policy) |
| `src/index.ts`           | Entry point: stdin → branch on hook → either sync `/api/holds` (`PreToolUse`) or async `send()` (everything else) |
| `hooks.json`             | Claude Code settings config snippet (with `timeout: 600000` on `PreToolUse` for the sync hold path) |

## Releasing

Releases are published to npm by CI:

1. Bump `version` in `package.json` (and `package-lock.json`), and merge.
2. Publish a GitHub Release tagged `claudecode-plugin-v<version>` (for example `claudecode-plugin-v0.5.1`).

The `Publish Claude Code plugin` workflow runs the tests, typecheck and build, then publishes with provenance. It fails if the tag does not match the package version or the version is already on npm. Running it manually tests, builds and lists the package contents without uploading.
