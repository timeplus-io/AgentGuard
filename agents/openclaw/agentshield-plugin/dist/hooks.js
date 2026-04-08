// Safe cast helper — avoids repetitive `as unknown as BaseCtx` at every call site.
function toBase(ctx) {
    return ctx;
}
// Defensive JSON serializer — prevents a circular reference or BigInt in any hook
// event from throwing synchronously inside the handler callback, which could
// surface as an error in the OpenClaw agent runtime.
function safeStringify(value) {
    try {
        return JSON.stringify(value);
    }
    catch {
        return JSON.stringify({ _error: "non-serializable event" });
    }
}
export function baseEvent(hookName, ctx, config) {
    return {
        hook_name: hookName,
        event_time: new Date().toISOString(),
        session_id: ctx.sessionId ?? "",
        run_id: ctx.runId ?? "",
        agent_id: ctx.agentId ?? "",
        deployment_id: config.deploymentId,
        deployment_name: config.deploymentName,
        session_key: ctx.sessionKey ?? "",
        tool_name: "",
        provider: "",
        model: "",
        hook_decision: "observe",
        block_reason: "",
        event_data: "",
    };
}
export function registerAllHooks(api, client, config) {
    // ── Agent lifecycle ──────────────────────────────────────────────────────
    api.on("before_model_resolve", (event, ctx) => {
        const base = baseEvent("before_model_resolve", ctx, config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("before_prompt_build", (event, ctx) => {
        const base = baseEvent("before_prompt_build", ctx, config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("before_agent_start", (event, ctx) => {
        const base = baseEvent("before_agent_start", ctx, config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("before_agent_reply", (event, ctx) => {
        const base = baseEvent("before_agent_reply", ctx, config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    // llm_input / llm_output: event carries provider, model, runId, sessionId
    api.on("llm_input", (event, ctx) => {
        const base = baseEvent("llm_input", ctx, config);
        client.enqueue({
            ...base,
            run_id: event.runId ?? base.run_id,
            provider: event.provider,
            model: event.model,
            event_data: safeStringify(event),
        });
    });
    api.on("llm_output", async (event, ctx) => {
        const base = baseEvent("llm_output", ctx, config);
        client.enqueue({
            ...base,
            run_id: event.runId ?? base.run_id,
            provider: event.provider,
            model: event.model,
            event_data: safeStringify(event),
        });
        // llm_output is the terminal hook of every agent turn — flush immediately
        // so events appear in Timeplus without waiting for the periodic timer.
        await client.flush();
    });
    api.on("agent_end", (event, ctx) => {
        const base = baseEvent("agent_end", ctx, config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("before_compaction", (event, ctx) => {
        const base = baseEvent("before_compaction", ctx, config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("after_compaction", (event, ctx) => {
        const base = baseEvent("after_compaction", ctx, config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("before_reset", (event, ctx) => {
        const base = baseEvent("before_reset", ctx, config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    // ── Tool hooks ───────────────────────────────────────────────────────────
    api.on("before_tool_call", (event, ctx) => {
        const base = baseEvent("before_tool_call", ctx, config);
        client.enqueue({
            ...base,
            tool_name: ctx.toolName,
            event_data: safeStringify(event),
        });
    });
    api.on("after_tool_call", (event, ctx) => {
        const base = baseEvent("after_tool_call", ctx, config);
        client.enqueue({
            ...base,
            tool_name: ctx.toolName,
            event_data: safeStringify(event),
        });
    });
    api.on("tool_result_persist", (event, ctx) => {
        // PluginHookToolResultPersistContext: agentId?, sessionKey?, toolName?, toolCallId?
        const base = baseEvent("tool_result_persist", toBase(ctx), config);
        client.enqueue({
            ...base,
            tool_name: ctx.toolName ?? "",
            event_data: safeStringify(event),
        });
    });
    // ── Message write ────────────────────────────────────────────────────────
    api.on("before_message_write", (event, ctx) => {
        // ctx: { agentId?: string; sessionKey?: string } — subset of BaseCtx
        const base = baseEvent("before_message_write", toBase(ctx), config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    // ── Session lifecycle ────────────────────────────────────────────────────
    api.on("session_start", (event, ctx) => {
        const base = baseEvent("session_start", ctx, config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("session_end", (event, ctx) => {
        const base = baseEvent("session_end", ctx, config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    // ── Message hooks ────────────────────────────────────────────────────────
    // PluginHookMessageContext: { channelId, accountId?, conversationId? }
    // No agentId/sessionKey/sessionId — cast via toBase()
    api.on("message_received", (event, ctx) => {
        const base = baseEvent("message_received", toBase(ctx), config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("message_sending", (event, ctx) => {
        const base = baseEvent("message_sending", toBase(ctx), config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("message_sent", (event, ctx) => {
        const base = baseEvent("message_sent", toBase(ctx), config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    // ── Inbound / dispatch ───────────────────────────────────────────────────
    api.on("inbound_claim", (event, ctx) => {
        const base = baseEvent("inbound_claim", toBase(ctx), config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("before_dispatch", (event, ctx) => {
        // PluginHookBeforeDispatchContext has sessionKey? but no agentId/sessionId
        const base = baseEvent("before_dispatch", toBase(ctx), config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("reply_dispatch", (event, ctx) => {
        // PluginHookReplyDispatchContext: cfg, dispatcher, abortSignal, etc. — no identity fields
        const base = baseEvent("reply_dispatch", toBase(ctx), config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    // ── Subagent hooks ───────────────────────────────────────────────────────
    // PluginHookSubagentContext: { runId?, childSessionKey?, requesterSessionKey? }
    // childSessionKey and requesterSessionKey identify the subagent relationship
    // graph — they don't map to HookEvent top-level columns, so they are merged
    // into event_data alongside the spawn event for security traceability.
    api.on("subagent_spawning", (event, ctx) => {
        const base = baseEvent("subagent_spawning", toBase(ctx), config);
        client.enqueue({
            ...base,
            event_data: safeStringify({ ...event, _ctx: { childSessionKey: ctx.childSessionKey, requesterSessionKey: ctx.requesterSessionKey } }),
        });
    });
    api.on("subagent_delivery_target", (event, ctx) => {
        const base = baseEvent("subagent_delivery_target", toBase(ctx), config);
        client.enqueue({
            ...base,
            event_data: safeStringify({ ...event, _ctx: { childSessionKey: ctx.childSessionKey, requesterSessionKey: ctx.requesterSessionKey } }),
        });
    });
    api.on("subagent_spawned", (event, ctx) => {
        const base = baseEvent("subagent_spawned", toBase(ctx), config);
        client.enqueue({
            ...base,
            event_data: safeStringify({ ...event, _ctx: { childSessionKey: ctx.childSessionKey, requesterSessionKey: ctx.requesterSessionKey } }),
        });
    });
    api.on("subagent_ended", async (event, ctx) => {
        const base = baseEvent("subagent_ended", toBase(ctx), config);
        client.enqueue({
            ...base,
            event_data: safeStringify({ ...event, _ctx: { childSessionKey: ctx.childSessionKey, requesterSessionKey: ctx.requesterSessionKey } }),
        });
        // sub-agent turns also need an immediate flush on completion
        await client.flush();
    });
    // ── Gateway hooks ────────────────────────────────────────────────────────
    // PluginHookGatewayContext: { port? } — no identity fields
    api.on("gateway_start", (event, ctx) => {
        const base = baseEvent("gateway_start", toBase(ctx), config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
    api.on("gateway_stop", async (event, ctx) => {
        const base = baseEvent("gateway_stop", toBase(ctx), config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
        await client.close();
    });
    // ── Install hook ─────────────────────────────────────────────────────────
    // PluginHookBeforeInstallContext: { targetType, requestKind, origin? }
    api.on("before_install", (event, ctx) => {
        const base = baseEvent("before_install", toBase(ctx), config);
        client.enqueue({ ...base, event_data: safeStringify(event) });
    });
}
//# sourceMappingURL=hooks.js.map