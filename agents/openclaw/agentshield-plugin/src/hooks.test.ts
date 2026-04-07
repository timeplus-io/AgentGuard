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
