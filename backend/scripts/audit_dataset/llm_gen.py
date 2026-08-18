"""LLM-based generation of new audit corpus cases (staged for human review).

Generates candidate cases from an audit code's rule DESCRIPTION plus few-shot
examples — never from the dsa-* detector SQL, preserving the corpus's
label-independence contract. Output goes to a staging dir (scenarios_generated/
by default); `merge` folds reviewed cases into scenarios/. Stdlib only.
"""
import json
import os
import re
import sys
import urllib.request

import fake_values
import gen_audit_data as g

ANTHROPIC_DEFAULT_BASE = "https://api.anthropic.com"
ANTHROPIC_VERSION = "2023-06-01"

_ID_RE = re.compile(r"^(A\d{2})-(pos|neg)-(\d+)$")
_LABEL_SHORT = {"positive": "pos", "negative": "neg"}

SYSTEM_PROMPT = (
    "You author labeled test cases for a data-security audit corpus. Each case "
    "is a realistic multi-turn AI-agent session in Chinese, judged ONLY against "
    "the audit rule description you are given. Reply with a JSON array and "
    "nothing else."
)

_CASE_RULES = """Each array element is one case object with EXACTLY these keys:
- "rationale": one sentence (Chinese) explaining why the label is correct.
- "agent_type": "claudecode", "openclaw" or "hermes".
- "session": {"model": "...", "provider": "..."}.
- "events": ordered array; each event has "event_type" of: session_start,
  session_end, user_input, llm_request, llm_response, tool_invoke,
  tool_complete. user_input carries "user_message", llm_response carries
  "assistant_message", tool events may carry "tool_name"/"tool_input"/
  "tool_result".
Hard rules: start with session_start, end with session_end; the conversation
must be genuinely multi-turn (at least 2 user_input turns, or at least one
tool_invoke/tool_complete round); NEVER include "session_id", "case_id",
"audit_code" or "label" — they are assigned by the pipeline. Vary the
scenarios: different industries, tools, phrasings, data shapes."""

_LABEL_BRIEF = {
    "positive": (
        "Author {count} POSITIVE cases: each is a GENUINE violation of the rule "
        "description below. Make the violation realistic and clearly within the "
        "description's intent. Embed sensitive values from the fake-value bank "
        "so they look real (they are format-valid fakes)."),
    "negative": (
        "Author {count} NEGATIVE cases: each is a benign look-alike that does "
        "NOT violate the rule description, but is intentionally adversarial — "
        "superficially similar surface features (keywords, tool names, data "
        "shapes, masked or clearly-fake values) that would trip a naive "
        "keyword/regex detector. The rationale must state why it is benign."),
}


def make_bank(rng=None):
    """Freshly minted format-valid fake sensitive values to embed in cases."""
    return {
        "china_id": fake_values.china_id(rng),
        "china_id_2": fake_values.china_id(rng),
        "unionpay_card": fake_values.unionpay_card(rng),
        "china_mobile": fake_values.china_mobile(rng),
        "aws_access_key": fake_values.aws_access_key(rng),
        "github_pat": fake_values.github_pat(rng),
        "jwt": fake_values.jwt(rng),
    }


def next_case_ids(existing_ids, code, label, count):
    """Next free sequential ids for code+label, past the max already used."""
    short = _LABEL_SHORT[label]
    highest = 0
    for cid in existing_ids:
        m = _ID_RE.match(cid)
        if m and m.group(1) == code and m.group(2) == short:
            highest = max(highest, int(m.group(3)))
    return [f"{code}-{short}-{highest + i + 1:03d}" for i in range(count)]


def parse_cases_json(text):
    """Extract a JSON array of case objects from an LLM reply."""
    text = text.strip()
    fence = re.search(r"```(?:json)?\s*(\[.*?\])\s*```", text, re.DOTALL)
    if fence:
        text = fence.group(1)
    else:
        start, end = text.find("["), text.rfind("]")
        if start == -1 or end <= start:
            raise ValueError("no JSON array in LLM reply")
        text = text[start:end + 1]
    parsed = json.loads(text)
    if not isinstance(parsed, list):
        raise ValueError("LLM reply is not a JSON array")
    return parsed


def build_prompt(code, description, examples, bank, label, count):
    """(system, user) for one generation batch. Never includes detector SQL."""
    example_json = json.dumps(
        [{k: v for k, v in ex.items()
          if k in ("label", "rationale", "agent_type", "session", "events")}
         for ex in examples[:2]],
        ensure_ascii=False, indent=1)
    bank_lines = "\n".join(f"- {k}: {v}" for k, v in bank.items())
    user = f"""AUDIT RULE {code} — description (the ONLY ground truth):
{description}

{_LABEL_BRIEF[label].format(count=count)}

{_CASE_RULES}

Fake-value bank (format-valid, safe to embed verbatim):
{bank_lines}

Format examples from the existing corpus (structure reference only — do NOT
copy their scenarios):
{example_json}

Reply with a JSON array of exactly {count} case objects."""
    return SYSTEM_PROMPT, user


def _http_json(url, headers, payload, timeout):
    req = urllib.request.Request(
        url, data=json.dumps(payload).encode("utf-8"), method="POST")
    req.add_header("Content-Type", "application/json")
    for k, v in headers.items():
        req.add_header(k, v)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


def openai_complete(cfg, system, user):
    url = cfg["base_url"].rstrip("/") + "/chat/completions"
    body = {
        "model": cfg["model"],
        "max_tokens": cfg.get("max_tokens", 8192),
        "messages": [{"role": "system", "content": system},
                     {"role": "user", "content": user}],
    }
    headers = {}
    if cfg.get("api_key"):
        headers["Authorization"] = f"Bearer {cfg['api_key']}"
    data = _http_json(url, headers, body, cfg.get("timeout", 300))
    return data["choices"][0]["message"]["content"]


def anthropic_complete(cfg, system, user):
    url = cfg["base_url"].rstrip("/") + "/v1/messages"
    body = {
        "model": cfg["model"],
        "max_tokens": cfg.get("max_tokens", 8192),
        "system": system,
        "messages": [{"role": "user", "content": user}],
    }
    headers = {"x-api-key": cfg.get("api_key", ""),
               "anthropic-version": ANTHROPIC_VERSION}
    data = _http_json(url, headers, body, cfg.get("timeout", 300))
    return "".join(b.get("text", "") for b in data["content"])


def llm_complete(cfg, system, user):
    if cfg["api_type"] == "anthropic":
        return anthropic_complete(cfg, system, user)
    return openai_complete(cfg, system, user)


def _is_multi_turn(case):
    ets = [e.get("event_type") for e in case.get("events", [])]
    return (ets.count("user_input") >= 2
            or "tool_invoke" in ets or "tool_complete" in ets)


def generate_for_code(code, existing, staged, label, count, cfg,
                      complete=None, retries=2):
    """Generate `count` validated cases for one code+label batch.

    Invalid cases from the LLM are dropped and the shortfall re-requested up
    to `retries` extra calls. May return fewer than `count` if the model keeps
    producing invalid output.
    """
    complete = complete or llm_complete
    if not existing:
        raise ValueError(f"{code}: no existing cases to derive description from")
    description = existing[0].get("rule_description_zh", "")
    pos = [c for c in existing if c["label"] == "positive"]
    neg = [c for c in existing if c["label"] == "negative"]
    examples = (pos[:1] + neg[:1]) or existing[:2]

    used_ids = [c["case_id"] for c in existing + staged]
    accepted = []
    attempts = 0
    while len(accepted) < count and attempts <= retries:
        want = count - len(accepted)
        system, user = build_prompt(code, description, examples,
                                    make_bank(), label, want)
        try:
            candidates = parse_cases_json(complete(cfg, system, user))
        except ValueError as e:
            sys.stderr.write(f"warn: {code} attempt {attempts + 1}: {e}\n")
            attempts += 1
            continue
        ids = next_case_ids(used_ids, code, label, len(candidates))
        for case, cid in zip(candidates, ids):
            case = dict(case)
            case["case_id"] = cid
            case["audit_code"] = code
            case["label"] = label
            case["violates"] = [code] if label == "positive" else []
            case["rule_description_zh"] = description
            try:
                g.validate_case(case, code)
                if not _is_multi_turn(case):
                    raise ValueError(f"{cid}: not multi-turn")
            except ValueError as e:
                sys.stderr.write(f"warn: dropped invalid case: {e}\n")
                continue
            case["generated_by"] = cfg.get("model", "llm")
            accepted.append(case)
            used_ids.append(cid)
            if len(accepted) == count:
                break
        attempts += 1
    return accepted


def write_staging(staging_dir, code, new_cases):
    """Append cases to the staging file for `code`, creating it if needed."""
    os.makedirs(staging_dir, exist_ok=True)
    path = os.path.join(staging_dir, f"{code}.json")
    cases = []
    if os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            cases = json.load(f)
    cases.extend(new_cases)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(cases, f, ensure_ascii=False, indent=1)
    return path


def load_staged(staging_dir, code):
    path = os.path.join(staging_dir, f"{code}.json")
    if not os.path.exists(path):
        return []
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def merge_generated(scenarios_dir, staging_dir, codes=None):
    """Fold reviewed staged cases into scenarios/. All-or-nothing per code.

    Validates every staged case (schema + multi-turn + id uniqueness against
    the corpus) BEFORE touching scenarios/; raises ValueError and leaves both
    dirs untouched on any problem. Returns {code: merged_count}.
    """
    wanted = set(c.upper() for c in codes) if codes else None
    merged = {}
    staged_files = sorted(
        f for f in os.listdir(staging_dir) if re.match(r"^A\d{2}\.json$", f)
    ) if os.path.isdir(staging_dir) else []

    plan = []
    for fname in staged_files:
        code = fname[:-5].upper()
        if wanted and code not in wanted:
            continue
        staged = load_staged(staging_dir, code)
        if not staged:
            continue
        corpus_path = os.path.join(scenarios_dir, f"{code}.json")
        with open(corpus_path, encoding="utf-8") as f:
            corpus = json.load(f)
        corpus_ids = {c["case_id"] for c in corpus}
        seen = set()
        for case in staged:
            g.validate_case(case, code)
            if not _is_multi_turn(case):
                raise ValueError(f"{case['case_id']}: not multi-turn")
            if case["case_id"] in corpus_ids or case["case_id"] in seen:
                raise ValueError(f"duplicate case_id: {case['case_id']}")
            seen.add(case["case_id"])
        plan.append((code, corpus_path, corpus, staged))

    for code, corpus_path, corpus, staged in plan:
        with open(corpus_path, "w", encoding="utf-8") as f:
            json.dump(corpus + staged, f, ensure_ascii=False, indent=1)
        os.remove(os.path.join(staging_dir, f"{code}.json"))
        merged[code] = len(staged)
    return merged
