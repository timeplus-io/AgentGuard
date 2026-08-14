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
