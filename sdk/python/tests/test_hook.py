import json
import threading
from unittest import mock

import pytest
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from agentguard.client import AgentGuard
from agentguard.hook import AgentGuardHooks


@pytest.fixture
def hooks(tmp_path, monkeypatch):
    """AgentGuardHooks whose spans land in memory and whose store is in tmp_path."""
    monkeypatch.setenv("HOME", str(tmp_path))
    AgentGuard._instance = None
    AgentGuard._lock = threading.Lock()
    with (
        mock.patch("agentguard.client.OTLPSpanExporter", autospec=True),
        mock.patch("agentguard.client.BatchSpanProcessor", autospec=True),
    ):
        instance = AgentGuardHooks(agent_id="test-agent")
        exporter = InMemorySpanExporter()
        instance._handler._provider.add_span_processor(SimpleSpanProcessor(exporter))
        instance.exported = exporter
        yield instance
    AgentGuard._instance = None


def only_span(hooks):
    (span,) = hooks.exported.get_finished_spans()
    return span


def test_llm_end_sets_openinference_model_name(hooks):
    hooks.on_llm_end("s1", provider="openai", model_name="gpt-5")

    attributes = only_span(hooks).attributes
    assert attributes["llm.model_name"] == "gpt-5"
    assert attributes["llm.provider"] == "openai"
    assert "llm.module_name" not in attributes
    assert attributes["openinference.span.kind"] == "LLM"


def test_llm_end_writes_the_prompt_as_json_with_a_user_role(hooks):
    hooks.on_llm_end("s1", user_message='say "hi" — it\'s fine')

    value = only_span(hooks).attributes["input.value"]
    assert json.loads(value) == {
        "messages": [{"role": "user", "content": 'say "hi" — it\'s fine'}]
    }


def test_llm_end_writes_the_assistant_message(hooks):
    hooks.on_llm_end("s1", assistant_message="done")

    attributes = only_span(hooks).attributes
    assert attributes["llm.output_messages.0.message.content"] == "done"


def test_llm_end_omits_attributes_that_were_not_given(hooks):
    hooks.on_llm_end("s1")

    attributes = only_span(hooks).attributes
    for name in (
        "llm.model_name",
        "llm.provider",
        "input.value",
        "llm.output_messages.0.message.content",
    ):
        assert name not in attributes


def test_llm_end_sets_session_run_and_task(hooks):
    hooks.on_llm_end("s1", run_id="r1", task_id="t1")

    span = only_span(hooks)
    assert span.name == "llm"
    assert span.attributes["session.id"] == "s1"
    assert span.attributes["run.id"] == "r1"
    assert span.attributes["task.id"] == "t1"


def test_llm_end_starts_at_llm_start(hooks):
    hooks.on_llm_start("s1", run_id="r1")
    started = hooks._store["llm"]["s1r1None"]

    hooks.on_llm_end("s1", run_id="r1")

    span = only_span(hooks)
    assert span.start_time == started
    assert span.end_time >= started
