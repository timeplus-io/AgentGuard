# Custom Rule Creation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow users to write, test, save, uninstall, and reinstall custom SQL-based detection rules from the AgentShield UI.

**Architecture:** Custom rules reuse the existing `agentshield_rules` stream and MV infrastructure, distinguished by a `custom-<unix-ms>` ID prefix. The backend substitutes `'__rule_id__'` and `'__rule_name__'` placeholders in the user's SQL before storing and creating the MV. The catalog is extended to surface deleted custom rules for reinstall.

**Tech Stack:** Go/Gin backend, React/Vite frontend, Tailwind CSS, react-simple-code-editor + prismjs (already installed), Proton streaming SQL.

---

## File Map

| File | Change |
|------|--------|
| `backend/internal/handlers/rules.go` | Add `CreateRule`; modify `ListCatalog`, `InstallRule` |
| `backend/cmd/server/main.go` | Register `POST /api/rules` |
| `frontend/src/api/client.js` | Add `createRule` |
| `frontend/src/App.jsx` | Add `/rules/new` route |
| `frontend/src/pages/RuleCreate.jsx` | New page |
| `frontend/src/pages/RulePacks.jsx` | Wire button; update catalog display |

---

## Task 1: Backend — CreateRule handler

**Files:**
- Modify: `backend/internal/handlers/rules.go`

### Context

`agentshield_rules` stream schema (from `init.go`):
```
rule_id, name, version, description, query_sql, enabled, deleted,
severity, category, required_telemetry, fp_rate, hook_sources, updated_at
```

The `query_sql` stored in the stream is passed verbatim as the body of `CREATE MATERIALIZED VIEW mv_rule_<id> INTO agentshield_security_events AS <query_sql>`. So it must SELECT all columns that `agentshield_security_events` expects: `detected_at, rule_id, rule_name, severity, agent_id, deployment_id, session_id, run_id, hook_name, tool_name, message, event_data`.

**Placeholder substitution strategy:** The frontend template uses `'__rule_id__'` and `'__rule_name__'` as literal SQL strings. On save, the backend generates the real `rule_id` and `name`, then does string replacement before storing and creating the MV. This means the stored `query_sql` always has the real values — reinstall works by re-using the stored SQL as-is.

- [ ] **Step 1: Add imports to `backend/internal/handlers/rules.go`**

Add `"strconv"` and `"time"` to the import block. The file currently imports:
```go
import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "strings"

    "github.com/gin-gonic/gin"
    "github.com/timeplus/agentshield/internal/config"
    "github.com/timeplus/agentshield/internal/models"
    tp "github.com/timeplus/agentshield/internal/timeplus"
)
```

Change to:
```go
import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "strconv"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/timeplus/agentshield/internal/config"
    "github.com/timeplus/agentshield/internal/models"
    tp "github.com/timeplus/agentshield/internal/timeplus"
)
```

- [ ] **Step 2: Add `CreateRule` handler at the end of `backend/internal/handlers/rules.go` (before the helpers section)**

```go
// CreateRule creates a new custom rule from a user-provided detection SQL query.
// The SQL body may contain the placeholder literals '__rule_id__' and '__rule_name__'
// which the handler substitutes with the generated rule ID and the rule name before
// storing and creating the MV.
func CreateRule(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        var body struct {
            Name        string `json:"name"`
            Description string `json:"description"`
            QuerySQL    string `json:"query_sql"`
            Severity    string `json:"severity"`
            Category    string `json:"category"`
            FPRate      string `json:"fp_rate"`
            HookSources string `json:"hook_sources"`
        }
        if err := c.ShouldBindJSON(&body); err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
            return
        }
        if body.Name == "" || body.Description == "" || body.QuerySQL == "" {
            c.JSON(http.StatusBadRequest, gin.H{"error": "name, description, and query_sql are required"})
            return
        }
        if body.Severity == "" {
            body.Severity = "warning"
        }

        ruleID := "custom-" + strconv.FormatInt(time.Now().UnixMilli(), 10)

        // Substitute placeholders so the stored SQL is self-contained.
        escapedName := strings.ReplaceAll(body.Name, "'", "''")
        finalSQL := strings.ReplaceAll(body.QuerySQL, "'__rule_id__'", "'"+ruleID+"'")
        finalSQL = strings.ReplaceAll(finalSQL, "'__rule_name__'", "'"+escapedName+"'")

        escapedSQL := strings.ReplaceAll(finalSQL, "'", "''")
        insertSQL := fmt.Sprintf(
            `INSERT INTO agentshield_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at) VALUES ('%s','%s','1.0.0','%s','%s',true,false,'%s','%s','','%s','%s',now64(3))`,
            ruleID,
            escapedName,
            strings.ReplaceAll(body.Description, "'", "''"),
            escapedSQL,
            strings.ReplaceAll(body.Severity, "'", "''"),
            strings.ReplaceAll(body.Category, "'", "''"),
            strings.ReplaceAll(body.FPRate, "'", "''"),
            strings.ReplaceAll(body.HookSources, "'", "''"),
        )
        if err := tp.ExecHTTP(&cfg.Timeplus, insertSQL); err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "insert failed: " + err.Error()})
            return
        }

        if err := tp.CreateRuleMV(&cfg.Timeplus, ruleID, finalSQL); err != nil {
            log.Printf("rules: create custom MV %s: %v", ruleID, err)
        }

        c.JSON(http.StatusCreated, models.RulePack{
            ID:          ruleID,
            Name:        body.Name,
            Version:     "1.0.0",
            Description: body.Description,
            QuerySQL:    finalSQL,
            Enabled:     true,
            Severity:    body.Severity,
            Category:    body.Category,
            FPRate:      body.FPRate,
            HookSources: body.HookSources,
        })
    }
}
```

- [ ] **Step 3: Verify compilation**

```bash
cd backend && CGO_ENABLED=0 go build ./...
```

Expected: no output (clean build).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/handlers/rules.go
git commit -m "feat(rules): add CreateRule handler for custom rules"
```

---

## Task 2: Backend — Extend ListCatalog to include deleted custom rules

**Files:**
- Modify: `backend/internal/handlers/rules.go` — `ListCatalog` function (currently at line ~306)

### Context

`ListCatalog` currently returns only builtin rules that are not installed. We extend it to also return custom rules where `deleted=true` in the stream. The `CatalogItem` struct (defined locally inside `ListCatalog`) gains an `IsCustom bool` field.

- [ ] **Step 1: Add the deleted-custom-rules query constant near the top of the file, alongside `sqlCurrentRules`**

```go
const sqlDeletedCustomRules = `
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources
FROM table(agentshield_rules)
WHERE rule_id LIKE 'custom-%'
GROUP BY rule_id
HAVING arg_max(deleted, updated_at) = true`
```

- [ ] **Step 2: Replace the `ListCatalog` function body**

Find the existing `ListCatalog` function (starts with `func ListCatalog(cfg *config.Config) gin.HandlerFunc {`) and replace it entirely with:

```go
func ListCatalog(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        installed, err := queryCurrentRulePacks(&cfg.Timeplus)
        if err != nil {
            log.Printf("rules: catalog query failed: %v", err)
        }
        installedIDs := map[string]bool{}
        for _, p := range installed {
            installedIDs[p.ID] = true
        }

        type CatalogItem struct {
            ID          string `json:"id"`
            Name        string `json:"name"`
            Version     string `json:"version"`
            Description string `json:"description"`
            Severity    string `json:"severity"`
            Category    string `json:"category"`
            FPRate      string `json:"fp_rate"`
            HookSources string `json:"hook_sources"`
            Installed   bool   `json:"installed"`
            IsCustom    bool   `json:"is_custom"`
        }

        catalog := tp.Catalog()
        items := make([]CatalogItem, 0, len(catalog))
        for _, e := range catalog {
            items = append(items, CatalogItem{
                ID:          e.ID,
                Name:        e.Name,
                Version:     e.Version,
                Description: e.Description,
                Severity:    e.Severity,
                Category:    e.Category,
                FPRate:      e.FPRate,
                HookSources: e.HookSources,
                Installed:   installedIDs[e.ID],
                IsCustom:    false,
            })
        }

        // Append deleted custom rules so they can be reinstalled.
        customRows, err := tp.Query(&cfg.Timeplus, sqlDeletedCustomRules)
        if err != nil {
            log.Printf("rules: deleted custom rules query failed: %v", err)
        }
        for _, row := range customRows {
            id := strVal(row, "rule_id")
            if installedIDs[id] {
                continue // currently installed, skip
            }
            items = append(items, CatalogItem{
                ID:          id,
                Name:        strVal(row, "name"),
                Version:     strVal(row, "version"),
                Description: strVal(row, "description"),
                Severity:    strVal(row, "severity"),
                Category:    strVal(row, "category"),
                FPRate:      strVal(row, "fp_rate"),
                HookSources: strVal(row, "hook_sources"),
                Installed:   false,
                IsCustom:    true,
            })
        }

        c.JSON(http.StatusOK, gin.H{"catalog": items})
    }
}
```

- [ ] **Step 3: Verify compilation**

```bash
cd backend && CGO_ENABLED=0 go build ./...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/handlers/rules.go
git commit -m "feat(rules): extend catalog to include deleted custom rules"
```

---

## Task 3: Backend — Extend InstallRule to handle custom rule reinstall

**Files:**
- Modify: `backend/internal/handlers/rules.go` — `InstallRule` function (currently at line ~349)

### Context

`InstallRule` currently only handles builtin (`rp-`) rules. Custom rules (`custom-`) need to re-read the stored SQL from the stream (already substituted), re-insert an active row, and recreate the MV.

- [ ] **Step 1: Replace the `InstallRule` function entirely**

```go
func InstallRule(cfg *config.Config) gin.HandlerFunc {
    return func(c *gin.Context) {
        id := c.Param("id")

        // Custom rules: reinstall by reading last state from the stream.
        if strings.HasPrefix(id, "custom-") {
            rows, err := tp.Query(&cfg.Timeplus, fmt.Sprintf(`
                SELECT
                  arg_max(name,               updated_at) AS name,
                  arg_max(version,            updated_at) AS version,
                  arg_max(description,        updated_at) AS description,
                  arg_max(query_sql,          updated_at) AS query_sql,
                  arg_max(severity,           updated_at) AS severity,
                  arg_max(category,           updated_at) AS category,
                  arg_max(required_telemetry, updated_at) AS required_telemetry,
                  arg_max(fp_rate,            updated_at) AS fp_rate,
                  arg_max(hook_sources,       updated_at) AS hook_sources
                FROM table(agentshield_rules)
                WHERE rule_id = '%s'
                GROUP BY rule_id
            `, strings.ReplaceAll(id, "'", "''")))
            if err != nil || len(rows) == 0 {
                c.JSON(http.StatusNotFound, gin.H{"error": "custom rule not found in stream"})
                return
            }
            row := rows[0]
            querySQL := strVal(row, "query_sql")

            reinstallSQL := fmt.Sprintf(`
INSERT INTO agentshield_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at)
SELECT
  rule_id,
  arg_max(name,               updated_at) AS name,
  arg_max(version,            updated_at) AS version,
  arg_max(description,        updated_at) AS description,
  arg_max(query_sql,          updated_at) AS query_sql,
  true                                    AS enabled,
  false                                   AS deleted,
  arg_max(severity,           updated_at) AS severity,
  arg_max(category,           updated_at) AS category,
  arg_max(required_telemetry, updated_at) AS required_telemetry,
  arg_max(fp_rate,            updated_at) AS fp_rate,
  arg_max(hook_sources,       updated_at) AS hook_sources,
  now64(3)                                AS updated_at
FROM table(agentshield_rules)
WHERE rule_id = '%s'
GROUP BY rule_id`, strings.ReplaceAll(id, "'", "''"))

            if err := tp.ExecHTTP(&cfg.Timeplus, reinstallSQL); err != nil {
                c.JSON(http.StatusInternalServerError, gin.H{"error": "reinstall insert failed"})
                return
            }
            if err := tp.CreateRuleMV(&cfg.Timeplus, id, querySQL); err != nil {
                log.Printf("rules: custom reinstall MV %s: %v", id, err)
            }
            c.JSON(http.StatusOK, gin.H{"installed": true, "rule_id": id})
            return
        }

        // Builtin rules: install from hardcoded catalog.
        var found *tp.CatalogEntry
        for _, e := range tp.Catalog() {
            if e.ID == id {
                entry := e
                found = &entry
                break
            }
        }
        if found == nil {
            c.JSON(http.StatusNotFound, gin.H{"error": "not in builtin catalog"})
            return
        }

        escapedSQL := strings.ReplaceAll(found.QuerySQL, "'", "''")
        insertSQL := fmt.Sprintf(
            `INSERT INTO agentshield_rules (rule_id, name, version, description, query_sql, enabled, deleted, severity, category, required_telemetry, fp_rate, hook_sources, updated_at) VALUES ('%s','%s','%s','%s','%s',true,false,'%s','%s','','%s','%s',now64(3))`,
            id,
            strings.ReplaceAll(found.Name, "'", "''"),
            found.Version,
            strings.ReplaceAll(found.Description, "'", "''"),
            escapedSQL,
            found.Severity, found.Category,
            found.FPRate, found.HookSources,
        )
        if err := tp.ExecHTTP(&cfg.Timeplus, insertSQL); err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "reinstall insert failed"})
            return
        }
        if err := tp.CreateRuleMV(&cfg.Timeplus, id, found.QuerySQL); err != nil {
            log.Printf("rules: reinstall create MV %s: %v", id, err)
        }
        c.JSON(http.StatusOK, gin.H{"installed": true, "rule_id": id})
    }
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd backend && CGO_ENABLED=0 go build ./...
```

Expected: no output.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/handlers/rules.go
git commit -m "feat(rules): extend InstallRule to handle custom rule reinstall"
```

---

## Task 4: Backend — Register route and verify binary

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add route registration in `main.go`**

In the `api` route group (around line 89–120), add this line after `api.GET("/rules/catalog", ...)`:

```go
api.POST("/rules", handlers.CreateRule(cfg))
```

The full rules section should look like:
```go
api.GET("/rules", handlers.ListRules(cfg))
api.GET("/rules/catalog", handlers.ListCatalog(cfg))
api.GET("/rules/mv-status", handlers.MVStatus(cfg))
api.POST("/rules/sync", handlers.SyncMVs(cfg))
api.POST("/rules", handlers.CreateRule(cfg))
api.GET("/rules/:id", handlers.GetRule(cfg))
api.POST("/rules/:id/install", handlers.InstallRule(cfg))
api.POST("/rules/:id/toggle", handlers.ToggleRule(cfg))
api.POST("/rules/:id/upgrade", handlers.UpgradeRule(cfg))
api.DELETE("/rules/:id", handlers.UninstallRule(cfg))
```

- [ ] **Step 2: Build the binary**

```bash
cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go
```

Expected: produces `./server` with no errors.

- [ ] **Step 3: Smoke-test the new endpoint (server must be running)**

```bash
curl -s -X POST http://localhost:8080/api/rules \
  -H 'Content-Type: application/json' \
  -d '{"name":"Test","description":"Test","query_sql":"SELECT now64(3) AS detected_at, '\''__rule_id__'\'' AS rule_id, '\''__rule_name__'\'' AS rule_name, '\''warning'\'' AS severity, agent_id, deployment_id, session_id, run_id, hook_name, tool_name, '\''test'\'' AS message, event_data FROM agentshield_hook_events WHERE 1=0","severity":"warning"}' | jq .
```

Expected: JSON with `id` starting with `custom-`, `enabled: true`.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd/server/main.go backend/server
git commit -m "feat(rules): register POST /api/rules route"
```

---

## Task 5: Frontend — API client + App route

**Files:**
- Modify: `frontend/src/api/client.js`
- Modify: `frontend/src/App.jsx`

- [ ] **Step 1: Add `createRule` to `frontend/src/api/client.js`**

After the `installRule` line (line 73), add:

```js
createRule: (body) => post('/rules', body),
```

Full rules section after change:
```js
listRules: () => get('/rules'),
getRulesCatalog: () => get('/rules/catalog'),
getRule: (id) => get(`/rules/${id}`),
toggleRule: (id) => post(`/rules/${id}/toggle`, {}),
upgradeRule: (id, querySql) => post(`/rules/${id}/upgrade`, { query_sql: querySql }),
createRule: (body) => post('/rules', body),
installRule: (id) => post(`/rules/${id}/install`, {}),
uninstallRule: (id) =>
  fetch(`${BASE}/rules/${id}`, { method: 'DELETE' })
    .then(res => { if (!res.ok) throw new Error(`HTTP ${res.status}`); return res.json() }),
```

- [ ] **Step 2: Add the `/rules/new` route in `frontend/src/App.jsx`**

Add the import at the top (with the other page imports):
```js
import RuleCreate from './pages/RuleCreate'
```

Add the route **before** `/rules/:id` so React Router matches `/rules/new` first:
```jsx
<Route path="/rules/new" element={<RuleCreate />} />
<Route path="/rules/:id" element={<RuleDetail />} />
```

Full routes section after change:
```jsx
<Route path="/" element={<Navigate to="/dashboard" replace />} />
<Route path="/dashboard" element={<Dashboard />} />
<Route path="/agents" element={<AgentList />} />
<Route path="/agents/:id" element={<AgentDetail />} />
<Route path="/threats" element={<Threats />} />
<Route path="/cost" element={<CostGovernance />} />
<Route path="/rules" element={<RulePacks />} />
<Route path="/rules/new" element={<RuleCreate />} />
<Route path="/rules/:id" element={<RuleDetail />} />
<Route path="/investigate" element={<Investigate />} />
<Route path="/onboarding" element={<Onboarding />} />
```

- [ ] **Step 3: Verify build (RuleCreate.jsx doesn't exist yet — expect a build error)**

```bash
cd frontend && npm run build 2>&1 | grep -i error
```

Expected: error about `./pages/RuleCreate` not found. This confirms the import is wired and the file just needs to be created.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/api/client.js frontend/src/App.jsx
git commit -m "feat(rules): wire createRule API call and /rules/new route"
```

---

## Task 6: Frontend — RuleCreate page

**Files:**
- Create: `frontend/src/pages/RuleCreate.jsx`

### Context

The page has two columns:
- **Left (fixed 320px):** metadata form (name, description, severity, category, fp_rate, hook_sources)
- **Right (flex-1):** SQL editor (react-simple-code-editor + prismjs, already installed) + Test Run button + results table

The SQL editor defaults to a template with `'__rule_id__'` and `'__rule_name__'` placeholders. The backend substitutes these on save.

The Test Run button streams up to 20 rows via `api.protonQuery` (same pattern as `Investigate.jsx`). The Save Rule button is disabled until name and SQL are non-empty.

- [ ] **Step 1: Create `frontend/src/pages/RuleCreate.jsx`**

```jsx
import { useState, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import Editor from 'react-simple-code-editor'
import { highlight, languages } from 'prismjs/components/prism-core'
import 'prismjs/components/prism-sql'
import 'prismjs/themes/prism-tomorrow.css'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import Footer from '../components/Footer'
import PageHeader from '../components/PageHeader'
import { api } from '../api/client'

const defaultSQL = `SELECT
  now64(3)                                          AS detected_at,
  '__rule_id__'                                     AS rule_id,
  '__rule_name__'                                   AS rule_name,
  'warning'                                         AS severity,
  agent_id,
  deployment_id,
  session_id,
  run_id,
  hook_name,
  tool_name,
  concat('Custom rule triggered: ', hook_name)      AS message,
  event_data
FROM agentshield_hook_events
WHERE 1 = 0 -- replace with your detection condition`

export default function RuleCreate() {
  const navigate = useNavigate()
  const [form, setForm] = useState({
    name: '',
    description: '',
    severity: 'warning',
    category: '',
    fp_rate: 'low',
    hook_sources: '',
  })
  const [sql, setSql] = useState(defaultSQL)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState(null)

  const [testRows, setTestRows] = useState([])
  const [testColumns, setTestColumns] = useState([])
  const [testStreaming, setTestStreaming] = useState(false)
  const [testError, setTestError] = useState(null)
  const [testMeta, setTestMeta] = useState(null)
  const abortRef = useRef(null)
  const startTimeRef = useRef(null)

  const setField = (key, val) => setForm(prev => ({ ...prev, [key]: val }))

  const handleTestRun = async () => {
    if (testStreaming) {
      abortRef.current?.abort()
      return
    }
    const controller = new AbortController()
    abortRef.current = controller
    setTestRows([])
    setTestColumns([])
    setTestError(null)
    setTestMeta(null)
    setTestStreaming(true)
    startTimeRef.current = performance.now()
    let count = 0
    try {
      for await (const row of api.protonQuery(sql, { signal: controller.signal })) {
        if (count >= 20) { controller.abort(); break }
        count++
        setTestRows(prev => {
          if (prev.length === 0) setTestColumns(Object.keys(row))
          return [...prev, row]
        })
      }
      const elapsed = ((performance.now() - startTimeRef.current) / 1000).toFixed(2)
      setTestMeta(`${count} rows · ${elapsed}s`)
    } catch (err) {
      if (err.name !== 'AbortError') setTestError(err.message)
    } finally {
      setTestStreaming(false)
    }
  }

  const handleSave = async () => {
    setSaving(true)
    setSaveError(null)
    try {
      await api.createRule({
        name: form.name,
        description: form.description,
        query_sql: sql,
        severity: form.severity,
        category: form.category,
        fp_rate: form.fp_rate,
        hook_sources: form.hook_sources,
      })
      navigate('/rules')
    } catch (err) {
      setSaveError(err.message || 'Failed to create rule')
    } finally {
      setSaving(false)
    }
  }

  const canSave = form.name.trim() !== '' && sql.trim() !== '' && !saving

  const inputCls = 'bg-[#0f0c17] border border-[#574149]/20 rounded-sm px-3 py-2 text-xs text-[#e7dff0] placeholder-zinc-600 focus:outline-none focus:border-[#ffb0cf]/40'
  const labelCls = 'text-[10px] font-semibold text-zinc-500 uppercase tracking-widest'

  return (
    <div className="min-h-screen bg-[#15121d] text-[#e7dff0]">
      <Sidebar />
      <TopNav />
      <Footer />
      <main className="pl-sidebar pt-16 pb-8 min-h-screen">
        <div className="p-6 lg:p-10 flex flex-col gap-6" style={{ height: 'calc(100vh - 64px)' }}>
          <PageHeader
            title="New Custom Rule"
            subtitle="Write a detection SQL query against agentshield_hook_events."
            actions={
              <div className="flex items-center gap-3">
                <button
                  onClick={() => navigate('/rules')}
                  className="px-4 py-2 text-xs font-semibold text-zinc-400 hover:text-white transition-colors"
                >
                  Cancel
                </button>
                <button
                  onClick={handleSave}
                  disabled={!canSave}
                  className="px-5 py-2 text-xs font-semibold rounded-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] disabled:opacity-40 disabled:cursor-not-allowed hover:scale-[0.98] transition-all"
                >
                  {saving ? 'Saving…' : 'Save Rule'}
                </button>
              </div>
            }
          />

          {saveError && (
            <div className="px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded-sm text-xs text-[#ffb4ab] font-mono flex items-center gap-2">
              <span className="material-symbols-outlined text-xs shrink-0">error</span>
              {saveError}
            </div>
          )}

          <div className="flex gap-6 flex-1 overflow-hidden min-h-0">
            {/* Left: metadata */}
            <aside className="w-80 shrink-0 overflow-y-auto custom-scrollbar">
              <div className="bg-[#2c2834] rounded-sm border border-[#574149]/10 p-6 flex flex-col gap-5">
                <h3 className="text-[10px] font-bold text-[#ffb0cf] tracking-widest uppercase">Rule Metadata</h3>

                <div className="flex flex-col gap-1.5">
                  <label className={labelCls}>Name *</label>
                  <input
                    type="text"
                    value={form.name}
                    onChange={e => setField('name', e.target.value)}
                    placeholder="My Custom Rule"
                    className={inputCls}
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label className={labelCls}>Description *</label>
                  <textarea
                    value={form.description}
                    onChange={e => setField('description', e.target.value)}
                    placeholder="What does this rule detect?"
                    rows={3}
                    className={`${inputCls} resize-none`}
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label className={labelCls}>Severity</label>
                  <select value={form.severity} onChange={e => setField('severity', e.target.value)} className={inputCls}>
                    <option value="critical">Critical</option>
                    <option value="warning">Warning</option>
                    <option value="info">Info</option>
                  </select>
                </div>

                <div className="flex flex-col gap-1.5">
                  <label className={labelCls}>Category</label>
                  <input
                    type="text"
                    value={form.category}
                    onChange={e => setField('category', e.target.value)}
                    placeholder="e.g. injection"
                    className={inputCls}
                  />
                </div>

                <div className="flex flex-col gap-1.5">
                  <label className={labelCls}>False Positive Rate</label>
                  <select value={form.fp_rate} onChange={e => setField('fp_rate', e.target.value)} className={inputCls}>
                    <option value="low">Low</option>
                    <option value="medium">Medium</option>
                    <option value="high">High</option>
                  </select>
                </div>

                <div className="flex flex-col gap-1.5">
                  <label className={labelCls}>Hook Sources</label>
                  <input
                    type="text"
                    value={form.hook_sources}
                    onChange={e => setField('hook_sources', e.target.value)}
                    placeholder="e.g. before_tool_call"
                    className={inputCls}
                  />
                </div>

                <p className="text-[10px] text-zinc-600 leading-relaxed">
                  Use <code className="text-zinc-500">'__rule_id__'</code> and <code className="text-zinc-500">'__rule_name__'</code> in your SQL — they are substituted with the generated ID and rule name on save.
                </p>
              </div>
            </aside>

            {/* Right: SQL editor + test results */}
            <section className="flex-1 flex flex-col min-w-0 gap-4 overflow-hidden">
              {/* Editor */}
              <div className="flex flex-col bg-[#2c2834] rounded-sm border border-[#574149]/10 overflow-hidden">
                <div className="bg-[#211e29] px-6 py-3 flex justify-between items-center border-b border-[#574149]/10">
                  <div className="flex items-center gap-4">
                    <span className="text-[10px] font-semibold text-zinc-400 tracking-widest uppercase">Detection SQL</span>
                    <div className="h-3 w-px bg-[#574149]/20" />
                    <div className="flex gap-2 items-center">
                      <span className={`w-1.5 h-1.5 rounded-full ${testStreaming ? 'bg-[#ffb0cf] animate-pulse' : 'bg-zinc-600'}`} />
                      <span className={`text-[10px] uppercase font-semibold tracking-tight ${testStreaming ? 'text-[#ffb0cf]' : 'text-zinc-600'}`}>
                        {testStreaming ? 'Running' : 'Idle'}
                      </span>
                    </div>
                  </div>
                  <button
                    onClick={handleTestRun}
                    className={`px-5 py-1.5 font-semibold text-[10px] rounded-sm flex items-center gap-2 uppercase tracking-widest transition-all ${
                      testStreaming
                        ? 'bg-[#93000a] text-[#ffdad6] hover:opacity-90'
                        : 'bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] hover:scale-[0.98] shadow-[0_2px_12px_rgba(213,63,140,0.2)]'
                    }`}
                  >
                    <span className="material-symbols-outlined text-xs" style={{ fontVariationSettings: "'FILL' 1" }}>
                      {testStreaming ? 'stop' : 'play_arrow'}
                    </span>
                    {testStreaming ? 'Stop' : 'Test Run'}
                  </button>
                </div>
                <div className="h-48 overflow-auto custom-scrollbar bg-[#0f0c17]">
                  <Editor
                    value={sql}
                    onValueChange={setSql}
                    highlight={code => highlight(code, languages.sql, 'sql')}
                    padding={24}
                    style={{
                      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
                      fontSize: 12,
                      lineHeight: 1.6,
                      minHeight: '100%',
                      background: 'transparent',
                      color: '#d4d4d8',
                    }}
                    textareaClassName="focus:outline-none"
                  />
                </div>
              </div>

              {/* Test Results */}
              <div className="flex-1 flex flex-col bg-[#2c2834] rounded-sm border border-[#574149]/10 overflow-hidden">
                <div className="p-4 flex items-baseline gap-3 border-b border-[#574149]/10 shrink-0">
                  <h3 className="text-base font-semibold text-[#e7dff0]">Test Results</h3>
                  {testMeta && (
                    <span className="text-[10px] font-mono bg-[#0f0c17] px-2 py-0.5 rounded-sm text-zinc-500 border border-[#574149]/5">{testMeta}</span>
                  )}
                  {testStreaming && (
                    <span className="text-[10px] font-mono bg-[#ffb0cf]/10 px-2 py-0.5 rounded-sm text-[#ffb0cf] border border-[#ffb0cf]/20">
                      {testRows.length} rows streaming…
                    </span>
                  )}
                </div>

                {testError && (
                  <div className="m-4 px-4 py-3 bg-[#93000a]/20 border border-[#ffb4ab]/20 rounded-sm text-xs text-[#ffb4ab] font-mono flex items-start gap-2">
                    <span className="material-symbols-outlined text-xs mt-0.5 shrink-0">error</span>
                    {testError}
                  </div>
                )}

                <div className="flex-1 overflow-auto custom-scrollbar">
                  {testRows.length === 0 && !testStreaming && !testError ? (
                    <div className="h-full flex flex-col items-center justify-center gap-3 text-zinc-600">
                      <span className="material-symbols-outlined text-4xl">play_circle</span>
                      <p className="text-xs">Click "Test Run" to preview query results</p>
                    </div>
                  ) : (
                    <table className="w-full text-left border-collapse">
                      <thead className="sticky top-0 z-10 bg-[#211e29] border-b border-[#574149]/10">
                        <tr>
                          {testColumns.map(col => (
                            <th key={col} className="px-4 py-3 text-[10px] font-bold text-zinc-500 uppercase tracking-widest whitespace-nowrap">{col}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-[#574149]/5 text-[11px] font-mono">
                        {testRows.map((row, i) => (
                          <tr key={i} className="hover:bg-[#211e29] transition-colors text-zinc-300">
                            {testColumns.map(col => (
                              <td key={col} className="px-4 py-3 whitespace-nowrap max-w-xs overflow-hidden text-ellipsis">
                                {String(row[col] ?? '')}
                              </td>
                            ))}
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>
              </div>
            </section>
          </div>
        </div>
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Verify build**

```bash
cd frontend && npm run build 2>&1 | tail -6
```

Expected: `✓ built in X.XXs` with no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/RuleCreate.jsx
git commit -m "feat(rules): add RuleCreate page with SQL editor and test run"
```

---

## Task 7: Frontend — Update RulePacks page

**Files:**
- Modify: `frontend/src/pages/RulePacks.jsx`

Two changes:
1. Wire the "New Custom Rule" button to navigate to `/rules/new`
2. In the "Available to Install" catalog section, show a `Custom` badge and `tune` icon for custom entries (`is_custom === true`)

- [ ] **Step 1: Add `useNavigate` import to `RulePacks.jsx`**

Line 1 currently:
```js
import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
```

Change to:
```js
import { useState, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
```

- [ ] **Step 2: Add `navigate` inside the `RulePacks` component**

After `export default function RulePacks() {`, add as the first line of the function body:
```js
const navigate = useNavigate()
```

- [ ] **Step 3: Wire the "New Custom Rule" button**

Find (line ~202):
```jsx
<button className="px-4 py-2 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] text-xs font-semibold rounded-sm">
  New Custom Rule
</button>
```

Replace with:
```jsx
<button
  onClick={() => navigate('/rules/new')}
  className="px-4 py-2 bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] text-xs font-semibold rounded-sm hover:scale-[0.98] transition-all"
>
  New Custom Rule
</button>
```

- [ ] **Step 4: Update the catalog item card to show custom badge and icon**

Find the catalog item card inside the `uninstalledCatalog.map` section (around line ~256):
```jsx
<div key={entry.id} className="bg-[#1d1a25]/60 border border-dashed border-zinc-700/40 p-5 rounded-sm flex flex-col gap-4">
  <div className="flex justify-between items-start">
    <div className="w-10 h-10 bg-[#0f0c17] flex items-center justify-center rounded-sm text-zinc-600">
      <span className="material-symbols-outlined">{RULE_ICONS[entry.id] || 'shield'}</span>
    </div>
    <span className={`text-[10px] px-2 py-0.5 rounded-full font-semibold uppercase tracking-wide border ${
      entry.severity === 'critical'
        ? 'bg-[#ffb0cf]/10 text-[#ffb0cf] border-[#ffb0cf]/20'
        : 'bg-amber-500/10 text-amber-400 border-amber-500/20'
    }`}>{entry.severity}</span>
  </div>
```

Replace with:
```jsx
<div key={entry.id} className="bg-[#1d1a25]/60 border border-dashed border-zinc-700/40 p-5 rounded-sm flex flex-col gap-4">
  <div className="flex justify-between items-start">
    <div className="w-10 h-10 bg-[#0f0c17] flex items-center justify-center rounded-sm text-zinc-600">
      <span className="material-symbols-outlined">{entry.is_custom ? 'tune' : (RULE_ICONS[entry.id] || 'shield')}</span>
    </div>
    <div className="flex items-center gap-2">
      {entry.is_custom && (
        <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-[#2c2834] text-zinc-400 border border-zinc-700/40 font-semibold uppercase tracking-wide">Custom</span>
      )}
      <span className={`text-[10px] px-2 py-0.5 rounded-full font-semibold uppercase tracking-wide border ${
        entry.severity === 'critical'
          ? 'bg-[#ffb0cf]/10 text-[#ffb0cf] border-[#ffb0cf]/20'
          : 'bg-amber-500/10 text-amber-400 border-amber-500/20'
      }`}>{entry.severity}</span>
    </div>
  </div>
```

- [ ] **Step 5: Verify build**

```bash
cd frontend && npm run build 2>&1 | tail -6
```

Expected: `✓ built in X.XXs` with no errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/pages/RulePacks.jsx
git commit -m "feat(rules): wire New Custom Rule button and add Custom badge in catalog"
```

---

## Manual End-to-End Verification

After all tasks are complete, start the server (`cd backend && CGO_ENABLED=0 go build -o server cmd/server/main.go && ./server`) and run the frontend dev server (`cd frontend && npm run dev`), then verify:

1. **Create:** Navigate to `/rules` → click "New Custom Rule" → lands on `/rules/new` ✓
2. **Form:** Fill Name + Description. SQL editor shows default template with syntax highlighting ✓
3. **Test Run:** Click "Test Run" → streams results or shows empty state (default SQL returns 0 rows) ✓
4. **Save:** Click "Save Rule" → redirected to `/rules` → new rule card appears in the installed grid ✓
5. **Detail:** Click "View Details" on the custom rule → rule detail page shows Detection SQL with highlighting ✓
6. **Uninstall:** Click Uninstall on the custom rule detail page → redirected to `/rules` → rule disappears from grid ✓
7. **Reinstall:** On `/rules`, the uninstalled custom rule appears in "Available to Install" with `Custom` badge and `tune` icon ✓
8. **Reinstall confirm:** Click Install → custom rule returns to installed grid ✓
