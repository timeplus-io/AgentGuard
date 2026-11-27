import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { writeFileSync, unlinkSync, mkdirSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { readTranscript, readTranscriptCalls, readTranscriptUsage } from "./transcript.js";

function makeTmpPath(): string {
  const dir = join(tmpdir(), "agentguard-test");
  mkdirSync(dir, { recursive: true });
  return join(dir, `transcript-${Date.now()}.jsonl`);
}

function usageEntry(opts: {
  inputTokens?: number;
  outputTokens?: number;
  cacheCreation?: number;
  cacheRead?: number;
  durationMs?: number;
  model?: string;
}) {
  return JSON.stringify({
    type: "assistant",
    timestamp: "2026-01-01T00:00:00.000Z",
    durationMs: opts.durationMs ?? 0,
    message: {
      role: "assistant",
      model: opts.model ?? "",
      usage: {
        input_tokens: opts.inputTokens ?? 0,
        output_tokens: opts.outputTokens ?? 0,
        cache_creation_input_tokens: opts.cacheCreation ?? 0,
        cache_read_input_tokens: opts.cacheRead ?? 0,
      },
    },
  });
}

function entry(role: "user" | "assistant", contentText: string, timestamp = "2026-01-01T00:00:00.000Z") {
  return JSON.stringify({
    type: role,
    timestamp,
    message: {
      role,
      content: [{ type: "text", text: contentText }],
    },
  });
}

describe("readTranscript", () => {
  let tmpPath: string;

  beforeEach(() => {
    tmpPath = makeTmpPath();
  });

  afterEach(() => {
    try { unlinkSync(tmpPath); } catch { /* ignore */ }
  });

  it("returns empty array for a missing file", () => {
    expect(readTranscript("/nonexistent/path/transcript.jsonl")).toEqual([]);
  });

  it("returns empty array for an empty file", () => {
    writeFileSync(tmpPath, "");
    expect(readTranscript(tmpPath)).toEqual([]);
  });

  it("parses a single user message", () => {
    writeFileSync(tmpPath, entry("user", "Hello world"));
    const result = readTranscript(tmpPath);
    expect(result).toHaveLength(1);
    expect(result[0].role).toBe("user");
    expect(result[0].timestamp).toBe("2026-01-01T00:00:00.000Z");
    expect(result[0].content).toEqual([{ type: "text", text: "Hello world" }]);
  });

  it("parses multiple messages preserving order", () => {
    const lines = [
      entry("user", "Hello"),
      entry("assistant", "Hi there"),
      entry("user", "How are you?"),
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscript(tmpPath);
    expect(result).toHaveLength(3);
    expect(result[0].role).toBe("user");
    expect(result[1].role).toBe("assistant");
    expect(result[2].role).toBe("user");
  });

  it("skips lines without a message field", () => {
    const lines = [
      JSON.stringify({ type: "system", timestamp: "2026-01-01T00:00:00.000Z", notMessage: true }),
      entry("user", "Valid message"),
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscript(tmpPath);
    expect(result).toHaveLength(1);
    expect(result[0].role).toBe("user");
  });

  it("skips malformed JSON lines without throwing", () => {
    const lines = [
      entry("user", "Before bad line"),
      "this is not json {{{",
      entry("assistant", "After bad line"),
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscript(tmpPath);
    expect(result).toHaveLength(2);
  });

  it("truncates long text content", () => {
    const longText = "x".repeat(20000);
    writeFileSync(tmpPath, entry("user", longText));
    const result = readTranscript(tmpPath);
    expect(result).toHaveLength(1);
    const textBlock = (result[0].content as Array<{ type: string; text: string }>)[0];
    expect(textBlock.text.length).toBeLessThan(20000);
    expect(textBlock.text).toContain("[truncated");
  });

  it("does not truncate content within the limit", () => {
    const shortText = "x".repeat(100);
    writeFileSync(tmpPath, entry("user", shortText));
    const result = readTranscript(tmpPath);
    const textBlock = (result[0].content as Array<{ type: string; text: string }>)[0];
    expect(textBlock.text).toBe(shortText);
  });

  it("respects the MAX_MESSAGES limit (default 50)", () => {
    const lines = Array.from({ length: 60 }, (_, i) =>
      entry(i % 2 === 0 ? "user" : "assistant", `Message ${i}`)
    ).join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscript(tmpPath);
    // Default MAX_MESSAGES is 50 — returns the last 50
    expect(result.length).toBe(50);
    // Last message should be message 59
    const lastContent = (result[49].content as Array<{ type: string; text: string }>)[0];
    expect(lastContent.text).toBe("Message 59");
  });

  it("handles tool_use content blocks without crashing", () => {
    const toolEntry = JSON.stringify({
      type: "assistant",
      timestamp: "2026-01-01T00:00:00.000Z",
      message: {
        role: "assistant",
        content: [
          { type: "text", text: "Let me run that." },
          { type: "tool_use", id: "toolu_1", name: "Bash", input: { command: "ls -la" } },
        ],
      },
    });
    writeFileSync(tmpPath, toolEntry);
    const result = readTranscript(tmpPath);
    expect(result).toHaveLength(1);
    const blocks = result[0].content as Array<Record<string, unknown>>;
    expect(blocks[1]["type"]).toBe("tool_use");
    expect(blocks[1]["name"]).toBe("Bash");
  });

  it("handles tool_result content blocks with nested content", () => {
    const toolResultEntry = JSON.stringify({
      type: "user",
      timestamp: "2026-01-01T00:00:00.000Z",
      message: {
        role: "user",
        content: [
          {
            type: "tool_result",
            tool_use_id: "toolu_1",
            content: [{ type: "text", text: "file1.txt\nfile2.txt" }],
          },
        ],
      },
    });
    writeFileSync(tmpPath, toolResultEntry);
    const result = readTranscript(tmpPath);
    expect(result).toHaveLength(1);
    const block = (result[0].content as Array<Record<string, unknown>>)[0];
    expect(block["type"]).toBe("tool_result");
  });

  it("truncates nested tool_result content that exceeds the limit", () => {
    const longOutput = "y".repeat(20000);
    const toolResultEntry = JSON.stringify({
      type: "user",
      timestamp: "2026-01-01T00:00:00.000Z",
      message: {
        role: "user",
        content: [
          {
            type: "tool_result",
            tool_use_id: "toolu_1",
            content: [{ type: "text", text: longOutput }],
          },
        ],
      },
    });
    writeFileSync(tmpPath, toolResultEntry);
    const result = readTranscript(tmpPath);
    const block = (result[0].content as Array<Record<string, unknown>>)[0];
    const innerContent = block["content"] as Array<{ type: string; text: string }>;
    expect(innerContent[0].text.length).toBeLessThan(20000);
    expect(innerContent[0].text).toContain("[truncated");
  });
});

describe("readTranscriptUsage", () => {
  let tmpPath: string;

  beforeEach(() => {
    tmpPath = makeTmpPath();
  });

  afterEach(() => {
    try { unlinkSync(tmpPath); } catch { /* ignore */ }
  });

  it("returns zero-value struct for a missing file", () => {
    const result = readTranscriptUsage("/nonexistent/path/transcript.jsonl");
    expect(result.input_tokens).toBe(0);
    expect(result.output_tokens).toBe(0);
    expect(result.total_tokens).toBe(0);
    expect(result.duration_ms).toBe(0);
    expect(result.model).toBe("");
    expect(result.llm_calls).toBe(0);
  });

  it("returns zero-value struct for an empty file", () => {
    writeFileSync(tmpPath, "");
    const result = readTranscriptUsage(tmpPath);
    expect(result.llm_calls).toBe(0);
    expect(result.total_tokens).toBe(0);
  });

  it("accumulates token counts from a single assistant message", () => {
    writeFileSync(tmpPath, usageEntry({ inputTokens: 100, outputTokens: 50, durationMs: 500, model: "claude-opus-4-5" }));
    const result = readTranscriptUsage(tmpPath);
    expect(result.input_tokens).toBe(100);
    expect(result.output_tokens).toBe(50);
    expect(result.total_tokens).toBe(150);
    expect(result.duration_ms).toBe(0); // the transcript has no request entry
    expect(result.model).toBe("claude-opus-4-5");
    expect(result.llm_calls).toBe(1);
  });

  it("accumulates token counts across multiple assistant messages", () => {
    const lines = [
      usageEntry({ inputTokens: 100, outputTokens: 50, durationMs: 300 }),
      usageEntry({ inputTokens: 200, outputTokens: 80, durationMs: 200 }),
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscriptUsage(tmpPath);
    expect(result.input_tokens).toBe(300);
    expect(result.output_tokens).toBe(130);
    expect(result.total_tokens).toBe(430);
    expect(result.llm_calls).toBe(2);
  });

  it("reports the model of the last request and splits the counts by model", () => {
    const lines = [
      usageEntry({ model: "claude-sonnet-4-6", inputTokens: 10, outputTokens: 1 }),
      usageEntry({ model: "claude-opus-4-7", inputTokens: 20, outputTokens: 2 }),
      usageEntry({ model: "claude-sonnet-4-6", inputTokens: 30, outputTokens: 3 }),
      usageEntry({ model: "claude-opus-4-7", inputTokens: 40, outputTokens: 4, cacheRead: 500, cacheCreation: 60 }),
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscriptUsage(tmpPath);
    expect(result.scope).toBe("turn");
    expect(result.model).toBe("claude-opus-4-7");
    expect(result.input_tokens).toBe(100);
    expect(result.context_tokens).toBe(600); // last request: 40 + 500 + 60
    expect(result.models).toEqual([
      { model: "claude-sonnet-4-6", input_tokens: 40, output_tokens: 4, cache_creation_input_tokens: 0,
        cache_read_input_tokens: 0, total_tokens: 44, duration_ms: 0, llm_calls: 2 },
      { model: "claude-opus-4-7", input_tokens: 60, output_tokens: 6, cache_creation_input_tokens: 60,
        cache_read_input_tokens: 500, total_tokens: 66, duration_ms: 0, llm_calls: 2 },
    ]);
  });

  it("ignores user and system messages", () => {
    const lines = [
      JSON.stringify({ type: "user", timestamp: "2026-01-01T00:00:00.000Z", durationMs: 999,
        message: { role: "user", usage: { input_tokens: 999, output_tokens: 999 } } }),
      usageEntry({ inputTokens: 10, outputTokens: 5 }),
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscriptUsage(tmpPath);
    expect(result.input_tokens).toBe(10);
    expect(result.output_tokens).toBe(5);
    expect(result.llm_calls).toBe(1);
  });

  it("accumulates cache token fields", () => {
    writeFileSync(tmpPath, usageEntry({ inputTokens: 50, cacheCreation: 30, cacheRead: 20 }));
    const result = readTranscriptUsage(tmpPath);
    expect(result.cache_creation_input_tokens).toBe(30);
    expect(result.cache_read_input_tokens).toBe(20);
  });

  it("skips malformed JSON lines without throwing", () => {
    const lines = [
      usageEntry({ inputTokens: 10, outputTokens: 5 }),
      "this is not json {{{",
      usageEntry({ inputTokens: 20, outputTokens: 10 }),
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscriptUsage(tmpPath);
    expect(result.llm_calls).toBe(2);
    expect(result.total_tokens).toBe(45);
  });

  it("does not count assistant entries without usage, synthetic messages or request blobs", () => {
    const lines = [
      JSON.stringify({ type: "assistant", timestamp: "2026-01-01T00:00:00.000Z",
        message: { role: "assistant", model: "claude-haiku-4-5" } }),
      JSON.stringify({ type: "assistant", timestamp: "2026-01-01T00:00:00.000Z",
        message: { role: "assistant", model: "<synthetic>", usage: { input_tokens: 7, output_tokens: 7 } } }),
      JSON.stringify({ type: "api-request-blob",
        message: { role: "assistant", id: "msg_old", usage: { input_tokens: 9, output_tokens: 9 } } }),
    ].join("\n");
    writeFileSync(tmpPath, lines);
    const result = readTranscriptUsage(tmpPath);
    expect(result.llm_calls).toBe(0);
    expect(result.total_tokens).toBe(0);
  });
});

/** One transcript entry of an API response, as Claude Code writes one per content block. */
function block(id: string, opts: {
  ts: string; model?: string; input?: number; output?: number; cacheRead?: number;
  requestRef?: string; stopReason?: string | null; sidechain?: boolean;
}) {
  return JSON.stringify({
    type: "assistant",
    uuid: `${id}-${opts.ts}`,
    timestamp: opts.ts,
    requestId: `req_${id}`,
    requestRef: opts.requestRef,
    isSidechain: opts.sidechain ?? false,
    message: {
      id, role: "assistant", model: opts.model ?? "claude-fable-5-2",
      stop_reason: opts.stopReason ?? null,
      usage: { input_tokens: opts.input ?? 0, output_tokens: opts.output ?? 0,
        cache_read_input_tokens: opts.cacheRead ?? 0, cache_creation_input_tokens: 0 },
    },
  });
}
const apiRequest = (id: string, ts: string) => JSON.stringify({ type: "api-request", id, timestamp: ts });
const stopMarker = (subtype = "stop_hook_summary") => JSON.stringify({ type: "system", subtype });
const userPrompt = (text: string, promptId?: string) =>
  JSON.stringify({ type: "user", promptId, message: { role: "user", content: text } });

describe("readTranscriptCalls", () => {
  let tmpPath: string;

  beforeEach(() => {
    tmpPath = makeTmpPath();
  });

  afterEach(() => {
    try { unlinkSync(tmpPath); } catch { /* ignore */ }
  });

  it("returns an empty array for a missing file", () => {
    expect(readTranscriptCalls("/nonexistent/path/transcript.jsonl")).toEqual([]);
  });

  it("counts a response written as several entries once", () => {
    writeFileSync(tmpPath, [
      block("msg_1", { ts: "2026-01-01T00:00:01.000Z", input: 100, output: 40, cacheRead: 5000 }),
      block("msg_1", { ts: "2026-01-01T00:00:02.000Z", input: 100, output: 40, cacheRead: 5000 }),
      block("msg_1", { ts: "2026-01-01T00:00:03.000Z", input: 100, output: 40, cacheRead: 5000, stopReason: "tool_use" }),
      block("msg_2", { ts: "2026-01-01T00:00:09.000Z", input: 10, output: 4, stopReason: "end_turn" }),
    ].join("\n"));
    const calls = readTranscriptCalls(tmpPath);
    expect(calls.map((c) => c.message_id)).toEqual(["msg_1", "msg_2"]);
    expect(calls[0]).toMatchObject({
      request_id: "req_msg_1", model: "claude-fable-5-2", stop_reason: "tool_use",
      timestamp: "2026-01-01T00:00:03.000Z",
      input_tokens: 100, output_tokens: 40, cache_read_input_tokens: 5000,
    });
    const usage = readTranscriptUsage(tmpPath);
    expect(usage.llm_calls).toBe(2);
    expect(usage.cache_read_input_tokens).toBe(5000);
  });

  it("keeps the largest count when the entries of a response disagree", () => {
    writeFileSync(tmpPath, [
      block("msg_1", { ts: "2026-01-01T00:00:01.000Z", input: 100, output: 1 }),
      block("msg_1", { ts: "2026-01-01T00:00:02.000Z", input: 100, output: 250 }),
    ].join("\n"));
    expect(readTranscriptCalls(tmpPath)[0].output_tokens).toBe(250);
  });

  it("returns only the requests after the previous Stop", () => {
    writeFileSync(tmpPath, [
      userPrompt("first"),
      block("msg_1", { ts: "2026-01-01T00:00:01.000Z", input: 100, output: 40 }),
      stopMarker("stop_hook_summary"),
      stopMarker("turn_duration"),
      userPrompt("second"),
      block("msg_2", { ts: "2026-01-01T00:01:01.000Z", input: 7, output: 3 }),
      userPrompt("typed while the turn was running"),
      block("msg_3", { ts: "2026-01-01T00:01:05.000Z", input: 8, output: 2 }),
    ].join("\n"));
    expect(readTranscriptCalls(tmpPath).map((c) => c.message_id)).toEqual(["msg_2", "msg_3"]);
    expect(readTranscriptUsage(tmpPath)).toMatchObject({ llm_calls: 2, input_tokens: 15, output_tokens: 5 });
    expect(readTranscriptCalls(tmpPath, { sinceLastStop: false })).toHaveLength(3);
  });

  it("returns nothing when no request follows the previous Stop", () => {
    writeFileSync(tmpPath, [
      block("msg_1", { ts: "2026-01-01T00:00:01.000Z", input: 100, output: 40 }),
      stopMarker(),
    ].join("\n"));
    expect(readTranscriptCalls(tmpPath)).toEqual([]);
  });

  it("takes the request time from the api-request entry", () => {
    writeFileSync(tmpPath, [
      apiRequest("r1", "2026-01-01T00:00:00.500Z"),
      block("msg_1", { ts: "2026-01-01T00:00:01.000Z", requestRef: "r1" }),
      block("msg_1", { ts: "2026-01-01T00:00:03.000Z", requestRef: "r1" }),
      block("msg_2", { ts: "2026-01-01T00:00:09.000Z", requestRef: "unknown" }),
    ].join("\n"));
    const calls = readTranscriptCalls(tmpPath);
    expect(calls[0]).toMatchObject({ started_at: "2026-01-01T00:00:00.500Z", duration_ms: 2500 });
    expect(calls[1]).toMatchObject({ started_at: "", duration_ms: null });
    expect(readTranscriptUsage(tmpPath).duration_ms).toBe(2500);
  });

  it("gives each request the prompt id of its turn", () => {
    writeFileSync(tmpPath, [
      block("msg_0", { ts: "2026-01-01T00:00:00.000Z" }),
      userPrompt("first", "p1"),
      block("msg_1", { ts: "2026-01-01T00:00:01.000Z" }),
      JSON.stringify({ type: "user", promptId: "p1",
        message: { role: "user", content: [{ type: "tool_result", content: "ok" }] } }),
      block("msg_2", { ts: "2026-01-01T00:00:02.000Z" }),
      userPrompt("second, the first turn ended without a Stop", "p2"),
      block("msg_3", { ts: "2026-01-01T00:00:03.000Z" }),
    ].join("\n"));
    expect(readTranscriptCalls(tmpPath).map((c) => c.prompt_id)).toEqual(["", "p1", "p1", "p2"]);
  });

  it("leaves sidechain requests out of context_tokens", () => {
    writeFileSync(tmpPath, [
      block("msg_1", { ts: "2026-01-01T00:00:01.000Z", input: 10, cacheRead: 90 }),
      block("msg_2", { ts: "2026-01-01T00:00:02.000Z", input: 5, cacheRead: 1, sidechain: true }),
    ].join("\n"));
    const usage = readTranscriptUsage(tmpPath);
    expect(usage.llm_calls).toBe(2);
    expect(usage.context_tokens).toBe(100);
  });
});
