# Streaming Threat Notifications Implementation Plan (#258)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Timeplus→AgentGuard alert callback (Python UDF + `POST /api/alerts/webhook`) with a backend streaming watcher over `agentguard_notify_events` plus a persisted `agentguard_notifications` seen-state mutable stream.

**Architecture:** A long-lived Go goroutine tails `agentguard_notify_events` via a new shared HTTP `JSONEachRow` streaming helper, dedups against a new mutable stream keyed on the threat identity, and publishes to the existing `GlobalAlertBroadcaster` → SSE → toast pipeline. The UDF, ALERT, webhook endpoint, and the server-side `agentguard.url` config key are removed. `mv_notify_filter`, `agentguard_notify_events`, and hold notifications are untouched.

**Tech Stack:** Go 1.x / Gin, Timeplus Enterprise (mutable streams, HTTP JSONEachRow), React (Vite).

**Spec:** `docs/superpowers/specs/2026-07-30-streaming-notifications-design.md`

## Global Constraints

- Branch: `feat/issue-258-streaming-notifications` (already created; the spec commit is on it).
- All Go builds/tests MUST use `CGO_ENABLED=0` (macOS 26 dyld abort otherwise): `cd backend && CGO_ENABLED=0 go build ./...`, `CGO_ENABLED=0 go test ./...`.
- The notification identity key everywhere is `(agent_id, deployment_id, session_id, rule_id)`; the stable SSE envelope id is the joined string `deployment_id|agent_id|session_id|rule_id` (same order as `useLiveThreatFeed`).
- Timeplus `datetime64(3)` values are compared as their fixed-width text form (`"2026-07-30 12:34:56.789"`) — lexicographic == chronological because both sides are rendered by the same server. Never parse them into `time.Time` for comparison.
- All SQL string literals built from event/user data go through the backslash-aware escaper (`rules.EscapeSQLString` / handlers' `escapeSQLStr`).
- The RBAC middleware (`backend/internal/rbac/middleware.go`) is fail-closed: every new protected route MUST be added to `routePermissions`.
- `AGENTGUARD_URL` in `agents/`, `backend/internal/scripts/install.sh`, and integration tests is the *plugin→server / test-target* base URL — a different concern. Do NOT touch those. Only the server-side webhook config key `agentguard.url` is retired.
- `docker-compose.yaml` has uncommitted local changes — edit only the specific line named in Task 6, do not revert anything else in that file.
- Working-tree also contains an untracked PDF under `docs/research/` — never `git add -A`; always add explicit paths.

---

### Task 1: `timeplus.StreamLines` + refactor `ProtonQueryHandler`

**Files:**
- Create: `backend/internal/timeplus/stream.go`
- Create: `backend/internal/timeplus/stream_test.go`
- Modify: `backend/internal/handlers/proton.go` (full rewrite of the handler body)

**Interfaces:**
- Produces: `timeplus.StreamLines(ctx context.Context, cfg *config.TimeplusConfig, sql string, onConnected func(), onLine func(line []byte) error) error` and `timeplus.StreamHTTPError{Status int; Body string}`. Task 4's watcher consumes both.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/timeplus/stream_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/ -run TestStreamLines -v`
Expected: FAIL — `undefined: StreamLines`

- [ ] **Step 3: Write the implementation**

Create `backend/internal/timeplus/stream.go`:

```go
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
func StreamLines(ctx context.Context, cfg *config.TimeplusConfig, sql string, onConnected func(), onLine func(line []byte) error) error {
	url := fmt.Sprintf("http://%s:%d/?default_format=JSONEachRow", cfg.Host, cfg.Port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(sql))
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/ -run TestStreamLines -v`
Expected: PASS (both tests)

- [ ] **Step 5: Refactor `ProtonQueryHandler` onto StreamLines**

Replace the entire contents of `backend/internal/handlers/proton.go` with:

```go
package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

type protonQueryRequest struct {
	SQL string `json:"sql" binding:"required"`
}

// errClientGone marks a write failure to the browser (navigated away). It ends
// the stream without logging — this is the normal end of a live tail.
var errClientGone = errors.New("client gone")

// ProtonQueryHandler returns a Gin handler that proxies SQL queries to the
// configured Proton/Timeplus server and streams NDJSON rows back to the client.
// Rows are forwarded byte-for-byte (no re-marshalling) and flushed per line so
// live events reach the browser immediately.
func ProtonQueryHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		var req protonQueryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if strings.TrimSpace(req.SQL) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sql is required"})
			return
		}

		flusher, canFlush := c.Writer.(http.Flusher)
		newline := []byte{'\n'}
		err := tp.StreamLines(c.Request.Context(), &cfg.Timeplus, req.SQL,
			func() {
				// Disable proxy buffering and stream NDJSON directly to the client.
				c.Header("Content-Type", "application/x-ndjson")
				c.Header("X-Accel-Buffering", "no")
				c.Header("Cache-Control", "no-cache")
				c.Status(http.StatusOK)
			},
			func(line []byte) error {
				if _, werr := c.Writer.Write(line); werr != nil {
					return errClientGone
				}
				if _, werr := c.Writer.Write(newline); werr != nil {
					return errClientGone
				}
				if canFlush {
					flusher.Flush()
				}
				return nil
			})
		if err == nil || errors.Is(err, errClientGone) {
			return
		}
		var httpErr *tp.StreamHTTPError
		if errors.As(err, &httpErr) {
			c.JSON(httpErr.Status, gin.H{"error": httpErr.Body})
			return
		}
		if !c.Writer.Written() {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf(
				"cannot reach Proton at %s:%d — %s", cfg.Timeplus.Host, cfg.Timeplus.Port, err.Error(),
			)})
			return
		}
		// context.Canceled is normal: the browser navigated away or the component
		// unmounted, closing the streaming connection. Not an error.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		// Headers already sent so we can't change status, but we can log.
		sql := req.SQL
		if len(sql) > 200 {
			sql = sql[:200] + "…"
		}
		log.Printf("[proton] stream error: %v | client=%s | sql=%s", err, c.ClientIP(), sql)
	}
}
```

- [ ] **Step 6: Build + full package tests**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/timeplus/ ./internal/handlers/`
Expected: build OK, tests PASS

- [ ] **Step 7: Commit**

```bash
git add backend/internal/timeplus/stream.go backend/internal/timeplus/stream_test.go backend/internal/handlers/proton.go
git commit -m "refactor(timeplus): extract StreamLines shared JSONEachRow streaming helper (#258)"
```

---

### Task 2: Stable SSE envelope id + `session_id` in the payload

**Files:**
- Modify: `backend/internal/models/models.go` (`AlertWebhookPayload`, ~line 512)
- Modify: `backend/internal/handlers/notifications.go` (`Publish` → add `PublishWithID`)
- Create: `backend/internal/handlers/notifications_broadcast_test.go`

**Interfaces:**
- Produces: `GlobalAlertBroadcaster.PublishWithID(id string, payload models.AlertWebhookPayload)` (Task 4's `Publisher` interface, Task 5's handlers). `models.AlertWebhookPayload` gains `SessionID string \`json:"session_id"\`` — hold publishers leave it empty; nothing breaks.
- Consumes: nothing new.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/handlers/notifications_broadcast_test.go`:

```go
package handlers

import (
	"encoding/json"
	"testing"

	"github.com/timeplus/agentguard/internal/models"
)

// The notify watcher publishes with a stable identity id so repeated
// deliveries of the same threat dedup client-side (multi-tab, backfill).
func TestPublishWithIDUsesCallerID(t *testing.T) {
	b := &alertBroadcaster{clients: make(map[chan []byte]struct{})}
	ch, cancel := b.subscribe()
	defer cancel()

	b.PublishWithID("dep|agent|sess|rp-001", models.AlertWebhookPayload{
		RuleID:    "rp-001",
		SessionID: "sess",
	})

	var env map[string]any
	if err := json.Unmarshal(<-ch, &env); err != nil {
		t.Fatal(err)
	}
	if env["id"] != "dep|agent|sess|rp-001" {
		t.Errorf("id = %v, want the caller-supplied identity key", env["id"])
	}
	if env["session_id"] != "sess" {
		t.Errorf("session_id = %v, want sess (payload must carry the session)", env["session_id"])
	}
}

// Publish (used by holds) must keep generating a fresh unique id per call.
func TestPublishGeneratesUniqueID(t *testing.T) {
	b := &alertBroadcaster{clients: make(map[chan []byte]struct{})}
	ch, cancel := b.subscribe()
	defer cancel()

	b.Publish(models.AlertWebhookPayload{RuleName: "hold_pending"})
	b.Publish(models.AlertWebhookPayload{RuleName: "hold_pending"})

	var a, bb map[string]any
	_ = json.Unmarshal(<-ch, &a)
	_ = json.Unmarshal(<-ch, &bb)
	if a["id"] == "" || a["id"] == bb["id"] {
		t.Errorf("Publish ids must be unique and non-empty, got %v / %v", a["id"], bb["id"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run 'TestPublish' -v`
Expected: FAIL — `b.PublishWithID undefined` and `unknown field SessionID`

- [ ] **Step 3: Implement**

In `backend/internal/models/models.go`, add `SessionID` to `AlertWebhookPayload` (after `DeploymentID`):

```go
type AlertWebhookPayload struct {
	RuleID       string `json:"rule_id"`
	RuleName     string `json:"rule_name"`
	Severity     string `json:"severity"`
	AgentID      string `json:"agent_id"`
	DeploymentID string `json:"deployment_id"`
	SessionID    string `json:"session_id"`
	Message      string `json:"message"`
	DetectedAt   string `json:"detected_at"`
}
```

In `backend/internal/handlers/notifications.go`, replace the existing `Publish` method with:

```go
// Publish sends a payload to every currently-connected SSE client with a
// fresh unique envelope id (used by holds, where every event is distinct).
// Slow clients whose channel buffer is full are silently skipped.
func (b *alertBroadcaster) Publish(payload models.AlertWebhookPayload) {
	b.PublishWithID(uuid.New().String(), payload)
}

// PublishWithID publishes with a caller-supplied stable envelope id so
// repeated deliveries of the same notification (reconnect backfill, multiple
// tabs) dedup client-side. Used by the notify watcher (#258).
func (b *alertBroadcaster) PublishWithID(id string, payload models.AlertWebhookPayload) {
	envelope := struct {
		models.AlertWebhookPayload
		ID string `json:"id"`
	}{
		AlertWebhookPayload: payload,
		ID:                  id,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		log.Printf("notifications: Publish marshal failed: %v", err)
		return
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- data:
		default:
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run 'TestPublish' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/models/models.go backend/internal/handlers/notifications.go backend/internal/handlers/notifications_broadcast_test.go
git commit -m "feat(notifications): stable envelope ids via PublishWithID + session_id in payload (#258)"
```

---

### Task 3: Provision `agentguard_notifications`, tear down the legacy UDF/ALERT

**Files:**
- Modify: `backend/internal/timeplus/init.go`
- Modify: `backend/internal/handlers/setup.go` (Init call site — the only caller)
- Create: `backend/internal/timeplus/init_notifications_test.go`

**Interfaces:**
- Produces: the `agentguard_notifications` mutable stream (schema below) that Tasks 4–5 read/write; `Init` loses its `agentGuardURL string` parameter → new signature `Init(timeplusCfg config.TimeplusConfig, dlpCfg config.DLPConfig, sentryCfg config.SentryConfig, emit func(status, msg string)) error`.
- Consumes: existing `execIfNotExists(ctx, conn, kind, name, ddl, emit)` helper.

- [ ] **Step 1: Write the failing DDL test**

Create `backend/internal/timeplus/init_notifications_test.go`:

```go
package timeplus

import (
	"strings"
	"testing"
)

// Pins the seen-state schema (#258): threat-identity PK, coalesced upserts,
// and a separate cf_confirm family so the confirm endpoint can write
// (confirmed, confirmed_at) without knowing the event columns.
func TestNotificationsStreamDDL(t *testing.T) {
	for _, want := range []string{
		"CREATE MUTABLE STREAM IF NOT EXISTS agentguard_notifications",
		"PRIMARY KEY (agent_id, deployment_id, session_id, rule_id)",
		"FAMILY cf_event(rule_name, severity, message, detected_at, notified_at)",
		"FAMILY cf_confirm(confirmed, confirmed_at)",
		"coalesced = true",
	} {
		if !strings.Contains(ddlNotificationsStream, want) {
			t.Errorf("ddlNotificationsStream missing %q", want)
		}
	}
}

// The legacy Timeplus→AgentGuard callback objects must be dropped on
// provision — the alert first (it references the UDF).
func TestLegacyNotifierTeardownDDL(t *testing.T) {
	if len(legacyNotifierDrops) != 2 {
		t.Fatalf("legacyNotifierDrops = %v, want exactly the alert + UDF drops", legacyNotifierDrops)
	}
	if !strings.Contains(legacyNotifierDrops[0], "DROP ALERT IF EXISTS agentguard_notifier") {
		t.Errorf("first drop must be the alert, got %q", legacyNotifierDrops[0])
	}
	if !strings.Contains(legacyNotifierDrops[1], "DROP FUNCTION IF EXISTS notify_agentguard_udf") {
		t.Errorf("second drop must be the UDF, got %q", legacyNotifierDrops[1])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/ -run 'TestNotificationsStreamDDL|TestLegacyNotifierTeardownDDL' -v`
Expected: FAIL — `undefined: ddlNotificationsStream`, `undefined: legacyNotifierDrops`

- [ ] **Step 3: Implement in `init.go`**

3a. Change the `Init` signature (line ~64): delete the `agentGuardURL string` parameter:

```go
func Init(timeplusCfg config.TimeplusConfig, dlpCfg config.DLPConfig, sentryCfg config.SentryConfig, emit func(status, msg string)) error {
```

3b. Replace the notify provisioning block (lines ~139–148, from `emit("running", "$ Creating notification UDF and alert…")` through the `createNotifierAlert` if-block) with:

```go
	emit("running", "$ Creating notifications stream…")
	if err := createNotificationsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating notifications stream: %v", err)
	}
	if err := createNotifyFilterMV(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating notify filter MV: %v", err)
	}
	emit("running", "$ Removing legacy notifier callback…")
	dropLegacyNotifier(ctx, conn, emit)
```

3c. Delete `createNotifyUDF` (lines ~479–486) and `createNotifierAlert` (~488–499) functions; delete the `ddlNotifyUDFTemplate` and `ddlNotifierAlert` constants (~1915–1950) including their doc comments. Keep `createNotifyFilterMV`, `ddlNotifyFilterMV`, `ddlNotifyEventsStream` unchanged. Remove the now-unused `"fmt"`-dependent line only if the compiler says `fmt` is unused (it won't be — `Init` uses it elsewhere).

3d. Add next to `createThreatsStream`:

```go
// createNotificationsStream provisions the persisted notification seen-state
// store (#258): one row per threat identity, written by the notify watcher
// and the confirm endpoint.
func createNotificationsStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_notifications", ddlNotificationsStream, emit)
}

// legacyNotifierDrops removes the pre-#258 Timeplus→AgentGuard callback
// objects. Order matters: the alert references the UDF, so it drops first.
var legacyNotifierDrops = []string{
	"DROP ALERT IF EXISTS agentguard_notifier",
	"DROP FUNCTION IF EXISTS notify_agentguard_udf",
}

// dropLegacyNotifier is best-effort: existing deployments keep POSTing to the
// removed webhook until this runs, so failures are logged but never fatal.
// Provisioning never recreates these objects.
func dropLegacyNotifier(ctx context.Context, conn proton.Conn, emit func(string, string)) {
	for _, ddl := range legacyNotifierDrops {
		if err := conn.Exec(ctx, ddl); err != nil {
			log.Printf("timeplus: legacy notifier cleanup %q failed: %v", ddl, err)
		} else {
			emit("ok", "  ✓ "+ddl)
		}
	}
}
```

3e. Add the DDL constant next to `ddlThreatsStream` (~line 1968):

```go
// ddlNotificationsStream is the persisted per-threat notification seen-state
// (#258). The notify watcher upserts the full row (confirmed=false); the
// confirm endpoint upserts cf_confirm alone. Same PK as agentguard_threats.
const ddlNotificationsStream = `
CREATE MUTABLE STREAM IF NOT EXISTS agentguard_notifications
(
    agent_id      string,
    deployment_id string,
    session_id    string,
    rule_id       string,
    rule_name     string,
    severity      string,
    message       string,
    detected_at   datetime64(3),
    notified_at   datetime64(3),
    confirmed     bool,
    confirmed_at  datetime64(3),
    FAMILY cf_event(rule_name, severity, message, detected_at, notified_at),
    FAMILY cf_confirm(confirmed, confirmed_at)
) PRIMARY KEY (agent_id, deployment_id, session_id, rule_id)
SETTINGS coalesced = true`
```

3f. In `backend/internal/handlers/setup.go`:
- Delete the `AgentGuardURL string \`json:"agentguard_url"\`` field from the request body struct (~line 61) and update the comment above `Provision` (~line 52) to `// Body: {"host":"...","port":3218,"native_port":8463,"username":"...","password":"..."}`.
- Delete the `agentGuardURL := body.AgentGuardURL … }` block (~lines 96–99).
- Change the Init call (~line 109) to `if err := tp.Init(timeplusCfg, cfg.DLP, cfg.Sentry, sendEvent); err != nil {`.
- Delete `newCfg.AgentGuardURL = agentGuardURL` (~line 146) and `cfg.AgentGuardURL = agentGuardURL` (~line 168). (The config *field* itself is retired in Task 6.)

- [ ] **Step 4: Run tests + build**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/timeplus/ ./internal/handlers/`
Expected: build OK, new DDL tests PASS, no references to the deleted functions remain (`grep -rn "createNotifyUDF\|createNotifierAlert\|ddlNotifyUDFTemplate\|ddlNotifierAlert" backend/` returns nothing).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/timeplus/init.go backend/internal/timeplus/init_notifications_test.go backend/internal/handlers/setup.go
git commit -m "feat(timeplus): provision agentguard_notifications seen-state, drop legacy notify UDF/alert (#258)"
```

---

### Task 4: The `internal/notify` watcher package

**Files:**
- Create: `backend/internal/notify/watcher.go`
- Create: `backend/internal/notify/store.go`
- Create: `backend/internal/notify/watcher_test.go`
- Create: `backend/internal/notify/store_test.go`

**Interfaces:**
- Consumes: `timeplus.StreamLines` + `StreamHTTPError` (Task 1), `PublishWithID` shape (Task 2), `rules.EscapeSQLString`, `timeplus.Query`/`ExecHTTP`, `config.Mu`.
- Produces: `notify.NewWatcher(cfg *config.Config, store Store, pub Publisher) *Watcher` with `Run(ctx context.Context)`; `notify.TimeplusStore{Cfg *config.Config}` implementing `Store`; `notify.NotificationID(deploymentID, agentID, sessionID, ruleID string) string`. Task 5 wires these in `main.go` and uses `NotificationID` in the unconfirmed handler.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/notify/watcher_test.go`:

```go
package notify

import (
	"errors"
	"testing"

	"github.com/timeplus/agentguard/internal/models"
)

type fakeStore struct {
	state  map[string]*State
	marked []Event
	getErr error
}

func stateKey(e Event) string {
	return e.AgentID + "|" + e.DeploymentID + "|" + e.SessionID + "|" + e.RuleID
}

func (f *fakeStore) Get(e Event) (*State, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.state[stateKey(e)], nil
}

func (f *fakeStore) MarkNotified(e Event) error {
	f.marked = append(f.marked, e)
	return nil
}

type fakePub struct {
	ids      []string
	payloads []models.AlertWebhookPayload
}

func (f *fakePub) PublishWithID(id string, p models.AlertWebhookPayload) {
	f.ids = append(f.ids, id)
	f.payloads = append(f.payloads, p)
}

func TestShouldNotify(t *testing.T) {
	cases := []struct {
		name       string
		st         *State
		detectedAt string
		want       bool
	}{
		{"never notified", nil, "2026-07-30 10:00:00.000", true},
		{"pending unconfirmed (burst dup)", &State{Confirmed: false}, "2026-07-30 10:00:01.000", false},
		{"re-emerged after confirm", &State{Confirmed: true, ConfirmedAt: "2026-07-30 10:00:00.000"}, "2026-07-30 10:05:00.000", true},
		{"replayed old event after confirm", &State{Confirmed: true, ConfirmedAt: "2026-07-30 10:00:00.000"}, "2026-07-30 09:59:59.999", false},
		{"event at exactly confirm time", &State{Confirmed: true, ConfirmedAt: "2026-07-30 10:00:00.000"}, "2026-07-30 10:00:00.000", false},
	}
	for _, tc := range cases {
		if got := shouldNotify(tc.st, tc.detectedAt); got != tc.want {
			t.Errorf("%s: shouldNotify = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHandleLineNewThreatPublishesWithStableID(t *testing.T) {
	st := &fakeStore{state: map[string]*State{}}
	pub := &fakePub{}
	w := &Watcher{store: st, pub: pub}

	w.handleLine([]byte(`{"detected_at":"2026-07-30 10:00:00.000","rule_id":"rp-001","rule_name":"Secrets in prompt","severity":"critical","agent_id":"a1","deployment_id":"d1","session_id":"s1","message":"m"}`))

	if len(pub.ids) != 1 {
		t.Fatalf("published %d times, want 1", len(pub.ids))
	}
	if pub.ids[0] != "d1|a1|s1|rp-001" {
		t.Errorf("id = %q, want deployment|agent|session|rule", pub.ids[0])
	}
	if pub.payloads[0].SessionID != "s1" || pub.payloads[0].RuleName != "Secrets in prompt" {
		t.Errorf("payload not filled from the event: %+v", pub.payloads[0])
	}
	if len(st.marked) != 1 {
		t.Errorf("MarkNotified called %d times, want 1", len(st.marked))
	}
}

func TestHandleLinePendingUnconfirmedSkips(t *testing.T) {
	e := Event{AgentID: "a1", DeploymentID: "d1", SessionID: "s1", RuleID: "rp-001"}
	st := &fakeStore{state: map[string]*State{stateKey(e): {Confirmed: false}}}
	pub := &fakePub{}
	w := &Watcher{store: st, pub: pub}

	w.handleLine([]byte(`{"detected_at":"2026-07-30 10:00:00.000","rule_id":"rp-001","agent_id":"a1","deployment_id":"d1","session_id":"s1"}`))

	if len(pub.ids) != 0 {
		t.Errorf("published %d times, want 0 (already pending)", len(pub.ids))
	}
}

func TestHandleLineStoreErrorFailsOpen(t *testing.T) {
	st := &fakeStore{getErr: errors.New("timeplus down")}
	pub := &fakePub{}
	w := &Watcher{store: st, pub: pub}

	w.handleLine([]byte(`{"detected_at":"2026-07-30 10:00:00.000","rule_id":"rp-001","agent_id":"a1"}`))

	if len(pub.ids) != 1 {
		t.Errorf("published %d times, want 1 (fail open toward notifying)", len(pub.ids))
	}
}

func TestHandleLineMalformedAndIncompleteRowsIgnored(t *testing.T) {
	pub := &fakePub{}
	w := &Watcher{store: &fakeStore{}, pub: pub}

	w.handleLine([]byte(`not json`))
	w.handleLine([]byte(`{"detected_at":"2026-07-30 10:00:00.000"}`)) // no agent_id/rule_id

	if len(pub.ids) != 0 {
		t.Errorf("published %d times, want 0", len(pub.ids))
	}
}
```

Create `backend/internal/notify/store_test.go`:

```go
package notify

import (
	"strings"
	"testing"
)

// The watcher's identifiers come from the event stream (ultimately from agent
// telemetry), so the seen-state SQL must use the backslash-aware escaper —
// same reasoning as the retired threatOpenCheckSQL webhook test.
func TestNotifyStoreSQLEscapesBackslashQuoteBypass(t *testing.T) {
	e := Event{
		AgentID:      `\' OR 1=1 --`,
		DeploymentID: "d",
		SessionID:    "s",
		RuleID:       "r",
		RuleName:     "n",
		Severity:     "critical",
		Message:      `it's a \' test`,
		DetectedAt:   "2026-07-30 10:00:00.000",
	}
	for name, sql := range map[string]string{
		"get":  getStateSQL(e),
		"mark": markNotifiedSQL(e),
	} {
		if strings.Contains(sql, `\' OR`) {
			t.Errorf("%s: raw \\' escape sequence survived into SQL: %s", name, sql)
		}
		if !strings.Contains(sql, `\\''`) {
			t.Errorf("%s: payload not backslash-escaped: %s", name, sql)
		}
	}
}

func TestMarkNotifiedSQLWritesUnconfirmed(t *testing.T) {
	e := Event{AgentID: "a", DeploymentID: "d", SessionID: "s", RuleID: "r", DetectedAt: "2026-07-30 10:00:00.000"}
	sql := markNotifiedSQL(e)
	for _, want := range []string{"INSERT INTO agentguard_notifications", "now64(3)", "false", "1970-01-01 00:00:00.000"} {
		if !strings.Contains(sql, want) {
			t.Errorf("markNotifiedSQL missing %q: %s", want, sql)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/notify/ -v`
Expected: FAIL — package does not compile (`undefined: Watcher`, `Event`, …)

- [ ] **Step 3: Implement `watcher.go`**

Create `backend/internal/notify/watcher.go`:

```go
// Package notify implements the AgentGuard-side replacement (#258) for the
// old Timeplus→AgentGuard alert callback: a long-lived streaming subscription
// on agentguard_notify_events that dedups against the persisted
// agentguard_notifications seen-state and publishes new-threat notifications
// to the in-process SSE broadcaster.
package notify

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/models"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// Event is one row of agentguard_notify_events (already filtered by
// mv_notify_filter to threats that are new or previously cleared).
// datetime64 columns arrive as fixed-width text, e.g. "2026-07-30 12:34:56.789".
type Event struct {
	DetectedAt   string `json:"detected_at"`
	RuleID       string `json:"rule_id"`
	RuleName     string `json:"rule_name"`
	Severity     string `json:"severity"`
	AgentID      string `json:"agent_id"`
	DeploymentID string `json:"deployment_id"`
	SessionID    string `json:"session_id"`
	HookName     string `json:"hook_name"`
	Message      string `json:"message"`
}

// State is the persisted seen-state for one threat identity.
type State struct {
	Confirmed   bool
	ConfirmedAt string // datetime64(3) text; zero-state is the 1970 epoch
}

// Store persists per-threat notification state.
type Store interface {
	// Get returns the current seen-state, or nil when never notified.
	Get(e Event) (*State, error)
	// MarkNotified upserts the row as notified-and-unconfirmed.
	MarkNotified(e Event) error
}

// Publisher is the subset of the SSE broadcaster the watcher needs.
type Publisher interface {
	PublishWithID(id string, payload models.AlertWebhookPayload)
}

// NotificationID is the stable SSE envelope id for a threat notification —
// the same key order useLiveThreatFeed dedups on.
func NotificationID(deploymentID, agentID, sessionID, ruleID string) string {
	return deploymentID + "|" + agentID + "|" + sessionID + "|" + ruleID
}

// shouldNotify decides whether an incoming event produces a notification.
// Lexicographic comparison of the datetime64 text is chronological: both
// values are rendered by the same Timeplus server in the same fixed-width
// format and timezone.
func shouldNotify(st *State, detectedAt string) bool {
	if st == nil {
		return true // never notified
	}
	if !st.Confirmed {
		return false // delivered, pending confirm — absorbs burst duplicates
	}
	return detectedAt > st.ConfirmedAt // re-emerged after the user confirmed
}

// watchSQL replays the last 24h on (re)connect so a server restart recovers
// missed threats; shouldNotify filters out rows already notified/confirmed.
const watchSQL = `SELECT detected_at, rule_id, rule_name, severity,
  agent_id, deployment_id, session_id, hook_name, message
FROM agentguard_notify_events
SETTINGS seek_to='-24h'`

const (
	initialBackoff = time.Second
	maxBackoff     = 30 * time.Second
	notReadyPoll   = 5 * time.Second
)

// Watcher tails agentguard_notify_events and publishes new-threat toasts.
type Watcher struct {
	cfg   *config.Config
	store Store
	pub   Publisher
}

func NewWatcher(cfg *config.Config, store Store, pub Publisher) *Watcher {
	return &Watcher{cfg: cfg, store: store, pub: pub}
}

// Run blocks until ctx is cancelled, reconnecting forever with capped
// exponential backoff. It self-gates on cfg.SetupComplete so it can be
// started unconditionally at boot (setup may complete later via the wizard).
func (w *Watcher) Run(ctx context.Context) {
	backoff := initialBackoff
	for ctx.Err() == nil {
		config.Mu.RLock()
		ready := w.cfg.SetupComplete
		tpCfg := w.cfg.Timeplus
		config.Mu.RUnlock()
		if !ready {
			sleepCtx(ctx, notReadyPoll)
			continue
		}

		err := timeplus.StreamLines(ctx, &tpCfg, watchSQL,
			func() {
				log.Printf("notify: watching agentguard_notify_events")
				backoff = initialBackoff // reset once a connection succeeds
			},
			func(line []byte) error {
				w.handleLine(line)
				return nil
			})
		if ctx.Err() != nil {
			return
		}
		log.Printf("notify: stream ended (%v), reconnecting in %s", err, backoff)
		sleepCtx(ctx, backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// handleLine processes one NDJSON row. Failures never stop the stream:
// malformed rows are skipped, seen-state errors fail open toward notifying.
func (w *Watcher) handleLine(line []byte) {
	var e Event
	if err := json.Unmarshal(line, &e); err != nil {
		log.Printf("notify: skipping malformed row: %v", err)
		return
	}
	if e.AgentID == "" || e.RuleID == "" {
		return
	}

	st, err := w.store.Get(e)
	if err != nil {
		log.Printf("notify: seen-state read failed (failing open): %v", err)
		st = nil
	}
	if !shouldNotify(st, e.DetectedAt) {
		return
	}
	if err := w.store.MarkNotified(e); err != nil {
		log.Printf("notify: seen-state write failed (still notifying): %v", err)
	}
	w.pub.PublishWithID(
		NotificationID(e.DeploymentID, e.AgentID, e.SessionID, e.RuleID),
		models.AlertWebhookPayload{
			RuleID:       e.RuleID,
			RuleName:     e.RuleName,
			Severity:     e.Severity,
			AgentID:      e.AgentID,
			DeploymentID: e.DeploymentID,
			SessionID:    e.SessionID,
			Message:      e.Message,
			DetectedAt:   e.DetectedAt,
		})
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
```

- [ ] **Step 4: Implement `store.go`**

Create `backend/internal/notify/store.go`:

```go
package notify

import (
	"encoding/json"
	"fmt"

	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/rules"
	"github.com/timeplus/agentguard/internal/timeplus"
)

// TimeplusStore persists seen-state in the agentguard_notifications mutable
// stream. It re-reads cfg.Timeplus under config.Mu per call so hot-applied
// endpoint changes take effect without a restart. Point queries on the PK
// are cheap at threat-notification volume — no in-memory cache to keep
// coherent across replicas.
type TimeplusStore struct {
	Cfg *config.Config
}

func esc(s string) string { return rules.EscapeSQLString(s) }

func getStateSQL(e Event) string {
	return fmt.Sprintf(
		`SELECT confirmed, to_string(confirmed_at) AS confirmed_at FROM table(agentguard_notifications) WHERE agent_id = '%s' AND deployment_id = '%s' AND session_id = '%s' AND rule_id = '%s' LIMIT 1`,
		esc(e.AgentID), esc(e.DeploymentID), esc(e.SessionID), esc(e.RuleID))
}

// markNotifiedSQL writes the full row: cf_event from the event, plus
// confirmed=false so a re-notify after confirm returns the identity to the
// "pending" state (which absorbs burst duplicates). confirmed_at resets to
// the epoch — shouldNotify only reads it when confirmed is true.
func markNotifiedSQL(e Event) string {
	return fmt.Sprintf(
		`INSERT INTO agentguard_notifications (agent_id, deployment_id, session_id, rule_id, rule_name, severity, message, detected_at, notified_at, confirmed, confirmed_at) VALUES ('%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', now64(3), false, '1970-01-01 00:00:00.000')`,
		esc(e.AgentID), esc(e.DeploymentID), esc(e.SessionID), esc(e.RuleID),
		esc(e.RuleName), esc(e.Severity), esc(e.Message), esc(e.DetectedAt))
}

func (s *TimeplusStore) tpCfg() config.TimeplusConfig {
	config.Mu.RLock()
	defer config.Mu.RUnlock()
	return s.Cfg.Timeplus
}

func (s *TimeplusStore) Get(e Event) (*State, error) {
	tpCfg := s.tpCfg()
	rows, err := timeplus.Query(&tpCfg, getStateSQL(e))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	st := &State{}
	switch v := rows[0]["confirmed"].(type) {
	case bool:
		st.Confirmed = v
	case json.Number: // Proton may serialize bool as 0/1
		st.Confirmed = v.String() != "0"
	}
	if v, ok := rows[0]["confirmed_at"].(string); ok {
		st.ConfirmedAt = v
	}
	return st, nil
}

func (s *TimeplusStore) MarkNotified(e Event) error {
	tpCfg := s.tpCfg()
	return timeplus.ExecHTTP(&tpCfg, markNotifiedSQL(e))
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/notify/ -v`
Expected: PASS (all tests from Step 1)

- [ ] **Step 6: Build everything**

Run: `cd backend && CGO_ENABLED=0 go build ./...`
Expected: OK (verifies no import cycle: notify → rules/timeplus/config/models only)

- [ ] **Step 7: Commit**

```bash
git add backend/internal/notify/
git commit -m "feat(notify): threat watcher with persisted seen-state dedup (#258)"
```

---

### Task 5: HTTP endpoints, wiring, and webhook removal

**Files:**
- Modify: `backend/internal/handlers/notifications.go` (add two handlers + SQL builders)
- Create: `backend/internal/handlers/notifications_handler_test.go`
- Modify: `backend/internal/handlers/rules.go` (delete `threatOpenCheckSQL`, `isThreatAlreadyOpen`, `AlertWebhook`, lines ~761–825)
- Modify: `backend/internal/handlers/rules_handler_test.go` (delete `TestThreatOpenCheckSQL_EscapesBackslashQuoteBypass` and its comment)
- Modify: `backend/internal/rbac/middleware.go` (two new route permissions)
- Modify: `backend/cmd/server/main.go` (remove webhook route; add 2 routes; start watcher)
- Modify: `backend/internal/handlers/docs.go` (remove the `/alerts/webhook` path entry)
- Modify: `backend/internal/handlers/dlp_test.go:143` (drop `,{"name":"notify_agentguard_udf"}` from the mock SHOW FUNCTIONS response)

**Interfaces:**
- Consumes: `notify.NotificationID`, `notify.NewWatcher`, `notify.TimeplusStore` (Task 4); `PublishWithID` (Task 2).
- Produces: `GET /api/notifications/unconfirmed` → `{"notifications":[{id, rule_id, rule_name, severity, agent_id, deployment_id, session_id, message, detected_at}]}` (newest-first, ≤20); `POST /api/notifications/confirm` body `{agent_id, deployment_id, session_id, rule_id}` → `{"ok":true}`. Task 7's frontend consumes both.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/handlers/notifications_handler_test.go`:

```go
package handlers

import (
	"strings"
	"testing"
)

// The confirm endpoint takes browser-supplied identifiers into a SQL literal,
// so the escaping must be backslash-aware (Proton reads \' as an escaped
// quote) — same invariant the retired webhook's threatOpenCheckSQL pinned.
func TestConfirmNotificationSQL_EscapesBackslashQuoteBypass(t *testing.T) {
	payload := `\' OR 1=1 --`
	sql := confirmNotificationSQL(notificationIdentity{
		AgentID:      payload,
		DeploymentID: payload,
		SessionID:    payload,
		RuleID:       payload,
	})
	if !strings.Contains(sql, `\\''`) {
		t.Fatalf("payload not backslash-escaped: %s", sql)
	}
	if strings.Contains(sql, `\' OR`) {
		t.Fatalf("raw \\' escape sequence survived into SQL: %s", sql)
	}
	for _, want := range []string{"INSERT INTO agentguard_notifications", "true, now64(3)"} {
		if !strings.Contains(sql, want) {
			t.Errorf("confirm SQL missing %q: %s", want, sql)
		}
	}
}

func TestUnconfirmedNotificationsSQLShape(t *testing.T) {
	for _, want := range []string{
		"FROM table(agentguard_notifications)",
		"WHERE confirmed = false",
		"ORDER BY notified_at DESC",
		"LIMIT 20",
	} {
		if !strings.Contains(unconfirmedNotificationsSQL, want) {
			t.Errorf("unconfirmed SQL missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run 'NotificationSQL|UnconfirmedNotifications' -v`
Expected: FAIL — `undefined: confirmNotificationSQL`, `notificationIdentity`, `unconfirmedNotificationsSQL`

- [ ] **Step 3: Add the handlers**

Append to `backend/internal/handlers/notifications.go` (extend imports with `github.com/timeplus/agentguard/internal/auth`, `github.com/timeplus/agentguard/internal/config`, `github.com/timeplus/agentguard/internal/notify`, and `tp "github.com/timeplus/agentguard/internal/timeplus"`):

```go
// ── Persisted notification state (#258) ─────────────────────────────────────

// notificationIdentity is the threat identity a browser confirms. deployment
// and session may legitimately be empty on older/partial events.
type notificationIdentity struct {
	AgentID      string `json:"agent_id"      binding:"required"`
	DeploymentID string `json:"deployment_id"`
	SessionID    string `json:"session_id"`
	RuleID       string `json:"rule_id"       binding:"required"`
}

// confirmNotificationSQL upserts only the cf_confirm family — the event
// columns keep their last-notified values. Identifiers are browser-supplied,
// hence the backslash-aware escaper (pinned by a unit test).
func confirmNotificationSQL(id notificationIdentity) string {
	return fmt.Sprintf(
		`INSERT INTO agentguard_notifications (agent_id, deployment_id, session_id, rule_id, confirmed, confirmed_at) VALUES ('%s', '%s', '%s', '%s', true, now64(3))`,
		escapeSQLStr(id.AgentID), escapeSQLStr(id.DeploymentID), escapeSQLStr(id.SessionID), escapeSQLStr(id.RuleID))
}

// ConfirmNotification handles POST /api/notifications/confirm — the user
// explicitly dismissed or clicked a toast. Idempotent.
func ConfirmNotification(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		var id notificationIdentity
		if err := c.ShouldBindJSON(&id); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id and rule_id are required"})
			return
		}
		if err := tp.ExecHTTP(&cfg.Timeplus, confirmNotificationSQL(id)); err != nil {
			log.Printf("notifications: confirm failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "confirm failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// unconfirmedNotificationsSQL backs browser backfill on SSE connect. The
// LIMIT bounds re-toasting after long disconnects; the visible toast stack
// caps at 5 client-side anyway.
const unconfirmedNotificationsSQL = `SELECT agent_id, deployment_id, session_id, rule_id, rule_name, severity, message, to_string(detected_at) AS detected_at, to_string(notified_at) AS notified_at
FROM table(agentguard_notifications)
WHERE confirmed = false
ORDER BY notified_at DESC
LIMIT 20`

// ListUnconfirmedNotifications handles GET /api/notifications/unconfirmed.
// Best-effort: on query failure (e.g. stream not provisioned yet) it returns
// an empty list rather than an error, so the toast UI stays quiet.
func ListUnconfirmedNotifications(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := auth.RequestCfg(c, cfg)
		rows, err := tp.Query(&cfg.Timeplus, unconfirmedNotificationsSQL)
		if err != nil {
			log.Printf("notifications: unconfirmed query failed: %v", err)
			c.JSON(http.StatusOK, gin.H{"notifications": []any{}})
			return
		}
		out := make([]gin.H, 0, len(rows))
		for _, r := range rows {
			get := func(k string) string { s, _ := r[k].(string); return s }
			out = append(out, gin.H{
				"id":            notify.NotificationID(get("deployment_id"), get("agent_id"), get("session_id"), get("rule_id")),
				"rule_id":       get("rule_id"),
				"rule_name":     get("rule_name"),
				"severity":      get("severity"),
				"agent_id":      get("agent_id"),
				"deployment_id": get("deployment_id"),
				"session_id":    get("session_id"),
				"message":       get("message"),
				"detected_at":   get("detected_at"),
			})
		}
		c.JSON(http.StatusOK, gin.H{"notifications": out})
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run 'NotificationSQL|UnconfirmedNotifications' -v`
Expected: PASS

- [ ] **Step 5: Delete the webhook path**

- In `backend/internal/handlers/rules.go`: delete `threatOpenCheckSQL`, `isThreatAlreadyOpen`, and `AlertWebhook` (the contiguous block at ~761–825, including their doc comments).
- In `backend/internal/handlers/rules_handler_test.go`: delete `TestThreatOpenCheckSQL_EscapesBackslashQuoteBypass` (~lines 270–299) including the comment block above it. Remove the `"strings"` import only if the compiler flags it as unused.
- In `backend/internal/handlers/docs.go`: locate the `"/alerts/webhook"` path entry (search for `alerts/webhook`, block starting ~line 1250) and delete the whole path object (the JSON key and its `post` operation, keeping the surrounding JSON valid). Leave the `AlertWebhookPayload` schema — it still documents the SSE envelope.
- In `backend/internal/handlers/dlp_test.go` line ~143: remove `,{"name":"notify_agentguard_udf"}` from the mocked SHOW FUNCTIONS list.

- [ ] **Step 6: RBAC route map**

In `backend/internal/rbac/middleware.go`, in the `// dashboard (+ live activity / notification feeds)` group, add:

```go
	"GET /api/notifications/unconfirmed": ReadKey("dashboard"),
	// Confirming (dismissing) a toast is part of consuming the notification
	// feed, not threat triage — read permission is enough.
	"POST /api/notifications/confirm": ReadKey("dashboard"),
```

- [ ] **Step 7: Wire `main.go`**

In `backend/cmd/server/main.go`:

7a. Delete the webhook route and its comment (~lines 183–185):

```go
		// /alerts/webhook is called by Timeplus (server-to-server), not by the browser,
		// so it must be reachable without a session cookie.
		api.POST("/alerts/webhook", handlers.AlertWebhook(cfg))
```

7b. After the `protected.GET("/notifications/stream", …)` line, add:

```go
			protected.GET("/notifications/unconfirmed", handlers.ListUnconfirmedNotifications(cfg))
			protected.POST("/notifications/confirm", handlers.ConfirmNotification(cfg))
```

7c. Add `"github.com/timeplus/agentguard/internal/notify"` to the imports, and immediately before the `shutdownCh, stopSignals := shutdownSignalChannel()` line add:

```go
	// #258: tail agentguard_notify_events and toast new threats via SSE. The
	// watcher self-gates on cfg.SetupComplete and reconnects with backoff, so
	// it is safe to start unconditionally, before or after the setup wizard.
	watchCtx, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	go notify.NewWatcher(cfg, &notify.TimeplusStore{Cfg: cfg}, handlers.GlobalAlertBroadcaster).Run(watchCtx)
```

- [ ] **Step 8: Build + all backend tests**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...`
Expected: PASS. Also verify removal: `grep -rn "AlertWebhook(\|isThreatAlreadyOpen\|threatOpenCheckSQL" backend/ --include="*.go"` → only the `models.AlertWebhookPayload` type remains (in models/, notifications.go, holds.go, sentry.go, notify/).

- [ ] **Step 9: Commit**

```bash
git add backend/internal/handlers/notifications.go backend/internal/handlers/notifications_handler_test.go backend/internal/handlers/rules.go backend/internal/handlers/rules_handler_test.go backend/internal/handlers/docs.go backend/internal/handlers/dlp_test.go backend/internal/rbac/middleware.go backend/cmd/server/main.go
git commit -m "feat(notifications): unconfirmed/confirm endpoints, start notify watcher, remove alert webhook (#258)"
```

---

### Task 6: Retire the server-side `agentguard.url` config key

**Files:**
- Modify: `backend/cmd/server/main.go` (flag ~68, BindPFlag ~75, banner Printf ~142)
- Modify: `backend/internal/config/config.go` (field ~115, default ~160, assign ~240, env bind ~309, Save ~352)
- Modify: `backend/internal/config/sources.go` (validate ~155, EffectiveValue ~209, ApplyChange ~287)
- Modify: `backend/internal/config/registry.go` (section list ~44, key entry ~81–83)
- Modify tests: `backend/cmd/server/config_precedence_test.go`, `backend/internal/config/{registry_test.go,sources_test.go,config_loadfile_test.go,config_test.go}`
- Modify: `backend/internal/handlers/docs.go` (`agentguard_url` in provision schema/examples, ~1448 and ~2062)
- Modify: `frontend/src/pages/Setup.jsx` (state ~21, provision body ~54, field row ~124)
- Modify: `frontend/src/i18n/locales/en/onboarding.json` + `frontend/src/i18n/locales/zh-CN/onboarding.json` (ONLY the `setup.connection.fields.agentguardUrl` and `setup.connection.placeholders.agentguardUrl` keys, ~lines 172/178 — the `common.fields/placeholders.agentguardUrl` entries near lines 49/60 belong to the plugin-onboarding page and MUST stay)
- Modify: `frontend/src/i18n/locales/en/settings.json` + `frontend/src/i18n/locales/zh-CN/settings.json` (remove the `"agentguard_url": {...}` block, ~line 122)
- Modify: `docker-compose.yaml` (remove only the `AGENTGUARD_URL: "http://agentguard:8080"` line; the file has unrelated uncommitted local edits — leave them)

**Interfaces:**
- Consumes: Task 3 already removed all functional readers (`Init`, setup wizard persistence). This task deletes the plumbing.
- Produces: nothing — pure removal. `frontend/src/utils/settingsNav.test.js` uses `agentguard.url` as an arbitrary fixture key and keeps passing; leave it.

- [ ] **Step 1: Backend removal**

- `main.go`: delete the `flags.String("agentguard-url", …)` line, the `viper.BindPFlag("agentguard.url", …)` line, and the `fmt.Printf("AgentGuard webhook URL: %s\n", cfg.AgentGuardURL)` line.
- `config.go`: delete the `AgentGuardURL string` struct field, `v.SetDefault("agentguard.url", "http://localhost:8080")`, `cfg.AgentGuardURL = v.GetString("agentguard.url")`, `_ = v.BindEnv("agentguard.url", "AGENTGUARD_URL")`, and `v.Set("agentguard.url", cfg.AgentGuardURL)` in `Save`.
- `sources.go`: change `case "agentguard.url", "dlp.base_url":` to `case "dlp.base_url":`; delete the `case "agentguard.url": return cfg.AgentGuardURL` arm in `EffectiveValue`; delete the `case "agentguard.url": return setString(&cfg.AgentGuardURL, value)` arm in the setter.
- `registry.go`: delete the `{Key: "agentguard.url", …}` entry with its `// ── agentguard ──` comment, and remove `{"agentguard", "AgentGuard"}` from the sections list (~line 44).

- [ ] **Step 2: Fix the enumerating tests**

- `backend/internal/config/registry_test.go` ~line 16: remove `"agentguard.url",` from the expected key list.
- `backend/internal/config/sources_test.go` ~line 30: delete the `{"agentguard.url", "https://h:8080", false, "https://h:8080"},` case (the `dlp.base_url` cases keep covering URL validation).
- `backend/internal/config/config_test.go`: remove `AgentGuardURL: "http://example.com:8080",` (~17) and the round-trip assertion block (~61–63).
- `backend/internal/config/config_loadfile_test.go`: remove `"AGENTGUARD_URL"` from the env-scrub list (~23), the `agentguard:`/`url:` lines from any YAML fixture in that file, the assertion blocks at ~87–88 and ~137–138, and the `t.Setenv("AGENTGUARD_URL", …)` at ~103 (if a test exists *solely* to prove `AGENTGUARD_URL` env override, delete that whole test).
- `backend/cmd/server/config_precedence_test.go`: remove `"AGENTGUARD_URL",` from the scrub list (~17), the `agentguard:` YAML fixture lines, and the assertion at ~71–72.

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./cmd/... ./internal/config/...`
Expected: PASS

- [ ] **Step 3: docs.go schema cleanup**

In `backend/internal/handlers/docs.go`: remove `"agentguard_url": "http://localhost:8080"` from the provision example (~1448, fix the trailing comma) and the `"agentguard_url": { "type": "string", …}` property (~2062). Also update the `// Body:` comment for Provision if it still mentions `agentguard_url` anywhere in docs.go strings (`grep -n agentguard_url backend/internal/handlers/docs.go` must return nothing afterward).

- [ ] **Step 4: Frontend setup wizard**

- `frontend/src/pages/Setup.jsx`: delete the `const [agentGuardURL, setAgentGuardURL] = useState('http://localhost:8080')` line; delete `agentguard_url: agentGuardURL` from the `api.setupProvision({...})` call; delete the field-descriptor line `{ label: t('setup.connection.fields.agentguardUrl'), val: agentGuardURL, set: setAgentGuardURL, ph: t('setup.connection.placeholders.agentguardUrl'), type: 'text' },`.
- In both `frontend/src/i18n/locales/en/onboarding.json` and `frontend/src/i18n/locales/zh-CN/onboarding.json`: inside the `setup.connection` object only, remove the `"agentguardUrl"` entries from `fields` and `placeholders` (fix trailing commas). Do NOT touch `common.fields.agentguardUrl` / `common.placeholders.agentguardUrl`.
- In both `frontend/src/i18n/locales/en/settings.json` and `frontend/src/i18n/locales/zh-CN/settings.json`: remove the `"agentguard_url": { "label": …, "description": … },` block.

- [ ] **Step 5: docker-compose**

In `docker-compose.yaml`, delete only the line `AGENTGUARD_URL: "http://agentguard:8080"` (under the agentguard service's `environment:`). Preserve every other local modification in the file.

- [ ] **Step 6: Verify**

```bash
cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...
cd ../frontend && npm run lint && npm test && npm run build
grep -rn "agentguard.url\|AgentGuardURL" ../backend --include="*.go"
```
Expected: builds/tests/lint PASS; the grep returns nothing (integration tests' `AGENTGUARD_URL` env var — different string — is fine).

- [ ] **Step 7: Commit**

```bash
git add backend/cmd/server/main.go backend/cmd/server/config_precedence_test.go backend/internal/config backend/internal/handlers/docs.go frontend/src/pages/Setup.jsx frontend/src/i18n/locales/en/onboarding.json frontend/src/i18n/locales/zh-CN/onboarding.json frontend/src/i18n/locales/en/settings.json frontend/src/i18n/locales/zh-CN/settings.json docker-compose.yaml
git commit -m "chore(config): retire server-side agentguard.url webhook key (#258)"
```

---

### Task 7: Frontend — confirm on dismiss, backfill on connect

**Files:**
- Modify: `frontend/src/api/client.js` (two new api functions)
- Modify: `frontend/src/components/NotificationsProvider.jsx`

**Interfaces:**
- Consumes: `GET /api/notifications/unconfirmed` and `POST /api/notifications/confirm` (Task 5); SSE envelope now carries `session_id` and a stable `id` (Tasks 2/4).
- Produces: `api.getUnconfirmedNotifications()`, `api.confirmNotification(identity)`.

- [ ] **Step 1: api client**

In `frontend/src/api/client.js`, next to `listHolds` (~line 210), add:

```js
  // ── Notifications (#258) ───────────────────────────────────────────────────
  getUnconfirmedNotifications: () => get('/notifications/unconfirmed'),
  confirmNotification: (identity) => post('/notifications/confirm', identity),
```

- [ ] **Step 2: NotificationsProvider**

In `frontend/src/components/NotificationsProvider.jsx`:

2a. Make `push` idempotent on the stable id (replace the existing `push`):

```js
  const push = useCallback((notification) => {
    setToasts(prev => {
      if (prev.some(t => t.id === notification.id)) return prev
      return [notification, ...prev].slice(0, 5)
    })
    // Auto-dismiss after 6 seconds. Auto-dismiss does NOT confirm — an unseen
    // toast reappears via backfill on the next connect (that's the point of
    // the persisted seen-state).
    clearTimeout(timersRef.current[notification.id])
    timersRef.current[notification.id] = setTimeout(() => dismiss(notification.id), 6000)
  }, [dismiss])
```

2b. Add a `confirm` callback after `push` (explicit dismiss/click marks the notification seen server-side):

```js
  const confirm = useCallback((n) => {
    if (n.rule_id) {
      api.confirmNotification({
        agent_id: n.agent_id ?? '',
        deployment_id: n.deployment_id ?? '',
        session_id: n.session_id ?? '',
        rule_id: n.rule_id,
      }).catch(() => { /* best-effort — it will re-toast on next connect */ })
    }
    dismiss(n.id)
  }, [dismiss])
```

2c. Backfill on connect — replace `es.onopen`:

```js
    es.onopen = () => {
      if (cancelled) return
      reconcileHolds()
      // Backfill notifications delivered-but-never-confirmed (missed while
      // disconnected, auto-dismissed unseen, or from before a restart).
      api.getUnconfirmedNotifications()
        .then(r => { if (!cancelled) for (const n of (r.notifications ?? [])) push(n) })
        .catch(() => { /* best-effort */ })
    }
```

Also add `push` to that `useEffect`'s dependency array if the linter asks (it is already there: `[push, reconcileHolds]`).

2d. Route explicit dismissal through `confirm`: in the toast stack render, change `<Toast n={n} onDismiss={dismiss} />` to `<Toast n={n} onDismiss={confirm} />`, and in the `Toast` component change the ✕ button's `onClick={() => onDismiss(n.id)}` to `onClick={() => onDismiss(n)}` and add `onClick={() => onDismiss(n)}` to the "View threats" `<Link>` so click-through also confirms.

- [ ] **Step 3: Lint + build**

Run: `cd frontend && npm run lint && npm run build`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.js frontend/src/components/NotificationsProvider.jsx
git commit -m "feat(ui): confirm toasts on dismiss, backfill unconfirmed notifications on connect (#258)"
```

---

### Task 8: Documentation + final verification

**Files:**
- Modify: `CLAUDE.md`
- Modify: `docs/spec/openapi.yaml`, `docs/spec/api.md`
- Modify: `docs/design.md`
- Test: full-repo verification commands below

- [ ] **Step 1: CLAUDE.md**

- In the API endpoint table, replace the row
  `| POST | /api/alerts/webhook | AlertWebhook | Receives rule-triggered alerts (unauthenticated) |`
  with:
  ```
  | GET | `/api/notifications/unconfirmed` | `ListUnconfirmedNotifications` | Unconfirmed threat notifications for browser backfill (newest-first, ≤20) |
  | POST | `/api/notifications/confirm` | `ConfirmNotification` | Marks a notification seen (`{agent_id, deployment_id, session_id, rule_id}`); explicit toast dismiss/click only |
  ```
- In the stream table, add after the `agentguard_security_events` row:
  ```
  | `agentguard_notifications` | Persisted per-threat notification seen-state (#258); PK = threat identity, upserted by the backend notify watcher (`internal/notify`) and the confirm endpoint |
  ```
- In the `NotificationsProvider` component bullet, append: "Threat toasts are driven by a backend watcher (`internal/notify`) tailing `agentguard_notify_events` — there is no Timeplus→AgentGuard callback; explicit dismiss confirms via `/api/notifications/confirm`, and unconfirmed notifications backfill on SSE connect."

- [ ] **Step 2: docs/spec + docs/design.md**

- `docs/spec/openapi.yaml`: delete the `/alerts/webhook:` path block (~line 1838) and the `agentguard_url:` property/example in the provision schema (~3239, ~2185); remove `/alerts/webhook` from the unauthenticated-exceptions sentence (~line 18).
- `docs/spec/api.md`: remove the `/alerts/webhook` endpoint section (~3200–3260) and the `agentguard_url` rows/examples (~4101, 4156, 6440, 6456); fix the authentication paragraph (~line 30) to drop `/alerts/webhook`.
- `docs/design.md`: update the alert-flow references (~lines 63, 90, 322, 787): replace "Alert UDF callback → POST /api/alerts/webhook" wording with "backend notify watcher tails `agentguard_notify_events` (streaming SQL) → SSE fan-out", and delete/replace the Python `requests.post` UDF snippet at ~787 with a one-line note that #258 removed the UDF callback. Keep edits surgical — this is a historical design doc, a short "superseded by #258" note beats a rewrite.

- [ ] **Step 3: Full verification sweep**

```bash
cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...
cd ../frontend && npm run lint && npm test && npm run build
cd .. && grep -rn "alerts/webhook\|notify_agentguard_udf\|agentguard_notifier" \
  --include="*.go" --include="*.js" --include="*.jsx" --include="*.yaml" --include="*.yml" \
  backend/ frontend/src/ docker-compose.yaml CLAUDE.md docs/spec/
```
Expected: all builds/tests/lint PASS; the grep returns nothing (historical mentions under `docs/superpowers/`, `docs/design.md`'s superseded-note, and `docs/research/` are acceptable — everything else must be clean).

- [ ] **Step 4: Manual end-to-end (docker-compose or make dev, optional but recommended)**

1. Provision via the setup wizard; confirm the provision log shows `DROP ALERT IF EXISTS agentguard_notifier` / `DROP FUNCTION IF EXISTS notify_agentguard_udf` and no "notification UDF" step.
2. Trigger a rule (or `INSERT INTO agentguard_security_events (...)` a synthetic row) → exactly one toast appears.
3. Dismiss it (✕) → restart the server → reload: no re-toast.
4. Trigger a second distinct threat, let the toast auto-dismiss without clicking → reload the page → it re-toasts (unconfirmed backfill).
5. Create a hold → `hold_pending` badge still works (holds path untouched).

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md docs/spec/openapi.yaml docs/spec/api.md docs/design.md
git commit -m "docs: streaming notifications replace the alert webhook (#258)"
```
