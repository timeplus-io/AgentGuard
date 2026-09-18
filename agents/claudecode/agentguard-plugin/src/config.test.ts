import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { hostname } from "os";
import { resolveConfig } from "./config.js";

describe("resolveConfig", () => {
  const ORIGINAL_ENV = { ...process.env };

  beforeEach(() => {
    for (const key of Object.keys(process.env)) {
      if (key.startsWith("AGENTGUARD_")) delete process.env[key];
    }
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (key.startsWith("AGENTGUARD_")) delete process.env[key];
    }
    Object.assign(process.env, ORIGINAL_ENV);
  });

  it("returns defaults when no env vars are set", () => {
    const cfg = resolveConfig();
    expect(cfg.timeplusUrl).toBe("http://localhost:3218");
    expect(cfg.database).toBe("ag");
    expect(cfg.stream).toBe("agentguard_hook_events");
    expect(cfg.username).toBe("proton");
    expect(cfg.password).toBe("");
    expect(cfg.deploymentId).toBe("default");
    expect(cfg.deploymentName).toBe("Default");
    // agentId falls back to os.hostname() when AGENTGUARD_AGENT_ID is unset.
    // Compare against the live hostname so the test is deterministic across hosts.
    expect(cfg.agentId).toBe(hostname());
  });

  it("reads AGENTGUARD_TIMEPLUS_URL from env", () => {
    process.env.AGENTGUARD_TIMEPLUS_URL = "http://remote:3218";
    expect(resolveConfig().timeplusUrl).toBe("http://remote:3218");
  });

  it("reads AGENTGUARD_AGENT_ID from env", () => {
    process.env.AGENTGUARD_AGENT_ID = "my-laptop";
    expect(resolveConfig().agentId).toBe("my-laptop");
  });

  it("reads AGENTGUARD_DEPLOYMENT_ID from env", () => {
    process.env.AGENTGUARD_DEPLOYMENT_ID = "prod";
    expect(resolveConfig().deploymentId).toBe("prod");
  });

  it("reads AGENTGUARD_DEPLOYMENT_NAME from env", () => {
    process.env.AGENTGUARD_DEPLOYMENT_NAME = "Production";
    expect(resolveConfig().deploymentName).toBe("Production");
  });

  it("reads AGENTGUARD_USERNAME and AGENTGUARD_PASSWORD from env", () => {
    process.env.AGENTGUARD_USERNAME = "admin";
    process.env.AGENTGUARD_PASSWORD = "secret";
    const cfg = resolveConfig();
    expect(cfg.username).toBe("admin");
    expect(cfg.password).toBe("secret");
  });

  it("reads AGENTGUARD_STREAM from env", () => {
    process.env.AGENTGUARD_STREAM = "custom_stream";
    expect(resolveConfig().stream).toBe("custom_stream");
  });

  it("reads AGENTGUARD_DATABASE from env", () => {
    process.env.AGENTGUARD_DATABASE = "custom_db";
    expect(resolveConfig().database).toBe("custom_db");
  });

  it("AGENTGUARD_DATABASE='' falls back to default 'ag'", () => {
    process.env.AGENTGUARD_DATABASE = "";
    expect(resolveConfig().database).toBe("ag");
  });

  it("agentguardUrl defaults to http://localhost:8080", () => {
    delete process.env.AGENTGUARD_URL;
    const cfg = resolveConfig();
    expect(cfg.agentguardUrl).toBe("http://localhost:8080");
  });

  it("agentguardUrl reads AGENTGUARD_URL", () => {
    process.env.AGENTGUARD_URL = "http://my-host:9090";
    const cfg = resolveConfig();
    expect(cfg.agentguardUrl).toBe("http://my-host:9090");
  });

  it("holdFailPolicy defaults to deny", () => {
    delete process.env.AGENTGUARD_HOLDS_FAIL_POLICY;
    expect(resolveConfig().holdFailPolicy).toBe("deny");
  });

  it("holdFailPolicy reads AGENTGUARD_HOLDS_FAIL_POLICY=allow", () => {
    process.env.AGENTGUARD_HOLDS_FAIL_POLICY = "allow";
    expect(resolveConfig().holdFailPolicy).toBe("allow");
  });

  it("holdFailPolicy unknown value falls back to deny", () => {
    process.env.AGENTGUARD_HOLDS_FAIL_POLICY = "bogus";
    expect(resolveConfig().holdFailPolicy).toBe("deny");
  });

  it("holdsEnabled defaults to false when AGENTGUARD_HOLDS_ENABLED is unset", () => {
    delete process.env.AGENTGUARD_HOLDS_ENABLED;
    expect(resolveConfig().holdsEnabled).toBe(false);
  });

  it("holdsEnabled is false when AGENTGUARD_HOLDS_ENABLED=\"false\" (case-insensitive)", () => {
    process.env.AGENTGUARD_HOLDS_ENABLED = "false";
    expect(resolveConfig().holdsEnabled).toBe(false);
    process.env.AGENTGUARD_HOLDS_ENABLED = "FALSE";
    expect(resolveConfig().holdsEnabled).toBe(false);
  });

  it("holdsEnabled is true only when env var is exactly 'true' (case-insensitive)", () => {
    process.env.AGENTGUARD_HOLDS_ENABLED = "true";
    expect(resolveConfig().holdsEnabled).toBe(true);
    process.env.AGENTGUARD_HOLDS_ENABLED = "TRUE";
    expect(resolveConfig().holdsEnabled).toBe(true);
    process.env.AGENTGUARD_HOLDS_ENABLED = "1";
    expect(resolveConfig().holdsEnabled).toBe(false);
    process.env.AGENTGUARD_HOLDS_ENABLED = "anything";
    expect(resolveConfig().holdsEnabled).toBe(false);
  });
});

describe("resolveConfig ingest API selection (#299)", () => {
  const ORIGINAL_ENV = { ...process.env };

  beforeEach(() => {
    for (const key of Object.keys(process.env)) {
      if (key.startsWith("AGENTGUARD_")) delete process.env[key];
    }
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (key.startsWith("AGENTGUARD_")) delete process.env[key];
    }
    Object.assign(process.env, ORIGINAL_ENV);
  });

  it("defaults to the timeplusd ingest API with the default workspace", () => {
    const cfg = resolveConfig();
    expect(cfg.ingestApi).toBe("timeplusd");
    expect(cfg.workspace).toBe("default");
  });

  it("reads AGENTGUARD_INGEST_API=rest from env", () => {
    process.env.AGENTGUARD_INGEST_API = "rest";
    expect(resolveConfig().ingestApi).toBe("rest");
  });

  it("falls back to timeplusd for an unknown AGENTGUARD_INGEST_API value", () => {
    process.env.AGENTGUARD_INGEST_API = "bogus";
    expect(resolveConfig().ingestApi).toBe("timeplusd");
  });

  it("ignores case and whitespace in AGENTGUARD_INGEST_API", () => {
    process.env.AGENTGUARD_INGEST_API = " REST ";
    expect(resolveConfig().ingestApi).toBe("rest");
  });

  it("reads AGENTGUARD_WORKSPACE from env", () => {
    process.env.AGENTGUARD_WORKSPACE = "ws-42";
    expect(resolveConfig().workspace).toBe("ws-42");
  });
});
