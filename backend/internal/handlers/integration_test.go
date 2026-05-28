//go:build integration

package handlers_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"
)

// Integration tests run against a real timeplusd instance.
// They are excluded from normal `go test ./...` runs and only
// execute when explicitly requested via `go test -tags integration`.
//
// Environment variables (set by CI or manually):
//   AGENTGUARD_URL       — base URL of a running AgentGuard server (default http://localhost:8080)
//   TIMEPLUS_HOST        — timeplusd host (default localhost)
//   TIMEPLUS_PORT        — timeplusd HTTP port (default 3218)
//   TIMEPLUS_NATIVE_PORT — timeplusd native TCP port (default 8463)
//   TIMEPLUS_USER        — timeplusd user (default "default")
//   TIMEPLUS_PASSWORD    — timeplusd password (default "")

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var baseURL = envOr("AGENTGUARD_URL", "http://localhost:8080")

// TestIntegration_Provision provisions AgentGuard against timeplusd,
// then tests all major endpoints that execute SQL queries.
func TestIntegration_Provision(t *testing.T) {
	client := newClient(t)

	waitForServer(t, client, 30*time.Second)

	// Provision — creates all streams and MVs.
	provisionBody := fmt.Sprintf(`{
		"host": "%s",
		"port": %s,
		"native_port": %s,
		"username": "%s",
		"password": "%s"
	}`,
		envOr("TIMEPLUS_HOST", "localhost"),
		envOr("TIMEPLUS_PORT", "3218"),
		envOr("TIMEPLUS_NATIVE_PORT", "8463"),
		envOr("TIMEPLUS_USER", "default"),
		envOr("TIMEPLUS_PASSWORD", ""),
	)

	resp := doPost(t, client, "/api/setup/provision", provisionBody)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		t.Fatalf("provision: status %d, body: %s", resp.StatusCode, body)
	}

	// Login to get a session cookie.
	loginBody := fmt.Sprintf(`{"username":"%s","password":"%s"}`,
		envOr("TIMEPLUS_USER", "default"),
		envOr("TIMEPLUS_PASSWORD", ""),
	)
	resp = doPost(t, client, "/api/auth/login", loginBody)
	assertStatus(t, "login", resp, http.StatusOK)

	// ── Read endpoints (every handler that runs SQL) ─────────────────────

	for _, tc := range []struct {
		name string
		path string
	}{
		{"dashboard", "/api/dashboard"},
		{"agents", "/api/agents"},
		{"threats", "/api/threats"},
		{"cost", "/api/cost"},
		{"cost/budget", "/api/cost/budget"},
		{"cost/pricing", "/api/cost/pricing"},
		{"metrics/llm", "/api/metrics/llm"},
		{"metrics/tools", "/api/metrics/tools"},
		{"rules", "/api/rules"},
		{"rules/catalog", "/api/rules/catalog"},
		{"activity", "/api/activity"},
		{"holds", "/api/holds"},
		{"investigate/tables", "/api/investigate/tables"},
	} {
		t.Run("GET "+tc.name, func(t *testing.T) {
			assertEndpointOK(t, client, "GET", tc.path)
		})
	}

	// ── Write endpoints ──────────────────────────────────────────────────

	t.Run("POST cost/pricing", func(t *testing.T) {
		resp := doPost(t, client, "/api/cost/pricing", `{
			"provider": "integration-test",
			"model": "test-model",
			"input_cost_per_million": 3.0,
			"output_cost_per_million": 15.0,
			"cache_read_cost_per_million": 0.3
		}`)
		assertStatus(t, "upsert pricing", resp, http.StatusOK)
	})

	t.Run("POST cost/budget", func(t *testing.T) {
		resp := doPost(t, client, "/api/cost/budget", `{
			"deployment_id": "integration-test",
			"monthly_budget": 100.0,
			"currency": "USD"
		}`)
		assertStatus(t, "upsert budget", resp, http.StatusOK)
	})

	t.Run("GET cost/budget after upsert", func(t *testing.T) {
		// Append-only stream needs a moment for the row to be visible via arg_max.
		time.Sleep(2 * time.Second)
		resp := doGet(t, client, "/api/cost/budget")
		assertStatus(t, "get budget after upsert", resp, http.StatusOK)
		body := readBody(t, resp)
		if !strings.Contains(body, "integration-test") {
			t.Fatalf("budget response missing integration-test: %s", body)
		}
	})

	t.Run("DELETE cost/pricing", func(t *testing.T) {
		resp := doReq(t, client, "DELETE", "/api/cost/pricing/integration-test/test-model", "")
		assertStatus(t, "delete pricing", resp, http.StatusOK)
	})
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &http.Client{Jar: jar, Timeout: 30 * time.Second}
}

func waitForServer(t *testing.T, client *http.Client, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/api/auth/status")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("server at %s not reachable after %v", baseURL, timeout)
}

func doGet(t *testing.T, client *http.Client, path string) *http.Response {
	t.Helper()
	return doReq(t, client, "GET", path, "")
}

func doPost(t *testing.T, client *http.Client, path, body string) *http.Response {
	t.Helper()
	return doReq(t, client, "POST", path, body)
}

func doReq(t *testing.T, client *http.Client, method, path, body string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, baseURL+path, bodyReader)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func assertStatus(t *testing.T, label string, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body := readBody(t, resp)
		t.Fatalf("%s: status %d (want %d), body: %s", label, resp.StatusCode, want, body)
	}
}

func assertEndpointOK(t *testing.T, client *http.Client, method, path string) {
	t.Helper()
	resp := doReq(t, client, method, path, "")
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: status %d, body: %s", method, path, resp.StatusCode, body)
	}
	if len(body) > 0 && !json.Valid([]byte(body)) {
		t.Fatalf("%s %s: response is not valid JSON: %s", method, path, body)
	}
}
