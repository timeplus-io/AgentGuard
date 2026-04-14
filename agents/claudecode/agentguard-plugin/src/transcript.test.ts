import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { writeFileSync, unlinkSync, mkdirSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { readTranscript } from "./transcript.js";

function makeTmpPath(): string {
  const dir = join(tmpdir(), "agentguard-test");
  mkdirSync(dir, { recursive: true });
  return join(dir, `transcript-${Date.now()}.jsonl`);
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
