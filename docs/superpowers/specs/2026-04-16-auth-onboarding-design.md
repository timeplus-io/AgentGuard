# Auth + Onboarding Redesign — Design Spec

> **For agentic workers:** This spec is ready for implementation planning. Invoke `superpowers:writing-plans` to produce the implementation plan.

## Goal

Add login/logout using Timeplus credentials, a first-time setup wizard that provisions all AgentGuard resources, and persistent non-credential config — removing credentials from the config file entirely and moving resource creation from server startup to the setup wizard.

---

## Background

Currently AgentGuard stores `TIMEPLUS_USER` and `TIMEPLUS_PASSWORD` in `config.yaml` / environment variables and calls `timeplus.Init()` on every server startup to create all required Timeplus streams, materialized views, inputs, UDFs, and alerts. There is no authentication layer — anyone who can reach the server has full access.

The six requirements being addressed:

1. Login/logout using Timeplus username/password; all queries run as the logged-in user
2. First-time onboarding collects Timeplus credentials and provisions all AgentGuard resources
3. Onboarding then guides the user to connect an agent
4. Onboarding runs only once; non-credential config (Timeplus host/port, AgentGuard URL) is persisted
5. Credentials are never stored in config; all queries use the logged-in user's session
6. AgentGuard resources are created only during setup, not on every server startup

---

## Architecture

### Boot Sequence

```
App loads
    │
    ▼
GET /api/auth/status
    { setup_complete: bool, logged_in: bool, username: string }
    │
    ├─ setup_complete = false  ──►  /setup   (first-time wizard)
    │
    ├─ logged_in = false       ──►  /login
    │
    └─ logged_in = true        ──►  normal routing
```

A top-level `<AuthGuard>` component in `App.jsx` runs this check on mount and handles all redirects. All existing page components are unchanged.

### Session Approach

Server-side session store (in-memory Go map). On login, backend validates credentials against Timeplus (runs `SELECT 1`), creates a session entry `{ID, User, Password, Created}`, and sets an `httpOnly` `agentguard_session` cookie. All protected API routes require this cookie. Sessions are lost on server restart — users re-login (acceptable for an ops tool).

Username is saved to `localStorage` to pre-fill the login form. Password is never written to disk or browser storage — it lives only in the server's in-memory session map.

---

## Section 1: Auth Subsystem

### New package: `internal/auth/`

```
internal/auth/
  store.go      — SessionStore: sync.RWMutex-protected map[string]*Session
  middleware.go — AuthRequired gin.HandlerFunc
  handlers.go   — Login, Logout, AuthStatus handlers
```

**Session struct:**
```go
type Session struct {
    ID       string
    User     string
    Password string
    Created  time.Time
}
```

### New API Endpoints (unauthenticated)

| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/api/auth/status` | Returns `{setup_complete, logged_in, username}` |
| `POST` | `/api/auth/login`  | Validates creds, creates session, sets cookie |
| `POST` | `/api/auth/logout` | Destroys session, clears cookie |

**Login flow:**
1. Frontend POSTs `{username, password}` to `/api/auth/login`
2. Backend opens a Timeplus connection with those credentials, runs `SELECT 1`
3. Success → creates session, sets `agentguard_session` cookie (httpOnly, SameSite=Strict), returns `{username}`
4. Failure → returns `401 {error: "invalid credentials"}`

### Auth Middleware

```go
func AuthRequired(store *SessionStore) gin.HandlerFunc {
    return func(c *gin.Context) {
        cookie, err := c.Cookie("agentguard_session")
        if err != nil {
            c.AbortWithStatusJSON(401, gin.H{"error": "not authenticated"})
            return
        }
        session, ok := store.Get(cookie)
        if !ok {
            c.AbortWithStatusJSON(401, gin.H{"error": "session expired"})
            return
        }
        c.Set("timeplus_user", session.User)
        c.Set("timeplus_password", session.Password)
        c.Next()
    }
}
```

### Handler Credential Injection

A single helper assembles a per-request config from file-based host/port and session credentials:

```go
// auth.RequestCfg — used at the top of every existing handler
func RequestCfg(c *gin.Context, cfg *config.Config) *config.Config {
    user, _ := c.Get("timeplus_user")
    pass, _ := c.Get("timeplus_password")
    copy := *cfg
    copy.Timeplus.User = user.(string)
    copy.Timeplus.Password = pass.(string)
    return &copy
}
```

Every existing handler adds one line:
```go
func GetDashboard(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        cfg := auth.RequestCfg(c, cfg)  // ← only change per handler
        // rest unchanged
    }
}
```

### Frontend: Login Page (`/login`)

```
┌─────────────────────────────────────────────────────────────┐
│  ◈ AgentGuard                                               │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│                ◈  Sign in to AgentGuard                     │
│              Use your Timeplus credentials                  │
│                                                             │
│         Username  [default                        ]         │
│         Password  [••••••••                       ]         │
│                                                             │
│                    [ Sign In ]                              │
│                                                             │
│  ✗ Invalid credentials              (error state)           │
└─────────────────────────────────────────────────────────────┘
```

Username pre-filled from `localStorage`. On success, redirect to `/dashboard` (or last attempted path).

### Frontend: TopNav with User Context

```
┌──────────────────────────────────────────────────────────────┐
│ ◈ AgentGuard    Dashboard  Agents  Threats  …   ● default ▾ │
└──────────────────────────────────────────────────────────────┘
                                                   └─ dropdown:
                                                        Logout
```

`TopNav` reads username from a new `AuthContext` (provided by `AuthGuard`).

---

## Section 2: Setup Subsystem

### New page: `/setup` (3 steps)

Only shown when `setup_complete = false`. Not accessible after setup completes.

**Step 1 — Connection:**
```
┌─────────────────────────────────────────────────────────────┐
│  ◈ AgentGuard Setup                                         │
│  First-time configuration                                   │
├─────────────────────────────────────────────────────────────┤
│  ① Connection ──── ② Credentials ──── ③ Provision          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Timeplus Host      [ localhost                ]           │
│  Timeplus Port      [ 3218                     ]           │
│  Native Port        [ 8463                     ]           │
│  AgentGuard URL     [ http://localhost:8080     ]           │
│                                                             │
│                                          [ Next → ]         │
└─────────────────────────────────────────────────────────────┘
```

**Step 2 — Credentials:**
```
┌─────────────────────────────────────────────────────────────┐
│  ① Connection ──── ② Credentials ──── ③ Provision          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Username           [ default                  ]           │
│  Password           [ ••••••••                 ]           │
│                                                             │
│  [ Test Connection ]                                        │
│  ✓ Connected to Timeplus as "default"  (success)            │
│  ✗ Connection failed: timeout          (error)              │
│                                                             │
│  [ ← Back ]                              [ Next → ]         │
└─────────────────────────────────────────────────────────────┘
```

`Next` is disabled until Test Connection succeeds.

**Step 3 — Provision:**
```
┌─────────────────────────────────────────────────────────────┐
│  ① Connection ──── ② Credentials ──── ③ Provision          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  [ Provision Resources ]  (click to start)                  │
│                                                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │ $ Connecting to Timeplus at localhost:8463 …        │   │
│  │   ✓ Connected                                       │   │
│  │ $ Creating OTel streams …                           │   │
│  │   ✓ otel_traces                                     │   │
│  │   ✓ otel_logs                                       │   │
│  │   ✓ otel_metrics_gauge  (+ 4 more)                  │   │
│  │ $ Creating AgentGuard streams …                     │   │
│  │   ✓ agentguard_hook_events                          │   │
│  │   ✓ agentguard_cim_event                            │   │
│  │ $ Creating materialized views …                     │   │
│  │   ✓ mv_cim_claudecode                               │   │
│  │   ● mv_cim_openclaw …              ← live cursor    │   │
│  └─────────────────────────────────────────────────────┘   │
│                                                             │
│  [ ← Back ]       [ Continue → ]  (enabled only when done)  │
└─────────────────────────────────────────────────────────────┘
```

On `Continue`: frontend navigates to `/onboarding` (the existing agent-connect wizard).

### New API Endpoints (unauthenticated — setup runs before login)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/setup/test-connection` | Opens Timeplus with submitted creds, runs `SELECT 1`, returns ok/error |
| `POST` | `/api/setup/provision`      | SSE stream — runs Init() step by step, emitting log lines; on completion writes config + creates session |

**Provision SSE event format:**
```
data: {"status":"running","msg":"$ Creating OTel streams …"}
data: {"status":"ok","msg":"  ✓ otel_traces"}
data: {"status":"ok","msg":"  ✓ otel_logs"}
data: {"status":"error","msg":"  ✗ failed to create mv_cim_openclaw: …"}
data: {"status":"done","msg":"Setup complete"}
```

On `done`: backend writes `setup_complete: true` + host/port to `config.yaml`, creates a session (user is now logged in), sets `agentguard_session` cookie.

---

## Section 3: Config Changes

### Remove from `TimeplusConfig`

```go
// Before
type TimeplusConfig struct {
    Host       string
    Port       int
    NativePort int
    User       string    // ← remove
    Password   string    // ← remove
}

// After
type TimeplusConfig struct {
    Host       string
    Port       int
    NativePort int
}
```

Remove corresponding: `config.yaml` keys, env vars (`TIMEPLUS_USER`, `TIMEPLUS_PASSWORD`), CLI flags (`--timeplus-user`, `--timeplus-password`), and `viper` bindings.

### Add to `Config`

```go
type Config struct {
    Server        ServerConfig
    Timeplus      TimeplusConfig
    CatalogDir    string
    AgentGuardURL string
    SetupComplete bool    // ← new: written by provision handler
}
```

### New: `config.Save(cfg *Config) error`

Writes current config back to `config.yaml` (host, port, native port, agentguard URL, setup_complete). Called by the provision handler after successful provisioning.

---

## Section 4: Startup Changes

### `main.go`

- Remove `timeplus.Init(cfg, mgr.All())` call — resources are created during setup, not startup
- Remove `startupSyncMVs()` from startup — it also requires Timeplus credentials, which are no longer available at boot. MV pause/resume state is persisted natively by Timeplus; manual reconciliation is available on-demand via the existing `POST /api/rules/sync` endpoint
- Remove credential CLI flags and viper bindings
- Add auth routes (unauthenticated) and wrap all existing routes in `AuthRequired` middleware

**Route registration:**
```go
// Unauthenticated
api.GET("/auth/status",            handlers.AuthStatus(cfg, store))
api.POST("/auth/login",            handlers.Login(cfg, store))
api.POST("/auth/logout",           handlers.Logout(store))
api.POST("/setup/test-connection", handlers.TestConnection())
api.POST("/setup/provision",       handlers.Provision(cfg, mgr, store))

// All existing routes — protected
protected := api.Group("/", auth.AuthRequired(store))
protected.GET("/dashboard",        handlers.GetDashboard(cfg))
protected.GET("/agents",           handlers.ListAgents(cfg))
// … all other existing routes unchanged in signature
```

### `timeplus/init.go`

`Init()` function signature gains a `TimeplusConfig` parameter (with credentials passed in from the provision handler's request body):

```go
func Init(cfg *config.Config, timeplusCreds config.TimeplusConfig, catalogEntries []catalog.Entry) error
```

The provision handler constructs `timeplusCreds` from the setup request body and passes it through. The rest of `Init()` is unchanged.

---

## Section 5: Frontend Changes

### New files

| File | Purpose |
|------|---------|
| `src/pages/Login.jsx` | Login form, POSTs to `/api/auth/login` |
| `src/pages/Setup.jsx` | 3-step setup wizard, SSE provision log |
| `src/context/AuthContext.jsx` | Provides `{loggedIn, username, setup_complete}` to app |

### Modified files

| File | Change |
|------|--------|
| `src/App.jsx` | Add `<AuthGuard>` wrapper; add `/login` and `/setup` routes |
| `src/components/TopNav.jsx` | Add username display + logout dropdown |
| `src/api/client.js` | Add `auth.login()`, `auth.logout()`, `auth.status()`, `setup.testConnection()` |

### `AuthGuard` component

Fetches `/api/auth/status` on mount. Renders:
- `<Setup />` if `!setup_complete`
- `<Login />` if `setup_complete && !logged_in`
- `{children}` if `logged_in`

---

## What Does NOT Change

- All existing page components (`Dashboard`, `AgentList`, `AgentDetail`, `Threats`, `CostGovernance`, `RulePacks`, `RuleDetail`, `Investigate`)
- The existing `/onboarding` page (agent-connect wizard) — unchanged, re-runnable
- All Timeplus query logic, CIM pipeline, security rules, handler business logic
- The `timeplus/init.go` DDL constants and resource creation logic — only the caller changes

---

## File Summary

| File | Action |
|------|--------|
| `backend/internal/auth/store.go` | Create |
| `backend/internal/auth/middleware.go` | Create |
| `backend/internal/auth/handlers.go` | Create |
| `backend/internal/config/config.go` | Modify — remove User/Password, add SetupComplete, add Save() |
| `backend/internal/timeplus/init.go` | Modify — Init() accepts explicit credentials param |
| `backend/internal/handlers/*.go` | Modify — add `cfg := auth.RequestCfg(c, cfg)` to each handler |
| `backend/internal/handlers/setup.go` | Create — TestConnection, Provision (SSE) |
| `backend/cmd/server/main.go` | Modify — remove Init() call, add auth/setup routes, add AuthRequired |
| `frontend/src/pages/Login.jsx` | Create |
| `frontend/src/pages/Setup.jsx` | Create |
| `frontend/src/context/AuthContext.jsx` | Create |
| `frontend/src/App.jsx` | Modify — AuthGuard, new routes |
| `frontend/src/components/TopNav.jsx` | Modify — username + logout |
| `frontend/src/api/client.js` | Modify — auth and setup API calls |
