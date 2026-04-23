# AgentGuard Plugin for Claude Code

Streams Claude Code hook events to the AgentGuard [Timeplus Proton](https://www.timeplus.com/) backend for real-time security monitoring, and **synchronously blocks risky tool calls** based on rule-driven decisions from the AgentGuard server. Works alongside the OpenClaw plugin — both write to the same `agentguard_hook_events` stream, tagged with `agent_type = "claudecode"` or `"openclaw"` respectively.

## How it works

Claude Code supports [hooks](https://code.claude.com/docs/en/hooks) — shell commands that fire at lifecycle points (tool calls, session start/end, etc.). This plugin registers `node dist/index.js` against every hook. Two paths run depending on the hook type:

### Async observation path (most hooks)

For every hook except `PreToolUse`, the plugin:
1. Reads the event JSON from stdin
2. Normalizes it to the `HookEvent` schema and POSTs to Timeplus async (fire-and-forget HTTP)
3. On `UserPromptSubmit` and `Stop`, additionally reads `transcript_path`, parses the JSONL conversation log, and posts a synthetic `prompt_context` or `llm_context` event carrying the full conversation history (truncated to `AGENTGUARD_TRANSCRIPT_MESSAGES`/`AGENTGUARD_TRANSCRIPT_CONTENT_CHARS`)
4. Exits 0 — never blocks

`SessionEnd` additionally fires a fire-and-forget `POST /api/holds/_abandon-session/:session_id` to the AgentGuard server so any hold pending for the ending session gets cleaned up cleanly.

### Synchronous hold-and-wait path (`PreToolUse` only)

When `AGENTGUARD_HOLDS_ENABLED` is `true` (the default) AND `PreToolUse` fires, the plugin follows a different path:

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

### Synthetic context events (virtual — not fired by Claude Code itself)

| Trigger event      | Stored as `hook_name` | `event_data` payload                              |
|--------------------|-----------------------|---------------------------------------------------|
| `UserPromptSubmit` | `prompt_context`      | `{ message_count, messages[] }` — conversation state before the LLM turn |
| `Stop`             | `llm_context`         | `{ message_count, messages[] }` — complete turn including all tool call/result pairs |

`messages[]` contains the parsed transcript in role/content format, truncated to the last `AGENTGUARD_TRANSCRIPT_MESSAGES` messages (default: 50). Text content is truncated to `AGENTGUARD_TRANSCRIPT_CONTENT_CHARS` characters per block (default: 10,000). If `transcript_path` is missing or unreadable the context event is skipped silently.

> **`run_id` note:** Claude Code has no "run" concept. `run_id` is always equal to `session_id` for all events.

## Prerequisites

- Node.js ≥ 18
- Claude Code CLI
- AgentGuard backend running with Timeplus Proton accessible

## Build

The compiled `dist/` is committed to the repository, so a build step is only needed after modifying source files.

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
| `AGENTGUARD_TIMEPLUS_URL`             | `http://localhost:3218`     | Timeplus Proton HTTP API base URL (async event ingest)                      |
| `AGENTGUARD_STREAM`                   | `agentguard_hook_events`    | Target stream name                                                          |
| `AGENTGUARD_USERNAME`                 | `proton`                    | Timeplus username                                                           |
| `AGENTGUARD_PASSWORD`                 | `""`                        | Timeplus password                                                           |
| `AGENTGUARD_URL`                      | `http://localhost:8080`     | AgentGuard server base URL (for `/api/holds` synchronous decisions)         |
| `AGENTGUARD_HOLD_FAIL_POLICY`         | `deny`                      | What to return when `/api/holds` is unreachable: `deny` blocks, `allow` lets call through |
| `AGENTGUARD_HOLDS_ENABLED`            | `true`                      | When `false`, `PreToolUse` skips `/api/holds` and reverts to async observation-only mode |
| `AGENTGUARD_TRANSCRIPT_MESSAGES`      | `50`                        | Max messages included in context events                                     |
| `AGENTGUARD_TRANSCRIPT_CONTENT_CHARS` | `10000`                     | Max chars per content block in transcript                                   |

Example:

```bash
export AGENTGUARD_AGENT_ID="gangtao-macbook"
export AGENTGUARD_DEPLOYMENT_ID="local-dev"
export AGENTGUARD_DEPLOYMENT_NAME="Local Dev"
# AGENTGUARD_TIMEPLUS_URL defaults to http://localhost:3218
# AGENTGUARD_URL defaults to http://localhost:8080
# Set AGENTGUARD_HOLDS_ENABLED=false to disable synchronous tool blocking
```

### Server-side prerequisite — Timeplus service credentials

The plugin's synchronous `/api/holds` call hits the AgentGuard server with no user session cookie (server-to-server). The AgentGuard server in turn needs Timeplus credentials to ingest the event and query rules. **You must set `TIMEPLUS_USER` and `TIMEPLUS_PASSWORD` env vars on the AgentGuard process** — these are the canonical service credentials for plugin endpoints.

Without these, `/api/holds` returns:

```
HTTP 503
{"error":"service credentials not configured: set TIMEPLUS_USER and TIMEPLUS_PASSWORD env vars on the AgentGuard server"}
```

For the standard `docker-compose.yaml` setup these are pre-set to `proton` / `timeplus@t+`. Edit them if your wizard credentials differ, then `docker compose up -d --build agentguard`.

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
  -H 'Content-Type: application/json' \
  -d '{"query":"SELECT hook_name, agent_id, tool_name, event_time FROM table(agentguard_hook_events) WHERE agent_type='"'"'claudecode'"'"' ORDER BY event_time DESC LIMIT 10"}'
```

You should see rows for `session_start`, `user_prompt_submit`, `prompt_context`, `before_tool_call`, `after_tool_call`, `llm_output`, `llm_context`, etc.

To verify context events carry the full transcript:

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"SELECT hook_name, json_value(event_data, '"'"'$.message_count'"'"') AS msg_count, event_time FROM table(agentguard_hook_events) WHERE agent_type='"'"'claudecode'"'"' AND hook_name IN ('"'"'prompt_context'"'"', '"'"'llm_context'"'"') ORDER BY event_time DESC LIMIT 5"}'
```

### Verifying the synchronous hold flow

To exercise the blocking path end-to-end:

1. **Open the AgentGuard UI** at `http://localhost:8080`, log in, and install a rule that matches Claude Code tool calls (e.g. Privilege Guard for Bash).
2. **Set its `block_policy` to `hold`** on the rule detail page (`/rules/:id` → Block Policy panel).
3. **In a Claude Code session**, ask the agent to run a matching tool call.
4. **In the AgentGuard UI** you should see within ~1 s:
   - A toast notification bottom-right: "Hold pending: `<tool>` on `<agent>`"
   - A pending-holds badge on the Threats sidebar entry
   - The threat detail page shows an Approve / Deny banner with the rule message and a truncated `args_summary`
5. **Click Approve or Deny** → Claude Code resumes (or rejects) the tool call within ~1 s.
6. **Audit trail:** every hold lifecycle row is in the `agentguard_holds` stream:
   ```bash
   curl -s -X POST http://localhost:3218/proton/v1/query \
     -u proton:'timeplus@t+' -H 'Content-Type: application/json' \
     -d '{"query":"SELECT hold_id, tool_name, status, decided_by, decided_at FROM table(mv_holds_current) ORDER BY created_at DESC LIMIT 10"}'
   ```

If you don't see the toast, check `docker logs agentguard 2>&1 | grep holds` — every error path on the server logs with prefix `holds:`.

If the plugin reports `AgentGuard unreachable — backend returned 503`, the AgentGuard server is missing service credentials — see [Server-side prerequisite](#server-side-prerequisite--timeplus-service-credentials).

To temporarily disable hold blocking without changing rules or unsetting hooks:

```bash
export AGENTGUARD_HOLDS_ENABLED=false
# Restart Claude Code so it re-reads the env block in ~/.claude/settings.json
```

(Or set `AGENTGUARD_HOLDS_ENABLED=false` when running `make install` to bake it into `~/.claude/settings.json`.)

## Development

```bash
npm test          # Run unit tests (48 tests across 3 suites)
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
