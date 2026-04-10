package timeplus

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/timeplus/agentshield/internal/config"
)

var protonHTTPClient = &http.Client{Timeout: 15 * time.Second}

// Row is a single result row from a Proton query.
type Row = map[string]any

// Query executes a SELECT SQL statement against the Proton HTTP interface and
// returns all rows. It appends "FORMAT JSON" to the query automatically.
// Returns a wrapped error on non-2xx status or parse failure.
func Query(cfg *config.TimeplusConfig, sql string) ([]Row, error) {
	url := fmt.Sprintf("http://%s:%d/", cfg.Host, cfg.Port)
	body := strings.NewReader(sql + " FORMAT JSON")

	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("proton query: build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	if cfg.User != "" {
		req.SetBasicAuth(cfg.User, cfg.Password)
	}

	resp, err := protonHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proton query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("proton query: status %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		Data      []Row  `json:"data"`
		Exception string `json:"exception"`
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("proton query: decode: %w", err)
	}
	if result.Exception != "" {
		return nil, fmt.Errorf("proton query: %s", result.Exception)
	}
	return result.Data, nil
}

// ExecHTTP executes a DDL or DML statement against the Proton HTTP interface.
// Use for CREATE/DROP/PAUSE/RESUME statements from handler code.
func ExecHTTP(cfg *config.TimeplusConfig, sql string) error {
	url := fmt.Sprintf("http://%s:%d/", cfg.Host, cfg.Port)
	body := strings.NewReader(sql)

	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("proton exec: build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	if cfg.User != "" {
		req.SetBasicAuth(cfg.User, cfg.Password)
	}

	resp, err := protonHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("proton exec: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("proton exec: status %d: %s", resp.StatusCode, string(respBody))
	}
	// Proton may return HTTP 200 with {"exception":"..."} for runtime errors.
	var result struct {
		Exception string `json:"exception"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && result.Exception != "" {
		return fmt.Errorf("proton exec: %s", result.Exception)
	}
	return nil
}
