#!/usr/bin/env python3
"""Generate / ingest / score the audit-rule test-data corpus (issue #280).

Loads a labeled corpus of simulated multi-turn agent sessions into
agentguard_cim_event and scores the installed dsa-* detectors against the
description-derived ground truth. Stdlib only.
"""
import argparse
import datetime as dt
import glob
import json
import os

CIM_COLUMNS = [
    "event_time", "event_type", "agent_type", "agent_id", "deployment_id",
    "session_id", "run_id", "provider", "model", "tool_name", "tool_input",
    "tool_result", "tool_success", "tool_duration_ms", "user_message",
    "assistant_message", "hook_decision", "block_reason", "raw_hook_name",
    "raw_event_data",
]

EVENT_TYPES = {
    "session_start", "session_end", "user_input", "llm_request",
    "llm_response", "tool_invoke", "tool_complete",
}

# event_type -> raw hook name, per agent_type (see docs/cim.md §2.4).
HOOK_NAMES = {
    "claudecode": {
        "session_start": "session_start", "session_end": "session_end",
        "user_input": "user_prompt_submit", "llm_response": "llm_output",
        "tool_invoke": "before_tool_call", "tool_complete": "after_tool_call",
        "llm_request": "user_prompt_submit",
    },
    "openclaw": {
        "session_start": "session_start", "session_end": "session_end",
        "user_input": "message_received", "llm_request": "llm_input",
        "llm_response": "llm_output", "tool_invoke": "before_tool_call",
        "tool_complete": "after_tool_call",
    },
    "hermes": {
        "session_start": "on_session_start", "session_end": "on_session_end",
        "user_input": "pre_llm_call", "llm_request": "pre_api_request",
        "llm_response": "post_llm_call", "tool_invoke": "pre_tool_call",
        "tool_complete": "post_tool_call",
    },
}

CODES_WITHOUT_RULE = {"A02", "A13", "A15"}
DEFAULT_DEPLOYMENT_ID = "audit-demo"
STEP_MS = 1500       # spacing between events within a case
CASE_GAP_MS = 60000  # spacing between cases

_REQUIRED_KEYS = ("case_id", "audit_code", "label", "events")


def validate_case(case, expected_code):
    for k in _REQUIRED_KEYS:
        if k not in case:
            raise ValueError(f"case missing key '{k}': {case.get('case_id', '?')}")
    if case["audit_code"] != expected_code:
        raise ValueError(
            f"{case['case_id']}: audit_code {case['audit_code']} != file code {expected_code}")
    if case["label"] not in ("positive", "negative"):
        raise ValueError(f"{case['case_id']}: bad label {case['label']!r}")
    if not case["events"]:
        raise ValueError(f"{case['case_id']}: no events")
    agent_type = case.get("session", {}).get("agent_type") or case.get("agent_type")
    if agent_type and agent_type not in HOOK_NAMES:
        raise ValueError(f"{case['case_id']}: unknown agent_type {agent_type!r}")
    for ev in case["events"]:
        et = ev.get("event_type")
        if et not in EVENT_TYPES:
            raise ValueError(f"{case['case_id']}: bad event_type {et!r}")
        if "session_id" in ev or "session_id" in case:
            raise ValueError(f"{case['case_id']}: session_id must not be authored")


def load_scenarios(scenarios_dir, codes=None):
    wanted = set(c.upper() for c in codes) if codes else None
    cases = []
    for path in sorted(glob.glob(os.path.join(scenarios_dir, "A*.json"))):
        code = os.path.splitext(os.path.basename(path))[0].upper()
        if wanted and code not in wanted:
            continue
        with open(path, encoding="utf-8") as f:
            file_cases = json.load(f)
        for case in file_cases:
            validate_case(case, code)
            cases.append(case)
    cases.sort(key=lambda c: c["case_id"])
    return cases


def case_session_id(case, suffix=""):
    return "audit-" + case["case_id"] + suffix


def _fmt_time(t):
    return t.strftime("%Y-%m-%d %H:%M:%S.") + f"{t.microsecond // 1000:03d}"


def _raw_event_data(event_type, ev):
    """Synthesize a minimal, non-empty raw hook payload JSON string."""
    payload = {}
    if event_type in ("user_input", "llm_request"):
        payload = {"prompt": ev.get("user_message", "")}
    elif event_type == "llm_response":
        payload = {"last_assistant_message": ev.get("assistant_message", "")}
    elif event_type == "tool_invoke":
        payload = {"tool_name": ev.get("tool_name", ""), "tool_input": ev.get("tool_input", "")}
    elif event_type == "tool_complete":
        payload = {"tool_name": ev.get("tool_name", ""),
                   "tool_response": ev.get("tool_result", "")}
    else:
        payload = {"event": event_type}
    return json.dumps(payload, ensure_ascii=False)


def case_to_rows(case, now, deployment_id, suffix=""):
    session = dict(case.get("session", {}))
    agent_type = session.get("agent_type") or case.get("agent_type") or "openclaw"
    hooks = HOOK_NAMES[agent_type]
    sid = case_session_id(case, suffix)
    events = case["events"]
    n = len(events)
    rows = []
    run_index = 0
    for i, ev in enumerate(events):
        et = ev["event_type"]
        if et == "user_input":
            run_index += 1
        if agent_type in ("claudecode", "hermes"):
            run_id = sid
        elif et in ("session_start", "session_end"):
            run_id = sid
        else:
            run_id = f"{sid}::run{max(run_index, 1)}"
        t = now - dt.timedelta(milliseconds=(n - 1 - i) * STEP_MS)

        def pick(field, default=""):
            if field in ev:
                return ev[field]
            if field in session:
                return session[field]
            return default

        row = {
            "event_time": _fmt_time(t),
            "event_type": et,
            "agent_type": agent_type,
            "agent_id": pick("agent_id", "audit-agent"),
            "deployment_id": deployment_id,
            "session_id": sid,
            "run_id": run_id,
            "provider": pick("provider", ""),
            "model": pick("model", ""),
            "tool_name": ev.get("tool_name", ""),
            "tool_input": ev.get("tool_input", ""),
            "tool_result": ev.get("tool_result", ""),
            "tool_success": bool(ev.get("tool_success", True)),
            "tool_duration_ms": int(ev.get("tool_duration_ms", 0)),
            "user_message": ev.get("user_message", ""),
            "assistant_message": ev.get("assistant_message", ""),
            "hook_decision": ev.get("hook_decision", "observe"),
            "block_reason": ev.get("block_reason", ""),
            "raw_hook_name": hooks.get(et, et),
            "raw_event_data": _raw_event_data(et, ev),
        }
        rows.append(row)
    return rows
