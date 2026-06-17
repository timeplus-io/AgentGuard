package timeplus

import (
	"strings"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
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

func TestUDFParameterization(t *testing.T) {
	cfg := config.DLPConfig{
		BaseURL: "http://x/v1", EmbeddingModel: "e", RiskModel: "r",
		SummaryModel: "s", RequestTimeoutSeconds: 45, MaxOutputTokens: 300,
	}
	cases := map[string]string{
		"risk":       DLPRiskUDFDDL(cfg),
		"understand": DLPUnderstandUDFDDL(cfg),
		"rerank":     DLPRerankUDFDDL(cfg),
		"summarize":  DLPSummarizeUDFDDL(cfg),
	}
	for name, ddl := range cases {
		if !strings.Contains(ddl, "timeout=45") {
			t.Errorf("%s: missing timeout=45", name)
		}
		if !strings.Contains(ddl, "_msg_content") {
			t.Errorf("%s: missing _msg_content fallback helper", name)
		}
	}
	// risk/understand/rerank use the configured 300; summarize floors at 800.
	if !strings.Contains(cases["risk"], "'max_tokens': 300") {
		t.Error("risk: want max_tokens 300")
	}
	if !strings.Contains(cases["summarize"], "'max_tokens': 800") {
		t.Error("summarize: want max_tokens floored to 800")
	}
	// embed only needs the timeout knob.
	if !strings.Contains(DLPEmbedUDFDDL(cfg), "timeout=45") {
		t.Error("embed: missing timeout=45")
	}
}

func TestSummarizeTokenFloorHonorsLargerValue(t *testing.T) {
	cfg := config.DLPConfig{BaseURL: "http://x/v1", SummaryModel: "s",
		RequestTimeoutSeconds: 50, MaxOutputTokens: 1000}
	if !strings.Contains(DLPSummarizeUDFDDL(cfg), "'max_tokens': 1000") {
		t.Error("summarize: want max_tokens 1000 when configured above 800")
	}
}

// Rerank must align scores to items robustly and sink unscored items, so a
// local model can't misrank irrelevant content into the top results.
func TestRerankUDFRobustness(t *testing.T) {
	ddl := DLPRerankUDFDDL(config.DLPConfig{
		BaseURL: "http://x/v1", RiskModel: "r",
		RequestTimeoutSeconds: 50, MaxOutputTokens: 512,
	})
	for _, want := range []string{"CHUNK = 8", "out = [0.0] * n", "str(k + 1)"} {
		if !strings.Contains(ddl, want) {
			t.Errorf("rerank DDL missing %q (alignment/low-default fix not applied)", want)
		}
	}
	if strings.Contains(ddl, "[0.5] * n") {
		t.Error("rerank still defaults unscored items to 0.5; they must sink to 0.0")
	}
	assertCleanDDL(t, "rerank", ddl)
}
