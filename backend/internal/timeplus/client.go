package timeplus

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/timeplus/agentshield/internal/config"
)

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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proton query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("proton query: status %d: %s", resp.StatusCode, string(errBody))
	}

	var result struct {
		Data []Row `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("proton query: decode: %w", err)
	}
	return result.Data, nil
}
