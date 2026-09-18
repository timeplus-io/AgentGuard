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
  flushMs: number;
  batchSize: number;
  username: string;
  password: string;
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
  holdsEnabled: false,
};

export function resolveConfig(
  pluginConfig?: Record<string, unknown>,
): AgentGuardConfig {
  const timeplusUrl =
    (process.env.AGENTGUARD_TIMEPLUS_URL || undefined) ??
    (pluginConfig?.timeplusUrl as string | undefined) ??
    DEFAULTS.timeplusUrl;

  const apiKey =
    (process.env.AGENTGUARD_API_KEY || undefined) ??
    (pluginConfig?.apiKey as string | undefined) ??
    DEFAULTS.apiKey;

  const ingestApi = resolveIngestApi(
    (process.env.AGENTGUARD_INGEST_API || undefined) ??
      (pluginConfig?.ingestApi as string | undefined),
    apiKey,
  );

  const workspace =
    (process.env.AGENTGUARD_WORKSPACE || undefined) ??
    (pluginConfig?.workspace as string | undefined) ??
    DEFAULTS.workspace;

  const database =
    (process.env.AGENTGUARD_DATABASE || undefined) ??
    (pluginConfig?.database as string | undefined) ??
    DEFAULTS.database;

  const stream =
    (process.env.AGENTGUARD_STREAM || undefined) ??
    (pluginConfig?.stream as string | undefined) ??
    DEFAULTS.stream;

  const rawFlushMs = Number(
    (process.env.AGENTGUARD_FLUSH_MS || undefined) ??
      pluginConfig?.flushMs ??
      DEFAULTS.flushMs,
  );
  const flushMs =
    Number.isFinite(rawFlushMs) && rawFlushMs > 0 ? rawFlushMs : DEFAULTS.flushMs;

  const rawBatchSize = Number(
    (process.env.AGENTGUARD_BATCH_SIZE || undefined) ??
      pluginConfig?.batchSize ??
      DEFAULTS.batchSize,
  );
  const batchSize =
    Number.isFinite(rawBatchSize) && rawBatchSize > 0
      ? rawBatchSize
      : DEFAULTS.batchSize;

  const username =
    (process.env.AGENTGUARD_USERNAME || undefined) ??
    (pluginConfig?.username as string | undefined) ??
    DEFAULTS.username;

  const password =
    (process.env.AGENTGUARD_PASSWORD || undefined) ??
    (pluginConfig?.password as string | undefined) ??
    DEFAULTS.password;

  // config-only: no env var override — deployment identity is set at installation time
  const deploymentId =
    (pluginConfig?.deploymentId as string | undefined) ??
    DEFAULTS.deploymentId;

  const deploymentName =
    (pluginConfig?.deploymentName as string | undefined) ??
    DEFAULTS.deploymentName;

  const agentguardUrl =
    (process.env.AGENTGUARD_URL || undefined) ??
    (pluginConfig?.agentguardUrl as string | undefined) ??
    DEFAULTS.agentguardUrl;

  const holdFailPolicy: "deny" | "allow" =
    process.env.AGENTGUARD_HOLDS_FAIL_POLICY === "allow" ||
    (pluginConfig?.holdFailPolicy as string | undefined) === "allow"
      ? "allow"
      : "deny";

  const envHolds = process.env.AGENTGUARD_HOLDS_ENABLED;
  const pluginHolds = pluginConfig?.holdsEnabled;
  const holdsEnabled =
    envHolds !== undefined
      ? envHolds.toLowerCase() === "true"
      : typeof pluginHolds === "boolean"
        ? pluginHolds
        : DEFAULTS.holdsEnabled;

  return {
    timeplusUrl,
    ingestApi,
    workspace,
    apiKey,
    database,
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
