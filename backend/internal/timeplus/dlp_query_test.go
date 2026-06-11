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
