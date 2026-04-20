import { hostname } from "os";

export type AgentGuardConfig = {
  timeplusUrl: string;
  stream: string;
  username: string;
  password: string;
  agentId: string;
  deploymentId: string;
  deploymentName: string;
};

const DEFAULTS: AgentGuardConfig = {
  timeplusUrl: "http://localhost:3218",
  stream: "agentguard_hook_events",
  username: "proton",
  password: "",
  agentId: "",
  deploymentId: "default",
  deploymentName: "Default",
};

export function resolveConfig(): AgentGuardConfig {
  return {
    timeplusUrl: process.env.AGENTGUARD_TIMEPLUS_URL || DEFAULTS.timeplusUrl,
    stream: process.env.AGENTGUARD_STREAM || DEFAULTS.stream,
    username: process.env.AGENTGUARD_USERNAME || DEFAULTS.username,
    password: process.env.AGENTGUARD_PASSWORD ?? DEFAULTS.password,
    agentId: process.env.AGENTGUARD_AGENT_ID || hostname(),
    deploymentId: process.env.AGENTGUARD_DEPLOYMENT_ID || DEFAULTS.deploymentId,
    deploymentName: process.env.AGENTGUARD_DEPLOYMENT_NAME || DEFAULTS.deploymentName,
  };
}
