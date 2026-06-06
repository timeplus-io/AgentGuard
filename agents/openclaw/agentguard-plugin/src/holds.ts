import type { AgentGuardConfig } from "./config.js";
import type { HookEvent } from "./timeplus-client.js";

export type HoldDecision = {
  decision: "allow" | "block";
  reason?: string;
  hold_id?: string;
};

const HOLD_TIMEOUT_MS = 540_000; // under any sane hook timeout

export async function requestHold(
  event: HookEvent,
  toolInput: unknown,
  config: AgentGuardConfig,
): Promise<HoldDecision> {
  const url = `${config.agentguardUrl}/api/holds`;
  const body = JSON.stringify({
    deployment_id:   event.deployment_id,
    deployment_name: event.deployment_name,
    agent_id:        event.agent_id,
    agent_type:      event.agent_type,  // "openclaw"
    session_id:      event.session_id,
    run_id:          event.run_id,
    tool_name:       event.tool_name,
    event_time:      event.event_time,
    tool_input:      toolInput ?? {},
    hook_payload:    safeJsonParse(event.event_data),
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
      // Do not block if HTTP request failed
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

function safeJsonParse(s: string): unknown {
  try { return JSON.parse(s); } catch { return {}; }
}
