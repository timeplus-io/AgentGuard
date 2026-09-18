import type { AgentGuardConfig } from "./config.js";

export type HookEvent = {
  hook_name: string;
  event_time: number; // epoch milliseconds — avoids server-timezone ambiguity of ISO strings
  session_id: string;
  run_id: string;
  agent_id: string;
  deployment_id: string;
  deployment_name: string;
  session_key: string;
  tool_name: string;
  provider: string;
  model: string;
  hook_decision: string;
  block_reason: string;
  event_data: string;
  agent_type: string;   // "openclaw" | "" for unknown/legacy
};

export type TimeplusClient = {
  enqueue: (event: HookEvent) => void;
  flush: () => Promise<void>;
  close: () => Promise<void>;
};

const COLUMNS: (keyof HookEvent)[] = [
  "hook_name", "event_time", "session_id", "run_id", "agent_id",
  "deployment_id", "deployment_name",
  "session_key", "tool_name", "provider", "model",
  "hook_decision", "block_reason", "event_data", "agent_type",
];

// Per-event event_data size cap. Large LLM responses (multi-MB Kimi/Claude
// outputs) routinely exceed Timeplus's HTTP body limit when batched, killing
// the whole batch with `SocketError: other side closed`. Truncate each event
// individually so one big response can't take down the rest of the batch.
// 256 KB is enough for typical chat turns + tool outputs; trimmed payloads
// keep a [...truncated N→M bytes] marker so the loss is visible downstream.
const EVENT_DATA_MAX_BYTES = 256 * 1024;

function capEventData(s: string): string {
  if (s.length <= EVENT_DATA_MAX_BYTES) return s;
  const head = s.slice(0, EVENT_DATA_MAX_BYTES);
  return `${head}...[truncated ${s.length}→${EVENT_DATA_MAX_BYTES} bytes]`;
}

/**
 * Ingest endpoint for the configured API surface.
 *  timeplusd: {url}/timeplusd/v2/ingest/{db}/streams/{stream}
 *  rest:      {url}/{workspace}/api/v1beta2/streams/{stream}/ingest?db={db}
 * Both accept the same compact `{columns, data}` body.
 */
export function buildIngestUrl(config: AgentGuardConfig): string {
  const base = config.timeplusUrl.replace(/\/+$/, "");
  if (config.ingestApi === "rest") {
    return `${base}/${config.workspace}/api/v1beta2/streams/${config.stream}/ingest?db=${config.database}`;
  }
  return `${base}/timeplusd/v2/ingest/${config.database}/streams/${config.stream}`;
}

/** API key (REST API only) when configured, otherwise Basic auth. */
export function authHeaders(config: AgentGuardConfig): Record<string, string> {
  if (config.ingestApi === "rest" && config.apiKey) {
    return { "X-Api-Key": config.apiKey };
  }
  return { Authorization: `Basic ${btoa(`${config.username}:${config.password}`)}` };
}

async function postBatch(
  url: string,
  headers: Record<string, string>,
  batch: HookEvent[],
): Promise<void> {
  const body = JSON.stringify({
    columns: COLUMNS,
    data: batch.map((e) => COLUMNS.map((col) => e[col])),
  });
  const resp = await fetch(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...headers,
    },
    body,
  });
  if (!resp.ok) {
    throw new Error(`Timeplus ingest failed: ${resp.status}`);
  }
}

export function createTimeplusClient(config: AgentGuardConfig): TimeplusClient {
  const url = buildIngestUrl(config);
  const headers = authHeaders(config);
  const queue: HookEvent[] = [];
  let timer: ReturnType<typeof setInterval> | null = null;

  async function flush(): Promise<void> {
    if (queue.length === 0) return;
    // Splice synchronously before the first await so any concurrent flush()
    // call that enters after this point sees an empty queue and returns early,
    // preventing the same events from being sent twice.
    const batch = queue.splice(0, queue.length);
    try {
      await postBatch(url, headers, batch);
    } catch {
      // retry once immediately, then drop the batch
      try {
        await postBatch(url, headers, batch);
      } catch (err) {
        console.error("[agentguard] Failed to flush batch, dropping:", err);
      }
    }
  }

  timer = setInterval(() => {
    void flush();
  }, config.flushMs);
  timer.unref();

  function enqueue(event: HookEvent): void {
    // Cap event_data BEFORE queueing — truncation must happen here, not in
    // postBatch, so a single oversize event can't bloat the JSON body even if
    // batchSize=1.
    if (event.event_data && event.event_data.length > EVENT_DATA_MAX_BYTES) {
      event = { ...event, event_data: capEventData(event.event_data) };
    }
    queue.push(event);
    if (queue.length >= config.batchSize) {
      // Deliberate tradeoff: if a flush is already in-flight, its splice has
      // already drained the queue so this call returns early. New events will
      // be picked up on the next timer tick — acceptable for low-to-moderate volume.
      void flush();
    }
  }

  async function close(): Promise<void> {
    if (timer !== null) {
      clearInterval(timer);
      timer = null;
    }
    await flush();
  }

  return { enqueue, flush, close };
}
