# AgentGuard Python SDK

[![PyPI version](https://img.shields.io/pypi/v/agentguard)](https://pypi.org/project/agentguard/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

Python SDK for [AgentGuard](https://github.com/timeplus-io/agentguard) — a real-time security monitoring, governance, and synchronous tool-call blocking platform for AI coding agents.

## Features

- **OpenTelemetry Tracing** — thread-safe singleton tracer with OTLP HTTP exporter
- **Auto-Instrumentation** — `enable_instrumentors()` automatically traces OpenAI SDK calls
- **Span Helpers** — manual and context-manager APIs with session/run/task context
- **Agent Scope Tracking** — `SpanContext` ties spans to session_id, run_id, task_id

## Installation

```bash
pip install agentguard
```

## CLI Tools

Organize and govern AI coding agents:

**./agentguard_cli** - OpenTelemetry tracing for sessions and tasks

- `session_start` - Start a new agent session
  ```bash
  ./agentguard_cli session_start result session-123
  ```
- `session_end` - End a session with status
  ```bash
  ./agentguard_cli session_end session-123 depleted incomplete
  ```
- `session_trace` - Create trace for an existing session
  ```bash
  ./agentguard_cli session_trace trace-id-456 resource-789
  ```

### Attributes
Sessions get extended with these attributes:
- `session.id` - The session ID
- `session.status` - Final evaluation result
- `run.id` - Associated code run ID
- `task.id` - Associated specific code task ID

## Configuration

Requires Python 3.10+.

## Quick Start

```python
from agentguard import AgentGuard

# Create the singleton tracer
ag = AgentGuard(
    agent_id="claude-code-1",
    deployment_id="prod",
    otlp_endpoint="http://localhost:4318",
)

# Enable auto-instrumentation for OpenAI SDK
ag.enable_instrumentors()

# Manual span (must call .end())
span = ag.start_span(
    "session_start",
    session_id="ses_abc123",
    run_id="run_xyz",
    task_id="task_001",
)
# ... do work ...
span.end()

# Context-manager span (auto-ends, parent automatically set from OTel context)
with ag.start_as_current_span(
    "tool_call",
    session_id="ses_abc123",
    run_id="run_xyz",
    task_id="task_001",
):
    # ... do work ...
    pass

# Flush and shutdown
ag.shutdown()
```

## Context Manager with Explicit Parent

```python
from agentguard import AgentGuard
from opentelemetry.trace import set_span_in_context
from opentelemetry.context import get_current

ag = AgentGuard()

with ag.start_as_current_span("parent") as parent:
    # Set explicit parent context for child
    ctx = set_span_in_context(parent, get_current())
    with ag.start_as_current_span("child", context=ctx) as child:
        pass
```

## Configuration

| Parameter | Env Var | Default |
|-----------|---------|---------|
| `agent_id` | `AGENTGUARD_AGENT_ID` | hostname |
| `deployment_id` | `AGENTGUARD_DEPLOYMENT_ID` | `"local"` |
| `deployment_name` | `AGENTGUARD_DEPLOYMENT_NAME` | `"Local Dev"` |
| `otlp_endpoint` | `OTEL_EXPORTER_OTLP_ENDPOINT` | `None` (uses OTel default) |

Parameter values take priority over environment variables.

## Documentation

- [Full AgentGuard documentation](https://github.com/timeplus-io/agentguard)
- [Common Information Model (CIM) spec](docs/cim.md)

## Development

```bash
git clone https://github.com/timeplus-io/agentguard
cd sdk/python
uv sync --dev
ruff check src/ tests/
pytest
```

## License

Apache 2.0
