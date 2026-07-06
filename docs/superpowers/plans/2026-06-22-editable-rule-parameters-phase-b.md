# Editable Rule Parameters — Phase B Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the deferred Phase B surfaces on top of the landed Phase A foundation: rename the external authoring contract to the Timeplus-Apps (`/apps`) manifest surface, harden global-UDF ownership, add `regex`/`duration` parameter types, and add clone-to-custom + catalog CRUD/compile APIs + the custom-definition authoring UI.

**Architecture:** The catalog (`internal/catalog`) owns versioned `Definition`s; `internal/rules` owns the strict parameter compiler (validate/render/compatibility), the revision-ordered snapshot `Store`, and the per-rule-serialized lifecycle `Service`. Phase B keeps that architecture intact. The `/apps` alignment changes only the **external surface** (YAML/JSON field names + placeholder spelling), not the strict, injection-safe compiler engine or AG's richer type vocabulary. New authoring endpoints reuse the existing lifecycle service seams (candidate preflight, drop/create MV replacement, snapshot append, rollback).

**Tech Stack:** Go 1.x (Gin, Cobra/Viper, Proton HTTP), React + Vite + Tailwind ("Digital Sentry" dark theme), Timeplus Enterprise / Proton streaming SQL.

## Global Constraints

- **Backend build/test:** always `CGO_ENABLED=0`. Build: `cd backend && CGO_ENABLED=0 go build ./...`. Test: `cd backend && CGO_ENABLED=0 go test ./...`. Vet: `CGO_ENABLED=0 go vet ./...`.
- **Frontend:** `cd frontend && npm run build`, `npm run lint`, `npm test`.
- **No backward compatibility for `agentguard_rules`** beyond Phase A's already-broken baseline. The `/apps` rename does **not** change the `agentguard_rules` stream schema (overrides are a JSON blob keyed by parameter key) — so it needs **no** new reprovision. Do not add `ALTER STREAM`, dual writes, compatibility readers, or legacy aliases.
- **Strict compiler is non-negotiable.** The renderer escapes quotes/backslashes only and substitutes exact placeholders. It MUST NOT adopt Go `text/template` execution, Sprig functions, conditionals, or any namespace other than `.Config.`. No parameter type may inject identifiers, operators, clauses, functions, or arbitrary SQL.
- **UDF names are global** (`CREATE OR REPLACE FUNCTION`). Definitions use rule-scoped names; ownership must be validated against **both** catalog definitions **and** installed snapshots' `deployed_udfs_json` before any deploy.
- **MV replacement is application-transactional:** validate first (candidate preflight), replace second (`DROP VIEW … NO DELAY` then `CREATE …` without `IF NOT EXISTS`), persist last (append complete snapshot), restore previous MV from the snapshot's last-known-good `rendered_sql`/`deployed_udfs_json` on any failure.
- **Per-rule serialization:** every rule mutation acquires the per-`rule_id` `keyedMutex` before compile/preflight/MV/snapshot.
- **Commits are thematic** (one per phase: B-align, B0, B1, B2, B3, B4), squashed only after approval. End each commit message with the repo's standard `Co-Authored-By` trailer.
- **Parameters are never a secret store.** A `secret: true` config field is rejected at validation time.

## `/apps` Alignment Decision (authoritative for this branch)

The external authoring contract adopts the `/apps` manifest surface; the internal strict engine and AG's type vocabulary are retained. Concretely:

| Concern | Phase A (before) | Phase B (after, `/apps`-aligned) |
|---|---|---|
| Parameter list YAML/JSON key | `parameters:` | `config:` |
| Parameter identifier field | `id` | `key` |
| Placeholder spelling | `{{params.<id>}}` (no whitespace) | `{{ .Config.<key> }}` (optional surrounding whitespace) |
| New parameter fields | — | `required` (bool, default true), `secret` (bool; `true` is rejected) |
| Template engine | strict scanner (kept) | strict scanner (kept — NOT Go text/template) |
| Type tokens | `integer/number/boolean/string/enum/string_list` | same + `regex`/`duration` (kept; `/apps` `float/bool/choice/list/multi_choice` documented as a mapping only) |
| DB templating (`{{ .DB }}`) | n/a | not supported (single fixed deployment; document the divergence) |

**Type-name mapping (documentation only — AG tokens remain canonical):**

| AG token | `/apps` token |
|---|---|
| `integer` | `integer` |
| `number` | `float` |
| `boolean` | `bool` |
| `string` | `string` |
| `enum` | `choice` |
| `string_list` | `multi_choice` / `list` |
| `regex` | (none in `/apps`) |
| `duration` | (none in `/apps`) |

Internal Go identifiers: rename `catalog.Parameter.ID` → `catalog.Parameter.Key`. Keep helper names `ValidParameterID`/`ParameterIDPattern`/`paramIDRe` (they validate the identifier syntax, which is unchanged) and the Go field `Definition.Parameters` (only its struct tag changes to `config`). Document the "id == key" equivalence in a code comment.

---

## File Structure

**Modified (backend):**
- `backend/internal/catalog/definition.go` — `Parameter.Key` rename, `Required`/`Secret` fields, `config` tag, exported `ExtractUDFNames`, `Secret` rejection in `Validate`, `RescopeUDFs` helper.
- `backend/internal/catalog/catalog.go` — `Clone`, `IsCustom`, expose `Save` through the service seam.
- `backend/internal/rules/render.go` — `.Config.` scanner + whitespace tolerance, regex/duration render cases, error-message spelling.
- `backend/internal/rules/validate.go` — regex/duration normalize cases.
- `backend/internal/rules/compatibility.go` — regex/duration are ordinary typed params (tests only; logic already type-generic).
- `backend/internal/rules/service.go` — store-aware `checkUDFOwnership`, ApplyUDFs-guarantee ordering assertions, `UpdateDefinition`, extend `defStore` seam with `Save`.
- `backend/internal/handlers/rules.go` / `catalog.go` — `GetCatalogRule`, `CompileCatalogRule`, `CreateCatalogRule`, `UpdateCatalogRule`, `CloneCatalogRule`; extend `respondRuleError`.
- `backend/cmd/server/main.go` — register the 5 new catalog routes.
- `backend/internal/handlers/docs.go` — OpenAPI entries for new routes.
- `backend/internal/catalog/seed/*.yaml` (125 files) — mechanical `parameters:`→`config:`, `id:`→`key:`, `{{params.X}}`→`{{ .Config.X }}` rename; representative `duration`/`regex` parameterization.

**Modified (frontend):**
- `frontend/src/components/rules/ParamField.jsx` — `regex` + `duration` controls; `p.id`→`p.key`.
- `frontend/src/components/rules/ConfigurationTab.jsx` — `p.id`→`p.key` keying.
- `frontend/src/pages/RuleDetail.jsx` — "Clone as custom" action; "Edit definition" for custom rules.
- `frontend/src/pages/RuleCreate.jsx` — refactor into the custom-Definition authoring editor.
- `frontend/src/api/client.js` — `getCatalogRule`, `compileCatalogRule`, `createCatalogRule`, `updateCatalogRule`, `cloneCatalogRule`.

**Created:**
- `frontend/src/components/rules/ParamSchemaEditor.jsx` — edits the parameter **schema** (key/label/type/default/constraints), distinct from `ParamField` (edits a value).
- `docs/superpowers/specs/2026-06-22-apps-alignment-mapping.md` — the `/apps` field-mapping decision doc (PR section source).
- Test files alongside each changed Go package; frontend tests under existing test setup.

---

# Commit B-align: `/apps` surface rename + mapping doc

> Land the external rename first so B1 extends a stable `/apps`-shaped compiler. This commit is mechanical + high-coverage; no new capability.

### Task 1: Write the `/apps` alignment mapping doc

**Files:**
- Create: `docs/superpowers/specs/2026-06-22-apps-alignment-mapping.md`

- [ ] **Step 1: Write the doc** — capture the "/apps Alignment Decision" table above (surface mapping + type-name mapping), the strict-engine rationale (no `text/template`), the `secret`-rejected rule, and the "no `{{ .DB }}`" divergence. This file is the source for the PR's alignment section.

- [ ] **Step 2: Commit**
```bash
git add docs/superpowers/specs/2026-06-22-apps-alignment-mapping.md
git commit -m "docs(rules): /apps authoring-surface alignment mapping (#33, Phase B)"
```

### Task 2: Rename the `Parameter` identifier field and add `required`/`secret`

**Files:**
- Modify: `backend/internal/catalog/definition.go:46-61` (`Parameter` struct), `:152-165` (`hashPayload`), `:184-214` (`Hash`), `:254-278` (`Validate` loop)
- Test: `backend/internal/catalog/definition_test.go`

**Interfaces:**
- Produces: `catalog.Parameter.Key string` (yaml/json `key`); `catalog.Parameter.Required bool` (yaml/json `required,omitempty`); `catalog.Parameter.Secret bool` (yaml/json `secret,omitempty`); `catalog.Definition.Parameters` now tagged `yaml:"config" json:"config,omitempty"`.

- [ ] **Step 1: Write the failing test**
```go
func TestParameter_KeyAndSecret(t *testing.T) {
	y := []byte("id: r\nname: n\nversion: \"1\"\nsql_template: \"x {{ .Config.k }}\"\nconfig:\n  - key: k\n    type: integer\n    default: 1\n")
	var d catalog.Definition
	if err := yaml.Unmarshal(y, &d); err != nil { t.Fatal(err) }
	if len(d.Parameters) != 1 || d.Parameters[0].Key != "k" {
		t.Fatalf("expected config[0].key=k, got %+v", d.Parameters)
	}
	// secret:true must be rejected by Validate
	d.Parameters[0].Secret = true
	if err := d.Validate(); err == nil {
		t.Fatal("expected secret:true to be rejected")
	}
}
```

- [ ] **Step 2: Run to verify it fails** — `cd backend && CGO_ENABLED=0 go test ./internal/catalog/ -run TestParameter_KeyAndSecret -v` → FAIL (field `Key` undefined).

- [ ] **Step 3: Implement** — In `definition.go`:
  - `Parameter`: rename `ID string \`yaml:"id" json:"id"\`` → `Key string \`yaml:"key" json:"key"\``; add `Required bool \`yaml:"required,omitempty" json:"required,omitempty"\``, `Secret bool \`yaml:"secret,omitempty" json:"secret,omitempty"\``.
  - `Definition.Parameters` tag → `yaml:"config" json:"config,omitempty"`.
  - `hashPayload.Parameters` and `Hash` are unchanged structurally (they marshal whole `Parameter` values; new fields hash deterministically — acceptable, defaults are `false`/empty and omit).
  - In `Validate`, replace every `p.ID` with `p.Key` and add, inside the parameter loop after the type check:
    ```go
    if p.Secret {
        return fmt.Errorf("config[%d] (%s): secret parameters are not supported — parameters are rendered into SQL and must never store secrets", i, p.Key)
    }
    ```
  - Update all loop error strings from `parameters[%d]` to `config[%d]` and `id %q` to `key %q`.

- [ ] **Step 4: Run to verify it passes** — same command → PASS. (Other catalog tests will fail to compile; fixed in Task 3.)

- [ ] **Step 5: Defer commit** to Task 4 (rename is one reviewable unit).

### Task 3: Propagate `Key` through the compiler and update the placeholder scanner

**Files:**
- Modify: `backend/internal/rules/render.go:12-56` (scanner + prefix), `:50-51,81,190` (error spelling), `:110-171` (renderValue messages keep `p.ID`→`p.Key`)
- Modify: `backend/internal/rules/validate.go:17-23` (`paramIndex`), `:311-326,364-385` (override/default keying)
- Modify: `backend/internal/rules/compatibility.go` (every `p.ID` → `p.Key`)
- Test: `backend/internal/rules/render_test.go`

**Interfaces:**
- Produces: scanner accepts `{{` `\s*` `.Config.` `<key>` `\s*` `}}`; `placeholderPrefix = ".Config."`.

- [ ] **Step 1: Write the failing test**
```go
func TestScanPlaceholders_ConfigSpelling(t *testing.T) {
	def := catalog.Definition{
		ID: "r", Name: "n", Version: "1",
		SQLTemplate: "a = {{ .Config.threshold }} OR b = {{.Config.threshold}}",
		Parameters:  []catalog.Parameter{{Key: "threshold", Type: "integer", Default: 5}},
	}
	if err := ValidateTemplate(def); err != nil {
		t.Fatalf("expected valid template, got %v", err)
	}
	// legacy {{params.x}} spelling must now be rejected
	def.SQLTemplate = "a = {{params.threshold}}"
	if err := ValidateTemplate(def); err == nil {
		t.Fatal("expected legacy {{params.x}} to be rejected")
	}
}
```

- [ ] **Step 2: Run to verify it fails** — `cd backend && CGO_ENABLED=0 go test ./internal/rules/ -run TestScanPlaceholders_ConfigSpelling -v` → FAIL.

- [ ] **Step 3: Implement** — In `render.go`, replace `scanPlaceholders` so each `{{ … }}` inner string is whitespace-trimmed before the prefix check:
```go
const placeholderPrefix = ".Config."

func scanPlaceholders(template string) ([]placeholder, error) {
	var out []placeholder
	for i := 0; ; {
		j := strings.Index(template[i:], "{{")
		if j < 0 {
			return out, nil
		}
		start := i + j
		rest := template[start+2:]
		k := strings.Index(rest, "}}")
		if k < 0 {
			return nil, fmt.Errorf("sql_template: unclosed '{{' at byte offset %d", start)
		}
		inner := strings.TrimSpace(rest[:k])
		id, ok := strings.CutPrefix(inner, placeholderPrefix)
		if !ok || !catalog.ValidParameterID(id) {
			return nil, fmt.Errorf("sql_template: invalid placeholder %q at byte offset %d (expected {{ .Config.<key> }})", "{{"+rest[:k]+"}}", start)
		}
		out = append(out, placeholder{Start: start, End: start + 2 + k + 2, ID: id})
		i = start + 2 + k + 2
	}
}
```
  - Update error strings in `validateTemplate`/`renderTemplate` from `{{params.%s}}` to `{{ .Config.%s }}`.
  - In `render.go`/`validate.go`/`compatibility.go`, replace every `p.ID`/`.ID` reference on a `catalog.Parameter` with `.Key` (compile errors guide you). `paramIndex` now keys by `p.Key`.

- [ ] **Step 4: Run to verify it passes** — `cd backend && CGO_ENABLED=0 go test ./internal/rules/ -run TestScanPlaceholders_ConfigSpelling -v` → PASS.

- [ ] **Step 5: Defer commit** to Task 4.

### Task 4: Mechanically migrate the 125 seed YAMLs + fix all remaining tests, then commit

**Files:**
- Modify: `backend/internal/catalog/seed/*.yaml`
- Modify: any `*_test.go` constructing `catalog.Parameter{ID: …}` or templates with `{{params.…}}`

- [ ] **Step 1: Rewrite seed YAMLs** — for every file under `backend/internal/catalog/seed/` that has a `parameters:` block: rename the block key `parameters:` → `config:`, each item `id:` → `key:`, and in `sql_template` rewrite `{{params.<x>}}` → `{{ .Config.<x> }}`. Use a scripted pass, then spot-check 3 files by eye. (Most seeds have no parameters; only the Phase A parameterized ones — e.g. `rp-003`, the metrics rule — change.)

- [ ] **Step 2: Fix Go test fixtures** — grep and fix: `cd backend && grep -rln 'params\.' internal/ ; grep -rln 'ID:' internal/catalog internal/rules | xargs grep -l 'Parameter{'`. Replace `catalog.Parameter{ID:` → `{Key:` and `{{params.x}}` → `{{ .Config.x }}` in test literals.

- [ ] **Step 3: Regenerate the pinned definition hash (deliberate).** `TestDefinitionHash_Golden` (`internal/catalog/definition_test.go:316`) pins an exact digest for a *parameterized* definition; renaming the `Parameter` json tag `id`→`key` changes the canonical serialization, so the digest changes. This is an intentional hash-function change. Because the `agentguard_rules` snapshot stream is clean-reprovisioned at this pre-release baseline (no persisted `definition_hash` values to migrate), the required "migration story" is simply "clean reprovision" — already the baseline. Update the test fixture to use `Key:` and replace the `want` constant with the new `d.Hash()` output (run the test once, copy the "got" value), and update the test's doc comment to record that the `/apps` rename deliberately rotated the hash. Note: the new `Required`/`Secret` fields are `omitempty`, so zero-value params hash identically apart from the `id`→`key` key name.

- [ ] **Step 4: Run the full backend suite** — `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...` → all PASS. The seed-golden test (`internal/rules/seed_golden_test.go`) must still prove default-rendered SQL equals the pre-parameterization SQL after whitespace normalization.

- [ ] **Step 5: Commit**
```bash
git add backend/internal/catalog backend/internal/rules
git commit -m "feat(rules): adopt /apps authoring surface — config:/key/{{ .Config.key }} (#33, Phase B)"
```

### Task 5: Update the rules frontend to the `key` field

**Files:**
- Modify: `frontend/src/components/rules/ParamField.jsx` (props comment + any `param.id`), `ConfigurationTab.jsx:155-162` (`buildInitial`), `:60-65` (`resetAll`)
- Test: existing frontend tests

- [ ] **Step 1: Implement** — In `ConfigurationTab.jsx`, replace `p.id` with `p.key` in `buildInitial` (`effective?.[p.key] ?? defaults?.[p.key]`, `out[p.key]`), `resetAll` (`next[p.key] = defaults[p.key]`), and `setOne`/`resetOne` callers. In `ParamField.jsx`, the destructured `param` now carries `key`; update the props doc comment and any label fallback from `param.id` to `param.key`.

- [ ] **Step 2: Verify** — `cd frontend && npm run lint && npm run build && npm test` → PASS.

- [ ] **Step 3: Amend into the rename commit**
```bash
git add frontend/src/components/rules
git commit --amend --no-edit
```

---

# Commit B0: store-aware UDF ownership + ApplyUDFs guarantee

### Task 6: Export a UDF-name extractor from the catalog

**Files:**
- Modify: `backend/internal/catalog/definition.go:124-146` (`UDFNames`)
- Test: `backend/internal/catalog/definition_test.go`

**Interfaces:**
- Produces: `func ExtractUDFNames(ddls []string) ([]string, error)` (package-level); `Definition.UDFNames()` delegates to it.

- [ ] **Step 1: Write the failing test**
```go
func TestExtractUDFNames(t *testing.T) {
	names, err := catalog.ExtractUDFNames([]string{
		"CREATE OR REPLACE FUNCTION ag_r_score(x string) RETURNS float LANGUAGE PYTHON AS $$...$$;",
	})
	if err != nil { t.Fatal(err) }
	if len(names) != 1 || names[0] != "ag_r_score" {
		t.Fatalf("got %v", names)
	}
}
```

- [ ] **Step 2: Run to verify it fails** — `cd backend && CGO_ENABLED=0 go test ./internal/catalog/ -run TestExtractUDFNames -v` → FAIL (undefined).

- [ ] **Step 3: Implement** — Add package func `ExtractUDFNames(ddls []string) ([]string, error)` containing the current body of `UDFNames` (the `udfNameRe`/`stripSQLComments`/reserved-word loop). Reduce `Definition.UDFNames()` to `return ExtractUDFNames(d.UDFs)`.

- [ ] **Step 4: Run to verify it passes** → PASS.

- [ ] **Step 5: Defer commit** to Task 8.

### Task 7: Make UDF-ownership validation consult installed snapshots

**Files:**
- Modify: `backend/internal/rules/service.go:82-86` (`defStore`), `:354-375` (Install), `:562-585` (Upgrade), add `checkUDFOwnership`
- Test: `backend/internal/rules/service_test.go`

**Interfaces:**
- Consumes: `catalog.ExtractUDFNames` (Task 6); `Service.List()` / `Store.ListCurrent()`.
- Produces: `func (s *Service) checkUDFOwnership(def catalog.Definition) error` — rejects when any of `def`'s UDF names collides with a UDF name in a **different**, non-deleted installed instance's `DeployedUDFs`, after the existing catalog-level `ValidateUDFOwnership`.

- [ ] **Step 1: Write the failing test** — install rule A whose definition declares UDF `ag_shared_fn`; then attempt to install a *different* rule B (catalog def changed so its UDF is also `ag_shared_fn`) and assert the second install is rejected with a UDF-ownership error, even though the catalog no longer shows A's collision (simulate by using the fake store seeded with A's snapshot `DeployedUDFs: ["CREATE OR REPLACE FUNCTION ag_shared_fn() ..."]`). Use the existing `service_test.go` fakes.
```go
func TestInstall_RejectsUDFOwnedByInstalledSnapshot(t *testing.T) {
	svc, fakes := newTestService(t) // existing helper
	fakes.seedInstalled(RuleInstance{RuleID: "rp-a", Enabled: true, DeployedUDFs: []string{
		"CREATE OR REPLACE FUNCTION ag_shared_fn(x string) RETURNS string LANGUAGE PYTHON AS $$...$$;"}})
	fakes.catalog["rp-b"] = catalog.Definition{ID: "rp-b", Name: "B", Version: "1",
		SQLTemplate: "select 1 -- x", UDFs: []string{
			"CREATE OR REPLACE FUNCTION ag_shared_fn(x string) RETURNS string LANGUAGE PYTHON AS $$...$$;"}}
	_, err := svc.Install("rp-b", nil, "tester", "t")
	if err == nil || !strings.Contains(err.Error(), "ag_shared_fn") {
		t.Fatalf("expected UDF-ownership rejection, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails** — `cd backend && CGO_ENABLED=0 go test ./internal/rules/ -run TestInstall_RejectsUDFOwnedByInstalledSnapshot -v` → FAIL (install succeeds).

- [ ] **Step 3: Implement**
```go
// checkUDFOwnership rejects def if any of its UDF function names is already
// deployed by a different, non-deleted installed instance. Global CREATE OR
// REPLACE FUNCTION would otherwise overwrite a UDF a live MV depends on.
// This consults the real ownership ledger (installed snapshots'
// DeployedUDFs), not just catalog definitions.
func (s *Service) checkUDFOwnership(def catalog.Definition) error {
	if err := s.catalog.ValidateUDFOwnership(def); err != nil {
		return err
	}
	mine, err := catalog.ExtractUDFNames(def.UDFs)
	if err != nil {
		return err
	}
	if len(mine) == 0 {
		return nil
	}
	owned := make(map[string]bool, len(mine))
	for _, n := range mine {
		owned[n] = true
	}
	current, err := s.store.ListCurrent()
	if err != nil {
		return err
	}
	for _, inst := range current {
		if inst.RuleID == def.ID || inst.Deleted {
			continue
		}
		names, err := catalog.ExtractUDFNames(inst.DeployedUDFs)
		if err != nil {
			continue // an unparseable deployed UDF cannot assert ownership
		}
		for _, n := range names {
			if owned[n] {
				return fmt.Errorf("UDF function %q is already deployed by installed rule %q", n, inst.RuleID)
			}
		}
	}
	return nil
}
```
  Replace the `s.catalog.ValidateUDFOwnership(def)` calls in `Install` (≈line 374) and `Upgrade` (≈line 582) with `s.checkUDFOwnership(def)`. **Verified:** the `snapshotStore` interface already declares `ListCurrent() ([]RuleInstance, error)` (service.go:84) and the test fake already implements it — **no interface change is needed**.

- [ ] **Step 4: Run to verify it passes** → PASS; run the whole `./internal/rules/` package.

- [ ] **Step 5: Update the catalog `LIMITATION (Phase B)` comment** in `definition.go:298-305` to note the ledger check now lives in `rules.Service.checkUDFOwnership`.

- [ ] **Step 6: Defer commit** to Task 8.

### Task 8: Assert the ApplyUDFs / preflight non-destructive guarantee, then commit

**Files:**
- Test: `backend/internal/rules/service_test.go`
- Modify (only if the test reveals a gap): `backend/internal/rules/service.go:761-835`

**Interfaces:**
- Consumes: existing `preflight`, `replaceAndPersist`, `rollback` (service.go).

- [ ] **Step 1: Write the failing/guard test** — assert two guarantees on an **enabled** rule being reconfigured: (a) if `ApplyUDFs(candidate)` fails during preflight, the live MV is never dropped and the persisted snapshot is unchanged; (b) if `CreateMVStrict` fails after the drop, `rollback` recreates the previous MV from `cur.RenderedSQL`+`cur.DeployedUDFs` and the snapshot is unchanged. Drive failures via the `mvOps` fake's injectable error hooks.
```go
func TestConfigure_ApplyFailLeavesLiveMVAndSnapshotIntact(t *testing.T) {
	svc, fakes := newTestService(t)
	fakes.seedInstalledEnabled("rp-x") // 1 snapshot, MV exists
	fakes.mv.failApplyUDFs = true       // candidate apply fails in preflight
	before := fakes.store.appendCount
	_, err := svc.Configure("rp-x", map[string]any{"threshold": 9}, "t", "r")
	if err == nil { t.Fatal("expected error") }
	if fakes.mv.dropCount != 0 { t.Fatalf("live MV was dropped before validation (%d)", fakes.mv.dropCount) }
	if fakes.store.appendCount != before { t.Fatal("snapshot must be unchanged") }
}
```

- [ ] **Step 2: Run to verify it fails or passes** — `cd backend && CGO_ENABLED=0 go test ./internal/rules/ -run TestConfigure_ApplyFail -v`. Phase A's `Configure` already preflights before `replaceAndPersist`, so (a) likely PASSES already — that's fine, it locks the guarantee. If it FAILS, the only allowed fix is to ensure preflight (apply-candidate-to-disposable-sink + restore) runs **before** any `DropMVNoDelay`; do not weaken rollback.

- [ ] **Step 3: Add the create-fail rollback test**
```go
func TestConfigure_CreateFailRestoresPreviousMV(t *testing.T) {
	svc, fakes := newTestService(t)
	fakes.seedInstalledEnabled("rp-x")
	fakes.mv.failCreateAfter = 1 // candidate create fails post-drop
	_, err := svc.Configure("rp-x", map[string]any{"threshold": 9}, "t", "r")
	if err == nil { t.Fatal("expected error") }
	if !fakes.mv.recreatedFrom("rp-x", fakes.prevRenderedSQL("rp-x")) {
		t.Fatal("previous MV not restored from last-known-good rendered_sql")
	}
}
```

- [ ] **Step 4: Run both** → PASS.

- [ ] **Step 5: Commit**
```bash
git add backend/internal/rules backend/internal/catalog/definition.go
git commit -m "fix(rules): validate UDF ownership against installed snapshots; lock ApplyUDFs non-destructive guarantee (#33, Phase B)"
```

---

# Commit B1: `regex` and `duration` parameter types

### Task 9: `regex` type — normalize (RE2 + length) and render

**Files:**
- Modify: `backend/internal/catalog/definition.go:64-71` (`parameterTypes`), `backend/internal/rules/validate.go:196-305` (`normalizeValue`), `backend/internal/rules/render.go:110-171` (`renderValue`)
- Test: `backend/internal/rules/render_test.go`, `validate_test.go`

**Interfaces:**
- Produces: `regex` canonical value = `string` (compiles under Go `regexp`/RE2, NUL-free, length-bounded by `max_length`); SQL rendering = escaped single-quoted literal.

- [ ] **Step 1: Write the failing tests**
```go
func TestNormalize_Regex(t *testing.T) {
	p := catalog.Parameter{Key: "pat", Type: "regex", Default: "a.*b"}
	if _, err := normalizeValue(p, "foo[0-9]+"); err != nil { t.Fatalf("valid regex rejected: %v", err) }
	if _, err := normalizeValue(p, "foo[0-9"); err == nil { t.Fatal("invalid RE2 must be rejected") }
}
func TestRender_Regex(t *testing.T) {
	p := catalog.Parameter{Key: "pat", Type: "regex", Default: "a'b"}
	out, err := renderValue(p, "a'b")
	if err != nil { t.Fatal(err) }
	if out != "'a''b'" { t.Fatalf("got %q", out) }
}
```

- [ ] **Step 2: Run to verify they fail** — `cd backend && CGO_ENABLED=0 go test ./internal/rules/ -run 'Regex' -v` → FAIL.

- [ ] **Step 3: Implement**
  - `definition.go`: add `"regex": true` to `parameterTypes`.
  - `validate.go` `normalizeValue`, add before the `default:`:
    ```go
    case "regex":
        s, ok := v.(string)
        if !ok {
            return nil, fmt.Errorf("expected string, got %s", typeName(v))
        }
        if err := checkNoNUL(s); err != nil {
            return nil, err
        }
        if err := checkStringLength(p, s); err != nil {
            return nil, err
        }
        if _, err := regexp.Compile(s); err != nil {
            return nil, fmt.Errorf("must be a valid RE2 regular expression: %v", err)
        }
        return s, nil
    ```
    (add `"regexp"` to the validate.go imports).
  - `render.go` `renderValue`, add a case mirroring `string`:
    ```go
    case "regex":
        s, ok := v.(string)
        if !ok {
            return "", fmt.Errorf("parameter %q: internal: expected string, got %T", p.Key, v)
        }
        return renderStringLiteral(s), nil
    ```
  - Update the `default:` "supported:" message lists in both files to include `regex`.

- [ ] **Step 4: Run to verify they pass** → PASS.

- [ ] **Step 5: Defer commit** to Task 11.

### Task 10: `duration` type — fixed-unit integer

**Files:**
- Modify: `backend/internal/catalog/definition.go` (`parameterTypes` + `Validate` requires `unit`), `validate.go`, `render.go`
- Test: `backend/internal/rules/validate_test.go`, `render_test.go`, `backend/internal/catalog/definition_test.go`

**Interfaces:**
- Produces: `duration` canonical value = `int64` count of the definition-fixed `unit` (constraints `min`/`max`); render = bare decimal integer. `unit` is required for `duration` and is UI metadata only (the template author writes e.g. `INTERVAL {{ .Config.window }} SECOND`).

- [ ] **Step 1: Write the failing tests**
```go
func TestNormalize_Duration(t *testing.T) {
	min := 1.0; max := 3600.0
	p := catalog.Parameter{Key: "w", Type: "duration", Unit: "seconds", Default: 60, Min: &min, Max: &max}
	v, err := normalizeValue(p, 90)
	if err != nil || v.(int64) != 90 { t.Fatalf("got %v %v", v, err) }
	if _, err := normalizeValue(p, 5000); err == nil { t.Fatal("over-max must reject") }
	if _, err := normalizeValue(p, 1.5); err == nil { t.Fatal("fractional must reject") }
}
func TestValidate_DurationRequiresUnit(t *testing.T) {
	d := catalog.Definition{ID: "r", Name: "n", Version: "1",
		SQLTemplate: "x {{ .Config.w }}",
		Parameters:  []catalog.Parameter{{Key: "w", Type: "duration", Default: 60}}}
	if err := d.Validate(); err == nil { t.Fatal("duration without unit must be rejected") }
}
func TestRender_Duration(t *testing.T) {
	p := catalog.Parameter{Key: "w", Type: "duration", Unit: "seconds", Default: 60}
	out, err := renderValue(p, int64(90))
	if err != nil || out != "90" { t.Fatalf("got %q %v", out, err) }
}
```

- [ ] **Step 2: Run to verify they fail** → FAIL.

- [ ] **Step 3: Implement**
  - `definition.go`: add `"duration": true` to `parameterTypes`; in `Validate`'s parameter loop add: `if p.Type == "duration" && p.Unit == "" { return fmt.Errorf("config[%d] (%s): duration requires a unit", i, p.Key) }`.
  - `validate.go` `normalizeValue` `case "duration":` — reuse the integer path (coerceInt64 + the 2^53 bound guard + `checkNumericBounds`), return `int64`.
  - `render.go` `renderValue` `case "duration":` — same body as `integer` (`strconv.FormatInt(i, 10)`), asserting `int64`.
  - Update "supported:" messages to include `duration`.

- [ ] **Step 4: Run to verify they pass** → PASS.

- [ ] **Step 5: Defer commit** to Task 11.

### Task 11: Compatibility tests + representative parameterization, then commit

**Files:**
- Test: `backend/internal/rules/compatibility_test.go`
- Modify: one window seed rule (add a `duration`), add a `regex` fixture (`backend/internal/rules/testdata/` or a seed)
- Test: `backend/internal/rules/seed_golden_test.go`

- [ ] **Step 1: Compatibility tests** — add cases proving: same-key same-type `regex`/`duration` overrides preserve across upgrade; a `regex→string` (or `duration→integer`) type change blocks with a conflict; a tightened `duration` `max` that the existing override violates blocks. These exercise the already-type-generic `compatibility.go`.

- [ ] **Step 2: Parameterize a representative window rule** — pick a seed rule with a hard-coded time window; replace the literal with `{{ .Config.window_seconds }}` and declare a `duration` param (`unit: seconds`, default = the prior literal, sensible `min`/`max`). **Proton caution (see the `timeplus-proton-emit-and-udaf` skill):** do NOT parameterize a streaming `MAXSPAN`/`TIMEOUT` interval — `MAXSPAN` only supports ms/sec/min and requires `TIMEOUT ≥ MAXSPAN`. Choose a rule whose window is a plain `tumble(...)`/`INTERVAL N SECOND` so an operator value can't produce an install-time `UNSUPPORTED`/`TIMEOUT` error; constrain `min`/`max` to a safe range. Add/extend a `regex` fixture used by a detection that matches operator-supplied text via `match()`/`extract()`.

- [ ] **Step 3: Golden assertion** — extend `seed_golden_test.go` so the modified rule's default render equals its pre-parameterization SQL after whitespace normalization.

- [ ] **Step 4: Run** — `cd backend && CGO_ENABLED=0 go test ./internal/rules/ ./internal/catalog/ -v` → PASS.

- [ ] **Step 5: Frontend controls** — in `ParamField.jsx` `TypedControl`, add before `case 'string'`:
```jsx
case 'regex':
  return (
    <input type="text" className={inputCls + ' font-mono'} value={value ?? ''}
      placeholder="RE2 regular expression" onChange={(e) => onChange(e.target.value)} />
  )
case 'duration':
  return (
    <div className="flex items-center gap-2">
      <input type="number" step="1" min={param.min ?? undefined} max={param.max ?? undefined}
        className={inputCls} value={value ?? ''}
        onChange={(e) => { const r = e.target.value; if (r === '') return onChange(''); const n = Number(r); onChange(Number.isNaN(n) ? r : n) }} />
      <span className="text-xs text-gray-400">{param.unit || 'units'}</span>
    </div>
  )
```
  (Regex is validated authoritatively on the backend; the control may add a non-blocking `try { new RegExp(value) }` hint, but never blocks save on it — RE2 ≠ JS regex.)

- [ ] **Step 6: Verify frontend** — `cd frontend && npm run lint && npm run build && npm test` → PASS.

- [ ] **Step 7: Commit**
```bash
git add backend/internal/rules backend/internal/catalog frontend/src/components/rules/ParamField.jsx
git commit -m "feat(rules): regex and duration parameter types + UI controls (#33, Phase B)"
```

---

# Commit B2: clone-to-custom

### Task 12: `RescopeUDFs` — rewrite UDF names + references for a clone

**Files:**
- Modify: `backend/internal/catalog/definition.go`
- Test: `backend/internal/catalog/definition_test.go`

**Interfaces:**
- Produces: `func RescopeUDFs(def Definition, newID string) (Definition, error)` — returns a copy whose every UDF DDL declares a new rule-scoped name `ag_<newID_normalized>_<i>`, with each old function name word-boundary-replaced in `SQLTemplate` and in the other UDF bodies. Definitions with no UDFs return a copy unchanged.

- [ ] **Step 1: Write the failing test**
```go
func TestRescopeUDFs(t *testing.T) {
	src := catalog.Definition{
		ID: "rp-003", SQLTemplate: "select ag_rp003_score(tool_input) as s",
		UDFs: []string{"CREATE OR REPLACE FUNCTION ag_rp003_score(x string) RETURNS float LANGUAGE PYTHON AS $$...$$;"},
	}
	out, err := catalog.RescopeUDFs(src, "my-clone")
	if err != nil { t.Fatal(err) }
	names, _ := out.UDFNames()
	if names[0] == "ag_rp003_score" { t.Fatal("UDF name was not rescoped") }
	if strings.Contains(out.SQLTemplate, "ag_rp003_score") { t.Fatal("reference not rewritten") }
	if !strings.Contains(out.SQLTemplate, names[0]) { t.Fatalf("template missing new name %q", names[0]) }
}
```

- [ ] **Step 2: Run to verify it fails** → FAIL (undefined).

- [ ] **Step 3: Implement** — normalize `newID` the same way `timeplus.MVName` does (strip `-`); for `i, ddl := range def.UDFs`, extract old name via `ExtractUDFNames([]string{ddl})`, compute `newName := fmt.Sprintf("ag_%s_%d", norm, i)`, and replace old→new with a word-boundary regex (`\b<old>\b`) in the DDL, the `SQLTemplate`, and every other UDF body. Return the rewritten copy. Reject (error) if two source UDFs share a name (already barred by `Validate`, but guard anyway). Add catalog sentinel errors used by clone/update handlers: `var ErrNotFound = errors.New("rule definition not found")`, `var ErrExists = errors.New("rule definition already exists")`, and `var ErrInvalid = errors.New("invalid rule definition")` in the catalog package. Wrap `Definition.Validate()` structural failures with `ErrInvalid` (`fmt.Errorf("%w: %s", ErrInvalid, …)`) so the handler can map them to 400.

- [ ] **Step 4: Run to verify it passes** → PASS.

- [ ] **Step 5: Defer commit** to Task 14.

### Task 13: `Manager.Clone` + `Manager.IsCustom`

**Files:**
- Modify: `backend/internal/catalog/catalog.go`
- Test: `backend/internal/catalog/catalog_test.go`

**Interfaces:**
- Produces: `func (m *Manager) Clone(srcID, newID, newName string) (Definition, error)`; `func (m *Manager) IsCustom(id string) bool`.

- [ ] **Step 1: Write the failing test**
```go
func TestManager_Clone(t *testing.T) {
	m := newTestManagerWithSeed(t) // existing helper
	cloned, err := m.Clone("rp-003", "rp-003-custom", "RP-003 (custom)")
	if err != nil { t.Fatal(err) }
	if cloned.Origin != catalog.OriginCustom { t.Fatalf("origin=%s", cloned.Origin) }
	if cloned.ID != "rp-003-custom" { t.Fatal("id not set") }
	if !m.Exists("rp-003-custom") || !m.IsCustom("rp-003-custom") { t.Fatal("clone not saved as custom") }
	// cloning onto an existing id must fail
	if _, err := m.Clone("rp-003", "rp-003-custom", "x"); err == nil { t.Fatal("dup id must fail") }
}
```

- [ ] **Step 2: Run to verify it fails** → FAIL.

- [ ] **Step 3: Implement**
  - `Clone`: `src, ok := m.Lookup(srcID)` → return `ErrNotFound` if absent; if `m.Exists(newID)` return `ErrExists`; `ValidateID(newID)` (syntax); copy `src`; set `ID=newID`, `Name=newName` (default to `src.Name+" (custom)"` if empty), `Origin=OriginCustom`; `RescopeUDFs(copy, newID)`; then `m.Save(rescoped)` (which validates structurally + semantically + checks UDF ownership). Return the saved definition.
  - `IsCustom`: `d, ok := m.Lookup(id); return ok && d.Origin == OriginCustom`.

- [ ] **Step 4: Run to verify it passes** → PASS.

- [ ] **Step 5: Defer commit** to Task 14.

### Task 14: `POST /api/rules/catalog/:id/clone` endpoint + client + commit

**Files:**
- Modify: `backend/internal/handlers/catalog.go`, `backend/cmd/server/main.go`, `backend/internal/handlers/docs.go`
- Modify: `frontend/src/api/client.js`, `frontend/src/pages/RuleDetail.jsx`
- Test: `backend/cmd/server/main_test.go` (route smoke) or handler test

**Interfaces:**
- Produces: `func CloneCatalogRule(mgr *catalog.Manager) gin.HandlerFunc` (body `{ id, name }`, returns 201 + the cloned `Definition`); `api.cloneCatalogRule(srcId, { id, name })`.

- [ ] **Step 1: Write the failing route test** — register routes via the existing test harness and assert `POST /api/rules/catalog/rp-003/clone` with `{"id":"rp-003-c","name":"C"}` returns 201 and a custom definition. Verify no gin route-tree panic when `/rules/catalog/:id/clone`, `/rules/catalog/import`, `/rules/catalog/export` coexist (run the server boot in the test). **If gin panics on mixed static+param at `/rules/catalog/`,** fall back to mounting param-based catalog routes under `/rules/definition/:id` and keep `:id/clone` there; record the chosen prefix in `docs.go`.

- [ ] **Step 2: Run to verify it fails** → FAIL.

- [ ] **Step 3: Implement**
  - **Add the `catalog.ErrInvalid` sentinel** (Task 13's error block) and have `Definition.Validate()` wrap its structural failures with it (`fmt.Errorf("%w: %s", ErrInvalid, msg)`), so a plain structural failure is `errors.Is`-detectable as a client error rather than defaulting to 500.
  - **Add the unified `respondCatalogError` helper** in `internal/handlers/catalog.go` — one responder for ALL catalog-authoring endpoints (compile/create/clone are pure authoring; update also touches Timeplus, so the responder covers the service errors too). Maps via `errors.As`/`errors.Is`:
    ```go
    func respondCatalogError(c *gin.Context, err error) {
        var verr *rules.ValidationError
        var derr *rules.DefinitionError
        var cerr *rules.ConflictError
        var crit *rules.CriticalError
        switch {
        case errors.Is(err, catalog.ErrNotFound):
            c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
        case errors.Is(err, catalog.ErrExists):
            c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
        case errors.As(err, &cerr): // upgrade/edit compatibility conflict
            c.JSON(http.StatusConflict, gin.H{"error": cerr.Error()})
        case errors.As(err, &verr):
            c.JSON(http.StatusBadRequest, gin.H{"error": verr.Error(), "fields": verr.Fields})
        case errors.As(err, &derr): // operator-submitted definition defect → caller fault (400, unlike install's builtin-default 500)
            c.JSON(http.StatusBadRequest, gin.H{"error": derr.Error(), "kind": derr.Kind, "fields": derr.Fields})
        case errors.Is(err, catalog.ErrInvalid): // wrapped structural Validate() failure
            c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        case errors.As(err, &crit):
            c.JSON(http.StatusInternalServerError, gin.H{"error": crit.Error()})
        case isTimeplusUnavailable(err): // reuse the same predicate respondRuleError uses for 503
            c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
        default:
            c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        }
    }
    ```
    Confirm the `rules.ConflictError`/`rules.CriticalError` type names and the Timeplus-unavailable predicate against `respondRuleError` (rules.go:82) before writing — reuse whatever it uses, don't invent a parallel one.
  - Handler:
    ```go
    func CloneCatalogRule(mgr *catalog.Manager) gin.HandlerFunc {
        return func(c *gin.Context) {
            src := c.Param("id")
            var body struct{ ID, Name string }
            if err := c.ShouldBindJSON(&body); err != nil { c.JSON(400, gin.H{"error": err.Error()}); return }
            d, err := mgr.Clone(src, strings.TrimSpace(body.ID), strings.TrimSpace(body.Name))
            if err != nil { respondCatalogError(c, err); return }
            c.JSON(201, d)
        }
    }
    ```
  - Route in `main.go` rules group: `protected.POST("/rules/catalog/:id/clone", handlers.CloneCatalogRule(mgr))`.
  - `client.js`: `cloneCatalogRule: (id, body) => post(\`/rules/catalog/${encodeURIComponent(id)}/clone\`, body)`.
  - `RuleDetail.jsx`: add a "Clone as custom" button (Digital Sentry secondary button styling) that prompts for an id/name (a small modal or inline form), calls `api.cloneCatalogRule(catalogId, { id, name })`, then `navigate(\`/rules/${cloned.id}/edit\`)` (the B4 editor route) or `/rules/${cloned.id}` if the editor isn't wired yet.

- [ ] **Step 4: Run to verify it passes** — backend route test PASS; `cd frontend && npm run build` PASS.

- [ ] **Step 5: Commit**
```bash
git add backend frontend
git commit -m "feat(rules): clone-to-custom — POST /api/rules/catalog/:id/clone + UI action (#33, Phase B)"
```

---

# Commit B3: catalog CRUD + compile APIs

### Task 15: `GET /api/rules/catalog/:id` + `POST /api/rules/catalog/compile`

**Files:**
- Modify: `backend/internal/handlers/catalog.go`, `backend/cmd/server/main.go`, `docs.go`
- Test: handler/route test

**Interfaces:**
- Produces: `func GetCatalogRule(mgr *catalog.Manager) gin.HandlerFunc` (returns full `Definition` or 404); `func CompileCatalogRule() gin.HandlerFunc` (body `{ definition: Definition, overrides?: map }`, returns `{ rendered_sql, definition_hash, effective, overrides }` on success, 400 `*ValidationError`/500 `*DefinitionError` mapped via `respondRuleError`, **no persistence, no MV**).

- [ ] **Step 1: Write the failing tests** — `GET /api/rules/catalog/rp-003` returns the definition with `config[].key`; `POST /api/rules/catalog/compile` with a valid candidate returns rendered SQL; with a bad placeholder returns 500 `DefinitionError`; with a bad override returns 400 `ValidationError`.

- [ ] **Step 2: Run to verify they fail** → FAIL.

- [ ] **Step 3: Implement**
  - `GetCatalogRule`: `d, ok := mgr.Lookup(c.Param("id"))`; 404 if absent; else `c.JSON(200, d)`.
  - `CompileCatalogRule`: bind `{ Definition catalog.Definition; Overrides map[string]any }` using a `json.Decoder` with `UseNumber()` (so large integers survive — mirror how `ConfigureRule` decodes); first run `body.Definition.Validate()` (structural) then `rules.Compile(body.Definition, body.Overrides)`; on success return the `Compiled` fields; on error `respondCatalogError(c, err)` (authoring context — a bad submitted template is a 400, not a 500).
  - Routes: `protected.GET("/rules/catalog/:id", ...)`, `protected.POST("/rules/catalog/compile", ...)`. **Register `/rules/catalog/compile` (static) consistently with the `:id` param route** — use the same prefix decision from Task 14.

- [ ] **Step 4: Run to verify they pass** → PASS.

- [ ] **Step 5: Defer commit** to Task 17.

### Task 16: `POST /api/rules/catalog` (create custom) + `PUT /api/rules/catalog/:id` (atomic update)

**Files:**
- Modify: `backend/internal/handlers/catalog.go`, `backend/internal/rules/service.go` (`UpdateDefinition` + extend `defStore` with `Save`), `backend/cmd/server/main.go`, `docs.go`
- Test: `backend/internal/rules/service_test.go`, handler test

**Interfaces:**
- Consumes: `Service.store`, `Service.catalog` (now also `Save`), existing `preflight`/`replaceAndPersist`/`rollback`.
- Produces:
  - `func CreateCatalogRule(mgr *catalog.Manager) gin.HandlerFunc` (body = `Definition`; forces `Origin=custom`; `mgr.Save`; 201).
  - `func (s *Service) UpdateDefinition(def catalog.Definition, actor, reason string) (RuleInstance, error)` — **immediate atomic publish with rollback**: under the per-rule lock, capture `prev` definition; reject if the existing definition is not `custom`; `s.catalog.Save(def)`; if an instance is installed, recompile against its stored overrides, `checkUDFOwnership`, preflight, and if enabled `replaceAndPersist`; on ANY failure restore `s.catalog.Save(prev)` and the previous MV, returning the original error. If no instance is installed, Save is the whole operation.
  - `defStore` gains `Save(catalog.Definition) error`.
  - `func UpdateCatalogRule(cfg *config.Config, mgr *catalog.Manager, lc *rules.Lifecycle) gin.HandlerFunc`.

- [ ] **Step 1: Write the failing service test**
```go
func TestUpdateDefinition_RejectsBuiltin(t *testing.T) {
	svc, fakes := newTestService(t)
	fakes.catalog["rp-003"] = catalog.Definition{ID: "rp-003", Origin: catalog.OriginBuiltin, /*…*/}
	_, err := svc.UpdateDefinition(catalog.Definition{ID: "rp-003", Origin: catalog.OriginBuiltin}, "t", "r")
	if err == nil || !strings.Contains(err.Error(), "custom") { t.Fatalf("expected builtin-edit rejection, got %v", err) }
}
func TestUpdateDefinition_RedeployFailRestoresDefinitionAndMV(t *testing.T) {
	svc, fakes := newTestService(t)
	fakes.seedCustomInstalledEnabled("rp-c") // custom def + enabled instance
	fakes.mv.failCreateAfter = 1
	_, err := svc.UpdateDefinition(fakes.mutatedDef("rp-c"), "t", "r")
	if err == nil { t.Fatal("expected redeploy failure") }
	if fakes.catalog["rp-c"].Hash() != fakes.prevHash("rp-c") { t.Fatal("definition not rolled back") }
	if !fakes.mv.recreatedFrom("rp-c", fakes.prevRenderedSQL("rp-c")) { t.Fatal("previous MV not restored") }
}
```

- [ ] **Step 2: Run to verify they fail** → FAIL.

- [ ] **Step 3: Implement** — Extend the `defStore` interface (`service.go:82-86`) with `Save(catalog.Definition) error` and confirm `*catalog.Manager` satisfies it. Implement `UpdateDefinition` following the plan's "installed custom-definition edit" ordering (validate → save → recompile against stored overrides → checkUDFOwnership → preflight → replaceAndPersist when enabled → on failure restore prev definition + MV). Reuse `buildSnapshot`/`replaceAndPersist`/`rollback`. `CreateCatalogRule` is a thin handler: force `Origin=custom`, `mgr.Save`, 201. `UpdateCatalogRule` handler binds the `Definition`, sets `def.ID = c.Param("id")`, `def.Origin = custom`, calls `lc.For(cfg).UpdateDefinition(def, actor, reason)`, maps errors with `respondCatalogError` (the union responder handles catalog sentinels + the service's `ConflictError`/`CriticalError`/Timeplus-down). `CreateCatalogRule` likewise uses `respondCatalogError`.

- [ ] **Step 4: Run to verify they pass** → PASS; run the whole `./internal/rules/` and `./internal/handlers/` packages.

- [ ] **Step 5: Routes** — `protected.POST("/rules/catalog", handlers.CreateCatalogRule(mgr))`, `protected.PUT("/rules/catalog/:id", handlers.UpdateCatalogRule(cfg, mgr, lifecycle))`.

- [ ] **Step 6: Defer commit** to Task 17.

### Task 17: Client wiring + docs + commit

**Files:**
- Modify: `frontend/src/api/client.js`, `backend/internal/handlers/docs.go`, `CLAUDE.md` API table

- [ ] **Step 1: Add client functions**
```js
getCatalogRule: (id) => get(`/rules/catalog/${encodeURIComponent(id)}`),
compileCatalogRule: (definition, overrides) => post('/rules/catalog/compile', { definition, overrides }),
createCatalogRule: (definition) => post('/rules/catalog', definition),
updateCatalogRule: (id, definition) => put(`/rules/catalog/${encodeURIComponent(id)}`, definition),
```

- [ ] **Step 2: Docs** — add the 5 new routes (clone, get, compile, create, update) to `docs.go` OpenAPI and the `CLAUDE.md` `/api/rules/catalog/*` table rows.

- [ ] **Step 3: Verify** — `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...`; `cd frontend && npm run build && npm run lint`.

- [ ] **Step 4: Commit**
```bash
git add backend frontend CLAUDE.md
git commit -m "feat(rules): catalog CRUD + compile-preview APIs with atomic publish (#33, Phase B)"
```

---

# Commit B4: custom-definition authoring UI

### Task 18: `ParamSchemaEditor` component

**Files:**
- Create: `frontend/src/components/rules/ParamSchemaEditor.jsx`
- Test: frontend test if the suite supports component tests

**Interfaces:**
- Produces: `export default function ParamSchemaEditor({ config, onChange })` where `config` is an array of `{ key, label, description, type, default, …constraints, unit, options }`. Emits the full array via `onChange`. Supports add/remove/reorder a param and editing its fields; the `type` selector lists all eight tokens; type-specific constraint inputs (min/max, min_length/max_length, options for enum, min_items/max_items for string_list, unit for duration). It edits **schema**, not values — it must not import `ParamField`.

- [ ] **Step 1: Implement** — a controlled list editor. Each row: `key` text input (validated against `^[a-z][a-z0-9_]{0,63}$`, show inline error), `label`, `type` `<select>`, `default` input (a plain text/number/JSON input appropriate to `type`), and a collapsible "constraints" area whose fields switch on `type`. "Add parameter" appends `{ key: '', label: '', type: 'string', default: '' }`. Reuse Digital Sentry tokens (`bg-surface-container-high`, 4px radius, no borders for sectioning).

- [ ] **Step 2: Verify** — `cd frontend && npm run lint && npm run build` → PASS.

- [ ] **Step 3: Defer commit** to Task 20.

### Task 19: Refactor `RuleCreate.jsx` into the authoring editor with compile preview

**Files:**
- Modify: `frontend/src/pages/RuleCreate.jsx`, `frontend/src/App.jsx` (add an `/rules/:id/edit` route reusing the editor for custom edits), `frontend/src/api/client.js` (already has compile/create/update from B3)

**Interfaces:**
- Consumes: `api.compileCatalogRule`, `api.createCatalogRule`, `api.updateCatalogRule`, `api.getCatalogRule`, `ParamSchemaEditor`.

- [ ] **Step 1: Implement the editor** — state holds a full candidate `Definition`: metadata fields (id, name, version, description, severity, category, fp_rate, hook_sources), `sql_template` (textarea with the existing Prism highlight if convenient), `config` (via `ParamSchemaEditor`), and `udfs` (a list of DDL textareas). Buttons:
  - **Compile preview**: `const r = await api.compileCatalogRule(def)` → render `r.rendered_sql` read-only and show `*ValidationError.fields` / `*DefinitionError` messages inline. The browser MUST NOT render SQL itself — preview comes only from the backend.
  - **Save**: for a new definition `api.createCatalogRule(def)`; for an edit (`mode === 'edit'`, route `/rules/:id/edit`, prefilled via `api.getCatalogRule(id)`) `api.updateCatalogRule(id, def)`. On success `navigate('/rules')` (or the rule detail).
  - Edits to an installed custom definition require an explicit `version` change (guard: if `mode==='edit'` and `version === original.version`, block Save with "bump the version to publish an edit").
- [ ] **Step 2: Clone-prefill** — when the editor is reached from "Clone as custom" (Task 14 navigates to `/rules/:id/edit`), prefill from `api.getCatalogRule(id)`.
- [ ] **Step 3: Route** — add `<Route path="/rules/:id/edit" element={<RuleCreate mode="edit" />} />` in `App.jsx`; keep `/rules/new` (or existing create route) with `mode="create"`.
- [ ] **Step 4: Verify** — `cd frontend && npm run lint && npm run build && npm test` → PASS.
- [ ] **Step 5: Defer commit** to Task 20.

### Task 20: RuleDetail "Edit definition" affordance + final UI wiring + commit

**Files:**
- Modify: `frontend/src/pages/RuleDetail.jsx`

- [ ] **Step 1: Implement** — for rules whose definition `origin === 'custom'`, show an "Edit definition" button linking to `/rules/${id}/edit`. Keep "Clone as custom" for builtin/imported. Keep Detection SQL read-only. Ensure the existing upgrade-conflict modal still shows conflicts before confirming an upgrade.
- [ ] **Step 2: Verify** — `cd frontend && npm run lint && npm run build && npm test` → PASS.
- [ ] **Step 3: Commit**
```bash
git add frontend
git commit -m "feat(rules): custom-definition authoring UI — template/param/UDF editor + compile preview (#33, Phase B)"
```

---

## Final Validation Matrix (Task 21)

Run before opening the PR. Record outputs.

- [ ] Backend: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go test -race ./internal/rules/...`.
- [ ] Frontend: `cd frontend && npm run build && npm run lint && npm test`.
- [ ] Whole-app build: `make build`.
- [ ] Catalog import/export round-trip preserves `config`/`key`, `regex`/`duration`, and templates (extend `import_test.go`/`catalog_test.go`).
- [ ] **Live Timeplus pass** (the same timeplusd CI uses — 3.2.11, per memory `feedback_run_integration_suite_before_push`): clean-provision; install a parameterized rule (incl. a `duration` and a `regex` param); configure (override + reset); clone-to-custom; edit the custom definition and publish (atomic); upgrade (compatible preserve + incompatible 409); toggle/policy/uninstall; `POST /api/rules/sync` recreates MVs from snapshots; confirm `DROP VIEW … NO DELAY` + non-`IF NOT EXISTS` recreate, and that a UDF-owned-by-installed-snapshot collision is rejected.
- [ ] Run the integration suite with `-tags integration` against that timeplusd, not just a manual enterprise pass.

---

## Self-Review (completed against the spec)

**Spec coverage** — `/apps` rename (B-align Tasks 1–5), UDF hardening + ApplyUDFs guarantee (B0 Tasks 6–8), regex/duration (B1 Tasks 9–11), clone-to-custom (B2 Tasks 12–14), catalog CRUD + compile (B3 Tasks 15–17), authoring UI (B4 Tasks 18–20), validation matrix (Task 21). All action items from the pasted Phase B plan map to a task.

**Open risks flagged inline** (resolve during execution, not silently): (1) ~~gin route-tree static+param coexistence~~ — **verified non-issue**: gin v1.9.1 already runs `/rules/catalog` (static) + `/rules/:id` (param) as siblings, so `/rules/catalog/:id` + `/rules/catalog/{import,export,compile}` and `/rules/catalog/:id/clone` follow proven patterns; keep only the Task 14 boot smoke-check, no fallback prefix needed; (2) the `/apps` rename **intentionally rotates `Definition.Hash()`** for parameterized defs (`id`→`key` in the serialization) — Task 4 Step 3 regenerates the pinned golden constant; no snapshot migration because the stream is clean-reprovisioned at this baseline; (3) `RescopeUDFs` word-boundary replacement is the trickiest logic — Task 12 tests it directly; (4) `UpdateDefinition` (Task 16) is the highest-risk task — catalog-publish + MV-redeploy + snapshot with full rollback; test the redeploy-failure-restores-definition-and-MV path explicitly.

**Type consistency** — `catalog.Parameter.Key`, `catalog.ExtractUDFNames`, `catalog.RescopeUDFs`, `Manager.Clone/IsCustom/Save`, `Service.checkUDFOwnership/UpdateDefinition`, `defStore.Save`, and the five handlers + five client functions are referenced consistently across tasks.

**`/apps` alignment decision is authoritative** for naming (see the decision section); where this plan and any earlier Phase A doc conflict, this plan wins for Phase B.
