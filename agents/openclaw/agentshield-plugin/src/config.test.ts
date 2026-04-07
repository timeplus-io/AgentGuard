import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { resolveConfig } from "./config.js";

describe("resolveConfig", () => {
  const ENV_KEYS = [
    "AGENTSHIELD_TIMEPLUS_URL",
    "AGENTSHIELD_STREAM",
    "AGENTSHIELD_FLUSH_MS",
    "AGENTSHIELD_BATCH_SIZE",
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
    expect(cfg.stream).toBe("agentshield_hook_events");
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
    process.env.AGENTSHIELD_TIMEPLUS_URL = "http://env-host:3218";
    process.env.AGENTSHIELD_STREAM = "env_stream";
    process.env.AGENTSHIELD_FLUSH_MS = "1000";
    process.env.AGENTSHIELD_BATCH_SIZE = "10";
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

  it("flushMs and batchSize are numbers, not strings", () => {
    process.env.AGENTSHIELD_FLUSH_MS = "3000";
    process.env.AGENTSHIELD_BATCH_SIZE = "25";
    const cfg = resolveConfig();
    expect(typeof cfg.flushMs).toBe("number");
    expect(typeof cfg.batchSize).toBe("number");
    expect(cfg.flushMs).toBe(3000);
    expect(cfg.batchSize).toBe(25);
  });
});
