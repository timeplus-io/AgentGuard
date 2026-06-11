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

// assertCleanDDL fails on leftover fmt verbs, failed interpolation markers,
// or unreplaced {{...}} template placeholders.
func assertCleanDDL(t *testing.T, name, ddl string) {
	t.Helper()
	if strings.Contains(ddl, "%!") {
		t.Errorf("%s: fmt interpolation error: %s", name, ddl)
	}
	if strings.Contains(ddl, "%s") || strings.Contains(ddl, "%d") {
		t.Errorf("%s: leftover fmt verb in DDL", name)
	}
	if strings.Contains(ddl, "{{") || strings.Contains(ddl, "}}") {
		t.Errorf("%s: unreplaced template placeholder in DDL", name)
	}
}

func TestDLPUDFDDLPercentSafe(t *testing.T) {
	cfg := testDLPCfg()
	cfg.APIKey = "sk-100%weird%2Fkey"
	for name, ddl := range map[string]string{
		"embed":      DLPEmbedUDFDDL(cfg),
		"risk":       DLPRiskUDFDDL(cfg),
		"understand": DLPUnderstandUDFDDL(cfg),
		"rerank":     DLPRerankUDFDDL(cfg),
		"summarize":  DLPSummarizeUDFDDL(cfg),
	} {
		assertCleanDDL(t, name, ddl)
		if !strings.Contains(ddl, `API_KEY = "sk-100%weird%2Fkey"`) {
			t.Errorf("%s: percent-bearing API key not interpolated verbatim", name)
		}
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
