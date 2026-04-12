import type { AgentShieldConfig } from "./config.js";
import type { HookEvent } from "./timeplus-client.js";
type ClaudeCodeInput = {
    hook_event_name?: string;
    session_id?: string;
    tool_name?: string;
    tool_use_id?: string;
    model?: string;
    [key: string]: unknown;
};
export declare function normalize(input: ClaudeCodeInput, config: AgentShieldConfig): HookEvent;
export {};
//# sourceMappingURL=normalize.d.ts.map