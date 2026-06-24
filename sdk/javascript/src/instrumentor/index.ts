import { createRequire } from "module";
import type { TracerProvider } from "@opentelemetry/api";
import { registerInstrumentations } from "@opentelemetry/instrumentation";

let _require: NodeRequire;
try {
  _require = createRequire(import.meta.url);
} catch {
  _require = require;
}

interface InstrumentorInfo {
  name: string;
  packageName: string;
  className: string;
  targetModule: string;
}

const INSTRUMENTORS: InstrumentorInfo[] = [
  {
    name: "anthropic",
    packageName: "@openinference/instrumentation-anthropic",
    className: "AnthropicInstrumentor",
    targetModule: "@anthropic-ai/sdk",
  },
  {
    name: "openai",
    packageName: "@arizeai/openinference-instrumentation-openai",
    className: "OpenAIInstrumentation",
    targetModule: "openai",
  },
  {
    name: "langchain",
    packageName: "@openinference/instrumentation-langchain",
    className: "LangChainInstrumentor",
    targetModule: "@langchain/core",
  },
];

export async function initInstrumentors(
  tracerProvider: TracerProvider,
  enabledInstrumentors: string[],
  disabledInstrumentors: string[],
  tracingEnabled = true,
  baseUrl?: string,
): Promise<void> {
  if (!tracingEnabled) return;

  const consumerRequire: NodeRequire = baseUrl
    ? createRequire(baseUrl)
    : _require;

  for (const info of INSTRUMENTORS) {
    const shouldEnable =
      (enabledInstrumentors.length === 0 ||
        enabledInstrumentors.includes(info.name)) &&
      !disabledInstrumentors.includes(info.name) &&
      _isModuleAvailable(info.targetModule, consumerRequire);

    if (!shouldEnable) continue;

    const instrumentations: any[] = [];

    try {
      const resolved = consumerRequire.resolve(info.packageName);
      const mod = await import(resolved);
      const InstrumentorClass = mod[info.className];
      if (InstrumentorClass) {
        console.info(`[agentguard] ${info.className} enabled`);
        const instrumentor = new InstrumentorClass({
          tracerProvider: tracerProvider,
        });

        // ESM modules are loaded before registerInstrumentations can intercept them;
        // manuallyInstrument patches the already-loaded export directly.
        if (typeof instrumentor.manuallyInstrument === "function") {
          try {
            const targetResolved = consumerRequire.resolve(info.targetModule);
            const targetMod = await import(targetResolved);
            instrumentor.manuallyInstrument(targetMod);
            console.info(`[agentguard] Manually instrument ${info.className}`);
          } catch (e) {
            console.warn(`[agentguard] Failed to manually instrument ${info.className}: ${e}`);
          }
        }

        instrumentations.push(instrumentor);
      }
    } catch (e) {
      console.warn(`[agentguard] Failed to enable ${info.className}: ${e}`);
    }

    if (instrumentations.length > 0) {
      registerInstrumentations({
        instrumentations: instrumentations,
        tracerProvider: tracerProvider,
      });
    }
  }
}

function _isModuleAvailable(
  moduleName: string,
  resolver: NodeRequire,
): boolean {
  try {
    resolver.resolve(moduleName);
    return true;
  } catch {
    return false;
  }
}
