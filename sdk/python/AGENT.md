# AGENT.md

This file provides guidance to AI coding agents when working with code in this directory.

## What This Project Is

The AgentGuard Python SDK (`agentguard`) is a library providing OpenTelemetry-based tracing and security monitoring for AI coding agents. It provides:

- **Singleton tracer** with OTLP HTTP exporter
- **Auto-instrumentation** for OpenAI SDK and other LLM providers
- **Span management** with session/run/task context tracking
- **Security holds** for synchronous tool-call blocking (planned)

## Development Commands

```bash
uv sync --dev          # install all deps (including pytest, ruff) in editable mode
uv run pytest          # run all tests
uv run pytest tests/ -v   # run tests verbosely
uv run ruff check src/ tests/    # lint
uv run ruff check --fix src/ tests/   # auto-fix lint issues
uv build               # build wheel + sdist into dist/
```

## Architecture

```
src/agentguard/
  __init__.py        — public API surface (exports AgentGuard, AgentGuardSpan, SpanContext, SpanManager)
  client.py          — AgentGuard singleton + OTel tracer
  constants.py       — OTel semantic conventions (GenAIAttribute, AgentGuardAttribute, EnvName)
  instrumentor/      — auto-instrumentation for LLM providers and AI frameworks
    __init__.py
    base.py          — init(), disable(), instrumentor registry
    openai.py        — OpenAI SDK auto-instrumentor

src/agentguard/spans/          (planned)
  span.py            — AgentGuardSpan wrapper, SpanContext, SpanManager

tests/
  test_client.py
  test_instrumentor/
```

### AgentGuard (client.py)

- **Singleton** via `__new__` with double-checked locking for thread safety.
- On first call, creates a `TracerProvider` with an `OTLPSpanExporter` and `BatchSpanProcessor`.
- Exposes `self.tracer` — the raw OTel `Tracer` instance.
- `start_span(name, session_id, run_id, task_id, **attributes)` — creates a span (must call `.end()`)
- `start_as_current_span(name, session_id, run_id, task_id, **attributes)` — context manager (auto-ends)
- `enable_instrumentors(disabled)` — auto-instrument detected LLM providers

### SpanContext (spans/span.py)

Frozen dataclass identifying a trace span's execution scope:
```python
@dataclass(frozen=True)
class SpanContext:
    session_id: str | None = None
    run_id: str | None = None
    task_id: str | None = None
```

### AgentGuardSpan (spans/span.py)

Wrapper around an OTel span that tracks agent execution scope:
- Delegates all OTel `Span` operations to the underlying span
- Adds `span_context` property for session/run/task tracking
- Properties: `name`, `attributes`, `context`, `is_recording()`, `set_attribute()`, etc.

### SpanManager (spans/span.py)

Manages span creation and lifecycle per SpanContext:
- `start_span(name, session_id, run_id, task_id)` — creates span, pushes to per-context stack
- `end_span(session_id, run_id, task_id)` — pops and ends top span
- `current_span(session_id, run_id, task_id)` — returns top span or None
- `end_all()` — ends all open spans across all contexts

Parent-child relationships are handled via OpenTelemetry's context system:
- First span in a context is the root
- Subsequent spans inherit parent from `use_span(parent_span, end_on_exit=False)`

## OpenTelemetry Context Patterns

### Creating parent-child spans

```python
from opentelemetry.trace import use_span

# Using AgentGuard's start_as_current_span (auto-sets parent from context)
with ag.start_as_current_span("parent") as parent:
    with ag.start_as_current_span("child") as child:
        # child automatically has parent as parent
        pass

# Or manually with use_span
with use_span(parent_span, end_on_exit=False):
    child = tracer.start_span("child")  # inherits parent
```

### Setting explicit parent context

```python
from opentelemetry.trace import set_span_in_context
from opentelemetry.context import get_current

ctx = set_span_in_context(parent_span, get_current())
with ag.start_as_current_span("child", context=ctx) as child:
    pass
```

## Testing

- Tests use `unittest.mock` to patch `OTLPSpanExporter` and `BatchSpanProcessor` so no network calls are made.
- A `reset_singleton` autouse fixture resets `AgentGuard._instance` and `AgentGuard._lock` before/after each test.
- Tests cover: singleton identity, env resolution, span creation, auto-instrumentation, instrumentor registration.

## Conventions

- Follow ruff rules: E, F, I, N, W, UP (import sorting, no unused vars, pyupgrade).
- `requires-python >= 3.10, < 4`.
- Tests live in `tests/`, discovered by pytest via `pyproject.toml`.
- Build system: hatchling.
