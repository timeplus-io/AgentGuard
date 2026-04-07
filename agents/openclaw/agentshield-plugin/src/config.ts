export type AgentShieldConfig = {
  timeplusUrl: string;
  stream: string;
  flushMs: number;
  batchSize: number;
};

const DEFAULTS: AgentShieldConfig = {
  timeplusUrl: "http://localhost:3218",
  stream: "agentshield_hook_events",
  flushMs: 2000,
  batchSize: 50,
};

export function resolveConfig(
  pluginConfig?: Record<string, unknown>,
): AgentShieldConfig {
  return {
    timeplusUrl:
      process.env.AGENTSHIELD_TIMEPLUS_URL ??
      (pluginConfig?.timeplusUrl as string) ??
      DEFAULTS.timeplusUrl,
    stream:
      process.env.AGENTSHIELD_STREAM ??
      (pluginConfig?.stream as string) ??
      DEFAULTS.stream,
    flushMs: Number(
      process.env.AGENTSHIELD_FLUSH_MS ??
        pluginConfig?.flushMs ??
        DEFAULTS.flushMs,
    ),
    batchSize: Number(
      process.env.AGENTSHIELD_BATCH_SIZE ??
        pluginConfig?.batchSize ??
        DEFAULTS.batchSize,
    ),
  };
}
