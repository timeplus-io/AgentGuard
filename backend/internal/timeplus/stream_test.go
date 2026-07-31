package timeplus

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

func streamTestCfg(t *testing.T, rawURL string) config.TimeplusConfig {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return config.TimeplusConfig{Host: u.Hostname(), Port: port}
}

func TestStreamLinesDeliversNonEmptyLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{\"a\":1}\n\n{\"a\":2}\n"))
	}))
	defer srv.Close()
	cfg := streamTestCfg(t, srv.URL)

	var connected bool
	var lines []string
	err := StreamLines(context.Background(), &cfg, "SELECT 1",
		func() { connected = true },
		func(line []byte) error { lines = append(lines, string(line)); return nil })
	if err != nil {
		t.Fatalf("StreamLines: %v", err)
	}
	if !connected {
		t.Error("onConnected was not called")
	}
	if len(lines) != 2 || lines[0] != `{"a":1}` || lines[1] != `{"a":2}` {
		t.Errorf("lines = %v, want the two non-empty rows", lines)
	}
}

func TestStreamLinesNon200ReturnsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := streamTestCfg(t, srv.URL)

	err := StreamLines(context.Background(), &cfg, "SELECT 1",
		func() { t.Error("onConnected must not fire on non-200") },
		func([]byte) error { t.Error("onLine must not fire on non-200"); return nil })
	httpErr, ok := err.(*StreamHTTPError)
	if !ok {
		t.Fatalf("err = %v (%T), want *StreamHTTPError", err, err)
	}
	if httpErr.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d, want 500", httpErr.Status)
	}
}
