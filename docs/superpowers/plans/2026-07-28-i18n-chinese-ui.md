# i18n v1: Switchable Chinese UI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add react-i18next infrastructure, a Settings language switcher, a `ui.default_language` server-config default, and a complete Simplified Chinese (`zh-CN`) translation of all static UI chrome.

**Architecture:** i18next is initialized once in `src/i18n/index.js` (imported at the top of `main.jsx`). UI strings become `t('ns.key')` calls resolved from namespaced JSON catalogs under `src/i18n/locales/{en,zh-CN}/`. Active locale resolves as: `localStorage` choice → `ui.default_language` from `GET /api/config` → English. A `LanguageSelector` on the Settings page (personal, localStorage-backed, available to all roles) is separate from the admin-only `ui.default_language` config key. `<html lang>` tracks the active locale. Dates/numbers/relative-times route through `Intl` helpers in `src/i18n/format.js`. Design precedent: the existing light/dark `ThemeContext` (#240).

**Tech Stack:** React 18 + Vite, react-i18next + i18next, Vitest, Go/Gin + Viper (config only), Tailwind.

**Spec:** `docs/superpowers/specs/2026-07-28-i18n-chinese-ui-design.md` · **Issue:** #248

## Global Constraints

- Frontend deps added: `i18next`, `react-i18next` ONLY. Do NOT add `i18next-browser-languagedetector` — resolution is explicit.
- All Go builds/tests use `CGO_ENABLED=0` (macOS 26 static-binary requirement).
- `en` is the source-of-truth catalog. `en` and `zh-CN` MUST have identical key sets across every namespace (guarded by `keyParity.test.js`).
- Translation keys are semantic dotted paths, never English-derived: `threats.emptyState.title`, `common.actions.save`.
- Namespaces: `common`, `auth`, `dashboard`, `agents`, `threats`, `rules`, `cost`, `dlp`, `sentry`, `settings`, `onboarding`.
- Proper nouns are NEVER translated: AgentGuard, Timeplus, Sentry, Claude Code, OpenClaw, OTel, DLP (keep acronym, gloss once).
- Backend/API-originated strings are OUT OF SCOPE (threat text, rule-catalog metadata, Sentry model output, server errors).
- Follow the "Digital Sentry" design tokens; do not introduce new colors/border styles for i18n UI.
- Each page task adds BOTH `en` and `zh-CN` keys in the same commit so the parity test never goes red.

## Canonical Extraction Procedure (referenced by every Phase-2 task)

For each file listed in a Phase-2 task, apply this exact transform:

1. Add `import { useTranslation } from 'react-i18next'` and, inside the component, `const { t } = useTranslation('<namespace>')`. (For non-component modules, import the shared instance: `import i18n from '../i18n'` and call `i18n.t('<namespace>:<key>')`.)
2. For every user-visible literal string in JSX (headings, labels, button text, placeholders, `aria-label`, `title`, empty/error/loading states, toast text, option labels), add a key to `locales/en/<namespace>.json` and replace the literal with `t('key')`.
3. Interpolated strings use i18next interpolation, e.g. `t('overBudgetBy', { amount })` with `"overBudgetBy": "Over budget by {{amount}}"`. Count-dependent strings use plural suffixes: `"items_one": "{{count}} item"`, `"items_other": "{{count}} items"`, called as `t('items', { count })`.
4. Add the SAME keys to `locales/zh-CN/<namespace>.json` with Simplified-Chinese values, following `GLOSSARY.md`. Proper nouns stay Latin.
5. Route any date/number formatting in the file through `src/i18n/format.js` helpers (`formatDate`, `formatNumber`, `formatCurrency`, `formatRelativeTime`).
6. Do NOT translate: values from API responses, code/SQL snippets, stream/column names, env-var names, URLs.

**Per-task verification** (identical for every Phase-2 task, run at the end):
- `cd frontend && npx vitest run src/i18n/keyParity.test.js` → PASS (en/zh-CN key sets identical).
- `cd frontend && npx vitest run src/i18n/translated.test.js` → PASS (the task's representative keys exist and zh-CN differs from en).
- `cd frontend && npm run build` → succeeds.
- Residual-literal heuristic on the task's files (expect no substantive English JSX text):
  `git diff --name-only origin/main -- 'frontend/src/**/*.jsx' | xargs grep -nE '>[A-Z][a-z]+ [a-z]' || echo "clean"` — review any hits; API-derived/proper-noun hits are acceptable.

---

## Phase 1 — Foundation

### Task 1: Add deps + i18next init + wire into app

**Files:**
- Modify: `frontend/package.json` (dependencies)
- Create: `frontend/src/i18n/index.js`
- Create: `frontend/src/i18n/locales/en/common.json`
- Create: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/main.jsx`
- Test: `frontend/src/i18n/init.test.js`

**Interfaces:**
- Produces: default export `i18n` (the configured i18next instance) from `src/i18n/index.js`; side-effect of importing it initializes i18next with `en`+`zh-CN` resources, `fallbackLng: 'en'`, namespaces, `interpolation.escapeValue: false`.

- [ ] **Step 1: Install deps**

Run: `cd frontend && npm install i18next@^23 react-i18next@^14`
Expected: `package.json` gains both in `dependencies`; `npm install` exits 0.

- [ ] **Step 1b: Set up the DOM test environment (foundation for T4/T6)**

The repo currently has NO vitest config and NO DOM test libs; existing tests
are pure-JS under the default `node` environment. Later i18n tasks need a DOM
(`localStorage` in T4, component render in T6). Add them now, once.

Run: `cd frontend && npm install -D jsdom @testing-library/react @testing-library/jest-dom @testing-library/user-event`

Create `frontend/vitest.config.js`:
```js
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: false,
    setupFiles: ['./src/test/setup.js'],
  },
})
```
Create `frontend/src/test/setup.js`:
```js
import '@testing-library/jest-dom'
```
Verify existing pure-JS tests still pass under jsdom:
Run: `cd frontend && npx vitest run`
Expected: existing suite green (jsdom is a superset for node-style tests).

- [ ] **Step 2: Seed catalogs**

`frontend/src/i18n/locales/en/common.json`:
```json
{
  "appName": "AgentGuard",
  "actions": { "save": "Save", "cancel": "Cancel" }
}
```
`frontend/src/i18n/locales/zh-CN/common.json`:
```json
{
  "appName": "AgentGuard",
  "actions": { "save": "保存", "cancel": "取消" }
}
```

- [ ] **Step 3: Write the failing test**

`frontend/src/i18n/init.test.js`:
```js
import { describe, it, expect } from 'vitest'
import i18n from './index.js'

describe('i18n init', () => {
  it('defaults to English and resolves a common key', () => {
    expect(i18n.t('common:actions.save')).toBe('Save')
  })
  it('switches to Simplified Chinese', async () => {
    await i18n.changeLanguage('zh-CN')
    expect(i18n.t('common:actions.save')).toBe('保存')
    await i18n.changeLanguage('en')
  })
})
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/i18n/init.test.js`
Expected: FAIL — cannot resolve `./index.js`.

- [ ] **Step 5: Implement `src/i18n/index.js`**

```js
import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import enCommon from './locales/en/common.json'
import zhCommon from './locales/zh-CN/common.json'

export const SUPPORTED_LOCALES = ['en', 'zh-CN']
export const DEFAULT_LOCALE = 'en'

// Namespaces are registered here as page tasks add catalogs. common is the base.
const resources = {
  en: { common: enCommon },
  'zh-CN': { common: zhCommon },
}

i18n.use(initReactI18next).init({
  resources,
  lng: DEFAULT_LOCALE,
  fallbackLng: DEFAULT_LOCALE,
  defaultNS: 'common',
  ns: ['common'],
  interpolation: { escapeValue: false }, // React already escapes
  returnEmptyString: false,
  initImmediate: false, // synchronous init so t() works immediately (tests + first paint)
})

export default i18n
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/i18n/init.test.js`
Expected: PASS.

- [ ] **Step 7: Wire into `main.jsx`**

Add as the FIRST import (before `App`), so i18next is ready before any component renders:
```js
import './i18n'
```
(Insert directly under `import ReactDOM from 'react-dom/client'`.)

- [ ] **Step 8: Verify build**

Run: `cd frontend && npm run build`
Expected: succeeds.

- [ ] **Step 9: Commit**

```bash
git add frontend/package.json frontend/package-lock.json frontend/vitest.config.js frontend/src/test frontend/src/i18n frontend/src/main.jsx
git commit -m "feat(i18n): add react-i18next infra + jsdom test env with en/zh-CN base catalog (#248)"
```

---

### Task 2: Locale-aware formatting helpers

**Files:**
- Create: `frontend/src/i18n/format.js`
- Test: `frontend/src/i18n/format.test.js`

**Interfaces:**
- Produces: `formatDate(value, opts?)`, `formatNumber(value, opts?)`, `formatCurrency(value, currency='USD')`, `formatRelativeTime(value, nowMs?)` — each reads the active locale from `i18n.language`.

- [ ] **Step 1: Write the failing test**

`frontend/src/i18n/format.test.js`:
```js
import { describe, it, expect, afterEach } from 'vitest'
import i18n from './index.js'
import { formatNumber, formatCurrency } from './format.js'

afterEach(async () => { await i18n.changeLanguage('en') })

describe('format helpers', () => {
  it('formats numbers with the active locale grouping', () => {
    expect(formatNumber(1234567)).toBe('1,234,567')
  })
  it('formats currency', () => {
    expect(formatCurrency(12.5)).toBe('$12.50')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/i18n/format.test.js`
Expected: FAIL — cannot resolve `./format.js`.

- [ ] **Step 3: Implement `src/i18n/format.js`**

```js
import i18n from './index.js'

const locale = () => i18n.language || 'en'

export function formatDate(value, opts = { dateStyle: 'medium', timeStyle: 'short' }) {
  const d = value instanceof Date ? value : new Date(value)
  if (isNaN(d.getTime())) return ''
  return new Intl.DateTimeFormat(locale(), opts).format(d)
}

export function formatNumber(value, opts = {}) {
  if (value == null || isNaN(value)) return ''
  return new Intl.NumberFormat(locale(), opts).format(value)
}

export function formatCurrency(value, currency = 'USD') {
  if (value == null || isNaN(value)) return ''
  return new Intl.NumberFormat(locale(), { style: 'currency', currency }).format(value)
}

const REL_STEPS = [
  ['year', 31536000], ['month', 2592000], ['day', 86400],
  ['hour', 3600], ['minute', 60], ['second', 1],
]

export function formatRelativeTime(value, nowMs) {
  const then = (value instanceof Date ? value : new Date(value)).getTime()
  if (isNaN(then)) return ''
  const now = nowMs ?? Date.now()
  let delta = Math.round((then - now) / 1000)
  const rtf = new Intl.RelativeTimeFormat(locale(), { numeric: 'auto' })
  for (const [unit, secs] of REL_STEPS) {
    if (Math.abs(delta) >= secs || unit === 'second') {
      return rtf.format(Math.round(delta / secs), unit)
    }
  }
  return rtf.format(delta, 'second')
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/i18n/format.test.js`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/i18n/format.js frontend/src/i18n/format.test.js
git commit -m "feat(i18n): locale-aware Intl format helpers (#248)"
```

---

### Task 3: Backend `ui.default_language` config key

**Files:**
- Modify: `backend/internal/config/config.go` (default; ~line 143 area)
- Modify: `backend/internal/config/registry.go` (new section + key)
- Test: `backend/internal/config/registry_test.go`

**Interfaces:**
- Produces: config key `ui.default_language` (viper), enum `["en","zh-CN"]`, default `"en"`, section `ui`; surfaced by the existing `GET /api/config` with no other handler change.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/config/registry_test.go`:
```go
func TestUIDefaultLanguageRegistered(t *testing.T) {
	var found *KeyMeta
	for i := range Registry() {
		if Registry()[i].Key == "ui.default_language" {
			found = &Registry()[i]
			break
		}
	}
	if found == nil {
		t.Fatal("ui.default_language not in registry")
	}
	if found.Type != TypeEnum {
		t.Errorf("want TypeEnum, got %s", found.Type)
	}
	want := []string{"en", "zh-CN"}
	if len(found.EnumValues) != len(want) {
		t.Fatalf("enum values = %v", found.EnumValues)
	}
	hasUI := false
	for _, s := range Sections() {
		if s.ID == "ui" {
			hasUI = true
		}
	}
	if !hasUI {
		t.Error("ui section not registered")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/ -run TestUIDefaultLanguageRegistered`
Expected: FAIL — `ui.default_language not in registry`.

- [ ] **Step 3: Add the default**

In `backend/internal/config/config.go`, next to the other `SetDefault` calls (after line 145 `agentguard.url`):
```go
	v.SetDefault("ui.default_language", "en")
```

- [ ] **Step 4: Register section + key**

In `backend/internal/config/registry.go`, add to the `Sections()` slice (before `{"setup", "Setup"}`):
```go
		{"ui", "User Interface"},
```
And add to the `Registry()` slice (after the `agentguard` block):
```go
		// ── ui ──
		{Key: "ui.default_language", Section: "ui", Label: "Default UI language",
			Description: "Language shown to users who haven't picked one in their browser.",
			Type: TypeEnum, EnumValues: []string{"en", "zh-CN"}},
```

- [ ] **Step 5: Run test + full config suite**

Run: `cd backend && CGO_ENABLED=0 go test ./internal/config/`
Expected: PASS (including existing registry tests).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/registry.go backend/internal/config/registry_test.go
git commit -m "feat(i18n): ui.default_language config key surfaced via /api/config (#248)"
```

---

### Task 4: Locale resolution + `<html lang>`

**Files:**
- Create: `frontend/src/i18n/resolveLocale.js`
- Modify: `frontend/src/i18n/index.js` (apply resolved locale at boot)
- Test: `frontend/src/i18n/resolveLocale.test.js`

**Interfaces:**
- Consumes: `SUPPORTED_LOCALES`, `DEFAULT_LOCALE` from `./index.js`; `api.getConfig()` from `../api/client`.
- Produces: `readStoredLocale()`, `storeLocale(locale)`, `applyLocale(locale)` (calls `i18n.changeLanguage` + sets `document.documentElement.lang`), `resolveInitialLocale()` (sync: localStorage else DEFAULT), `adoptServerDefault()` (async: if no stored choice, read `ui.default_language` from config and apply).

- [ ] **Step 1: Write the failing test**

`frontend/src/i18n/resolveLocale.test.js`:
```js
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { readStoredLocale, storeLocale, resolveInitialLocale } from './resolveLocale.js'

beforeEach(() => localStorage.clear())

describe('locale resolution', () => {
  it('returns null when nothing stored', () => {
    expect(readStoredLocale()).toBeNull()
  })
  it('round-trips a stored, supported locale', () => {
    storeLocale('zh-CN')
    expect(readStoredLocale()).toBe('zh-CN')
  })
  it('ignores an unsupported stored value', () => {
    localStorage.setItem('agentguard.locale', 'fr')
    expect(readStoredLocale()).toBeNull()
  })
  it('resolveInitialLocale falls back to English with no storage', () => {
    expect(resolveInitialLocale()).toBe('en')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/i18n/resolveLocale.test.js`
Expected: FAIL — cannot resolve `./resolveLocale.js`.

- [ ] **Step 3: Implement `src/i18n/resolveLocale.js`**

```js
import i18n, { SUPPORTED_LOCALES, DEFAULT_LOCALE } from './index.js'
import { api } from '../api/client'

const KEY = 'agentguard.locale'

export function readStoredLocale() {
  try {
    const v = localStorage.getItem(KEY)
    return SUPPORTED_LOCALES.includes(v) ? v : null
  } catch { return null }
}

export function storeLocale(locale) {
  try { localStorage.setItem(KEY, locale) } catch { /* storage unavailable */ }
}

export function applyLocale(locale) {
  const loc = SUPPORTED_LOCALES.includes(locale) ? locale : DEFAULT_LOCALE
  i18n.changeLanguage(loc)
  try { document.documentElement.lang = loc } catch { /* no document */ }
  return loc
}

export function resolveInitialLocale() {
  return readStoredLocale() || DEFAULT_LOCALE
}

// After auth/config are available: if the user hasn't chosen, adopt the
// deployment default from ui.default_language. Fail-open to English.
export async function adoptServerDefault() {
  if (readStoredLocale()) return
  try {
    const cfg = await api.getConfig()
    const key = (cfg.sections || [])
      .flatMap((s) => s.keys || [])
      .find((k) => k.key === 'ui.default_language')
    const val = key && key.value
    if (val && SUPPORTED_LOCALES.includes(val)) applyLocale(val)
  } catch { /* not authenticated / config unavailable — stay on English */ }
}
```

- [ ] **Step 4: Apply resolved locale at boot in `index.js`**

Because `init()` runs synchronously (`initImmediate: false`), apply the stored
locale directly right after the `init(...)` call — do NOT use an
`i18n.on('initialized', …)` listener, which would attach after the event has
already fired and never run. Do it inline (no import of `resolveLocale`, to
avoid a circular import), before `export default i18n`:
```js
// Apply the synchronous initial locale: localStorage choice, else English.
// Async server-default adoption (ui.default_language) happens in App after auth.
try {
  const stored = localStorage.getItem('agentguard.locale')
  const loc = ['en', 'zh-CN'].includes(stored) ? stored : 'en'
  i18n.changeLanguage(loc)
  if (typeof document !== 'undefined') document.documentElement.lang = loc
} catch { /* storage/document unavailable — stays on English */ }
```

- [ ] **Step 5: Adopt server default after auth**

In `frontend/src/App.jsx`, import and call once on mount (inside the authenticated app tree — place next to existing top-level effects):
```js
import { adoptServerDefault } from './i18n/resolveLocale'
// ...inside the App component body:
useEffect(() => { adoptServerDefault() }, [])
```
(If `App.jsx` has no top-level component body for effects, add the effect to the nearest always-mounted authenticated layout component; the call is idempotent and fail-open.)

- [ ] **Step 6: Run tests + build**

Run: `cd frontend && npx vitest run src/i18n/resolveLocale.test.js && npm run build`
Expected: PASS + build succeeds.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/i18n/resolveLocale.js frontend/src/i18n/resolveLocale.test.js frontend/src/i18n/index.js frontend/src/App.jsx
git commit -m "feat(i18n): locale resolution (localStorage > config > en) + html lang (#248)"
```

---

### Task 5: Glossary + key-parity + translated-keys tests

**Files:**
- Create: `frontend/src/i18n/locales/GLOSSARY.md`
- Create: `frontend/src/i18n/keyParity.test.js`
- Create: `frontend/src/i18n/translated.test.js`

**Interfaces:**
- Produces: `keyParity.test.js` (dynamically imports every `en/*.json` + `zh-CN/*.json` and asserts identical flattened key sets); `translated.test.js` (asserts representative keys per namespace are non-empty and zh-CN ≠ en). Both use `import.meta.glob` so new namespace files are picked up automatically.

- [ ] **Step 1: Write GLOSSARY.md**

`frontend/src/i18n/locales/GLOSSARY.md`:
```markdown
# zh-CN translation glossary

Proper nouns (never translated): AgentGuard, Timeplus, Sentry, Claude Code, OpenClaw, OTel.

| English | 简体中文 |
|---|---|
| Threat | 威胁 |
| Hold | 挂起 |
| Rule | 规则 |
| Materialized view | 物化视图 |
| Budget | 预算 |
| Session | 会话 |
| Agent | 代理 |
| Cost | 成本 |
| Governance | 治理 |
| Dashboard | 仪表盘 |
| Settings | 设置 |
| DLP | 数据防泄露 (DLP) |
| Severity | 严重程度 |
| Acknowledge | 确认 |
| Clear | 清除 |
| Approve / Deny | 批准 / 拒绝 |
```

- [ ] **Step 2: Write `keyParity.test.js`**

```js
import { describe, it, expect } from 'vitest'

const en = import.meta.glob('./locales/en/*.json', { eager: true })
const zh = import.meta.glob('./locales/zh-CN/*.json', { eager: true })

function flatten(obj, prefix = '') {
  return Object.entries(obj).flatMap(([k, v]) => {
    const key = prefix ? `${prefix}.${k}` : k
    return v && typeof v === 'object' && !Array.isArray(v) ? flatten(v, key) : [key]
  })
}
const nsKeys = (mods) => {
  const out = {}
  for (const [path, mod] of Object.entries(mods)) {
    const ns = path.split('/').pop().replace('.json', '')
    out[ns] = new Set(flatten(mod.default))
  }
  return out
}

describe('en/zh-CN key parity', () => {
  it('has the same namespaces and keys', () => {
    const e = nsKeys(en), z = nsKeys(zh)
    expect(Object.keys(e).sort()).toEqual(Object.keys(z).sort())
    for (const ns of Object.keys(e)) {
      expect([...z[ns]].sort(), `missing keys in zh-CN/${ns}`).toEqual([...e[ns]].sort())
    }
  })
})
```

- [ ] **Step 3: Write `translated.test.js`**

```js
import { describe, it, expect } from 'vitest'

const en = import.meta.glob('./locales/en/*.json', { eager: true })
const zh = import.meta.glob('./locales/zh-CN/*.json', { eager: true })
const load = (mods, ns) =>
  Object.entries(mods).find(([p]) => p.endsWith(`/${ns}.json`))?.[1].default

// Representative keys appended by each page task. Each entry: [ns, dottedKey].
export const REPRESENTATIVE = [
  ['common', 'actions.save'],
]

describe('zh-CN is actually translated', () => {
  it('representative keys differ from English and are non-empty', () => {
    for (const [ns, key] of REPRESENTATIVE) {
      const e = key.split('.').reduce((o, k) => o?.[k], load(en, ns))
      const z = key.split('.').reduce((o, k) => o?.[k], load(zh, ns))
      expect(z, `${ns}.${key} empty`).toBeTruthy()
      expect(z, `${ns}.${key} not translated`).not.toBe(e)
    }
  })
})
```

- [ ] **Step 4: Run tests**

Run: `cd frontend && npx vitest run src/i18n/keyParity.test.js src/i18n/translated.test.js`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/i18n/locales/GLOSSARY.md frontend/src/i18n/keyParity.test.js frontend/src/i18n/translated.test.js
git commit -m "test(i18n): glossary + en/zh-CN key parity and translation guards (#248)"
```

---

### Task 6: Language selector on Settings

**Files:**
- Create: `frontend/src/components/settings/LanguageSelector.jsx`
- Modify: `frontend/src/pages/Settings.jsx` (render the selector)
- Modify: `frontend/src/i18n/locales/en/settings.json` (create)
- Modify: `frontend/src/i18n/locales/zh-CN/settings.json` (create)
- Modify: `frontend/src/i18n/index.js` (register `settings` namespace)
- Test: `frontend/src/components/settings/LanguageSelector.test.jsx`

**Interfaces:**
- Consumes: `applyLocale`, `storeLocale`, `readStoredLocale` from `../../i18n/resolveLocale`; `SUPPORTED_LOCALES` from `../../i18n`.
- Produces: default-export `LanguageSelector` — a `<select>` of supported locales; onChange persists + applies immediately. Available to ALL roles (not gated by config-write permission).

- [ ] **Step 1: Create settings catalogs**

`frontend/src/i18n/locales/en/settings.json`:
```json
{
  "language": {
    "label": "Language",
    "help": "Choose the language for the AgentGuard interface. This is saved in your browser.",
    "english": "English",
    "chinese": "简体中文"
  }
}
```
`frontend/src/i18n/locales/zh-CN/settings.json`:
```json
{
  "language": {
    "label": "语言",
    "help": "选择 AgentGuard 界面的语言。该设置保存在您的浏览器中。",
    "english": "English",
    "chinese": "简体中文"
  }
}
```

- [ ] **Step 2: Register the namespace in `index.js`**

Import the two files and add to `resources` + `ns`:
```js
import enSettings from './locales/en/settings.json'
import zhSettings from './locales/zh-CN/settings.json'
// resources.en.settings = enSettings; resources['zh-CN'].settings = zhSettings
// ns: ['common', 'settings']
```
(Edit the `resources` object and `ns` array accordingly.)

- [ ] **Step 3: Write the failing test**

`frontend/src/components/settings/LanguageSelector.test.jsx`:
```jsx
import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import i18n from '../../i18n'
import LanguageSelector from './LanguageSelector.jsx'

beforeEach(async () => { localStorage.clear(); await i18n.changeLanguage('en') })

describe('LanguageSelector', () => {
  it('switches the app language and persists the choice', () => {
    render(<LanguageSelector />)
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'zh-CN' } })
    expect(i18n.language).toBe('zh-CN')
    expect(localStorage.getItem('agentguard.locale')).toBe('zh-CN')
  })
})
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/components/settings/LanguageSelector.test.jsx`
Expected: FAIL — cannot resolve `./LanguageSelector.jsx`.

- [ ] **Step 5: Implement `LanguageSelector.jsx`**

```jsx
import { useTranslation } from 'react-i18next'
import { SUPPORTED_LOCALES } from '../../i18n'
import { applyLocale, storeLocale, readStoredLocale } from '../../i18n/resolveLocale'

const LABEL_KEY = { en: 'language.english', 'zh-CN': 'language.chinese' }

export default function LanguageSelector() {
  const { t, i18n } = useTranslation('settings')
  const current = readStoredLocale() || i18n.language || 'en'

  const onChange = (e) => {
    const loc = e.target.value
    storeLocale(loc)
    applyLocale(loc)
  }

  return (
    <div className="flex flex-col gap-1">
      <label htmlFor="ag-language" className="text-xs font-medium text-gray-300">
        {t('language.label')}
      </label>
      <select
        id="ag-language"
        value={SUPPORTED_LOCALES.includes(current) ? current : 'en'}
        onChange={onChange}
        className="bg-gray-700 text-gray-100 rounded-sm px-3 py-2 text-sm w-56"
      >
        {SUPPORTED_LOCALES.map((loc) => (
          <option key={loc} value={loc}>{t(LABEL_KEY[loc])}</option>
        ))}
      </select>
      <p className="text-xs text-gray-400">{t('language.help')}</p>
    </div>
  )
}
```

- [ ] **Step 6: Render the selector on Settings**

In `frontend/src/pages/Settings.jsx`, import it and render it near the top of the settings content (above the config sections, so it's reachable by read-only roles):
```jsx
import LanguageSelector from '../components/settings/LanguageSelector'
// ...in the JSX, inside the main content wrapper, before the SettingsNav/section grid:
<div className="mb-6"><LanguageSelector /></div>
```

- [ ] **Step 7: Add representative key to translated test**

In `frontend/src/i18n/translated.test.js`, extend `REPRESENTATIVE`:
```js
  ['settings', 'language.label'],
```

- [ ] **Step 8: Run tests + build**

Run: `cd frontend && npx vitest run src/components/settings/LanguageSelector.test.jsx src/i18n/keyParity.test.js src/i18n/translated.test.js && npm run build`
Expected: PASS + build succeeds.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/components/settings/LanguageSelector.jsx frontend/src/components/settings/LanguageSelector.test.jsx frontend/src/pages/Settings.jsx frontend/src/i18n
git commit -m "feat(i18n): Settings language selector (personal, localStorage) (#248)"
```

---

## Phase 2 — Full-sweep string extraction

Every task below: (a) register its namespace in `src/i18n/index.js` (`resources` + `ns` array) if new, (b) follow the **Canonical Extraction Procedure**, (c) append one representative key to `REPRESENTATIVE` in `translated.test.js`, (d) run the **Per-task verification**, (e) commit with message `feat(i18n): translate <area> (#248)`.

Representative before→after (illustrative — apply to ALL literals in the listed files):
```jsx
// before
<h1 className="text-xl">Threats</h1>
<button>Acknowledge</button>
// after — const { t } = useTranslation('threats')
<h1 className="text-xl">{t('title')}</h1>
<button>{t('actions.acknowledge')}</button>
```

### Task 7: App shell chrome → `common`
**Files:** Modify `frontend/src/components/{Sidebar,TopNav,Footer,PageHeader}.jsx`; extend `locales/{en,zh-CN}/common.json`; register nothing new (common exists).
Nav item labels (Dashboard, Agents, Threats, Cost, Rules, Investigate, DLP, Sentry, Settings), footer status text, and any `aria-label`s. Representative key: `common.nav.dashboard`.

### Task 8: Auth pages → `auth`
**Files:** Modify `frontend/src/pages/{Login,AccessControl}.jsx`, `frontend/src/components/NoAccess.jsx`; create `locales/{en,zh-CN}/auth.json`; register `auth` ns. Representative: `auth.login.submit`.

### Task 9: Dashboard → `dashboard`
**Files:** Modify `frontend/src/pages/Dashboard.jsx`, `frontend/src/components/{ThreatAnalytics,LiveThreatFeed,LiveAgentActivity}.jsx`; create `dashboard` catalogs; register ns. Representative: `dashboard.title`.

### Task 10: Agents → `agents`
**Files:** Modify `frontend/src/pages/{AgentList,AgentDetail}.jsx`; create `agents` catalogs; register ns. Include session/run/trace chrome labels. Representative: `agents.list.title`.

### Task 11: Threats → `threats`
**Files:** Modify `frontend/src/pages/{Threats,ThreatDetail}.jsx`, `frontend/src/components/{ThreatsList,Badge}.jsx`; create `threats` catalogs; register ns. For `Badge`, translate the human labels of `SeverityBadge`/`StatusBadge` (keep the underlying value string from the API untranslated — map value→label via keys, e.g. `threats.severity.high`). Representative: `threats.title`.

### Task 12: Rules → `rules`
**Files:** Modify `frontend/src/pages/{RulePacks,RuleCreate,RuleDetail}.jsx`, `frontend/src/components/BlockPolicySelect.jsx`, everything under `frontend/src/components/rules/`; create `rules` catalogs; register ns. Do NOT translate rule-catalog names/descriptions/SQL coming from the API — only UI chrome (buttons, tab labels, policy names like log-only/auto-block/hold, MV-health status labels). Representative: `rules.title`.

### Task 13: Cost → `cost`
**Files:** Modify `frontend/src/pages/CostGovernance.jsx`; create `cost` catalogs; register ns. Route currency/number via `formatCurrency`/`formatNumber`. Representative: `cost.title`.

### Task 14: DLP → `dlp`
**Files:** Modify `frontend/src/pages/SemanticDLP.jsx`, `frontend/src/components/{DLPConfigEditor,DLPEnableWizard}.jsx`; create `dlp` catalogs; register ns. Chrome only — funnel-stage labels, form labels, wizard steps. Representative: `dlp.title`.

### Task 15: Sentry → `sentry`
**Files:** Modify `frontend/src/pages/Sentry.jsx`, `frontend/src/components/SentryConfigEditor.jsx`; create `sentry` catalogs; register ns. Translate chat UI chrome (input placeholder, send button, approval prompts, conversation-list labels) — NOT streamed model output or tool results. Representative: `sentry.composer.placeholder`.

### Task 16: Settings body → `settings`
**Files:** Modify `frontend/src/pages/Settings.jsx`, `frontend/src/components/settings/{SettingsNav,SettingsSection}.jsx`; extend `settings` catalogs. Translate page chrome, section titles, save/banner/reprovision text, dirty-count sr-only strings. Config-key labels/descriptions come from the API (`config.Registry()`), so they stay English in v1 (backend scope). Representative: `settings.save`.

### Task 17: Onboarding + Setup → `onboarding`
**Files:** Modify `frontend/src/pages/{Onboarding,Setup}.jsx`; create `onboarding` catalogs; register ns. Wizard step titles, instructions, buttons. Representative: `onboarding.title`.

### Task 18: Remaining shared components → `common`
**Files:** Modify `frontend/src/components/{HoldToast,ActivityDrawer,ActivityLog,NotificationsProvider,Toggle,Markdown}.jsx`; extend `common`. Toast/notification chrome (not the alert payload text from the API), activity column headers, Toggle on/off `aria` labels. Representative: `common.activity.title`.

---

## Phase 3 — Finalize

### Task 19: Sweep audit, lint, PR

**Files:** none new (audit + fixes only).

- [ ] **Step 1: Residual-string audit**

Run:
```bash
cd frontend && grep -rnE ">[[:space:]]*[A-Z][a-zA-Z]+([[:space:]][a-z]+)+[[:space:]]*<" src/pages src/components \
  | grep -v 't(' | grep -vE 'AgentGuard|Timeplus|Sentry|OpenClaw|Claude|OTel|DLP'
```
Expected: no substantive matches. Investigate each hit; extract if it's real UI chrome, leave if it's API-derived/proper-noun.

- [ ] **Step 2: Parity + translation + unit tests**

Run: `cd frontend && npx vitest run`
Expected: all PASS (key parity, translated guards, LanguageSelector, format, init, resolveLocale).

- [ ] **Step 3: Lint + build (both stacks)**

Run: `cd frontend && npm run lint && npm run build` then `cd backend && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test ./internal/config/`
Expected: clean.

- [ ] **Step 4: Manual QA checklist (record results in PR body)**

Start `make dev`. In the browser: Settings → switch to 简体中文 → confirm every page (`/dashboard`, `/agents`, `/threats`, `/cost`, `/rules`, `/investigate`, `/dlp`, `/sentry`, `/settings`, `/onboarding`, `/login`) renders Chinese chrome; reload → choice persists; set `ui.default_language: zh-CN` in config, clear `localStorage`, reload while authenticated → defaults to Chinese; `<html lang>` updates with the switch.

- [ ] **Step 5: Push + open PR**

```bash
git push -u origin feature/248-i18n
gh pr create --title "i18n v1: switchable Chinese UI (#248)" \
  --body "Implements #248. Frontend static-UI i18n via react-i18next; Settings language selector; ui.default_language config default; full zh-CN translation. Backend-originated strings out of scope. See docs/superpowers/specs/2026-07-28-i18n-chinese-ui-design.md."
```

---

## Self-Review notes

- **Spec coverage:** library/init (T1), catalogs+parity (T5), locale precedence localStorage>config>en (T3 config, T4 resolution), switcher in Settings (T6), full-sweep all pages+components (T7–T18), Intl formatting (T2 + used across pages), `<html lang>` (T4), glossary (T5), key-parity test (T5), third-language-is-drop-in (namespace-glob tests, no page code needed). All acceptance criteria mapped.
- **Type consistency:** `applyLocale`/`storeLocale`/`readStoredLocale`/`resolveInitialLocale`/`adoptServerDefault` names are used identically in T4 and T6. `SUPPORTED_LOCALES`/`DEFAULT_LOCALE` exported in T1, consumed in T4/T6. `REPRESENTATIVE` export defined in T5, extended in T6–T18.
- **No placeholders:** all foundation tasks carry full code; Phase-2 tasks share the one Canonical Extraction Procedure with concrete file lists + representative keys (mechanical, per-file identical transform).
