import { hostname } from "os";

/**
 * Which Timeplus HTTP surface receives hook events.
 *  - "timeplusd": timeplusd's own HTTP interface (port 3218) — the default.
 *  - "rest":      the Timeplus REST API served by the web/API server
 *                 (port 8000, or Timeplus Cloud over HTTPS) — the only
 *                 surface exposed when timeplusd is not directly reachable.
 */
export type IngestApi = "timeplusd" | "rest";

/** Explicit value wins; otherwise a configured API key implies the REST API. */
export function resolveIngestApi(raw: string | undefined, apiKey: string): IngestApi {
  const v = raw?.trim().toLowerCase();
  if (v === "rest" || v === "timeplusd") return v;
  return apiKey ? "rest" : "timeplusd";
}

export type AgentGuardConfig = {
  timeplusUrl: string;
  ingestApi: IngestApi;
  workspace: string;
  apiKey: string;
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
  ingestApi: "timeplusd",
  workspace: "default",
  apiKey: "",
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
  const apiKey = process.env.AGENTGUARD_API_KEY ?? DEFAULTS.apiKey;
  return {
    timeplusUrl: process.env.AGENTGUARD_TIMEPLUS_URL || DEFAULTS.timeplusUrl,
    ingestApi: resolveIngestApi(process.env.AGENTGUARD_INGEST_API, apiKey),
    workspace: process.env.AGENTGUARD_WORKSPACE || DEFAULTS.workspace,
    apiKey,
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
