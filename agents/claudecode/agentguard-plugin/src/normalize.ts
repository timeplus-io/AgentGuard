import type { AgentGuardConfig } from "./config.js";
import type { HookEvent } from "./timeplus-client.js";
import type { LlmCall, TranscriptMessage } from "./transcript.js";

/**
 * Infers the LLM provider from a Claude Code model identifier.
 *
 * Claude Code surfaces the model ID as returned by the underlying API response.
 * The format differs by backend:
 *   - Direct Anthropic API:  "claude-sonnet-4-6"
 *   - Google Vertex AI:      "claude-sonnet-4-6@20251001"  (contains "@")
 *   - AWS Bedrock:           "anthropic.claude-3-5-sonnet-20241022-v2:0"
 *                            or with region prefix "us.anthropic.claude-..."
 */
export function inferProvider(model: string): string {
  if (!model) return "anthropic";
  if (model.includes("@")) return "vertex";
  if (/^(us\.|eu\.|ap\.)?anthropic\./.test(model)) return "bedrock";
  return "anthropic";
}

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
  InstructionsLoaded:  "instructions_loaded",
  PreCompact:          "pre_compact",
  PostCompact:         "post_compact",
  PermissionRequest:   "permission_request",
  Notification:        "notification",
  StopFailure:         "stop_failure",
  TaskCreated:         "task_created",
  TaskCompleted:       "task_completed",
  PostModelSwitch:     "model_switched",
  ConfigChange:        "config_change",
};

type ClaudeCodeInput = {
  hook_event_name?: string;
  session_id?: string;
  tool_name?: string;
  tool_use_id?: string;
  model?: string;
  [key: string]: unknown;
};

function promptId(input: ClaudeCodeInput): string {
  return typeof input.prompt_id === "string" ? input.prompt_id : "";
}

/**
 * `run_id` is the id of the turn: Claude Code sends it as `prompt_id` on the
 * hooks that fire inside a turn. Hooks outside a turn (SessionStart,
 * PostModelSwitch) and older Claude Code versions have none, and fall back to
 * the session id.
 */
function runId(input: ClaudeCodeInput): string {
  if (promptId(input)) return promptId(input);
  return typeof input.session_id === "string" ? input.session_id : "";
}

export function normalize(input: ClaudeCodeInput, config: AgentGuardConfig): HookEvent {
  const hookEventName = input.hook_event_name ?? "";
  const hookName = HOOK_NAME_MAP[hookEventName] ?? hookEventName.toLowerCase();

  const isToolEvent = hookEventName === "PreToolUse"
    || hookEventName === "PostToolUse"
    || hookEventName === "PostToolUseFailure";

  // For Stop events the index.ts attaches _llm_usage from the transcript,
  // which includes the model of the last model request of the turn.
  const llmUsage = (input._llm_usage as { model?: string } | undefined) ?? {};

  const model = hookEventName === "SessionStart"
    ? (typeof input.model === "string" ? input.model : "")
    : hookEventName === "Stop"
    ? (typeof llmUsage.model === "string" ? llmUsage.model : "")
    : "";

  return {
    hook_name:       hookName,
    event_time:      Date.now(),
    session_id:      typeof input.session_id === "string" ? input.session_id : "",
    run_id:          runId(input),
    agent_id:        config.agentId,
    deployment_id:   config.deploymentId,
    deployment_name: config.deploymentName,
    session_key:     "",
    tool_name:       isToolEvent && typeof input.tool_name === "string" ? input.tool_name : "",
    provider:        model ? inferProvider(model) : "",
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
    event_time:      Date.now(),
    session_id:      typeof input.session_id === "string" ? input.session_id : "",
    run_id:          runId(input),
    agent_id:        config.agentId,
    deployment_id:   config.deploymentId,
    deployment_name: config.deploymentName,
    session_key:     "",
    tool_name:       "",
    provider:        "",
    model:           "",
    hook_decision:   "observe",
    block_reason:    "",
    event_data:      JSON.stringify({
      ...(promptId(input) ? { prompt_id: promptId(input) } : {}),
      message_count: transcript.length,
      messages: transcript,
    }),
    agent_type:      "claudecode",
  };
}

/**
 * Builds one synthetic `llm_call` event per model request found in a transcript.
 * Like the context events these are not fired by Claude Code: they are sent
 * alongside Stop (the requests of the turn) and SubagentStop (the requests of
 * the sub-agent), and give the app one row per request with its token usage.
 *
 * `event_time` is the time of the response, not of the hook. `usage` keeps the
 * Anthropic meaning: `input_tokens` does not include cached tokens.
 * `duration_ms` is left out when the transcript does not record the request.
 */
export function normalizeLlmCallEvents(
  input: ClaudeCodeInput,
  config: AgentGuardConfig,
  calls: LlmCall[],
  subagent?: { agent_id: string; agent_type: string },
): HookEvent[] {
  const sessionId = typeof input.session_id === "string" ? input.session_id : "";
  return calls.map((call) => {
    const eventTime = Date.parse(call.timestamp);
    return {
      hook_name:       "llm_call",
      event_time:      Number.isFinite(eventTime) ? eventTime : Date.now(),
      session_id:      sessionId,
      // The turn the request was made in, which can differ from the turn of the
      // hook: a Stop also reports requests of a turn that ended without one.
      run_id:          call.prompt_id || runId(input),
      agent_id:        config.agentId,
      deployment_id:   config.deploymentId,
      deployment_name: config.deploymentName,
      session_key:     "",
      tool_name:       "",
      provider:        call.model ? inferProvider(call.model) : "",
      model:           call.model,
      hook_decision:   "observe",
      block_reason:    "",
      event_data:      JSON.stringify({
        ...(call.prompt_id || promptId(input) ? { prompt_id: call.prompt_id || promptId(input) } : {}),
        message_id:  call.message_id,
        request_id:  call.request_id,
        model:       call.model,
        stop_reason: call.stop_reason,
        usage: {
          input_tokens:                call.input_tokens,
          output_tokens:               call.output_tokens,
          cache_read_input_tokens:     call.cache_read_input_tokens,
          cache_creation_input_tokens: call.cache_creation_input_tokens,
          total_tokens:                call.input_tokens + call.output_tokens,
        },
        ...(call.duration_ms !== null ? { duration_ms: call.duration_ms, started_at: call.started_at } : {}),
        is_sidechain: call.is_sidechain || subagent !== undefined,
        ...(subagent ? { subagent_id: subagent.agent_id, subagent_type: subagent.agent_type } : {}),
      }),
      agent_type:      "claudecode",
    };
  });
}
