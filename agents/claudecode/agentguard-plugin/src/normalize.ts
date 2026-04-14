import type { AgentGuardConfig } from "./config.js";
import type { HookEvent } from "./timeplus-client.js";
import type { TranscriptMessage } from "./transcript.js";

const HOOK_NAME_MAP: Record<string, string> = {
  SessionStart:        "session_start",
  SessionEnd:          "session_end",
  UserPromptSubmit:    "user_prompt_submit",
  PreToolUse:          "before_tool_call",
  PostToolUse:         "after_tool_call",
  PostToolUseFailure:  "tool_call_failure",
  SubagentStart:       "subagent_spawning",
  SubagentStop:        "subagent_ended",
  Stop:                "llm_output",
  PermissionDenied:    "permission_denied",
};

type ClaudeCodeInput = {
  hook_event_name?: string;
  session_id?: string;
  tool_name?: string;
  tool_use_id?: string;
  model?: string;
  [key: string]: unknown;
};

export function normalize(input: ClaudeCodeInput, config: AgentGuardConfig): HookEvent {
  const hookEventName = input.hook_event_name ?? "";
  const hookName = HOOK_NAME_MAP[hookEventName] ?? hookEventName.toLowerCase();

  const isToolEvent = hookEventName === "PreToolUse"
    || hookEventName === "PostToolUse"
    || hookEventName === "PostToolUseFailure";

  const model = hookEventName === "SessionStart"
    ? (typeof input.model === "string" ? input.model : "")
    : "";

  return {
    hook_name:       hookName,
    event_time:      new Date().toISOString(),
    session_id:      typeof input.session_id === "string" ? input.session_id : "",
    run_id:          typeof input.session_id === "string" ? input.session_id : "",
    agent_id:        config.agentId,
    deployment_id:   config.deploymentId,
    deployment_name: config.deploymentName,
    session_key:     "",
    tool_name:       isToolEvent && typeof input.tool_name === "string" ? input.tool_name : "",
    provider:        "",
    model,
    hook_decision:   "observe",
    block_reason:    "",
    event_data:      JSON.stringify(input),
    agent_type:      "claudecode",
  };
}

/**
 * Builds a synthetic context event (prompt_context or llm_context) containing
 * the full conversation transcript. These are virtual hook events — not fired by
 * Claude Code itself — sent once per LLM turn to capture the assembled prompt
 * context for security analysis.
 *
 * - prompt_context: fired alongside UserPromptSubmit (context before the turn starts)
 * - llm_context:    fired alongside Stop (complete turn including all tool results)
 */
export function normalizeContextEvent(
  input: ClaudeCodeInput,
  config: AgentGuardConfig,
  hookName: "prompt_context" | "llm_context",
  transcript: TranscriptMessage[],
): HookEvent {
  return {
    hook_name:       hookName,
    event_time:      new Date().toISOString(),
    session_id:      typeof input.session_id === "string" ? input.session_id : "",
    run_id:          typeof input.session_id === "string" ? input.session_id : "",
    agent_id:        config.agentId,
    deployment_id:   config.deploymentId,
    deployment_name: config.deploymentName,
    session_key:     "",
    tool_name:       "",
    provider:        "",
    model:           "",
    hook_decision:   "observe",
    block_reason:    "",
    event_data:      JSON.stringify({ message_count: transcript.length, messages: transcript }),
    agent_type:      "claudecode",
  };
}
