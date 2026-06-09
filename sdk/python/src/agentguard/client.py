from __future__ import annotations

import os
import socket
import threading
from contextlib import contextmanager

from opentelemetry import trace
from opentelemetry.context import attach, detach, get_current, get_value, set_value
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import SERVICE_NAME, Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from agentguard.constants import AGENTGUARD_TRACER_NAME, EnvName, ResourceAttribute
from agentguard.instrumentor import init as _init_instrumentor


class AgentGuard:
    _instance: AgentGuard | None = None
    _lock: threading.Lock = threading.Lock()

    def __new__(
        cls,
        *,
        service_name: str | None = None,
        agent_id: str | None = None,
        deployment_id: str | None = None,
        deployment_name: str | None = None,
        otlp_endpoint: str | None = None,
    ) -> AgentGuard:
        with cls._lock:
            if cls._instance is None:
                instance = super().__new__(cls)
                instance._initialize_instance(
                    service_name=service_name,
                    agent_id=agent_id,
                    deployment_id=deployment_id,
                    deployment_name=deployment_name,
                    otlp_endpoint=otlp_endpoint,
                )

                cls._instance = instance

            return cls._instance

    def _initialize_instance(
        self,
        *,
        service_name: str | None = None,
        agent_id: str | None = None,
        deployment_id: str | None = None,
        deployment_name: str | None = None,
        otlp_endpoint: str | None = None,
    ):
        resolved_agent_id = agent_id or os.environ.get(EnvName.AGENT_ID) or socket.gethostname()
        resolved_deployment_id = deployment_id or os.environ.get(EnvName.DEPLOYMENT_ID, "local")
        resolved_deployment_name = deployment_name or os.environ.get(
            EnvName.DEPLOYMENT_NAME, "Local Dev"
        )

        self._service_name = service_name or AGENTGUARD_TRACER_NAME
        self._agent_id = resolved_agent_id
        self._deployment_id = resolved_deployment_id
        self._deployment_name = resolved_deployment_name

        resource = Resource(
            attributes={
                SERVICE_NAME: self._service_name,
                ResourceAttribute.AGENT_ID: resolved_agent_id,
                ResourceAttribute.DEPLOYMENT_ID: resolved_deployment_id,
                ResourceAttribute.DEPLOYMENT_NAME: resolved_deployment_name,
            }
        )

        if otlp_endpoint is not None:
            os.environ["OTEL_EXPORTER_OTLP_ENDPOINT"] = otlp_endpoint

        self._provider = TracerProvider(resource=resource)
        self._provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))

        self.tracer = trace.get_tracer(self._service_name, tracer_provider=self._provider)

    def enable_instrumentors(
        self,
        *,
        enabled: list[str] | None = None,
        disabled: list[str] | None = None
    ) -> None:
        _init_instrumentor(
            tracer_provider=self._provider,
            enabled_instrumentors=enabled or [],
            disabled_instrumentors=disabled or [],
        )

    def flush(self):
        if self._provider is not None:
            self._provider.force_flush()

    def shutdown(self):
        self.flush()
        if self._provider is not None:
            self._provider.shutdown()

    def start_span(
        self,
        name: str,
        *,
        session_id: str | None = None,
        run_id: str | None = None,
        task_id: str | None = None,
        **kwargs,
    ) -> trace.Span:
        span = self.tracer.start_span(name, **kwargs)
        if session_id is not None:
            span.set_attribute("session.id", session_id)
        elif (parent_session_id := get_value("session.id")) is not None:
            span.set_attribute("session.id", str(parent_session_id))

        if run_id is not None:
            span.set_attribute("run.id", run_id)
        if task_id is not None:
            span.set_attribute("task.id", task_id)

        return span

    @contextmanager
    def start_as_current_span(
        self,
        name: str,
        *,
        session_id: str | None = None,
        run_id: str | None = None,
        task_id: str | None = None,
        **kwargs,
    ):
        parent_session_id = get_value("session.id")
        token = None
        with self.tracer.start_as_current_span(name, **kwargs) as span:
            if session_id is not None:
                span.set_attribute("session.id", session_id)
                token = self._attach_context(session_id)
                if session_id != parent_session_id:
                    span.set_attribute("agentguard.span.kind", "SESSION")
            elif parent_session_id is not None:
                span.set_attribute("session.id", str(parent_session_id))

            if run_id is not None:
                span.set_attribute("run.id", run_id)
            if task_id is not None:
                span.set_attribute("task.id", task_id)

            try:
                yield span
            finally:
                if token is not None:
                    detach(token)

    def _attach_context(self, session_id: str | None):
        if session_id is not None:
            ctx = set_value("session.id", session_id, get_current())
            return attach(ctx)
        return None
