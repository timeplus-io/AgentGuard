package timeplus

import (
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

// dlpUnderstandUDFTemplate placeholders: {{BASE_URL}}, {{API_KEY}}, {{MODEL}}.
const dlpUnderstandUDFTemplate = `CREATE OR REPLACE FUNCTION dlp_understand_query(question string)
RETURNS string
LANGUAGE PYTHON AS $$
import json
import re
import requests

BASE_URL = "{{BASE_URL}}"
API_KEY = "{{API_KEY}}"
MODEL = "{{MODEL}}"

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
        'max_tokens': 200,
    }, timeout=30)
    raw = resp.json()['choices'][0]['message']['content']
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

// dlpRerankUDFTemplate placeholders: {{BASE_URL}}, {{API_KEY}}, {{MODEL}}.
// Scores each (question, content) pair 0..1. Chunks the batch into groups of
// 20 per chat call; a failed chunk leaves its rows at the 0.5 default.
const dlpRerankUDFTemplate = `CREATE OR REPLACE FUNCTION dlp_rerank(question string, content string)
RETURNS float64
LANGUAGE PYTHON AS $$
import json
import requests

BASE_URL = "{{BASE_URL}}"
API_KEY = "{{API_KEY}}"
MODEL = "{{MODEL}}"
CHUNK = 20

def _score_chunk(question, snippets):
    headers = {'Content-Type': 'application/json'}
    if API_KEY:
        headers['Authorization'] = 'Bearer ' + API_KEY
    numbered = '\n'.join(str(i + 1) + '. ' + s[:600] for i, s in enumerate(snippets))
    prompt = ('TASK: RERANK. Score the relevance of each numbered ITEM to the QUESTION, '
              '0.0 (irrelevant) to 1.0 (directly answers it). Respond with strict JSON only: '
              '{"scores": [one float per item, in order]}. ITEMS: ' + str(len(snippets)) +
              '\nQUESTION: ' + question[:1000] + '\n' + numbered)
    resp = requests.post(BASE_URL + '/chat/completions', headers=headers, json={
        'model': MODEL,
        'messages': [{'role': 'user', 'content': prompt}],
        'temperature': 0,
        'max_tokens': 400,
    }, timeout=30)
    raw = resp.json()['choices'][0]['message']['content']
    parsed = json.loads(raw[raw.find('{'):raw.rfind('}') + 1])
    return [float(s) for s in parsed.get('scores', [])]

def dlp_rerank(questions, contents):
    n = len(contents)
    out = [0.5] * n
    for start in range(0, n, CHUNK):
        idx = list(range(start, min(start + CHUNK, n)))
        try:
            # Assumes all rows in a batch share the same question (one audit query
            # per invocation); mixed-question batches would score every row against
            # questions[idx[0]].
            scores = _score_chunk(questions[idx[0]] or '', [contents[i] or '' for i in idx])
            for j, i in enumerate(idx):
                if j < len(scores):
                    out[i] = max(0.0, min(1.0, scores[j]))
        except Exception:
            pass
    return out
$$`

// dlpSummarizeUDFTemplate placeholders: {{BASE_URL}}, {{API_KEY}}, {{MODEL}}.
// Produces the final audit report over the top-N evidence JSON. Returns ''
// on failure; the Go handler substitutes a deterministic fallback line.
const dlpSummarizeUDFTemplate = `CREATE OR REPLACE FUNCTION dlp_summarize(question string, evidence string)
RETURNS string
LANGUAGE PYTHON AS $$
import requests

BASE_URL = "{{BASE_URL}}"
API_KEY = "{{API_KEY}}"
MODEL = "{{MODEL}}"

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
        'max_tokens': 800,
    }, timeout=60)
    return resp.json()['choices'][0]['message']['content']

def dlp_summarize(questions, evidences):
    out = []
    for q, e in zip(questions, evidences):
        try:
            out.append(_summarize(q or '', e or ''))
        except Exception:
            out.append('')
    return out
$$`

// dlpReplacer returns a strings.Replacer that fills in the three UDF
// template placeholders from cfg. Centralised here so the embed and risk
// builders in dlp.go also call it, keeping the placeholder set consistent.
func dlpReplacer(cfg config.DLPConfig, model string) *strings.Replacer {
	return strings.NewReplacer(
		"{{BASE_URL}}", cfg.BaseURL,
		"{{API_KEY}}", cfg.APIKey,
		"{{MODEL}}", model,
	)
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
func DLPSummarizeUDFDDL(cfg config.DLPConfig) string {
	return dlpReplacer(cfg, cfg.SummaryModel).Replace(dlpSummarizeUDFTemplate)
}
