export type AgentShieldConfig = {
  timeplusUrl: string;
  stream: string;
  username: string;
  password: string;
  agentId: string;
  deploymentId: string;
  deploymentName: string;
};

const DEFAULTS: AgentShieldConfig = {
  timeplusUrl: "http://localhost:3218",
  stream: "agentshield_hook_events",
  username: "proton",
  password: "",
  agentId: "",
  deploymentId: "default",
  deploymentName: "Default",
};

export function resolveConfig(): AgentShieldConfig {
  return {
    timeplusUrl: process.env.AGENTSHIELD_TIMEPLUS_URL || DEFAULTS.timeplusUrl,
    stream: process.env.AGENTSHIELD_STREAM || DEFAULTS.stream,
    username: process.env.AGENTSHIELD_USERNAME || DEFAULTS.username,
    password: process.env.AGENTSHIELD_PASSWORD ?? DEFAULTS.password,
    agentId: process.env.AGENTSHIELD_AGENT_ID ?? DEFAULTS.agentId,
    deploymentId: process.env.AGENTSHIELD_DEPLOYMENT_ID || DEFAULTS.deploymentId,
    deploymentName: process.env.AGENTSHIELD_DEPLOYMENT_NAME || DEFAULTS.deploymentName,
  };
}
