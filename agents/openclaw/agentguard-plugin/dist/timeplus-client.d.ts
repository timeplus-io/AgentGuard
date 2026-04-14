import type { AgentGuardConfig } from "./config.js";
export type HookEvent = {
    hook_name: string;
    event_time: string;
    session_id: string;
    run_id: string;
    agent_id: string;
    deployment_id: string;
    deployment_name: string;
    session_key: string;
    tool_name: string;
    provider: string;
    model: string;
    hook_decision: string;
    block_reason: string;
    event_data: string;
    agent_type: string;
};
export type TimeplusClient = {
    enqueue: (event: HookEvent) => void;
    flush: () => Promise<void>;
    close: () => Promise<void>;
};
export declare function createTimeplusClient(config: AgentGuardConfig): TimeplusClient;
//# sourceMappingURL=timeplus-client.d.ts.map