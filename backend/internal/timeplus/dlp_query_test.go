package timeplus

import (
	"strings"
	"testing"
)

// The query-funnel UDFs are static constants taking a trailing config param.
// They must declare the right signatures and carry their TASK markers (the mock
// model dispatches on those), with no baked endpoint.
func TestDLPQueryUDFSignatures(t *testing.T) {
	cases := []struct {
		name string
		ddl  string
		want []string
	}{
		{"understand", ddlDLPUnderstandUDF, []string{
			"CREATE OR REPLACE FUNCTION dlp_understand_query(question string, shared string, config string)",
			"RETURNS string", "LANGUAGE PYTHON", "TASK: QUERY_UNDERSTANDING",
		}},
		{"rerank", ddlDLPRerankUDF, []string{
			"CREATE OR REPLACE FUNCTION dlp_rerank(question string, content string, shared string, config string)",
			"RETURNS float64", "TASK: RERANK",
		}},
		{"summarize", ddlDLPSummarizeUDF, []string{
			"CREATE OR REPLACE FUNCTION dlp_summarize(question string, evidence string, shared string, config string)",
			"RETURNS string", "TASK: AUDIT_SUMMARY",
		}},
	}
	for _, tc := range cases {
		assertCleanDDL(t, tc.name, tc.ddl)
		for _, want := range tc.want {
			if !strings.Contains(tc.ddl, want) {
				t.Errorf("%s DDL missing %q", tc.name, want)
			}
		}
	}
}

// Each network funnel UDF must parse model/endpoint/timeout/tokens from config
// and keep the _msg_content fallback helper.
func TestDLPQueryUDFsParseConfig(t *testing.T) {
	for name, ddl := range map[string]string{
		"understand": ddlDLPUnderstandUDF,
		"rerank":     ddlDLPRerankUDF,
		"summarize":  ddlDLPSummarizeUDF,
	} {
		for _, want := range []string{
			"_merged(shareds, configs)", "cfg.get('base_url')", "cfg.get('model')",
			"cfg.get('timeout')", "_msg_content", "timeout=timeout",
			"base, api_key, model = _endpoint(cfg)", // per-model endpoint resolution
			"entry.get('base_url')",                 // catalog entry can override base_url
		} {
			if !strings.Contains(ddl, want) {
				t.Errorf("%s: missing %q", name, want)
			}
		}
	}
}

// Rerank must align scores to items robustly and sink unscored items, so a
// local model can't misrank irrelevant content into the top results.
func TestRerankUDFRobustness(t *testing.T) {
	ddl := ddlDLPRerankUDF
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
