import { describe, it, expect } from "vitest";
import { normalize, normalizeContextEvent } from "./normalize.js";

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

  it("maps PreToolUse → hook_name 'before_tool_call' with tool_name", () => {
    const e = normalize({
      hook_event_name: "PreToolUse",
      session_id: "s1",
      tool_name: "Bash",
      tool_use_id: "tu-123",
      tool_input: { command: "ls" },
    }, BASE_CONFIG);
    expect(e.hook_name).toBe("before_tool_call");
    expect(e.tool_name).toBe("Bash");
    expect(e.run_id).toBe("s1");
  });

  it("maps PostToolUse → 'after_tool_call' with tool_name", () => {
    const e = normalize({
      hook_event_name: "PostToolUse",
      session_id: "s1",
      tool_name: "Write",
      tool_use_id: "tu-456",
    }, BASE_CONFIG);
    expect(e.hook_name).toBe("after_tool_call");
    expect(e.tool_name).toBe("Write");
    expect(e.run_id).toBe("s1");
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

  it("sets run_id to session_id for all events (no run concept in Claude Code)", () => {
    const tool = normalize({ hook_event_name: "PreToolUse", session_id: "s1", tool_use_id: "tu-abc" }, BASE_CONFIG);
    expect(tool.run_id).toBe("s1");
    const stop = normalize({ hook_event_name: "Stop", session_id: "s1" }, BASE_CONFIG);
    expect(stop.run_id).toBe("s1");
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

  it("never embeds _transcript in regular hook event_data", () => {
    const e = normalize({ hook_event_name: "Stop", session_id: "s1" }, BASE_CONFIG);
    const data = JSON.parse(e.event_data);
    expect(data["_transcript"]).toBeUndefined();
  });
});

describe("normalizeContextEvent", () => {
  const transcript = [
    { role: "user", timestamp: "2026-01-01T00:00:00.000Z", content: [{ type: "text", text: "Hello" }] },
    { role: "assistant", timestamp: "2026-01-01T00:00:01.000Z", content: [{ type: "text", text: "Hi" }] },
  ];

  it("produces hook_name 'prompt_context' for UserPromptSubmit trigger", () => {
    const e = normalizeContextEvent(
      { hook_event_name: "UserPromptSubmit", session_id: "s1" },
      BASE_CONFIG, "prompt_context", transcript,
    );
    expect(e.hook_name).toBe("prompt_context");
  });

  it("produces hook_name 'llm_context' for Stop trigger", () => {
    const e = normalizeContextEvent(
      { hook_event_name: "Stop", session_id: "s1" },
      BASE_CONFIG, "llm_context", transcript,
    );
    expect(e.hook_name).toBe("llm_context");
  });

  it("carries session_id from triggering input", () => {
    const e = normalizeContextEvent(
      { hook_event_name: "Stop", session_id: "sess-xyz" },
      BASE_CONFIG, "llm_context", transcript,
    );
    expect(e.session_id).toBe("sess-xyz");
  });

  it("sets run_id to session_id, tool_name/model/provider to empty strings", () => {
    const e = normalizeContextEvent(
      { hook_event_name: "Stop", session_id: "s1" },
      BASE_CONFIG, "llm_context", transcript,
    );
    expect(e.run_id).toBe("s1");
    expect(e.tool_name).toBe("");
    expect(e.model).toBe("");
    expect(e.provider).toBe("");
  });

  it("embeds messages array and message_count in event_data", () => {
    const e = normalizeContextEvent(
      { hook_event_name: "Stop", session_id: "s1" },
      BASE_CONFIG, "llm_context", transcript,
    );
    const data = JSON.parse(e.event_data);
    expect(data["message_count"]).toBe(2);
    expect(data["messages"]).toEqual(transcript);
  });

  it("sets agent_type to 'claudecode'", () => {
    const e = normalizeContextEvent(
      { hook_event_name: "Stop", session_id: "s1" },
      BASE_CONFIG, "llm_context", transcript,
    );
    expect(e.agent_type).toBe("claudecode");
  });

  it("sets hook_decision to 'observe'", () => {
    const e = normalizeContextEvent(
      { hook_event_name: "Stop", session_id: "s1" },
      BASE_CONFIG, "llm_context", transcript,
    );
    expect(e.hook_decision).toBe("observe");
  });
});
