"""Tests for the OpenAI auto-instrumentor."""

from unittest import mock

import pytest
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from agentguard.constants import AgentGuardAttribute, GenAIAttribute


class _FakeUsage:
    def __init__(self, prompt=100, completion=50, total=150):
        self.prompt_tokens = prompt
        self.completion_tokens = completion
        self.total_tokens = total
        self.prompt_tokens_details = None
        self.completion_tokens_details = None


class _FakeMessage:
    def __init__(self, content="test response", role="assistant"):
        self.content = content
        self.role = role


class _FakeChoice:
    def __init__(self, content="test response", finish_reason="stop"):
        self.message = _FakeMessage(content)
        self.finish_reason = finish_reason


class _FakeChatResponse:
    def __init__(self, model="gpt-4o", content="Hello, world!", usage=None, finish_reason="stop"):
        self.id = "chatcmpl-test"
        self.model = model
        self.choices = [_FakeChoice(content, finish_reason)]
        self.usage = usage or _FakeUsage()


def _reload_openai_completions():
    """Reload the Completions class to get a clean, unpatched version."""
    import sys
    # Clear cached openai modules to get fresh classes
    for key in list(sys.modules.keys()):
        if key.startswith("openai.resources.chat"):
            del sys.modules[key]
    from openai.resources.chat.completions import Completions
    return Completions


@pytest.fixture(autouse=True)
def reset_instrumentation():
    """Reset instrumentation before/after each test."""
    from agentguard.instrumentor.base import _instrumentors, disable
    from agentguard.instrumentor.openai import OpenAIInstrumentor

    disable()
    _instrumentors.clear()
    OpenAIInstrumentor._instrumented = False
    yield
    disable()
    _instrumentors.clear()
    OpenAIInstrumentor._instrumented = False


@pytest.fixture
def exporter():
    """Set up an in-memory span exporter."""
    exp = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exp))
    trace._TRACER_PROVIDER = provider
    yield exp


def _fake_create(**kwargs):
    """Fake wrapped create function that returns a mock response."""
    return _FakeChatResponse(
        model=kwargs.get("model", "gpt-4o"),
        content="Hello!",
        usage=_FakeUsage(prompt=10, completion=5, total=15),
    )


def test_instrumentor_registers():
    """Test that the OpenAI instrumentor registers successfully."""
    from agentguard.instrumentor.base import _instrumentors, init

    init()
    assert "openai" in _instrumentors


def test_instrumentor_is_idempotent():
    """Test that calling instrument twice doesn't double-patch."""
    from agentguard.instrumentor.openai import OpenAIInstrumentor

    instr = OpenAIInstrumentor()
    instr._instrument()
    assert instr._instrumented

    instr._instrument()
    assert instr._instrumented


def test_uninstrument_restores_methods():
    """Test that uninstrument restores original methods."""
    completions_cls = _reload_openai_completions()
    original = completions_cls.create

    from agentguard.instrumentor.openai import OpenAIInstrumentor

    instr = OpenAIInstrumentor()
    instr._instrument()
    assert completions_cls.create is not original

    instr._uninstrument()
    assert completions_cls.create is original


def test_chat_span_has_genai_attributes(exporter):
    """Test that the chat wrapper produces a span with GenAI attributes."""
    from agentguard.instrumentor.openai import _wrap_chat_create

    wrapped = mock.MagicMock(side_effect=_fake_create)
    instance = mock.MagicMock()
    kwargs = {"model": "gpt-4o", "messages": [{"role": "user", "content": "Hi"}]}

    result = _wrap_chat_create(wrapped, instance, (), kwargs)

    assert result.model == "gpt-4o"

    spans = exporter.get_finished_spans()
    assert len(spans) >= 1

    span = spans[-1]
    assert span.name == "openai.chat"
    assert span.attributes[GenAIAttribute.SYSTEM] == "openai"
    assert span.attributes[GenAIAttribute.REQUEST_MODEL] == "gpt-4o"
    assert span.attributes[GenAIAttribute.USAGE_INPUT_TOKENS] == 10
    assert span.attributes[GenAIAttribute.USAGE_OUTPUT_TOKENS] == 5
    assert span.attributes[AgentGuardAttribute.OBSERVATION_TYPE] == "generation"
    assert span.attributes[AgentGuardAttribute.COST_TOTAL] >= 0
    assert span.status.status_code.name == "OK"


def test_chat_span_records_error_on_exception(exporter):
    """Test that exceptions are recorded on the span."""
    from agentguard.instrumentor.openai import _wrap_chat_create

    wrapped = mock.MagicMock(side_effect=RuntimeError("API error"))
    instance = mock.MagicMock()
    kwargs = {"model": "gpt-4o", "messages": [{"role": "user", "content": "Hi"}]}

    with pytest.raises(RuntimeError, match="API error"):
        _wrap_chat_create(wrapped, instance, (), kwargs)

    spans = exporter.get_finished_spans()
    assert len(spans) >= 1

    span = spans[-1]
    assert span.status.status_code.name == "ERROR"


def test_chat_span_captures_request_params(exporter):
    """Test that temperature, top_p, etc. are captured as span attributes."""
    from agentguard.instrumentor.openai import _wrap_chat_create

    wrapped = mock.MagicMock(side_effect=_fake_create)
    instance = mock.MagicMock()
    kwargs = {
        "model": "gpt-4o",
        "messages": [{"role": "user", "content": "Hi"}],
        "temperature": 0.7,
        "top_p": 0.9,
        "max_tokens": 100,
    }

    _wrap_chat_create(wrapped, instance, (), kwargs)

    spans = exporter.get_finished_spans()
    span = spans[-1]
    assert span.attributes[GenAIAttribute.REQUEST_TEMPERATURE] == 0.7
    assert span.attributes[GenAIAttribute.REQUEST_TOP_P] == 0.9
    assert span.attributes[GenAIAttribute.REQUEST_MAX_TOKENS] == 100


def test_chat_span_captures_completion(exporter):
    """Test that the completion text is captured."""
    from agentguard.instrumentor.openai import _wrap_chat_create

    wrapped = mock.MagicMock(side_effect=_fake_create)
    instance = mock.MagicMock()
    kwargs = {"model": "gpt-4o", "messages": [{"role": "user", "content": "Hi"}]}

    _wrap_chat_create(wrapped, instance, (), kwargs)

    spans = exporter.get_finished_spans()
    span = spans[-1]
    assert "Hello!" in span.attributes.get(GenAIAttribute.OUTPUT, "")


def test_cost_estimation():
    """Test cost estimation logic."""
    from agentguard.instrumentor.openai import _estimate_cost

    cost = _estimate_cost("gpt-4o", {"input_tokens": 1_000_000, "output_tokens": 500_000})
    assert cost["input"] == 2.50
    assert cost["output"] == 5.00
    assert cost["total"] == 7.50


def test_model_base_name_extraction():
    """Test that model family is extracted from versioned model strings."""
    from agentguard.instrumentor.openai import _model_base_name

    assert _model_base_name("gpt-4o-2024-05-13") == "gpt-4o"
    assert _model_base_name("gpt-4o-mini-2024-07-18") == "gpt-4o-mini"
    assert _model_base_name("gpt-4.1-nano-2024-01-01") == "gpt-4.1-nano"
    assert _model_base_name("unknown-model") == "unknown-model"


def test_init_with_disabled_instrumentors():
    """Test that specific instrumentors can be disabled."""
    from agentguard.instrumentor.base import _instrumentors, init

    init(disabled_instrumentors=["openai"])
    assert "openai" not in _instrumentors
