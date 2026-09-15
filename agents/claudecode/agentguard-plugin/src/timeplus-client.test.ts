import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { send } from "./timeplus-client.js";
import type { AgentGuardConfig } from "./config.js";
import type { HookEvent } from "./timeplus-client.js";

const BASE_CONFIG: AgentGuardConfig = {
  timeplusUrl: "http://localhost:3218",
  database: "ag",
  stream: "agentguard_hook_events",
  username: "proton",
  password: "",
  agentId: "a",
  deploymentId: "d",
  deploymentName: "D",
  agentguardUrl: "http://localhost:8080",
  holdFailPolicy: "deny",
  holdsEnabled: false,
};

const SAMPLE_EVENT: HookEvent = {
  hook_name: "session_start",
  event_time: 1745323200000,
  session_id: "s1",
  run_id: "r1",
  agent_id: "a",
  deployment_id: "d",
  deployment_name: "D",
  session_key: "",
  tool_name: "",
  provider: "",
  model: "",
  hook_decision: "observe",
  block_reason: "",
  event_data: "{}",
  agent_type: "claudecode",
};

describe("send (timeplus-client)", () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  let stderrWrite: { mockRestore: () => void };

  beforeEach(() => {
    fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal("fetch", fetchMock);
    stderrWrite = vi
      .spyOn(process.stderr, "write")
      .mockImplementation(() => true);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    stderrWrite.mockRestore();
  });

  it("add database ag in the ingest URL", async () => {
    await send(SAMPLE_EVENT, BASE_CONFIG);
    const url = (fetchMock.mock.calls[0] as [string])[0];
    expect(url).toBe(
      "http://localhost:3218/timeplusd/v2/ingest/ag/streams/agentguard_hook_events",
    );
  });

  it("omits the database query when database='default'", async () => {
    await send(SAMPLE_EVENT, { ...BASE_CONFIG, database: "default" });
    const url = (fetchMock.mock.calls[0] as [string])[0];
    expect(url).toBe(
      "http://localhost:3218/timeplusd/v2/ingest/default/streams/agentguard_hook_events",
    );
  });

  it("POSTs JSON with the expected column shape", async () => {
    await send(SAMPLE_EVENT, BASE_CONFIG);
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(init.method).toBe("POST");
    const body = JSON.parse(init.body as string) as {
      columns: string[];
      data: unknown[][];
    };
    expect(body.columns).toEqual([
      "hook_name", "event_time", "session_id", "run_id", "agent_id",
      "deployment_id", "deployment_name", "session_key", "tool_name",
      "provider", "model", "hook_decision", "block_reason", "event_data", "agent_type",
    ]);
    expect(body.data).toHaveLength(1);
    expect(body.data[0]?.[0]).toBe("session_start");
  });

  it("writes to stderr and does not throw on non-2xx", async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500 });
    await expect(send(SAMPLE_EVENT, BASE_CONFIG)).resolves.toBeUndefined();
    expect(stderrWrite).toHaveBeenCalled();
  });

  it("writes to stderr and does not throw on network failure", async () => {
    fetchMock.mockRejectedValue(new Error("ECONNREFUSED"));
    await expect(send(SAMPLE_EVENT, BASE_CONFIG)).resolves.toBeUndefined();
    expect(stderrWrite).toHaveBeenCalled();
  });
});
