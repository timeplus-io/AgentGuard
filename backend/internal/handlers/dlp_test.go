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
