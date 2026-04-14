# Claude Code Onboarding UI — Design Spec

## Goal

Enable the Claude Code agent card in the onboarding wizard and add agent-branched content for steps 1 and 2. The backend handler (`generateClaudeCodeConfig`) is already implemented; this is a purely frontend change.

## Context

The current onboarding wizard (`frontend/src/pages/Onboarding.jsx`) has a 4-step flow:

| Step | Label |
|------|-------|
| 0 | Select Agent |
| 1 | Install Plugin |
| 2 | OTel Config |
| 3 | Activate Rules |

OpenClaw is the only agent with real content. The `claude-code` card is disabled (`disabled: true`, badge "Coming Soon"). Steps 1–3 have no Claude Code branching.

## Changes Required

### Step 0 — Select Agent card

Change the `claude-code` entry:
- `disabled: false`
- Badge: `"Supported"` (was `"Coming Soon"`)
- Description: `"Anthropic's Claude Code CLI. Monitor tool use, LLM calls, cost, and security events in real time."`

### Step 1 — Install Plugin

Branch the step content on `selectedAgent === 'claude-code'`.

**Config fields** (rendered identically to OpenClaw fields):

| Field | State key | Default | Required |
|-------|-----------|---------|----------|
| Agent ID | `agentId` | `""` | Yes — no fallback; identifies the instance |
| Deployment ID | `deploymentId` | `"local"` | No |
| Deployment Name | `deploymentName` | `"Local Dev"` | No |
| Timeplus URL | `timeplusUrl` | `"http://localhost:3218"` | No |

**Install command** displayed above the config fields:
```bash
npm install -g @timeplus/agentguard-claudecode-plugin
```

**Two tabs** — `settings.json` and `env`:

#### `settings.json` tab

Shows the full block to merge into `~/.claude/settings.json`. Two sub-sections rendered together as one copyable JSON:

```json
{
  "hooks": {
    "SessionStart":        [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "SessionEnd":          [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "UserPromptSubmit":    [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "PreToolUse":          [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "PostToolUse":         [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "PostToolUseFailure":  [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "SubagentStart":       [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "SubagentStop":        [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "Stop":                [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}],
    "PermissionDenied":    [{"matcher":"*","hooks":[{"type":"command","command":"agentguard-hook","async":true}]}]
  },
  "env": {
    "AGENTGUARD_AGENT_ID":          "<agentId>",
    "AGENTGUARD_DEPLOYMENT_ID":     "<deploymentId>",
    "AGENTGUARD_DEPLOYMENT_NAME":   "<deploymentName>",
    "AGENTGUARD_TIMEPLUS_URL":      "<timeplusUrl>"
  }
}
```

Values in `<>` are substituted from the form fields. Add a note: *"Merge with your existing `~/.claude/settings.json` — do not replace the whole file."*

#### `env` tab

Shell exports for users who prefer setting env vars in their shell profile:

```bash
export AGENTGUARD_AGENT_ID="<agentId>"
export AGENTGUARD_DEPLOYMENT_ID="<deploymentId>"
export AGENTGUARD_DEPLOYMENT_NAME="<deploymentName>"
export AGENTGUARD_TIMEPLUS_URL="<timeplusUrl>"
```

### Step 2 — OTel Config

Branch on `selectedAgent === 'claude-code'`.

**One editable field:**
- OTLP Endpoint — auto-derived from `timeplusUrl` by replacing the port with `4318`. Editable in case the user runs Timeplus on a non-standard host.
  - Example: `http://localhost:3218` → `http://localhost:4318`

**Generated block** to copy-paste into `~/.claude/settings.json`:

```json
{
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY":        "1",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
    "OTEL_METRICS_EXPORTER":               "otlp",
    "OTEL_LOGS_EXPORTER":                  "otlp",
    "OTEL_TRACES_EXPORTER":                "otlp",
    "OTEL_EXPORTER_OTLP_PROTOCOL":         "http/protobuf",
    "OTEL_EXPORTER_OTLP_ENDPOINT":         "<otelEndpoint>",
    "OTEL_METRIC_EXPORT_INTERVAL":         "10000",
    "OTEL_LOGS_EXPORT_INTERVAL":           "5000",
    "OTEL_LOG_TOOL_DETAILS":               "1"
  }
}
```

Note below snippet: *"These go inside your existing `~/.claude/settings.json` — merge with any existing `env` keys."*

**Skip button** — same as OpenClaw. Skipping omits LLM token/cost/latency metrics; hook events still work.

### Step 3 — Activate Rules

No changes. Rules are CIM-based and agent-agnostic.

## State

No new state keys needed beyond reusing existing ones (`deploymentId`, `deploymentName`, `timeplusUrl`) plus adding:
- `agentId` — string, default `""`
- `otelEndpoint` — string, derived from `timeplusUrl` (port swap), user-editable in step 2

## Files to Change

| File | Change |
|------|--------|
| `frontend/src/pages/Onboarding.jsx` | All changes — card enable, step 1 branch, step 2 branch, new state |

No backend changes needed.

## Out of Scope

- Publishing `@timeplus/agentguard-claudecode-plugin` to npm (assumed available)
- `make install` / automated injection flow (Option A, not chosen)
- Any changes to step 3 (Activate Rules)
