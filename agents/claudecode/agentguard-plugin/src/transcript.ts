import { readFileSync } from "fs";
import { homedir } from "os";

const MAX_MESSAGES = parseInt(process.env.AGENTGUARD_TRANSCRIPT_MESSAGES ?? "50", 10);
const MAX_CONTENT_CHARS = parseInt(process.env.AGENTGUARD_TRANSCRIPT_CONTENT_CHARS ?? "10000", 10);

export type TranscriptMessage = {
  role: string;
  timestamp: string;
  content: unknown;
};

/** Token counts as the Anthropic API reports them: input_tokens excludes cached tokens. */
type TokenCounts = {
  input_tokens: number;
  output_tokens: number;
  cache_creation_input_tokens: number;
  cache_read_input_tokens: number;
};

/** One model request (one API response), rebuilt from the transcript. */
export type LlmCall = TokenCounts & {
  message_id: string;
  request_id: string;
  model: string;
  stop_reason: string;
  /** ISO time of the last transcript entry of the response. */
  timestamp: string;
  /** ISO time the request was sent; "" when the transcript does not record it. */
  started_at: string;
  /** Request-to-response time; null when the transcript does not record the request. */
  duration_ms: number | null;
  is_sidechain: boolean;
};

export type ModelUsage = TokenCounts & {
  model: string;
  total_tokens: number;
  duration_ms: number;
  llm_calls: number;
};

/** Token usage of one turn: the model requests since the previous Stop. */
export type TranscriptUsage = TokenCounts & {
  scope: "turn";
  total_tokens: number;
  duration_ms: number;
  /** Model of the last request of the turn. */
  model: string;
  llm_calls: number;
  /** Prompt size of the last main-thread request: input + cache read + cache write. */
  context_tokens: number;
  /** The same counts split by model, in order of first use. */
  models: ModelUsage[];
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

const TOKEN_FIELDS = [
  "input_tokens",
  "output_tokens",
  "cache_creation_input_tokens",
  "cache_read_input_tokens",
] as const;

// Claude Code writes these after the Stop hooks of a turn have run, so the
// entries after the last one belong to the turn that is ending now.
const TURN_END_SUBTYPES = new Set(["stop_hook_summary", "turn_duration"]);

function toStr(v: unknown): string {
  return typeof v === "string" ? v : "";
}

/**
 * Reads a Claude Code transcript JSONL file and returns its model requests,
 * oldest first. With `sinceLastStop` (the default) only the requests after the
 * last end-of-turn marker are returned, i.e. the turn that is ending; a
 * transcript without markers is read from the start.
 *
 * Claude Code writes one assistant entry per content block, and they all repeat
 * the `message.id` and `usage` of the response, so entries are merged by
 * `message.id`. The request time comes from the `api-request` entry that the
 * assistant entry names in `requestRef`.
 *
 * Returns an empty array if the file cannot be read.
 */
export function readTranscriptCalls(
  transcriptPath: string,
  opts: { sinceLastStop?: boolean } = {},
): LlmCall[] {
  const sinceLastStop = opts.sinceLastStop ?? true;
  const requestTimes = new Map<string, string>();
  const requestRefs = new Map<string, string>();
  let calls = new Map<string, LlmCall>();
  try {
    const raw = readFileSync(resolvePath(transcriptPath), "utf8");
    for (const line of raw.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      try {
        const entry = JSON.parse(trimmed) as Record<string, unknown>;
        const type = entry["type"];

        if (type === "api-request") {
          const id = toStr(entry["id"]);
          if (id) requestTimes.set(id, toStr(entry["timestamp"]));
          continue;
        }
        if (type === "system" && TURN_END_SUBTYPES.has(toStr(entry["subtype"]))) {
          if (sinceLastStop) calls = new Map();
          continue;
        }
        if (type !== "assistant") continue;

        const msg = entry["message"] as Record<string, unknown> | undefined;
        if (!msg || msg["role"] !== "assistant") continue;
        const usage = msg["usage"] as Record<string, unknown> | undefined;
        const model = toStr(msg["model"]);
        // Messages Claude Code makes up itself (API errors, interrupts) are not requests.
        if (!usage || model === "<synthetic>") continue;

        const key = toStr(msg["id"]) || toStr(entry["uuid"]) || `line-${calls.size}`;
        let call = calls.get(key);
        if (!call) {
          call = {
            message_id: toStr(msg["id"]),
            request_id: toStr(entry["requestId"]),
            model,
            stop_reason: "",
            timestamp: "",
            started_at: "",
            duration_ms: null,
            is_sidechain: entry["isSidechain"] === true,
            input_tokens: 0,
            output_tokens: 0,
            cache_creation_input_tokens: 0,
            cache_read_input_tokens: 0,
          };
          calls.set(key, call);
        }
        // Repeated entries normally carry the same usage; take the largest
        // value in case an early entry was written before the final count.
        for (const f of TOKEN_FIELDS) call[f] = Math.max(call[f], toNum(usage[f]));
        if (!call.model) call.model = model;
        if (toStr(msg["stop_reason"])) call.stop_reason = toStr(msg["stop_reason"]);
        const ts = toStr(entry["timestamp"]);
        if (ts > call.timestamp) call.timestamp = ts;
        const ref = toStr(entry["requestRef"]);
        if (ref) requestRefs.set(key, ref);
      } catch {
        // skip malformed lines
      }
    }
  } catch {
    return [];
  }

  for (const [key, call] of calls) {
    const startedAt = requestTimes.get(requestRefs.get(key) ?? "") ?? "";
    const ms = Date.parse(call.timestamp) - Date.parse(startedAt);
    if (startedAt && Number.isFinite(ms) && ms >= 0) {
      call.started_at = startedAt;
      call.duration_ms = ms;
    }
  }
  return [...calls.values()];
}

/** Sums model requests into the usage of one turn, in total and per model. */
export function summarizeCalls(calls: LlmCall[]): TranscriptUsage {
  const result: TranscriptUsage = {
    scope: "turn",
    input_tokens: 0,
    output_tokens: 0,
    cache_creation_input_tokens: 0,
    cache_read_input_tokens: 0,
    total_tokens: 0,
    duration_ms: 0,
    model: "",
    llm_calls: 0,
    context_tokens: 0,
    models: [],
  };
  const byModel = new Map<string, ModelUsage>();
  for (const call of calls) {
    let m = byModel.get(call.model);
    if (!m) {
      m = {
        model: call.model,
        input_tokens: 0,
        output_tokens: 0,
        cache_creation_input_tokens: 0,
        cache_read_input_tokens: 0,
        total_tokens: 0,
        duration_ms: 0,
        llm_calls: 0,
      };
      byModel.set(call.model, m);
    }
    for (const target of [result, m]) {
      for (const f of TOKEN_FIELDS) target[f] += call[f];
      target.total_tokens += call.input_tokens + call.output_tokens;
      target.duration_ms += call.duration_ms ?? 0;
      target.llm_calls++;
    }
    if (call.model) result.model = call.model;
    if (!call.is_sidechain) {
      result.context_tokens =
        call.input_tokens + call.cache_read_input_tokens + call.cache_creation_input_tokens;
    }
  }
  result.models = [...byModel.values()];
  return result;
}

/**
 * Token usage of the turn that is ending: the model requests since the previous
 * Stop, counted once each. Returns a zero-value struct if the file cannot be
 * read or the turn made no model request.
 */
export function readTranscriptUsage(transcriptPath: string): TranscriptUsage {
  return summarizeCalls(readTranscriptCalls(transcriptPath));
}
