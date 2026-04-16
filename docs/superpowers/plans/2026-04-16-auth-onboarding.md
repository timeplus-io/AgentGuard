# Auth + Onboarding Redesign — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add login/logout with Timeplus credentials, a first-time setup wizard that provisions all AgentGuard Timeplus resources, and persistent non-credential config — removing credentials from config entirely and moving resource creation from server startup to the setup wizard.

**Architecture:** Server-side session store (in-memory Go map) issues `agentguard_session` httpOnly cookies on login. `auth.RequestCfg(c, cfg)` assembles per-request Timeplus credentials from the session, injected into every existing handler with a single line. A new `/setup` wizard (3 steps: connection → credentials → provision) calls a refactored `Init()` that streams live progress via SSE. `config.yaml` stores host/port/setup_complete but never credentials.

**Tech Stack:** Go 1.23, Gin, proton-go-driver, React 18, Vite, Tailwind CSS. No new dependencies required.

---

## File Map

| File | Action | Responsibility |
|------|--------|----------------|
| `backend/internal/config/config.go` | Modify | Remove User/Password loading; add SetupComplete; add Save() |
| `backend/internal/config/config_test.go` | Create | Test Save() round-trips correctly |
| `backend/internal/auth/store.go` | Create | In-memory session store |
| `backend/internal/auth/store_test.go` | Create | Session store unit tests |
| `backend/internal/auth/middleware.go` | Create | AuthRequired middleware + RequestCfg helper |
| `backend/internal/auth/middleware_test.go` | Create | Middleware + RequestCfg tests |
| `backend/internal/auth/handlers.go` | Create | Login, Logout, AuthStatus handlers |
| `backend/internal/handlers/setup.go` | Create | TestConnection + Provision (SSE) handlers |
| `backend/internal/timeplus/init.go` | Modify | Init() accepts explicit TimeplusConfig + emit callback |
| `backend/cmd/server/main.go` | Modify | Remove Init(), add auth/setup routes, AuthRequired middleware |
| `backend/internal/handlers/dashboard.go` (and 11 others) | Modify | Add `cfg := auth.RequestCfg(c, cfg)` at top of each closure |
| `frontend/src/api/client.js` | Modify | Add credentials:include to all fetch; add auth + setup API calls |
| `frontend/src/context/AuthContext.jsx` | Create | Auth state: setupComplete, loggedIn, username |
| `frontend/src/pages/Login.jsx` | Create | Login form |
| `frontend/src/pages/Setup.jsx` | Create | 3-step setup wizard with SSE provision log |
| `frontend/src/App.jsx` | Modify | Wrap routes in AuthGuard; add /login and /setup |
| `frontend/src/components/TopNav.jsx` | Modify | Add username + logout button |

---

## Task 1: Config — Remove Credentials, Add SetupComplete, Add Save()

**Files:**
- Modify: `backend/internal/config/config.go`
- Create: `backend/internal/config/config_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/config/config_test.go`:

```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/timeplus/agentguard/internal/config"
)

func TestSave_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")

	cfg := &config.Config{
		Server:        config.ServerConfig{Port: 9090},
		Timeplus:      config.TimeplusConfig{Host: "myhost", Port: 3218, NativePort: 8463},
		AgentGuardURL: "http://example.com:8080",
		SetupComplete: true,
	}
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := config.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if loaded.Timeplus.Host != "myhost" {
		t.Errorf("host: want myhost, got %s", loaded.Timeplus.Host)
	}
	if !loaded.SetupComplete {
		t.Error("want setup_complete=true")
	}
	if loaded.Timeplus.User != "" {
		t.Errorf("user should not be saved, got %q", loaded.Timeplus.User)
	}
	if loaded.Timeplus.Password != "" {
		t.Errorf("password should not be saved, got %q", loaded.Timeplus.Password)
	}
	if loaded.Server.Port != 9090 {
		t.Errorf("port: want 9090, got %d", loaded.Server.Port)
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd backend
CGO_ENABLED=0 go test ./internal/config/... -run TestSave_RoundTrip -v
```

Expected: FAIL — `config.Save` and `config.LoadFrom` are undefined.

- [ ] **Step 3: Update `backend/internal/config/config.go`**

Replace the entire file:

```go
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type TimeplusConfig struct {
	Host       string
	Port       int
	NativePort int
	// User and Password are NOT loaded from config file or env vars.
	// They are populated at runtime: by auth.RequestCfg (from the session)
	// or by handlers that receive them directly (setup/provision).
	User     string
	Password string
}

type ServerConfig struct {
	Port int
}

type Config struct {
	Server        ServerConfig
	Timeplus      TimeplusConfig
	CatalogDir    string
	AgentGuardURL string
	SetupComplete bool
}

// Load reads configuration from (in order of increasing precedence):
//  1. Built-in defaults
//  2. Config file (config.yaml in working dir or $HOME/.agentguard)
//  3. Environment variables prefixed with TIMEPLUS_ or SERVER_
//  4. CLI flags (bound by the caller via viper.BindPFlag)
//
// Credentials (TIMEPLUS_USER, TIMEPLUS_PASSWORD) are intentionally not loaded.
func Load() (*Config, error) {
	return load(viper.New(), true)
}

// LoadFrom reads configuration from a specific file path. Used in tests.
func LoadFrom(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}
	return load(v, false)
}

func load(v *viper.Viper, searchPaths bool) (*Config, error) {
	v.SetDefault("server.port", 8080)
	v.SetDefault("catalog.dir", "./catalog")
	v.SetDefault("agentguard.url", "http://localhost:8080")
	v.SetDefault("timeplus.host", "localhost")
	v.SetDefault("timeplus.port", 3218)
	v.SetDefault("timeplus.native-port", 8463)
	v.SetDefault("setup.complete", false)

	if searchPaths {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.agentguard")

		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				return nil, fmt.Errorf("reading config file: %w", err)
			}
		}

		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		v.AutomaticEnv()
	}

	cfg := &Config{}
	cfg.Server.Port = v.GetInt("server.port")
	cfg.CatalogDir = v.GetString("catalog.dir")
	cfg.AgentGuardURL = v.GetString("agentguard.url")
	cfg.Timeplus.Host = v.GetString("timeplus.host")
	cfg.Timeplus.Port = v.GetInt("timeplus.port")
	cfg.Timeplus.NativePort = v.GetInt("timeplus.native-port")
	cfg.SetupComplete = v.GetBool("setup.complete")
	// Credentials are NOT loaded — always empty in the file-based config.

	return cfg, nil
}

// Save writes non-sensitive config fields to the specified path (typically "config.yaml").
// Credentials are never written.
func Save(cfg *Config, path string) error {
	v := viper.New()
	v.SetConfigType("yaml")
	v.Set("server.port", cfg.Server.Port)
	v.Set("catalog.dir", cfg.CatalogDir)
	v.Set("agentguard.url", cfg.AgentGuardURL)
	v.Set("timeplus.host", cfg.Timeplus.Host)
	v.Set("timeplus.port", cfg.Timeplus.Port)
	v.Set("timeplus.native-port", cfg.Timeplus.NativePort)
	v.Set("setup.complete", cfg.SetupComplete)
	return v.WriteConfigAs(path)
}
```

- [ ] **Step 4: Run the test**

```bash
cd backend
CGO_ENABLED=0 go test ./internal/config/... -run TestSave_RoundTrip -v
```

Expected: PASS

- [ ] **Step 5: Verify the package still compiles**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: errors about `cfg.Timeplus.User` in `main.go` and `init.go` — this is expected; we'll fix those in later tasks.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go
git commit -m "feat(config): remove credential loading, add SetupComplete + Save()"
```

---

## Task 2: Auth — Session Store

**Files:**
- Create: `backend/internal/auth/store.go`
- Create: `backend/internal/auth/store_test.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/auth/store_test.go`:

```go
package auth_test

import (
	"testing"

	"github.com/timeplus/agentguard/internal/auth"
)

func TestSessionStore_CreateAndGet(t *testing.T) {
	store := auth.NewSessionStore()
	sess := store.Create("alice", "s3cret")

	if sess.ID == "" {
		t.Fatal("expected non-empty session ID")
	}
	if sess.User != "alice" {
		t.Errorf("user: want alice, got %s", sess.User)
	}

	got, ok := store.Get(sess.ID)
	if !ok {
		t.Fatal("expected session to exist after Create")
	}
	if got.User != "alice" {
		t.Errorf("got user: want alice, got %s", got.User)
	}
	if got.Password != "s3cret" {
		t.Errorf("got password: want s3cret, got %s", got.Password)
	}
}

func TestSessionStore_Delete(t *testing.T) {
	store := auth.NewSessionStore()
	sess := store.Create("bob", "pw")
	store.Delete(sess.ID)

	_, ok := store.Get(sess.ID)
	if ok {
		t.Error("expected session to be gone after Delete")
	}
}

func TestSessionStore_GetMissing(t *testing.T) {
	store := auth.NewSessionStore()
	_, ok := store.Get("nonexistent-id")
	if ok {
		t.Error("expected false for missing session")
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd backend
CGO_ENABLED=0 go test ./internal/auth/... -run TestSessionStore -v
```

Expected: FAIL — package `auth` doesn't exist yet.

- [ ] **Step 3: Create `backend/internal/auth/store.go`**

```go
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Session holds the runtime credentials for a logged-in user.
// Password is held in memory only — never written to disk.
type Session struct {
	ID       string
	User     string
	Password string
	Created  time.Time
}

// SessionStore is a concurrency-safe in-memory session map.
// Sessions are lost on server restart; users re-login (acceptable for an ops tool).
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*Session)}
}

// Create generates a new session for the given user and adds it to the store.
func (s *SessionStore) Create(user, password string) *Session {
	id := randomID()
	sess := &Session{
		ID:       id,
		User:     user,
		Password: password,
		Created:  time.Now(),
	}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess
}

// Get looks up a session by ID. Returns (session, true) if found.
func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()
	return sess, ok
}

// Delete removes a session by ID. No-op if the session doesn't exist.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

func randomID() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
```

- [ ] **Step 4: Run the tests**

```bash
cd backend
CGO_ENABLED=0 go test ./internal/auth/... -run TestSessionStore -v
```

Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/
git commit -m "feat(auth): add in-memory session store"
```

---

## Task 3: Auth — Middleware + RequestCfg

**Files:**
- Create: `backend/internal/auth/middleware.go`
- Create: `backend/internal/auth/middleware_test.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/auth/middleware_test.go`:

```go
package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/config"
)

func init() { gin.SetMode(gin.TestMode) }

func TestAuthRequired_NoCookie(t *testing.T) {
	store := auth.NewSessionStore()
	r := gin.New()
	r.Use(auth.AuthRequired(store))
	r.GET("/test", func(c *gin.Context) { c.JSON(200, nil) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestAuthRequired_InvalidCookie(t *testing.T) {
	store := auth.NewSessionStore()
	r := gin.New()
	r.Use(auth.AuthRequired(store))
	r.GET("/test", func(c *gin.Context) { c.JSON(200, nil) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: "agentguard_session", Value: "bogus-id"})
	r.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Errorf("want 401, got %d", w.Code)
	}
}

func TestAuthRequired_ValidSession_InjectsCredentials(t *testing.T) {
	store := auth.NewSessionStore()
	sess := store.Create("alice", "s3cret")

	r := gin.New()
	r.Use(auth.AuthRequired(store))
	r.GET("/test", func(c *gin.Context) {
		user, _ := c.Get("timeplus_user")
		pass, _ := c.Get("timeplus_password")
		c.JSON(200, gin.H{"user": user, "pass": pass})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: "agentguard_session", Value: sess.ID})
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("want 200, got %d", w.Code)
	}
}

func TestRequestCfg_InjectsFromContext(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("timeplus_user", "alice")
	c.Set("timeplus_password", "s3cret")

	base := &config.Config{
		Timeplus: config.TimeplusConfig{Host: "localhost", Port: 3218, NativePort: 8463},
	}
	got := auth.RequestCfg(c, base)

	if got.Timeplus.User != "alice" {
		t.Errorf("user: want alice, got %s", got.Timeplus.User)
	}
	if got.Timeplus.Password != "s3cret" {
		t.Errorf("password: want s3cret, got %s", got.Timeplus.Password)
	}
	if got.Timeplus.Host != "localhost" {
		t.Error("host should be preserved from base config")
	}
	// Verify base config is not mutated
	if base.Timeplus.User != "" {
		t.Error("base config should not be mutated")
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd backend
CGO_ENABLED=0 go test ./internal/auth/... -run "TestAuthRequired|TestRequestCfg" -v
```

Expected: FAIL — `auth.AuthRequired` and `auth.RequestCfg` are undefined.

- [ ] **Step 3: Create `backend/internal/auth/middleware.go`**

```go
package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
)

// AuthRequired is a Gin middleware that requires a valid agentguard_session cookie.
// On success it injects "timeplus_user" and "timeplus_password" into the context.
func AuthRequired(store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("agentguard_session")
		if err != nil || cookie == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
			return
		}
		sess, ok := store.Get(cookie)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "session expired"})
			return
		}
		c.Set("timeplus_user", sess.User)
		c.Set("timeplus_password", sess.Password)
		c.Next()
	}
}

// RequestCfg returns a copy of cfg with Timeplus credentials populated from
// the current request's session context. Call this at the top of every
// handler closure that queries Timeplus.
func RequestCfg(c *gin.Context, cfg *config.Config) *config.Config {
	copy := *cfg
	if user, exists := c.Get("timeplus_user"); exists {
		copy.Timeplus.User = user.(string)
	}
	if pass, exists := c.Get("timeplus_password"); exists {
		copy.Timeplus.Password = pass.(string)
	}
	return &copy
}
```

- [ ] **Step 4: Run the tests**

```bash
cd backend
CGO_ENABLED=0 go test ./internal/auth/... -v
```

Expected: PASS (all 7 tests across store_test.go and middleware_test.go)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth/middleware.go backend/internal/auth/middleware_test.go
git commit -m "feat(auth): add AuthRequired middleware and RequestCfg helper"
```

---

## Task 4: Auth — Login, Logout, Status Handlers

**Files:**
- Create: `backend/internal/auth/handlers.go`

- [ ] **Step 1: Create `backend/internal/auth/handlers.go`**

```go
package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// AuthStatus returns the current setup and login state.
// This is the first call the frontend makes on load.
func AuthStatus(cfg *config.Config, store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := gin.H{
			"setup_complete": cfg.SetupComplete,
			"logged_in":      false,
			"username":       "",
		}
		if cookie, err := c.Cookie("agentguard_session"); err == nil {
			if sess, ok := store.Get(cookie); ok {
				status["logged_in"] = true
				status["username"] = sess.User
			}
		}
		c.JSON(http.StatusOK, status)
	}
}

// Login validates Timeplus credentials and creates a session.
// Body: {"username": "...", "password": "..."}
func Login(cfg *config.Config, store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Username string `json:"username" binding:"required"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "username is required"})
			return
		}

		// Validate by running a lightweight query against Timeplus with these creds.
		testCfg := config.TimeplusConfig{
			Host:     cfg.Timeplus.Host,
			Port:     cfg.Timeplus.Port,
			User:     body.Username,
			Password: body.Password,
		}
		if _, err := tp.Query(&testCfg, "SELECT 1 AS ok"); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}

		sess := store.Create(body.Username, body.Password)
		// httpOnly=true keeps the cookie out of JavaScript; SameSite=Strict prevents CSRF.
		c.SetCookie("agentguard_session", sess.ID, 0, "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{"username": body.Username})
	}
}

// Logout destroys the current session and clears the cookie.
func Logout(store *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cookie, err := c.Cookie("agentguard_session"); err == nil {
			store.Delete(cookie)
		}
		// MaxAge=-1 instructs the browser to delete the cookie immediately.
		c.SetCookie("agentguard_session", "", -1, "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd backend
CGO_ENABLED=0 go build ./internal/auth/...
```

Expected: compiles cleanly (no Timeplus connection needed to compile).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/auth/handlers.go
git commit -m "feat(auth): add Login, Logout, AuthStatus handlers"
```

---

## Task 5: Timeplus Init() — Add Explicit Credentials + Emit Callback

**Files:**
- Modify: `backend/internal/timeplus/init.go`

The goal: `Init()` now receives `timeplusCfg config.TimeplusConfig` (with User/Password set from the setup form) instead of reading from the file-based `cfg`. It also receives an `emit func(status, msg string)` callback for live progress reporting.

- [ ] **Step 1: Add `connectOnce` and update `Init()` signature in `backend/internal/timeplus/init.go`**

Replace the top of `init.go` (the `Init` and `waitForTimeplus` functions) with:

```go
// Init provisions all AgentGuard resources in Timeplus. It is called once from
// the /api/setup/provision handler, not from server startup.
//
//   - timeplusCfg carries the connection details AND credentials from the setup form.
//   - agentGuardURL is the webhook base URL (e.g. "http://localhost:8080").
//   - catalogEntries seeds the agentguard_rules stream on first run.
//   - emit is called for each provisioning event; pass nil to use log.Printf only.
func Init(timeplusCfg config.TimeplusConfig, agentGuardURL string, catalogEntries []catalog.Entry, emit func(status, msg string)) error {
	if emit == nil {
		emit = func(status, msg string) { log.Printf("[provision:%s] %s", status, msg) }
	}

	emit("running", "$ Connecting to Timeplus at "+timeplusCfg.Host+"…")
	conn, err := connectOnce(&timeplusCfg)
	if err != nil {
		emit("error", "  ✗ "+err.Error())
		return fmt.Errorf("timeplus init: connect: %w", err)
	}
	emit("ok", "  ✓ Connected")
	defer conn.Close()

	ctx := context.Background()

	emit("running", "$ Creating OTel streams…")
	if err := createOTelStreams(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating otel streams: %w", err)
	}
	emit("running", "$ Creating OTel input…")
	if err := createOTelInput(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating otel input: %w", err)
	}
	emit("running", "$ Creating AgentGuard streams…")
	if err := createAgentGuardStreams(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating agentguard streams: %w", err)
	}
	emit("running", "$ Creating CIM stream…")
	if err := createCIMStream(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating CIM stream: %w", err)
	}
	emit("running", "$ Creating CIM materialized views…")
	if err := createCIMMVs(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating CIM MVs: %w", err)
	}
	emit("running", "$ Creating CIM metrics streams…")
	if err := createCIMMetricsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating CIM metrics stream: %v", err)
	}
	emit("running", "$ Creating CIM metrics materialized views…")
	if err := createCIMMetricsMVs(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating CIM metrics MVs: %v", err)
	}
	emit("running", "$ Creating security streams…")
	if err := createSecurityStreams(ctx, conn, emit); err != nil {
		return fmt.Errorf("timeplus init: creating security streams: %w", err)
	}
	if err := createThreatsStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating threats stream: %v", err)
	}
	if err := createThreatsMV(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating threats MV: %v", err)
	}
	if err := createPricingStream(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating pricing stream: %v", err)
	}
	if err := seedPricingData(ctx, conn); err != nil {
		log.Printf("timeplus init: warning seeding pricing: %v", err)
	}
	if err := seedCatalogRules(ctx, conn, catalogEntries); err != nil {
		log.Printf("timeplus init: warning seeding rules: %v", err)
	}
	emit("running", "$ Creating notification UDF and alert…")
	if err := createNotifyUDF(ctx, conn, agentGuardURL); err != nil {
		log.Printf("timeplus init: warning creating UDF: %v", err)
	}
	if err := createNotifyFilterMV(ctx, conn, emit); err != nil {
		log.Printf("timeplus init: warning creating notify filter MV: %v", err)
	}
	if err := createNotifierAlert(ctx, conn); err != nil {
		log.Printf("timeplus init: warning creating alert: %v", err)
	}
	emit("running", "$ Syncing rule materialized views…")
	if err := startupSyncMVs(&timeplusCfg, ctx, conn); err != nil {
		log.Printf("timeplus init: warning syncing MVs: %v", err)
	}
	emit("ok", "  ✓ All resources provisioned")
	return nil
}

// connectOnce opens a single Timeplus connection attempt (no retry loop).
// Used during provisioning where the connection was already verified by test-connection.
func connectOnce(cfg *config.TimeplusConfig) (proton.Conn, error) {
	opts := &proton.Options{
		Addr: []string{fmt.Sprintf("%s:%d", cfg.Host, cfg.NativePort)},
		Auth: proton.Auth{
			Username: cfg.User,
			Password: cfg.Password,
		},
	}
	conn, err := proton.Open(opts)
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(context.Background()); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}
```

Keep `waitForTimeplus` in the file — it is still used by `startupSyncMVs` (which is called from within Init). Actually wait — `startupSyncMVs` takes `cfg *config.TimeplusConfig` and an already-open `conn`. It does NOT call `waitForTimeplus`. So `waitForTimeplus` is now unused. Delete it.

- [ ] **Step 2: Update all internal helper functions to accept `emit`**

Update `execIfNotExists` to report each resource:

```go
func execIfNotExists(ctx context.Context, conn proton.Conn, kind, name, ddl string, emit func(string, string)) error {
	var exists bool
	var err error
	switch kind {
	case "stream", "view":
		exists, err = streamExists(ctx, conn, name)
	default:
		exists, err = inputExists(ctx, conn, name)
	}
	if err != nil {
		return fmt.Errorf("checking %s %q: %w", kind, name, err)
	}
	if exists {
		emit("ok", "  ✓ "+name+" (already exists)")
		return nil
	}
	if err := conn.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("creating %s %q: %w", kind, name, err)
	}
	emit("ok", "  ✓ "+name)
	return nil
}
```

Update each group function to pass `emit` down to `execIfNotExists`. Here are all the functions that need `emit func(string,string)` added as a parameter:

`createOTelStreams`:
```go
func createOTelStreams(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	streams := []struct {
		name string
		ddl  string
	}{
		{"otel_traces", ddlOTelTraces},
		{"otel_logs", ddlOTelLogs},
		{"otel_metrics_gauge", ddlOTelMetricsGauge},
		{"otel_metrics_sum", ddlOTelMetricsSum},
		{"otel_metrics_histogram", ddlOTelMetricsHistogram},
		{"otel_metrics_exponential_histogram", ddlOTelMetricsExponentialHistogram},
		{"otel_metrics_summary", ddlOTelMetricsSummary},
	}
	for _, s := range streams {
		if err := execIfNotExists(ctx, conn, "stream", s.name, s.ddl, emit); err != nil {
			return err
		}
	}
	return nil
}
```

`createOTelInput`:
```go
func createOTelInput(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "input", "openclaw_otel_input", ddlOTelInput, emit)
}
```

`createAgentGuardStreams`:
```go
func createAgentGuardStreams(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_hook_events", ddlAgentGuardHookEvents, emit)
}
```

`createCIMStream`:
```go
func createCIMStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_cim_event", ddlCIMEvent, emit)
}
```

`createCIMMVs`:
```go
func createCIMMVs(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	mvs := []struct {
		name string
		ddl  string
	}{
		{"mv_cim_claudecode", ddlCIMClaudeCodeMV},
		{"mv_cim_openclaw", ddlCIMOpenClawMV},
	}
	for _, mv := range mvs {
		if err := execIfNotExists(ctx, conn, "view", mv.name, mv.ddl, emit); err != nil {
			return err
		}
	}
	return nil
}
```

`createCIMMetricsStream`:
```go
func createCIMMetricsStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_session_agents", ddlSessionAgentsStream, emit); err != nil {
		return err
	}
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_run_latency", ddlRunLatencyStream, emit); err != nil {
		return err
	}
	return execIfNotExists(ctx, conn, "stream", "agentguard_cim_metrics", ddlCIMMetrics, emit)
}
```

`createCIMMetricsMVs`:
```go
func createCIMMetricsMVs(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	mvs := []struct {
		name string
		ddl  string
	}{
		{"mv_session_agents", ddlSessionAgentsMV},
		{"mv_run_latency", ddlRunLatencyMV},
		{"mv_cim_metrics_openclaw_llm", ddlCIMMetricsOpenClawLlmMV},
		{"mv_cim_metrics_openclaw_tools", ddlCIMMetricsOpenClawToolsMV},
		{"mv_cim_metrics_claudecode_otel", ddlCIMMetricsClaudeCodeOtelMV},
		{"mv_cim_metrics_claudecode_tools", ddlCIMMetricsClaudeCodeToolsMV},
	}
	for _, mv := range mvs {
		if err := execIfNotExists(ctx, conn, "view", mv.name, mv.ddl, emit); err != nil {
			return err
		}
	}
	return nil
}
```

`createSecurityStreams`:
```go
func createSecurityStreams(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_security_events", ddlSecurityEvents, emit); err != nil {
		return err
	}
	if err := execIfNotExists(ctx, conn, "stream", "agentguard_notify_events", ddlNotifyEventsStream, emit); err != nil {
		return err
	}
	return execIfNotExists(ctx, conn, "stream", "agentguard_rules", ddlRulesStream, emit)
}
```

`createThreatsStream`:
```go
func createThreatsStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_threats", ddlThreatsStream, emit)
}
```

`createThreatsMV`:
```go
func createThreatsMV(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "view", "mv_threats", ddlThreatsMV, emit)
}
```

`createPricingStream`:
```go
func createPricingStream(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "agentguard_pricing", ddlPricingStream, emit)
}
```

`createNotifyFilterMV`:
```go
func createNotifyFilterMV(ctx context.Context, conn proton.Conn, emit func(string, string)) error {
	return execIfNotExists(ctx, conn, "stream", "mv_notify_filter", ddlNotifyFilterMV, emit)
}
```

`seedPricingData` and `seedCatalogRules` and `createNotifyUDF` and `createNotifierAlert` don't use `execIfNotExists` — leave them unchanged (they log internally).

Also delete `waitForTimeplus` — it is no longer called from anywhere.

- [ ] **Step 3: Verify compilation**

```bash
cd backend
CGO_ENABLED=0 go build ./internal/timeplus/...
```

Expected: compiles cleanly. Fix any missed `emit` parameter calls until it compiles.

- [ ] **Step 4: Run existing tests**

```bash
cd backend
CGO_ENABLED=0 go test ./...
```

Expected: all existing tests pass (timeline tests, config tests, auth tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/timeplus/init.go
git commit -m "feat(timeplus): Init() accepts explicit credentials and emit callback"
```

---

## Task 6: Setup Handlers — TestConnection + Provision (SSE)

**Files:**
- Create: `backend/internal/handlers/setup.go`

- [ ] **Step 1: Create `backend/internal/handlers/setup.go`**

```go
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	tp "github.com/timeplus/agentguard/internal/timeplus"
)

// TestConnection validates Timeplus credentials without creating a session.
// Body: {"host":"localhost","port":3218,"username":"default","password":""}
func TestConnection() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Host     string `json:"host"     binding:"required"`
			Port     int    `json:"port"     binding:"required"`
			Username string `json:"username" binding:"required"`
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}

		testCfg := config.TimeplusConfig{
			Host:     body.Host,
			Port:     body.Port,
			User:     body.Username,
			Password: body.Password,
		}
		if _, err := tp.Query(&testCfg, "SELECT 1 AS ok"); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

type provisionEvent struct {
	Status string `json:"status"` // "running" | "ok" | "error" | "done"
	Msg    string `json:"msg"`
}

// Provision runs Init() in-process and streams progress as SSE events.
// On success it writes config.yaml and the frontend then calls /api/auth/login.
//
// Body: {"host":"...","port":3218,"native_port":8463,"username":"...","password":"...","agentguard_url":"..."}
func Provision(cfg *config.Config, mgr *catalog.Manager, store *auth.SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Host          string `json:"host"           binding:"required"`
			Port          int    `json:"port"           binding:"required"`
			NativePort    int    `json:"native_port"    binding:"required"`
			Username      string `json:"username"       binding:"required"`
			Password      string `json:"password"`
			AgentGuardURL string `json:"agentguard_url"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// SSE headers must be set before the first write.
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no") // disable nginx buffering

		w := c.Writer
		flusher, ok := w.(http.Flusher)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
			return
		}

		sendEvent := func(status, msg string) {
			data, _ := json.Marshal(provisionEvent{Status: status, Msg: msg})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		agentGuardURL := body.AgentGuardURL
		if agentGuardURL == "" {
			agentGuardURL = cfg.AgentGuardURL
		}

		timeplusCfg := config.TimeplusConfig{
			Host:       body.Host,
			Port:       body.Port,
			NativePort: body.NativePort,
			User:       body.Username,
			Password:   body.Password,
		}

		if err := tp.Init(timeplusCfg, agentGuardURL, mgr.All(), sendEvent); err != nil {
			sendEvent("error", "Setup failed: "+err.Error())
			return
		}

		// Persist non-credential config so server knows setup is done after restart.
		newCfg := *cfg
		newCfg.Timeplus.Host = body.Host
		newCfg.Timeplus.Port = body.Port
		newCfg.Timeplus.NativePort = body.NativePort
		newCfg.AgentGuardURL = agentGuardURL
		newCfg.SetupComplete = true
		if err := config.Save(&newCfg, "config.yaml"); err != nil {
			sendEvent("error", "Failed to save config: "+err.Error())
			return
		}

		// Update the live cfg pointer so /api/auth/status immediately reflects setup_complete=true.
		cfg.Timeplus.Host = body.Host
		cfg.Timeplus.Port = body.Port
		cfg.Timeplus.NativePort = body.NativePort
		cfg.AgentGuardURL = agentGuardURL
		cfg.SetupComplete = true

		// Session is created by the frontend calling /api/auth/login after receiving "done".
		sendEvent("done", "Setup complete")
	}
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd backend
CGO_ENABLED=0 go build ./internal/handlers/...
```

Expected: compiles cleanly.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/handlers/setup.go
git commit -m "feat(handlers): add TestConnection and Provision (SSE) setup handlers"
```

---

## Task 7: Main.go — Wire Auth + Setup, Remove Startup Init()

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Update `backend/cmd/server/main.go`**

Replace the entire file:

```go
package main

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/timeplus/agentguard/internal/auth"
	"github.com/timeplus/agentguard/internal/catalog"
	"github.com/timeplus/agentguard/internal/config"
	"github.com/timeplus/agentguard/internal/handlers"
	"github.com/timeplus/agentguard/internal/web"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agentguard",
		Short: "AgentGuard — AI agent security monitoring platform",
		RunE:  runServer,
	}

	flags := cmd.Flags()
	flags.String("config", "", "config file (default: ./config.yaml)")
	flags.Int("port", 8080, "server port")
	flags.String("timeplus-host", "localhost", "Timeplus host")
	flags.Int("timeplus-port", 3218, "Timeplus HTTP port")
	flags.Int("timeplus-native-port", 8463, "Timeplus native TCP port")
	flags.String("catalog-dir", "", "catalog directory (default: ./catalog)")
	flags.String("agentguard-url", "", "URL Timeplus uses to call back AgentGuard (default: http://localhost:8080)")

	_ = viper.BindPFlag("server.port", flags.Lookup("port"))
	_ = viper.BindPFlag("catalog.dir", flags.Lookup("catalog-dir"))
	_ = viper.BindPFlag("timeplus.host", flags.Lookup("timeplus-host"))
	_ = viper.BindPFlag("timeplus.port", flags.Lookup("timeplus-port"))
	_ = viper.BindPFlag("timeplus.native-port", flags.Lookup("timeplus-native-port"))
	_ = viper.BindPFlag("agentguard.url", flags.Lookup("agentguard-url"))

	cmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cfgFile, _ := flags.GetString("config"); cfgFile != "" {
			viper.SetConfigFile(cfgFile)
		}
		return nil
	}

	return cmd
}

func runServer(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	frontendFS, err := web.FS()
	if err != nil {
		return fmt.Errorf("loading frontend assets: %w", err)
	}

	mgr, err := catalog.New(catalog.SeedFS, cfg.CatalogDir)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}

	fmt.Printf("AgentGuard starting on :%d\n", cfg.Server.Port)
	fmt.Printf("Timeplus endpoint: %s:%d (native: %d)\n", cfg.Timeplus.Host, cfg.Timeplus.Port, cfg.Timeplus.NativePort)
	fmt.Printf("Catalog directory: %s (%d entries)\n", cfg.CatalogDir, len(mgr.All()))
	fmt.Printf("AgentGuard webhook URL: %s\n", cfg.AgentGuardURL)
	fmt.Printf("Setup complete: %v\n", cfg.SetupComplete)
	// Note: timeplus.Init() is no longer called at startup.
	// All Timeplus resources are provisioned during the one-time /api/setup/provision flow.

	store := auth.NewSessionStore()

	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
	}))

	r.GET("/docs", handlers.APIDocs())
	r.GET("/docs/redoc", handlers.APIRedoc())
	r.GET("/docs/openapi.json", handlers.APISpec())

	api := r.Group("/api")
	{
		// Unauthenticated — must be reachable before login/setup.
		api.GET("/auth/status", auth.AuthStatus(cfg, store))
		api.POST("/auth/login", auth.Login(cfg, store))
		api.POST("/auth/logout", auth.Logout(store))
		api.POST("/setup/test-connection", handlers.TestConnection())
		api.POST("/setup/provision", handlers.Provision(cfg, mgr, store))

		// All existing routes — require a valid session cookie.
		protected := api.Group("/", auth.AuthRequired(store))
		{
			protected.GET("/dashboard", handlers.GetDashboard(cfg))
			protected.GET("/agents", handlers.ListAgents(cfg))
			protected.GET("/agents/:id", handlers.GetAgent(cfg))
			protected.GET("/agents/:id/sessions", handlers.ListSessions(cfg))
			protected.GET("/agents/:id/sessions/:session_id", handlers.GetSession(cfg))
			protected.GET("/agents/:id/sessions/:session_id/runs/:run_id/trace", handlers.GetRunTrace(cfg))
			protected.GET("/agents/:id/events", handlers.ListEvents(cfg))
			protected.GET("/metrics/llm", handlers.GetLLMMetrics(cfg))
			protected.GET("/metrics/tools", handlers.GetToolMetrics(cfg))
			protected.GET("/threats", handlers.ListThreats(cfg))
			protected.GET("/threats/:id", handlers.GetThreat(cfg))
			protected.POST("/threats/:id/acknowledge", handlers.AcknowledgeThreat(cfg))
			protected.POST("/threats/:id/clear", handlers.ClearThreat(cfg))
			protected.GET("/cost", handlers.GetCost(cfg))
			protected.GET("/cost/pricing", handlers.ListPricing(cfg))
			protected.POST("/cost/pricing", handlers.UpsertPricing(cfg))
			protected.DELETE("/cost/pricing/:provider/:model", handlers.DeletePricing(cfg))
			protected.GET("/rules", handlers.ListRules(cfg, mgr))
			protected.GET("/rules/catalog", handlers.ListCatalog(cfg, mgr))
			protected.POST("/rules/catalog/import", handlers.ImportCatalog(mgr))
			protected.GET("/rules/catalog/export", handlers.ExportCatalog(mgr))
			protected.GET("/rules/mv-status", handlers.MVStatus(cfg, mgr))
			protected.POST("/rules/sync", handlers.SyncMVs(cfg, mgr))
			protected.POST("/rules", handlers.CreateRule(cfg, mgr))
			protected.GET("/rules/:id", handlers.GetRule(cfg, mgr))
			protected.POST("/rules/:id/install", handlers.InstallRule(cfg, mgr))
			protected.POST("/rules/:id/toggle", handlers.ToggleRule(cfg, mgr))
			protected.POST("/rules/:id/upgrade", handlers.UpgradeRule(cfg, mgr))
			protected.DELETE("/rules/:id", handlers.UninstallRule(cfg, mgr))
			protected.POST("/alerts/webhook", handlers.AlertWebhook(cfg))
			protected.GET("/notifications/stream", handlers.NotificationsSSEHandler())
			protected.GET("/investigate/tables", handlers.ListTables(cfg))
			protected.POST("/onboarding/generate", handlers.GenerateConfig)
			protected.POST("/proton/query", handlers.ProtonQueryHandler(cfg))
		}
	}

	r.NoRoute(func(c *gin.Context) {
		name := strings.TrimPrefix(c.Request.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		data, err := fs.ReadFile(frontendFS, name)
		if err != nil {
			data, err = fs.ReadFile(frontendFS, "index.html")
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
			name = "index.html"
		}
		mimeType := mime.TypeByExtension(filepath.Ext(name))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		c.Data(http.StatusOK, mimeType, data)
	})

	return r.Run(fmt.Sprintf(":%d", cfg.Server.Port))
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd backend
CGO_ENABLED=0 go build ./cmd/server/...
```

Expected: compiles cleanly.

- [ ] **Step 3: Run all tests**

```bash
cd backend
CGO_ENABLED=0 go test ./...
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/server/main.go
git commit -m "feat(server): wire auth/setup routes, remove startup Init(), require session for all API routes"
```

---

## Task 8: Existing Handlers — Inject Session Credentials

Every handler that calls `tp.Query` or `tp.ExecHTTP` needs to use session credentials instead of the empty `cfg.Timeplus.User/Password`. The fix is one line at the top of each handler closure.

**Files to modify** (all in `backend/internal/handlers/`):
- `dashboard.go`
- `agents.go`
- `agent_sessions.go`
- `agent_events.go`
- `cost.go`
- `threats.go`
- `metrics.go`
- `rules.go`
- `investigate.go`
- `proton.go`
- `timeline.go`
- `timeline_openclaw.go`
- `timeline_fallback.go`

**Files that do NOT need this change** (don't use `tp.Query`/`tp.ExecHTTP`):
- `catalog.go` — in-memory catalog only
- `onboarding.go` — generates config text
- `notifications.go` — SSE push, no Timeplus query
- `docs.go` — serves API docs
- `helpers.go` — utility functions

- [ ] **Step 1: Apply the pattern to each handler**

For every function of the form `func SomeName(cfg *config.Config) gin.HandlerFunc`, add this as the first line inside the returned closure:

```go
cfg := auth.RequestCfg(c, cfg)
```

Example — `dashboard.go` becomes:

```go
func GetDashboard(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        cfg := auth.RequestCfg(c, cfg)  // ← add this line
        // rest of handler unchanged
        agentRows, err := tp.Query(&cfg.Timeplus, ...)
```

Apply this to every handler closure in each of the 13 files listed above. Each file gets the import added:

```go
import (
    // existing imports ...
    "github.com/timeplus/agentguard/internal/auth"
)
```

Note: `rules.go` and `catalog.go` handlers also call `tp.ExecHTTP`. Apply to all closures in those files that use `cfg.Timeplus`.

- [ ] **Step 2: Verify compilation**

```bash
cd backend
CGO_ENABLED=0 go build ./...
```

Expected: compiles cleanly with no unused import or undefined symbol errors.

- [ ] **Step 3: Run all tests**

```bash
cd backend
CGO_ENABLED=0 go test ./...
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/handlers/
git commit -m "feat(handlers): inject Timeplus credentials from session via auth.RequestCfg"
```

---

## Task 9: Frontend — Add credentials:include + Auth/Setup API Calls

The browser must send the `agentguard_session` cookie with every request. Currently the `fetch` calls in `client.js` omit `credentials: 'include'`, so cookies are never sent. This task fixes that and adds the new API methods.

**Files:**
- Modify: `frontend/src/api/client.js`

- [ ] **Step 1: Update `frontend/src/api/client.js`**

Replace the entire file:

```js
import { ndjsonStreamParser } from '@timeplus/proton-javascript-driver'

const BASE = '/api'

async function get(path) {
  const res = await fetch(`${BASE}${path}`, { credentials: 'include' })
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try { const j = await res.json(); if (j?.error) msg = j.error } catch { /* use status */ }
    throw new Error(msg)
  }
  return res.json()
}

async function post(path, body) {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    credentials: 'include',
  })
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try { const j = await res.json(); if (j?.error) msg = j.error } catch { /* use status */ }
    throw new Error(msg)
  }
  return res.json()
}

async function del(path) {
  const res = await fetch(`${BASE}${path}`, { method: 'DELETE', credentials: 'include' })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res.json()
}

export const api = {
  // ── Auth ────────────────────────────────────────────────────────────────────
  authStatus: () => get('/auth/status'),
  authLogin: (username, password) => post('/auth/login', { username, password }),
  authLogout: () => post('/auth/logout', {}),

  // ── Setup ───────────────────────────────────────────────────────────────────
  setupTestConnection: (host, port, username, password) =>
    post('/setup/test-connection', { host, port, username, password }),

  // setupProvision returns an EventSource-like async generator consuming the SSE stream.
  // Usage: for await (const event of api.setupProvision(body)) { ... }
  async *setupProvision(body) {
    const res = await fetch(`${BASE}/setup/provision`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      credentials: 'include',
    })
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const parts = buffer.split('\n\n')
      buffer = parts.pop() // keep incomplete chunk
      for (const part of parts) {
        const line = part.trim()
        if (line.startsWith('data: ')) {
          try { yield JSON.parse(line.slice(6)) } catch { /* skip malformed */ }
        }
      }
    }
  },

  // ── Existing endpoints ────────────────────────────────────────────────────
  getDashboard: () => get('/dashboard'),
  listAgents: (page = 1, limit = 15) => get(`/agents?page=${page}&limit=${limit}`),
  getAgent: (id, params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/agents/${id}${q ? '?' + q : ''}`)
  },
  listSessions: (agentId, params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/agents/${agentId}/sessions${q ? '?' + q : ''}`)
  },
  getSession: (agentId, sessionId) => get(`/agents/${agentId}/sessions/${sessionId}`),
  getRunTrace: (agentId, sessionId, runId) =>
    get(`/agents/${agentId}/sessions/${sessionId}/runs/${encodeURIComponent(runId)}/trace`),
  listEvents: (agentId, params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/agents/${agentId}/events${q ? '?' + q : ''}`)
  },
  getLLMMetrics: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/metrics/llm${q ? '?' + q : ''}`)
  },
  getToolMetrics: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/metrics/tools${q ? '?' + q : ''}`)
  },
  getThreats: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/threats${q ? '?' + q : ''}`)
  },
  acknowledgeThreat: (id, body) => post(`/threats/${id}/acknowledge`, body),
  clearThreat: (id, body) => post(`/threats/${id}/clear`, body),
  getThreat: (id) => get(`/threats/${id}`),
  getCost: (params = {}) => {
    const q = new URLSearchParams(params).toString()
    return get(`/cost${q ? '?' + q : ''}`)
  },
  listPricing: () => get('/cost/pricing'),
  upsertPricing: (entry) => post('/cost/pricing', entry),
  deletePricing: (provider, model) => del(`/cost/pricing/${encodeURIComponent(provider)}/${encodeURIComponent(model)}`),
  listRules: () => get('/rules'),
  getRulesCatalog: () => get('/rules/catalog'),
  importCatalog: (file, force = false) => {
    const fd = new FormData()
    fd.append('file', file)
    return fetch(`${BASE}/rules/catalog/import${force ? '?force=true' : ''}`, {
      method: 'POST',
      body: fd,
      credentials: 'include',
    }).then(res => { if (!res.ok) throw new Error(`HTTP ${res.status}`); return res.json() })
  },
  exportCatalogUrl: () => `${BASE}/rules/catalog/export`,
  getRule: (id) => get(`/rules/${id}`),
  toggleRule: (id) => post(`/rules/${id}/toggle`, {}),
  upgradeRule: (id, querySql) => post(`/rules/${id}/upgrade`, { query_sql: querySql }),
  createRule: (body) => post('/rules', body),
  installRule: (id) => post(`/rules/${id}/install`, {}),
  uninstallRule: (id) => del(`/rules/${id}`),
  listTables: () => get('/investigate/tables'),
  generateConfig: (data) => post('/onboarding/generate', data),

  async *protonQuery(sql, { signal } = {}) {
    const res = await fetch(`${BASE}/proton/query`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sql }),
      signal,
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({}))
      throw new Error(err.error || `HTTP ${res.status}`)
    }
    yield* ndjsonStreamParser(res.body.getReader())
  },
}
```

- [ ] **Step 2: Verify no lint errors**

```bash
cd frontend
npm run lint
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.js
git commit -m "feat(api): add credentials:include to all fetch calls; add auth + setup API methods"
```

---

## Task 10: Frontend — AuthContext

**Files:**
- Create: `frontend/src/context/AuthContext.jsx`

- [ ] **Step 1: Create `frontend/src/context/AuthContext.jsx`**

```jsx
import { createContext, useContext, useState, useEffect, useCallback } from 'react'
import { api } from '../api/client'

const AuthContext = createContext(null)

export function AuthProvider({ children }) {
  const [auth, setAuth] = useState({
    loading: true,
    setupComplete: false,
    loggedIn: false,
    username: '',
  })

  const refresh = useCallback(() => {
    api.authStatus()
      .then(data => setAuth({
        loading: false,
        setupComplete: data.setup_complete,
        loggedIn: data.logged_in,
        username: data.username || '',
      }))
      .catch(() => setAuth({ loading: false, setupComplete: false, loggedIn: false, username: '' }))
  }, [])

  useEffect(() => { refresh() }, [refresh])

  return (
    <AuthContext.Provider value={{ auth, setAuth, refresh }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/context/AuthContext.jsx
git commit -m "feat(auth): add AuthContext with setup_complete + logged_in state"
```

---

## Task 11: Frontend — Login Page

**Files:**
- Create: `frontend/src/pages/Login.jsx`

- [ ] **Step 1: Create `frontend/src/pages/Login.jsx`**

```jsx
import { useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { api } from '../api/client'

export default function Login() {
  const { setAuth } = useAuth()
  const [username, setUsername] = useState(() => localStorage.getItem('agentguard_username') || '')
  const [password, setPassword] = useState('')
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e) => {
    e.preventDefault()
    setError(null)
    setLoading(true)
    try {
      const data = await api.authLogin(username, password)
      localStorage.setItem('agentguard_username', data.username)
      setAuth(prev => ({ ...prev, loggedIn: true, username: data.username }))
    } catch (err) {
      setError(err.message === 'HTTP 401' ? 'Invalid credentials' : (err.message || 'Login failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0] flex flex-col">
      <nav className="bg-[#15121d] shadow-[0_0_12px_rgba(213,63,140,0.12)] sticky top-0 z-50 h-16 w-full flex items-center px-6 backdrop-blur-md">
        <span className="text-lg font-semibold bg-clip-text text-transparent bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] tracking-tight">AgentGuard</span>
      </nav>

      <main className="flex-1 flex items-center justify-center p-6">
        <div className="w-full max-w-sm space-y-8">
          <div className="text-center space-y-2">
            <span
              className="material-symbols-outlined text-4xl text-[#ffb0cf]"
              style={{ fontVariationSettings: "'FILL' 1" }}
            >security</span>
            <h1 className="text-2xl font-bold text-white">Sign in to AgentGuard</h1>
            <p className="text-zinc-500 text-xs">Use your Timeplus credentials</p>
          </div>

          <form onSubmit={handleSubmit} className="bg-[#2c2834] rounded-sm p-8 space-y-4">
            <div className="space-y-1">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Username</label>
              <input
                type="text"
                value={username}
                onChange={e => setUsername(e.target.value)}
                placeholder="default"
                required
                autoComplete="username"
                className="w-full bg-[#0f0c17] text-sm py-2.5 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
              />
            </div>

            <div className="space-y-1">
              <label className="text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">Password</label>
              <input
                type="password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                placeholder="Leave blank if not set"
                autoComplete="current-password"
                className="w-full bg-[#0f0c17] text-sm py-2.5 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
              />
            </div>

            {error && (
              <div className="flex items-center gap-2 text-xs text-[#ffb4ab]">
                <span className="material-symbols-outlined text-xs">error</span>
                {error}
              </div>
            )}

            <button
              type="submit"
              disabled={loading || !username}
              className="w-full bg-primary-button py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] transition-transform disabled:opacity-70 disabled:cursor-not-allowed"
            >
              {loading ? 'Signing in…' : 'Sign In'}
            </button>
          </form>
        </div>
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/Login.jsx
git commit -m "feat(frontend): add Login page"
```

---

## Task 12: Frontend — Setup Page

**Files:**
- Create: `frontend/src/pages/Setup.jsx`

- [ ] **Step 1: Create `frontend/src/pages/Setup.jsx`**

```jsx
import { useState, useRef } from 'react'
import { useAuth } from '../context/AuthContext'
import { api } from '../api/client'

const STEPS = [
  { label: 'Connection', icon: 'cable' },
  { label: 'Credentials', icon: 'key' },
  { label: 'Provision', icon: 'build' },
]

export default function Setup() {
  const { setAuth } = useAuth()
  const [step, setStep] = useState(0)

  // Step 0 — connection
  const [host, setHost] = useState('localhost')
  const [port, setPort] = useState(3218)
  const [nativePort, setNativePort] = useState(8463)
  const [agentGuardURL, setAgentGuardURL] = useState('http://localhost:8080')

  // Step 1 — credentials
  const [username, setUsername] = useState('default')
  const [password, setPassword] = useState('')
  const [testResult, setTestResult] = useState(null) // null | {ok, error}
  const [testing, setTesting] = useState(false)

  // Step 2 — provision
  const [log, setLog] = useState([])
  const [provisioning, setProvisioning] = useState(false)
  const [done, setDone] = useState(false)
  const [provisionError, setProvisionError] = useState(null)
  const logEndRef = useRef(null)

  const handleTestConnection = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      await api.setupTestConnection(host, port, username, password)
      setTestResult({ ok: true })
    } catch (err) {
      setTestResult({ ok: false, error: err.message })
    } finally {
      setTesting(false)
    }
  }

  const handleProvision = async () => {
    setProvisioning(true)
    setLog([])
    setProvisionError(null)
    try {
      for await (const event of api.setupProvision({ host, port, native_port: nativePort, username, password, agentguard_url: agentGuardURL })) {
        setLog(prev => [...prev, event])
        logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
        if (event.status === 'done') {
          // Provision succeeded — log the user in automatically.
          try {
            const data = await api.authLogin(username, password)
            localStorage.setItem('agentguard_username', data.username)
            setAuth(prev => ({ ...prev, setupComplete: true, loggedIn: true, username: data.username }))
          } catch {
            setProvisionError('Provisioning succeeded but login failed — please reload and log in.')
          }
          setDone(true)
        }
        if (event.status === 'error') {
          setProvisionError(event.msg)
        }
      }
    } catch (err) {
      setProvisionError(err.message)
    } finally {
      setProvisioning(false)
    }
  }

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0] flex flex-col">
      <nav className="bg-[#15121d] shadow-[0_0_12px_rgba(213,63,140,0.12)] sticky top-0 z-50 h-16 w-full flex items-center px-6 backdrop-blur-md">
        <span className="text-lg font-semibold bg-clip-text text-transparent bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] tracking-tight">AgentGuard</span>
        <span className="ml-3 text-[10px] font-semibold text-zinc-500 uppercase tracking-widest">First-time Setup</span>
      </nav>

      <main className="flex-1 flex items-start justify-center p-6 md:p-12">
        <div className="w-full max-w-2xl space-y-8">
          <div>
            <h1 className="text-2xl font-bold text-white tracking-tight">Setup AgentGuard</h1>
            <p className="text-zinc-500 text-xs mt-1 leading-relaxed">Connect to Timeplus and provision all monitoring resources.</p>
          </div>

          {/* Step indicator */}
          <div className="flex items-center gap-0">
            {STEPS.map((s, i) => (
              <div key={i} className="flex items-center">
                <div className={`flex items-center gap-2 px-4 py-2 rounded-sm text-xs font-semibold ${i === step ? 'bg-[#ffb0cf]/10 text-[#ffb0cf]' : i < step ? 'text-zinc-400' : 'text-zinc-600'}`}>
                  <span className={`w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-bold ${i < step ? 'bg-[#f256a3] text-[#570033]' : i === step ? 'bg-[#ffb0cf]/20 text-[#ffb0cf]' : 'bg-[#37333f] text-zinc-600'}`}>
                    {i < step ? '✓' : i + 1}
                  </span>
                  {s.label}
                </div>
                {i < STEPS.length - 1 && <div className="h-px w-8 bg-[#37333f]" />}
              </div>
            ))}
          </div>

          <div className="bg-[#2c2834] rounded-sm overflow-hidden">
            <div className="p-8 space-y-6">

              {/* Step 0: Connection */}
              {step === 0 && (
                <>
                  <div>
                    <h2 className="text-base font-semibold text-white">Timeplus Connection</h2>
                    <p className="text-zinc-400 text-xs mt-1">Enter the address of your Timeplus instance.</p>
                  </div>
                  <div className="space-y-3">
                    {[
                      { label: 'Timeplus Host', val: host, set: setHost, ph: 'localhost', type: 'text' },
                      { label: 'HTTP Port', val: port, set: v => setPort(Number(v)), ph: '3218', type: 'number' },
                      { label: 'Native Port', val: nativePort, set: v => setNativePort(Number(v)), ph: '8463', type: 'number' },
                      { label: 'AgentGuard URL', val: agentGuardURL, set: setAgentGuardURL, ph: 'http://localhost:8080', type: 'text' },
                    ].map(({ label, val, set, ph, type }) => (
                      <div key={label} className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">{label}</label>
                        <input
                          type={type}
                          value={val}
                          onChange={e => set(e.target.value)}
                          placeholder={ph}
                          className="flex-1 bg-[#0f0c17] text-xs py-2 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
                        />
                      </div>
                    ))}
                  </div>
                </>
              )}

              {/* Step 1: Credentials */}
              {step === 1 && (
                <>
                  <div>
                    <h2 className="text-base font-semibold text-white">Timeplus Credentials</h2>
                    <p className="text-zinc-400 text-xs mt-1">Enter your Timeplus username and password. These will not be stored.</p>
                  </div>
                  <div className="space-y-3">
                    {[
                      { label: 'Username', val: username, set: setUsername, ph: 'default', type: 'text' },
                      { label: 'Password', val: password, set: setPassword, ph: 'Leave blank if not set', type: 'password' },
                    ].map(({ label, val, set, ph, type }) => (
                      <div key={label} className="flex items-center gap-4">
                        <label className="text-[10px] font-medium text-zinc-500 uppercase tracking-widest w-36 shrink-0">{label}</label>
                        <input
                          type={type}
                          value={val}
                          onChange={e => { set(e.target.value); setTestResult(null) }}
                          placeholder={ph}
                          className="flex-1 bg-[#0f0c17] text-xs py-2 px-3 rounded-sm focus:ring-1 focus:ring-[#ffb0cf] focus:outline-none text-[#e7dff0] font-mono"
                        />
                      </div>
                    ))}
                  </div>
                  <div className="flex items-center gap-4">
                    <button
                      onClick={handleTestConnection}
                      disabled={testing || !username}
                      className="px-4 py-2 text-xs font-semibold rounded-sm bg-[#37333f] text-zinc-300 hover:text-white hover:bg-[#4b4754] transition-colors disabled:opacity-50"
                    >
                      {testing ? 'Testing…' : 'Test Connection'}
                    </button>
                    {testResult?.ok && (
                      <span className="flex items-center gap-1.5 text-xs text-emerald-400">
                        <span className="material-symbols-outlined text-xs">check_circle</span>
                        Connected as "{username}"
                      </span>
                    )}
                    {testResult && !testResult.ok && (
                      <span className="flex items-center gap-1.5 text-xs text-[#ffb4ab]">
                        <span className="material-symbols-outlined text-xs">error</span>
                        {testResult.error}
                      </span>
                    )}
                  </div>
                </>
              )}

              {/* Step 2: Provision */}
              {step === 2 && (
                <>
                  <div>
                    <h2 className="text-base font-semibold text-white">Provision Resources</h2>
                    <p className="text-zinc-400 text-xs mt-1">Create all AgentGuard streams, materialized views, and rules in Timeplus.</p>
                  </div>

                  {!provisioning && log.length === 0 && !done && (
                    <button
                      onClick={handleProvision}
                      className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] transition-transform"
                    >
                      <span className="flex items-center gap-2">
                        <span className="material-symbols-outlined text-xs">build</span>
                        Provision Resources
                      </span>
                    </button>
                  )}

                  {(log.length > 0 || provisioning) && (
                    <div className="bg-[#0f0c17] rounded-sm border border-[#574149]/10 p-4 font-mono text-[11px] leading-relaxed max-h-72 overflow-y-auto custom-scrollbar">
                      {log.map((entry, i) => (
                        <div key={i} className={
                          entry.status === 'error' ? 'text-[#ffb4ab]' :
                          entry.status === 'done'  ? 'text-emerald-400 font-semibold' :
                          entry.status === 'running' ? 'text-zinc-400' :
                          'text-zinc-300'
                        }>
                          {entry.msg}
                        </div>
                      ))}
                      {provisioning && <div className="text-[#ffb0cf] animate-pulse">▌</div>}
                      <div ref={logEndRef} />
                    </div>
                  )}

                  {provisionError && (
                    <div className="flex items-start gap-2 text-xs text-[#ffb4ab] bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded-sm p-3">
                      <span className="material-symbols-outlined text-xs mt-0.5 shrink-0">error</span>
                      {provisionError}
                    </div>
                  )}
                </>
              )}
            </div>

            {/* Footer */}
            <div className="bg-[#37333f]/30 px-8 py-5 flex justify-between items-center">
              <button
                onClick={() => setStep(s => Math.max(0, s - 1))}
                disabled={step === 0 || provisioning || done}
                className="px-4 py-2 text-xs font-medium text-zinc-400 hover:text-white transition-all flex items-center gap-2 disabled:opacity-40 disabled:cursor-not-allowed"
              >
                <span className="material-symbols-outlined text-xs">arrow_back</span>
                Back
              </button>

              {step < 2 ? (
                <button
                  onClick={() => setStep(s => s + 1)}
                  disabled={step === 1 && !testResult?.ok}
                  className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text hover:scale-[1.02] active:scale-95 transition-all flex items-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:scale-100"
                >
                  Next
                  <span className="material-symbols-outlined text-xs">arrow_forward</span>
                </button>
              ) : (
                <button
                  disabled={!done}
                  className="bg-primary-button px-6 py-2.5 rounded-sm text-xs font-semibold text-primary-button-text flex items-center gap-2 opacity-50 cursor-not-allowed"
                >
                  <span className="material-symbols-outlined text-xs">check_circle</span>
                  {done ? 'Redirecting…' : 'Waiting for provisioning…'}
                </button>
              )}
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Commit**

```bash
git add frontend/src/pages/Setup.jsx
git commit -m "feat(frontend): add 3-step Setup wizard with live SSE provision log"
```

---

## Task 13: Frontend — App.jsx AuthGuard + TopNav Logout

**Files:**
- Modify: `frontend/src/App.jsx`
- Modify: `frontend/src/components/TopNav.jsx`

- [ ] **Step 1: Read the existing TopNav**

```bash
cat frontend/src/components/TopNav.jsx
```

Note the current structure — we will add the username display and logout button to the right side of the existing top bar without changing its layout.

- [ ] **Step 2: Update `frontend/src/App.jsx`**

Replace the entire file:

```jsx
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { AuthProvider, useAuth } from './context/AuthContext'
import { NotificationsProvider } from './components/NotificationsProvider'
import Login from './pages/Login'
import Setup from './pages/Setup'
import Dashboard from './pages/Dashboard'
import AgentList from './pages/AgentList'
import AgentDetail from './pages/AgentDetail'
import Threats from './pages/Threats'
import ThreatDetail from './pages/ThreatDetail'
import CostGovernance from './pages/CostGovernance'
import RulePacks from './pages/RulePacks'
import RuleDetail from './pages/RuleDetail'
import RuleCreate from './pages/RuleCreate'
import Investigate from './pages/Investigate'
import Onboarding from './pages/Onboarding'

function AuthGuard({ children }) {
  const { auth } = useAuth()
  if (auth.loading) {
    return (
      <div className="min-h-screen bg-[#15121d] flex items-center justify-center">
        <span className="text-zinc-500 text-sm">Loading…</span>
      </div>
    )
  }
  if (!auth.setupComplete) return <Setup />
  if (!auth.loggedIn) return <Login />
  return children
}

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <NotificationsProvider>
          <AuthGuard>
            <Routes>
              <Route path="/" element={<Navigate to="/dashboard" replace />} />
              <Route path="/dashboard" element={<Dashboard />} />
              <Route path="/agents" element={<AgentList />} />
              <Route path="/agents/:id" element={<AgentDetail />} />
              <Route path="/threats" element={<Threats />} />
              <Route path="/threats/:id" element={<ThreatDetail />} />
              <Route path="/cost" element={<CostGovernance />} />
              <Route path="/rules" element={<RulePacks />} />
              <Route path="/rules/new" element={<RuleCreate />} />
              <Route path="/rules/:id" element={<RuleDetail />} />
              <Route path="/investigate" element={<Investigate />} />
              <Route path="/onboarding" element={<Onboarding />} />
            </Routes>
          </AuthGuard>
        </NotificationsProvider>
      </AuthProvider>
    </BrowserRouter>
  )
}
```

- [ ] **Step 3: Add username + logout to TopNav**

Read `frontend/src/components/TopNav.jsx` first, then add the following to the right side of the top bar (inside the existing nav element, before the closing tag):

```jsx
// Add these imports at the top of TopNav.jsx:
import { useAuth } from '../context/AuthContext'
import { api } from '../api/client'

// Inside the TopNav component, add:
const { auth, setAuth } = useAuth()

const handleLogout = async () => {
  await api.authLogout().catch(() => {})
  setAuth(prev => ({ ...prev, loggedIn: false, username: '' }))
}

// Add this JSX inside the nav element, on the right side:
{auth.loggedIn && (
  <div className="flex items-center gap-3">
    <span className="flex items-center gap-1.5 text-xs text-zinc-400">
      <span className="w-1.5 h-1.5 rounded-full bg-emerald-400"></span>
      {auth.username}
    </span>
    <button
      onClick={handleLogout}
      className="text-[10px] font-semibold text-zinc-500 hover:text-[#ffb0cf] uppercase tracking-widest transition-colors"
    >
      Logout
    </button>
  </div>
)}
```

Place the username/logout block to the right of any existing elements in the nav (e.g., after the notification bell if one exists).

- [ ] **Step 4: Verify the frontend builds**

```bash
cd frontend
npm run build
```

Expected: build succeeds with no errors.

- [ ] **Step 5: Start the dev server and test the full flow**

```bash
cd frontend
npm run dev
```

Open `http://localhost:5173`. Verify:
1. With no `config.yaml` (or `setup_complete: false`): the Setup wizard appears
2. Step 1 accepts host/port
3. Step 2: Test Connection works against a running Timeplus instance
4. Step 3: Provision streams log lines live, "done" auto-logs-in and redirects
5. After logout: Login page appears with username pre-filled from localStorage
6. After login: Dashboard loads; TopNav shows username and Logout button
7. Logout: returns to Login page

- [ ] **Step 6: Commit**

```bash
git add frontend/src/App.jsx frontend/src/components/TopNav.jsx
git commit -m "feat(frontend): add AuthGuard, wire Login/Setup into App, add logout to TopNav"
```

---

## Self-Review

**Spec coverage check:**
- ✅ Login/logout with Timeplus credentials → Tasks 4, 11
- ✅ First-time onboarding provisions resources → Tasks 5, 6, 12
- ✅ Onboarding guides to agent connect → Setup redirects to /onboarding on done (Task 12)
- ✅ Onboarding runs only once (setup_complete flag) → Tasks 1, 6, 13
- ✅ No credentials stored in config → Task 1
- ✅ All queries use logged-in user → Tasks 3, 8
- ✅ Resources created only during setup, not startup → Tasks 5, 7

**Type consistency check:**
- `config.TimeplusConfig.User` / `.Password` — kept as fields (Task 1), populated by `auth.RequestCfg` (Task 3) and `handlers.Provision` (Task 6)
- `store.Create(user, pass)` → `*Session` — defined in Task 2, used in Tasks 4, 6
- `auth.AuthStatus`, `auth.Login`, `auth.Logout` — defined in Task 4, registered in Task 7
- `tp.Init(timeplusCfg, agentGuardURL, entries, emit)` — defined in Task 5, called in Task 6
- `config.Save(cfg, path)` — defined in Task 1, called in Task 6
- `api.authLogin`, `api.setupProvision` — defined in Task 9, used in Tasks 11, 12
- `useAuth()` returns `{auth, setAuth, refresh}` — defined in Task 10, used in Tasks 11, 12, 13
