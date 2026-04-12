import type { AgentShieldConfig } from "./config.js";

export type HookEvent = {
  hook_name: string;
  event_time: string;
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

export async function send(event: HookEvent, config: AgentShieldConfig): Promise<void> {
  const url = `${config.timeplusUrl}/proton/v1/ingest/streams/${config.stream}`;
  const authHeader = `Basic ${btoa(`${config.username}:${config.password}`)}`;
  const body = JSON.stringify({
    columns: COLUMNS,
    data: [COLUMNS.map((col) => event[col])],
  });
  try {
    const resp = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "Authorization": authHeader,
      },
      body,
    });
    if (!resp.ok) {
      process.stderr.write(`[agentshield] ingest failed: ${resp.status}\n`);
    }
  } catch (err) {
    process.stderr.write(`[agentshield] network error: ${String(err)}\n`);
  }
}
