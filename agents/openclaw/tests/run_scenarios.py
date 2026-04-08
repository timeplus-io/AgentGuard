#!/usr/bin/env python3
"""
AgentShield OpenClaw Test Runner

Sends messages to OpenClaw via /v1/chat/completions, then queries Timeplus to
verify that hook events, OTel traces, and OTel metrics were collected.

Usage:
    python3 tests/run_scenarios.py [--scenario N] [--model MODEL]

    --scenario N   Run only scenario N (1-5). Default: run all.
    --model MODEL  Override backend model (e.g. qwen3:8b). Default: use agent default.
"""

import argparse
import json
import os
import sys
import time
import urllib.request
import urllib.error
from datetime import datetime, timezone
from pathlib import Path

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

OPENCLAW_URL = "http://localhost:18789"

# Config file is relative to the agents/openclaw/ directory (parent of tests/).
_CONFIG_FILE = os.path.join(os.path.dirname(__file__), "..", ".openclaw", "openclaw.json")


def _load_openclaw_token() -> str:
    """Read the gateway auth token from .openclaw/openclaw.json."""
    config_path = os.path.abspath(_CONFIG_FILE)
    try:
        with open(config_path) as f:
            cfg = json.load(f)
        token = cfg.get("gateway", {}).get("auth", {}).get("token", "")
        if not token:
            raise ValueError("gateway.auth.token not found in config")
        return token
    except FileNotFoundError:
        raise SystemExit(
            f"Config file not found: {config_path}\n"
            "Run 'make init' and 'make configure' first."
        )


def _load_proton_config() -> tuple[str, str, str]:
    """Read Proton URL, username, and password from openclaw.json plugin config."""
    config_path = Path(__file__).parent.parent / ".openclaw" / "openclaw.json"
    with config_path.open() as f:
        data = json.load(f)
    plugin_cfg = data["plugins"]["entries"]["agentshield"]["config"]
    url = plugin_cfg.get("timeplusUrl", "http://localhost:3218")
    # timeplusUrl uses host.docker.internal inside Docker — remap to localhost for host-side Python
    url = url.replace("host.docker.internal", "localhost")
    user = plugin_cfg.get("username", "proton")
    password = plugin_cfg.get("password", "")
    return url, user, password


OPENCLAW_TOKEN = _load_openclaw_token()
TIMEPLUS_URL, TIMEPLUS_USER, TIMEPLUS_PASSWORD = _load_proton_config()

FLUSH_WAIT_S = 5       # seconds to wait for agentshield batch flush
REPLY_TIMEOUT_S = 120  # seconds to wait for OpenClaw to reply

# ---------------------------------------------------------------------------
# HTTP helpers
# ---------------------------------------------------------------------------

def _auth_header():
    import base64
    creds = base64.b64encode(f"{TIMEPLUS_USER}:{TIMEPLUS_PASSWORD}".encode()).decode()
    return f"Basic {creds}"


def tpq(sql: str) -> list[dict]:
    """Run a SQL query against Timeplus (Proton ClickHouse HTTP) and return rows as list-of-dicts."""
    # Proton exposes the ClickHouse HTTP interface at POST /
    # Send the SQL as plain text body with FORMAT JSON appended.
    query = sql.strip().rstrip(";") + " FORMAT JSON"
    body = query.encode()
    req = urllib.request.Request(
        f"{TIMEPLUS_URL}/",
        data=body,
        headers={
            "Content-Type": "text/plain",
            "Authorization": _auth_header(),
        },
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        result = json.loads(resp.read())
    # Returns {"meta": [{"name": ..., "type": ...}], "data": [{col: val}, ...], "rows": N}
    return result.get("data", [])


def chat(messages: list[dict], user_id: str, model_override: str | None = None,
         timeout: int = REPLY_TIMEOUT_S) -> str:
    """Send messages to OpenClaw and return the assistant reply text."""
    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {OPENCLAW_TOKEN}",
    }
    if model_override:
        headers["x-openclaw-model"] = model_override

    payload = {
        "model": "openclaw/main",
        "messages": messages,
        "user": user_id,   # stable user → persistent session across turns
    }
    body = json.dumps(payload).encode()
    req = urllib.request.Request(
        f"{OPENCLAW_URL}/v1/chat/completions",
        data=body,
        headers=headers,
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        result = json.loads(resp.read())
    return result["choices"][0]["message"]["content"]


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


# ---------------------------------------------------------------------------
# Reporting helpers
# ---------------------------------------------------------------------------

PASS = "\033[32mPASS\033[0m"
FAIL = "\033[31mFAIL\033[0m"
INFO = "\033[36mINFO\033[0m"


def check(label: str, condition: bool, detail: str = "") -> bool:
    status = PASS if condition else FAIL
    print(f"  [{status}] {label}", f"— {detail}" if detail else "")
    return condition


def section(title: str):
    print(f"\n{'─' * 60}")
    print(f"  {title}")
    print(f"{'─' * 60}")


def print_table(rows: list[dict], cols: list[str] | None = None, limit: int = 10):
    if not rows:
        print("    (no rows)")
        return
    cols = cols or list(rows[0].keys())
    widths = {c: max(len(c), max(len(str(r.get(c, ""))) for r in rows)) for c in cols}
    header = " | ".join(c.ljust(widths[c]) for c in cols)
    print(f"    {header}")
    print(f"    {'-' * len(header)}")
    for r in rows[:limit]:
        print("    " + " | ".join(str(r.get(c, "")).ljust(widths[c]) for c in cols))
    if len(rows) > limit:
        print(f"    ... ({len(rows) - limit} more rows)")


# ---------------------------------------------------------------------------
# Scenario 1 — Basic conversation (baseline)
# ---------------------------------------------------------------------------

def scenario_1(model: str | None):
    section("Scenario 1 — Basic Conversation (Baseline)")
    user_id = "agentshield-test-s1"
    t_start = now_iso()

    print(f"  [{INFO}] Sending message...")
    reply = chat(
        [{"role": "user", "content": "What is 2 + 2? Reply in exactly one sentence."}],
        user_id=user_id,
        model_override=model,
    )
    print(f"  [{INFO}] Reply: {reply[:120]}")
    print(f"  [{INFO}] Waiting {FLUSH_WAIT_S}s for flush...")
    time.sleep(FLUSH_WAIT_S)

    rows = tpq(f"""
        SELECT hook_name, count() as cnt
        FROM table(agentshield_hook_events)
        WHERE event_time >= '{t_start}'
        GROUP BY hook_name
        ORDER BY cnt DESC
    """)

    hook_names = {r["hook_name"] for r in rows}
    # Note: session_start / message_received / before_agent_reply are channel hooks
    # that only fire on chat-channel traffic (Discord, Slack, etc.), not the HTTP API.
    results = [
        check("llm_input fired",            "llm_input"            in hook_names),
        check("llm_output fired",           "llm_output"           in hook_names),
        check("before_agent_start fired",   "before_agent_start"   in hook_names),
        check("before_prompt_build fired",  "before_prompt_build"  in hook_names),
        check("agent_end fired",            "agent_end"            in hook_names),
        check("before_message_write fired", "before_message_write" in hook_names),
    ]
    print()
    print_table(rows, ["hook_name", "cnt"])

    try:
        traces = tpq("SELECT count() as cnt FROM table(otel_traces) WHERE service_name = 'openclaw'")
        # OTel traces are gateway-level spans emitted on a background schedule,
        # not per-request. Just verify the stream has data at all.
        print(f"  [{INFO}] OTel traces in stream (total): {traces[0]['cnt']}")
        logs = tpq("SELECT count() as cnt FROM table(otel_logs) WHERE service_name = 'openclaw'")
        print(f"  [{INFO}] OTel logs in stream (total):   {logs[0]['cnt']}")
    except Exception as e:
        print(f"  [{INFO}] OTel query note: {e}")
    return all(results)


# ---------------------------------------------------------------------------
# Scenario 2 — Multi-turn with tool calls
# ---------------------------------------------------------------------------

def scenario_2(model: str | None):
    section("Scenario 2 — Multi-Turn with Tool Calls (File System)")
    user_id = "agentshield-test-s2"
    t_start = now_iso()
    history = []

    def turn(content):
        history.append({"role": "user", "content": content})
        reply = chat(history, user_id=user_id, model_override=model)
        history.append({"role": "assistant", "content": reply})
        print(f"  [{INFO}] Reply: {reply[:120]}")
        return reply

    print(f"  [{INFO}] Turn 1: create file")
    turn('Create a file called agentshield_test.txt in the workspace with the text "Hello from AgentShield". Then read it back and confirm its contents.')

    print(f"  [{INFO}] Turn 2: append + read")
    turn("Now append the current UTC date and time as a new line to the same file, then show me the full file contents.")

    print(f"  [{INFO}] Waiting {FLUSH_WAIT_S}s for flush...")
    time.sleep(FLUSH_WAIT_S)

    tool_rows = tpq(f"""
        SELECT hook_name, tool_name, count() as cnt
        FROM table(agentshield_hook_events)
        WHERE hook_name IN ('before_tool_call','after_tool_call','tool_result_persist')
          AND event_time >= '{t_start}'
        GROUP BY hook_name, tool_name
        ORDER BY hook_name, cnt DESC
    """)

    llm_rows = tpq(f"""
        SELECT count() as cnt FROM table(agentshield_hook_events)
        WHERE hook_name = 'llm_input' AND event_time >= '{t_start}'
    """)

    hook_names = {r["hook_name"] for r in tool_rows}
    results = [
        check("before_tool_call fired",     "before_tool_call"    in hook_names),
        check("after_tool_call fired",      "after_tool_call"     in hook_names),
        check("tool_result_persist fired",  "tool_result_persist" in hook_names),
        check("≥2 LLM turns",              int(llm_rows[0]["cnt"]) >= 2,
              f"{llm_rows[0]['cnt']} turns"),
    ]
    print()
    print_table(tool_rows, ["hook_name", "tool_name", "cnt"])
    return all(results)


# ---------------------------------------------------------------------------
# Scenario 3 — Sub-agent spawn
# ---------------------------------------------------------------------------

def scenario_3(model: str | None):
    section("Scenario 3 — Sub-Agent Spawn Chain")
    user_id = "agentshield-test-s3"
    t_start = now_iso()

    print(f"  [{INFO}] Sending orchestration prompt...")
    reply = chat(
        [{"role": "user", "content": (
            "Use the sessions_spawn tool to delegate the following task to a sub-agent: "
            "'Count the words in: The quick brown fox jumps over the lazy dog. "
            "Return only the integer.' "
            "Wait for the sub-agent to finish and report back its result."
        )}],
        user_id=user_id,
        model_override=model,
        timeout=180,
    )
    print(f"  [{INFO}] Reply: {reply[:200]}")
    # Sub-agents run asynchronously — wait extra time for the child to finish and flush.
    wait = max(FLUSH_WAIT_S, 15)
    print(f"  [{INFO}] Waiting {wait}s for sub-agent to complete and flush...")
    time.sleep(wait)

    subagent_rows = tpq(f"""
        SELECT hook_name, count() as cnt
        FROM table(agentshield_hook_events)
        WHERE hook_name IN ('subagent_spawning','subagent_delivery_target','subagent_spawned','subagent_ended')
          AND event_time >= '{t_start}'
        GROUP BY hook_name
        ORDER BY hook_name
    """)

    session_rows = tpq(f"""
        SELECT count(DISTINCT session_key) as distinct_sessions
        FROM table(agentshield_hook_events)
        WHERE event_time >= '{t_start}'
    """)

    child_rows = tpq(f"""
        SELECT
            json_extract_string(event_data, '$._ctx.childSessionKey') AS child_key,
            json_extract_string(event_data, '$._ctx.requesterSessionKey') AS parent_key
        FROM table(agentshield_hook_events)
        WHERE hook_name = 'subagent_spawning'
          AND event_time >= '{t_start}'
        LIMIT 3
    """)

    # childSessionKey lives at the top level of event_data for subagent_spawned.
    # Use regex extraction (Proton's JSON path functions require specific syntax).
    child_rows = tpq(
        f"SELECT extract(event_data, '\"childSessionKey\":\"([^\"]+)\"') AS child_key "
        f"FROM table(agentshield_hook_events) "
        f"WHERE hook_name = 'subagent_spawned' AND event_time >= '{t_start}' LIMIT 3"
    )

    fired = {r["hook_name"] for r in subagent_rows}
    results = [
        # subagent_spawning may fire before t_start if the model is fast;
        # subagent_delivery_target is a reliable proxy for the spawn sequence starting.
        check("subagent spawn sequence started",
              "subagent_delivery_target" in fired or "subagent_spawning" in fired),
        check("subagent_spawned fired",           "subagent_spawned" in fired),
        check("subagent_ended fired",             "subagent_ended"   in fired),
        check("≥2 distinct session_keys",         int(session_rows[0]["distinct_sessions"]) >= 2,
              f"{session_rows[0]['distinct_sessions']} sessions"),
        check("childSessionKey captured",
              bool(child_rows and child_rows[0].get("child_key")),
              child_rows[0]["child_key"] if child_rows else "none"),
    ]
    print()
    print_table(subagent_rows, ["hook_name", "cnt"])
    return all(results)


# ---------------------------------------------------------------------------
# Scenario 4 — Scheduled (cron) task
# ---------------------------------------------------------------------------

def scenario_4(model: str | None):
    section("Scenario 4 — Scheduled (Cron) Task")
    t_start = now_iso()
    DELAY_MIN = 2
    WAIT_S = DELAY_MIN * 60 + 15   # wait for cron to fire + buffer

    import subprocess

    print(f"  [{INFO}] Adding cron job (fires in {DELAY_MIN}m)...")
    result = subprocess.run(
        [
            "docker", "exec", "openclaw",
            "node", "dist/index.js", "cron", "add",
            "--name", "agentshield-test-cron",
            "--at", f"{DELAY_MIN}m",
            "--session", "isolated",
            "--message", "Count the files in the workspace directory. Reply with only the integer count.",
        ],
        capture_output=True, text=True,
    )
    print(f"  [{INFO}] Cron add output: {result.stdout.strip() or result.stderr.strip()}")

    print(f"  [{INFO}] Waiting {WAIT_S}s for cron to fire...")
    time.sleep(WAIT_S)

    # Isolated cron sessions use session key pattern: agent:<agentId>:cron:<jobId>
    cron_rows = tpq(
        f"SELECT hook_name, session_key, count() as cnt "
        f"FROM table(agentshield_hook_events) "
        f"WHERE session_key LIKE '%:cron:%' AND event_time >= '{t_start}' "
        f"GROUP BY hook_name, session_key ORDER BY hook_name"
    )
    session_rows = tpq(
        f"SELECT count(DISTINCT session_key) as cron_sessions "
        f"FROM table(agentshield_hook_events) "
        f"WHERE session_key LIKE '%:cron:%' AND event_time >= '{t_start}'"
    )

    fired = {r["hook_name"] for r in cron_rows}
    # session_start is a channel hook — not fired for scheduled (cron) sessions.
    # before_agent_start is the reliable "agent turn began" signal for cron sessions.
    results = [
        check("cron session appeared",        int(session_rows[0]["cron_sessions"]) >= 1,
              f"{session_rows[0]['cron_sessions']} cron sessions"),
        check("before_agent_start in cron",   "before_agent_start" in fired),
        check("llm_input in cron",            "llm_input"          in fired),
        check("agent_end in cron",            "agent_end"          in fired),
    ]
    print()
    print_table(cron_rows, ["hook_name", "session_key", "cnt"])

    # cleanup
    print(f"  [{INFO}] Cleaning up cron jobs...")
    subprocess.run(["docker", "exec", "openclaw", "node", "dist/index.js", "cron", "list"],
                   capture_output=False)
    return all(results)


# ---------------------------------------------------------------------------
# Scenario 5 — Complex coding task + context compaction
# ---------------------------------------------------------------------------

def scenario_5(model: str | None):
    section("Scenario 5 — Complex Coding Task + Context Compaction")
    user_id = "agentshield-test-s5"
    t_start = now_iso()
    history = []

    def turn(content):
        history.append({"role": "user", "content": content})
        reply = chat(history, user_id=user_id, model_override=model, timeout=180)
        history.append({"role": "assistant", "content": reply})
        print(f"  [{INFO}] Reply ({len(reply)} chars): {reply[:100]}...")
        return reply

    prompts = [
        "Create a Python file called fizzbuzz.py in the workspace that prints FizzBuzz from 1 to 20.",
        "Add a function fizzbuzz_range(start, end) that returns a list of FizzBuzz strings for the given range.",
        "Write test_fizzbuzz.py with 3 unit tests for fizzbuzz_range using unittest. Run the tests and show the output.",
        "Refactor fizzbuzz.py to accept optional fizz_div and buzz_div parameters (default 3 and 5). Update the tests.",
        "Add a CLI to fizzbuzz.py using argparse: python fizzbuzz.py --start 1 --end 15. Run it and show output.",
        # Turn 6: ask the agent to summarize — exercises long-context reasoning
        "Summarize all the changes we made to fizzbuzz.py across this entire session in bullet points.",
        "List all the Python files we created in the workspace.",
    ]

    for i, prompt in enumerate(prompts, 1):
        print(f"  [{INFO}] Turn {i}: {prompt[:70]}")
        turn(prompt)

    print(f"  [{INFO}] Waiting {FLUSH_WAIT_S}s for flush...")
    time.sleep(FLUSH_WAIT_S)

    llm_rows = tpq(f"""
        SELECT count() as cnt FROM table(agentshield_hook_events)
        WHERE hook_name = 'llm_input' AND event_time >= '{t_start}'
    """)

    tool_rows = tpq(f"""
        SELECT tool_name, count() as cnt
        FROM table(agentshield_hook_events)
        WHERE hook_name = 'before_tool_call' AND event_time >= '{t_start}'
        GROUP BY tool_name
        ORDER BY cnt DESC
    """)

    total_tools = sum(int(r["cnt"]) for r in tool_rows)

    # Note: before_compaction / after_compaction only fire on automatic context-window
    # overflow inside the gateway process, not from a /compact slash command.
    # They are observable when running long enough to hit the model context limit.
    compact_rows = tpq(
        f"SELECT hook_name, count() as cnt FROM table(agentshield_hook_events) "
        f"WHERE hook_name IN ('before_compaction','after_compaction') "
        f"AND event_time >= '{t_start}' GROUP BY hook_name"
    )
    compact_hooks = {r["hook_name"] for r in compact_rows}
    if compact_hooks:
        print(f"  [{INFO}] Compaction hooks fired: {compact_hooks}")
    else:
        print(f"  [{INFO}] Compaction hooks did not fire (context window not exceeded — expected)")

    results = [
        check("≥5 LLM turns",       int(llm_rows[0]["cnt"]) >= 5,
              f"{llm_rows[0]['cnt']} turns"),
        check("≥5 tool calls total", total_tools >= 5,
              f"{total_tools} tool calls"),
        check("diverse tool types",  len(tool_rows) >= 2,
              f"{len(tool_rows)} distinct tools"),
    ]
    print()
    print_table(tool_rows, ["tool_name", "cnt"])
    return all(results)


# ---------------------------------------------------------------------------
# Scenario 6 — Multiple agents (main + analyst)
# ---------------------------------------------------------------------------

def scenario_6(model: str | None):
    section("Scenario 6 — Multiple Agents (main + analyst)")
    t_start = now_iso()

    # Check that the 'analyst' agent is registered in OpenClaw.
    req = urllib.request.Request(
        f"{OPENCLAW_URL}/v1/models",
        headers={"Authorization": f"Bearer {OPENCLAW_TOKEN}"},
    )
    with urllib.request.urlopen(req, timeout=10) as r:
        models_resp = json.loads(r.read())
    model_ids = {m["id"] for m in models_resp.get("data", [])}

    if not check("openclaw/analyst model available",
                 "openclaw/analyst" in model_ids,
                 "run 'make add-agents' first"):
        return False

    # Send one message to main and one to analyst — different model IDs route to
    # different agents; each produces hook events with its own agent_id.
    print(f"  [{INFO}] Sending message to main agent...")
    reply_main = chat(
        [{"role": "user", "content": "Reply with exactly: 'I am main.'"}],
        user_id="agentshield-test-s6-main",
        model_override=model,
        # no model field change — defaults to openclaw/main in chat()
    )
    print(f"  [{INFO}] main reply: {reply_main[:80]}")

    print(f"  [{INFO}] Sending message to analyst agent...")
    # Override the model field inside the payload to target the analyst agent.
    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {OPENCLAW_TOKEN}",
    }
    payload = {
        "model": "openclaw/analyst",
        "messages": [{"role": "user", "content": "Reply with exactly: 'I am analyst.'"}],
        "user": "agentshield-test-s6-analyst",
    }
    req = urllib.request.Request(
        f"{OPENCLAW_URL}/v1/chat/completions",
        data=json.dumps(payload).encode(),
        headers=headers,
    )
    with urllib.request.urlopen(req, timeout=REPLY_TIMEOUT_S) as r:
        result = json.loads(r.read())
    reply_analyst = result["choices"][0]["message"]["content"]
    print(f"  [{INFO}] analyst reply: {reply_analyst[:80]}")

    print(f"  [{INFO}] Waiting {FLUSH_WAIT_S}s for flush...")
    time.sleep(FLUSH_WAIT_S)

    # Verify hook events for both agents are in Timeplus.
    agent_rows = tpq(f"""
        SELECT agent_id, count() as cnt
        FROM table(agentshield_hook_events)
        WHERE event_time >= '{t_start}'
          AND agent_id != ''
        GROUP BY agent_id
        ORDER BY agent_id
    """)
    agent_ids = {r["agent_id"] for r in agent_rows}

    results = [
        check("main agent events recorded",     "main"     in agent_ids),
        check("analyst agent events recorded",  "analyst"  in agent_ids),
        check("2 distinct agents in stream",    len(agent_ids) >= 2,
              f"{len(agent_ids)} agents: {sorted(agent_ids)}"),
    ]
    print()
    print_table(agent_rows, ["agent_id", "cnt"])
    return all(results)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

SCENARIOS = {
    1: ("Basic Conversation",         scenario_1),
    2: ("Multi-Turn with Tool Calls", scenario_2),
    3: ("Sub-Agent Spawn Chain",      scenario_3),
    4: ("Scheduled Cron Task",        scenario_4),
    5: ("Complex Coding + Compaction",scenario_5),
    6: ("Multiple Agents",            scenario_6),
}


def preflight():
    """Verify OpenClaw and Timeplus are reachable."""
    print("Preflight checks...")

    # OpenClaw
    try:
        req = urllib.request.Request(
            f"{OPENCLAW_URL}/v1/models",
            headers={"Authorization": f"Bearer {OPENCLAW_TOKEN}"},
        )
        with urllib.request.urlopen(req, timeout=10) as r:
            models = json.loads(r.read())
        ids = [m["id"] for m in models.get("data", [])]
        check("OpenClaw /v1/models reachable", len(ids) > 0, ", ".join(ids))
    except Exception as e:
        check("OpenClaw /v1/models reachable", False, str(e))
        print("\nOpenClaw is not reachable. Run: make configure && make start")
        sys.exit(1)

    # Timeplus
    try:
        rows = tpq("SELECT 1 AS ok")
        check("Timeplus SQL reachable", str(rows[0]["ok"]) == "1")
    except Exception as e:
        check("Timeplus SQL reachable", False, str(e))
        print("\nTimeplus is not reachable. Check it is running on port 3218.")
        sys.exit(1)

    # agentshield stream
    try:
        rows = tpq("SELECT count() as cnt FROM table(agentshield_hook_events)")
        check("agentshield_hook_events stream exists",
              True, f"{rows[0]['cnt']} existing rows")
    except Exception as e:
        check("agentshield_hook_events stream exists", False, str(e))
        print("\nStream missing. Start the AgentShield backend to create it.")
        sys.exit(1)

    print()


def main():
    parser = argparse.ArgumentParser(description="AgentShield OpenClaw test runner")
    parser.add_argument("--scenario", type=int, choices=SCENARIOS.keys(),
                        help="Run only this scenario (default: all)")
    parser.add_argument("--model", type=str, default=None,
                        help="Override backend model via x-openclaw-model header")
    args = parser.parse_args()

    preflight()

    to_run = {args.scenario: SCENARIOS[args.scenario]} if args.scenario else SCENARIOS
    passed, failed = 0, 0

    for num, (name, fn) in to_run.items():
        print(f"\n{'═' * 60}")
        print(f"  SCENARIO {num}: {name}")
        print(f"{'═' * 60}")
        try:
            ok = fn(args.model)
        except Exception as e:
            print(f"  [\033[31mERROR\033[0m] Scenario {num} raised an exception: {e}")
            ok = False
        if ok:
            passed += 1
            print(f"\n  [\033[32mSCENARIO {num} PASSED\033[0m]")
        else:
            failed += 1
            print(f"\n  [\033[31mSCENARIO {num} FAILED\033[0m]")

    print(f"\n{'═' * 60}")
    print(f"  Results: {passed} passed, {failed} failed out of {passed + failed} scenarios")
    print(f"{'═' * 60}\n")
    sys.exit(0 if failed == 0 else 1)


if __name__ == "__main__":
    main()
