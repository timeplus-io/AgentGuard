package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
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

	// Multibyte truncation: 200 CJK runes = 600 bytes; must stay ≤500 bytes and be valid UTF-8.
	row2 := tp.Row{}
	for k, v := range row {
		row2[k] = v
	}
	row2["content"] = strings.Repeat("世", 200) // 600 bytes, 200 runes
	ev2 := rowToDLPEvidence(row2, 0.1)
	if len(ev2.Content) > 500 || !utf8.ValidString(ev2.Content) {
		t.Errorf("multibyte truncation broken: len=%d valid=%v", len(ev2.Content), utf8.ValidString(ev2.Content))
	}
}

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
