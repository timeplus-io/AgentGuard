import { describe, it, expect, vi, beforeEach } from "vitest";
import { requestHold } from "./holds.js";
import type { HookEvent } from "./timeplus-client.js";
import type { AgentGuardConfig } from "./config.js";

const BASE_CONFIG: AgentGuardConfig = {
  timeplusUrl:    "http://localhost:3218",
  stream:         "agentguard_hook_events",
  username:       "proton",
  password:       "",
  agentId:        "a",
  deploymentId:   "d",
  deploymentName: "D",
  agentguardUrl:  "http://localhost:8080",
  holdFailPolicy: "deny",
};

const BASE_EVENT: HookEvent = {
  hook_name: "before_tool_call",
  event_time: "2026-04-22T12:00:00.000Z",
  session_id: "s1",
  run_id: "r1",
  agent_id: "a",
  deployment_id: "d",
  deployment_name: "D",
  session_key: "",
  tool_name: "Bash",
  provider: "anthropic",
  model: "",
  hook_decision: "observe",
  block_reason: "",
  event_data: JSON.stringify({ hook_event_name: "PreToolUse", tool_name: "Bash" }),
  agent_type: "claudecode",
};

describe("requestHold", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("returns allow when backend responds 200 + decision allow", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(
      JSON.stringify({ decision: "allow" }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    )));
    const d = await requestHold(BASE_EVENT, BASE_CONFIG);
    expect(d.decision).toBe("allow");
  });

  it("returns block + reason when backend responds 200 + decision block", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(
      JSON.stringify({ decision: "block", reason: "destructive command" }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    )));
    const d = await requestHold(BASE_EVENT, BASE_CONFIG);
    expect(d.decision).toBe("block");
    expect(d.reason).toBe("destructive command");
  });

  it("applies fail-policy=deny on HTTP 503", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("oops", { status: 503 })));
    const d = await requestHold(BASE_EVENT, BASE_CONFIG);
    expect(d.decision).toBe("block");
    expect(d.reason).toContain("503");
  });

  it("applies fail-policy=deny on network error", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("ECONNREFUSED"); }));
    const d = await requestHold(BASE_EVENT, BASE_CONFIG);
    expect(d.decision).toBe("block");
    expect(d.reason).toContain("ECONNREFUSED");
  });

  it("applies fail-policy=allow when configured", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("network down"); }));
    const d = await requestHold(BASE_EVENT, { ...BASE_CONFIG, holdFailPolicy: "allow" });
    expect(d.decision).toBe("allow");
  });

  it("POSTs the right body shape", async () => {
    const fetchMock = vi.fn(async () => new Response(
      JSON.stringify({ decision: "allow" }), { status: 200 },
    ));
    vi.stubGlobal("fetch", fetchMock);
    await requestHold(BASE_EVENT, BASE_CONFIG);

    expect(fetchMock).toHaveBeenCalledWith(
      "http://localhost:8080/api/holds",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({ "Content-Type": "application/json" }),
      }),
    );
    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.deployment_id).toBe("d");
    expect(body.agent_id).toBe("a");
    expect(body.session_id).toBe("s1");
    expect(body.tool_name).toBe("Bash");
    expect(body.hook_payload).toEqual({ hook_event_name: "PreToolUse", tool_name: "Bash" });
  });
});
