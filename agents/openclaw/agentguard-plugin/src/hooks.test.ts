import { describe, it, expect } from "vitest";
import { baseEvent } from "./hooks.js";

const TEST_CONFIG = {
  timeplusUrl: "http://localhost:3218",
  stream: "agentguard_hook_events",
  flushMs: 2000,
  batchSize: 50,
  username: "proton",
  password: "",
  deploymentId: "test-deployment",
  deploymentName: "Test Deployment",
};

describe("baseEvent", () => {
  it("populates hook_name and event_time", () => {
    const e = baseEvent("llm_input", {}, TEST_CONFIG);
    expect(e.hook_name).toBe("llm_input");
    expect(e.event_time).toMatch(/^\d{4}-\d{2}-\d{2}T/);
  });

  it("fills context fields from ctx when provided", () => {
    const e = baseEvent("before_tool_call", {
      agentId: "main",
      sessionKey: "agent:main:main",
      sessionId: "sess-abc",
    }, TEST_CONFIG);
    expect(e.agent_id).toBe("main");
    expect(e.session_key).toBe("agent:main:main");
    expect(e.session_id).toBe("sess-abc");
  });

  it("defaults context fields to empty strings when ctx is empty", () => {
    const e = baseEvent("gateway_start", {}, TEST_CONFIG);
    expect(e.agent_id).toBe("");
    expect(e.session_key).toBe("");
    expect(e.session_id).toBe("");
    expect(e.run_id).toBe("");
    expect(e.tool_name).toBe("");
    expect(e.provider).toBe("");
    expect(e.model).toBe("");
  });

  it("stamps deployment fields from config", () => {
    const e = baseEvent("session_start", {}, TEST_CONFIG);
    expect(e.deployment_id).toBe("test-deployment");
    expect(e.deployment_name).toBe("Test Deployment");
  });

  it("sets hook_decision to 'observe' and block_reason to empty", () => {
    const e = baseEvent("message_sent", {}, TEST_CONFIG);
    expect(e.hook_decision).toBe("observe");
    expect(e.block_reason).toBe("");
  });

  it("sets event_data to empty string by default", () => {
    const e = baseEvent("session_end", {}, TEST_CONFIG);
    expect(e.event_data).toBe("");
  });
});
