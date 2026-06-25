import { useEffect, useMemo, useState } from 'react'
import ParamField from './ParamField'
import { api } from '../../api/client'

// ConfigurationTab renders the editable parameter form for an installed rule.
// It owns edit state, client-side validation (immediate feedback; the backend is
// authoritative and its 400 field errors are merged on save), reset-one/reset-all,
// dirty tracking with unsaved-change protection, and the PUT save.
//
// Props:
//   ruleId    – rule id
//   schema    – ordered array of parameter definitions
//   defaults  – { key: normalizedDefault }
//   effective – { key: effectiveValue } (defaults overlaid with current overrides)
//   onSaved(updatedInstance) – called after a successful configuration save
export default function ConfigurationTab({ ruleId, schema, defaults, effective, onSaved }) {
  // baseline is the last-saved effective config; edits diff against it.
  const baseline = useMemo(() => buildInitial(schema, effective, defaults), [schema, effective, defaults])
  const [values, setValues] = useState(baseline)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState(null)
  const [fieldErrorsFromServer, setFieldErrorsFromServer] = useState({})

  // Re-seed when the rule reloads (e.g. after a save elsewhere).
  useEffect(() => {
    setValues(baseline)
    setFieldErrorsFromServer({})
    setSaveError(null)
  }, [baseline])

  const clientErrors = useMemo(() => validateAll(schema, values), [schema, values])
  const errors = { ...clientErrors, ...fieldErrorsFromServer }
  const dirty = useMemo(() => !deepEqual(values, baseline), [values, baseline])
  const hasErrors = Object.keys(clientErrors).length > 0

  // Unsaved-change protection: warn on browser navigation away while dirty.
  useEffect(() => {
    if (!dirty) return undefined
    const handler = (e) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  }, [dirty])

  if (!schema || schema.length === 0) {
    return (
      <p className="text-xs text-gray-400">
        This rule has no configurable parameters. Its detection SQL is fixed.
      </p>
    )
  }

  const setOne = (id, v) => {
    setValues((prev) => ({ ...prev, [id]: v }))
    setFieldErrorsFromServer((prev) => (prev[id] ? { ...prev, [id]: undefined } : prev))
  }
  const resetOne = (id) => setOne(id, defaults[id])
  const resetAll = () => {
    const next = {}
    for (const p of schema) next[p.key] = defaults[p.key]
    setValues(next)
    setFieldErrorsFromServer({})
  }
  // Discard reverts to the last-saved baseline (effective config with its saved
  // overrides), NOT to defaults — discarding edits must not wipe saved overrides.
  // After this, values === baseline so the form is clean (dirty becomes false).
  const discard = () => {
    setValues(baseline)
    setFieldErrorsFromServer({})
    setSaveError(null)
  }

  const save = async () => {
    if (hasErrors || saving) return
    setSaving(true)
    setSaveError(null)
    setFieldErrorsFromServer({})
    try {
      // Send the full edited map; the backend canonicalizes (drops values equal
      // to the current default) and resets any omitted key to its default.
      const updated = await api.configureRule(ruleId, values, 'configure via UI')
      onSaved?.(updated)
    } catch (e) {
      // Surface per-parameter 400 field errors next to their controls.
      const fields = e?.body?.fields
      if (Array.isArray(fields)) {
        const m = {}
        for (const f of fields) m[f.parameter] = f.message
        setFieldErrorsFromServer(m)
      }
      // 409 means an upgrade became available since this tab opened (the page
      // normally gates configuration behind Upgrade). The 409 body carries
      // conflicts/warnings, not field errors — give a clear, actionable message
      // rather than a raw banner.
      if (e?.status === 409) {
        setSaveError('This rule has a pending upgrade. Upgrade it first (Configuration is locked until then); reload the page to continue.')
      } else {
        setSaveError(e?.message || 'Save failed')
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-[11px] text-gray-400">
          Edit typed parameters. Values are validated and the detection SQL is re-rendered on save.
        </p>
        <button
          type="button"
          onClick={resetAll}
          disabled={!dirty && !someOverridden(values, defaults)}
          className="text-[10px] uppercase tracking-wide text-gray-400 hover:text-[#ffb0cf] transition-colors disabled:opacity-40"
        >
          Reset all to defaults
        </button>
      </div>

      <div className="space-y-3">
        {schema.map((p) => (
          <ParamField
            key={p.key}
            param={p}
            value={values[p.key]}
            defaultValue={defaults[p.key]}
            overridden={!deepEqual(values[p.key], defaults[p.key])}
            error={errors[p.key]}
            onChange={(v) => setOne(p.key, v)}
            onReset={() => resetOne(p.key)}
          />
        ))}
      </div>

      {saveError && (
        <div className="bg-red-500/10 border border-red-500/20 rounded-sm px-4 py-3 flex items-start gap-3">
          <span className="material-symbols-outlined text-red-400 text-[18px] shrink-0 mt-0.5">error</span>
          <p className="text-xs text-red-400 break-all">{saveError}</p>
        </div>
      )}

      {/* Save bar (sticky-ish): only actionable when there are unsaved, valid changes. */}
      <div className="flex items-center justify-end gap-3 pt-2">
        {dirty && <span className="text-[11px] text-amber-400 mr-auto">Unsaved changes</span>}
        <button
          type="button"
          onClick={discard}
          disabled={!dirty}
          className="px-4 py-2 text-xs font-semibold rounded-sm bg-zinc-700 text-white hover:bg-zinc-600 transition-colors disabled:opacity-40"
        >
          Discard
        </button>
        <button
          type="button"
          onClick={save}
          disabled={!dirty || hasErrors || saving}
          className="px-4 py-2 text-xs font-semibold rounded-sm bg-primary-button text-primary-button-text transition-colors disabled:opacity-40"
        >
          {saving ? 'Saving…' : 'Save configuration'}
        </button>
      </div>
    </div>
  )
}

// buildInitial seeds the editable map from effective values, falling back to
// defaults; arrays are copied so edits don't mutate props.
function buildInitial(schema, effective, defaults) {
  const out = {}
  for (const p of schema || []) {
    const v = effective?.[p.key] ?? defaults?.[p.key]
    out[p.key] = Array.isArray(v) ? [...v] : v
  }
  return out
}

function someOverridden(values, defaults) {
  return Object.keys(values).some((key) => !deepEqual(values[key], defaults[key]))
}

// validateAll mirrors the backend's per-type constraints for immediate feedback.
// The backend remains authoritative (its field errors are merged on save).
function validateAll(schema, values) {
  const errs = {}
  for (const p of schema || []) {
    const e = validateOne(p, values[p.key])
    if (e) errs[p.key] = e
  }
  return errs
}

function validateOne(p, v) {
  switch (p.type) {
    case 'integer':
    case 'float': {
      if (v === '' || v == null) return 'value is required'
      const n = Number(v)
      if (Number.isNaN(n) || !Number.isFinite(n)) return 'must be a finite number'
      if (p.type === 'integer' && !Number.isInteger(n)) return 'must be a whole number'
      if (p.min != null && n < p.min) return `must be ≥ ${p.min}`
      if (p.max != null && n > p.max) return `must be ≤ ${p.max}`
      return null
    }
    case 'string': {
      const s = v ?? ''
      if (typeof s !== 'string') return 'must be a string'
      const len = [...s].length
      if (p.min_length != null && len < p.min_length) return `must be ≥ ${p.min_length} characters`
      if (p.max_length != null && len > p.max_length) return `must be ≤ ${p.max_length} characters`
      return null
    }
    case 'choice':
      if (!p.options?.includes(v)) return 'must be one of the allowed options'
      return null
    case 'list': {
      const list = Array.isArray(v) ? v : []
      if (list.length === 0) return 'must contain at least one item'
      if (new Set(list).size !== list.length) return 'items must be unique'
      if (p.min_items != null && list.length < p.min_items) return `must contain ≥ ${p.min_items} items`
      if (p.max_items != null && list.length > p.max_items) return `must contain ≤ ${p.max_items} items`
      return null
    }
    case 'regex': {
      const s = v ?? ''
      if (typeof s !== 'string') return 'must be a string'
      if (s === '') return 'pattern is required'
      const len = [...s].length
      if (p.min_length != null && len < p.min_length) return `must be ≥ ${p.min_length} characters`
      if (p.max_length != null && len > p.max_length) return `must be ≤ ${p.max_length} characters`
      // Do NOT compile the pattern client-side: the backend validates it as RE2
      // (authoritative), and JS RegExp syntax diverges from RE2 (e.g. inline flags
      // like `(?m)`), so `new RegExp` would falsely block valid AgentGuard
      // patterns — and clientErrors gates the Save button. Let the server return
      // the authoritative field error on save.
      return null
    }
    case 'duration': {
      if (v === '' || v == null) return 'value is required'
      const n = Number(v)
      if (!Number.isInteger(n)) return 'must be a whole number'
      if (p.min != null && n < p.min) return `must be ≥ ${p.min}`
      if (p.max != null && n > p.max) return `must be ≤ ${p.max}`
      return null
    }
    case 'bool':
    default:
      return null
  }
}

function deepEqual(a, b) {
  if (a === b) return true
  if (Array.isArray(a) && Array.isArray(b)) {
    return a.length === b.length && a.every((x, i) => x === b[i])
  }
  // Numbers may arrive as json.Number-decoded strings vs JS numbers; compare loosely.
  if (typeof a !== typeof b) return String(a) === String(b)
  return false
}
