# Semantic DLP 2/4: Query Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the NL forensic-query funnel — query-time UDFs (`dlp_understand_query`, `dlp_rerank`, `dlp_summarize`), the streaming `POST /api/dlp/query` orchestrator, and the `GET /api/dlp/messages[/:id]` + `GET /api/dlp/profiles` browse APIs (GitHub issue #172, spec §4/§7).

**Architecture:** Go orchestrates the funnel but every model touch is a Python UDF invoked via SQL: understand → multi-recall (structured channel + vector channel over `agentguard_dlp_enriched`, merged/deduped in Go) → rerank (UDF over candidate rows inside Timeplus) → summarize (UDF over top-N evidence JSON). The endpoint streams NDJSON stage events (proton.go flush pattern). Retrieval uses brute-force `cosine_distance` (verified: TE 3.2.1 has no vector index type; rows with empty embeddings are recalled by the structured channel).

**Tech Stack:** Go (Gin), Timeplus Python UDFs (placeholder-interpolated like the ingest UDFs), streaming SQL over `table(agentguard_dlp_enriched)`.

**Branch:** `feature/172-dlp-query-pipeline` (off main, which already contains #171/PR #175). PR closes #172.

**Build/test (macOS 26+):** always `CGO_ENABLED=0`; run Go commands from `backend/`.

**Live findings from #171 that constrain this plan:**
- Brute-force `cosine_distance(embedding, [..])` works; `vector_similarity` index does not exist on TE 3.2.1.
- `cosine_distance` on an empty array errors → the vector channel must filter `length(embedding) > 0`.
- Mock endpoint (`backend/scripts/dlp_mock_model.py`) returns a fixed risk JSON for every chat completion — this task extends it to dispatch on `TASK:` markers in the prompt.
- `QueryNative` (native port) is the package convention for queries returning user-content columns; it cannot be mocked with httptest, so handler logic is decomposed into pure, unit-testable helpers (intent parsing, SQL builders, merge/dedup, evidence building) with end-to-end behavior covered by live verification (Task 5).
- Use `escapeSQLStr` (rules.go — escapes `\` and `'`) for every user/model-derived string placed in SQL.

---

## File map

| File | Action | Responsibility |
|------|--------|----------------|
| `backend/internal/timeplus/dlp_query.go` | Create | 3 query-UDF templates + builders |
| `backend/internal/timeplus/dlp_query_test.go` | Create | Builder unit tests |
| `backend/internal/timeplus/dlp.go` | Modify | Append query UDFs to `InitDLP`'s udfs slice |
| `backend/scripts/dlp_mock_model.py` | Modify | Dispatch chat completions on `TASK:` markers |
| `backend/internal/models/models.go` | Modify | `DLPIntent`, `DLPEvidence`, `DLPEnrichedMessage`, `DLPProfiles` structs |
| `backend/internal/handlers/dlp_query.go` | Create | `DLPQuery` handler + pure funnel helpers |
| `backend/internal/handlers/dlp_query_test.go` | Create | Unit tests for the pure helpers |
| `backend/internal/handlers/dlp.go` | Modify | `ListEnrichedMessages`, `GetEnrichedMessage`, `DLPProfiles` handlers |
| `backend/internal/handlers/dlp_test.go` | Modify | SQL-builder/filter tests for messages + profiles |
| `backend/cmd/server/main.go` | Modify | 4 new routes in the `protected` group |

---

### Task 1: Query-time UDFs + mock dispatch

**Files:**
- Create: `backend/internal/timeplus/dlp_query.go`, `backend/internal/timeplus/dlp_query_test.go`
- Modify: `backend/internal/timeplus/dlp.go` (udfs slice), `backend/scripts/dlp_mock_model.py`

- [ ] **Step 1: Write the failing tests** — create `backend/internal/timeplus/dlp_query_test.go`:

```go
package timeplus

import (
	"strings"
	"testing"
)

func TestDLPQueryUDFDDLs(t *testing.T) {
	cfg := testDLPCfg() // from dlp_test.go
	cases := []struct {
		name  string
		ddl   string
		wants []string
	}{
		{"understand", DLPUnderstandUDFDDL(cfg), []string{
			"CREATE OR REPLACE FUNCTION dlp_understand_query(question string)",
			"RETURNS string", "LANGUAGE PYTHON",
			`MODEL = "gpt-4o-mini"`, // risk model
			"TASK: QUERY_UNDERSTANDING",
		}},
		{"rerank", DLPRerankUDFDDL(cfg), []string{
			"CREATE OR REPLACE FUNCTION dlp_rerank(question string, content string)",
			"RETURNS float64",
			`MODEL = "gpt-4o-mini"`, // risk model
			"TASK: RERANK",
		}},
		{"summarize", DLPSummarizeUDFDDL(cfg), []string{
			"CREATE OR REPLACE FUNCTION dlp_summarize(question string, evidence string)",
			"RETURNS string",
			`MODEL = "gpt-4o"`, // summary model
			"TASK: AUDIT_SUMMARY",
		}},
	}
	for _, tc := range cases {
		assertCleanDDL(t, tc.name, tc.ddl) // also checks {{ }} leftovers
		for _, want := range tc.wants {
			if !strings.Contains(tc.ddl, want) {
				t.Errorf("%s DDL missing %q", tc.name, want)
			}
		}
		if !strings.Contains(tc.ddl, `BASE_URL = "http://mock:9999/v1"`) {
			t.Errorf("%s DDL missing base url", tc.name)
		}
	}
}
```

- [ ] **Step 2: Verify failure**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/... -run TestDLPQueryUDFDDLs -v`
Expected: `undefined: DLPUnderstandUDFDDL` etc.

- [ ] **Step 3: Create `backend/internal/timeplus/dlp_query.go`:**

```go
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

func dlpReplacer(cfg config.DLPConfig, model string) *strings.Replacer {
	return strings.NewReplacer(
		"{{BASE_URL}}", cfg.BaseURL,
		"{{API_KEY}}", cfg.APIKey,
		"{{MODEL}}", model,
	)
}

func DLPUnderstandUDFDDL(cfg config.DLPConfig) string {
	return dlpReplacer(cfg, cfg.RiskModel).Replace(dlpUnderstandUDFTemplate)
}

func DLPRerankUDFDDL(cfg config.DLPConfig) string {
	return dlpReplacer(cfg, cfg.RiskModel).Replace(dlpRerankUDFTemplate)
}

func DLPSummarizeUDFDDL(cfg config.DLPConfig) string {
	return dlpReplacer(cfg, cfg.SummaryModel).Replace(dlpSummarizeUDFTemplate)
}
```

Refactor note: `DLPEmbedUDFDDL`/`DLPRiskUDFDDL` in dlp.go build their own `strings.NewReplacer` — switch them to `dlpReplacer(cfg, cfg.EmbeddingModel)` / `dlpReplacer(cfg, cfg.RiskModel)` so the placeholder set has one definition (existing tests must stay green).

- [ ] **Step 4: Wire into `InitDLP`** — in `backend/internal/timeplus/dlp.go`, extend the udfs slice:

```go
	udfs := []struct{ name, ddl string }{
		{"dlp_extract_entities", ddlDLPEntitiesUDF},
		{"dlp_embed", DLPEmbedUDFDDL(dlpCfg)},
		{"dlp_risk", DLPRiskUDFDDL(dlpCfg)},
		{"dlp_understand_query", DLPUnderstandUDFDDL(dlpCfg)},
		{"dlp_rerank", DLPRerankUDFDDL(dlpCfg)},
		{"dlp_summarize", DLPSummarizeUDFDDL(dlpCfg)},
	}
```

- [ ] **Step 5: Extend the mock** — in `backend/scripts/dlp_mock_model.py`, add `import re` at the top and replace the chat/completions branch body with marker dispatch:

```python
        elif self.path.endswith("/chat/completions"):
            msgs = body.get("messages", [])
            prompt = msgs[-1].get("content", "") if msgs else ""
            if "TASK: QUERY_UNDERSTANDING" in prompt:
                content = json.dumps({"keywords": ["customer", "phone", "export"],
                                      "entity_types": ["phone"],
                                      "risk_tags": ["PII"],
                                      "time_range_days": 30})
            elif "TASK: RERANK" in prompt:
                m = re.search(r"ITEMS: (\d+)", prompt)
                n = int(m.group(1)) if m else 1
                content = json.dumps(
                    {"scores": [round(max(0.05, 0.95 - 0.05 * i), 2) for i in range(n)]})
            elif "TASK: AUDIT_SUMMARY" in prompt:
                content = ("Suspected leak: an agent exported customer phone numbers. "
                           "Evidence in the cited messages.")
            else:
                content = '{"score": 0.9, "tags": ["PII", "CUSTOMER_DATA"]}'
            resp = {"choices": [{"message": {"content": content}}]}
```

Smoke-test each marker with curl (start on a free port, POST a chat completion whose content contains each marker, verify the dispatch; then the no-marker case still returns the risk JSON).

- [ ] **Step 6: Run tests**

`cd backend && CGO_ENABLED=0 go test ./internal/timeplus/... -v -run TestDLP` → all PASS (old + new). `CGO_ENABLED=0 go build ./...` clean.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/timeplus/ backend/scripts/dlp_mock_model.py
git commit -m "feat(dlp): query-time UDFs (understand/rerank/summarize) + mock dispatch"
```

---

### Task 2: Models + pure funnel helpers

**Files:**
- Modify: `backend/internal/models/models.go`
- Create: `backend/internal/handlers/dlp_query.go` (helpers only in this task), `backend/internal/handlers/dlp_query_test.go`

- [ ] **Step 1: Add models** to `backend/internal/models/models.go`:

```go
// DLPIntent is the structured retrieval intent parsed from an NL question.
type DLPIntent struct {
	Keywords      []string `json:"keywords"`
	EntityTypes   []string `json:"entity_types"`
	RiskTags      []string `json:"risk_tags"`
	TimeRangeDays int      `json:"time_range_days"`
}

// DLPEvidence is one cited message in a DLP query answer.
type DLPEvidence struct {
	MessageID   string   `json:"message_id"`
	SessionID   string   `json:"session_id"`
	RunID       string   `json:"run_id"`
	EventTime   string   `json:"event_time"`
	AgentID     string   `json:"agent_id"`
	AgentType   string   `json:"agent_type"`
	Role        string   `json:"role"`
	Content     string   `json:"content"`
	RiskTags    []string `json:"risk_tags"`
	RiskScore   float64  `json:"risk_score"`
	RerankScore float64  `json:"rerank_score"`
}

// DLPEnrichedMessage is a row of agentguard_dlp_enriched for the browser API.
type DLPEnrichedMessage struct {
	EventTime    string              `json:"event_time"`
	MessageID    string              `json:"message_id"`
	SessionID    string              `json:"session_id"`
	RunID        string              `json:"run_id"`
	Role         string              `json:"role"`
	AgentType    string              `json:"agent_type"`
	AgentID      string              `json:"agent_id"`
	DeploymentID string              `json:"deployment_id"`
	Model        string              `json:"model"`
	ToolName     string              `json:"tool_name"`
	Content      string              `json:"content"`
	Entities     map[string][]string `json:"entities"`
	RiskTags     []string            `json:"risk_tags"`
	RiskScore    float64             `json:"risk_score"`
	EmbeddingDim int                 `json:"embedding_dim"`
}

// DLPProfileEntry is one row of a risk-profile ranking.
type DLPProfileEntry struct {
	Key          string  `json:"key"`
	FlaggedCount int64   `json:"flagged_count"`
	MaxRiskScore float64 `json:"max_risk_score"`
	AvgRiskScore float64 `json:"avg_risk_score"`
	LastSeen     string  `json:"last_seen"`
}

// DLPProfiles aggregates top risk agents/sessions/tags over a window.
type DLPProfiles struct {
	WindowDays  int               `json:"window_days"`
	TopAgents   []DLPProfileEntry `json:"top_agents"`
	TopSessions []DLPProfileEntry `json:"top_sessions"`
	TopTags     []DLPProfileEntry `json:"top_tags"`
}
```

- [ ] **Step 2: Write failing helper tests** — `backend/internal/handlers/dlp_query_test.go`:

```go
package handlers

import (
	"strings"
	"testing"

	tp "github.com/timeplus/agentguard/internal/timeplus"
)

func TestParseDLPIntent(t *testing.T) {
	got := parseDLPIntent(`{"keywords":["customer","phone"],"entity_types":["phone"],"risk_tags":["PII"],"time_range_days":7}`, "ignored")
	if len(got.Keywords) != 2 || got.EntityTypes[0] != "phone" || got.RiskTags[0] != "PII" || got.TimeRangeDays != 7 {
		t.Errorf("parsed intent wrong: %+v", got)
	}

	// Malformed JSON falls back to question-derived keywords + 30d window.
	fb := parseDLPIntent("not json at all", "Who exported the customer contact list?")
	if fb.TimeRangeDays != 30 {
		t.Errorf("fallback window = %d, want 30", fb.TimeRangeDays)
	}
	joined := strings.Join(fb.Keywords, " ")
	if !strings.Contains(joined, "customer") || !strings.Contains(joined, "exported") {
		t.Errorf("fallback keywords = %v", fb.Keywords)
	}

	// Out-of-range window is clamped.
	if w := parseDLPIntent(`{"time_range_days":10000}`, "q").TimeRangeDays; w != 365 {
		t.Errorf("clamped window = %d, want 365", w)
	}
	if w := parseDLPIntent(`{"time_range_days":-5}`, "q").TimeRangeDays; w != 30 {
		t.Errorf("nonpositive window = %d, want default 30", w)
	}
}

func TestBuildDLPRecallSQL(t *testing.T) {
	intent := parseDLPIntent(`{"keywords":["o'brien"],"entity_types":["phone"],"risk_tags":["PII"],"time_range_days":14}`, "q")
	structured, vector := buildDLPRecallSQL(intent, "who leaked o'brien's number?")

	for _, want := range []string{
		"table(agentguard_dlp_enriched)",
		"INTERVAL 14 DAY",
		`not empty(entities['phone'])`,
		`has(risk_tags, 'PII')`,
		`position(lower(content), 'o''brien')`,
		"ORDER BY risk_score DESC, event_time DESC",
		"LIMIT 200",
	} {
		if !strings.Contains(structured, want) {
			t.Errorf("structured SQL missing %q:\n%s", want, structured)
		}
	}
	for _, want := range []string{
		"length(embedding) > 0",
		"cosine_distance(embedding, dlp_embed(",
		`o''brien`, // question escaped inside dlp_embed(...)
		"LIMIT 100",
	} {
		if !strings.Contains(vector, want) {
			t.Errorf("vector SQL missing %q:\n%s", want, vector)
		}
	}

	// No predicates at all → structured SQL has only the time filter.
	s2, _ := buildDLPRecallSQL(parseDLPIntent(`{}`, ""), "q")
	if strings.Contains(s2, "entities[") || strings.Contains(s2, "has(risk_tags") {
		t.Errorf("empty intent should not emit predicates:\n%s", s2)
	}
}

func TestMergeDLPCandidates(t *testing.T) {
	a := []tp.Row{{"message_id": "m1"}, {"message_id": "m2"}}
	b := []tp.Row{{"message_id": "m2"}, {"message_id": "m3"}, {"message_id": "m4"}}
	got := mergeDLPCandidates(a, b, 3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (cap)", len(got))
	}
	ids := []string{got[0]["message_id"].(string), got[1]["message_id"].(string), got[2]["message_id"].(string)}
	if ids[0] != "m1" || ids[1] != "m2" || ids[2] != "m3" {
		t.Errorf("merged order/dedup wrong: %v", ids)
	}
}

func TestBuildDLPRerankSQL(t *testing.T) {
	sql := buildDLPRerankSQL("what's up?", []string{"m1", "m'2"})
	for _, want := range []string{
		"dlp_rerank('what''s up?', content)",
		`message_id IN ('m1', 'm''2')`,
		"table(agentguard_dlp_enriched)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("rerank SQL missing %q:\n%s", want, sql)
		}
	}
}

func TestRowToDLPEvidence(t *testing.T) {
	row := tp.Row{
		"message_id": "m1", "session_id": "s1", "run_id": "r1",
		"event_time": "2026-06-11 10:00:00.000", "agent_id": "a1", "agent_type": "claudecode",
		"role": "user", "content": strings.Repeat("x", 600),
		"risk_tags": []any{"PII"}, "risk_score": 0.9,
	}
	ev := rowToDLPEvidence(row, 0.87)
	if ev.MessageID != "m1" || ev.RerankScore != 0.87 || ev.RiskScore != 0.9 {
		t.Errorf("evidence mapping wrong: %+v", ev)
	}
	if len(ev.Content) != 500 {
		t.Errorf("content not truncated to 500: %d", len(ev.Content))
	}
	if len(ev.RiskTags) != 1 || ev.RiskTags[0] != "PII" {
		t.Errorf("risk tags wrong: %v", ev.RiskTags)
	}
}
```

- [ ] **Step 3: Verify failure** — `CGO_ENABLED=0 go test ./internal/handlers/... -run 'TestParseDLPIntent|TestBuildDLP|TestMergeDLP|TestRowToDLP' -v` → undefined symbols.

- [ ] **Step 4: Implement the helpers** — create `backend/internal/handlers/dlp_query.go` (handler itself comes in Task 3; this task adds package-level helpers):

```go
package handlers

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/timeplus/agentguard/internal/models"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

const (
	dlpStructuredLimit = 200
	dlpVectorLimit     = 100
	dlpCandidateCap    = 300
	dlpEvidenceChars   = 500
	dlpDefaultTopN     = 20
	dlpMaxTopN         = 50
)

var dlpWordRe = regexp.MustCompile(`[A-Za-z0-9]{4,}`)

// parseDLPIntent decodes the dlp_understand_query JSON; on failure it derives
// keywords from the question itself. The window is clamped to [1, 365].
func parseDLPIntent(raw, question string) models.DLPIntent {
	var intent models.DLPIntent
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		for _, w := range dlpWordRe.FindAllString(question, 8) {
			intent.Keywords = append(intent.Keywords, strings.ToLower(w))
		}
	}
	if intent.TimeRangeDays <= 0 {
		intent.TimeRangeDays = 30
	}
	if intent.TimeRangeDays > 365 {
		intent.TimeRangeDays = 365
	}
	return intent
}

const dlpRecallCols = `event_time, message_id, session_id, run_id, role,
       agent_type, agent_id, content, risk_tags, risk_score`

// buildDLPRecallSQL produces the two recall channels. The structured channel
// catches rows by entity/tag/keyword predicates regardless of embedding
// presence; the vector channel ranks embedded rows by cosine distance to the
// question embedding (dlp_embed runs inside Timeplus). Rows with empty
// embeddings would make cosine_distance error, hence the length() guard.
func buildDLPRecallSQL(intent models.DLPIntent, question string) (structured, vector string) {
	timeFilter := fmt.Sprintf("event_time > now() - INTERVAL %d DAY", intent.TimeRangeDays)

	var preds []string
	for _, et := range intent.EntityTypes {
		preds = append(preds, fmt.Sprintf("not empty(entities['%s'])", escapeSQLStr(et)))
	}
	for _, tag := range intent.RiskTags {
		preds = append(preds, fmt.Sprintf("has(risk_tags, '%s')", escapeSQLStr(tag)))
	}
	for _, kw := range intent.Keywords {
		preds = append(preds, fmt.Sprintf("position(lower(content), '%s') > 0", escapeSQLStr(strings.ToLower(kw))))
	}
	where := timeFilter
	if len(preds) > 0 {
		where += " AND (" + strings.Join(preds, " OR ") + ")"
	}
	structured = fmt.Sprintf(`SELECT %s
FROM table(agentguard_dlp_enriched)
WHERE %s
ORDER BY risk_score DESC, event_time DESC
LIMIT %d`, dlpRecallCols, where, dlpStructuredLimit)

	vector = fmt.Sprintf(`SELECT %s,
       cosine_distance(embedding, dlp_embed('%s', 'user')) AS dist
FROM table(agentguard_dlp_enriched)
WHERE %s AND length(embedding) > 0
ORDER BY dist ASC
LIMIT %d`, dlpRecallCols, escapeSQLStr(question), timeFilter, dlpVectorLimit)
	return structured, vector
}

// mergeDLPCandidates unions the channels in order, deduping by message_id.
func mergeDLPCandidates(a, b []tp.Row, cap int) []tp.Row {
	seen := make(map[string]bool, len(a)+len(b))
	var out []tp.Row
	for _, row := range append(append([]tp.Row{}, a...), b...) {
		id, _ := row["message_id"].(string)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, row)
		if len(out) >= cap {
			break
		}
	}
	return out
}

// buildDLPRerankSQL scores candidates inside Timeplus via the dlp_rerank UDF.
func buildDLPRerankSQL(question string, ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = "'" + escapeSQLStr(id) + "'"
	}
	return fmt.Sprintf(`SELECT message_id, dlp_rerank('%s', content) AS score
FROM table(agentguard_dlp_enriched)
WHERE message_id IN (%s)`, escapeSQLStr(question), strings.Join(quoted, ", "))
}

func rowStr(row tp.Row, key string) string {
	s, _ := row[key].(string)
	return s
}

func rowStrSlice(row tp.Row, key string) []string {
	var out []string
	switch v := row[key].(type) {
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = v
	}
	return out
}

// rowToDLPEvidence maps a recall row to a cited-evidence record, truncating
// content for the summarizer/UI.
func rowToDLPEvidence(row tp.Row, rerankScore float64) models.DLPEvidence {
	content := rowStr(row, "content")
	if len(content) > dlpEvidenceChars {
		content = content[:dlpEvidenceChars]
	}
	return models.DLPEvidence{
		MessageID:   rowStr(row, "message_id"),
		SessionID:   rowStr(row, "session_id"),
		RunID:       rowStr(row, "run_id"),
		EventTime:   rowStr(row, "event_time"),
		AgentID:     rowStr(row, "agent_id"),
		AgentType:   rowStr(row, "agent_type"),
		Role:        rowStr(row, "role"),
		Content:     content,
		RiskTags:    rowStrSlice(row, "risk_tags"),
		RiskScore:   parseFloat64(row["risk_score"]),
		RerankScore: rerankScore,
	}
}
```

Check `parseFloat64` exists in helpers.go (a float sibling of `parseInt64`); if it doesn't, add it next to `parseInt64` handling `float64`, `json.Number`, `string`, `int64` → float64, with table tests in helpers_test.go.

- [ ] **Step 5: Run tests** — the four new test funcs PASS; whole handlers package PASS; build clean.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/models/models.go backend/internal/handlers/dlp_query.go backend/internal/handlers/dlp_query_test.go backend/internal/handlers/helpers.go backend/internal/handlers/helpers_test.go
git commit -m "feat(dlp): query funnel models and pure helpers (intent, recall SQL, merge, evidence)"
```

---

### Task 3: `POST /api/dlp/query` streaming handler

**Files:**
- Modify: `backend/internal/handlers/dlp_query.go` (add the handler), `backend/internal/handlers/dlp_query_test.go`, `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing tests** (append to dlp_query_test.go):

```go
func TestDLPQueryUnprovisioned(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tpCfg := mockTimeplus(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`) // no stream, no MV
	})
	cfg := &config.Config{Timeplus: tpCfg}
	cfg.DLP.Enabled = true

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/dlp/query",
		strings.NewReader(`{"question":"who leaked?"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	DLPQuery(cfg)(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", w.Code, w.Body.String())
	}
}

func TestDLPQueryValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.DLP.Enabled = true
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/dlp/query", strings.NewReader(`{"question":""}`))
	c.Request.Header.Set("Content-Type", "application/json")
	DLPQuery(cfg)(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestDLPQueryDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{} // DLP disabled
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/dlp/query", strings.NewReader(`{"question":"x"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	DLPQuery(cfg)(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}
```

(Add the needed imports; `mockTimeplus` already exists in dlp_test.go, same package.)

- [ ] **Step 2: Verify failure** — undefined: DLPQuery.

- [ ] **Step 3: Implement the handler** (append to dlp_query.go; add imports `log`, `net/http`, `github.com/gin-gonic/gin`, `github.com/timeplus/agentguard/internal/auth`, `github.com/timeplus/agentguard/internal/config`, `sort`):

```go
// dlpStage writes one NDJSON stage event and flushes it to the client.
func dlpStage(c *gin.Context, flusher http.Flusher, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = c.Writer.Write(data)
	_, _ = c.Writer.Write([]byte{'\n'})
	if flusher != nil {
		flusher.Flush()
	}
}

// DLPQuery runs the NL forensic funnel: understand → multi-recall → rerank →
// summarize, streaming NDJSON stage events:
//
//	{"stage":"intent","intent":{...}}
//	{"stage":"candidates","count":N,"structured":N1,"vector":N2}
//	{"stage":"reranked","kept":K}
//	{"stage":"answer","report":"...","evidence":[...]}
//	{"stage":"error","error":"..."}   (terminal, any point after headers)
func DLPQuery(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		if !cfg.DLP.Enabled {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Semantic DLP is disabled (dlp.enabled=false)"})
			return
		}

		var body struct {
			Question string `json:"question"`
			TopN     int    `json:"top_n"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Question) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "question is required"})
			return
		}
		question := strings.TrimSpace(body.Question)
		topN := body.TopN
		if topN <= 0 {
			topN = dlpDefaultTopN
		}
		if topN > dlpMaxTopN {
			topN = dlpMaxTopN
		}

		// Provision check happens BEFORE streaming headers so failures are
		// plain JSON the client can handle uniformly.
		provRows, err := tp.Query(&cfg.Timeplus, `
			SELECT name FROM system.tables
			WHERE name IN ('agentguard_dlp_enriched', 'mv_dlp_enrich')
			  AND database = current_database()`)
		if err != nil || len(provRows) < 2 {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "Semantic DLP is not provisioned; run setup or POST /api/rules/sync"})
			return
		}

		c.Header("Content-Type", "application/x-ndjson")
		c.Header("X-Accel-Buffering", "no")
		c.Header("Cache-Control", "no-cache")
		c.Status(http.StatusOK)
		flusher, _ := c.Writer.(http.Flusher)

		fail := func(stage string, err error) {
			log.Printf("dlp: query %s: %v", stage, err)
			dlpStage(c, flusher, gin.H{"stage": "error", "error": stage + ": " + err.Error()})
		}

		// 1. Understand.
		intentRows, err := tp.Query(&cfg.Timeplus,
			fmt.Sprintf(`SELECT dlp_understand_query('%s') AS intent`, escapeSQLStr(question)))
		if err != nil {
			fail("understand", err)
			return
		}
		rawIntent := ""
		if len(intentRows) == 1 {
			rawIntent = rowStr(intentRows[0], "intent")
		}
		intent := parseDLPIntent(rawIntent, question)
		dlpStage(c, flusher, gin.H{"stage": "intent", "intent": intent})

		// 2. Multi-recall (content columns → native protocol).
		structuredSQL, vectorSQL := buildDLPRecallSQL(intent, question)
		structuredRows, err := tp.QueryNative(&cfg.Timeplus, structuredSQL)
		if err != nil {
			fail("recall", err)
			return
		}
		vectorRows, err := tp.QueryNative(&cfg.Timeplus, vectorSQL)
		if err != nil {
			// Vector channel is best-effort (e.g. embedding dim drift); the
			// structured channel alone still answers.
			log.Printf("dlp: query vector recall (continuing structured-only): %v", err)
			vectorRows = nil
		}
		candidates := mergeDLPCandidates(structuredRows, vectorRows, dlpCandidateCap)
		dlpStage(c, flusher, gin.H{"stage": "candidates", "count": len(candidates),
			"structured": len(structuredRows), "vector": len(vectorRows)})

		if len(candidates) == 0 {
			dlpStage(c, flusher, gin.H{"stage": "answer",
				"report":   "No matching messages found in the selected time window.",
				"evidence": []models.DLPEvidence{}})
			return
		}

		// 3. Rerank inside Timeplus; on failure fall back to recall order.
		scores := make(map[string]float64, len(candidates))
		if len(candidates) > topN {
			ids := make([]string, len(candidates))
			for i, row := range candidates {
				ids[i] = rowStr(row, "message_id")
			}
			scoreRows, err := tp.QueryNative(&cfg.Timeplus, buildDLPRerankSQL(question, ids))
			if err != nil {
				log.Printf("dlp: query rerank (falling back to recall order): %v", err)
			} else {
				for _, row := range scoreRows {
					scores[rowStr(row, "message_id")] = parseFloat64(row["score"])
				}
				sort.SliceStable(candidates, func(i, j int) bool {
					return scores[rowStr(candidates[i], "message_id")] > scores[rowStr(candidates[j], "message_id")]
				})
			}
			candidates = candidates[:topN]
		}
		dlpStage(c, flusher, gin.H{"stage": "reranked", "kept": len(candidates)})

		// 4. Summarize over the top-N evidence.
		evidence := make([]models.DLPEvidence, len(candidates))
		for i, row := range candidates {
			evidence[i] = rowToDLPEvidence(row, scores[rowStr(row, "message_id")])
		}
		evidenceJSON, _ := json.Marshal(evidence)
		report := ""
		reportRows, err := tp.Query(&cfg.Timeplus, fmt.Sprintf(
			`SELECT dlp_summarize('%s', '%s') AS report`,
			escapeSQLStr(question), escapeSQLStr(string(evidenceJSON))))
		if err != nil {
			log.Printf("dlp: query summarize (returning evidence without report): %v", err)
		} else if len(reportRows) == 1 {
			report = rowStr(reportRows[0], "report")
		}
		if report == "" {
			report = fmt.Sprintf("Model summary unavailable. Top %d candidate messages are attached as evidence.", len(evidence))
		}
		dlpStage(c, flusher, gin.H{"stage": "answer", "report": report, "evidence": evidence})
	}
}
```

- [ ] **Step 4: Register the route** in `backend/cmd/server/main.go`, next to the existing `/dlp/status` registration (find it — main.go was restructured recently):

```go
				protected.POST("/dlp/query", handlers.DLPQuery(cfg))
```

- [ ] **Step 5: Run** — new tests + whole package PASS; build clean.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/handlers/dlp_query.go backend/internal/handlers/dlp_query_test.go backend/cmd/server/main.go
git commit -m "feat(dlp): POST /api/dlp/query NDJSON streaming funnel"
```

---

### Task 4: Messages browser + risk profiles APIs

**Files:**
- Modify: `backend/internal/handlers/dlp.go`, `backend/internal/handlers/dlp_test.go`, `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing tests** (append to dlp_test.go):

```go
func TestBuildDLPMessagesSQL(t *testing.T) {
	where, args := buildDLPMessagesWhere(dlpMessagesFilters{
		RiskTag:    "PII",
		EntityType: "phone",
		AgentID:    "a'1",
		SessionID:  "s1",
		Role:       "user",
		Since:      "2026-06-01 00:00:00",
		Until:      "2026-06-11 00:00:00",
		MinScore:   0.5,
	})
	_ = args
	for _, want := range []string{
		`has(risk_tags, 'PII')`,
		`not empty(entities['phone'])`,
		`agent_id = 'a''1'`,
		`session_id = 's1'`,
		`role = 'user'`,
		`event_time >= '2026-06-01 00:00:00'`,
		`event_time <= '2026-06-11 00:00:00'`,
		`risk_score >= 0.500000`,
	} {
		if !strings.Contains(where, want) {
			t.Errorf("WHERE missing %q:\n%s", want, where)
		}
	}
	// Empty filters → trivially-true clause, no stray AND.
	empty, _ := buildDLPMessagesWhere(dlpMessagesFilters{})
	if empty != "1 = 1" {
		t.Errorf("empty where = %q", empty)
	}
}

func TestDLPProfilesSQLWindow(t *testing.T) {
	sqls := buildDLPProfilesSQLs(14)
	if len(sqls) != 3 {
		t.Fatalf("want 3 profile queries, got %d", len(sqls))
	}
	for _, q := range sqls {
		if !strings.Contains(q, "INTERVAL 14 DAY") || !strings.Contains(q, "risk_score > 0") {
			t.Errorf("profile SQL missing window/flag filter:\n%s", q)
		}
	}
}
```

- [ ] **Step 2: Verify failure**, then **Step 3: Implement** in `backend/internal/handlers/dlp.go`:

```go
type dlpMessagesFilters struct {
	RiskTag    string
	EntityType string
	AgentID    string
	SessionID  string
	Role       string
	Since      string
	Until      string
	MinScore   float64
}

// buildDLPMessagesWhere returns the WHERE clause for the enriched-message
// browser. Returns "1 = 1" when no filters are set. (Second return value is
// reserved for future parameterized queries; currently always nil.)
func buildDLPMessagesWhere(f dlpMessagesFilters) (string, []any) {
	var preds []string
	if f.RiskTag != "" {
		preds = append(preds, fmt.Sprintf("has(risk_tags, '%s')", escapeSQLStr(f.RiskTag)))
	}
	if f.EntityType != "" {
		preds = append(preds, fmt.Sprintf("not empty(entities['%s'])", escapeSQLStr(f.EntityType)))
	}
	if f.AgentID != "" {
		preds = append(preds, fmt.Sprintf("agent_id = '%s'", escapeSQLStr(f.AgentID)))
	}
	if f.SessionID != "" {
		preds = append(preds, fmt.Sprintf("session_id = '%s'", escapeSQLStr(f.SessionID)))
	}
	if f.Role != "" {
		preds = append(preds, fmt.Sprintf("role = '%s'", escapeSQLStr(f.Role)))
	}
	if f.Since != "" {
		preds = append(preds, fmt.Sprintf("event_time >= '%s'", escapeSQLStr(f.Since)))
	}
	if f.Until != "" {
		preds = append(preds, fmt.Sprintf("event_time <= '%s'", escapeSQLStr(f.Until)))
	}
	if f.MinScore > 0 {
		preds = append(preds, fmt.Sprintf("risk_score >= %f", f.MinScore))
	}
	if len(preds) == 0 {
		return "1 = 1", nil
	}
	return strings.Join(preds, " AND "), nil
}

// buildDLPProfilesSQLs returns the three ranking queries (agents, sessions,
// tags) over flagged rows in the window. Tags are expanded with array_join.
func buildDLPProfilesSQLs(days int) []string {
	base := func(keyExpr string) string {
		return fmt.Sprintf(`SELECT %s AS key,
       count() AS flagged_count,
       max(risk_score) AS max_risk_score,
       avg(risk_score) AS avg_risk_score,
       to_string(max(event_time)) AS last_seen
FROM table(agentguard_dlp_enriched)
WHERE risk_score > 0 AND event_time > now() - INTERVAL %d DAY
GROUP BY key
ORDER BY flagged_count DESC, max_risk_score DESC
LIMIT 10`, keyExpr, days)
	}
	return []string{
		base("agent_id"),
		base("session_id"),
		base("array_join(risk_tags)"),
	}
}
```

Handlers (same file):

```go
// ListEnrichedMessages browses agentguard_dlp_enriched with filters.
// GET /api/dlp/messages?risk_tag=&entity_type=&agent_id=&session_id=&role=&since=&until=&min_score=&page=&limit=
func ListEnrichedMessages(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		minScore, _ := strconv.ParseFloat(c.Query("min_score"), 64)
		filters := dlpMessagesFilters{
			RiskTag:    c.Query("risk_tag"),
			EntityType: c.Query("entity_type"),
			AgentID:    c.Query("agent_id"),
			SessionID:  c.Query("session_id"),
			Role:       c.Query("role"),
			Since:      c.Query("since"),
			Until:      c.Query("until"),
			MinScore:   minScore,
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		if page < 1 {
			page = 1
		}
		if limit < 1 || limit > 500 {
			limit = 50
		}
		where, _ := buildDLPMessagesWhere(filters)

		countRows, err := tp.Query(&cfg.Timeplus, fmt.Sprintf(
			`SELECT count() AS total FROM table(agentguard_dlp_enriched) WHERE %s`, where))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		total := int64(0)
		if len(countRows) == 1 {
			total = parseInt64(countRows[0]["total"])
		}

		rows, err := tp.QueryNative(&cfg.Timeplus, fmt.Sprintf(`
			SELECT to_string(event_time) AS event_time, message_id, session_id, run_id, role,
			       agent_type, agent_id, deployment_id, model, tool_name, content,
			       entities, risk_tags, risk_score, length(embedding) AS embedding_dim
			FROM table(agentguard_dlp_enriched)
			WHERE %s
			ORDER BY event_time DESC
			LIMIT %d OFFSET %d`, where, limit, (page-1)*limit))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		msgs := make([]models.DLPEnrichedMessage, 0, len(rows))
		for _, row := range rows {
			msgs = append(msgs, rowToDLPMessage(row))
		}
		c.JSON(http.StatusOK, gin.H{"messages": msgs, "total": total, "page": page, "limit": limit})
	}
}

// GetEnrichedMessage returns one enriched record by message_id.
func GetEnrichedMessage(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		rows, err := tp.QueryNative(&cfg.Timeplus, fmt.Sprintf(`
			SELECT to_string(event_time) AS event_time, message_id, session_id, run_id, role,
			       agent_type, agent_id, deployment_id, model, tool_name, content,
			       entities, risk_tags, risk_score, length(embedding) AS embedding_dim
			FROM table(agentguard_dlp_enriched)
			WHERE message_id = '%s'
			LIMIT 1`, escapeSQLStr(c.Param("id"))))
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		if len(rows) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "message not found"})
			return
		}
		c.JSON(http.StatusOK, rowToDLPMessage(rows[0]))
	}
}

// DLPProfilesHandler returns top risk agents / sessions / tags over a window.
// GET /api/dlp/profiles?days=30
func DLPProfilesHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
		if days < 1 || days > 365 {
			days = 30
		}
		sqls := buildDLPProfilesSQLs(days)
		out := models.DLPProfiles{WindowDays: days}
		dests := []*[]models.DLPProfileEntry{&out.TopAgents, &out.TopSessions, &out.TopTags}
		for i, sql := range sqls {
			rows, err := tp.Query(&cfg.Timeplus, sql)
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
				return
			}
			for _, row := range rows {
				*dests[i] = append(*dests[i], models.DLPProfileEntry{
					Key:          rowStr(row, "key"),
					FlaggedCount: parseInt64(row["flagged_count"]),
					MaxRiskScore: parseFloat64(row["max_risk_score"]),
					AvgRiskScore: parseFloat64(row["avg_risk_score"]),
					LastSeen:     rowStr(row, "last_seen"),
				})
			}
		}
		c.JSON(http.StatusOK, out)
	}
}

// rowToDLPMessage maps an enriched row (native or HTTP) to the API model.
func rowToDLPMessage(row tp.Row) models.DLPEnrichedMessage {
	entities := map[string][]string{}
	switch v := row["entities"].(type) {
	case map[string][]string:
		entities = v
	case map[string]any:
		for k, raw := range v {
			if arr, ok := raw.([]any); ok {
				var vals []string
				for _, x := range arr {
					if s, ok := x.(string); ok {
						vals = append(vals, s)
					}
				}
				entities[k] = vals
			}
		}
	}
	return models.DLPEnrichedMessage{
		EventTime:    rowStr(row, "event_time"),
		MessageID:    rowStr(row, "message_id"),
		SessionID:    rowStr(row, "session_id"),
		RunID:        rowStr(row, "run_id"),
		Role:         rowStr(row, "role"),
		AgentType:    rowStr(row, "agent_type"),
		AgentID:      rowStr(row, "agent_id"),
		DeploymentID: rowStr(row, "deployment_id"),
		Model:        rowStr(row, "model"),
		ToolName:     rowStr(row, "tool_name"),
		Content:      rowStr(row, "content"),
		Entities:     entities,
		RiskTags:     rowStrSlice(row, "risk_tags"),
		RiskScore:    parseFloat64(row["risk_score"]),
		EmbeddingDim: int(parseInt64(row["embedding_dim"])),
	}
}
```

NOTE: the native driver may scan `entities` as `map[string][]string` and `risk_tags` as `[]string`; the HTTP path produces `map[string]any` / `[]any`. `rowToDLPMessage`/`rowStrSlice` handle both; the live verification in Task 5 confirms the native shapes and the implementer adjusts the type switch if the driver returns something else (e.g. pointers) — keep the adjustment inside these two functions.

- [ ] **Step 4: Register routes** in main.go next to `/dlp/status`:

```go
				protected.GET("/dlp/messages", handlers.ListEnrichedMessages(cfg))
				protected.GET("/dlp/messages/:id", handlers.GetEnrichedMessage(cfg))
				protected.GET("/dlp/profiles", handlers.DLPProfilesHandler(cfg))
```

- [ ] **Step 5: Run** — new tests + whole package + build PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/handlers/dlp.go backend/internal/handlers/dlp_test.go backend/cmd/server/main.go
git commit -m "feat(dlp): enriched-message browser and risk-profile APIs"
```

---

### Task 5: Live end-to-end verification

Same environment as #171's verification: Timeplus Enterprise 3.2.1 docker container (`proton` / `timeplus@t+`, SQL via `echo "SQL" | curl -s -u "proton:timeplus@t+" http://localhost:8123/ --data-binary @-`), mock on `host.docker.internal:4000`, `backend/config.yaml` already carries the dlp section (embedding_dim 8). The server port comes from that config (was 8082).

- [ ] **Step 1:** Start mock (`python3 backend/scripts/dlp_mock_model.py 4000 &`) and server (`cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server > /tmp/ag-server.log 2>&1 &`). Login (`POST /api/auth/login`, proton/timeplus@t+, cookie jar). `POST /api/rules/sync` → log shows all 6 UDFs ✓ (3 ingest + 3 query).
- [ ] **Step 2:** Probe the new UDFs in isolation via SQL: `SELECT dlp_understand_query('who exported customer phones?')` → intent JSON from mock; `SELECT dlp_rerank('q', 'some content')` → 0.95; `SELECT dlp_summarize('q', '[]')` → mock report text.
- [ ] **Step 3:** Seed 4+ varied CIM events (different sessions/agents: one phone-bearing user message, one secret-bearing, one clean assistant message, one tool row) and wait for enrichment.
- [ ] **Step 4:** `curl -sN -X POST http://localhost:<port>/api/dlp/query -b <jar> -H 'Content-Type: application/json' -d '{"question":"who exported customer phone numbers?"}'` → assert NDJSON lines in order: intent (keywords from mock), candidates (count ≥ 1, structured+vector counts), reranked, answer (mock report + evidence array with message_ids, risk_tags, rerank_score). Verify a second query with `"top_n": 1` keeps 1.
- [ ] **Step 5:** Messages API: no-filter list (total ≥ 4), `?risk_tag=PII`, `?entity_type=secret`, `?role=tool`, `?min_score=0.5`, pagination `limit=1&page=2`; `GET /api/dlp/messages/<id>` returns the full record (entities map populated) and 404 for `nope`. Profiles: `GET /api/dlp/profiles?days=7` → top_agents/top_sessions/top_tags populated, window_days 7. **If `array_join` fails on 3.2.1**, adjust `buildDLPProfilesSQLs` tag query to the working syntax (`ARRAY JOIN` clause form: `SELECT tag AS key, ... FROM table(agentguard_dlp_enriched) ARRAY JOIN risk_tags AS tag WHERE ... GROUP BY tag ...`) and update the unit test.
- [ ] **Step 6:** Fail-open: kill the mock, re-run the query → intent falls back (keywords from question), rerank falls back to recall order, answer stage still arrives with the deterministic "Model summary unavailable..." report and evidence. Restart mock.
- [ ] **Step 7:** Kill server + mock (leave docker running). Commit any live-fix adjustments as `fix(dlp): live verification adjustments (<what>)`; commit nothing if none.

---

### Task 6: Final checks and PR

- [ ] **Step 1:** `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...` — all pass. (Local golangci-lint has a known v1-binary/v2-config mismatch; skip.)
- [ ] **Step 2:** Final whole-implementation code review (controller dispatches reviewer over `git diff main..HEAD -- backend/`); fix findings.
- [ ] **Step 3:** Push and open PR:

```bash
git push -u origin feature/172-dlp-query-pipeline
gh pr create --repo timeplus-io/AgentGuard \
  --title "Semantic DLP 2/4: query pipeline" \
  --body "<summary of funnel, APIs, live verification>. Closes #172. Part of #159."
```

---

## Self-review notes (already applied)

- **Spec coverage:** §4 funnel steps 1–5 → Task 3 handler (+UDFs in Task 1); §7 messages/profiles rows of the API table → Task 4; spec's "hybrid single-statement" recall is implemented as two channels (structured + vector) because `cosine_distance` errors on empty embeddings — noted as a deliberate deviation to record in the spec at PR time.
- **Type consistency:** `parseDLPIntent`/`buildDLPRecallSQL`/`mergeDLPCandidates`/`buildDLPRerankSQL`/`rowToDLPEvidence` names match between Tasks 2 and 3; `parseFloat64` is added in Task 2 if absent and used in Tasks 3–4; `mockTimeplus` reused from dlp_test.go.
- **No placeholders:** every step carries full code; the two genuinely server-dependent items (native driver's map/array scan shapes, `array_join` vs `ARRAY JOIN`) have explicit fallback instructions in Tasks 4–5.
