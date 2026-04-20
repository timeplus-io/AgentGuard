"""
AgentGuard plugin for Hermes Agent.

Forwards raw Hermes plugin hook events to the AgentGuard Timeplus stream.
All kwargs are stored verbatim in event_data. conversation_history is dropped
— it only contains user/assistant turns (no system prompt) and grows
unboundedly. The system prompt is not exposed by any Hermes plugin hook.

Plugin hooks (CLI + gateway):
  on_session_start      session_id, model, platform
  on_session_end        session_id, completed, interrupted, model, platform
  on_session_finalize   session_id (may be None), platform
  on_session_reset      session_id, platform
  pre_llm_call          session_id, conversation_history, is_first_turn, model, platform, sender_id
  post_llm_call         session_id, conversation_history, model, platform
  pre_tool_call         tool_name, args, task_id, session_id, tool_call_id
  post_tool_call        tool_name, args, result, task_id, session_id, tool_call_id
  pre_api_request       session_id, model, provider, approx_input_tokens, message_count, ...
  post_api_request      session_id, model, provider, usage{tokens}, api_duration, ...

Hook name mapping (Hermes → AgentGuard):
  on_session_end        → "conversation_end"  (fires per chat turn, not true session end)
  on_session_finalize   → "on_session_end"    (true session teardown)
  all others            → unchanged

Run ID: session_id (all events in a session share the same run_id).

Install: drop plugin.yaml + __init__.py into ~/.hermes/plugins/agentguard/

Config via environment variables:
  AGENTGUARD_TIMEPLUS_URL    AGENTGUARD_STREAM   AGENTGUARD_USERNAME
  AGENTGUARD_PASSWORD        AGENTGUARD_AGENT_ID AGENTGUARD_DEPLOYMENT_ID
  AGENTGUARD_DEPLOYMENT_NAME
"""

import json
import os
import socket
import urllib.error
import urllib.request
from base64 import b64encode
from datetime import datetime, timezone

_TIMEPLUS_URL    = os.environ.get("AGENTGUARD_TIMEPLUS_URL", "http://localhost:3218")
_STREAM          = os.environ.get("AGENTGUARD_STREAM", "agentguard_hook_events")
_USERNAME        = os.environ.get("AGENTGUARD_USERNAME", "proton")
_PASSWORD        = os.environ.get("AGENTGUARD_PASSWORD", "")
_AGENT_ID        = os.environ.get("AGENTGUARD_AGENT_ID") or socket.gethostname()
_DEPLOYMENT_ID   = os.environ.get("AGENTGUARD_DEPLOYMENT_ID", "local")
_DEPLOYMENT_NAME = os.environ.get("AGENTGUARD_DEPLOYMENT_NAME", "Local Dev")

_COLUMNS = [
    "hook_name", "event_time", "session_id", "run_id",
    "agent_id", "deployment_id", "deployment_name", "session_key",
    "tool_name", "provider", "model", "hook_decision",
    "block_reason", "event_data", "agent_type",
]

_SKIP_KEYS = {"conversation_history"}


def _ingest(rows: list) -> None:
    url = f"{_TIMEPLUS_URL.rstrip('/')}/proton/v1/ingest/streams/{_STREAM}"
    payload = json.dumps({"columns": _COLUMNS, "data": rows}).encode()
    req = urllib.request.Request(url, data=payload, method="POST")
    req.add_header("Content-Type", "application/json")
    if _USERNAME:
        token = b64encode(f"{_USERNAME}:{_PASSWORD}".encode()).decode()
        req.add_header("Authorization", f"Basic {token}")
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            resp.read()
    except urllib.error.URLError as e:
        print(f"[agentguard] ingest error: {e}", flush=True)


def _make_row(hook_name: str, kwargs: dict, *, session_id: str = None) -> list:
    sid = session_id if session_id is not None else (
        kwargs.get("session_id") or kwargs.get("task_id") or ""
    )
    data = {k: v for k, v in kwargs.items() if k not in _SKIP_KEYS}
    return [
        hook_name,
        datetime.now(timezone.utc).isoformat(),
        sid,
        sid,          # run_id = session_id
        _AGENT_ID,
        _DEPLOYMENT_ID,
        _DEPLOYMENT_NAME,
        "",           # session_key — not available in plugin hooks
        kwargs.get("tool_name") or "",
        kwargs.get("provider") or "",
        kwargs.get("model") or "",
        "observe",
        "",
        json.dumps(data, default=str),
        "hermes",
    ]


def _hook(hook_name: str):
    def _cb(**kwargs):
        _ingest([_make_row(hook_name, kwargs)])
    _cb.__name__ = f"_{hook_name}"
    return _cb


# ---------------------------------------------------------------------------
# Lifecycle hooks with non-trivial semantics
# ---------------------------------------------------------------------------

def _on_session_end(**kwargs):
    """
    Hermes fires this after every run_conversation() call (per chat turn).
    Reported as 'conversation_end' — NOT treated as session termination by AgentGuard.
    """
    _ingest([_make_row("conversation_end", kwargs)])


def _on_session_finalize(**kwargs):
    """
    True session teardown (CLI exit, /new, /reset, gateway GC).
    Reported as 'on_session_end' so AgentGuard marks the session as closed.
    """
    sid = kwargs.get("session_id") or ""
    _ingest([_make_row("on_session_end", kwargs, session_id=sid)])


# ---------------------------------------------------------------------------
# Plugin registration — called by Hermes on plugin load
# ---------------------------------------------------------------------------
def register(ctx):
    ctx.register_hook("on_session_start",    _hook("on_session_start"))
    ctx.register_hook("on_session_end",      _on_session_end)       # per-turn → conversation_end
    ctx.register_hook("on_session_finalize", _on_session_finalize)  # true end → on_session_end
    ctx.register_hook("on_session_reset",    _hook("on_session_reset"))
    ctx.register_hook("pre_llm_call",        _hook("pre_llm_call"))
    ctx.register_hook("post_llm_call",       _hook("post_llm_call"))
    ctx.register_hook("pre_api_request",     _hook("pre_api_request"))
    ctx.register_hook("post_api_request",    _hook("post_api_request"))
    ctx.register_hook("pre_tool_call",       _hook("pre_tool_call"))
    ctx.register_hook("post_tool_call",      _hook("post_tool_call"))
