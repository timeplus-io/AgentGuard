#!/usr/bin/env node
import { resolveConfig } from "./config.js";
import { send } from "./timeplus-client.js";
import { normalize, normalizeContextEvent } from "./normalize.js";
import { readTranscript } from "./transcript.js";

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
    process.stderr.write(`[agentshield] failed to parse hook input: ${raw.slice(0, 200)}\n`);
    return;
  }

  const config = resolveConfig();
  const event = normalize(input, config);
  await send(event, config);

  // For the two transcript-bearing hooks, read the transcript file and send a
  // separate context event. All other hooks send no transcript data.
  const hookEventName = typeof input["hook_event_name"] === "string" ? input["hook_event_name"] : "";
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
  process.stderr.write(`[agentshield] unexpected error: ${String(err)}\n`);
});
