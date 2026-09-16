import { definePluginEntry } from "openclaw/plugin-sdk/plugin-entry";
import { resolveConfig } from "./config.js";
import { createTimeplusClient } from "./timeplus-client.js";
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
      `[agentguard] active — streaming to ${config.timeplusUrl}/timeplusd/v2/ingest/${config.database}/streams/${config.stream} ` +
        `(flush every ${config.flushMs}ms, batch size ${config.batchSize}) ` +
        `deployment=${config.deploymentId}`,
    );
  },
});
