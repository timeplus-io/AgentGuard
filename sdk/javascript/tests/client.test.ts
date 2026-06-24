import os from "node:os";
import { afterEach, beforeEach, describe, expect, jest, test } from "@jest/globals";
import { AgentGuard } from "../src/client.js";
import { EnvName } from "../src/constants.js";

// Prevent real OTLP exports in tests
jest.mock("@opentelemetry/exporter-trace-otlp-http", () => ({
  OTLPTraceExporter: jest.fn().mockImplementation(() => ({
    export: jest.fn((_spans: unknown, cb: (result: { code: number }) => void) => cb({ code: 0 })),
    shutdown: jest.fn<() => Promise<void>>().mockResolvedValue(),
  })),
}));

jest.mock("@opentelemetry/sdk-trace-node", () => {
  const actual = jest.requireActual("@opentelemetry/sdk-trace-node") as Record<string, unknown>;
  return {
    ...actual,
    BatchSpanProcessor: jest.fn().mockImplementation(() => ({
      onStart: jest.fn(),
      onEnd: jest.fn(),
      shutdown: jest.fn<() => Promise<void>>().mockResolvedValue(),
      forceFlush: jest.fn<() => Promise<void>>().mockResolvedValue(),
    })),
  };
});

beforeEach(() => {
  AgentGuard._reset();
  delete process.env[EnvName.AGENT_ID];
  delete process.env[EnvName.DEPLOYMENT_ID];
  delete process.env[EnvName.DEPLOYMENT_NAME];
});

afterEach(() => {
  AgentGuard._reset();
});

describe("singleton", () => {
  test("returns the same instance on repeated calls", () => {
    const a1 = AgentGuard.getInstance({ agentId: "test-agent" });
    const a2 = AgentGuard.getInstance({ agentId: "test-agent" });
    expect(a1).toBe(a2);
  });

  test("ignores options on second call", () => {
    const a1 = AgentGuard.getInstance({ agentId: "first" });
    const a2 = AgentGuard.getInstance({ agentId: "second" });
    expect(a1).toBe(a2);
    expect(a1._agentId).toBe("first");
  });
});

describe("tracer", () => {
  test("exposes an OTel tracer", () => {
    const ag = AgentGuard.getInstance({ agentId: "test-agent" });
    expect(ag.tracer).toBeDefined();
    expect(typeof ag.tracer.startSpan).toBe("function");
  });
});

describe("span creation", () => {
  test("startSpan creates a recording span", () => {
    const ag = AgentGuard.getInstance({ agentId: "test-agent" });
    const span = ag.startSpan("test-operation");
    expect(span.isRecording()).toBe(true);
    span.end();
  });

  test("withSpan ends span after fn resolves", async () => {
    const ag = AgentGuard.getInstance({ agentId: "test-agent" });
    let capturedSpan: any;
    await ag.withSpan("ctx-operation", {}, async (span) => {
      capturedSpan = span;
      expect(span.isRecording()).toBe(true);
    });
    expect(capturedSpan.isRecording()).toBe(false);
  });

  test("withSpan ends span after fn rejects", async () => {
    const ag = AgentGuard.getInstance({ agentId: "test-agent" });
    let capturedSpan: any;
    await expect(
      ag.withSpan("failing-op", {}, async (span) => {
        capturedSpan = span;
        throw new Error("boom");
      }),
    ).rejects.toThrow("boom");
    expect(capturedSpan.isRecording()).toBe(false);
  });
});

describe("configuration resolution", () => {
  test("agentId from parameter", () => {
    const ag = AgentGuard.getInstance({ agentId: "my-agent" });
    expect(ag._agentId).toBe("my-agent");
  });

  test("agentId from env var", () => {
    process.env[EnvName.AGENT_ID] = "env-agent";
    const ag = AgentGuard.getInstance();
    expect(ag._agentId).toBe("env-agent");
  });

  test("agentId defaults to hostname", () => {
    const ag = AgentGuard.getInstance();
    expect(ag._agentId).toBe(os.hostname());
  });

  test("parameter overrides env var", () => {
    process.env[EnvName.AGENT_ID] = "env-agent";
    const ag = AgentGuard.getInstance({ agentId: "param-agent" });
    expect(ag._agentId).toBe("param-agent");
  });

  test("deploymentId from parameter", () => {
    const ag = AgentGuard.getInstance({ agentId: "test", deploymentId: "deploy-1" });
    expect(ag._deploymentId).toBe("deploy-1");
  });

  test("deploymentId from env var", () => {
    process.env[EnvName.DEPLOYMENT_ID] = "env-deploy";
    const ag = AgentGuard.getInstance({ agentId: "test" });
    expect(ag._deploymentId).toBe("env-deploy");
  });

  test("deploymentId defaults to 'local'", () => {
    const ag = AgentGuard.getInstance({ agentId: "test" });
    expect(ag._deploymentId).toBe("local");
  });

  test("deploymentName from parameter", () => {
    const ag = AgentGuard.getInstance({ agentId: "test", deploymentName: "prod-cluster" });
    expect(ag._deploymentName).toBe("prod-cluster");
  });

  test("deploymentName from env var", () => {
    process.env[EnvName.DEPLOYMENT_NAME] = "staging";
    const ag = AgentGuard.getInstance({ agentId: "test" });
    expect(ag._deploymentName).toBe("staging");
  });

  test("deploymentName defaults to 'Local Dev'", () => {
    const ag = AgentGuard.getInstance({ agentId: "test" });
    expect(ag._deploymentName).toBe("Local Dev");
  });
});

describe("concurrency", () => {
  test("returns same instance across concurrent calls", async () => {
    const instances = await Promise.all(
      Array.from({ length: 20 }, () =>
        Promise.resolve(AgentGuard.getInstance({ agentId: "test-agent" })),
      ),
    );
    const first = instances[0];
    for (const inst of instances.slice(1)) {
      expect(inst).toBe(first);
      expect(inst.tracer).toBe(first.tracer);
    }
  });
});
