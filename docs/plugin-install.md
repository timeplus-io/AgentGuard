# Plugin Installation

AgentGuard plugins stream agent lifecycle events into Timeplus. They need nothing but a reachable Timeplus Enterprise with the [AgentGuard app](https://github.com/timeplus-io/apps/tree/main/apps/agentguard) installed — the app creates database `ag`, the `agentguard_hook_events` stream the plugins write to, and an OTLP/HTTP input (default port `4318`).

There is no installer script and no AgentGuard server to point at: each plugin is installed with its package manager and configured with environment variables (Claude Code, Hermes, Codex) or plugin config (OpenClaw).

> **Agent-guided install:** the [`skills/agentguard`](../skills/agentguard/SKILL.md) skill walks an agent through everything below — it collects the values, installs the package, and patches the config files.

## Prerequisites

| Agent | Required tools |
|-------|---------------|
| Claude Code | `node` >= 18, `npm` |
| OpenClaw | `node` >= 18, OpenClaw >= 2026.4.0 with an initialised `openclaw.json` (`openclaw onboard`) |
| Hermes | Python >= 3.10, `pip` |
| Codex | Python >= 3.10, `pip` |

## Choosing the ingest API

| Situation | `AGENTGUARD_INGEST_API` | `AGENTGUARD_TIMEPLUS_URL` |
|---|---|---|
| timeplusd's HTTP port is reachable from the agent host (single host / Docker) | `timeplusd` (default) | `http://<host>:3218` |
| Only the Timeplus web console / REST API is exposed (Kubernetes, behind an ingress) | `rest` | `http://<host>:8000` or the console's HTTPS URL |

Both modes use the same Timeplus username/password (Basic auth) and send the same payload; only the URL path differs.

## Claude Code

1. Install the hook binary:

   ```bash
   npm install -g @timeplus/agentguard-claudecode-plugin
   ```

2. Merge the following into `~/.claude/settings.json` (keep any existing `env` / `hooks` entries):

   ```json
   {
     "env": {
       "AGENTGUARD_AGENT_ID": "<your-hostname>",
       "AGENTGUARD_DEPLOYMENT_ID": "local",
       "AGENTGUARD_DEPLOYMENT_NAME": "Local Dev",
       "AGENTGUARD_TIMEPLUS_URL": "http://localhost:3218",
       "AGENTGUARD_INGEST_API": "timeplusd",
       "AGENTGUARD_DATABASE": "ag",
       "AGENTGUARD_STREAM": "agentguard_hook_events",
       "AGENTGUARD_USERNAME": "<timeplus user>",
       "AGENTGUARD_PASSWORD": "<timeplus password>",
       "AGENTGUARD_HOLDS_ENABLED": "false"
     },
     "hooks": {
       "PreToolUse":   [{ "matcher": "*", "hooks": [{ "type": "command", "command": "agentguard-hook", "timeout": 600000 }] }],
       "SessionStart": [{ "matcher": "*", "hooks": [{ "type": "command", "command": "agentguard-hook", "async": true }] }],
       "PostToolUse":  [{ "matcher": "*", "hooks": [{ "type": "command", "command": "agentguard-hook", "async": true }] }]
     }
   }
   ```

   Register the same async entry for `SessionEnd`, `UserPromptSubmit`, `PostToolUseFailure`, `SubagentStart`, `SubagentStop`, `Stop`, `PermissionDenied`, `InstructionsLoaded`, `PreCompact`, `PostCompact`, `PermissionRequest`, `Notification`, `StopFailure`, `TaskCreated`, `TaskCompleted`, `PostModelSwitch`, and `ConfigChange`. The full snippet is in [`hooks.json`](../agents/claudecode/agentguard-plugin/hooks.json) and the [skill](../skills/agentguard/SKILL.md).

3. Optional — OTel metrics (tokens, cost, latency). Add to the same `env` block, pointing at the app's OTLP input:

   ```json
   "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
   "OTEL_METRICS_EXPORTER": "otlp",
   "OTEL_LOGS_EXPORTER": "otlp",
   "OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
   "OTEL_EXPORTER_OTLP_ENDPOINT": "http://<timeplus-host>:4318"
   ```

   See [`agents/claudecode/otel_timeplus.md`](../agents/claudecode/otel_timeplus.md) for details.

4. Quit and reopen Claude Code so the hooks take effect.

Working from a checkout of this repo instead? `cd agents/claudecode && make install` builds the plugin and writes the hooks, `AGENTGUARD_*`, and OTel variables into `~/.claude/settings.json` in one step (`make unconfigure unconfigure-otel` removes them).

## OpenClaw

1. Install the plugin:

   ```bash
   openclaw plugins install @timeplus/agentguard-openclaw-plugin
   ```

2. Add the plugin entry to `.openclaw/openclaw.json` (back the file up first):

   ```json
   {
     "plugins": {
       "entries": {
         "agentguard": {
           "enabled": true,
           "config": {
             "timeplusUrl": "http://localhost:3218",
             "ingestApi": "timeplusd",
             "database": "ag",
             "stream": "agentguard_hook_events",
             "username": "<timeplus user>",
             "password": "<timeplus password>",
             "deploymentId": "local",
             "deploymentName": "Local Dev",
             "flushMs": 2000,
             "batchSize": 50,
             "holdsEnabled": false
           }
         }
       }
     }
   }
   ```

   When OpenClaw runs in Docker and Timeplus on the host, use `http://host.docker.internal:3218`.

3. Restart: `openclaw gateway restart`.

OTel export for OpenClaw uses its `diagnostics-otel` plugin — see [`agents/openclaw/otel_timeplus.md`](../agents/openclaw/otel_timeplus.md). The plugin README covers the [manual / Docker install](../agents/openclaw/agentguard-plugin/README.md#installation).

## Hermes

```bash
pip install agentguard-hermes-plugin
agentguard-hermes install          # copies the plugin into ~/.hermes/plugins/agentguard/
hermes plugins enable agentguard
```

Then export the configuration in your shell profile and restart Hermes:

```bash
export AGENTGUARD_TIMEPLUS_URL=http://localhost:3218
export AGENTGUARD_USERNAME=<timeplus user>
export AGENTGUARD_PASSWORD=<timeplus password>
export AGENTGUARD_DATABASE=ag
export AGENTGUARD_AGENT_ID=$(hostname)
export AGENTGUARD_DEPLOYMENT_ID=local
export AGENTGUARD_DEPLOYMENT_NAME="Local Dev"
```

## Codex

> **Status: initial scaffold.** The Codex plugin exports OTel spans instead of
> writing `agentguard_hook_events`, covers only five lifecycle hooks, and needs
> the Python SDK (`timeplus-agentguard` on PyPI). The plugin itself is not on PyPI yet. See the
> [plugin README](../agents/codex/agentguard-plugin/README.md) and the
> [implementation TODO](../agents/codex/docs/todo.md).

1. Install the plugin from a checkout of this repo (the SDK is pulled from PyPI as a dependency):

   ```bash
   pip install -e agents/codex/agentguard-plugin
   ```

2. Merge [`agents/codex/agentguard-plugin/hooks.json`](../agents/codex/agentguard-plugin/hooks.json) into `~/.codex/hooks.json` (or the equivalent `[hooks]` table in `~/.codex/config.toml`), keeping any existing entries.

3. Export the configuration. Hooks run in a fresh process, so environment variables are the usual way to configure the plugin; the `--agent-id`, `--deployment-id`, `--deployment-name` and `--endpoint` flags override them per hook.

   ```bash
   export AGENTGUARD_AGENT_ID=$(hostname)
   export AGENTGUARD_DEPLOYMENT_ID=local
   export AGENTGUARD_DEPLOYMENT_NAME="Local Dev"
   export OTEL_EXPORTER_OTLP_ENDPOINT=http://<timeplus-host>:4318
   ```

4. Start Codex and run `/hooks` once to review and trust the AgentGuard entries.

Codex spans go to the app's OTLP/HTTP input (port `4318`), not to `agentguard_hook_events`, so the Verify query below does not apply to Codex yet.

## Verify

```sql
SELECT agent_type, hook_name, agent_id, tool_name, event_time
FROM table(ag.agentguard_hook_events)
ORDER BY event_time DESC
LIMIT 10;
```

Rows should appear within a few seconds of agent activity. If nothing arrives, check the URL/port for the chosen ingest mode, the credentials, and that the AgentGuard app is installed (the `ag` database exists). Plugins fail silently by design so they never block the agent.

## Configuration reference

| Variable | OpenClaw config key | Default | Purpose |
|----------|--------------------|---------|---------|
| `AGENTGUARD_TIMEPLUS_URL` | `timeplusUrl` | `http://localhost:3218` | Timeplus base URL (see [ingest API](#choosing-the-ingest-api)) |
| `AGENTGUARD_INGEST_API` | `ingestApi` | `timeplusd` | `timeplusd` or `rest` |
| `AGENTGUARD_WORKSPACE` | `workspace` | `default` | REST API workspace id; `default` is correct for self-hosted |
| `AGENTGUARD_DATABASE` | `database` | `ag` | Database owning the AgentGuard streams (the app's `db_name`) |
| `AGENTGUARD_STREAM` | `stream` | `agentguard_hook_events` | Target stream |
| `AGENTGUARD_USERNAME` | `username` | `proton` | Timeplus username |
| `AGENTGUARD_PASSWORD` | `password` | _(empty)_ | Timeplus password |
| `AGENTGUARD_AGENT_ID` | — | hostname | Identifies this agent instance (Claude Code, Hermes, Codex) |
| `AGENTGUARD_DEPLOYMENT_ID` | `deploymentId` | `default` / `local` | Logical deployment group |
| `AGENTGUARD_DEPLOYMENT_NAME` | `deploymentName` | | Human-readable deployment label |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | `http://localhost:4318` | Codex: OTLP/HTTP endpoint for span export |
| `AGENTGUARD_FLUSH_MS` / `AGENTGUARD_BATCH_SIZE` | `flushMs` / `batchSize` | `2000` / `50` | OpenClaw batching |
| `AGENTGUARD_TRANSCRIPT_MESSAGES` / `AGENTGUARD_TRANSCRIPT_CONTENT_CHARS` | — | `50` / `10000` | Claude Code context-event truncation |

### Legacy hold settings

`AGENTGUARD_HOLDS_ENABLED` (`holdsEnabled`), `AGENTGUARD_URL` (`agentguardUrl`), and `AGENTGUARD_HOLDS_FAIL_POLICY` (`holdFailPolicy`) drive the synchronous tool-call hold path, which called the retired standalone AgentGuard server. The Timeplus app has no hold capability — keep holds disabled (the default) and leave the other two unset.
