#!/usr/bin/env node
"use strict";

// src/config.ts
var DEFAULTS = {
  timeplusUrl: "http://localhost:3218",
  stream: "agentshield_hook_events",
  username: "proton",
  password: "",
  agentId: "",
  deploymentId: "default",
  deploymentName: "Default"
};
function resolveConfig() {
  return {
    timeplusUrl: process.env.AGENTSHIELD_TIMEPLUS_URL || DEFAULTS.timeplusUrl,
    stream: process.env.AGENTSHIELD_STREAM || DEFAULTS.stream,
    username: process.env.AGENTSHIELD_USERNAME || DEFAULTS.username,
    password: process.env.AGENTSHIELD_PASSWORD ?? DEFAULTS.password,
    agentId: process.env.AGENTSHIELD_AGENT_ID ?? DEFAULTS.agentId,
    deploymentId: process.env.AGENTSHIELD_DEPLOYMENT_ID || DEFAULTS.deploymentId,
    deploymentName: process.env.AGENTSHIELD_DEPLOYMENT_NAME || DEFAULTS.deploymentName
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
      process.stderr.write(`[agentshield] ingest failed: ${resp.status}
`);
    }
  } catch (err) {
    process.stderr.write(`[agentshield] network error: ${String(err)}
`);
  }
}

// src/normalize.ts
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
function normalize(input, config, transcript = []) {
  const hookEventName = input.hook_event_name ?? "";
  const hookName = HOOK_NAME_MAP[hookEventName] ?? hookEventName.toLowerCase();
  const isToolEvent = hookEventName === "PreToolUse" || hookEventName === "PostToolUse" || hookEventName === "PostToolUseFailure";
  const model = hookEventName === "SessionStart" ? typeof input.model === "string" ? input.model : "" : "";
  const eventData = { ...input };
  if (transcript.length > 0) {
    eventData["_transcript"] = transcript;
  }
  return {
    hook_name: hookName,
    event_time: (/* @__PURE__ */ new Date()).toISOString(),
    session_id: typeof input.session_id === "string" ? input.session_id : "",
    run_id: typeof input.session_id === "string" ? input.session_id : "",
    agent_id: config.agentId,
    deployment_id: config.deploymentId,
    deployment_name: config.deploymentName,
    session_key: "",
    tool_name: isToolEvent && typeof input.tool_name === "string" ? input.tool_name : "",
    provider: "",
    model,
    hook_decision: "observe",
    block_reason: "",
    event_data: JSON.stringify(eventData),
    agent_type: "claudecode"
  };
}

// src/transcript.ts
var import_fs = require("fs");
var import_os = require("os");
var MAX_MESSAGES = parseInt(process.env.AGENTSHIELD_TRANSCRIPT_MESSAGES ?? "50", 10);
var MAX_CONTENT_CHARS = parseInt(process.env.AGENTSHIELD_TRANSCRIPT_CONTENT_CHARS ?? "10000", 10);
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
    const resolvedPath = transcriptPath.startsWith("~/") ? transcriptPath.replace("~", (0, import_os.homedir)()) : transcriptPath;
    const raw = (0, import_fs.readFileSync)(resolvedPath, "utf8");
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

// src/index.ts
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
    process.stderr.write(`[agentshield] failed to parse hook input: ${raw.slice(0, 200)}
`);
    return;
  }
  const transcriptPath = typeof input["transcript_path"] === "string" ? input["transcript_path"] : null;
  const transcript = transcriptPath ? readTranscript(transcriptPath) : [];
  const config = resolveConfig();
  const event = normalize(input, config, transcript);
  await send(event, config);
}
main().catch((err) => {
  process.stderr.write(`[agentshield] unexpected error: ${String(err)}
`);
});
