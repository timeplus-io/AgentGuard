#!/usr/bin/env python3
"""Generate / ingest / score the audit-rule test-data corpus (issue #280).

Loads a labeled corpus of simulated multi-turn agent sessions into
agentguard_cim_event and scores the installed dsa-* detectors against the
description-derived ground truth. Stdlib only.
"""
import argparse
import base64
import datetime as dt
import glob
import json
import os
import sys
import time
import urllib.request

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


def filter_cases(cases, case_ids):
    """Keep only the named case_ids (case-insensitive); error on unknown ids."""
    wanted = {cid.lower() for cid in case_ids}
    picked = [c for c in cases if c["case_id"].lower() in wanted]
    found = {c["case_id"].lower() for c in picked}
    unknown = sorted(cid for cid in case_ids if cid.lower() not in found)
    if unknown:
        raise ValueError(f"unknown case id(s): {', '.join(unknown)}")
    return picked


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


INSERT_PREFIX = b"INSERT INTO agentguard_cim_event FORMAT JSONEachRow\n"


def rows_to_jsoneachrow(rows):
    return "\n".join(json.dumps(r, ensure_ascii=False) for r in rows)


def build_insert_payload(rows):
    return INSERT_PREFIX + rows_to_jsoneachrow(rows).encode("utf-8")


def build_all_rows(cases, now, deployment_id, repeat=1):
    rows = []
    for r in range(max(1, repeat)):
        suffix = "" if r == 0 else f"-r{r}"
        for idx, case in enumerate(cases):
            case_now = now - dt.timedelta(milliseconds=idx * CASE_GAP_MS)
            rows.extend(case_to_rows(case, case_now, deployment_id, suffix))
    return rows


def http_exec(cfg, payload):
    url = f"http://{cfg['host']}:{cfg['port']}/"
    req = urllib.request.Request(url, data=payload, method="POST")
    token = base64.b64encode(f"{cfg['user']}:{cfg['password']}".encode()).decode()
    req.add_header("Authorization", f"Basic {token}")
    try:
        with urllib.request.urlopen(req, timeout=cfg.get("timeout", 30)) as resp:
            return resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        sys.stderr.write(f"error: HTTP {e.code}: {e.read().decode(errors='replace')}\n")
        sys.exit(1)
    except urllib.error.URLError as e:
        sys.stderr.write(f"error: cannot reach Timeplus at {url}: {e.reason}\n")
        sys.exit(1)


def _cfg_from_args(args):
    return {"host": args.host, "port": args.port, "user": args.user,
            "password": args.password}


def _scenarios_dir(args):
    return args.scenarios or os.path.join(os.path.dirname(os.path.abspath(__file__)), "scenarios")


def _select_cases(args):
    codes = args.codes.split(",") if args.codes else None
    cases = load_scenarios(_scenarios_dir(args), codes)
    if args.cases:
        try:
            cases = filter_cases(cases, args.cases.split(","))
        except ValueError as e:
            sys.stderr.write(f"error: {e}\n")
            sys.exit(1)
    return cases


def cmd_load(args):
    cases = _select_cases(args)
    if args.count:
        by_code = {}
        capped = []
        for c in cases:
            by_code.setdefault(c["audit_code"], 0)
            if by_code[c["audit_code"]] < args.count:
                capped.append(c)
                by_code[c["audit_code"]] += 1
        cases = capped
    now = dt.datetime.now(dt.timezone.utc)
    rows = build_all_rows(cases, now, args.deployment_id, args.repeat)
    payload = build_insert_payload(rows)
    if args.dry_run:
        print(f"[dry-run] {len(cases)} cases -> {len(rows)} rows "
              f"(deployment_id={args.deployment_id})")
        return
    http_exec(_cfg_from_args(args), payload)
    print(f"Loaded {len(cases)} cases -> {len(rows)} rows into agentguard_cim_event "
          f"(deployment_id={args.deployment_id})")


def expected_rule_id(code):
    if code.upper() in CODES_WITHOUT_RULE:
        return None
    return "dsa-" + code.lower()


def score_cases(cases, fired):
    by_code = {}
    for case in cases:
        by_code.setdefault(case["audit_code"], []).append(case)
    report = {}
    for code, code_cases in sorted(by_code.items()):
        rule = expected_rule_id(code)
        tp = fp = fn = tn = 0
        misses, false_alarms = [], []
        for case in code_cases:
            sid = case_session_id(case)
            hit = rule is not None and (rule, sid) in fired
            if case["label"] == "positive":
                if hit:
                    tp += 1
                else:
                    fn += 1
                    misses.append(case["case_id"])
            else:
                if hit:
                    fp += 1
                    false_alarms.append(case["case_id"])
                else:
                    tn += 1
        precision = tp / (tp + fp) if (tp + fp) else None
        recall = tp / (tp + fn) if (tp + fn) else None
        report[code] = {
            "tp": tp, "fp": fp, "fn": fn, "tn": tn,
            "precision": precision, "recall": recall,
            "no_rule": rule is None,
            "misses": misses, "false_alarms": false_alarms,
        }
    return report


def parse_security_events_tsv(text):
    fired = set()
    for line in text.splitlines():
        line = line.strip("\n")
        if not line.strip():
            continue
        parts = line.split("\t")
        if len(parts) >= 2:
            fired.add((parts[0].strip(), parts[1].strip()))
    return fired


def query_fired(cfg, deployment_id):
    sql = (
        "SELECT rule_id, session_id FROM table(agentguard_security_events) "
        f"WHERE deployment_id = '{deployment_id}' FORMAT TabSeparated"
    )
    text = http_exec(cfg, sql.encode("utf-8"))
    return parse_security_events_tsv(text)


def _fmt_pct(x):
    return "  n/a" if x is None else f"{x*100:5.1f}%"


def cmd_score(args):
    cases = _select_cases(args)
    if args.settle:
        time.sleep(args.settle)
    fired = query_fired(_cfg_from_args(args), args.deployment_id)
    report = score_cases(cases, fired)
    print(f"{'code':5} {'TP':>3} {'FP':>3} {'FN':>3} {'TN':>3} "
          f"{'prec':>6} {'recall':>6}  notes")
    for code, r in report.items():
        note = "NO RULE (gap)" if r["no_rule"] else ""
        print(f"{code:5} {r['tp']:>3} {r['fp']:>3} {r['fn']:>3} {r['tn']:>3} "
              f"{_fmt_pct(r['precision'])} {_fmt_pct(r['recall'])}  {note}")
        if r["misses"]:
            print(f"      false negatives: {', '.join(r['misses'])}")
        if r["false_alarms"]:
            print(f"      false positives: {', '.join(r['false_alarms'])}")


def _default_staging(args):
    return args.staging or os.path.join(
        os.path.dirname(os.path.abspath(__file__)), "scenarios_generated")


def cmd_generate(args):
    import llm_gen  # local import: llm_gen imports this module
    codes = [c.strip().upper() for c in args.codes.split(",") if c.strip()]
    if not codes:
        # default: every code present in the scenarios dir
        codes = sorted(
            os.path.splitext(os.path.basename(p))[0].upper()
            for p in glob.glob(os.path.join(_scenarios_dir(args), "A*.json")))
    if not codes:
        sys.stderr.write("error: no codes given and no scenarios/A*.json found\n")
        sys.exit(1)
    labels = ["positive", "negative"] if args.label == "both" else [args.label]
    args.api_type = (args.api_type
                     or os.environ.get("AUDIT_GEN_API_TYPE", "").strip()
                     or "openai")
    if args.api_type not in ("openai", "anthropic"):
        sys.stderr.write(
            f"error: invalid api type {args.api_type!r} "
            "(--api-type / AUDIT_GEN_API_TYPE must be 'openai' or 'anthropic')\n")
        sys.exit(1)
    args.model = args.model or os.environ.get("AUDIT_GEN_MODEL", "").strip()
    if not args.model:
        sys.stderr.write("error: no --model (or AUDIT_GEN_MODEL) set\n")
        sys.exit(1)
    if args.api_type == "anthropic":
        base_url = args.base_url or os.environ.get(
            "ANTHROPIC_BASE_URL", llm_gen.ANTHROPIC_DEFAULT_BASE)
        api_key = args.api_key or os.environ.get("ANTHROPIC_API_KEY", "")
    else:
        base_url = args.base_url or os.environ.get("OPENAI_BASE_URL", "")
        api_key = args.api_key or os.environ.get("OPENAI_API_KEY", "")
    if not base_url:
        sys.stderr.write("error: no --base-url (or OPENAI_BASE_URL) set\n")
        sys.exit(1)
    cfg = {"api_type": args.api_type, "base_url": base_url, "api_key": api_key,
           "model": args.model, "max_tokens": args.max_tokens,
           "timeout": args.timeout}
    staging = _default_staging(args)
    for code in codes:
        existing = load_scenarios(_scenarios_dir(args), [code])
        for label in labels:
            staged = llm_gen.load_staged(staging, code)
            cases = llm_gen.generate_for_code(
                code, existing, staged, label, args.per_code, cfg,
                complete=llm_gen.llm_complete, retries=args.retries)
            if cases:
                path = llm_gen.write_staging(staging, code, cases)
                print(f"{code}: staged {len(cases)} {label} case(s) -> {path}")
            if len(cases) < args.per_code:
                sys.stderr.write(
                    f"warn: {code}/{label}: only {len(cases)}/{args.per_code} "
                    "valid cases after retries\n")
    print("Review the staged files, then run: gen_audit_data.py merge")


def cmd_merge(args):
    import llm_gen  # local import: llm_gen imports this module
    codes = [c for c in args.codes.split(",") if c.strip()] or None
    try:
        merged = llm_gen.merge_generated(
            _scenarios_dir(args), _default_staging(args), codes)
    except ValueError as e:
        sys.stderr.write(f"error: {e} (nothing merged)\n")
        sys.exit(1)
    if not merged:
        print("Nothing staged to merge.")
        return
    for code, n in sorted(merged.items()):
        print(f"{code}: merged {n} case(s) into scenarios/{code}.json")
    print("Run `python3 -m unittest test_corpus` to re-check the corpus contract.")


def cmd_clear(args):
    print("agentguard_cim_event is append-only; seeded rows cannot be deleted "
          "in place.\nTo remove this run's data, drop and recreate the stream, "
          "or filter it out by:\n"
          f"  deployment_id = '{args.deployment_id}'\n"
          "Seeded security_events are similarly append-only.")


def _add_common(p):
    p.add_argument("--host", default=os.environ.get("TIMEPLUS_HOST", "localhost"))
    p.add_argument("--port", default="8123")
    p.add_argument("--user", default=os.environ.get("TIMEPLUS_USER", "proton"))
    p.add_argument("--password", default=os.environ.get("TIMEPLUS_PASSWORD", ""))
    p.add_argument("--deployment-id", default=DEFAULT_DEPLOYMENT_ID)
    p.add_argument("--codes", default="", help="comma-separated, e.g. A01,A03")
    p.add_argument("--cases", default="",
                   help="comma-separated case ids, e.g. A01-pos-001")
    p.add_argument("--scenarios", default="", help="override scenarios dir")


def main(argv=None):
    ap = argparse.ArgumentParser(description="Audit-rule test-data generator (#280)")
    sub = ap.add_subparsers(dest="cmd", required=True)

    lp = sub.add_parser("load", help="ingest corpus into agentguard_cim_event")
    _add_common(lp)
    lp.add_argument("--count", type=int, default=0, help="cap cases per code (0 = all)")
    lp.add_argument("--repeat", type=int, default=1)
    lp.add_argument("--dry-run", action="store_true")
    lp.set_defaults(func=cmd_load)

    sp = sub.add_parser("score", help="score dsa-* detectors vs. labels")
    _add_common(sp)
    sp.add_argument("--settle", type=float, default=5.0)
    sp.set_defaults(func=cmd_score)

    cp = sub.add_parser("clear", help="print teardown guidance (append-only stream)")
    _add_common(cp)
    cp.set_defaults(func=cmd_clear)

    gp = sub.add_parser("generate",
                        help="LLM-generate new cases into the staging dir")
    gp.add_argument("--codes", default="",
                    help="comma-separated, e.g. A03,A10 (default: all codes)")
    gp.add_argument("--scenarios", default="", help="override scenarios dir")
    gp.add_argument("--staging", default="",
                    help="override staging dir (default scenarios_generated/)")
    gp.add_argument("--per-code", type=int, default=6,
                    help="cases per code per label (default 6)")
    gp.add_argument("--label", choices=["positive", "negative", "both"],
                    default="both")
    gp.add_argument("--api-type", choices=["openai", "anthropic"], default="",
                    help="env: AUDIT_GEN_API_TYPE; default openai")
    gp.add_argument("--base-url", default="",
                    help="LLM endpoint (env: OPENAI_BASE_URL / ANTHROPIC_BASE_URL)")
    gp.add_argument("--api-key", default="",
                    help="env: OPENAI_API_KEY / ANTHROPIC_API_KEY")
    gp.add_argument("--model", default="", help="env: AUDIT_GEN_MODEL")
    gp.add_argument("--max-tokens", type=int, default=8192)
    gp.add_argument("--timeout", type=float, default=300.0)
    gp.add_argument("--retries", type=int, default=2,
                    help="extra LLM calls to refill dropped invalid cases")
    gp.set_defaults(func=cmd_generate)

    mp = sub.add_parser("merge",
                        help="fold reviewed staged cases into scenarios/")
    mp.add_argument("--codes", default="", help="comma-separated, e.g. A03,A10")
    mp.add_argument("--scenarios", default="", help="override scenarios dir")
    mp.add_argument("--staging", default="",
                    help="override staging dir (default scenarios_generated/)")
    mp.set_defaults(func=cmd_merge)

    args = ap.parse_args(argv)
    args.func(args)


if __name__ == "__main__":
    main()
