# Settings Page Tabbed Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Settings page's single long scroll with a left sticky section nav + one-section-at-a-time tabbed content, with per-section dirty/restart/reprovision badges and a restructured DLP section (issue #199).

**Architecture:** Frontend-only. Pure nav-state helpers live in a vitest-tested util module (`src/utils/settingsNav.js`). Two new presentational components (`SettingsNav`, `SettingsSection`) render the nav and one section; `Settings.jsx` keeps all state ownership (drafts, save/discard, banners, test-connection, re-provision) and stores the active tab in the URL hash.

**Tech Stack:** React 18, react-router-dom v6 (`useLocation`/`useNavigate`), Tailwind, vitest (logic tests only — no DOM test library in this repo).

**Spec:** `docs/superpowers/specs/2026-07-03-settings-ux-design.md`

## Global Constraints

- No backend/API changes. `/api/config` already returns per-key `restart`/`reprovision` flags and global `restart_required`/`reprovision_required`.
- Design system ("Digital Sentry"): no border lines for sectioning, no drop shadows, tonal layering (bg tier shifts), border radius ≤ 4px (`rounded-sm`) except count chips (`rounded-full`). The DLP card's existing `border-t` must be removed.
- Preserve behavior exactly: draft/save/discard with the fixed bottom bar (global across tabs), per-key source chips, secret handling (never echoed, blank = keep), Timeplus test-connection using draft-or-saved values, field-level validation errors, DLP re-provision flow, top banners.
- `DLPConfigEditor.jsx` must not be modified.
- Frontend commands run from `frontend/`: `npm test` (vitest), `npm run lint`, `npm run build`.
- All commits on branch `feature/199-settings-page-ux-better-grouping-and-navigation-of-settings-sections`.

---

### Task 1: Pure nav-state helpers (`settingsNav.js`) — TDD

**Files:**
- Create: `frontend/src/utils/settingsNav.js`
- Test: `frontend/src/utils/settingsNav.test.js`

**Interfaces:**
- Consumes: nothing (pure functions over the `/api/config` response shape: `sections: [{id, title, keys: [{key, restart, reprovision, ...}]}]`).
- Produces (used by Task 3):
  - `keyToSectionMap(sections) -> Object<string,string>` — dotted key → section id.
  - `dirtyCountsBySection(drafts, keyToSection) -> Object<string,number>` — section id → count of dirty keys (keys of `drafts` present in the map).
  - `sectionIdsWithFlag(sections, flag) -> Set<string>` — ids of sections containing ≥1 key where `key[flag]` is truthy (`flag` is `'restart'` or `'reprovision'`).
  - `activeSectionFromHash(hash, sections) -> string|null` — strips a leading `#`; returns the matching section id, else the first section's id, else `null` when sections are empty/undefined.

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/utils/settingsNav.test.js`:

```js
import { describe, it, expect } from 'vitest'
import {
  keyToSectionMap,
  dirtyCountsBySection,
  sectionIdsWithFlag,
  activeSectionFromHash,
} from './settingsNav.js'

// Shape mirrors GET /api/config: sections -> keys, with static per-key
// restart/reprovision flags (see backend/internal/handlers/settings.go).
const sections = [
  { id: 'server', title: 'Server', keys: [{ key: 'server.port', restart: true }] },
  { id: 'timeplus', title: 'Timeplus Connection', keys: [{ key: 'timeplus.host' }, { key: 'timeplus.port' }] },
  { id: 'dlp', title: 'Semantic DLP', keys: [{ key: 'dlp.enabled' }, { key: 'dlp.embedding_model', reprovision: true }] },
]

describe('keyToSectionMap', () => {
  it('maps every dotted key to its section id', () => {
    const m = keyToSectionMap(sections)
    expect(m['server.port']).toBe('server')
    expect(m['timeplus.host']).toBe('timeplus')
    expect(m['dlp.embedding_model']).toBe('dlp')
  })

  it('returns an empty map for undefined sections', () => {
    expect(keyToSectionMap(undefined)).toEqual({})
  })
})

describe('dirtyCountsBySection', () => {
  const map = keyToSectionMap(sections)

  it('counts dirty keys per section', () => {
    const drafts = { 'timeplus.host': 'x', 'timeplus.port': 1234, 'dlp.enabled': true }
    expect(dirtyCountsBySection(drafts, map)).toEqual({ timeplus: 2, dlp: 1 })
  })

  it('ignores draft keys not present in any section', () => {
    expect(dirtyCountsBySection({ 'ghost.key': 1 }, map)).toEqual({})
  })

  it('returns an empty object for no drafts', () => {
    expect(dirtyCountsBySection({}, map)).toEqual({})
    expect(dirtyCountsBySection(undefined, map)).toEqual({})
  })
})

describe('sectionIdsWithFlag', () => {
  it('collects section ids containing a restart-flagged key', () => {
    expect(sectionIdsWithFlag(sections, 'restart')).toEqual(new Set(['server']))
  })

  it('collects section ids containing a reprovision-flagged key', () => {
    expect(sectionIdsWithFlag(sections, 'reprovision')).toEqual(new Set(['dlp']))
  })

  it('returns an empty set for undefined sections', () => {
    expect(sectionIdsWithFlag(undefined, 'restart')).toEqual(new Set())
  })
})

describe('activeSectionFromHash', () => {
  it('returns the section id matching the hash', () => {
    expect(activeSectionFromHash('#dlp', sections)).toBe('dlp')
  })

  it('accepts a hash without the leading #', () => {
    expect(activeSectionFromHash('timeplus', sections)).toBe('timeplus')
  })

  it('falls back to the first section for an unknown hash', () => {
    expect(activeSectionFromHash('#nope', sections)).toBe('server')
  })

  it('falls back to the first section for an empty hash', () => {
    expect(activeSectionFromHash('', sections)).toBe('server')
    expect(activeSectionFromHash(undefined, sections)).toBe('server')
  })

  it('returns null when there are no sections', () => {
    expect(activeSectionFromHash('#dlp', [])).toBeNull()
    expect(activeSectionFromHash('#dlp', undefined)).toBeNull()
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/utils/settingsNav.test.js`
Expected: FAIL — `Failed to resolve import "./settingsNav.js"`.

- [ ] **Step 3: Write the implementation**

Create `frontend/src/utils/settingsNav.js`:

```js
// Pure helpers for the Settings page section nav (issue #199).
// All functions operate on the GET /api/config response shape:
// { sections: [{ id, title, keys: [{ key, restart, reprovision, ... }] }] }

export function keyToSectionMap(sections) {
  const map = {}
  for (const s of sections || []) {
    for (const k of s.keys || []) map[k.key] = s.id
  }
  return map
}

export function dirtyCountsBySection(drafts, keyToSection) {
  const counts = {}
  for (const key of Object.keys(drafts || {})) {
    const sid = keyToSection[key]
    if (!sid) continue
    counts[sid] = (counts[sid] || 0) + 1
  }
  return counts
}

export function sectionIdsWithFlag(sections, flag) {
  const out = new Set()
  for (const s of sections || []) {
    if ((s.keys || []).some((k) => k[flag])) out.add(s.id)
  }
  return out
}

export function activeSectionFromHash(hash, sections) {
  const id = (hash || '').replace(/^#/, '')
  if (sections?.some((s) => s.id === id)) return id
  return sections?.[0]?.id ?? null
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && npx vitest run src/utils/settingsNav.test.js`
Expected: PASS (13 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/utils/settingsNav.js frontend/src/utils/settingsNav.test.js
git commit -m "feat(settings): pure helpers for section nav state (#199)"
```

---

### Task 2: `SettingsNav` and `SettingsSection` components

**Files:**
- Create: `frontend/src/components/settings/SettingsNav.jsx`
- Create: `frontend/src/components/settings/SettingsSection.jsx`

**Interfaces:**
- Consumes: `Toggle` (`frontend/src/components/Toggle.jsx`), `DLPConfigEditor` (`frontend/src/components/DLPConfigEditor.jsx`, unchanged).
- Produces (used by Task 3):
  - `SettingsNav({ sections, activeId, dirtyCounts, restartIds, reprovisionIds, showRestart, showReprovision, onSelect })` — default export. `sections` is the array from `/api/config`; `dirtyCounts` an object id→count; `restartIds`/`reprovisionIds` are `Set`s; `showRestart`/`showReprovision` booleans gate the amber dots; `onSelect(id)` is called on click.
  - `SettingsSection({ section, draftFor, setDraft, fieldErrors, tpTest, onTestTimeplus, reprovisioning, onReprovision })` — default export. `draftFor(k)` returns the display value for a key object; `setDraft(key, value)` records an edit; `fieldErrors` maps key→message. `tpTest`/`onTestTimeplus` only used when `section.id === 'timeplus'`; `reprovisioning`/`onReprovision` only when `section.id === 'dlp'`.

There is no DOM test library in this repo — these presentational components are verified by ESLint + the production build in this task and manually in Task 4.

- [ ] **Step 1: Create `SettingsNav.jsx`**

Create `frontend/src/components/settings/SettingsNav.jsx`:

```jsx
// Sticky section nav for the Settings page (issue #199). Purely
// presentational: the page computes active id, dirty counts and flag sets.
export default function SettingsNav({
  sections,
  activeId,
  dirtyCounts,
  restartIds,
  reprovisionIds,
  showRestart,
  showReprovision,
  onSelect,
}) {
  return (
    <nav className="w-48 shrink-0 sticky top-24 self-start flex flex-col gap-1">
      {sections.map((s) => {
        const active = s.id === activeId
        const dirty = dirtyCounts[s.id] || 0
        const restart = showRestart && restartIds.has(s.id)
        const reprovision = showReprovision && reprovisionIds.has(s.id)
        return (
          <button
            key={s.id}
            onClick={() => onSelect(s.id)}
            className={`flex items-center gap-2 px-3 py-2 rounded-sm text-left text-xs font-medium transition-all ${
              active ? 'text-pink-400 bg-gray-700' : 'text-gray-400 hover:bg-gray-700 hover:text-gray-100'
            }`}
          >
            <span className="flex-1 truncate">{s.title}</span>
            {dirty > 0 && (
              <span className="bg-[#ffb0cf] text-[#3d0023] text-[9px] font-bold rounded-full px-1.5 py-0.5 min-w-[16px] text-center">
                {dirty}
              </span>
            )}
            {(restart || reprovision) && (
              <span
                title={restart ? 'Restart required to apply saved changes' : 'Re-provision required to apply model changes'}
                className="w-1.5 h-1.5 rounded-full bg-amber-400 shrink-0"
              />
            )}
          </button>
        )
      })}
    </nav>
  )
}
```

- [ ] **Step 2: Create `SettingsSection.jsx`**

`SourceChip`, `KeyField`, `SOURCE_LABEL` and `DLP_SERVER_KEYS` move here **verbatim** from `frontend/src/pages/Settings.jsx` (they are deleted from the page in Task 3). The DLP card is restructured into three tonal sub-group tiles (`bg-gray-900/40`, no `border-t`).

Create `frontend/src/components/settings/SettingsSection.jsx`:

```jsx
import Toggle from '../Toggle'
import DLPConfigEditor from '../DLPConfigEditor'

// DLP keys that remain server-config (config.yaml): the enable switch and the
// embedding dimension (baked into the stream/index). Everything else — endpoint,
// models, timeouts, tokens, entity catalog — is edited live below via the config
// stream, so it's hidden from the flat key list to avoid two sources of truth.
const DLP_SERVER_KEYS = new Set(['dlp.enabled', 'dlp.embedding_dim'])

const SOURCE_LABEL = {
  default: 'default',
  file: 'config file',
  env: 'environment',
  flag: 'CLI flag',
}

function SourceChip({ k }) {
  const label = SOURCE_LABEL[k.source] || k.source
  const title =
    k.source === 'env' ? `Set by ${k.env_var} — file edits have no effect until the variable is removed`
    : k.source === 'flag' ? `Set by --${k.flag} — file edits have no effect`
    : undefined
  return (
    <span title={title} className="text-[10px] uppercase tracking-wide rounded-full px-2 py-0.5 bg-gray-700 text-gray-300">
      {label}
    </span>
  )
}

function KeyField({ k, draftValue, onChange }) {
  const disabled = !k.editable
  const value = draftValue
  const baseInput =
    'w-full bg-gray-900 text-gray-100 text-sm rounded-sm px-3 py-2 outline-none focus:bg-gray-800 disabled:opacity-50 disabled:cursor-not-allowed'

  let input
  if (k.type === 'bool') {
    input = <Toggle checked={!!value} onChange={(v) => onChange(v)} disabled={disabled} />
  } else if (k.type === 'enum') {
    input = (
      <select className={baseInput} value={value ?? ''} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
        {k.enum_values?.map((opt) => <option key={opt} value={opt}>{opt}</option>)}
      </select>
    )
  } else if (k.type === 'secret') {
    input = (
      <input
        type="password"
        className={baseInput}
        disabled={disabled}
        placeholder={k.set ? '•••••••• (set)' : 'not set'}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
      />
    )
  } else if (k.type === 'int') {
    input = (
      <input
        type="number"
        className={baseInput}
        disabled={disabled}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value === '' ? '' : Number(e.target.value))}
      />
    )
  } else {
    input = (
      <input
        type="text"
        className={baseInput}
        disabled={disabled}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
      />
    )
  }

  return (
    <div className="py-3">
      <div className="flex items-center justify-between gap-3 mb-1">
        <div className="flex items-center gap-2">
          <span className="text-sm text-gray-100">{k.label}</span>
          <SourceChip k={k} />
          {k.restart && <span className="text-[10px] text-amber-400">requires restart</span>}
          {k.reprovision && <span className="text-[10px] text-amber-400">requires re-provision</span>}
        </div>
      </div>
      <p className="text-xs text-gray-400 mb-2">{k.description}</p>
      <div className="max-w-md">{input}</div>
    </div>
  )
}

// Renders one settings section card. Section-specific extras: the Timeplus
// test-connection row, and the DLP card's three tonal sub-groups
// (server config / runtime config / operations).
export default function SettingsSection({
  section,
  draftFor,
  setDraft,
  fieldErrors,
  tpTest,
  onTestTimeplus,
  reprovisioning,
  onReprovision,
}) {
  const keys = section.id === 'dlp'
    ? section.keys.filter((k) => DLP_SERVER_KEYS.has(k.key))
    : section.keys

  const keyList = (
    <div className="divide-y divide-gray-700/50">
      {keys.map((k) => (
        <div key={k.key}>
          <KeyField k={k} draftValue={draftFor(k)} onChange={(v) => setDraft(k.key, v)} />
          {fieldErrors[k.key] && <p className="text-error text-xs pb-2">{fieldErrors[k.key]}</p>}
        </div>
      ))}
    </div>
  )

  if (section.id !== 'dlp') {
    return (
      <section className="bg-gray-800 rounded-sm px-6 py-4">
        <h3 className="text-base font-medium text-gray-100 mb-2">{section.title}</h3>
        {keyList}
        {section.id === 'timeplus' && (
          <div className="mt-3 flex items-center gap-3">
            <button onClick={onTestTimeplus} className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600">Test connection</button>
            {tpTest?.pending && <span className="text-xs text-gray-400">Testing…</span>}
            {tpTest && !tpTest.pending && (
              <span className={`text-xs ${tpTest.ok ? 'text-emerald-400' : 'text-error'}`}>
                {tpTest.ok ? 'Connection OK' : `Failed: ${tpTest.error}`}
              </span>
            )}
          </div>
        )}
      </section>
    )
  }

  return (
    <section className="bg-gray-800 rounded-sm px-6 py-4">
      <h3 className="text-base font-medium text-gray-100 mb-3">{section.title}</h3>
      <div className="flex flex-col gap-4">
        <div className="bg-gray-900/40 rounded-sm px-4 py-3">
          <h4 className="text-sm font-semibold text-gray-100 mb-1">Server config</h4>
          <p className="text-xs text-gray-400">
            Boot defaults stored in config.yaml — saved with the Save bar below. All other DLP settings are edited live under Runtime configuration.
          </p>
          {keyList}
        </div>
        <div className="bg-gray-900/40 rounded-sm px-4 py-3">
          <h4 className="text-sm font-semibold text-gray-100 mb-1">Runtime configuration</h4>
          <p className="text-xs text-gray-400 mb-2">
            Live state in the agentguard_dlp_config stream — changes apply immediately, with no restart or re-provision, independent of the Save bar.
          </p>
          <DLPConfigEditor />
        </div>
        <div className="bg-gray-900/40 rounded-sm px-4 py-3">
          <h4 className="text-sm font-semibold text-gray-100 mb-1">Operations</h4>
          <div className="flex items-center gap-3">
            <button onClick={onReprovision} disabled={reprovisioning}
              className="rounded-sm px-3 py-1.5 text-xs bg-gray-700 text-gray-100 hover:bg-gray-600 disabled:opacity-50">
              {reprovisioning ? 'Re-provisioning…' : 'Re-provision DLP'}
            </button>
            <span className="text-[11px] text-gray-500">
              Re-detects the embedding dimension and rebuilds the pipeline. Run after switching the embedding model.
            </span>
          </div>
        </div>
      </div>
    </section>
  )
}
```

- [ ] **Step 3: Lint the new files**

Run: `cd frontend && npm run lint`
Expected: exits 0 (the components are not imported anywhere yet — that's fine, ESLint checks them standalone; unused-import or prop errors would surface here).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/settings/SettingsNav.jsx frontend/src/components/settings/SettingsSection.jsx
git commit -m "feat(settings): SettingsNav and SettingsSection components (#199)"
```

---

### Task 3: Rewire `Settings.jsx` to tabbed layout

**Files:**
- Modify: `frontend/src/pages/Settings.jsx` (full replacement below)

**Interfaces:**
- Consumes: `SettingsNav` and `SettingsSection` (Task 2 signatures), helpers from `../utils/settingsNav` (Task 1 signatures), `useLocation`/`useNavigate` from react-router-dom.
- Produces: the `/settings` route, with active section in the URL hash (`/settings#dlp`).

Key changes from the old file: `KeyField`/`SourceChip`/`SOURCE_LABEL`/`DLP_SERVER_KEYS` and the per-section JSX are gone (moved in Task 2); the sections map is replaced by nav + single active section; container widens `max-w-4xl` → `max-w-5xl` to fit the nav column. All state, handlers, banners and the save bar are unchanged.

- [ ] **Step 1: Replace `frontend/src/pages/Settings.jsx` with:**

```jsx
import { useState, useEffect, useCallback } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import Sidebar from '../components/Sidebar'
import TopNav from '../components/TopNav'
import PageHeader from '../components/PageHeader'
import SettingsNav from '../components/settings/SettingsNav'
import SettingsSection from '../components/settings/SettingsSection'
import {
  keyToSectionMap,
  dirtyCountsBySection,
  sectionIdsWithFlag,
  activeSectionFromHash,
} from '../utils/settingsNav'
import { api } from '../api/client'

export default function Settings() {
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [drafts, setDrafts] = useState({}) // key -> new value
  const [fieldErrors, setFieldErrors] = useState({})
  const [saving, setSaving] = useState(false)
  const [banner, setBanner] = useState(null) // {type, msg}
  const [tpTest, setTpTest] = useState(null)
  const [reprovisioning, setReprovisioning] = useState(false)
  const location = useLocation()
  const navigate = useNavigate()

  const load = useCallback(() => {
    setLoading(true)
    api.getConfig()
      .then((d) => { setData(d); setError(null) })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { load() }, [load])

  const setDraft = (key, value) => {
    setDrafts((d) => ({ ...d, [key]: value }))
    setFieldErrors((e) => { const n = { ...e }; delete n[key]; return n })
  }

  const dirtyKeys = Object.keys(drafts)

  const save = async () => {
    setSaving(true)
    setFieldErrors({})
    try {
      const res = await api.updateConfig(drafts)
      setDrafts({})
      setBanner(res.restart_required
        ? { type: 'warn', msg: 'Saved. Some changes require an AgentGuard restart to take effect.' }
        : { type: 'ok', msg: 'Configuration saved.' })
      load()
    } catch (e) {
      if (e.errors) setFieldErrors(e.errors)
      setBanner({ type: 'error', msg: e.message })
    } finally {
      setSaving(false)
    }
  }

  const discard = () => { setDrafts({}); setFieldErrors({}) }

  const draftFor = (k) => (k.key in drafts ? drafts[k.key] : (k.type === 'secret' ? '' : k.value))

  const testTimeplus = async () => {
    const host = drafts['timeplus.host'] ?? findValue('timeplus.host')
    const port = drafts['timeplus.port'] ?? findValue('timeplus.port')
    setTpTest({ pending: true })
    try {
      const r = await api.testTimeplusConfig(host, Number(port))
      setTpTest(r)
    } catch (e) { setTpTest({ ok: false, error: e.message }) }
  }

  const reprovisionDLP = async () => {
    setReprovisioning(true)
    try {
      await api.syncRules()
      setBanner({ type: 'ok', msg: 'DLP re-provision triggered.' })
      load()
    } catch (e) {
      setBanner({ type: 'error', msg: `Re-provision failed: ${e.message}` })
    } finally {
      setReprovisioning(false)
    }
  }

  const findValue = (key) => {
    for (const s of data?.sections || []) {
      for (const k of s.keys) if (k.key === key) return k.value
    }
    return ''
  }

  const sections = data?.sections || []
  const activeId = activeSectionFromHash(location.hash, sections)
  const activeSection = sections.find((s) => s.id === activeId)
  const dirtyCounts = dirtyCountsBySection(drafts, keyToSectionMap(sections))
  const restartIds = sectionIdsWithFlag(sections, 'restart')
  const reprovisionIds = sectionIdsWithFlag(sections, 'reprovision')

  return (
    <div className="min-h-screen bg-[#15121d] text-gray-100">
      <Sidebar />
      <TopNav />
      <main className="pl-sidebar pt-16 pb-24 min-h-screen">
        <div className="p-8 max-w-5xl">
          <PageHeader title="Settings" subtitle="View and edit AgentGuard server configuration. Keys set by environment variables or CLI flags are read-only." />

          {loading && <p className="text-gray-400 text-sm">Loading…</p>}
          {error && <p className="text-error text-sm">{error}</p>}

          {banner && (
            <div className={`mb-6 rounded-sm px-4 py-3 text-sm ${
              banner.type === 'error' ? 'bg-red-900/40 text-red-300'
              : banner.type === 'warn' ? 'bg-amber-900/40 text-amber-200'
              : 'bg-emerald-900/40 text-emerald-200'}`}>
              {banner.msg}
            </div>
          )}

          {data?.restart_required && (
            <div className="mb-6 rounded-sm px-4 py-3 text-sm bg-amber-900/40 text-amber-200">
              Saved configuration differs from the running server — restart AgentGuard to apply.
            </div>
          )}

          {data?.reprovision_required && (
            <div className="mb-6 rounded-sm px-4 py-3 text-sm bg-amber-900/40 text-amber-200 flex items-center justify-between gap-4">
              <span>DLP model settings differ from what&apos;s provisioned. Re-provision to apply baked-in model/dimension changes. (Changing the embedding dimension does not recreate the existing stream automatically.)</span>
              <button onClick={reprovisionDLP} disabled={reprovisioning}
                className="shrink-0 rounded-sm px-3 py-1.5 text-xs bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-60">
                {reprovisioning ? 'Re-provisioning…' : 'Re-provision DLP now'}
              </button>
            </div>
          )}

          {sections.length > 0 && (
            <div className="flex gap-6 items-start">
              <SettingsNav
                sections={sections}
                activeId={activeId}
                dirtyCounts={dirtyCounts}
                restartIds={restartIds}
                reprovisionIds={reprovisionIds}
                showRestart={!!data?.restart_required}
                showReprovision={!!data?.reprovision_required}
                onSelect={(id) => navigate(`#${id}`, { replace: true })}
              />
              <div className="flex-1 min-w-0">
                {activeSection && (
                  <SettingsSection
                    section={activeSection}
                    draftFor={draftFor}
                    setDraft={setDraft}
                    fieldErrors={fieldErrors}
                    tpTest={tpTest}
                    onTestTimeplus={testTimeplus}
                    reprovisioning={reprovisioning}
                    onReprovision={reprovisionDLP}
                  />
                )}
              </div>
            </div>
          )}
        </div>

        {dirtyKeys.length > 0 && (
          <div className="fixed bottom-0 left-0 right-0 pl-sidebar bg-gray-800/95 backdrop-blur-md py-3 z-30 flex items-center justify-between [&>*:first-child]:pl-10 [&>*:last-child]:pr-10">
            <span className="text-sm text-gray-300">{dirtyKeys.length} unsaved change{dirtyKeys.length > 1 ? 's' : ''}</span>
            <div className="flex gap-3">
              <button onClick={discard} disabled={saving} className="rounded-sm px-4 py-2 text-sm bg-gray-700 text-gray-100 hover:bg-gray-600">Discard</button>
              <button onClick={save} disabled={saving}
                className="rounded-sm px-4 py-2 text-sm bg-gradient-to-br from-[#ffb0cf] to-[#f256a3] text-[#3d0023] font-medium disabled:opacity-60">
                {saving ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        )}
      </main>
    </div>
  )
}
```

- [ ] **Step 2: Lint, test, build**

Run: `cd frontend && npm run lint && npm test && npm run build`
Expected: all exit 0. Lint would catch leftover unused imports (`Toggle`, `DLPConfigEditor` must no longer be imported by `Settings.jsx`); build catches broken import paths.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/Settings.jsx
git commit -m "feat(settings): tabbed section navigation with per-section state badges (#199)"
```

---

### Task 4: Manual smoke walkthrough

**Files:** none (verification only).

**Interfaces:** consumes the running app (`make dev` from the repo root: backend :8080 + frontend :5173). Requires a reachable Timeplus for full save/re-provision flows; nav/tab/badge behavior is verifiable even without one.

- [ ] **Step 1: Start the stack**

Run from repo root: `make dev`
Expected: backend on :8080, frontend on :5173.

- [ ] **Step 2: Walk through the checklist at http://localhost:5173/settings**

- Nav lists 7 sections (Server, Timeplus Connection, AgentGuard, Catalog, Approval Holds, Semantic DLP, Setup); first is active by default; only one section card renders at a time.
- Deep link: open `http://localhost:5173/settings#dlp` → DLP tab active; refresh keeps it. Unknown hash (`#zzz`) falls back to the first section.
- Edit a key on Timeplus tab and another on Holds tab → save bar shows "2 unsaved changes"; each tab's nav item shows a count chip of 1; switching tabs preserves both edits. Discard clears bar and chips.
- Save an edit to `server.port` → restart banner appears **and** an amber dot appears on the Server nav item (and Catalog, the other restart-flagged section).
- Timeplus tab: "Test connection" works using drafted host/port values.
- DLP tab: three sub-group tiles (Server config / Runtime config / Operations) with no border lines; `DLPConfigEditor` loads and saves as before; "Re-provision DLP" works from the Operations tile and (when shown) from the top banner.
- Field validation: enter an invalid value (e.g. `holds.timeout_seconds` = 999999), Save → error shows under the key; if the key is on a non-active tab, its section chip stays visible.

- [ ] **Step 3: Fix anything found, re-run `npm run lint && npm test && npm run build`, amend or commit fixes**

```bash
git add -A frontend/src
git commit -m "fix(settings): walkthrough fixes for tabbed navigation (#199)"
```

(Skip the commit if the walkthrough found nothing.)
