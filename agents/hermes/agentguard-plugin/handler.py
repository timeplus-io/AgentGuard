"""
AgentGuard gateway hook for Hermes Agent.

Forwards Hermes gateway lifecycle events to the agentguard_hook_events Timeplus
stream for real-time security monitoring.

IMPORTANT: Gateway hooks only fire when Hermes runs in gateway mode
('hermes gateway run' / Docker). They do NOT fire in CLI mode ('hermes').
For CLI + gateway coverage, use the companion plugin (plugin.py).

Installed into ~/.hermes/hooks/agentguard/ via 'make configure'.

Events captured (from ~/.hermes/hooks/agentguard/HOOK.yaml):
  gateway:startup  — gateway process started
  session:start    — new messaging session created
  session:end      — session ended
  session:reset    — user ran /new or /reset
  agent:start      — agent begins processing a message
  agent:step       — each tool-calling loop iteration
  agent:end        — agent finished processing
  command:*        — any slash command executed

Context keys per event (from official docs):
  gateway:startup  → { platforms }
  session:start    → { platform, user_id, session_id, session_key }
  session:end      → { platform, user_id, session_key }
  session:reset    → { platform, user_id, session_key }
  agent:start      → { platform, user_id, session_id, message }
  agent:step       → { platform, user_id, session_id, iteration, tool_names }
  agent:end        → { platform, user_id, session_id, message, response }
  command:*        → { platform, user_id, command, args }

Config via environment variables:
  AGENTGUARD_TIMEPLUS_URL    — Timeplus HTTP endpoint (default: http://localhost:3218)
  AGENTGUARD_STREAM          — target stream name (default: agentguard_hook_events)
  AGENTGUARD_DATABASE        — Timeplus database (default: ag)
  AGENTGUARD_USERNAME        — Timeplus username (default: proton)
  AGENTGUARD_PASSWORD        — Timeplus password (default: empty)
  AGENTGUARD_AGENT_ID        — agent identifier (default: hostname)
  AGENTGUARD_DEPLOYMENT_ID   — deployment tag (default: local)
  AGENTGUARD_DEPLOYMENT_NAME — human-readable deployment name (default: Local Dev)
"""

import json
import os
import socket
import urllib.error
import urllib.request
from base64 import b64encode
from datetime import datetime, timezone

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
_TIMEPLUS_URL    = os.environ.get("AGENTGUARD_TIMEPLUS_URL", "http://localhost:3218")
_STREAM          = os.environ.get("AGENTGUARD_STREAM", "agentguard_hook_events")
_DATABASE        = os.environ.get("AGENTGUARD_DATABASE", "ag")
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

# Hermes gateway event → AgentGuard hook_name
_EVENT_MAP = {
    "gateway:startup": "session_start",      # gateway process start; no session yet
    "session:start":   "session_start",
    "session:end":     "session_end",
    "session:reset":   "session_end",
    "agent:start":     "user_prompt_submit",
    "agent:step":      "before_tool_call",
    "agent:end":       "llm_output",
    # command:* is handled via wildcard match — mapped to "permission_denied" equivalent
}

# ---------------------------------------------------------------------------
# Ingest
# ---------------------------------------------------------------------------
def _ingest(rows: list) -> None:
    url = f"{_TIMEPLUS_URL.rstrip('/')}/timeplusd/v2/ingest/{_DATABASE}/streams/{_STREAM}"
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
        print(f"[agentguard] ingest error: url={url} error={e}", flush=True)


def _row(hook_name: str, ctx: dict, tool_name: str = "", model: str = "") -> list:
    return [
        hook_name,
        datetime.now(timezone.utc).isoformat(),
        ctx.get("session_id", ""),
        ctx.get("session_id", ""),   # run_id mirrors session_id
        _AGENT_ID,
        _DEPLOYMENT_ID,
        _DEPLOYMENT_NAME,
        ctx.get("session_key", ""),
        tool_name,
        "",       # provider — not exposed by gateway hooks
        model,
        "observe",
        "",
        json.dumps(ctx),
        "hermes",
    ]


# ---------------------------------------------------------------------------
# Hook entry point — called by Hermes for each registered event
# ---------------------------------------------------------------------------
def handle(event_type: str, context: dict) -> None:
    # command:* wildcard — map all slash commands to a dedicated hook_name
    if event_type.startswith("command:"):
        cmd = context.get("command", event_type.replace("command:", ""))
        rows = [_row("slash_command", context, tool_name=cmd)]
        _ingest(rows)
        return

    hook_name = _EVENT_MAP.get(event_type)
    if hook_name is None:
        return

    # agent:step carries a list of tool names — emit one row per tool so
    # AgentGuard rule MVs can match on individual tool_name values.
    if event_type == "agent:step":
        tool_names = context.get("tool_names") or []
        rows = [_row(hook_name, context, t) for t in tool_names] if tool_names else [_row(hook_name, context)]
    else:
        rows = [_row(hook_name, context)]

    _ingest(rows)
