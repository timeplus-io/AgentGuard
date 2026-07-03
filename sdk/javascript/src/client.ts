import * as os from "node:os";
import {
  context,
  createContextKey,
  trace,
  type Span,
  type Tracer,
} from "@opentelemetry/api";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-proto";
import { Resource } from "@opentelemetry/resources";
import {
  BatchSpanProcessor,
  NodeTracerProvider,
} from "@opentelemetry/sdk-trace-node";
import { SEMRESATTRS_SERVICE_NAME } from "@opentelemetry/semantic-conventions";
import {
  AGENTGUARD_TRACER_NAME,
  EnvName,
  ResourceAttribute,
} from "./constants.js";
import { initInstrumentors } from "./instrumentor/index.js";

import { SESSION_ID } from "@arizeai/openinference-semantic-conventions";
import { ContextAttributes } from "@arizeai/openinference-core";

const { [SESSION_ID]: SESSION_ID_KEY } = ContextAttributes;

export interface AgentGuardOptions {
  serviceName?: string;
  agentId?: string;
  deploymentId?: string;
  deploymentName?: string;
  otlpEndpoint?: string;
}

export interface SpanOptions {
  sessionId?: string;
  runId?: string;
  taskId?: string;
}

export class AgentGuard {
  private static _instance: AgentGuard | null = null;

  private _serviceName: string;
  readonly _agentId: string;
  readonly _deploymentId: string;
  readonly _deploymentName: string;
  private _provider: NodeTracerProvider;
  readonly tracer: Tracer;

  private constructor(options: AgentGuardOptions = {}) {
    const agentId =
      options.agentId ?? process.env[EnvName.AGENT_ID] ?? os.hostname();
    const deploymentId =
      options.deploymentId ?? process.env[EnvName.DEPLOYMENT_ID] ?? "local";
    const deploymentName =
      options.deploymentName ??
      process.env[EnvName.DEPLOYMENT_NAME] ??
      "Local Dev";
    const serviceName = options.serviceName ?? AGENTGUARD_TRACER_NAME;

    this._serviceName = serviceName;
    this._agentId = agentId;
    this._deploymentId = deploymentId;
    this._deploymentName = deploymentName;

    if (options.otlpEndpoint) {
      process.env.OTEL_EXPORTER_OTLP_ENDPOINT = options.otlpEndpoint;
    }

    const resource = new Resource({
      [SEMRESATTRS_SERVICE_NAME]: serviceName,
      [ResourceAttribute.AGENT_ID]: agentId,
      [ResourceAttribute.DEPLOYMENT_ID]: deploymentId,
      [ResourceAttribute.DEPLOYMENT_NAME]: deploymentName,
    });

    this._provider = new NodeTracerProvider({ resource });
    this._provider.addSpanProcessor(
      new BatchSpanProcessor(new OTLPTraceExporter()),
    );
    this._provider.register();

    this.tracer = trace.getTracer(serviceName);
  }

  static getInstance(options: AgentGuardOptions = {}): AgentGuard {
    if (!AgentGuard._instance) {
      AgentGuard._instance = new AgentGuard(options);
    }
    return AgentGuard._instance;
  }

  async enableInstrumentors(
    opts: { enabled?: string[]; disabled?: string[]; baseUrl?: string } = {},
  ): Promise<void> {
    await initInstrumentors(
      this._provider,
      opts.enabled ?? [],
      opts.disabled ?? [],
      true,
      opts.baseUrl,
    );
  }

  startSpan(name: string, opts: SpanOptions = {}): Span {
    const span = this.tracer.startSpan(name);
    const parentSessionId = context.active().getValue(SESSION_ID_KEY) as
      | string
      | undefined;

    if (opts.sessionId != null) {
      span.setAttribute("session.id", opts.sessionId);
    } else if (parentSessionId != null) {
      span.setAttribute("session.id", parentSessionId);
    }

    if (opts.runId != null) span.setAttribute("run.id", opts.runId);
    if (opts.taskId != null) span.setAttribute("task.id", opts.taskId);

    return span;
  }

  async withSpan<T>(
    name: string,
    opts: SpanOptions,
    fn: (span: Span) => Promise<T>,
  ): Promise<T> {
    const parentSessionId = context.active().getValue(SESSION_ID_KEY) as
      | string
      | undefined;

    return this.tracer.startActiveSpan(name, async (span) => {
      let ctx = context.active();

      if (opts.sessionId != null) {
        span.setAttribute("session.id", opts.sessionId);
        if (opts.sessionId !== parentSessionId) {
          span.setAttribute("agentguard.span.kind", "SESSION");
          ctx = ctx.setValue(SESSION_ID_KEY, opts.sessionId);
        }
      } else if (parentSessionId != null) {
        span.setAttribute("session.id", parentSessionId);
      }

      if (opts.runId != null) span.setAttribute("run.id", opts.runId);
      if (opts.taskId != null) span.setAttribute("task.id", opts.taskId);

      try {
        return await context.with(ctx, () => fn(span));
      } finally {
        span.end();
      }
    });
  }

  get provider(): NodeTracerProvider {
    return this._provider;
  }

  flush(): Promise<void> {
    return this._provider.forceFlush();
  }

  shutdown(): Promise<void> {
    return this._provider.shutdown();
  }

  /** Reset singleton — test use only. */
  static _reset(): void {
    AgentGuard._instance = null;
  }
}
