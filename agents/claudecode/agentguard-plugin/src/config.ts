import { hostname } from "os";

export type AgentGuardConfig = {
  timeplusUrl: string;
  database: string;
  stream: string;
  username: string;
  password: string;
  agentId: string;
  deploymentId: string;
  deploymentName: string;
  agentguardUrl: string;
  holdFailPolicy: "deny" | "allow";
  holdsEnabled: boolean;
};

const DEFAULTS: AgentGuardConfig = {
  timeplusUrl: "http://localhost:3218",
  database: "ag",
  stream: "agentguard_hook_events",
  username: "proton",
  password: "",
  agentId: "",
  deploymentId: "default",
  deploymentName: "Default",
  agentguardUrl: "http://localhost:8080",
  holdFailPolicy: "deny",
  holdsEnabled: false,
};

export function resolveConfig(): AgentGuardConfig {
  return {
    timeplusUrl: process.env.AGENTGUARD_TIMEPLUS_URL || DEFAULTS.timeplusUrl,
    database: process.env.AGENTGUARD_DATABASE || DEFAULTS.database,
    stream: process.env.AGENTGUARD_STREAM || DEFAULTS.stream,
    username: process.env.AGENTGUARD_USERNAME || DEFAULTS.username,
    password: process.env.AGENTGUARD_PASSWORD ?? DEFAULTS.password,
    agentId: process.env.AGENTGUARD_AGENT_ID || hostname(),
    deploymentId: process.env.AGENTGUARD_DEPLOYMENT_ID || DEFAULTS.deploymentId,
    deploymentName: process.env.AGENTGUARD_DEPLOYMENT_NAME || DEFAULTS.deploymentName,
    agentguardUrl: process.env.AGENTGUARD_URL ?? DEFAULTS.agentguardUrl,
    holdFailPolicy: (process.env.AGENTGUARD_HOLDS_FAIL_POLICY === "allow") ? "allow" : "deny",
    holdsEnabled: process.env.AGENTGUARD_HOLDS_ENABLED?.toLowerCase() === "true",
  };
}
