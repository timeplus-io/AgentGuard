#!/usr/bin/env node
var __defProp = Object.defineProperty;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __esm = (fn, res) => function __init() {
  return fn && (res = (0, fn[__getOwnPropNames(fn)[0]])(fn = 0)), res;
};
var __export = (target, all) => {
  for (var name in all)
    __defProp(target, name, { get: all[name], enumerable: true });
};

// src/holds.ts
var holds_exports = {};
__export(holds_exports, {
  requestHold: () => requestHold
});
async function requestHold(event, config) {
  const url = `${config.agentguardUrl}/api/holds`;
  let hookPayload = {};
  try {
    hookPayload = JSON.parse(event.event_data);
  } catch {
    hookPayload = {};
  }
  const body = JSON.stringify({
    deployment_id: event.deployment_id,
    agent_id: event.agent_id,
    session_id: event.session_id,
    run_id: event.run_id,
    tool_name: event.tool_name,
    event_time: event.event_time,
    tool_input: hookPayload.tool_input ?? {},
    hook_payload: hookPayload
  });
  try {
    const ctrl = new AbortController();
    const t = setTimeout(() => ctrl.abort(), HOLD_TIMEOUT_MS);
    const resp = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body,
      signal: ctrl.signal
    });
    clearTimeout(t);
    if (!resp.ok) {
      return failPolicy(config, `backend returned ${resp.status}`);
    }
    return await resp.json();
  } catch (err) {
    return failPolicy(config, `request failed: ${err instanceof Error ? err.message : String(err)}`);
  }
}
function failPolicy(config, reason) {
  if (config.holdFailPolicy === "allow") {
    return { decision: "allow" };
  }
  return { decision: "block", reason: `AgentGuard unreachable \u2014 ${reason}` };
}
var HOLD_TIMEOUT_MS;
var init_holds = __esm({
  "src/holds.ts"() {
    "use strict";
    HOLD_TIMEOUT_MS = 54e4;
  }
});

// src/config.ts
import { hostname } from "os";
var DEFAULTS = {
  timeplusUrl: "http://localhost:3218",
  stream: "agentguard_hook_events",
  username: "proton",
  password: "",
  agentId: "",
  deploymentId: "default",
  deploymentName: "Default",
  agentguardUrl: "http://localhost:8080",
  holdFailPolicy: "deny",
  holdsEnabled: true
};
function resolveConfig() {
  return {
    timeplusUrl: process.env.AGENTGUARD_TIMEPLUS_URL || DEFAULTS.timeplusUrl,
    stream: process.env.AGENTGUARD_STREAM || DEFAULTS.stream,
    username: process.env.AGENTGUARD_USERNAME || DEFAULTS.username,
    password: process.env.AGENTGUARD_PASSWORD ?? DEFAULTS.password,
    agentId: process.env.AGENTGUARD_AGENT_ID || hostname(),
    deploymentId: process.env.AGENTGUARD_DEPLOYMENT_ID || DEFAULTS.deploymentId,
    deploymentName: process.env.AGENTGUARD_DEPLOYMENT_NAME || DEFAULTS.deploymentName,
    agentguardUrl: process.env.AGENTGUARD_URL ?? DEFAULTS.agentguardUrl,
    holdFailPolicy: process.env.AGENTGUARD_HOLD_FAIL_POLICY === "allow" ? "allow" : "deny",
    holdsEnabled: process.env.AGENTGUARD_HOLDS_ENABLED?.toLowerCase() !== "false"
  };
}

// src/timeplus-client.ts
var COLUMNS = [
  "hook_name",
  "event_time",
  "session_id",
  "run_id",
  "agent_id",
  "deployment_id",
  "deployment_name",
  "session_key",
  "tool_name",
  "provider",
  "model",
  "hook_decision",
  "block_reason",
  "event_data",
  "agent_type"
];
async function send(event, config) {
  const url = `${config.timeplusUrl}/proton/v1/ingest/streams/${config.stream}`;
  const authHeader = `Basic ${btoa(`${config.username}:${config.password}`)}`;
  const body = JSON.stringify({
    columns: COLUMNS,
    data: [COLUMNS.map((col) => event[col])]
  });
  try {
    const resp = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": authHeader
      },
      body
    });
    if (!resp.ok) {
      process.stderr.write(`[agentguard] ingest failed: ${resp.status}
`);
    }
  } catch (err) {
    process.stderr.write(`[agentguard] network error: ${String(err)}
`);
  }
}

// src/normalize.ts
function inferProvider(model) {
  if (!model) return "anthropic";
  if (model.includes("@")) return "vertex";
  if (/^(us\.|eu\.|ap\.)?anthropic\./.test(model)) return "bedrock";
  return "anthropic";
}
var HOOK_NAME_MAP = {
  SessionStart: "session_start",
  SessionEnd: "session_end",
  UserPromptSubmit: "user_prompt_submit",
  PreToolUse: "before_tool_call",
  PostToolUse: "after_tool_call",
  PostToolUseFailure: "tool_call_failure",
  SubagentStart: "subagent_spawning",
  SubagentStop: "subagent_ended",
  Stop: "llm_output",
  PermissionDenied: "permission_denied"
};
function normalize(input, config) {
  const hookEventName = input.hook_event_name ?? "";
  const hookName = HOOK_NAME_MAP[hookEventName] ?? hookEventName.toLowerCase();
  const isToolEvent = hookEventName === "PreToolUse" || hookEventName === "PostToolUse" || hookEventName === "PostToolUseFailure";
  const llmUsage = input._llm_usage ?? {};
  const model = hookEventName === "SessionStart" ? typeof input.model === "string" ? input.model : "" : hookEventName === "Stop" ? typeof llmUsage.model === "string" ? llmUsage.model : "" : "";
  return {
    hook_name: hookName,
    event_time: Date.now(),
    session_id: typeof input.session_id === "string" ? input.session_id : "",
    run_id: typeof input.session_id === "string" ? input.session_id : "",
    agent_id: config.agentId,
    deployment_id: config.deploymentId,
    deployment_name: config.deploymentName,
    session_key: "",
    tool_name: isToolEvent && typeof input.tool_name === "string" ? input.tool_name : "",
    provider: model ? inferProvider(model) : "",
    model,
    hook_decision: "observe",
    block_reason: "",
    event_data: JSON.stringify(input),
    agent_type: "claudecode"
  };
}
function normalizeContextEvent(input, config, hookName, transcript) {
  return {
    hook_name: hookName,
    event_time: Date.now(),
    session_id: typeof input.session_id === "string" ? input.session_id : "",
    run_id: typeof input.session_id === "string" ? input.session_id : "",
    agent_id: config.agentId,
    deployment_id: config.deploymentId,
    deployment_name: config.deploymentName,
    session_key: "",
    tool_name: "",
    provider: "",
    model: "",
    hook_decision: "observe",
    block_reason: "",
    event_data: JSON.stringify({ message_count: transcript.length, messages: transcript }),
    agent_type: "claudecode"
  };
}

// src/transcript.ts
import { readFileSync } from "fs";
import { homedir } from "os";
var MAX_MESSAGES = parseInt(process.env.AGENTGUARD_TRANSCRIPT_MESSAGES ?? "50", 10);
var MAX_CONTENT_CHARS = parseInt(process.env.AGENTGUARD_TRANSCRIPT_CONTENT_CHARS ?? "10000", 10);
function toNum(v) {
  return typeof v === "number" ? v : 0;
}
function resolvePath(p) {
  return p.startsWith("~/") ? p.replace("~", homedir()) : p;
}
function truncateContent(content) {
  if (typeof content === "string") {
    return content.length > MAX_CONTENT_CHARS ? content.slice(0, MAX_CONTENT_CHARS) + `...[truncated ${content.length - MAX_CONTENT_CHARS} chars]` : content;
  }
  if (Array.isArray(content)) {
    return content.map((block) => {
      if (typeof block !== "object" || block === null) return block;
      const b = block;
      if (typeof b["text"] === "string") {
        return { ...b, text: truncateContent(b["text"]) };
      }
      if (Array.isArray(b["content"])) {
        return { ...b, content: truncateContent(b["content"]) };
      }
      return b;
    });
  }
  return content;
}
function readTranscript(transcriptPath) {
  try {
    const resolvedPath = resolvePath(transcriptPath);
    const raw = readFileSync(resolvedPath, "utf8");
    const messages = [];
    for (const line of raw.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        const entry = JSON.parse(trimmed);
        const msg = entry["message"];
        if (!msg) continue;
        messages.push({
          role: typeof msg["role"] === "string" ? msg["role"] : String(entry["type"] ?? "unknown"),
          timestamp: typeof entry["timestamp"] === "string" ? entry["timestamp"] : "",
          content: truncateContent(msg["content"])
        });
      } catch {
      }
    }
    return messages.slice(-MAX_MESSAGES);
  } catch {
    return [];
  }
}
function readTranscriptUsage(transcriptPath) {
  const result = {
    input_tokens: 0,
    output_tokens: 0,
    cache_creation_input_tokens: 0,
    cache_read_input_tokens: 0,
    total_tokens: 0,
    duration_ms: 0,
    model: "",
    llm_calls: 0
  };
  try {
    const raw = readFileSync(resolvePath(transcriptPath), "utf8");
    for (const line of raw.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        const entry = JSON.parse(trimmed);
        const msg = entry["message"];
        if (!msg || msg["role"] !== "assistant") continue;
        result.llm_calls++;
        result.duration_ms += toNum(entry["durationMs"]);
        const usage = msg["usage"];
        if (usage) {
          result.input_tokens += toNum(usage["input_tokens"]);
          result.output_tokens += toNum(usage["output_tokens"]);
          result.cache_creation_input_tokens += toNum(usage["cache_creation_input_tokens"]);
          result.cache_read_input_tokens += toNum(usage["cache_read_input_tokens"]);
        }
        if (!result.model && typeof msg["model"] === "string" && msg["model"]) {
          result.model = msg["model"];
        }
      } catch {
      }
    }
    result.total_tokens = result.input_tokens + result.output_tokens;
  } catch {
  }
  return result;
}

// src/index.ts
var CONTEXT_HOOKS = {
  UserPromptSubmit: "prompt_context",
  Stop: "llm_context"
};
async function main() {
  const chunks = [];
  for await (const chunk of process.stdin) {
    chunks.push(chunk);
  }
  const raw = Buffer.concat(chunks).toString("utf8").trim();
  if (!raw) {
    return;
  }
  let input;
  try {
    input = JSON.parse(raw);
  } catch {
    process.stderr.write(`[agentguard] failed to parse hook input: ${raw.slice(0, 200)}
`);
    return;
  }
  const config = resolveConfig();
  const hookEventName = typeof input["hook_event_name"] === "string" ? input["hook_event_name"] : "";
  if (hookEventName === "PreToolUse") {
    if (!config.holdsEnabled) {
      process.stdout.write(JSON.stringify({
        hookSpecificOutput: {
          hookEventName: "PreToolUse",
          permissionDecision: "allow",
          permissionDecisionReason: ""
        }
      }));
    } else {
      const event2 = normalize(input, config);
      const { requestHold: requestHold2 } = await Promise.resolve().then(() => (init_holds(), holds_exports));
      const decision = await requestHold2(event2, config);
      process.stdout.write(JSON.stringify({
        hookSpecificOutput: {
          hookEventName: "PreToolUse",
          permissionDecision: decision.decision === "block" ? "deny" : "allow",
          permissionDecisionReason: decision.reason ?? ""
        }
      }));
      return;
    }
  }
  if (hookEventName === "SessionEnd") {
    const sessionId = typeof input["session_id"] === "string" ? input["session_id"] : "";
    if (sessionId) {
      fetch(`${config.agentguardUrl}/api/holds/_abandon-session/${encodeURIComponent(sessionId)}`, {
        method: "POST"
      }).catch(() => {
      });
    }
  }
  if (hookEventName === "Stop") {
    const transcriptPath = typeof input["transcript_path"] === "string" ? input["transcript_path"] : null;
    if (transcriptPath) {
      input = { ...input, _llm_usage: readTranscriptUsage(transcriptPath) };
    }
  }
  const event = normalize(input, config);
  await send(event, config);
  const contextHookName = CONTEXT_HOOKS[hookEventName];
  if (contextHookName) {
    const transcriptPath = typeof input["transcript_path"] === "string" ? input["transcript_path"] : null;
    if (transcriptPath) {
      const transcript = readTranscript(transcriptPath);
      if (transcript.length > 0) {
        const contextEvent = normalizeContextEvent(input, config, contextHookName, transcript);
        await send(contextEvent, config);
      }
    }
  }
}
main().catch((err) => {
  process.stderr.write(`[agentguard] unexpected error: ${String(err)}
`);
});
