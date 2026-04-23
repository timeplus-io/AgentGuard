import { describe, it, expect, vi, beforeEach } from "vitest";
import { requestHold } from "./holds.js";
import type { HookEvent } from "./timeplus-client.js";
import type { AgentGuardConfig } from "./config.js";

const BASE_CONFIG: AgentGuardConfig = {
  timeplusUrl:    "http://localhost:3218",
  stream:         "agentguard_hook_events",
  flushMs:        2000,
  batchSize:      50,
  username:       "proton",
  password:       "",
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
  provider: "",
  model: "",
  hook_decision: "observe",
  block_reason: "",
  event_data: JSON.stringify({ params: { command: "ls" } }),
  agent_type: "openclaw",
};

describe("requestHold (openclaw)", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("returns allow when backend responds 200 + decision allow", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(
      JSON.stringify({ decision: "allow" }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    )));
    const d = await requestHold(BASE_EVENT, { command: "ls" }, BASE_CONFIG);
    expect(d.decision).toBe("allow");
  });

  it("returns block + reason when backend responds 200 + decision block", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(
      JSON.stringify({ decision: "block", reason: "destructive command" }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    )));
    const d = await requestHold(BASE_EVENT, {}, BASE_CONFIG);
    expect(d.decision).toBe("block");
    expect(d.reason).toBe("destructive command");
  });

  it("applies fail-policy=deny on HTTP 503", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("oops", { status: 503 })));
    const d = await requestHold(BASE_EVENT, {}, BASE_CONFIG);
    expect(d.decision).toBe("block");
    expect(d.reason).toContain("503");
  });

  it("applies fail-policy=allow when configured", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("network down"); }));
    const d = await requestHold(BASE_EVENT, {}, { ...BASE_CONFIG, holdFailPolicy: "allow" });
    expect(d.decision).toBe("allow");
  });

  it("POSTs the right body shape (with agent_type=openclaw)", async () => {
    const fetchMock = vi.fn(async () => new Response(
      JSON.stringify({ decision: "allow" }), { status: 200 },
    ));
    vi.stubGlobal("fetch", fetchMock);
    await requestHold(BASE_EVENT, { command: "ls" }, BASE_CONFIG);

    expect(fetchMock).toHaveBeenCalledWith(
      "http://localhost:8080/api/holds",
      expect.objectContaining({
        method: "POST",
      }),
    );
    const body = JSON.parse(fetchMock.mock.calls[0][1].body);
    expect(body.agent_type).toBe("openclaw");
    expect(body.deployment_id).toBe("d");
    expect(body.tool_name).toBe("Bash");
    expect(body.tool_input).toEqual({ command: "ls" });
  });
});
