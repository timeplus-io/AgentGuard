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

func TestDLPProfilesEmptyArrays(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tpCfg := mockTimeplus(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	cfg := &config.Config{Timeplus: tpCfg}
	cfg.DLP.Enabled = true

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/dlp/profiles?days=30", nil)

	DLPProfilesHandler(cfg)(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`"top_agents":[]`, `"top_sessions":[]`, `"top_tags":[]`} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q (got null instead of []?):\n%s", want, body)
		}
	}
	var got models.DLPProfiles
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.TopAgents == nil || got.TopSessions == nil || got.TopTags == nil {
		t.Errorf("slices must not be nil: agents=%v sessions=%v tags=%v",
			got.TopAgents, got.TopSessions, got.TopTags)
	}
}

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
	if len(got.UDFs) != 6 {
		t.Errorf("UDFs map should list all 6 tracked UDFs even when disabled, got %d", len(got.UDFs))
	}
}

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
			_, _ = io.WriteString(w, `{"data":[{"name":"dlp_extract_entities"},{"name":"dlp_embed"},{"name":"dlp_risk"},{"name":"dlp_understand_query"},{"name":"dlp_rerank"},{"name":"dlp_summarize"},{"name":"notify_agentguard_udf"}]}`)
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
	for _, fn := range []string{"dlp_extract_entities", "dlp_embed", "dlp_risk", "dlp_understand_query", "dlp_rerank", "dlp_summarize"} {
		if !got.UDFs[fn] {
			t.Errorf("UDF %s should be present", fn)
		}
	}
	if got.TotalRows != 100 || got.EmbeddedRows != 80 || got.FlaggedRows != 7 {
		t.Errorf("rows = total %d, embedded %d, flagged %d (want 100/80/7)", got.TotalRows, got.EmbeddedRows, got.FlaggedRows)
	}
	if got.EmbeddingCoverage < 0.79 || got.EmbeddingCoverage > 0.81 {
		t.Errorf("coverage = %f, want 0.80", got.EmbeddingCoverage)
	}
}
