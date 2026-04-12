import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { resolveConfig } from "./config.js";

describe("resolveConfig", () => {
  const ORIGINAL_ENV = { ...process.env };

  beforeEach(() => {
    for (const key of Object.keys(process.env)) {
      if (key.startsWith("AGENTSHIELD_")) delete process.env[key];
    }
  });

  afterEach(() => {
    for (const key of Object.keys(process.env)) {
      if (key.startsWith("AGENTSHIELD_")) delete process.env[key];
    }
    Object.assign(process.env, ORIGINAL_ENV);
  });

  it("returns defaults when no env vars are set", () => {
    const cfg = resolveConfig();
    expect(cfg.timeplusUrl).toBe("http://localhost:3218");
    expect(cfg.stream).toBe("agentshield_hook_events");
    expect(cfg.username).toBe("proton");
    expect(cfg.password).toBe("");
    expect(cfg.deploymentId).toBe("default");
    expect(cfg.deploymentName).toBe("Default");
    expect(cfg.agentId).toBe("");
  });

  it("reads AGENTSHIELD_TIMEPLUS_URL from env", () => {
    process.env.AGENTSHIELD_TIMEPLUS_URL = "http://remote:3218";
    expect(resolveConfig().timeplusUrl).toBe("http://remote:3218");
  });

  it("reads AGENTSHIELD_AGENT_ID from env", () => {
    process.env.AGENTSHIELD_AGENT_ID = "my-laptop";
    expect(resolveConfig().agentId).toBe("my-laptop");
  });

  it("reads AGENTSHIELD_DEPLOYMENT_ID from env", () => {
    process.env.AGENTSHIELD_DEPLOYMENT_ID = "prod";
    expect(resolveConfig().deploymentId).toBe("prod");
  });

  it("reads AGENTSHIELD_DEPLOYMENT_NAME from env", () => {
    process.env.AGENTSHIELD_DEPLOYMENT_NAME = "Production";
    expect(resolveConfig().deploymentName).toBe("Production");
  });

  it("reads AGENTSHIELD_USERNAME and AGENTSHIELD_PASSWORD from env", () => {
    process.env.AGENTSHIELD_USERNAME = "admin";
    process.env.AGENTSHIELD_PASSWORD = "secret";
    const cfg = resolveConfig();
    expect(cfg.username).toBe("admin");
    expect(cfg.password).toBe("secret");
  });

  it("reads AGENTSHIELD_STREAM from env", () => {
    process.env.AGENTSHIELD_STREAM = "custom_stream";
    expect(resolveConfig().stream).toBe("custom_stream");
  });
});
