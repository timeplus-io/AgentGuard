# i18n v1: Switchable Chinese UI

**Issue:** [#248](https://github.com/timeplus-io/AgentGuard/issues/248)
**Date:** 2026-07-28
**Status:** Approved design

## Goal

The AgentGuard SPA is English-only — every label, button, heading, and message is a
hardcoded English string in JSX. This adds internationalization (i18n) infrastructure, a
user-facing language selector under **Settings**, and a complete **Simplified Chinese
(`zh-CN`)** translation of the static UI. The framework must make a third locale a matter
of dropping in a catalog, with no page code changes.

**v1 scope:** static UI chrome only (frontend). Backend/API-originated content is out of
scope (see below).

## Non-goals (v1)

- Localizing backend/API-originated content: threat descriptions, rule-catalog names/
  descriptions, Sentry LLM output, server error messages from `/api/*`.
- RTL layout support.
- Per-user **server-side** locale persistence (client `localStorage` is sufficient for v1).
- Translating `docs/`, `spec/`, or agent-plugin output.

## Library

**`react-i18next` + `i18next`** — the React-standard i18n stack: `useTranslation()` hooks,
namespaced catalogs, `{{interpolation}}`, pluralization, `<Trans>` for embedded markup, and
`Intl`-backed formatters. Rejected alternatives: `react-intl`/FormatJS (heavier ICU
boilerplate for our needs) and a hand-rolled React Context (reinvents pluralization/
formatting across 17 pages).

New frontend deps: `i18next`, `react-i18next`. No `i18next-browser-languagedetector` — we
implement resolution explicitly (see Locale resolution) because the server-config default
is not something the stock detector knows about.

## Architecture

### Init

`frontend/src/i18n/index.js` initializes i18next once (resources, fallback `en`, interpolation
config) and is imported at the top of `frontend/src/main.jsx` before the app renders. The app
tree does **not** need an explicit provider wrapper — `react-i18next` binds to the default
i18next instance — but we set the initial locale during init from the resolver below.

### Catalog structure

```
frontend/src/i18n/
  index.js                     # init + locale resolution + setLocale()
  locales/
    GLOSSARY.md                # security-term translation glossary (source of truth for terms)
    en/
      common.json  dashboard.json  agents.json  threats.json  rules.json
      cost.json  dlp.json  sentry.json  settings.json  onboarding.json  auth.json
    zh-CN/
      (same set of namespace files, identical key sets)
```

- `en` is the **source of truth**. `zh-CN` mirrors its key set exactly.
- Namespaces map roughly to page/domain. `common` holds cross-cutting strings (actions like
  save/cancel/delete, status words, pagination, generic empty/error states).
- **Key convention:** semantic dotted paths, never English-derived. E.g.
  `threats.emptyState.title`, `common.actions.save`, `rules.policy.autoBlock`.

### Locale resolution (precedence)

Resolved once at startup, and re-applied when the user changes it:

1. **User choice** — `localStorage['agentguard.locale']`, set by the Settings switcher.
   Once set, it always wins.
2. **Server config default** — `ui.default_language` from `GET /api/config`. Empty/unset ⇒ skip.
3. **Fallback** — `en`.

Because `/api/config` is async, boot uses steps 1 & 3 synchronously (localStorage or `en`) so
first paint is never blocked; when the config response arrives, if the user has **no**
localStorage choice and `ui.default_language` is set and differs, the app switches to it. In
practice most authenticated app shells already fetch config early; we reuse that.

The resolved locale is written to `document.documentElement.lang`.

### Language switcher (Settings only)

A selector on the Settings page (English / 简体中文). Selecting a language:
- calls `i18n.changeLanguage(locale)`,
- writes `localStorage['agentguard.locale']`,
- updates `<html lang>`.

No TopNav/Sidebar change. Live switch — no reload.

### Locale-aware formatting

Replace ad-hoc date/number formatting with helpers bound to the active locale:
- `Intl.DateTimeFormat(locale, …)` for timestamps/dates.
- `Intl.NumberFormat(locale, …)` for counts, costs, percentages.
- Relative times via `Intl.RelativeTimeFormat(locale, …)` (or i18next formatter) where the UI
  shows "3 minutes ago" style strings.

A small `frontend/src/i18n/format.js` exposes `formatDate`, `formatNumber`, `formatCurrency`,
`formatRelativeTime` that read the current i18next language.

## Backend change (minimal)

The **only** backend change in v1: a new config key **`ui.default_language`**.

- Added to defaults in `internal/config/config.go` (default `""` ⇒ English).
- Surfaced by the existing `GET /api/config` handler alongside other keys (per-key source
  reporting as usual). No new endpoint, no Timeplus stream, no reprovision, not a restart key.
- `PUT /api/config` can set it like any other hot-applied key (optional for v1; the value can
  also just live in `config.yaml`).

No changes to any stream, MV, or provisioning.

## Full-sweep string extraction

Convert all hardcoded UI strings to `t('ns.key')`, page by page, so each page is an independently
reviewable unit. Covered surfaces:

- **Pages** (`frontend/src/pages/`): Dashboard, AgentList, AgentDetail, Threats, ThreatDetail,
  CostGovernance, RulePacks, RuleCreate, RuleDetail, Investigate, SemanticDLP, Sentry, Settings,
  Setup, Onboarding, Login, AccessControl.
- **Shared components** (`frontend/src/components/`): Sidebar, TopNav, Footer, PageHeader, Badge
  (Severity/Status label text), Toggle labels, HoldToast, NotificationsProvider messages,
  ActivityDrawer/ActivityLog, Live* feeds, DLP/Sentry config editors and wizards, NoAccess,
  and the `components/rules` + `components/settings` subtrees.

Rules for extraction:
- Dynamic/interpolated strings use i18next interpolation: `t('cost.overBudgetBy', { amount })`.
- Pluralized counts use i18next plural keys (`_one`/`_other`).
- Chrome inside otherwise-dynamic surfaces **is** translated (Sentry chat labels/buttons,
  Onboarding wizard steps) even though their streamed/model content is not.

## Chinese translation

I author the full `zh-CN` catalog from the `en` source, governed by `GLOSSARY.md` for consistent
security/streaming terminology. Seed glossary entries (extend during translation):

| English | 简体中文 |
|---|---|
| Threat | 威胁 |
| Hold | 挂起 |
| Rule | 规则 |
| Materialized view | 物化视图 |
| Budget | 预算 |
| Session | 会话 |
| Agent | 代理 |
| Cost / Governance | 成本 / 治理 |
| DLP | 数据防泄露 (DLP) |
| Dashboard | 仪表盘 |

Proper nouns kept as-is: AgentGuard, Timeplus, Sentry, Claude Code, OpenClaw, OTel.
A native speaker can refine later; translations are not frozen.

## Testing

- **Key-parity test** (vitest): asserts `en` and `zh-CN` expose identical key sets across all
  namespaces — guards against drift when strings are added.
- **Render smoke test**: mounts a component under a forced `zh-CN` locale and asserts a known key
  renders its Chinese value (and that switching language updates it).
- Existing tests must stay green; extraction is behavior-preserving for English.

## Acceptance criteria

- [ ] `react-i18next` wired up; `useTranslation()` renders from catalogs.
- [ ] Settings language selector switches the whole UI between English and 简体中文, live, no reload.
- [ ] Every static UI string on all pages renders translated under `zh-CN` — no English leaking in the chrome.
- [ ] Locale precedence works: localStorage choice > `ui.default_language` config > English.
- [ ] `<html lang>` reflects the active locale; dates/numbers/relative-times format per-locale.
- [ ] `en` and `zh-CN` have identical key sets (guarded by a test).
- [ ] Adding a third language needs only a new locale catalog + a switcher entry — no page code changes.
- [ ] `ui.default_language` config key exists, defaults to English, and is read by the frontend via `/api/config`.

## Rollout / follow-ups

- Follow-up issue: localize backend/API-originated content (threat text, rule-catalog metadata,
  server errors) once term glossary is settled.
- Follow-up: additional locales as demand appears (framework already supports them).
