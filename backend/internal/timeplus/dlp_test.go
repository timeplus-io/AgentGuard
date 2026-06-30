package timeplus

import (
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
		"entities":   ddlDLPEntitiesUDF,
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
		if name == "entities" {
			continue // network-free; takes only (content, config), no shared/merge
		}
		if !strings.Contains(ddl, "shared string, config string") {
			t.Errorf("%s: UDF signature missing trailing 'shared string, config string' params", name)
		}
		if !strings.Contains(ddl, "_merged(shareds, configs)") {
			t.Errorf("%s: UDF missing shared⊕config merge", name)
		}
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
	// Model catalog: names → actual model ids, defined once.
	for _, want := range []string{`"models":{`, `"chat":"gpt-4o-mini"`, `"embedding":"text-embedding-3-small"`, `"summary":"gpt-4o"`} {
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
		"CREATE OR REPLACE FUNCTION dlp_embed(content string, role string, shared string, config string)",
		"RETURNS array(float32)", "LANGUAGE PYTHON", "/embeddings",
	} {
		if !strings.Contains(ddlDLPEmbedUDF, want) {
			t.Errorf("embed UDF missing %q", want)
		}
	}
}

func TestDLPRiskUDFSignature(t *testing.T) {
	for _, want := range []string{
		"CREATE OR REPLACE FUNCTION dlp_risk(content string, entity_types array(string), shared string, config string)",
		"RETURNS string", "/chat/completions",
	} {
		if !strings.Contains(ddlDLPRiskUDF, want) {
			t.Errorf("risk UDF missing %q", want)
		}
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
		"dlp_risk(content, map_keys(entities), cfg_shared, cfg_risk)",
		"dlp_embed(content, role, cfg_shared, cfg_embed)",
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
