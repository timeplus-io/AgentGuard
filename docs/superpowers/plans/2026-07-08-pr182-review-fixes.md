# PR #182 Review Follow-up Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the six backend defects found in the post-merge review of PR #182 (editable rule parameters), each with tests, in one PR.

**Architecture:** Six independent, per-task-committed fixes across `internal/handlers`, `internal/catalog`, and `internal/timeplus`. No schema or API-breaking changes: two hardening swaps (escaper, strict JSON decode), one credential fix, one durability fix (atomic file write), one import-policy guard (new `ImportResult` fields + 2 display lines in RulePacks.jsx), and one preflight-fidelity fix (derive the validation sink from the live stream's DDL instead of a compiled-in constant).

**Tech Stack:** Go 1.23 (backend), Gin, Timeplus Proton (timeplusd), React/Vite (one small JSX change), `gh` CLI for the PR.

## Global Constraints

- Always build/test with `CGO_ENABLED=0` (repo convention, CLAUDE.md).
- Work from repo root `/home/haohang/AgentGuard`; backend commands run in `backend/`.
- The merged state of PR #182 is `52519fa` = `main`. Branch: `fix/pr182-review-followups` off `main`.
- Do NOT touch `backend/internal/rules/` compiler/service/store code — those findings are explicitly deferred (out of scope).
- No new dependencies.
- Every commit message ends with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Line numbers below refer to the state at commit `52519fa`; re-locate by content if drifted.
- Untracked local files (`.worktrees/`, `demo.sh`, screenshots, `AGENTS.md`, `docker-compose.override.yaml`) must NOT be committed.

---

### Task 1: Backslash-aware escaping in the unauthenticated webhook dedup query

`POST /api/alerts/webhook` is intentionally unauthenticated. `isThreatAlreadyOpen` interpolates the payload's `agent_id`/`deployment_id`/`rule_id` with quote-only `escapeSQL` (helpers.go:148). Proton reads `\'` as an escaped quote, so a value like `\' OR 1=1 --` terminates the string literal. The correct escaper `escapeSQLStr` (backslash-first, delegates to `rules.EscapeSQLString`) already exists in this same file (rules.go:31-33) — the fix swaps three call sites and extracts the SQL builder so escaping is unit-testable.

**Files:**
- Modify: `backend/internal/handlers/rules.go:714-740` (`isThreatAlreadyOpen`)
- Test: `backend/internal/handlers/rules_handler_test.go`

**Interfaces:**
- Produces: `threatOpenCheckSQL(agentID, deploymentID, ruleID string) string` (package-private, same file). `isThreatAlreadyOpen`'s signature is unchanged.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/handlers/rules_handler_test.go`:

```go
// The alert webhook is unauthenticated, so the dedup query's identifiers are
// attacker-controllable. Proton interprets \' as an escaped quote inside a
// string literal, so quote-only doubling ('' escaping) is bypassable with a
// preceding backslash; the query must use the backslash-aware escaper.
func TestThreatOpenCheckSQL_EscapesBackslashQuoteBypass(t *testing.T) {
	payload := `\' OR 1=1 --`

	sql := threatOpenCheckSQL(payload, payload, payload)
	// escapeSQLStr doubles backslashes first, then quotes: \' → \\''.
	if !strings.Contains(sql, `\\''`) {
		t.Fatalf("payload not backslash-escaped: %s", sql)
	}
	// The raw breakout sequence must not survive into the SQL.
	if strings.Contains(sql, `\' OR`) {
		t.Fatalf("raw \\' escape sequence survived into SQL: %s", sql)
	}

	// The no-deployment query shape must escape identically.
	sql2 := threatOpenCheckSQL(payload, "", payload)
	if strings.Contains(sql2, `\' OR`) {
		t.Fatalf("raw \\' survived in no-deployment shape: %s", sql2)
	}
	if !strings.Contains(sql2, `\\''`) {
		t.Fatalf("no-deployment shape not backslash-escaped: %s", sql2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestThreatOpenCheckSQL -v`
Expected: FAIL to compile with `undefined: threatOpenCheckSQL`

- [ ] **Step 3: Implement**

In `backend/internal/handlers/rules.go`, replace the body-building portion of `isThreatAlreadyOpen` (lines 714-740) with:

```go
// threatOpenCheckSQL builds the open-threat dedup query for the alert webhook.
// The three identifiers arrive on the UNAUTHENTICATED /api/alerts/webhook
// endpoint, so they must go through escapeSQLStr (backslash-aware): with the
// quote-only escapeSQL, a value like `\' OR (...) --` would terminate the
// literal (Proton reads \' as an escaped quote). Extracted from
// isThreatAlreadyOpen so the escaping is unit-testable.
func threatOpenCheckSQL(agentID, deploymentID, ruleID string) string {
	if deploymentID != "" {
		return fmt.Sprintf(
			`SELECT count() AS cnt FROM table(agentguard_threats) WHERE agent_id = '%s' AND deployment_id = '%s' AND rule_id = '%s' AND (status = '' OR status = 'open')`,
			escapeSQLStr(agentID), escapeSQLStr(deploymentID), escapeSQLStr(ruleID),
		)
	}
	return fmt.Sprintf(
		`SELECT count() AS cnt FROM table(agentguard_threats) WHERE agent_id = '%s' AND rule_id = '%s' AND (status = '' OR status = 'open')`,
		escapeSQLStr(agentID), escapeSQLStr(ruleID),
	)
}

// isThreatAlreadyOpen queries agentguard_threats to check whether an open
// threat already exists for the given (agent_id, deployment_id, rule_id)
// combination. When deployment_id is empty the check is by (agent_id, rule_id)
// alone. Returns false on any error so the caller fails open.
func isThreatAlreadyOpen(cfg *config.Config, agentID, deploymentID, ruleID string) bool {
	if agentID == "" || ruleID == "" {
		return false
	}
	rows, err := tp.Query(&cfg.Timeplus, threatOpenCheckSQL(agentID, deploymentID, ruleID))
	if err != nil || len(rows) == 0 {
		return false
	}
	return parseInt64(rows[0]["cnt"]) > 0
}
```

(Keep the existing doc comment content for `isThreatAlreadyOpen`; the diff is: extract builder, swap `escapeSQL` → `escapeSQLStr`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -v -run 'TestThreatOpenCheckSQL'`
Expected: PASS. Then full package: `CGO_ENABLED=0 go test ./internal/handlers/` — PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/rules.go backend/internal/handlers/rules_handler_test.go
git commit -m "fix(threats): backslash-aware escaping in unauthenticated webhook dedup query

isThreatAlreadyOpen interpolated attacker-controllable webhook fields with the
quote-only escapeSQL; Proton reads \\' as an escaped quote, so a trailing
backslash let a payload break out of the string literal (bounded to blind
read-only subqueries). Swap to escapeSQLStr (backslash-first) and extract
threatOpenCheckSQL so the escaping is pinned by a unit test.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

Note for reviewer: other quote-only `escapeSQL` sites (threats.go, cost.go, timeline*.go) sit behind authenticated endpoints and are left for a separate sweep — do not expand scope here.

---

### Task 2: Reject unknown fields in rule mutation bodies

A nil `Overrides` map on `PUT /api/rules/:id/configuration` means "reset ALL parameters to defaults" (documented semantics). Because `decodeMutationBody` tolerates unknown JSON fields, a typo'd key (`{"overides": {...}}`) decodes to nil overrides and silently performs a destructive full reset (and `POST /rules/:id/install` silently installs with defaults). Add `DisallowUnknownFields`. Both callers (rules.go:253, :278) already return 400 on decode error — no handler change needed.

**Files:**
- Modify: `backend/internal/handlers/rules.go:53-66` (`decodeMutationBody`)
- Test: `backend/internal/handlers/rules_handler_test.go:144-185` (`TestDecodeMutationBody`)

**Interfaces:**
- Consumes/Produces: `decodeMutationBody(c *gin.Context) (mutationBody, error)` — signature unchanged; empty body and `{}` remain valid (nil overrides).

- [ ] **Step 1: Write the failing tests**

Add two sub-tests inside `TestDecodeMutationBody` (after the "malformed JSON errors" sub-test at line 178-184):

```go
	t.Run("unknown fields are rejected (typo must not become reset-all)", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPut, "/",
			strings.NewReader(`{"overides":{"threshold":5}}`))
		if _, err := decodeMutationBody(c); err == nil {
			t.Fatal("expected error for unknown field; nil Overrides would silently reset every parameter to defaults")
		}
	})

	t.Run("empty JSON object stays valid with nil overrides", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
		body, err := decodeMutationBody(c)
		if err != nil {
			t.Fatalf("decode {}: %v", err)
		}
		if body.Overrides != nil {
			t.Errorf("expected nil overrides for {}, got %#v", body.Overrides)
		}
	})
```

- [ ] **Step 2: Run test to verify the new sub-test fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestDecodeMutationBody -v`
Expected: FAIL on "unknown fields are rejected" (decode currently succeeds); "empty JSON object" passes.

- [ ] **Step 3: Implement**

In `decodeMutationBody`, add one line and extend the doc comment:

```go
// decodeMutationBody parses an optional {overrides, reason} body. It uses
// json.Decoder.UseNumber() so large integer override values reach the compiler
// without float64 rounding (per the rules package's JSON-consumer contract). An
// empty body is valid and yields a nil override map (install-with-defaults /
// configure-reset-all). Unknown fields are rejected: nil Overrides means
// "reset every parameter to defaults", so a typo'd key (e.g. {"overides": …})
// must fail loudly rather than silently decode into a destructive full reset.
func decodeMutationBody(c *gin.Context) (mutationBody, error) {
	var body mutationBody
	dec := json.NewDecoder(c.Request.Body)
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		return mutationBody{}, err
	}
	return body, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestDecodeMutationBody -v`
Expected: PASS (all 5 sub-tests). Frontend contract check: the UI sends only `{overrides, reason}` (ConfigurationTab) and `{}` (upgrade uses a different bodyless handler) — no client sends extra keys.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/handlers/rules.go backend/internal/handlers/rules_handler_test.go
git commit -m "fix(rules): reject unknown fields in install/configure bodies

nil Overrides is the documented reset-all-to-defaults semantics, so a typo'd
body key ({\"overides\": ...}) silently wiped every override and redeployed the
MV with defaults, returning 200. DisallowUnknownFields turns that into a 400.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Session credentials for the embedding-dimension probe in SyncMVs

`SyncMVs` (rules.go:664-698) resolves `reqCfg := auth.RequestCfg(c, cfg)` (overlays session Timeplus user/password — middleware.go:33-46) and uses it for `Sync()` and `SyncDLP`, but the probe on line 692 reads `tp.ProbeEmbeddingDim(&cfg.Timeplus)` — the process-wide config without session credentials. Where credentials come only from session login, the probe fails, its error is discarded, and `recordDLPProvisioned` records a stale fallback dim after every sync. Pre-PR code used the request-scoped config (the variable was shadowed as `cfg`); the rename to `reqCfg` missed this call. `RequestCfg` overlays only `Timeplus.User`/`Timeplus.Password`, so `cfg.DLP == reqCfg.DLP` — the `recordDLPProvisioned(shared, cfg.DLP, …)` argument is unaffected.

**Files:**
- Modify: `backend/internal/handlers/rules.go:692`

**Interfaces:** none (internal call-site fix).

- [ ] **Step 1: Implement the one-line fix**

```go
			detectedDim, _ := tp.ProbeEmbeddingDim(&reqCfg.Timeplus)
```

**No new unit test.** Justification (product-vs-harness): the defect is a wrong variable reference inside a handler closure; `tp.ProbeEmbeddingDim` and `tp.SyncDLP` are package-level functions with no injection seam, so a unit test would require adding a test-only seam to production code — scope creep for a one-token fix. The behavior is exercised live by the integration suite's `/api/rules/sync` calls (Task 7 runs it against a real timeplusd with session-credential auth).

- [ ] **Step 2: Compile and run package tests**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/handlers/`
Expected: build OK, tests PASS.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/handlers/rules.go
git commit -m "fix(rules): use session credentials for embedding-dim probe in SyncMVs

The reqCfg rename left ProbeEmbeddingDim on the base config; on deployments
where Timeplus credentials come only from session login the probe silently
failed and recordDLPProvisioned fell back to the stale configured dim after
every POST /api/rules/sync.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Atomic catalog definition writes

`saveLocked` (catalog.go:371-428) persists definitions with bare `os.WriteFile` (truncate-then-write). A crash mid-write leaves truncated YAML that `loadDir` silently skips on restart — the definition vanishes and its installed rule is orphaned. `internal/rules/service.go:92-94` explicitly relies on "Save being atomic enough" for the `UpdateDefinition` rollback contract. Fix: same-directory temp file + fsync + rename.

**Files:**
- Modify: `backend/internal/catalog/catalog.go:420-423` (inside `saveLocked`) + new helper in the same file
- Test: `backend/internal/catalog/catalog_test.go`

**Interfaces:**
- Produces: `writeFileAtomic(path string, data []byte, perm os.FileMode) error` (package-private, catalog.go).

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/catalog/catalog_test.go`:

```go
// Save must go through temp-file+rename so a crash mid-write can never leave a
// truncated definition file (loadDir silently skips unparseable files, so
// truncation means the definition — and the UpdateDefinition rollback source —
// vanishes on restart). Crash timing itself is not unit-testable; these pin
// the observable half: content round-trips through overwrite, no temp litter
// survives a Save, and stray temp files from a crashed Save never load.
func TestSave_AtomicWriteLeavesNoTempLitter(t *testing.T) {
	dir := t.TempDir()
	m, err := New(fstest.MapFS{}, dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	def := Definition{ID: "rp-atomic", Name: "A", Version: "1.0.0",
		SQLTemplate: "SELECT 1", Origin: OriginCustom}
	if err := m.Save(def); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Overwrite: the UpdateDefinition rollback path re-Saves an existing file.
	def.Name = "B"
	if err := m.Save(def); err != nil {
		t.Fatalf("re-Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "rp-atomic.yaml" {
			t.Errorf("unexpected file in catalog dir after Save: %s", e.Name())
		}
	}
	m2, err := New(fstest.MapFS{}, dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := m2.Lookup("rp-atomic")
	if !ok || got.Name != "B" {
		t.Errorf("reloaded definition = %+v, ok=%v; want Name=B", got, ok)
	}
}

func TestLoadDir_IgnoresAtomicTempFiles(t *testing.T) {
	dir := t.TempDir()
	stray := filepath.Join(dir, ".rp-stray.yaml.tmp-12345")
	if err := os.WriteFile(stray, []byte("id: rp-stray\nname: X\nversion: 1.0.0\nsql_template: SELECT 1\n"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	m, err := New(fstest.MapFS{}, dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.Exists("rp-stray") {
		t.Error("stray atomic-write temp file must not be loaded as a definition")
	}
}
```

(Add `"path/filepath"` and `"os"` to the test file's imports if not already present.)

- [ ] **Step 2: Run tests to verify current state**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/ -run 'TestSave_Atomic|TestLoadDir_IgnoresAtomic' -v`
Expected: both PASS already (WriteFile leaves no litter either) — these tests pin the contract so the refactor can't regress it. This is a refactor-with-pinned-behavior step, not strict red-green.

- [ ] **Step 3: Implement**

In `catalog.go`, replace lines 420-423:

```go
	path := filepath.Join(m.dir, d.ID+".yaml")
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
```

and add the helper (place it directly after `saveLocked`):

```go
// writeFileAtomic writes data to path via a same-directory temp file, fsync,
// and rename, so a crash mid-write can never leave a truncated file at path —
// readers see either the old content or the new content, never a partial one.
// This backs the UpdateDefinition rollback contract (internal/rules/service.go):
// re-Saving the previous definition restores it byte-for-byte, which only holds
// if a Save can never destroy the on-disk copy it replaces. The temp name
// starts with '.' and never ends in ".yaml", so loadDir ignores strays left by
// a crash between create and rename. The directory itself is not fsynced: on
// power loss the rename may be lost, but either the old or the new complete
// file survives, which is all the contract needs.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/`
Expected: PASS (full package — the existing Save/Clone/round-trip tests exercise the new write path heavily).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/catalog/catalog.go backend/internal/catalog/catalog_test.go
git commit -m "fix(catalog): atomic definition writes (temp file + fsync + rename)

saveLocked used bare os.WriteFile (truncate-then-write); a crash mid-write left
truncated YAML that loadDir silently skips on restart, vanishing the definition
and orphaning its installed rule. UpdateDefinition's rollback explicitly relies
on Save being atomic (service.go). Readers now see either the old or the new
complete file, never a partial one.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Import must not overwrite builtins, nor silently clobber custom rules

`mergeEntry` (import.go:108-123) overwrites ANY existing entry when `force` or the version is greater: a zip entry with a builtin's ID and `version: 99.0.0` shadows the builtin (local-wins lookup) so one "upgrade" click deploys imported SQL under the builtin's identity — the exact mutation `PUT /rules/catalog/:id` rejects with 409. A same-ID custom rule is silently replaced (origin flips to `imported`, locking it out of the editor), reported merely as "updated". New policy: builtins are never overwritten (even with force); custom rules require explicit `force`; imported→imported keeps the existing version/force policy.

**Files:**
- Modify: `backend/internal/catalog/import.go` (ImportResult, ImportZip doc + switch, mergeEntry)
- Modify: `backend/internal/handlers/catalog.go:307-309` (ImportCatalog doc comment)
- Modify: `backend/internal/handlers/docs.go` (`/rules/catalog/import` response schema, near line 832)
- Modify: `frontend/src/pages/RulePacks.jsx:300` (two new display lines)
- Test: `backend/internal/catalog/import_test.go`

**Interfaces:**
- Produces: `ImportResult` gains `SkippedBuiltin []string` (JSON `skipped_builtin`) and `SkippedCustom []string` (JSON `skipped_custom`). `mergeEntry` returns two new action strings: `"skipped_builtin"`, `"skipped_custom"`. `Manager.Lookup`, `OriginBuiltin`/`OriginCustom`/`OriginImported` (definition.go) are consumed as-is.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/catalog/import_test.go` (helpers `buildZip` at line 44 and `fstest.MapFS` are already in scope):

```go
// Import must never overwrite a builtin definition — not even with force. A
// local shadow would replace builtin detection SQL while keeping the builtin's
// id and name (the exact edit PUT /rules/catalog/:id rejects with 409), and
// the installed builtin would then show "upgradeable" with the imported SQL
// one upgrade click away from deployment.
func TestImportZip_NeverOverwritesBuiltin(t *testing.T) {
	seed := fstest.MapFS{
		"rp-b.yaml": &fstest.MapFile{Data: []byte("id: rp-b\nname: Builtin\nversion: 1.0.0\nsql_template: SELECT 1\n")},
	}
	m, err := New(seed, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	zipData := buildZip(t, map[string]string{
		"rp-b.yaml": "id: rp-b\nname: Shadow\nversion: 99.0.0\nsql_template: SELECT 'evil'\n",
	})
	for _, force := range []bool{false, true} {
		res, err := m.ImportBytes(zipData, force)
		if err != nil {
			t.Fatalf("ImportBytes(force=%v): %v", force, err)
		}
		if len(res.SkippedBuiltin) != 1 || res.SkippedBuiltin[0] != "rp-b" {
			t.Errorf("force=%v: SkippedBuiltin = %v, want [rp-b]", force, res.SkippedBuiltin)
		}
		if len(res.Updated) != 0 || len(res.Added) != 0 {
			t.Errorf("force=%v: builtin reported added/updated: %+v", force, res)
		}
		got, _ := m.Lookup("rp-b")
		if got.Origin != OriginBuiltin || got.Name != "Builtin" {
			t.Errorf("force=%v: builtin was shadowed: %+v", force, got)
		}
	}
}

// Import must not silently clobber a user's custom rule on a bare version
// comparison (that also flips origin to "imported", locking it out of the
// editor); force explicitly authorizes the overwrite.
func TestImportZip_CustomRuleRequiresForce(t *testing.T) {
	m, err := New(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	custom := Definition{ID: "rp-c", Name: "Mine", Version: "1.0.0",
		SQLTemplate: "SELECT 1", Origin: OriginCustom}
	if err := m.Save(custom); err != nil {
		t.Fatalf("Save: %v", err)
	}
	zipData := buildZip(t, map[string]string{
		"rp-c.yaml": "id: rp-c\nname: Theirs\nversion: 2.0.0\nsql_template: SELECT 2\n",
	})

	res, err := m.ImportBytes(zipData, false)
	if err != nil {
		t.Fatalf("ImportBytes: %v", err)
	}
	if len(res.SkippedCustom) != 1 || res.SkippedCustom[0] != "rp-c" {
		t.Errorf("SkippedCustom = %v, want [rp-c]", res.SkippedCustom)
	}
	if got, _ := m.Lookup("rp-c"); got.Name != "Mine" || got.Origin != OriginCustom {
		t.Errorf("custom rule clobbered without force: %+v", got)
	}

	res, err = m.ImportBytes(zipData, true)
	if err != nil {
		t.Fatalf("ImportBytes(force): %v", err)
	}
	if len(res.Updated) != 1 || res.Updated[0] != "rp-c" {
		t.Errorf("force: Updated = %v, want [rp-c]", res.Updated)
	}
	if got, _ := m.Lookup("rp-c"); got.Name != "Theirs" || got.Origin != OriginImported {
		t.Errorf("force overwrite did not apply: %+v", got)
	}
}

// The imported→imported merge policy is unchanged: newer version updates,
// equal-or-older skips, force overrides.
func TestImportZip_ImportedVersionPolicyUnchanged(t *testing.T) {
	m, err := New(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	v1 := buildZip(t, map[string]string{"r.yaml": "id: rp-i\nname: V1\nversion: 1.0.0\nsql_template: SELECT 1\n"})
	v2 := buildZip(t, map[string]string{"r.yaml": "id: rp-i\nname: V2\nversion: 2.0.0\nsql_template: SELECT 2\n"})

	if res, _ := m.ImportBytes(v1, false); len(res.Added) != 1 {
		t.Fatalf("initial import: %+v", res)
	}
	if res, _ := m.ImportBytes(v1, false); len(res.Skipped) != 1 {
		t.Errorf("same-version re-import should skip: %+v", res)
	}
	if res, _ := m.ImportBytes(v2, false); len(res.Updated) != 1 {
		t.Errorf("newer version should update: %+v", res)
	}
	if res, _ := m.ImportBytes(v1, true); len(res.Updated) != 1 {
		t.Errorf("force should downgrade an imported entry: %+v", res)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/ -run TestImportZip -v`
Expected: compile FAIL (`res.SkippedBuiltin` undefined). The two pre-existing TestImportZip_* tests must be re-run after the fix.

- [ ] **Step 3: Implement**

In `backend/internal/catalog/import.go`:

Replace `ImportResult` (lines 14-20):

```go
// ImportResult summarizes the outcome of a catalog import operation.
type ImportResult struct {
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
	Skipped []string `json:"skipped"`
	// SkippedBuiltin lists zip entries whose ID belongs to a builtin rule.
	// Builtins are never overwritten by import — not even with force: a local
	// shadow would replace builtin detection SQL under the builtin's identity.
	SkippedBuiltin []string `json:"skipped_builtin"`
	// SkippedCustom lists zip entries whose ID belongs to a user-authored
	// custom rule and force was not set; a version comparison must not
	// silently clobber user content.
	SkippedCustom []string `json:"skipped_custom"`
	Errors        []string `json:"errors"`
}
```

Update the `ImportZip` doc comment (lines 22-23):

```go
// ImportZip reads a zip archive and merges YAML rule files into the local catalog.
// Merge policy per existing entry origin: builtin — never overwritten (even with
// force); custom — overwritten only with force; imported — overwritten when the
// zip version is newer, or with force.
```

Extend the action switch (lines 86-93):

```go
		switch action {
		case "added":
			result.Added = append(result.Added, entry.ID)
		case "updated":
			result.Updated = append(result.Updated, entry.ID)
		case "skipped":
			result.Skipped = append(result.Skipped, entry.ID)
		case "skipped_builtin":
			result.SkippedBuiltin = append(result.SkippedBuiltin, entry.ID)
		case "skipped_custom":
			result.SkippedCustom = append(result.SkippedCustom, entry.ID)
		}
```

Replace `mergeEntry` (lines 103-123):

```go
// mergeEntry applies one definition to the local catalog. Returns the action:
//   - "added":           no existing entry
//   - "updated":         existing imported entry replaced (newer version or
//                        force), or existing custom entry replaced (force only)
//   - "skipped":         existing imported entry at an equal-or-newer version,
//                        without force
//   - "skipped_builtin": existing entry is builtin — never overwritten, even
//                        with force. A local shadow would silently replace
//                        builtin detection SQL under the builtin's identity
//                        (the exact edit PUT /rules/catalog/:id 409s).
//   - "skipped_custom":  existing entry is user-authored custom and force was
//                        not set — a version comparison must not silently
//                        clobber user content (and flip it to origin
//                        "imported", locking it out of the editor).
//
// A failed Save (IO/marshal error) is returned as an error so the caller
// reports it instead of mislabeling it as skipped.
func (m *Manager) mergeEntry(e Definition, force bool) (string, error) {
	existing, exists := m.Lookup(e.ID)
	if !exists {
		if err := m.Save(e); err != nil {
			return "", err
		}
		return "added", nil
	}
	if existing.Origin == OriginBuiltin {
		return "skipped_builtin", nil
	}
	if existing.Origin == OriginCustom && !force {
		return "skipped_custom", nil
	}
	if force || versionGreater(e.Version, existing.Version) {
		if err := m.Save(e); err != nil {
			return "", err
		}
		return "updated", nil
	}
	return "skipped", nil
}
```

In `backend/internal/handlers/catalog.go`, update the `ImportCatalog` doc comment (lines 307-309):

```go
// ImportCatalog handles POST /api/rules/catalog/import.
// Accepts a multipart form with a "file" field containing a zip archive of YAML
// rule files. Query param ?force=true overwrites existing imported/custom
// entries even when the imported version is not newer; builtin entries are
// never overwritten (reported as skipped_builtin).
```

In `backend/internal/handlers/docs.go`, make three edits.

(a) The `/rules/catalog/import` `description` (line ~836) — replace the merge-strategy paragraph. **The whole OpenAPI document is a Go raw string literal (backtick-delimited), so a literal backtick would terminate it. Every markdown code span inside these JSON strings MUST be written as the six-character escape ``` — exactly as the existing lines in this file already do. Do not "fix" them into real backticks.**

    "description": "Uploads a zip file containing YAML rule definition files and merges them into the local catalog directory.\n\nMerge strategy (per rule ID, by the EXISTING entry's origin):\n- **New ID** — always added.\n- **Builtin** — never overwritten, not even with `?force=true` (reported as `skipped_builtin`).\n- **Custom** — overwritten only with `?force=true` (otherwise `skipped_custom`).\n- **Imported** — overwritten when the imported version is newer, or with `?force=true`; otherwise skipped.\n\nNo rules are auto-installed after import — call `/rules/{id}/install` explicitly.",

(b) The `force` query-param description on the next line:

```
          { "name": "force", "in": "query", "description": "If true, overwrite existing imported/custom entries even when the imported version is not newer. Builtin entries are never overwritten.", "schema": { "type": "boolean", "default": false } }
```

(c) The `ImportResult` schema component (line 2007-2016) — add two properties and extend the 200 example (line ~857):

```
      "ImportResult": {
        "type": "object",
        "description": "Summary of a catalog import operation.",
        "properties": {
          "added":   { "type": "array", "items": { "type": "string" }, "description": "Rule IDs that were newly added to the catalog." },
          "updated": { "type": "array", "items": { "type": "string" }, "description": "Rule IDs whose catalog entry was overwritten (newer version or force=true)." },
          "skipped": { "type": "array", "items": { "type": "string" }, "description": "Rule IDs that were skipped because the imported version was not newer than the existing imported entry." },
          "skipped_builtin": { "type": "array", "items": { "type": "string" }, "description": "Rule IDs skipped because the existing entry is builtin. Import never overwrites a builtin, even with force=true — a local shadow would replace builtin detection SQL under the builtin's identity." },
          "skipped_custom":  { "type": "array", "items": { "type": "string" }, "description": "Rule IDs skipped because the existing entry is a user-authored custom rule and force was not set." },
          "errors":  { "type": "array", "items": { "type": "string" }, "description": "Parse or write errors encountered per file (non-fatal — other files are still processed)." }
        }
      },
```

and the response example:

```
            "example": { "added": ["custom-my-rule"], "updated": ["rp-imported-pack"], "skipped": ["rp-002"], "skipped_builtin": ["rp-001"], "skipped_custom": ["custom-mine"], "errors": [] }
```

In `frontend/src/pages/RulePacks.jsx`, after the `skipped` line (line 300), add:

```jsx
                {(importResult.skipped_builtin?.length > 0) && <p className="text-gray-400">Skipped (builtin, cannot be overwritten by import): <span className="font-mono">{importResult.skipped_builtin.join(', ')}</span></p>}
                {(importResult.skipped_custom?.length > 0) && <p className="text-gray-400">Skipped (existing custom rule kept): <span className="font-mono">{importResult.skipped_custom.join(', ')}</span></p>}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/ ./internal/handlers/ -v -run 'TestImportZip|TestVersionGreater'`
Expected: PASS (3 new + 3 pre-existing import tests). Then `cd ../frontend && npm run lint` — clean.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/catalog/import.go backend/internal/catalog/import_test.go backend/internal/handlers/catalog.go backend/internal/handlers/docs.go frontend/src/pages/RulePacks.jsx
git commit -m "fix(catalog): import can no longer overwrite builtins or silently clobber custom rules

mergeEntry overwrote any existing entry on force/newer-version: a zip entry
with a builtin's ID and version 99.0.0 shadowed the builtin (one upgrade click
from deploying imported SQL under the builtin's identity — the exact mutation
PUT /rules/catalog/:id rejects with 409), and a same-ID custom rule was
silently replaced with its origin flipped to imported. New policy: builtin —
never overwritten (even with force); custom — force only; imported — version/
force policy unchanged. Outcomes surface as skipped_builtin/skipped_custom in
the import result and the RulePacks import banner.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Preflight sink derived from the live security-events stream

`candidateSinkDDL` (timeplus/rules.go:126-128) builds the preflight sink from the compiled-in `ddlSecurityEvents` constant, while the live `agentguard_security_events` is only `execIfNotExists`-created (init.go:289) and never schema-validated. If the live schema drifted (stream created by a different release), a rule passes preflight against the constant, then `DROP … NO DELAY` succeeds and the real `CREATE` fails deterministically — and the startup/sync paths have no rollback, so `/api/rules/sync` turns a healthy MV into no MV on every run, contradicting the invariant documented at service.go:924-926. Fix: derive the sink DDL from the live stream via `SHOW CREATE`, so schema drift fails preflight *before* anything is dropped. `ddlSecurityEvents` remains the creation template for provisioning.

**Files:**
- Modify: `backend/internal/timeplus/rules.go:123-134, 166-193` (`candidateSinkDDL` → `liveSinkDDL`, `preflightCandidate`)
- Test: `backend/internal/timeplus/rules_test.go` (5 existing `TestPreflightCandidate_*` tests + 2 new)

**Interfaces:**
- Consumes: `ruleExecutor.Query` (rules.go:90-93), `fakeRuleExec` fake (rules_test.go:15-34).
- Produces: `liveSinkDDL(ex ruleExecutor, sink string) (string, error)` (package-private). `PreflightCandidate`'s exported signature is unchanged. `candidateSinkDDL` is deleted.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/timeplus/rules_test.go`:

```go
// The preflight sink must mirror the LIVE agentguard_security_events stream,
// not this binary's compiled-in DDL constant: if the live schema drifted, a
// candidate that passes against the constant would deterministically fail the
// real CREATE *after* the previous MV was dropped — and the startup/sync path
// has no rollback. Deriving the sink from SHOW CREATE moves that failure to
// preflight, before anything is dropped.
func TestPreflightCandidate_SinkDerivedFromLiveDDL(t *testing.T) {
	ex := &fakeRuleExec{rows: []Row{{
		"statement": "CREATE STREAM default.agentguard_security_events (id string, drifted_col uint32)",
	}}}
	if err := preflightCandidate(ex, "rp-001", "SELECT 1", "tok"); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if len(ex.queries) != 1 || !strings.Contains(ex.queries[0], "SHOW CREATE") {
		t.Fatalf("expected one SHOW CREATE query, got %v", ex.queries)
	}
	// The sink create must carry the LIVE schema and the throwaway name.
	wantContains(t, ex.execs[0], "drifted_col uint32")
	wantContains(t, ex.execs[0], "ag_preflight_sink_rp001_tok")
	wantNotContains(t, ex.execs[0], "agentguard_security_events")
}

// If the live DDL cannot be read (stream missing / Timeplus error), preflight
// must fail before creating anything — a preflight that guessed the schema
// could pass and let the replace path drop a healthy MV.
func TestPreflightCandidate_LiveDDLUnavailableFailsBeforeCreate(t *testing.T) {
	ex := &fakeRuleExec{queryErr: errors.New("stream not found")}
	err := preflightCandidate(ex, "rp-001", "SELECT 1", "tok")
	if err == nil {
		t.Fatal("expected error when live DDL is unavailable")
	}
	if len(ex.execs) != 0 {
		t.Errorf("no DDL may execute when the live schema is unreadable, got %v", ex.execs)
	}
}
```

Add `"errors"` to the test file imports if absent.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/ -run TestPreflightCandidate -v`
Expected: the two new tests FAIL (no SHOW CREATE query is issued; sink uses the constant). The 5 pre-existing preflight tests still pass.

- [ ] **Step 3: Implement**

In `backend/internal/timeplus/rules.go`, replace `candidateSinkDDL` (lines 123-128) with:

```go
// liveSinkDDL derives the disposable validation sink's DDL from the LIVE
// agentguard_security_events stream (SHOW CREATE), substituting the throwaway
// name. Preflight must mirror the stream the real MV will write into — not
// this binary's compiled-in ddlSecurityEvents constant: if the live schema has
// drifted (e.g. the stream was created by an older release), a candidate that
// validates against the constant would deterministically fail the real CREATE
// *after* the previous MV was dropped, which the startup/sync path cannot roll
// back (service.go's "deterministic create failure cannot turn a healthy MV
// into no MV" invariant). ddlSecurityEvents remains the creation template used
// by provisioning.
func liveSinkDDL(ex ruleExecutor, sink string) (string, error) {
	rows, err := ex.Query("SHOW CREATE STREAM agentguard_security_events")
	if err != nil {
		return "", fmt.Errorf("preflight read live agentguard_security_events DDL: %w", err)
	}
	var stmt string
	if len(rows) > 0 {
		stmt, _ = rows[0]["statement"].(string)
		if stmt == "" {
			// Column name differs across engine versions; take any string value
			// that looks like a CREATE statement.
			for _, v := range rows[0] {
				if s, ok := v.(string); ok && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s)), "CREATE") {
					stmt = s
					break
				}
			}
		}
	}
	if stmt == "" {
		return "", fmt.Errorf("preflight: agentguard_security_events DDL unavailable (stream missing? run provisioning via POST /api/setup/provision)")
	}
	return strings.Replace(stmt, "agentguard_security_events", sink, 1), nil
}
```

Update `preflightCandidate` (lines 166-193) to fetch the live DDL BEFORE installing the cleanup defer (nothing to clean up if the read fails), and use it for the sink create:

```go
func preflightCandidate(ex ruleExecutor, ruleID, renderedSQL, token string) (err error) {
	sink, mv := candidateNames(ruleID, token)
	sinkDDL, err := liveSinkDDL(ex, sink)
	if err != nil {
		return err
	}
	// Always tear both down, NO DELAY, regardless of outcome. A cleanup failure
	// is logged and only surfaced when validation itself succeeded — it must not
	// mask the validation error that is the caller's real signal.
	defer func() {
		if dropErr := ex.Exec(dropViewNoDelaySQL(mv)); dropErr != nil {
			log.Printf("rules: preflight cleanup drop MV %s: %v", mv, dropErr)
			if err == nil {
				err = fmt.Errorf("preflight cleanup drop MV %s: %w", mv, dropErr)
			}
		}
		if dropErr := ex.Exec(dropStreamNoDelaySQL(sink)); dropErr != nil {
			log.Printf("rules: preflight cleanup drop sink %s: %v", sink, dropErr)
			if err == nil {
				err = fmt.Errorf("preflight cleanup drop sink %s: %w", sink, dropErr)
			}
		}
	}()

	if cErr := ex.Exec(sinkDDL); cErr != nil {
		return fmt.Errorf("preflight create sink %s: %w", sink, cErr)
	}
	if cErr := ex.Exec(candidateMVDDL(mv, sink, renderedSQL)); cErr != nil {
		return fmt.Errorf("preflight validate rule SQL: %w", cErr)
	}
	return nil
}
```

Update the 5 pre-existing `TestPreflightCandidate_*` tests (rules_test.go:94-177): each `fakeRuleExec` they construct must now provide a SHOW CREATE result, e.g. add to each fake's literal:

```go
	rows: []Row{{"statement": "CREATE STREAM agentguard_security_events (rule_id string)"}},
```

and where a test asserts the sink-create SQL contains text from `ddlSecurityEvents`, assert against the fake statement's columns (`rule_id string`) instead. `TestPreflightCandidate_SinkCreateFailsStillCleansUp` and the cleanup-failure tests keep their `execErr` logic unchanged — only the added `rows` field changes. NOTE: `fakeRuleExec.queryErr` also affects the `mvExists` tests — do not share fakes across tests.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/timeplus/ ./internal/rules/ -v -run 'TestPreflight|TestService'`
Expected: PASS — including `internal/rules` service tests (its `mv` fake implements PreflightCandidate independently, so it must be unaffected; if any service test fails, stop and reassess).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/timeplus/rules.go backend/internal/timeplus/rules_test.go
git commit -m "fix(timeplus): derive preflight sink from live security-events DDL

The preflight sink was built from the compiled-in ddlSecurityEvents constant,
so live-schema drift (a stream created by a different release) passed preflight
and then deterministically failed the real CREATE after DROP NO DELAY — and the
startup/sync path has no rollback, so every /api/rules/sync turned the healthy
MV into no MV. SHOW CREATE on the live stream moves that failure to preflight,
before anything is dropped.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: Verification — full suites + live integration against proton-enterprise timeplusd

**Files:** none (verification only).

- [ ] **Step 1: Full unit suites + lint**

```bash
cd /home/haohang/AgentGuard/backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...
cd /home/haohang/AgentGuard/frontend && npm run lint && npm test
cd /home/haohang/AgentGuard && make lint
```
Expected: all PASS/clean. (If `make lint` needs missing tooling, run `golangci-lint run` in backend/ and note any pre-existing failures — do not fix unrelated lint.)

- [ ] **Step 2: Start timeplusd from the proton-enterprise build**

```bash
~/sub/proton-enterprise/build/programs/timeplusd server --version   # record the version
mkdir -p /tmp/claude-1000/-home-haohang-AgentGuard/9b76bfd3-e446-431b-b64c-da44546c58fe/scratchpad/tpd-data
cd /tmp/claude-1000/-home-haohang-AgentGuard/9b76bfd3-e446-431b-b64c-da44546c58fe/scratchpad/tpd-data && ~/sub/proton-enterprise/build/programs/timeplusd server
```
(run in background; timeplusd defaults its data path to the cwd. If it demands a config file, copy the repo's docker-compose Timeplus config or use `timeplusd server --config-file=...` from `~/sub/proton-enterprise/` — inspect `~/sub/proton-enterprise/BUILD.md` for the dev-run recipe.)
Wait for readiness: `curl -sf http://localhost:8123/proton/ping` (or port 3218 `/timeplusd/ping` depending on version).

- [ ] **Step 3: Run the integration suite against it (CI recipe, ci.yml:96-145)**

```bash
cd /home/haohang/AgentGuard/backend && CGO_ENABLED=0 go build -o ../agentguard-server cmd/server/main.go
cd /home/haohang/AgentGuard && ./agentguard-server --timeplus-host localhost --timeplus-port 3218 --timeplus-native-port 8463 &
# wait: curl -sf http://localhost:8080/api/auth/status
cd backend && AGENTGUARD_URL=http://localhost:8080 TIMEPLUS_HOST=localhost TIMEPLUS_PORT=3218 TIMEPLUS_NATIVE_PORT=8463 TIMEPLUS_USER=default TIMEPLUS_PASSWORD='' \
  CGO_ENABLED=0 go test -tags integration -v -timeout 120s ./internal/handlers/...
```
Expected: PASS. Known risk (memory: local timeplusd builds have shown API drift vs 3.2.11 — missing `arg_max`, PYTHON UDF syntax, tzdata): if provisioning fails on engine drift, capture the exact errors and REPORT them to the user as engine findings; do not paper over them.

- [ ] **Step 4: CI-parity gate against timeplusd 3.2.11 (required regardless of Step 3's outcome)**

```bash
docker run -d --name timeplusd-ci -p 13218:3218 -p 18463:8463 --entrypoint /bin/bash timeplus/timeplusd:3.2.11 \
  -c "sed -i 's/preallocate: true/preallocate: false/g' /etc/timeplusd-server/config.yaml && /entrypoint.sh"
# wait for readiness, then rerun the server + integration suite with TIMEPLUS_PORT=13218 TIMEPLUS_NATIVE_PORT=18463
```
Expected: PASS (this is the engine CI runs; per project practice the suite must pass here before pushing).

- [ ] **Step 5: Manual end-to-end exercise of each fix (against whichever engine is up)**

1. Provision via `POST /api/setup/provision`, install a parameterized rule (e.g. rp-003).
2. `PUT /api/rules/rp-003/configuration` with body `{"overides":{}}` → expect **400** (Task 2); with valid `{"overrides":{...}}` → 200.
3. `POST /api/alerts/webhook` with `{"rule_id":"\\' OR 1=1 --","agent_id":"\\' OR 1=1 --","severity":"low","message":"x"}` → expect 200 `{"received":true}` and NO Timeplus syntax error in server logs (Task 1).
4. Export the catalog, craft a zip containing a builtin ID at version 99.0.0, import with and without `?force=true` → expect `skipped_builtin` (Task 5).
5. Install → toggle a rule to confirm preflight works live (SHOW CREATE path, Task 6); check server logs for the preflight queries succeeding.
6. `POST /api/rules/sync` twice → 200, `results` stable (Tasks 3/6 smoke).

- [ ] **Step 6: Cleanup**

```bash
docker rm -f timeplusd-ci; kill %1 2>/dev/null  # stop agentguard-server and local timeplusd
```
No commit for this task.

---

### Task 8: Push branch and open the PR

**Files:** none.

- [ ] **Step 1: Push**

```bash
cd /home/haohang/AgentGuard && git push -u origin fix/pr182-review-followups
```

- [ ] **Step 2: Open the PR**

```bash
gh pr create --title "fix(rules): post-merge review follow-ups for editable rule parameters (#182)" --body "$(cat <<'EOF'
Fixes the six backend defects found in the post-merge review of #182.

| Fix | Defect |
|---|---|
| webhook escaping | `isThreatAlreadyOpen` used quote-only `escapeSQL` on unauthenticated webhook fields; Proton reads `\'` as an escaped quote, so a trailing backslash broke out of the literal (blind read-only subqueries). Now uses `escapeSQLStr`, pinned by test. |
| strict mutation decode | A typo'd body key on install/configure decoded to nil overrides = documented reset-all-to-defaults; now 400 via `DisallowUnknownFields`. |
| SyncMVs probe credentials | `ProbeEmbeddingDim` ran on the base config instead of the session-credentialed `reqCfg`, silently breaking embedding-dim auto-detection on session-auth deployments. |
| atomic catalog writes | `saveLocked` used truncate-then-write; a crash mid-write vanished the definition on reload (loadDir skips unparseable files) and broke the documented `UpdateDefinition` rollback contract. Now temp file + fsync + rename. |
| import origin guard | Import could shadow a builtin's SQL under the builtin's identity (then one upgrade click deploys it) and silently clobber custom rules. New policy: builtin — never; custom — force only; imported — unchanged. Surfaced as `skipped_builtin`/`skipped_custom`. |
| preflight fidelity | The preflight sink came from the compiled-in DDL constant; live-schema drift passed preflight then deterministically failed the real CREATE after `DROP NO DELAY` (no rollback on the sync path). The sink is now derived from `SHOW CREATE` on the live stream. |

Verification: full unit suites; integration suite (`-tags integration`) against timeplusd 3.2.11 (CI parity) and against the proton-enterprise build; manual end-to-end exercise of each fix (see plan `docs/superpowers/plans/2026-07-08-pr182-review-fixes.md`).

Deferred (tracked separately, out of scope here): ambiguous-append verify-read (service.go), ownership-check cache overlay, cache invalidation on reprovision, corrupt-row fail-open in `ListCurrent`, ConfigurationTab unsaved-edit loss, request-size caps, preflight-leak sweep.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 2b: Include the plan file in the branch**

```bash
git add docs/superpowers/plans/2026-07-08-pr182-review-fixes.md
git commit -m "docs: implementation plan for #182 review follow-ups

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
git push
```
(Do this before `gh pr create` if preferred; order is not load-bearing.)

- [ ] **Step 3: Report the PR URL to the user**, plus the Step-3/Step-4 integration outcomes (especially any engine-drift findings from the proton-enterprise build).
