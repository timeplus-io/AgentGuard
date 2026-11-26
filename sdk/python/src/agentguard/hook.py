"""AgentGuard Hooks — session lifecycle hooks with OpenTelemetry trace emission.

Provides :class:`AgentGuardHooks` that can be used by the CLI and other
programs to emit trace spans when session-start / session-end events
are received.  Cleanup (flush, shutdown, store persistence) happens
automatically when the object is garbage-collected.
"""

import json
import os
import time
from typing import Any

import agentguard


class AgentGuardHooks:
    """Lifecycle hooks that emit OpenTelemetry trace spans for agent sessions.

    The hooks maintain a small JSON store on disk that records the
    wall-clock start time of each session so that the span emitted at
    ``session_end`` carries the true session duration.

    Cleanup (flush, shutdown, store persistence) happens automatically
    via the destructor — callers do **not** need to explicitly call
    ``close()`` or ``flush()``.

    Parameters
    ----------
    agent_id:
        Unique identifier for the agent (default ``"hostname"``).
    deployment_id:
        Identifier for the deployment (default ``"local"``).
    deployment_name:
        Human-readable name for the deployment (default ``"Local Dev"``).
    endpoint:
        OTLP HTTP endpoint to export spans to
        (default ``"http://localhost:4318"``).
    """

    def __init__(
        self,
        agent_id: str | None = None,
        deployment_id: str | None = None,
        deployment_name: str | None = None,
        endpoint: str | None = None,
    ) -> None:
        self._agent_id = agent_id or "hostname"
        self._handler = agentguard.AgentGuard(
            agent_id=agent_id,
            deployment_id=deployment_id,
            deployment_name=deployment_name,
            otlp_endpoint=endpoint,
        )
        self._store_folder = os.path.join(os.path.expanduser("~"), ".agentguard", self._agent_id)
        self._store_types = ["session", "llm", "tool"]
        self._load_store()

    def __del__(self) -> None:
        self._cleanup()

    # ------------------------------------------------------------------
    # Store helpers
    # ------------------------------------------------------------------

    def _load_store(self):
        self._store = {}
        for store_type in self._store_types:
            try:
                with open(os.path.join(self._store_folder, store_type), encoding="utf-8") as f:
                    self._store[store_type] = json.load(f)
            except Exception:
                self._store[store_type] = {}

    def _save_store(self):
        os.makedirs(self._store_folder, exist_ok=True)
        for store_type in self._store_types:
            try:
                with open(os.path.join(self._store_folder, store_type), "w", encoding="utf-8") as f:
                    json.dump(self._store[store_type], f)
            except Exception:
                pass

    def _store_key(self, session_id, run_id, task_id):
        return str(session_id) + str(run_id) + str(task_id)

    def _cleanup(self) -> None:
        """Flush spans, shut down the provider, and persist the store."""
        try:
            self._handler.flush()
        except Exception:
            pass
        try:
            self._handler.shutdown()
        except Exception:
            pass
        try:
            self._save_store()
        except Exception:
            pass

    # ------------------------------------------------------------------
    # Lifecycle hooks
    # ------------------------------------------------------------------

    def on_session_start(
        self, session_id: str, run_id: str | None = None, task_id: str | None = None
    ) -> None:
        """Record the start timestamp for *session_id*.

        Returns the recorded start time in nanoseconds so callers can
        pass it explicitly to :meth:`on_session_end` if desired.
        """
        store_key = self._store_key(session_id, run_id, task_id)
        start_ns = time.time_ns()
        self._store["session"][store_key] = start_ns

    def on_session_end(
        self, session_id: str, run_id: str | None = None, task_id: str | None = None
    ) -> None:
        """Emit a ``session_end`` trace span with the session duration.

        The span start time is taken from the store written by
        :meth:`on_session_start`, so the emitted span reflects the
        true wall-clock duration of the session.
        """
        store_key = self._store_key(session_id, run_id, task_id)
        start_ns = self._store["session"].pop(store_key, None)

        kwargs: dict[str, Any] = {}
        if start_ns is not None:
            kwargs["start_time"] = start_ns

        with self._handler.start_as_current_span(
            name="session",
            session_id=session_id,
            run_id=run_id,
            task_id=task_id,
            **kwargs,
        ) as span:
            span.set_attribute("agentguard.span.kind", "SESSION")

    def on_llm_start(self, session_id: str, run_id: str | None = None, task_id: str | None = None):
        store_key = self._store_key(session_id, run_id, task_id)
        start_ns = time.time_ns()
        self._store["llm"][store_key] = start_ns

    def on_llm_end(
        self,
        session_id: str,
        run_id=None,
        task_id=None,
        provider=None,
        model_name=None,
        user_message=None,
        assistant_message=None,
    ):
        store_key = self._store_key(session_id, run_id, task_id)
        start_ns = self._store["llm"].pop(store_key, None)

        kwargs: dict[str, Any] = {}
        if start_ns is not None:
            kwargs["start_time"] = start_ns

        with self._handler.start_as_current_span(
            name="llm",
            session_id=session_id,
            run_id=run_id,
            task_id=task_id,
            **kwargs,
        ) as span:
            span.set_attribute("openinference.span.kind", "LLM")
            if provider is not None:
                span.set_attribute("llm.provider", provider)
            if model_name is not None:
                # OpenInference name; "llm.module_name" (sent up to 0.3.0)
                # was a typo that left the model empty downstream.
                span.set_attribute("llm.model_name", model_name)
            if user_message is not None:
                # JSON with a role, not a Python repr: consumers parse this
                # and pick the last message whose role is "user".
                span.set_attribute(
                    "input.value",
                    json.dumps(
                        {"messages": [{"role": "user", "content": str(user_message)}]},
                        ensure_ascii=False,
                    ),
                )
            if assistant_message is not None:
                span.set_attribute("llm.output_messages.0.message.content", str(assistant_message))

    def on_pre_tool(self, session_id: str, run_id: str | None = None, task_id: str | None = None):
        store_key = self._store_key(session_id, run_id, task_id)
        start_ns = time.time_ns()
        self._store["tool"][store_key] = start_ns

    def on_post_tool(self,
                     session_id: str,
                     run_id: str | None = None,
                     task_id: str | None = None,
                     tool_name: str | None = None,
                     tool_input: str | None = None,
                     tool_result: str | None = None):
        store_key = self._store_key(session_id, run_id, task_id)
        start_ns = self._store["tool"].pop(store_key, None)

        kwargs: dict[str, Any] = {}
        if start_ns is not None:
            kwargs["start_time"] = start_ns

        with self._handler.start_as_current_span(
            name="tool",
            session_id=session_id,
            run_id=run_id,
            task_id=task_id,
            **kwargs,
        ) as span:
            span.set_attribute("openinference.span.kind", "TOOL")
            if tool_name is not None:
                span.set_attribute("tool.name", tool_name)
            if tool_input is not None:
                span.set_attribute("tool.parameters", tool_input)
            if tool_result is not None:
                span.set_attribute("output.value", tool_result)
