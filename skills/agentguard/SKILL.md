---
name: agentguard
description: >
  Install AgentGuard monitoring plugins onto AI coding agents (Claude Code, OpenClaw, Hermes, Codex).
---

# AgentGuard Agent Installation

## Overview

AgentGuard monitors AI coding agents by installing a lightweight hook plugin that streams lifecycle events (session start/end, LLM calls, tool invocations) to Timeplus, where the AgentGuard Timeplus app (database `ag`) turns them into normalized events, metrics, and security detections.

The AgentGuard app must already be installed on the target Timeplus — it creates the `ag.agentguard_hook_events` stream the plugins write to. If the user has not installed it, point them to https://github.com/timeplus-io/apps/tree/main/apps/agentguard first.

Three agent types are supported, plus an initial Codex scaffold:

| Agent | Plugin package | Install method |
|-------|---------------|----------------|
| Claude Code | `@timeplus/agentguard-claudecode-plugin` (npm) | npm global install + `~/.claude/settings.json` hooks |
| OpenClaw | `@timeplus/agentguard-openclaw-plugin` (npm) | `openclaw plugins install` + `openclaw.json` config |
| Hermes | `agentguard-hermes-plugin` (pip) | pip install + `hermes plugins enable` + env vars |
| Codex | `agentguard-codex-plugin` (pip, from a repo checkout) | pip install + `~/.codex/hooks.json` + env vars (OTel spans, initial scaffold) |

## Prerequisites

- **Node.js ≥ 18** (Claude Code, OpenClaw) or **Python ≥ 3.10** (Hermes, Codex)
- **npm** (Claude Code, OpenClaw) or **pip/pip3** (Hermes, Codex)

## Shared configuration values

Every hook-event agent (Claude Code, OpenClaw, Hermes) needs these values. Codex only uses `AGENT_ID`, `DEPLOYMENT_ID`, `DEPLOYMENT_NAME`, and an OTLP endpoint — skip the Timeplus URL, ingest mode, and credentials for it. Ask the user to provide all applicable values before running the commands. Use the default value as a hint but ask the user to explicitly confirm or give a different value.

| Value | Default | Description |
|-------|---------|-------------|
| `AGENT_ID` | | Unique identifier for this machine |
| `DEPLOYMENT_ID` | local | Grouping key for multi-machine deploys |
| `DEPLOYMENT_NAME` | Local Dev | Human-readable label |
| `TIMEPLUS_URL` | http://localhost:3218 | Timeplus base URL. With `INGEST_API=timeplusd` this is timeplusd's HTTP port (`http://host:3218`). With `INGEST_API=rest` it is the Timeplus web console / REST API base (`http://host:8000`, or its HTTPS URL when it sits behind an ingress or gateway). |
| `INGEST_API` | timeplusd | `timeplusd` (direct to port 3218) or `rest` (Timeplus REST API on port 8000, the web console / API server). See [Choosing the ingest API](#choosing-the-ingest-api). |
| `TIMEPLUS_USER` | | Timeplus username — used for Basic auth in both modes |
| `TIMEPLUS_PASSWORD` | | Timeplus password — used for Basic auth in both modes |
| `AGENTGUARD_DATABASE` | ag | Timeplus database owning the AgentGuard streams. Matches the AgentGuard Timeplus app's `db_name: ag`. |

The plugins also carry legacy hold settings (`AGENTGUARD_URL`, `HOLDS_ENABLED`, `HOLD_FAIL_POLICY`) for the retired standalone AgentGuard server. The Timeplus app has no hold capability: do not ask the user for these, always write `HOLDS_ENABLED=false`, and omit the other two.

### Choosing the ingest API

Ask where Timeplus runs before filling in the URL:

- **timeplusd's port 3218 is reachable from the agent host** (typical single-host / Docker setup) → `INGEST_API=timeplusd` (default), `TIMEPLUS_URL=http://host:3218`.
- **Only the Timeplus web console is exposed** (Kubernetes / on-prem behind an ingress or gateway; port 8000 or its HTTPS URL is reachable but 3218 is not) → `INGEST_API=rest`, `TIMEPLUS_URL=http://host:8000` (or the console's HTTPS URL).

Both modes use the **same** `TIMEPLUS_USER` / `TIMEPLUS_PASSWORD` (Basic auth) — the REST API validates them against timeplusd, so no separate credential is needed. Both send the same event payload; only the URL and the path differ. The REST API workspace id (`AGENTGUARD_WORKSPACE`) defaults to `default`, which is correct for self-hosted deployments — do not ask for it.


## Claude Code

### 1. Install the plugin

```bash
npm install -g @timeplus/agentguard-claudecode-plugin
```

This provides the `agentguard-hook` command that Claude Code invokes on each hook event.

### 2. Register hooks in `~/.claude/settings.json`

Add / update below content in `~/.claude/settings.json`. The parameters surrounded by '<>' are replaced by [shared configuration values](#shared-configuration-values)

Note that user may already configured "env" and "hooks" keys. Do not replace the existing value but merge and update with the new keys / values.

```
{
    "env": {
        "AGENTGUARD_AGENT_ID": <AGENT_ID>,
        "AGENTGUARD_DEPLOYMENT_ID": <DEPLOYMENT_ID>,
        "AGENTGUARD_DEPLOYMENT_NAME": <DEPLOYMENT_NAME>,
        "AGENTGUARD_TIMEPLUS_URL": <TIMEPLUS_URL>,
        "AGENTGUARD_INGEST_API": <INGEST_API>,
        "AGENTGUARD_DATABASE": <AGENTGUARD_DATABASE>,
        "AGENTGUARD_STREAM": "agentguard_hook_events",
        "AGENTGUARD_USERNAME": <TIMEPLUS_USER>,
        "AGENTGUARD_PASSWORD": <TIMEPLUS_PASSWORD>,
        "AGENTGUARD_HOLDS_ENABLED": "false"
    },
    "hooks": {
        "PreToolUse": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "timeout": 600000}]
            }
        ],
        "SessionStart": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "SessionEnd": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "UserPromptSubmit": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "PostToolUse": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "PostToolUseFailure": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "SubagentStart": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "SubagentStop": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "Stop": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "PermissionDenied": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "InstructionsLoaded": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "PreCompact": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "PostCompact": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "PermissionRequest": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "Notification": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "StopFailure": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "TaskCreated": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "TaskCompleted": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "PostModelSwitch": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ],
        "ConfigChange": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": true}]
            }
        ]
    }
}
```

## OpenClaw

### 1. Install the plugin

```bash
openclaw plugins install @timeplus/agentguard-openclaw-plugin
```

### 2. Patch `.openclaw/openclaw.json`

If the config file doesn't exist yet, run `openclaw onboard` first.

Backup the openclaw.json file before doing the changes.

Add / update below content in openclaw.json. The parameters surrounded by '<>' are replaced by [shared configuration values](#shared-configuration-values)

```
{
    "plugins": {
        "entries": {
            "agentguard": {
                "enabled": true,
                "config": {
                    "timeplusUrl": <TIMEPLUS_URL>,
                    "ingestApi": <INGEST_API>,
                    "stream": "agentguard_hook_events",
                    "flushMs": 2000,
                    "batchSize": 50,
                    "username": <TIMEPLUS_USER>,
                    "password": <TIMEPLUS_PASSWORD>,
                    "database": <AGENTGUARD_DATABASE>,
                    "deploymentId": <DEPLOYMENT_ID>,
                    "deploymentName": <DEPLOYMENT_NAME>,
                    "holdsEnabled": false
                }
            }
        }
    }
}
```

### 3. Restart OpenClaw

```bash
openclaw gateway restart
```

## Hermes

### 1. Install the plugin

```bash
pip install agentguard-hermes-plugin
agentguard-hermes install
hermes plugins enable agentguard
```

### 2. Set environment variables

Modify the shell configuration file to set the environment variables by the user provided [shared configuration values](#shared-configuration-values). The environment variables have prefix 'AGENTGUARD_'.

- AGENTGUARD_TIMEPLUS_URL
- AGENTGUARD_INGEST_API
- AGENTGUARD_USERNAME
- AGENTGUARD_PASSWORD
- AGENTGUARD_AGENT_ID
- AGENTGUARD_DEPLOYMENT_ID
- AGENTGUARD_DEPLOYMENT_NAME
- AGENTGUARD_DATABASE
- AGENTGUARD_HOLDS_ENABLED (always `false`)

## Codex

> **Initial scaffold.** Codex support exports OTel spans (not `agentguard_hook_events`), covers only the five lifecycle hooks, and is installed from a checkout of this repo (the plugin is not on PyPI yet; its SDK dependency `timeplus-agentguard` is). Point users at the [plugin README](../../agents/codex/agentguard-plugin/README.md) and [implementation TODO](../../agents/codex/docs/todo.md) for the open items.

### 1. Install the plugin

From a checkout of this repository:

```bash
pip install -e agents/codex/agentguard-plugin
```

### 2. Register hooks in `~/.codex/hooks.json`

Merge [`agents/codex/agentguard-plugin/hooks.json`](../../agents/codex/agentguard-plugin/hooks.json) into `~/.codex/hooks.json` (or the equivalent `[hooks]` table in `~/.codex/config.toml`), keeping existing entries.

### 3. Set environment variables

Codex uses the OTLP endpoint rather than `TIMEPLUS_URL` / `INGEST_API` / `TIMEPLUS_USER` / `TIMEPLUS_PASSWORD`:

```bash
export AGENTGUARD_AGENT_ID=<AGENT_ID>
export AGENTGUARD_DEPLOYMENT_ID=<DEPLOYMENT_ID>
export AGENTGUARD_DEPLOYMENT_NAME="<DEPLOYMENT_NAME>"
export OTEL_EXPORTER_OTLP_ENDPOINT=http://<timeplus-host>:4318
```

### 4. Trust the hooks

Start Codex and run `/hooks` once to review and trust the AgentGuard entries.
