import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { send, sendBatch } from "./timeplus-client.js";
import type { AgentGuardConfig } from "./config.js";
import type { HookEvent } from "./timeplus-client.js";

const BASE_CONFIG: AgentGuardConfig = {
  timeplusUrl: "http://localhost:3218",
  ingestApi: "timeplusd",
  workspace: "default",
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

  it("uses the configured database verbatim in the ingest URL", async () => {
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

describe("sendBatch (timeplus-client)", () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const rows = (n: number) => Array.from({ length: n }, (_, i) => ({ ...SAMPLE_EVENT, session_id: `s${i}` }));
  const bodyRows = (call: number) =>
    (JSON.parse((fetchMock.mock.calls[call] as [string, RequestInit])[1].body as string) as { data: unknown[][] }).data;

  it("sends several rows in one request", async () => {
    await sendBatch(rows(3), BASE_CONFIG);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(bodyRows(0).map((r) => r[2])).toEqual(["s0", "s1", "s2"]);
  });

  it("sends nothing for an empty list", async () => {
    await sendBatch([], BASE_CONFIG);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("splits a long list into requests of 500 rows", async () => {
    await sendBatch(rows(1201), BASE_CONFIG);
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect([0, 1, 2].map((i) => bodyRows(i).length)).toEqual([500, 500, 201]);
  });
});

describe("send via the Timeplus REST API (#299)", () => {
  let fetchMock: ReturnType<typeof vi.fn>;
  let stderrWrite: { mockRestore: () => void };

  const REST_CONFIG: AgentGuardConfig = {
    ...BASE_CONFIG,
    ingestApi: "rest",
    timeplusUrl: "https://us-west-2.timeplus.cloud",
    workspace: "default",
    username: "u",
    password: "p",
  };

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

  it("builds the workspace-prefixed v1beta2 ingest URL with db query param", async () => {
    await send(SAMPLE_EVENT, REST_CONFIG);
    const url = (fetchMock.mock.calls[0] as [string])[0];
    expect(url).toBe(
      "https://us-west-2.timeplus.cloud/default/api/v1beta2/streams/agentguard_hook_events/ingest?db=ag",
    );
  });

  it("uses the configured workspace in the URL path", async () => {
    await send(SAMPLE_EVENT, { ...REST_CONFIG, workspace: "ws-42" });
    const url = (fetchMock.mock.calls[0] as [string])[0];
    expect(url).toBe(
      "https://us-west-2.timeplus.cloud/ws-42/api/v1beta2/streams/agentguard_hook_events/ingest?db=ag",
    );
  });

  it("tolerates a trailing slash on the base URL", async () => {
    await send(SAMPLE_EVENT, { ...REST_CONFIG, timeplusUrl: "http://localhost:8000/" });
    const url = (fetchMock.mock.calls[0] as [string])[0];
    expect(url).toBe(
      "http://localhost:8000/default/api/v1beta2/streams/agentguard_hook_events/ingest?db=ag",
    );
  });

  it("authenticates with Basic auth using the Timeplus username/password in rest mode", async () => {
    await send(SAMPLE_EVENT, REST_CONFIG);
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = init.headers as Record<string, string>;
    expect(headers["Authorization"]).toBe(`Basic ${btoa("u:p")}`);
    expect(headers["Content-Type"]).toBe("application/json");
  });

  it("keeps the same {columns,data} body shape in rest mode", async () => {
    await send(SAMPLE_EVENT, REST_CONFIG);
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const body = JSON.parse(init.body as string) as { columns: string[]; data: unknown[][] };
    expect(body.columns[0]).toBe("hook_name");
    expect(body.data).toHaveLength(1);
  });

  it("default (timeplusd) config still hits the legacy path with Basic auth", async () => {
    await send(SAMPLE_EVENT, { ...BASE_CONFIG, ingestApi: "timeplusd" });
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(
      "http://localhost:3218/timeplusd/v2/ingest/ag/streams/agentguard_hook_events",
    );
    const headers = init.headers as Record<string, string>;
    expect(headers["Authorization"]).toBe(`Basic ${btoa("proton:")}`);
  });
});
