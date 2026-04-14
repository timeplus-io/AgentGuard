import { readFileSync } from "fs";
import { homedir } from "os";

const MAX_MESSAGES = parseInt(process.env.AGENTGUARD_TRANSCRIPT_MESSAGES ?? "50", 10);
const MAX_CONTENT_CHARS = parseInt(process.env.AGENTGUARD_TRANSCRIPT_CONTENT_CHARS ?? "10000", 10);

export type TranscriptMessage = {
  role: string;
  timestamp: string;
  content: unknown;
};

/** Aggregate token usage and timing across all LLM API calls in a transcript. */
export type TranscriptUsage = {
  input_tokens: number;
  output_tokens: number;
  cache_creation_input_tokens: number;
  cache_read_input_tokens: number;
  total_tokens: number;
  duration_ms: number;
  model: string;
  llm_calls: number;
};

function toNum(v: unknown): number {
  return typeof v === "number" ? v : 0;
}

function resolvePath(p: string): string {
  return p.startsWith("~/") ? p.replace("~", homedir()) : p;
}

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
    const resolvedPath = resolvePath(transcriptPath);
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

/**
 * Reads a Claude Code transcript JSONL file and accumulates token usage across
 * all assistant messages (each assistant message = one LLM API call).
 * Returns zero-value struct if the file cannot be read or contains no assistant messages.
 */
export function readTranscriptUsage(transcriptPath: string): TranscriptUsage {
  const result: TranscriptUsage = {
    input_tokens: 0,
    output_tokens: 0,
    cache_creation_input_tokens: 0,
    cache_read_input_tokens: 0,
    total_tokens: 0,
    duration_ms: 0,
    model: "",
    llm_calls: 0,
  };
  try {
    const raw = readFileSync(resolvePath(transcriptPath), "utf8");
    for (const line of raw.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        const entry = JSON.parse(trimmed) as Record<string, unknown>;
        const msg = entry["message"] as Record<string, unknown> | undefined;
        if (!msg || msg["role"] !== "assistant") continue;

        result.llm_calls++;
        result.duration_ms += toNum(entry["durationMs"]);

        const usage = msg["usage"] as Record<string, unknown> | undefined;
        if (usage) {
          result.input_tokens += toNum(usage["input_tokens"]);
          result.output_tokens += toNum(usage["output_tokens"]);
          result.cache_creation_input_tokens += toNum(usage["cache_creation_input_tokens"]);
          result.cache_read_input_tokens += toNum(usage["cache_read_input_tokens"]);
        }

        // Use the model from the first assistant message that has one.
        if (!result.model && typeof msg["model"] === "string" && msg["model"]) {
          result.model = msg["model"];
        }
      } catch {
        // skip malformed lines
      }
    }
    result.total_tokens = result.input_tokens + result.output_tokens;
  } catch {
    // file unreadable — return zero struct
  }
  return result;
}
