"""Translate Codex hook events into AgentGuard SDK calls.

Codex invokes one short-lived command per hook event and writes a single
JSON object to stdin. Every object carries ``hook_event_name`` and
``session_id``; turn-scoped events also carry ``turn_id``. This module only
does the translation — the SDK owns session bookkeeping and span export.

Codex hooks reference: https://learn.chatgpt.com/docs/hooks
"""

from __future__ import annotations

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

    def on_pre_tool(self, session_id: str) -> None: ...

    def on_post_tool(
        self,
        *,
        session_id: str,
        tool_name: str | None = None,
        tool_input: str | None = None,
        tool_result: str | None = None,
    ) -> None: ...


def dispatch(event: dict[str, Any], hooks: HookSink) -> bool:
    """Handle one Codex hook *event*.

    Returns ``True`` when the event was handled and ``False`` for events this
    plugin does not observe yet. Unrecognised events are ignored so a newer
    Codex can add hooks without breaking the plugin.
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
            task_id=event["turn_id"],
            model_name=event["model"],
            user_message=event["prompt"],
        )

    elif event_name == "PreToolUse":
        hooks.on_pre_tool(session_id=event["session_id"])

    elif event_name == "PostToolUse":
        hooks.on_post_tool(
            session_id=event["session_id"],
            tool_name=event["tool_name"],
            tool_input=str(event["tool_input"]),
            tool_result=str(event["tool_response"]),
        )

    else:
        return False

    return True
