"""The sink :func:`agentguard_codex.events.dispatch` writes to.

:class:`SdkSink` wraps the AgentGuard Python SDK. The five original hooks go
straight to the SDK's ``AgentGuardHooks`` lifecycle methods; the other events
are emitted as their own span through the SDK client, with ``Pre*``/``Post*``
pairs timed by :class:`StartStore`.
"""

from __future__ import annotations

import hashlib
import os
import time
from typing import Any

# Start times nobody collected (a Post* hook that never ran) are dropped after
# this long, so the store cannot grow without bound.
STALE_AFTER_SECONDS = 24 * 60 * 60


class StartStore:
    """Start timestamps shared between short-lived hook processes.

    Each key is its own file, written once and deleted when read, so hooks
    that run concurrently never rewrite each other's entries.
    """

    def __init__(self, folder: str) -> None:
        self._folder = folder

    def _path(self, key: str) -> str:
        return os.path.join(self._folder, hashlib.sha256(key.encode("utf-8")).hexdigest())

    def mark(self, key: str) -> None:
        try:
            os.makedirs(self._folder, exist_ok=True)
            self._prune()
            with open(self._path(key), "w", encoding="utf-8") as f:
                f.write(str(time.time_ns()))
        except OSError:
            pass  # timing is best effort; the span is still emitted

    def take(self, key: str) -> int | None:
        path = self._path(key)
        try:
            with open(path, encoding="utf-8") as f:
                value = int(f.read().strip())
            os.remove(path)
            return value
        except (OSError, ValueError):
            return None

    def _prune(self) -> None:
        cutoff = time.time() - STALE_AFTER_SECONDS
        with os.scandir(self._folder) as entries:
            for entry in entries:
                try:
                    if entry.is_file() and entry.stat().st_mtime < cutoff:
                        os.remove(entry.path)
                except OSError:
                    pass


class SdkSink:
    """Adapts the AgentGuard SDK to :class:`agentguard_codex.events.HookSink`."""

    def __init__(self, hooks: Any, client: Any, starts: StartStore) -> None:
        self._hooks = hooks
        self._client = client
        self._starts = starts

    def __getattr__(self, name: str) -> Any:
        # on_session_start / on_session_end / on_llm_end / on_pre_tool /
        # on_post_tool are the SDK's own lifecycle hooks.
        return getattr(self._hooks, name)

    def emit_span(
        self,
        name: str,
        *,
        session_id: str,
        run_id: str | None = None,
        task_id: str | None = None,
        attributes: dict[str, Any] | None = None,
        start_time_ns: int | None = None,
        error: str | None = None,
    ) -> None:
        kwargs: dict[str, Any] = {}
        if start_time_ns is not None:
            kwargs["start_time"] = start_time_ns

        # start_span, not start_as_current_span: the latter tags the first
        # span of every process agentguard.span.kind=SESSION, and each hook
        # is its own process.
        span = self._client.start_span(
            name, session_id=session_id, run_id=run_id, task_id=task_id, **kwargs
        )
        try:
            for key, value in (attributes or {}).items():
                span.set_attribute(key, value)
            if error is not None:
                from opentelemetry.trace import Status, StatusCode

                span.set_status(Status(StatusCode.ERROR, error))
        finally:
            span.end()

    def mark_start(self, key: str) -> None:
        self._starts.mark(key)

    def take_start(self, key: str) -> int | None:
        return self._starts.take(key)

    def flush(self) -> None:
        self._client.flush()
