# AgentShield OpenClaw Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an OpenClaw plugin that intercepts all 29 lifecycle hooks and streams event data to a Timeplus stream (`agentshield_hook_events`) via the Proton Ingest REST API.

**Architecture:** A TypeScript npm package (`@agentshield/openclaw-plugin`) under `agents/openclaw/agentshield-plugin/`. On `activate`, it creates a batched ingest client and registers handlers for all 29 OpenClaw plugin hooks. Each handler enqueues a `HookEvent` with common top-level columns and a full-event JSON blob; a background timer flushes batches to Timeplus every 2s (configurable). Config is resolved from `openclaw.json` plugin config with env var overrides.

**Tech Stack:** TypeScript (ESNext modules), `openclaw` npm package (peer dep for types), `vitest` for unit tests, no runtime dependencies beyond Node.js built-in `fetch`.

---

## File Map

| File | Action | Purpose |
|---|---|---|
| `agents/openclaw/agentshield-plugin/package.json` | Create | npm package definition, scripts, peer deps |
| `agents/openclaw/agentshield-plugin/tsconfig.json` | Create | TypeScript config for ESNext module output |
| `agents/openclaw/agentshield-plugin/vitest.config.ts` | Create | Vitest config |
| `agents/openclaw/agentshield-plugin/src/config.ts` | Create | `AgentShieldConfig` type + `resolveConfig()` |
| `agents/openclaw/agentshield-plugin/src/timeplus-client.ts` | Create | `HookEvent` type, `TimeplusClient`, `createTimeplusClient()` |
| `agents/openclaw/agentshield-plugin/src/hooks.ts` | Create | `baseEvent()` + `registerAllHooks()` for all 29 hooks |
| `agents/openclaw/agentshield-plugin/src/index.ts` | Create | `OpenClawPluginDefinition` default export |
| `agents/openclaw/agentshield-plugin/src/config.test.ts` | Create | Unit tests for `resolveConfig()` |
| `agents/openclaw/agentshield-plugin/src/timeplus-client.test.ts` | Create | Unit tests for `TimeplusClient` |
| `agents/openclaw/agentshield-plugin/src/hooks.test.ts` | Create | Unit tests for `baseEvent()` |
| `agents/openclaw/.openclaw/openclaw.json` | Modify | Add agentshield plugin entry with local path |
| `backend/internal/timeplus/init.go` | Modify | Add `agentshield_hook_events` stream DDL + creation |

---

## Task 1: Scaffold the plugin package

**Files:**
- Create: `agents/openclaw/agentshield-plugin/package.json`
- Create: `agents/openclaw/agentshield-plugin/tsconfig.json`
- Create: `agents/openclaw/agentshield-plugin/vitest.config.ts`
- Create: `agents/openclaw/agentshield-plugin/.gitignore`

- [ ] **Step 1: Create the directory**

```bash
mkdir -p agents/openclaw/agentshield-plugin/src
```

- [ ] **Step 2: Write package.json**

```json
{
  "name": "@agentshield/openclaw-plugin",
  "version": "0.1.0",
  "description": "AgentShield security monitoring plugin for OpenClaw",
  "type": "module",
  "main": "./dist/index.js",
  "exports": {
    ".": {
      "types": "./dist/index.d.ts",
      "default": "./dist/index.js"
    }
  },
  "scripts": {
    "build": "tsc",
    "dev": "tsc --watch",
    "test": "vitest run",
    "test:watch": "vitest"
  },
  "peerDependencies": {
    "openclaw": ">=2026.4.0"
  },
  "devDependencies": {
    "openclaw": "^2026.4.5",
    "typescript": "^5.4.0",
    "vitest": "^2.0.0"
  }
}
```

Save to `agents/openclaw/agentshield-plugin/package.json`.

- [ ] **Step 3: Write tsconfig.json**

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

Save to `agents/openclaw/agentshield-plugin/tsconfig.json`.

- [ ] **Step 4: Write vitest.config.ts**

```ts
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
  },
});
```

Save to `agents/openclaw/agentshield-plugin/vitest.config.ts`.

- [ ] **Step 5: Write .gitignore**

```
dist/
node_modules/
*.js.map
```

Save to `agents/openclaw/agentshield-plugin/.gitignore`.

- [ ] **Step 6: Install dependencies**

```bash
cd agents/openclaw/agentshield-plugin
npm install
```

Expected: `node_modules/` created, `package-lock.json` generated. `openclaw` and `vitest` packages installed.

- [ ] **Step 7: Commit**

```bash
git add agents/openclaw/agentshield-plugin/package.json \
        agents/openclaw/agentshield-plugin/package-lock.json \
        agents/openclaw/agentshield-plugin/tsconfig.json \
        agents/openclaw/agentshield-plugin/vitest.config.ts \
        agents/openclaw/agentshield-plugin/.gitignore
git commit -m "feat: scaffold agentshield openclaw plugin package"
```

---

## Task 2: Implement config.ts

**Files:**
- Create: `agents/openclaw/agentshield-plugin/src/config.ts`
- Create: `agents/openclaw/agentshield-plugin/src/config.test.ts`

- [ ] **Step 1: Write the failing tests**

Save to `agents/openclaw/agentshield-plugin/src/config.test.ts`:

```ts
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { resolveConfig } from "./config.js";

describe("resolveConfig", () => {
  const ENV_KEYS = [
    "AGENTSHIELD_TIMEPLUS_URL",
    "AGENTSHIELD_STREAM",
    "AGENTSHIELD_FLUSH_MS",
    "AGENTSHIELD_BATCH_SIZE",
  ];

  beforeEach(() => {
    ENV_KEYS.forEach((k) => delete process.env[k]);
  });

  afterEach(() => {
    ENV_KEYS.forEach((k) => delete process.env[k]);
  });

  it("returns all defaults when called with no arguments", () => {
    const cfg = resolveConfig();
    expect(cfg.timeplusUrl).toBe("http://localhost:3218");
    expect(cfg.stream).toBe("agentshield_hook_events");
    expect(cfg.flushMs).toBe(2000);
    expect(cfg.batchSize).toBe(50);
  });

  it("applies pluginConfig values over defaults", () => {
    const cfg = resolveConfig({
      timeplusUrl: "http://timeplus:3218",
      stream: "my_stream",
      flushMs: 5000,
      batchSize: 100,
    });
    expect(cfg.timeplusUrl).toBe("http://timeplus:3218");
    expect(cfg.stream).toBe("my_stream");
    expect(cfg.flushMs).toBe(5000);
    expect(cfg.batchSize).toBe(100);
  });

  it("env vars override pluginConfig", () => {
    process.env.AGENTSHIELD_TIMEPLUS_URL = "http://env-host:3218";
    process.env.AGENTSHIELD_STREAM = "env_stream";
    process.env.AGENTSHIELD_FLUSH_MS = "1000";
    process.env.AGENTSHIELD_BATCH_SIZE = "10";
    const cfg = resolveConfig({
      timeplusUrl: "http://plugin-host:3218",
      stream: "plugin_stream",
      flushMs: 9999,
      batchSize: 999,
    });
    expect(cfg.timeplusUrl).toBe("http://env-host:3218");
    expect(cfg.stream).toBe("env_stream");
    expect(cfg.flushMs).toBe(1000);
    expect(cfg.batchSize).toBe(10);
  });

  it("flushMs and batchSize are numbers, not strings", () => {
    process.env.AGENTSHIELD_FLUSH_MS = "3000";
    process.env.AGENTSHIELD_BATCH_SIZE = "25";
    const cfg = resolveConfig();
    expect(typeof cfg.flushMs).toBe("number");
    expect(typeof cfg.batchSize).toBe("number");
    expect(cfg.flushMs).toBe(3000);
    expect(cfg.batchSize).toBe(25);
  });
});
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd agents/openclaw/agentshield-plugin
npm test
```

Expected: FAIL — `Cannot find module './config.js'`

- [ ] **Step 3: Write config.ts**

Save to `agents/openclaw/agentshield-plugin/src/config.ts`:

```ts
export type AgentShieldConfig = {
  timeplusUrl: string;
  stream: string;
  flushMs: number;
  batchSize: number;
};

const DEFAULTS: AgentShieldConfig = {
  timeplusUrl: "http://localhost:3218",
  stream: "agentshield_hook_events",
  flushMs: 2000,
  batchSize: 50,
};

export function resolveConfig(
  pluginConfig?: Record<string, unknown>,
): AgentShieldConfig {
  return {
    timeplusUrl:
      process.env.AGENTSHIELD_TIMEPLUS_URL ??
      (pluginConfig?.timeplusUrl as string) ??
      DEFAULTS.timeplusUrl,
    stream:
      process.env.AGENTSHIELD_STREAM ??
      (pluginConfig?.stream as string) ??
      DEFAULTS.stream,
    flushMs: Number(
      process.env.AGENTSHIELD_FLUSH_MS ??
        pluginConfig?.flushMs ??
        DEFAULTS.flushMs,
    ),
    batchSize: Number(
      process.env.AGENTSHIELD_BATCH_SIZE ??
        pluginConfig?.batchSize ??
        DEFAULTS.batchSize,
    ),
  };
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd agents/openclaw/agentshield-plugin
npm test
```

Expected: All 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add agents/openclaw/agentshield-plugin/src/config.ts \
        agents/openclaw/agentshield-plugin/src/config.test.ts
git commit -m "feat: add agentshield plugin config resolver with env var overrides"
```

---

## Task 3: Implement timeplus-client.ts

**Files:**
- Create: `agents/openclaw/agentshield-plugin/src/timeplus-client.ts`
- Create: `agents/openclaw/agentshield-plugin/src/timeplus-client.test.ts`

- [ ] **Step 1: Write the failing tests**

Save to `agents/openclaw/agentshield-plugin/src/timeplus-client.test.ts`:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createTimeplusClient } from "./timeplus-client.js";
import type { HookEvent } from "./timeplus-client.js";

const BASE_CONFIG = {
  timeplusUrl: "http://localhost:3218",
  stream: "agentshield_hook_events",
  flushMs: 60000, // long interval so timer doesn't auto-flush during tests
  batchSize: 50,
};

function makeEvent(hookName: string): HookEvent {
  return {
    hook_name: hookName,
    event_time: "2026-04-07T00:00:00.000Z",
    session_id: "sess-1",
    run_id: "run-1",
    agent_id: "main",
    session_key: "agent:main:main",
    tool_name: "",
    provider: "",
    model: "",
    hook_decision: "observe",
    block_reason: "",
    event_data: JSON.stringify({ test: true }),
  };
}

describe("TimeplusClient", () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal("fetch", fetchMock);
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("does not POST immediately on enqueue", () => {
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("session_start"));
    expect(fetchMock).not.toHaveBeenCalled();
    client.close();
  });

  it("POSTs on flush() with correct URL and payload shape", async () => {
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("before_tool_call"));
    await client.flush();

    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, opts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(
      "http://localhost:3218/proton/v1/ingest/streams/agentshield_hook_events",
    );
    expect(opts.method).toBe("POST");

    const body = JSON.parse(opts.body as string) as {
      columns: string[];
      data: string[][];
    };
    expect(body.columns).toEqual([
      "hook_name", "event_time", "session_id", "run_id", "agent_id",
      "session_key", "tool_name", "provider", "model",
      "hook_decision", "block_reason", "event_data",
    ]);
    expect(body.data).toHaveLength(1);
    expect(body.data[0][0]).toBe("before_tool_call");
    await client.close();
  });

  it("clears the queue after flush so events are not sent twice", async () => {
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("llm_input"));
    await client.flush();
    await client.flush(); // second flush should be a no-op
    expect(fetchMock).toHaveBeenCalledOnce();
    await client.close();
  });

  it("flushes immediately when batchSize is reached", async () => {
    const client = createTimeplusClient({ ...BASE_CONFIG, batchSize: 2 });
    client.enqueue(makeEvent("llm_input"));
    // not yet at batch size
    expect(fetchMock).not.toHaveBeenCalled();
    client.enqueue(makeEvent("llm_output"));
    // now at batch size — flush should have been triggered asynchronously
    await Promise.resolve(); // drain microtask queue
    expect(fetchMock).toHaveBeenCalledOnce();
    await client.close();
  });

  it("retries once on failure then drops the batch", async () => {
    fetchMock
      .mockResolvedValueOnce({ ok: false, status: 500 })
      .mockResolvedValueOnce({ ok: false, status: 500 });

    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("session_end"));
    await client.flush();

    // Should have tried twice (initial + 1 retry)
    expect(fetchMock).toHaveBeenCalledTimes(2);
    await client.close();
  });

  it("never throws even when fetch rejects completely", async () => {
    fetchMock.mockRejectedValue(new Error("network error"));
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("gateway_stop"));
    await expect(client.flush()).resolves.toBeUndefined();
    await client.close();
  });

  it("close() flushes remaining events", async () => {
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("gateway_stop"));
    await client.close();
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it("sends events batched in a single POST", async () => {
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("llm_input"));
    client.enqueue(makeEvent("llm_output"));
    client.enqueue(makeEvent("before_tool_call"));
    await client.flush();

    expect(fetchMock).toHaveBeenCalledOnce();
    const body = JSON.parse(
      (fetchMock.mock.calls[0] as [string, RequestInit])[1].body as string,
    ) as { data: string[][] };
    expect(body.data).toHaveLength(3);
    await client.close();
  });
});
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd agents/openclaw/agentshield-plugin
npm test
```

Expected: FAIL — `Cannot find module './timeplus-client.js'`

- [ ] **Step 3: Write timeplus-client.ts**

Save to `agents/openclaw/agentshield-plugin/src/timeplus-client.ts`:

```ts
import type { AgentShieldConfig } from "./config.js";

export type HookEvent = {
  hook_name: string;
  event_time: string;
  session_id: string;
  run_id: string;
  agent_id: string;
  session_key: string;
  tool_name: string;
  provider: string;
  model: string;
  hook_decision: string;
  block_reason: string;
  event_data: string;
};

export type TimeplusClient = {
  enqueue: (event: HookEvent) => void;
  flush: () => Promise<void>;
  close: () => Promise<void>;
};

const COLUMNS: (keyof HookEvent)[] = [
  "hook_name", "event_time", "session_id", "run_id", "agent_id",
  "session_key", "tool_name", "provider", "model",
  "hook_decision", "block_reason", "event_data",
];

async function postBatch(
  url: string,
  batch: HookEvent[],
): Promise<void> {
  const body = JSON.stringify({
    columns: COLUMNS,
    data: batch.map((e) => COLUMNS.map((col) => e[col])),
  });
  const resp = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body,
  });
  if (!resp.ok) {
    throw new Error(`Timeplus ingest failed: ${resp.status}`);
  }
}

export function createTimeplusClient(config: AgentShieldConfig): TimeplusClient {
  const url = `${config.timeplusUrl}/proton/v1/ingest/streams/${config.stream}`;
  const queue: HookEvent[] = [];
  let timer: ReturnType<typeof setInterval> | null = null;

  async function flush(): Promise<void> {
    if (queue.length === 0) return;
    const batch = queue.splice(0, queue.length);
    try {
      await postBatch(url, batch);
    } catch {
      // retry once after 1s
      await new Promise((r) => setTimeout(r, 1000));
      try {
        await postBatch(url, batch);
      } catch (err) {
        console.error("[agentshield] Failed to flush batch, dropping:", err);
      }
    }
  }

  timer = setInterval(() => {
    void flush();
  }, config.flushMs);

  function enqueue(event: HookEvent): void {
    queue.push(event);
    if (queue.length >= config.batchSize) {
      void flush();
    }
  }

  async function close(): Promise<void> {
    if (timer !== null) {
      clearInterval(timer);
      timer = null;
    }
    await flush();
  }

  return { enqueue, flush, close };
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd agents/openclaw/agentshield-plugin
npm test
```

Expected: All 8 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add agents/openclaw/agentshield-plugin/src/timeplus-client.ts \
        agents/openclaw/agentshield-plugin/src/timeplus-client.test.ts
git commit -m "feat: add TimeplusClient with async batch buffering and retry"
```

---

## Task 4: Implement hooks.ts

**Files:**
- Create: `agents/openclaw/agentshield-plugin/src/hooks.ts`
- Create: `agents/openclaw/agentshield-plugin/src/hooks.test.ts`

- [ ] **Step 1: Write the failing tests**

Save to `agents/openclaw/agentshield-plugin/src/hooks.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import { baseEvent } from "./hooks.js";

describe("baseEvent", () => {
  it("populates hook_name and event_time", () => {
    const e = baseEvent("llm_input", {});
    expect(e.hook_name).toBe("llm_input");
    expect(e.event_time).toMatch(/^\d{4}-\d{2}-\d{2}T/);
  });

  it("fills context fields from ctx when provided", () => {
    const e = baseEvent("before_tool_call", {
      agentId: "main",
      sessionKey: "agent:main:main",
      sessionId: "sess-abc",
    });
    expect(e.agent_id).toBe("main");
    expect(e.session_key).toBe("agent:main:main");
    expect(e.session_id).toBe("sess-abc");
  });

  it("defaults context fields to empty strings when ctx is empty", () => {
    const e = baseEvent("gateway_start", {});
    expect(e.agent_id).toBe("");
    expect(e.session_key).toBe("");
    expect(e.session_id).toBe("");
    expect(e.run_id).toBe("");
    expect(e.tool_name).toBe("");
    expect(e.provider).toBe("");
    expect(e.model).toBe("");
  });

  it("sets hook_decision to 'observe' and block_reason to empty", () => {
    const e = baseEvent("message_sent", {});
    expect(e.hook_decision).toBe("observe");
    expect(e.block_reason).toBe("");
  });

  it("sets event_data to empty string by default", () => {
    const e = baseEvent("session_end", {});
    expect(e.event_data).toBe("");
  });
});
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd agents/openclaw/agentshield-plugin
npm test
```

Expected: FAIL — `Cannot find module './hooks.js'`

- [ ] **Step 3: Write hooks.ts**

Save to `agents/openclaw/agentshield-plugin/src/hooks.ts`:

```ts
import type { OpenClawPluginApi } from "openclaw/plugin-sdk";
import type { HookEvent, TimeplusClient } from "./timeplus-client.js";

type BaseCtx = {
  agentId?: string;
  sessionKey?: string;
  sessionId?: string;
};

export function baseEvent(hookName: string, ctx: BaseCtx): HookEvent {
  return {
    hook_name: hookName,
    event_time: new Date().toISOString(),
    session_id: ctx.sessionId ?? "",
    run_id: "",
    agent_id: ctx.agentId ?? "",
    session_key: ctx.sessionKey ?? "",
    tool_name: "",
    provider: "",
    model: "",
    hook_decision: "observe",
    block_reason: "",
    event_data: "",
  };
}

export function registerAllHooks(
  api: OpenClawPluginApi,
  client: TimeplusClient,
): void {
  // ── Tier 1: Agent context hooks ─────────────────────────────────────────

  api.on("llm_input", (event, ctx) => {
    client.enqueue({
      ...baseEvent("llm_input", ctx),
      run_id: event.runId ?? "",
      session_id: event.sessionId ?? ctx.sessionId ?? "",
      provider: event.provider,
      model: event.model,
      event_data: JSON.stringify({
        systemPrompt: event.systemPrompt,
        prompt: event.prompt,
        imagesCount: event.imagesCount,
        historyMessages: event.historyMessages,
      }),
    });
  });

  api.on("llm_output", (event, ctx) => {
    client.enqueue({
      ...baseEvent("llm_output", ctx),
      run_id: event.runId ?? "",
      session_id: event.sessionId ?? ctx.sessionId ?? "",
      provider: event.provider,
      model: event.model,
      event_data: JSON.stringify({
        assistantTexts: event.assistantTexts,
        usage: event.usage,
        lastAssistant: event.lastAssistant,
      }),
    });
  });

  api.on("before_model_resolve", (event, ctx) => {
    client.enqueue({
      ...baseEvent("before_model_resolve", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
  });

  api.on("before_prompt_build", (event, ctx) => {
    client.enqueue({
      ...baseEvent("before_prompt_build", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
  });

  api.on("before_agent_start", (event, ctx) => {
    client.enqueue({
      ...baseEvent("before_agent_start", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
  });

  api.on("before_agent_reply", (event, ctx) => {
    client.enqueue({
      ...baseEvent("before_agent_reply", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
  });

  api.on("agent_end", (event, ctx) => {
    client.enqueue({
      ...baseEvent("agent_end", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
  });

  api.on("before_compaction", (event, ctx) => {
    client.enqueue({
      ...baseEvent("before_compaction", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
  });

  api.on("after_compaction", (event, ctx) => {
    client.enqueue({
      ...baseEvent("after_compaction", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
  });

  api.on("before_reset", (event, ctx) => {
    client.enqueue({
      ...baseEvent("before_reset", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
  });

  // ── Tier 2: Tool hooks ───────────────────────────────────────────────────

  api.on("before_tool_call", (event, ctx) => {
    client.enqueue({
      ...baseEvent("before_tool_call", ctx),
      run_id: event.runId ?? ctx.runId ?? "",
      tool_name: event.toolName,
      event_data: JSON.stringify({
        params: event.params,
        toolCallId: event.toolCallId,
      }),
    });
    // Phase 1: pass-through (return undefined)
  });

  api.on("after_tool_call", (event, ctx) => {
    client.enqueue({
      ...baseEvent("after_tool_call", ctx),
      run_id: event.runId ?? ctx.runId ?? "",
      tool_name: event.toolName,
      event_data: JSON.stringify({
        params: event.params,
        result: event.result,
        error: event.error,
        durationMs: event.durationMs,
        toolCallId: event.toolCallId,
      }),
    });
  });

  api.on("tool_result_persist", (event, ctx) => {
    client.enqueue({
      ...baseEvent("tool_result_persist", ctx),
      tool_name: event.toolName ?? "",
      event_data: JSON.stringify({
        toolCallId: event.toolCallId,
        isSynthetic: event.isSynthetic,
        message: event.message,
      }),
    });
    // Phase 1: pass-through (return undefined)
  });

  // ── Tier 3: Message, session, gateway hooks ──────────────────────────────

  api.on("session_start", (event, ctx) => {
    client.enqueue({
      ...baseEvent("session_start", ctx),
      session_id: event.sessionId,
      event_data: JSON.stringify(event),
    });
  });

  api.on("session_end", (event, ctx) => {
    client.enqueue({
      ...baseEvent("session_end", ctx),
      event_data: JSON.stringify(event),
    });
  });

  api.on("message_received", (event, ctx) => {
    client.enqueue({
      ...baseEvent("message_received", ctx),
      event_data: JSON.stringify(event),
    });
  });

  api.on("message_sending", (event, ctx) => {
    client.enqueue({
      ...baseEvent("message_sending", ctx),
      event_data: JSON.stringify(event),
    });
    // Phase 1: pass-through (return undefined)
  });

  api.on("message_sent", (event, ctx) => {
    client.enqueue({
      ...baseEvent("message_sent", ctx),
      event_data: JSON.stringify(event),
    });
  });

  api.on("before_message_write", (event, ctx) => {
    client.enqueue({
      ...baseEvent("before_message_write", ctx),
      event_data: JSON.stringify(event),
    });
    // Phase 1: pass-through (return undefined)
  });

  api.on("inbound_claim", (event, ctx) => {
    client.enqueue({
      ...baseEvent("inbound_claim", ctx),
      event_data: JSON.stringify(event),
    });
    // Phase 1: pass-through (return undefined)
  });

  api.on("before_dispatch", (event, ctx) => {
    client.enqueue({
      ...baseEvent("before_dispatch", ctx),
      event_data: JSON.stringify(event),
    });
    // Phase 1: pass-through (return undefined)
  });

  api.on("reply_dispatch", (event, ctx) => {
    client.enqueue({
      ...baseEvent("reply_dispatch", ctx),
      event_data: JSON.stringify(event),
    });
    // Phase 1: pass-through (return undefined)
  });

  api.on("subagent_spawning", (event, ctx) => {
    client.enqueue({
      ...baseEvent("subagent_spawning", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
    // Phase 1: pass-through (return undefined)
  });

  api.on("subagent_delivery_target", (event, ctx) => {
    client.enqueue({
      ...baseEvent("subagent_delivery_target", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
    // Phase 1: pass-through (return undefined)
  });

  api.on("subagent_spawned", (event, ctx) => {
    client.enqueue({
      ...baseEvent("subagent_spawned", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
  });

  api.on("subagent_ended", (event, ctx) => {
    client.enqueue({
      ...baseEvent("subagent_ended", ctx),
      run_id: ctx.runId ?? "",
      event_data: JSON.stringify(event),
    });
  });

  api.on("before_install", (event, ctx) => {
    client.enqueue({
      ...baseEvent("before_install", ctx),
      event_data: JSON.stringify(event),
    });
    // Phase 1: pass-through (return undefined)
  });

  api.on("gateway_start", (event, ctx) => {
    client.enqueue({
      ...baseEvent("gateway_start", ctx),
      event_data: JSON.stringify(event),
    });
  });

  api.on("gateway_stop", async (event, ctx) => {
    client.enqueue({
      ...baseEvent("gateway_stop", ctx),
      event_data: JSON.stringify(event),
    });
    await client.close();
  });
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd agents/openclaw/agentshield-plugin
npm test
```

Expected: All 5 tests PASS (config: 4, hooks: 5 — timeplus-client tests also pass: 8 total).

- [ ] **Step 5: Commit**

```bash
git add agents/openclaw/agentshield-plugin/src/hooks.ts \
        agents/openclaw/agentshield-plugin/src/hooks.test.ts
git commit -m "feat: implement registerAllHooks for all 29 OpenClaw plugin hooks"
```

---

## Task 5: Implement index.ts and build

**Files:**
- Create: `agents/openclaw/agentshield-plugin/src/index.ts`

- [ ] **Step 1: Write index.ts**

Save to `agents/openclaw/agentshield-plugin/src/index.ts`:

```ts
import type { OpenClawPluginDefinition } from "openclaw/plugin-sdk";
import { resolveConfig } from "./config.js";
import { createTimeplusClient } from "./timeplus-client.js";
import { registerAllHooks } from "./hooks.js";

const plugin: OpenClawPluginDefinition = {
  id: "agentshield",
  name: "AgentShield Security Monitor",
  version: "0.1.0",
  description:
    "Streams all OpenClaw lifecycle events to Timeplus for real-time security monitoring.",

  activate: async (api) => {
    const config = resolveConfig(api.pluginConfig);
    const client = createTimeplusClient(config);
    registerAllHooks(api, client);
    api.logger.info(
      `[agentshield] active — streaming to ${config.timeplusUrl}/proton/v1/ingest/streams/${config.stream} ` +
      `(flush every ${config.flushMs}ms, batch size ${config.batchSize})`,
    );
  },
};

export default plugin;
```

- [ ] **Step 2: Build the plugin**

```bash
cd agents/openclaw/agentshield-plugin
npm run build
```

Expected: `dist/` directory created with `index.js`, `index.d.ts`, and map files. No TypeScript errors.

If there are type errors from `openclaw/plugin-sdk`, verify the openclaw package is installed:
```bash
ls node_modules/openclaw
```
If missing: `npm install`

- [ ] **Step 3: Run all tests to confirm everything passes**

```bash
npm test
```

Expected: All 17 tests PASS (config: 4, timeplus-client: 8, hooks: 5).

- [ ] **Step 4: Commit**

```bash
git add agents/openclaw/agentshield-plugin/src/index.ts \
        agents/openclaw/agentshield-plugin/dist/
git commit -m "feat: add plugin entry point and compiled dist"
```

---

## Task 6: Extend backend to create agentshield_hook_events stream

**Files:**
- Modify: `backend/internal/timeplus/init.go`

- [ ] **Step 1: Add the DDL constant**

Open `backend/internal/timeplus/init.go`. After the `ddlOTelInput` constant (around line 274), add:

```go
const ddlAgentShieldHookEvents = `
CREATE STREAM agentshield_hook_events
(
  hook_name     low_cardinality(string),
  event_time    datetime64(3),
  session_id    string,
  run_id        string,
  agent_id      string,
  session_key   string,
  tool_name     string,
  provider      string,
  model         string,
  hook_decision low_cardinality(string),
  block_reason  string,
  event_data    string
)`
```

- [ ] **Step 2: Add creation call in Init()**

In `Init()`, after `createOTelInput`, add a call to create the hook events stream:

```go
func Init(cfg *config.TimeplusConfig) error {
	conn, err := waitForTimeplus(cfg)
	if err != nil {
		return fmt.Errorf("timeplus init: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()

	if err := createOTelStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating streams: %w", err)
	}
	if err := createOTelInput(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating input: %w", err)
	}
	if err := createAgentShieldStreams(ctx, conn); err != nil {
		return fmt.Errorf("timeplus init: creating agentshield streams: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Add createAgentShieldStreams()**

Add the function after `createOTelInput`:

```go
func createAgentShieldStreams(ctx context.Context, conn proton.Conn) error {
	return execIfNotExists(ctx, conn, "stream", "agentshield_hook_events", ddlAgentShieldHookEvents)
}
```

- [ ] **Step 4: Compile check**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: No output (clean build).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/timeplus/init.go
git commit -m "feat: auto-create agentshield_hook_events stream on backend startup"
```

---

## Task 7: Wire plugin into openclaw.json and test end-to-end

**Files:**
- Modify: `agents/openclaw/.openclaw/openclaw.json`

- [ ] **Step 1: Add the plugin entry to openclaw.json**

Open `agents/openclaw/.openclaw/openclaw.json`. In the `plugins.entries` object, add the agentshield entry. The `path` points to the compiled `dist/index.js` relative to the openclaw config dir. Since OpenClaw runs in Docker and the `.openclaw` directory is mounted at `/home/node/.openclaw`, the plugin directory needs to be accessible inside the container.

Add a volume mount for the plugin in the `start` Makefile target (see Step 2), then add to `openclaw.json`:

```json
"agentshield": {
  "enabled": true,
  "path": "/home/node/.openclaw/agentshield-plugin/dist/index.js",
  "config": {
    "timeplusUrl": "http://host.docker.internal:3218",
    "stream": "agentshield_hook_events",
    "flushMs": 2000,
    "batchSize": 50
  }
}
```

The full `plugins.entries` section becomes:

```json
"plugins": {
  "entries": {
    "ollama": { "enabled": true },
    "diagnostics-otel": { "enabled": true },
    "agentshield": {
      "enabled": true,
      "path": "/home/node/.openclaw/agentshield-plugin/dist/index.js",
      "config": {
        "timeplusUrl": "http://host.docker.internal:3218",
        "stream": "agentshield_hook_events",
        "flushMs": 2000,
        "batchSize": 50
      }
    }
  }
}
```

- [ ] **Step 2: Mount the plugin directory in the Makefile**

Open `agents/openclaw/Makefile`. In the `start` target, add a volume mount for the plugin:

```makefile
start:
	docker stop openclaw || true
	docker rm openclaw || true
	docker run -d \
		--name openclaw \
		-p 18789:18789 \
		-v "$(PWD)/.openclaw":/home/node/.openclaw \
		-v "$(PWD)/.openclaw/workspace":/home/node/.openclaw/workspace \
		-v "$(PWD)/agentshield-plugin":/home/node/.openclaw/agentshield-plugin \
		-e HOME=/home/node \
		--restart unless-stopped \
		$(IMAGE) \
		node dist/index.js gateway run
```

Also update the `run` target to match:

```makefile
run:
	docker run -d \
		--name openclaw \
		-p 18789:18789 \
		-v "$(PWD)/.openclaw":/home/node/.openclaw \
		-v "$(PWD)/.openclaw/workspace":/home/node/.openclaw/workspace \
		-v "$(PWD)/agentshield-plugin":/home/node/.openclaw/agentshield-plugin \
		--restart unless-stopped \
		$(IMAGE) \
		node dist/index.js gateway run
```

- [ ] **Step 3: Restart OpenClaw to load the plugin**

```bash
cd agents/openclaw
make start
```

Then watch the logs for the AgentShield activation message:

```bash
docker logs openclaw --follow 2>&1 | grep -i agentshield
```

Expected output (within ~5s):
```
[agentshield] active — streaming to http://host.docker.internal:3218/proton/v1/ingest/streams/agentshield_hook_events (flush every 2000ms, batch size 50)
```

- [ ] **Step 4: Verify the stream was created by the backend**

The AgentShield backend (running via docker-compose) should have already created the stream. Check in Timeplus:

```bash
# Query the Timeplus HTTP API to list streams
curl -s "http://localhost:3218/proton/v1/ddl/streams" | python3 -m json.tool | grep agentshield
```

Expected: `"agentshield_hook_events"` appears in the response.

- [ ] **Step 5: Send a chat message in OpenClaw and verify events arrive**

Send a message in OpenClaw, then wait ~5s for the batch to flush. Query Timeplus for the events:

```bash
curl -s -X POST "http://localhost:3218/proton/v1/sql" \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT hook_name, event_time, session_id, agent_id FROM table(agentshield_hook_events) ORDER BY event_time DESC LIMIT 20"}' \
  | python3 -m json.tool
```

Expected: rows with `hook_name` values like `llm_input`, `llm_output`, `before_tool_call`, `message_received`, etc.

- [ ] **Step 6: Commit**

```bash
git add agents/openclaw/.openclaw/openclaw.json \
        agents/openclaw/Makefile
git commit -m "feat: wire agentshield plugin into openclaw docker setup"
```

---

## Self-Review Checklist (already completed)

- **Spec section 2 (layout):** Covered in Task 1.
- **Spec section 3 (plugin registration):** Covered in Task 5.
- **Spec section 4 (config + env vars + defaults):** Covered in Task 2.
- **Spec section 5 (stream schema + backend pre-create):** Covered in Task 6.
- **Spec section 6 (TimeplusClient + retry + close):** Covered in Task 3.
- **Spec section 7 (all 29 hooks, 3 tiers, gateway_stop calls close):** Covered in Task 4.
- **Spec section 8 (backend changes):** Covered in Task 6.
- **Type consistency:** `HookEvent` defined in Task 3, used in Tasks 4/5. `AgentShieldConfig` defined in Task 2, used in Tasks 3/5. `baseEvent` defined and exported in Task 4, tested in Task 4.
- **No placeholders:** All steps have concrete code or commands.
