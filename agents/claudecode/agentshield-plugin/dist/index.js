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
function normalize(input, config) {
  const hookEventName = input.hook_event_name ?? "";
  const hookName = HOOK_NAME_MAP[hookEventName] ?? hookEventName.toLowerCase();
  const isToolEvent = hookEventName === "PreToolUse" || hookEventName === "PostToolUse" || hookEventName === "PostToolUseFailure";
  const model = hookEventName === "SessionStart" ? typeof input.model === "string" ? input.model : "" : "";
  return {
    hook_name: hookName,
    event_time: (/* @__PURE__ */ new Date()).toISOString(),
    session_id: typeof input.session_id === "string" ? input.session_id : "",
    run_id: isToolEvent && typeof input.tool_use_id === "string" ? input.tool_use_id : "",
    agent_id: config.agentId,
    deployment_id: config.deploymentId,
    deployment_name: config.deploymentName,
    session_key: "",
    tool_name: isToolEvent && typeof input.tool_name === "string" ? input.tool_name : "",
    provider: "",
    model,
    hook_decision: "observe",
    block_reason: "",
    event_data: JSON.stringify(input),
    agent_type: "claudecode"
  };
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
  const config = resolveConfig();
  const event = normalize(input, config);
  await send(event, config);
}
main().catch((err) => {
  process.stderr.write(`[agentshield] unexpected error: ${String(err)}
`);
});
