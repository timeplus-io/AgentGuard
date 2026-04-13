import { readFileSync } from "fs";
import { homedir } from "os";

const MAX_MESSAGES = parseInt(process.env.AGENTSHIELD_TRANSCRIPT_MESSAGES ?? "50", 10);
const MAX_CONTENT_CHARS = parseInt(process.env.AGENTSHIELD_TRANSCRIPT_CONTENT_CHARS ?? "10000", 10);

export type TranscriptMessage = {
  role: string;
  timestamp: string;
  content: unknown;
};

function truncateContent(content: unknown): unknown {
  if (typeof content === "string") {
    return content.length > MAX_CONTENT_CHARS
      ? content.slice(0, MAX_CONTENT_CHARS) + `...[truncated ${content.length - MAX_CONTENT_CHARS} chars]`
      : content;
  }
  if (Array.isArray(content)) {
    return content.map((block) => {
      if (typeof block !== "object" || block === null) return block;
      const b = block as Record<string, unknown>;
      if (typeof b["text"] === "string") {
        return { ...b, text: truncateContent(b["text"]) };
      }
      if (Array.isArray(b["content"])) {
        return { ...b, content: truncateContent(b["content"]) };
      }
      return b;
    });
  }
  return content;
}

/**
 * Reads a Claude Code transcript JSONL file and returns the last MAX_MESSAGES messages.
 * Each message contains the role, timestamp, and (truncated) content blocks.
 * Returns an empty array if the file cannot be read or parsed.
 */
export function readTranscript(transcriptPath: string): TranscriptMessage[] {
  try {
    const resolvedPath = transcriptPath.startsWith("~/")
      ? transcriptPath.replace("~", homedir())
      : transcriptPath;

    const raw = readFileSync(resolvedPath, "utf8");
    const messages: TranscriptMessage[] = [];

    for (const line of raw.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        const entry = JSON.parse(trimmed) as Record<string, unknown>;
        const msg = entry["message"] as Record<string, unknown> | undefined;
        if (!msg) continue;
        messages.push({
          role: typeof msg["role"] === "string"
            ? msg["role"]
            : String(entry["type"] ?? "unknown"),
          timestamp: typeof entry["timestamp"] === "string" ? entry["timestamp"] : "",
          content: truncateContent(msg["content"]),
        });
      } catch {
        // skip malformed lines
      }
    }

    return messages.slice(-MAX_MESSAGES);
  } catch {
    return [];
  }
}
