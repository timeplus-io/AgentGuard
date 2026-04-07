const DEFAULTS = {
    timeplusUrl: "http://localhost:3218",
    stream: "agentshield_hook_events",
    flushMs: 2000,
    batchSize: 50,
};
export function resolveConfig(pluginConfig) {
    const timeplusUrl = (process.env.AGENTSHIELD_TIMEPLUS_URL || undefined) ??
        pluginConfig?.timeplusUrl ??
        DEFAULTS.timeplusUrl;
    const stream = (process.env.AGENTSHIELD_STREAM || undefined) ??
        pluginConfig?.stream ??
        DEFAULTS.stream;
    const rawFlushMs = Number((process.env.AGENTSHIELD_FLUSH_MS || undefined) ??
        pluginConfig?.flushMs ??
        DEFAULTS.flushMs);
    const flushMs = Number.isFinite(rawFlushMs) && rawFlushMs > 0 ? rawFlushMs : DEFAULTS.flushMs;
    const rawBatchSize = Number((process.env.AGENTSHIELD_BATCH_SIZE || undefined) ??
        pluginConfig?.batchSize ??
        DEFAULTS.batchSize);
    const batchSize = Number.isFinite(rawBatchSize) && rawBatchSize > 0
        ? rawBatchSize
        : DEFAULTS.batchSize;
    return { timeplusUrl, stream, flushMs, batchSize };
}
//# sourceMappingURL=config.js.map