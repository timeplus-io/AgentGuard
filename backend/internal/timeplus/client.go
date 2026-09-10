package timeplus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/timeplus/agentguard/internal/config"
)

// 60s aligns the HTTP query path with queryNativeTimeout and with the DLP
// UDFs' own 60s model-call budget (dlp_summarize): a local-LLM summary over
// the top-N evidence can legitimately take longer than 15s, and the client
// must not abandon the query before the UDF's own timeout fires.
var protonHTTPClient = &http.Client{Timeout: 60 * time.Second}

// queryNativeTimeout caps a single native-binary query. 60s gives large
// agentguard_hook_events scans (e.g. ListEvents over a busy session) room
// to finish; it matches the HTTP client timeout above.
const queryNativeTimeout = 60 * time.Second

// Row is a single result row from a Proton query.
type Row = map[string]any

// HTTPURL builds the Timeplus HTTP endpoint for cfg. extra is an optional
// pre-encoded query string (e.g. "default_format=JSONEachRow"). When
// cfg.Database is set and not "default", database=<name> is appended so every
// unqualified stream name in handler SQL resolves inside that database — how
// the UI attaches to an app-installed pipeline (db_name "ag").
func HTTPURL(cfg *config.TimeplusConfig, extra string) string {
	u := fmt.Sprintf("http://%s:%d/", cfg.Host, cfg.Port)
	var params []string
	if extra != "" {
		params = append(params, extra)
	}
	if cfg.Database != "" && cfg.Database != "default" {
		params = append(params, "database="+url.QueryEscape(cfg.Database))
	}
	if len(params) == 0 {
		return u
	}
	return u + "?" + strings.Join(params, "&")
}

// Query executes a SELECT SQL statement against the Proton HTTP interface and
// returns all rows. It appends "FORMAT JSON" to the query automatically.
// Returns a wrapped error on non-2xx status or parse failure.
func Query(cfg *config.TimeplusConfig, sql string) ([]Row, error) {
	endpoint := HTTPURL(cfg, "")
	body := strings.NewReader(sql + " FORMAT JSON")

	req, err := http.NewRequest(http.MethodPost, endpoint, body)
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

// QueryNative runs a SELECT through the native binary protocol (port 8463)
// instead of HTTP+JSON, to bypass Proton's text JSON serializer. Use this for
// any query that pulls user-content columns (e.g. agentguard_hook_events.event_data),
// where Proton's `FORMAT JSON` can emit malformed escape sequences (e.g. raw
// `\l`) that Go's strict JSON decoder rejects, killing the whole response.
//
// Returns rows in the same `[]Row` shape as Query so handlers consume them
// identically. The native driver requires concrete typed pointers per column —
// we use ColumnTypes().ScanType() + reflect.New() to allocate the right pointer
// dynamically, then deref nullables (which the driver presents as *T) so the
// handler-side parseStr/parseInt/parseTime helpers see plain values.
func QueryNative(cfg *config.TimeplusConfig, sql string) ([]Row, error) {
	conn, err := connectOnce(cfg)
	if err != nil {
		return nil, fmt.Errorf("proton native: connect: %w", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), queryNativeTimeout)
	defer cancel()

	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("proton native: query: %w", err)
	}
	defer rows.Close()

	cols := rows.Columns()
	colTypes := rows.ColumnTypes()
	if len(colTypes) != len(cols) {
		return nil, fmt.Errorf("proton native: column count mismatch (%d cols vs %d types)", len(cols), len(colTypes))
	}

	// Pre-resolve scan types so we don't reflect every row.
	// scanType is what the driver expects to scan into (after one extra ptr level).
	// For non-nullable T → reflect.New(T) gives *T → Scan gets *T (correct).
	// For nullable T (ScanType=*T) → reflect.New(*T) gives **T → Scan gets **T (correct).
	scanTypes := make([]reflect.Type, len(colTypes))
	for i, ct := range colTypes {
		st := ct.ScanType()
		if st == nil {
			// Fallback for unknown types — driver will likely error, but try string.
			st = reflect.TypeOf("")
		}
		scanTypes[i] = st
	}

	out := make([]Row, 0, 64)
	for rows.Next() {
		ptrs := make([]interface{}, len(cols))
		for i, st := range scanTypes {
			ptrs[i] = reflect.New(st).Interface()
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("proton native: scan: %w", err)
		}
		row := make(Row, len(cols))
		for i, c := range cols {
			// reflect.New returned *T, so .Elem() yields T (or *T for nullable).
			v := reflect.ValueOf(ptrs[i]).Elem().Interface()
			row[c] = derefPtr(v)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("proton native: iterate: %w", err)
	}
	return out, nil
}

// derefPtr unwraps one level of pointer so callers see plain string/int/time.Time.
// The native driver hands back *T for nullable columns; the parse helpers in
// handlers/ expect the underlying value, not a pointer. nil pointer → zero value.
func derefPtr(v interface{}) interface{} {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr {
		return v
	}
	if rv.IsNil() {
		// Return the zero value of the element type so downstream parse* helpers
		// see "" / 0 / time.Time{} instead of a typed nil.
		return reflect.Zero(rv.Type().Elem()).Interface()
	}
	return rv.Elem().Interface()
}

// ExecHTTP executes a DDL or DML statement against the Proton HTTP interface.
// Use for CREATE/DROP/PAUSE/RESUME statements from handler code.
func ExecHTTP(cfg *config.TimeplusConfig, sql string) error {
	endpoint := HTTPURL(cfg, "")
	body := strings.NewReader(sql)

	req, err := http.NewRequest(http.MethodPost, endpoint, body)
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
