package timeplus

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

func testDLPCfg() config.DLPConfig {
	return config.DLPConfig{
		Enabled:               true,
		BaseURL:               "http://mock:9999/v1",
		APIKey:                "sk-test-abc",
		EmbeddingModel:        "text-embedding-3-small",
		EmbeddingDim:          1536,
		RiskModel:             "gpt-4o-mini",
		SummaryModel:          "gpt-4o",
		RequestTimeoutSeconds: 50,
		MaxOutputTokens:       512,
	}
}

// assertCleanDDL fails on leftover fmt verbs or unreplaced {{...}} template
// placeholders. The UDF bodies are now static constants, so neither should ever
// appear.
func assertCleanDDL(t *testing.T, name, ddl string) {
	t.Helper()
	if strings.Contains(ddl, "%!") {
		t.Errorf("%s: fmt interpolation error: %s", name, ddl)
	}
	if strings.Contains(ddl, "{{") || strings.Contains(ddl, "}}") {
		t.Errorf("%s: unreplaced template placeholder in DDL", name)
	}
}

// allDLPUDFs lists every UDF body constant for shared invariants.
func allDLPUDFs() map[string]string {
	return map[string]string{
		"resolve":    ddlDLPConfigResolveUDF,
		"entities":   ddlDLPEntitiesUDF,
		"embed":      ddlDLPEmbedUDF,
		"risk":       ddlDLPRiskUDF,
		"understand": ddlDLPUnderstandUDF,
		"rerank":     ddlDLPRerankUDF,
		"summarize":  ddlDLPSummarizeUDF,
	}
}

// networkDLPUDFs are the UDFs that call the model endpoint. Each now takes a
// SINGLE resolved `config` param (dlp_config_resolve folds shared⊕own before the
// call) — no `shared` param, no in-body merge/catalog walk.
func networkDLPUDFs() map[string]string {
	return map[string]string{
		"embed":      ddlDLPEmbedUDF,
		"risk":       ddlDLPRiskUDF,
		"understand": ddlDLPUnderstandUDF,
		"rerank":     ddlDLPRerankUDF,
		"summarize":  ddlDLPSummarizeUDF,
	}
}

// UDF bodies must be fully static: no interpolation markers, and crucially no
// baked API key or base URL (those now arrive via the config parameter, so they
// no longer leak through SHOW FUNCTIONS).
func TestDLPUDFsAreStaticAndKeyless(t *testing.T) {
	for name, ddl := range allDLPUDFs() {
		assertCleanDDL(t, name, ddl)
		if strings.Contains(ddl, "API_KEY =") || strings.Contains(ddl, "BASE_URL =") {
			t.Errorf("%s: UDF body bakes endpoint/key; must read from config param", name)
		}
	}
	// Network UDFs take one resolved config param and never see `shared` or the
	// model catalog — the merge/resolution lives only in dlp_config_resolve.
	for name, ddl := range networkDLPUDFs() {
		if strings.Contains(ddl, "shared string, config string") {
			t.Errorf("%s: network UDF must not take a `shared` param; config is pre-resolved", name)
		}
		if strings.Contains(ddl, "_merged(") || strings.Contains(ddl, "_endpoint(") {
			t.Errorf("%s: merge/endpoint resolution must move to dlp_config_resolve", name)
		}
		if !strings.Contains(ddl, "_cfg(configs)") {
			t.Errorf("%s: network UDF should read its single config via _cfg(configs)", name)
		}
	}
}

// dlp_config_resolve is the ONE place shared⊕own merge + model-catalog resolution
// lives. It must merge, resolve the model reference (incl. per-entry endpoint
// overrides), and strip the `models` catalog so the resolved config carries only
// what a single UDF runs with.
func TestDLPConfigResolveUDF(t *testing.T) {
	ddl := ddlDLPConfigResolveUDF
	assertCleanDDL(t, "resolve", ddl)
	for _, want := range []string{
		"CREATE OR REPLACE FUNCTION dlp_config_resolve(shared string, config string)",
		"RETURNS string", "LANGUAGE PYTHON",
		"cfg.update(_parse(config_s))", // shared ⊕ own, own wins
		"entry.get('base_url')",        // per-model endpoint override honored
		"cfg.pop('models', None)",      // catalog stripped from output
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("resolve UDF missing %q", want)
		}
	}
	// Pure: it must not make network calls.
	if strings.Contains(ddl, "import requests") || strings.Contains(ddl, "requests.post") {
		t.Error("dlp_config_resolve must be pure (no network)")
	}
}

func TestDLPConfigStreamDDL(t *testing.T) {
	for _, want := range []string{
		"CREATE MUTABLE STREAM IF NOT EXISTS agentguard_dlp_config",
		"key        string",
		"value      string",
		"PRIMARY KEY (key)",
	} {
		if !strings.Contains(ddlDLPConfigStream, want) {
			t.Errorf("config stream DDL missing %q", want)
		}
	}
}

func TestDLPConfigRows(t *testing.T) {
	rows := DLPConfigRows(testDLPCfg())
	for _, k := range []string{
		DLPCfgKeyShared, DLPCfgKeyEntities, DLPCfgKeyEmbed, DLPCfgKeyRisk,
		DLPCfgKeyUnderstand, DLPCfgKeyRerank, DLPCfgKeySummarize,
	} {
		if rows[k] == "" {
			t.Fatalf("config rows missing key %q", k)
		}
	}
	// Endpoint/key/timeout + the named-model catalog live ONLY in the shared row.
	if !strings.Contains(rows[DLPCfgKeyShared], `"api_key":"sk-test-abc"`) ||
		!strings.Contains(rows[DLPCfgKeyShared], `"base_url":"http://mock:9999/v1"`) ||
		!strings.Contains(rows[DLPCfgKeyShared], `"timeout":50`) {
		t.Errorf("shared row missing endpoint/key/timeout: %s", rows[DLPCfgKeyShared])
	}
	// Model catalog: names → endpoint objects (each can carry its own base_url/
	// api_key), defined once.
	for _, want := range []string{`"models":{`, `"chat":{"model":"gpt-4o-mini"}`, `"embedding":{"model":"text-embedding-3-small"}`, `"summary":{"model":"gpt-4o"}`} {
		if !strings.Contains(rows[DLPCfgKeyShared], want) {
			t.Errorf("shared row missing model catalog %q: %s", want, rows[DLPCfgKeyShared])
		}
	}
	// Per-UDF rows must NOT duplicate the shared endpoint/key/catalog.
	for _, k := range []string{DLPCfgKeyEmbed, DLPCfgKeyRisk, DLPCfgKeyUnderstand, DLPCfgKeyRerank, DLPCfgKeySummarize} {
		if strings.Contains(rows[k], "api_key") || strings.Contains(rows[k], "base_url") || strings.Contains(rows[k], `"models"`) {
			t.Errorf("%s row should not carry shared endpoint/catalog: %s", k, rows[k])
		}
	}
	// Per-UDF model is a NAME referencing the shared catalog (resolved at call time).
	if !strings.Contains(rows[DLPCfgKeyEmbed], `"model":"embedding"`) {
		t.Error("embed row: model should reference catalog name 'embedding'")
	}
	if !strings.Contains(rows[DLPCfgKeyEmbed], `"max_chars":8000`) {
		t.Error("embed row: missing max_chars")
	}
	if !strings.Contains(rows[DLPCfgKeyRisk], `"model":"chat"`) {
		t.Error("risk row: model should reference catalog name 'chat'")
	}
	if !strings.Contains(rows[DLPCfgKeyRisk], `"keywords":[`) || !strings.Contains(rows[DLPCfgKeyRisk], `"tag_for":{`) {
		t.Error("risk row: missing keywords/tag_for")
	}
	if !strings.Contains(rows[DLPCfgKeyUnderstand], `"model":"chat"`) {
		t.Error("understand row: should reference catalog name 'chat'")
	}
	if !strings.Contains(rows[DLPCfgKeySummarize], `"model":"summary"`) {
		t.Error("summarize row: should reference catalog name 'summary'")
	}
	// Token budgets: risk uses the configured 512; summarize floors at 800.
	if !strings.Contains(rows[DLPCfgKeyRisk], `"max_tokens":512`) {
		t.Error("risk row: want max_tokens 512")
	}
	if !strings.Contains(rows[DLPCfgKeySummarize], `"max_tokens":800`) {
		t.Error("summarize row: want max_tokens floored to 800")
	}
	// Entity catalog preserves today's 5 types incl. masked secret.
	ent := rows[DLPCfgKeyEntities]
	for _, want := range []string{
		`"max_per_type":20`, `"type":"phone"`, `"type":"email"`, `"type":"ip"`,
		`"type":"domain"`, `"type":"secret"`, `"store":"masked"`, `AKIA[0-9A-Z]{16}`,
	} {
		if !strings.Contains(ent, want) {
			t.Errorf("entities row missing %q", want)
		}
	}
	// Each LLM UDF row seeds a tunable prompt template carrying its placeholders (#197).
	for key, phs := range map[string][]string{
		DLPCfgKeyRisk:       {"{entity_types}", "{message}"},
		DLPCfgKeyUnderstand: {"{question}"},
		DLPCfgKeyRerank:     {"{count}", "{question}", "{items}"},
		DLPCfgKeySummarize:  {"{question}", "{evidence}"},
	} {
		if !strings.Contains(rows[key], `"prompt":"`) {
			t.Errorf("%s row: missing seeded prompt", key)
		}
		for _, ph := range phs {
			if !strings.Contains(rows[key], ph) {
				t.Errorf("%s row: prompt missing placeholder %s", key, ph)
			}
		}
	}
	// Non-LLM rows carry no prompt.
	for _, key := range []string{DLPCfgKeyShared, DLPCfgKeyEntities, DLPCfgKeyEmbed} {
		if strings.Contains(rows[key], `"prompt"`) {
			t.Errorf("%s row should not carry a prompt", key)
		}
	}
}

// The Go seed constants must be byte-identical to the DEFAULT_PROMPT fallback
// inside each UDF body (with real newlines normalized to the Python \n escape),
// so "row has default prompt" and "row has no prompt" behave the same.
func TestDLPDefaultPromptsMatchUDFs(t *testing.T) {
	for name, tc := range map[string]struct{ seed, ddl string }{
		"risk":       {dlpDefaultRiskPrompt, ddlDLPRiskUDF},
		"understand": {dlpDefaultUnderstandPrompt, ddlDLPUnderstandUDF},
		"rerank":     {dlpDefaultRerankPrompt, ddlDLPRerankUDF},
		"summarize":  {dlpDefaultSummarizePrompt, ddlDLPSummarizeUDF},
	} {
		pythonLiteral := "DEFAULT_PROMPT = '" + strings.ReplaceAll(tc.seed, "\n", `\n`) + "'"
		if !strings.Contains(tc.ddl, pythonLiteral) {
			t.Errorf("%s: UDF DEFAULT_PROMPT is not byte-identical to the Go seed", name)
		}
	}
}

func TestDLPSummarizeTokenFloorHonorsLargerValue(t *testing.T) {
	cfg := testDLPCfg()
	cfg.MaxOutputTokens = 1000
	rows := DLPConfigRows(cfg)
	if !strings.Contains(rows[DLPCfgKeySummarize], `"max_tokens":1000`) {
		t.Error("summarize: want max_tokens 1000 when configured above 800")
	}
}

func TestDLPTagForFromCatalog(t *testing.T) {
	tagFor := dlpTagForFromCatalog(defaultDLPEntities())
	for k, v := range map[string]string{
		"phone": "PHONE", "email": "EMAIL", "secret": "CREDENTIAL",
		"person": "PERSON", "company": "COMPANY",
	} {
		if tagFor[k] != v {
			t.Errorf("tag_for[%q] = %q, want %q", k, tagFor[k], v)
		}
	}
}

func TestDLPEntitiesUDFSignature(t *testing.T) {
	if !strings.Contains(ddlDLPEntitiesUDF, "CREATE OR REPLACE FUNCTION dlp_extract_entities(content string, config string)") {
		t.Error("entities UDF signature must be (content string, config string)")
	}
	if !strings.Contains(ddlDLPEntitiesUDF, "map(string, array(string))") {
		t.Error("entities UDF must return map(string, array(string))")
	}
}

func TestDLPEmbedUDFSignature(t *testing.T) {
	for _, want := range []string{
		"CREATE OR REPLACE FUNCTION dlp_embed(content string, role string, config string)",
		"RETURNS array(float32)", "LANGUAGE PYTHON", "/embeddings",
	} {
		if !strings.Contains(ddlDLPEmbedUDF, want) {
			t.Errorf("embed UDF missing %q", want)
		}
	}
}

func TestDLPRiskUDFSignature(t *testing.T) {
	for _, want := range []string{
		"CREATE OR REPLACE FUNCTION dlp_risk(content string, entity_types array(string), config string)",
		"RETURNS string", "/chat/completions",
		// Tunable prompt (#197): config prompt with fallback, single-pass render.
		"cfg.get('prompt') or DEFAULT_PROMPT",
		`re.sub(r'\{(entity_types|message)\}'`,
	} {
		if !strings.Contains(ddlDLPRiskUDF, want) {
			t.Errorf("risk UDF missing %q", want)
		}
	}
	if strings.Contains(ddlDLPRiskUDF, "prompt = ('") {
		t.Error("risk UDF still builds its prompt inline; must render the config template")
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

// The MV must call each UDF with its config column and LEFT JOIN the config
// stream so runtime upserts take effect live without dropping events.
func TestDLPMVReferencesConfigJoins(t *testing.T) {
	for _, want := range []string{
		"CREATE MATERIALIZED VIEW IF NOT EXISTS mv_dlp_enrich",
		"INTO agentguard_dlp_enriched",
		"dlp_extract_entities(content, cfg_entities)",
		"dlp_risk(content, map_keys(entities), dlp_config_resolve(cfg_shared, cfg_risk))",
		"dlp_embed(content, role, dlp_config_resolve(cfg_shared, cfg_embed))",
		"LEFT JOIN agentguard_dlp_config AS cs ON base.k_shared = cs.key",
		"LEFT JOIN agentguard_dlp_config AS ce ON base.k_ent = ce.key",
		"LEFT JOIN agentguard_dlp_config AS cr ON base.k_risk = cr.key",
		"LEFT JOIN agentguard_dlp_config AS cm ON base.k_emb = cm.key",
		"coalesce(cs.value, '') AS cfg_shared",
		"coalesce(ce.value, '') AS cfg_entities",
		"'user_input'", "'llm_response'", "'tool_complete'",
	} {
		if !strings.Contains(ddlDLPEnrichMV, want) {
			t.Errorf("MV DDL missing %q", want)
		}
	}
}

// pythonBody extracts the Python source between the $$ delimiters of a UDF DDL.
func pythonBody(t *testing.T, ddl string) string {
	t.Helper()
	start := strings.Index(ddl, "$$")
	end := strings.LastIndex(ddl, "$$")
	if start < 0 || end <= start {
		t.Fatal("DDL has no $$-delimited Python body")
	}
	return ddl[start+2 : end]
}

// bytesArgsDriver executes a UDF body with every string leaf of the argument
// columns encoded to bytes — the shape timeplusd ≥3.2.2 delivers (str before
// that). `requests` is stubbed: post returns the canned `resp` payload, or
// raises when resp is null so the no-network fallback paths run.
const bytesArgsDriver = `
import json, sys, types

spec = json.loads(sys.stdin.read())

class _Resp:
    def __init__(self, payload): self._p = payload
    def json(self): return self._p

def _post(*a, **k):
    if spec['resp'] is None:
        raise RuntimeError('network disabled')
    # requests would utf-8 encode json= payloads; reject bytes like it does.
    json.dumps(k.get('json'))
    return _Resp(spec['resp'])

req = types.ModuleType('requests')
req.post = _post
sys.modules['requests'] = req

def _b(v):
    if isinstance(v, str): return v.encode('utf-8')
    if isinstance(v, list): return [_b(x) for x in v]
    return v

g = {}
exec(compile(spec['body'], '<udf>', 'exec'), g)
print(json.dumps(g[spec['fn']](*[_b(a) for a in spec['args']])))
`

// timeplusd ≥3.2.2 marshals string columns (and array(string) elements) to
// Python UDFs as bytes, not str — proton-enterprise #11788 flipped
// PyUnicode→PyBytes for binary safety; 3.2.1 delivered str. The bodies must
// behave identically under both, so every string arg is normalized at entry.
// These cases run the real bodies under python3 with bytes args and assert
// str-parity results.
func TestDLPUDFsDecodeBytesStringArgs(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	driver := filepath.Join(t.TempDir(), "driver.py")
	if err := os.WriteFile(driver, []byte(bytesArgsDriver), 0o600); err != nil {
		t.Fatal(err)
	}

	run := func(t *testing.T, ddl, fn string, args []any, resp any) []any {
		t.Helper()
		spec, err := json.Marshal(map[string]any{
			"body": pythonBody(t, ddl), "fn": fn, "args": args, "resp": resp,
		})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(py, driver)
		cmd.Stdin = bytes.NewReader(spec)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s raised on bytes args:\n%s", fn, out)
		}
		var got []any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("%s: bad driver output %q: %v", fn, out, err)
		}
		return got
	}

	chatResp := func(content string) any {
		return map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}}
	}

	t.Run("extract_entities matches patterns in bytes content", func(t *testing.T) {
		cfg := `{"max_per_type":20,"entities":[{"type":"email","kind":"regex","pattern":"[a-z0-9._%+-]+@[a-z0-9.-]+\\.[a-z]{2,}","flags":"i"}]}`
		got := run(t, ddlDLPEntitiesUDF, "dlp_extract_entities",
			[]any{[]any{"contact bob@example.com today"}, []any{cfg}}, nil)
		ents, _ := got[0].(map[string]any)
		emails, _ := ents["email"].([]any)
		if len(emails) != 1 || emails[0] != "bob@example.com" {
			t.Errorf("want email [bob@example.com], got %v", got[0])
		}
	})

	t.Run("config_resolve resolves bytes rows", func(t *testing.T) {
		got := run(t, ddlDLPConfigResolveUDF, "dlp_config_resolve",
			[]any{
				[]any{`{"base_url":"http://shared/v1","api_key":"k1","models":{"chat":{"model":"m1"}}}`},
				[]any{`{"model":"chat","max_tokens":64}`},
			}, nil)
		var cfg map[string]any
		if err := json.Unmarshal([]byte(got[0].(string)), &cfg); err != nil {
			t.Fatalf("resolved config not JSON: %v", err)
		}
		if cfg["model"] != "m1" || cfg["base_url"] != "http://shared/v1" {
			t.Errorf("want model=m1 base_url=http://shared/v1, got %v", cfg)
		}
	})

	t.Run("embed gates on bytes role and posts decoded input", func(t *testing.T) {
		resp := map[string]any{"data": []any{map[string]any{"embedding": []any{0.5, 0.25}}}}
		got := run(t, ddlDLPEmbedUDF, "dlp_embed",
			[]any{
				[]any{"hello world"},
				[]any{"user"},
				[]any{`{"base_url":"http://m/v1","model":"e","timeout":5}`},
			}, resp)
		emb, _ := got[0].([]any)
		if len(emb) != 2 || emb[0] != 0.5 {
			t.Errorf("want embedding [0.5 0.25] for bytes user row, got %v", got[0])
		}
	})

	t.Run("risk heuristic handles bytes content and entity types", func(t *testing.T) {
		got := run(t, ddlDLPRiskUDF, "dlp_risk",
			[]any{
				[]any{"my password is hunter2", "attached the customer list"},
				[]any{[]any{}, []any{"email"}},
				[]any{"", ""},
			}, nil)
		var r0, r1 map[string]any
		if err := json.Unmarshal([]byte(got[0].(string)), &r0); err != nil {
			t.Fatalf("row0 not JSON: %v (%v)", err, got[0])
		}
		if s, _ := r0["score"].(float64); s <= 0 {
			t.Errorf("keyword bytes content should score > 0, got %v", got[0])
		}
		if err := json.Unmarshal([]byte(got[1].(string)), &r1); err != nil {
			t.Fatalf("row1 not JSON: %v (%v)", err, got[1])
		}
		tags, _ := json.Marshal(r1["tags"])
		if !strings.Contains(string(tags), "EMAIL") || !strings.Contains(string(tags), "PII") {
			t.Errorf("bytes entity type should map to EMAIL+PII tags, got %v", got[1])
		}
	})

	t.Run("understand fallback parses bytes question", func(t *testing.T) {
		got := run(t, ddlDLPUnderstandUDF, "dlp_understand_query",
			[]any{[]any{"Who sent customer email data outside?"}, []any{""}}, nil)
		var intent map[string]any
		if err := json.Unmarshal([]byte(got[0].(string)), &intent); err != nil {
			t.Fatalf("intent not JSON: %v (%v)", err, got[0])
		}
		kw, _ := json.Marshal(intent["keywords"])
		ets, _ := json.Marshal(intent["entity_types"])
		if !strings.Contains(string(kw), "customer") || !strings.Contains(string(ets), "email") {
			t.Errorf("fallback should extract keywords/entity types from bytes question, got %v", got[0])
		}
	})

	t.Run("rerank scores bytes snippets", func(t *testing.T) {
		cfg := `{"base_url":"http://m/v1","model":"c","timeout":5}`
		got := run(t, ddlDLPRerankUDF, "dlp_rerank",
			[]any{
				[]any{"who leaked data", "who leaked data"},
				[]any{"snippet a", "snippet b"},
				[]any{cfg, cfg},
			}, chatResp(`{"1": 0.9, "2": 0.1}`))
		if got[0] != 0.9 || got[1] != 0.1 {
			t.Errorf("want [0.9 0.1], got %v", got)
		}
	})

	t.Run("summarize renders bytes question and evidence", func(t *testing.T) {
		got := run(t, ddlDLPSummarizeUDF, "dlp_summarize",
			[]any{
				[]any{"who leaked?"},
				[]any{`[{"message_id":"m1"}]`},
				[]any{`{"base_url":"http://m/v1","model":"c","timeout":5}`},
			}, chatResp("REPORT [m1]"))
		if got[0] != "REPORT [m1]" {
			t.Errorf("want report text, got %v", got)
		}
	})
}
