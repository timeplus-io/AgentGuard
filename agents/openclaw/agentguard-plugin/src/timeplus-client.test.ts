import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createTimeplusClient } from "./timeplus-client.js";
import type { HookEvent } from "./timeplus-client.js";

const BASE_CONFIG = {
  timeplusUrl: "http://localhost:3218",
  database: "ag",
  stream: "agentguard_hook_events",
  flushMs: 60000, // long interval so timer doesn't auto-flush during tests
  batchSize: 50,
};

function makeEvent(hookName: string): HookEvent {
  return {
    hook_name: hookName,
    event_time: "2026-04-07T00:00:00.000Z",
    session_id: "sess-1",
    run_id: "run-1",
    agent_id: "main",
    deployment_id: "default",
    deployment_name: "Default",
    session_key: "agent:main:main",
    tool_name: "",
    provider: "",
    model: "",
    hook_decision: "observe",
    block_reason: "",
    event_data: JSON.stringify({ test: true }),
  };
}

describe("TimeplusClient", () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal("fetch", fetchMock);
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("does not POST immediately on enqueue", () => {
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("session_start"));
    expect(fetchMock).not.toHaveBeenCalled();
    client.close();
  });

  it("POSTs on flush() with correct URL and payload shape", async () => {
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("before_tool_call"));
    await client.flush();

    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, opts] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(
      "http://localhost:3218/timeplusd/v2/ingest/ag/streams/agentguard_hook_events",
    );
    expect(opts.method).toBe("POST");

    const body = JSON.parse(opts.body as string) as {
      columns: string[];
      data: string[][];
    };
    expect(body.columns).toEqual([
      "hook_name", "event_time", "session_id", "run_id", "agent_id",
      "deployment_id", "deployment_name",
      "session_key", "tool_name", "provider", "model",
      "hook_decision", "block_reason", "event_data", "agent_type",
    ]);
    expect(body.data).toHaveLength(1);
    expect(body.data[0][0]).toBe("before_tool_call");
    await client.close();
  });

  it("clears the queue after flush so events are not sent twice", async () => {
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("llm_input"));
    await client.flush();
    await client.flush(); // second flush should be a no-op
    expect(fetchMock).toHaveBeenCalledOnce();
    await client.close();
  });

  it("flushes immediately when batchSize is reached", async () => {
    const client = createTimeplusClient({ ...BASE_CONFIG, batchSize: 2 });
    client.enqueue(makeEvent("llm_input"));
    // not yet at batch size
    expect(fetchMock).not.toHaveBeenCalled();
    client.enqueue(makeEvent("llm_output"));
    // now at batch size — flush should have been triggered asynchronously
    await Promise.resolve(); // drain microtask queue
    expect(fetchMock).toHaveBeenCalledOnce();
    await client.close();
  });

  it("retries once on failure then drops the batch", async () => {
    const consoleSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    fetchMock
      .mockResolvedValueOnce({ ok: false, status: 500 })
      .mockResolvedValueOnce({ ok: false, status: 500 });

    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("session_end"));
    await client.flush();

    // Should have tried twice (initial + 1 retry)
    expect(fetchMock).toHaveBeenCalledTimes(2);
    // console.error should have been called exactly once for the dropped batch
    expect(consoleSpy).toHaveBeenCalledTimes(1);
    consoleSpy.mockRestore();
    await client.close();
  });

  it("never throws even when fetch rejects completely", async () => {
    const consoleSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    fetchMock.mockRejectedValue(new Error("network error"));
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("gateway_stop"));
    await expect(client.flush()).resolves.toBeUndefined();
    consoleSpy.mockRestore();
    await client.close();
  });

  it("close() flushes remaining events", async () => {
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("gateway_stop"));
    await client.close();
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it("sends events batched in a single POST", async () => {
    const client = createTimeplusClient(BASE_CONFIG);
    client.enqueue(makeEvent("llm_input"));
    client.enqueue(makeEvent("llm_output"));
    client.enqueue(makeEvent("before_tool_call"));
    await client.flush();

    expect(fetchMock).toHaveBeenCalledOnce();
    const body = JSON.parse(
      (fetchMock.mock.calls[0] as [string, RequestInit])[1].body as string,
    ) as { data: string[][] };
    expect(body.data).toHaveLength(3);
    await client.close();
  });

  it("flushes automatically when the timer fires", async () => {
    const client = createTimeplusClient(BASE_CONFIG);
    // enqueue one event (below batchSize so it doesn't trigger immediate flush)
    client.enqueue(makeEvent("timer_test"));
    expect(fetchMock).not.toHaveBeenCalled();
    // advance fake timers by flushMs to trigger the background interval
    await vi.advanceTimersByTimeAsync(BASE_CONFIG.flushMs);
    // fetch should have been called exactly once
    expect(fetchMock).toHaveBeenCalledOnce();
    const body = JSON.parse(
      (fetchMock.mock.calls[0] as [string, RequestInit])[1].body as string,
    ) as { data: string[][] };
    expect(body.data[0][0]).toBe("timer_test");
    await client.close();
  });
});
