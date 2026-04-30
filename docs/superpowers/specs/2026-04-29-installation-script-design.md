# Installation Script Design

**Issue:** #74  
**Date:** 2026-04-29  
**Status:** Approved

## Overview

Add an interactive shell script (`agents/install.sh`) that automates the full AgentGuard plugin installation for Claude Code, OpenClaw, and Hermes agents. The onboarding UI Step 1 replaces its manual copy-paste code blocks with a "Download Install Script" button that serves this file.

## Script (`agents/install.sh`)

A single portable bash script (bash 3.2+, macOS and Linux compatible).

### Flow

1. **Banner** — welcome message with AgentGuard branding
2. **Agent selection** — `select` menu: Claude Code / OpenClaw / Hermes
3. **Shared config prompts** — all with defaults shown in brackets:
   - Agent ID (default: `$(hostname)`)
   - Deployment ID (default: `local`)
   - Deployment Name (default: `Local Dev`)
   - Timeplus URL (default: `http://localhost:3218`)
   - Timeplus Username (default: `proton`)
   - Timeplus Password (silent input, no echo via `read -s`)
   - AgentGuard URL (default: `http://localhost:8080`)
   - Holds Enabled (y/n, default: `y`)
   - Hold Fail Policy (`deny`/`allow`, default: `deny`)
4. **Agent-specific install steps:**

   **Claude Code:**
   - `npm install -g @timeplus/agentguard-claudecode-plugin`
   - Inline Python: merge hooks + env into `~/.claude/settings.json` (idempotent, same logic as `agents/claudecode/Makefile`)
   - Inline Python: merge OTel env keys into `~/.claude/settings.json`

   **OpenClaw:**
   - Prompt for OpenClaw project directory (default: current directory); `cd` there before running
   - `npm install @timeplus/agentguard-openclaw-plugin` (local, inside that project directory)
   - Inline Python: patch `<project-dir>/.openclaw/openclaw.json` with plugin config + OTel diagnostics section (same logic as `agents/openclaw/Makefile`)
   - Prompt for OTel endpoint (defaults to `http://host.docker.internal:4318` for Docker setups)

   **Hermes:**
   - `pip install agentguard-hermes-plugin`
   - Print `export` lines to stdout
   - Offer to append them to `~/.zshrc` or `~/.bashrc` (user confirms y/n)

5. **Success summary** — lists what was configured and prints next steps (restart Claude Code / OpenClaw, open AgentGuard dashboard)

### Design constraints
- Requires: `bash`, `python3`, `node`/`npm` (Claude Code/OpenClaw), or `pip` (Hermes)
- Pre-flight checks: verify required tools exist, print friendly error if missing
- Idempotent: re-running the script safely updates existing config without duplication (uses same upsert logic as the Makefile)
- No network calls to AgentGuard backend from the script — pure local install

## Backend endpoint

**`GET /api/onboarding/install-script`**

- Handler: add to `backend/internal/handlers/onboarding.go`
- Route: register in `backend/cmd/server/main.go`
- Response: serves `agents/install.sh` with:
  - `Content-Type: text/x-shellscript`
  - `Content-Disposition: attachment; filename="agentguard-install.sh"`
- Embedding: new `backend/internal/scripts/embed.go` with `//go:embed` pointing at `../../agents/install.sh`

## Frontend changes (Onboarding Step 1)

Replace the per-agent manual code blocks with:
1. A **"Download Install Script"** button — `GET /api/onboarding/install-script` → browser download
2. A `CodeBlock` showing how to run it: `bash agentguard-install.sh`
3. Keep the config form fields as a **"Values to have ready"** reference checklist — they do not feed into the script, but help users know what to gather before running

Step 2 (OTel Config) remains unchanged — it becomes informational confirmation that the script already handled OTel setup.

## Files to create / modify

| File | Action |
|------|--------|
| `agents/install.sh` | Create — interactive install script |
| `backend/internal/scripts/embed.go` | Create — `//go:embed` for install.sh |
| `backend/internal/handlers/onboarding.go` | Add `ServeInstallScript` handler |
| `backend/cmd/server/main.go` | Register `GET /api/onboarding/install-script` |
| `frontend/src/pages/Onboarding.jsx` | Replace Step 1 code blocks with download button |
