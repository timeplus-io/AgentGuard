"""Translate Codex hook events into AgentGuard SDK calls.

Codex invokes one short-lived command per hook event and writes a single
JSON object to stdin. Every object carries ``hook_event_name`` and
``session_id``; turn-scoped events also carry ``turn_id``. This module only
does the translation — the SDK owns session bookkeeping and span export.

Codex hooks reference: https://learn.chatgpt.com/docs/hooks
"""

from __future__ import annotations

import json
from typing import Any, Protocol


class HookSink(Protocol):
    """The subset of ``agentguard.hook.AgentGuardHooks`` this plugin uses."""

    def on_session_start(self, session_id: str) -> None: ...

    def on_session_end(self, session_id: str) -> None: ...

    def on_llm_end(
        self,
        session_id: str,
        *,
        task_id: str | None = None,
        model_name: str | None = None,
        user_message: str | None = None,
    ) -> None: ...

    def on_pre_tool(
        self,
        session_id: str,
        *,
        run_id: str | None = None,
        task_id: str | None = None,
    ) -> None: ...

    def on_post_tool(
        self,
        *,
        session_id: str,
        run_id: str | None = None,
        task_id: str | None = None,
        tool_name: str | None = None,
        tool_input: str | None = None,
        tool_result: str | None = None,
    ) -> None: ...


def _as_string(value: Any) -> str | None:
    """Render a Codex payload field for a span attribute.

    Strings pass through unchanged; dicts/lists become JSON (not Python
    ``repr``) so the Timeplus-side JSON functions can parse ``tool.parameters``
    and ``output.value``. ``None`` stays ``None`` so the SDK omits the
    attribute.
    """
    if value is None:
        return None
    if isinstance(value, str):
        return value
    return json.dumps(value, ensure_ascii=False)


def dispatch(event: dict[str, Any], hooks: HookSink) -> bool:
    """Handle one Codex hook *event*.

    Returns ``True`` when the event was handled and ``False`` for events this
    plugin does not observe yet. Unrecognised events are ignored so a newer
    Codex can add hooks without breaking the plugin. Optional payload fields
    are read with ``.get`` because the exact per-hook shapes are still being
    verified (see ``docs/todo.md``).
    """
    event_name = event.get("hook_event_name")
    if not event_name:
        return False

    if event_name == "SessionStart":
        hooks.on_session_start(event["session_id"])

    elif event_name == "SessionEnd":
        hooks.on_session_end(event["session_id"])

    elif event_name == "UserPromptSubmit":
        hooks.on_llm_end(
            session_id=event["session_id"],
            task_id=event.get("turn_id"),
            model_name=event.get("model"),
            user_message=event.get("prompt"),
        )

    elif event_name == "PreToolUse":
        hooks.on_pre_tool(
            session_id=event["session_id"],
            run_id=event.get("turn_id"),
            task_id=event.get("tool_use_id"),
        )

    elif event_name == "PostToolUse":
        hooks.on_post_tool(
            session_id=event["session_id"],
            run_id=event.get("turn_id"),
            task_id=event.get("tool_use_id"),
            tool_name=event.get("tool_name"),
            tool_input=_as_string(event.get("tool_input")),
            tool_result=_as_string(event.get("tool_response")),
        )

    else:
        return False

    return True
