import type { AgentGuardConfig } from "./config.js";
import type { HookEvent } from "./timeplus-client.js";

export type HoldDecision = {
  decision: "allow" | "block";
  reason?: string;
  hold_id?: string;
};

const HOLD_TIMEOUT_MS = 540_000; // under Claude Code's 600s hook timeout

export async function requestHold(event: HookEvent, config: AgentGuardConfig): Promise<HoldDecision> {
  const url = `${config.agentguardUrl}/api/holds`;
  let hookPayload: unknown = {};
  try {
    hookPayload = JSON.parse(event.event_data);
  } catch {
    hookPayload = {};
  }
  const body = JSON.stringify({
    deployment_id: event.deployment_id,
    agent_id:      event.agent_id,
    session_id:    event.session_id,
    run_id:        event.run_id,
    tool_name:     event.tool_name,
    event_time:    event.event_time,
    tool_input:    (hookPayload as { tool_input?: unknown }).tool_input ?? {},
    hook_payload:  hookPayload,
  });
  try {
    const ctrl = new AbortController();
    const t = setTimeout(() => ctrl.abort(), HOLD_TIMEOUT_MS);
    const resp = await fetch(url, {
      method:  "POST",
      headers: { "Content-Type": "application/json" },
      body,
      signal:  ctrl.signal,
    });
    clearTimeout(t);
    if (!resp.ok) {
      // Do not block on HTTP errors
      return { decision: "allow" };
    }
    return await resp.json() as HoldDecision;
  } catch (err) {
    // Network/connectivity failure: don't block the agent
    return { decision: "allow" };
  }
}

function failPolicy(config: AgentGuardConfig, reason: string): HoldDecision {
  if (config.holdFailPolicy === "allow") {
    return { decision: "allow" };
  }
  return { decision: "block", reason: `AgentGuard unreachable — ${reason}` };
}
