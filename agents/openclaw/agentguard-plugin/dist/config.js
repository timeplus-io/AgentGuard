const DEFAULTS = {
    timeplusUrl: "http://localhost:3218",
    stream: "agentguard_hook_events",
    flushMs: 2000,
    batchSize: 50,
    username: "proton",
    password: "",
    deploymentId: "default",
    deploymentName: "Default",
    // Default to localhost for the standalone-plugin case (plugin and AgentGuard
    // on the same host). The Makefile-driven Docker setup writes the
    // Docker-appropriate URL (host.docker.internal) into openclaw.json explicitly.
    agentguardUrl: "http://localhost:8080",
    holdFailPolicy: "deny",
    holdsEnabled: true,
};
export function resolveConfig(pluginConfig) {
    const timeplusUrl = (process.env.AGENTGUARD_TIMEPLUS_URL || undefined) ??
        pluginConfig?.timeplusUrl ??
        DEFAULTS.timeplusUrl;
    const stream = (process.env.AGENTGUARD_STREAM || undefined) ??
        pluginConfig?.stream ??
        DEFAULTS.stream;
    const rawFlushMs = Number((process.env.AGENTGUARD_FLUSH_MS || undefined) ??
        pluginConfig?.flushMs ??
        DEFAULTS.flushMs);
    const flushMs = Number.isFinite(rawFlushMs) && rawFlushMs > 0 ? rawFlushMs : DEFAULTS.flushMs;
    const rawBatchSize = Number((process.env.AGENTGUARD_BATCH_SIZE || undefined) ??
        pluginConfig?.batchSize ??
        DEFAULTS.batchSize);
    const batchSize = Number.isFinite(rawBatchSize) && rawBatchSize > 0
        ? rawBatchSize
        : DEFAULTS.batchSize;
    const username = (process.env.AGENTGUARD_USERNAME || undefined) ??
        pluginConfig?.username ??
        DEFAULTS.username;
    const password = (process.env.AGENTGUARD_PASSWORD || undefined) ??
        pluginConfig?.password ??
        DEFAULTS.password;
    // config-only: no env var override — deployment identity is set at installation time
    const deploymentId = pluginConfig?.deploymentId ??
        DEFAULTS.deploymentId;
    const deploymentName = pluginConfig?.deploymentName ??
        DEFAULTS.deploymentName;
    const agentguardUrl = (process.env.AGENTGUARD_URL || undefined) ??
        pluginConfig?.agentguardUrl ??
        DEFAULTS.agentguardUrl;
    const holdFailPolicy = process.env.AGENTGUARD_HOLD_FAIL_POLICY === "allow" ||
        pluginConfig?.holdFailPolicy === "allow"
        ? "allow"
        : "deny";
    const envHolds = process.env.AGENTGUARD_HOLDS_ENABLED;
    const pluginHolds = pluginConfig?.holdsEnabled;
    const holdsEnabled = envHolds !== undefined
        ? envHolds.toLowerCase() !== "false"
        : typeof pluginHolds === "boolean"
            ? pluginHolds
            : DEFAULTS.holdsEnabled;
    return {
        timeplusUrl,
        stream,
        flushMs,
        batchSize,
        username,
        password,
        deploymentId,
        deploymentName,
        agentguardUrl,
        holdFailPolicy,
        holdsEnabled,
    };
}
//# sourceMappingURL=config.js.map