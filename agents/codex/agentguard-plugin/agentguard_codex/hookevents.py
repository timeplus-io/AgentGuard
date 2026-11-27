"""Hook-event export: one ``agentguard_hook_events`` row per Codex hook.

The alternative to the OTel path (:mod:`agentguard_codex.sink`). It writes the
same stream, with the same columns and ingest settings, as the Claude Code,
OpenClaw and Hermes plugins, and needs only the standard library.

Ingest modes (``AGENTGUARD_INGEST_API``):

* ``timeplusd`` (default): ``POST {url}/timeplusd/v2/ingest/{db}/streams/{stream}``
* ``rest``: the Timeplus REST API,
  ``POST {url}/{workspace}/api/v1beta2/streams/{stream}/ingest?db={db}``

Both take Basic auth and the same ``{"columns", "data"}`` body.
"""

from __future__ import annotations

import json
import os
import time
import urllib.request
from base64 import b64encode
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any

from agentguard_codex.sink import StartStore

AGENT_TYPE = "codex"

#: Must match the app's ``agentguard_hook_events`` DDL, in this order.
COLUMNS = [
    "hook_name",
    "event_time",
    "session_id",
    "run_id",
    "agent_id",
    "deployment_id",
    "deployment_name",
    "session_key",
    "tool_name",
    "provider",
    "model",
    "hook_decision",
    "block_reason",
    "event_data",
    "agent_type",
]

#: ``event_data`` key that marks a row as written by this path. On the OTel
#: path the app bridges some spans into the same stream with the same
#: ``agent_type``; the app's Codex hook views tell the two apart by this key.
EXPORT_MARKER = "agentguard.export"
EXPORT_MARKER_VALUE = "hooks"

#: ``event_data`` key for the time since the opening event of a pair.
DURATION_KEY = "agentguard.duration_ms"

#: ``agentguard.hook_name`` -> ``hook_name`` column, where the stream already
#: has a name for the event (the one the other plugins and the app's bridge use).
HOOK_NAME_OVERRIDES = {"subagent_stop": "subagent_ended"}

# Codex gives SessionEnd and Interrupt hooks 3 seconds, interpreter start
# included, so the request has to give up well before that.
INGEST_TIMEOUT_SECONDS = 2.0


@dataclass(frozen=True)
class IngestConfig:
    """Where and how hook-event rows are written."""

    url: str = "http://localhost:3218"
    database: str = "ag"
    stream: str = "agentguard_hook_events"
    username: str = "proton"
    password: str = ""
    ingest_api: str = "timeplusd"
    workspace: str = "default"

    @classmethod
    def from_env(cls, env: dict[str, str] | None = None) -> IngestConfig:
        """Read the settings the other hook-event plugins use. Empty means unset."""
        env = os.environ if env is None else env
        default = cls()

        def get(name: str, fallback: str) -> str:
            return env.get(name) or fallback

        return cls(
            url=get("AGENTGUARD_TIMEPLUS_URL", default.url),
            database=get("AGENTGUARD_DATABASE", default.database),
            stream=get("AGENTGUARD_STREAM", default.stream),
            username=get("AGENTGUARD_USERNAME", default.username),
            password=env.get("AGENTGUARD_PASSWORD", default.password),
            # Anything but "rest" is the default, as in the other plugins.
            ingest_api=(
                "rest"
                if env.get("AGENTGUARD_INGEST_API", "").strip().lower() == "rest"
                else "timeplusd"
            ),
            workspace=get("AGENTGUARD_WORKSPACE", default.workspace),
        )

    def ingest_url(self) -> str:
        base = self.url.rstrip("/")
        if self.ingest_api == "rest":
            return (
                f"{base}/{self.workspace}/api/v1beta2/streams/{self.stream}"
                f"/ingest?db={self.database}"
            )
        return f"{base}/timeplusd/v2/ingest/{self.database}/streams/{self.stream}"

    def headers(self) -> dict[str, str]:
        headers = {"Content-Type": "application/json"}
        if self.username:
            token = b64encode(f"{self.username}:{self.password}".encode()).decode()
            headers["Authorization"] = f"Basic {token}"
        return headers


class HookEventSink:
    """Adapts hook-event ingest to :class:`agentguard_codex.events.HookSink`.

    Rows are collected and sent in one request by :meth:`close`.
    """

    def __init__(
        self,
        config: IngestConfig,
        starts: StartStore,
        *,
        agent_id: str,
        deployment_id: str,
        deployment_name: str,
        timeout: float = INGEST_TIMEOUT_SECONDS,
    ) -> None:
        self._config = config
        self._starts = starts
        self._agent_id = agent_id
        self._deployment_id = deployment_id
        self._deployment_name = deployment_name
        self._timeout = timeout
        self.rows: list[list[Any]] = []

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
        payload: dict[str, Any] | None = None,
    ) -> None:
        self._add_row(session_id, run_id, attributes or {}, payload or {}, start_time_ns)

    def emit_no_span(
        self,
        *,
        session_id: str,
        run_id: str | None = None,
        attributes: dict[str, Any] | None = None,
        payload: dict[str, Any] | None = None,
    ) -> None:
        self._add_row(session_id, run_id, attributes or {}, payload or {}, None)

    def _add_row(
        self,
        session_id: str,
        run_id: str | None,
        attributes: dict[str, Any],
        payload: dict[str, Any],
        start_time_ns: int | None,
    ) -> None:
        hook_name = attributes.get("agentguard.hook_name") or ""
        data = {**payload, EXPORT_MARKER: EXPORT_MARKER_VALUE}
        if start_time_ns is not None:
            data[DURATION_KEY] = max(0, (time.time_ns() - start_time_ns) // 1_000_000)

        self.rows.append(
            [
                HOOK_NAME_OVERRIDES.get(hook_name, hook_name),
                datetime.now(timezone.utc).isoformat(),
                session_id,
                run_id or session_id,
                self._agent_id,
                self._deployment_id,
                self._deployment_name,
                "",  # session_key: Codex has none
                attributes.get("tool.name") or "",
                attributes.get("llm.provider") or "",
                attributes.get("llm.model_name") or "",
                "observe",
                "",
                json.dumps(data, ensure_ascii=False, default=str),
                AGENT_TYPE,
            ]
        )

    def mark_start(self, key: str) -> None:
        self._starts.mark(key)

    def take_start(self, key: str) -> int | None:
        return self._starts.take(key)

    def close(self) -> None:
        """Send the collected rows; a hook process ends right after this."""
        if not self.rows:
            return
        body = json.dumps({"columns": COLUMNS, "data": self.rows}).encode("utf-8")
        self.rows = []
        request = urllib.request.Request(
            self._config.ingest_url(), data=body, method="POST", headers=self._config.headers()
        )
        with urllib.request.urlopen(request, timeout=self._timeout) as response:
            response.read()
