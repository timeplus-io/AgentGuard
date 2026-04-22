export type AgentGuardConfig = {
    timeplusUrl: string;
    stream: string;
    flushMs: number;
    batchSize: number;
    username: string;
    password: string;
    deploymentId: string;
    deploymentName: string;
    agentguardUrl: string;
    holdFailPolicy: "deny" | "allow";
};
export declare function resolveConfig(pluginConfig?: Record<string, unknown>): AgentGuardConfig;
//# sourceMappingURL=config.d.ts.map