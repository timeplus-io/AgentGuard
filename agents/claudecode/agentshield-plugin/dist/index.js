import { resolveConfig } from "./config.js";
import { send } from "./timeplus-client.js";
import { normalize } from "./normalize.js";
async function main() {
    const chunks = [];
    for await (const chunk of process.stdin) {
        chunks.push(chunk);
    }
    const raw = Buffer.concat(chunks).toString("utf8").trim();
    if (!raw) {
        return;
    }
    let input;
    try {
        input = JSON.parse(raw);
    }
    catch {
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
//# sourceMappingURL=index.js.map