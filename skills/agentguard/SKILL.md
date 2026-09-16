---
name: agentguard
description: >
  Install AgentGuard monitoring plugins onto AI coding agents (Claude Code, OpenClaw, Hermes).
---

# AgentGuard Agent Installation

## Overview

AgentGuard monitors AI coding agents by installing a lightweight hook plugin that streams lifecycle events (session start/end, LLM calls, tool invocations) to Timeplus.

The plugin also enables real-time security holds so AgentGuard can block dangerous tool calls before they execute.

Three agent types are supported:

| Agent | Plugin package | Install method |
|-------|---------------|----------------|
| Claude Code | `@timeplus/agentguard-claudecode-plugin` (npm) | npm global install + `~/.claude/settings.json` hooks |
| OpenClaw | `@timeplus/agentguard-openclaw-plugin` (npm) | `openclaw plugins install` + `openclaw.json` config |
| Hermes | `agentguard-hermes-plugin` (pip) | pip install + `hermes plugins enable` + env vars |

## Prerequisites

- **Node.js ≥ 18** (Claude Code, OpenClaw) or **Python 3** (Hermes)
- **npm** (Claude Code, OpenClaw) or **pip/pip3** (Hermes)

## Shared configuration values

Every agent needs these values. Ask user to provide ALL of them before running the commands. Use default value as a hint but ask user to explicitly confirm or give a different value.

| Value | Default | Description |
|-------|---------|-------------|
| `AGENT_ID` | | Unique identifier for this machine |
| `DEPLOYMENT_ID` | local | Grouping key for multi-machine deploys |
| `DEPLOYMENT_NAME` | Local Dev | Human-readable label |
| `TIMEPLUS_URL` | http://localhost:3218 | Timeplus HTTP endpoint |
| `TIMEPLUS_USER` | | Timeplus username |
| `TIMEPLUS_PASSWORD` | | Timeplus password |
| `AGENTGUARD_DATABASE` | ag | Timeplus database owning the AgentGuard streams. Matches the AgentGuard Timeplus app's `db_name: ag`. |
| `AGENTGUARD_URL` | http://localhost:8080 | AgentGuard backend (for hold callbacks) |
| `HOLDS_ENABLED` | false | Opt-in to synchronous tool-call blocking. Set to `true` to enable. |
| `HOLD_FAIL_POLICY` | deny | deny (block on error) or allow (fail open) |


## Claude Code

### 1. Install the plugin

```bash
npm install -g @timeplus/agentguard-claudecode-plugin
```

This provides the `agentguard-hook` command that Claude Code invokes on each hook event.

### 2. Register hooks in `~/.claude/settings.json`

Add / update below content in openclaw.json. The parameters surrounded by '<>' are replaced by [shared configuration values](#shared-configuration-values)

Note that user may already configured "env" and "hooks" keys. Do not replace the existing value but merge and update with the new keys / values.

```
{
    "env": {
        "AGENTGUARD_AGENT_ID": <AGENT_ID>,
        "AGENTGUARD_DEPLOYMENT_ID": <DEPLOYMENT_ID>,
        "AGENTGUARD_DEPLOYMENT_NAME": <DEPLOYMENT_NAME>,
        "AGENTGUARD_TIMEPLUS_URL": <TIMEPLUS_URL>,
        "AGENTGUARD_DATABASE": <AGENTGUARD_DATABASE>,
        "AGENTGUARD_STREAM": "agentguard_hook_events",
        "AGENTGUARD_USERNAME": <TIMEPLUS_USER>,
        "AGENTGUARD_PASSWORD": <TIMEPLUS_PASSWORD>,
        "AGENTGUARD_URL": <AGENTGUARD_URL>,
        "AGENTGUARD_HOLDS_FAIL_POLICY": <HOLDS_FAIL_POLICY>,
        "AGENTGUARD_HOLDS_ENABLED": <HOLDS_ENABLED>
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
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": True}]
            }
        ],
        "SessionEnd": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": True}]
            }
        ],
        "UserPromptSubmit": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": True}]
            }
        ],
        "PostToolUse": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": True}]
            }
        ],
        "PostToolUseFailure": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": True}]
            }
        ],
        "SubagentStart": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": True}]
            }
        ],
        "SubagentStop": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": True}]
            }
        ],
        "Stop": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": True}]
            }
        ],
        "PermissionDenied": [
            {
                "matcher": "*",
                "hooks": [{"type": "command", "command": "agentguard-hook", "async": True}]
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
                    "stream": "agentguard_hook_events",
                    "flushMs": 2000,
                    "batchSize": 50,
                    "username": <TIMEPLUS_USER>,
                    "password": <TIMEPLUS_PASSWORD>,
                    "database": <AGENTGUARD_DATABASE>,
                    "deploymentId": <DEPLOYMENT_ID>,
                    "deploymentName": <DEPLOYMENT_NAME>,
                    "agentguardUrl": <AGENTGUARD_URL>,
                    "holdFailPolicy": <HOLD_FAIL_POLICY>,
                    "holdsEnabled": <HOLDS_ENABLED>
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
- AGENTGUARD_USERNAME
- AGENTGUARD_PASSWORD
- AGENTGUARD_AGENT_ID
- AGENTGUARD_DEPLOYMENT_ID
- AGENTGUARD_DEPLOYMENT_NAME
- AGENTGUARD_URL
- AGENTGUARD_DATABASE
- AGENTGUARD_HOLDS_ENABLED
- AGENTGUARD_HOLDS_FAIL_POLICY
