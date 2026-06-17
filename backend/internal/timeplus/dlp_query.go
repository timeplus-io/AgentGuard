package timeplus

import (
	"strconv"
	"strings"

	"github.com/timeplus/agentguard/internal/config"
)

// Query-time DLP UDFs. Same conventions as the ingest UDFs in dlp.go:
// batched lists in/out, fail-open (never raise), config interpolated via
// {{...}} placeholders (percent-safe), API key visible via SHOW FUNCTIONS
// (accepted limitation, see dlp.go).
//
// The "TASK: <NAME>" prompt prefixes are load-bearing: the mock model server
// (backend/scripts/dlp_mock_model.py) dispatches on them for keyless tests.
//
// Security note: question and evidence text are interpolated verbatim into
// model prompts. Prompt injection is an accepted risk for this internal
// operator tool — same rationale as the API-key exposure note in dlp.go.

// dlpUnderstandUDFTemplate placeholders: {{BASE_URL}}, {{API_KEY}}, {{MODEL}}, {{TIMEOUT}}, {{MAX_TOKENS}}.
const dlpUnderstandUDFTemplate = `CREATE OR REPLACE FUNCTION dlp_understand_query(question string)
RETURNS string
LANGUAGE PYTHON AS $$
import json
import re
import requests

BASE_URL = "{{BASE_URL}}"
API_KEY = "{{API_KEY}}"
MODEL = "{{MODEL}}"

def _msg_content(resp):
    m = resp.json()['choices'][0]['message']
    return (m.get('content') or '').strip() or \
           (m.get('reasoning') or m.get('reasoning_content') or '').strip()

ENTITY_TYPES = ('phone', 'email', 'ip', 'domain', 'person', 'company', 'secret')
RISK_TAGS = ('PII', 'PHONE', 'EMAIL', 'CREDENTIAL', 'SOURCE_CODE',
             'CUSTOMER_DATA', 'FINANCIAL', 'EXFILTRATION')

def _fallback(question):
    words = [w.lower() for w in re.findall(r'[A-Za-z0-9]{4,}', question)][:8]
    ets = [t for t in ENTITY_TYPES if t in question.lower()]
    return json.dumps({'keywords': words, 'entity_types': ets,
                       'risk_tags': [], 'time_range_days': 30})

def _understand(question):
    headers = {'Content-Type': 'application/json'}
    if API_KEY:
        headers['Authorization'] = 'Bearer ' + API_KEY
    prompt = ('TASK: QUERY_UNDERSTANDING. Parse this security-audit question into '
              'retrieval intent. Respond with strict JSON only: '
              '{"keywords": [up to 8 lowercase search words], '
              '"entity_types": [zero or more of phone, email, ip, domain, person, company, secret], '
              '"risk_tags": [zero or more of PII, PHONE, EMAIL, CREDENTIAL, SOURCE_CODE, '
              'CUSTOMER_DATA, FINANCIAL, EXFILTRATION], '
              '"time_range_days": <integer>}. Question: ' + question[:2000])
    resp = requests.post(BASE_URL + '/chat/completions', headers=headers, json={
        'model': MODEL,
        'messages': [{'role': 'user', 'content': prompt}],
        'temperature': 0,
        'max_tokens': {{MAX_TOKENS}},
    }, timeout={{TIMEOUT}})
    raw = _msg_content(resp)
    parsed = json.loads(raw[raw.find('{'):raw.rfind('}') + 1])
    return json.dumps({
        'keywords': [str(k).lower() for k in parsed.get('keywords', [])][:8],
        'entity_types': [t for t in parsed.get('entity_types', []) if t in ENTITY_TYPES],
        'risk_tags': [t for t in parsed.get('risk_tags', []) if t in RISK_TAGS],
        'time_range_days': int(parsed.get('time_range_days') or 30),
    })

def dlp_understand_query(questions):
    out = []
    for q in questions:
        q = q or ''
        try:
            out.append(_understand(q))
        except Exception:
            out.append(_fallback(q))
    return out
$$`

// dlpRerankUDFTemplate placeholders: {{BASE_URL}}, {{API_KEY}}, {{MODEL}}, {{TIMEOUT}}, {{MAX_TOKENS}}.
// Scores each (question, content) pair 0..1. Chunks the batch into small groups
// per chat call and asks for number-keyed scores so a local model can't misalign
// a chunk's scores onto the wrong items; any item the model does not score
// defaults to 0.0 so it sinks below scored-relevant rows.
const dlpRerankUDFTemplate = `CREATE OR REPLACE FUNCTION dlp_rerank(question string, content string)
RETURNS float64
LANGUAGE PYTHON AS $$
import json
import requests

BASE_URL = "{{BASE_URL}}"
API_KEY = "{{API_KEY}}"
MODEL = "{{MODEL}}"
CHUNK = 8

def _msg_content(resp):
    m = resp.json()['choices'][0]['message']
    return (m.get('content') or '').strip() or \
           (m.get('reasoning') or m.get('reasoning_content') or '').strip()

def _score_chunk(question, snippets):
    headers = {'Content-Type': 'application/json'}
    if API_KEY:
        headers['Authorization'] = 'Bearer ' + API_KEY
    numbered = '\n'.join(str(i + 1) + '. ' + s[:600] for i, s in enumerate(snippets))
    prompt = ('TASK: RERANK. Score how well each numbered ITEM answers the QUESTION, '
              '0.0 (irrelevant) to 1.0 (directly answers it). Respond with strict JSON only, '
              'mapping each item number to its score, e.g. {"1": 0.0, "2": 1.0}. ITEMS: ' + str(len(snippets)) +
              '\nQUESTION: ' + question[:1000] + '\n' + numbered)
    resp = requests.post(BASE_URL + '/chat/completions', headers=headers, json={
        'model': MODEL,
        'messages': [{'role': 'user', 'content': prompt}],
        'temperature': 0,
        'max_tokens': {{MAX_TOKENS}},
    }, timeout={{TIMEOUT}})
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

def dlp_rerank(questions, contents):
    n = len(contents)
    out = [0.0] * n
    for start in range(0, n, CHUNK):
        idx = list(range(start, min(start + CHUNK, n)))
        try:
            # Assumes all rows in a batch share the same question (one audit query
            # per invocation); mixed-question batches would score every row against
            # questions[idx[0]].
            scores = _score_chunk(questions[idx[0]] or '', [contents[i] or '' for i in idx])
            for j, i in enumerate(idx):
                if j < len(scores) and scores[j] is not None:
                    out[i] = max(0.0, min(1.0, scores[j]))
        except Exception:
            pass
    return out
$$`

// dlpSummarizeUDFTemplate placeholders: {{BASE_URL}}, {{API_KEY}}, {{MODEL}}, {{TIMEOUT}}, {{MAX_TOKENS}}.
// Produces the final audit report over the top-N evidence JSON. Returns ''
// on failure; the Go handler substitutes a deterministic fallback line.
const dlpSummarizeUDFTemplate = `CREATE OR REPLACE FUNCTION dlp_summarize(question string, evidence string)
RETURNS string
LANGUAGE PYTHON AS $$
import requests

BASE_URL = "{{BASE_URL}}"
API_KEY = "{{API_KEY}}"
MODEL = "{{MODEL}}"

def _msg_content(resp):
    m = resp.json()['choices'][0]['message']
    return (m.get('content') or '').strip() or \
           (m.get('reasoning') or m.get('reasoning_content') or '').strip()

def _summarize(question, evidence):
    headers = {'Content-Type': 'application/json'}
    if API_KEY:
        headers['Authorization'] = 'Bearer ' + API_KEY
    prompt = ('TASK: AUDIT_SUMMARY. You are a security auditor. Using ONLY the JSON '
              'evidence messages below, answer the question: who, when, and what data '
              'was involved. Cite evidence inline as [message_id]. If the evidence does '
              'not support a finding, say so. QUESTION: ' + question[:1000] +
              '\nEVIDENCE: ' + evidence[:12000])
    resp = requests.post(BASE_URL + '/chat/completions', headers=headers, json={
        'model': MODEL,
        'messages': [{'role': 'user', 'content': prompt}],
        'temperature': 0,
        'max_tokens': {{MAX_TOKENS}},
    }, timeout={{TIMEOUT}})
    return _msg_content(resp)

def dlp_summarize(questions, evidences):
    out = []
    for q, e in zip(questions, evidences):
        try:
            out.append(_summarize(q or '', e or ''))
        except Exception:
            out.append('')
    return out
$$`

// dlpReplacerWith fills the UDF template placeholders. maxTokens lets
// dlp_summarize raise its budget above the global dlp.max_output_tokens.
func dlpReplacerWith(cfg config.DLPConfig, model string, maxTokens int) *strings.Replacer {
	return strings.NewReplacer(
		"{{BASE_URL}}", cfg.BaseURL,
		"{{API_KEY}}", cfg.APIKey,
		"{{MODEL}}", model,
		"{{TIMEOUT}}", strconv.Itoa(cfg.RequestTimeoutSeconds),
		"{{MAX_TOKENS}}", strconv.Itoa(maxTokens),
	)
}

// dlpReplacer is the common case: timeout + the global max_output_tokens.
func dlpReplacer(cfg config.DLPConfig, model string) *strings.Replacer {
	return dlpReplacerWith(cfg, model, cfg.MaxOutputTokens)
}

// DLPUnderstandUDFDDL returns the CREATE OR REPLACE FUNCTION DDL for
// dlp_understand_query, interpolated with cfg and the risk model.
func DLPUnderstandUDFDDL(cfg config.DLPConfig) string {
	return dlpReplacer(cfg, cfg.RiskModel).Replace(dlpUnderstandUDFTemplate)
}

// DLPRerankUDFDDL returns the CREATE OR REPLACE FUNCTION DDL for
// dlp_rerank, interpolated with cfg and the risk model.
func DLPRerankUDFDDL(cfg config.DLPConfig) string {
	return dlpReplacer(cfg, cfg.RiskModel).Replace(dlpRerankUDFTemplate)
}

// DLPSummarizeUDFDDL returns the CREATE OR REPLACE FUNCTION DDL for
// dlp_summarize, interpolated with cfg and the summary model.
// The token budget is floored at 800 to ensure the summary is not truncated.
func DLPSummarizeUDFDDL(cfg config.DLPConfig) string {
	tokens := cfg.MaxOutputTokens
	if tokens < 800 {
		tokens = 800
	}
	return dlpReplacerWith(cfg, cfg.SummaryModel, tokens).Replace(dlpSummarizeUDFTemplate)
}
