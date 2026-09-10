package timeplus

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/timeplus/agentguard/internal/config"
)

// StreamHTTPError is returned by StreamLines when Timeplus answers with a
// non-200 status (bad SQL, missing stream) before any row is streamed.
type StreamHTTPError struct {
	Status int
	Body   string
}

func (e *StreamHTTPError) Error() string {
	return fmt.Sprintf("timeplus stream: status %d: %s", e.Status, e.Body)
}

// StreamLines POSTs sql to the Timeplus HTTP interface with
// default_format=JSONEachRow and invokes onLine for every non-empty NDJSON
// line until the stream ends, ctx is cancelled, or onLine returns an error.
// onConnected (optional) fires once after a 200 response, before the first
// line. It deliberately uses http.DefaultClient (no timeout): streaming
// SELECTs are open-ended by design — protonHTTPClient's 60s cap would kill
// them. Cancellation comes from ctx.
//
// Half-open connections: if the peer dies without a FIN (network partition,
// hard kill), the read blocks silently until the transport's TCP keepalive
// probes (enabled by default on http.DefaultClient's dialer) detect the dead
// peer and fail the read — typically within a couple of minutes. Callers that
// need gap-free delivery should pair reconnects with a replay window (e.g.
// the notify watcher's seek_to='-24h') rather than an application-level
// heartbeat.
func StreamLines(ctx context.Context, cfg *config.TimeplusConfig, sql string, onConnected func(), onLine func(line []byte) error) error {
	endpoint := HTTPURL(cfg, "default_format=JSONEachRow")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(sql))
	if err != nil {
		return fmt.Errorf("timeplus stream: build request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")
	if cfg.User != "" {
		req.SetBasicAuth(cfg.User, cfg.Password)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("timeplus stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &StreamHTTPError{Status: resp.StatusCode, Body: string(body)}
	}
	if onConnected != nil {
		onConnected()
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 256*1024), 256*1024) // 256 KB max line (large event_data)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := onLine(line); err != nil {
			return err
		}
	}
	return scanner.Err()
}
