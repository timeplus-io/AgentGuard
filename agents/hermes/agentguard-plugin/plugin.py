"""
AgentGuard plugin for Hermes Agent.

Registers plugin hooks that fire in BOTH CLI and gateway modes, providing full
session coverage regardless of how Hermes is started. Complements the gateway
hook (handler.py) which only fires in gateway mode.

Plugin hooks used and their AgentGuard mapping:
  on_session_start  → session_start    (session_id, model, platform)
  on_session_end    → session_end      (session_id, completed, interrupted, model, platform)
  pre_llm_call      → user_prompt_submit (session_id, user_message, model, platform)
  post_llm_call     → llm_output       (session_id, assistant_response, model, platform)
  pre_tool_call     → before_tool_call  (tool_name, args, task_id)
  post_tool_call    → after_tool_call   (tool_name, args, result, task_id)

Install by dropping this file into ~/.hermes/plugins/agentguard/plugin.py
and registering it in ~/.hermes/config.yaml:
  plugins:
    - path: ~/.hermes/plugins/agentguard/plugin.py

Config via environment variables (same as handler.py):
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


def _row(hook_name: str, session_id: str, tool_name: str = "", model: str = "",
         platform: str = "", event_data: dict = None) -> list:
    return [
        hook_name,
        datetime.now(timezone.utc).isoformat(),
        session_id,
        session_id,   # run_id mirrors session_id
        _AGENT_ID,
        _DEPLOYMENT_ID,
        _DEPLOYMENT_NAME,
        "",           # session_key — not available in plugin hooks
        tool_name,
        "",           # provider — not exposed
        model,
        "observe",
        "",
        json.dumps(event_data or {}),
        "hermes",
    ]


# ---------------------------------------------------------------------------
# Plugin hook callbacks
# ---------------------------------------------------------------------------

def _on_session_start(session_id: str, model: str = "", platform: str = "", **kwargs):
    _ingest([_row("session_start", session_id, model=model, platform=platform,
                  event_data={"session_id": session_id, "model": model, "platform": platform})])


def _on_session_end(session_id: str, completed: bool = False, interrupted: bool = False,
                    model: str = "", platform: str = "", **kwargs):
    _ingest([_row("session_end", session_id, model=model, platform=platform,
                  event_data={"session_id": session_id, "completed": completed,
                              "interrupted": interrupted, "platform": platform})])


def _pre_llm_call(session_id: str, user_message: str = "", model: str = "",
                  platform: str = "", is_first_turn: bool = False, **kwargs):
    _ingest([_row("user_prompt_submit", session_id, model=model, platform=platform,
                  event_data={"session_id": session_id, "platform": platform,
                              "model": model, "is_first_turn": is_first_turn,
                              "message": (user_message or "")[:500]})])


def _post_llm_call(session_id: str, user_message: str = "", assistant_response: str = "",
                   model: str = "", platform: str = "", **kwargs):
    _ingest([_row("llm_output", session_id, model=model, platform=platform,
                  event_data={"session_id": session_id, "platform": platform,
                              "model": model,
                              "message": (user_message or "")[:500],
                              "response": (assistant_response or "")[:500]})])


def _pre_tool_call(tool_name: str, args: dict = None, task_id: str = "", **kwargs):
    _ingest([_row("before_tool_call", task_id, tool_name=tool_name,
                  event_data={"tool_name": tool_name, "args": args or {},
                              "task_id": task_id})])


def _post_tool_call(tool_name: str, args: dict = None, result: str = "",
                    task_id: str = "", **kwargs):
    _ingest([_row("after_tool_call", task_id, tool_name=tool_name,
                  event_data={"tool_name": tool_name, "args": args or {},
                              "result": (result or "")[:500], "task_id": task_id})])


# ---------------------------------------------------------------------------
# Plugin registration entry point — called by Hermes on plugin load
# ---------------------------------------------------------------------------
def register(ctx):
    ctx.register_hook("on_session_start", _on_session_start)
    ctx.register_hook("on_session_end",   _on_session_end)
    ctx.register_hook("pre_llm_call",     _pre_llm_call)
    ctx.register_hook("post_llm_call",    _post_llm_call)
    ctx.register_hook("pre_tool_call",    _pre_tool_call)
    ctx.register_hook("post_tool_call",   _post_tool_call)
