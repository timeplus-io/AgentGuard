"""Translate Codex hook events into AgentGuard SDK calls.

Codex invokes one short-lived command per hook event and writes a single
JSON object to stdin. Every object carries ``hook_event_name`` and
``session_id``; turn-scoped events also carry ``turn_id``. This module only
does the translation — the SDK owns session bookkeeping and span export.

Two generations of handlers live here:

* ``SessionStart``, ``SessionEnd``, ``UserPromptSubmit``, ``PreToolUse`` and
  ``PostToolUse`` drive the SDK's lifecycle hooks, which emit the ``session``,
  ``llm`` and ``tool`` spans.
* The other nine events each emit one span named after the hook (see
  :data:`HOOK_SPAN_NAMES`) through :meth:`HookSink.emit_span`. They never set
  ``openinference.span.kind``, so a consumer counting LLM calls by that
  attribute still sees one ``llm`` span per prompt, however many model request
  attempts the turn needed.

Codex hooks reference: https://learn.chatgpt.com/docs/hooks
"""

from __future__ import annotations

import json
from collections.abc import Callable
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
    ) -> None: ...

    def mark_start(self, key: str) -> None: ...

    def take_start(self, key: str) -> int | None: ...


#: Codex hook name -> span name, for the events emitted as their own span.
HOOK_SPAN_NAMES = {
    "PreModelRequest": "pre_model_request",
    "PostModelRequest": "post_model_request",
    "Stop": "stop",
    "Interrupt": "interrupt",
    "SubagentStart": "subagent_start",
    "SubagentStop": "subagent_stop",
    "PermissionRequest": "permission_request",
    "PreCompact": "pre_compact",
    "PostCompact": "post_compact",
}

#: ``PostModelRequest.token_usage`` field -> span attribute (OpenInference names).
TOKEN_USAGE_ATTRIBUTES = {
    "input_tokens": "llm.token_count.prompt",
    "output_tokens": "llm.token_count.completion",
    "total_tokens": "llm.token_count.total",
    "cached_input_tokens": "llm.token_count.prompt_details.cache_read",
    "cache_write_input_tokens": "llm.token_count.prompt_details.cache_write",
    "reasoning_output_tokens": "llm.token_count.completion_details.reasoning",
}


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


def _common_attributes(event: dict[str, Any]) -> dict[str, Any]:
    """Attributes shared by every span this module emits itself.

    ``None`` values are dropped by :func:`_emit`, so absent optional fields
    simply leave the attribute off the span.
    """
    return {
        "agentguard.hook_name": HOOK_SPAN_NAMES[event["hook_event_name"]],
        "llm.model_name": event.get("model"),
        "codex.permission_mode": event.get("permission_mode"),
        # Subagent hooks carry the *parent* session id; these two identify
        # the child. Codex also sets them on other hooks fired inside a
        # subagent (model requests, compaction, permission requests).
        "agentguard.subagent.id": event.get("agent_id"),
        "agentguard.subagent.type": event.get("agent_type"),
    }


def _emit(
    event: dict[str, Any],
    hooks: HookSink,
    extra: dict[str, Any] | None = None,
    *,
    task_id: str | None = None,
    start_time_ns: int | None = None,
    error: str | None = None,
) -> None:
    attributes = {**_common_attributes(event), **(extra or {})}
    hooks.emit_span(
        HOOK_SPAN_NAMES[event["hook_event_name"]],
        session_id=event["session_id"],
        run_id=event.get("turn_id"),
        task_id=task_id,
        attributes={k: v for k, v in attributes.items() if v is not None},
        start_time_ns=start_time_ns,
        error=error,
    )


def _pair_key(kind: str, event: dict[str, Any], *parts: Any) -> str:
    """Key that pairs a ``Pre*`` event with its ``Post*`` event."""
    fields = (kind, event["session_id"], event.get("turn_id"), event.get("agent_id"), *parts)
    return "|".join("" if f is None else str(f) for f in fields)


def _session_start(event: dict[str, Any], hooks: HookSink) -> None:
    hooks.on_session_start(event["session_id"])


def _session_end(event: dict[str, Any], hooks: HookSink) -> None:
    hooks.on_session_end(event["session_id"])


def _user_prompt_submit(event: dict[str, Any], hooks: HookSink) -> None:
    hooks.on_llm_end(
        session_id=event["session_id"],
        task_id=event.get("turn_id"),
        model_name=event.get("model"),
        user_message=event.get("prompt"),
    )


def _pre_tool_use(event: dict[str, Any], hooks: HookSink) -> None:
    hooks.on_pre_tool(
        session_id=event["session_id"],
        run_id=event.get("turn_id"),
        task_id=event.get("tool_use_id"),
    )


def _post_tool_use(event: dict[str, Any], hooks: HookSink) -> None:
    hooks.on_post_tool(
        session_id=event["session_id"],
        run_id=event.get("turn_id"),
        task_id=event.get("tool_use_id"),
        tool_name=event.get("tool_name"),
        tool_input=_as_string(event.get("tool_input")),
        tool_result=_as_string(event.get("tool_response")),
    )


def _model_request_attributes(event: dict[str, Any]) -> dict[str, Any]:
    return {
        "codex.request.id": event.get("request_id"),
        "codex.request.attempt": event.get("attempt_number"),
        "codex.request.kind": event.get("kind"),
        "codex.thread_id": event.get("thread_id"),
    }


def _model_request_key(event: dict[str, Any]) -> str:
    # One request_id spans every retry of a generation; attempt_number tells
    # the attempts apart, so each PostModelRequest finds its own start time.
    return _pair_key("model", event, event.get("request_id"), event.get("attempt_number"))


def _pre_model_request(event: dict[str, Any], hooks: HookSink) -> None:
    hooks.mark_start(_model_request_key(event))
    _emit(event, hooks, _model_request_attributes(event), task_id=event.get("request_id"))


def _post_model_request(event: dict[str, Any], hooks: HookSink) -> None:
    extra = _model_request_attributes(event)
    status = event.get("status")
    error = event.get("error")
    extra["codex.request.status"] = status
    extra["codex.request.error"] = error

    usage = event.get("token_usage")
    if isinstance(usage, dict):
        for field, attribute in TOKEN_USAGE_ATTRIBUTES.items():
            value = usage.get(field)
            # bool is an int subclass; a stray true/false is not a token count.
            if isinstance(value, int) and not isinstance(value, bool):
                extra[attribute] = value

    _emit(
        event,
        hooks,
        extra,
        task_id=event.get("request_id"),
        start_time_ns=hooks.take_start(_model_request_key(event)),
        error=(error or "model request failed") if status == "failed" else None,
    )


def _stop(event: dict[str, Any], hooks: HookSink) -> None:
    _emit(
        event,
        hooks,
        {
            "output.value": event.get("last_assistant_message"),
            "codex.stop_hook_active": event.get("stop_hook_active"),
        },
    )


def _interrupt(event: dict[str, Any], hooks: HookSink) -> None:
    _emit(event, hooks)


def _subagent_start(event: dict[str, Any], hooks: HookSink) -> None:
    hooks.mark_start(_pair_key("subagent", event))
    _emit(event, hooks, task_id=event.get("agent_id"))


def _subagent_stop(event: dict[str, Any], hooks: HookSink) -> None:
    _emit(
        event,
        hooks,
        {
            "output.value": event.get("last_assistant_message"),
            "codex.stop_hook_active": event.get("stop_hook_active"),
            "codex.agent_transcript_path": event.get("agent_transcript_path"),
        },
        task_id=event.get("agent_id"),
        start_time_ns=hooks.take_start(_pair_key("subagent", event)),
    )


def _permission_request(event: dict[str, Any], hooks: HookSink) -> None:
    tool_input = event.get("tool_input")
    description = tool_input.get("description") if isinstance(tool_input, dict) else None
    _emit(
        event,
        hooks,
        {
            "tool.name": event.get("tool_name"),
            "tool.parameters": _as_string(tool_input),
            "codex.permission.description": _as_string(description),
        },
        task_id=event.get("tool_use_id"),
    )


def _pre_compact(event: dict[str, Any], hooks: HookSink) -> None:
    hooks.mark_start(_pair_key("compact", event))
    _emit(event, hooks, {"codex.compact.trigger": event.get("trigger")})


def _post_compact(event: dict[str, Any], hooks: HookSink) -> None:
    _emit(
        event,
        hooks,
        {"codex.compact.trigger": event.get("trigger")},
        start_time_ns=hooks.take_start(_pair_key("compact", event)),
    )


#: Every Codex hook this plugin observes. ``hooks.json`` must register exactly
#: these events (``tests/test_events.py`` checks the two agree).
HANDLERS: dict[str, Callable[[dict[str, Any], HookSink], None]] = {
    "SessionStart": _session_start,
    "SessionEnd": _session_end,
    "UserPromptSubmit": _user_prompt_submit,
    "PreToolUse": _pre_tool_use,
    "PostToolUse": _post_tool_use,
    "PreModelRequest": _pre_model_request,
    "PostModelRequest": _post_model_request,
    "Stop": _stop,
    "Interrupt": _interrupt,
    "SubagentStart": _subagent_start,
    "SubagentStop": _subagent_stop,
    "PermissionRequest": _permission_request,
    "PreCompact": _pre_compact,
    "PostCompact": _post_compact,
}

SUPPORTED_EVENTS = frozenset(HANDLERS)


def dispatch(event: dict[str, Any], hooks: HookSink) -> bool:
    """Handle one Codex hook *event*.

    Returns ``True`` when the event was handled and ``False`` for events this
    plugin does not observe. Unrecognised events are ignored so a newer Codex
    can add hooks without breaking the plugin. Only ``hook_event_name`` and
    ``session_id`` are required; every other payload field is read with
    ``.get`` so an older or newer Codex that omits it still produces a span.
    """
    handler = HANDLERS.get(event.get("hook_event_name"))
    if handler is None:
        return False

    handler(event, hooks)
    return True
