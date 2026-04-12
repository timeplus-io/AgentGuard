#!/usr/bin/env node
import { resolveConfig } from "./config.js";
import { send } from "./timeplus-client.js";
import { normalize } from "./normalize.js";

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
}

main().catch((err) => {
  process.stderr.write(`[agentshield] unexpected error: ${String(err)}\n`);
});
