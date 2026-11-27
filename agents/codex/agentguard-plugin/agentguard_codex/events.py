"""Translate Codex hook events into AgentGuard telemetry.

Codex invokes one short-lived command per hook event and writes a single
JSON object to stdin. Every object carries ``hook_event_name`` and
``session_id``; turn-scoped events also carry ``turn_id``. This module only
does the translation — the sink owns the export and start-time bookkeeping.

The translation is the same on both export paths; only the sink differs. The
OTel sink (:class:`agentguard_codex.sink.SdkSink`) turns each call into a span.
The hook-event sink (:class:`agentguard_codex.hookevents.HookEventSink`) turns
each call into one ``agentguard_hook_events`` row, including ``PreToolUse``,
which has no span of its own.

Each event becomes at most one span. Three of them carry the kind attributes
the AgentGuard app's OpenInference views key on:

* ``post_model_request`` is the LLM call (``openinference.span.kind=LLM``):
  one per model request attempt, with the model, token usage and real latency.
* ``tool`` is the tool call (``openinference.span.kind=TOOL``).
* ``session`` is the whole session (``agentguard.span.kind=SESSION``).

The prompt (``user_prompt_submit``) and the final reply (``stop``) are their
own spans and are deliberately *not* LLM-kind, so a consumer counting LLM
calls or summing usage sees model requests only.

Codex hooks reference: https://learn.chatgpt.com/docs/hooks
"""

from __future__ import annotations

import json
from collections.abc import Callable
from typing import Any, Protocol

#: Reported as the provider when the session transcript does not name one.
DEFAULT_PROVIDER = "openai"


class HookSink(Protocol):
    """What :func:`dispatch` needs: emit an event, and pair Pre/Post events.

    ``payload`` is the Codex hook payload as received. A span sink ignores it;
    the hook-event sink stores it as the row's ``event_data``.
    """

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
    ) -> None: ...

    def emit_no_span(
        self,
        *,
        session_id: str,
        run_id: str | None = None,
        attributes: dict[str, Any] | None = None,
        payload: dict[str, Any] | None = None,
    ) -> None:
        """An event that opens a pair and has no span of its own (``PreToolUse``)."""
        ...

    def mark_start(self, key: str) -> None: ...

    def take_start(self, key: str) -> int | None: ...


#: Codex hook name -> span name. ``PreToolUse`` only records a start time.
HOOK_SPAN_NAMES = {
    "SessionStart": "session_start",
    "SessionEnd": "session",
    "UserPromptSubmit": "user_prompt_submit",
    "PostToolUse": "tool",
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

#: Codex hook name -> ``agentguard.hook_name``.
HOOK_NAMES = {
    **HOOK_SPAN_NAMES,
    "SessionEnd": "session_end",
    "PreToolUse": "pre_tool_use",
    "PostToolUse": "post_tool_use",
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


def session_provider(transcript_path: Any) -> str | None:
    """Read the model provider from the first record of a Codex transcript.

    Codex starts each rollout file with a ``session_meta`` record whose
    payload names the ``model_provider``. The transcript format is not a
    stable interface, so anything unexpected yields ``None``.
    """
    if not isinstance(transcript_path, str) or not transcript_path:
        return None
    try:
        with open(transcript_path, encoding="utf-8") as f:
            record = json.loads(f.readline())
        provider = record["payload"]["model_provider"]
    except (OSError, ValueError, KeyError, TypeError):
        return None
    return provider if isinstance(provider, str) and provider else None


def _common_attributes(event: dict[str, Any]) -> dict[str, Any]:
    """Attributes set on every span.

    ``None`` values are dropped by :func:`_emit`, so absent optional fields
    simply leave the attribute off the span.
    """
    model = event.get("model")
    return {
        "agentguard.hook_name": HOOK_NAMES[event["hook_event_name"]],
        "llm.model_name": model,
        # Without llm.provider the app derives the provider from the model
        # name, which for a Codex slug such as "gpt-5-codex" is the slug.
        "llm.provider": (
            (session_provider(event.get("transcript_path")) or DEFAULT_PROVIDER)
            if model
            else None
        ),
        "cwd": event.get("cwd"),
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
    span: bool = True,
) -> None:
    attributes = {**_common_attributes(event), **(extra or {})}
    attributes = {k: v for k, v in attributes.items() if v is not None}
    if not span:
        hooks.emit_no_span(
            session_id=event["session_id"],
            run_id=event.get("turn_id"),
            attributes=attributes,
            payload=event,
        )
        return
    hooks.emit_span(
        HOOK_SPAN_NAMES[event["hook_event_name"]],
        session_id=event["session_id"],
        run_id=event.get("turn_id"),
        task_id=task_id,
        attributes=attributes,
        start_time_ns=start_time_ns,
        error=error,
        payload=event,
    )


def _pair_key(kind: str, event: dict[str, Any], *parts: Any) -> str:
    """Key that pairs an opening event with its closing event."""
    fields = (kind, event["session_id"], event.get("turn_id"), event.get("agent_id"), *parts)
    return "|".join("" if f is None else str(f) for f in fields)


def _session_key(event: dict[str, Any]) -> str:
    # SessionEnd carries no turn id, so the key is the session alone.
    return "session|" + str(event["session_id"])


def _session_start(event: dict[str, Any], hooks: HookSink) -> None:
    # Emitted right away so a running session is visible downstream; the
    # `session` span that covers the whole session only exists once it ends.
    hooks.mark_start(_session_key(event))
    _emit(event, hooks, {"source": event.get("source")})


def _session_end(event: dict[str, Any], hooks: HookSink) -> None:
    _emit(
        event,
        hooks,
        {"agentguard.span.kind": "SESSION", "codex.session.end_reason": event.get("reason")},
        start_time_ns=hooks.take_start(_session_key(event)),
    )


def _user_prompt_submit(event: dict[str, Any], hooks: HookSink) -> None:
    prompt = event.get("prompt")
    _emit(
        event,
        hooks,
        {
            # The shape the app reads a prompt from: JSON, with a user role.
            "input.value": (
                None
                if prompt is None
                else json.dumps(
                    {"messages": [{"role": "user", "content": _as_string(prompt)}]},
                    ensure_ascii=False,
                )
            ),
        },
    )


def _tool_key(event: dict[str, Any]) -> str:
    return _pair_key("tool", event, event.get("tool_use_id"))


def _tool_attributes(event: dict[str, Any]) -> dict[str, Any]:
    return {
        "tool.name": event.get("tool_name"),
        "tool.parameters": _as_string(event.get("tool_input")),
        "tool_use_id": event.get("tool_use_id"),
    }


def _pre_tool_use(event: dict[str, Any], hooks: HookSink) -> None:
    hooks.mark_start(_tool_key(event))
    # No span: the `tool` span of PostToolUse covers the call from here on.
    _emit(event, hooks, _tool_attributes(event), span=False)


def _post_tool_use(event: dict[str, Any], hooks: HookSink) -> None:
    _emit(
        event,
        hooks,
        {
            "openinference.span.kind": "TOOL",
            **_tool_attributes(event),
            "output.value": _as_string(event.get("tool_response")),
        },
        task_id=event.get("tool_use_id"),
        start_time_ns=hooks.take_start(_tool_key(event)),
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
    # The one LLM-kind span: the app turns it into llm_request / llm_response
    # rows and the llm.* metrics, one llm.call per attempt.
    extra["openinference.span.kind"] = "LLM"
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
