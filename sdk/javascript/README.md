# AgentGuard JavaScript SDK

JavaScript/TypeScript SDK for [AgentGuard](https://github.com/timeplus-io/AgentGuard). It instruments a custom AI agent with OpenTelemetry and exports spans over OTLP/HTTP to the [AgentGuard Timeplus app](https://github.com/timeplus-io/apps/tree/main/apps/agentguard) (default `http://localhost:4318`).

## Installation

```bash
npm install @timeplus/agentguard
```

Requires Node.js 18 or later.

## Usage

```ts
import { AgentGuard } from "@timeplus/agentguard";

const ag = AgentGuard.getInstance({
  agentId: "my-agent",
  deploymentId: "local",
  deploymentName: "Local Dev",
  otlpEndpoint: "http://localhost:4318",
});

// Auto-instrument the LLM SDKs that are installed (openai, anthropic, langchain).
await ag.enableInstrumentors();

await ag.withSpan("session", { sessionId: "session-123" }, async () => {
  await ag.withSpan("task", { taskId: "task-1" }, async (span) => {
    span.setAttribute("input.value", "hello");
    // ... call your model or tools here ...
  });
});

await ag.flush();
await ag.shutdown();
```

`getInstance()` returns a process-wide singleton; the options are only read on the first call.

## Configuration

| Option | Environment variable | Default |
|---|---|---|
| `agentId` | `AGENTGUARD_AGENT_ID` | hostname |
| `deploymentId` | `AGENTGUARD_DEPLOYMENT_ID` | `local` |
| `deploymentName` | `AGENTGUARD_DEPLOYMENT_NAME` | `Local Dev` |
| `otlpEndpoint` | `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` |
| `serviceName` | — | `agentguard-sdk` |

Options take precedence over environment variables.

## Development

```bash
make build   # npm ci + bundle into dist/
make pack    # list what would be published
```

See [`examples/openai`](examples/openai) for a complete multi-step workflow.

## Publishing

Bump `version` in `package.json`, merge, then publish a GitHub Release tagged `sdk-js-v<version>`; the `Publish JavaScript SDK` workflow builds and publishes to npm. `make publish-dry-run` rehearses it locally.

## License

Apache-2.0
