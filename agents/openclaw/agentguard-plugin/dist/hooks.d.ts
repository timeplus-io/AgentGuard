import type { OpenClawPluginApi } from "openclaw/plugin-sdk";
import type { AgentGuardConfig } from "./config.js";
import type { HookEvent, TimeplusClient } from "./timeplus-client.js";
type BaseCtx = {
    agentId?: string;
    sessionKey?: string;
    sessionId?: string;
    runId?: string;
};
export declare function baseEvent(hookName: string, ctx: BaseCtx, config: AgentGuardConfig): HookEvent;
export declare function registerAllHooks(api: OpenClawPluginApi, client: TimeplusClient, config: AgentGuardConfig): void;
export {};
//# sourceMappingURL=hooks.d.ts.map