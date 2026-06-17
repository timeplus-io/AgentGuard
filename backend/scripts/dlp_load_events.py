#!/usr/bin/env python3
"""Load Semantic DLP sample hook events into agentguard_hook_events.

Reads backend/scripts/dlp_sample_events.json, stamps each row with a recent
event_time (now - offset_minutes), and inserts the rows into the
agentguard_hook_events stream over the ClickHouse-compatible HTTP interface
(:8123) using JSONEachRow. No third-party dependencies.

The CIM materialized views (mv_cim_claudecode / mv_cim_openclaw) and the DLP
enrichment MV (mv_dlp_enrich) must already exist — run the setup wizard /
POST /api/setup/provision (or `make dlp-integration`) first.

Env (with CLI fallbacks):
  TIMEPLUS_HOST      default localhost
  TIMEPLUS_USER      default proton
  TIMEPLUS_PASSWORD  default empty

Usage:
  python3 dlp_load_events.py [--host H] [--port 8123] [--user U] [--password P]
                             [--file events.json] [--repeat N]
"""
import argparse
import base64
import datetime as dt
import json
import os
import sys
import urllib.request

HOOK_COLUMNS = [
    "hook_name", "event_time", "session_id", "run_id", "agent_id",
    "deployment_id", "deployment_name", "session_key", "tool_name",
    "provider", "model", "hook_decision", "block_reason", "event_data",
    "agent_type",
]


def to_row(ev, now, suffix=""):
    """Map one sample event to an agentguard_hook_events JSONEachRow object."""
    offset = float(ev.get("offset_minutes", 0))
    ts = now - dt.timedelta(minutes=offset)
    row = {col: "" for col in HOOK_COLUMNS}
    row["event_time"] = ts.strftime("%Y-%m-%d %H:%M:%S.") + f"{ts.microsecond // 1000:03d}"
    for col in HOOK_COLUMNS:
        if col in ("event_time", "event_data"):
            continue
        if col in ev and ev[col] is not None:
            row[col] = ev[col]
    if suffix:
        row["session_id"] = f"{row['session_id']}{suffix}"
        row["agent_id"] = f"{row['agent_id']}{suffix}"
    # event_data is a string column holding the raw hook payload JSON.
    row["event_data"] = json.dumps(ev.get("event_data", {}), ensure_ascii=False)
    return row


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--host", default=os.environ.get("TIMEPLUS_HOST", "localhost"))
    ap.add_argument("--port", default="8123")
    ap.add_argument("--user", default=os.environ.get("TIMEPLUS_USER", "proton"))
    ap.add_argument("--password", default=os.environ.get("TIMEPLUS_PASSWORD", ""))
    here = os.path.dirname(os.path.abspath(__file__))
    ap.add_argument("--file", default=os.path.join(here, "dlp_sample_events.json"))
    ap.add_argument("--repeat", type=int, default=1,
                    help="insert the dataset N times with distinct session/agent suffixes")
    args = ap.parse_args()

    with open(args.file, encoding="utf-8") as f:
        events = [e for e in json.load(f) if isinstance(e, dict) and e.get("hook_name")]

    now = dt.datetime.now(dt.timezone.utc)
    lines = []
    for i in range(max(1, args.repeat)):
        suffix = "" if i == 0 else f"-r{i}"
        for ev in events:
            lines.append(json.dumps(to_row(ev, now, suffix), ensure_ascii=False))
    payload = ("INSERT INTO agentguard_hook_events FORMAT JSONEachRow\n"
               + "\n".join(lines)).encode("utf-8")

    url = f"http://{args.host}:{args.port}/"
    req = urllib.request.Request(url, data=payload, method="POST")
    token = base64.b64encode(f"{args.user}:{args.password}".encode()).decode()
    req.add_header("Authorization", "Basic " + token)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            resp.read()
    except urllib.error.HTTPError as e:
        sys.stderr.write(f"insert failed ({e.code}): {e.read().decode(errors='replace')}\n")
        sys.exit(1)
    except urllib.error.URLError as e:
        sys.stderr.write(f"cannot reach Timeplus at {url}: {e.reason}\n")
        sys.exit(1)

    print(f"inserted {len(lines)} hook events into agentguard_hook_events "
          f"({len(events)} events x {max(1, args.repeat)})")


if __name__ == "__main__":
    main()
