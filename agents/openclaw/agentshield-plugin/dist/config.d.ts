export type AgentShieldConfig = {
    timeplusUrl: string;
    stream: string;
    flushMs: number;
    batchSize: number;
    username: string;
    password: string;
};
export declare function resolveConfig(pluginConfig?: Record<string, unknown>): AgentShieldConfig;
//# sourceMappingURL=config.d.ts.map