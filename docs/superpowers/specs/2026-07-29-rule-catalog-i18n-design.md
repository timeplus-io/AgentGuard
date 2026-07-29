# Localized rule name & description (issue #253 slice)

**Date:** 2026-07-29
**Issue:** [#253 — i18n follow-up: localize backend/API-originated content](https://github.com/timeplus-io/AgentGuard/issues/253)
**Scope of this spec:** the *rule catalog metadata* slice of #253 — specifically the builtin rule **name** and **description**. Everything else in #253 (API error messages, threat text, Sentry output, config-param labels/help) is explicitly **out of scope** here.

## Background

#248 shipped i18n for the static UI chrome: all frontend JSX literals render from `en` / `zh-CN` catalogs. Backend/API-originated strings were out of scope for that pass and still render in English.

Rule `name` and `description` originate in the backend catalog. They live in the seed YAML files under `backend/internal/catalog/seed/` (~125 builtin rules), are served through the rules/catalog API, and render directly in the UI (`r.name`, `r.desc`) — bypassing the frontend i18n system entirely. When a user switches the UI to Chinese, rule names and descriptions stay English.

## Decisions (from brainstorming)

1. **Where translations live:** in the catalog YAML (backend-owned), co-located with the rule content via a `translations` map keyed by locale.
2. **API delivery:** server-side resolution — the backend overwrites the `name`/`description` fields with the localized text based on the request locale, and does **not** expose the raw translations map on read endpoints.
3. **Locale conveyance:** an explicit `?locale=` query param carrying the active UI locale (not `Accept-Language`).
4. **Content scope:** produce zh-CN `name` + `description` for **all ~125 builtin rules** in this pass.
5. **Installed rules:** localize via a read-time overlay from the current catalog (confirmed recommendation), not by changing the installed-snapshot schema.

## Design

### 1. Data model — `translations` in the catalog Definition

Add an optional field to `catalog.Definition` (`backend/internal/catalog/definition.go`):

```go
type Definition struct {
    ...
    Description  string
    ...
    Translations map[string]Translation `yaml:"translations,omitempty" json:"translations,omitempty"`
}

// Translation holds locale-specific display text for a rule definition.
// Any empty field falls back to the base (English) Definition field.
type Translation struct {
    Name        string `yaml:"name"        json:"name"`
    Description string `yaml:"description" json:"description"`
}
```

- Keyed by locale string (`"zh-CN"`).
- Authored in seed YAML alongside the English `name`/`description`.
- A locale entry may translate name only, description only, or both; anything absent falls back to English.

**`Translations` is excluded from `Definition.Hash()`.** The `hashPayload` struct (definition.go) deliberately omits provenance fields (`Origin`, `Author`); `Translations` joins them. Rationale: the hash drives `definition_hash` and the "upgradeable" comparison (`rules.CanonicalDefinitionHash(def) != inst.DefinitionHash`). If translations fed the hash, adding a zh-CN block to a builtin would flip every installed rule's hash and falsely flag it as upgradeable. Translations are presentation metadata with no behavioral effect.

No changes to SQL templates, `rendered_sql`, MVs, `agentguard_security_events`, or the `agentguard_rules` snapshot schema. The baked `rule_name` inside security events stays English (that is the deferred "threat text" item).

### 2. Resolution — server-side, presentation-only overlay

A helper on the catalog side:

```go
// Localized returns a copy of d with Name/Description replaced by the locale's
// translation when present (per-field fallback to the base value), and with the
// Translations map cleared so it never leaks onto a read response. An empty or
// unknown locale, or a missing entry, returns the base fields unchanged.
func (d Definition) Localized(locale string) Definition
```

- Read handlers resolve the request locale, call `Localized`, and return the result.
- The raw `translations` map is stripped from every read response (small payload, no drift — matches decision #2).
- `omitempty` on the JSON tag keeps the field absent from responses once cleared, and absent from the JSON surface of definitions that carry no translations.

### 3. Locale conveyance — explicit `?locale=` query param

- Frontend sends its **active UI locale** (`i18n.language`, the deliberate localStorage choice) as `?locale=zh-CN`.
- Backend validates against the known supported set. Reuse the existing vocabulary: `ui.default_language` is already a `TypeEnum` with `EnumValues: ["en", "zh-CN"]` (`internal/config/registry.go`). A small helper validates the param and defaults anything unknown/absent to `"en"`.
- Not `Accept-Language`: the UI locale is a user setting that can differ from the browser's language.

### 4. Endpoint coverage

| Endpoint | Handler | Change |
|---|---|---|
| `GET /api/rules/catalog` | `ListCatalog` (rules.go) | Resolve each catalog entry's name/desc by locale |
| `GET /api/rules/catalog/:id` | `GetCatalogRule` (catalog.go) | Resolve + strip translations map |
| `GET /api/rules` | `ListRules` (rules.go) | Overlay localized name/desc from the **current catalog** by `rule_id` |
| `GET /api/rules/:id` | `GetRule` (rules.go) | Overlay on both `instance` and `definition` in the response |
| `GET /api/rules/catalog/export` | `ExportCatalog` (catalog.go) | **Unchanged** — canonical English, translations preserved for round-trip |

**Installed rules overlay.** Installed instances (`rules.RuleInstance`) carry a frozen English `Name`/`Description` snapshot; `instanceToPack` copies them into the `RulePack`. To localize the installed list/detail without a stream-schema change, the handler looks up the current catalog definition by `rule_id` (`mgr.Lookup`), and if a translation exists for the locale, overlays the localized name/description onto the response. Custom rules and rules no longer in the catalog fall back to their stored snapshot name.

- No `agentguard_rules` schema change (respects the project's "clean reprovision, no dual-write" rule).
- Accepted edge case: if a builtin's base name changed post-install without an upgrade, the localized name follows the current catalog rather than the stale snapshot. This only manifests when a translation is present, and only for the display string.

**Export stays English on purpose:** exported catalogs must be portable and diffable in a single canonical language; the raw `translations` map rides along inside each definition for round-trip, but the base `name`/`description` are never overwritten on export.

### 5. Frontend wiring

- `api/client.js`: append `?locale=${i18n.language}` to the four GET calls (`getRules`, `getRule`, `getRulesCatalog`, `getCatalogRule`). A single helper builds the query so the locale source is defined once.
- `RulePacks.jsx` / `RuleDetail.jsx`: add the active locale (`i18n.language`) to the fetch `useEffect` dependency arrays so switching language **refetches** and re-renders localized text.
- No per-field `t()` calls for rule name/description — the fields arrive pre-localized from the API. This keeps the frontend rendering unchanged apart from the refetch trigger.

### 6. Content

- Author zh-CN `name` + `description` into the `translations.zh-CN` block of all ~125 seed YAML files under `backend/internal/catalog/seed/`.
- Machine-assisted translation, then human review. Technical terms, CVE identifiers, URLs, and reference citations are preserved verbatim; only prose is translated.
- Config-param labels/help are **out of scope** (this pass is name + description only).
- Threat descriptions and Sentry model output remain English (deferred by the issue).

### 7. Testing

- **Catalog unit tests** (`definition_test.go`):
  - `Localized("zh-CN")` swaps both fields when present; per-field fallback when only one is present; returns base fields for unknown/empty locale; clears the `Translations` map on the returned copy.
  - **Hash regression guard:** a definition's `Hash()` is identical with and without a `translations` block added.
- **Handler tests** (`catalog_test.go`, `rules_handler_test.go`):
  - `?locale=zh-CN` returns zh text on catalog list, catalog detail, installed list, and rule detail.
  - Unknown / missing `locale` returns English.
  - Export response is unchanged English regardless of `?locale`.
  - Installed-rule overlay: a rule with a zh translation shows zh; a custom rule with no catalog entry falls back to its snapshot name.
- **Seed guard test:** every seed rule that declares a `translations.zh-CN` block has non-empty `name` and `description`, and the YAML parses. (Does not require every rule to be translated in code — guards quality of the ones that are.)

## Out of scope

- API error messages, threat descriptions, Sentry LLM output, onboarding/generated copy (other #253 slices).
- Config-parameter labels and help text.
- RTL layout, server-side per-user locale persistence, `Accept-Language` negotiation, docs/spec translation, agent-plugin output.
- Any change to SQL templates, MVs, security-event content, or the installed-rule snapshot schema.
