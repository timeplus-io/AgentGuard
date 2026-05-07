import type { OpenClawPluginDefinition } from "openclaw/plugin-sdk/plugin-entry";
import { resolveConfig } from "./config.js";
import { createTimeplusClient } from "./timeplus-client.js";
import { registerAllHooks } from "./hooks.js";

const plugin: OpenClawPluginDefinition = {
  id: "agentguard",
  name: "AgentGuard Security Monitor",
  version: "0.2.1",
  description:
    "Streams all OpenClaw lifecycle events to Timeplus for real-time security monitoring.",

  activate: (api) => {
    const config = resolveConfig(api.pluginConfig);
    const client = createTimeplusClient(config);
    registerAllHooks(api, client, config);
    api.logger.info(
      `[agentguard] active — streaming to ${config.timeplusUrl}/proton/v1/ingest/streams/${config.stream} ` +
        `(flush every ${config.flushMs}ms, batch size ${config.batchSize}) ` +
        `deployment=${config.deploymentId}`,
    );
  },
};

export default plugin;
