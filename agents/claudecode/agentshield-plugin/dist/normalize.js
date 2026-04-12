const HOOK_NAME_MAP = {
    SessionStart: "session_start",
    SessionEnd: "session_end",
    UserPromptSubmit: "user_prompt_submit",
    PreToolUse: "before_tool_call",
    PostToolUse: "after_tool_call",
    PostToolUseFailure: "tool_call_failure",
    SubagentStart: "subagent_spawning",
    SubagentStop: "subagent_ended",
    Stop: "llm_output",
    PermissionDenied: "permission_denied",
};
export function normalize(input, config) {
    const hookEventName = input.hook_event_name ?? "";
    const hookName = HOOK_NAME_MAP[hookEventName] ?? hookEventName.toLowerCase();
    const isToolEvent = hookEventName === "PreToolUse"
        || hookEventName === "PostToolUse"
        || hookEventName === "PostToolUseFailure";
    const model = hookEventName === "SessionStart"
        ? (typeof input.model === "string" ? input.model : "")
        : "";
    return {
        hook_name: hookName,
        event_time: new Date().toISOString(),
        session_id: typeof input.session_id === "string" ? input.session_id : "",
        run_id: isToolEvent && typeof input.tool_use_id === "string" ? input.tool_use_id : "",
        agent_id: config.agentId,
        deployment_id: config.deploymentId,
        deployment_name: config.deploymentName,
        session_key: "",
        tool_name: isToolEvent && typeof input.tool_name === "string" ? input.tool_name : "",
        provider: "",
        model,
        hook_decision: "observe",
        block_reason: "",
        event_data: JSON.stringify(input),
        agent_type: "claudecode",
    };
}
//# sourceMappingURL=normalize.js.map