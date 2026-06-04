# Plugin Installation

AgentGuard ships an interactive installer script that handles the full plugin setup for Claude Code, OpenClaw, and Hermes agents in one step.

## Quick Start

Download the script from the AgentGuard onboarding UI (**Connect Agent → Install Plugin → Download Install Script**), then run it:

```bash
bash agentguard-install.sh
```

The script prompts for all required values and configures everything automatically. No manual file editing needed.

## Prerequisites

| Agent | Required tools |
|-------|---------------|
| Claude Code | `node` >= 18, `npm`, `python3` |
| OpenClaw | `node` >= 18, `npm`, `python3`, running OpenClaw project (run `make init` first) |
| Hermes | `pip3` or `pip` (Python 3) |

## What the Script Does

### Shared prompts (all agents)

The script asks for these values before branching per-agent. Press Enter to accept the default shown in brackets.

| Prompt | Default | Purpose |
|--------|---------|---------|
| Agent ID | hostname | Identifies this machine in AgentGuard |
| Deployment ID | `local` | Groups agents under a logical deployment |
| Deployment Name | `Local Dev` | Human-readable deployment label |
| Timeplus URL | `http://localhost:3218` | Timeplus HTTP endpoint |
| Timeplus Username | `proton` | Timeplus auth |
| Timeplus Password | _(none)_ | Timeplus auth (input is hidden) |
| AgentGuard URL | `http://localhost:8080` | AgentGuard server for hold callbacks |
| Enable holds | `Y` | Whether PreToolUse blocking is active |
| Hold fail policy | `deny` | What happens if AgentGuard is unreachable: `deny` blocks the tool call, `allow` passes it through |

### Claude Code

1. `npm install -g @timeplus/agentguard-claudecode-plugin` — installs the `agentguard-hook` binary globally
2. Merges the following into `~/.claude/settings.json` (idempotent — safe to re-run):
   - **hooks** — `PreToolUse` wired synchronously so tool calls can be blocked; all other events wired asynchronously
   - **env** — all `AGENTGUARD_*` vars plus OTel vars for Claude Code telemetry
3. OTel endpoint is derived automatically from the Timeplus URL (same host, port 4318)

After installation, quit and reopen Claude Code for the hooks to take effect.

**What gets written to `~/.claude/settings.json`:**

```json
{
  "hooks": {
    "PreToolUse": [{ "matcher": "*", "hooks": [{ "type": "command", "command": "agentguard-hook", "timeout": 600000 }] }],
    "SessionStart": [{ "matcher": "*", "hooks": [{ "type": "command", "command": "agentguard-hook", "async": true }] }],
    "PostToolUse": [{ "matcher": "*", "hooks": [{ "type": "command", "command": "agentguard-hook", "async": true }] }]
  },
  "env": {
    "AGENTGUARD_AGENT_ID": "<your-hostname>",
    "AGENTGUARD_TIMEPLUS_URL": "http://localhost:3218",
    "AGENTGUARD_HOLDS_ENABLED": "true",
    "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318",
    "..."
  }
}
```

### OpenClaw

OpenClaw runs in Docker, so the script must be run from your OpenClaw project directory (the one containing `Makefile` and `.openclaw/`).

1. Prompts for the OpenClaw project directory (defaults to current directory)
2. Prompts for the OTel endpoint (defaults to `http://host.docker.internal:4318` — the correct value when Timeplus runs on the host and OpenClaw runs in Docker)
3. `npm install @timeplus/agentguard-openclaw-plugin` — installs locally into the project
4. Patches `.openclaw/openclaw.json` (idempotent — safe to re-run):
   - Adds `plugins.load.paths` entry pointing to the installed plugin directory
   - Adds `plugins.entries.agentguard` with full connection config
   - Adds `diagnostics.otel` section for the `diagnostics-otel` plugin
   - Sets `gateway.bind = "lan"` so Docker port mapping works

After installation, restart OpenClaw (`make start`).

> **Note:** The OpenClaw project must already be initialised (`make init` completed and `.openclaw/openclaw.json` exists) before running the installer.

### Hermes

1. `pip3 install agentguard-hermes-plugin`
2. `agentguard-hermes install` — places the plugin in Hermes's plugin directory
3. Prints the required `export` lines and offers to append them to `~/.zshrc` or `~/.bashrc`

**Idempotency:** If `AGENTGUARD_TIMEPLUS_URL` is already present in your shell profile, the script skips the append and prints the new values so you can update them manually.

After installation, reload your shell (`source ~/.zshrc`) and restart Hermes.

## Re-running the Script

The script is designed to be re-run safely:

- **Claude Code:** Hook entries are replaced (not duplicated) by matching on the command name. Env vars are overwritten.
- **OpenClaw:** All config keys use `setdefault` patterns — only missing keys are added; existing values are overwritten for the `agentguard` plugin block.
- **Hermes:** Detects existing AgentGuard vars in the shell profile and skips the append, printing new values instead.

## Serving the Script from AgentGuard

The script is embedded in the AgentGuard server binary at build time. The endpoint `GET /api/onboarding/install-script` (authenticated) serves it as a file download. The onboarding UI's "Download Install Script" button calls this endpoint.

When you edit `agents/install.sh`, run `make scripts-sync` to copy it into `backend/internal/scripts/` before building the binary:

```bash
make scripts-sync   # sync script to backend embed dir
make build          # full production build (includes scripts-sync automatically)
```

## Environment Variables Reference

All variables written by the installer:

| Variable | Agent | Purpose |
|----------|-------|---------|
| `AGENTGUARD_AGENT_ID` | CC, Hermes | Machine identifier shown in AgentGuard |
| `AGENTGUARD_DEPLOYMENT_ID` | CC, OC, Hermes | Logical deployment group |
| `AGENTGUARD_DEPLOYMENT_NAME` | CC, OC, Hermes | Human-readable deployment label |
| `AGENTGUARD_TIMEPLUS_URL` | CC, OC, Hermes | Timeplus HTTP endpoint |
| `AGENTGUARD_USERNAME` | CC, OC, Hermes | Timeplus username |
| `AGENTGUARD_PASSWORD` | CC, OC, Hermes | Timeplus password |
| `AGENTGUARD_STREAM` | CC, OC | Target Timeplus stream (`agentguard_hook_events`) |
| `AGENTGUARD_URL` | CC, OC, Hermes | AgentGuard server for hold callbacks |
| `AGENTGUARD_HOLDS_ENABLED` | CC, OC, Hermes | `true` / `false` |
| `AGENTGUARD_HOLDS_FAIL_POLICY` | CC, OC, Hermes | `deny` / `allow` |
| `CLAUDE_CODE_ENABLE_TELEMETRY` | CC | Enables Claude Code OTel export |
| `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA` | CC | Enables enhanced telemetry |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | CC | OTLP HTTP endpoint (port 4318) |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | CC | Always `http/protobuf` |
| `OTEL_METRIC_EXPORT_INTERVAL` | CC | `10000` ms |
| `OTEL_LOGS_EXPORT_INTERVAL` | CC | `5000` ms |
| `OTEL_LOG_TOOL_DETAILS` | CC | `1` — include tool call detail in OTel logs |

OC = OpenClaw (config goes into `openclaw.json`, not env vars). CC = Claude Code (`~/.claude/settings.json` env block). Hermes = shell profile.
