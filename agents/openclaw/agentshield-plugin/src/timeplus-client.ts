import type { AgentShieldConfig } from "./config.js";

export type HookEvent = {
  hook_name: string;
  event_time: string;
  session_id: string;
  run_id: string;
  agent_id: string;
  session_key: string;
  tool_name: string;
  provider: string;
  model: string;
  hook_decision: string;
  block_reason: string;
  event_data: string;
};

export type TimeplusClient = {
  enqueue: (event: HookEvent) => void;
  flush: () => Promise<void>;
  close: () => Promise<void>;
};

const COLUMNS: (keyof HookEvent)[] = [
  "hook_name", "event_time", "session_id", "run_id", "agent_id",
  "session_key", "tool_name", "provider", "model",
  "hook_decision", "block_reason", "event_data",
];

async function postBatch(
  url: string,
  batch: HookEvent[],
): Promise<void> {
  const body = JSON.stringify({
    columns: COLUMNS,
    data: batch.map((e) => COLUMNS.map((col) => e[col])),
  });
  const resp = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body,
  });
  if (!resp.ok) {
    throw new Error(`Timeplus ingest failed: ${resp.status}`);
  }
}

export function createTimeplusClient(config: AgentShieldConfig): TimeplusClient {
  const url = `${config.timeplusUrl}/proton/v1/ingest/streams/${config.stream}`;
  const queue: HookEvent[] = [];
  let timer: ReturnType<typeof setInterval> | null = null;

  async function flush(): Promise<void> {
    if (queue.length === 0) return;
    const batch = queue.splice(0, queue.length);
    try {
      await postBatch(url, batch);
    } catch {
      // retry once immediately, then drop the batch
      try {
        await postBatch(url, batch);
      } catch (err) {
        console.error("[agentshield] Failed to flush batch, dropping:", err);
      }
    }
  }

  timer = setInterval(() => {
    void flush();
  }, config.flushMs);

  function enqueue(event: HookEvent): void {
    queue.push(event);
    if (queue.length >= config.batchSize) {
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
