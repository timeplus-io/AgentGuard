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
  agent_type: string;
};

const COLUMNS: (keyof HookEvent)[] = [
  "hook_name", "event_time", "session_id", "run_id", "agent_id",
  "deployment_id", "deployment_name", "session_key", "tool_name",
  "provider", "model", "hook_decision", "block_reason", "event_data", "agent_type",
];

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

/**
 * Basic auth with the Timeplus username/password in both modes — the REST API
 * validates the credentials against timeplusd, so no separate key is needed.
 */
export function authHeaders(config: AgentGuardConfig): Record<string, string> {
  return { Authorization: `Basic ${btoa(`${config.username}:${config.password}`)}` };
}

// Rows per request. A turn rarely has more than a few dozen model requests;
// the cap only matters for a transcript read from the start.
const MAX_BATCH_ROWS = 500;

export async function send(event: HookEvent, config: AgentGuardConfig): Promise<void> {
  await sendBatch([event], config);
}

/** Sends several events, in as few requests as MAX_BATCH_ROWS allows. */
export async function sendBatch(events: HookEvent[], config: AgentGuardConfig): Promise<void> {
  for (let i = 0; i < events.length; i += MAX_BATCH_ROWS) {
    await post(events.slice(i, i + MAX_BATCH_ROWS), config);
  }
}

async function post(events: HookEvent[], config: AgentGuardConfig): Promise<void> {
  const url = buildIngestUrl(config);
  const body = JSON.stringify({
    columns: COLUMNS,
    data: events.map((event) => COLUMNS.map((col) => event[col])),
  });
  try {
    const resp = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...authHeaders(config),
      },
      body,
    });
    if (!resp.ok) {
      process.stderr.write(`[agentguard] ingest failed: ${resp.status}\n`);
    }
  } catch (err) {
    process.stderr.write(`[agentguard] network error: ${String(err)}\n`);
  }
}
