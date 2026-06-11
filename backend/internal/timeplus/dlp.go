package timeplus

import (
	"context"
	"fmt"
	"log"
	"strings"

	proton "github.com/timeplus-io/proton-go-driver/v2"
	"github.com/timeplus/agentguard/internal/config"
)

// ---------- Stream ----------

const ddlDLPEnrichedStream = `
CREATE STREAM IF NOT EXISTS agentguard_dlp_enriched
(
  event_time     datetime64(3),
  message_id     string,
  session_id     string,
  run_id         string,
  role           low_cardinality(string),
  agent_type     low_cardinality(string),
  agent_id       string,
  deployment_id  low_cardinality(string),
  model          low_cardinality(string),
  tool_name      string,
  content        string,
  entities       map(string, array(string)),
  risk_tags      array(low_cardinality(string)),
  risk_score     float64,
  embedding      array(float32)
)`

// dlpIndexDDLCandidates returns vector-index DDL variants to try in order.
// HNSW index support/syntax varies across Timeplus Enterprise versions; if
// every candidate fails, queries fall back to brute-force cosine_distance,
// which is acceptable at MVP scale (spec §2.1).
// Both candidates use IF NOT EXISTS so re-runs are no-ops when the index
// already exists (e.g. repeated calls to InitDLP / SyncDLP).
func dlpIndexDDLCandidates(dim int) []string {
	return []string{
		fmt.Sprintf(`ALTER STREAM agentguard_dlp_enriched ADD INDEX IF NOT EXISTS idx_dlp_embedding embedding TYPE vector_similarity('hnsw', 'cosineDistance', %d)`, dim),
		fmt.Sprintf(`ALTER STREAM agentguard_dlp_enriched ADD INDEX IF NOT EXISTS idx_dlp_embedding embedding TYPE vector_similarity('hnsw', 'cosine_distance', %d)`, dim),
	}
}

// ---------- UDFs ----------

// dlp_extract_entities: pure-Python regex/dictionary extraction (L0/L1).
// No network. Secret values are masked before they are stored.
const ddlDLPEntitiesUDF = `CREATE OR REPLACE FUNCTION dlp_extract_entities(content string)
RETURNS map(string, array(string))
LANGUAGE PYTHON AS $$
import re

PATTERNS = {
    'phone':  re.compile(r'(?<![\dA-Za-z])(?:\+?\d{1,3}[-. ]?)?(?:\(\d{2,4}\)[-. ]?)?\d{3,4}[-. ]\d{4}(?![\dA-Za-z])'),
    'email':  re.compile(r'[A-Za-z0-9._+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}'),
    'ip':     re.compile(r'(?<!\d)(?:\d{1,3}\.){3}\d{1,3}(?!\d)'),
    'domain': re.compile(r'\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+(?:com|net|org|io|ai|dev|app|co)\b', re.I),
}
SECRET_PATTERNS = [
    re.compile(r'AKIA[0-9A-Z]{16}'),
    re.compile(r'sk-[A-Za-z0-9_-]{20,}'),
    re.compile(r'ghp_[A-Za-z0-9]{36}'),
    re.compile(r'gho_[A-Za-z0-9]{36}'),
    re.compile(r'xox[baprs]-[A-Za-z0-9-]{10,}'),
    re.compile(r'-----BEGIN [A-Z ]*PRIVATE KEY-----'),
    re.compile(r'eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}'),
]
MAX_PER_TYPE = 20

def _mask(v):
    if len(v) <= 8:
        return v[:2] + '***'
    return v[:4] + '***' + v[-4:]

def dlp_extract_entities(contents):
    out = []
    for text in contents:
        ents = {}
        try:
            t = text or ''
            for name, pat in PATTERNS.items():
                vals = [m.strip() for m in pat.findall(t)]
                vals = list(dict.fromkeys(v for v in vals if v))[:MAX_PER_TYPE]
                if vals:
                    ents[name] = vals
            secrets = []
            for pat in SECRET_PATTERNS:
                secrets.extend(pat.findall(t))
            if secrets:
                ents['secret'] = [_mask(s) for s in dict.fromkeys(secrets)][:MAX_PER_TYPE]
        except Exception:
            ents = {}
        out.append(ents)
    return out
$$`

// dlpEmbedUDFTemplate placeholders: {{BASE_URL}}, {{API_KEY}}, {{MODEL}}.
//
// Security note: cfg.APIKey is embedded verbatim in the UDF source, so
// SHOW FUNCTIONS exposes it to anyone with Timeplus DDL access. Known,
// accepted limitation (spec §3.2): Timeplus credentials already gate the
// raw conversation content; use a scoped key for the model endpoint.
const dlpEmbedUDFTemplate = `CREATE OR REPLACE FUNCTION dlp_embed(content string, role string)
RETURNS array(float32)
LANGUAGE PYTHON AS $$
import requests

BASE_URL = "{{BASE_URL}}"
API_KEY = "{{API_KEY}}"
MODEL = "{{MODEL}}"
MAX_CHARS = 8000

def dlp_embed(contents, roles):
    n = len(contents)
    out = [[] for _ in range(n)]
    idx = []
    inputs = []
    for i in range(n):
        if roles[i] in ('user', 'assistant') and contents[i]:
            idx.append(i)
            inputs.append(contents[i][:MAX_CHARS])
    if not inputs:
        return out
    try:
        headers = {'Content-Type': 'application/json'}
        if API_KEY:
            headers['Authorization'] = 'Bearer ' + API_KEY
        resp = requests.post(BASE_URL + '/embeddings', headers=headers,
                             json={'model': MODEL, 'input': inputs}, timeout=30)
        for j, item in enumerate(resp.json().get('data', [])):
            if j < len(idx):
                out[idx[j]] = [float(x) for x in item.get('embedding', [])]
    except Exception:
        pass
    return out
$$`

func DLPEmbedUDFDDL(cfg config.DLPConfig) string {
	return strings.NewReplacer(
		"{{BASE_URL}}", cfg.BaseURL,
		"{{API_KEY}}", cfg.APIKey,
		"{{MODEL}}", cfg.EmbeddingModel,
	).Replace(dlpEmbedUDFTemplate)
}

// dlpRiskUDFTemplate placeholders: {{BASE_URL}}, {{API_KEY}}, {{MODEL}}. Same API-key exposure note as dlpEmbedUDFTemplate.
const dlpRiskUDFTemplate = `CREATE OR REPLACE FUNCTION dlp_risk(content string, entity_types array(string))
RETURNS string
LANGUAGE PYTHON AS $$
import json
import requests

BASE_URL = "{{BASE_URL}}"
API_KEY = "{{API_KEY}}"
MODEL = "{{MODEL}}"

KEYWORDS = ('password', 'secret', 'api key', 'apikey', 'token', 'credential',
            'customer', 'contact list', 'address book', 'export', 'confidential',
            'private key', 'salary', 'ssn', 'id card')

TAG_FOR = {'phone': 'PHONE', 'email': 'EMAIL', 'ip': 'IP', 'domain': 'DOMAIN',
           'person': 'PERSON', 'company': 'COMPANY', 'secret': 'CREDENTIAL'}

def _heuristic(ets, text):
    tags = sorted(set(TAG_FOR.get(t, t.upper()) for t in ets))
    score = min(1.0, 0.2 * len(tags))
    if any(k in text.lower() for k in KEYWORDS):
        score = min(1.0, score + 0.2)
    if 'secret' in ets:
        score = max(score, 0.8)
    if set(ets) & {'phone', 'email', 'person'}:
        tags.append('PII')
    return json.dumps({'score': round(score, 2), 'tags': sorted(set(tags))})

def _classify(text, ets):
    headers = {'Content-Type': 'application/json'}
    if API_KEY:
        headers['Authorization'] = 'Bearer ' + API_KEY
    prompt = ('You are a data-leakage classifier for AI-agent conversation logs. '
              'Respond with strict JSON only: {"score": <0..1>, "tags": [<zero or more of '
              'PII, PHONE, EMAIL, CREDENTIAL, SOURCE_CODE, CUSTOMER_DATA, FINANCIAL, EXFILTRATION>]}. '
              'Detected entity types: ' + ', '.join(ets) + '. Message: ' + text[:4000])
    resp = requests.post(BASE_URL + '/chat/completions', headers=headers, json={
        'model': MODEL,
        'messages': [{'role': 'user', 'content': prompt}],
        'temperature': 0,
        'max_tokens': 100,
    }, timeout=30)
    raw = resp.json()['choices'][0]['message']['content']
    parsed = json.loads(raw[raw.find('{'):raw.rfind('}') + 1])
    return json.dumps({'score': float(parsed.get('score', 0)),
                       'tags': [str(x) for x in parsed.get('tags', [])]})

def dlp_risk(contents, entity_types_list):
    out = []
    for i, text in enumerate(contents):
        text = text or ''
        ets = list(entity_types_list[i] or [])
        if not ets and not any(k in text.lower() for k in KEYWORDS):
            out.append('{"score":0,"tags":[]}')
            continue
        try:
            out.append(_classify(text, ets))
        except Exception:
            out.append(_heuristic(ets, text))
    return out
$$`

func DLPRiskUDFDDL(cfg config.DLPConfig) string {
	return strings.NewReplacer(
		"{{BASE_URL}}", cfg.BaseURL,
		"{{API_KEY}}", cfg.APIKey,
		"{{MODEL}}", cfg.RiskModel,
	).Replace(dlpRiskUDFTemplate)
}

// ---------- Materialized view ----------

// One enriched row per conversation message. Inner query maps CIM events to
// (role, content); middle query computes entities once and feeds them to
// dlp_risk; outer query projects final columns. Embedding gating (tool rows
// get []) happens inside dlp_embed via the role param.
const ddlDLPEnrichMV = `
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_dlp_enrich
INTO agentguard_dlp_enriched AS
SELECT
  event_time,
  to_string(city_hash64(session_id, run_id, to_string(event_time), role)) AS message_id,
  session_id,
  run_id,
  role,
  agent_type,
  agent_id,
  deployment_id,
  model,
  tool_name,
  content,
  entities,
  array_map(x -> replace_all(x, '"', ''), json_extract_array_raw(risk_json, 'tags')) AS risk_tags,
  json_extract_float(risk_json, 'score') AS risk_score,
  dlp_embed(content, role) AS embedding
FROM (
  SELECT
    *,
    dlp_extract_entities(content) AS entities,
    dlp_risk(content, map_keys(entities)) AS risk_json
  FROM (
    SELECT
      event_time, session_id, run_id, agent_type, agent_id, deployment_id,
      model, tool_name,
      multi_if(event_type = 'user_input', 'user',
               event_type = 'llm_response', 'assistant',
               'tool') AS role,
      multi_if(event_type = 'user_input', user_message,
               event_type = 'llm_response', assistant_message,
               tool_input) AS content
    FROM agentguard_cim_event
    WHERE event_type IN ('user_input', 'llm_response', 'tool_complete')
  )
  WHERE content != ''
)`

// ---------- Provisioning ----------

// InitDLP provisions the Semantic DLP ingest layer: enriched stream, vector
// index (best-effort), enrichment UDFs, and the enrichment MV. Idempotent.
// Called from Init() (setup wizard) and SyncDLP (POST /api/rules/sync).
func InitDLP(ctx context.Context, conn proton.Conn, dlpCfg config.DLPConfig, emit func(status, msg string)) error {
	if emit == nil {
		emit = func(status, msg string) { log.Printf("[provision:%s] %s", status, msg) }
	}
	if !dlpCfg.Enabled {
		emit("ok", "  - Semantic DLP disabled (dlp.enabled=false), skipping")
		return nil
	}

	emit("running", "$ Creating DLP enriched stream…")
	if err := conn.Exec(ctx, ddlDLPEnrichedStream); err != nil {
		emit("error", "  ✗ "+err.Error())
		return fmt.Errorf("dlp: creating enriched stream: %w", err)
	}
	emit("ok", "  ✓ agentguard_dlp_enriched")

	// Vector index: best-effort. Brute-force cosine_distance still works
	// without it, so failure is logged, not fatal.
	indexed := false
	for _, ddl := range dlpIndexDDLCandidates(dlpCfg.EmbeddingDim) {
		if err := conn.Exec(ctx, ddl); err == nil {
			indexed = true
			break
		} else {
			log.Printf("dlp: vector index attempt failed (continuing): %v", err)
		}
	}
	if indexed {
		emit("ok", "  ✓ HNSW vector index on embedding")
	} else {
		emit("ok", "  - vector index unavailable; using brute-force cosine_distance")
	}

	emit("running", "$ Creating DLP enrichment UDFs…")
	udfs := []struct{ name, ddl string }{
		{"dlp_extract_entities", ddlDLPEntitiesUDF},
		{"dlp_embed", DLPEmbedUDFDDL(dlpCfg)},
		{"dlp_risk", DLPRiskUDFDDL(dlpCfg)},
	}
	for _, u := range udfs {
		if err := conn.Exec(ctx, u.ddl); err != nil {
			emit("error", "  ✗ "+u.name+": "+err.Error())
			return fmt.Errorf("dlp: creating UDF %s: %w", u.name, err)
		}
		emit("ok", "  ✓ "+u.name)
	}

	emit("running", "$ Creating DLP enrichment materialized view…")
	if err := conn.Exec(ctx, ddlDLPEnrichMV); err != nil {
		emit("error", "  ✗ "+err.Error())
		return fmt.Errorf("dlp: creating enrichment MV: %w", err)
	}
	emit("ok", "  ✓ mv_dlp_enrich")
	return nil
}

// SyncDLP re-runs DLP provisioning over a fresh connection. Used by
// POST /api/rules/sync so config changes (endpoint, models) can be applied
// without re-running the setup wizard.
func SyncDLP(timeplusCfg config.TimeplusConfig, dlpCfg config.DLPConfig) error {
	if !dlpCfg.Enabled {
		return nil // nothing to do; avoid an unnecessary connection
	}
	conn, err := connectOnce(&timeplusCfg)
	if err != nil {
		return fmt.Errorf("dlp sync: connect: %w", err)
	}
	defer conn.Close()
	return InitDLP(context.Background(), conn, dlpCfg, nil)
}
