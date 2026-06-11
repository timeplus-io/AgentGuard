# Semantic DLP 1/4: Ingest Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provision the Semantic DLP ingest layer — `agentguard_dlp_enriched` stream, three fail-open Python enrichment UDFs, the `mv_dlp_enrich` materialized view, the `dlp` config section, and `GET /api/dlp/status` (GitHub issue #171, spec `docs/superpowers/specs/2026-06-11-semantic-dlp-design.md` §2/§3/§5/§6).

**Architecture:** All enrichment runs inside Timeplus Enterprise: a single MV reads `agentguard_cim_event`, calls `dlp_extract_entities` (pure-Python regex), `dlp_risk` (gated chat-completion callout), and `dlp_embed` (batched embeddings callout), and writes one row per conversation message. Go code only generates DDL (with config values interpolated, like the existing `notify_agentguard_udf`) and reports status. UDFs never raise — endpoint failures degrade to empty embedding / heuristic risk.

**Tech Stack:** Go 1.x (Gin, viper, proton-go-driver), Timeplus Enterprise Python UDFs (embedded Python 3.10 + `requests`), streaming SQL MVs.

**Branch:** work on `feature/159-semantic-dlp-natural-language-forensic-auditing-of-agent-conversation-logs` (already checked out) or a child branch; PR closes #171.

**Build/test commands (macOS 26+ requires CGO_ENABLED=0):**
- Compile: `cd backend && CGO_ENABLED=0 go build ./...`
- All tests: `cd backend && CGO_ENABLED=0 go test ./...`
- One package: `CGO_ENABLED=0 go test ./internal/config/... -run TestDLP -v`

---

## File map

| File | Action | Responsibility |
|------|--------|----------------|
| `backend/internal/config/config.go` | Modify | `DLPConfig` struct, defaults, env bindings, `Save` round-trip |
| `backend/internal/config/config_dlp_test.go` | Create | Config defaults / file / env tests |
| `backend/internal/timeplus/dlp.go` | Create | All DLP DDL (stream, index, 3 UDFs, MV) + `InitDLP` + `SyncDLP` |
| `backend/internal/timeplus/dlp_test.go` | Create | DDL-builder unit tests (interpolation correctness) |
| `backend/internal/timeplus/init.go` | Modify | Call `InitDLP` from `Init()`; signature gains `dlpCfg` |
| `backend/internal/handlers/setup.go` | Modify | Pass `cfg.DLP` to `tp.Init` |
| `backend/internal/handlers/rules.go` | Modify | `SyncMVs` also re-syncs DLP (best-effort) |
| `backend/internal/models/models.go` | Modify | `DLPStatus` response struct |
| `backend/internal/handlers/dlp.go` | Create | `DLPStatusHandler` |
| `backend/internal/handlers/dlp_test.go` | Create | Status handler test against `httptest` mock Timeplus |
| `backend/cmd/server/main.go` | Modify | Register `GET /api/dlp/status` |
| `backend/scripts/dlp_mock_model.py` | Create | Stdlib-only mock OpenAI-compatible endpoint for verification |

---

### Task 1: `dlp` config section

**Files:**
- Modify: `backend/internal/config/config.go`
- Create: `backend/internal/config/config_dlp_test.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/config/config_dlp_test.go` (package `config_test` — the `isolate(t)` helper already exists in `config_loadfile_test.go`):

```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

func TestDLPDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.DLP.Enabled {
		t.Error("DLP.Enabled default should be false (opt-in feature)")
	}
	if cfg.DLP.BaseURL != "http://localhost:4000/v1" {
		t.Errorf("BaseURL default = %q", cfg.DLP.BaseURL)
	}
	if cfg.DLP.EmbeddingModel != "text-embedding-3-small" {
		t.Errorf("EmbeddingModel default = %q", cfg.DLP.EmbeddingModel)
	}
	if cfg.DLP.EmbeddingDim != 1536 {
		t.Errorf("EmbeddingDim default = %d", cfg.DLP.EmbeddingDim)
	}
	if cfg.DLP.RiskModel != "gpt-4o-mini" {
		t.Errorf("RiskModel default = %q", cfg.DLP.RiskModel)
	}
	if cfg.DLP.SummaryModel != "gpt-4o" {
		t.Errorf("SummaryModel default = %q", cfg.DLP.SummaryModel)
	}
}

func TestDLPFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
dlp:
  enabled: true
  base_url: http://ollama:11434/v1
  embedding_model: nomic-embed-text
  embedding_dim: 768
  risk_model: qwen2.5:3b
  summary_model: llama3:70b
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !cfg.DLP.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.DLP.BaseURL != "http://ollama:11434/v1" {
		t.Errorf("BaseURL = %q", cfg.DLP.BaseURL)
	}
	if cfg.DLP.EmbeddingDim != 768 {
		t.Errorf("EmbeddingDim = %d", cfg.DLP.EmbeddingDim)
	}
	if cfg.DLP.RiskModel != "qwen2.5:3b" || cfg.DLP.SummaryModel != "llama3:70b" {
		t.Errorf("models = %q / %q", cfg.DLP.RiskModel, cfg.DLP.SummaryModel)
	}
}

func TestDLPAPIKeyFromEnv(t *testing.T) {
	isolate(t)
	t.Setenv("DLP_API_KEY", "sk-test-123")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DLP.APIKey != "sk-test-123" {
		t.Errorf("APIKey = %q, want sk-test-123", cfg.DLP.APIKey)
	}
}

func TestDLPSaveRoundTrip(t *testing.T) {
	// Provision rewrites config.yaml via Save; the dlp section must survive
	// (except api_key, which is never written).
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := config.LoadFrom(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DLP.Enabled = true
	cfg.DLP.BaseURL = "http://litellm:4000/v1"
	cfg.DLP.APIKey = "sk-never-saved"
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !reloaded.DLP.Enabled || reloaded.DLP.BaseURL != "http://litellm:4000/v1" {
		t.Errorf("dlp section lost on Save round-trip: %+v", reloaded.DLP)
	}
	if reloaded.DLP.APIKey != "" {
		t.Error("api_key must never be written to config.yaml")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) == "" || os.IsNotExist(err) {
		t.Fatal("config file not written")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/... -run TestDLP -v`
Expected: compile error — `cfg.DLP undefined (type *config.Config has no field or method DLP)`

- [ ] **Step 3: Implement**

In `backend/internal/config/config.go`:

Add the struct after `HoldsConfig`:

```go
type DLPConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	BaseURL        string `mapstructure:"base_url"`
	APIKey         string `mapstructure:"api_key"`
	EmbeddingModel string `mapstructure:"embedding_model"`
	EmbeddingDim   int    `mapstructure:"embedding_dim"`
	RiskModel      string `mapstructure:"risk_model"`
	SummaryModel   string `mapstructure:"summary_model"`
}
```

Add the field to `Config` (after `Holds`):

```go
	DLP DLPConfig `mapstructure:"dlp"`
```

In `load()`, add defaults next to the holds defaults:

```go
	v.SetDefault("dlp.enabled", false)
	v.SetDefault("dlp.base_url", "http://localhost:4000/v1")
	v.SetDefault("dlp.api_key", "")
	v.SetDefault("dlp.embedding_model", "text-embedding-3-small")
	v.SetDefault("dlp.embedding_dim", 1536)
	v.SetDefault("dlp.risk_model", "gpt-4o-mini")
	v.SetDefault("dlp.summary_model", "gpt-4o")
```

In `load()`, read into the struct next to the holds reads:

```go
	cfg.DLP.Enabled = v.GetBool("dlp.enabled")
	cfg.DLP.BaseURL = v.GetString("dlp.base_url")
	cfg.DLP.APIKey = v.GetString("dlp.api_key")
	cfg.DLP.EmbeddingModel = v.GetString("dlp.embedding_model")
	cfg.DLP.EmbeddingDim = v.GetInt("dlp.embedding_dim")
	cfg.DLP.RiskModel = v.GetString("dlp.risk_model")
	cfg.DLP.SummaryModel = v.GetString("dlp.summary_model")
```

In `bindEnv()`:

```go
	_ = v.BindEnv("dlp.enabled", "DLP_ENABLED")
	_ = v.BindEnv("dlp.base_url", "DLP_BASE_URL")
	_ = v.BindEnv("dlp.api_key", "DLP_API_KEY")
	_ = v.BindEnv("dlp.embedding_model", "DLP_EMBEDDING_MODEL")
	_ = v.BindEnv("dlp.embedding_dim", "DLP_EMBEDDING_DIM")
	_ = v.BindEnv("dlp.risk_model", "DLP_RISK_MODEL")
	_ = v.BindEnv("dlp.summary_model", "DLP_SUMMARY_MODEL")
```

In `Save()` (api_key deliberately excluded — same policy as Timeplus credentials):

```go
	v.Set("dlp.enabled", cfg.DLP.Enabled)
	v.Set("dlp.base_url", cfg.DLP.BaseURL)
	v.Set("dlp.embedding_model", cfg.DLP.EmbeddingModel)
	v.Set("dlp.embedding_dim", cfg.DLP.EmbeddingDim)
	v.Set("dlp.risk_model", cfg.DLP.RiskModel)
	v.Set("dlp.summary_model", cfg.DLP.SummaryModel)
```

Also add `DLP_*` keys to the env-var list inside `isolate()` in `config_loadfile_test.go` so tests stay hermetic:

```go
		"DLP_ENABLED", "DLP_BASE_URL", "DLP_API_KEY", "DLP_EMBEDDING_MODEL",
		"DLP_EMBEDDING_DIM", "DLP_RISK_MODEL", "DLP_SUMMARY_MODEL",
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/... -v`
Expected: all PASS (including pre-existing config tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config/
git commit -m "feat(dlp): add dlp config section (endpoint, models, embedding dim)"
```

---

### Task 2: DDL builders in `timeplus/dlp.go`

All DDL lives in one new file as constants + small builder functions so interpolation is unit-testable without a live server.

**Files:**
- Create: `backend/internal/timeplus/dlp.go`
- Create: `backend/internal/timeplus/dlp_test.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/timeplus/dlp_test.go`:

```go
package timeplus

import (
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

func testDLPCfg() config.DLPConfig {
	return config.DLPConfig{
		Enabled:        true,
		BaseURL:        "http://mock:9999/v1",
		APIKey:         "sk-test-abc",
		EmbeddingModel: "text-embedding-3-small",
		EmbeddingDim:   1536,
		RiskModel:      "gpt-4o-mini",
		SummaryModel:   "gpt-4o",
	}
}

// assertCleanDDL fails on leftover fmt verbs or failed interpolation markers.
func assertCleanDDL(t *testing.T, name, ddl string) {
	t.Helper()
	if strings.Contains(ddl, "%!") {
		t.Errorf("%s: fmt interpolation error: %s", name, ddl)
	}
	if strings.Contains(ddl, "%s") || strings.Contains(ddl, "%d") {
		t.Errorf("%s: leftover fmt verb in DDL", name)
	}
}

func TestDLPEmbedUDFDDL(t *testing.T) {
	ddl := DLPEmbedUDFDDL(testDLPCfg())
	assertCleanDDL(t, "embed", ddl)
	for _, want := range []string{
		"CREATE OR REPLACE FUNCTION dlp_embed",
		"RETURNS array(float32)",
		"LANGUAGE PYTHON",
		`BASE_URL = "http://mock:9999/v1"`,
		`API_KEY = "sk-test-abc"`,
		`MODEL = "text-embedding-3-small"`,
		"/embeddings",
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("embed DDL missing %q", want)
		}
	}
}

func TestDLPRiskUDFDDL(t *testing.T) {
	ddl := DLPRiskUDFDDL(testDLPCfg())
	assertCleanDDL(t, "risk", ddl)
	for _, want := range []string{
		"CREATE OR REPLACE FUNCTION dlp_risk",
		"entity_types array(string)",
		"RETURNS string",
		`MODEL = "gpt-4o-mini"`,
		"/chat/completions",
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("risk DDL missing %q", want)
		}
	}
}

func TestDLPEntitiesUDFDDLIsStatic(t *testing.T) {
	if !strings.Contains(ddlDLPEntitiesUDF, "CREATE OR REPLACE FUNCTION dlp_extract_entities") {
		t.Error("entities UDF DDL malformed")
	}
	if !strings.Contains(ddlDLPEntitiesUDF, "map(string, array(string))") {
		t.Error("entities UDF must return map(string, array(string))")
	}
}

func TestDLPIndexDDLCandidates(t *testing.T) {
	cands := dlpIndexDDLCandidates(1536)
	if len(cands) == 0 {
		t.Fatal("no index DDL candidates")
	}
	for _, ddl := range cands {
		assertCleanDDL(t, "index", ddl)
		if !strings.Contains(ddl, "1536") {
			t.Errorf("index DDL missing dim: %s", ddl)
		}
	}
}

func TestDLPMVReferencesAllUDFs(t *testing.T) {
	for _, want := range []string{
		"CREATE MATERIALIZED VIEW IF NOT EXISTS mv_dlp_enrich",
		"INTO agentguard_dlp_enriched",
		"dlp_extract_entities(content)",
		"dlp_risk(content, map_keys(entities))",
		"dlp_embed(content, role)",
		"'user_input'", "'llm_response'", "'tool_complete'",
	} {
		if !strings.Contains(ddlDLPEnrichMV, want) {
			t.Errorf("MV DDL missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/... -run TestDLP -v`
Expected: compile error — `undefined: DLPEmbedUDFDDL` etc.

- [ ] **Step 3: Implement the DDL builders**

Create `backend/internal/timeplus/dlp.go`. Notes baked into this code:
- Python UDF inputs/outputs are **batched lists**; `dlp_embed` makes one API call per batch.
- Embedding gating (skip `tool` rows) lives **inside** `dlp_embed` via the `role` param, because SQL `if()` evaluates both branches.
- `dlp_risk` takes `map_keys(entities)` as `array(string)` (avoids depending on map-typed UDF params); only flagged/keyword rows hit the API, others short-circuit to `{"score":0,"tags":[]}`.
- All UDF bodies are `%%`-free so `fmt.Sprintf` is safe; person/company dictionaries ship empty in Phase 1 (org-specific dictionaries are a later enhancement — keys simply absent from the map).

```go
package timeplus

import (
	"context"
	"fmt"
	"log"

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
  risk_tags      array(string),
  risk_score     float64,
  embedding      array(float32)
)`

// dlpIndexDDLCandidates returns vector-index DDL variants to try in order.
// HNSW index support/syntax varies across Timeplus Enterprise versions; if
// every candidate fails, queries fall back to brute-force cosine_distance,
// which is acceptable at MVP scale (spec §2.1).
func dlpIndexDDLCandidates(dim int) []string {
	return []string{
		fmt.Sprintf(`ALTER STREAM agentguard_dlp_enriched ADD INDEX idx_dlp_embedding embedding TYPE vector_similarity('hnsw', 'cosineDistance', %d)`, dim),
		fmt.Sprintf(`ALTER STREAM agentguard_dlp_enriched ADD INDEX idx_dlp_embedding embedding TYPE vector_similarity('hnsw', 'cosine_distance', %d)`, dim),
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

// dlpEmbedUDFTemplate interpolation order: BaseURL, APIKey, EmbeddingModel.
const dlpEmbedUDFTemplate = `CREATE OR REPLACE FUNCTION dlp_embed(content string, role string)
RETURNS array(float32)
LANGUAGE PYTHON AS $$
import requests

BASE_URL = "%s"
API_KEY = "%s"
MODEL = "%s"
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
	return fmt.Sprintf(dlpEmbedUDFTemplate, cfg.BaseURL, cfg.APIKey, cfg.EmbeddingModel)
}

// dlpRiskUDFTemplate interpolation order: BaseURL, APIKey, RiskModel.
const dlpRiskUDFTemplate = `CREATE OR REPLACE FUNCTION dlp_risk(content string, entity_types array(string))
RETURNS string
LANGUAGE PYTHON AS $$
import json
import requests

BASE_URL = "%s"
API_KEY = "%s"
MODEL = "%s"

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
	return fmt.Sprintf(dlpRiskUDFTemplate, cfg.BaseURL, cfg.APIKey, cfg.RiskModel)
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
	for name, ddl := range map[string]string{
		"dlp_extract_entities": ddlDLPEntitiesUDF,
		"dlp_embed":            DLPEmbedUDFDDL(dlpCfg),
		"dlp_risk":             DLPRiskUDFDDL(dlpCfg),
	} {
		if err := conn.Exec(ctx, ddl); err != nil {
			emit("error", "  ✗ "+name+": "+err.Error())
			return fmt.Errorf("dlp: creating UDF %s: %w", name, err)
		}
		emit("ok", "  ✓ "+name)
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
	conn, err := connectOnce(&timeplusCfg)
	if err != nil {
		return fmt.Errorf("dlp sync: connect: %w", err)
	}
	defer conn.Close()
	return InitDLP(context.Background(), conn, dlpCfg, nil)
}
```

> **Map-return fallback (verify in Task 6):** if `RETURNS map(string, array(string))`
> is rejected by the live server, change `dlp_extract_entities` to
> `RETURNS string` returning `json.dumps(ents)` and in the MV replace
> `dlp_extract_entities(content) AS entities` with
> `cast(dlp_extract_entities(content), 'map(string, array(string))') AS entities`
> (and update the DDL unit test accordingly).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/... -run TestDLP -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/timeplus/dlp.go backend/internal/timeplus/dlp_test.go
git commit -m "feat(dlp): enriched stream, enrichment UDFs, and MV DDL builders"
```

---

### Task 3: Wire `InitDLP` into provisioning and sync

**Files:**
- Modify: `backend/internal/timeplus/init.go` (Init signature + call)
- Modify: `backend/internal/handlers/setup.go` (pass `cfg.DLP`)
- Modify: `backend/internal/handlers/rules.go` (`SyncMVs` best-effort DLP sync)

- [ ] **Step 1: Change `Init` signature**

In `backend/internal/timeplus/init.go`, change:

```go
func Init(timeplusCfg config.TimeplusConfig, agentGuardURL string, catalogEntries []catalog.Entry, emit func(status, msg string)) error {
```

to:

```go
func Init(timeplusCfg config.TimeplusConfig, agentGuardURL string, catalogEntries []catalog.Entry, dlpCfg config.DLPConfig, emit func(status, msg string)) error {
```

and add immediately before the final `emit("ok", "  ✓ All resources provisioned")`:

```go
	emit("running", "$ Provisioning Semantic DLP…")
	if err := InitDLP(ctx, conn, dlpCfg, emit); err != nil {
		log.Printf("timeplus init: warning provisioning DLP: %v (continuing)", err)
	}
```

(Warning-not-fatal, matching how CIM metrics / threats / pricing failures are handled.)

- [ ] **Step 2: Update all `Init` callers**

Run: `grep -rn "tp.Init(\|timeplus.Init(" backend/ --include="*.go"`
Expected: only `backend/internal/handlers/setup.go:109`. Update it:

```go
		if err := tp.Init(timeplusCfg, agentGuardURL, mgr.All(), cfg.DLP, sendEvent); err != nil {
```

If grep finds other callers (tests), update them the same way.

- [ ] **Step 3: Add DLP sync to `SyncMVs`**

In `backend/internal/handlers/rules.go`, inside `SyncMVs` immediately before the final success `c.JSON(...)` response, add:

```go
		// Best-effort DLP re-provision so endpoint/model config changes apply
		// without the setup wizard. Failure must not fail rule-MV sync.
		if err := tp.SyncDLP(cfg.Timeplus, cfg.DLP); err != nil {
			log.Printf("rules: SyncMVs DLP sync: %v", err)
		}
```

(`cfg` here is already the request-scoped config from `auth.RequestCfg(c, cfg)`; `cfg.DLP` is copied from the base config.)

- [ ] **Step 4: Compile and run the full test suite**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...`
Expected: build OK, all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/timeplus/init.go backend/internal/handlers/setup.go backend/internal/handlers/rules.go
git commit -m "feat(dlp): provision DLP layer from setup wizard and rules sync"
```

---

### Task 4: `GET /api/dlp/status`

**Files:**
- Modify: `backend/internal/models/models.go` (add `DLPStatus`)
- Create: `backend/internal/handlers/dlp.go`
- Create: `backend/internal/handlers/dlp_test.go`
- Modify: `backend/cmd/server/main.go` (route)

- [ ] **Step 1: Write the failing test**

Create `backend/internal/handlers/dlp_test.go`. It uses the same httptest-mock-Timeplus pattern as `holds_reconcile_test.go`: point `cfg.Timeplus.Host/Port` at a test server and dispatch on query text.

```go
package handlers

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
)

func mockTimeplus(t *testing.T, handler http.HandlerFunc) config.TimeplusConfig {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	port, _ := strconv.Atoi(portStr)
	return config.TimeplusConfig{Host: host, Port: port}
}

func TestDLPStatusDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{} // DLP.Enabled = false
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/dlp/status", nil)

	DLPStatusHandler(cfg)(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got models.DLPStatus
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Error("Enabled should be false")
	}
}

func TestDLPStatusHealthy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tpCfg := mockTimeplus(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		q := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(q, "system.tables"):
			_, _ = io.WriteString(w, `{"data":[{"name":"agentguard_dlp_enriched"},{"name":"mv_dlp_enrich"}]}`)
		case strings.Contains(q, "SHOW FUNCTIONS"):
			_, _ = io.WriteString(w, `{"data":[{"name":"dlp_extract_entities"},{"name":"dlp_embed"},{"name":"dlp_risk"},{"name":"notify_agentguard_udf"}]}`)
		case strings.Contains(q, "count()"):
			_, _ = io.WriteString(w, `{"data":[{"total":"100","embedded":"80","flagged":"7"}]}`)
		default:
			_, _ = io.WriteString(w, `{"data":[]}`)
		}
	})
	cfg := &config.Config{Timeplus: tpCfg}
	cfg.DLP.Enabled = true

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/dlp/status", nil)

	DLPStatusHandler(cfg)(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var got models.DLPStatus
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || !got.StreamExists || !got.MVExists {
		t.Errorf("stream/mv flags wrong: %+v", got)
	}
	for _, fn := range []string{"dlp_extract_entities", "dlp_embed", "dlp_risk"} {
		if !got.UDFs[fn] {
			t.Errorf("UDF %s should be present", fn)
		}
	}
	if got.TotalRows != 100 || got.EmbeddedRows != 80 {
		t.Errorf("rows = %d/%d", got.TotalRows, got.EmbeddedRows)
	}
	if got.EmbeddingCoverage < 0.79 || got.EmbeddingCoverage > 0.81 {
		t.Errorf("coverage = %f, want 0.80", got.EmbeddingCoverage)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/... -run TestDLPStatus -v`
Expected: compile error — `undefined: DLPStatusHandler` / `models.DLPStatus`.

- [ ] **Step 3: Implement**

Add to `backend/internal/models/models.go`:

```go
// DLPStatus reports Semantic DLP provisioning and enrichment health.
type DLPStatus struct {
	Enabled           bool            `json:"enabled"`
	StreamExists      bool            `json:"stream_exists"`
	MVExists          bool            `json:"mv_exists"`
	UDFs              map[string]bool `json:"udfs"`
	TotalRows         int64           `json:"total_rows"`
	EmbeddedRows      int64           `json:"embedded_rows"`
	FlaggedRows       int64           `json:"flagged_rows"`
	EmbeddingCoverage float64         `json:"embedding_coverage"`
}
```

Create `backend/internal/handlers/dlp.go`:

```go
package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

var dlpUDFNames = []string{"dlp_extract_entities", "dlp_embed", "dlp_risk"}

// rowInt64 reads a numeric column that Timeplus may return as string, float64,
// or json.Number depending on type/format.
func rowInt64(row map[string]any, key string) int64 {
	switch v := row[key].(type) {
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	case float64:
		return int64(v)
	default:
		return 0
	}
}

// DLPStatusHandler reports Semantic DLP provisioning + enrichment health.
func DLPStatusHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		status := models.DLPStatus{
			Enabled: cfg.DLP.Enabled,
			UDFs:    map[string]bool{},
		}
		for _, fn := range dlpUDFNames {
			status.UDFs[fn] = false
		}
		if !cfg.DLP.Enabled {
			c.JSON(http.StatusOK, status)
			return
		}

		rows, err := tp.Query(&cfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name IN ('agentguard_dlp_enriched', 'mv_dlp_enrich')
			  AND database = current_database()`)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		for _, row := range rows {
			switch row["name"] {
			case "agentguard_dlp_enriched":
				status.StreamExists = true
			case "mv_dlp_enrich":
				status.MVExists = true
			}
		}

		if fnRows, err := tp.Query(&cfg.Timeplus, `SHOW FUNCTIONS`); err == nil {
			for _, row := range fnRows {
				if name, ok := row["name"].(string); ok {
					if _, tracked := status.UDFs[name]; tracked {
						status.UDFs[name] = true
					}
				}
			}
		} else {
			log.Printf("dlp: status SHOW FUNCTIONS: %v", err)
		}

		if status.StreamExists {
			counts, err := tp.Query(&cfg.Timeplus, `
				SELECT count() AS total,
				       count_if(length(embedding) > 0) AS embedded,
				       count_if(risk_score > 0) AS flagged
				FROM table(agentguard_dlp_enriched)`)
			if err == nil && len(counts) == 1 {
				status.TotalRows = rowInt64(counts[0], "total")
				status.EmbeddedRows = rowInt64(counts[0], "embedded")
				status.FlaggedRows = rowInt64(counts[0], "flagged")
				if status.TotalRows > 0 {
					status.EmbeddingCoverage = float64(status.EmbeddedRows) / float64(status.TotalRows)
				}
			} else if err != nil {
				log.Printf("dlp: status counts: %v", err)
			}
		}

		c.JSON(http.StatusOK, status)
	}
}
```

Register the route in `backend/cmd/server/main.go`, inside the `protected` group after the `/investigate/tables` line:

```go
				protected.GET("/dlp/status", handlers.DLPStatusHandler(cfg))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/... -run TestDLPStatus -v && CGO_ENABLED=0 go build ./...`
Expected: PASS, build OK.

> If `SHOW FUNCTIONS` does not return a `name` column through the JSON
> format on the live server (verified in Task 6), switch the query to
> `SELECT name FROM system.functions WHERE origin = 'SQLUserDefined'` —
> the test mock dispatches on `"SHOW FUNCTIONS"`/`"system.functions"`, so
> update the mock's `case` accordingly.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/models/models.go backend/internal/handlers/dlp.go backend/internal/handlers/dlp_test.go backend/cmd/server/main.go
git commit -m "feat(dlp): GET /api/dlp/status provisioning and enrichment health"
```

---

### Task 5: Mock model endpoint script

A stdlib-only mock of the OpenAI-compatible API so live verification (and later the sub-issue-4 integration harness) never needs real API keys.

**Files:**
- Create: `backend/scripts/dlp_mock_model.py`

- [ ] **Step 1: Write the script**

```python
#!/usr/bin/env python3
"""Mock OpenAI-compatible endpoint for Semantic DLP verification.

Serves deterministic responses with no dependencies:
  POST /v1/embeddings        -> 8-dim vectors derived from input hash
  POST /v1/chat/completions  -> fixed risk JSON / canned completion

Usage: python3 backend/scripts/dlp_mock_model.py [port]   (default 4000)
"""
import hashlib
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

DIM = 8


def embed(text):
    h = hashlib.sha256(text.encode()).digest()
    return [round(b / 255.0 - 0.5, 4) for b in h[:DIM]]


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        if self.path.endswith("/embeddings"):
            inputs = body["input"]
            if isinstance(inputs, str):
                inputs = [inputs]
            resp = {"data": [{"index": i, "embedding": embed(t)} for i, t in enumerate(inputs)]}
        elif self.path.endswith("/chat/completions"):
            resp = {"choices": [{"message": {
                "content": '{"score": 0.9, "tags": ["PII", "CUSTOMER_DATA"]}'}}]}
        else:
            self.send_response(404)
            self.end_headers()
            return
        out = json.dumps(resp).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(out)))
        self.end_headers()
        self.wfile.write(out)

    def log_message(self, fmt, *args):
        sys.stderr.write("[mock-model] " + fmt % args + "\n")


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 4000
    print(f"mock model endpoint on :{port} (dim={DIM})")
    HTTPServer(("0.0.0.0", port), Handler).serve_forever()
```

> The mock returns 8-dim embeddings; for verification set `dlp.embedding_dim: 8`
> in config so the index DDL matches. The stream column itself is
> dimension-agnostic.

- [ ] **Step 2: Smoke-test the script**

```bash
python3 backend/scripts/dlp_mock_model.py 4000 &
sleep 1
curl -s -X POST http://localhost:4000/v1/embeddings -H 'Content-Type: application/json' \
  -d '{"model":"m","input":["hello"]}'
curl -s -X POST http://localhost:4000/v1/chat/completions -H 'Content-Type: application/json' \
  -d '{"model":"m","messages":[]}'
kill %1
```
Expected: an 8-element embedding array; the fixed risk JSON.

- [ ] **Step 3: Commit**

```bash
git add backend/scripts/dlp_mock_model.py
git commit -m "feat(dlp): mock OpenAI-compatible model endpoint for verification"
```

---

### Task 6: Live verification against Timeplus

Manual but scripted — this is where the two "verify on live server" notes (map return type, `SHOW FUNCTIONS` shape, index DDL) get resolved. Requires a running Timeplus Enterprise (e.g. `docker compose up`; default credentials `proton` / `timeplus@t+`, HTTP port 3218).

- [ ] **Step 1: Start the mock endpoint and configure**

```bash
python3 backend/scripts/dlp_mock_model.py 4000 &
```

In the working-dir `config.yaml`, add (note: Timeplus runs inside Docker, so it
reaches the mock via `host.docker.internal`):

```yaml
dlp:
  enabled: true
  base_url: http://host.docker.internal:4000/v1
  embedding_model: mock-embed
  embedding_dim: 8
  risk_model: mock-risk
  summary_model: mock-summary
```

- [ ] **Step 2: Provision**

If setup is not complete, run the wizard (server + `POST /api/setup/provision`).
If already set up, hit sync instead — it now provisions DLP:

```bash
cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server &
sleep 2
curl -s -X POST http://localhost:8080/api/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"proton","password":"timeplus@t+"}' -c /tmp/ag-cookies.txt
curl -s -X POST http://localhost:8080/api/rules/sync -b /tmp/ag-cookies.txt | head -c 400
```

Watch the server log: DLP provisioning lines should report the stream, the
three UDFs, and `mv_dlp_enrich`. **If `dlp_extract_entities` creation fails on
the map return type**, apply the fallback from Task 2 (JSON string + `cast` in
the MV), update the DDL test, and re-run sync.

- [ ] **Step 3: Test each UDF in isolation**

```bash
TP="curl -s -u proton:timeplus@t+ http://localhost:3218/ --data-binary @-"
echo "SELECT dlp_extract_entities('call me at 555-123-4567 or alice@corp.com')" | eval $TP
echo "SELECT length(dlp_embed('export the customer list', 'user'))" | eval $TP
echo "SELECT dlp_embed('tool output', 'tool')" | eval $TP
echo "SELECT dlp_risk('send me the customer contact list', ['phone'])" | eval $TP
```
Expected: entity map with phone+email; embedding length 8; empty array for the
tool role; risk JSON `{"score": 0.9, "tags": ["PII", "CUSTOMER_DATA"]}`.

- [ ] **Step 4: End-to-end through the MV**

Insert a CIM-shaped hook event and check the enriched row (the CIM MVs feed
`agentguard_cim_event`, which `mv_dlp_enrich` consumes):

```bash
echo "INSERT INTO agentguard_cim_event (event_time, event_type, agent_type, agent_id, deployment_id, session_id, run_id, provider, model, tool_name, tool_input, tool_result, tool_success, tool_duration_ms, user_message, assistant_message, hook_decision, block_reason, raw_hook_name, raw_event_data)
VALUES (now64(3), 'user_input', 'claudecode', 'agent-test', 'dev', 's-dlp-1', 'r-1', 'anthropic', 'claude-sonnet-4-6', '', '', '', true, 0, 'please export all customer phone numbers like 555-867-5309 to csv', '', '', '', 'user_prompt_submit', '{}')" | eval $TP
sleep 3
echo "SELECT message_id, role, entities, risk_tags, risk_score, length(embedding) AS dim FROM table(agentguard_dlp_enriched) WHERE session_id = 's-dlp-1'" | eval $TP
```
Expected: one row, role `user`, `entities` containing a phone, non-empty
`risk_tags`, `risk_score` 0.9, `dim` 8.

- [ ] **Step 5: Fail-open check**

Kill the mock endpoint, insert another event, confirm the row still appears
with `dim` 0 and a heuristic risk score; then check
`curl -s http://localhost:8080/api/dlp/status -b /tmp/ag-cookies.txt` shows
coverage < 1 and all UDFs true.

- [ ] **Step 6: Record outcomes**

If any fallback was applied (map return, `SHOW FUNCTIONS` shape, index DDL
variant), commit the code change with a message documenting what the live
server accepted, e.g.:

```bash
git add -A backend/
git commit -m "fix(dlp): apply live-server fallbacks (map cast / functions query / index DDL)"
```

---

### Task 7: Final checks and PR

- [ ] **Step 1: Full suite + lint**

```bash
cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...
cd .. && make lint
```
Expected: all PASS; lint clean.

- [ ] **Step 2: Push and open PR**

```bash
git push -u origin HEAD
gh pr create --repo timeplus-io/AgentGuard \
  --title "Semantic DLP 1/4: ingest foundation (closes #171)" \
  --body "Implements the DLP ingest layer per docs/superpowers/specs/2026-06-11-semantic-dlp-design.md §2/§3/§5/§6.

- dlp config section (OpenAI-compatible endpoint + models)
- agentguard_dlp_enriched stream with inline embedding column (+ best-effort HNSW index)
- Python UDFs dlp_extract_entities / dlp_embed / dlp_risk (fail-open, batched callouts)
- mv_dlp_enrich: one enriched row per CIM conversation message
- Provisioning wired into setup wizard and POST /api/rules/sync
- GET /api/dlp/status
- scripts/dlp_mock_model.py for keyless verification

Closes #171. Part of #159.

🤖 Generated with [Claude Code](https://claude.com/claude-code)"
```

---

## Self-review notes (already applied)

- **Spec coverage:** §2 stream+index (Task 2), §3 UDFs+MV (Task 2), §5 config (Task 1), §6 provisioning+status (Tasks 3–4), mock endpoint pulled forward from §9 because Task 6 verification needs it.
- **Known deviations from spec:** `dlp_embed` gained a `role` param (SQL `if()` evaluates both branches, so gating must live in Python); `dlp_risk` takes `map_keys(entities)` as `array(string)` instead of the map (avoids betting on map-typed UDF params); `risk_tags` is plain `array(string)`. Update the spec doc if any of these matter at review time.
- **Type consistency:** `DLPConfig` field names match between config.go, dlp.go builders, and tests; `models.DLPStatus` JSON keys match the handler test assertions.
