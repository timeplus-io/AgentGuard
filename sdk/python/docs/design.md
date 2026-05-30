# Python SDK Design Document

## Overview

The AgentGuard Python SDK (`agentguard`) is an **OpenTelemetry-native observability and security SDK** for integrating third-party AI agent programs with AgentGuard. It provides:

1. **Singleton tracer** — thread-safe OTel tracer with OTLP HTTP exporter
2. **Auto-instrumentation** — `enable_instrumentor()` automatically traces OpenAI SDK and other LLM providers
3. **Manual span APIs** — context managers and helpers with session/run/task context
> 2026-06-02 updates: Tried implement internal span tracer by session/run/task. But it turns out hard to maintain and integrate with otel context. 
> Maybe should let user to implement the ops relationship by the ids. We only provide some util functions.

4. ~~**REST API client** — full access to AgentGuard server (holds, threats, rules, cost)~~

5. **Security holds** — synchronous tool-call blocking with human-in-the-loop approval (planned)

The SDK targets Python 3.10+.

---

## Architecture

```
src/agentguard/
  __init__.py          — public API surface
  client.py            — AgentGuard singleton + tracer + span APIs
  constants.py         — OTel semantic conventions (GenAIAttribute, EnvName)

  instrumentor/        — auto-instrumentation for LLM providers
    __init__.py
    base.py            — init(), disable(), instrumentor registry
    openai.py         — OpenAI SDK auto-instrumentor

  # Planned (phase 2+)
  ~~spans.py             — AgentGuardSpan, SpanContext, SpanManager~~
  holds.py             — synchronous hold client
  rest.py              — AgentGuard REST API client
  ingest.py            — Timeplus HTTP ingest client
  cim.py               — CIM event builders

tests/
  test_client.py       — AgentGuard singleton + span tests
  test_instrumentor/   — auto-instrumentation tests
```

---

## Component Design

### 1. AgentGuard Singleton (`client.py`)

Thread-safe singleton that initializes the OTel tracer on first call.

```python
from agentguard import AgentGuard

ag = AgentGuard(
    agent_id="my-agent",
    deployment_id="prod",
    deployment_name="Production",
    otlp_endpoint="http://localhost:4318",
)
```

**Configuration priority**: constructor args > env vars > defaults.

| Parameter | Env Var | Default |
|-----------|---------|---------|
| `agent_id` | `AGENTGUARD_AGENT_ID` | hostname |
| `deployment_id` | `AGENTGUARD_DEPLOYMENT_ID` | `"local"` |
| `deployment_name` | `AGENTGUARD_DEPLOYMENT_NAME` | `"Local Dev"` |
| `otlp_endpoint` | `OTEL_EXPORTER_OTLP_ENDPOINT` | OTel default |

### 2. Span Creation

Client provides opentelemetry-like APIs to create tracing spans. User could use the returned span / context manager with regular otel operations.

Currently, it is just a very thin wrapper of the configured tracer and return just OTel span. But available for future extension.

#### `start_span()` — manual (must call `.end()`)

```python
span = ag.start_span(
    "session_start",
    session_id="ses_abc123",
    run_id="run_xyz",
    task_id="task_001",
)
span.set_attribute("user.id", "user_123")
# ... do work ...
span.end()
```

#### `start_as_current_span()` — context manager (auto-ends)

```python
with ag.start_as_current_span(
    "tool_call",
    session_id="ses_abc123",
    run_id="run_xyz",
    task_id="task_001",
) as span:
    span.set_attribute("tool.name", "bash")
    # ... do work ...
```

Parent-child relationships are handled automatically via OTel's context system. The first span in a context is the root; subsequent spans inherit parent from the OTel context.

### 3. Parent-Child Span Relationships

OTel's context system automatically propagates parent-child relationships:

```python
# Child inherits parent automatically via OTel context
with ag.start_as_current_span("parent") as parent:
    with ag.start_as_current_span("child") as child:
        # child has parent as its parent
        pass
```

#### Explicit parent context with `set_span_in_context`

```python
from opentelemetry.trace import set_span_in_context
from opentelemetry.context import get_current

# Create a context with parent_span as the active span
ctx = set_span_in_context(parent_span, get_current())

with ag.start_as_current_span("child", context=ctx) as child:
    pass
```

### 4. Auto-Instrumentation (`instrumentor/`)

`enable_instrumentor()` automatically patches known LLM provider SDKs to capture spans:

```python
ag.enable_instrumentor()  # instruments all detected providers
ag.enable_instrumentor(disabled_instrumentors=["openai"])  # skip specific
```

**Supported instrumentors:**

| Provider | Spans captured |
|----------|----------------|
| OpenAI | `openai.chat`, `openai.embeddings` with model, tokens, cost |

**Span attributes captured:**

```python
# GenAI conventions
"gen_ai.system" = "openai"
"gen_ai.request.model" = "gpt-4o"
"gen_ai.response.model" = "gpt-4o-2024-05-13"
"gen_ai.usage.input_tokens" = 150
"gen_ai.usage.output_tokens" = 320

# AgentGuard extensions (example)
"agentguard.cost.input" = 0.0025
"agentguard.cost.output" = 0.005
"agentguard.cost.total" = 0.0075
"agentguard.usage.cache_read_tokens" = 100
"agentguard.usage.cache_write_tokens" = 50
```

---

## Planned Components

### Synchronous Hold Client (`holds.py`)

Blocks a tool call pending human approval via the AgentGuard server.

```python
class HoldClient:
    def request_hold(
        self,
        session_id: str,
        agent_id: str,
        tool_name: str,
        tool_input: dict,
        deployment_id: str = "",
        run_id: str = "",
        agent_type: str = "",
        deployment_name: str = "",
    ) -> HoldResponse:
        """POST /api/holds. Blocks until decision or timeout (540s)."""
```

## Not Planned Things

Below components will NOT be included in SDK.

### Timeplus Client

Ingest events to `agentguard_hook_events` stream.

### REST API Client

Thin wrapper around AgentGuard REST endpoints.
* Holds
* Threats
* Rules
* Cost
* Agents
* Metrics
* Notifications

---

## Dependencies

| Package | Version | Purpose |
|---------|---------|---------|
| `opentelemetry-api` | `>=1.42,<2` | OTel tracing API |
| `opentelemetry-sdk` | `>=1.42,<2` | OTel tracing SDK |
| `opentelemetry-exporter-otlp-proto-http` | `>=1.42,<2` | OTLP HTTP exporter |
| `wrapt` | `>=1.14,<2` | Module patching for instrumentors |

Dev: `pytest`, `ruff`, `openai` (for testing instrumentor).

---

## Testing Strategy

| Component | Approach |
|-----------|----------|
| `AgentGuard` | Mock `OTLPSpanExporter` and `BatchSpanProcessor`; test singleton identity, env resolution, span creation, thread safety |
| `instrumentor/*` | Mock LLM provider clients; verify spans have correct attributes, usage, cost |
| Planned components | Mock HTTP responses; verify typed response parsing, error handling |
