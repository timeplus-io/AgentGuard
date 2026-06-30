package timeplus

import (
	"context"
	"encoding/json"
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

// ---------- Config mutable stream ----------

// agentguard_dlp_config holds per-UDF configuration as key→JSON-value rows.
// It is a MUTABLE stream: an operator upserts a row (same primary key) to change
// DLP behavior at runtime. The ingest MV (via a streaming LEFT JOIN) and the
// query funnel (via a scalar sub-query) read the current value on every
// event/execution, so a config change takes effect with no UDF redeploy and no
// re-provision. See docs/dlp.md.
const ddlDLPConfigStream = `
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_dlp_config
(
  key        string,
  value      string,
  updated_at datetime64(3) DEFAULT now64(3),
  updated_by string
)
PRIMARY KEY (key)`

// Config keys. DLPCfgKeyShared holds the endpoint settings (base_url, api_key,
// timeout) common to every network UDF; the per-UDF rows override/add only what
// differs (model, max_tokens, catalog, …). Each network UDF reads BOTH its
// shared row and its own row and merges them (own wins). These strings double as
// the literal JOIN keys the ingest MV uses and the WHERE key='…' filter the
// funnel sub-queries use.
const (
	DLPCfgKeyShared     = "shared"
	DLPCfgKeyEntities   = "dlp_extract_entities"
	DLPCfgKeyEmbed      = "dlp_embed"
	DLPCfgKeyRisk       = "dlp_risk"
	DLPCfgKeyUnderstand = "dlp_understand_query"
	DLPCfgKeyRerank     = "dlp_rerank"
	DLPCfgKeySummarize  = "dlp_summarize"
)

// ---------- UDFs ----------
//
// Every DLP UDF takes a trailing `config` string (JSON) and parses ALL of its
// settings from it. The UDF bodies are therefore STATIC constants — no Go
// interpolation, and no API key baked into the source (so SHOW FUNCTIONS no
// longer leaks it). Config flows in at call time from agentguard_dlp_config.
// All UDFs fail open: a malformed/empty config or a model-endpoint outage
// yields empty/zero output, never an exception that would stall ingest.

// dlp_extract_entities: pure-Python regex/dictionary extraction (L0/L1), no
// network. The entity catalog comes from `config` (config.entities[]); patterns
// are compiled once per distinct config blob (per-batch cache). Secret values
// are masked before storage when store == 'masked'.
const ddlDLPEntitiesUDF = `CREATE OR REPLACE FUNCTION dlp_extract_entities(content string, config string)
RETURNS map(string, array(string))
LANGUAGE PYTHON AS $$
import re, json

_CACHE = {}

def _mask(v):
    if len(v) <= 8:
        return v[:2] + '***'
    return v[:4] + '***' + v[-4:]

def _compile(cfg_str):
    spec = _CACHE.get(cfg_str)
    if spec is not None:
        return spec
    try:
        cfg = json.loads(cfg_str) if cfg_str else {}
    except Exception:
        cfg = {}
    try:
        max_per = int(cfg.get('max_per_type', 20) or 20)
    except Exception:
        max_per = 20
    compiled = []
    for e in cfg.get('entities', []):
        try:
            t = e.get('type')
            if not t:
                continue
            store = e.get('store', 'raw')
            kind = e.get('kind', 'regex')
            if kind == 'dictionary':
                terms = [str(x) for x in e.get('terms', []) if str(x)]
                if not terms:
                    continue
                pat = re.compile(r'\b(?:' + '|'.join(re.escape(x) for x in terms) + r')\b', re.I)
            else:
                flags = re.I if 'i' in (e.get('flags') or '') else 0
                pat = re.compile(e.get('pattern', ''), flags)
            compiled.append((t, store, pat))
        except Exception:
            continue
    spec = (max_per, compiled)
    if len(_CACHE) < 32:
        _CACHE[cfg_str] = spec
    return spec

def dlp_extract_entities(contents, configs):
    out = []
    for i, text in enumerate(contents):
        ents = {}
        try:
            max_per, compiled = _compile(configs[i] or '')
            t = text or ''
            buckets, seen = {}, {}
            for typ, store, pat in compiled:
                for m in pat.findall(t):
                    raw = (m if isinstance(m, str) else (m[0] if m else '')).strip()
                    if not raw:
                        continue
                    s = seen.setdefault(typ, set())
                    if raw in s:
                        continue
                    s.add(raw)
                    buckets.setdefault(typ, []).append(_mask(raw) if store == 'masked' else raw)
            for typ, vals in buckets.items():
                if vals:
                    ents[typ] = vals[:max_per]
        except Exception:
            ents = {}
        out.append(ents)
    return out
$$`

// dlp_embed: batched embeddings via the OpenAI-compatible endpoint. Config is
// the merge of the shared row (base_url/api_key/timeout) and this UDF's row
// (model/max_chars). Only user/assistant rows with content are embedded (tool
// rows get []).
const ddlDLPEmbedUDF = `CREATE OR REPLACE FUNCTION dlp_embed(content string, role string, shared string, config string)
RETURNS array(float32)
LANGUAGE PYTHON AS $$
import json
import requests

def _merged(shareds, configs):
    def first(xs):
        for s in xs:
            if s:
                try:
                    return json.loads(s)
                except Exception:
                    return {}
        return {}
    m = first(shareds)
    m.update(first(configs))
    return m

def dlp_embed(contents, roles, shareds, configs):
    n = len(contents)
    out = [[] for _ in range(n)]
    cfg = _merged(shareds, configs)
    base = cfg.get('base_url') or ''
    api_key = cfg.get('api_key') or ''
    model = cfg.get('model') or ''
    try:
        timeout = float(cfg.get('timeout') or 50)
    except Exception:
        timeout = 50
    try:
        max_chars = int(cfg.get('max_chars') or 8000)
    except Exception:
        max_chars = 8000
    if not base:
        return out
    idx = []
    inputs = []
    for i in range(n):
        if roles[i] in ('user', 'assistant') and contents[i]:
            idx.append(i)
            inputs.append(contents[i][:max_chars])
    if not inputs:
        return out
    try:
        headers = {'Content-Type': 'application/json'}
        if api_key:
            headers['Authorization'] = 'Bearer ' + api_key
        resp = requests.post(base + '/embeddings', headers=headers,
                             json={'model': model, 'input': inputs}, timeout=timeout)
        for j, item in enumerate(resp.json().get('data', [])):
            if j < len(idx):
                out[idx[j]] = [float(x) for x in item.get('embedding', [])]
    except Exception:
        pass
    return out
$$`

// dlp_risk: gated risk classification. Keywords + tag_for + the model endpoint
// all come from `config`. Falls back to a deterministic heuristic when the
// endpoint is unset or errors.
const ddlDLPRiskUDF = `CREATE OR REPLACE FUNCTION dlp_risk(content string, entity_types array(string), shared string, config string)
RETURNS string
LANGUAGE PYTHON AS $$
import json
import requests

DEFAULT_KEYWORDS = ('password', 'secret', 'api key', 'apikey', 'token', 'credential',
            'customer', 'contact list', 'address book', 'export', 'confidential',
            'private key', 'salary', 'ssn', 'id card')
DEFAULT_TAG_FOR = {'phone': 'PHONE', 'email': 'EMAIL', 'ip': 'IP', 'domain': 'DOMAIN',
           'person': 'PERSON', 'company': 'COMPANY', 'secret': 'CREDENTIAL'}

def _merged(shareds, configs):
    def first(xs):
        for s in xs:
            if s:
                try:
                    return json.loads(s)
                except Exception:
                    return {}
        return {}
    m = first(shareds)
    m.update(first(configs))
    return m

def _msg_content(resp):
    m = resp.json()['choices'][0]['message']
    return (m.get('content') or '').strip() or \
           (m.get('reasoning') or m.get('reasoning_content') or '').strip()

def _heuristic(ets, text, tag_for, keywords):
    tags = sorted(set(tag_for.get(t, t.upper()) for t in ets))
    score = min(1.0, 0.2 * len(tags))
    if any(k in text.lower() for k in keywords):
        score = min(1.0, score + 0.2)
    if 'secret' in ets:
        score = max(score, 0.8)
    if set(ets) & {'phone', 'email', 'person'}:
        tags.append('PII')
    return json.dumps({'score': round(score, 2), 'tags': sorted(set(tags))})

def _classify(text, ets, base, api_key, model, max_tokens, timeout):
    headers = {'Content-Type': 'application/json'}
    if api_key:
        headers['Authorization'] = 'Bearer ' + api_key
    prompt = ('You are a data-leakage classifier for AI-agent conversation logs. '
              'Respond with strict JSON only: {"score": <0..1>, "tags": [<zero or more of '
              'PII, PHONE, EMAIL, CREDENTIAL, SOURCE_CODE, CUSTOMER_DATA, FINANCIAL, EXFILTRATION>]}. '
              'Detected entity types: ' + ', '.join(ets) + '. Message: ' + text[:4000])
    resp = requests.post(base + '/chat/completions', headers=headers, json={
        'model': model,
        'messages': [{'role': 'user', 'content': prompt}],
        'temperature': 0,
        'max_tokens': max_tokens,
    }, timeout=timeout)
    raw = _msg_content(resp)
    parsed = json.loads(raw[raw.find('{'):raw.rfind('}') + 1])
    return json.dumps({'score': float(parsed.get('score', 0)),
                       'tags': [str(x) for x in parsed.get('tags', [])]})

def dlp_risk(contents, entity_types_list, shareds, configs):
    cfg = _merged(shareds, configs)
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
    keywords = tuple(cfg.get('keywords') or DEFAULT_KEYWORDS)
    tag_for = cfg.get('tag_for') or DEFAULT_TAG_FOR
    out = []
    for i, text in enumerate(contents):
        text = text or ''
        ets = list(entity_types_list[i] or [])
        if not ets and not any(k in text.lower() for k in keywords):
            out.append('{"score":0,"tags":[]}')
            continue
        try:
            if not base:
                raise Exception('no endpoint')
            out.append(_classify(text, ets, base, api_key, model, max_tokens, timeout))
        except Exception:
            out.append(_heuristic(ets, text, tag_for, keywords))
    return out
$$`

// ---------- Config rendering & seeding ----------

// dlpEntitySpec is one entry in the entity catalog (the dlp_extract_entities
// config). regex entries use Pattern (+ optional Flags, e.g. "i"); dictionary
// entries use Terms. Store is raw|masked; Tag feeds dlp_risk's tag_for.
type dlpEntitySpec struct {
	Type    string   `json:"type"`
	Kind    string   `json:"kind"` // regex | dictionary
	Pattern string   `json:"pattern,omitempty"`
	Terms   []string `json:"terms,omitempty"`
	Flags   string   `json:"flags,omitempty"`
	Store   string   `json:"store"` // raw | masked
	Tag     string   `json:"tag,omitempty"`
}

// defaultDLPEntities returns the builtin catalog, preserving exactly the
// patterns and masking behavior of the pre-config hardcoded extractor
// (phone, email, ip, domain, secret).
func defaultDLPEntities() []dlpEntitySpec {
	return []dlpEntitySpec{
		{Type: "phone", Kind: "regex", Store: "raw", Tag: "PHONE", Pattern: `(?<![\dA-Za-z])(?:\+?\d{1,3}[-. ]?)?(?:\(\d{2,4}\)[-. ]?)?\d{3,4}[-. ]\d{4}(?![\dA-Za-z])`},
		{Type: "email", Kind: "regex", Store: "raw", Tag: "EMAIL", Pattern: `[A-Za-z0-9._+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`},
		{Type: "ip", Kind: "regex", Store: "raw", Tag: "IP", Pattern: `(?<!\d)(?:\d{1,3}\.){3}\d{1,3}(?!\d)`},
		{Type: "domain", Kind: "regex", Store: "raw", Tag: "DOMAIN", Flags: "i", Pattern: `\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+(?:com|net|org|io|ai|dev|app|co)\b`},
		{Type: "secret", Kind: "regex", Store: "masked", Tag: "CREDENTIAL", Pattern: `AKIA[0-9A-Z]{16}`},
		{Type: "secret", Kind: "regex", Store: "masked", Tag: "CREDENTIAL", Pattern: `sk-[A-Za-z0-9_-]{20,}`},
		{Type: "secret", Kind: "regex", Store: "masked", Tag: "CREDENTIAL", Pattern: `ghp_[A-Za-z0-9]{36}`},
		{Type: "secret", Kind: "regex", Store: "masked", Tag: "CREDENTIAL", Pattern: `gho_[A-Za-z0-9]{36}`},
		{Type: "secret", Kind: "regex", Store: "masked", Tag: "CREDENTIAL", Pattern: `xox[baprs]-[A-Za-z0-9-]{10,}`},
		{Type: "secret", Kind: "regex", Store: "masked", Tag: "CREDENTIAL", Pattern: `-----BEGIN [A-Z ]*PRIVATE KEY-----`},
		{Type: "secret", Kind: "regex", Store: "masked", Tag: "CREDENTIAL", Pattern: `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`},
	}
}

// dlpDefaultRiskKeywords gates dlp_risk: a message with no detected entities and
// none of these keywords skips the (costly) LLM call.
var dlpDefaultRiskKeywords = []string{
	"password", "secret", "api key", "apikey", "token", "credential",
	"customer", "contact list", "address book", "export", "confidential",
	"private key", "salary", "ssn", "id card",
}

// dlpTagForFromCatalog derives dlp_risk's tag_for map from the catalog (each
// type's Tag, defaulting to TYPE.upper()), merged over the builtin semantic
// types that have no extractor (person/company).
func dlpTagForFromCatalog(cat []dlpEntitySpec) map[string]string {
	tagFor := map[string]string{"person": "PERSON", "company": "COMPANY"}
	for _, e := range cat {
		if e.Type == "" {
			continue
		}
		tag := e.Tag
		if tag == "" {
			tag = strings.ToUpper(e.Type)
		}
		tagFor[e.Type] = tag
	}
	return tagFor
}

// DLPConfigRows renders the default per-UDF config rows (key → JSON value) from
// cfg. This is the source of truth seeded into agentguard_dlp_config on first
// provision; operators may then override any row at runtime. Defaults preserve
// the pre-config behavior exactly.
func DLPConfigRows(cfg config.DLPConfig) map[string]string {
	catalog := defaultDLPEntities()
	summarizeTokens := cfg.MaxOutputTokens
	if summarizeTokens < 800 {
		summarizeTokens = 800
	}
	// Per-UDF chat rows carry only what differs from the shared row.
	chat := func(model string, maxTokens int) map[string]any {
		return map[string]any{"model": model, "max_tokens": maxTokens}
	}
	riskRow := chat(cfg.RiskModel, cfg.MaxOutputTokens)
	riskRow["keywords"] = dlpDefaultRiskKeywords
	riskRow["tag_for"] = dlpTagForFromCatalog(catalog)

	rows := map[string]any{
		// Endpoint settings shared by every network UDF; rotate the key or swap
		// the endpoint here once instead of per UDF.
		DLPCfgKeyShared: map[string]any{
			"base_url": cfg.BaseURL,
			"api_key":  cfg.APIKey,
			"timeout":  cfg.RequestTimeoutSeconds,
		},
		DLPCfgKeyEntities: map[string]any{
			"max_per_type": 20,
			"entities":     catalog,
		},
		DLPCfgKeyEmbed: map[string]any{
			"model":     cfg.EmbeddingModel,
			"max_chars": 8000,
		},
		DLPCfgKeyRisk:       riskRow,
		DLPCfgKeyUnderstand: chat(cfg.RiskModel, cfg.MaxOutputTokens),
		DLPCfgKeyRerank:     chat(cfg.RiskModel, cfg.MaxOutputTokens),
		DLPCfgKeySummarize:  chat(cfg.SummaryModel, summarizeTokens),
	}
	out := make(map[string]string, len(rows))
	for k, v := range rows {
		b, _ := json.Marshal(v)
		out[k] = string(b)
	}
	return out
}

// seedDLPConfig inserts a default config row for any key not already present
// (seed-if-absent), so a re-provision or POST /api/rules/sync never clobbers
// runtime operator edits.
func seedDLPConfig(ctx context.Context, conn proton.Conn, dlpCfg config.DLPConfig, emit func(status, msg string)) error {
	rows := DLPConfigRows(dlpCfg)

	existing := map[string]bool{}
	rs, err := conn.Query(ctx, "SELECT key FROM table(agentguard_dlp_config)")
	if err != nil {
		return fmt.Errorf("dlp: reading existing config keys: %w", err)
	}
	for rs.Next() {
		var k string
		if err := rs.Scan(&k); err != nil {
			_ = rs.Close()
			return fmt.Errorf("dlp: scanning config key: %w", err)
		}
		existing[k] = true
	}
	_ = rs.Close()

	var missing []string
	for k := range rows {
		if !existing[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) == 0 {
		emit("ok", "  - DLP config already seeded; preserving runtime edits")
		return nil
	}

	batch, err := conn.PrepareBatch(ctx, "INSERT INTO agentguard_dlp_config (key, value, updated_by)")
	if err != nil {
		return fmt.Errorf("dlp: prepare config seed: %w", err)
	}
	defer func() { _ = batch.Abort() }()
	for _, k := range missing {
		if err := batch.Append(k, rows[k], "system"); err != nil {
			return fmt.Errorf("dlp: append config %s: %w", k, err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("dlp: send config seed: %w", err)
	}
	emit("ok", fmt.Sprintf("  ✓ seeded %d DLP config rows", len(missing)))
	return nil
}

// ---------- Materialized view ----------

// One enriched row per conversation message. Config for each ingest UDF is read
// live from agentguard_dlp_config via streaming LEFT JOINs on literal keys, so a
// config upsert takes effect on the next event with no MV restart. LEFT JOIN
// (with coalesce) guarantees an event is never dropped when a config row is
// missing — the UDF just fails open. Layers: base (CIM → role/content/keys) →
// join+filter (attach cfg_* columns) → enrich (entities, risk) → final (project
// + embed).
const ddlDLPEnrichMV = `
CREATE MATERIALIZED VIEW IF NOT EXISTS mv_dlp_enrich
INTO agentguard_dlp_enriched AS
SELECT
  event_time,
  message_id,
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
  dlp_embed(content, role, cfg_shared, cfg_embed) AS embedding
FROM (
  SELECT
    *,
    dlp_extract_entities(content, cfg_entities) AS entities,
    dlp_risk(content, map_keys(entities), cfg_shared, cfg_risk) AS risk_json
  FROM (
    SELECT
      base.event_time AS event_time,
      base.message_id AS message_id,
      base.session_id AS session_id,
      base.run_id AS run_id,
      base.role AS role,
      base.agent_type AS agent_type,
      base.agent_id AS agent_id,
      base.deployment_id AS deployment_id,
      base.model AS model,
      base.tool_name AS tool_name,
      base.content AS content,
      coalesce(cs.value, '') AS cfg_shared,
      coalesce(ce.value, '') AS cfg_entities,
      coalesce(cr.value, '') AS cfg_risk,
      coalesce(cm.value, '') AS cfg_embed
    FROM (
      SELECT
        event_time,
        to_string(city_hash64(session_id, run_id, to_string(event_time), role)) AS message_id,
        session_id, run_id, agent_type, agent_id, deployment_id,
        model, tool_name,
        multi_if(event_type = 'user_input', 'user',
                 event_type = 'llm_response', 'assistant',
                 'tool') AS role,
        multi_if(event_type = 'user_input', user_message,
                 event_type = 'llm_response', assistant_message,
                 tool_input) AS content,
        'shared' AS k_shared,
        'dlp_extract_entities' AS k_ent,
        'dlp_risk' AS k_risk,
        'dlp_embed' AS k_emb
      FROM agentguard_cim_event
      WHERE event_type IN ('user_input', 'llm_response', 'tool_complete')
    ) AS base
    LEFT JOIN agentguard_dlp_config AS cs ON base.k_shared = cs.key
    LEFT JOIN agentguard_dlp_config AS ce ON base.k_ent = ce.key
    LEFT JOIN agentguard_dlp_config AS cr ON base.k_risk = cr.key
    LEFT JOIN agentguard_dlp_config AS cm ON base.k_emb = cm.key
    WHERE base.content != ''
  )
)`

// ---------- Provisioning ----------

// InitDLP provisions the Semantic DLP ingest layer: enriched stream, vector
// index (best-effort), config mutable stream (seeded), enrichment UDFs, and the
// enrichment MV. Idempotent. Called from Init() (setup wizard) and SyncDLP
// (POST /api/rules/sync).
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

	// Drop the enrichment MV up front, before touching the index or the UDFs it
	// depends on. UDF signatures changed when config moved to the stream, and a
	// dependent MV can block a CREATE OR REPLACE FUNCTION (or fail an ALTER STREAM
	// that validates dependents); dropping first also forces the MV to be rebuilt
	// with the current config-JOIN definition rather than kept stale by
	// CREATE … IF NOT EXISTS. The target stream (agentguard_dlp_enriched) is
	// untouched, so already-enriched rows persist.
	if err := conn.Exec(ctx, "DROP VIEW IF EXISTS mv_dlp_enrich"); err != nil {
		emit("error", "  ✗ "+err.Error())
		return fmt.Errorf("dlp: dropping enrichment MV: %w", err)
	}

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

	emit("running", "$ Creating DLP config stream…")
	if err := conn.Exec(ctx, ddlDLPConfigStream); err != nil {
		emit("error", "  ✗ "+err.Error())
		return fmt.Errorf("dlp: creating config stream: %w", err)
	}
	emit("ok", "  ✓ agentguard_dlp_config")
	if err := seedDLPConfig(ctx, conn, dlpCfg, emit); err != nil {
		emit("error", "  ✗ "+err.Error())
		return fmt.Errorf("dlp: seeding config: %w", err)
	}

	emit("running", "$ Creating DLP enrichment UDFs…")
	udfs := []struct{ name, ddl string }{
		{"dlp_extract_entities", ddlDLPEntitiesUDF},
		{"dlp_embed", ddlDLPEmbedUDF},
		{"dlp_risk", ddlDLPRiskUDF},
		{"dlp_understand_query", ddlDLPUnderstandUDF},
		{"dlp_rerank", ddlDLPRerankUDF},
		{"dlp_summarize", ddlDLPSummarizeUDF},
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
