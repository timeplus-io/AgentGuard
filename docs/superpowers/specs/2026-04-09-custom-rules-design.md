# Custom Rule Creation Design

## Goal

Allow users to create, test, install, uninstall, and reinstall custom detection rules from the AgentShield UI, alongside the existing builtin rule packs.

## Architecture

Custom rules reuse the existing `agentshield_rules` stream and MV infrastructure. They are distinguished from builtins by a `custom-<unix-ms>` ID prefix. The catalog is extended to surface deleted custom rules so they can be reinstalled. No new streams or tables are required.

---

## Backend

### New endpoint: `POST /api/rules`

Handler: `CreateRule` in `backend/internal/handlers/rules.go`

**Request body:**
```json
{
  "name": "string (required)",
  "description": "string (required)",
  "query_sql": "string (required)",
  "severity": "critical | warning | info (required)",
  "category": "string (optional)",
  "fp_rate": "low | medium | high (optional)",
  "hook_sources": "string (optional, e.g. before_tool_call)"
}
```

**Behaviour:**
- Returns 400 if `name`, `description`, or `query_sql` is empty
- Generates `rule_id = "custom-" + strconv.FormatInt(time.Now().UnixMilli(), 10)`
- Inserts one row into `agentshield_rules` with `enabled=true, deleted=false, version="1.0.0"`
- Calls `tp.CreateRuleMV(cfg, ruleID, querySQL)` to start detection immediately
- Returns the full `RulePack` JSON on success

**Route registration** in `cmd/server/main.go`:
```go
api.POST("/rules", handlers.CreateRule(cfg))
```

---

### Modified: `GET /api/rules/catalog` (`ListCatalog`)

After loading builtin catalog entries, also queries the stream for deleted custom rules:

```sql
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
HAVING arg_max(deleted, updated_at) = true
```

Each deleted custom rule is appended to the catalog response with `"is_custom": true`. Installed custom rules (not deleted) are excluded via the `installedIDs` map already in the handler.

The `CatalogItem` struct in the handler gains a boolean field `IsCustom bool json:"is_custom"`.

---

### Modified: `POST /api/rules/:id/install` (`InstallRule`)

Check the ID prefix:

- **`rp-` prefix:** existing builtin catalog logic unchanged
- **`custom-` prefix:** read last known state from the stream, re-insert an `enabled=true, deleted=false` row, recreate the MV:

```sql
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
WHERE rule_id = '<id>'
GROUP BY rule_id
```

Then call `tp.CreateRuleMV(cfg, id, querySql)` where `querySql` is read from the row above.

---

## Frontend

### New file: `frontend/src/pages/RuleCreate.jsx`

**Route:** `/rules/new` (added to `App.jsx` before `/rules/:id` to avoid param collision)

**Layout:** Standard Sidebar/TopNav/Footer shell. Page content is a two-column flex row:

- **Left column (40%):** metadata form
  - Name — `<input>` text, required
  - Description — `<textarea>`, required
  - Severity — `<select>`: critical / warning / info, default `warning`
  - Category — `<input>` text, optional
  - FP Rate — `<select>`: low / medium / high, default `low`
  - Hook Sources — `<input>` text, optional (placeholder: `before_tool_call`)

- **Right column (60%):** SQL editor + test run
  - `react-simple-code-editor` + prismjs SQL highlighting (same setup as `Investigate.jsx`)
  - "Test Run" button: streams via `api.protonQuery`, shows up to 20 rows with row count + elapsed time badge
  - Results table below editor: same style as Investigate results panel
  - Abort support via `AbortController` while streaming

**Page header actions:**
- "Cancel" — plain text link back to `/rules`
- "Save Rule" — gradient CTA button, disabled when `name` or `query_sql` is empty or while saving
- On save: calls `api.createRule(body)`, then `navigate('/rules')`

---

### Modified: `frontend/src/pages/RulePacks.jsx`

- "New Custom Rule" button: add `useNavigate` and wire `onClick={() => navigate('/rules/new')}`
- In the "Available to Install" catalog section: custom entries (`is_custom === true`) render a `Custom` pill badge (`text-[10px] px-1.5 py-0.5 rounded-full bg-[#2c2834] text-zinc-400 border border-zinc-700/40`) and use the `tune` material icon

---

### Modified: `frontend/src/api/client.js`

Add one function:
```js
createRule: (body) => post('/rules', body),
```

---

## Rule Lifecycle

| State | Visible in | Action available |
|-------|-----------|-----------------|
| Created (enabled) | Installed grid | Toggle, View Detail, Uninstall |
| Created (disabled) | Installed grid | Toggle, View Detail, Uninstall |
| Uninstalled (deleted) | Available to Install catalog | Install |
| Reinstalled | Installed grid | Toggle, View Detail, Uninstall |

## Out of Scope

- Editing a custom rule's SQL after creation (use the existing Upgrade path)
- SQL validation on the backend (test run is client-side via the streaming query endpoint)
- Custom rule versioning beyond `1.0.0`
