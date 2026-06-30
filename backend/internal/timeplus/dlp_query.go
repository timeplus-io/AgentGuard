package timeplus

// Query-time DLP UDFs. Same conventions as the ingest UDFs in dlp.go: batched
// lists in/out, fail-open (never raise), and a trailing `config` string (JSON)
// that carries the endpoint, key, model, timeout and token budget. The bodies
// are STATIC — config is supplied at call time from agentguard_dlp_config (the
// funnel passes it via a coalesced scalar sub-query), so there is no API key in
// the UDF source and no re-provision needed to change models/endpoint.
//
// The "TASK: <NAME>" prompt prefixes are load-bearing: the mock model server
// (backend/scripts/dlp_mock_model.py) dispatches on them for keyless tests.
//
// Security note: question and evidence text are interpolated verbatim into
// model prompts. Prompt injection is an accepted risk for this internal
// operator tool — same rationale as the API-key note in dlp.go.

// _dlp_cfg(configs) (defined inline in each UDF) returns the first non-empty
// parsed config in the batch, or {} on absence/parse failure.

// dlp_understand_query(question, config): parse an audit question into retrieval
// intent. Falls back to keyword extraction when the endpoint is unset or errors.
const ddlDLPUnderstandUDF = `CREATE OR REPLACE FUNCTION dlp_understand_query(question string, config string)
RETURNS string
LANGUAGE PYTHON AS $$
import json
import re
import requests

ENTITY_TYPES = ('phone', 'email', 'ip', 'domain', 'person', 'company', 'secret')
RISK_TAGS = ('PII', 'PHONE', 'EMAIL', 'CREDENTIAL', 'SOURCE_CODE',
             'CUSTOMER_DATA', 'FINANCIAL', 'EXFILTRATION')

def _cfg(configs):
    for s in configs:
        if s:
            try:
                return json.loads(s)
            except Exception:
                return {}
    return {}

def _msg_content(resp):
    m = resp.json()['choices'][0]['message']
    return (m.get('content') or '').strip() or \
           (m.get('reasoning') or m.get('reasoning_content') or '').strip()

def _fallback(question):
    words = [w.lower() for w in re.findall(r'[A-Za-z0-9]{4,}', question)][:8]
    ets = [t for t in ENTITY_TYPES if t in question.lower()]
    return json.dumps({'keywords': words, 'entity_types': ets,
                       'risk_tags': [], 'time_range_days': 30})

def _understand(question, base, api_key, model, max_tokens, timeout):
    headers = {'Content-Type': 'application/json'}
    if api_key:
        headers['Authorization'] = 'Bearer ' + api_key
    prompt = ('TASK: QUERY_UNDERSTANDING. Parse this security-audit question into '
              'retrieval intent. Respond with strict JSON only: '
              '{"keywords": [up to 8 lowercase search words], '
              '"entity_types": [zero or more of phone, email, ip, domain, person, company, secret], '
              '"risk_tags": [zero or more of PII, PHONE, EMAIL, CREDENTIAL, SOURCE_CODE, '
              'CUSTOMER_DATA, FINANCIAL, EXFILTRATION], '
              '"time_range_days": <integer>}. Question: ' + question[:2000])
    resp = requests.post(base + '/chat/completions', headers=headers, json={
        'model': model,
        'messages': [{'role': 'user', 'content': prompt}],
        'temperature': 0,
        'max_tokens': max_tokens,
    }, timeout=timeout)
    raw = _msg_content(resp)
    parsed = json.loads(raw[raw.find('{'):raw.rfind('}') + 1])
    return json.dumps({
        'keywords': [str(k).lower() for k in parsed.get('keywords', [])][:8],
        'entity_types': [t for t in parsed.get('entity_types', []) if t in ENTITY_TYPES],
        'risk_tags': [t for t in parsed.get('risk_tags', []) if t in RISK_TAGS],
        'time_range_days': int(parsed.get('time_range_days') or 30),
    })

def dlp_understand_query(questions, configs):
    cfg = _cfg(configs)
    base = cfg.get('base_url') or ''
    api_key = cfg.get('api_key') or ''
    model = cfg.get('model') or ''
    try:
        max_tokens = int(cfg.get('max_tokens') or 512)
    except Exception:
        max_tokens = 512
    try:
        timeout = float(cfg.get('timeout') or 50)
    except Exception:
        timeout = 50
    out = []
    for q in questions:
        q = q or ''
        try:
            if not base:
                raise Exception('no endpoint')
            out.append(_understand(q, base, api_key, model, max_tokens, timeout))
        except Exception:
            out.append(_fallback(q))
    return out
$$`

// dlp_rerank(question, content, config): score each (question, content) pair
// 0..1. Chunks the batch into small groups per chat call and asks for
// number-keyed scores so a local model can't misalign a chunk's scores onto the
// wrong items; any item the model does not score defaults to 0.0.
const ddlDLPRerankUDF = `CREATE OR REPLACE FUNCTION dlp_rerank(question string, content string, config string)
RETURNS float64
LANGUAGE PYTHON AS $$
import json
import requests

CHUNK = 8

def _cfg(configs):
    for s in configs:
        if s:
            try:
                return json.loads(s)
            except Exception:
                return {}
    return {}

def _msg_content(resp):
    m = resp.json()['choices'][0]['message']
    return (m.get('content') or '').strip() or \
           (m.get('reasoning') or m.get('reasoning_content') or '').strip()

def _score_chunk(question, snippets, base, api_key, model, max_tokens, timeout):
    headers = {'Content-Type': 'application/json'}
    if api_key:
        headers['Authorization'] = 'Bearer ' + api_key
    numbered = '\n'.join(str(i + 1) + '. ' + s[:600] for i, s in enumerate(snippets))
    prompt = ('TASK: RERANK. Score how well each numbered ITEM answers the QUESTION, '
              '0.0 (irrelevant) to 1.0 (directly answers it). Respond with strict JSON only, '
              'mapping each item number to its score, e.g. {"1": 0.0, "2": 1.0}. ITEMS: ' + str(len(snippets)) +
              '\nQUESTION: ' + question[:1000] + '\n' + numbered)
    resp = requests.post(base + '/chat/completions', headers=headers, json={
        'model': model,
        'messages': [{'role': 'user', 'content': prompt}],
        'temperature': 0,
        'max_tokens': max_tokens,
    }, timeout=timeout)
    raw = _msg_content(resp)
    parsed = json.loads(raw[raw.find('{'):raw.rfind('}') + 1])
    # Map by explicit item number so a mis-ordered or short array can't shift
    # scores onto the wrong items. Tolerate a flat number-to-score map, a nested
    # scores object, or a legacy ordered scores array. Unscored items stay None
    # and the caller defaults them to 0.0.
    sc = parsed.get('scores', parsed) if isinstance(parsed, dict) else parsed
    out = [None] * len(snippets)
    if isinstance(sc, dict):
        for k in range(len(snippets)):
            v = sc.get(str(k + 1))
            if v is not None:
                out[k] = float(v)
    elif isinstance(sc, list):
        for k in range(min(len(sc), len(snippets))):
            out[k] = float(sc[k])
    return out

def dlp_rerank(questions, contents, configs):
    cfg = _cfg(configs)
    base = cfg.get('base_url') or ''
    api_key = cfg.get('api_key') or ''
    model = cfg.get('model') or ''
    try:
        max_tokens = int(cfg.get('max_tokens') or 512)
    except Exception:
        max_tokens = 512
    try:
        timeout = float(cfg.get('timeout') or 50)
    except Exception:
        timeout = 50
    n = len(contents)
    out = [0.0] * n
    if not base:
        return out
    for start in range(0, n, CHUNK):
        idx = list(range(start, min(start + CHUNK, n)))
        try:
            # Assumes all rows in a batch share the same question (one audit query
            # per invocation); mixed-question batches would score every row against
            # questions[idx[0]].
            scores = _score_chunk(questions[idx[0]] or '', [contents[i] or '' for i in idx],
                                  base, api_key, model, max_tokens, timeout)
            for j, i in enumerate(idx):
                if j < len(scores) and scores[j] is not None:
                    out[i] = max(0.0, min(1.0, scores[j]))
        except Exception:
            pass
    return out
$$`

// dlp_summarize(question, evidence, config): produce the final audit report over
// the top-N evidence JSON. Returns ” on failure; the Go handler substitutes a
// deterministic fallback line.
const ddlDLPSummarizeUDF = `CREATE OR REPLACE FUNCTION dlp_summarize(question string, evidence string, config string)
RETURNS string
LANGUAGE PYTHON AS $$
import json
import requests

def _cfg(configs):
    for s in configs:
        if s:
            try:
                return json.loads(s)
            except Exception:
                return {}
    return {}

def _msg_content(resp):
    m = resp.json()['choices'][0]['message']
    return (m.get('content') or '').strip() or \
           (m.get('reasoning') or m.get('reasoning_content') or '').strip()

def _summarize(question, evidence, base, api_key, model, max_tokens, timeout):
    headers = {'Content-Type': 'application/json'}
    if api_key:
        headers['Authorization'] = 'Bearer ' + api_key
    prompt = ('TASK: AUDIT_SUMMARY. You are a security auditor. Using ONLY the JSON '
              'evidence messages below, answer the question: who, when, and what data '
              'was involved. Cite evidence inline as [message_id]. If the evidence does '
              'not support a finding, say so. QUESTION: ' + question[:1000] +
              '\nEVIDENCE: ' + evidence[:12000])
    resp = requests.post(base + '/chat/completions', headers=headers, json={
        'model': model,
        'messages': [{'role': 'user', 'content': prompt}],
        'temperature': 0,
        'max_tokens': max_tokens,
    }, timeout=timeout)
    return _msg_content(resp)

def dlp_summarize(questions, evidences, configs):
    cfg = _cfg(configs)
    base = cfg.get('base_url') or ''
    api_key = cfg.get('api_key') or ''
    model = cfg.get('model') or ''
    try:
        max_tokens = int(cfg.get('max_tokens') or 800)
    except Exception:
        max_tokens = 800
    try:
        timeout = float(cfg.get('timeout') or 50)
    except Exception:
        timeout = 50
    out = []
    for q, e in zip(questions, evidences):
        try:
            if not base:
                raise Exception('no endpoint')
            out.append(_summarize(q or '', e or '', base, api_key, model, max_tokens, timeout))
        except Exception:
            out.append('')
    return out
$$`
