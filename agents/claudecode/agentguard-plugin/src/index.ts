#!/usr/bin/env node
import { resolveConfig } from "./config.js";
import { send } from "./timeplus-client.js";
import { normalize, normalizeContextEvent } from "./normalize.js";
import { readTranscript, readTranscriptUsage } from "./transcript.js";

// Only these two hooks produce a companion context event carrying the transcript.
// UserPromptSubmit → prompt_context (context before the new turn begins)
// Stop             → llm_context    (complete turn including all tool results)
const CONTEXT_HOOKS: Record<string, "prompt_context" | "llm_context"> = {
  UserPromptSubmit: "prompt_context",
  Stop:             "llm_context",
};

async function main(): Promise<void> {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(chunk as Buffer);
  }
  const raw = Buffer.concat(chunks).toString("utf8").trim();
  if (!raw) {
    return;
  }

  let input: Record<string, unknown>;
  try {
    input = JSON.parse(raw) as Record<string, unknown>;
  } catch {
    process.stderr.write(`[agentguard] failed to parse hook input: ${raw.slice(0, 200)}\n`);
    return;
  }

  const config = resolveConfig();
  const hookEventName = typeof input["hook_event_name"] === "string" ? input["hook_event_name"] : "";

  // PreToolUse: synchronous hold path. POST to /api/holds and write Claude Code's
  // permissionDecision response to stdout. Skip the async send() — /api/holds
  // ingests the event into agentguard_hook_events itself.
  if (hookEventName === "PreToolUse") {
    // If holds are disabled, fall back to async observation-only:
    // skip the synchronous /api/holds call, write a permissive permissionDecision,
    // AND let the async send() path run so the event still reaches Timeplus.
    if (!config.holdsEnabled) {
      process.stdout.write(JSON.stringify({
        hookSpecificOutput: {
          hookEventName: "PreToolUse",
          permissionDecision: "allow",
          permissionDecisionReason: "",
        },
      }));
      // Fall through to the regular send() so observability data still flows.
      // (Don't return — let the existing async ingest run below.)
    } else {
      const event = normalize(input, config);
      const { requestHold } = await import("./holds.js");
      const decision = await requestHold(event, config);
      process.stdout.write(JSON.stringify({
        hookSpecificOutput: {
          hookEventName: "PreToolUse",
          permissionDecision: decision.decision === "block" ? "deny" : "allow",
          permissionDecisionReason: decision.reason ?? "",
        },
      }));
      return;  // do NOT also call send() — /api/holds ingested the event already
    }
  }

  // SessionEnd: fire-and-forget abandon-session to clean up zombie holds.
  if (hookEventName === "SessionEnd") {
    const sessionId = typeof input["session_id"] === "string" ? input["session_id"] : "";
    if (sessionId) {
      fetch(`${config.agentguardUrl}/api/holds/_abandon-session/${encodeURIComponent(sessionId)}`, {
        method: "POST",
      }).catch(() => { /* best-effort */ });
    }
    // continue with existing async ingest path below
  }

  // For the Stop hook, accumulate per-call token usage from the transcript and
  // attach it as _llm_usage in event_data. The mv_cim_metrics_claudecode_cim
  // MV extracts this to produce LLM metrics without requiring OTel.
  if (hookEventName === "Stop") {
    const transcriptPath = typeof input["transcript_path"] === "string" ? input["transcript_path"] : null;
    if (transcriptPath) {
      input = { ...input, _llm_usage: readTranscriptUsage(transcriptPath) };
    }
  }

  const event = normalize(input, config);
  await send(event, config);

  // For the two transcript-bearing hooks, read the transcript file and send a
  // separate context event. All other hooks send no transcript data.
  const contextHookName = CONTEXT_HOOKS[hookEventName];
  if (contextHookName) {
    const transcriptPath = typeof input["transcript_path"] === "string" ? input["transcript_path"] : null;
    if (transcriptPath) {
      const transcript = readTranscript(transcriptPath);
      if (transcript.length > 0) {
        const contextEvent = normalizeContextEvent(input, config, contextHookName, transcript);
        await send(contextEvent, config);
      }
    }
  }
}

main().catch((err) => {
  process.stderr.write(`[agentguard] unexpected error: ${String(err)}\n`);
});
