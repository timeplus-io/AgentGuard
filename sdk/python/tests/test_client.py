import os
import socket
import threading
from unittest import mock

import pytest
from opentelemetry import trace
from opentelemetry.sdk.trace import Tracer, TracerProvider

from agentguard.client import AgentGuard
from agentguard.constants import EnvName


@pytest.fixture(autouse=True)
def reset_singleton():
    AgentGuard._instance = None
    AgentGuard._lock = threading.Lock()
    yield
    AgentGuard._instance = None


@pytest.fixture
def mock_exporter():
    with (
        mock.patch("agentguard.client.OTLPSpanExporter", autospec=True),
        mock.patch("agentguard.client.BatchSpanProcessor", autospec=True),
    ):
        yield


def test_singleton_returns_same_instance(mock_exporter):
    a1 = AgentGuard(agent_id="test-agent")
    a2 = AgentGuard(agent_id="test-agent")
    assert a1 is a2


def test_singleton_ignores_second_agent_id(mock_exporter):
    a1 = AgentGuard(agent_id="first")
    a2 = AgentGuard(agent_id="second")
    assert a1 is a2
    assert a1._agent_id == "first"


def test_tracer_is_otel_tracer(mock_exporter):
    ag = AgentGuard(agent_id="test-agent")
    assert isinstance(ag.tracer, Tracer)


def test_tracer_provider_is_registered_globally(mock_exporter):
    AgentGuard(agent_id="test-svc")
    provider = trace.get_tracer_provider()
    assert isinstance(provider, TracerProvider)


def test_start_span_creates_valid_span(mock_exporter):
    ag = AgentGuard(agent_id="test-agent")
    span = ag.start_span("test-operation", key="value")
    assert span.name == "test-operation"
    assert span.is_recording()
    span.end()


def test_span_context_manager(mock_exporter):
    ag = AgentGuard(agent_id="test-agent")
    with ag.start_as_current_span("ctx-operation", component="auth") as s:
        assert s.name == "ctx-operation"
        assert s.is_recording()
    assert not s.is_recording()


def test_thread_safety_same_instance(mock_exporter):
    results = []

    def create():
        results.append(AgentGuard(agent_id="test-agent"))

    threads = [threading.Thread(target=create) for _ in range(20)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()

    first = results[0]
    for r in results[1:]:
        assert r is first
        assert r.tracer is first.tracer


def test_agent_id_from_parameter(mock_exporter):
    ag = AgentGuard(agent_id="my-agent")
    assert ag._agent_id == "my-agent"


def test_agent_id_from_env(mock_exporter):
    with mock.patch.dict(os.environ, {EnvName.AGENT_ID: "env-agent"}):
        ag = AgentGuard()
        assert ag._agent_id == "env-agent"


def test_agent_id_from_hostname(mock_exporter):
    with mock.patch.dict(os.environ, {}, clear=True):
        ag = AgentGuard()
        assert ag._agent_id == socket.gethostname()


def test_agent_id_parameter_overrides_env(mock_exporter):
    with mock.patch.dict(os.environ, {EnvName.AGENT_ID: "env-agent"}):
        ag = AgentGuard(agent_id="param-agent")
        assert ag._agent_id == "param-agent"


def test_deployment_id_from_parameter(mock_exporter):
    ag = AgentGuard(agent_id="test-agent", deployment_id="deploy-1")
    assert ag._deployment_id == "deploy-1"


def test_deployment_id_from_env(mock_exporter):
    with mock.patch.dict(os.environ, {EnvName.DEPLOYMENT_ID: "env-deploy"}):
        ag = AgentGuard(agent_id="test-agent")
        assert ag._deployment_id == "env-deploy"


def test_deployment_id_default_is_local(mock_exporter):
    with mock.patch.dict(os.environ, {}, clear=True):
        ag = AgentGuard(agent_id="test-agent")
        assert ag._deployment_id == "local"


def test_deployment_name_from_parameter(mock_exporter):
    ag = AgentGuard(agent_id="test-agent", deployment_name="prod-cluster")
    assert ag._deployment_name == "prod-cluster"


def test_deployment_name_from_env(mock_exporter):
    with mock.patch.dict(os.environ, {EnvName.DEPLOYMENT_NAME: "staging"}):
        ag = AgentGuard(agent_id="test-agent")
        assert ag._deployment_name == "staging"


def test_deployment_name_default(mock_exporter):
    with mock.patch.dict(os.environ, {}, clear=True):
        ag = AgentGuard(agent_id="test-agent")
        assert ag._deployment_name == "Local Dev"


def test_enable_instrumentor_registers_openai(mock_exporter):
    """Test that enable_instrumentor() registers the OpenAI instrumentor."""
    from agentguard.instrumentor.base import _instrumentors

    _instrumentors.clear()
    ag = AgentGuard(agent_id="test-agent")
    ag.enable_instrumentor()
    assert "openai" in _instrumentors


def test_enable_instrumentor_with_disabled(mock_exporter):
    """Test that enable_instrumentor() respects disabled_instrumentors."""
    from agentguard.instrumentor.base import _instrumentors

    _instrumentors.clear()
    ag = AgentGuard(agent_id="test-agent")
    ag.enable_instrumentor(disabled_instrumentors=["openai"])
    assert "openai" not in _instrumentors
