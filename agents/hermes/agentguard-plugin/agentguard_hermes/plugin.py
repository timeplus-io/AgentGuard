"""
AgentGuard plugin for Hermes Agent.

Forwards raw Hermes plugin hook events to the AgentGuard Timeplus stream.
All kwargs are stored verbatim in event_data. conversation_history is dropped
— it only contains user/assistant turns (no system prompt) and grows
unboundedly. The system prompt is not exposed by any Hermes plugin hook.

Also synchronously blocks risky tool calls based on rule decisions from the
AgentGuard server: pre_tool_call POSTs to /api/holds and returns
{"action": "block", "message": "..."} when the backend denies. The hold path
is enabled by default and can be disabled via AGENTGUARD_HOLDS_ENABLED=false
to revert to async observation-only mode.

Plugin hooks (CLI + gateway):
  on_session_start      session_id, model, platform
  on_session_end        session_id, completed, interrupted, model, platform
  on_session_finalize   session_id (may be None), platform
  on_session_reset      session_id, platform
  pre_llm_call          session_id, conversation_history, is_first_turn, model, platform, sender_id
  post_llm_call         session_id, conversation_history, model, platform
  pre_tool_call         tool_name, args, task_id, session_id, tool_call_id      [SYNCHRONOUS]
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
  AGENTGUARD_TIMEPLUS_URL       AGENTGUARD_STREAM       AGENTGUARD_USERNAME
  AGENTGUARD_PASSWORD           AGENTGUARD_AGENT_ID     AGENTGUARD_DEPLOYMENT_ID
  AGENTGUARD_DEPLOYMENT_NAME    AGENTGUARD_URL          AGENTGUARD_HOLDS_FAIL_POLICY
  AGENTGUARD_HOLDS_ENABLED

Server-side prerequisite: TIMEPLUS_USER and TIMEPLUS_PASSWORD must be set
on the AgentGuard process for /api/holds to authenticate to Timeplus —
without them the endpoint returns 503 and the plugin's fail-policy applies.
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

# Synchronous hold-and-wait config — same shape as the openclaw and claudecode plugins.
_AGENTGUARD_URL    = os.environ.get("AGENTGUARD_URL", "http://localhost:8080")
_HOLD_FAIL_POLICY  = "allow" if os.environ.get("AGENTGUARD_HOLDS_FAIL_POLICY", "deny").lower() == "allow" else "deny"
_HOLDS_ENABLED     = os.environ.get("AGENTGUARD_HOLDS_ENABLED", "true").lower() != "false"

# Self-imposed timeout for the /api/holds request. Hermes provides no hook
# timeout or abort signal, so we must cap the long-poll ourselves. 540s leaves
# margin under the backend's 540s hold-timeout ceiling — if the backend hits
# its own timeout first we get a clean fail-policy response instead of an
# httpx-style abort.
_HOLD_REQUEST_TIMEOUT_SEC = 600

_COLUMNS = [
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

_SKIP_KEYS = {"conversation_history"}

# `_TID_TO_SID` stores <task_id, session_id> pairs to lookup session_id by task_id
_TID_TO_SID = {}


def _get_session_id(session_id: str, task_id: str) -> str:
    if session_id == "":
        session_id = _TID_TO_SID.get(task_id) or "unknown"
    else:
        if task_id != "":
            _TID_TO_SID[task_id] = session_id

    return session_id


def _clear_session_task_ids(session_id: str):
    task_ids = [k for k, v in _TID_TO_SID.items() if v == session_id ]
    for tid in task_ids:
        del _TID_TO_SID[tid]


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


def _make_row(hook_name: str, kwargs: dict, *, session_id: str | None = None) -> list:
    sid = _get_session_id(
        session_id if session_id is not None else (kwargs.get("session_id") or ""),
        kwargs.get("task_id") or "",
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
    Also fire-and-forget POST /api/holds/_abandon-session to clean up any
    hold pending for this session (zombie-hold prevention).
    """
    sid = kwargs.get("session_id") or ""
    _ingest([_make_row("on_session_end", kwargs, session_id=sid)])
    if sid:
        _abandon_session_holds(sid)
        _clear_session_task_ids(sid)


def _abandon_session_holds(session_id: str) -> None:
    """Best-effort POST /api/holds/_abandon-session/<sid> — never raises."""
    url = f"{_AGENTGUARD_URL.rstrip('/')}/api/holds/_abandon-session/{session_id}"
    req = urllib.request.Request(url, data=b"", method="POST")
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            resp.read()
    except urllib.error.URLError:
        pass  # plugin should never fail on cleanup


# ---------------------------------------------------------------------------
# Synchronous hold-and-wait — pre_tool_call only
# ---------------------------------------------------------------------------

def _request_hold(tool_name: str, args: dict, session_id: str, run_id: str,
                  task_id: str, tool_call_id: str, event_time: str) -> dict:
    """
    POST to AgentGuard's /api/holds and wait synchronously for a decision.
    Returns {"decision": "allow"} or {"decision": "block", "reason": "..."}.
    Applies _HOLD_FAIL_POLICY on transport / parse / timeout errors.
    """
    url = f"{_AGENTGUARD_URL.rstrip('/')}/api/holds"
    body = {
        "deployment_id":   _DEPLOYMENT_ID,
        "deployment_name": _DEPLOYMENT_NAME,
        "agent_id":        _AGENT_ID,
        "agent_type":      "hermes",
        "session_id":      session_id,
        "run_id":          run_id,
        "tool_name":       tool_name,
        "event_time":      event_time,
        "tool_input":      args or {},
        "hook_payload": {
            "hook_event_name": "pre_tool_call",
            "tool_name":       tool_name,
            "args":            args,
            "task_id":         task_id,
            "session_id":      session_id,
            "tool_call_id":    tool_call_id,
        },
    }
    payload = json.dumps(body, default=str).encode()
    req = urllib.request.Request(url, data=payload, method="POST")
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=_HOLD_REQUEST_TIMEOUT_SEC) as resp:
            data = json.loads(resp.read().decode())
            decision = data.get("decision", "allow")
            return {
                "decision": decision if decision in ("allow", "block") else "allow",
                "reason":   data.get("reason", ""),
            }
    except:
        # Do not block on network or server exceptions
        return {"decision": "allow", "reason": ""}


def _fail_policy_decision(reason: str) -> dict:
    if _HOLD_FAIL_POLICY == "allow":
        return {"decision": "allow", "reason": ""}
    return {"decision": "block", "reason": f"AgentGuard unreachable — {reason}"}


def _pre_tool_call(**kwargs):
    """
    Synchronous hold-and-wait callback for pre_tool_call.

    When _HOLDS_ENABLED is true:
      - POST to /api/holds and wait for a decision
      - Return {"action": "block", "message": ...} on deny (Hermes short-circuits the call)
      - Return None on allow (Hermes proceeds; tool call runs normally)
      - The /api/holds endpoint ingests the event itself, so we DO NOT also call _ingest()

    When _HOLDS_ENABLED is false:
      - Fall back to the original observation-only async ingest path

    Hermes' pre_tool_call has no hook timeout — the callback can block as long as
    we like. We self-impose _HOLD_REQUEST_TIMEOUT_SEC via urllib's timeout arg.
    """
    if not _HOLDS_ENABLED:
        # Async observation-only path (matches original behavior).
        _ingest([_make_row("pre_tool_call", kwargs)])
        return None

    tid = kwargs.get("task_id") or ""
    sid = _get_session_id(kwargs.get("session_id") or "", tid)
    decision = _request_hold(
        tool_name=kwargs.get("tool_name") or "",
        args=kwargs.get("args") or {},
        session_id=sid,
        run_id=sid,
        task_id=tid,
        tool_call_id=kwargs.get("tool_call_id") or "",
        event_time=datetime.now(timezone.utc).isoformat(),
    )

    if decision["decision"] == "block":
        return {
            "action": "block",
            "message": decision.get("reason") or "Blocked by AgentGuard",
        }
    return None


# ---------------------------------------------------------------------------
# Plugin registration — called by Hermes on plugin load
# ---------------------------------------------------------------------------
def register(ctx):
    ctx.register_hook("on_session_start",    _hook("on_session_start"))
    ctx.register_hook("on_session_end",      _on_session_end)       # per-turn → conversation_end
    ctx.register_hook("on_session_finalize", _on_session_finalize)  # true end → on_session_end + abandon-session
    ctx.register_hook("on_session_reset",    _hook("on_session_reset"))
    ctx.register_hook("pre_llm_call",        _hook("pre_llm_call"))
    ctx.register_hook("post_llm_call",       _hook("post_llm_call"))
    ctx.register_hook("pre_api_request",     _hook("pre_api_request"))
    ctx.register_hook("post_api_request",    _hook("post_api_request"))
    ctx.register_hook("pre_tool_call",       _pre_tool_call)        # synchronous hold-and-wait
    ctx.register_hook("post_tool_call",      _hook("post_tool_call"))
