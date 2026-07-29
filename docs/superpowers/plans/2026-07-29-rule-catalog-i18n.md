# Localized Rule Name & Description Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve builtin rule `name`/`description` in the active UI locale (starting with `zh-CN`) by resolving locale-specific text server-side from a `translations` map in the catalog definition.

**Architecture:** Add an optional `Translations` map to `catalog.Definition`, authored in the seed YAML. A presentation-only `Localized(locale)` overlay resolves it at read time; four rule read endpoints apply the overlay based on an explicit `?locale=` query param. The frontend sends its active i18n locale and refetches on language change. No SQL/MV/hash/snapshot changes.

**Tech Stack:** Go 1.x (Gin, `gopkg.in/yaml.v3`), React 18 + Vite, i18next / react-i18next.

## Global Constraints

- `Translations` MUST be excluded from `Definition.Hash()` (the `hashPayload` struct) — adding a translation must not change a definition's hash or flag installed rules as upgradeable.
- Supported locales are exactly `en` and `zh-CN` (mirror the `ui.default_language` enum in `internal/config/registry.go`). Unknown/absent locale → `en`.
- Read endpoints MUST NOT expose the raw `translations` map — it is cleared from the response after resolution.
- `GET /api/rules/catalog/export` MUST remain canonical English (never localized).
- No changes to SQL templates, `rendered_sql`, materialized views, `agentguard_security_events`, or the `agentguard_rules` snapshot schema.
- Backend build/test uses `CGO_ENABLED=0` (macOS 26+ static-binary requirement).
- Translated content preserves CVE identifiers, URLs, arXiv/reference citations, and code/SQL fragments verbatim; only prose is translated.

---

### Task 1: Catalog data model — `Translations`, `TranslationFor`, `Localized`

Add the translations field and the resolution helpers to the catalog definition, and prove the hash is unaffected.

**Files:**
- Modify: `backend/internal/catalog/definition.go` (add `Translation` type + `Translations` field on `Definition` after `Description`; add `TranslationFor` and `Localized` methods)
- Test: `backend/internal/catalog/definition_test.go`

**Interfaces:**
- Produces:
  - `catalog.Translation` struct: `{ Name string; Description string }` with `yaml:"name"/"description"` and `json:"name"/"description"` tags.
  - `Definition.Translations map[string]Translation` (`yaml:"translations,omitempty" json:"translations,omitempty"`).
  - `func (d Definition) TranslationFor(locale string) (Translation, bool)` — returns the entry for `locale` and whether it exists.
  - `func (d Definition) Localized(locale string) Definition` — copy with `Name`/`Description` overlaid by the locale's non-empty translated fields (per-field fallback to base), and `Translations` cleared to `nil`.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/catalog/definition_test.go`:

```go
func TestDefinition_Localized(t *testing.T) {
	base := Definition{
		ID:          "rp-001",
		Name:        "Prompt Injection Shield",
		Version:     "1.0.0",
		Description: "Detects prompt injection.",
		SQLTemplate: "SELECT 1",
		Translations: map[string]Translation{
			"zh-CN": {Name: "提示注入防护", Description: "检测提示注入。"},
		},
	}

	// Full translation present.
	zh := base.Localized("zh-CN")
	if zh.Name != "提示注入防护" || zh.Description != "检测提示注入。" {
		t.Errorf("zh-CN overlay wrong: name=%q desc=%q", zh.Name, zh.Description)
	}
	if zh.Translations != nil {
		t.Errorf("Localized must clear Translations, got %v", zh.Translations)
	}

	// Unknown locale falls back to base, still cleared.
	en := base.Localized("en")
	if en.Name != "Prompt Injection Shield" || en.Description != "Detects prompt injection." {
		t.Errorf("en fallback wrong: name=%q desc=%q", en.Name, en.Description)
	}
	if en.Translations != nil {
		t.Errorf("Localized must clear Translations on fallback, got %v", en.Translations)
	}

	// Per-field fallback: name translated, description empty → keep base desc.
	partial := base
	partial.Translations = map[string]Translation{"zh-CN": {Name: "仅名称"}}
	p := partial.Localized("zh-CN")
	if p.Name != "仅名称" || p.Description != "Detects prompt injection." {
		t.Errorf("partial overlay wrong: name=%q desc=%q", p.Name, p.Description)
	}

	// Base definition is not mutated.
	if base.Translations == nil {
		t.Error("Localized must not mutate the receiver's Translations")
	}

	// TranslationFor reports presence.
	if _, ok := base.TranslationFor("zh-CN"); !ok {
		t.Error("TranslationFor(zh-CN) should exist")
	}
	if _, ok := base.TranslationFor("fr"); ok {
		t.Error("TranslationFor(fr) should not exist")
	}
}

func TestDefinition_Hash_IgnoresTranslations(t *testing.T) {
	base := Definition{
		ID: "rp-001", Name: "N", Version: "1.0.0",
		Description: "D", SQLTemplate: "SELECT 1",
	}
	withTr := base
	withTr.Translations = map[string]Translation{"zh-CN": {Name: "名", Description: "述"}}
	if base.Hash() != withTr.Hash() {
		t.Errorf("Hash changed by translations: %s vs %s", base.Hash(), withTr.Hash())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/ -run 'TestDefinition_Localized|TestDefinition_Hash_IgnoresTranslations' -v`
Expected: FAIL — `Translation`/`Translations`/`TranslationFor`/`Localized` undefined.

- [ ] **Step 3: Add the type, field, and methods**

In `backend/internal/catalog/definition.go`, add the `Translations` field to `Definition` immediately after the `Origin` field (or after `Parameters` — anywhere in the struct is fine; keep it last to minimize churn):

```go
	Origin            string      `yaml:"origin"             json:"origin,omitempty"`

	// Translations holds locale-specific display text keyed by locale
	// (e.g. "zh-CN"). It is presentation metadata only: excluded from Hash,
	// resolved server-side at read time, and never exposed on read responses.
	Translations map[string]Translation `yaml:"translations,omitempty" json:"translations,omitempty"`
}

// Translation is locale-specific display text for a rule definition. Any empty
// field falls back to the base (English) Definition field.
type Translation struct {
	Name        string `yaml:"name"        json:"name"`
	Description string `yaml:"description" json:"description"`
}

// TranslationFor returns the translation entry for locale and whether one exists.
func (d Definition) TranslationFor(locale string) (Translation, bool) {
	if d.Translations == nil {
		return Translation{}, false
	}
	tr, ok := d.Translations[locale]
	return tr, ok
}

// Localized returns a copy of d with Name/Description replaced by the locale's
// translated values (per-field fallback to the base when a translated field is
// empty), and with Translations cleared so the map never leaks onto a read
// response. An unknown/empty locale or a missing entry returns the base copy.
func (d Definition) Localized(locale string) Definition {
	out := d
	out.Translations = nil
	if tr, ok := d.TranslationFor(locale); ok {
		if tr.Name != "" {
			out.Name = tr.Name
		}
		if tr.Description != "" {
			out.Description = tr.Description
		}
	}
	return out
}
```

(The `}` shown above closes the existing `Definition` struct — do not add a second closing brace.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/ -run 'TestDefinition_Localized|TestDefinition_Hash_IgnoresTranslations' -v`
Expected: PASS.

- [ ] **Step 5: Run the full catalog package tests (no regressions)**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/`
Expected: `ok  github.com/timeplus/agentguard/internal/catalog`

- [ ] **Step 6: Commit**

```bash
git add backend/internal/catalog/definition.go backend/internal/catalog/definition_test.go
git commit -m "feat(catalog): add translations map + Localized overlay (#253)"
```

---

### Task 2: Backend — locale resolver + apply to the four read endpoints

Resolve the request locale and overlay localized name/description on the catalog list, catalog detail, installed list, and rule detail. Export stays English.

**Files:**
- Create: `backend/internal/handlers/locale.go`
- Modify: `backend/internal/handlers/rules.go` (`ListCatalog`, `ListRules`, `GetRule`)
- Modify: `backend/internal/handlers/catalog.go` (`GetCatalogRule`)
- Test: `backend/internal/handlers/locale_test.go`
- Test: `backend/internal/handlers/catalog_test.go` (add a localized GetCatalogRule case)

**Interfaces:**
- Consumes (from Task 1): `Definition.Localized(locale)`, `Definition.TranslationFor(locale)`.
- Produces: `func resolveLocale(c *gin.Context) string` in package `handlers` — returns `"zh-CN"` when `?locale=zh-CN`, else `"en"`.

- [ ] **Step 1: Write the failing resolver test**

Create `backend/internal/handlers/locale_test.go`:

```go
package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResolveLocale(t *testing.T) {
	cases := map[string]string{
		"zh-CN":   "zh-CN",
		"en":      "en",
		"":        "en",
		"fr":      "en",
		"zh":      "en",
		"ZH-CN":   "en", // case-sensitive, matches the config enum exactly
	}
	for in, want := range cases {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/x?locale="+in, nil)
		if got := resolveLocale(c); got != want {
			t.Errorf("resolveLocale(locale=%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestResolveLocale -v`
Expected: FAIL — `resolveLocale` undefined.

- [ ] **Step 3: Implement the resolver**

Create `backend/internal/handlers/locale.go`:

```go
package handlers

import "github.com/gin-gonic/gin"

// supportedLocales mirrors the ui.default_language enum in
// internal/config/registry.go. Keep the two in sync.
var supportedLocales = map[string]bool{
	"en":    true,
	"zh-CN": true,
}

// resolveLocale returns the request's UI locale from the ?locale= query param,
// defaulting to "en" for an absent or unsupported value. The UI sends its
// active i18n locale explicitly (not Accept-Language).
func resolveLocale(c *gin.Context) string {
	loc := c.Query("locale")
	if supportedLocales[loc] {
		return loc
	}
	return "en"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestResolveLocale -v`
Expected: PASS.

- [ ] **Step 5: Apply overlay in `GetCatalogRule`**

In `backend/internal/handlers/catalog.go`, change the success return of `GetCatalogRule` (currently `c.JSON(http.StatusOK, d)`):

```go
		d, ok := mgr.Lookup(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "rule definition not found"})
			return
		}
		c.JSON(http.StatusOK, d.Localized(resolveLocale(c)))
```

- [ ] **Step 6: Apply overlay in `ListCatalog`**

In `backend/internal/handlers/rules.go`, inside `ListCatalog`, resolve the locale once and localize each catalog entry before it is copied into `CatalogItem`. Add `locale := resolveLocale(c)` near the top of the handler (after `reqCfg := ...`), then change the entries loop:

```go
		locale := resolveLocale(c)
		entries := mgr.All()
		items := make([]CatalogItem, 0, len(entries))
		for _, e := range entries {
			e = e.Localized(locale)
			item := CatalogItem{
				ID:                e.ID,
				Name:              e.Name,
				Version:           e.Version,
				Description:       e.Description,
				Severity:          e.Severity,
				Category:          e.Category,
				FPRate:            e.FPRate,
				HookSources:       e.HookSources,
				RequiredTelemetry: e.RequiredTelemetry,
				Author:            e.Author,
				Tags:              e.Tags,
			}
			if inst, ok := installedMap[e.ID]; ok {
				item.Installed = true
				item.Enabled = inst.Enabled
				item.InstalledVersion = inst.DefinitionVersion
				item.Upgradeable = rules.CanonicalDefinitionHash(e) != inst.DefinitionHash
			}
			items = append(items, item)
		}
```

Note: `CanonicalDefinitionHash(e)` is still correct after `Localized` because the hash ignores name/description-translation differences and `Localized` only swaps `Name`/`Description` — but to be unambiguous and avoid hashing a mutated copy, compute upgradeable against the original. Replace the `item.Upgradeable` line above with a lookup of the un-localized definition:

```go
			if inst, ok := installedMap[e.ID]; ok {
				item.Installed = true
				item.Enabled = inst.Enabled
				item.InstalledVersion = inst.DefinitionVersion
				if orig, ok := mgr.Lookup(e.ID); ok {
					item.Upgradeable = rules.CanonicalDefinitionHash(orig) != inst.DefinitionHash
				}
			}
```

- [ ] **Step 7: Apply overlay in `ListRules` (installed list)**

In `backend/internal/handlers/rules.go`, inside `ListRules`, add `locale := resolveLocale(c)` after `svc := lc.For(...)`, then extend the existing instance loop to overlay translated fields onto the pack (only when a translation exists, so the snapshot name is preserved otherwise):

```go
		locale := resolveLocale(c)
		packs := make([]models.RulePack, 0, len(instances))
		for _, inst := range instances {
			p := instanceToPack(inst)
			if def, ok := mgr.Lookup(inst.RuleID); ok {
				if rules.CanonicalDefinitionHash(def) != inst.DefinitionHash {
					p.Upgradeable = true
					p.LatestVersion = def.Version
				}
				if tr, ok := def.TranslationFor(locale); ok {
					if tr.Name != "" {
						p.Name = tr.Name
					}
					if tr.Description != "" {
						p.Description = tr.Description
					}
				}
			}
			packs = append(packs, p)
		}
```

- [ ] **Step 8: Apply overlay in `GetRule` (detail)**

In `backend/internal/handlers/rules.go`, inside `GetRule`, after `view, err := svc.Describe(id)` succeeds, build localized copies of the instance and definition and use them in the response `gin.H`. Add:

```go
		locale := resolveLocale(c)
		inst := view.Instance
		def := view.Definition
		if view.InCatalog {
			def = def.Localized(locale)
			if tr, ok := view.Definition.TranslationFor(locale); ok {
				if tr.Name != "" {
					inst.Name = tr.Name
				}
				if tr.Description != "" {
					inst.Description = tr.Description
				}
			}
		}
```

Then change the final response object to use `inst` and `def` in place of `view.Instance` / `view.Definition`:

```go
		c.JSON(http.StatusOK, gin.H{
			"instance":          inst,
			"in_catalog":        view.InCatalog,
			"definition":        def,
			"schema":            view.Schema,
			"defaults":          view.Defaults,
			"effective":         view.Effective,
			"overrides":         inst.Overrides,
			"rendered_sql":      inst.RenderedSQL,
			"upgrade":           view.Upgrade,
			"mv":                view.MV,
			"detections_24h":    detections24h,
			"recent_detections": recentDetections,
		})
```

- [ ] **Step 9: Write the failing handler-overlay test**

Add to `backend/internal/handlers/catalog_test.go`:

```go
func TestGetCatalogRule_Localized(t *testing.T) {
	seed := fstest.MapFS{
		"rp-900.yaml": {Data: []byte(
			"id: rp-900\n" +
				"name: Base Name\n" +
				"version: 1.0.0\n" +
				"description: Base description.\n" +
				"sql_template: \"SELECT 1\"\n" +
				"translations:\n" +
				"  zh-CN:\n" +
				"    name: 中文名称\n" +
				"    description: 中文描述。\n",
		)},
	}
	mgr, err := catalog.New(seed, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New: %v", err)
	}
	r := gin.New()
	r.GET("/rules/catalog/:id", GetCatalogRule(mgr))

	// zh-CN → localized, translations map stripped.
	reqZh := httptest.NewRequest(http.MethodGet, "/rules/catalog/rp-900?locale=zh-CN", nil)
	wZh := httptest.NewRecorder()
	r.ServeHTTP(wZh, reqZh)
	if wZh.Code != http.StatusOK {
		t.Fatalf("zh: want 200, got %d: %s", wZh.Code, wZh.Body.String())
	}
	var zh catalog.Definition
	if err := json.Unmarshal(wZh.Body.Bytes(), &zh); err != nil {
		t.Fatalf("zh unmarshal: %v", err)
	}
	if zh.Name != "中文名称" || zh.Description != "中文描述。" {
		t.Errorf("zh not localized: name=%q desc=%q", zh.Name, zh.Description)
	}
	if zh.Translations != nil {
		t.Errorf("translations map must be stripped, got %v", zh.Translations)
	}
	if strings.Contains(wZh.Body.String(), "translations") {
		t.Errorf("response leaked translations key: %s", wZh.Body.String())
	}

	// No locale → English base.
	reqEn := httptest.NewRequest(http.MethodGet, "/rules/catalog/rp-900", nil)
	wEn := httptest.NewRecorder()
	r.ServeHTTP(wEn, reqEn)
	var en catalog.Definition
	if err := json.Unmarshal(wEn.Body.Bytes(), &en); err != nil {
		t.Fatalf("en unmarshal: %v", err)
	}
	if en.Name != "Base Name" || en.Description != "Base description." {
		t.Errorf("en not base: name=%q desc=%q", en.Name, en.Description)
	}
}
```

- [ ] **Step 10: Run the handler test to verify it fails, then passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/handlers/ -run TestGetCatalogRule_Localized -v`
Expected first (before Step 5 is applied): FAIL. After Steps 5–8 are in place: PASS.

- [ ] **Step 11: Compile-check and run the full handlers + build**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/handlers/`
Expected: build succeeds; `ok  github.com/timeplus/agentguard/internal/handlers`.

- [ ] **Step 12: Commit**

```bash
git add backend/internal/handlers/locale.go backend/internal/handlers/locale_test.go backend/internal/handlers/rules.go backend/internal/handlers/catalog.go backend/internal/handlers/catalog_test.go
git commit -m "feat(rules): resolve localized name/description on read endpoints (#253)"
```

---

### Task 3: Frontend — send active locale + refetch on language change

Thread the active UI locale into the four rule GET calls and refetch the rule pages when the language changes.

**Files:**
- Modify: `frontend/src/api/client.js` (add `withLocale` helper; apply to `listRules`, `getRulesCatalog`, `getCatalogRule`, `getRule`)
- Modify: `frontend/src/pages/RulePacks.jsx` (add `i18n.language` to the fetch effect deps)
- Modify: `frontend/src/pages/RuleDetail.jsx` (add `i18n.language` to the fetch effect deps)

**Interfaces:**
- Consumes (from Task 2): endpoints honoring `?locale=`.
- Produces: `withLocale(path)` — appends `locale=<i18n.language>` using the correct `?`/`&` separator.

- [ ] **Step 1: Add the `withLocale` helper and import i18n in `client.js`**

At the top of `frontend/src/api/client.js`, add the i18n import (alongside the existing imports):

```js
import i18n from '../i18n'
```

Then, near the `get` helper definition, add:

```js
// withLocale appends the active UI locale as a query param so the backend can
// resolve localized rule name/description. Uses the correct separator when the
// path already carries a query string.
function withLocale(path) {
  const loc = i18n.language || 'en'
  const sep = path.includes('?') ? '&' : '?'
  return `${path}${sep}locale=${encodeURIComponent(loc)}`
}
```

- [ ] **Step 2: Apply `withLocale` to the four calls**

In `frontend/src/api/client.js`, change these four lines:

```js
  listRules: () => get(withLocale('/rules')),
  getRulesCatalog: () => get(withLocale('/rules/catalog')),
  getCatalogRule: (id) => get(withLocale(`/rules/catalog/${encodeURIComponent(id)}`)),
  getRule: (id) => get(withLocale(`/rules/${id}`)),
```

- [ ] **Step 3: Refetch RulePacks on language change**

In `frontend/src/pages/RulePacks.jsx`, change the translation hook to also expose `i18n`:

```js
  const { t, i18n } = useTranslation('rules')
```

and change the data-fetch effect's dependency array from `[t]` to include the active locale:

```js
  useEffect(() => {
    Promise.all([
      loadRules(),
      api.getRulesCatalog().then(data => setCatalog(data.catalog || [])),
    ])
      .catch(() => { setLoadError(t('list.errors.loadFailed')) })
      .finally(() => setLoading(false))
  }, [t, i18n.language])
```

- [ ] **Step 4: Refetch RuleDetail on language change**

In `frontend/src/pages/RuleDetail.jsx`, change the translation hook to expose `i18n`:

```js
  const { t, i18n } = useTranslation('rules')
```

and add `i18n.language` to the dependency array of the effect that calls `api.getRule(id)` (around line 74). For example, if it currently reads `}, [id])`, change it to:

```js
  }, [id, i18n.language])
```

(If the effect already lists other deps, append `i18n.language` to that list — do not remove existing deps.)

- [ ] **Step 5: Lint and build the frontend**

Run: `cd frontend && npm run lint && npm run build`
Expected: lint passes (no new errors); build completes to `dist/`.

- [ ] **Step 6: Manual smoke check (documented, not automated)**

With `make dev` running and Timeplus provisioned: open `/rules`, switch the UI language to 中文, and confirm the installed-rule and catalog-card names/descriptions change to Chinese for translated rules and stay English for untranslated/custom rules; open a rule detail page and confirm the same. (This step is a checklist item; no code.)

- [ ] **Step 7: Commit**

```bash
git add frontend/src/api/client.js frontend/src/pages/RulePacks.jsx frontend/src/pages/RuleDetail.jsx
git commit -m "feat(rules-ui): send active locale and refetch rules on language change (#253)"
```

---

### Task 4: Seed content — zh-CN translations for all builtin rules + completeness guard

Add a `translations.zh-CN` block (name + description) to every builtin seed YAML, guarded by a test that fails until every builtin is translated with non-empty text.

**Files:**
- Modify: every `backend/internal/catalog/seed/rp-*.yaml` (add a `translations:` block)
- Test: `backend/internal/catalog/seed_translations_test.go`

**Interfaces:**
- Consumes (from Task 1): `Definition.TranslationFor("zh-CN")`, `catalog.SeedFS`, `catalog.New`, `catalog.OriginBuiltin`.

- [ ] **Step 1: Write the failing completeness + quality guard test**

Create `backend/internal/catalog/seed_translations_test.go`:

```go
package catalog

import "testing"

// TestSeedRulesHaveChineseTranslations asserts every builtin (embedded seed)
// rule carries a non-empty zh-CN name and description. It is the definition of
// done for the translation content pass and guards against regressions where a
// new seed rule is added without a translation.
func TestSeedRulesHaveChineseTranslations(t *testing.T) {
	m, err := New(SeedFS, t.TempDir())
	if err != nil {
		t.Fatalf("catalog.New(SeedFS): %v", err)
	}
	var missing []string
	for _, d := range m.All() {
		if d.Origin != OriginBuiltin {
			continue
		}
		tr, ok := d.TranslationFor("zh-CN")
		switch {
		case !ok:
			missing = append(missing, d.ID+" (no zh-CN block)")
		case tr.Name == "":
			missing = append(missing, d.ID+" (empty zh-CN name)")
		case tr.Description == "":
			missing = append(missing, d.ID+" (empty zh-CN description)")
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d builtin rules missing zh-CN translation:\n%v", len(missing), missing)
	}
}
```

- [ ] **Step 2: Run the guard to confirm it fails (nothing translated yet)**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/ -run TestSeedRulesHaveChineseTranslations -v`
Expected: FAIL, listing all ~125 rule IDs as missing.

- [ ] **Step 3: Translate every seed YAML**

For each `backend/internal/catalog/seed/rp-*.yaml`, append a top-level `translations` block translating the file's own `name` and `description` into Chinese. Worked example for `rp-001.yaml` (append at the end of the file, at column 0):

```yaml
translations:
  zh-CN:
    name: "提示注入防护"
    description: |
      检测跨 user_input 和 llm_request 界面的直接与间接提示注入攻击、越狱尝试和目标劫持。
      （……此处翻译该规则的完整 description 正文，保留所有 CVE 编号、URL、arXiv 引用、
      示例 payload 与技术名词原文，仅翻译说明性文字……）
```

Rules for the translation pass:
- Translate only prose. Preserve verbatim: CVE identifiers (e.g. `CVE-2026-3098`), URLs, arXiv IDs, OWASP codes, product/technique names, and any `Example:`/SQL/code fragments.
- Match the source `description` structure — if the English uses a literal block scalar (`|`), the zh-CN one does too; keep reference lists intact.
- Use the file's existing `name`/`description` as the source of truth (translate the current English, do not paraphrase from memory).
- This is bulk data entry over ~125 files. It MAY be executed via a batch translation pass (e.g. a translation subagent per file or a small script feeding the model), but the acceptance gate is the Step-1 guard test, not the mechanism.

- [ ] **Step 4: Run the guard test to verify it passes**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/ -run TestSeedRulesHaveChineseTranslations -v`
Expected: PASS (0 missing).

- [ ] **Step 5: Verify all seed YAML still parses and hashes are unchanged**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/catalog/`
Expected: `ok` — the whole catalog package (including any existing hash/golden tests) passes, proving the added translations did not alter definition hashes or break YAML loading.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/catalog/seed/ backend/internal/catalog/seed_translations_test.go
git commit -m "feat(catalog): zh-CN translations for all builtin rules (#253)"
```

---

### Task 5: Final verification

Full-stack verification that nothing regressed and the feature builds end-to-end.

**Files:** none (verification only).

- [ ] **Step 1: Backend build + full test suite**

Run: `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./...`
Expected: build succeeds; all packages report `ok`.

- [ ] **Step 2: Frontend lint + build**

Run: `cd frontend && npm run lint && npm run build`
Expected: lint clean; production build to `dist/`.

- [ ] **Step 3: Confirm export is unaffected**

Confirm no change was made to `ExportCatalog` / `mgr.ExportZip` and that export still emits canonical English (the `translations` blocks ride along inside each definition for round-trip, but base name/description are never localized on export). This is guaranteed by construction — Task 2 touched only the four read handlers — so this is a code-inspection checklist item, not a new test.

- [ ] **Step 4: Update issue reference (optional)**

If desired, note in the PR body that this implements the *rule catalog metadata* slice of #253 and lists the deferred slices (API errors, threat text, Sentry output, config-param labels).

---

## Self-Review

**Spec coverage:**
- §1 data model → Task 1 ✅
- §1 hash exclusion → Task 1 (`TestDefinition_Hash_IgnoresTranslations`) ✅
- §2 server-side resolution overlay → Task 1 (`Localized`) + Task 2 (handlers) ✅
- §3 `?locale=` conveyance + validation → Task 2 (`resolveLocale`) + Task 3 (`withLocale`) ✅
- §4 endpoint coverage (catalog list, catalog detail, installed list, detail; export unchanged) → Task 2 + Task 5 Step 3 ✅
- §4 installed-rule read-time overlay → Task 2 Steps 7–8 ✅
- §5 frontend threading + refetch → Task 3 ✅
- §6 content: all 125 zh-CN → Task 4 ✅
- §7 testing (Localized, hash guard, handler locale, seed guard) → Tasks 1, 2, 4 ✅

**Placeholder scan:** The only non-literal content is the bulk translation of 125 descriptions in Task 4 Step 3 — inherently generated data, gated by a full-code completeness test (Task 4 Step 1). A worked example block and explicit preservation rules are provided. No `TBD`/`TODO`/"add error handling"/"similar to Task N" placeholders elsewhere.

**Type consistency:** `Translation{Name, Description}`, `Translations map[string]Translation`, `TranslationFor(locale) (Translation, bool)`, `Localized(locale) Definition`, `resolveLocale(c) string`, `withLocale(path) string` — used consistently across Tasks 1–4. `InstanceView` fields (`Instance`, `Definition`, `InCatalog`) match `internal/rules/service.go`. `RulePack`/`RuleInstance` `Name`/`Description` fields match `models.go`/`types.go`.
