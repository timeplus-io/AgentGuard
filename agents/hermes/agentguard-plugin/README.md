# agentguard-hermes

AgentGuard security monitoring plugin for [Hermes Agent](https://github.com/nousresearch/hermes-agent).

Forwards Hermes session, LLM, and tool-call events to an [AgentGuard](https://github.com/timeplus/agentguard) Timeplus stream for real-time observability, and **synchronously blocks risky tool calls** based on rule decisions from the AgentGuard server.

## How it works

Two paths run depending on the hook type:

### Async observation path (most hooks)

For every hook except `pre_tool_call`, the plugin POSTs the normalized event to Timeplus via `urllib` and the callback returns immediately — Hermes never waits.

### Synchronous hold-and-wait path (`pre_tool_call` only)

When `AGENTGUARD_HOLDS_ENABLED` is `true` (the default), `pre_tool_call`:

1. POSTs the event to AgentGuard's `/api/holds` endpoint **synchronously** via `urllib.request.urlopen(timeout=600)`. The endpoint ingests the event itself, so the plugin does NOT also call its async ingest path for this hook.
2. AgentGuard's backend ingests the event, then waits up to ~500 ms for any rule's materialized view to detect a match (also checks for any pre-existing open threats from earlier in the session).
3. Backend resolves to one of three outcomes based on the matched rule's `block_policy`:
   - `allow` (no rule fired, or rule policy is `log_only`) → returned immediately
   - `block` (rule policy is `auto_block`) → returned immediately with reason
   - `hold` (rule policy is `hold`) → opens a hold record, long-polls for human Approve/Deny in the AgentGuard UI (up to 5 minutes), returns the human's decision
4. Plugin returns `{"action": "block", "message": "..."}` from the callback on `block` (Hermes short-circuits the tool call), or `None` on `allow` (Hermes runs the tool normally).

The agent's hot path is paused for the duration of the hold — no LLM tokens are burned waiting for human review.

If `AGENTGUARD_HOLDS_ENABLED` is `false`, `pre_tool_call` falls back to the async observation path and never blocks. Useful for toggling blocking on/off during testing without uninstalling the plugin.

> **Hermes timeout note:** unlike Claude Code (600s hook cap) and OpenClaw (configurable approval timeout), Hermes provides **no hook timeout and no abort signal**. The plugin self-imposes `urllib.request.urlopen(timeout=600)` so a stuck `/api/holds` request can't hang Hermes forever. The backend's hold-timeout ceiling is 540 s, leaving 60 s of margin under the plugin timeout.

## Installation

```bash
pip install agentguard-hermes-plugin
agentguard-hermes install
```

This copies the plugin into `~/.hermes/plugins/agentguard/` where Hermes auto-loads it on startup.

## Configuration

Set environment variables before starting Hermes:

### Async event ingest (Hermes → Timeplus)

| Variable | Default | Description |
|---|---|---|
| `AGENTGUARD_TIMEPLUS_URL` | `http://localhost:3218` | Timeplus Enterprise HTTP endpoint |
| `AGENTGUARD_USERNAME` | `proton` | Timeplus username |
| `AGENTGUARD_PASSWORD` | _(empty)_ | Timeplus password |
| `AGENTGUARD_STREAM` | `agentguard_hook_events` | Target Timeplus stream |

### Identity

| Variable | Default | Description |
|---|---|---|
| `AGENTGUARD_AGENT_ID` | hostname | Identifier for this agent instance |
| `AGENTGUARD_DEPLOYMENT_ID` | `local` | Deployment environment tag |
| `AGENTGUARD_DEPLOYMENT_NAME` | `Local Dev` | Human-readable deployment name |

### Synchronous tool blocking (`pre_tool_call` → AgentGuard `/api/holds`)

| Variable | Default | Description |
|---|---|---|
| `AGENTGUARD_URL` | `http://localhost:8080` | AgentGuard server base URL — where `pre_tool_call` POSTs its hold request |
| `AGENTGUARD_HOLD_FAIL_POLICY` | `deny` | What to return when `/api/holds` is unreachable: `deny` blocks the tool call, `allow` lets it proceed |
| `AGENTGUARD_HOLDS_ENABLED` | `true` | When `false`, `pre_tool_call` reverts to async observation-only mode (no blocking, event still ingested) |

### Server-side prerequisite — Timeplus service credentials

The plugin's synchronous `/api/holds` call hits the AgentGuard server with no user session cookie (server-to-server). The AgentGuard server in turn needs Timeplus credentials to ingest the event and query rules. **You must set `TIMEPLUS_USER` and `TIMEPLUS_PASSWORD` env vars on the AgentGuard process** — these are the canonical service credentials for plugin endpoints.

Without these, `/api/holds` returns:

```
HTTP 503
{"error":"service credentials not configured: set TIMEPLUS_USER and TIMEPLUS_PASSWORD env vars on the AgentGuard server"}
```

For the standard `docker-compose.yaml` setup these are pre-set to `proton` / `timeplus@t+`. Edit them if your wizard credentials differ, then `docker compose up -d --build agentguard`.

### Example

```bash
# Async ingest
export AGENTGUARD_TIMEPLUS_URL=http://timeplus.example.com:3218
export AGENTGUARD_USERNAME=proton
export AGENTGUARD_PASSWORD=secret

# Identity
export AGENTGUARD_DEPLOYMENT_ID=production
export AGENTGUARD_DEPLOYMENT_NAME="Production Hermes"

# Sync tool blocking
export AGENTGUARD_URL=http://agentguard.example.com:8080
export AGENTGUARD_HOLD_FAIL_POLICY=deny
# AGENTGUARD_HOLDS_ENABLED defaults to true

hermes
```

## What gets captured

Every Hermes hook event is forwarded to the `agentguard_hook_events` stream:

| Hermes hook | Sent as | Description |
|---|---|---|
| `on_session_start` | `on_session_start` | New session begins |
| `on_session_end` | `conversation_end` | Single chat turn completed |
| `on_session_finalize` | `on_session_end` | Session fully torn down (CLI exit, `/reset`) — also fires `POST /api/holds/_abandon-session` to clean up any pending hold |
| `on_session_reset` | `on_session_reset` | Session rotated via `/new` |
| `pre_llm_call` | `pre_llm_call` | Before each LLM turn |
| `post_llm_call` | `post_llm_call` | After each LLM turn |
| `pre_api_request` | `pre_api_request` | Before each raw API call (token metrics) |
| `post_api_request` | `post_api_request` | After each raw API call |
| `pre_tool_call` | `pre_tool_call` | Before each tool execution — **synchronous when `AGENTGUARD_HOLDS_ENABLED=true`**: ingest happens via `/api/holds` (not the async path), and the callback returns `{"action": "block", "message": ...}` on deny |
| `post_tool_call` | `post_tool_call` | After each tool execution |

`conversation_history` is stripped from all events before ingestion — it is unbounded and contains no information not already available from the individual turn events.

## Verifying the synchronous hold flow

To exercise blocking end-to-end:

1. **Open the AgentGuard UI** at `http://localhost:8080`, log in, and install a rule that matches Hermes tool calls (e.g. Privilege Guard for shell-like tools).
2. **Set its `block_policy` to `hold`** on the rule detail page (`/rules/:id` → Block Policy panel).
3. **In Hermes**, ask the agent to run a matching tool call.
4. **In the AgentGuard UI** you should see within ~1 s:
   - A toast notification bottom-right: "Hold pending: `<tool>` on `<agent>`"
   - A pending-holds badge on the Threats sidebar entry
   - The threat detail page shows an Approve / Deny banner with the rule message and a truncated `args_summary`
5. **Click Approve or Deny** → Hermes resumes (or rejects) the tool call within ~1 s. On deny, Hermes feeds the deny message back to the LLM as a tool error.
6. **Audit trail:** every hold lifecycle row is in the `agentguard_holds` stream:
   ```bash
   curl -s -X POST http://localhost:3218/proton/v1/query \
     -u proton:'timeplus@t+' -H 'Content-Type: application/json' \
     -d '{"query":"SELECT hold_id, tool_name, status, decided_by, decided_at FROM table(mv_holds_current) ORDER BY created_at DESC LIMIT 10"}'
   ```

If the plugin reports `AgentGuard unreachable — backend returned 503`, the AgentGuard server is missing service credentials — see [Server-side prerequisite](#server-side-prerequisite--timeplus-service-credentials).

To temporarily disable hold blocking without changing rules or uninstalling:

```bash
export AGENTGUARD_HOLDS_ENABLED=false
hermes  # restart so the env var takes effect
```

## Custom install path

```bash
agentguard-hermes install --hermes-dir /path/to/hermes/data
```

## Manual installation (Makefile / Docker)

If you run Hermes via the provided Docker Compose setup in `agents/hermes/`:

```bash
make configure   # copies plugin into .hermes/plugins/agentguard/
make cli         # start Hermes CLI with AgentGuard env vars pre-set
make start       # start Hermes gateway + web dashboard
```

## Publishing to PyPI

```bash
pip install hatch
cd agents/hermes/agentguard-plugin
hatch build     # produces dist/agentguard_hermes_plugin-*.whl
hatch publish
```
