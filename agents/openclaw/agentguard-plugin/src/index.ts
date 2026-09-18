import { definePluginEntry } from "openclaw/plugin-sdk/plugin-entry";
import { resolveConfig } from "./config.js";
import { buildIngestUrl, createTimeplusClient } from "./timeplus-client.js";
import { registerAllHooks } from "./hooks.js";

export default definePluginEntry({
  id: "agentguard",
  name: "AgentGuard Security Monitor",
  description:
    "Streams all OpenClaw lifecycle events to Timeplus for real-time security monitoring.",

  register(api) {
    const config = resolveConfig(api.pluginConfig);
    const client = createTimeplusClient(config);
    registerAllHooks(api, client, config);
    api.logger.info(
      `[agentguard] active — streaming to ${buildIngestUrl(config)} (${config.ingestApi}) ` +
        `(flush every ${config.flushMs}ms, batch size ${config.batchSize}) ` +
        `deployment=${config.deploymentId}`,
    );
  },
});
