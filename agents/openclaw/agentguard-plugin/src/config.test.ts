import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { resolveConfig } from "./config.js";

describe("resolveConfig", () => {
  const ENV_KEYS = [
    "AGENTGUARD_TIMEPLUS_URL",
    "AGENTGUARD_STREAM",
    "AGENTGUARD_FLUSH_MS",
    "AGENTGUARD_BATCH_SIZE",
    "AGENTGUARD_URL",
    "AGENTGUARD_HOLDS_FAIL_POLICY",
    "AGENTGUARD_HOLDS_ENABLED",
  ];

  beforeEach(() => {
    ENV_KEYS.forEach((k) => delete process.env[k]);
  });

  afterEach(() => {
    ENV_KEYS.forEach((k) => delete process.env[k]);
  });

  it("returns all defaults when called with no arguments", () => {
    const cfg = resolveConfig();
    expect(cfg.timeplusUrl).toBe("http://localhost:3218");
    expect(cfg.stream).toBe("agentguard_hook_events");
    expect(cfg.flushMs).toBe(2000);
    expect(cfg.batchSize).toBe(50);
  });

  it("applies pluginConfig values over defaults", () => {
    const cfg = resolveConfig({
      timeplusUrl: "http://timeplus:3218",
      stream: "my_stream",
      flushMs: 5000,
      batchSize: 100,
    });
    expect(cfg.timeplusUrl).toBe("http://timeplus:3218");
    expect(cfg.stream).toBe("my_stream");
    expect(cfg.flushMs).toBe(5000);
    expect(cfg.batchSize).toBe(100);
  });

  it("env vars override pluginConfig", () => {
    process.env.AGENTGUARD_TIMEPLUS_URL = "http://env-host:3218";
    process.env.AGENTGUARD_STREAM = "env_stream";
    process.env.AGENTGUARD_FLUSH_MS = "1000";
    process.env.AGENTGUARD_BATCH_SIZE = "10";
    const cfg = resolveConfig({
      timeplusUrl: "http://plugin-host:3218",
      stream: "plugin_stream",
      flushMs: 9999,
      batchSize: 999,
    });
    expect(cfg.timeplusUrl).toBe("http://env-host:3218");
    expect(cfg.stream).toBe("env_stream");
    expect(cfg.flushMs).toBe(1000);
    expect(cfg.batchSize).toBe(10);
  });

  it("treats empty-string AGENTGUARD_TIMEPLUS_URL as unset, falls back to default", () => {
    process.env.AGENTGUARD_TIMEPLUS_URL = "";
    const cfg = resolveConfig();
    expect(cfg.timeplusUrl).toBe("http://localhost:3218");
  });

  it("falls back to default flushMs when env var is not a valid positive number", () => {
    process.env.AGENTGUARD_FLUSH_MS = "not-a-number";
    const cfg = resolveConfig();
    expect(cfg.flushMs).toBe(2000);
  });

  it("flushMs and batchSize are numbers, not strings", () => {
    process.env.AGENTGUARD_FLUSH_MS = "3000";
    process.env.AGENTGUARD_BATCH_SIZE = "25";
    const cfg = resolveConfig();
    expect(typeof cfg.flushMs).toBe("number");
    expect(typeof cfg.batchSize).toBe("number");
    expect(cfg.flushMs).toBe(3000);
    expect(cfg.batchSize).toBe(25);
  });

  it("returns default deploymentId and deploymentName when called with no arguments", () => {
    const cfg = resolveConfig();
    expect(cfg.deploymentId).toBe("default");
    expect(cfg.deploymentName).toBe("Default");
  });

  it("applies pluginConfig deploymentId and deploymentName over defaults", () => {
    const cfg = resolveConfig({ deploymentId: "prod", deploymentName: "Production" });
    expect(cfg.deploymentId).toBe("prod");
    expect(cfg.deploymentName).toBe("Production");
  });

  it("agentguardUrl defaults to http://localhost:8080", () => {
    delete process.env.AGENTGUARD_URL;
    expect(resolveConfig().agentguardUrl).toBe("http://localhost:8080");
  });

  it("holdFailPolicy defaults to deny and reads env override to allow", () => {
    delete process.env.AGENTGUARD_HOLDS_FAIL_POLICY;
    expect(resolveConfig().holdFailPolicy).toBe("deny");
    process.env.AGENTGUARD_HOLDS_FAIL_POLICY = "allow";
    expect(resolveConfig().holdFailPolicy).toBe("allow");
  });

  it("holdsEnabled defaults to true", () => {
    delete process.env.AGENTGUARD_HOLDS_ENABLED;
    expect(resolveConfig().holdsEnabled).toBe(true);
  });

  it("holdsEnabled reads AGENTGUARD_HOLDS_ENABLED=false to disable", () => {
    process.env.AGENTGUARD_HOLDS_ENABLED = "false";
    expect(resolveConfig().holdsEnabled).toBe(false);
  });

  it("holdsEnabled reads pluginConfig.holdsEnabled boolean", () => {
    delete process.env.AGENTGUARD_HOLDS_ENABLED;
    expect(resolveConfig({ holdsEnabled: false }).holdsEnabled).toBe(false);
    expect(resolveConfig({ holdsEnabled: true }).holdsEnabled).toBe(true);
  });
});
