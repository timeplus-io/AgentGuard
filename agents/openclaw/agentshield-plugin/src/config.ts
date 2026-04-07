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
  const timeplusUrl =
    (process.env.AGENTSHIELD_TIMEPLUS_URL || undefined) ??
    (pluginConfig?.timeplusUrl as string | undefined) ??
    DEFAULTS.timeplusUrl;

  const stream =
    (process.env.AGENTSHIELD_STREAM || undefined) ??
    (pluginConfig?.stream as string | undefined) ??
    DEFAULTS.stream;

  const rawFlushMs = Number(
    (process.env.AGENTSHIELD_FLUSH_MS || undefined) ??
      pluginConfig?.flushMs ??
      DEFAULTS.flushMs,
  );
  const flushMs =
    Number.isFinite(rawFlushMs) && rawFlushMs > 0 ? rawFlushMs : DEFAULTS.flushMs;

  const rawBatchSize = Number(
    (process.env.AGENTSHIELD_BATCH_SIZE || undefined) ??
      pluginConfig?.batchSize ??
      DEFAULTS.batchSize,
  );
  const batchSize =
    Number.isFinite(rawBatchSize) && rawBatchSize > 0
      ? rawBatchSize
      : DEFAULTS.batchSize;

  return { timeplusUrl, stream, flushMs, batchSize };
}
