# Claude Code AgentShield Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a TypeScript plugin that streams Claude Code hook events to the AgentShield Timeplus backend for real-time security monitoring, mirroring the existing OpenClaw plugin structure.

**Architecture:** Each Claude Code hook invocation spawns `node dist/index.js` as a short-lived process. It reads the Claude Code event JSON from stdin, maps it to the shared `HookEvent` schema, POSTs a single row to the Timeplus REST ingest API, and exits 0. All hooks are configured `async: true` so they never block Claude's operation.

**Tech Stack:** TypeScript (ESNext/ESM), Node.js ≥18, Timeplus Proton REST ingest API, vitest for tests.

---

## File Map

| File | Responsibility |
|---|---|
| `agents/claudecode/agentshield-plugin/package.json` | Package metadata, build/test scripts, no runtime dependencies |
| `agents/claudecode/agentshield-plugin/tsconfig.json` | TypeScript config — mirrors OpenClaw plugin exactly |
| `agents/claudecode/agentshield-plugin/src/config.ts` | Resolve `AgentShieldConfig` from env vars; no `pluginConfig` (no SDK) |
| `agents/claudecode/agentshield-plugin/src/timeplus-client.ts` | `HookEvent` type + `send(event)` — single POST, no queue |
| `agents/claudecode/agentshield-plugin/src/normalize.ts` | Map Claude Code stdin JSON → `HookEvent` |
| `agents/claudecode/agentshield-plugin/src/index.ts` | Entry point: read stdin → normalize → send → exit |
| `agents/claudecode/agentshield-plugin/src/normalize.test.ts` | Unit tests for normalize |
| `agents/claudecode/agentshield-plugin/src/config.test.ts` | Unit tests for config resolution |
| `agents/claudecode/agentshield-plugin/hooks.json` | Ready-to-paste Claude Code settings hooks config |

---

### Task 1: Package scaffold

**Files:**
- Create: `agents/claudecode/agentshield-plugin/package.json`
- Create: `agents/claudecode/agentshield-plugin/tsconfig.json`

- [ ] **Step 1: Create `package.json`**

```json
{
  "name": "@timeplus/agentshield-claudecode-plugin",
  "version": "0.1.0",
  "description": "AgentShield security monitoring hook for Claude Code",
  "type": "module",
  "main": "./dist/index.js",
  "engines": { "node": ">=18.0.0" },
  "scripts": {
    "build": "tsc",
    "dev": "tsc --watch",
    "test": "vitest run",
    "test:watch": "vitest"
  },
  "devDependencies": {
    "typescript": "^5.4.0",
    "vitest": "^2.0.0"
  }
}
```

- [ ] **Step 2: Create `tsconfig.json`**

```json
{
  "compilerOptions": {
    "target": "ESNext",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "outDir": "./dist",
    "rootDir": "./src",
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true,
    "strict": true,
    "verbatimModuleSyntax": true,
    "skipLibCheck": true
  },
  "include": ["src/**/*.ts"],
  "exclude": ["src/**/*.test.ts", "node_modules", "dist"]
}
```

- [ ] **Step 3: Install dev dependencies**

```bash
cd agents/claudecode/agentshield-plugin
npm install
```

Expected: `node_modules` directory created with typescript and vitest.

- [ ] **Step 4: Commit**

```bash
git add agents/claudecode/agentshield-plugin/package.json agents/claudecode/agentshield-plugin/package-lock.json agents/claudecode/agentshield-plugin/tsconfig.json
git commit -m "feat(claudecode-plugin): add package scaffold"
```

---

### Task 2: Config module

**Files:**
- Create: `agents/claudecode/agentshield-plugin/src/config.ts`
- Create: `agents/claudecode/agentshield-plugin/src/config.test.ts`

- [ ] **Step 1: Write the failing tests**

Create `agents/claudecode/agentshield-plugin/src/config.test.ts`:

```typescript
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { resolveConfig } from "./config.js";

describe("resolveConfig", () => {
  const ORIGINAL_ENV = { ...process.env };

  beforeEach(() => {
    // Clear all AGENTSHIELD_ env vars before each test
    for (const key of Object.keys(process.env)) {
      if (key.startsWith("AGENTSHIELD_")) delete process.env[key];
    }
  });

  afterEach(() => {
    // Restore original env
    for (const key of Object.keys(process.env)) {
      if (key.startsWith("AGENTSHIELD_")) delete process.env[key];
    }
    Object.assign(process.env, ORIGINAL_ENV);
  });

  it("returns defaults when no env vars are set", () => {
    const cfg = resolveConfig();
    expect(cfg.timeplusUrl).toBe("http://localhost:3218");
    expect(cfg.stream).toBe("agentshield_hook_events");
    expect(cfg.username).toBe("proton");
    expect(cfg.password).toBe("");
    expect(cfg.deploymentId).toBe("default");
    expect(cfg.deploymentName).toBe("Default");
    expect(cfg.agentId).toBe("");
  });

  it("reads AGENTSHIELD_TIMEPLUS_URL from env", () => {
    process.env.AGENTSHIELD_TIMEPLUS_URL = "http://remote:3218";
    expect(resolveConfig().timeplusUrl).toBe("http://remote:3218");
  });

  it("reads AGENTSHIELD_AGENT_ID from env", () => {
    process.env.AGENTSHIELD_AGENT_ID = "my-laptop";
    expect(resolveConfig().agentId).toBe("my-laptop");
  });

  it("reads AGENTSHIELD_DEPLOYMENT_ID from env", () => {
    process.env.AGENTSHIELD_DEPLOYMENT_ID = "prod";
    expect(resolveConfig().deploymentId).toBe("prod");
  });

  it("reads AGENTSHIELD_DEPLOYMENT_NAME from env", () => {
    process.env.AGENTSHIELD_DEPLOYMENT_NAME = "Production";
    expect(resolveConfig().deploymentName).toBe("Production");
  });

  it("reads AGENTSHIELD_USERNAME and AGENTSHIELD_PASSWORD from env", () => {
    process.env.AGENTSHIELD_USERNAME = "admin";
    process.env.AGENTSHIELD_PASSWORD = "secret";
    const cfg = resolveConfig();
    expect(cfg.username).toBe("admin");
    expect(cfg.password).toBe("secret");
  });

  it("reads AGENTSHIELD_STREAM from env", () => {
    process.env.AGENTSHIELD_STREAM = "custom_stream";
    expect(resolveConfig().stream).toBe("custom_stream");
  });
});
```

- [ ] **Step 2: Run tests — expect failure**

```bash
cd agents/claudecode/agentshield-plugin
npm test
```

Expected: FAIL — `Cannot find module './config.js'`

- [ ] **Step 3: Implement `config.ts`**

Create `agents/claudecode/agentshield-plugin/src/config.ts`:

```typescript
export type AgentShieldConfig = {
  timeplusUrl: string;
  stream: string;
  username: string;
  password: string;
  agentId: string;
  deploymentId: string;
  deploymentName: string;
};

const DEFAULTS: AgentShieldConfig = {
  timeplusUrl: "http://localhost:3218",
  stream: "agentshield_hook_events",
  username: "proton",
  password: "",
  agentId: "",
  deploymentId: "default",
  deploymentName: "Default",
};

export function resolveConfig(): AgentShieldConfig {
  return {
    timeplusUrl: process.env.AGENTSHIELD_TIMEPLUS_URL || DEFAULTS.timeplusUrl,
    stream: process.env.AGENTSHIELD_STREAM || DEFAULTS.stream,
    username: process.env.AGENTSHIELD_USERNAME || DEFAULTS.username,
    password: process.env.AGENTSHIELD_PASSWORD ?? DEFAULTS.password,
    agentId: process.env.AGENTSHIELD_AGENT_ID ?? DEFAULTS.agentId,
    deploymentId: process.env.AGENTSHIELD_DEPLOYMENT_ID || DEFAULTS.deploymentId,
    deploymentName: process.env.AGENTSHIELD_DEPLOYMENT_NAME || DEFAULTS.deploymentName,
  };
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
cd agents/claudecode/agentshield-plugin
npm test
```

Expected: all 7 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add agents/claudecode/agentshield-plugin/src/config.ts agents/claudecode/agentshield-plugin/src/config.test.ts
git commit -m "feat(claudecode-plugin): add config module"
```

---

### Task 3: Timeplus client module

**Files:**
- Create: `agents/claudecode/agentshield-plugin/src/timeplus-client.ts`

No separate test file — the client makes a real HTTP call; it is exercised via integration in Task 6. The `HookEvent` type and `COLUMNS` constant are tested indirectly via normalize tests.

- [ ] **Step 1: Create `timeplus-client.ts`**

```typescript
import type { AgentShieldConfig } from "./config.js";

export type HookEvent = {
  hook_name: string;
  event_time: string;
  session_id: string;
  run_id: string;
  agent_id: string;
  deployment_id: string;
  deployment_name: string;
  session_key: string;
  tool_name: string;
  provider: string;
  model: string;
  hook_decision: string;
  block_reason: string;
  event_data: string;
  agent_type: string;
};

const COLUMNS: (keyof HookEvent)[] = [
  "hook_name", "event_time", "session_id", "run_id", "agent_id",
  "deployment_id", "deployment_name", "session_key", "tool_name",
  "provider", "model", "hook_decision", "block_reason", "event_data", "agent_type",
];

export async function send(event: HookEvent, config: AgentShieldConfig): Promise<void> {
  const url = `${config.timeplusUrl}/proton/v1/ingest/streams/${config.stream}`;
  const authHeader = `Basic ${btoa(`${config.username}:${config.password}`)}`;
  const body = JSON.stringify({
    columns: COLUMNS,
    data: [COLUMNS.map((col) => event[col])],
  });
  try {
    const resp = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": authHeader,
      },
      body,
    });
    if (!resp.ok) {
      process.stderr.write(`[agentshield] ingest failed: ${resp.status}\n`);
    }
  } catch (err) {
    process.stderr.write(`[agentshield] network error: ${String(err)}\n`);
  }
}
```

- [ ] **Step 2: Compile check**

```bash
cd agents/claudecode/agentshield-plugin
npm run build
```

Expected: `dist/timeplus-client.js` and `dist/timeplus-client.d.ts` created. No errors.

- [ ] **Step 3: Commit**

```bash
git add agents/claudecode/agentshield-plugin/src/timeplus-client.ts
git commit -m "feat(claudecode-plugin): add timeplus client"
```

---

### Task 4: Normalize module

**Files:**
- Create: `agents/claudecode/agentshield-plugin/src/normalize.ts`
- Create: `agents/claudecode/agentshield-plugin/src/normalize.test.ts`

The normalize module maps raw Claude Code hook stdin JSON to the `HookEvent` schema. It is the most logic-heavy file — all field mapping lives here.

- [ ] **Step 1: Write failing tests**

Create `agents/claudecode/agentshield-plugin/src/normalize.test.ts`:

```typescript
import { describe, it, expect } from "vitest";
import { normalize } from "./normalize.js";

const BASE_CONFIG = {
  timeplusUrl: "http://localhost:3218",
  stream: "agentshield_hook_events",
  username: "proton",
  password: "",
  agentId: "test-agent",
  deploymentId: "test-deploy",
  deploymentName: "Test",
};

describe("normalize", () => {
  it("sets agent_type to 'claudecode'", () => {
    const e = normalize({ hook_event_name: "Stop", session_id: "s1" }, BASE_CONFIG);
    expect(e.agent_type).toBe("claudecode");
  });

  it("sets hook_decision to 'observe' always", () => {
    const e = normalize({ hook_event_name: "PreToolUse", session_id: "s1" }, BASE_CONFIG);
    expect(e.hook_decision).toBe("observe");
    expect(e.block_reason).toBe("");
  });

  it("maps session_id from input", () => {
    const e = normalize({ hook_event_name: "Stop", session_id: "sess-abc" }, BASE_CONFIG);
    expect(e.session_id).toBe("sess-abc");
  });

  it("populates agent_id and deployment fields from config", () => {
    const e = normalize({ hook_event_name: "Stop", session_id: "s1" }, BASE_CONFIG);
    expect(e.agent_id).toBe("test-agent");
    expect(e.deployment_id).toBe("test-deploy");
    expect(e.deployment_name).toBe("Test");
  });

  it("maps PreToolUse → hook_name 'before_tool_call' with tool_name and run_id", () => {
    const e = normalize({
      hook_event_name: "PreToolUse",
      session_id: "s1",
      tool_name: "Bash",
      tool_use_id: "tu-123",
      tool_input: { command: "ls" },
    }, BASE_CONFIG);
    expect(e.hook_name).toBe("before_tool_call");
    expect(e.tool_name).toBe("Bash");
    expect(e.run_id).toBe("tu-123");
  });

  it("maps PostToolUse → 'after_tool_call' with tool_name and run_id", () => {
    const e = normalize({
      hook_event_name: "PostToolUse",
      session_id: "s1",
      tool_name: "Write",
      tool_use_id: "tu-456",
    }, BASE_CONFIG);
    expect(e.hook_name).toBe("after_tool_call");
    expect(e.tool_name).toBe("Write");
    expect(e.run_id).toBe("tu-456");
  });

  it("maps PostToolUseFailure → 'tool_call_failure'", () => {
    const e = normalize({
      hook_event_name: "PostToolUseFailure",
      session_id: "s1",
      tool_name: "Bash",
      tool_use_id: "tu-789",
    }, BASE_CONFIG);
    expect(e.hook_name).toBe("tool_call_failure");
  });

  it("maps SessionStart → 'session_start' with model", () => {
    const e = normalize({
      hook_event_name: "SessionStart",
      session_id: "s1",
      model: "claude-opus-4-5",
    }, BASE_CONFIG);
    expect(e.hook_name).toBe("session_start");
    expect(e.model).toBe("claude-opus-4-5");
  });

  it("maps SessionEnd → 'session_end'", () => {
    const e = normalize({ hook_event_name: "SessionEnd", session_id: "s1" }, BASE_CONFIG);
    expect(e.hook_name).toBe("session_end");
  });

  it("maps UserPromptSubmit → 'user_prompt_submit'", () => {
    const e = normalize({ hook_event_name: "UserPromptSubmit", session_id: "s1", prompt: "hello" }, BASE_CONFIG);
    expect(e.hook_name).toBe("user_prompt_submit");
  });

  it("maps SubagentStart → 'subagent_spawning'", () => {
    const e = normalize({ hook_event_name: "SubagentStart", session_id: "s1" }, BASE_CONFIG);
    expect(e.hook_name).toBe("subagent_spawning");
  });

  it("maps SubagentStop → 'subagent_ended'", () => {
    const e = normalize({ hook_event_name: "SubagentStop", session_id: "s1" }, BASE_CONFIG);
    expect(e.hook_name).toBe("subagent_ended");
  });

  it("maps Stop → 'llm_output'", () => {
    const e = normalize({ hook_event_name: "Stop", session_id: "s1" }, BASE_CONFIG);
    expect(e.hook_name).toBe("llm_output");
  });

  it("maps PermissionDenied → 'permission_denied'", () => {
    const e = normalize({ hook_event_name: "PermissionDenied", session_id: "s1" }, BASE_CONFIG);
    expect(e.hook_name).toBe("permission_denied");
  });

  it("falls back to lowercased hook_event_name for unknown events", () => {
    const e = normalize({ hook_event_name: "SomeNewEvent", session_id: "s1" }, BASE_CONFIG);
    expect(e.hook_name).toBe("somenewevent");
  });

  it("stores full input JSON in event_data", () => {
    const input = { hook_event_name: "Stop", session_id: "s1", some_field: 42 };
    const e = normalize(input, BASE_CONFIG);
    expect(JSON.parse(e.event_data)).toEqual(input);
  });

  it("defaults run_id to empty string for non-tool events", () => {
    const e = normalize({ hook_event_name: "Stop", session_id: "s1" }, BASE_CONFIG);
    expect(e.run_id).toBe("");
  });

  it("defaults model to empty string for non-SessionStart events", () => {
    const e = normalize({ hook_event_name: "PreToolUse", session_id: "s1", tool_name: "Bash" }, BASE_CONFIG);
    expect(e.model).toBe("");
  });

  it("defaults session_key to empty string always", () => {
    const e = normalize({ hook_event_name: "Stop", session_id: "s1" }, BASE_CONFIG);
    expect(e.session_key).toBe("");
  });

  it("defaults provider to empty string always", () => {
    const e = normalize({ hook_event_name: "Stop", session_id: "s1" }, BASE_CONFIG);
    expect(e.provider).toBe("");
  });

  it("event_time is an ISO 8601 timestamp", () => {
    const e = normalize({ hook_event_name: "Stop", session_id: "s1" }, BASE_CONFIG);
    expect(e.event_time).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
  });
});
```

- [ ] **Step 2: Run tests — expect failure**

```bash
cd agents/claudecode/agentshield-plugin
npm test
```

Expected: FAIL — `Cannot find module './normalize.js'`

- [ ] **Step 3: Implement `normalize.ts`**

Create `agents/claudecode/agentshield-plugin/src/normalize.ts`:

```typescript
import type { AgentShieldConfig } from "./config.js";
import type { HookEvent } from "./timeplus-client.js";

// Maps Claude Code hook_event_name values to the hook_name values
// used in the agentshield_hook_events stream (matches OpenClaw conventions).
const HOOK_NAME_MAP: Record<string, string> = {
  SessionStart:        "session_start",
  SessionEnd:          "session_end",
  UserPromptSubmit:    "user_prompt_submit",
  PreToolUse:          "before_tool_call",
  PostToolUse:         "after_tool_call",
  PostToolUseFailure:  "tool_call_failure",
  SubagentStart:       "subagent_spawning",
  SubagentStop:        "subagent_ended",
  Stop:                "llm_output",
  PermissionDenied:    "permission_denied",
};

// Minimal structural type for the Claude Code hook stdin payload.
// Only fields we actually read are listed; everything else is captured in event_data.
type ClaudeCodeInput = {
  hook_event_name?: string;
  session_id?: string;
  tool_name?: string;
  tool_use_id?: string;
  model?: string;
  [key: string]: unknown;
};

export function normalize(input: ClaudeCodeInput, config: AgentShieldConfig): HookEvent {
  const hookEventName = input.hook_event_name ?? "";
  const hookName = HOOK_NAME_MAP[hookEventName] ?? hookEventName.toLowerCase();

  // tool_use_id serves as run_id for tool events — it uniquely identifies
  // a PreToolUse/PostToolUse pair within a session.
  const isToolEvent = hookEventName === "PreToolUse"
    || hookEventName === "PostToolUse"
    || hookEventName === "PostToolUseFailure";

  // model is only present on SessionStart
  const model = hookEventName === "SessionStart"
    ? (typeof input.model === "string" ? input.model : "")
    : "";

  return {
    hook_name:       hookName,
    event_time:      new Date().toISOString(),
    session_id:      typeof input.session_id === "string" ? input.session_id : "",
    run_id:          isToolEvent && typeof input.tool_use_id === "string" ? input.tool_use_id : "",
    agent_id:        config.agentId,
    deployment_id:   config.deploymentId,
    deployment_name: config.deploymentName,
    session_key:     "",
    tool_name:       isToolEvent && typeof input.tool_name === "string" ? input.tool_name : "",
    provider:        "",
    model,
    hook_decision:   "observe",
    block_reason:    "",
    event_data:      JSON.stringify(input),
    agent_type:      "claudecode",
  };
}
```

- [ ] **Step 4: Run tests — expect pass**

```bash
cd agents/claudecode/agentshield-plugin
npm test
```

Expected: all 20 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add agents/claudecode/agentshield-plugin/src/normalize.ts agents/claudecode/agentshield-plugin/src/normalize.test.ts
git commit -m "feat(claudecode-plugin): add normalize module"
```

---

### Task 5: Entry point

**Files:**
- Create: `agents/claudecode/agentshield-plugin/src/index.ts`

The entry point reads all of stdin, parses JSON, normalizes, sends to Timeplus, and exits. It must handle parse errors gracefully — a bad payload should not crash (exit non-zero would show as an error in Claude Code's debug log).

- [ ] **Step 1: Create `index.ts`**

```typescript
import { resolveConfig } from "./config.js";
import { send } from "./timeplus-client.js";
import { normalize } from "./normalize.js";

async function main(): Promise<void> {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(chunk as Buffer);
  }
  const raw = Buffer.concat(chunks).toString("utf8").trim();
  if (!raw) {
    // Empty stdin — nothing to do. Happens if Claude Code fires the hook
    // but provides no payload (should not occur in practice).
    return;
  }

  let input: Record<string, unknown>;
  try {
    input = JSON.parse(raw) as Record<string, unknown>;
  } catch {
    process.stderr.write(`[agentshield] failed to parse hook input: ${raw.slice(0, 200)}\n`);
    return; // exit 0 — don't block Claude Code on our parse error
  }

  const config = resolveConfig();
  const event = normalize(input, config);
  await send(event, config);
}

main().catch((err) => {
  process.stderr.write(`[agentshield] unexpected error: ${String(err)}\n`);
  // Still exit 0 — observe-only hooks must never block Claude Code
});
```

- [ ] **Step 2: Build and verify**

```bash
cd agents/claudecode/agentshield-plugin
npm run build
ls dist/
```

Expected: `index.js`, `index.d.ts`, `config.js`, `config.d.ts`, `timeplus-client.js`, `timeplus-client.d.ts`, `normalize.js`, `normalize.d.ts` all present.

- [ ] **Step 3: Smoke test with a real hook payload**

```bash
cd agents/claudecode/agentshield-plugin
echo '{"hook_event_name":"PreToolUse","session_id":"test-sess","tool_name":"Bash","tool_use_id":"tu-001","tool_input":{"command":"ls"}}' | node dist/index.js
```

Expected: exits 0. If Timeplus is not running, a stderr line `[agentshield] network error: ...` is printed but the process still exits 0.

- [ ] **Step 4: Commit**

```bash
git add agents/claudecode/agentshield-plugin/src/index.ts
git commit -m "feat(claudecode-plugin): add entry point"
```

---

### Task 6: hooks.json — Claude Code settings config

**Files:**
- Create: `agents/claudecode/agentshield-plugin/hooks.json`

This file contains exactly what users paste into `~/.claude/settings.json` (or merge into the `hooks` key if settings already exist). The command uses an absolute path via `$CLAUDE_PLUGIN_ROOT` so it works regardless of working directory. `async: true` on every hook means Claude Code never waits for the script to complete.

- [ ] **Step 1: Create `hooks.json`**

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node \"$CLAUDE_PLUGIN_ROOT/dist/index.js\"",
            "async": true
          }
        ]
      }
    ],
    "SessionEnd": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node \"$CLAUDE_PLUGIN_ROOT/dist/index.js\"",
            "async": true
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node \"$CLAUDE_PLUGIN_ROOT/dist/index.js\"",
            "async": true
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node \"$CLAUDE_PLUGIN_ROOT/dist/index.js\"",
            "async": true
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node \"$CLAUDE_PLUGIN_ROOT/dist/index.js\"",
            "async": true
          }
        ]
      }
    ],
    "PostToolUseFailure": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node \"$CLAUDE_PLUGIN_ROOT/dist/index.js\"",
            "async": true
          }
        ]
      }
    ],
    "SubagentStart": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node \"$CLAUDE_PLUGIN_ROOT/dist/index.js\"",
            "async": true
          }
        ]
      }
    ],
    "SubagentStop": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node \"$CLAUDE_PLUGIN_ROOT/dist/index.js\"",
            "async": true
          }
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node \"$CLAUDE_PLUGIN_ROOT/dist/index.js\"",
            "async": true
          }
        ]
      }
    ],
    "PermissionDenied": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node \"$CLAUDE_PLUGIN_ROOT/dist/index.js\"",
            "async": true
          }
        ]
      }
    ]
  }
}
```

- [ ] **Step 2: Commit**

```bash
git add agents/claudecode/agentshield-plugin/hooks.json
git commit -m "feat(claudecode-plugin): add hooks.json settings config"
```

---

### Task 7: End-to-end smoke test with live Timeplus

This task verifies the full pipeline against a real Timeplus instance. Skip if Timeplus is not available in your environment — the unit tests in Tasks 2 and 4 already cover all logic.

- [ ] **Step 1: Ensure Timeplus is running**

```bash
curl -s http://localhost:3218/proton/v1/info | python3 -m json.tool
```

Expected: JSON with `{"version": "..."}`. If this fails, skip this task.

- [ ] **Step 2: Set required env vars**

```bash
export AGENTSHIELD_AGENT_ID="smoke-test-agent"
export AGENTSHIELD_DEPLOYMENT_ID="smoke-test"
export AGENTSHIELD_DEPLOYMENT_NAME="Smoke Test"
```

- [ ] **Step 3: Send a PreToolUse event**

```bash
cd agents/claudecode/agentshield-plugin
echo '{"hook_event_name":"PreToolUse","session_id":"smoke-sess-001","tool_name":"Bash","tool_use_id":"tu-smoke-001","tool_input":{"command":"echo hello"}}' | node dist/index.js
```

Expected: exits 0, no stderr.

- [ ] **Step 4: Verify the event in Timeplus**

```bash
curl -s -X POST http://localhost:3218/proton/v1/query \
  -H 'Content-Type: application/json' \
  -d '{"query":"SELECT hook_name, agent_id, agent_type, session_id, tool_name FROM table(agentshield_hook_events) WHERE agent_id='"'"'smoke-test-agent'"'"' ORDER BY event_time DESC LIMIT 1"}' \
  | python3 -m json.tool
```

Expected: one row with `hook_name=before_tool_call`, `agent_type=claudecode`, `tool_name=Bash`.

- [ ] **Step 5: Send a SessionStart event**

```bash
echo '{"hook_event_name":"SessionStart","session_id":"smoke-sess-001","model":"claude-opus-4-5","source":"startup"}' | node dist/index.js
```

Expected: exits 0.

- [ ] **Step 6: Commit**

```bash
git add agents/claudecode/agentshield-plugin/
git commit -m "feat(claudecode-plugin): complete Claude Code AgentShield hook plugin"
```
