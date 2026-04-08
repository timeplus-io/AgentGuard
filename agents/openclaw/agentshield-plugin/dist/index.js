import { resolveConfig } from "./config.js";
import { createTimeplusClient } from "./timeplus-client.js";
import { registerAllHooks } from "./hooks.js";
const plugin = {
    id: "agentshield",
    name: "AgentShield Security Monitor",
    version: "0.1.0",
    description: "Streams all OpenClaw lifecycle events to Timeplus for real-time security monitoring.",
    activate: async (api) => {
        const config = resolveConfig(api.pluginConfig);
        const client = createTimeplusClient(config);
        registerAllHooks(api, client, config);
        api.logger.info(`[agentshield] active — streaming to ${config.timeplusUrl}/proton/v1/ingest/streams/${config.stream} ` +
            `(flush every ${config.flushMs}ms, batch size ${config.batchSize}) ` +
            `deployment=${config.deploymentId}`);
    },
};
export default plugin;
//# sourceMappingURL=index.js.map