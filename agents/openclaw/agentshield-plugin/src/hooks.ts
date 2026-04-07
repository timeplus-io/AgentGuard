import type { OpenClawPluginApi } from "openclaw/plugin-sdk";
import type { HookEvent, TimeplusClient } from "./timeplus-client.js";

// Minimal structural type covering the optional identity fields common across
// most hook contexts. Contexts that don't share these fields are cast via toBase().
type BaseCtx = {
  agentId?: string;
  sessionKey?: string;
  sessionId?: string;
  runId?: string;
};

// Safe cast helper — avoids repetitive `as unknown as BaseCtx` at every call site.
function toBase(ctx: unknown): BaseCtx {
  return ctx as unknown as BaseCtx;
}

export function baseEvent(hookName: string, ctx: BaseCtx): HookEvent {
  return {
    hook_name: hookName,
    event_time: new Date().toISOString(),
    session_id: ctx.sessionId ?? "",
    run_id: ctx.runId ?? "",
    agent_id: ctx.agentId ?? "",
    session_key: ctx.sessionKey ?? "",
    tool_name: "",
    provider: "",
    model: "",
    hook_decision: "observe",
    block_reason: "",
    event_data: "",
  };
}

export function registerAllHooks(
  api: OpenClawPluginApi,
  client: TimeplusClient,
): void {
  // ── Agent lifecycle ──────────────────────────────────────────────────────

  api.on("before_model_resolve", (event, ctx) => {
    const base = baseEvent("before_model_resolve", ctx);
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("before_prompt_build", (event, ctx) => {
    const base = baseEvent("before_prompt_build", ctx);
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("before_agent_start", (event, ctx) => {
    const base = baseEvent("before_agent_start", ctx);
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("before_agent_reply", (event, ctx) => {
    const base = baseEvent("before_agent_reply", ctx);
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  // llm_input / llm_output: event carries provider, model, runId, sessionId
  api.on("llm_input", (event, ctx) => {
    const base = baseEvent("llm_input", ctx);
    client.enqueue({
      ...base,
      run_id: event.runId,
      session_id: event.sessionId,
      provider: event.provider,
      model: event.model,
      event_data: JSON.stringify(event),
    });
  });

  api.on("llm_output", (event, ctx) => {
    const base = baseEvent("llm_output", ctx);
    client.enqueue({
      ...base,
      run_id: event.runId,
      session_id: event.sessionId,
      provider: event.provider,
      model: event.model,
      event_data: JSON.stringify(event),
    });
  });

  api.on("agent_end", (event, ctx) => {
    const base = baseEvent("agent_end", ctx);
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("before_compaction", (event, ctx) => {
    const base = baseEvent("before_compaction", ctx);
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("after_compaction", (event, ctx) => {
    const base = baseEvent("after_compaction", ctx);
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("before_reset", (event, ctx) => {
    const base = baseEvent("before_reset", ctx);
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  // ── Tool hooks ───────────────────────────────────────────────────────────

  api.on("before_tool_call", (event, ctx) => {
    const base = baseEvent("before_tool_call", ctx);
    client.enqueue({
      ...base,
      tool_name: ctx.toolName,
      event_data: JSON.stringify(event),
    });
  });

  api.on("after_tool_call", (event, ctx) => {
    const base = baseEvent("after_tool_call", ctx);
    client.enqueue({
      ...base,
      tool_name: ctx.toolName,
      event_data: JSON.stringify(event),
    });
  });

  api.on("tool_result_persist", (event, ctx) => {
    // PluginHookToolResultPersistContext: agentId?, sessionKey?, toolName?, toolCallId?
    const base = baseEvent("tool_result_persist", toBase(ctx));
    client.enqueue({
      ...base,
      tool_name: ctx.toolName ?? "",
      event_data: JSON.stringify(event),
    });
  });

  // ── Message write ────────────────────────────────────────────────────────

  api.on("before_message_write", (event, ctx) => {
    // ctx: { agentId?: string; sessionKey?: string } — subset of BaseCtx
    const base = baseEvent("before_message_write", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  // ── Session lifecycle ────────────────────────────────────────────────────

  api.on("session_start", (event, ctx) => {
    const base = baseEvent("session_start", ctx);
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("session_end", (event, ctx) => {
    const base = baseEvent("session_end", ctx);
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  // ── Message hooks ────────────────────────────────────────────────────────
  // PluginHookMessageContext: { channelId, accountId?, conversationId? }
  // No agentId/sessionKey/sessionId — cast via toBase()

  api.on("message_received", (event, ctx) => {
    const base = baseEvent("message_received", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("message_sending", (event, ctx) => {
    const base = baseEvent("message_sending", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("message_sent", (event, ctx) => {
    const base = baseEvent("message_sent", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  // ── Inbound / dispatch ───────────────────────────────────────────────────

  api.on("inbound_claim", (event, ctx) => {
    const base = baseEvent("inbound_claim", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("before_dispatch", (event, ctx) => {
    // PluginHookBeforeDispatchContext has sessionKey? but no agentId/sessionId
    const base = baseEvent("before_dispatch", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("reply_dispatch", (event, ctx) => {
    // PluginHookReplyDispatchContext: cfg, dispatcher, abortSignal, etc. — no identity fields
    const base = baseEvent("reply_dispatch", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  // ── Subagent hooks ───────────────────────────────────────────────────────
  // PluginHookSubagentContext: { runId?, childSessionKey?, requesterSessionKey? }

  api.on("subagent_spawning", (event, ctx) => {
    const base = baseEvent("subagent_spawning", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("subagent_delivery_target", (event, ctx) => {
    const base = baseEvent("subagent_delivery_target", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("subagent_spawned", (event, ctx) => {
    const base = baseEvent("subagent_spawned", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("subagent_ended", (event, ctx) => {
    const base = baseEvent("subagent_ended", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  // ── Gateway hooks ────────────────────────────────────────────────────────
  // PluginHookGatewayContext: { port? } — no identity fields

  api.on("gateway_start", (event, ctx) => {
    const base = baseEvent("gateway_start", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });

  api.on("gateway_stop", async (event, ctx) => {
    const base = baseEvent("gateway_stop", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
    await client.close();
  });

  // ── Install hook ─────────────────────────────────────────────────────────
  // PluginHookBeforeInstallContext: { targetType, requestKind, origin? }

  api.on("before_install", (event, ctx) => {
    const base = baseEvent("before_install", toBase(ctx));
    client.enqueue({ ...base, event_data: JSON.stringify(event) });
  });
}
